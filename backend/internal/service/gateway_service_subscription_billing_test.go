//go:build unit

package service

import (
	"testing"
)

// TestBuildUsageBillingCommand_SubscriptionAppliesRateMultiplier locks in the fix
// that subscription-mode billing honours the group (and any user-specific) rate
// multiplier — i.e. cmd.SubscriptionCost tracks ActualCost (= TotalCost *
// RateMultiplier), not raw TotalCost.
func TestBuildUsageBillingCommand_SubscriptionAppliesRateMultiplier(t *testing.T) {
	t.Parallel()

	groupID := int64(7)
	subID := int64(42)

	tests := []struct {
		name           string
		totalCost      float64
		actualCost     float64
		isSubscription bool
		wantSub        float64
		wantBalance    float64
	}{
		{
			name:           "subscription with 2x multiplier consumes 2x quota",
			totalCost:      1.0,
			actualCost:     2.0,
			isSubscription: true,
			wantSub:        2.0,
			wantBalance:    0,
		},
		{
			name:           "subscription with 0.5x multiplier consumes 0.5x quota",
			totalCost:      1.0,
			actualCost:     0.5,
			isSubscription: true,
			wantSub:        0.5,
			wantBalance:    0,
		},
		{
			name:           "free subscription (multiplier 0) consumes no quota",
			totalCost:      1.0,
			actualCost:     0,
			isSubscription: true,
			wantSub:        0,
			wantBalance:    0,
		},
		{
			name:           "balance billing keeps using ActualCost (regression)",
			totalCost:      1.0,
			actualCost:     2.0,
			isSubscription: false,
			wantSub:        0,
			wantBalance:    2.0,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			p := &postUsageBillingParams{
				Cost:               &CostBreakdown{TotalCost: tt.totalCost, ActualCost: tt.actualCost},
				User:               &User{ID: 1},
				APIKey:             &APIKey{ID: 2, GroupID: &groupID},
				Account:            &Account{ID: 3},
				Subscription:       &UserSubscription{ID: subID},
				IsSubscriptionBill: tt.isSubscription,
			}

			cmd := buildUsageBillingCommand("req-1", nil, p)
			if cmd == nil {
				t.Fatal("buildUsageBillingCommand returned nil")
			}
			if cmd.SubscriptionCost != tt.wantSub {
				t.Errorf("SubscriptionCost = %v, want %v", cmd.SubscriptionCost, tt.wantSub)
			}
			if cmd.BalanceCost != tt.wantBalance {
				t.Errorf("BalanceCost = %v, want %v", cmd.BalanceCost, tt.wantBalance)
			}
		})
	}
}

func TestBuildUsageBillingCommand_UsageCardCostTracksActualCost(t *testing.T) {
	t.Parallel()

	groupID := int64(7)

	tests := []struct {
		name                string
		usageCardEnabled    bool
		usageCardDisabled   bool
		wantUsageCardCost   float64
		wantUsageCardEnable bool
	}{
		{
			name:                "enabled balance billing exposes usage card cost",
			usageCardEnabled:    true,
			usageCardDisabled:   false,
			wantUsageCardCost:   2.0,
			wantUsageCardEnable: true,
		},
		{
			name:                "disabled group suppresses usage card cost",
			usageCardEnabled:    true,
			usageCardDisabled:   true,
			wantUsageCardCost:   0,
			wantUsageCardEnable: false,
		},
		{
			name:                "global off suppresses usage card cost",
			usageCardEnabled:    false,
			usageCardDisabled:   false,
			wantUsageCardCost:   0,
			wantUsageCardEnable: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			p := &postUsageBillingParams{
				Cost:                    &CostBreakdown{TotalCost: 1.0, ActualCost: 2.0},
				User:                    &User{ID: 1},
				APIKey:                  &APIKey{ID: 2, GroupID: &groupID, Group: &Group{ID: groupID, UsageCardDisabled: tt.usageCardDisabled}},
				Account:                 &Account{ID: 3},
				UsageCardBillingEnabled: tt.usageCardEnabled,
				IsSubscriptionBill:      false,
			}

			cmd := buildUsageBillingCommand("req-usage-card", nil, p)
			if cmd == nil {
				t.Fatal("buildUsageBillingCommand returned nil")
			}
			if cmd.BalanceCost != 2.0 {
				t.Errorf("BalanceCost = %v, want 2", cmd.BalanceCost)
			}
			if cmd.UsageCardCost != tt.wantUsageCardCost {
				t.Errorf("UsageCardCost = %v, want %v", cmd.UsageCardCost, tt.wantUsageCardCost)
			}
			if cmd.UsageCardBillingEnabled != tt.wantUsageCardEnable {
				t.Errorf("UsageCardBillingEnabled = %v, want %v", cmd.UsageCardBillingEnabled, tt.wantUsageCardEnable)
			}
		})
	}
}
