package repository

import (
	"context"
	"database/sql"
	"fmt"
	"math"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/Wei-Shaw/sub2api/internal/service"
)

const maxAccountPerformanceUpsertKeys = 1000

var accountPerformanceNow = time.Now

var accountPerformanceMetricColumns = []string{
	"attempt_count", "success_count", "client_canceled_count", "ttft_timeout_count",
	"rate_limit_count", "auth_count", "upstream_4xx_count", "upstream_5xx_count",
	"transport_count", "protocol_count", "other_failure_count", "failover_count",
	"ttft_sample_count", "ttft_sum_ms", "duration_sample_count", "duration_sum_ms",
	"ttft_le_1000_ms", "ttft_le_2500_ms", "ttft_le_5000_ms", "ttft_le_10000_ms", "ttft_le_30000_ms", "ttft_gt_30000_ms",
	"duration_le_1000_ms", "duration_le_2500_ms", "duration_le_5000_ms", "duration_le_10000_ms", "duration_le_30000_ms", "duration_gt_30000_ms",
}

type accountPerformanceRepository struct {
	db *sql.DB
}

func NewAccountPerformanceRepository(db *sql.DB) service.AccountPerformanceRepository {
	return &accountPerformanceRepository{db: db}
}

type accountPerformanceKey struct {
	bucketStart time.Time
	accountID   int64
	platform    string
	groupID     int64
	model       string
	protocol    string
	outcome     string
}

type accountPerformanceRow struct {
	key      accountPerformanceKey
	counters service.AccountPerformanceCounters
}

func (r *accountPerformanceRepository) UpsertMinuteBatch(ctx context.Context, deltas []service.AccountPerformanceDelta) error {
	if len(deltas) == 0 {
		return nil
	}
	rows := make([]accountPerformanceRow, 0, len(deltas))
	byKey := make(map[accountPerformanceKey]int, len(deltas))
	for i, delta := range deltas {
		row, err := newAccountPerformanceRow(delta)
		if err != nil {
			return fmt.Errorf("invalid account performance delta %d: %w", i, err)
		}
		if index, ok := byKey[row.key]; ok {
			if err := addAccountPerformanceCounters(&rows[index].counters, row.counters); err != nil {
				return fmt.Errorf("aggregate account performance delta %d: %w", i, err)
			}
			continue
		}
		if len(rows) >= maxAccountPerformanceUpsertKeys {
			return fmt.Errorf("account performance unique key limit exceeded: max %d", maxAccountPerformanceUpsertKeys)
		}
		byKey[row.key] = len(rows)
		rows = append(rows, row)
	}
	if r == nil || r.db == nil {
		return fmt.Errorf("nil account performance repository")
	}

	columns := append([]string{"bucket_start", "account_id", "platform", "group_id", "model", "protocol", "outcome"}, accountPerformanceMetricColumns...)
	var query strings.Builder
	query.WriteString("INSERT INTO account_performance_minute (")
	query.WriteString(strings.Join(columns, ", "))
	query.WriteString(") VALUES ")
	args := make([]any, 0, len(rows)*len(columns))
	for i, row := range rows {
		if i > 0 {
			query.WriteString(", ")
		}
		query.WriteByte('(')
		for column := range columns {
			if column > 0 {
				query.WriteString(", ")
			}
			query.WriteByte('$')
			query.WriteString(strconv.Itoa(len(args) + column + 1))
		}
		query.WriteByte(')')
		args = append(args, accountPerformanceRowArgs(row)...)
	}
	query.WriteString(" ON CONFLICT (bucket_start, account_id, platform, group_id, model, protocol, outcome) DO UPDATE SET ")
	for i, column := range accountPerformanceMetricColumns {
		if i > 0 {
			query.WriteString(", ")
		}
		query.WriteString(column)
		query.WriteString(" = account_performance_minute.")
		query.WriteString(column)
		query.WriteString(" + EXCLUDED.")
		query.WriteString(column)
	}
	query.WriteString(", updated_at = NOW()")
	if _, err := r.db.ExecContext(ctx, query.String(), args...); err != nil {
		return fmt.Errorf("upsert account performance minute batch: %w", err)
	}
	return nil
}

