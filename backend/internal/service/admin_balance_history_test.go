package service

import (
	"testing"
	"time"

	dbent "github.com/Wei-Shaw/sub2api/ent"
	"github.com/Wei-Shaw/sub2api/internal/payment"
	"github.com/Wei-Shaw/sub2api/internal/pkg/pagination"
	"github.com/stretchr/testify/require"
)

func TestMergeBalanceHistoryCodesIncludesAffiliateTransfersByDefault(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, 5, 3, 12, 0, 0, 0, time.UTC)
	older := now.Add(-2 * time.Hour)
	newer := now.Add(time.Hour)

	usedBy := int64(10)
	redeemCodes := []RedeemCode{
		{
			ID:        1,
			Type:      RedeemTypeBalance,
			Value:     8,
			Status:    StatusUsed,
			UsedBy:    &usedBy,
			UsedAt:    &now,
			CreatedAt: now,
		},
		{
			ID:        2,
			Type:      RedeemTypeConcurrency,
			Value:     1,
			Status:    StatusUsed,
			UsedBy:    &usedBy,
			UsedAt:    &older,
			CreatedAt: older,
		},
	}
	affiliateCodes := []RedeemCode{
		{
			ID:        -20,
			Type:      RedeemTypeAffiliateBalance,
			Value:     3.5,
			Status:    StatusUsed,
			UsedBy:    &usedBy,
			UsedAt:    &newer,
			CreatedAt: newer,
		},
	}

	got := mergeBalanceHistoryCodes(redeemCodes, affiliateCodes, pagination.PaginationParams{
		Page:     1,
		PageSize: 2,
	})

	require.Len(t, got, 2)
	require.Equal(t, RedeemTypeAffiliateBalance, got[0].Type)
	require.Equal(t, RedeemTypeBalance, got[1].Type)
}

func TestMergeBalanceHistoryCodesPaginatesAfterCombiningSources(t *testing.T) {
	t.Parallel()

	base := time.Date(2026, 5, 3, 12, 0, 0, 0, time.UTC)
	usedBy := int64(10)
	at := func(hours int) *time.Time {
		v := base.Add(time.Duration(hours) * time.Hour)
		return &v
	}

	got := mergeBalanceHistoryCodes(
		[]RedeemCode{
			{ID: 1, Type: RedeemTypeBalance, UsedBy: &usedBy, UsedAt: at(4), CreatedAt: *at(4)},
			{ID: 2, Type: RedeemTypeConcurrency, UsedBy: &usedBy, UsedAt: at(2), CreatedAt: *at(2)},
		},
		[]RedeemCode{
			{ID: -3, Type: RedeemTypeAffiliateBalance, UsedBy: &usedBy, UsedAt: at(3), CreatedAt: *at(3)},
			{ID: -4, Type: RedeemTypeAffiliateBalance, UsedBy: &usedBy, UsedAt: at(1), CreatedAt: *at(1)},
		},
		pagination.PaginationParams{Page: 2, PageSize: 2},
	)

	require.Len(t, got, 2)
	require.Equal(t, RedeemTypeConcurrency, got[0].Type)
	require.Equal(t, int64(-4), got[1].ID)
}

func TestExcludeRedeemCodesByCodeDropsPurchaseFulfillmentBalanceEntries(t *testing.T) {
	t.Parallel()

	usedBy := int64(10)
	now := time.Date(2026, 6, 9, 10, 0, 0, 0, time.UTC)
	codes := []RedeemCode{
		{
			ID:        1,
			Code:      "PAY-123",
			Type:      RedeemTypeBalance,
			UsedBy:    &usedBy,
			UsedAt:    &now,
			CreatedAt: now,
		},
		{
			ID:        2,
			Code:      "CODE-REAL",
			Type:      RedeemTypeBalance,
			UsedBy:    &usedBy,
			UsedAt:    &now,
			CreatedAt: now,
		},
		{
			ID:        3,
			Code:      "PAY-123",
			Type:      RedeemTypeSubscription,
			UsedBy:    &usedBy,
			UsedAt:    &now,
			CreatedAt: now,
		},
	}

	got := excludeRedeemCodesByCode(codes, map[string]struct{}{"PAY-123": {}})

	require.Len(t, got, 2)
	require.Equal(t, int64(2), got[0].ID)
	require.Equal(t, int64(3), got[1].ID)
}

func TestPaymentOrderHistoryFromOrderIncludesSubscriptionGroup(t *testing.T) {
	t.Parallel()

	groupID := int64(33)
	order := &dbent.PaymentOrder{
		ID:                  42,
		UserID:              10,
		OutTradeNo:          "sub2_test_order",
		OrderType:           payment.OrderTypeSubscription,
		Status:              OrderStatusCompleted,
		SubscriptionGroupID: &groupID,
		CreatedAt:           time.Date(2026, 6, 9, 10, 0, 0, 0, time.UTC),
	}

	got := paymentOrderHistoryFromOrder(order, nil)

	require.Equal(t, RedeemTypeSubscription, got.Type)
	require.NotNil(t, got.GroupID)
	require.Equal(t, groupID, *got.GroupID)
	require.Equal(t, "subscription_purchase", got.Notes)
}
