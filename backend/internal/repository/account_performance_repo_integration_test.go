//go:build integration

package repository

import (
	"context"
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/stretchr/testify/require"
)

func TestAccountPerformanceRollupClosedHoursIsIdempotent(t *testing.T) {
	ctx := context.Background()
	previousNow := accountPerformanceNow
	accountPerformanceNow = func() time.Time { return time.Date(2026, 7, 17, 11, 0, 0, 0, time.UTC) }
	t.Cleanup(func() { accountPerformanceNow = previousNow })
	repo := NewAccountPerformanceRepository(integrationDB)
	bucket := time.Date(2026, 7, 17, 8, 12, 0, 0, time.UTC)
	ttftA, durationA := int64(900), int64(2_000)
	ttftB, durationB := int64(2_000), int64(35_000)
	model := "account-performance-rollup-idempotence"
	_, err := integrationDB.ExecContext(ctx, `DELETE FROM account_performance_minute WHERE model = $1`, model)
	require.NoError(t, err)
	_, err = integrationDB.ExecContext(ctx, `DELETE FROM account_performance_hourly WHERE model = $1`, model)
	require.NoError(t, err)
	t.Cleanup(func() {
		_, _ = integrationDB.ExecContext(context.Background(), `DELETE FROM account_performance_minute WHERE model = $1`, model)
		_, _ = integrationDB.ExecContext(context.Background(), `DELETE FROM account_performance_hourly WHERE model = $1`, model)
	})

	require.NoError(t, repo.UpsertMinuteBatch(ctx, []service.AccountPerformanceDelta{
		{BucketStart: bucket, AccountID: 901, Platform: "openai", GroupID: 1, Model: model, Protocol: "responses", Outcome: service.AccountPerformanceOutcomeSuccess, AttemptCount: 1, TTFTMS: &ttftA, DurationMS: &durationA},
		{BucketStart: bucket.Add(time.Minute), AccountID: 901, Platform: "openai", GroupID: 1, Model: model, Protocol: "responses", Outcome: service.AccountPerformanceOutcomeSuccess, AttemptCount: 1, TTFTMS: &ttftB, DurationMS: &durationB},
		{BucketStart: bucket.Add(2 * time.Minute), AccountID: 901, Platform: "openai", GroupID: 1, Model: model, Protocol: "responses", Outcome: service.AccountPerformanceOutcomeRateLimit, AttemptCount: 1},
		{BucketStart: bucket.Add(3 * time.Minute), AccountID: 901, Platform: "openai", GroupID: 1, Model: model, Protocol: "responses", Outcome: service.AccountPerformanceOutcomeRateLimit, AttemptCount: 1},
		{BucketStart: bucket.Add(4 * time.Minute), AccountID: 901, Platform: "openai", GroupID: 1, Model: model, Protocol: "responses", Outcome: service.AccountPerformanceOutcomeRateLimit, AttemptCount: 1},
	}))

	before := bucket.Add(2 * time.Hour)
	require.NoError(t, repo.RollupClosedHours(ctx, before))
	require.NoError(t, repo.RollupClosedHours(ctx, before))
	var rows, attempts, ttftSamples, ttftLE1000, ttftLE2500, durationSamples, durationLE2500, durationLE30000, durationGT30000 int64
	require.NoError(t, integrationDB.QueryRowContext(ctx, `
SELECT COUNT(*), COALESCE(SUM(attempt_count), 0),
       COALESCE(SUM(ttft_sample_count), 0), COALESCE(SUM(ttft_le_1000_ms), 0), COALESCE(SUM(ttft_le_2500_ms), 0),
       COALESCE(SUM(duration_sample_count), 0), COALESCE(SUM(duration_le_2500_ms), 0), COALESCE(SUM(duration_le_30000_ms), 0), COALESCE(SUM(duration_gt_30000_ms), 0)
FROM account_performance_hourly
WHERE bucket_start = $1 AND model = $2`, bucket.Truncate(time.Hour), model).Scan(&rows, &attempts, &ttftSamples, &ttftLE1000, &ttftLE2500, &durationSamples, &durationLE2500, &durationLE30000, &durationGT30000))
	require.EqualValues(t, 2, rows)
	require.EqualValues(t, 5, attempts)
	require.EqualValues(t, 2, ttftSamples)
	require.EqualValues(t, 1, ttftLE1000)
	require.EqualValues(t, 2, ttftLE2500)
	require.EqualValues(t, 2, durationSamples)
	require.EqualValues(t, 1, durationLE2500)
	require.EqualValues(t, 1, durationLE30000)
	require.EqualValues(t, 1, durationGT30000)
}

func TestAccountPerformanceAvailabilityExcludesClientCancellations(t *testing.T) {
	ctx := context.Background()
	previousNow := accountPerformanceNow
	accountPerformanceNow = func() time.Time { return time.Date(2026, 7, 17, 11, 0, 0, 0, time.UTC) }
	t.Cleanup(func() { accountPerformanceNow = previousNow })
	repo := NewAccountPerformanceRepository(integrationDB)
	bucket := time.Date(2026, 7, 17, 8, 12, 0, 0, time.UTC)
	model := "account-performance-cancellation-denominator"
	_, err := integrationDB.ExecContext(ctx, `DELETE FROM account_performance_minute WHERE model = $1`, model)
	require.NoError(t, err)
	t.Cleanup(func() {
		_, _ = integrationDB.ExecContext(context.Background(), `DELETE FROM account_performance_minute WHERE model = $1`, model)
	})

	deltas := []service.AccountPerformanceDelta{{
		BucketStart: bucket, AccountID: 902, Platform: "openai", GroupID: 1, Model: model, Protocol: "responses", Outcome: service.AccountPerformanceOutcomeSuccess, AttemptCount: 1,
	}}
	for i := 0; i < 9; i++ {
		deltas = append(deltas, service.AccountPerformanceDelta{
			BucketStart: bucket.Add(time.Duration(i+1) * time.Minute), AccountID: 902, Platform: "openai", GroupID: 1, Model: model, Protocol: "responses", Outcome: service.AccountPerformanceOutcomeClientCanceled, AttemptCount: 1,
		})
	}
	require.NoError(t, repo.UpsertMinuteBatch(ctx, deltas))

	page, err := repo.QueryAccounts(ctx, service.AccountPerformanceAccountFilter{
		Start: bucket.Add(-time.Minute), End: bucket.Add(time.Hour), AccountID: 902, Model: model,
		SortBy: service.AccountPerformanceSortAvailability, SortOrder: "desc", Page: 1, PageSize: 10,
	})
	require.NoError(t, err)
	require.Len(t, page.Rows, 1)
	require.Equal(t, 1.0, page.Rows[0].Availability)
	require.Equal(t, 0.0, page.Rows[0].FailureRate)
}
