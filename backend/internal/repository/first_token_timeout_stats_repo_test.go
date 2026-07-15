package repository

import (
	"context"
	"database/sql"
	"database/sql/driver"
	"errors"
	"fmt"
	"math"
	"os"
	"strings"
	"sync"
	"testing"
	"time"

	sqlmock "github.com/DATA-DOG/go-sqlmock"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/stretchr/testify/require"
)

func TestFirstTokenTimeoutStatsUpsertBatchAggregatesSameKey(t *testing.T) {
	db, mock, err := sqlmock.New(sqlmock.QueryMatcherOption(sqlmock.QueryMatcherRegexp))
	require.NoError(t, err)
	defer func() { _ = db.Close() }()

	repo := NewFirstTokenTimeoutStatsRepository(db)
	bucket := time.Date(2026, 7, 15, 13, 47, 22, 0, time.FixedZone("CST", 8*60*60))

	mock.ExpectExec("INSERT INTO first_token_timeout_stats_hourly").
		WithArgs(
			time.Date(2026, 7, 15, 5, 0, 0, 0, time.UTC),
			service.FirstTokenStatsScopeAttempt,
			int64(42),
			"openai_responses",
			"openai",
			"gpt-5.2",
			30,
			service.FirstTokenStatsAttemptSuccess,
			"",
			int64(5),
			int64(3),
			int64(1250),
			int64(700),
			int64(0),
		).
		WillReturnResult(sqlmock.NewResult(0, 1))

	err = repo.UpsertBatch(context.Background(), []service.FirstTokenStatsDelta{
		{
			BucketStart:       bucket,
			Scope:             service.FirstTokenStatsScopeAttempt,
			AccountID:         42,
			Protocol:          "openai_responses",
			Platform:          "openai",
			Model:             "gpt-5.2",
			TimeoutSeconds:    30,
			Outcome:           service.FirstTokenStatsAttemptSuccess,
			SampleCount:       2,
			TTFTSampleCount:   1,
			TTFTSumMS:         500,
			TTFTMaxMS:         500,
			TTFTAffectedCount: 0,
		},
		{
			BucketStart:       bucket.Add(12 * time.Minute),
			Scope:             service.FirstTokenStatsScopeAttempt,
			AccountID:         42,
			Protocol:          "openai_responses",
			Platform:          "openai",
			Model:             "gpt-5.2",
			TimeoutSeconds:    30,
			Outcome:           service.FirstTokenStatsAttemptSuccess,
			SampleCount:       3,
			TTFTSampleCount:   2,
			TTFTSumMS:         750,
			TTFTMaxMS:         700,
			TTFTAffectedCount: 0,
		},
	})
	require.NoError(t, err)
	require.NoError(t, mock.ExpectationsWereMet())
}

func TestFirstTokenTimeoutStatsUpsertBatchForcesRequestSentinels(t *testing.T) {
	db, mock, err := sqlmock.New(sqlmock.QueryMatcherOption(sqlmock.QueryMatcherRegexp))
	require.NoError(t, err)
	defer func() { _ = db.Close() }()

	repo := NewFirstTokenTimeoutStatsRepository(db)
	bucket := time.Date(2026, 7, 15, 5, 59, 0, 0, time.UTC)
	mock.ExpectExec("INSERT INTO first_token_timeout_stats_hourly").
		WithArgs(
			bucket.Truncate(time.Hour),
			service.FirstTokenStatsScopeRequest,
			int64(0),
			"anthropic_messages",
			"",
			"claude-sonnet-4-6",
			20,
			service.FirstTokenStatsRequestRecoveredAfterTTFT,
			"",
			int64(1),
			int64(0),
			int64(0),
			int64(0),
			int64(1),
		).
		WillReturnResult(sqlmock.NewResult(0, 1))

	err = repo.UpsertBatch(context.Background(), []service.FirstTokenStatsDelta{{
		BucketStart:       bucket,
		Scope:             service.FirstTokenStatsScopeRequest,
		AccountID:         99,
		Protocol:          "anthropic_messages",
		Platform:          "anthropic",
		Model:             "claude-sonnet-4-6",
		TimeoutSeconds:    20,
		Outcome:           service.FirstTokenStatsRequestRecoveredAfterTTFT,
		SampleCount:       1,
		TTFTSampleCount:   0,
		TTFTSumMS:         0,
		TTFTMaxMS:         0,
		TTFTAffectedCount: 1,
	}})
	require.NoError(t, err)
	require.NoError(t, mock.ExpectationsWereMet())
}

