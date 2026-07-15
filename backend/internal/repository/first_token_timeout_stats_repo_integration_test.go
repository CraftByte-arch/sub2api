//go:build integration

package repository

import (
	"context"
	"database/sql"
	"fmt"
	"math"
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/stretchr/testify/require"
)

func TestFirstTokenTimeoutStatsUpsertBatchAccumulatesAcrossInstancesAndSeparatesThresholds(t *testing.T) {
	dbA, err := sql.Open("postgres", integrationPostgresDSN)
	require.NoError(t, err)
	defer func() { _ = dbA.Close() }()
	dbB, err := sql.Open("postgres", integrationPostgresDSN)
	require.NoError(t, err)
	defer func() { _ = dbB.Close() }()
	require.NotSame(t, dbA, dbB)
	require.NoError(t, dbA.PingContext(context.Background()))
	require.NoError(t, dbB.PingContext(context.Background()))

	repoA := NewFirstTokenTimeoutStatsRepository(dbA)
	repoB := NewFirstTokenTimeoutStatsRepository(dbB)
	bucket := time.Date(2026, 7, 15, 5, 0, 0, 0, time.UTC)
	model := "gpt-5.2-concurrent-upsert-test"
	_, err = integrationDB.ExecContext(context.Background(), `
DELETE FROM first_token_timeout_stats_hourly WHERE model = $1
`, model)
	require.NoError(t, err)
	t.Cleanup(func() {
		_, _ = integrationDB.ExecContext(context.Background(), `
DELETE FROM first_token_timeout_stats_hourly WHERE model = $1
`, model)
	})

	base := service.FirstTokenStatsDelta{
		BucketStart:     bucket,
		Scope:           service.FirstTokenStatsScopeAttempt,
		AccountID:       42,
		Protocol:        "openai_responses",
		Platform:        "openai",
		Model:           model,
		TimeoutSeconds:  30,
		Outcome:         service.FirstTokenStatsAttemptSuccess,
		SampleCount:     2,
		TTFTSampleCount: 1,
		TTFTSumMS:       500,
		TTFTMaxMS:       500,
	}
	second := base
	second.SampleCount = 3
	second.TTFTSampleCount = 2
	second.TTFTSumMS = 900
	second.TTFTMaxMS = 700
	start := make(chan struct{})
	errs := make(chan error, 2)
	go func() {
		<-start
		errs <- repoA.UpsertBatch(context.Background(), []service.FirstTokenStatsDelta{base})
	}()
	go func() {
		<-start
		errs <- repoB.UpsertBatch(context.Background(), []service.FirstTokenStatsDelta{second})
	}()
	close(start)
	require.NoError(t, <-errs)
	require.NoError(t, <-errs)

	differentThreshold := base
	differentThreshold.TimeoutSeconds = 20
	differentThreshold.SampleCount = 7
	require.NoError(t, repoB.UpsertBatch(context.Background(), []service.FirstTokenStatsDelta{differentThreshold}))

	rows, err := integrationDB.QueryContext(context.Background(), `
SELECT timeout_seconds, sample_count, ttft_sample_count, ttft_sum_ms, ttft_max_ms
FROM first_token_timeout_stats_hourly
WHERE bucket_start = $1 AND scope = 'attempt' AND account_id = 42 AND model = $2
ORDER BY timeout_seconds
`, bucket, model)
	require.NoError(t, err)
	defer func() { _ = rows.Close() }()

	type aggregate struct {
		threshold int
		samples   int64
		ttftCount int64
		ttftSumMS int64
		ttftMaxMS int64
	}
	var got []aggregate
	for rows.Next() {
		var item aggregate
		require.NoError(t, rows.Scan(&item.threshold, &item.samples, &item.ttftCount, &item.ttftSumMS, &item.ttftMaxMS))
		got = append(got, item)
	}
	require.NoError(t, rows.Err())
	require.Equal(t, []aggregate{
		{threshold: 20, samples: 7, ttftCount: 1, ttftSumMS: 500, ttftMaxMS: 500},
		{threshold: 30, samples: 5, ttftCount: 3, ttftSumMS: 1400, ttftMaxMS: 700},
	}, got)
}