func newAccountPerformanceRow(delta service.AccountPerformanceDelta) (accountPerformanceRow, error) {
	if delta.BucketStart.IsZero() {
		return accountPerformanceRow{}, fmt.Errorf("bucket start is required")
	}
	if delta.AccountID <= 0 {
		return accountPerformanceRow{}, fmt.Errorf("account id must be positive")
	}
	if delta.GroupID < 0 {
		return accountPerformanceRow{}, fmt.Errorf("group id must be non-negative")
	}
	if delta.AttemptCount != 1 {
		return accountPerformanceRow{}, fmt.Errorf("attempt count must be exactly one")
	}
	if err := validateAccountPerformanceDimension("platform", delta.Platform, 32, true); err != nil {
		return accountPerformanceRow{}, err
	}
	if err := validateAccountPerformanceDimension("model", delta.Model, 255, false); err != nil {
		return accountPerformanceRow{}, err
	}
	if err := validateAccountPerformanceDimension("protocol", delta.Protocol, 32, true); err != nil {
		return accountPerformanceRow{}, err
	}
	if !isValidAccountPerformanceOutcome(delta.Outcome) {
		return accountPerformanceRow{}, fmt.Errorf("invalid outcome %q", delta.Outcome)
	}
	if delta.TTFTMS != nil && *delta.TTFTMS < 0 {
		return accountPerformanceRow{}, fmt.Errorf("ttft latency must be non-negative")
	}
	if delta.DurationMS != nil && *delta.DurationMS < 0 {
		return accountPerformanceRow{}, fmt.Errorf("duration latency must be non-negative")
	}

	row := accountPerformanceRow{key: accountPerformanceKey{
		bucketStart: delta.BucketStart.UTC().Truncate(time.Minute), accountID: delta.AccountID,
		platform: delta.Platform, groupID: delta.GroupID, model: delta.Model, protocol: delta.Protocol, outcome: delta.Outcome,
	}}
	row.counters.AttemptCount = delta.AttemptCount
	switch delta.Outcome {
	case service.AccountPerformanceOutcomeSuccess:
		row.counters.SuccessCount = delta.AttemptCount
	case service.AccountPerformanceOutcomeClientCanceled:
		row.counters.ClientCanceledCount = delta.AttemptCount
	case service.AccountPerformanceOutcomeTTFTTimeout:
		row.counters.TTFTTimeoutCount = delta.AttemptCount
	case service.AccountPerformanceOutcomeRateLimit:
		row.counters.RateLimitCount = delta.AttemptCount
	case service.AccountPerformanceOutcomeAuth:
		row.counters.AuthCount = delta.AttemptCount
	case service.AccountPerformanceOutcomeUpstream4xx:
		row.counters.Upstream4xxCount = delta.AttemptCount
	case service.AccountPerformanceOutcomeUpstream5xx:
		row.counters.Upstream5xxCount = delta.AttemptCount
	case service.AccountPerformanceOutcomeTransport:
		row.counters.TransportCount = delta.AttemptCount
	case service.AccountPerformanceOutcomeProtocol:
		row.counters.ProtocolCount = delta.AttemptCount
	case service.AccountPerformanceOutcomeOtherFailure:
		row.counters.OtherFailureCount = delta.AttemptCount
	}
	if delta.Failover {
		row.counters.FailoverCount = 1
	}
	if latency, ok := delta.TTFTLatencySample(); ok {
		row.counters.TTFTLatency.Add(latency)
		row.counters.TTFTSumMS = latency
	}
	if latency, ok := delta.DurationLatencySample(); ok {
		row.counters.DurationLatency.Add(latency)
		row.counters.DurationSumMS = latency
	}
	return row, nil
}

func validateAccountPerformanceDimension(name, value string, maxRunes int, required bool) error {
	if strings.ContainsRune(value, '\x00') {
		return fmt.Errorf("%s contains NUL", name)
	}
	if required && strings.TrimSpace(value) == "" {
		return fmt.Errorf("%s is required", name)
	}
	if utf8.RuneCountInString(value) > maxRunes {
		return fmt.Errorf("%s exceeds %d characters", name, maxRunes)
	}
	return nil
}

func isValidAccountPerformanceOutcome(outcome string) bool {
	switch outcome {
	case service.AccountPerformanceOutcomeSuccess, service.AccountPerformanceOutcomeTTFTTimeout, service.AccountPerformanceOutcomeRateLimit,
		service.AccountPerformanceOutcomeAuth, service.AccountPerformanceOutcomeUpstream4xx, service.AccountPerformanceOutcomeUpstream5xx,
		service.AccountPerformanceOutcomeTransport, service.AccountPerformanceOutcomeProtocol, service.AccountPerformanceOutcomeOtherFailure,
		service.AccountPerformanceOutcomeClientCanceled:
		return true
	default:
		return false
	}
}

