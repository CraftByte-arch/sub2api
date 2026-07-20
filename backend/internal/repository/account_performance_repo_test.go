package repository

import (
	"context"
	"database/sql/driver"
	"testing"
	"time"

	"github.com/DATA-DOG/go-sqlmock"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/stretchr/testify/require"
)

func TestAccountPerformanceUpsertMinuteBatchWritesCumulativeHistograms(t *testing.T) {
	db, mock, err := sqlmock.New(sqlmock.QueryMatcherOption(sqlmock.QueryMatcherRegexp))
	require.NoError(t, err)
	defer func() { _ = db.Close() }()

	ttft, duration := int64(900), int64(2100)
	delta := service.AccountPerformanceDelta{
		BucketStart: time.Date(2026, 7, 17, 8, 17, 0, 0, time.FixedZone("CST", 8*60*60)),
		AccountID:   42, Platform: "openai", GroupID: 7, Model: "gpt-5", Protocol: "responses",
		Outcome: service.AccountPerformanceOutcomeSuccess, AttemptCount: 1, TTFTMS: &ttft, DurationMS: &duration, Failover: true,
	}
	mock.ExpectExec("INSERT INTO account_performance_minute .*ON CONFLICT \\(bucket_start, account_id, platform, group_id, model, protocol, outcome\\)").
		WithArgs(
			time.Date(2026, 7, 17, 0, 17, 0, 0, time.UTC), int64(42), "openai", int64(7), "gpt-5", "responses", service.AccountPerformanceOutcomeSuccess,
			int64(1), int64(1), int64(0), int64(0), int64(0), int64(0), int64(0), int64(0), int64(0), int64(0), int64(0), int64(1),
			int64(1), int64(900), int64(1), int64(2100),
			int64(1), int64(1), int64(1), int64(1), int64(1), int64(0),
			int64(0), int64(1), int64(1), int64(1), int64(1), int64(0),
		).
		WillReturnResult(sqlmock.NewResult(0, 1))

	err = NewAccountPerformanceRepository(db).UpsertMinuteBatch(context.Background(), []service.AccountPerformanceDelta{delta})
	require.NoError(t, err)
	require.NoError(t, mock.ExpectationsWereMet())
}

func TestAccountPerformanceUpsertMinuteBatchRejectsInvalidDeltaAndNilDB(t *testing.T) {
	valid := service.AccountPerformanceDelta{
		BucketStart: time.Now(), AccountID: 1, Platform: "openai", Model: "gpt-5", Protocol: "responses",
		Outcome: service.AccountPerformanceOutcomeSuccess, AttemptCount: 1,
	}
	invalid := valid
	invalid.AccountID = 0
	repo := &accountPerformanceRepository{}
	err := repo.UpsertMinuteBatch(context.Background(), []service.AccountPerformanceDelta{invalid})
	require.ErrorContains(t, err, "account id")
	err = repo.UpsertMinuteBatch(context.Background(), []service.AccountPerformanceDelta{valid})
	require.ErrorContains(t, err, "nil account performance repository")
}

func TestAccountPerformanceUpsertMinuteBatchRejectsMultipleAttemptsPerDelta(t *testing.T) {
	repo := &accountPerformanceRepository{}
	err := repo.UpsertMinuteBatch(context.Background(), []service.AccountPerformanceDelta{{
		BucketStart: time.Now(), AccountID: 1, Platform: "openai", Model: "gpt-5", Protocol: "responses",
		Outcome: service.AccountPerformanceOutcomeSuccess, AttemptCount: 2,
	}})
	require.ErrorContains(t, err, "exactly one")
}

func TestAccountPerformanceUpsertMinuteBatchRejectsInvalidDimensionsOutcomesAndTimestamps(t *testing.T) {
	valid := service.AccountPerformanceDelta{BucketStart: time.Now(), AccountID: 1, Platform: "openai", Model: "gpt-5", Protocol: "responses", Outcome: service.AccountPerformanceOutcomeSuccess, AttemptCount: 1}
	tests := []struct {
		name   string
		mutate func(*service.AccountPerformanceDelta)
		want   string
	}{
		{name: "missing timestamp", mutate: func(d *service.AccountPerformanceDelta) { d.BucketStart = time.Time{} }, want: "bucket start"},
		{name: "missing platform", mutate: func(d *service.AccountPerformanceDelta) { d.Platform = "" }, want: "platform is required"},
		{name: "missing protocol", mutate: func(d *service.AccountPerformanceDelta) { d.Protocol = "" }, want: "protocol is required"},
		{name: "nul model", mutate: func(d *service.AccountPerformanceDelta) { d.Model = "gpt\x005" }, want: "model contains NUL"},
		{name: "negative group", mutate: func(d *service.AccountPerformanceDelta) { d.GroupID = -1 }, want: "group id"},
		{name: "invalid outcome", mutate: func(d *service.AccountPerformanceDelta) { d.Outcome = "unknown" }, want: "invalid outcome"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			delta := valid
			tt.mutate(&delta)
			err := (&accountPerformanceRepository{}).UpsertMinuteBatch(context.Background(), []service.AccountPerformanceDelta{delta})
			require.ErrorContains(t, err, tt.want)
		})
	}
}

