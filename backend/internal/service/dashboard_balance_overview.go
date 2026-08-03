package service

import (
	"context"
	"errors"
	"time"
)

var ErrBalanceOverviewUnavailable = errors.New("balance overview is unavailable")

// BalanceOverview is the platform-wide balance snapshot shown to administrators.
type BalanceOverview struct {
	TotalAvailableBalance          float64    `json:"total_available_balance"`
	TotalUsageCardAvailableBalance float64    `json:"total_usage_card_available_balance"`
	UsageCardLatestExpiresAt       *time.Time `json:"usage_card_latest_expires_at"`
	GeneratedAt                    time.Time  `json:"generated_at"`
}

type balanceOverviewFetcher interface {
	GetBalanceOverview(ctx context.Context) (*BalanceOverview, error)
}

// GetBalanceOverview fetches a live balance snapshot without changing the
// existing dashboard-statistics contract.
func (s *DashboardService) GetBalanceOverview(ctx context.Context) (*BalanceOverview, error) {
	if s == nil || s.usageRepo == nil {
		return nil, ErrBalanceOverviewUnavailable
	}

	fetcher, ok := s.usageRepo.(balanceOverviewFetcher)
	if !ok {
		return nil, ErrBalanceOverviewUnavailable
	}
	return fetcher.GetBalanceOverview(ctx)
}