func accountPerformanceRowArgs(row accountPerformanceRow) []any {
	c := row.counters
	return []any{row.key.bucketStart, row.key.accountID, row.key.platform, row.key.groupID, row.key.model, row.key.protocol, row.key.outcome,
		c.AttemptCount, c.SuccessCount, c.ClientCanceledCount, c.TTFTTimeoutCount, c.RateLimitCount, c.AuthCount, c.Upstream4xxCount, c.Upstream5xxCount, c.TransportCount, c.ProtocolCount, c.OtherFailureCount, c.FailoverCount,
		c.TTFTLatency.Samples, c.TTFTSumMS, c.DurationLatency.Samples, c.DurationSumMS,
		c.TTFTLatency.LE1000MS, c.TTFTLatency.LE2500MS, c.TTFTLatency.LE5000MS, c.TTFTLatency.LE10000MS, c.TTFTLatency.LE30000MS, c.TTFTLatency.GT30000MS,
		c.DurationLatency.LE1000MS, c.DurationLatency.LE2500MS, c.DurationLatency.LE5000MS, c.DurationLatency.LE10000MS, c.DurationLatency.LE30000MS, c.DurationLatency.GT30000MS}
}

func addAccountPerformanceCounters(dst *service.AccountPerformanceCounters, src service.AccountPerformanceCounters) error {
	for _, pair := range [][2]*int64{
		{&dst.AttemptCount, &src.AttemptCount}, {&dst.SuccessCount, &src.SuccessCount}, {&dst.ClientCanceledCount, &src.ClientCanceledCount}, {&dst.TTFTTimeoutCount, &src.TTFTTimeoutCount},
		{&dst.RateLimitCount, &src.RateLimitCount}, {&dst.AuthCount, &src.AuthCount}, {&dst.Upstream4xxCount, &src.Upstream4xxCount}, {&dst.Upstream5xxCount, &src.Upstream5xxCount},
		{&dst.TransportCount, &src.TransportCount}, {&dst.ProtocolCount, &src.ProtocolCount}, {&dst.OtherFailureCount, &src.OtherFailureCount}, {&dst.FailoverCount, &src.FailoverCount},
		{&dst.TTFTSumMS, &src.TTFTSumMS}, {&dst.DurationSumMS, &src.DurationSumMS},
		{&dst.TTFTLatency.Samples, &src.TTFTLatency.Samples}, {&dst.TTFTLatency.LE1000MS, &src.TTFTLatency.LE1000MS}, {&dst.TTFTLatency.LE2500MS, &src.TTFTLatency.LE2500MS}, {&dst.TTFTLatency.LE5000MS, &src.TTFTLatency.LE5000MS}, {&dst.TTFTLatency.LE10000MS, &src.TTFTLatency.LE10000MS}, {&dst.TTFTLatency.LE30000MS, &src.TTFTLatency.LE30000MS}, {&dst.TTFTLatency.GT30000MS, &src.TTFTLatency.GT30000MS},
		{&dst.DurationLatency.Samples, &src.DurationLatency.Samples}, {&dst.DurationLatency.LE1000MS, &src.DurationLatency.LE1000MS}, {&dst.DurationLatency.LE2500MS, &src.DurationLatency.LE2500MS}, {&dst.DurationLatency.LE5000MS, &src.DurationLatency.LE5000MS}, {&dst.DurationLatency.LE10000MS, &src.DurationLatency.LE10000MS}, {&dst.DurationLatency.LE30000MS, &src.DurationLatency.LE30000MS}, {&dst.DurationLatency.GT30000MS, &src.DurationLatency.GT30000MS},
	} {
		if *pair[1] > math.MaxInt64-*pair[0] {
			return fmt.Errorf("counter overflow")
		}
		*pair[0] += *pair[1]
	}
	return nil
}

