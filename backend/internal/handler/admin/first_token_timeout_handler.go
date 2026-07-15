package admin

import (
	"bytes"
	"encoding/json"
	"errors"
	"io"
	"math"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/pkg/response"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/gin-gonic/gin"
)

const (
	firstTokenTimeoutAdminMinSeconds = 1
	firstTokenTimeoutAdminMaxSeconds = 300
)

type FirstTokenTimeoutHandler struct {
	policy   *service.FirstTokenTimeoutPolicy
	repo     service.FirstTokenTimeoutStatsRepository
	recorder *service.FirstTokenTimeoutStatsRecorder
}

func NewFirstTokenTimeoutHandler(
	policy *service.FirstTokenTimeoutPolicy,
	repo service.FirstTokenTimeoutStatsRepository,
	recorder *service.FirstTokenTimeoutStatsRecorder,
) *FirstTokenTimeoutHandler {
	return &FirstTokenTimeoutHandler{policy: policy, repo: repo, recorder: recorder}
}

type firstTokenTimeoutSettingsValue struct {
	Enabled        bool `json:"enabled"`
	TimeoutSeconds int  `json:"timeout_seconds"`
}

type firstTokenTimeoutSettingsResponse struct {
	Saved     firstTokenTimeoutSettingsValue `json:"saved"`
	Effective firstTokenTimeoutSettingsValue `json:"effective"`
	LoadedAt  time.Time                      `json:"loaded_at"`
}

type firstTokenTimeoutSettingsUpdateRequest struct {
	Enabled        *bool `json:"enabled"`
	TimeoutSeconds *int  `json:"timeout_seconds"`
}

func (h *FirstTokenTimeoutHandler) GetSettings(c *gin.Context) {
	if h == nil || h.policy == nil {
		response.Error(c, http.StatusServiceUnavailable, "First token timeout settings are unavailable")
		return
	}
	response.Success(c, firstTokenTimeoutSettingsResponseFromSnapshot(h.policy.Snapshot()))
}

func (h *FirstTokenTimeoutHandler) UpdateSettings(c *gin.Context) {
	if h == nil || h.policy == nil {
		response.Error(c, http.StatusServiceUnavailable, "First token timeout settings are unavailable")
		return
	}

	request, ok := decodeFirstTokenTimeoutSettingsUpdate(c)
	if !ok {
		return
	}
	settings := service.FirstTokenTimeoutSettings{
		Enabled:        *request.Enabled,
		TimeoutSeconds: *request.TimeoutSeconds,
	}
	if err := h.policy.Update(c.Request.Context(), settings); err != nil {
		response.Error(c, http.StatusInternalServerError, "Failed to update first token timeout settings")
		return
	}

	snapshot := h.policy.Snapshot()
	response.Success(c, firstTokenTimeoutSettingsResponse{
		Saved: firstTokenTimeoutSettingsValue{
			Enabled:        settings.Enabled,
			TimeoutSeconds: settings.TimeoutSeconds,
		},
		Effective: firstTokenTimeoutSettingsValue{
			Enabled:        snapshot.Enabled,
			TimeoutSeconds: int(snapshot.Timeout / time.Second),
		},
		LoadedAt: snapshot.LoadedAt,
	})
}

