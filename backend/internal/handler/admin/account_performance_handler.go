package admin

import (
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/pkg/response"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/gin-gonic/gin"
)

type AccountPerformanceHandler struct {
	service *service.AccountPerformanceService
}

func NewAccountPerformanceHandler(service *service.AccountPerformanceService) *AccountPerformanceHandler {
	return &AccountPerformanceHandler{service: service}
}

func (h *AccountPerformanceHandler) GetOverview(c *gin.Context) {
	if h == nil || h.service == nil {
		response.Error(c, http.StatusServiceUnavailable, "Account performance is unavailable")
		return
	}
	filter, ok := parseAccountPerformanceFilter(c)
	if !ok {
		return
	}
	result, err := h.service.Overview(c.Request.Context(), filter.overview())
	if err != nil {
		writeAccountPerformanceError(c, err)
		return
	}
	response.Success(c, result)
}

func (h *AccountPerformanceHandler) GetAccounts(c *gin.Context) {
	if h == nil || h.service == nil {
		response.Error(c, http.StatusServiceUnavailable, "Account performance is unavailable")
		return
	}
	filter, ok := parseAccountPerformanceFilter(c)
	if !ok {
		return
	}
	page, pageSize, ok := parseAccountPerformancePage(c)
	if !ok {
		return
	}
	result, err := h.service.Accounts(c.Request.Context(), service.AccountPerformanceAccountFilter{
		Start: filter.start, End: filter.end, Platform: filter.platform, GroupID: filter.groupID, Model: filter.model, Protocol: filter.protocol, AccountID: filter.accountID,
		SortBy: c.Query("sort"), SortOrder: c.Query("order"), Page: page, PageSize: pageSize,
	})
	if err != nil {
		writeAccountPerformanceError(c, err)
		return
	}
	response.Success(c, result)
}

func (h *AccountPerformanceHandler) GetInvestigation(c *gin.Context) {
	if h == nil || h.service == nil {
		response.Error(c, http.StatusServiceUnavailable, "Account performance is unavailable")
		return
	}
	filter, ok := parseAccountPerformanceFilter(c)
	if !ok {
		return
	}
	result, err := h.service.Investigation(c.Request.Context(), service.AccountPerformanceInvestigationFilter{
		Start: filter.start, End: filter.end, Platform: filter.platform, GroupID: filter.groupID, Model: filter.model, Protocol: filter.protocol, AccountID: filter.accountID,
	})
	if err != nil {
		writeAccountPerformanceError(c, err)
		return
	}
	response.Success(c, result)
}

func (h *AccountPerformanceHandler) GetHealth(c *gin.Context) {
	if h == nil || h.service == nil {
		response.Error(c, http.StatusServiceUnavailable, "Account performance is unavailable")
		return
	}
	response.Success(c, h.service.CollectionHealth())
}

type accountPerformanceFilter struct {
	start, end      time.Time
	platform        string
	groupID         int64
	model, protocol string
	accountID       int64
}

func (f accountPerformanceFilter) overview() service.AccountPerformanceOverviewFilter {
	return service.AccountPerformanceOverviewFilter{Start: f.start, End: f.end, Platform: f.platform, GroupID: f.groupID, Model: f.model, Protocol: f.protocol, AccountID: f.accountID}
}

func parseAccountPerformanceFilter(c *gin.Context) (accountPerformanceFilter, bool) {
	if c == nil {
		return accountPerformanceFilter{}, false
	}
	allowed := map[string]bool{"range": true, "platform": true, "group_id": true, "model": true, "protocol": true, "account_id": true, "sort": true, "order": true, "page": true, "page_size": true, "timezone": true}
	for key, values := range c.Request.URL.Query() {
		if !allowed[key] || len(values) != 1 {
			response.BadRequest(c, "Invalid account performance query")
			return accountPerformanceFilter{}, false
		}
	}
	duration, ok := map[string]time.Duration{"1h": time.Hour, "6h": 6 * time.Hour, "24h": 24 * time.Hour, "7d": 7 * 24 * time.Hour, "30d": 30 * 24 * time.Hour, "90d": 90 * 24 * time.Hour}[strings.TrimSpace(defaultAccountPerformanceRange(c.Query("range")))]
	if !ok {
		response.BadRequest(c, "Invalid account performance range")
		return accountPerformanceFilter{}, false
	}
	now := time.Now().UTC()
	filter := accountPerformanceFilter{start: now.Add(-duration), end: now, platform: strings.TrimSpace(c.Query("platform")), model: strings.TrimSpace(c.Query("model")), protocol: strings.TrimSpace(c.Query("protocol"))}
	var valid bool
	if filter.groupID, valid = parseAccountPerformanceID(c, "group_id"); !valid {
		return accountPerformanceFilter{}, false
	}
	if filter.accountID, valid = parseAccountPerformanceID(c, "account_id"); !valid {
		return accountPerformanceFilter{}, false
	}
	return filter, true
}

func defaultAccountPerformanceRange(value string) string {
	if value == "" {
		return "24h"
	}
	return value
}
func parseAccountPerformanceID(c *gin.Context, key string) (int64, bool) {
	value := c.Query(key)
	if value == "" {
		return 0, true
	}
	parsed, err := strconv.ParseInt(value, 10, 64)
	if err != nil || parsed <= 0 {
		response.BadRequest(c, "Invalid "+key)
		return 0, false
	}
	return parsed, true
}
func parseAccountPerformancePage(c *gin.Context) (int, int, bool) {
	page, size := 1, 20
	if raw := c.Query("page"); raw != "" {
		parsed, err := strconv.Atoi(raw)
		if err != nil || parsed < 1 {
			response.BadRequest(c, "Invalid page")
			return 0, 0, false
		}
		page = parsed
	}
	if raw := c.Query("page_size"); raw != "" {
		parsed, err := strconv.Atoi(raw)
		if err != nil || parsed < 1 || parsed > 100 {
			response.BadRequest(c, "Invalid page_size")
			return 0, 0, false
		}
		size = parsed
	}
	return page, size, true
}
func writeAccountPerformanceError(c *gin.Context, err error) {
	if err == service.ErrAccountPerformanceUnavailable {
		response.Error(c, http.StatusServiceUnavailable, err.Error())
		return
	}
	response.BadRequest(c, err.Error())
}