func (r *accountPerformanceRepository) RollupClosedHours(ctx context.Context, before time.Time) error {
	if before.IsZero() {
		return fmt.Errorf("account performance rollup cutoff is required")
	}
	if r == nil || r.db == nil {
		return fmt.Errorf("nil account performance repository")
	}
	closedBefore := accountPerformanceRollupCutoff(before, accountPerformanceNow())
	if closedBefore.IsZero() {
		return nil
	}
	var hasClosedRows bool
	if err := r.db.QueryRowContext(ctx, `SELECT EXISTS(SELECT 1 FROM account_performance_minute WHERE bucket_start < $1)`, closedBefore).Scan(&hasClosedRows); err != nil {
		return fmt.Errorf("check closed account performance minutes: %w", err)
	}
	if !hasClosedRows {
		return nil
	}
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("begin account performance rollup: %w", err)
	}
	committed := false
	defer func() {
		if !committed {
			_ = tx.Rollback()
		}
	}()
	const deleteSQL = `
DELETE FROM account_performance_hourly
WHERE bucket_start IN (
    SELECT DISTINCT date_trunc('hour', bucket_start)
    FROM account_performance_minute
    WHERE bucket_start < $1
)`
	if _, err := tx.ExecContext(ctx, deleteSQL, closedBefore); err != nil {
		return fmt.Errorf("clear closed account performance hours: %w", err)
	}
	columns := append([]string{"bucket_start", "account_id", "platform", "group_id", "model", "protocol", "outcome"}, accountPerformanceMetricColumns...)
	selectMetrics := make([]string, len(accountPerformanceMetricColumns))
	for i, column := range accountPerformanceMetricColumns {
		selectMetrics[i] = "SUM(" + column + ") AS " + column
	}
	insertSQL := `INSERT INTO account_performance_hourly (` + strings.Join(columns, ", ") + `)
SELECT date_trunc('hour', bucket_start), account_id, platform, group_id, model, protocol, outcome, ` + strings.Join(selectMetrics, ", ") + `
FROM account_performance_minute
WHERE bucket_start < $1
GROUP BY date_trunc('hour', bucket_start), account_id, platform, group_id, model, protocol, outcome
ON CONFLICT (bucket_start, account_id, platform, group_id, model, protocol, outcome) DO UPDATE SET ` + accountPerformanceReplaceMetricsSQL() + `, updated_at = NOW()`
	if _, err := tx.ExecContext(ctx, insertSQL, closedBefore); err != nil {
		return fmt.Errorf("recompute closed account performance hours: %w", err)
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("commit account performance rollup: %w", err)
	}
	committed = true
	return nil
}

func (r *accountPerformanceRepository) DeleteMinuteBefore(ctx context.Context, cutoff time.Time) (int64, error) {
	if !cutoff.IsZero() {
		currentHour := accountPerformanceNow().UTC().Truncate(time.Hour)
		if cutoff.UTC().After(currentHour) {
			cutoff = currentHour
		}
	}
	return r.deleteBefore(ctx, "account_performance_minute", cutoff, time.Minute)
}

func accountPerformanceRollupCutoff(before, now time.Time) time.Time {
	cutoff := before.UTC().Truncate(time.Hour)
	currentHour := now.UTC().Truncate(time.Hour)
	if cutoff.After(currentHour) {
		return currentHour
	}
	return cutoff
}

func (r *accountPerformanceRepository) DeleteHourlyBefore(ctx context.Context, cutoff time.Time) (int64, error) {
	return r.deleteBefore(ctx, "account_performance_hourly", cutoff, time.Hour)
}

func (r *accountPerformanceRepository) deleteBefore(ctx context.Context, table string, cutoff time.Time, unit time.Duration) (int64, error) {
	if cutoff.IsZero() {
		return 0, fmt.Errorf("account performance delete cutoff is required")
	}
	if r == nil || r.db == nil {
		return 0, fmt.Errorf("nil account performance repository")
	}
	result, err := r.db.ExecContext(ctx, "DELETE FROM "+table+" WHERE bucket_start < $1", cutoff.UTC().Truncate(unit))
	if err != nil {
		return 0, fmt.Errorf("delete account performance rows: %w", err)
	}
	count, err := result.RowsAffected()
	if err != nil {
		return 0, fmt.Errorf("read deleted account performance rows: %w", err)
	}
	return count, nil
}

func (r *accountPerformanceRepository) QueryOverview(ctx context.Context, filter service.AccountPerformanceOverviewFilter) (*service.AccountPerformanceOverview, error) {
	start, end, table, args, err := normalizeAccountPerformanceFilter(filter.Start, filter.End, filter.Platform, filter.GroupID, filter.Model, filter.Protocol, filter.AccountID)
	if err != nil {
		return nil, err
	}
	if r == nil || r.db == nil {
		return nil, fmt.Errorf("nil account performance repository")
	}
	tx, err := r.beginReadSnapshot(ctx)
	if err != nil {
		return nil, err
	}
	committed := false
	defer func() {
		if !committed {
			_ = tx.Rollback()
		}
	}()
	summarySQL := "SELECT " + accountPerformanceAggregateColumns() + " FROM " + table + accountPerformanceWhereClause()
	var counters service.AccountPerformanceCounters
	if err := tx.QueryRowContext(ctx, summarySQL, args...).Scan(accountPerformanceCounterDestinations(&counters)...); err != nil {
		return nil, fmt.Errorf("query account performance overview: %w", err)
	}
	pointsSQL := "SELECT bucket_start, " + accountPerformanceAggregateColumns() + " FROM " + table + accountPerformanceWhereClause() + " GROUP BY bucket_start ORDER BY bucket_start"
	rows, err := tx.QueryContext(ctx, pointsSQL, args...)
	if err != nil {
		return nil, fmt.Errorf("query account performance overview points: %w", err)
	}
	points := make([]service.AccountPerformanceTimePoint, 0)
	for rows.Next() {
		var point service.AccountPerformanceTimePoint
		values := accountPerformanceCounterDestinations(&point.Counters)
		if err := rows.Scan(append([]any{&point.BucketStart}, values...)...); err != nil {
			return nil, fmt.Errorf("scan account performance overview point: %w", err)
		}
		point.BucketStart = point.BucketStart.UTC()
		points = append(points, point)
	}
	if err := rows.Err(); err != nil {
		_ = rows.Close()
		return nil, fmt.Errorf("iterate account performance overview points: %w", err)
	}
	if err := rows.Close(); err != nil {
		return nil, fmt.Errorf("close account performance overview points: %w", err)
	}
	if err := tx.Commit(); err != nil {
		return nil, fmt.Errorf("commit account performance overview snapshot: %w", err)
	}
	committed = true
	_ = start
	_ = end
	return &service.AccountPerformanceOverview{Counters: counters, TimePoints: points}, nil
}