func TestFirstTokenTimeoutStatsUpsertBatchRejectsInvalidDelta(t *testing.T) {
	valid := service.FirstTokenStatsDelta{
		BucketStart:    time.Date(2026, 7, 15, 5, 0, 0, 0, time.UTC),
		Scope:          service.FirstTokenStatsScopeAttempt,
		AccountID:      42,
		Protocol:       "openai_responses",
		Platform:       "openai",
		Model:          "gpt-5.2",
		TimeoutSeconds: 30,
		Outcome:        service.FirstTokenStatsAttemptSuccess,
		SampleCount:    1,
	}

	tests := map[string]func(*service.FirstTokenStatsDelta){
		"zero bucket":         func(d *service.FirstTokenStatsDelta) { d.BucketStart = time.Time{} },
		"unknown scope":       func(d *service.FirstTokenStatsDelta) { d.Scope = "global" },
		"attempt account":     func(d *service.FirstTokenStatsDelta) { d.AccountID = 0 },
		"attempt platform":    func(d *service.FirstTokenStatsDelta) { d.Platform = "" },
		"empty protocol":      func(d *service.FirstTokenStatsDelta) { d.Protocol = "" },
		"long protocol":       func(d *service.FirstTokenStatsDelta) { d.Protocol = strings.Repeat("p", 33) },
		"long platform":       func(d *service.FirstTokenStatsDelta) { d.Platform = strings.Repeat("p", 33) },
		"long model":          func(d *service.FirstTokenStatsDelta) { d.Model = strings.Repeat("m", 256) },
		"low threshold":       func(d *service.FirstTokenStatsDelta) { d.TimeoutSeconds = 0 },
		"high threshold":      func(d *service.FirstTokenStatsDelta) { d.TimeoutSeconds = 301 },
		"wrong scope outcome": func(d *service.FirstTokenStatsDelta) { d.Outcome = service.FirstTokenStatsRequestTTFTExhausted },
		"unknown failure kind": func(d *service.FirstTokenStatsDelta) {
			d.Outcome, d.FailureKind = service.FirstTokenStatsAttemptOtherFailure, "dns"
		},
		"missing failure kind": func(d *service.FirstTokenStatsDelta) { d.Outcome = service.FirstTokenStatsAttemptOtherFailure },
		"unexpected failure":   func(d *service.FirstTokenStatsDelta) { d.FailureKind = service.FirstTokenStatsFailureTransport },
		"negative sample":      func(d *service.FirstTokenStatsDelta) { d.SampleCount = -1 },
		"negative ttft sample": func(d *service.FirstTokenStatsDelta) { d.TTFTSampleCount = -1 },
		"negative ttft sum":    func(d *service.FirstTokenStatsDelta) { d.TTFTSumMS = -1 },
		"negative ttft max":    func(d *service.FirstTokenStatsDelta) { d.TTFTMaxMS = -1 },
		"ttft max over int4":   func(d *service.FirstTokenStatsDelta) { d.TTFTMaxMS = math.MaxInt32 + 1 },
		"negative affected":    func(d *service.FirstTokenStatsDelta) { d.TTFTAffectedCount = -1 },
		"ttft samples exceed samples": func(d *service.FirstTokenStatsDelta) {
			d.TTFTSampleCount = 2
		},
		"zero ttft samples with sum": func(d *service.FirstTokenStatsDelta) {
			d.TTFTSumMS = 1
		},
		"zero ttft samples with max": func(d *service.FirstTokenStatsDelta) {
			d.TTFTMaxMS = 1
		},
		"ttft max exceeds sum": func(d *service.FirstTokenStatsDelta) {
			d.TTFTSampleCount = 1
			d.TTFTSumMS = 4
			d.TTFTMaxMS = 5
		},
		"request has ttft sample": func(d *service.FirstTokenStatsDelta) {
			d.Scope = service.FirstTokenStatsScopeRequest
			d.Outcome = service.FirstTokenStatsRequestSuccess
			d.TTFTSampleCount = 1
		},
		"attempt has affected request count": func(d *service.FirstTokenStatsDelta) {
			d.TTFTAffectedCount = 1
		},
		"request affected exceeds samples": func(d *service.FirstTokenStatsDelta) {
			d.Scope = service.FirstTokenStatsScopeRequest
			d.Outcome = service.FirstTokenStatsRequestOtherFailure
			d.FailureKind = service.FirstTokenStatsFailureTransport
			d.TTFTAffectedCount = 2
		},
		"request success is affected": func(d *service.FirstTokenStatsDelta) {
			d.Scope = service.FirstTokenStatsScopeRequest
			d.Outcome = service.FirstTokenStatsRequestSuccess
			d.TTFTAffectedCount = 1
		},
		"recovered request affected mismatch": func(d *service.FirstTokenStatsDelta) {
			d.Scope = service.FirstTokenStatsScopeRequest
			d.Outcome = service.FirstTokenStatsRequestRecoveredAfterTTFT
			d.SampleCount = 2
			d.TTFTAffectedCount = 1
		},
		"exhausted request affected mismatch": func(d *service.FirstTokenStatsDelta) {
			d.Scope = service.FirstTokenStatsScopeRequest
			d.Outcome = service.FirstTokenStatsRequestTTFTExhausted
			d.SampleCount = 2
			d.TTFTAffectedCount = 1
		},
		"ttft timeout has ttft sample": func(d *service.FirstTokenStatsDelta) {
			d.Outcome = service.FirstTokenStatsAttemptTTFTTimeout
			d.TTFTSampleCount = 1
		},
	}

	for name, mutate := range tests {
		t.Run(name, func(t *testing.T) {
			delta := valid
			mutate(&delta)
			repo := &firstTokenTimeoutStatsRepository{}
			err := repo.UpsertBatch(context.Background(), []service.FirstTokenStatsDelta{delta})
			require.ErrorContains(t, err, "invalid first token stats delta")
		})
	}
}

