package core

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math"
	"net/http"
	"net/url"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/Wei-Shaw/sub2api-account-auto-scheduler/internal/model"
)

const (
	maxJSONResponseBytes = 4 << 20
	maxErrorBodyBytes    = 8 << 10
	maxSSEEventBytes     = 20 << 20
	maxResponseTextBytes = 4 << 10
	menuItemID           = "account-auto-scheduler"

	todayAccountCacheStatsTTL          = 5 * time.Minute
	todayAccountCacheStatsTimeout      = 5 * time.Second
	todayAccountCacheStatsConcurrency  = 4
	todayAccountCacheStatsRetryBackoff = time.Minute
)

type Client struct {
	apiRoot     string
	adminAPIKey string
	httpClient  *http.Client
	streamHTTP  *http.Client

	todayCacheMu       sync.Mutex
	todayCacheStats    map[int64]cachedAccountCacheStats
	todayCacheFailures map[int64]time.Time
	todayCacheFlights  map[int64]*todayAccountCacheStatsFlight
}

type cachedAccountCacheStats struct {
	stats     model.AccountCacheStats
	expiresAt time.Time
}

type todayAccountCacheStatsFlight struct {
	done  chan struct{}
	stats *model.AccountCacheStats
	err   error
}

var errTodayAccountCacheStatsBackoff = errors.New("cache statistics retry is temporarily backed off")

type ProbeOutcome struct {
	Success      bool
	ResponseText string
	ErrorMessage string
	Latency      time.Duration
	Usage        *model.ProbeUsage
}

type ModelPricing struct {
	Found           bool
	InputPrice      *float64
	OutputPrice     *float64
	CacheWritePrice *float64
	CacheReadPrice  *float64
}

type ForwardedIdentity struct {
	ClientIP  string
	UserAgent string
}

// DirectProbeExport contains only the sensitive routing material needed for a
// single direct probe. It is intentionally not JSON serializable so callers
// cannot accidentally return an account export to the browser.
type DirectProbeExport struct {
	Snapshot model.DirectProbeSnapshot `json:"-"`
}

type AdminUser struct {
	ID    int64  `json:"id"`
	Email string `json:"email"`
	Role  string `json:"role"`
}

type HTTPError struct {
	StatusCode int
	Code       string
	Message    string
}

func (e *HTTPError) Error() string {
	return fmt.Sprintf("Sub2API returned HTTP %d: %s", e.StatusCode, e.Message)
}

type apiEnvelope struct {
	Code    json.RawMessage `json:"code"`
	Message string          `json:"message"`
	Data    json.RawMessage `json:"data"`
}

type pageResponse[T any] struct {
	Items    []T   `json:"items"`
	Total    int64 `json:"total"`
	Page     int   `json:"page"`
	PageSize int   `json:"page_size"`
	Pages    int   `json:"pages"`
}

type testEvent struct {
	Type    string         `json:"type"`
	Text    string         `json:"text"`
	Success bool           `json:"success"`
	Error   string         `json:"error"`
	Model   string         `json:"model"`
	Usage   map[string]any `json:"usage"`
}

func NewClient(baseURL, adminAPIKey string) (*Client, error) {
	apiRoot, err := normalizeAPIRoot(baseURL)
	if err != nil {
		return nil, err
	}
	transport := http.DefaultTransport.(*http.Transport).Clone()
	transport.MaxIdleConns = 50
	transport.MaxIdleConnsPerHost = 20
	transport.IdleConnTimeout = 60 * time.Second

	return &Client{
		apiRoot:     apiRoot,
		adminAPIKey: strings.TrimSpace(adminAPIKey),
		httpClient: &http.Client{
			Transport: transport,
			Timeout:   30 * time.Second,
		},
		streamHTTP: &http.Client{Transport: transport},
	}, nil
}

func normalizeAPIRoot(baseURL string) (string, error) {
	parsed, err := url.Parse(strings.TrimRight(strings.TrimSpace(baseURL), "/"))
	if err != nil || parsed.Host == "" || (parsed.Scheme != "http" && parsed.Scheme != "https") {
		return "", errors.New("invalid Sub2API base URL")
	}
	parsed.RawQuery = ""
	parsed.Fragment = ""
	path := strings.TrimRight(parsed.Path, "/")
	if !strings.HasSuffix(path, "/api/v1") {
		path += "/api/v1"
	}
	parsed.Path = path
	return strings.TrimRight(parsed.String(), "/"), nil
}

func (c *Client) ListAPIKeyAccounts(ctx context.Context) ([]model.UpstreamAccount, error) {
	accounts, err := c.listAccounts(ctx, url.Values{"type": {"apikey"}, "lite": {"true"}})
	if err != nil {
		return nil, err
	}
	filtered := make([]model.UpstreamAccount, 0, len(accounts))
	for _, account := range accounts {
		if account.IsAPIKey() {
			filtered = append(filtered, account)
		}
	}
	return filtered, nil
}

func (c *Client) ListAccounts(ctx context.Context) ([]model.UpstreamAccount, error) {
	return c.listAccounts(ctx, nil)
}