func (r *accountPerformanceRepository) QueryAccounts(ctx context.Context, filter service.AccountPerformanceAccountFilter) (*service.AccountPerformanceAccountPage, error) {
	_, _, table, args, err := normalizeAccountPerformanceFilter(filter.Start, filter.End, filter.Platform, filter.GroupID, filter.Model, filter.Protocol, filter.AccountID)
	if err != nil {
		return nil, err
	}
	sortColumn, sortOrder, page, pageSize, offset, err := normalizeAccountPerformancePage(filter)
	if err != nil {
		return nil, err
	}
	if r == nil || r.db == nil {
		return nil, fmt.Errorf("nil account performance repository")
	}
	tx, err := r.beginReadSnapshot(ctx)
	if err != nil {
		return nil, err
	}
	committed := false
	defer func() {
		if !committed {
			_ = tx.Rollback()
		}
	}()
	search := ""
	if filter.Search != "" {
		search = "%" + escapeAccountPerformanceLike(filter.Search) + "%"
	}
	query := `WITH aggregated AS (
SELECT account_id, platform, ` + accountPerformanceAggregateColumns() + `
FROM ` + table + accountPerformanceWhereClause() + `
GROUP BY account_id, platform
), scored AS (
SELECT *,
` + accountPerformanceScoredRateColumns() + `,
CASE WHEN ttft_sample_count = 0 THEN 0 WHEN CEIL(ttft_sample_count * 0.95) <= ttft_le_1000_ms THEN 1000 WHEN CEIL(ttft_sample_count * 0.95) <= ttft_le_2500_ms THEN 2500 WHEN CEIL(ttft_sample_count * 0.95) <= ttft_le_5000_ms THEN 5000 WHEN CEIL(ttft_sample_count * 0.95) <= ttft_le_10000_ms THEN 10000 WHEN CEIL(ttft_sample_count * 0.95) <= ttft_le_30000_ms THEN 30000 ELSE 30001 END AS p95_ttft_ms,
CASE WHEN duration_sample_count = 0 THEN 0 WHEN CEIL(duration_sample_count * 0.95) <= duration_le_1000_ms THEN 1000 WHEN CEIL(duration_sample_count * 0.95) <= duration_le_2500_ms THEN 2500 WHEN CEIL(duration_sample_count * 0.95) <= duration_le_5000_ms THEN 5000 WHEN CEIL(duration_sample_count * 0.95) <= duration_le_10000_ms THEN 10000 WHEN CEIL(duration_sample_count * 0.95) <= duration_le_30000_ms THEN 30000 ELSE 30001 END AS p95_duration_ms,
GREATEST(attempt_count - client_canceled_count - success_count, 0) AS failure_count,
attempt_count AS samples
FROM aggregated
), enriched AS (
SELECT
    scored.*,
    COALESCE(account.name, '#' || scored.account_id::text) AS account_name,
    COALESCE(account.type, '') AS account_type,
    COALESCE(
        NULLIF(account.credentials->>'auth_mode', ''),
        NULLIF(account.credentials->>'openai_auth_mode', ''),
        NULLIF(parent.credentials->>'auth_mode', ''),
        NULLIF(parent.credentials->>'openai_auth_mode', ''),
        ''
    ) AS auth_mode
FROM scored
LEFT JOIN accounts AS account ON account.id = scored.account_id
LEFT JOIN accounts AS parent ON parent.id = account.parent_account_id
)
SELECT account_id, platform, account_name, account_type, auth_mode, ` + accountPerformanceCounterColumns() + `, availability, failure_rate, health_score, COUNT(*) OVER() AS total FROM enriched
WHERE ($20::text = '' OR account_name ILIKE $20 ESCAPE '\')
ORDER BY ` + sortColumn + ` ` + sortOrder + `, account_id ASC LIMIT $18 OFFSET $19`
	queryArgs := append(args, int64(pageSize), offset, search)
	rows, err := tx.QueryContext(ctx, query, queryArgs...)
	if err != nil {
		return nil, fmt.Errorf("query account performance accounts: %w", err)
	}
	result := &service.AccountPerformanceAccountPage{Rows: make([]service.AccountPerformanceAccount, 0), Page: page, PageSize: pageSize}
	for rows.Next() {
		var item service.AccountPerformanceAccount
		destinations := append([]any{&item.AccountID, &item.Platform, &item.AccountName, &item.AccountType, &item.AuthMode}, accountPerformanceCounterDestinations(&item.Counters)...)
		destinations = append(destinations, &item.Availability, &item.FailureRate, &item.HealthScore, &result.Total)
		if err := rows.Scan(destinations...); err != nil {
			return nil, fmt.Errorf("scan account performance account: %w", err)
		}
		result.Rows = append(result.Rows, item)
	}
	if err := rows.Err(); err != nil {
		_ = rows.Close()
		return nil, fmt.Errorf("iterate account performance accounts: %w", err)
	}
	if err := rows.Close(); err != nil {
		return nil, fmt.Errorf("close account performance accounts: %w", err)
	}
	if result.Total > 0 {
		result.Pages = int((result.Total + int64(pageSize) - 1) / int64(pageSize))
	}
	if err := tx.Commit(); err != nil {
		return nil, fmt.Errorf("commit account performance accounts snapshot: %w", err)
	}
	committed = true
	return result, nil
}

