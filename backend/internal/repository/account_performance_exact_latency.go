package repository

import (
	"context"
	"database/sql"
	"fmt"
	"math"
	"strings"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/service"
)

// accountPerformanceExactFilter is the common dimension set used by the
// overview, account, and investigation percentile queries.
type accountPerformanceExactFilter struct {
	Start     time.Time
	End       time.Time
	Platform  string
	GroupID   int64
	Model     string
	Protocol  string
	AccountID int64
}

type accountPerformanceExactQueryBuilder struct {
	args []any
}

func (b *accountPerformanceExactQueryBuilder) arg(value any) string {
	b.args = append(b.args, value)
	return fmt.Sprintf("$%d", len(b.args))
}

func normalizeAccountPerformanceExactFilter(start, end time.Time, platform string, groupID int64, model, protocol string, accountID int64) (accountPerformanceExactFilter, error) {
	start, end, _, _, err := normalizeAccountPerformanceFilter(start, end, platform, groupID, model, protocol, accountID)
	if err != nil {
		return accountPerformanceExactFilter{}, err
	}
	return accountPerformanceExactFilter{
		Start: start, End: end, Platform: strings.TrimSpace(platform), GroupID: groupID,
		Model: strings.TrimSpace(model), Protocol: strings.TrimSpace(protocol), AccountID: accountID,
	}, nil
}

func exactFilterFromOverview(filter service.AccountPerformanceOverviewFilter) (accountPerformanceExactFilter, error) {
	return normalizeAccountPerformanceExactFilter(filter.Start, filter.End, filter.Platform, filter.GroupID, filter.Model, filter.Protocol, filter.AccountID)
}

func exactFilterFromAccounts(filter service.AccountPerformanceAccountFilter) (accountPerformanceExactFilter, error) {
	return normalizeAccountPerformanceExactFilter(filter.Start, filter.End, filter.Platform, filter.GroupID, filter.Model, filter.Protocol, filter.AccountID)
}

func exactFilterFromInvestigation(filter service.AccountPerformanceInvestigationFilter) (accountPerformanceExactFilter, error) {
	return normalizeAccountPerformanceExactFilter(filter.Start, filter.End, filter.Platform, filter.GroupID, filter.Model, filter.Protocol, filter.AccountID)
}

