package core

import (
	"context"
	"fmt"
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
	onlineUserWindow              = 10 * time.Minute
	onlineUserRequestTimeout      = 12 * time.Second
	onlineUserPageSize            = 100
	onlineUserMaxPages            = 100
	onlineUserFallbackPageSize    = 1000
	onlineUserFallbackMaxPages    = 10
	onlineUserIdentityConcurrency = 8
	onlineUserIdentityCacheTTL    = 5 * time.Minute
	onlineUserUsageLimit          = 200
)

type onlineUserCandidate struct {
	id         int64
	lastCallAt time.Time
	email      string
	username   string
}

type opsRequestDetail struct {
	CreatedAt time.Time `json:"created_at"`
	UserID    *int64    `json:"user_id"`
}

type adminUsageLog struct {
	UserID              int64           `json:"user_id"`
	CreatedAt           time.Time       `json:"created_at"`
	InputTokens         int64           `json:"input_tokens"`
	OutputTokens        int64           `json:"output_tokens"`
	CacheCreationTokens int64           `json:"cache_creation_tokens"`
	CacheReadTokens     int64           `json:"cache_read_tokens"`
	ActualCost          float64         `json:"actual_cost"`
	User                *adminUsageUser `json:"user"`
}

type adminUsageUser struct {
	ID       int64  `json:"id"`
	Email    string `json:"email"`
	Username string `json:"username"`
}

type todayUserBreakdown struct {
	UserID      int64   `json:"user_id"`
	Email       string  `json:"email"`
	Requests    int64   `json:"requests"`
	TotalTokens int64   `json:"total_tokens"`
	ActualCost  float64 `json:"actual_cost"`
}

type todayUserUsage struct {
	cost     *float64
	tokens   *int64
	requests *int64
	email    string
}

type batchTodayUsage struct {
	TodayActualCost float64 `json:"today_actual_cost"`
	TodayTokens     *int64  `json:"today_tokens"`
	TodayRequests   *int64  `json:"today_requests"`
}

type adminUserIdentity struct {
	ID       int64  `json:"id"`
	Email    string `json:"email"`
	Username string `json:"username"`
}

type cachedOnlineUserIdentity struct {
	identity  adminUserIdentity
	expiresAt time.Time
}

// GetOnlineUsers returns the users that have a recorded request in the last
// ten minutes. It intentionally uses existing administrator HTTP endpoints;
// the sidecar never opens a database connection to Sub2API.
func (c *Client) GetOnlineUsers(ctx context.Context) (model.OnlineUsersSnapshot, error) {
	queryCtx, cancel := context.WithTimeout(ctx, onlineUserRequestTimeout)
	defer cancel()
	return c.getOnlineUsersAt(queryCtx, time.Now().UTC())
}