func TestFirstTokenTimeoutStatsQueriesUseDocumentedDenominators(t *testing.T) {
	repo := NewFirstTokenTimeoutStatsRepository(integrationDB)
	ctx := context.Background()
	model := "gpt-5.2-query-snapshot-test"
	_, err := integrationDB.ExecContext(ctx, `DELETE FROM first_token_timeout_stats_hourly WHERE model = $1`, model)
	require.NoError(t, err)
	_, err = integrationDB.ExecContext(ctx, `DELETE FROM accounts WHERE name IN ('deleted stats account', 'active stats account')`)
	require.NoError(t, err)

	var deletedAccountID int64
	require.NoError(t, integrationDB.QueryRowContext(ctx, `
INSERT INTO accounts (name, platform, type, deleted_at)
VALUES ('deleted stats account', 'openai', 'api_key', NOW())
RETURNING id
	`).Scan(&deletedAccountID))
	var activeAccountID int64
	require.NoError(t, integrationDB.QueryRowContext(ctx, `
INSERT INTO accounts (name, platform, type)
VALUES ('active stats account', 'openai', 'api_key')
RETURNING id
	`).Scan(&activeAccountID))
	t.Cleanup(func() {
		_, _ = integrationDB.ExecContext(context.Background(), `DELETE FROM first_token_timeout_stats_hourly WHERE model = $1`, model)
		_, _ = integrationDB.ExecContext(context.Background(), `DELETE FROM accounts WHERE id IN ($1, $2)`, deletedAccountID, activeAccountID)
	})

	bucket := time.Date(2026, 7, 15, 5, 0, 0, 0, time.UTC)
	baseAttempt := service.FirstTokenStatsDelta{
		BucketStart:    bucket,
		Scope:          service.FirstTokenStatsScopeAttempt,
		Protocol:       "openai_responses",
		Platform:       "openai",
		Model:          model,
		TimeoutSeconds: 30,
	}
	baseRequest := service.FirstTokenStatsDelta{
		BucketStart:    bucket,
		Scope:          service.FirstTokenStatsScopeRequest,
		Protocol:       "openai_responses",
		Model:          model,
		TimeoutSeconds: 30,
	}
	deltas := []service.FirstTokenStatsDelta{
		withFirstTokenStatsDelta(baseAttempt, deletedAccountID, service.FirstTokenStatsAttemptSuccess, "", 4, 4, 400, 150, 0),
		withFirstTokenStatsDelta(baseAttempt, deletedAccountID, service.FirstTokenStatsAttemptTTFTTimeout, "", 2, 0, 0, 0, 0),
		withFirstTokenStatsDelta(baseAttempt, deletedAccountID, service.FirstTokenStatsAttemptOtherFailure, service.FirstTokenStatsFailureTransport, 1, 1, 300, 300, 0),
		withFirstTokenStatsDelta(baseAttempt, deletedAccountID, service.FirstTokenStatsAttemptClientCanceled, "", 3, 0, 0, 0, 0),
		withFirstTokenStatsDelta(baseAttempt, activeAccountID, service.FirstTokenStatsAttemptSuccess, "", 1, 1, 50, 50, 0),
		withFirstTokenStatsDelta(baseAttempt, activeAccountID, service.FirstTokenStatsAttemptTTFTTimeout, "", 1, 0, 0, 0, 0),
		withFirstTokenStatsDelta(baseRequest, 0, service.FirstTokenStatsRequestSuccess, "", 5, 0, 0, 0, 0),
		withFirstTokenStatsDelta(baseRequest, 0, service.FirstTokenStatsRequestRecoveredAfterTTFT, "", 2, 0, 0, 0, 2),
		withFirstTokenStatsDelta(func() service.FirstTokenStatsDelta {
			delta := baseRequest
			delta.TimeoutSeconds = 20
			return delta
		}(), 0, service.FirstTokenStatsRequestRecoveredAfterTTFT, "", 1, 0, 0, 0, 1),
		withFirstTokenStatsDelta(baseRequest, 0, service.FirstTokenStatsRequestTTFTExhausted, "", 1, 0, 0, 0, 1),
		withFirstTokenStatsDelta(baseRequest, 0, service.FirstTokenStatsRequestOtherFailure, service.FirstTokenStatsFailureTransport, 2, 0, 0, 0, 1),
		withFirstTokenStatsDelta(baseRequest, 0, service.FirstTokenStatsRequestClientCanceled, "", 4, 0, 0, 0, 1),
	}
	require.NoError(t, repo.UpsertBatch(ctx, deltas))

	overview, err := repo.QueryOverview(ctx, service.FirstTokenStatsOverviewFilter{
		Range:    service.FirstTokenStatsRange24Hours,
		End:      time.Date(2026, 7, 15, 5, 23, 0, 0, time.UTC),
		Protocol: "openai_responses",
		Model:    model,
	})
	require.NoError(t, err)
	require.Equal(t, int64(15), overview.Summary.ControlledRequests)
	require.Equal(t, service.FirstTokenStatsRatio{Numerator: 3, Denominator: 9, Rate: 1.0 / 3.0}, overview.Summary.AttemptTTFTTimeoutRate)
	require.Equal(t, service.FirstTokenStatsRatio{Numerator: 3, Denominator: 5, Rate: 0.6}, overview.Summary.RecoveryRate)
	require.Equal(t, service.FirstTokenStatsRatio{Numerator: 1, Denominator: 11, Rate: 1.0 / 11.0}, overview.Summary.FinalTTFTFailureRate)
	require.Equal(t, service.FirstTokenStatsRatio{Numerator: 2, Denominator: 11, Rate: 2.0 / 11.0}, overview.Summary.OtherFinalFailureRate)
	require.Len(t, overview.Trend, 24)
	require.Equal(t, bucket, overview.Trend[23].BucketStart)
	require.Equal(t, overview.Summary.AttemptTTFTTimeoutRate, overview.Trend[23].AttemptTTFTTimeoutRate)
	require.Equal(t, service.FirstTokenStatsRatio{Numerator: 3, Denominator: 5, Rate: 0.6}, overview.Trend[23].RecoveryRate)
	require.Equal(t, service.FirstTokenStatsRatio{}, overview.Trend[0].AttemptTTFTTimeoutRate)
	require.Equal(t, []service.FirstTokenStatsFailureDistribution{{
		FailureKind: service.FirstTokenStatsFailureTransport,
		SampleCount: 2,
	}}, overview.OtherFailures)

	accountPage, err := repo.QueryAccounts(ctx, service.FirstTokenStatsAccountFilter{
		Range:     service.FirstTokenStatsRange24Hours,
		End:       time.Date(2026, 7, 15, 5, 23, 0, 0, time.UTC),
		Protocol:  "openai_responses",
		Model:     model,
		Platform:  "openai",
		SortBy:    service.FirstTokenStatsAccountSortAvgTTFTMS,
		SortOrder: "desc",
		Page:      1,
		PageSize:  1,
	})
	require.NoError(t, err)
	require.Equal(t, int64(2), accountPage.Total)
	require.Equal(t, 2, accountPage.Pages)
	require.Len(t, accountPage.Items, 1)
	require.Equal(t, deletedAccountID, accountPage.Items[0].AccountID)
	require.Equal(t, fmt.Sprintf("#%d", deletedAccountID), accountPage.Items[0].AccountName)
	require.Equal(t, int64(7), accountPage.Items[0].Samples)
	require.Equal(t, 140.0, accountPage.Items[0].AvgTTFTMS)
	require.Equal(t, service.FirstTokenStatsRatio{Numerator: 2, Denominator: 7, Rate: 2.0 / 7.0}, accountPage.Items[0].TTFTTimeoutRate)

	searched, err := repo.QueryAccounts(ctx, service.FirstTokenStatsAccountFilter{
		Range:     service.FirstTokenStatsRange24Hours,
		End:       time.Date(2026, 7, 15, 5, 23, 0, 0, time.UTC),
		Protocol:  "openai_responses",
		Model:     model,
		Platform:  "openai",
		AccountID: deletedAccountID,
		Search:    fmt.Sprintf("#%d", deletedAccountID),
		SortBy:    service.FirstTokenStatsAccountSortSamples,
		Page:      1,
		PageSize:  20,
	})
	require.NoError(t, err)
	require.Equal(t, int64(1), searched.Total)
	require.Len(t, searched.Items, 1)
	require.Equal(t, deletedAccountID, searched.Items[0].AccountID)
}

