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

type firstTokenTimeoutStatsRepository struct {
	db         sqlExecutor
	snapshotDB *sql.DB
}

func NewFirstTokenTimeoutStatsRepository(db *sql.DB) service.FirstTokenTimeoutStatsRepository {
	return &firstTokenTimeoutStatsRepository{db: db, snapshotDB: db}
}

func newFirstTokenTimeoutStatsRepositoryWithSQL(db sqlExecutor) *firstTokenTimeoutStatsRepository {
	repository := &firstTokenTimeoutStatsRepository{db: db}
	if sqlDB, ok := db.(*sql.DB); ok {
		repository.snapshotDB = sqlDB
	}
	return repository
}

type firstTokenStatsKey struct {
	bucketStart    time.Time
	scope          string
	accountID      int64
	protocol       string
	platform       string
	model          string
	timeoutSeconds int
	outcome        string
	failureKind    string
}

const maxFirstTokenStatsUpsertKeys = 1000

func (r *firstTokenTimeoutStatsRepository) UpsertBatch(ctx context.Context, deltas []service.FirstTokenStatsDelta) error {
	if len(deltas) == 0 {
		return nil
	}

	aggregated := make([]service.FirstTokenStatsDelta, 0, len(deltas))
	indexByKey := make(map[firstTokenStatsKey]int, len(deltas))
	for i, input := range deltas {
		delta, err := normalizeFirstTokenStatsDelta(input)
		if err != nil {
			return fmt.Errorf("invalid first token stats delta %d: %w", i, err)
		}
		key := firstTokenStatsKey{
			bucketStart:    delta.BucketStart,
			scope:          delta.Scope,
			accountID:      delta.AccountID,
			protocol:       delta.Protocol,
			platform:       delta.Platform,
			model:          delta.Model,
			timeoutSeconds: delta.TimeoutSeconds,
			outcome:        delta.Outcome,
			failureKind:    delta.FailureKind,
		}
		if idx, ok := indexByKey[key]; ok {
			existing := &aggregated[idx]
			var err error
			if existing.SampleCount, err = addFirstTokenStatsCounter(existing.SampleCount, delta.SampleCount); err != nil {
				return fmt.Errorf("sample_count overflow for delta %d: %w", i, err)
			}
			if existing.TTFTSampleCount, err = addFirstTokenStatsCounter(existing.TTFTSampleCount, delta.TTFTSampleCount); err != nil {
				return fmt.Errorf("ttft_sample_count overflow for delta %d: %w", i, err)
			}
			if existing.TTFTSumMS, err = addFirstTokenStatsCounter(existing.TTFTSumMS, delta.TTFTSumMS); err != nil {
				return fmt.Errorf("ttft_sum_ms overflow for delta %d: %w", i, err)
			}
			if existing.TTFTAffectedCount, err = addFirstTokenStatsCounter(existing.TTFTAffectedCount, delta.TTFTAffectedCount); err != nil {
				return fmt.Errorf("ttft_affected_count overflow for delta %d: %w", i, err)
			}
			if delta.TTFTMaxMS > existing.TTFTMaxMS {
				existing.TTFTMaxMS = delta.TTFTMaxMS
			}
			continue
		}
		if len(aggregated) >= maxFirstTokenStatsUpsertKeys {
			return fmt.Errorf("first token stats unique key limit exceeded: max %d", maxFirstTokenStatsUpsertKeys)
		}
		indexByKey[key] = len(aggregated)
		aggregated = append(aggregated, delta)
	}
	for i, delta := range aggregated {
		if err := validateFirstTokenStatsCrossInvariants(delta); err != nil {
			return fmt.Errorf("invalid aggregated first token stats delta %d: %w", i, err)
		}
	}
	if r == nil || r.db == nil {
		return fmt.Errorf("nil first token timeout stats repository")
	}

	var query strings.Builder
	query.WriteString(`INSERT INTO first_token_timeout_stats_hourly (
bucket_start, scope, account_id, protocol, platform, model, timeout_seconds,
outcome, failure_kind, sample_count, ttft_sample_count, ttft_sum_ms,
ttft_max_ms, ttft_affected_count
) VALUES `)
	args := make([]any, 0, len(aggregated)*14)
	for i, delta := range aggregated {
		if i > 0 {
			query.WriteString(", ")
		}
		query.WriteByte('(')
		for column := 0; column < 14; column++ {
			if column > 0 {
				query.WriteString(", ")
			}
			query.WriteByte('$')
			query.WriteString(strconv.Itoa(len(args) + column + 1))
		}
		query.WriteByte(')')
		args = append(args,
			delta.BucketStart,
			delta.Scope,
			delta.AccountID,
			delta.Protocol,
			delta.Platform,
			delta.Model,
			delta.TimeoutSeconds,
			delta.Outcome,
			delta.FailureKind,
			delta.SampleCount,
			delta.TTFTSampleCount,
			delta.TTFTSumMS,
			delta.TTFTMaxMS,
			delta.TTFTAffectedCount,
		)
	}
	query.WriteString(`
ON CONFLICT (
bucket_start, scope, account_id, protocol, platform, model,
timeout_seconds, outcome, failure_kind
) DO UPDATE SET
sample_count = first_token_timeout_stats_hourly.sample_count + EXCLUDED.sample_count,
ttft_sample_count = first_token_timeout_stats_hourly.ttft_sample_count + EXCLUDED.ttft_sample_count,
ttft_sum_ms = first_token_timeout_stats_hourly.ttft_sum_ms + EXCLUDED.ttft_sum_ms,
ttft_max_ms = GREATEST(first_token_timeout_stats_hourly.ttft_max_ms, EXCLUDED.ttft_max_ms),
ttft_affected_count = first_token_timeout_stats_hourly.ttft_affected_count + EXCLUDED.ttft_affected_count,
updated_at = NOW()`)

	_, err := r.db.ExecContext(ctx, query.String(), args...)
	return err
}

