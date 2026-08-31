package onlineusers

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"log/slog"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/Wei-Shaw/sub2api-account-auto-scheduler/internal/model"
	_ "github.com/jackc/pgx/v5/stdlib"
)

const (
	windowDuration       = 10 * time.Minute
	defaultCacheTTL      = time.Minute
	defaultQueryTimeout  = 3 * time.Second
	defaultStaleAfter    = 3 * time.Minute
	defaultUnavailableAt = 10 * time.Minute
)

const summaryQuery = `
WITH watermark AS MATERIALIZED (
  SELECT data_through
  FROM channel_monitor_v2_watermarks
  WHERE id = 1
), presence AS (
  SELECT m.user_id, m.group_id
  FROM channel_monitor_v2_user_metrics_1m m
  WHERE m.bucket_start >= (SELECT data_through - INTERVAL '10 minutes' FROM watermark)
    AND m.bucket_start < (SELECT data_through FROM watermark)
    AND (m.success_requests > 0 OR m.error_requests > 0)
  GROUP BY m.user_id, m.group_id
), global_count AS (
  SELECT COUNT(DISTINCT user_id)::BIGINT AS count
  FROM presence
), group_counts AS (
  SELECT group_id, COUNT(DISTINCT user_id)::BIGINT AS count
  FROM presence
  GROUP BY group_id
)
SELECT w.data_through, gc.count, groups.group_id, groups.count
FROM watermark w
CROSS JOIN global_count gc
LEFT JOIN group_counts groups ON TRUE
ORDER BY groups.group_id`

const detailQuery = `
WITH watermark AS MATERIALIZED (
  SELECT data_through
  FROM channel_monitor_v2_watermarks
  WHERE id = 1
), presence AS (
  SELECT m.user_id, m.group_id, MAX(m.bucket_start) AS group_last_call_at
  FROM channel_monitor_v2_user_metrics_1m m
  WHERE m.bucket_start >= (SELECT data_through - INTERVAL '10 minutes' FROM watermark)
    AND m.bucket_start < (SELECT data_through FROM watermark)
    AND (m.success_requests > 0 OR m.error_requests > 0)
  GROUP BY m.user_id, m.group_id
), user_presence AS (
  SELECT user_id, MAX(group_last_call_at) AS last_call_at
  FROM presence
  GROUP BY user_id
), daily_state AS (
  SELECT COALESCE((
    SELECT ready
    FROM user_dashboard_route_daily_state
    WHERE id = 1
  ), FALSE) AS ready
), daily AS (
  SELECT d.user_id,
         SUM(d.requests)::BIGINT AS requests,
         SUM(d.input_tokens + d.output_tokens + d.cache_creation_tokens + d.cache_read_tokens)::BIGINT AS tokens,
         SUM(d.actual_cost)::DOUBLE PRECISION AS actual_cost
  FROM user_dashboard_route_daily d
  JOIN user_presence active ON active.user_id = d.user_id
  CROSS JOIN daily_state state
  WHERE state.ready
    AND d.bucket_date = CURRENT_DATE
  GROUP BY d.user_id
)
SELECT w.data_through,
       state.ready,
       p.user_id,
       p.group_id,
       users.last_call_at,
       COALESCE(identity.email, ''),
       COALESCE(identity.username, ''),
       daily.requests,
       daily.tokens,
       daily.actual_cost
FROM watermark w
CROSS JOIN daily_state state
LEFT JOIN presence p ON TRUE
LEFT JOIN user_presence users ON users.user_id = p.user_id
LEFT JOIN users identity ON identity.id = p.user_id
LEFT JOIN daily ON daily.user_id = p.user_id
ORDER BY users.last_call_at DESC, p.user_id, p.group_id`