func TestFirstTokenTimeoutStatsDeleteBeforeIsStrictAndIdempotent(t *testing.T) {
	tx := testTx(t)
	repo := newFirstTokenTimeoutStatsRepositoryWithSQL(tx)
	ctx := context.Background()
	cutoff := time.Date(2026, 7, 15, 5, 47, 0, 0, time.UTC)
	base := service.FirstTokenStatsDelta{
		Scope:          service.FirstTokenStatsScopeRequest,
		Protocol:       "openai_responses",
		Model:          "gpt-5.2",
		TimeoutSeconds: 30,
		Outcome:        service.FirstTokenStatsRequestSuccess,
		SampleCount:    1,
	}
	old := base
	old.BucketStart = time.Date(2026, 7, 15, 3, 0, 0, 0, time.UTC)
	boundary := base
	boundary.BucketStart = time.Date(2026, 7, 15, 5, 0, 0, 0, time.UTC)
	require.NoError(t, repo.UpsertBatch(ctx, []service.FirstTokenStatsDelta{old, boundary}))

	deleted, err := repo.DeleteBefore(ctx, cutoff)
	require.NoError(t, err)
	require.Equal(t, int64(1), deleted)
	deleted, err = repo.DeleteBefore(ctx, cutoff)
	require.NoError(t, err)
	require.Zero(t, deleted)

	var remaining int64
	require.NoError(t, tx.QueryRowContext(ctx, `
SELECT COUNT(*) FROM first_token_timeout_stats_hourly WHERE bucket_start = $1
`, boundary.BucketStart).Scan(&remaining))
	require.Equal(t, int64(1), remaining)
}

