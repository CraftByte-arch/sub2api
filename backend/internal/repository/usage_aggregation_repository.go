package repository

import (
	"github.com/Wei-Shaw/sub2api/internal/service"
)

// usageAggregationRepository is the single composition point for custom usage
// statistics. Each Store remains independent; embedding the legacy repository
// keeps all unmodified and optional methods promoted automatically.
type usageAggregationRepository struct {
	*usageLogRepository

	apiKeyUsageDaily   *apiKeyUsageDailyStore
	accountUsageStats  *accountUsageStatsStore
	userDashboardStats *userDashboardStatsStore
	analytics          *userUsageAnalyticsStore
}

var _ service.UsageLogRepository = (*usageAggregationRepository)(nil)

func newUsageAggregationRepository(repo *usageLogRepository, sqlq sqlExecutor) *usageAggregationRepository {
	decorated := &usageAggregationRepository{
		usageLogRepository: repo,
		apiKeyUsageDaily:   newAPIKeyUsageDailyStore(sqlq),
		accountUsageStats:  newAccountUsageStatsStore(sqlq),
		userDashboardStats: newUserDashboardStatsStore(sqlq),
		analytics:          newUserUsageAnalyticsStore(sqlq),
	}
	decorated.apiKeyUsageDaily.StartAutomaticBackfill()
	decorated.accountUsageStats.StartAutomaticBackfill()
	decorated.userDashboardStats.StartAutomaticBackfill()
	decorated.analytics.StartAutomaticBackfill()
	return decorated
}