func TestFirstTokenTimeoutStatsUpsertBatchEmptyIsNoop(t *testing.T) {
	repo := &firstTokenTimeoutStatsRepository{}
	require.NoError(t, repo.UpsertBatch(context.Background(), nil))
}

func TestFirstTokenTimeoutStatsUpsertBatchRejectsCounterOverflow(t *testing.T) {
	base := service.FirstTokenStatsDelta{
		BucketStart:    time.Date(2026, 7, 15, 5, 0, 0, 0, time.UTC),
		Scope:          service.FirstTokenStatsScopeAttempt,
		AccountID:      42,
		Protocol:       "openai_responses",
		Platform:       "openai",
		Model:          "gpt-5.2",
		TimeoutSeconds: 30,
		Outcome:        service.FirstTokenStatsAttemptSuccess,
		SampleCount:    math.MaxInt64,
	}
	second := base
	second.SampleCount = 1

	repo := &firstTokenTimeoutStatsRepository{}
	err := repo.UpsertBatch(context.Background(), []service.FirstTokenStatsDelta{base, second})
	require.ErrorContains(t, err, "overflow")
}

func TestFirstTokenTimeoutStatsUpsertBatchRejectsMoreThanOneThousandUniqueKeys(t *testing.T) {
	deltas := make([]service.FirstTokenStatsDelta, 1001)
	for i := range deltas {
		deltas[i] = service.FirstTokenStatsDelta{
			BucketStart:    time.Date(2026, 7, 15, 5, 0, 0, 0, time.UTC),
			Scope:          service.FirstTokenStatsScopeAttempt,
			AccountID:      int64(i + 1),
			Protocol:       "openai_responses",
			Platform:       "openai",
			Model:          "gpt-5.2",
			TimeoutSeconds: 30,
			Outcome:        service.FirstTokenStatsAttemptSuccess,
			SampleCount:    1,
		}
	}

	repo := &firstTokenTimeoutStatsRepository{}
	err := repo.UpsertBatch(context.Background(), deltas)
	require.ErrorContains(t, err, "unique key limit")
}

