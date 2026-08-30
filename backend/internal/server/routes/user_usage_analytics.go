package routes

import (
	"github.com/Wei-Shaw/sub2api/internal/pkg/userusageanalytics"

	"github.com/gin-gonic/gin"
)

func markUserUsageAnalyticsRequests() gin.HandlerFunc {
	return func(c *gin.Context) {
		c.Request = c.Request.WithContext(userusageanalytics.Mark(c.Request.Context()))
		c.Next()
	}
}
