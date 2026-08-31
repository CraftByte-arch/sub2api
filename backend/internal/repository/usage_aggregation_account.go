package repository

import (
	"context"
	"fmt"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/pkg/usagestats"
)

func (r *usageAggregationRepository) GetAccountUsageStats(
	ctx context.Context,
	accountID int64,
	startTime, endTime time.Time,
) (*usagestats.AccountUsageStatsResponse, error) {
	if r == nil || r.usageLogRepository == nil {
		return nil, fmt.Errorf("usage aggregation repository is not configured")
	}
	if r.accountUsageStats == nil {
		return r.usageLogRepository.GetAccountUsageStats(ctx, accountID, startTime, endTime)
	}
	return r.accountUsageStats.Get(ctx, accountID, startTime, endTime, r.usageLogRepository.GetAccountUsageStats)
}
