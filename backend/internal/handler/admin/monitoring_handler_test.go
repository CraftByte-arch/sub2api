package admin

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

type stubPerformanceService struct {
	result *service.AccountPerformanceOverviewResult
	filter service.AccountPerformanceOverviewFilter
}

func (s *stubPerformanceService) Overview(ctx context.Context, filter service.AccountPerformanceOverviewFilter) (*service.AccountPerformanceOverviewResult, error) {
	s.filter = filter
	return s.result, nil
}

type stubTTFTRepo struct{}

func (s *stubTTFTRepo) UpsertBatch(ctx context.Context, deltas []service.FirstTokenStatsDelta) error {
	panic("unused in handler test")
}

func (s *stubTTFTRepo) QueryOverview(ctx context.Context, filter service.FirstTokenStatsOverviewFilter) (*service.FirstTokenStatsOverview, error) {
	return &service.FirstTokenStatsOverview{}, nil
}

func (s *stubTTFTRepo) QueryAccounts(ctx context.Context, filter service.FirstTokenStatsAccountFilter) (*service.FirstTokenStatsAccountPage, error) {
	panic("unused in handler test")
}

func (s *stubTTFTRepo) DeleteBefore(ctx context.Context, cutoff time.Time) (int64, error) {
	panic("unused in handler test")
}

func TestMonitoringGetOverviewUnavailableWithoutDependencies(t *testing.T) {
	gin.SetMode(gin.TestMode)
	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Request = httptest.NewRequest(http.MethodGet, "/?range=24h", nil)

	handler := &MonitoringHandler{}
	handler.GetOverview(c)

	require.Equal(t, http.StatusServiceUnavailable, w.Code)
}

func TestMonitoringGetOverviewRejectsInvalidRange(t *testing.T) {
	gin.SetMode(gin.TestMode)
	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Request = httptest.NewRequest(http.MethodGet, "/?range=15m", nil)

	handler := &MonitoringHandler{}
	handler.GetOverview(c)

	require.Equal(t, http.StatusBadRequest, w.Code)
}

func TestMonitoringGetOverviewRejectsUnknownQueryKey(t *testing.T) {
	gin.SetMode(gin.TestMode)
	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Request = httptest.NewRequest(http.MethodGet, "/?range=24h&bogus=1", nil)

	handler := &MonitoringHandler{}
	handler.GetOverview(c)

	require.Equal(t, http.StatusBadRequest, w.Code)
}

func TestMonitoringGetOverviewHappyPath(t *testing.T) {
	gin.SetMode(gin.TestMode)
	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Request = httptest.NewRequest(http.MethodGet, "/?range=6h&model=gpt-5", nil)

	perfStub := &stubPerformanceService{
		result: &service.AccountPerformanceOverviewResult{
			CoverageStart: time.Now().Add(-6 * time.Hour),
			CoverageEnd:   time.Now(),
		},
	}
	ttftStub := &stubTTFTRepo{}

	handler := NewMonitoringHandler(perfStub, ttftStub, nil)
	handler.GetOverview(c)

	require.Equal(t, http.StatusOK, w.Code)

	var response struct {
		Data struct {
			Performance interface{} `json:"performance"`
			TTFT        interface{} `json:"ttft"`
		} `json:"data"`
	}
	err := json.Unmarshal(w.Body.Bytes(), &response)
	require.NoError(t, err)
	require.NotNil(t, response.Data.Performance)
	require.NotNil(t, response.Data.TTFT)

	// Verify filter received correct query params (filter has Start/End, not Range)
	require.Equal(t, "gpt-5", perfStub.filter.Model)
	require.True(t, perfStub.filter.End.After(perfStub.filter.Start))
}