func (c *Client) listAccounts(ctx context.Context, filters url.Values) ([]model.UpstreamAccount, error) {
	const pageSize = 100
	accounts := make([]model.UpstreamAccount, 0)
	for page := 1; page <= 10000; page++ {
		query := make(url.Values, len(filters)+2)
		for key, values := range filters {
			query[key] = append([]string(nil), values...)
		}
		query.Set("page", strconv.Itoa(page))
		query.Set("page_size", strconv.Itoa(pageSize))
		var result pageResponse[model.UpstreamAccount]
		if err := c.adminJSON(ctx, http.MethodGet, "/admin/accounts?"+query.Encode(), nil, &result); err != nil {
			return nil, err
		}
		accounts = append(accounts, result.Items...)
		if len(result.Items) == 0 || page >= result.Pages || int64(len(accounts)) >= result.Total {
			break
		}
	}
	return accounts, nil
}

func (c *Client) GetAccount(ctx context.Context, accountID int64) (model.UpstreamAccount, error) {
	account, err := c.GetAnyAccount(ctx, accountID)
	if err != nil {
		return model.UpstreamAccount{}, err
	}
	if !account.IsAPIKey() {
		return model.UpstreamAccount{}, errors.New("only API Key accounts are supported")
	}
	return account, nil
}

func (c *Client) GetAnyAccount(ctx context.Context, accountID int64) (model.UpstreamAccount, error) {
	var account model.UpstreamAccount
	path := "/admin/accounts/" + strconv.FormatInt(accountID, 10)
	if err := c.adminJSON(ctx, http.MethodGet, path, nil, &account); err != nil {
		return model.UpstreamAccount{}, err
	}
	return account, nil
}

func (c *Client) ListGroups(ctx context.Context) ([]model.UpstreamGroup, error) {
	var groups []model.UpstreamGroup
	if err := c.adminJSON(ctx, http.MethodGet, "/admin/groups/all?include_inactive=true", nil, &groups); err != nil {
		return nil, err
	}
	return groups, nil
}

func (c *Client) GetTodayStatsBatch(ctx context.Context, accountIDs []int64) (map[string]model.WindowStats, error) {
	stats := make(map[string]model.WindowStats)
	ids := uniquePositiveIDs(accountIDs)
	const batchSize = 200
	for start := 0; start < len(ids); start += batchSize {
		end := min(start+batchSize, len(ids))
		var response struct {
			Stats map[string]model.WindowStats `json:"stats"`
		}
		body := map[string]any{"account_ids": ids[start:end]}
		if err := c.adminJSON(ctx, http.MethodPost, "/admin/accounts/today-stats/batch", body, &response); err != nil {
			return nil, err
		}
		for accountID, value := range response.Stats {
			stats[accountID] = value
		}
	}
	c.enrichTodayAccountCacheStats(ctx, ids, stats)
	return stats, nil
}

func (c *Client) GetAccountPerformanceHealth(ctx context.Context) (model.AccountPerformanceCollectionHealth, error) {
	var health model.AccountPerformanceCollectionHealth
	if err := c.adminJSON(ctx, http.MethodGet, "/admin/account-performance/health", nil, &health); err != nil {
		return model.AccountPerformanceCollectionHealth{}, err
	}
	return health, nil
}

// enrichTodayAccountCacheStats uses the indexed usage-statistics endpoint only
// for accounts that already have at least one request today. An account with no
// requests has a known-zero cache rate, so it never needs an extra main-service
// statistics query.
func (c *Client) enrichTodayAccountCacheStats(ctx context.Context, accountIDs []int64, stats map[string]model.WindowStats) {
	if len(accountIDs) == 0 || stats == nil {
		return
	}
	activeIDs := make([]int64, 0, len(accountIDs))
	seen := make(map[int64]struct{}, len(accountIDs))
	for _, accountID := range accountIDs {
		if accountID <= 0 {
			continue
		}
		if _, exists := seen[accountID]; exists {
			continue
		}
		seen[accountID] = struct{}{}
		key := strconv.FormatInt(accountID, 10)
		usage := stats[key]
		if usage.Requests <= 0 {
			zero := model.AccountCacheStats{}
			usage.Cache = &zero
			stats[key] = usage
			continue
		}
		activeIDs = append(activeIDs, accountID)
	}
	if len(activeIDs) == 0 {
		return
	}

	enrichmentCtx, cancel := context.WithTimeout(ctx, todayAccountCacheStatsTimeout)
	defer cancel()

	workerCount := min(todayAccountCacheStatsConcurrency, len(activeIDs))
	jobs := make(chan int64)
	var workers sync.WaitGroup
	var statsMu sync.Mutex
	workers.Add(workerCount)
	for range workerCount {
		go func() {
			defer workers.Done()
			for accountID := range jobs {
				cacheStats, err := c.getTodayAccountCacheStats(enrichmentCtx, accountID)
				if err != nil || cacheStats == nil {
					continue
				}
				key := strconv.FormatInt(accountID, 10)
				statsMu.Lock()
				value := stats[key]
				value.Cache = cacheStats
				stats[key] = value
				statsMu.Unlock()
			}
		}()
	}

sendLoop:
	for _, accountID := range activeIDs {
		select {
		case jobs <- accountID:
		case <-enrichmentCtx.Done():
			break sendLoop
		}
	}
	close(jobs)
	workers.Wait()
}