func TestAccountPerformanceUpsertMinuteBatchRejectsMoreThanOneThousandKeys(t *testing.T) {
	deltas := make([]service.AccountPerformanceDelta, maxAccountPerformanceUpsertKeys+1)
	for i := range deltas {
		deltas[i] = service.AccountPerformanceDelta{BucketStart: time.Date(2026, 7, 17, 8, 0, 0, 0, time.UTC), AccountID: int64(i + 1), Platform: "openai", Model: "gpt-5", Protocol: "responses", Outcome: service.AccountPerformanceOutcomeSuccess, AttemptCount: 1}
	}
	err := (&accountPerformanceRepository{}).UpsertMinuteBatch(context.Background(), deltas)
	require.ErrorContains(t, err, "unique key limit")
}

func TestAccountPerformanceQueryAccountsRejectsUnsafeSort(t *testing.T) {
	repo := &accountPerformanceRepository{}
	_, err := repo.QueryAccounts(context.Background(), service.AccountPerformanceAccountFilter{
		Start: time.Now().Add(-time.Hour), End: time.Now(), SortBy: "samples; DROP TABLE accounts",
	})
	require.ErrorContains(t, err, "unsupported account performance sort")
}

func TestAccountPerformanceQueryRangeAndPageValidation(t *testing.T) {
	now := time.Date(2026, 7, 17, 12, 0, 0, 0, time.UTC)
	previousNow := accountPerformanceNow
	accountPerformanceNow = func() time.Time { return now }
	t.Cleanup(func() { accountPerformanceNow = previousNow })
	recentStart := now.Add(-time.Hour)
	_, _, source, _, err := normalizeAccountPerformanceFilter(recentStart, recentStart.Add(time.Hour), "", 0, "", "", 0)
	require.NoError(t, err)
	require.Contains(t, source, "account_performance_minute")
	require.Contains(t, source, "account_performance_hourly")
	historicalStart := now.Add(-8 * 24 * time.Hour)
	_, _, source, _, err = normalizeAccountPerformanceFilter(historicalStart, historicalStart.Add(time.Hour), "", 0, "", "", 0)
	require.NoError(t, err)
	require.Contains(t, source, "account_performance_hourly")
	longStart := now.Add(-7 * 24 * time.Hour)
	_, _, source, _, err = normalizeAccountPerformanceFilter(longStart, now.Add(time.Nanosecond), "", 0, "", "", 0)
	require.NoError(t, err)
	require.Contains(t, source, "account_performance_minute")
	_, _, _, _, err = normalizeAccountPerformanceFilter(now.Add(-90*24*time.Hour), now.Add(time.Nanosecond), "", 0, "", "", 0)
	require.ErrorContains(t, err, "exceeds 90 days")

	for sortBy, want := range map[string]string{
		service.AccountPerformanceSortHealthScore: "health_score", service.AccountPerformanceSortAvailability: "availability", service.AccountPerformanceSortFailureRate: "failure_rate",
		service.AccountPerformanceSortP95TTFTMS: "p95_ttft_ms", service.AccountPerformanceSortP95DurationMS: "p95_duration_ms", service.AccountPerformanceSortSamples: "samples",
		service.AccountPerformanceSortSuccessCount: "success_count", service.AccountPerformanceSortFailureCount: "failure_count",
	} {
		got, _, _, _, _, err := normalizeAccountPerformancePage(service.AccountPerformanceAccountFilter{SortBy: sortBy, SortOrder: "asc", Page: 1, PageSize: 1})
		require.NoError(t, err)
		require.Equal(t, want, got)
	}
	for _, filter := range []service.AccountPerformanceAccountFilter{
		{SortBy: "unknown", Page: 1, PageSize: 1}, {SortOrder: "sideways", Page: 1, PageSize: 1}, {Page: 0, PageSize: 1}, {Page: 1, PageSize: 0}, {Page: 1, PageSize: 101},
	} {
		_, _, _, _, _, err := normalizeAccountPerformancePage(filter)
		require.Error(t, err)
	}
}

