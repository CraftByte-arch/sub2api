package onlineusers

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/Wei-Shaw/sub2api-account-auto-scheduler/internal/model"
)

const (
	accountSuccessWindow          = time.Hour
	accountSuccessReferenceWindow = 24 * time.Hour
	accountSuccessCacheTTL        = 2 * time.Minute
	accountSuccessHourlyCacheTTL  = 5 * time.Minute
	accountSuccessOverlap         = 2 * time.Minute
	accountSuccessQueryTimeout    = 5 * time.Second
)

const accountSuccessMinuteQuery = `
SELECT bucket_start, group_id, account_id,
       COALESCE(SUM(success_count), 0)::BIGINT,
       COALESCE(SUM(attempt_count), 0)::BIGINT,
       COALESCE(SUM(client_canceled_count), 0)::BIGINT,
       COALESCE(SUM(failover_count), 0)::BIGINT
FROM account_performance_minute
WHERE bucket_start >= $1 AND bucket_start < $2
GROUP BY bucket_start, group_id, account_id
ORDER BY bucket_start, group_id, account_id`

const accountSuccessHourlyQuery = `
SELECT group_id, account_id,
       COALESCE(SUM(success_count), 0)::BIGINT,
       COALESCE(SUM(attempt_count), 0)::BIGINT,
       COALESCE(SUM(client_canceled_count), 0)::BIGINT,
       COALESCE(SUM(failover_count), 0)::BIGINT,
       MAX(bucket_start)
FROM account_performance_hourly
WHERE bucket_start >= $1 AND bucket_start < $2
GROUP BY group_id, account_id
ORDER BY group_id, account_id`

type groupAccountKey struct {
	groupID   int64
	accountID int64
}

type accountSuccessBucketKey struct {
	bucketStart time.Time
	groupAccountKey
}

type accountSuccessCounters struct {
	success        int64
	attempts       int64
	clientCanceled int64
	failovers      int64
	lastObservedAt time.Time
}

type accountSuccessFlight struct {
	done   chan struct{}
	result model.AccountSuccessSnapshot
}

type accountSuccessMinuteRow struct {
	key      accountSuccessBucketKey
	counters accountSuccessCounters
}

// GetAccountSuccessRates returns passive group-account reliability computed
// exclusively from Sub2API's existing bounded account-performance aggregates.
// It performs no periodic work; administrator overview requests drive refreshes.
func (s *Service) GetAccountSuccessRates(ctx context.Context) (model.AccountSuccessSnapshot, error) {
	if s == nil || s.db == nil {
		return unavailableAccountSuccess(time.Now().UTC(), "未配置实际成功率聚合数据库只读连接"), nil
	}
	if ctx == nil {
		ctx = context.Background()
	}
	now := s.now().UTC()

	s.successMu.Lock()
	if s.successCached != nil && now.Before(s.successCachedAt.Add(accountSuccessCacheTTL)) {
		result := cloneAccountSuccessSnapshot(*s.successCached)
		s.successMu.Unlock()
		return result, nil
	}
	if flight := s.successInFlight; flight != nil {
		s.successMu.Unlock()
		select {
		case <-flight.done:
			return cloneAccountSuccessSnapshot(flight.result), nil
		case <-ctx.Done():
			return model.AccountSuccessSnapshot{}, ctx.Err()
		}
	}
	flight := &accountSuccessFlight{done: make(chan struct{})}
	s.successInFlight = flight
	lastGood := cloneAccountSuccessSnapshotPointer(s.successLastGood)
	s.successMu.Unlock()

	queryCtx, cancel := context.WithTimeout(ctx, accountSuccessQueryTimeout)
	result, err := s.refreshAccountSuccess(queryCtx, now)
	cancel()
	if err != nil {
		s.logAccountSuccessFailure(err)
		result = failedAccountSuccess(now, lastGood)
	}

	s.successMu.Lock()
	stored := cloneAccountSuccessSnapshot(result)
	s.successCached = &stored
	s.successCachedAt = now
	if err == nil {
		good := cloneAccountSuccessSnapshot(result)
		s.successLastGood = &good
	}
	flight.result = cloneAccountSuccessSnapshot(result)
	s.successInFlight = nil
	close(flight.done)
	s.successMu.Unlock()
	return cloneAccountSuccessSnapshot(result), nil
}