func TestFirstTokenTimeoutStatsUpsertBatchAcceptsOneThousandUniqueKeys(t *testing.T) {
	db, mock, err := sqlmock.New(sqlmock.QueryMatcherOption(sqlmock.QueryMatcherRegexp))
	require.NoError(t, err)
	defer func() { _ = db.Close() }()
	mock.ExpectExec("INSERT INTO first_token_timeout_stats_hourly").
		WillReturnResult(sqlmock.NewResult(0, 1000))

	deltas := make([]service.FirstTokenStatsDelta, 1000)
	for i := range deltas {
		deltas[i] = service.FirstTokenStatsDelta{
			BucketStart:    time.Date(2026, 7, 15, 5, 0, 0, 0, time.UTC),
			Scope:          service.FirstTokenStatsScopeAttempt,
			AccountID:      int64(i + 1),
			Protocol:       "openai_responses",
			Platform:       "openai",
			Model:          "gpt-5.2",
			TimeoutSeconds: 30,
			Outcome:        service.FirstTokenStatsAttemptSuccess,
			SampleCount:    1,
		}
	}

	err = NewFirstTokenTimeoutStatsRepository(db).UpsertBatch(context.Background(), deltas)
	require.NoError(t, err)
	require.NoError(t, mock.ExpectationsWereMet())
}

func TestFirstTokenTimeoutStatsQueryOverviewCalculatesStableRates(t *testing.T) {
	db, mock, err := sqlmock.New(sqlmock.QueryMatcherOption(sqlmock.QueryMatcherRegexp))
	require.NoError(t, err)
	defer func() { _ = db.Close() }()

	end := time.Date(2026, 7, 15, 5, 23, 0, 0, time.UTC)
	wantEnd := time.Date(2026, 7, 15, 6, 0, 0, 0, time.UTC)
	wantStart := wantEnd.Add(-24 * time.Hour)
	args := []driver.Value{wantStart, wantEnd, "openai_responses", "gpt-5.2"}

	mock.ExpectBegin()
	mock.ExpectQuery("FROM first_token_timeout_stats_hourly").
		WithArgs(args...).
		WillReturnRows(sqlmock.NewRows([]string{
			"controlled_requests",
			"attempt_ttft_timeout_count",
			"attempt_denominator",
			"recovered_count",
			"affected_count",
			"final_ttft_count",
			"request_denominator",
			"other_final_count",
		}).AddRow(int64(14), int64(2), int64(10), int64(3), int64(5), int64(1), int64(12), int64(2)))

	mock.ExpectQuery("generate_series").
		WithArgs(args...).
		WillReturnRows(sqlmock.NewRows([]string{
			"bucket_start",
			"attempt_ttft_timeout_count",
			"attempt_denominator",
			"recovered_count",
			"affected_count",
			"final_ttft_count",
			"request_denominator",
			"other_final_count",
		}).
			AddRow(wantStart, int64(1), int64(4), int64(1), int64(2), int64(1), int64(3), int64(1)).
			AddRow(wantStart.Add(time.Hour), int64(0), int64(0), int64(0), int64(0), int64(0), int64(0), int64(0)))

	mock.ExpectQuery("outcome = 'other_failure'").
		WithArgs(args...).
		WillReturnRows(sqlmock.NewRows([]string{"failure_kind", "sample_count"}).
			AddRow(service.FirstTokenStatsFailureTransport, int64(2)).
			AddRow(service.FirstTokenStatsFailureUpstream5xx, int64(1)))
	mock.ExpectCommit()

	overview, err := NewFirstTokenTimeoutStatsRepository(db).QueryOverview(context.Background(), service.FirstTokenStatsOverviewFilter{
		Range:    service.FirstTokenStatsRange24Hours,
		End:      end,
		Protocol: "openai_responses",
		Model:    "gpt-5.2",
	})
	require.NoError(t, err)
	require.Equal(t, int64(14), overview.Summary.ControlledRequests)
	require.Equal(t, service.FirstTokenStatsRatio{Numerator: 2, Denominator: 10, Rate: 0.2}, overview.Summary.AttemptTTFTTimeoutRate)
	require.Equal(t, service.FirstTokenStatsRatio{Numerator: 3, Denominator: 5, Rate: 0.6}, overview.Summary.RecoveryRate)
	require.Equal(t, service.FirstTokenStatsRatio{Numerator: 1, Denominator: 12, Rate: 1.0 / 12.0}, overview.Summary.FinalTTFTFailureRate)
	require.Equal(t, service.FirstTokenStatsRatio{Numerator: 2, Denominator: 12, Rate: 1.0 / 6.0}, overview.Summary.OtherFinalFailureRate)
	require.Len(t, overview.Trend, 2)
	require.Equal(t, service.FirstTokenStatsRatio{Numerator: 0, Denominator: 0, Rate: 0}, overview.Trend[1].AttemptTTFTTimeoutRate)
	require.Equal(t, []service.FirstTokenStatsFailureDistribution{
		{FailureKind: service.FirstTokenStatsFailureTransport, SampleCount: 2},
		{FailureKind: service.FirstTokenStatsFailureUpstream5xx, SampleCount: 1},
	}, overview.OtherFailures)
	require.NoError(t, mock.ExpectationsWereMet())
}