func (c *Client) getTodayAccountCacheStats(ctx context.Context, accountID int64) (*model.AccountCacheStats, error) {
	cached, flight, leader, err := c.acquireTodayAccountCacheStatsFlight(accountID, time.Now())
	if cached != nil || err != nil {
		return cached, err
	}
	if !leader {
		select {
		case <-flight.done:
			if flight.stats == nil {
				return nil, flight.err
			}
			return cloneAccountCacheStats(*flight.stats), flight.err
		case <-ctx.Done():
			return nil, ctx.Err()
		}
	}

	cacheStats, err := c.fetchTodayAccountCacheStats(ctx, accountID)
	c.completeTodayAccountCacheStatsFlight(accountID, flight, cacheStats, err, time.Now())
	if cacheStats == nil {
		return nil, err
	}
	return cloneAccountCacheStats(*cacheStats), err
}

func (c *Client) acquireTodayAccountCacheStatsFlight(accountID int64, now time.Time) (
	cached *model.AccountCacheStats,
	flight *todayAccountCacheStatsFlight,
	leader bool,
	err error,
) {
	c.todayCacheMu.Lock()
	defer c.todayCacheMu.Unlock()

	entry, ok := c.todayCacheStats[accountID]
	if ok && now.Before(entry.expiresAt) {
		return cloneAccountCacheStats(entry.stats), nil, false, nil
	}
	if ok {
		delete(c.todayCacheStats, accountID)
	}

	if retryAt, failed := c.todayCacheFailures[accountID]; failed {
		if now.Before(retryAt) {
			return nil, nil, false, errTodayAccountCacheStatsBackoff
		}
		delete(c.todayCacheFailures, accountID)
	}

	if flight = c.todayCacheFlights[accountID]; flight != nil {
		return nil, flight, false, nil
	}
	if c.todayCacheFlights == nil {
		c.todayCacheFlights = make(map[int64]*todayAccountCacheStatsFlight)
	}
	flight = &todayAccountCacheStatsFlight{done: make(chan struct{})}
	c.todayCacheFlights[accountID] = flight
	return nil, flight, true, nil
}

func (c *Client) completeTodayAccountCacheStatsFlight(
	accountID int64,
	flight *todayAccountCacheStatsFlight,
	stats *model.AccountCacheStats,
	err error,
	now time.Time,
) {
	c.todayCacheMu.Lock()
	defer c.todayCacheMu.Unlock()

	if err == nil && stats != nil {
		if c.todayCacheStats == nil {
			c.todayCacheStats = make(map[int64]cachedAccountCacheStats)
		}
		c.todayCacheStats[accountID] = cachedAccountCacheStats{
			stats:     *stats,
			expiresAt: now.Add(todayAccountCacheStatsTTL),
		}
		delete(c.todayCacheFailures, accountID)
	} else {
		if c.todayCacheFailures == nil {
			c.todayCacheFailures = make(map[int64]time.Time)
		}
		c.todayCacheFailures[accountID] = now.Add(todayAccountCacheStatsRetryBackoff)
	}

	if stats != nil {
		flight.stats = cloneAccountCacheStats(*stats)
	}
	flight.err = err
	delete(c.todayCacheFlights, accountID)
	close(flight.done)
}

func (c *Client) fetchTodayAccountCacheStats(ctx context.Context, accountID int64) (*model.AccountCacheStats, error) {
	var response struct {
		InputTokens         int64 `json:"total_input_tokens"`
		CacheCreationTokens int64 `json:"total_cache_creation_tokens"`
		CacheReadTokens     int64 `json:"total_cache_read_tokens"`
	}
	query := url.Values{
		"account_id": {strconv.FormatInt(accountID, 10)},
		"period":     {"today"},
	}
	if err := c.adminJSON(ctx, http.MethodGet, "/admin/usage/stats?"+query.Encode(), nil, &response); err != nil {
		return nil, err
	}

	cacheStats := model.AccountCacheStats{
		InputTokens:         max(response.InputTokens, 0),
		CacheCreationTokens: max(response.CacheCreationTokens, 0),
		CacheReadTokens:     max(response.CacheReadTokens, 0),
	}
	// Match the usage-record page: cache writes are displayed separately and do
	// not participate in the cache-hit denominator.
	cacheStats.PromptTokens = cacheStats.InputTokens + cacheStats.CacheReadTokens
	if cacheStats.PromptTokens > 0 {
		cacheStats.HitRate = float64(cacheStats.CacheReadTokens) / float64(cacheStats.PromptTokens) * 100
	}
	return &cacheStats, nil
}

func cloneAccountCacheStats(stats model.AccountCacheStats) *model.AccountCacheStats {
	copy := stats
	return &copy
}