func TestAccountPerformanceDeleteBeforeReturnsAffectedCounts(t *testing.T) {
	db, mock, err := sqlmock.New(sqlmock.QueryMatcherOption(sqlmock.QueryMatcherRegexp))
	require.NoError(t, err)
	defer func() { _ = db.Close() }()
	previousNow := accountPerformanceNow
	accountPerformanceNow = func() time.Time { return time.Date(2026, 7, 17, 9, 0, 0, 0, time.UTC) }
	t.Cleanup(func() { accountPerformanceNow = previousNow })
	minuteCutoff := time.Date(2026, 7, 17, 8, 12, 0, 0, time.UTC)
	hourlyCutoff := time.Date(2026, 7, 17, 8, 12, 0, 0, time.UTC)
	mock.ExpectExec("DELETE FROM account_performance_minute WHERE bucket_start < \\$1").WithArgs(minuteCutoff).WillReturnResult(sqlmock.NewResult(0, 3))
	mock.ExpectExec("DELETE FROM account_performance_hourly WHERE bucket_start < \\$1").WithArgs(hourlyCutoff.Truncate(time.Hour)).WillReturnResult(sqlmock.NewResult(0, 4))
	repo := NewAccountPerformanceRepository(db)
	deleted, err := repo.DeleteMinuteBefore(context.Background(), minuteCutoff)
	require.NoError(t, err)
	require.EqualValues(t, 3, deleted)
	deleted, err = repo.DeleteHourlyBefore(context.Background(), hourlyCutoff)
	require.NoError(t, err)
	require.EqualValues(t, 4, deleted)
	require.NoError(t, mock.ExpectationsWereMet())
}

func TestAccountPerformanceRollupCutoffCapsFutureBeforeAtCurrentHour(t *testing.T) {
	now := time.Date(2026, 7, 17, 10, 37, 0, 0, time.UTC)
	cutoff := accountPerformanceRollupCutoff(now.Add(48*time.Hour), now)
	require.Equal(t, time.Date(2026, 7, 17, 10, 0, 0, 0, time.UTC), cutoff)
	require.Equal(t, time.Date(2026, 7, 17, 8, 0, 0, 0, time.UTC), accountPerformanceRollupCutoff(time.Date(2026, 7, 17, 8, 42, 0, 0, time.UTC), now))
}