func (s *Service) refreshAccountSuccess(ctx context.Context, now time.Time) (model.AccountSuccessSnapshot, error) {
	boundary := now.Truncate(time.Minute)
	windowStart := boundary.Add(-accountSuccessWindow)

	s.successMu.Lock()
	queryStart := windowStart
	if !s.successBoundary.IsZero() && s.successBoundary.After(windowStart) {
		queryStart = s.successBoundary.Add(-accountSuccessOverlap)
		if queryStart.Before(windowStart) {
			queryStart = windowStart
		}
	}
	needHourly := s.hourlyCachedAt.IsZero() || !now.Before(s.hourlyCachedAt.Add(accountSuccessHourlyCacheTTL))
	s.successMu.Unlock()

	minuteRows, err := s.queryAccountSuccessMinutes(ctx, queryStart, boundary)
	if err != nil {
		return model.AccountSuccessSnapshot{}, err
	}

	var hourly map[groupAccountKey]accountSuccessCounters
	var hourlyThrough time.Time
	if needHourly {
		hourlyThrough = now.Truncate(time.Hour)
		hourly, err = s.queryAccountSuccessHours(ctx, hourlyThrough.Add(-accountSuccessReferenceWindow), hourlyThrough)
		if err != nil {
			hourly = nil
		}
	}

	s.successMu.Lock()
	for key := range s.successBuckets {
		if !key.bucketStart.Before(queryStart) || key.bucketStart.Before(windowStart) || !key.bucketStart.Before(boundary) {
			delete(s.successBuckets, key)
		}
	}
	for _, row := range minuteRows {
		s.successBuckets[row.key] = row.counters
	}
	s.successBoundary = boundary
	if hourly != nil {
		s.hourlySuccess = hourly
		s.hourlyThrough = hourlyThrough
		s.hourlyCachedAt = now
	}
	result := s.buildAccountSuccessSnapshotLocked(now, boundary)
	if needHourly && hourly == nil {
		result.Partial = true
		result.Notice = appendNotice(result.Notice, "近 24 小时参考暂不可用")
	}
	s.successMu.Unlock()
	return result, nil
}

func (s *Service) queryAccountSuccessMinutes(ctx context.Context, start, end time.Time) ([]accountSuccessMinuteRow, error) {
	rows, err := s.db.QueryContext(ctx, accountSuccessMinuteQuery, start, end)
	if err != nil {
		return nil, fmt.Errorf("query account success minute aggregates: %w", err)
	}
	defer rows.Close()
	result := make([]accountSuccessMinuteRow, 0)
	for rows.Next() {
		var bucket time.Time
		var groupID, accountID int64
		var counters accountSuccessCounters
		if err := rows.Scan(&bucket, &groupID, &accountID, &counters.success, &counters.attempts, &counters.clientCanceled, &counters.failovers); err != nil {
			return nil, err
		}
		if accountID <= 0 || groupID < 0 {
			continue
		}
		bucket = bucket.UTC().Truncate(time.Minute)
		counters.lastObservedAt = bucket
		result = append(result, accountSuccessMinuteRow{key: accountSuccessBucketKey{bucketStart: bucket, groupAccountKey: groupAccountKey{groupID: groupID, accountID: accountID}}, counters: counters})
	}
	return result, rows.Err()
}

func (s *Service) queryAccountSuccessHours(ctx context.Context, start, end time.Time) (map[groupAccountKey]accountSuccessCounters, error) {
	rows, err := s.db.QueryContext(ctx, accountSuccessHourlyQuery, start, end)
	if err != nil {
		return nil, fmt.Errorf("query account success hourly aggregates: %w", err)
	}
	defer rows.Close()
	result := make(map[groupAccountKey]accountSuccessCounters)
	for rows.Next() {
		var key groupAccountKey
		var counters accountSuccessCounters
		var observed sql.NullTime
		if err := rows.Scan(&key.groupID, &key.accountID, &counters.success, &counters.attempts, &counters.clientCanceled, &counters.failovers, &observed); err != nil {
			return nil, err
		}
		if key.accountID <= 0 || key.groupID < 0 {
			continue
		}
		if observed.Valid {
			counters.lastObservedAt = observed.Time.UTC().Truncate(time.Hour)
		}
		result[key] = counters
	}
	return result, rows.Err()
}

func (s *Service) buildAccountSuccessSnapshotLocked(now, boundary time.Time) model.AccountSuccessSnapshot {
	recent := make(map[groupAccountKey]accountSuccessCounters)
	for key, counters := range s.successBuckets {
		aggregated := recent[key.groupAccountKey]
		addAccountSuccessCounters(&aggregated, counters)
		recent[key.groupAccountKey] = aggregated
	}
	keys := make(map[groupAccountKey]struct{}, len(recent)+len(s.hourlySuccess))
	for key := range recent {
		keys[key] = struct{}{}
	}
	for key := range s.hourlySuccess {
		keys[key] = struct{}{}
	}
	ordered := make([]groupAccountKey, 0, len(keys))
	for key := range keys {
		ordered = append(ordered, key)
	}
	sort.Slice(ordered, func(i, j int) bool {
		if ordered[i].groupID == ordered[j].groupID {
			return ordered[i].accountID < ordered[j].accountID
		}
		return ordered[i].groupID < ordered[j].groupID
	})
	activeAccounts := make(map[int64]struct{})
	items := make([]model.GroupAccountSuccessRate, 0, len(ordered))
	for _, key := range ordered {
		recentWindow := accountSuccessWindowFromCounters(recent[key], int(accountSuccessWindow/time.Minute), boundary)
		referenceWindow := accountSuccessWindowFromCounters(s.hourlySuccess[key], int(accountSuccessReferenceWindow/time.Minute), s.hourlyThrough)
		if recentWindow.EffectiveAttempts > 0 {
			activeAccounts[key.accountID] = struct{}{}
		}
		items = append(items, model.GroupAccountSuccessRate{GroupID: key.groupID, AccountID: key.accountID, Recent: recentWindow, Reference: referenceWindow})
	}
	through := boundary
	result := model.AccountSuccessSnapshot{Ready: true, QueriedAt: now, DataThrough: &through, Source: "account_performance_aggregate", ActiveAccountCount: len(activeAccounts), Items: items}
	if !s.hourlyThrough.IsZero() {
		referenceThrough := s.hourlyThrough
		result.ReferenceThrough = &referenceThrough
	}
	return result
}