func addFirstTokenStatsCounter(left, right int64) (int64, error) {
	if right > math.MaxInt64-left {
		return 0, fmt.Errorf("counter overflow")
	}
	return left + right, nil
}

func normalizeFirstTokenStatsDelta(delta service.FirstTokenStatsDelta) (service.FirstTokenStatsDelta, error) {
	if delta.BucketStart.IsZero() {
		return service.FirstTokenStatsDelta{}, fmt.Errorf("bucket start is required")
	}
	delta.BucketStart = delta.BucketStart.UTC().Truncate(time.Hour)
	if strings.ContainsRune(delta.Protocol, '\x00') {
		return service.FirstTokenStatsDelta{}, fmt.Errorf("protocol contains NUL")
	}
	if strings.ContainsRune(delta.Platform, '\x00') {
		return service.FirstTokenStatsDelta{}, fmt.Errorf("platform contains NUL")
	}
	if strings.ContainsRune(delta.Model, '\x00') {
		return service.FirstTokenStatsDelta{}, fmt.Errorf("model contains NUL")
	}

	switch delta.Scope {
	case service.FirstTokenStatsScopeAttempt:
		if delta.AccountID <= 0 {
			return service.FirstTokenStatsDelta{}, fmt.Errorf("attempt account id must be positive")
		}
		if strings.TrimSpace(delta.Platform) == "" {
			return service.FirstTokenStatsDelta{}, fmt.Errorf("attempt platform is required")
		}
	case service.FirstTokenStatsScopeRequest:
		delta.AccountID = 0
		delta.Platform = ""
	default:
		return service.FirstTokenStatsDelta{}, fmt.Errorf("unknown scope %q", delta.Scope)
	}

	if strings.TrimSpace(delta.Protocol) == "" {
		return service.FirstTokenStatsDelta{}, fmt.Errorf("protocol is required")
	}
	if utf8.RuneCountInString(delta.Protocol) > 32 {
		return service.FirstTokenStatsDelta{}, fmt.Errorf("protocol exceeds 32 characters")
	}
	if utf8.RuneCountInString(delta.Platform) > 32 {
		return service.FirstTokenStatsDelta{}, fmt.Errorf("platform exceeds 32 characters")
	}
	if utf8.RuneCountInString(delta.Model) > 255 {
		return service.FirstTokenStatsDelta{}, fmt.Errorf("model exceeds 255 characters")
	}
	if delta.TimeoutSeconds < 1 || delta.TimeoutSeconds > 300 {
		return service.FirstTokenStatsDelta{}, fmt.Errorf("timeout seconds must be between 1 and 300")
	}
	if !isValidFirstTokenStatsOutcome(delta.Scope, delta.Outcome) {
		return service.FirstTokenStatsDelta{}, fmt.Errorf("invalid %s outcome %q", delta.Scope, delta.Outcome)
	}
	if delta.Outcome == service.FirstTokenStatsAttemptOtherFailure {
		if !isValidFirstTokenStatsFailureKind(delta.FailureKind) {
			return service.FirstTokenStatsDelta{}, fmt.Errorf("other failure requires a valid failure kind")
		}
	} else if delta.FailureKind != "" {
		return service.FirstTokenStatsDelta{}, fmt.Errorf("failure kind is only valid for other_failure")
	}
	if delta.SampleCount < 0 || delta.TTFTSampleCount < 0 || delta.TTFTSumMS < 0 || delta.TTFTMaxMS < 0 || delta.TTFTAffectedCount < 0 {
		return service.FirstTokenStatsDelta{}, fmt.Errorf("counters must be non-negative")
	}
	if delta.TTFTMaxMS > math.MaxInt32 {
		return service.FirstTokenStatsDelta{}, fmt.Errorf("ttft max exceeds PostgreSQL integer range")
	}
	if err := validateFirstTokenStatsCrossInvariants(delta); err != nil {
		return service.FirstTokenStatsDelta{}, err
	}
	return delta, nil
}