func (c *Client) getOnlineUsersAt(ctx context.Context, now time.Time) (model.OnlineUsersSnapshot, error) {
	now = now.UTC()
	start := now.Add(-onlineUserWindow)

	candidates, source, truncated, opsErr := c.collectOnlineUsersFromOps(ctx, start, now)
	if opsErr != nil {
		var fallbackErr error
		candidates, truncated, fallbackErr = c.collectOnlineUsersFromUsage(ctx, start, now)
		if fallbackErr != nil {
			return model.OnlineUsersSnapshot{}, fmt.Errorf("read online users: ops: %v; fallback: %w", opsErr, fallbackErr)
		}
		source = "usage_fallback"
	}

	ids := make([]int64, 0, len(candidates))
	for id := range candidates {
		ids = append(ids, id)
	}
	sort.Slice(ids, func(i, j int) bool { return ids[i] < ids[j] })

	usageByID, usageErr := c.getTodayOnlineUserUsage(ctx, ids, now)
	partial := source != "ops"
	notices := make([]string, 0, 4)
	if source != "ops" {
		notices = append(notices, "Ops 请求明细不可用，当前仅根据成功用量记录判定在线")
	}
	if truncated {
		partial = true
		notices = append(notices, "近期请求量较大，在线请求列表已达到读取上限")
	}
	if usageErr != nil {
		partial = true
		notices = append(notices, "今日消耗暂不可用")
	}
	if usageErr == nil {
		missingUsage := 0
		for id := range candidates {
			if usageByID[id].cost == nil {
				missingUsage++
			}
		}
		if missingUsage > 0 {
			partial = true
			notices = append(notices, "部分用户今日消耗暂不可用")
		}
	}

	identityErrors := c.enrichOnlineUserIdentities(ctx, candidates, usageByID)
	if identityErrors > 0 {
		partial = true
		notices = append(notices, "部分用户身份暂不可用")
	}

	users := make([]model.OnlineUser, 0, len(candidates))
	for _, candidate := range candidates {
		usage := usageByID[candidate.id]
		email := strings.TrimSpace(candidate.email)
		if email == "" {
			email = strings.TrimSpace(usage.email)
		}
		username := strings.TrimSpace(candidate.username)
		displayName := username
		if displayName == "" {
			displayName = email
		}
		if displayName == "" {
			displayName = fmt.Sprintf("用户 #%d", candidate.id)
		}
		users = append(users, model.OnlineUser{
			ID:            candidate.id,
			DisplayName:   displayName,
			Email:         email,
			LastCallAt:    candidate.lastCallAt.UTC(),
			TodayCost:     usage.cost,
			TodayTokens:   usage.tokens,
			TodayRequests: usage.requests,
		})
	}
	sort.Slice(users, func(i, j int) bool {
		if !users[i].LastCallAt.Equal(users[j].LastCallAt) {
			return users[i].LastCallAt.After(users[j].LastCallAt)
		}
		return users[i].ID < users[j].ID
	})

	return model.OnlineUsersSnapshot{
		Count:         len(users),
		WindowMinutes: int(onlineUserWindow / time.Minute),
		QueriedAt:     now,
		Source:        source,
		Partial:       partial,
		Truncated:     truncated,
		Notice:        strings.Join(notices, "；"),
		Users:         users,
	}, nil
}

func (c *Client) collectOnlineUsersFromOps(ctx context.Context, start, end time.Time) (map[int64]onlineUserCandidate, string, bool, error) {
	result := make(map[int64]onlineUserCandidate)
	query := url.Values{}
	query.Set("start_time", start.UTC().Format(time.RFC3339Nano))
	query.Set("end_time", end.UTC().Format(time.RFC3339Nano))
	query.Set("kind", "all")
	query.Set("sort", "created_at_desc")
	query.Set("page_size", strconv.Itoa(onlineUserPageSize))

	truncated := false
	for page := 1; page <= onlineUserMaxPages; page++ {
		query.Set("page", strconv.Itoa(page))
		var response pageResponse[opsRequestDetail]
		if err := c.adminJSON(ctx, http.MethodGet, "/admin/ops/requests?"+query.Encode(), nil, &response); err != nil {
			return nil, "", false, err
		}
		if len(response.Items) == 0 {
			break
		}
		oldest := end
		for _, item := range response.Items {
			createdAt := item.CreatedAt.UTC()
			if createdAt.Before(oldest) {
				oldest = createdAt
			}
			if item.UserID == nil || *item.UserID <= 0 || createdAt.Before(start) || !createdAt.Before(end) {
				continue
			}
			id := *item.UserID
			candidate, exists := result[id]
			if !exists || createdAt.After(candidate.lastCallAt) {
				result[id] = onlineUserCandidate{id: id, lastCallAt: createdAt}
			}
		}
		if oldest.Before(start) || !hasMoreOnlineUserPages(page, response, onlineUserPageSize) {
			break
		}
		if page == onlineUserMaxPages {
			truncated = true
		}
	}
	return result, "ops", truncated, nil
}

