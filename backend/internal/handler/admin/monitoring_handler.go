package admin

import (
	"net/http"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/pkg/response"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/gin-gonic/gin"
)

// MonitoringHandler serves the unified monitoring center by composing the
// account performance and first-token-timeout read models into one response.
type MonitoringHandler struct {
	performance  *service.AccountPerformanceService
	ttftRepo     service.FirstTokenTimeoutStatsRepository
	ttftRecorder *service.FirstTokenTimeoutStatsRecorder
}

func NewMonitoringHandler(performance *service.AccountPerformanceService, ttftRepo service.FirstTokenTimeoutStatsRepository, ttftRecorder *service.FirstTokenTimeoutStatsRecorder) *MonitoringHandler {
	return &MonitoringHandler{performance: performance, ttftRepo: ttftRepo, ttftRecorder: ttftRecorder}
}

type monitoringOverviewResponse struct {
	Performance *service.AccountPerformanceOverviewResult `json:"performance"`
	TTFT        firstTokenStatsOverviewResponse           `json:"ttft"`
}

func (h *MonitoringHandler) GetOverview(c *gin.Context) {
	filter, ok := parseAccountPerformanceFilter(c)
	if !ok {
		return
	}
	statsRange, ok := parseFirstTokenStatsRange(c.Query("range"))
	if !ok {
		response.BadRequest(c, "range must be one of 1h, 6h, 24h, 7d, 30d, or 90d")
		return
	}
	if h == nil || h.performance == nil || h.ttftRepo == nil {
		response.Error(c, http.StatusServiceUnavailable, "Monitoring overview is unavailable")
		return
	}
	performance, err := h.performance.Overview(c.Request.Context(), filter.overview())
	if err != nil {
		writeAccountPerformanceError(c, err)
		return
	}
	ttft, err := h.ttftRepo.QueryOverview(c.Request.Context(), service.FirstTokenStatsOverviewFilter{
		Range:    statsRange,
		End:      time.Now().UTC(),
		Protocol: filter.protocol,
		Model:    filter.model,
	})
	if err != nil {
		response.Error(c, http.StatusInternalServerError, "Failed to load first token timeout overview")
		return
	}
	var health service.FirstTokenTimeoutStatsRecorderHealth
	if h.ttftRecorder != nil {
		health = h.ttftRecorder.Health()
	}
	response.Success(c, monitoringOverviewResponse{
		Performance: performance,
		TTFT:        firstTokenStatsOverviewResponseFromService(ttft, health),
	})
}