func decodeFirstTokenTimeoutSettingsUpdate(c *gin.Context) (firstTokenTimeoutSettingsUpdateRequest, bool) {
	var request firstTokenTimeoutSettingsUpdateRequest
	decoder := json.NewDecoder(c.Request.Body)
	opening, err := decoder.Token()
	if err != nil || opening != json.Delim('{') {
		response.BadRequest(c, "Invalid first token timeout settings")
		return firstTokenTimeoutSettingsUpdateRequest{}, false
	}
	seen := make(map[string]struct{}, 2)
	for decoder.More() {
		keyToken, err := decoder.Token()
		if err != nil {
			response.BadRequest(c, "Invalid first token timeout settings")
			return firstTokenTimeoutSettingsUpdateRequest{}, false
		}
		key, ok := keyToken.(string)
		if !ok || (key != "enabled" && key != "timeout_seconds") {
			response.BadRequest(c, "Invalid first token timeout settings")
			return firstTokenTimeoutSettingsUpdateRequest{}, false
		}
		if _, duplicate := seen[key]; duplicate {
			response.BadRequest(c, "Invalid first token timeout settings")
			return firstTokenTimeoutSettingsUpdateRequest{}, false
		}
		seen[key] = struct{}{}

		var raw json.RawMessage
		if err := decoder.Decode(&raw); err != nil || bytes.Equal(bytes.TrimSpace(raw), []byte("null")) {
			response.BadRequest(c, "Invalid first token timeout settings")
			return firstTokenTimeoutSettingsUpdateRequest{}, false
		}
		switch key {
		case "enabled":
			var enabled bool
			if err := json.Unmarshal(raw, &enabled); err != nil {
				response.BadRequest(c, "Invalid first token timeout settings")
				return firstTokenTimeoutSettingsUpdateRequest{}, false
			}
			request.Enabled = &enabled
		case "timeout_seconds":
			var timeoutSeconds int
			if err := json.Unmarshal(raw, &timeoutSeconds); err != nil {
				response.BadRequest(c, "Invalid first token timeout settings")
				return firstTokenTimeoutSettingsUpdateRequest{}, false
			}
			request.TimeoutSeconds = &timeoutSeconds
		}
	}
	closing, err := decoder.Token()
	if err != nil || closing != json.Delim('}') {
		response.BadRequest(c, "Invalid first token timeout settings")
		return firstTokenTimeoutSettingsUpdateRequest{}, false
	}
	if err := requireJSONEOF(decoder); err != nil {
		response.BadRequest(c, "Invalid first token timeout settings")
		return firstTokenTimeoutSettingsUpdateRequest{}, false
	}
	if request.Enabled == nil || request.TimeoutSeconds == nil {
		response.BadRequest(c, "enabled and timeout_seconds are required")
		return firstTokenTimeoutSettingsUpdateRequest{}, false
	}
	if *request.TimeoutSeconds < firstTokenTimeoutAdminMinSeconds || *request.TimeoutSeconds > firstTokenTimeoutAdminMaxSeconds {
		response.BadRequest(c, "timeout_seconds must be an integer between 1 and 300")
		return firstTokenTimeoutSettingsUpdateRequest{}, false
	}
	return request, true
}

func requireJSONEOF(decoder *json.Decoder) error {
	var extra any
	err := decoder.Decode(&extra)
	if err == io.EOF {
		return nil
	}
	if err == nil {
		return errors.New("multiple JSON values")
	}
	return err
}

func firstTokenTimeoutSettingsResponseFromSnapshot(snapshot service.FirstTokenTimeoutSnapshot) firstTokenTimeoutSettingsResponse {
	settings := firstTokenTimeoutSettingsValue{
		Enabled:        snapshot.Enabled,
		TimeoutSeconds: int(snapshot.Timeout / time.Second),
	}
	return firstTokenTimeoutSettingsResponse{
		Saved:     settings,
		Effective: settings,
		LoadedAt:  snapshot.LoadedAt,
	}
}

type firstTokenStatsRatioResponse struct {
	Numerator   int64   `json:"numerator"`
	Denominator int64   `json:"denominator"`
	Rate        float64 `json:"rate"`
}

type firstTokenStatsSummaryResponse struct {
	ControlledRequests     int64                        `json:"controlled_requests"`
	ClientCanceledRequests int64                        `json:"client_canceled_requests"`
	AttemptTTFTTimeoutRate firstTokenStatsRatioResponse `json:"attempt_ttft_timeout_rate"`
	RecoveryRate           firstTokenStatsRatioResponse `json:"recovery_rate"`
	FinalTTFTFailureRate   firstTokenStatsRatioResponse `json:"final_ttft_failure_rate"`
	OtherFinalFailureRate  firstTokenStatsRatioResponse `json:"other_final_failure_rate"`
}

type firstTokenStatsTrendPointResponse struct {
	BucketStart            time.Time                    `json:"bucket_start"`
	AttemptTTFTTimeoutRate firstTokenStatsRatioResponse `json:"attempt_ttft_timeout_rate"`
	RecoveryRate           firstTokenStatsRatioResponse `json:"recovery_rate"`
	FinalTTFTFailureRate   firstTokenStatsRatioResponse `json:"final_ttft_failure_rate"`
	OtherFinalFailureRate  firstTokenStatsRatioResponse `json:"other_final_failure_rate"`
}

type firstTokenStatsFailureDistributionResponse struct {
	FailureKind string `json:"failure_kind"`
	SampleCount int64  `json:"sample_count"`
}

type firstTokenStatsOverviewResponse struct {
	Summary       firstTokenStatsSummaryResponse               `json:"summary"`
	Trend         []firstTokenStatsTrendPointResponse          `json:"trend"`
	OtherFailures []firstTokenStatsFailureDistributionResponse `json:"other_failures"`
	Completeness  service.FirstTokenTimeoutStatsRecorderHealth `json:"completeness"`
}

