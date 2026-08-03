package admin

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

type balanceOverviewUsageRepoStub struct {
	service.UsageLogRepository
	overview *service.BalanceOverview
	err      error
}

func (s *balanceOverviewUsageRepoStub) GetBalanceOverview(_ context.Context) (*service.BalanceOverview, error) {
	return s.overview, s.err
}

func TestDashboardHandlerGetBalanceOverview(t *testing.T) {
	gin.SetMode(gin.TestMode)
	expiresAt := time.Date(2026, time.August, 31, 12, 0, 0, 0, time.UTC)
	repo := &balanceOverviewUsageRepoStub{
		overview: &service.BalanceOverview{
			TotalAvailableBalance:          125.75,
			TotalUsageCardAvailableBalance: 38.5,
			UsageCardLatestExpiresAt:       &expiresAt,
			GeneratedAt:                    time.Date(2026, time.July, 30, 12, 0, 0, 0, time.UTC),
		},
	}
	handler := NewDashboardHandler(service.NewDashboardService(repo, nil, nil, nil), nil)
	router := gin.New()
	router.GET("/admin/balance-overview", handler.GetBalanceOverview)

	req := httptest.NewRequest(http.MethodGet, "/admin/balance-overview", nil)
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)

	require.Equal(t, http.StatusOK, rec.Code)
	require.Contains(t, rec.Body.String(), `"total_available_balance":125.75`)
	require.Contains(t, rec.Body.String(), `"total_usage_card_available_balance":38.5`)
	require.Contains(t, rec.Body.String(), `"usage_card_latest_expires_at":"2026-08-31T12:00:00Z"`)
}