func (r *accountPerformanceRepository) QueryInvestigation(ctx context.Context, filter service.AccountPerformanceInvestigationFilter) (*service.AccountPerformanceInvestigation, error) {
	_, _, table, args, err := normalizeAccountPerformanceFilter(filter.Start, filter.End, filter.Platform, filter.GroupID, filter.Model, filter.Protocol, filter.AccountID)
	if err != nil {
		return nil, err
	}
	if r == nil || r.db == nil {
		return nil, fmt.Errorf("nil account performance repository")
	}
	tx, err := r.beginReadSnapshot(ctx)
	if err != nil {
		return nil, err
	}
	committed := false
	defer func() {
		if !committed {
			_ = tx.Rollback()
		}
	}()
	pointsQuery := "SELECT bucket_start, " + accountPerformanceAggregateColumns() + " FROM " + table + accountPerformanceWhereClause() + " GROUP BY bucket_start ORDER BY bucket_start"
	rows, err := tx.QueryContext(ctx, pointsQuery, args...)
	if err != nil {
		return nil, fmt.Errorf("query account performance investigation points: %w", err)
	}
	result := &service.AccountPerformanceInvestigation{TimePoints: make([]service.AccountPerformanceTimePoint, 0), Failures: make([]service.AccountPerformanceFailureBreakdown, 0)}
	for rows.Next() {
		var point service.AccountPerformanceTimePoint
		if err := rows.Scan(append([]any{&point.BucketStart}, accountPerformanceCounterDestinations(&point.Counters)...)...); err != nil {
			return nil, fmt.Errorf("scan account performance investigation point: %w", err)
		}
		point.BucketStart = point.BucketStart.UTC()
		result.TimePoints = append(result.TimePoints, point)
	}
	if err := rows.Err(); err != nil {
		_ = rows.Close()
		return nil, fmt.Errorf("iterate account performance investigation points: %w", err)
	}
	if err := rows.Close(); err != nil {
		return nil, fmt.Errorf("close account performance investigation points: %w", err)
	}
	failureQuery := "SELECT outcome, COALESCE(SUM(attempt_count), 0) AS count FROM " + table + accountPerformanceWhereClause() + " AND outcome NOT IN ('success', 'client_canceled') GROUP BY outcome ORDER BY count DESC, outcome ASC"
	failureRows, err := tx.QueryContext(ctx, failureQuery, args...)
	if err != nil {
		return nil, fmt.Errorf("query account performance investigation failures: %w", err)
	}
	for failureRows.Next() {
		var item service.AccountPerformanceFailureBreakdown
		if err := failureRows.Scan(&item.Outcome, &item.Count); err != nil {
			return nil, fmt.Errorf("scan account performance investigation failure: %w", err)
		}
		result.Failures = append(result.Failures, item)
	}
	if err := failureRows.Err(); err != nil {
		_ = failureRows.Close()
		return nil, fmt.Errorf("iterate account performance investigation failures: %w", err)
	}
	if err := failureRows.Close(); err != nil {
		return nil, fmt.Errorf("close account performance investigation failures: %w", err)
	}
	if err := tx.Commit(); err != nil {
		return nil, fmt.Errorf("commit account performance investigation snapshot: %w", err)
	}
	committed = true
	return result, nil
}