// Service serves online-user summaries and details from existing PostgreSQL
// aggregates. It performs no background work; database reads happen only on a
// cache miss caused by an administrator request.
type Service struct {
	db       *sql.DB
	ownsDB   bool
	logger   *slog.Logger
	now      func() time.Time
	cacheTTL time.Duration
	timeout  time.Duration
	staleAt  time.Duration
	deadAt   time.Duration

	mu sync.Mutex

	summaryCached   *model.OnlineUsersSummary
	summaryCachedAt time.Time
	summaryLastGood *model.OnlineUsersSummary
	summaryInFlight *summaryFlight
	detailCached    *model.OnlineUsersSnapshot
	detailCachedAt  time.Time
	detailLastGood  *model.OnlineUsersSnapshot
	detailInFlight  *detailFlight
}

type summaryFlight struct {
	done   chan struct{}
	result model.OnlineUsersSummary
}

type detailFlight struct {
	done   chan struct{}
	result model.OnlineUsersSnapshot
}

// Open creates the optional read-only aggregate service. An empty URL returns
// a disabled service that reports only the online feature unavailable.
func Open(databaseURL string, logger *slog.Logger) (*Service, error) {
	service := newService(nil, logger)
	databaseURL = strings.TrimSpace(databaseURL)
	if databaseURL == "" {
		return service, nil
	}
	db, err := sql.Open("pgx", databaseURL)
	if err != nil {
		return service, fmt.Errorf("open online aggregate database: %w", err)
	}
	db.SetMaxOpenConns(2)
	db.SetMaxIdleConns(1)
	db.SetConnMaxIdleTime(5 * time.Minute)
	db.SetConnMaxLifetime(30 * time.Minute)
	service.db = db
	service.ownsDB = true
	return service, nil
}

func newService(db *sql.DB, logger *slog.Logger) *Service {
	if logger == nil {
		logger = slog.Default()
	}
	return &Service{
		db:       db,
		logger:   logger,
		now:      time.Now,
		cacheTTL: defaultCacheTTL,
		timeout:  defaultQueryTimeout,
		staleAt:  defaultStaleAfter,
		deadAt:   defaultUnavailableAt,
	}
}

// Close releases the sidecar-owned pool. Test-injected pools are not owned.
func (s *Service) Close() error {
	if s == nil || s.db == nil || !s.ownsDB {
		return nil
	}
	return s.db.Close()
}

// Configured reports whether aggregate database access was supplied.
func (s *Service) Configured() bool {
	return s != nil && s.db != nil
}

// GetOnlineUsersSummary returns a cached compact aggregate or performs one
// bounded query. Concurrent cache misses share the leader's result.
func (s *Service) GetOnlineUsersSummary(ctx context.Context) (model.OnlineUsersSummary, error) {
	if s == nil || s.db == nil {
		return unavailableSummary(time.Now().UTC(), "未配置在线人数聚合数据库只读连接"), nil
	}
	now := s.now().UTC()

	s.mu.Lock()
	if s.summaryCached != nil && now.Before(s.summaryCachedAt.Add(s.cacheTTL)) {
		result := cloneSummary(*s.summaryCached)
		s.mu.Unlock()
		return result, nil
	}
	if flight := s.summaryInFlight; flight != nil {
		s.mu.Unlock()
		select {
		case <-flight.done:
			return cloneSummary(flight.result), nil
		case <-ctx.Done():
			return model.OnlineUsersSummary{}, ctx.Err()
		}
	}
	flight := &summaryFlight{done: make(chan struct{})}
	s.summaryInFlight = flight
	lastGood := cloneSummaryPointer(s.summaryLastGood)
	s.mu.Unlock()

	queryCtx, cancel := context.WithTimeout(ctx, s.timeout)
	result, err := s.querySummary(queryCtx, now)
	cancel()
	if err != nil {
		s.logQueryFailure("summary", err)
		result = failedSummary(now, lastGood, s.staleAt, s.deadAt)
	}

	s.mu.Lock()
	stored := cloneSummary(result)
	s.summaryCached = &stored
	s.summaryCachedAt = now
	if err == nil {
		good := cloneSummary(result)
		s.summaryLastGood = &good
	}
	flight.result = cloneSummary(result)
	s.summaryInFlight = nil
	close(flight.done)
	s.mu.Unlock()
	return cloneSummary(result), nil
}

