package admin

import (
	"net/http"

	"github.com/Wei-Shaw/sub2api/internal/pkg/response"
	"github.com/gin-gonic/gin"
)

// GetBalanceOverview handles the standalone administrator balance overview.
// GET /api/v1/admin/balance-overview
func (h *DashboardHandler) GetBalanceOverview(c *gin.Context) {
	if h == nil || h.dashboardService == nil {
		response.Error(c, http.StatusServiceUnavailable, "Balance overview is not available")
		return
	}

	overview, err := h.dashboardService.GetBalanceOverview(c.Request.Context())
	if err != nil {
		response.InternalError(c, "Failed to get balance overview")
		return
	}

	response.Success(c, overview)
}
