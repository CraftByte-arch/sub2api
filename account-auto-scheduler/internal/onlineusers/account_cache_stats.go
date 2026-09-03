package onlineusers

import (
	"context"
	"fmt"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/Wei-Shaw/sub2api-account-auto-scheduler/internal/model"
)

// accountCacheStatsQuery intentionally reads only the three fields used by
// the cache-hit label. This avoids the history/model/endpoint work performed
// by the full per-account administrator statistics endpoint.
const accountCacheStatsQuery = `
SELECT
  account_id,
  COALESCE(SUM(GREATEST(input_tokens, 0)), 0)::BIGINT AS input_tokens,
  COALESCE(SUM(GREATEST(cache_creation_tokens, 0)), 0)::BIGINT AS cache_creation_tokens,
  COALESCE(SUM(GREATEST(cache_read_tokens, 0)), 0)::BIGINT AS cache_read_tokens
FROM usage_logs
WHERE account_id = ANY($1::BIGINT[])
  AND created_at >= CURRENT_DATE
GROUP BY account_id
ORDER BY account_id`

type accountCacheStatsFlight struct {
	done   chan struct{}
	result model.AccountCacheStatsSnapshot
}

// GetTodayAccountCacheStats retrieves cache-token totals for all supplied
// accounts in one grouped query. CURRENT_DATE is intentionally evaluated by
// PostgreSQL, whose configured timezone matches Sub2API's today boundary on
// HC2.
func (s *Service) GetTodayAccountCacheStats(ctx context.Context, accountIDs []int64) (model.AccountCacheStatsSnapshot, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	now := time.Now().UTC()
	if s == nil || s.db == nil {
		return unavailableAccountCacheStats(now, false, "未配置账号缓存统计数据库只读连接"), nil
	}
	now = s.now().UTC()
	ids := normalizeAccountCacheStatsIDs(accountIDs)
	if len(ids) == 0 {
		return model.AccountCacheStatsSnapshot{
			Configured: true,
			Ready:      true,
			QueriedAt:  now,
			Stats:      map[int64]model.AccountCacheStats{},
		}, nil
	}
	key := accountCacheStatsKey(ids)
	ttl := s.cacheStatsTTL
	if ttl <= 0 {
		ttl = defaultCacheTTL
	}

	s.cacheStatsMu.Lock()
	if s.cacheStatsInFlight == nil {
		s.cacheStatsInFlight = make(map[string]*accountCacheStatsFlight)
	}
	if s.cacheStatsLastGood == nil {
		s.cacheStatsLastGood = make(map[string]model.AccountCacheStatsSnapshot)
	}
	if s.cacheStatsCached != nil && s.cacheStatsCachedKey == key && now.Before(s.cacheStatsCachedAt.Add(ttl)) {
		result := cloneAccountCacheStatsSnapshot(*s.cacheStatsCached)
		s.cacheStatsMu.Unlock()
		return result, nil
	}
	if flight := s.cacheStatsInFlight[key]; flight != nil {
		s.cacheStatsMu.Unlock()
		select {
		case <-flight.done:
			return cloneAccountCacheStatsSnapshot(flight.result), nil
		case <-ctx.Done():
			return model.AccountCacheStatsSnapshot{}, ctx.Err()
		}
	}
	flight := &accountCacheStatsFlight{done: make(chan struct{})}
	s.cacheStatsInFlight[key] = flight
	lastGood, hasLastGood := s.cacheStatsLastGood[key]
	if hasLastGood {
		lastGood = cloneAccountCacheStatsSnapshot(lastGood)
	}
	s.cacheStatsMu.Unlock()

	timeout := s.cacheStatsQueryTimeout
	if timeout <= 0 {
		timeout = defaultQueryTimeout
	}
	queryCtx, cancel := context.WithTimeout(ctx, timeout)
	stats, err := s.queryTodayAccountCacheStats(queryCtx, ids)
	cancel()

	var result model.AccountCacheStatsSnapshot
	if err != nil {
		s.logQueryFailure("account_cache_stats", err)
		if hasLastGood {
			result = failedAccountCacheStats(now, lastGood)
		} else {
			result = unavailableAccountCacheStats(now, true, "账号缓存统计暂不可用")
		}
	} else {
		result = model.AccountCacheStatsSnapshot{
			Configured: true,
			Ready:      true,
			QueriedAt:  now,
			Stats:      stats,
		}
	}

	s.cacheStatsMu.Lock()
	stored := cloneAccountCacheStatsSnapshot(result)
	s.cacheStatsCachedKey = key
	s.cacheStatsCached = &stored
	s.cacheStatsCachedAt = now
	if err == nil {
		s.cacheStatsLastGood[key] = cloneAccountCacheStatsSnapshot(result)
		for oldKey, snapshot := range s.cacheStatsLastGood {
			if now.Sub(snapshot.QueriedAt) > defaultUnavailableAt {
				delete(s.cacheStatsLastGood, oldKey)
			}
		}
	}
	flight.result = cloneAccountCacheStatsSnapshot(result)
	delete(s.cacheStatsInFlight, key)
	close(flight.done)
	s.cacheStatsMu.Unlock()
	return cloneAccountCacheStatsSnapshot(result), nil
}