// GetOnlineUsers returns details only on demand. It has a separate cache from
// the summary so background indicator refreshes never load identities or usage.
func (s *Service) GetOnlineUsers(ctx context.Context) (model.OnlineUsersSnapshot, error) {
	if s == nil || s.db == nil {
		return unavailableSummary(time.Now().UTC(), "未配置在线人数聚合数据库只读连接").WithUsers(nil), nil
	}
	now := s.now().UTC()

	s.mu.Lock()
	if s.detailCached != nil && now.Before(s.detailCachedAt.Add(s.cacheTTL)) {
		result := cloneSnapshot(*s.detailCached)
		s.mu.Unlock()
		return result, nil
	}
	if flight := s.detailInFlight; flight != nil {
		s.mu.Unlock()
		select {
		case <-flight.done:
			return cloneSnapshot(flight.result), nil
		case <-ctx.Done():
			return model.OnlineUsersSnapshot{}, ctx.Err()
		}
	}
	flight := &detailFlight{done: make(chan struct{})}
	s.detailInFlight = flight
	lastGood := cloneSnapshotPointer(s.detailLastGood)
	s.mu.Unlock()

	queryCtx, cancel := context.WithTimeout(ctx, s.timeout)
	result, err := s.queryDetail(queryCtx, now)
	cancel()
	if err != nil {
		s.logQueryFailure("detail", err)
		result = failedDetail(now, lastGood, s.staleAt, s.deadAt)
	}

	s.mu.Lock()
	stored := cloneSnapshot(result)
	s.detailCached = &stored
	s.detailCachedAt = now
	if err == nil {
		good := cloneSnapshot(result)
		s.detailLastGood = &good
	}
	flight.result = cloneSnapshot(result)
	s.detailInFlight = nil
	close(flight.done)
	s.mu.Unlock()
	return cloneSnapshot(result), nil
}

func (s *Service) querySummary(ctx context.Context, now time.Time) (model.OnlineUsersSummary, error) {
	rows, err := s.db.QueryContext(ctx, summaryQuery)
	if err != nil {
		return model.OnlineUsersSummary{}, err
	}
	defer rows.Close()

	result := baseSummary(now)
	seenRow := false
	var dataThrough sql.NullTime
	for rows.Next() {
		seenRow = true
		var rowThrough sql.NullTime
		var globalCount int64
		var groupID sql.NullInt64
		var groupCount sql.NullInt64
		if err := rows.Scan(&rowThrough, &globalCount, &groupID, &groupCount); err != nil {
			return model.OnlineUsersSummary{}, err
		}
		if rowThrough.Valid {
			dataThrough = rowThrough
		}
		result.Count = boundedInt(globalCount)
		if groupID.Valid && groupID.Int64 >= 0 && groupCount.Valid {
			result.GroupCounts[groupID.Int64] = boundedInt(groupCount.Int64)
		}
	}
	if err := rows.Err(); err != nil {
		return model.OnlineUsersSummary{}, err
	}
	if !seenRow || !dataThrough.Valid || dataThrough.Time.IsZero() {
		return unavailableSummary(now, "在线人数聚合水位尚未建立"), nil
	}
	through := dataThrough.Time.UTC()
	result.DataThrough = &through
	result.Ready = true
	result.GroupCountsAvailable = true
	applyFreshness(&result, now, s.staleAt, s.deadAt)
	return result, nil
}