func (c *Client) GetPassiveUsage(ctx context.Context, accountID int64) (model.AccountUsageInfo, error) {
	var usage model.AccountUsageInfo
	path := "/admin/accounts/" + strconv.FormatInt(accountID, 10) + "/usage?source=passive"
	if err := c.adminJSON(ctx, http.MethodGet, path, nil, &usage); err != nil {
		return model.AccountUsageInfo{}, err
	}
	return usage, nil
}

func (c *Client) GetModelPricing(ctx context.Context, modelID string) (ModelPricing, error) {
	modelID = strings.TrimSpace(modelID)
	if modelID == "" {
		return ModelPricing{}, errors.New("model ID is required")
	}
	query := url.Values{"model": {modelID}}
	var response struct {
		Found           bool     `json:"found"`
		InputPrice      *float64 `json:"input_price"`
		OutputPrice     *float64 `json:"output_price"`
		CacheWritePrice *float64 `json:"cache_write_price"`
		CacheReadPrice  *float64 `json:"cache_read_price"`
	}
	if err := c.adminJSON(ctx, http.MethodGet, "/admin/channels/model-pricing?"+query.Encode(), nil, &response); err != nil {
		return ModelPricing{}, err
	}
	return ModelPricing{
		Found:           response.Found,
		InputPrice:      response.InputPrice,
		OutputPrice:     response.OutputPrice,
		CacheWritePrice: response.CacheWritePrice,
		CacheReadPrice:  response.CacheReadPrice,
	}, nil
}

func (c *Client) SetAccountGroup(ctx context.Context, accountID, groupID int64, bound bool) (model.UpstreamAccount, error) {
	if accountID <= 0 || groupID <= 0 {
		return model.UpstreamAccount{}, errors.New("account ID and group ID must be positive")
	}
	account, err := c.GetAnyAccount(ctx, accountID)
	if err != nil {
		return model.UpstreamAccount{}, err
	}
	groupIDs := append([]int64(nil), account.GroupIDs...)
	contains := false
	for _, existingID := range groupIDs {
		if existingID == groupID {
			contains = true
			break
		}
	}
	if contains == bound {
		return account, nil
	}
	if bound {
		groupIDs = append(groupIDs, groupID)
	} else {
		filtered := groupIDs[:0]
		for _, existingID := range groupIDs {
			if existingID != groupID {
				filtered = append(filtered, existingID)
			}
		}
		groupIDs = filtered
	}
	sort.Slice(groupIDs, func(i, j int) bool { return groupIDs[i] < groupIDs[j] })
	var updated model.UpstreamAccount
	path := "/admin/accounts/" + strconv.FormatInt(accountID, 10)
	if err := c.adminJSON(ctx, http.MethodPut, path, map[string]any{"group_ids": groupIDs}, &updated); err != nil {
		return model.UpstreamAccount{}, err
	}
	return updated, nil
}

func (c *Client) TestAccount(
	ctx context.Context,
	accountID int64,
	modelID string,
	prompt string,
) (ProbeOutcome, error) {
	startedAt := time.Now()
	payload, err := json.Marshal(map[string]string{
		"model_id": strings.TrimSpace(modelID),
		"prompt":   strings.TrimSpace(prompt),
	})
	if err != nil {
		return ProbeOutcome{}, err
	}

	endpoint := c.apiRoot + "/admin/accounts/" + strconv.FormatInt(accountID, 10) + "/test"
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, bytes.NewReader(payload))
	if err != nil {
		return ProbeOutcome{}, err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "text/event-stream")
	req.Header.Set("x-api-key", c.adminAPIKey)

	resp, err := c.streamHTTP.Do(req)
	if err != nil {
		return ProbeOutcome{Latency: time.Since(startedAt)}, err
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		message := readErrorMessage(resp.Body)
		return ProbeOutcome{Latency: time.Since(startedAt)}, fmt.Errorf("account test returned HTTP %d: %s", resp.StatusCode, message)
	}

	var responseText strings.Builder
	completed := false
	success := false
	errorMessage := ""
	var outcomeUsage *model.ProbeUsage
	scanner := bufio.NewScanner(resp.Body)
	scanner.Buffer(make([]byte, 64*1024), maxSSEEventBytes)
	for scanner.Scan() {
		line := bytes.TrimSpace(scanner.Bytes())
		if !bytes.HasPrefix(line, []byte("data:")) {
			continue
		}
		data := bytes.TrimSpace(bytes.TrimPrefix(line, []byte("data:")))
		if bytes.Equal(data, []byte("[DONE]")) || len(data) == 0 {
			continue
		}
		var event testEvent
		if err := json.Unmarshal(data, &event); err != nil {
			continue
		}
		if usage := normalizeProbeUsage(event.Usage, event.Model); usage != nil {
			outcomeUsage = usage
		}
		switch event.Type {
		case "content":
			appendLimited(&responseText, event.Text, maxResponseTextBytes)
		case "error":
			errorMessage = strings.TrimSpace(event.Error)
			if errorMessage == "" {
				errorMessage = "account test failed"
			}
			return ProbeOutcome{
				Success:      false,
				ResponseText: responseText.String(),
				ErrorMessage: errorMessage,
				Latency:      time.Since(startedAt),
				Usage:        outcomeUsage,
			}, nil
		case "test_complete":
			completed = true
			success = event.Success
		}
		if completed {
			break
		}
	}
	if err := scanner.Err(); err != nil {
		return ProbeOutcome{Latency: time.Since(startedAt), ResponseText: responseText.String()}, err
	}
	if !completed {
		return ProbeOutcome{
			Success:      false,
			ResponseText: responseText.String(),
			ErrorMessage: "stream ended before test_complete",
			Latency:      time.Since(startedAt),
			Usage:        outcomeUsage,
		}, nil
	}
	if !success {
		errorMessage = "account test did not report success"
	}
	return ProbeOutcome{
		Success:      success,
		ResponseText: responseText.String(),
		ErrorMessage: errorMessage,
		Latency:      time.Since(startedAt),
		Usage:        outcomeUsage,
	}, nil
}