// QueryExactOverviewLatency calculates request-level percentiles and returns
// trend values using the same hour/minute bucket source as the aggregate query.
// The raw usage log range is clipped to the first available performance bucket
// so historical logs from before the collector started cannot skew the result.
func (r *accountPerformanceRepository) QueryExactOverviewLatency(ctx context.Context, filter service.AccountPerformanceOverviewFilter) (*service.AccountPerformanceExactOverview, error) {
	exactFilter, err := exactFilterFromOverview(filter)
	if err != nil {
		return nil, err
	}
	if r == nil || r.db == nil {
		return nil, fmt.Errorf("nil account performance repository")
	}
	builder := &accountPerformanceExactQueryBuilder{}
	common := buildAccountPerformanceExactLatencyCTEs(builder, exactFilter)
	query := `WITH ` + common + `,
summary AS (
    SELECT
        percentile_cont(0.50) WITHIN GROUP (ORDER BY first_token_ms) FILTER (WHERE first_token_ms IS NOT NULL) AS p50_ttft_ms,
        percentile_cont(0.95) WITHIN GROUP (ORDER BY first_token_ms) FILTER (WHERE first_token_ms IS NOT NULL) AS p95_ttft_ms,
        percentile_cont(0.95) WITHIN GROUP (ORDER BY duration_ms) FILTER (WHERE duration_ms IS NOT NULL) AS p95_duration_ms,
        COUNT(first_token_ms) AS ttft_samples,
        COUNT(duration_ms) AS duration_samples
    FROM filtered
), trend AS (
    SELECT
        b.bucket_start,
        percentile_cont(0.50) WITHIN GROUP (ORDER BY f.first_token_ms) FILTER (WHERE f.first_token_ms IS NOT NULL) AS p50_ttft_ms,
        percentile_cont(0.95) WITHIN GROUP (ORDER BY f.first_token_ms) FILTER (WHERE f.first_token_ms IS NOT NULL) AS p95_ttft_ms,
        percentile_cont(0.95) WITHIN GROUP (ORDER BY f.duration_ms) FILTER (WHERE f.duration_ms IS NOT NULL) AS p95_duration_ms,
        COUNT(f.first_token_ms) AS ttft_samples,
        COUNT(f.duration_ms) AS duration_samples
    FROM buckets b
    LEFT JOIN filtered f
      ON f.created_at >= b.bucket_start
     AND f.created_at < b.bucket_start + b.bucket_width
    GROUP BY b.bucket_start
)
SELECT 0::smallint AS row_kind, NULL::timestamptz AS bucket_start,
       p50_ttft_ms, p95_ttft_ms, p95_duration_ms, ttft_samples, duration_samples
FROM summary
UNION ALL
SELECT 1::smallint AS row_kind, bucket_start,
       p50_ttft_ms, p95_ttft_ms, p95_duration_ms, ttft_samples, duration_samples
FROM trend
ORDER BY row_kind, bucket_start NULLS FIRST`

	rows, err := r.db.QueryContext(ctx, query, builder.args...)
	if err != nil {
		return nil, fmt.Errorf("query exact account performance overview latency: %w", err)
	}
	defer func() { _ = rows.Close() }()
	result := &service.AccountPerformanceExactOverview{Trend: make(map[time.Time]service.AccountPerformanceExactLatency)}
	if err := scanAccountPerformanceExactLatencyRows(rows, &result.Summary, result.Trend); err != nil {
		return nil, fmt.Errorf("scan exact account performance overview latency: %w", err)
	}
	return result, nil
}

// QueryExactAccountsLatency returns exact P95 values keyed by account id for
// the currently visible account table.
func (r *accountPerformanceRepository) QueryExactAccountsLatency(ctx context.Context, filter service.AccountPerformanceAccountFilter) (map[int64]service.AccountPerformanceExactLatency, error) {
	exactFilter, err := exactFilterFromAccounts(filter)
	if err != nil {
		return nil, err
	}
	if r == nil || r.db == nil {
		return nil, fmt.Errorf("nil account performance repository")
	}
	builder := &accountPerformanceExactQueryBuilder{}
	common := buildAccountPerformanceExactLatencyCTEs(builder, exactFilter)
	query := `WITH ` + common + `
SELECT account_id,
       percentile_cont(0.95) WITHIN GROUP (ORDER BY first_token_ms) FILTER (WHERE first_token_ms IS NOT NULL) AS p95_ttft_ms,
       percentile_cont(0.95) WITHIN GROUP (ORDER BY duration_ms) FILTER (WHERE duration_ms IS NOT NULL) AS p95_duration_ms,
       COUNT(first_token_ms) AS ttft_samples,
       COUNT(duration_ms) AS duration_samples
FROM filtered
GROUP BY account_id`
	rows, err := r.db.QueryContext(ctx, query, builder.args...)
	if err != nil {
		return nil, fmt.Errorf("query exact account performance account latency: %w", err)
	}
	defer func() { _ = rows.Close() }()
	result := make(map[int64]service.AccountPerformanceExactLatency)
	for rows.Next() {
		var accountID int64
		var p95TTFT, p95Duration sql.NullFloat64
		var ttftSamples, durationSamples int64
		if err := rows.Scan(&accountID, &p95TTFT, &p95Duration, &ttftSamples, &durationSamples); err != nil {
			return nil, fmt.Errorf("scan exact account performance account latency: %w", err)
		}
		result[accountID] = service.AccountPerformanceExactLatency{
			P95TTFTMS: p95LatencyValue(p95TTFT), P95DurationMS: p95LatencyValue(p95Duration),
			TTFTSampleCount: ttftSamples, DurationSamples: durationSamples,
		}
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate exact account performance account latency: %w", err)
	}
	return result, nil
}

