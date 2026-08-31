package repository

import (
	"context"
	"fmt"
)

func (r *usageAggregationRepository) GetUserDashboardStats(ctx context.Context, userID int64) (*UserDashboardStats, error) {
	if r == nil || r.usageLogRepository == nil {
		return nil, fmt.Errorf("usage aggregation repository is not configured")
	}
	if r.userDashboardStats == nil {
		return r.usageLogRepository.GetUserDashboardStats(ctx, userID)
	}
	stats, handled, err := r.userDashboardStats.Get(ctx, userID)
	if err != nil {
		return nil, err
	}
	if handled {
		return stats, nil
	}
	return r.usageLogRepository.GetUserDashboardStats(ctx, userID)
}
