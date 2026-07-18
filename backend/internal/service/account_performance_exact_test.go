package service

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

type exactLatencyPerformanceRepoStub struct {
	overview           *AccountPerformanceOverview
	exact              *AccountPerformanceExactOverview
	exactAccounts      map[int64]AccountPerformanceExactLatency
	exactInvestigation map[time.Time]AccountPerformanceExactLatency
	exactErr           error
}

func (s *exactLatencyPerformanceRepoStub) UpsertMinuteBatch(context.Context, []AccountPerformanceDelta) error {
	return nil
}

func (s *exactLatencyPerformanceRepoStub) QueryOverview(context.Context, AccountPerformanceOverviewFilter) (*AccountPerformanceOverview, error) {
	return s.overview, nil
}

func (s *exactLatencyPerformanceRepoStub) QueryAccounts(context.Context, AccountPerformanceAccountFilter) (*AccountPerformanceAccountPage, error) {
	return &AccountPerformanceAccountPage{}, nil
}

func (s *exactLatencyPerformanceRepoStub) QueryInvestigation(context.Context, AccountPerformanceInvestigationFilter) (*AccountPerformanceInvestigation, error) {
	return &AccountPerformanceInvestigation{}, nil
}

func (s *exactLatencyPerformanceRepoStub) RollupClosedHours(context.Context, time.Time) error {
	return nil
}

func (s *exactLatencyPerformanceRepoStub) DeleteMinuteBefore(context.Context, time.Time) (int64, error) {
	return 0, nil
}

func (s *exactLatencyPerformanceRepoStub) DeleteHourlyBefore(context.Context, time.Time) (int64, error) {
	return 0, nil
}

func (s *exactLatencyPerformanceRepoStub) QueryExactOverviewLatency(context.Context, AccountPerformanceOverviewFilter) (*AccountPerformanceExactOverview, error) {
	return s.exact, s.exactErr
}

func (s *exactLatencyPerformanceRepoStub) QueryExactAccountsLatency(context.Context, AccountPerformanceAccountFilter) (map[int64]AccountPerformanceExactLatency, error) {
	return s.exactAccounts, s.exactErr
}

func (s *exactLatencyPerformanceRepoStub) QueryExactInvestigationLatency(context.Context, AccountPerformanceInvestigationFilter) (map[time.Time]AccountPerformanceExactLatency, error) {
	return s.exactInvestigation, s.exactErr
}

func TestAccountPerformanceAccountsAndInvestigationPreferExactUsageLogLatency(t *testing.T) {
	start := time.Date(2026, 7, 18, 0, 0, 0, 0, time.UTC)
	p95Bucketed := AccountPerformanceCounters{
		AttemptCount:    10,
		SuccessCount:    10,
		TTFTLatency:     AccountPerformanceLatencyHistogram{Samples: 10, LE5000MS: 9, LE30000MS: 9, GT30000MS: 1},
		DurationLatency: AccountPerformanceLatencyHistogram{Samples: 10, LE10000MS: 9, LE30000MS: 9, GT30000MS: 1},
	}
	repo := &exactLatencyPerformanceRepoStub{
		overview:           &AccountPerformanceOverview{},
		exactAccounts:      map[int64]AccountPerformanceExactLatency{42: {P95TTFTMS: 31454, P95DurationMS: 62768, TTFTSampleCount: 10, DurationSamples: 10}},
		exactInvestigation: map[time.Time]AccountPerformanceExactLatency{start: {P50TTFTMS: 3516, P95TTFTMS: 31454, P95DurationMS: 62768, TTFTSampleCount: 10, DurationSamples: 10}},
	}
	repo.overview = &AccountPerformanceOverview{Counters: p95Bucketed, TimePoints: []AccountPerformanceTimePoint{{BucketStart: start, Counters: p95Bucketed}}}
	// The stub returns this row through QueryAccounts below via a small wrapper.
	service := NewAccountPerformanceService(&exactLatencyAccountsRepoStub{exactLatencyPerformanceRepoStub: repo, row: AccountPerformanceAccount{AccountID: 42, Counters: p95Bucketed}}, nil)
	accounts, err := service.Accounts(context.Background(), AccountPerformanceAccountFilter{Start: start, End: start.Add(time.Hour), Page: 1, PageSize: 20})
	require.NoError(t, err)
	require.Len(t, accounts.Items, 1)
	require.EqualValues(t, 31454, accounts.Items[0].P95TTFTMS)
	require.EqualValues(t, 62768, accounts.Items[0].P95DurationMS)

	investigation, err := service.Investigation(context.Background(), AccountPerformanceInvestigationFilter{Start: start, End: start.Add(time.Hour), AccountID: 42})
	require.NoError(t, err)
	require.EqualValues(t, 31454, investigation.TimePoints[0].P95TTFTMS)
	require.EqualValues(t, 62768, investigation.TimePoints[0].P95DurationMS)
}