// QueryExactInvestigationLatency returns exact trend values for the selected
// account investigation drawer.
func (r *accountPerformanceRepository) QueryExactInvestigationLatency(ctx context.Context, filter service.AccountPerformanceInvestigationFilter) (map[time.Time]service.AccountPerformanceExactLatency, error) {
	exactFilter, err := exactFilterFromInvestigation(filter)
	if err != nil {
		return nil, err
	}
	if r == nil || r.db == nil {
		return nil, fmt.Errorf("nil account performance repository")
	}
	builder := &accountPerformanceExactQueryBuilder{}
	common := buildAccountPerformanceExactLatencyCTEs(builder, exactFilter)
	query := `WITH ` + common + `
SELECT b.bucket_start,
       percentile_cont(0.50) WITHIN GROUP (ORDER BY f.first_token_ms) FILTER (WHERE f.first_token_ms IS NOT NULL) AS p50_ttft_ms,
       percentile_cont(0.95) WITHIN GROUP (ORDER BY f.first_token_ms) FILTER (WHERE f.first_token_ms IS NOT NULL) AS p95_ttft_ms,
       percentile_cont(0.95) WITHIN GROUP (ORDER BY f.duration_ms) FILTER (WHERE f.duration_ms IS NOT NULL) AS p95_duration_ms,
       COUNT(f.first_token_ms) AS ttft_samples,
       COUNT(f.duration_ms) AS duration_samples
FROM buckets b
LEFT JOIN filtered f
  ON f.created_at >= b.bucket_start
 AND f.created_at < b.bucket_start + b.bucket_width
GROUP BY b.bucket_start
ORDER BY b.bucket_start`
	rows, err := r.db.QueryContext(ctx, query, builder.args...)
	if err != nil {
		return nil, fmt.Errorf("query exact account performance investigation latency: %w", err)
	}
	defer func() { _ = rows.Close() }()
	result := make(map[time.Time]service.AccountPerformanceExactLatency)
	for rows.Next() {
		var bucket time.Time
		var p50TTFT, p95TTFT, p95Duration sql.NullFloat64
		var ttftSamples, durationSamples int64
		if err := rows.Scan(&bucket, &p50TTFT, &p95TTFT, &p95Duration, &ttftSamples, &durationSamples); err != nil {
			return nil, fmt.Errorf("scan exact account performance investigation latency: %w", err)
		}
		result[bucket.UTC()] = service.AccountPerformanceExactLatency{
			P50TTFTMS: p95LatencyValue(p50TTFT), P95TTFTMS: p95LatencyValue(p95TTFT), P95DurationMS: p95LatencyValue(p95Duration),
			TTFTSampleCount: ttftSamples, DurationSamples: durationSamples,
		}
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate exact account performance investigation latency: %w", err)
	}
	return result, nil
}