func TestFirstTokenTimeoutStatsQueryOverviewRejectsUnsupportedRange(t *testing.T) {
	repo := &firstTokenTimeoutStatsRepository{}
	_, err := repo.QueryOverview(context.Background(), service.FirstTokenStatsOverviewFilter{Range: "48h"})
	require.Error(t, err)
}

func TestFirstTokenTimeoutStatsQueryAccountsFiltersSortsAndPaginates(t *testing.T) {
	db, mock, err := sqlmock.New(sqlmock.QueryMatcherOption(sqlmock.QueryMatcherRegexp))
	require.NoError(t, err)
	defer func() { _ = db.Close() }()

	end := time.Date(2026, 7, 15, 5, 23, 0, 0, time.UTC)
	wantEnd := time.Date(2026, 7, 15, 6, 0, 0, 0, time.UTC)
	wantStart := wantEnd.Add(-7 * 24 * time.Hour)
	filterArgs := []driver.Value{wantStart, wantEnd, "openai_responses", "gpt-5.2", "openai", int64(99), "%#99%"}

	mock.ExpectBegin()
	mock.ExpectQuery("SELECT COUNT\\(\\*\\) FROM filtered").
		WithArgs(filterArgs...).
		WillReturnRows(sqlmock.NewRows([]string{"count"}).AddRow(int64(2)))
	mock.ExpectQuery("ORDER BY other_failure_rate ASC").
		WithArgs(append(filterArgs, int64(1), int64(1))...).
		WillReturnRows(sqlmock.NewRows([]string{
			"account_id",
			"account_name",
			"platform",
			"samples",
			"success_count",
			"ttft_timeout_count",
			"other_failure_count",
			"ttft_sum_ms",
			"ttft_sample_count",
		}).
			AddRow(int64(99), "#99", "openai", int64(10), int64(6), int64(3), int64(1), int64(900), int64(2)).
			AddRow(int64(100), "active", "openai", int64(0), int64(0), int64(0), int64(0), int64(0), int64(0)))
	mock.ExpectCommit()

	page, err := NewFirstTokenTimeoutStatsRepository(db).QueryAccounts(context.Background(), service.FirstTokenStatsAccountFilter{
		Range:     service.FirstTokenStatsRange7Days,
		End:       end,
		Protocol:  "openai_responses",
		Model:     "gpt-5.2",
		Platform:  "openai",
		AccountID: 99,
		Search:    "#99",
		SortBy:    service.FirstTokenStatsAccountSortOtherFailureRate,
		SortOrder: "asc",
		Page:      2,
		PageSize:  1,
	})
	require.NoError(t, err)
	require.Equal(t, int64(2), page.Total)
	require.Equal(t, 2, page.Page)
	require.Equal(t, 1, page.PageSize)
	require.Equal(t, 2, page.Pages)
	require.Equal(t, []service.FirstTokenStatsAccount{
		{
			AccountID:         99,
			AccountName:       "#99",
			Platform:          "openai",
			Samples:           10,
			SuccessCount:      6,
			TTFTTimeoutCount:  3,
			TTFTTimeoutRate:   service.FirstTokenStatsRatio{Numerator: 3, Denominator: 10, Rate: 0.3},
			OtherFailureCount: 1,
			OtherFailureRate:  service.FirstTokenStatsRatio{Numerator: 1, Denominator: 10, Rate: 0.1},
			AvgTTFTMS:         450,
			LowSample:         true,
		},
		{
			AccountID:        100,
			AccountName:      "active",
			Platform:         "openai",
			TTFTTimeoutRate:  service.FirstTokenStatsRatio{},
			OtherFailureRate: service.FirstTokenStatsRatio{},
			LowSample:        true,
		},
	}, page.Items)
	require.NoError(t, mock.ExpectationsWereMet())
}