type exactLatencyAccountsRepoStub struct {
	*exactLatencyPerformanceRepoStub
	row AccountPerformanceAccount
}

func (s *exactLatencyAccountsRepoStub) QueryAccounts(context.Context, AccountPerformanceAccountFilter) (*AccountPerformanceAccountPage, error) {
	return &AccountPerformanceAccountPage{Rows: []AccountPerformanceAccount{s.row}, Page: 1, PageSize: 20}, nil
}

func (s *exactLatencyAccountsRepoStub) QueryInvestigation(context.Context, AccountPerformanceInvestigationFilter) (*AccountPerformanceInvestigation, error) {
	return &AccountPerformanceInvestigation{TimePoints: []AccountPerformanceTimePoint{{BucketStart: time.Date(2026, 7, 18, 0, 0, 0, 0, time.UTC), Counters: s.row.Counters}}}, nil
}

func TestAccountPerformanceExactLatencyErrorsFallBackToBuckets(t *testing.T) {
	start := time.Date(2026, 7, 18, 0, 0, 0, 0, time.UTC)
	counters := AccountPerformanceCounters{
		AttemptCount:    10,
		SuccessCount:    10,
		TTFTLatency:     AccountPerformanceLatencyHistogram{Samples: 10, LE5000MS: 10, LE30000MS: 10},
		DurationLatency: AccountPerformanceLatencyHistogram{Samples: 10, LE10000MS: 10, LE30000MS: 10},
	}
	repo := &exactLatencyPerformanceRepoStub{
		overview: &AccountPerformanceOverview{Counters: counters, TimePoints: []AccountPerformanceTimePoint{{BucketStart: start, Counters: counters}}},
		exactErr: errors.New("raw log query failed"),
	}
	result, err := NewAccountPerformanceService(repo, nil).Overview(context.Background(), AccountPerformanceOverviewFilter{Start: start, End: start.Add(time.Hour)})
	require.NoError(t, err)
	require.EqualValues(t, 5000, result.Summary.P95TTFTMS)
	require.EqualValues(t, 10000, result.Summary.P95DurationMS)
}

func TestAccountPerformanceOverviewPrefersExactUsageLogLatency(t *testing.T) {
	start := time.Date(2026, 7, 18, 0, 0, 0, 0, time.UTC)
	p95Bucketed := AccountPerformanceTimePoint{
		BucketStart: start,
		Counters: AccountPerformanceCounters{
			AttemptCount:    10,
			SuccessCount:    10,
			TTFTLatency:     AccountPerformanceLatencyHistogram{Samples: 10, LE5000MS: 5, LE30000MS: 9, GT30000MS: 1},
			DurationLatency: AccountPerformanceLatencyHistogram{Samples: 10, LE10000MS: 8, LE30000MS: 9, GT30000MS: 1},
		},
	}
	repo := &exactLatencyPerformanceRepoStub{
		overview: &AccountPerformanceOverview{Counters: p95Bucketed.Counters, TimePoints: []AccountPerformanceTimePoint{p95Bucketed}},
		exact: &AccountPerformanceExactOverview{
			Summary: AccountPerformanceExactLatency{P50TTFTMS: 3516, P95TTFTMS: 31454, P95DurationMS: 62768, TTFTSampleCount: 10, DurationSamples: 10},
			Trend: map[time.Time]AccountPerformanceExactLatency{
				start: {P50TTFTMS: 3516, P95TTFTMS: 31454, P95DurationMS: 62768, TTFTSampleCount: 10, DurationSamples: 10},
			},
		},
	}

	result, err := NewAccountPerformanceService(repo, nil).Overview(context.Background(), AccountPerformanceOverviewFilter{
		Start: start,
		End:   start.Add(time.Hour),
	})
	require.NoError(t, err)
	require.EqualValues(t, 3516, result.Summary.P50TTFTMS)
	require.EqualValues(t, 31454, result.Summary.P95TTFTMS)
	require.EqualValues(t, 62768, result.Summary.P95DurationMS)
	require.EqualValues(t, 31454, result.Trend[0].P95TTFTMS)
	require.EqualValues(t, 62768, result.Trend[0].P95DurationMS)
}