func (c *Client) collectOnlineUsersFromUsage(ctx context.Context, start, end time.Time) (map[int64]onlineUserCandidate, bool, error) {
	result := make(map[int64]onlineUserCandidate)
	query := url.Values{}
	query.Set("start_date", start.Format("2006-01-02"))
	query.Set("end_date", end.Format("2006-01-02"))
	query.Set("sort_by", "created_at")
	query.Set("sort_order", "desc")
	query.Set("page_size", strconv.Itoa(onlineUserFallbackPageSize))

	truncated := false
	for page := 1; page <= onlineUserFallbackMaxPages; page++ {
		query.Set("page", strconv.Itoa(page))
		var response pageResponse[adminUsageLog]
		if err := c.adminJSON(ctx, http.MethodGet, "/admin/usage?"+query.Encode(), nil, &response); err != nil {
			return nil, false, err
		}
		if len(response.Items) == 0 {
			break
		}
		oldest := end
		for _, item := range response.Items {
			createdAt := item.CreatedAt.UTC()
			if createdAt.Before(oldest) {
				oldest = createdAt
			}
			if item.UserID <= 0 || createdAt.Before(start) || !createdAt.Before(end) {
				continue
			}
			candidate := result[item.UserID]
			if candidate.id == 0 || createdAt.After(candidate.lastCallAt) {
				candidate.id = item.UserID
				candidate.lastCallAt = createdAt
			}
			if item.User != nil {
				if candidate.email == "" {
					candidate.email = strings.TrimSpace(item.User.Email)
				}
				if candidate.username == "" {
					candidate.username = strings.TrimSpace(item.User.Username)
				}
			}
			result[item.UserID] = candidate
		}
		if oldest.Before(start) || !hasMoreOnlineUserPages(page, response, onlineUserFallbackPageSize) {
			break
		}
		if page == onlineUserFallbackMaxPages {
			truncated = true
		}
	}
	return result, truncated, nil
}

func (c *Client) getTodayOnlineUserUsage(ctx context.Context, userIDs []int64, now time.Time) (map[int64]todayUserUsage, error) {
	result := make(map[int64]todayUserUsage, len(userIDs))
	if len(userIDs) == 0 {
		return result, nil
	}

	breakdown, breakdownErr := c.getTodayUserBreakdown(ctx, now)
	for id, value := range breakdown {
		if !hasPositiveID(userIDs, id) {
			continue
		}
		cost := value.ActualCost
		tokens := value.TotalTokens
		requests := value.Requests
		result[id] = todayUserUsage{cost: &cost, tokens: &tokens, requests: &requests, email: value.Email}
	}

	batch, batchErr := c.getBatchUserUsage(ctx, userIDs)
	for id, value := range batch {
		entry := result[id]
		if entry.cost == nil {
			cost := value.TodayActualCost
			entry.cost = &cost
		}
		if entry.tokens == nil && value.TodayTokens != nil {
			entry.tokens = value.TodayTokens
		}
		if entry.requests == nil && value.TodayRequests != nil {
			entry.requests = value.TodayRequests
		}
		result[id] = entry
	}
	if breakdownErr != nil && batchErr != nil {
		return result, fmt.Errorf("breakdown: %v; batch: %w", breakdownErr, batchErr)
	}
	return result, nil
}

func (c *Client) getTodayUserBreakdown(ctx context.Context, now time.Time) (map[int64]todayUserBreakdown, error) {
	today := now.In(time.Local).Format("2006-01-02")
	query := url.Values{}
	query.Set("start_date", today)
	query.Set("end_date", today)
	query.Set("limit", strconv.Itoa(onlineUserUsageLimit))
	query.Set("sort_by", "actual_cost")
	var response struct {
		Users []todayUserBreakdown `json:"users"`
	}
	if err := c.adminJSON(ctx, http.MethodGet, "/admin/dashboard/user-breakdown?"+query.Encode(), nil, &response); err != nil {
		return nil, err
	}
	result := make(map[int64]todayUserBreakdown, len(response.Users))
	for _, item := range response.Users {
		if item.UserID > 0 {
			result[item.UserID] = item
		}
	}
	return result, nil
}

func (c *Client) getBatchUserUsage(ctx context.Context, userIDs []int64) (map[int64]batchTodayUsage, error) {
	var response struct {
		Stats map[string]batchTodayUsage `json:"stats"`
	}
	if err := c.adminJSON(ctx, http.MethodPost, "/admin/dashboard/users-usage", map[string]any{"user_ids": userIDs}, &response); err != nil {
		return nil, err
	}
	result := make(map[int64]batchTodayUsage, len(response.Stats))
	for rawID, value := range response.Stats {
		id, err := strconv.ParseInt(rawID, 10, 64)
		if err == nil && id > 0 {
			result[id] = value
		}
	}
	return result, nil
}