func validateFirstTokenStatsCrossInvariants(delta service.FirstTokenStatsDelta) error {
	if delta.TTFTSampleCount > delta.SampleCount {
		return fmt.Errorf("ttft sample count cannot exceed sample count")
	}
	if delta.TTFTSampleCount == 0 && (delta.TTFTSumMS != 0 || delta.TTFTMaxMS != 0) {
		return fmt.Errorf("ttft metrics require at least one ttft sample")
	}
	if delta.TTFTMaxMS > delta.TTFTSumMS {
		return fmt.Errorf("ttft max cannot exceed ttft sum")
	}

	switch delta.Scope {
	case service.FirstTokenStatsScopeAttempt:
		if delta.TTFTAffectedCount != 0 {
			return fmt.Errorf("attempt delta cannot carry affected request count")
		}
		if delta.Outcome == service.FirstTokenStatsAttemptTTFTTimeout && delta.TTFTSampleCount != 0 {
			return fmt.Errorf("ttft timeout attempt cannot carry ttft samples")
		}
	case service.FirstTokenStatsScopeRequest:
		if delta.TTFTSampleCount != 0 || delta.TTFTSumMS != 0 || delta.TTFTMaxMS != 0 {
			return fmt.Errorf("request delta cannot carry ttft metrics")
		}
		if delta.TTFTAffectedCount > delta.SampleCount {
			return fmt.Errorf("affected request count cannot exceed sample count")
		}
		switch delta.Outcome {
		case service.FirstTokenStatsRequestSuccess:
			if delta.TTFTAffectedCount != 0 {
				return fmt.Errorf("successful request cannot be ttft affected")
			}
		case service.FirstTokenStatsRequestRecoveredAfterTTFT, service.FirstTokenStatsRequestTTFTExhausted:
			if delta.TTFTAffectedCount != delta.SampleCount {
				return fmt.Errorf("ttft recovery and exhaustion require every sample to be affected")
			}
		}
	}
	return nil
}

func isValidFirstTokenStatsOutcome(scope, outcome string) bool {
	switch scope {
	case service.FirstTokenStatsScopeAttempt:
		switch outcome {
		case service.FirstTokenStatsAttemptSuccess,
			service.FirstTokenStatsAttemptTTFTTimeout,
			service.FirstTokenStatsAttemptClientCanceled,
			service.FirstTokenStatsAttemptOtherFailure:
			return true
		}
	case service.FirstTokenStatsScopeRequest:
		switch outcome {
		case service.FirstTokenStatsRequestSuccess,
			service.FirstTokenStatsRequestRecoveredAfterTTFT,
			service.FirstTokenStatsRequestTTFTExhausted,
			service.FirstTokenStatsRequestClientCanceled,
			service.FirstTokenStatsRequestOtherFailure:
			return true
		}
	}
	return false
}

func isValidFirstTokenStatsFailureKind(failureKind string) bool {
	switch failureKind {
	case service.FirstTokenStatsFailureRateLimit,
		service.FirstTokenStatsFailureAuth,
		service.FirstTokenStatsFailureUpstream4xx,
		service.FirstTokenStatsFailureUpstream5xx,
		service.FirstTokenStatsFailureTransport,
		service.FirstTokenStatsFailureStreamIdleTimeout,
		service.FirstTokenStatsFailureProtocol,
		service.FirstTokenStatsFailureOther:
		return true
	default:
		return false
	}
}