// buildAccountPerformanceExactLatencyCTEs centralizes the source/window rules
// so all three endpoints use exactly the same coverage and dimension filters.
func buildAccountPerformanceExactLatencyCTEs(builder *accountPerformanceExactQueryBuilder, filter accountPerformanceExactFilter) string {
	startArg := builder.arg(filter.Start)
	now := accountPerformanceNow().UTC()
	currentHour := now.Truncate(time.Hour)
	hourlyEnd := filter.End.Truncate(time.Hour)
	if hourlyEnd.After(currentHour) {
		hourlyEnd = currentHour
	}
	minuteLower := filter.Start.Truncate(time.Minute)
	minuteEnd := filter.End.Truncate(time.Minute)
	if !filter.End.Equal(minuteEnd) {
		minuteEnd = minuteEnd.Add(time.Minute)
	}
	startHour := filter.Start.Truncate(time.Hour)
	endHour := filter.End.Truncate(time.Hour)
	startPartial := !filter.Start.Equal(startHour)
	endPartial := !filter.End.Equal(endHour)
	minuteRetentionStart := now.Add(-7 * 24 * time.Hour).Truncate(time.Minute)
	hourlyEndArg := builder.arg(hourlyEnd)
	minuteLowerArg := builder.arg(minuteLower)
	minuteEndArg := builder.arg(minuteEnd)
	retentionArg := builder.arg(minuteRetentionStart)
	currentHourArg := builder.arg(currentHour)
	startPartialArg := builder.arg(startPartial)
	startHourArg := builder.arg(startHour)
	startHourEndArg := builder.arg(startHour.Add(time.Hour))
	endPartialArg := builder.arg(endPartial)
	endHourArg := builder.arg(endHour)

	source := fmt.Sprintf(`aggregate_source AS (
    SELECT bucket_start, account_id, platform, group_id, model, protocol, attempt_count,
           INTERVAL '1 hour' AS bucket_width
    FROM account_performance_hourly
    WHERE bucket_start >= %s AND bucket_start < %s
    UNION ALL
    SELECT bucket_start, account_id, platform, group_id, model, protocol, attempt_count,
           INTERVAL '1 minute' AS bucket_width
    FROM account_performance_minute
    WHERE bucket_start >= %s AND bucket_start < %s
      AND bucket_start >= %s
      AND (bucket_start >= %s OR (%s AND bucket_start >= %s AND bucket_start < %s) OR (%s AND bucket_start >= %s))
)`, startArg, hourlyEndArg, minuteLowerArg, minuteEndArg, retentionArg, currentHourArg, startPartialArg, startHourArg, startHourEndArg, endPartialArg, endHourArg)

	aggregateDimensions := accountPerformanceExactAggregateDimensions(builder, filter, "ap")
	coverage := "scoped_source AS (SELECT * FROM aggregate_source ap WHERE ap.attempt_count > 0"
	if len(aggregateDimensions) > 0 {
		coverage += " AND " + strings.Join(aggregateDimensions, " AND ")
	}
	coverage += `),
coverage AS (
    SELECT MIN(bucket_start) AS coverage_start FROM scoped_source
),
buckets AS (
    SELECT DISTINCT bucket_start, bucket_width FROM scoped_source
)`

	usageJoin, usageConditions := accountPerformanceExactUsageWhere(builder, filter)
	filtered := `filtered AS (
    SELECT ul.account_id, ul.created_at, ul.first_token_ms, ul.duration_ms
    FROM usage_logs ul
    ` + usageJoin + `
    CROSS JOIN coverage c
    WHERE ` + strings.Join(usageConditions, " AND ") + `
)`
	return source + ",\n" + coverage + ",\n" + filtered
}

func accountPerformanceExactAggregateDimensions(builder *accountPerformanceExactQueryBuilder, filter accountPerformanceExactFilter, alias string) []string {
	conditions := make([]string, 0, 5)
	if filter.Platform != "" {
		conditions = append(conditions, fmt.Sprintf("%s.platform = %s", alias, builder.arg(filter.Platform)))
	}
	if filter.GroupID > 0 {
		conditions = append(conditions, fmt.Sprintf("%s.group_id = %s", alias, builder.arg(filter.GroupID)))
	}
	if filter.Model != "" {
		conditions = append(conditions, fmt.Sprintf("%s.model = %s", alias, builder.arg(filter.Model)))
	}
	if filter.Protocol != "" {
		conditions = append(conditions, fmt.Sprintf("%s.protocol = %s", alias, builder.arg(filter.Protocol)))
	}
	if filter.AccountID > 0 {
		conditions = append(conditions, fmt.Sprintf("%s.account_id = %s", alias, builder.arg(filter.AccountID)))
	}
	return conditions
}