func TestAccountPerformanceQueryAccountsUsesBoundedArgsAndAllowlistedSort(t *testing.T) {
	db, mock, err := sqlmock.New(sqlmock.QueryMatcherOption(sqlmock.QueryMatcherRegexp))
	require.NoError(t, err)
	defer func() { _ = db.Close() }()
	start := time.Date(2026, 7, 16, 0, 0, 0, 0, time.UTC)
	end := start.Add(48 * time.Hour)
	args := append(accountPerformanceMockArgs(t, start, end, "openai", int64(7), "gpt-5", "responses", int64(42), int64(10), int64(0)), "")
	mock.ExpectBegin()
	mock.ExpectQuery("(?s)account_performance_minute.*COALESCE\\(success_count::double precision / NULLIF\\(attempt_count - client_canceled_count, 0\\), 0\\) AS availability.*LEFT JOIN accounts AS account ON account.id = scored.account_id.*LEFT JOIN accounts AS parent ON parent.id = account.parent_account_id").
		WithArgs(args...).
		WillReturnRows(sqlmock.NewRows([]string{
			"account_id", "platform", "account_name", "account_type", "auth_mode", "attempt_count", "success_count", "client_canceled_count", "ttft_timeout_count", "rate_limit_count", "auth_count", "upstream_4xx_count", "upstream_5xx_count", "transport_count", "protocol_count", "other_failure_count", "failover_count", "ttft_sample_count", "ttft_sum_ms", "duration_sample_count", "duration_sum_ms", "ttft_le_1000_ms", "ttft_le_2500_ms", "ttft_le_5000_ms", "ttft_le_10000_ms", "ttft_le_30000_ms", "ttft_gt_30000_ms", "duration_le_1000_ms", "duration_le_2500_ms", "duration_le_5000_ms", "duration_le_10000_ms", "duration_le_30000_ms", "duration_gt_30000_ms",
			"availability", "failure_rate", "health_score", "total",
		}).AddRow(int64(42), "openai", "Codex Team", "oauth", "personalAccessToken", int64(10), int64(1), int64(9), int64(0), int64(0), int64(0), int64(0), int64(0), int64(0), int64(0), int64(0), int64(0), int64(1), int64(900), int64(1), int64(1400), int64(1), int64(1), int64(1), int64(1), int64(1), int64(0), int64(0), int64(1), int64(1), int64(1), int64(1), int64(0), float64(1), float64(0), float64(1), int64(1)))
	mock.ExpectCommit()

	page, err := NewAccountPerformanceRepository(db).QueryAccounts(context.Background(), service.AccountPerformanceAccountFilter{
		Start: start, End: end, Platform: "openai", GroupID: 7, Model: "gpt-5", Protocol: "responses", AccountID: 42,
		SortBy: service.AccountPerformanceSortHealthScore, SortOrder: "asc", Page: 1, PageSize: 10,
	})
	require.NoError(t, err)
	require.Len(t, page.Rows, 1)
	require.EqualValues(t, 42, page.Rows[0].AccountID)
	require.Equal(t, "Codex Team", page.Rows[0].AccountName)
	require.Equal(t, "oauth", page.Rows[0].AccountType)
	require.Equal(t, "personalAccessToken", page.Rows[0].AuthMode)
	require.EqualValues(t, 10, page.Rows[0].Counters.AttemptCount)
	require.EqualValues(t, 1, page.Rows[0].Counters.TTFTLatency.Samples)
	require.EqualValues(t, 1, page.Rows[0].Counters.DurationLatency.LE30000MS)
	require.Equal(t, 1.0, page.Rows[0].Availability)
	require.Equal(t, 0.0, page.Rows[0].FailureRate)
	require.Equal(t, 1.0, page.Rows[0].HealthScore)
	require.EqualValues(t, 1, page.Total)
	require.Equal(t, 1, page.Pages)
	require.NoError(t, mock.ExpectationsWereMet())
}

func TestAccountPerformanceQueryOverviewScansAggregateAndHistogramValues(t *testing.T) {
	db, mock, err := sqlmock.New(sqlmock.QueryMatcherOption(sqlmock.QueryMatcherRegexp))
	require.NoError(t, err)
	defer func() { _ = db.Close() }()
	start := time.Date(2026, 7, 16, 0, 0, 0, 0, time.UTC)
	end := start.Add(time.Hour)
	args := accountPerformanceMockArgs(t, start, end, "", int64(0), "", "", int64(0))
	mock.ExpectBegin()
	mock.ExpectQuery("(?s)SELECT COALESCE\\(SUM\\(attempt_count\\), 0\\) AS attempt_count, .*account_performance_minute").
		WithArgs(args...).WillReturnRows(sqlmock.NewRows(accountPerformanceMetricColumns).AddRow(accountPerformanceMetricsValues()...))
	pointColumns := append([]string{"bucket_start"}, accountPerformanceMetricColumns...)
	mock.ExpectQuery("(?s)SELECT bucket_start, COALESCE\\(SUM\\(attempt_count\\), 0\\) AS attempt_count, .*account_performance_minute").
		WithArgs(args...).WillReturnRows(sqlmock.NewRows(pointColumns).AddRow(append([]driver.Value{start}, accountPerformanceMetricsValues()...)...))
	mock.ExpectCommit()

	overview, err := NewAccountPerformanceRepository(db).QueryOverview(context.Background(), service.AccountPerformanceOverviewFilter{Start: start, End: end})
	require.NoError(t, err)
	require.EqualValues(t, 5, overview.Counters.AttemptCount)
	require.EqualValues(t, 2, overview.Counters.TTFTLatency.Samples)
	require.EqualValues(t, 2, overview.Counters.TTFTLatency.LE5000MS)
	require.EqualValues(t, 2, overview.Counters.DurationLatency.LE30000MS)
	require.Len(t, overview.TimePoints, 1)
	require.EqualValues(t, 5, overview.TimePoints[0].Counters.AttemptCount)
	require.NoError(t, mock.ExpectationsWereMet())
}