func (c *Client) SetSchedulable(ctx context.Context, accountID int64, schedulable bool) (model.UpstreamAccount, error) {
	var account model.UpstreamAccount
	path := "/admin/accounts/" + strconv.FormatInt(accountID, 10) + "/schedulable"
	body := map[string]bool{"schedulable": schedulable}
	if err := c.adminJSON(ctx, http.MethodPost, path, body, &account); err != nil {
		return model.UpstreamAccount{}, err
	}
	return account, nil
}

// SetFinalCostMultiplier updates only the optional scheduling signal through
// Sub2API's existing atomic Extra-merge endpoint. It never sends
// rate_multiplier, so account billing and quota accounting remain unchanged. A
// nil multiplier clears the signal and restores legacy scheduling behavior.
func (c *Client) SetFinalCostMultiplier(ctx context.Context, accountID int64, multiplier *float64) (model.UpstreamAccount, error) {
	if accountID <= 0 {
		return model.UpstreamAccount{}, errors.New("account ID must be positive")
	}
	body := map[string]any{
		"account_ids": []int64{accountID},
		"extra": map[string]any{
			model.FinalCostMultiplierExtraKey: multiplier,
		},
	}
	if err := c.adminJSON(ctx, http.MethodPost, "/admin/accounts/bulk-update", body, nil); err != nil {
		return model.UpstreamAccount{}, err
	}
	return model.UpstreamAccount{}, nil
}

// SetAccountBalanceQuota projects an upstream balance into Sub2API's existing
// account quota Extra fields. It never sends rate_multiplier, status, or
// schedulable state.
func (c *Client) SetAccountBalanceQuota(ctx context.Context, accountID int64, update model.AccountBalanceQuotaUpdate) error {
	if accountID <= 0 {
		return errors.New("account ID must be positive")
	}
	if update.QuotaLimit < 0 || math.IsNaN(update.QuotaLimit) || math.IsInf(update.QuotaLimit, 0) {
		return errors.New("quota limit must be a finite number >= 0")
	}
	if update.QuotaUsed != nil && (*update.QuotaUsed < 0 || math.IsNaN(*update.QuotaUsed) || math.IsInf(*update.QuotaUsed, 0)) {
		return errors.New("quota used must be a finite number >= 0")
	}
	if update.Remaining != nil && (*update.Remaining < 0 || math.IsNaN(*update.Remaining) || math.IsInf(*update.Remaining, 0)) {
		return errors.New("quota remaining must be a finite number >= 0")
	}

	extra := map[string]any{
		"quota_limit": update.QuotaLimit,
		model.UpstreamBalanceQuotaManagedExtraKey:    update.Managed,
		model.UpstreamBalanceQuotaRemainingExtraKey:  update.Remaining,
		model.UpstreamBalanceQuotaExhaustedExtraKey:  update.Exhausted,
		model.UpstreamBalanceQuotaUnlimitedExtraKey:  update.Unlimited,
		model.UpstreamBalanceQuotaObservedAtExtraKey: nil,
	}
	if update.QuotaUsed != nil {
		extra["quota_used"] = *update.QuotaUsed
	}
	if update.ObservedAt != nil && !update.ObservedAt.IsZero() {
		extra[model.UpstreamBalanceQuotaObservedAtExtraKey] = update.ObservedAt.UTC().Format(time.RFC3339Nano)
	}
	body := map[string]any{
		"account_ids": []int64{accountID},
		"extra":       extra,
	}
	return c.adminJSON(ctx, http.MethodPost, "/admin/accounts/bulk-update", body, nil)
}

func (c *Client) ValidateAdminJWT(
	ctx context.Context,
	token string,
	identity ForwardedIdentity,
) (AdminUser, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.apiRoot+"/auth/me", nil)
	if err != nil {
		return AdminUser{}, err
	}
	req.Header.Set("Authorization", "Bearer "+strings.TrimSpace(token))
	if identity.UserAgent != "" {
		req.Header.Set("User-Agent", identity.UserAgent)
	}
	if identity.ClientIP != "" {
		req.Header.Set("X-Forwarded-For", identity.ClientIP)
		req.Header.Set("X-Real-IP", identity.ClientIP)
	}
	var user AdminUser
	if err := c.doJSON(req, &user); err != nil {
		return AdminUser{}, err
	}
	if user.Role != "admin" {
		return AdminUser{}, errors.New("admin access required")
	}
	return user, nil
}