func accountPerformanceExactUsageWhere(builder *accountPerformanceExactQueryBuilder, filter accountPerformanceExactFilter) (string, []string) {
	join := ""
	conditions := []string{
		"c.coverage_start IS NOT NULL",
		fmt.Sprintf("ul.created_at >= %s", builder.arg(filter.Start)),
		fmt.Sprintf("ul.created_at < %s", builder.arg(filter.End)),
		"ul.created_at >= c.coverage_start",
		"(ul.first_token_ms IS NOT NULL OR ul.duration_ms IS NOT NULL)",
		// Cyber policy usage rows are billing/audit placeholders and do not
		// represent a completed upstream attempt recorded by performance.
		"COALESCE(ul.request_type, 0) <> 4",
		accountPerformanceExactUsageProtocolExpression("ul") + " IN ('responses', 'chat_completions', 'anthropic_messages')",
	}
	if filter.Platform != "" {
		// Account performance records the selected account's platform, so keep
		// this filter aligned with the aggregate dimension rather than the
		// effective group platform used by the operations dashboard.
		join = "LEFT JOIN accounts a ON a.id = ul.account_id"
		platformExpr := "a.platform"
		conditions = append(conditions, fmt.Sprintf("%s = %s", platformExpr, builder.arg(filter.Platform)))
	}
	if filter.GroupID > 0 {
		conditions = append(conditions, fmt.Sprintf("ul.group_id = %s", builder.arg(filter.GroupID)))
	}
	if filter.Model != "" {
		conditions = append(conditions, fmt.Sprintf("%s = %s", accountPerformanceExactUsageModelExpression("ul"), builder.arg(filter.Model)))
	}
	if filter.Protocol != "" {
		conditions = append(conditions, fmt.Sprintf("%s = %s", accountPerformanceExactUsageProtocolExpression("ul"), builder.arg(filter.Protocol)))
	}
	if filter.AccountID > 0 {
		conditions = append(conditions, fmt.Sprintf("ul.account_id = %s", builder.arg(filter.AccountID)))
	}
	return join, conditions
}

func accountPerformanceExactUsageModelExpression(alias string) string {
	return fmt.Sprintf("COALESCE(NULLIF(TRIM(%s.requested_model), ''), %s.model)", alias, alias)
}

func accountPerformanceExactUsageProtocolExpression(alias string) string {
	endpoint := fmt.Sprintf("RTRIM(COALESCE(NULLIF(TRIM(%s.upstream_endpoint), ''), NULLIF(TRIM(%s.inbound_endpoint), '')), '/')", alias, alias)
	return "CASE " +
		"WHEN " + endpoint + " LIKE '%/messages' THEN 'anthropic_messages' " +
		"WHEN " + endpoint + " LIKE '%/chat/completions' THEN 'chat_completions' " +
		"WHEN " + endpoint + " LIKE '%/responses' THEN 'responses' " +
		"ELSE '' END"
}

func scanAccountPerformanceExactLatencyRows(rows *sql.Rows, summary *service.AccountPerformanceExactLatency, trend map[time.Time]service.AccountPerformanceExactLatency) error {
	for rows.Next() {
		var kind int16
		var bucket sql.NullTime
		var p50TTFT, p95TTFT, p95Duration sql.NullFloat64
		var ttftSamples, durationSamples int64
		if err := rows.Scan(&kind, &bucket, &p50TTFT, &p95TTFT, &p95Duration, &ttftSamples, &durationSamples); err != nil {
			return err
		}
		latency := service.AccountPerformanceExactLatency{
			P50TTFTMS: p95LatencyValue(p50TTFT), P95TTFTMS: p95LatencyValue(p95TTFT), P95DurationMS: p95LatencyValue(p95Duration),
			TTFTSampleCount: ttftSamples, DurationSamples: durationSamples,
		}
		if kind == 0 {
			if summary != nil {
				*summary = latency
			}
			continue
		}
		if kind == 1 && bucket.Valid && trend != nil {
			trend[bucket.Time.UTC()] = latency
		}
	}
	return rows.Err()
}

func p95LatencyValue(value sql.NullFloat64) int64 {
	if !value.Valid || math.IsNaN(value.Float64) || math.IsInf(value.Float64, 0) || value.Float64 <= 0 {
		return 0
	}
	return int64(math.Round(value.Float64))
}