func TestAccountPerformanceQueryInvestigationScansFailureBreakdownInCountOrder(t *testing.T) {
	db, mock, err := sqlmock.New(sqlmock.QueryMatcherOption(sqlmock.QueryMatcherRegexp))
	require.NoError(t, err)
	defer func() { _ = db.Close() }()
	start := time.Date(2026, 7, 16, 0, 0, 0, 0, time.UTC)
	end := start.Add(2 * time.Hour)
	args := accountPerformanceMockArgs(t, start, end, "openai", int64(7), "gpt-5", "responses", int64(42))
	pointColumns := append([]string{"bucket_start"}, accountPerformanceMetricColumns...)
	mock.ExpectBegin()
	mock.ExpectQuery("(?s)SELECT bucket_start, .*account_performance_minute").
		WithArgs(args...).
		WillReturnRows(sqlmock.NewRows(pointColumns).AddRow(append([]driver.Value{start}, accountPerformanceMetricsValues()...)...))
	mock.ExpectQuery("SELECT outcome, COALESCE\\(SUM\\(attempt_count\\), 0\\) AS count .*ORDER BY count DESC, outcome ASC").
		WithArgs(args...).
		WillReturnRows(sqlmock.NewRows([]string{"outcome", "count"}).
			AddRow(service.AccountPerformanceOutcomeRateLimit, int64(3)).
			AddRow(service.AccountPerformanceOutcomeTransport, int64(1)))
	mock.ExpectCommit()

	result, err := NewAccountPerformanceRepository(db).QueryInvestigation(context.Background(), service.AccountPerformanceInvestigationFilter{
		Start: start, End: end, Platform: "openai", GroupID: 7, Model: "gpt-5", Protocol: "responses", AccountID: 42,
	})
	require.NoError(t, err)
	require.Equal(t, []service.AccountPerformanceFailureBreakdown{
		{Outcome: service.AccountPerformanceOutcomeRateLimit, Count: 3},
		{Outcome: service.AccountPerformanceOutcomeTransport, Count: 1},
	}, result.Failures)
	require.Len(t, result.TimePoints, 1)
	require.EqualValues(t, 2, result.TimePoints[0].Counters.DurationLatency.Samples)
	require.EqualValues(t, 2, result.TimePoints[0].Counters.TTFTLatency.LE2500MS)
	require.NoError(t, mock.ExpectationsWereMet())
}

func accountPerformanceMockArgs(t *testing.T, start, end time.Time, platform string, groupID int64, model, protocol string, accountID int64, extra ...int64) []driver.Value {
	t.Helper()
	_, _, _, args, err := normalizeAccountPerformanceFilter(start, end, platform, groupID, model, protocol, accountID)
	require.NoError(t, err)
	result := make([]driver.Value, 0, len(args)+len(extra))
	for _, arg := range args {
		value, ok := arg.(driver.Value)
		require.Truef(t, ok, "unexpected query argument type %T", arg)
		result = append(result, value)
	}
	for _, value := range extra {
		result = append(result, value)
	}
	return result
}

func TestAccountPerformanceRollupClosedHoursReplacesHourlyRowsInTransaction(t *testing.T) {
	db, mock, err := sqlmock.New(sqlmock.QueryMatcherOption(sqlmock.QueryMatcherRegexp))
	require.NoError(t, err)
	defer func() { _ = db.Close() }()
	previousNow := accountPerformanceNow
	accountPerformanceNow = func() time.Time { return time.Date(2026, 7, 17, 11, 0, 0, 0, time.UTC) }
	t.Cleanup(func() { accountPerformanceNow = previousNow })
	before := time.Date(2026, 7, 17, 10, 45, 0, 0, time.UTC)
	closed := time.Date(2026, 7, 17, 10, 0, 0, 0, time.UTC)
	mock.ExpectQuery("SELECT EXISTS\\(SELECT 1 FROM account_performance_minute WHERE bucket_start < \\$1\\)").WithArgs(closed).WillReturnRows(sqlmock.NewRows([]string{"exists"}).AddRow(true))
	mock.ExpectBegin()
	mock.ExpectExec("DELETE FROM account_performance_hourly").WithArgs(closed).WillReturnResult(sqlmock.NewResult(0, 2))
	mock.ExpectExec("INSERT INTO account_performance_hourly").WithArgs(closed).WillReturnResult(sqlmock.NewResult(0, 2))
	mock.ExpectCommit()

	err = NewAccountPerformanceRepository(db).RollupClosedHours(context.Background(), before)
	require.NoError(t, err)
	require.NoError(t, mock.ExpectationsWereMet())
}

