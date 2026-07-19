package handler

import (
	"strconv"
	"strings"

	infraerrors "github.com/Wei-Shaw/sub2api/internal/pkg/errors"
	"github.com/Wei-Shaw/sub2api/internal/pkg/response"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/gin-gonic/gin"
)

// GetOrderStatistics returns paid-order aggregates for the authenticated user.
// GET /api/v1/payment/orders/statistics
func (h *PaymentHandler) GetOrderStatistics(c *gin.Context) {
	subject, ok := requireAuth(c)
	if !ok {
		return
	}

	result, err := h.paymentService.GetUserOrderStatistics(
		c.Request.Context(),
		subject.UserID,
		orderStatisticsQueryFromContext(c),
	)
	if err != nil {
		response.ErrorFrom(c, err)
		return
	}
	response.Success(c, result)
}

// GetOrderStatisticsDetails returns a fixed-size read-only drilldown page.
// GET /api/v1/payment/orders/statistics/details
func (h *PaymentHandler) GetOrderStatisticsDetails(c *gin.Context) {
	subject, ok := requireAuth(c)
	if !ok {
		return
	}

	page, err := parseOrderStatisticsPage(c.Query("page"))
	if err != nil {
		response.ErrorFrom(c, err)
		return
	}
	items, total, err := h.paymentService.GetUserOrderStatisticsDetails(
		c.Request.Context(),
		subject.UserID,
		service.OrderStatisticsDetailsQuery{
			OrderStatisticsQuery: orderStatisticsQueryFromContext(c),
			Page:                 page,
			OrderType:            c.Query("order_type"),
			Date:                 c.Query("date"),
		},
	)
	if err != nil {
		response.ErrorFrom(c, err)
		return
	}
	response.Paginated(c, items, int64(total), page, service.OrderStatisticsDetailPageSize)
}

func orderStatisticsQueryFromContext(c *gin.Context) service.OrderStatisticsQuery {
	return service.OrderStatisticsQuery{
		StartDate: c.Query("start_date"),
		EndDate:   c.Query("end_date"),
		Timezone:  c.Query("timezone"),
	}
}

func parseOrderStatisticsPage(raw string) (int, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return 1, nil
	}
	page, err := strconv.Atoi(raw)
	if err != nil || page <= 0 {
		return 0, infraerrors.BadRequest("INVALID_ORDER_STATISTICS_PAGE", "page must be a positive integer")
	}
	return page, nil
}