func (r *accountPerformanceRepository) beginReadSnapshot(ctx context.Context) (*sql.Tx, error) {
	if r == nil || r.db == nil {
		return nil, fmt.Errorf("account performance repository does not support read snapshots")
	}
	tx, err := r.db.BeginTx(ctx, &sql.TxOptions{Isolation: sql.LevelRepeatableRead, ReadOnly: true})
	if err != nil {
		return nil, fmt.Errorf("begin account performance read snapshot: %w", err)
	}
	return tx, nil
}

func normalizeAccountPerformanceFilter(start, end time.Time, platform string, groupID int64, model, protocol string, accountID int64) (time.Time, time.Time, string, []any, error) {
	if start.IsZero() || end.IsZero() {
		return time.Time{}, time.Time{}, "", nil, fmt.Errorf("account performance start and end are required")
	}
	start, end = start.UTC(), end.UTC()
	if !end.After(start) {
		return time.Time{}, time.Time{}, "", nil, fmt.Errorf("account performance end must be after start")
	}
	if end.Sub(start) > 90*24*time.Hour {
		return time.Time{}, time.Time{}, "", nil, fmt.Errorf("account performance range exceeds 90 days")
	}
	if groupID < 0 || accountID < 0 {
		return time.Time{}, time.Time{}, "", nil, fmt.Errorf("account performance ids must be non-negative")
	}
	if err := validateAccountPerformanceDimension("platform", platform, 32, false); err != nil {
		return time.Time{}, time.Time{}, "", nil, err
	}
	if err := validateAccountPerformanceDimension("model", model, 255, false); err != nil {
		return time.Time{}, time.Time{}, "", nil, err
	}
	if err := validateAccountPerformanceDimension("protocol", protocol, 32, false); err != nil {
		return time.Time{}, time.Time{}, "", nil, err
	}
	source, sourceArgs := accountPerformanceQuerySource(start, end)
	args := append([]any{start, end, platform, groupID, model, protocol, accountID}, sourceArgs...)
	return start, end, source, args, nil
}

// accountPerformanceQuerySource combines closed hourly aggregates with the
// minute rows that cannot yet be represented by a complete hourly bucket. It
// keeps partial range boundaries out of the hourly source, preventing both
// stale/current-hour gaps and double counting. Minute aggregation means the
// first/last partial minute is necessarily included as its full bucket.
func accountPerformanceQuerySource(start, end time.Time) (string, []any) {
	now := accountPerformanceNow().UTC()
	currentHour := now.Truncate(time.Hour)
	hourlyEnd := end.Truncate(time.Hour)
	if hourlyEnd.After(currentHour) {
		hourlyEnd = currentHour
	}
	minuteLower := start.Truncate(time.Minute)
	minuteEnd := end.Truncate(time.Minute)
	if !end.Equal(minuteEnd) {
		minuteEnd = minuteEnd.Add(time.Minute)
	}
	startHour := start.Truncate(time.Hour)
	endHour := end.Truncate(time.Hour)
	startPartial := !start.Equal(startHour)
	endPartial := !end.Equal(endHour)
	minuteRetentionStart := now.Add(-7 * 24 * time.Hour).Truncate(time.Minute)

	source := "(SELECT * FROM account_performance_hourly WHERE bucket_start >= $1 AND bucket_start < $8 " +
		"UNION ALL SELECT * FROM account_performance_minute WHERE bucket_start >= $9 AND bucket_start < $10 AND bucket_start >= $11 AND (bucket_start >= $12 OR ($13 AND bucket_start >= $14 AND bucket_start < $15) OR ($16 AND bucket_start >= $17))) AS account_performance_source"
	return source, []any{hourlyEnd, minuteLower, minuteEnd, minuteRetentionStart, currentHour, startPartial, startHour, startHour.Add(time.Hour), endPartial, endHour}
}