func accountSuccessWindowFromCounters(counters accountSuccessCounters, minutes int, through time.Time) model.AccountSuccessWindow {
	effective := counters.attempts - counters.clientCanceled
	window := model.AccountSuccessWindow{SuccessCount: counters.success, EffectiveAttempts: effective, AttemptCount: counters.attempts, ClientCanceledCount: counters.clientCanceled, FailoverCount: counters.failovers, WindowMinutes: minutes}
	if !through.IsZero() {
		value := through.UTC()
		window.DataThrough = &value
	}
	if !counters.lastObservedAt.IsZero() {
		value := counters.lastObservedAt.UTC()
		window.LastObservedAt = &value
	}
	window.Finalize()
	return window
}

func addAccountSuccessCounters(target *accountSuccessCounters, value accountSuccessCounters) {
	target.success += value.success
	target.attempts += value.attempts
	target.clientCanceled += value.clientCanceled
	target.failovers += value.failovers
	if value.lastObservedAt.After(target.lastObservedAt) {
		target.lastObservedAt = value.lastObservedAt
	}
}

func unavailableAccountSuccess(now time.Time, notice string) model.AccountSuccessSnapshot {
	return model.AccountSuccessSnapshot{QueriedAt: now, Source: "account_performance_aggregate", Notice: strings.TrimSpace(notice), Items: []model.GroupAccountSuccessRate{}}
}

func failedAccountSuccess(now time.Time, lastGood *model.AccountSuccessSnapshot) model.AccountSuccessSnapshot {
	if lastGood == nil {
		return unavailableAccountSuccess(now, "实际成功率聚合数据库暂不可用")
	}
	result := cloneAccountSuccessSnapshot(*lastGood)
	result.QueriedAt = now
	result.Stale = true
	result.Partial = true
	result.Notice = appendNotice(result.Notice, "数据库查询失败，当前显示最近一次成功结果")
	age := now.Sub(lastGood.QueriedAt)
	if age < 0 {
		age = 0
	}
	if age > defaultUnavailableAt {
		result.Ready = false
		result.ActiveAccountCount = 0
		result.Items = []model.GroupAccountSuccessRate{}
		result.Notice = appendNotice(result.Notice, "实际成功率超过 10 分钟未更新")
	}
	return result
}

func cloneAccountSuccessSnapshotPointer(value *model.AccountSuccessSnapshot) *model.AccountSuccessSnapshot {
	if value == nil {
		return nil
	}
	copy := cloneAccountSuccessSnapshot(*value)
	return &copy
}

func cloneAccountSuccessSnapshot(value model.AccountSuccessSnapshot) model.AccountSuccessSnapshot {
	copy := value
	if value.DataThrough != nil {
		v := *value.DataThrough
		copy.DataThrough = &v
	}
	if value.ReferenceThrough != nil {
		v := *value.ReferenceThrough
		copy.ReferenceThrough = &v
	}
	if value.CollectionHealth != nil {
		health := *value.CollectionHealth
		if value.CollectionHealth.LastSuccessfulFlushAt != nil {
			flush := *value.CollectionHealth.LastSuccessfulFlushAt
			health.LastSuccessfulFlushAt = &flush
		}
		copy.CollectionHealth = &health
	}
	copy.Items = make([]model.GroupAccountSuccessRate, len(value.Items))
	for i, item := range value.Items {
		copy.Items[i] = item
		cloneSuccessWindowTimes(&copy.Items[i].Recent)
		cloneSuccessWindowTimes(&copy.Items[i].Reference)
	}
	return copy
}

func cloneSuccessWindowTimes(window *model.AccountSuccessWindow) {
	if window.DataThrough != nil {
		v := *window.DataThrough
		window.DataThrough = &v
	}
	if window.LastObservedAt != nil {
		v := *window.LastObservedAt
		window.LastObservedAt = &v
	}
}

func (s *Service) logAccountSuccessFailure(err error) {
	if errors.Is(err, context.Canceled) {
		return
	}
	s.logger.Warn("account success aggregate query failed", "error", err)
}