func TestFirstTokenTimeoutStatsQueryAccountsRejectsUnsafeSort(t *testing.T) {
	repo := &firstTokenTimeoutStatsRepository{}
	_, err := repo.QueryAccounts(context.Background(), service.FirstTokenStatsAccountFilter{
		Range:  service.FirstTokenStatsRange24Hours,
		SortBy: "samples; DROP TABLE accounts",
	})
	require.Error(t, err)
}

func TestFirstTokenTimeoutStatsQueryAccountsRejectsPageOffsetOverflow(t *testing.T) {
	repo := &firstTokenTimeoutStatsRepository{}
	_, err := repo.QueryAccounts(context.Background(), service.FirstTokenStatsAccountFilter{
		Range:    service.FirstTokenStatsRange24Hours,
		Page:     math.MaxInt,
		PageSize: 100,
	})
	require.ErrorContains(t, err, "page offset")
}

func TestFirstTokenTimeoutStatsQueryOverviewRollsBackOnQueryError(t *testing.T) {
	db, mock, err := sqlmock.New(sqlmock.QueryMatcherOption(sqlmock.QueryMatcherRegexp))
	require.NoError(t, err)
	defer func() { _ = db.Close() }()

	mock.ExpectBegin()
	mock.ExpectQuery("FROM first_token_timeout_stats_hourly").
		WillReturnError(errors.New("summary unavailable"))
	mock.ExpectRollback()

	_, err = NewFirstTokenTimeoutStatsRepository(db).QueryOverview(context.Background(), service.FirstTokenStatsOverviewFilter{
		Range: service.FirstTokenStatsRange24Hours,
		End:   time.Date(2026, 7, 15, 5, 23, 0, 0, time.UTC),
	})
	require.ErrorContains(t, err, "summary unavailable")
	require.NoError(t, mock.ExpectationsWereMet())
}

func TestFirstTokenTimeoutStatsQueryOverviewReturnsCommitError(t *testing.T) {
	db, mock, err := sqlmock.New(sqlmock.QueryMatcherOption(sqlmock.QueryMatcherRegexp))
	require.NoError(t, err)
	defer func() { _ = db.Close() }()

	mock.ExpectBegin()
	mock.ExpectQuery("FROM first_token_timeout_stats_hourly").
		WillReturnRows(sqlmock.NewRows([]string{
			"controlled_requests",
			"attempt_ttft_timeout_count",
			"attempt_denominator",
			"recovered_count",
			"affected_count",
			"final_ttft_count",
			"request_denominator",
			"other_final_count",
		}).AddRow(int64(0), int64(0), int64(0), int64(0), int64(0), int64(0), int64(0), int64(0)))
	mock.ExpectQuery("generate_series").
		WillReturnRows(sqlmock.NewRows([]string{
			"bucket_start",
			"attempt_ttft_timeout_count",
			"attempt_denominator",
			"recovered_count",
			"affected_count",
			"final_ttft_count",
			"request_denominator",
			"other_final_count",
		}))
	mock.ExpectQuery("outcome = 'other_failure'").
		WillReturnRows(sqlmock.NewRows([]string{"failure_kind", "sample_count"}))
	mock.ExpectCommit().WillReturnError(errors.New("commit snapshot"))

	_, err = NewFirstTokenTimeoutStatsRepository(db).QueryOverview(context.Background(), service.FirstTokenStatsOverviewFilter{
		Range: service.FirstTokenStatsRange24Hours,
		End:   time.Date(2026, 7, 15, 5, 23, 0, 0, time.UTC),
	})
	require.ErrorContains(t, err, "commit snapshot")
	require.NoError(t, mock.ExpectationsWereMet())
}