func TestAccountPerformanceRollupClosedHoursCapsFutureCutoffBeforeWriting(t *testing.T) {
	db, mock, err := sqlmock.New(sqlmock.QueryMatcherOption(sqlmock.QueryMatcherRegexp))
	require.NoError(t, err)
	defer func() { _ = db.Close() }()
	previousNow := accountPerformanceNow
	accountPerformanceNow = func() time.Time { return time.Date(2026, 7, 17, 10, 37, 0, 0, time.UTC) }
	t.Cleanup(func() { accountPerformanceNow = previousNow })
	cutoff := time.Date(2026, 7, 17, 10, 0, 0, 0, time.UTC)
	mock.ExpectQuery("SELECT EXISTS\\(SELECT 1 FROM account_performance_minute WHERE bucket_start < \\$1\\)").WithArgs(cutoff).WillReturnRows(sqlmock.NewRows([]string{"exists"}).AddRow(true))
	mock.ExpectBegin()
	mock.ExpectExec("DELETE FROM account_performance_hourly").WithArgs(cutoff).WillReturnResult(sqlmock.NewResult(0, 1))
	mock.ExpectExec("INSERT INTO account_performance_hourly").WithArgs(cutoff).WillReturnResult(sqlmock.NewResult(0, 1))
	mock.ExpectCommit()

	require.NoError(t, NewAccountPerformanceRepository(db).RollupClosedHours(context.Background(), time.Date(2026, 7, 20, 1, 0, 0, 0, time.UTC)))
	require.NoError(t, mock.ExpectationsWereMet())
}

func TestAccountPerformanceRollupClosedHoursSkipsEmptyClosedWindow(t *testing.T) {
	db, mock, err := sqlmock.New(sqlmock.QueryMatcherOption(sqlmock.QueryMatcherRegexp))
	require.NoError(t, err)
	defer func() { _ = db.Close() }()
	previousNow := accountPerformanceNow
	accountPerformanceNow = func() time.Time { return time.Date(2026, 7, 17, 10, 37, 0, 0, time.UTC) }
	t.Cleanup(func() { accountPerformanceNow = previousNow })
	cutoff := time.Date(2026, 7, 17, 10, 0, 0, 0, time.UTC)
	mock.ExpectQuery("SELECT EXISTS\\(SELECT 1 FROM account_performance_minute WHERE bucket_start < \\$1\\)").WithArgs(cutoff).WillReturnRows(sqlmock.NewRows([]string{"exists"}).AddRow(false))

	require.NoError(t, NewAccountPerformanceRepository(db).RollupClosedHours(context.Background(), time.Date(2026, 7, 18, 10, 0, 0, 0, time.UTC)))
	require.NoError(t, mock.ExpectationsWereMet())
}

func TestQueryAccountsAppliesSearchFilter(t *testing.T) {
	db, mock, err := sqlmock.New(sqlmock.QueryMatcherOption(sqlmock.QueryMatcherRegexp))
	require.NoError(t, err)
	defer func() { _ = db.Close() }()

	repo := NewAccountPerformanceRepository(db)
	mock.ExpectBegin()
	mock.ExpectQuery("FROM enriched WHERE \\(\\$20::text = '' OR account_name ILIKE \\$20").
		WithArgs(sqlmock.AnyArg(), sqlmock.AnyArg(), sqlmock.AnyArg(), sqlmock.AnyArg(), sqlmock.AnyArg(), sqlmock.AnyArg(), sqlmock.AnyArg(), sqlmock.AnyArg(), sqlmock.AnyArg(), sqlmock.AnyArg(), sqlmock.AnyArg(), sqlmock.AnyArg(), sqlmock.AnyArg(), sqlmock.AnyArg(), sqlmock.AnyArg(), sqlmock.AnyArg(), sqlmock.AnyArg(), int64(20), int64(0), "%prod%").
		WillReturnRows(sqlmock.NewRows([]string{"account_id"}))
	mock.ExpectCommit()

	_, err = repo.QueryAccounts(context.Background(), service.AccountPerformanceAccountFilter{
		Start: time.Now().Add(-time.Hour), End: time.Now(), Page: 1, PageSize: 20, Search: "prod", SortBy: "health_score", SortOrder: "asc",
	})
	require.NoError(t, err)
	require.NoError(t, mock.ExpectationsWereMet())
}

func accountPerformanceMetricsValues() []driver.Value {
	return []driver.Value{
		int64(5), int64(3), int64(0), int64(1), int64(1), int64(0), int64(0), int64(0), int64(0), int64(0), int64(0), int64(2),
		int64(2), int64(1500), int64(2), int64(3500),
		int64(1), int64(2), int64(2), int64(2), int64(2), int64(0),
		int64(0), int64(1), int64(2), int64(2), int64(2), int64(0),
	}
}