func (h *FirstTokenTimeoutHandler) GetOverview(c *gin.Context) {
	if h == nil || h.repo == nil {
		response.Error(c, http.StatusServiceUnavailable, "First token timeout statistics are unavailable")
		return
	}
	filter, ok := parseFirstTokenStatsOverviewFilter(c)
	if !ok {
		return
	}
	overview, err := h.repo.QueryOverview(c.Request.Context(), filter)
	if err != nil {
		response.Error(c, http.StatusInternalServerError, "Failed to load first token timeout overview")
		return
	}
	response.Success(c, firstTokenStatsOverviewResponseFromService(overview, h.recorder.Health()))
}

func parseFirstTokenStatsOverviewFilter(c *gin.Context) (service.FirstTokenStatsOverviewFilter, bool) {
	query := c.Request.URL.Query()
	if query.Has("account") || query.Has("account_id") || query.Has("platform") {
		response.BadRequest(c, "account and platform filters are only supported by the accounts endpoint")
		return service.FirstTokenStatsOverviewFilter{}, false
	}
	if !validateFirstTokenStatsQueryParams(c, "range", "protocol", "model") {
		return service.FirstTokenStatsOverviewFilter{}, false
	}
	statsRange, ok := parseFirstTokenStatsRange(c.Query("range"))
	if !ok {
		response.BadRequest(c, "range must be one of 24h, 7d, 30d, or 90d")
		return service.FirstTokenStatsOverviewFilter{}, false
	}
	protocol, ok := parseFirstTokenStatsProtocol(c.Query("protocol"))
	if !ok {
		response.BadRequest(c, "protocol must be responses, chat_completions, or anthropic_messages")
		return service.FirstTokenStatsOverviewFilter{}, false
	}
	return service.FirstTokenStatsOverviewFilter{
		Range:    statsRange,
		End:      time.Now().UTC(),
		Protocol: protocol,
		Model:    strings.TrimSpace(c.Query("model")),
	}, true
}

func firstTokenStatsOverviewResponseFromService(
	overview *service.FirstTokenStatsOverview,
	health service.FirstTokenTimeoutStatsRecorderHealth,
) firstTokenStatsOverviewResponse {
	if overview == nil {
		overview = &service.FirstTokenStatsOverview{}
	}
	trend := make([]firstTokenStatsTrendPointResponse, 0, len(overview.Trend))
	for _, point := range overview.Trend {
		trend = append(trend, firstTokenStatsTrendPointResponse{
			BucketStart:            point.BucketStart,
			AttemptTTFTTimeoutRate: firstTokenStatsRatioResponseFromService(point.AttemptTTFTTimeoutRate),
			RecoveryRate:           firstTokenStatsRatioResponseFromService(point.RecoveryRate),
			FinalTTFTFailureRate:   firstTokenStatsRatioResponseFromService(point.FinalTTFTFailureRate),
			OtherFinalFailureRate:  firstTokenStatsRatioResponseFromService(point.OtherFinalFailureRate),
		})
	}
	otherFailures := make([]firstTokenStatsFailureDistributionResponse, 0, len(overview.OtherFailures))
	for _, failure := range overview.OtherFailures {
		otherFailures = append(otherFailures, firstTokenStatsFailureDistributionResponse{
			FailureKind: failure.FailureKind,
			SampleCount: failure.SampleCount,
		})
	}
	return firstTokenStatsOverviewResponse{
		Summary: firstTokenStatsSummaryResponse{
			ControlledRequests:     overview.Summary.ControlledRequests,
			ClientCanceledRequests: overview.Summary.ClientCanceledRequests,
			AttemptTTFTTimeoutRate: firstTokenStatsRatioResponseFromService(overview.Summary.AttemptTTFTTimeoutRate),
			RecoveryRate:           firstTokenStatsRatioResponseFromService(overview.Summary.RecoveryRate),
			FinalTTFTFailureRate:   firstTokenStatsRatioResponseFromService(overview.Summary.FinalTTFTFailureRate),
			OtherFinalFailureRate:  firstTokenStatsRatioResponseFromService(overview.Summary.OtherFinalFailureRate),
		},
		Trend:         trend,
		OtherFailures: otherFailures,
		Completeness:  health,
	}
}