func (s *Service) queryDetail(ctx context.Context, now time.Time) (model.OnlineUsersSnapshot, error) {
	rows, err := s.db.QueryContext(ctx, detailQuery)
	if err != nil {
		return model.OnlineUsersSnapshot{}, err
	}
	defer rows.Close()

	summary := baseSummary(now)
	usersByID := make(map[int64]*model.OnlineUser)
	seenGroups := make(map[int64]map[int64]struct{})
	seenRow := false
	dailyReady := false
	var dataThrough sql.NullTime

	for rows.Next() {
		seenRow = true
		var rowThrough sql.NullTime
		var rowDailyReady bool
		var userID sql.NullInt64
		var groupID sql.NullInt64
		var lastCallAt sql.NullTime
		var email string
		var username string
		var todayRequests sql.NullInt64
		var todayTokens sql.NullInt64
		var todayCost sql.NullFloat64
		if err := rows.Scan(
			&rowThrough,
			&rowDailyReady,
			&userID,
			&groupID,
			&lastCallAt,
			&email,
			&username,
			&todayRequests,
			&todayTokens,
			&todayCost,
		); err != nil {
			return model.OnlineUsersSnapshot{}, err
		}
		dailyReady = rowDailyReady
		if rowThrough.Valid {
			dataThrough = rowThrough
		}
		if !userID.Valid || userID.Int64 <= 0 || !lastCallAt.Valid {
			continue
		}

		user := usersByID[userID.Int64]
		if user == nil {
			displayName := firstNonEmpty(username, email)
			if displayName == "" {
				displayName = fmt.Sprintf("用户 #%d", userID.Int64)
			}
			user = &model.OnlineUser{
				ID:          userID.Int64,
				DisplayName: displayName,
				Email:       strings.TrimSpace(email),
				LastCallAt:  lastCallAt.Time.UTC(),
			}
			if dailyReady {
				if todayRequests.Valid {
					value := todayRequests.Int64
					user.TodayRequests = &value
				}
				if todayTokens.Valid {
					value := todayTokens.Int64
					user.TodayTokens = &value
				}
				if todayCost.Valid {
					value := todayCost.Float64
					user.TodayCost = &value
				}
			}
			usersByID[userID.Int64] = user
			seenGroups[userID.Int64] = make(map[int64]struct{})
		}
		if lastCallAt.Time.After(user.LastCallAt) {
			user.LastCallAt = lastCallAt.Time.UTC()
		}
		if groupID.Valid && groupID.Int64 >= 0 {
			if _, exists := seenGroups[userID.Int64][groupID.Int64]; !exists {
				seenGroups[userID.Int64][groupID.Int64] = struct{}{}
				user.GroupIDs = append(user.GroupIDs, groupID.Int64)
				summary.GroupCounts[groupID.Int64]++
			}
		}
	}
	if err := rows.Err(); err != nil {
		return model.OnlineUsersSnapshot{}, err
	}
	if !seenRow || !dataThrough.Valid || dataThrough.Time.IsZero() {
		return unavailableSummary(now, "在线人数聚合水位尚未建立").WithUsers(nil), nil
	}

	through := dataThrough.Time.UTC()
	summary.DataThrough = &through
	summary.Ready = true
	summary.GroupCountsAvailable = true
	if !dailyReady {
		summary.Partial = true
		summary.Notice = appendNotice(summary.Notice, "今日消耗聚合尚未就绪")
	}

	users := make([]model.OnlineUser, 0, len(usersByID))
	for _, user := range usersByID {
		sort.Slice(user.GroupIDs, func(i, j int) bool { return user.GroupIDs[i] < user.GroupIDs[j] })
		users = append(users, *user)
	}
	sort.Slice(users, func(i, j int) bool {
		if !users[i].LastCallAt.Equal(users[j].LastCallAt) {
			return users[i].LastCallAt.After(users[j].LastCallAt)
		}
		return users[i].ID < users[j].ID
	})
	summary.Count = len(users)
	applyFreshness(&summary, now, s.staleAt, s.deadAt)
	if !summary.Ready {
		users = nil
	}
	return summary.WithUsers(users), nil
}

func (s *Service) logQueryFailure(kind string, err error) {
	if errors.Is(err, context.Canceled) {
		return
	}
	s.logger.Warn("online aggregate query failed", "kind", kind, "error", err)
}

func baseSummary(now time.Time) model.OnlineUsersSummary {
	return model.OnlineUsersSummary{
		WindowMinutes: int(windowDuration / time.Minute),
		QueriedAt:     now.UTC(),
		Source:        "aggregate_db",
		GroupCounts:   make(map[int64]int),
	}
}

func unavailableSummary(now time.Time, notice string) model.OnlineUsersSummary {
	result := baseSummary(now)
	result.Notice = strings.TrimSpace(notice)
	return result
}