func (s *Service) queryTodayAccountCacheStats(ctx context.Context, accountIDs []int64) (map[int64]model.AccountCacheStats, error) {
	rows, err := s.db.QueryContext(ctx, accountCacheStatsQuery, postgresInt64ArrayLiteral(accountIDs))
	if err != nil {
		return nil, fmt.Errorf("query today account cache statistics: %w", err)
	}
	defer rows.Close()

	result := make(map[int64]model.AccountCacheStats, len(accountIDs))
	for _, accountID := range accountIDs {
		result[accountID] = model.AccountCacheStats{}
	}
	for rows.Next() {
		var accountID int64
		var stats model.AccountCacheStats
		if err := rows.Scan(&accountID, &stats.InputTokens, &stats.CacheCreationTokens, &stats.CacheReadTokens); err != nil {
			return nil, err
		}
		if _, requested := result[accountID]; !requested {
			continue
		}
		finalizeAccountCacheStats(&stats)
		result[accountID] = stats
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return result, nil
}

func finalizeAccountCacheStats(stats *model.AccountCacheStats) {
	if stats == nil {
		return
	}
	if stats.InputTokens < 0 {
		stats.InputTokens = 0
	}
	if stats.CacheCreationTokens < 0 {
		stats.CacheCreationTokens = 0
	}
	if stats.CacheReadTokens < 0 {
		stats.CacheReadTokens = 0
	}
	stats.PromptTokens = stats.InputTokens + stats.CacheCreationTokens + stats.CacheReadTokens
	if stats.PromptTokens > 0 {
		stats.HitRate = float64(stats.CacheReadTokens) / float64(stats.PromptTokens) * 100
	}
}

func unavailableAccountCacheStats(now time.Time, configured bool, notice string) model.AccountCacheStatsSnapshot {
	return model.AccountCacheStatsSnapshot{
		Configured: configured,
		QueriedAt:  now,
		Notice:     strings.TrimSpace(notice),
		Stats:      map[int64]model.AccountCacheStats{},
	}
}

func failedAccountCacheStats(now time.Time, lastGood model.AccountCacheStatsSnapshot) model.AccountCacheStatsSnapshot {
	result := cloneAccountCacheStatsSnapshot(lastGood)
	age := now.Sub(lastGood.QueriedAt)
	if age < 0 {
		age = 0
	}
	result.Configured = true
	result.QueriedAt = now
	result.Stale = true
	result.Notice = appendNotice(result.Notice, "数据库查询失败，当前显示最近一次成功结果")
	if age > defaultUnavailableAt {
		result.Ready = false
		result.Stats = map[int64]model.AccountCacheStats{}
		result.Notice = appendNotice(result.Notice, "账号缓存统计超过 10 分钟未更新")
	}
	return result
}

func cloneAccountCacheStatsSnapshot(value model.AccountCacheStatsSnapshot) model.AccountCacheStatsSnapshot {
	copy := value
	copy.Stats = make(map[int64]model.AccountCacheStats, len(value.Stats))
	for accountID, stats := range value.Stats {
		copy.Stats[accountID] = stats
	}
	return copy
}

func normalizeAccountCacheStatsIDs(values []int64) []int64 {
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

func accountCacheStatsKey(accountIDs []int64) string {
	parts := make([]string, len(accountIDs))
	for index, accountID := range accountIDs {
		parts[index] = strconv.FormatInt(accountID, 10)
	}
	return strings.Join(parts, ",")
}

// postgresInt64ArrayLiteral is safe because callers first normalize IDs to
// positive int64 values. Passing a plain string keeps database/sql and the
// pgx driver compatible while the SQL cast fixes the intended BIGINT[] type.
func postgresInt64ArrayLiteral(accountIDs []int64) string {
	if len(accountIDs) == 0 {
		return "{}"
	}
	return "{" + accountCacheStatsKey(accountIDs) + "}"
}
