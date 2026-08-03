package routes

import (
	"github.com/Wei-Shaw/sub2api/internal/handler"
	"github.com/gin-gonic/gin"
)

func registerBalanceOverviewRoutes(admin *gin.RouterGroup, h *handler.Handlers) {
	balanceOverview := admin.Group("/balance-overview")
	{
		balanceOverview.GET("", h.Admin.Dashboard.GetBalanceOverview)
	}
}