func failedSummary(now time.Time, lastGood *model.OnlineUsersSummary, staleAt, deadAt time.Duration) model.OnlineUsersSummary {
	if lastGood == nil {
		return unavailableSummary(now, "在线人数聚合数据库暂不可用")
	}
	result := cloneSummary(*lastGood)
	result.Stale = true
	result.Partial = true
	result.GroupCountsPartial = true
	result.Notice = appendNotice(result.Notice, "数据库查询失败，当前显示最近一次成功结果")
	applyFreshness(&result, now, staleAt, deadAt)
	return result
}

func failedDetail(now time.Time, lastGood *model.OnlineUsersSnapshot, staleAt, deadAt time.Duration) model.OnlineUsersSnapshot {
	if lastGood == nil {
		return unavailableSummary(now, "在线人数聚合数据库暂不可用").WithUsers(nil)
	}
	result := cloneSnapshot(*lastGood)
	summary := result.Summary()
	summary.Stale = true
	summary.Partial = true
	summary.GroupCountsPartial = true
	summary.Notice = appendNotice(summary.Notice, "数据库查询失败，当前显示最近一次成功结果")
	applyFreshness(&summary, now, staleAt, deadAt)
	if !summary.Ready {
		return summary.WithUsers(nil)
	}
	return summary.WithUsers(result.Users)
}

func applyFreshness(summary *model.OnlineUsersSummary, now time.Time, staleAt, deadAt time.Duration) {
	if summary == nil || summary.DataThrough == nil || summary.DataThrough.IsZero() {
		return
	}
	through := summary.DataThrough.UTC()
	if through.After(now.Add(time.Minute)) {
		summary.Ready = false
		summary.Stale = true
		summary.Partial = true
		summary.Count = 0
		summary.GroupCounts = make(map[int64]int)
		summary.GroupCountsAvailable = false
		summary.GroupCountsPartial = true
		summary.Notice = appendNotice(summary.Notice, "在线人数聚合水位时间异常")
		return
	}
	lag := now.Sub(through)
	if lag < 0 {
		lag = 0
	}
	summary.AggregationLagSeconds = int64(lag / time.Second)
	switch {
	case lag > deadAt:
		summary.Ready = false
		summary.Stale = true
		summary.Partial = true
		summary.Count = 0
		summary.GroupCounts = make(map[int64]int)
		summary.GroupCountsAvailable = false
		summary.GroupCountsPartial = true
		summary.Notice = appendNotice(summary.Notice, "在线人数聚合超过 10 分钟未更新")
	case lag > staleAt:
		summary.Stale = true
		summary.Partial = true
		summary.GroupCountsPartial = true
		summary.Notice = appendNotice(summary.Notice, "在线人数聚合更新延迟较高")
	}
}

func appendNotice(existing, message string) string {
	existing = strings.TrimSpace(existing)
	message = strings.TrimSpace(message)
	if existing == "" {
		return message
	}
	if message == "" || strings.Contains(existing, message) {
		return existing
	}
	return existing + "；" + message
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if trimmed := strings.TrimSpace(value); trimmed != "" {
			return trimmed
		}
	}
	return ""
}

func boundedInt(value int64) int {
	maxInt := int64(^uint(0) >> 1)
	if value <= 0 {
		return 0
	}
	if value > maxInt {
		return int(maxInt)
	}
	return int(value)
}

func cloneSummary(value model.OnlineUsersSummary) model.OnlineUsersSummary {
	return value.WithUsers(nil).Summary()
}

func cloneSnapshot(value model.OnlineUsersSnapshot) model.OnlineUsersSnapshot {
	return value.Summary().WithUsers(value.Users)
}

func cloneSummaryPointer(value *model.OnlineUsersSummary) *model.OnlineUsersSummary {
	if value == nil {
		return nil
	}
	copy := cloneSummary(*value)
	return &copy
}

func cloneSnapshotPointer(value *model.OnlineUsersSnapshot) *model.OnlineUsersSnapshot {
	if value == nil {
		return nil
	}
	copy := cloneSnapshot(*value)
	return &copy
}
