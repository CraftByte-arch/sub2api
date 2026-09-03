package onlineusers

import (
	"context"
	"errors"
	"regexp"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/DATA-DOG/go-sqlmock"
	"github.com/Wei-Shaw/sub2api-account-auto-scheduler/internal/model"
)

func TestAccountSuccessQueriesUseOnlyBoundedPerformanceAggregates(t *testing.T) {
	for name, query := range map[string]string{"minute": accountSuccessMinuteQuery, "hourly": accountSuccessHourlyQuery} {
		t.Run(name, func(t *testing.T) {
			for _, required := range []string{"bucket_start >= $1", "bucket_start < $2", "group_id", "account_id", "client_canceled_count", "failover_count"} {
				if !strings.Contains(query, required) {
					t.Fatalf("query is missing %q:\n%s", required, query)
				}
			}
			if strings.Contains(strings.ToLower(query), "usage_logs") {
				t.Fatalf("query reads forbidden raw usage logs:\n%s", query)
			}
		})
	}
	if !strings.Contains(accountSuccessMinuteQuery, "account_performance_minute") || !strings.Contains(accountSuccessHourlyQuery, "account_performance_hourly") {
		t.Fatal("actual success queries do not use the compact performance tables")
	}
}

func TestAccountSuccessCalculatesCancellationAwareGroupRates(t *testing.T) {
	now := time.Date(2026, 9, 3, 12, 34, 45, 0, time.UTC)
	boundary := now.Truncate(time.Minute)
	service, mock, closeDB := newMockService(t, now)
	defer closeDB()

	mock.ExpectQuery(regexp.QuoteMeta(accountSuccessMinuteQuery)).
		WithArgs(boundary.Add(-time.Hour), boundary).
		WillReturnRows(sqlmock.NewRows(accountSuccessMinuteColumns()).
			AddRow(boundary.Add(-2*time.Minute), int64(1), int64(10), int64(8), int64(10), int64(2), int64(1)).
			AddRow(boundary.Add(-time.Minute), int64(2), int64(10), int64(3), int64(5), int64(1), int64(2)).
			AddRow(boundary.Add(-time.Minute), int64(1), int64(11), int64(1), int64(2), int64(0), int64(0)))
	hourlyThrough := now.Truncate(time.Hour)
	mock.ExpectQuery(regexp.QuoteMeta(accountSuccessHourlyQuery)).
		WithArgs(hourlyThrough.Add(-24*time.Hour), hourlyThrough).
		WillReturnRows(sqlmock.NewRows(accountSuccessHourlyColumns()).
			AddRow(int64(1), int64(10), int64(80), int64(100), int64(20), int64(3), hourlyThrough.Add(-time.Hour)).
			AddRow(int64(2), int64(10), int64(50), int64(100), int64(0), int64(4), hourlyThrough.Add(-time.Hour)))

	got, err := service.GetAccountSuccessRates(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if !got.Ready || got.Partial || got.Stale || got.ActiveAccountCount != 2 || len(got.Items) != 3 {
		t.Fatalf("unexpected snapshot: %#v", got)
	}
	first := findSuccessRate(t, got.Items, 1, 10)
	if first.Recent.SuccessCount != 8 || first.Recent.EffectiveAttempts != 8 || first.Recent.AttemptCount != 10 || first.Recent.ClientCanceledCount != 2 || first.Recent.Rate != 1 || !first.Recent.LowSample {
		t.Fatalf("cancellation-aware recent formula is wrong: %#v", first.Recent)
	}
	if first.Reference.SuccessCount != 80 || first.Reference.EffectiveAttempts != 80 || first.Reference.Rate != 1 || first.Reference.WindowMinutes != 24*60 {
		t.Fatalf("24-hour reference is wrong: %#v", first.Reference)
	}
	secondGroup := findSuccessRate(t, got.Items, 2, 10)
	if secondGroup.Recent.SuccessCount != 3 || secondGroup.Recent.EffectiveAttempts != 4 || secondGroup.Recent.Rate != 0.75 || secondGroup.Reference.Rate != 0.5 {
		t.Fatalf("group-account isolation is wrong: %#v", secondGroup)
	}
	if got.DataThrough == nil || !got.DataThrough.Equal(boundary) || got.ReferenceThrough == nil || !got.ReferenceThrough.Equal(hourlyThrough) {
		t.Fatalf("snapshot boundaries missing: %#v", got)
	}

	got.Items[0].Recent.SuccessCount = 999
	cached, err := service.GetAccountSuccessRates(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if findSuccessRate(t, cached.Items, 1, 10).Recent.SuccessCount != 8 {
		t.Fatal("caller mutated the cached success snapshot")
	}
	assertExpectations(t, mock)
}

func TestAccountSuccessIncrementalRefreshReplacesOverlapAndKeepsOlderBuckets(t *testing.T) {
	now := time.Date(2026, 9, 3, 10, 5, 30, 0, time.UTC)
	service, mock, closeDB := newMockService(t, now)
	defer closeDB()
	firstBoundary := now.Truncate(time.Minute)

	mock.ExpectQuery(regexp.QuoteMeta(accountSuccessMinuteQuery)).
		WithArgs(firstBoundary.Add(-time.Hour), firstBoundary).
		WillReturnRows(sqlmock.NewRows(accountSuccessMinuteColumns()).
			AddRow(firstBoundary.Add(-3*time.Minute), int64(1), int64(1), int64(1), int64(1), int64(0), int64(0)).
			AddRow(firstBoundary.Add(-2*time.Minute), int64(1), int64(1), int64(1), int64(2), int64(0), int64(0)).
			AddRow(firstBoundary.Add(-time.Minute), int64(1), int64(1), int64(1), int64(1), int64(0), int64(0)))
	mock.ExpectQuery(regexp.QuoteMeta(accountSuccessHourlyQuery)).
		WithArgs(now.Truncate(time.Hour).Add(-24*time.Hour), now.Truncate(time.Hour)).
		WillReturnRows(sqlmock.NewRows(accountSuccessHourlyColumns()))
	first, _ := service.GetAccountSuccessRates(context.Background())
	if recent := findSuccessRate(t, first.Items, 1, 1).Recent; recent.SuccessCount != 3 || recent.EffectiveAttempts != 4 {
		t.Fatalf("unexpected bootstrap counters: %#v", recent)
	}

	now = now.Add(2*time.Minute + 10*time.Second)
	service.now = func() time.Time { return now }
	secondBoundary := now.Truncate(time.Minute)
	overlapStart := firstBoundary.Add(-accountSuccessOverlap)
	mock.ExpectQuery(regexp.QuoteMeta(accountSuccessMinuteQuery)).
		WithArgs(overlapStart, secondBoundary).
		WillReturnRows(sqlmock.NewRows(accountSuccessMinuteColumns()).
			AddRow(firstBoundary.Add(-2*time.Minute), int64(1), int64(1), int64(2), int64(2), int64(0), int64(0)).
			AddRow(firstBoundary.Add(-time.Minute), int64(1), int64(1), int64(1), int64(1), int64(0), int64(0)).
			AddRow(firstBoundary, int64(1), int64(1), int64(0), int64(1), int64(0), int64(1)).
			AddRow(firstBoundary.Add(time.Minute), int64(1), int64(1), int64(1), int64(1), int64(0), int64(0)))
	second, _ := service.GetAccountSuccessRates(context.Background())
	recent := findSuccessRate(t, second.Items, 1, 1).Recent
	if recent.SuccessCount != 5 || recent.EffectiveAttempts != 6 || recent.FailoverCount != 1 {
		t.Fatalf("overlap was added instead of replaced, or older bucket was lost: %#v", recent)
	}
	assertExpectations(t, mock)
}

func TestAccountSuccessGapForcesBoundedBootstrapAndPrunesOldWindow(t *testing.T) {
	now := time.Date(2026, 9, 3, 10, 5, 30, 0, time.UTC)
	service, mock, closeDB := newMockService(t, now)
	defer closeDB()
	firstBoundary := now.Truncate(time.Minute)
	mock.ExpectQuery(regexp.QuoteMeta(accountSuccessMinuteQuery)).WithArgs(firstBoundary.Add(-time.Hour), firstBoundary).WillReturnRows(
		sqlmock.NewRows(accountSuccessMinuteColumns()).AddRow(firstBoundary.Add(-time.Minute), int64(1), int64(1), int64(1), int64(1), int64(0), int64(0)),
	)
	mock.ExpectQuery(regexp.QuoteMeta(accountSuccessHourlyQuery)).WithArgs(now.Truncate(time.Hour).Add(-24*time.Hour), now.Truncate(time.Hour)).WillReturnRows(sqlmock.NewRows(accountSuccessHourlyColumns()))
	if _, err := service.GetAccountSuccessRates(context.Background()); err != nil {
		t.Fatal(err)
	}

	now = now.Add(time.Hour + time.Minute)
	service.now = func() time.Time { return now }
	boundary := now.Truncate(time.Minute)
	windowStart := boundary.Add(-time.Hour)
	mock.ExpectQuery(regexp.QuoteMeta(accountSuccessMinuteQuery)).WithArgs(windowStart, boundary).WillReturnRows(
		sqlmock.NewRows(accountSuccessMinuteColumns()).AddRow(boundary.Add(-time.Minute), int64(2), int64(2), int64(4), int64(5), int64(1), int64(0)),
	)
	mock.ExpectQuery(regexp.QuoteMeta(accountSuccessHourlyQuery)).WithArgs(now.Truncate(time.Hour).Add(-24*time.Hour), now.Truncate(time.Hour)).WillReturnRows(sqlmock.NewRows(accountSuccessHourlyColumns()))
	got, _ := service.GetAccountSuccessRates(context.Background())
	if len(got.Items) != 1 || got.Items[0].GroupID != 2 || got.Items[0].AccountID != 2 {
		t.Fatalf("old rolling-window buckets survived a cold bootstrap: %#v", got.Items)
	}
	assertExpectations(t, mock)
}

func TestConcurrentAccountSuccessMissesShareOneRefresh(t *testing.T) {
	now := time.Date(2026, 9, 3, 9, 0, 10, 0, time.UTC)
	service, mock, closeDB := newMockService(t, now)
	defer closeDB()
	boundary := now.Truncate(time.Minute)
	mock.ExpectQuery(regexp.QuoteMeta(accountSuccessMinuteQuery)).
		WithArgs(boundary.Add(-time.Hour), boundary).
		WillDelayFor(75 * time.Millisecond).
		WillReturnRows(sqlmock.NewRows(accountSuccessMinuteColumns()).AddRow(boundary.Add(-time.Minute), int64(1), int64(1), int64(20), int64(20), int64(0), int64(0)))
	mock.ExpectQuery(regexp.QuoteMeta(accountSuccessHourlyQuery)).
		WithArgs(now.Truncate(time.Hour).Add(-24*time.Hour), now.Truncate(time.Hour)).
		WillReturnRows(sqlmock.NewRows(accountSuccessHourlyColumns()))

	const callers = 12
	var wait sync.WaitGroup
	errs := make(chan error, callers)
	for range callers {
		wait.Add(1)
		go func() {
			defer wait.Done()
			got, err := service.GetAccountSuccessRates(context.Background())
			if err == nil && (!got.Ready || got.ActiveAccountCount != 1) {
				err = errors.New("unexpected shared success snapshot")
			}
			errs <- err
		}()
	}
	wait.Wait()
	close(errs)
	for err := range errs {
		if err != nil {
			t.Fatal(err)
		}
	}
	assertExpectations(t, mock)
}

func TestAccountSuccessFailureKeepsRecentLastGoodThenExpiresIt(t *testing.T) {
	now := time.Date(2026, 9, 3, 9, 0, 10, 0, time.UTC)
	service, mock, closeDB := newMockService(t, now)
	defer closeDB()
	boundary := now.Truncate(time.Minute)
	mock.ExpectQuery(regexp.QuoteMeta(accountSuccessMinuteQuery)).WithArgs(boundary.Add(-time.Hour), boundary).WillReturnRows(
		sqlmock.NewRows(accountSuccessMinuteColumns()).AddRow(boundary.Add(-time.Minute), int64(1), int64(1), int64(2), int64(3), int64(0), int64(0)),
	)
	mock.ExpectQuery(regexp.QuoteMeta(accountSuccessHourlyQuery)).WithArgs(now.Truncate(time.Hour).Add(-24*time.Hour), now.Truncate(time.Hour)).WillReturnRows(sqlmock.NewRows(accountSuccessHourlyColumns()))
	first, _ := service.GetAccountSuccessRates(context.Background())
	if !first.Ready {
		t.Fatalf("initial snapshot unavailable: %#v", first)
	}

	now = now.Add(2*time.Minute + time.Second)
	service.now = func() time.Time { return now }
	mock.ExpectQuery(regexp.QuoteMeta(accountSuccessMinuteQuery)).WillReturnError(errors.New("database offline"))
	stale, err := service.GetAccountSuccessRates(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if !stale.Ready || !stale.Stale || !stale.Partial || len(stale.Items) != 1 || stale.Notice == "" {
		t.Fatalf("recent last-good snapshot not preserved: %#v", stale)
	}

	now = now.Add(9 * time.Minute)
	service.now = func() time.Time { return now }
	mock.ExpectQuery(regexp.QuoteMeta(accountSuccessMinuteQuery)).WillReturnError(errors.New("database still offline"))
	expired, err := service.GetAccountSuccessRates(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if expired.Ready || !expired.Stale || !expired.Partial || len(expired.Items) != 0 || expired.ActiveAccountCount != 0 {
		t.Fatalf("expired last-good data was presented as current: %#v", expired)
	}
	assertExpectations(t, mock)
}

func TestAbsentDatabaseConfigurationOnlyDisablesActualSuccess(t *testing.T) {
	service, err := Open("", discardLogger())
	if err != nil {
		t.Fatal(err)
	}
	got, err := service.GetAccountSuccessRates(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if got.Ready || len(got.Items) != 0 || got.Notice == "" {
		t.Fatalf("missing database was presented as measured success: %#v", got)
	}
}

func findSuccessRate(t *testing.T, items []model.GroupAccountSuccessRate, groupID, accountID int64) model.GroupAccountSuccessRate {
	t.Helper()
	for _, item := range items {
		if item.GroupID == groupID && item.AccountID == accountID {
			return item
		}
	}
	t.Fatalf("missing group/account success rate %d:%d in %#v", groupID, accountID, items)
	return model.GroupAccountSuccessRate{}
}

func accountSuccessMinuteColumns() []string {
	return []string{"bucket_start", "group_id", "account_id", "success_count", "attempt_count", "client_canceled_count", "failover_count"}
}

func accountSuccessHourlyColumns() []string {
	return []string{"group_id", "account_id", "success_count", "attempt_count", "client_canceled_count", "failover_count", "last_observed_at"}
}
