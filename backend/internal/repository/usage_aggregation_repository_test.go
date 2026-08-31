package repository

import (
	"context"
	"database/sql"
	"testing"
	"time"

	"github.com/DATA-DOG/go-sqlmock"
	"github.com/Wei-Shaw/sub2api/internal/pkg/usagestats"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/stretchr/testify/require"
)

type wrappedUsageAggregationSQL struct {
	*sql.DB
}

type usageAggregationOptionalMethods interface {
	CreateBestEffort(context.Context, *service.UsageLog) error
	GetAccountWindowStatsBatch(context.Context, []int64, time.Time) (map[int64]*usagestats.AccountStats, error)
	GetBalanceOverview(context.Context) (*service.BalanceOverview, error)
}

func TestUsageAggregationRepositoryComposesIndependentStoresAndPromotesOptionalMethods(t *testing.T) {
	db, _, err := sqlmock.New()
	require.NoError(t, err)
	t.Cleanup(func() { _ = db.Close() })

	sqlq := wrappedUsageAggregationSQL{DB: db}
	legacy := newUsageLogRepositoryWithSQL(nil, sqlq)
	decorated := newUsageAggregationRepository(legacy, sqlq)

	require.Same(t, legacy, decorated.usageLogRepository)
	require.NotNil(t, decorated.apiKeyUsageDaily)
	require.NotNil(t, decorated.accountUsageStats)
	require.NotNil(t, decorated.userDashboardStats)
	require.NotNil(t, decorated.analytics)

	var repo service.UsageLogRepository = decorated
	require.Same(t, decorated, repo)
	_, ok := any(decorated).(usageAggregationOptionalMethods)
	require.True(t, ok)
}

func TestNewUsageLogRepositoryUsesAggregationDecorator(t *testing.T) {
	repo := NewUsageLogRepository(nil, nil)
	decorated, ok := repo.(*usageAggregationRepository)
	require.True(t, ok)
	require.NotNil(t, decorated.usageLogRepository)
}