func TestFirstTokenTimeoutStatsMigrationRejectsCrossInvariantViolations(t *testing.T) {
	tests := map[string]struct {
		scope             string
		outcome           string
		failureKind       string
		sampleCount       int64
		ttftSampleCount   int64
		ttftSumMS         int64
		ttftMaxMS         int64
		ttftAffectedCount int64
	}{
		"ttft samples exceed samples": {
			scope: service.FirstTokenStatsScopeAttempt, outcome: service.FirstTokenStatsAttemptSuccess,
			sampleCount: 1, ttftSampleCount: 2,
		},
		"zero ttft samples carry metrics": {
			scope: service.FirstTokenStatsScopeAttempt, outcome: service.FirstTokenStatsAttemptSuccess,
			sampleCount: 1, ttftSumMS: 1, ttftMaxMS: 1,
		},
		"ttft max exceeds sum": {
			scope: service.FirstTokenStatsScopeAttempt, outcome: service.FirstTokenStatsAttemptSuccess,
			sampleCount: 1, ttftSampleCount: 1, ttftSumMS: 4, ttftMaxMS: 5,
		},
		"request carries ttft metrics": {
			scope: service.FirstTokenStatsScopeRequest, outcome: service.FirstTokenStatsRequestSuccess,
			sampleCount: 1, ttftSampleCount: 1,
		},
		"attempt carries affected count": {
			scope: service.FirstTokenStatsScopeAttempt, outcome: service.FirstTokenStatsAttemptSuccess,
			sampleCount: 1, ttftAffectedCount: 1,
		},
		"request success is affected": {
			scope: service.FirstTokenStatsScopeRequest, outcome: service.FirstTokenStatsRequestSuccess,
			sampleCount: 1, ttftAffectedCount: 1,
		},
		"recovered request affected mismatch": {
			scope: service.FirstTokenStatsScopeRequest, outcome: service.FirstTokenStatsRequestRecoveredAfterTTFT,
			sampleCount: 2, ttftAffectedCount: 1,
		},
		"other failure missing kind": {
			scope: service.FirstTokenStatsScopeRequest, outcome: service.FirstTokenStatsRequestOtherFailure,
			sampleCount: 1,
		},
		"success has failure kind": {
			scope: service.FirstTokenStatsScopeAttempt, outcome: service.FirstTokenStatsAttemptSuccess,
			failureKind: service.FirstTokenStatsFailureTransport, sampleCount: 1,
		},
		"ttft timeout has sample": {
			scope: service.FirstTokenStatsScopeAttempt, outcome: service.FirstTokenStatsAttemptTTFTTimeout,
			sampleCount: 1, ttftSampleCount: 1,
		},
	}

	for name, test := range tests {
		t.Run(name, func(t *testing.T) {
			tx := testTx(t)
			accountID := int64(1)
			platform := "openai"
			if test.scope == service.FirstTokenStatsScopeRequest {
				accountID = 0
				platform = ""
			}
			_, err := tx.ExecContext(context.Background(), `
INSERT INTO first_token_timeout_stats_hourly (
    bucket_start, scope, account_id, protocol, platform, model, timeout_seconds,
    outcome, failure_kind, sample_count, ttft_sample_count, ttft_sum_ms,
    ttft_max_ms, ttft_affected_count
) VALUES ($1, $2, $3, 'openai_responses', $4, 'gpt-5.2', 30, $5, $6, $7, $8, $9, $10, $11)
`, time.Date(2026, 7, 13, 1, 0, 0, 0, time.UTC), test.scope, accountID, platform,
				test.outcome, test.failureKind, test.sampleCount, test.ttftSampleCount,
				test.ttftSumMS, test.ttftMaxMS, test.ttftAffectedCount)
			require.Error(t, err)
		})
	}
}

