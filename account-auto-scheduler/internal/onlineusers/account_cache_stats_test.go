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
)

func TestTodayAccountCacheStatsUseOneBulkQueryAndPreserveZeroAccounts(t *testing.T) {
	for _, required := range []string{
		"FROM usage_logs",
		"account_id = ANY($1::BIGINT[])",
		"created_at >= CURRENT_DATE",
		"GROUP BY account_id",
		"cache_creation_tokens",
		"cache_read_tokens",
	} {
		if !strings.Contains(accountCacheStatsQuery, required) {
			t.Fatalf("cache-stat query is missing %q:\n%s", required, accountCacheStatsQuery)
		}
	}

	now := time.Date(2026, 9, 3, 9, 0, 0, 0, time.UTC)
	service, mock, closeDB := newMockService(t, now)
	defer closeDB()
	mock.ExpectQuery(regexp.QuoteMeta(accountCacheStatsQuery)).
		WithArgs("{1,2}").
		WillReturnRows(sqlmock.NewRows(accountCacheStatsColumns()).
			AddRow(int64(2), int64(600), int64(300), int64(600)))

	first, err := service.GetTodayAccountCacheStats(context.Background(), []int64{2, 1, 2, 0})
	if err != nil {
		t.Fatal(err)
	}
	if !first.Configured || !first.Ready || first.Stale || len(first.Stats) != 2 {
		t.Fatalf("unexpected bulk snapshot: %#v", first)
	}
	if zero := first.Stats[1]; zero.PromptTokens != 0 || zero.HitRate != 0 {
		t.Fatalf("missing account did not receive known-zero projection: %#v", zero)
	}
	stats := first.Stats[2]
	if stats.InputTokens != 600 || stats.CacheCreationTokens != 300 || stats.CacheReadTokens != 600 || stats.PromptTokens != 1500 || stats.HitRate != 40 {
		t.Fatalf("unexpected cache projection: %#v", stats)
	}
	first.Stats[2] = first.Stats[1]
	second, err := service.GetTodayAccountCacheStats(context.Background(), []int64{1, 2})
	if err != nil {
		t.Fatal(err)
	}
	if got := second.Stats[2]; got.PromptTokens != 1500 || got.HitRate != 40 {
		t.Fatalf("cached projection changed unexpectedly: %#v", got)
	}
	assertExpectations(t, mock)
}

func TestTodayAccountCacheStatsCachesStaleLastGoodAndNeverFabricatesData(t *testing.T) {
	now := time.Date(2026, 9, 3, 9, 0, 0, 0, time.UTC)
	service, mock, closeDB := newMockService(t, now)
	defer closeDB()
	service.cacheStatsTTL = time.Second
	mock.ExpectQuery(regexp.QuoteMeta(accountCacheStatsQuery)).
		WithArgs("{1}").
		WillReturnRows(sqlmock.NewRows(accountCacheStatsColumns()).
			AddRow(int64(1), int64(10), int64(5), int64(15)))
	first, err := service.GetTodayAccountCacheStats(context.Background(), []int64{1})
	if err != nil || !first.Ready || first.Stats[1].HitRate != 50 {
		t.Fatalf("initial cache stats unavailable: %#v err=%v", first, err)
	}

	now = now.Add(2 * time.Second)
	service.now = func() time.Time { return now }
	mock.ExpectQuery(regexp.QuoteMeta(accountCacheStatsQuery)).WithArgs("{1}").WillReturnError(errors.New("database offline"))
	stale, err := service.GetTodayAccountCacheStats(context.Background(), []int64{1})
	if err != nil || !stale.Configured || !stale.Ready || !stale.Stale || stale.Stats[1].HitRate != 50 || stale.Notice == "" {
		t.Fatalf("last good cache stats were not preserved: %#v err=%v", stale, err)
	}

	now = now.Add(11 * time.Minute)
	service.now = func() time.Time { return now }
	mock.ExpectQuery(regexp.QuoteMeta(accountCacheStatsQuery)).WithArgs("{1}").WillReturnError(errors.New("database still offline"))
	expired, err := service.GetTodayAccountCacheStats(context.Background(), []int64{1})
	if err != nil || !expired.Configured || expired.Ready || !expired.Stale || len(expired.Stats) != 0 {
		t.Fatalf("expired last good cache stats were presented as current: %#v err=%v", expired, err)
	}
	assertExpectations(t, mock)
}

func TestConcurrentTodayAccountCacheStatsMissesShareOneQuery(t *testing.T) {
	now := time.Date(2026, 9, 3, 9, 0, 0, 0, time.UTC)
	service, mock, closeDB := newMockService(t, now)
	defer closeDB()
	mock.ExpectQuery(regexp.QuoteMeta(accountCacheStatsQuery)).
		WithArgs("{7}").
		WillDelayFor(75 * time.Millisecond).
		WillReturnRows(sqlmock.NewRows(accountCacheStatsColumns()).
			AddRow(int64(7), int64(1), int64(0), int64(9)))

	const callers = 10
	var wait sync.WaitGroup
	errs := make(chan error, callers)
	for range callers {
		wait.Add(1)
		go func() {
			defer wait.Done()
			got, err := service.GetTodayAccountCacheStats(context.Background(), []int64{7})
			if err == nil && (!got.Ready || got.Stats[7].HitRate != 90) {
				err = errors.New("unexpected shared cache-stat result")
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

func TestTodayAccountCacheStatsReportsUnconfiguredDatabase(t *testing.T) {
	service, err := Open("", discardLogger())
	if err != nil {
		t.Fatal(err)
	}
	got, err := service.GetTodayAccountCacheStats(context.Background(), []int64{1})
	if err != nil {
		t.Fatal(err)
	}
	if got.Configured || got.Ready || len(got.Stats) != 0 || got.Notice == "" {
		t.Fatalf("unconfigured database is misleading: %#v", got)
	}
}

func accountCacheStatsColumns() []string {
	return []string{"account_id", "input_tokens", "cache_creation_tokens", "cache_read_tokens"}
}