type firstTokenStatsAccountResponse struct {
	AccountID         int64                        `json:"account_id"`
	AccountName       string                       `json:"account_name"`
	Platform          string                       `json:"platform"`
	Samples           int64                        `json:"samples"`
	SuccessCount      int64                        `json:"success_count"`
	TTFTTimeoutCount  int64                        `json:"ttft_timeout_count"`
	TTFTTimeoutRate   firstTokenStatsRatioResponse `json:"ttft_timeout_rate"`
	OtherFailureCount int64                        `json:"other_failure_count"`
	OtherFailureRate  firstTokenStatsRatioResponse `json:"other_failure_rate"`
	AvgTTFTMS         float64                      `json:"avg_ttft_ms"`
	LowSample         bool                         `json:"low_sample"`
}

type firstTokenStatsAccountPageResponse struct {
	Items    []firstTokenStatsAccountResponse `json:"items"`
	Total    int64                            `json:"total"`
	Page     int                              `json:"page"`
	PageSize int                              `json:"page_size"`
	Pages    int                              `json:"pages"`
}

func (h *FirstTokenTimeoutHandler) GetAccounts(c *gin.Context) {
	if h == nil || h.repo == nil {
		response.Error(c, http.StatusServiceUnavailable, "First token timeout statistics are unavailable")
		return
	}
	filter, ok := parseFirstTokenStatsAccountFilter(c)
	if !ok {
		return
	}
	page, err := h.repo.QueryAccounts(c.Request.Context(), filter)
	if err != nil {
		response.Error(c, http.StatusInternalServerError, "Failed to load first token timeout accounts")
		return
	}
	response.Success(c, firstTokenStatsAccountPageResponseFromService(page))
}

func parseFirstTokenStatsAccountFilter(c *gin.Context) (service.FirstTokenStatsAccountFilter, bool) {
	if !validateFirstTokenStatsQueryParams(
		c,
		"range",
		"protocol",
		"model",
		"platform",
		"account_id",
		"search",
		"sort",
		"order",
		"page",
		"page_size",
	) {
		return service.FirstTokenStatsAccountFilter{}, false
	}
	statsRange, ok := parseFirstTokenStatsRange(c.Query("range"))
	if !ok {
		response.BadRequest(c, "range must be one of 24h, 7d, 30d, or 90d")
		return service.FirstTokenStatsAccountFilter{}, false
	}
	protocol, ok := parseFirstTokenStatsProtocol(c.Query("protocol"))
	if !ok {
		response.BadRequest(c, "protocol must be responses, chat_completions, or anthropic_messages")
		return service.FirstTokenStatsAccountFilter{}, false
	}
	filter := service.FirstTokenStatsAccountFilter{
		Range:     statsRange,
		End:       time.Now().UTC(),
		Protocol:  protocol,
		Model:     strings.TrimSpace(c.Query("model")),
		Platform:  strings.TrimSpace(c.Query("platform")),
		Search:    strings.TrimSpace(c.Query("search")),
		SortBy:    service.FirstTokenStatsAccountSortSamples,
		SortOrder: "desc",
		Page:      1,
		PageSize:  20,
	}

	query := c.Request.URL.Query()
	if query.Has("account_id") {
		accountID, err := strconv.ParseInt(strings.TrimSpace(c.Query("account_id")), 10, 64)
		if err != nil || accountID <= 0 {
			response.BadRequest(c, "account_id must be a positive integer")
			return service.FirstTokenStatsAccountFilter{}, false
		}
		filter.AccountID = accountID
	}
	if query.Has("sort") {
		filter.SortBy = strings.TrimSpace(c.Query("sort"))
		if !isFirstTokenStatsAccountSortAllowed(filter.SortBy) {
			response.BadRequest(c, "sort is not supported")
			return service.FirstTokenStatsAccountFilter{}, false
		}
	}
	if query.Has("order") {
		filter.SortOrder = strings.ToLower(strings.TrimSpace(c.Query("order")))
		if filter.SortOrder != "asc" && filter.SortOrder != "desc" {
			response.BadRequest(c, "order must be asc or desc")
			return service.FirstTokenStatsAccountFilter{}, false
		}
	}
	if query.Has("page") {
		page, err := strconv.Atoi(strings.TrimSpace(c.Query("page")))
		if err != nil || page <= 0 {
			response.BadRequest(c, "page must be a positive integer")
			return service.FirstTokenStatsAccountFilter{}, false
		}
		filter.Page = page
	}
	if query.Has("page_size") {
		pageSize, err := strconv.Atoi(strings.TrimSpace(c.Query("page_size")))
		if err != nil || !isFirstTokenStatsPageSizeAllowed(pageSize) {
			response.BadRequest(c, "page_size must be one of 10, 20, 50, or 100")
			return service.FirstTokenStatsAccountFilter{}, false
		}
		filter.PageSize = pageSize
	}
	return filter, true
}