func normalizeAccountPerformancePage(filter service.AccountPerformanceAccountFilter) (string, string, int, int, int64, error) {
	sortColumns := map[string]string{
		service.AccountPerformanceSortHealthScore: "health_score", service.AccountPerformanceSortAvailability: "availability", service.AccountPerformanceSortFailureRate: "failure_rate",
		service.AccountPerformanceSortP95TTFTMS: "p95_ttft_ms", service.AccountPerformanceSortP95DurationMS: "p95_duration_ms", service.AccountPerformanceSortSamples: "samples",
		service.AccountPerformanceSortSuccessCount: "success_count", service.AccountPerformanceSortFailureCount: "failure_count",
	}
	sortBy := filter.SortBy
	if sortBy == "" {
		sortBy = service.AccountPerformanceSortHealthScore
	}
	sortColumn, ok := sortColumns[sortBy]
	if !ok {
		return "", "", 0, 0, 0, fmt.Errorf("unsupported account performance sort %q", filter.SortBy)
	}
	sortOrder := strings.ToUpper(strings.TrimSpace(filter.SortOrder))
	if sortOrder == "" {
		sortOrder = "DESC"
	}
	if sortOrder != "ASC" && sortOrder != "DESC" {
		return "", "", 0, 0, 0, fmt.Errorf("unsupported account performance sort order %q", filter.SortOrder)
	}
	page := filter.Page
	if page < 1 {
		return "", "", 0, 0, 0, fmt.Errorf("account performance page must be at least 1")
	}
	pageSize := filter.PageSize
	if pageSize < 1 || pageSize > 100 {
		return "", "", 0, 0, 0, fmt.Errorf("account performance page size must be between 1 and 100")
	}
	if int64(page-1) > math.MaxInt64/int64(pageSize) {
		return "", "", 0, 0, 0, fmt.Errorf("account performance page offset exceeds int64 range")
	}
	return sortColumn, sortOrder, page, pageSize, int64(page-1) * int64(pageSize), nil
}

func accountPerformanceWhereClause() string {
	return " WHERE bucket_start >= $1 AND bucket_start < $2 AND ($3 = '' OR platform = $3) AND ($4 = 0 OR group_id = $4) AND ($5 = '' OR model = $5) AND ($6 = '' OR protocol = $6) AND ($7 = 0 OR account_id = $7)"
}

func accountPerformanceAggregateColumns() string {
	columns := make([]string, len(accountPerformanceMetricColumns))
	for i, column := range accountPerformanceMetricColumns {
		columns[i] = "COALESCE(SUM(" + column + "), 0) AS " + column
	}
	return strings.Join(columns, ", ")
}

func accountPerformanceCounterColumns() string {
	return strings.Join(accountPerformanceMetricColumns, ", ")
}

func accountPerformanceScoredRateColumns() string {
	const denominator = "NULLIF(attempt_count - client_canceled_count, 0)"
	return "COALESCE(success_count::double precision / " + denominator + ", 0) AS availability, " +
		"COALESCE((attempt_count - client_canceled_count - success_count)::double precision / " + denominator + ", 0) AS failure_rate, " +
		"COALESCE((2 * success_count - attempt_count + client_canceled_count)::double precision / " + denominator + ", 0) AS health_score"
}

func accountPerformanceReplaceMetricsSQL() string {
	assignments := make([]string, len(accountPerformanceMetricColumns))
	for i, column := range accountPerformanceMetricColumns {
		assignments[i] = column + " = EXCLUDED." + column
	}
	return strings.Join(assignments, ", ")
}

func accountPerformanceCounterDestinations(c *service.AccountPerformanceCounters) []any {
	return []any{&c.AttemptCount, &c.SuccessCount, &c.ClientCanceledCount, &c.TTFTTimeoutCount, &c.RateLimitCount, &c.AuthCount, &c.Upstream4xxCount, &c.Upstream5xxCount, &c.TransportCount, &c.ProtocolCount, &c.OtherFailureCount, &c.FailoverCount,
		&c.TTFTLatency.Samples, &c.TTFTSumMS, &c.DurationLatency.Samples, &c.DurationSumMS,
		&c.TTFTLatency.LE1000MS, &c.TTFTLatency.LE2500MS, &c.TTFTLatency.LE5000MS, &c.TTFTLatency.LE10000MS, &c.TTFTLatency.LE30000MS, &c.TTFTLatency.GT30000MS,
		&c.DurationLatency.LE1000MS, &c.DurationLatency.LE2500MS, &c.DurationLatency.LE5000MS, &c.DurationLatency.LE10000MS, &c.DurationLatency.LE30000MS, &c.DurationLatency.GT30000MS}
}

func escapeAccountPerformanceLike(value string) string {
	value = strings.ReplaceAll(value, `\`, `\\`)
	value = strings.ReplaceAll(value, `%`, `\%`)
	return strings.ReplaceAll(value, `_`, `\_`)
}