func (r *firstTokenTimeoutStatsRepository) QueryOverview(ctx context.Context, filter service.FirstTokenStatsOverviewFilter) (*service.FirstTokenStatsOverview, error) {
	start, end, err := normalizeFirstTokenStatsOverviewFilter(filter)
	if err != nil {
		return nil, err
	}
	if r == nil || r.db == nil {
		return nil, fmt.Errorf("nil first token timeout stats repository")
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

	args := []any{start, end, filter.Protocol, filter.Model}
	const summaryQuery = `
SELECT
    COALESCE(SUM(sample_count) FILTER (WHERE scope = 'request'), 0) AS controlled_requests,
    COALESCE(SUM(sample_count) FILTER (WHERE scope = 'request' AND outcome = 'client_canceled'), 0) AS client_canceled_requests,
    COALESCE(SUM(sample_count) FILTER (WHERE scope = 'attempt' AND outcome = 'ttft_timeout'), 0) AS attempt_ttft_timeout_count,
    COALESCE(SUM(sample_count) FILTER (WHERE scope = 'attempt' AND outcome IN ('success', 'ttft_timeout', 'other_failure')), 0) AS attempt_denominator,
    COALESCE(SUM(sample_count) FILTER (WHERE scope = 'request' AND outcome = 'recovered_after_ttft'), 0) AS recovered_count,
    COALESCE(SUM(ttft_affected_count) FILTER (WHERE scope = 'request' AND outcome <> 'client_canceled'), 0) AS affected_count,
    COALESCE(SUM(sample_count) FILTER (WHERE scope = 'request' AND outcome = 'ttft_exhausted'), 0) AS final_ttft_count,
    COALESCE(SUM(sample_count) FILTER (WHERE scope = 'request' AND outcome IN ('success', 'recovered_after_ttft', 'ttft_exhausted', 'other_failure')), 0) AS request_denominator,
    COALESCE(SUM(sample_count) FILTER (WHERE scope = 'request' AND outcome = 'other_failure'), 0) AS other_final_count
FROM first_token_timeout_stats_hourly
WHERE bucket_start >= $1 AND bucket_start < $2
  AND ($3 = '' OR protocol = $3)
  AND ($4 = '' OR model = $4)`

	var summaryValues firstTokenStatsRateValues
	if err := scanSingleRow(ctx, tx, summaryQuery, args,
		&summaryValues.controlledRequests,
		&summaryValues.clientCanceledRequests,
		&summaryValues.attemptTTFTTimeoutCount,
		&summaryValues.attemptDenominator,
		&summaryValues.recoveredCount,
		&summaryValues.affectedCount,
		&summaryValues.finalTTFTCount,
		&summaryValues.requestDenominator,
		&summaryValues.otherFinalCount,
	); err != nil {
		return nil, fmt.Errorf("query first token stats summary: %w", err)
	}

	const trendQuery = `
WITH buckets AS (
    SELECT generate_series(
        $1::timestamptz,
        $2::timestamptz - INTERVAL '1 hour',
        INTERVAL '1 hour'
    ) AS bucket_start
), aggregated AS (
    SELECT
        bucket_start,
        COALESCE(SUM(sample_count) FILTER (WHERE scope = 'attempt' AND outcome = 'ttft_timeout'), 0) AS attempt_ttft_timeout_count,
        COALESCE(SUM(sample_count) FILTER (WHERE scope = 'attempt' AND outcome IN ('success', 'ttft_timeout', 'other_failure')), 0) AS attempt_denominator,
        COALESCE(SUM(sample_count) FILTER (WHERE scope = 'request' AND outcome = 'recovered_after_ttft'), 0) AS recovered_count,
        COALESCE(SUM(ttft_affected_count) FILTER (WHERE scope = 'request' AND outcome <> 'client_canceled'), 0) AS affected_count,
        COALESCE(SUM(sample_count) FILTER (WHERE scope = 'request' AND outcome = 'ttft_exhausted'), 0) AS final_ttft_count,
        COALESCE(SUM(sample_count) FILTER (WHERE scope = 'request' AND outcome IN ('success', 'recovered_after_ttft', 'ttft_exhausted', 'other_failure')), 0) AS request_denominator,
        COALESCE(SUM(sample_count) FILTER (WHERE scope = 'request' AND outcome = 'other_failure'), 0) AS other_final_count
    FROM first_token_timeout_stats_hourly
    WHERE bucket_start >= $1 AND bucket_start < $2
      AND ($3 = '' OR protocol = $3)
      AND ($4 = '' OR model = $4)
    GROUP BY bucket_start
)
SELECT
    buckets.bucket_start,
    COALESCE(aggregated.attempt_ttft_timeout_count, 0),
    COALESCE(aggregated.attempt_denominator, 0),
    COALESCE(aggregated.recovered_count, 0),
    COALESCE(aggregated.affected_count, 0),
    COALESCE(aggregated.final_ttft_count, 0),
    COALESCE(aggregated.request_denominator, 0),
    COALESCE(aggregated.other_final_count, 0)
FROM buckets
LEFT JOIN aggregated USING (bucket_start)
ORDER BY buckets.bucket_start`

	rows, err := tx.QueryContext(ctx, trendQuery, args...)
	if err != nil {
		return nil, fmt.Errorf("query first token stats trend: %w", err)
	}
	trend := make([]service.FirstTokenStatsTrendPoint, 0)
	for rows.Next() {
		var bucket time.Time
		var values firstTokenStatsRateValues
		if err := rows.Scan(
			&bucket,
			&values.attemptTTFTTimeoutCount,
			&values.attemptDenominator,
			&values.recoveredCount,
			&values.affectedCount,
			&values.finalTTFTCount,
			&values.requestDenominator,
			&values.otherFinalCount,
		); err != nil {
			_ = rows.Close()
			return nil, fmt.Errorf("scan first token stats trend: %w", err)
		}
		trend = append(trend, values.trendPoint(bucket))
	}
	if err := rows.Err(); err != nil {
		_ = rows.Close()
		return nil, fmt.Errorf("iterate first token stats trend: %w", err)
	}
	if err := rows.Close(); err != nil {
		return nil, fmt.Errorf("close first token stats trend: %w", err)
	}

	const failureQuery = `
SELECT failure_kind, SUM(sample_count) AS sample_count
FROM first_token_timeout_stats_hourly
WHERE bucket_start >= $1 AND bucket_start < $2
  AND ($3 = '' OR protocol = $3)
  AND ($4 = '' OR model = $4)
  AND scope = 'request'
  AND outcome = 'other_failure'
GROUP BY failure_kind
ORDER BY sample_count DESC, failure_kind ASC`
	failureRows, err := tx.QueryContext(ctx, failureQuery, args...)
	if err != nil {
		return nil, fmt.Errorf("query first token stats failure distribution: %w", err)
	}
	failures := make([]service.FirstTokenStatsFailureDistribution, 0)
	for failureRows.Next() {
		var item service.FirstTokenStatsFailureDistribution
		if err := failureRows.Scan(&item.FailureKind, &item.SampleCount); err != nil {
			_ = failureRows.Close()
			return nil, fmt.Errorf("scan first token stats failure distribution: %w", err)
		}
		failures = append(failures, item)
	}
	if err := failureRows.Err(); err != nil {
		_ = failureRows.Close()
		return nil, fmt.Errorf("iterate first token stats failure distribution: %w", err)
	}
	if err := failureRows.Close(); err != nil {
		return nil, fmt.Errorf("close first token stats failure distribution: %w", err)
	}

	overview := &service.FirstTokenStatsOverview{
		Summary:       summaryValues.summary(),
		Trend:         trend,
		OtherFailures: failures,
	}
	if err := tx.Commit(); err != nil {
		return nil, fmt.Errorf("commit first token stats overview snapshot: %w", err)
	}
	committed = true
	return overview, nil
}

type firstTokenStatsRateValues struct {
	controlledRequests      int64
	clientCanceledRequests  int64
	attemptTTFTTimeoutCount int64
	attemptDenominator      int64
	recoveredCount          int64
	affectedCount           int64
	finalTTFTCount          int64
	requestDenominator      int64
	otherFinalCount         int64
}

func (v firstTokenStatsRateValues) summary() service.FirstTokenStatsSummary {
	return service.FirstTokenStatsSummary{
		ControlledRequests:     v.controlledRequests,
		ClientCanceledRequests: v.clientCanceledRequests,
		AttemptTTFTTimeoutRate: firstTokenStatsRatio(v.attemptTTFTTimeoutCount, v.attemptDenominator),
		RecoveryRate:           firstTokenStatsRatio(v.recoveredCount, v.affectedCount),
		FinalTTFTFailureRate:   firstTokenStatsRatio(v.finalTTFTCount, v.requestDenominator),
		OtherFinalFailureRate:  firstTokenStatsRatio(v.otherFinalCount, v.requestDenominator),
	}
}

func (v firstTokenStatsRateValues) trendPoint(bucket time.Time) service.FirstTokenStatsTrendPoint {
	return service.FirstTokenStatsTrendPoint{
		BucketStart:            bucket.UTC(),
		AttemptTTFTTimeoutRate: firstTokenStatsRatio(v.attemptTTFTTimeoutCount, v.attemptDenominator),
		RecoveryRate:           firstTokenStatsRatio(v.recoveredCount, v.affectedCount),
		FinalTTFTFailureRate:   firstTokenStatsRatio(v.finalTTFTCount, v.requestDenominator),
		OtherFinalFailureRate:  firstTokenStatsRatio(v.otherFinalCount, v.requestDenominator),
	}
}

func firstTokenStatsRatio(numerator, denominator int64) service.FirstTokenStatsRatio {
	ratio := service.FirstTokenStatsRatio{Numerator: numerator, Denominator: denominator}
	if denominator > 0 {
		ratio.Rate = float64(numerator) / float64(denominator)
	}
	return ratio
}

func normalizeFirstTokenStatsOverviewFilter(filter service.FirstTokenStatsOverviewFilter) (time.Time, time.Time, error) {
	var duration time.Duration
	switch filter.Range {
	case service.FirstTokenStatsRange24Hours:
		duration = 24 * time.Hour
	case service.FirstTokenStatsRange7Days:
		duration = 7 * 24 * time.Hour
	case service.FirstTokenStatsRange30Days:
		duration = 30 * 24 * time.Hour
	case service.FirstTokenStatsRange90Days:
		duration = 90 * 24 * time.Hour
	default:
		return time.Time{}, time.Time{}, fmt.Errorf("unsupported first token stats range %q", filter.Range)
	}
	if utf8.RuneCountInString(filter.Protocol) > 32 {
		return time.Time{}, time.Time{}, fmt.Errorf("protocol exceeds 32 characters")
	}
	if utf8.RuneCountInString(filter.Model) > 255 {
		return time.Time{}, time.Time{}, fmt.Errorf("model exceeds 255 characters")
	}
	end := filter.End
	if end.IsZero() {
		end = time.Now()
	}
	end = end.UTC().Truncate(time.Hour).Add(time.Hour)
	return end.Add(-duration), end, nil
}

func (r *firstTokenTimeoutStatsRepository) QueryAccounts(ctx context.Context, filter service.FirstTokenStatsAccountFilter) (*service.FirstTokenStatsAccountPage, error) {
	start, end, sortColumn, sortOrder, page, pageSize, offset, err := normalizeFirstTokenStatsAccountFilter(filter)
	if err != nil {
		return nil, err
	}
	if r == nil || r.db == nil {
		return nil, fmt.Errorf("nil first token timeout stats repository")
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
		search = "%" + escapeFirstTokenStatsLike(filter.Search) + "%"
	}
	args := []any{start, end, filter.Protocol, filter.Model, filter.Platform, filter.AccountID, search}
	const accountsCTE = `
WITH aggregated AS (
    SELECT
        account_id,
        platform,
        COALESCE(SUM(sample_count) FILTER (WHERE outcome IN ('success', 'ttft_timeout', 'other_failure')), 0) AS samples,
        COALESCE(SUM(sample_count) FILTER (WHERE outcome = 'success'), 0) AS success_count,
        COALESCE(SUM(sample_count) FILTER (WHERE outcome = 'ttft_timeout'), 0) AS ttft_timeout_count,
        COALESCE(SUM(sample_count) FILTER (WHERE outcome = 'other_failure'), 0) AS other_failure_count,
        COALESCE(SUM(ttft_sum_ms), 0) AS ttft_sum_ms,
        COALESCE(SUM(ttft_sample_count), 0) AS ttft_sample_count
    FROM first_token_timeout_stats_hourly
    WHERE bucket_start >= $1 AND bucket_start < $2
      AND scope = 'attempt'
      AND ($3 = '' OR protocol = $3)
      AND ($4 = '' OR model = $4)
      AND ($5 = '' OR platform = $5)
      AND ($6 = 0 OR account_id = $6)
    GROUP BY account_id, platform
), named AS (
    SELECT
        aggregated.*,
        CASE
            WHEN accounts.id IS NULL OR accounts.deleted_at IS NOT NULL
                THEN '#' || aggregated.account_id::text
            ELSE accounts.name
        END AS account_name
    FROM aggregated
    LEFT JOIN accounts ON accounts.id = aggregated.account_id
), filtered AS (
    SELECT
        named.*,
        CASE WHEN samples > 0 THEN ttft_timeout_count::double precision / samples ELSE 0 END AS ttft_timeout_rate,
        CASE WHEN samples > 0 THEN other_failure_count::double precision / samples ELSE 0 END AS other_failure_rate,
        CASE WHEN ttft_sample_count > 0 THEN ttft_sum_ms::double precision / ttft_sample_count ELSE 0 END AS avg_ttft_ms
    FROM named
    WHERE ($7 = '' OR account_name ILIKE $7 ESCAPE '\')
)`

	var total int64
	if err := scanSingleRow(ctx, tx, accountsCTE+`
SELECT COUNT(*) FROM filtered`, args, &total); err != nil {
		return nil, fmt.Errorf("count first token stats accounts: %w", err)
	}

	query := accountsCTE + fmt.Sprintf(`
SELECT
    account_id,
    account_name,
    platform,
    samples,
    success_count,
    ttft_timeout_count,
    other_failure_count,
    ttft_sum_ms,
    ttft_sample_count
FROM filtered
ORDER BY %s %s, account_id ASC
LIMIT $8 OFFSET $9`, sortColumn, sortOrder)
	queryArgs := append(args, int64(pageSize), offset)
	rows, err := tx.QueryContext(ctx, query, queryArgs...)
	if err != nil {
		return nil, fmt.Errorf("query first token stats accounts: %w", err)
	}
	items := make([]service.FirstTokenStatsAccount, 0)
	for rows.Next() {
		var (
			item            service.FirstTokenStatsAccount
			ttftSumMS       int64
			ttftSampleCount int64
		)
		if err := rows.Scan(
			&item.AccountID,
			&item.AccountName,
			&item.Platform,
			&item.Samples,
			&item.SuccessCount,
			&item.TTFTTimeoutCount,
			&item.OtherFailureCount,
			&ttftSumMS,
			&ttftSampleCount,
		); err != nil {
			_ = rows.Close()
			return nil, fmt.Errorf("scan first token stats account: %w", err)
		}
		item.TTFTTimeoutRate = firstTokenStatsRatio(item.TTFTTimeoutCount, item.Samples)
		item.OtherFailureRate = firstTokenStatsRatio(item.OtherFailureCount, item.Samples)
		if ttftSampleCount > 0 {
			item.AvgTTFTMS = float64(ttftSumMS) / float64(ttftSampleCount)
		}
		item.LowSample = item.Samples < 20
		items = append(items, item)
	}
	if err := rows.Err(); err != nil {
		_ = rows.Close()
		return nil, fmt.Errorf("iterate first token stats accounts: %w", err)
	}
	if err := rows.Close(); err != nil {
		return nil, fmt.Errorf("close first token stats accounts: %w", err)
	}

	pages := int(total) / pageSize
	if int(total)%pageSize != 0 {
		pages++
	}
	result := &service.FirstTokenStatsAccountPage{
		Items:    items,
		Total:    total,
		Page:     page,
		PageSize: pageSize,
		Pages:    pages,
	}
	if err := tx.Commit(); err != nil {
		return nil, fmt.Errorf("commit first token stats accounts snapshot: %w", err)
	}
	committed = true
	return result, nil
}

func normalizeFirstTokenStatsAccountFilter(filter service.FirstTokenStatsAccountFilter) (time.Time, time.Time, string, string, int, int, int64, error) {
	start, end, err := normalizeFirstTokenStatsOverviewFilter(service.FirstTokenStatsOverviewFilter{
		Range:    filter.Range,
		End:      filter.End,
		Protocol: filter.Protocol,
		Model:    filter.Model,
	})
	if err != nil {
		return time.Time{}, time.Time{}, "", "", 0, 0, 0, err
	}
	if utf8.RuneCountInString(filter.Platform) > 32 {
		return time.Time{}, time.Time{}, "", "", 0, 0, 0, fmt.Errorf("platform exceeds 32 characters")
	}
	if filter.AccountID < 0 {
		return time.Time{}, time.Time{}, "", "", 0, 0, 0, fmt.Errorf("account id must be non-negative")
	}
	if utf8.RuneCountInString(filter.Search) > 255 {
		return time.Time{}, time.Time{}, "", "", 0, 0, 0, fmt.Errorf("search exceeds 255 characters")
	}

	sortColumns := map[string]string{
		service.FirstTokenStatsAccountSortSamples:           "samples",
		service.FirstTokenStatsAccountSortSuccess:           "success_count",
		service.FirstTokenStatsAccountSortTTFTTimeoutCount:  "ttft_timeout_count",
		service.FirstTokenStatsAccountSortTTFTTimeoutRate:   "ttft_timeout_rate",
		service.FirstTokenStatsAccountSortOtherFailureCount: "other_failure_count",
		service.FirstTokenStatsAccountSortOtherFailureRate:  "other_failure_rate",
		service.FirstTokenStatsAccountSortAvgTTFTMS:         "avg_ttft_ms",
	}
	sortBy := filter.SortBy
	if sortBy == "" {
		sortBy = service.FirstTokenStatsAccountSortSamples
	}
	sortColumn, ok := sortColumns[sortBy]
	if !ok {
		return time.Time{}, time.Time{}, "", "", 0, 0, 0, fmt.Errorf("unsupported account stats sort %q", filter.SortBy)
	}
	sortOrder := strings.ToLower(filter.SortOrder)
	if sortOrder == "" {
		sortOrder = "desc"
	}
	if sortOrder != "asc" && sortOrder != "desc" {
		return time.Time{}, time.Time{}, "", "", 0, 0, 0, fmt.Errorf("unsupported account stats sort order %q", filter.SortOrder)
	}

	page := filter.Page
	if page <= 0 {
		page = 1
	}
	pageSize := filter.PageSize
	if pageSize <= 0 {
		pageSize = 20
	}
	if pageSize > 100 {
		pageSize = 100
	}
	pageIndex := int64(page - 1)
	if pageIndex > math.MaxInt64/int64(pageSize) {
		return time.Time{}, time.Time{}, "", "", 0, 0, 0, fmt.Errorf("account stats page offset exceeds int64 range")
	}
	offset := pageIndex * int64(pageSize)
	return start, end, sortColumn, strings.ToUpper(sortOrder), page, pageSize, offset, nil
}

func escapeFirstTokenStatsLike(value string) string {
	value = strings.ReplaceAll(value, `\`, `\\`)
	value = strings.ReplaceAll(value, `%`, `\%`)
	return strings.ReplaceAll(value, `_`, `\_`)
}

func (r *firstTokenTimeoutStatsRepository) DeleteBefore(ctx context.Context, cutoff time.Time) (int64, error) {
	if cutoff.IsZero() {
		return 0, fmt.Errorf("first token stats delete cutoff is required")
	}
	if r == nil || r.db == nil {
		return 0, fmt.Errorf("nil first token timeout stats repository")
	}
	cutoff = cutoff.UTC().Truncate(time.Hour)
	result, err := r.db.ExecContext(ctx, `DELETE FROM first_token_timeout_stats_hourly WHERE bucket_start < $1`, cutoff)
	if err != nil {
		return 0, fmt.Errorf("delete first token timeout stats before cutoff: %w", err)
	}
	deleted, err := result.RowsAffected()
	if err != nil {
		return 0, fmt.Errorf("read deleted first token timeout stats rows: %w", err)
	}
	return deleted, nil
}

func (r *firstTokenTimeoutStatsRepository) beginReadSnapshot(ctx context.Context) (*sql.Tx, error) {
	if r == nil || r.snapshotDB == nil {
		return nil, fmt.Errorf("first token timeout stats repository does not support read snapshots")
	}
	tx, err := r.snapshotDB.BeginTx(ctx, &sql.TxOptions{
		Isolation: sql.LevelRepeatableRead,
		ReadOnly:  true,
	})
	if err != nil {
		return nil, fmt.Errorf("begin first token stats read snapshot: %w", err)
	}
	return tx, nil
}