func (c *Client) enrichOnlineUserIdentities(ctx context.Context, candidates map[int64]onlineUserCandidate, usage map[int64]todayUserUsage) int {
	missing := make([]int64, 0)
	for id, candidate := range candidates {
		if strings.TrimSpace(candidate.username) != "" {
			continue
		}
		if identity, ok := c.loadOnlineUserIdentity(id, time.Now()); ok {
			candidate.email = firstNonEmpty(strings.TrimSpace(candidate.email), strings.TrimSpace(identity.Email))
			candidate.username = strings.TrimSpace(identity.Username)
			candidates[id] = candidate
			continue
		}
		// The usage breakdown can provide an email, but not a username. Keep
		// the lookup in that case so the UI can honor username-first display.
		if strings.TrimSpace(candidate.email) == "" && strings.TrimSpace(usage[id].email) != "" {
			candidate.email = strings.TrimSpace(usage[id].email)
			candidates[id] = candidate
		}
		missing = append(missing, id)
	}
	if len(missing) == 0 {
		return 0
	}

	jobs := make(chan int64)
	var wg sync.WaitGroup
	var mu sync.Mutex
	errorsCount := 0
	workerCount := min(onlineUserIdentityConcurrency, len(missing))
	wg.Add(workerCount)
	for range workerCount {
		go func() {
			defer wg.Done()
			for id := range jobs {
				var identity adminUserIdentity
				identity, cached := c.loadOnlineUserIdentity(id, time.Now())
				var err error
				if !cached {
					err = c.adminJSON(ctx, http.MethodGet, "/admin/users/"+strconv.FormatInt(id, 10), nil, &identity)
					if err == nil {
						c.storeOnlineUserIdentity(id, identity, time.Now().Add(onlineUserIdentityCacheTTL))
					}
				}
				mu.Lock()
				candidate := candidates[id]
				if err != nil {
					errorsCount++
				} else {
					candidate.email = strings.TrimSpace(identity.Email)
					candidate.username = strings.TrimSpace(identity.Username)
					candidates[id] = candidate
				}
				mu.Unlock()
			}
		}()
	}
	for _, id := range missing {
		select {
		case jobs <- id:
		case <-ctx.Done():
			mu.Lock()
			errorsCount++
			mu.Unlock()
		}
	}
	close(jobs)
	wg.Wait()
	return errorsCount
}

func (c *Client) loadOnlineUserIdentity(id int64, now time.Time) (adminUserIdentity, bool) {
	c.onlineIdentityMu.Lock()
	defer c.onlineIdentityMu.Unlock()
	if c.onlineIdentityCache == nil {
		return adminUserIdentity{}, false
	}
	entry, ok := c.onlineIdentityCache[id]
	if !ok {
		return adminUserIdentity{}, false
	}
	if !now.Before(entry.expiresAt) {
		delete(c.onlineIdentityCache, id)
		return adminUserIdentity{}, false
	}
	return entry.identity, true
}

func (c *Client) storeOnlineUserIdentity(id int64, identity adminUserIdentity, expiresAt time.Time) {
	c.onlineIdentityMu.Lock()
	defer c.onlineIdentityMu.Unlock()
	if c.onlineIdentityCache == nil {
		c.onlineIdentityCache = make(map[int64]cachedOnlineUserIdentity)
	}
	c.onlineIdentityCache[id] = cachedOnlineUserIdentity{identity: identity, expiresAt: expiresAt}
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return strings.TrimSpace(value)
		}
	}
	return ""
}

func hasPositiveID(ids []int64, target int64) bool {
	for _, id := range ids {
		if id == target {
			return true
		}
	}
	return false
}

// hasMoreOnlineUserPages tolerates older administrator responses that omit
// the derived pages field while still avoiding an unbounded walk. Current
// Sub2API responses include pages, but total/page_size are a safe fallback.
func hasMoreOnlineUserPages[T any](page int, response pageResponse[T], requestedPageSize int) bool {
	if response.Pages > 0 {
		return page < response.Pages
	}
	if response.Total > 0 && response.PageSize > 0 {
		return int64(page*response.PageSize) < response.Total
	}
	return len(response.Items) >= requestedPageSize
}
