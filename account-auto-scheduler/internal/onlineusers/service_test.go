package onlineusers

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"regexp"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/DATA-DOG/go-sqlmock"
)

func TestAggregateQueriesUseDistinctPresenceAndNeverRawLogs(t *testing.T) {
	for name, query := range map[string]string{"summary": summaryQuery, "detail": detailQuery} {
		t.Run(name, func(t *testing.T) {
			for _, required := range []string{
				"watermark AS MATERIALIZED",
				"channel_monitor_v2_watermarks",
				"channel_monitor_v2_user_metrics_1m",
				"success_requests > 0 OR m.error_requests > 0",
				"m.bucket_start >= (SELECT data_through - INTERVAL '10 minutes' FROM watermark)",
				"m.bucket_start < (SELECT data_through FROM watermark)",
			} {
				if !strings.Contains(query, required) {
					t.Fatalf("query is missing %q:\n%s", required, query)
				}
			}
			for _, forbidden := range []string{"usage_logs", "ops_error_logs", "admin/ops", "admin/usage"} {
				if strings.Contains(strings.ToLower(query), forbidden) {
					t.Fatalf("query unexpectedly references raw source %q:\n%s", forbidden, query)
				}
			}
		})
	}
	if !strings.Contains(summaryQuery, "COUNT(DISTINCT user_id)") || !strings.Contains(summaryQuery, "COUNT(DISTINCT user_id)::BIGINT") {
		t.Fatalf("summary query does not preserve distinct global/group counting:\n%s", summaryQuery)
	}
	if !strings.Contains(detailQuery, "JOIN user_presence active ON active.user_id = d.user_id") {
		t.Fatalf("detail daily aggregate is not restricted to active users:\n%s", detailQuery)
	}
}