func (c *Client) ExportAPIKeySecrets(
	ctx context.Context,
	accountIDs []int64,
	adminJWT string,
	identity ForwardedIdentity,
) (map[int64]string, error) {
	adminJWT = strings.TrimSpace(adminJWT)
	if adminJWT == "" {
		return nil, errors.New("administrator JWT is required")
	}
	ids := uniquePositiveIDs(accountIDs)
	if len(ids) == 0 {
		return map[int64]string{}, nil
	}
	if len(ids) > 10000 {
		return nil, errors.New("too many accounts requested")
	}
	secrets := make(map[int64]string, len(ids))
	for _, accountID := range ids {
		query := url.Values{
			"ids":             {strconv.FormatInt(accountID, 10)},
			"include_proxies": {"false"},
		}
		req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.apiRoot+"/admin/accounts/data?"+query.Encode(), nil)
		if err != nil {
			return nil, err
		}
		req.Header.Set("Accept", "application/json")
		req.Header.Set("Authorization", "Bearer "+adminJWT)
		if identity.UserAgent != "" {
			req.Header.Set("User-Agent", identity.UserAgent)
		}
		if identity.ClientIP != "" {
			req.Header.Set("X-Forwarded-For", identity.ClientIP)
			req.Header.Set("X-Real-IP", identity.ClientIP)
		}
		var payload struct {
			Accounts []struct {
				Type        string         `json:"type"`
				Credentials map[string]any `json:"credentials"`
			} `json:"accounts"`
		}
		if err := c.doJSON(req, &payload); err != nil {
			return nil, err
		}
		if len(payload.Accounts) != 1 || !strings.EqualFold(strings.TrimSpace(payload.Accounts[0].Type), "apikey") {
			return nil, fmt.Errorf("account %d export did not return one API Key account", accountID)
		}
		secret, _ := payload.Accounts[0].Credentials["api_key"].(string)
		secret = strings.TrimSpace(secret)
		if secret == "" {
			return nil, fmt.Errorf("account %d export did not include api_key", accountID)
		}
		secrets[accountID] = secret
	}
	return secrets, nil
}

// ExportDirectProbeSnapshot forwards the current browser administrator JWT to
// Sub2API's existing step-up-protected export endpoint. This method never uses
// the service Admin API key because exporting credentials is an explicit human
// authorization action.
func (c *Client) ExportDirectProbeSnapshot(
	ctx context.Context,
	accountID int64,
	adminJWT string,
	identity ForwardedIdentity,
) (DirectProbeExport, error) {
	adminJWT = strings.TrimSpace(adminJWT)
	if accountID <= 0 {
		return DirectProbeExport{}, errors.New("account ID must be positive")
	}
	if adminJWT == "" {
		return DirectProbeExport{}, errors.New("administrator JWT is required")
	}

	query := url.Values{
		"ids":             {strconv.FormatInt(accountID, 10)},
		"include_proxies": {"true"},
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.apiRoot+"/admin/accounts/data?"+query.Encode(), nil)
	if err != nil {
		return DirectProbeExport{}, err
	}
	req.Header.Set("Accept", "application/json")
	req.Header.Set("Authorization", "Bearer "+adminJWT)
	if identity.UserAgent != "" {
		req.Header.Set("User-Agent", identity.UserAgent)
	}
	if identity.ClientIP != "" {
		req.Header.Set("X-Forwarded-For", identity.ClientIP)
		req.Header.Set("X-Real-IP", identity.ClientIP)
	}

	var payload accountExportPayload
	if err := c.doJSON(req, &payload); err != nil {
		return DirectProbeExport{}, err
	}
	if len(payload.Accounts) != 1 {
		return DirectProbeExport{}, fmt.Errorf("account %d export did not return exactly one account", accountID)
	}
	snapshot, err := directProbeSnapshotFromExport(accountID, payload)
	if err != nil {
		return DirectProbeExport{}, err
	}
	return DirectProbeExport{Snapshot: snapshot}, nil
}

type accountExportPayload struct {
	Accounts []exportedAccount `json:"accounts"`
	Proxies  []exportedProxy   `json:"proxies"`
}

type exportedAccount struct {
	Platform    string         `json:"platform"`
	Type        string         `json:"type"`
	Credentials map[string]any `json:"credentials"`
	Extra       map[string]any `json:"extra"`
	ProxyKey    *string        `json:"proxy_key"`
}

type exportedProxy struct {
	ProxyKey  string `json:"proxy_key"`
	Protocol  string `json:"protocol"`
	Host      string `json:"host"`
	Port      int    `json:"port"`
	Username  string `json:"username"`
	Password  string `json:"password"`
	Status    string `json:"status"`
	ExpiresAt *int64 `json:"expires_at"`
}