func TestFirstTokenTimeoutStatsQueriesUseRepeatableReadReadOnly(t *testing.T) {
	state := &firstTokenStatsTxOptionsState{}
	driverName := fmt.Sprintf("first_token_stats_tx_options_%d", time.Now().UnixNano())
	sql.Register(driverName, firstTokenStatsTxOptionsDriver{state: state})
	db, err := sql.Open(driverName, "")
	require.NoError(t, err)
	defer func() { _ = db.Close() }()

	_, err = NewFirstTokenTimeoutStatsRepository(db).QueryOverview(context.Background(), service.FirstTokenStatsOverviewFilter{
		Range: service.FirstTokenStatsRange24Hours,
		End:   time.Date(2026, 7, 15, 5, 23, 0, 0, time.UTC),
	})
	require.ErrorContains(t, err, "stop after begin")

	state.mu.Lock()
	defer state.mu.Unlock()
	require.True(t, state.beginCalled)
	require.Equal(t, driver.IsolationLevel(sql.LevelRepeatableRead), state.options.Isolation)
	require.True(t, state.options.ReadOnly)
	require.True(t, state.rollbackCalled)
}

func TestFirstTokenTimeoutStatsDeleteBeforeUsesTruncatedUTCCutoff(t *testing.T) {
	db, mock, err := sqlmock.New(sqlmock.QueryMatcherOption(sqlmock.QueryMatcherRegexp))
	require.NoError(t, err)
	defer func() { _ = db.Close() }()

	cutoff := time.Date(2026, 7, 15, 13, 47, 0, 0, time.FixedZone("CST", 8*60*60))
	wantCutoff := time.Date(2026, 7, 15, 5, 0, 0, 0, time.UTC)
	mock.ExpectExec("DELETE FROM first_token_timeout_stats_hourly WHERE bucket_start < \\$1").
		WithArgs(wantCutoff).
		WillReturnResult(sqlmock.NewResult(0, 3))

	deleted, err := NewFirstTokenTimeoutStatsRepository(db).DeleteBefore(context.Background(), cutoff)
	require.NoError(t, err)
	require.Equal(t, int64(3), deleted)
	require.NoError(t, mock.ExpectationsWereMet())
}

func TestFirstTokenTimeoutStatsDeleteBeforeRejectsZeroCutoff(t *testing.T) {
	repo := &firstTokenTimeoutStatsRepository{}
	_, err := repo.DeleteBefore(context.Background(), time.Time{})
	require.Error(t, err)
}

func TestFirstTokenTimeoutStatsProviderRegistered(t *testing.T) {
	content, err := os.ReadFile("wire.go")
	require.NoError(t, err)
	require.Contains(t, string(content), "NewFirstTokenTimeoutStatsRepository,")
}

type firstTokenStatsTxOptionsState struct {
	mu             sync.Mutex
	beginCalled    bool
	rollbackCalled bool
	options        driver.TxOptions
}

type firstTokenStatsTxOptionsDriver struct {
	state *firstTokenStatsTxOptionsState
}

func (d firstTokenStatsTxOptionsDriver) Open(string) (driver.Conn, error) {
	return &firstTokenStatsTxOptionsConn{state: d.state}, nil
}

type firstTokenStatsTxOptionsConn struct {
	state *firstTokenStatsTxOptionsState
}

func (c *firstTokenStatsTxOptionsConn) Prepare(string) (driver.Stmt, error) {
	return nil, errors.New("prepare not supported")
}

func (c *firstTokenStatsTxOptionsConn) Close() error { return nil }

func (c *firstTokenStatsTxOptionsConn) Begin() (driver.Tx, error) {
	return c.BeginTx(context.Background(), driver.TxOptions{})
}

func (c *firstTokenStatsTxOptionsConn) BeginTx(_ context.Context, options driver.TxOptions) (driver.Tx, error) {
	c.state.mu.Lock()
	c.state.beginCalled = true
	c.state.options = options
	c.state.mu.Unlock()
	return &firstTokenStatsTxOptionsTx{state: c.state}, nil
}

func (c *firstTokenStatsTxOptionsConn) QueryContext(context.Context, string, []driver.NamedValue) (driver.Rows, error) {
	return nil, errors.New("stop after begin")
}

type firstTokenStatsTxOptionsTx struct {
	state *firstTokenStatsTxOptionsState
}

func (t *firstTokenStatsTxOptionsTx) Commit() error { return nil }

func (t *firstTokenStatsTxOptionsTx) Rollback() error {
	t.state.mu.Lock()
	t.state.rollbackCalled = true
	t.state.mu.Unlock()
	return nil
}