func TestSummaryReadsDistinctAggregateCounts(t *testing.T) {
	now := time.Date(2026, 8, 31, 9, 0, 0, 0, time.UTC)
	through := now.Add(-time.Minute)
	service, mock, closeDB := newMockService(t, now)
	defer closeDB()

	mock.ExpectQuery(regexp.QuoteMeta(summaryQuery)).WillReturnRows(
		sqlmock.NewRows([]string{"data_through", "global_count", "group_id", "group_count"}).
			AddRow(through, int64(3), int64(10), int64(2)).
			AddRow(through, int64(3), int64(20), int64(1)),
	)

	got, err := service.GetOnlineUsersSummary(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if !got.Ready || got.Stale || got.Count != 3 || got.GroupCounts[10] != 2 || got.GroupCounts[20] != 1 || got.AggregationLagSeconds != 60 {
		t.Fatalf("unexpected summary: %#v", got)
	}
	if got.DataThrough == nil || !got.DataThrough.Equal(through) || !got.GroupCountsAvailable {
		t.Fatalf("summary metadata missing: %#v", got)
	}
	assertExpectations(t, mock)
}

func TestDetailJoinsUsersDailyUsageAndGroups(t *testing.T) {
	now := time.Date(2026, 8, 31, 9, 0, 0, 0, time.UTC)
	through := now.Add(-time.Minute)
	service, mock, closeDB := newMockService(t, now)
	defer closeDB()

	mock.ExpectQuery(regexp.QuoteMeta(detailQuery)).WillReturnRows(
		sqlmock.NewRows(detailColumns()).
			AddRow(through, true, int64(1), int64(10), now.Add(-2*time.Minute), "one@example.com", "one", int64(4), int64(1200), 1.25).
			AddRow(through, true, int64(1), int64(20), now.Add(-2*time.Minute), "one@example.com", "one", int64(4), int64(1200), 1.25).
			AddRow(through, true, int64(2), int64(10), now.Add(-3*time.Minute), "two@example.com", "", int64(0), int64(0), 0.0),
	)

	got, err := service.GetOnlineUsers(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if !got.Ready || got.Partial || got.Count != 2 || got.GroupCounts[10] != 2 || got.GroupCounts[20] != 1 || len(got.Users) != 2 {
		t.Fatalf("unexpected detail snapshot: %#v", got)
	}
	if got.Users[0].ID != 1 || got.Users[0].DisplayName != "one" || len(got.Users[0].GroupIDs) != 2 || got.Users[0].GroupIDs[0] != 10 || got.Users[0].GroupIDs[1] != 20 {
		t.Fatalf("unexpected first user: %#v", got.Users[0])
	}
	if got.Users[0].TodayCost == nil || *got.Users[0].TodayCost != 1.25 || got.Users[0].TodayRequests == nil || *got.Users[0].TodayRequests != 4 {
		t.Fatalf("daily usage missing: %#v", got.Users[0])
	}
	if got.Users[1].DisplayName != "two@example.com" || got.Users[1].TodayCost == nil || *got.Users[1].TodayCost != 0 {
		t.Fatalf("zero usage or identity not preserved: %#v", got.Users[1])
	}
	assertExpectations(t, mock)
}

func TestDetailKeepsPresenceWhenDailyAggregateIsNotReady(t *testing.T) {
	now := time.Date(2026, 8, 31, 9, 0, 0, 0, time.UTC)
	service, mock, closeDB := newMockService(t, now)
	defer closeDB()

	mock.ExpectQuery(regexp.QuoteMeta(detailQuery)).WillReturnRows(
		sqlmock.NewRows(detailColumns()).
			AddRow(now.Add(-time.Minute), false, int64(7), int64(30), now.Add(-2*time.Minute), "seven@example.com", "seven", nil, nil, nil),
	)

	got, err := service.GetOnlineUsers(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if !got.Ready || !got.Partial || got.Count != 1 || len(got.Users) != 1 || got.Users[0].TodayCost != nil || got.Users[0].TodayTokens != nil || got.Users[0].TodayRequests != nil {
		t.Fatalf("daily readiness handling failed: %#v", got)
	}
	if got.Notice == "" {
		t.Fatalf("missing partial notice: %#v", got)
	}
	assertExpectations(t, mock)
}

func TestSummaryCacheIsDeepCopiedAndAvoidsDuplicateQueries(t *testing.T) {
	now := time.Date(2026, 8, 31, 9, 0, 0, 0, time.UTC)
	service, mock, closeDB := newMockService(t, now)
	defer closeDB()
	mock.ExpectQuery(regexp.QuoteMeta(summaryQuery)).WillReturnRows(
		sqlmock.NewRows([]string{"data_through", "global_count", "group_id", "group_count"}).
			AddRow(now.Add(-time.Minute), int64(1), int64(10), int64(1)),
	)

	first, err := service.GetOnlineUsersSummary(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	first.GroupCounts[10] = 999
	second, err := service.GetOnlineUsersSummary(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if second.GroupCounts[10] != 1 {
		t.Fatalf("cached map was mutated by caller: %#v", second.GroupCounts)
	}
	assertExpectations(t, mock)
}

func TestDetailCacheIsDeepCopiedAndAvoidsDuplicateQueries(t *testing.T) {
	now := time.Date(2026, 8, 31, 9, 0, 0, 0, time.UTC)
	service, mock, closeDB := newMockService(t, now)
	defer closeDB()
	mock.ExpectQuery(regexp.QuoteMeta(detailQuery)).WillReturnRows(
		sqlmock.NewRows(detailColumns()).
			AddRow(now.Add(-time.Minute), true, int64(1), int64(10), now.Add(-2*time.Minute), "one@example.com", "one", int64(4), int64(1200), 1.25),
	)

	first, err := service.GetOnlineUsers(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	first.GroupCounts[10] = 999
	first.Users[0].GroupIDs[0] = 999
	*first.Users[0].TodayCost = 999

	second, err := service.GetOnlineUsers(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if second.GroupCounts[10] != 1 || second.Users[0].GroupIDs[0] != 10 || second.Users[0].TodayCost == nil || *second.Users[0].TodayCost != 1.25 {
		t.Fatalf("detail cache was mutated by caller: %#v", second)
	}
	assertExpectations(t, mock)
}

func TestConcurrentSummaryMissesShareOneQuery(t *testing.T) {
	now := time.Date(2026, 8, 31, 9, 0, 0, 0, time.UTC)
	service, mock, closeDB := newMockService(t, now)
	defer closeDB()
	mock.ExpectQuery(regexp.QuoteMeta(summaryQuery)).WillDelayFor(75 * time.Millisecond).WillReturnRows(
		sqlmock.NewRows([]string{"data_through", "global_count", "group_id", "group_count"}).
			AddRow(now.Add(-time.Minute), int64(2), int64(10), int64(2)),
	)

	const callers = 12
	var wg sync.WaitGroup
	errs := make(chan error, callers)
	for range callers {
		wg.Add(1)
		go func() {
			defer wg.Done()
			got, err := service.GetOnlineUsersSummary(context.Background())
			if err == nil && got.Count != 2 {
				err = errors.New("unexpected shared result")
			}
			errs <- err
		}()
	}
	wg.Wait()
	close(errs)
	for err := range errs {
		if err != nil {
			t.Fatal(err)
		}
	}
	assertExpectations(t, mock)
}

func TestFreshnessMarksStaleAndThenUnavailable(t *testing.T) {
	now := time.Date(2026, 8, 31, 9, 0, 0, 0, time.UTC)

	t.Run("stale", func(t *testing.T) {
		service, mock, closeDB := newMockService(t, now)
		defer closeDB()
		mock.ExpectQuery(regexp.QuoteMeta(summaryQuery)).WillReturnRows(
			sqlmock.NewRows([]string{"data_through", "global_count", "group_id", "group_count"}).
				AddRow(now.Add(-4*time.Minute), int64(2), int64(10), int64(2)),
		)
		got, _ := service.GetOnlineUsersSummary(context.Background())
		if !got.Ready || !got.Stale || !got.Partial || !got.GroupCountsPartial || got.Count != 2 {
			t.Fatalf("stale result was not preserved: %#v", got)
		}
		assertExpectations(t, mock)
	})

	t.Run("unavailable", func(t *testing.T) {
		service, mock, closeDB := newMockService(t, now)
		defer closeDB()
		mock.ExpectQuery(regexp.QuoteMeta(summaryQuery)).WillReturnRows(
			sqlmock.NewRows([]string{"data_through", "global_count", "group_id", "group_count"}).
				AddRow(now.Add(-11*time.Minute), int64(2), int64(10), int64(2)),
		)
		got, _ := service.GetOnlineUsersSummary(context.Background())
		if got.Ready || !got.Stale || got.Count != 0 || got.GroupCountsAvailable || len(got.GroupCounts) != 0 {
			t.Fatalf("expired result was presented as current: %#v", got)
		}
		assertExpectations(t, mock)
	})
}

func TestQueryFailureUsesLastGoodWithoutRawFallback(t *testing.T) {
	now := time.Date(2026, 8, 31, 9, 0, 0, 0, time.UTC)
	service, mock, closeDB := newMockService(t, now)
	defer closeDB()
	service.cacheTTL = time.Second

	mock.ExpectQuery(regexp.QuoteMeta(summaryQuery)).WillReturnRows(
		sqlmock.NewRows([]string{"data_through", "global_count", "group_id", "group_count"}).
			AddRow(now.Add(-time.Minute), int64(1), int64(10), int64(1)),
	)
	first, _ := service.GetOnlineUsersSummary(context.Background())
	if !first.Ready {
		t.Fatalf("initial result not ready: %#v", first)
	}

	now = now.Add(2 * time.Second)
	service.now = func() time.Time { return now }
	mock.ExpectQuery(regexp.QuoteMeta(summaryQuery)).WillReturnError(errors.New("database offline"))
	second, err := service.GetOnlineUsersSummary(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if !second.Ready || !second.Stale || !second.Partial || second.Count != 1 || second.Notice == "" {
		t.Fatalf("last-good fallback missing: %#v", second)
	}
	assertExpectations(t, mock)
}

func TestQueryFailureWithoutLastGoodReportsUnavailable(t *testing.T) {
	now := time.Date(2026, 8, 31, 9, 0, 0, 0, time.UTC)
	service, mock, closeDB := newMockService(t, now)
	defer closeDB()
	mock.ExpectQuery(regexp.QuoteMeta(summaryQuery)).WillReturnError(errors.New("database offline"))

	got, err := service.GetOnlineUsersSummary(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if got.Ready || got.Count != 0 || got.GroupCountsAvailable || got.Notice == "" {
		t.Fatalf("query failure was presented as a measured zero: %#v", got)
	}
	assertExpectations(t, mock)
}

func TestLastGoodBecomesUnavailableWhenItsWatermarkIsTooOld(t *testing.T) {
	now := time.Date(2026, 8, 31, 9, 0, 0, 0, time.UTC)
	service, mock, closeDB := newMockService(t, now)
	defer closeDB()
	service.cacheTTL = time.Second
	mock.ExpectQuery(regexp.QuoteMeta(summaryQuery)).WillReturnRows(
		sqlmock.NewRows([]string{"data_through", "global_count", "group_id", "group_count"}).
			AddRow(now.Add(-time.Minute), int64(1), int64(10), int64(1)),
	)
	if got, _ := service.GetOnlineUsersSummary(context.Background()); !got.Ready {
		t.Fatalf("initial result not ready: %#v", got)
	}

	now = now.Add(10*time.Minute + 2*time.Second)
	service.now = func() time.Time { return now }
	mock.ExpectQuery(regexp.QuoteMeta(summaryQuery)).WillReturnError(errors.New("database offline"))
	got, err := service.GetOnlineUsersSummary(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if got.Ready || got.Count != 0 || got.GroupCountsAvailable || len(got.GroupCounts) != 0 || !got.Stale {
		t.Fatalf("expired last-good result remained available: %#v", got)
	}
	assertExpectations(t, mock)
}

func TestAbsentDatabaseConfigurationOnlyDisablesOnlineFeature(t *testing.T) {
	service, err := Open("", discardLogger())
	if err != nil {
		t.Fatal(err)
	}
	if service.Configured() {
		t.Fatal("empty DSN unexpectedly configured a database")
	}
	summary, err := service.GetOnlineUsersSummary(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if summary.Ready || summary.GroupCountsAvailable || summary.Notice == "" {
		t.Fatalf("disabled summary is misleading: %#v", summary)
	}
	detail, err := service.GetOnlineUsers(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if detail.Ready || len(detail.Users) != 0 {
		t.Fatalf("disabled detail is misleading: %#v", detail)
	}
}

func newMockService(t *testing.T, now time.Time) (*Service, sqlmock.Sqlmock, func()) {
	t.Helper()
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatal(err)
	}
	service := newService(db, discardLogger())
	service.now = func() time.Time { return now }
	return service, mock, func() { _ = db.Close() }
}

func detailColumns() []string {
	return []string{
		"data_through",
		"daily_ready",
		"user_id",
		"group_id",
		"last_call_at",
		"email",
		"username",
		"today_requests",
		"today_tokens",
		"today_cost",
	}
}

func discardLogger() *slog.Logger {
	return slog.New(slog.NewTextHandler(io.Discard, nil))
}

func assertExpectations(t *testing.T, mock sqlmock.Sqlmock) {
	t.Helper()
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}
}