func directProbeSnapshotFromExport(accountID int64, payload accountExportPayload) (snapshot model.DirectProbeSnapshot, err error) {
	account := payload.Accounts[0]
	if !strings.EqualFold(strings.TrimSpace(account.Type), "apikey") {
		return model.DirectProbeSnapshot{}, errors.New("only API Key accounts are supported")
	}
	platform := model.NormalizeDirectProbePlatform(account.Platform)
	if !model.IsDirectProbePlatformSupported(platform) {
		return model.DirectProbeSnapshot{}, fmt.Errorf("暂不支持 %s 平台的直连探测", strings.TrimSpace(account.Platform))
	}
	apiKey, _ := account.Credentials["api_key"].(string)
	apiKey = strings.TrimSpace(apiKey)
	if apiKey == "" {
		return model.DirectProbeSnapshot{}, errors.New("账号导出未包含 API Key")
	}
	baseURL, _ := account.Credentials["base_url"].(string)
	baseURL, err = model.EffectiveDirectProbeBaseURL(baseURL, platform)
	if err != nil {
		return model.DirectProbeSnapshot{}, err
	}

	snapshot = model.DirectProbeSnapshot{
		Version:             model.DirectProbeSnapshotVersion,
		AccountID:           accountID,
		Platform:            platform,
		BaseURL:             baseURL,
		APIKey:              apiKey,
		ModelMapping:        model.DirectProbeModelMapping(account.Credentials),
		HeaderOverrides:     model.FilterDirectProbeHeaderOverrides(directProbeHeaderOverridesEnabled(platform, account.Type, account.Credentials), account.Credentials["header_overrides"]),
		AnthropicAuthMode:   mapString(account.Extra, "anthropic_apikey_auth_scheme"),
		OpenAIResponsesMode: mapString(account.Extra, "openai_responses_mode"),
		OpenAIResponsesOK:   mapBoolPointer(account.Extra, "openai_responses_supported"),
	}
	if account.ProxyKey != nil && strings.TrimSpace(*account.ProxyKey) != "" {
		proxy, found := findExportedProxy(payload.Proxies, *account.ProxyKey)
		if !found {
			model.ClearDirectProbeSnapshot(&snapshot)
			return model.DirectProbeSnapshot{}, errors.New("账号配置的代理无法从授权导出中读取")
		}
		snapshot.Proxy = &model.DirectProbeProxy{
			Protocol:  proxy.Protocol,
			Host:      proxy.Host,
			Port:      proxy.Port,
			Username:  proxy.Username,
			Password:  proxy.Password,
			Status:    proxy.Status,
			ExpiresAt: cloneInt64Pointer(proxy.ExpiresAt),
		}
		if _, proxyErr := model.DirectProbeProxyURL(snapshot.Proxy, time.Now()); proxyErr != nil {
			model.ClearDirectProbeSnapshot(&snapshot)
			return model.DirectProbeSnapshot{}, proxyErr
		}
	}
	snapshot.RoutingFingerprint = model.DirectProbeRoutingFingerprint(snapshot)
	return snapshot, nil
}

func directProbeHeaderOverridesEnabled(platform, accountType string, credentials map[string]any) bool {
	enabled, _ := credentials["header_override_enabled"].(bool)
	if !enabled || !strings.EqualFold(strings.TrimSpace(accountType), "apikey") {
		return false
	}
	switch model.NormalizeDirectProbePlatform(platform) {
	case "openai", "anthropic", "grok":
		return true
	default:
		return false
	}
}

func findExportedProxy(proxies []exportedProxy, key string) (exportedProxy, bool) {
	for _, proxy := range proxies {
		if proxy.ProxyKey == key {
			return proxy, true
		}
	}
	return exportedProxy{}, false
}

func mapString(values map[string]any, key string) string {
	value, _ := values[key].(string)
	return strings.TrimSpace(value)
}

func mapBoolPointer(values map[string]any, key string) *bool {
	value, ok := values[key].(bool)
	if !ok {
		return nil
	}
	return &value
}

func cloneInt64Pointer(value *int64) *int64 {
	if value == nil {
		return nil
	}
	copy := *value
	return &copy
}

func (c *Client) RegisterAdminMenu(ctx context.Context, publicURL string) error {
	publicURL = strings.TrimRight(strings.TrimSpace(publicURL), "/") + "/"
	var settings struct {
		CustomMenuItems []map[string]any `json:"custom_menu_items"`
	}
	if err := c.adminJSON(ctx, http.MethodGet, "/admin/settings", nil, &settings); err != nil {
		return fmt.Errorf("load admin settings: %w", err)
	}

	maxOrder := -1
	found := false
	for _, item := range settings.CustomMenuItems {
		if order, ok := numberAsInt(item["sort_order"]); ok && order > maxOrder {
			maxOrder = order
		}
		if strings.TrimSpace(stringValue(item["id"])) != menuItemID {
			continue
		}
		item["label"] = "账号自动调度"
		item["url"] = publicURL
		item["content_type"] = "url"
		item["visibility"] = "admin"
		item["icon_svg"] = schedulerMenuIcon
		found = true
	}
	if !found {
		settings.CustomMenuItems = append(settings.CustomMenuItems, map[string]any{
			"id":           menuItemID,
			"label":        "账号自动调度",
			"url":          publicURL,
			"content_type": "url",
			"visibility":   "admin",
			"sort_order":   maxOrder + 1,
			"icon_svg":     schedulerMenuIcon,
		})
	}

	payload := map[string]any{"custom_menu_items": settings.CustomMenuItems}
	if err := c.adminJSON(ctx, http.MethodPut, "/admin/settings", payload, nil); err != nil {
		return fmt.Errorf("update admin menu: %w", err)
	}
	return nil
}