func firstTokenStatsAccountPageResponseFromService(page *service.FirstTokenStatsAccountPage) firstTokenStatsAccountPageResponse {
	if page == nil {
		page = &service.FirstTokenStatsAccountPage{Page: 1, PageSize: 20}
	}
	items := make([]firstTokenStatsAccountResponse, 0, len(page.Items))
	for _, item := range page.Items {
		items = append(items, firstTokenStatsAccountResponse{
			AccountID:         item.AccountID,
			AccountName:       item.AccountName,
			Platform:          item.Platform,
			Samples:           item.Samples,
			SuccessCount:      item.SuccessCount,
			TTFTTimeoutCount:  item.TTFTTimeoutCount,
			TTFTTimeoutRate:   firstTokenStatsRatioResponseFromService(item.TTFTTimeoutRate),
			OtherFailureCount: item.OtherFailureCount,
			OtherFailureRate:  firstTokenStatsRatioResponseFromService(item.OtherFailureRate),
			AvgTTFTMS:         finiteOrZero(item.AvgTTFTMS),
			LowSample:         item.LowSample,
		})
	}
	return firstTokenStatsAccountPageResponse{
		Items:    items,
		Total:    page.Total,
		Page:     page.Page,
		PageSize: page.PageSize,
		Pages:    page.Pages,
	}
}

func firstTokenStatsRatioResponseFromService(ratio service.FirstTokenStatsRatio) firstTokenStatsRatioResponse {
	rate := ratio.Rate
	if ratio.Denominator == 0 {
		rate = 0
	}
	return firstTokenStatsRatioResponse{
		Numerator:   ratio.Numerator,
		Denominator: ratio.Denominator,
		Rate:        finiteOrZero(rate),
	}
}

func finiteOrZero(value float64) float64 {
	if math.IsNaN(value) || math.IsInf(value, 0) {
		return 0
	}
	return value
}

func parseFirstTokenStatsRange(raw string) (service.FirstTokenStatsRange, bool) {
	switch service.FirstTokenStatsRange(strings.TrimSpace(raw)) {
	case "", service.FirstTokenStatsRange24Hours:
		return service.FirstTokenStatsRange24Hours, true
	case service.FirstTokenStatsRange7Days:
		return service.FirstTokenStatsRange7Days, true
	case service.FirstTokenStatsRange30Days:
		return service.FirstTokenStatsRange30Days, true
	case service.FirstTokenStatsRange90Days:
		return service.FirstTokenStatsRange90Days, true
	default:
		return "", false
	}
}

func parseFirstTokenStatsProtocol(raw string) (string, bool) {
	protocol := service.FirstTokenProtocol(strings.TrimSpace(raw))
	switch protocol {
	case "":
		return "", true
	case service.ProtocolResponses, service.ProtocolChatCompletions, service.ProtocolAnthropicMessages:
		return string(protocol), true
	default:
		return "", false
	}
}

func isFirstTokenStatsAccountSortAllowed(sortBy string) bool {
	switch sortBy {
	case service.FirstTokenStatsAccountSortSamples,
		service.FirstTokenStatsAccountSortSuccess,
		service.FirstTokenStatsAccountSortTTFTTimeoutCount,
		service.FirstTokenStatsAccountSortTTFTTimeoutRate,
		service.FirstTokenStatsAccountSortOtherFailureCount,
		service.FirstTokenStatsAccountSortOtherFailureRate,
		service.FirstTokenStatsAccountSortAvgTTFTMS:
		return true
	default:
		return false
	}
}

func isFirstTokenStatsPageSizeAllowed(pageSize int) bool {
	switch pageSize {
	case 10, 20, 50, 100:
		return true
	default:
		return false
	}
}

func validateFirstTokenStatsQueryParams(c *gin.Context, allowed ...string) bool {
	allowedSet := make(map[string]struct{}, len(allowed))
	for _, key := range allowed {
		allowedSet[key] = struct{}{}
	}
	for key := range c.Request.URL.Query() {
		if _, ok := allowedSet[key]; ok {
			continue
		}
		response.BadRequest(c, "Unsupported query parameter: "+key)
		return false
	}
	return true
}