func withFirstTokenStatsDelta(
	base service.FirstTokenStatsDelta,
	accountID int64,
	outcome string,
	failureKind string,
	sampleCount int64,
	ttftSampleCount int64,
	ttftSumMS int64,
	ttftMaxMS int64,
	ttftAffectedCount int64,
) service.FirstTokenStatsDelta {
	base.AccountID = accountID
	base.Outcome = outcome
	base.FailureKind = failureKind
	base.SampleCount = sampleCount
	base.TTFTSampleCount = ttftSampleCount
	base.TTFTSumMS = ttftSumMS
	base.TTFTMaxMS = ttftMaxMS
	base.TTFTAffectedCount = ttftAffectedCount
	return base
}

func TestFirstTokenTimeoutStatsUpsertBatchRollsBackWholeStatement(t *testing.T) {
	repo := newFirstTokenTimeoutStatsRepositoryWithSQL(integrationDB)
	bucket := time.Date(2026, 7, 15, 6, 0, 0, 0, time.UTC)
	_, err := integrationDB.ExecContext(context.Background(), `
DELETE FROM first_token_timeout_stats_hourly WHERE bucket_start = $1
`, bucket)
	require.NoError(t, err)
	t.Cleanup(func() {
		_, _ = integrationDB.ExecContext(context.Background(), `
DELETE FROM first_token_timeout_stats_hourly WHERE bucket_start = $1
`, bucket)
	})

	_, err = integrationDB.ExecContext(context.Background(), `
INSERT INTO first_token_timeout_stats_hourly (
    bucket_start, scope, account_id, protocol, platform, model, timeout_seconds,
    outcome, failure_kind, sample_count, ttft_sample_count, ttft_sum_ms,
    ttft_max_ms, ttft_affected_count
) VALUES ($1, 'attempt', 2, 'openai_responses', 'openai', 'gpt-5.2', 30,
          'success', '', $2, 0, 0, 0, 0)
`, bucket, int64(math.MaxInt64))
	require.NoError(t, err)

	deltas := []service.FirstTokenStatsDelta{
		{
			BucketStart:    bucket,
			Scope:          service.FirstTokenStatsScopeAttempt,
			AccountID:      1,
			Protocol:       "openai_responses",
			Platform:       "openai",
			Model:          "gpt-5.2",
			TimeoutSeconds: 30,
			Outcome:        service.FirstTokenStatsAttemptSuccess,
			SampleCount:    1,
		},
		{
			BucketStart:    bucket,
			Scope:          service.FirstTokenStatsScopeAttempt,
			AccountID:      2,
			Protocol:       "openai_responses",
			Platform:       "openai",
			Model:          "gpt-5.2",
			TimeoutSeconds: 30,
			Outcome:        service.FirstTokenStatsAttemptSuccess,
			SampleCount:    1,
		},
	}
	require.Error(t, repo.UpsertBatch(context.Background(), deltas))

	var count int64
	require.NoError(t, integrationDB.QueryRowContext(context.Background(), `
SELECT COUNT(*) FROM first_token_timeout_stats_hourly
WHERE bucket_start = $1 AND account_id = 1
`, bucket).Scan(&count))
	require.Zero(t, count)
}