func (c *Client) adminJSON(ctx context.Context, method, path string, body any, target any) error {
	var reader io.Reader
	if body != nil {
		raw, err := json.Marshal(body)
		if err != nil {
			return err
		}
		reader = bytes.NewReader(raw)
	}
	req, err := http.NewRequestWithContext(ctx, method, c.apiRoot+path, reader)
	if err != nil {
		return err
	}
	req.Header.Set("Accept", "application/json")
	req.Header.Set("x-api-key", c.adminAPIKey)
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	return c.doJSON(req, target)
}

func (c *Client) doJSON(req *http.Request, target any) error {
	resp, err := c.httpClient.Do(req)
	if err != nil {
		return err
	}
	defer func() { _ = resp.Body.Close() }()
	raw, err := io.ReadAll(io.LimitReader(resp.Body, maxJSONResponseBytes+1))
	if err != nil {
		return err
	}
	if len(raw) > maxJSONResponseBytes {
		return errors.New("Sub2API response exceeds size limit")
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return &HTTPError{StatusCode: resp.StatusCode, Code: envelopeCode(raw), Message: envelopeMessage(raw)}
	}
	if len(bytes.TrimSpace(raw)) == 0 {
		return nil
	}

	var envelope apiEnvelope
	if err := json.Unmarshal(raw, &envelope); err == nil && envelope.Data != nil {
		if !isSuccessCode(envelope.Code) {
			message := strings.TrimSpace(envelope.Message)
			if message == "" {
				message = "Sub2API request failed"
			}
			return errors.New(message)
		}
		if target == nil || len(bytes.TrimSpace(envelope.Data)) == 0 || bytes.Equal(bytes.TrimSpace(envelope.Data), []byte("null")) {
			return nil
		}
		return json.Unmarshal(envelope.Data, target)
	}
	if target == nil {
		return nil
	}
	return json.Unmarshal(raw, target)
}

func uniquePositiveIDs(values []int64) []int64 {
	seen := make(map[int64]struct{}, len(values))
	result := make([]int64, 0, len(values))
	for _, value := range values {
		if value <= 0 {
			continue
		}
		if _, exists := seen[value]; exists {
			continue
		}
		seen[value] = struct{}{}
		result = append(result, value)
	}
	sort.Slice(result, func(i, j int) bool { return result[i] < result[j] })
	return result
}

func isSuccessCode(raw json.RawMessage) bool {
	trimmed := strings.Trim(strings.TrimSpace(string(raw)), `"`)
	return trimmed == "" || trimmed == "0"
}

func envelopeMessage(raw []byte) string {
	var envelope apiEnvelope
	if json.Unmarshal(raw, &envelope) == nil && strings.TrimSpace(envelope.Message) != "" {
		return strings.TrimSpace(envelope.Message)
	}
	message := strings.TrimSpace(string(raw))
	if len(message) > maxErrorBodyBytes {
		message = message[:maxErrorBodyBytes]
	}
	if message == "" {
		return http.StatusText(http.StatusBadGateway)
	}
	return message
}

func envelopeCode(raw []byte) string {
	var envelope apiEnvelope
	if json.Unmarshal(raw, &envelope) != nil {
		return ""
	}
	return strings.Trim(strings.TrimSpace(string(envelope.Code)), `"`)
}

func readErrorMessage(reader io.Reader) string {
	raw, _ := io.ReadAll(io.LimitReader(reader, maxErrorBodyBytes))
	return envelopeMessage(raw)
}

func appendLimited(builder *strings.Builder, text string, limit int) {
	if text == "" || builder.Len() >= limit {
		return
	}
	remaining := limit - builder.Len()
	if len(text) > remaining {
		text = text[:remaining]
	}
	builder.WriteString(text)
}

func numberAsInt(value any) (int, bool) {
	switch number := value.(type) {
	case float64:
		return int(number), true
	case int:
		return number, true
	case json.Number:
		parsed, err := strconv.Atoi(number.String())
		return parsed, err == nil
	default:
		return 0, false
	}
}

func stringValue(value any) string {
	if text, ok := value.(string); ok {
		return text
	}
	return ""
}

const schedulerMenuIcon = `<svg xmlns="http://www.w3.org/2000/svg" width="24" height="24" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round"><path d="M22 12h-4l-3 9L9 3l-3 9H2"/></svg>`
