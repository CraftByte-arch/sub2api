package repository

import (
	"context"
	"fmt"
	"time"
)

func (r *usageAggregationRepository) GetBatchAPIKeyUsageStats(
	ctx context.Context,
	apiKeyIDs []int64,
	startTime, endTime time.Time,
) (map[int64]*BatchAPIKeyUsageStats, error) {
	if r == nil || r.usageLogRepository == nil {
		return nil, fmt.Errorf("usage aggregation repository is not configured")
	}
	if r.apiKeyUsageDaily == nil {
		return r.usageLogRepository.GetBatchAPIKeyUsageStats(ctx, apiKeyIDs, startTime, endTime)
	}
	return r.apiKeyUsageDaily.GetBatch(ctx, apiKeyIDs, startTime, endTime)
}
