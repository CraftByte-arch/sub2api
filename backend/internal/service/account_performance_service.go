package service

import (
	"context"
	"time"
)

const accountPerformanceLowSampleThreshold = 20

const accountPerformanceExactQueryTimeout = 2 * time.Second

// AccountPerformanceService applies stable metric definitions to the raw
// aggregate repository. It does not participate in gateway request handling.
type AccountPerformanceService struct {
	repo     AccountPerformanceRepository
	recorder AccountPerformanceRecorder
}

func NewAccountPerformanceService(repo AccountPerformanceRepository, recorder AccountPerformanceRecorder) *AccountPerformanceService {
	return &AccountPerformanceService{repo: repo, recorder: recorder}
}

type AccountPerformanceRatio struct {
	Numerator   int64   `json:"numerator"`
	Denominator int64   `json:"denominator"`
	Rate        float64 `json:"rate"`
}

type AccountPerformanceSummary struct {
	Attempts         int64                   `json:"attempts"`
	Availability     AccountPerformanceRatio `json:"availability"`
	FailureRate      AccountPerformanceRatio `json:"failure_rate"`
	P50TTFTMS        int64                   `json:"p50_ttft_ms"`
	P95TTFTMS        int64                   `json:"p95_ttft_ms"`
	P95DurationMS    int64                   `json:"p95_duration_ms"`
	TTFTTimeoutCount int64                   `json:"ttft_timeout_count"`
}

type AccountPerformanceOverviewResult struct {
	Summary          AccountPerformanceSummary          `json:"summary"`
	Trend            []AccountPerformanceTimePoint      `json:"trend"`
	CollectionHealth AccountPerformanceCollectionHealth `json:"collection_health"`
	CoverageStart    time.Time                          `json:"coverage_start"`
	CoverageEnd      time.Time                          `json:"coverage_end"`
}

type AccountPerformanceAccountResult struct {
	AccountPerformanceAccount
	P95TTFTMS     int64 `json:"p95_ttft_ms"`
	P95DurationMS int64 `json:"p95_duration_ms"`
	LowSample     bool  `json:"low_sample"`
}

type AccountPerformanceAccountsResult struct {
	Items            []AccountPerformanceAccountResult  `json:"items"`
	Total            int64                              `json:"total"`
	Page             int                                `json:"page"`
	PageSize         int                                `json:"page_size"`
	Pages            int                                `json:"pages"`
	CollectionHealth AccountPerformanceCollectionHealth `json:"collection_health"`
}

type AccountPerformanceInvestigationResult struct {
	AccountPerformanceInvestigation
	CollectionHealth AccountPerformanceCollectionHealth `json:"collection_health"`
}

func (s *AccountPerformanceService) Overview(ctx context.Context, filter AccountPerformanceOverviewFilter) (*AccountPerformanceOverviewResult, error) {
	if s == nil || s.repo == nil {
		return nil, ErrAccountPerformanceUnavailable
	}
	overview, err := s.repo.QueryOverview(ctx, filter)
	if err != nil {
		return nil, err
	}
	summary := accountPerformanceSummary(overview.Counters)
	trend := overview.TimePoints
	if overview.Counters.AttemptCount > 0 {
		if exact := s.queryExactOverviewLatency(ctx, filter); exact != nil {
			applyExactSummaryLatency(&summary, &exact.Summary)
			applyExactTrendLatency(trend, exact.Trend)
		}
	}
	return &AccountPerformanceOverviewResult{
		Summary:          summary,
		Trend:            trend,
		CollectionHealth: s.collectionHealth(),
		CoverageStart:    filter.Start.UTC(),
		CoverageEnd:      filter.End.UTC(),
	}, nil
}

func (s *AccountPerformanceService) Accounts(ctx context.Context, filter AccountPerformanceAccountFilter) (*AccountPerformanceAccountsResult, error) {
	if s == nil || s.repo == nil {
		return nil, ErrAccountPerformanceUnavailable
	}
	page, err := s.repo.QueryAccounts(ctx, filter)
	if err != nil {
		return nil, err
	}
	var exact map[int64]AccountPerformanceExactLatency
	if len(page.Rows) > 0 {
		exact = s.queryExactAccountsLatency(ctx, filter)
	}
	items := make([]AccountPerformanceAccountResult, 0, len(page.Rows))
	for _, row := range page.Rows {
		p95TTFT := row.Counters.TTFTLatency.Percentile(0.95)
		p95Duration := row.Counters.DurationLatency.Percentile(0.95)
		if latency, ok := exact[row.AccountID]; ok {
			if latency.TTFTSampleCount > 0 {
				p95TTFT = latency.P95TTFTMS
			}
			if latency.DurationSamples > 0 {
				p95Duration = latency.P95DurationMS
			}
		}
		items = append(items, AccountPerformanceAccountResult{
			AccountPerformanceAccount: row,
			P95TTFTMS:                 p95TTFT, P95DurationMS: p95Duration,
			LowSample: row.Counters.AttemptCount < accountPerformanceLowSampleThreshold,
		})
	}
	return &AccountPerformanceAccountsResult{Items: items, Total: page.Total, Page: page.Page, PageSize: page.PageSize, Pages: page.Pages, CollectionHealth: s.collectionHealth()}, nil
}

func (s *AccountPerformanceService) Investigation(ctx context.Context, filter AccountPerformanceInvestigationFilter) (*AccountPerformanceInvestigationResult, error) {
	if s == nil || s.repo == nil {
		return nil, ErrAccountPerformanceUnavailable
	}
	result, err := s.repo.QueryInvestigation(ctx, filter)
	if err != nil {
		return nil, err
	}
	if len(result.TimePoints) > 0 {
		s.applyExactInvestigationLatency(ctx, result, filter)
	}
	return &AccountPerformanceInvestigationResult{AccountPerformanceInvestigation: *result, CollectionHealth: s.collectionHealth()}, nil
}

func (s *AccountPerformanceService) exactLatencyRepository() (accountPerformanceExactLatencyRepository, bool) {
	if s == nil || s.repo == nil {
		return nil, false
	}
	repo, ok := s.repo.(accountPerformanceExactLatencyRepository)
	return repo, ok && repo != nil
}

func exactLatencyContext(parent context.Context) (context.Context, context.CancelFunc) {
	if parent == nil {
		parent = context.Background()
	}
	return context.WithTimeout(parent, accountPerformanceExactQueryTimeout)
}

func (s *AccountPerformanceService) queryExactOverviewLatency(ctx context.Context, filter AccountPerformanceOverviewFilter) *AccountPerformanceExactOverview {
	repo, ok := s.exactLatencyRepository()
	if !ok {
		return nil
	}
	queryCtx, cancel := exactLatencyContext(ctx)
	defer cancel()
	exact, err := repo.QueryExactOverviewLatency(queryCtx, filter)
	if err != nil || exact == nil {
		return nil
	}
	return exact
}

func (s *AccountPerformanceService) queryExactAccountsLatency(ctx context.Context, filter AccountPerformanceAccountFilter) map[int64]AccountPerformanceExactLatency {
	repo, ok := s.exactLatencyRepository()
	if !ok {
		return nil
	}
	queryCtx, cancel := exactLatencyContext(ctx)
	defer cancel()
	exact, err := repo.QueryExactAccountsLatency(queryCtx, filter)
	if err != nil {
		return nil
	}
	return exact
}

func (s *AccountPerformanceService) queryExactInvestigationLatency(ctx context.Context, filter AccountPerformanceInvestigationFilter) map[time.Time]AccountPerformanceExactLatency {
	repo, ok := s.exactLatencyRepository()
	if !ok {
		return nil
	}
	queryCtx, cancel := exactLatencyContext(ctx)
	defer cancel()
	exact, err := repo.QueryExactInvestigationLatency(queryCtx, filter)
	if err != nil {
		return nil
	}
	return exact
}

func (s *AccountPerformanceService) applyExactInvestigationLatency(ctx context.Context, investigation *AccountPerformanceInvestigation, filter AccountPerformanceInvestigationFilter) {
	if investigation == nil {
		return
	}
	exact := s.queryExactInvestigationLatency(ctx, filter)
	applyExactTrendLatency(investigation.TimePoints, exact)
}

func applyExactSummaryLatency(summary *AccountPerformanceSummary, exact *AccountPerformanceExactLatency) {
	if summary == nil || exact == nil {
		return
	}
	if exact.TTFTSampleCount > 0 {
		summary.P50TTFTMS = exact.P50TTFTMS
		summary.P95TTFTMS = exact.P95TTFTMS
	}
	if exact.DurationSamples > 0 {
		summary.P95DurationMS = exact.P95DurationMS
	}
}

func applyExactTrendLatency(points []AccountPerformanceTimePoint, exact map[time.Time]AccountPerformanceExactLatency) {
	if len(points) == 0 || len(exact) == 0 {
		return
	}
	for i := range points {
		latency, ok := exact[points[i].BucketStart.UTC()]
		if !ok {
			// Repositories may return a timestamp with sub-minute precision; the
			// aggregate trend is minute/hour aligned, so try the normalized key.
			latency, ok = exact[points[i].BucketStart.UTC().Truncate(time.Minute)]
		}
		if !ok {
			continue
		}
		if latency.TTFTSampleCount > 0 {
			points[i].P50TTFTMS = latency.P50TTFTMS
			points[i].P95TTFTMS = latency.P95TTFTMS
		}
		if latency.DurationSamples > 0 {
			points[i].P95DurationMS = latency.P95DurationMS
		}
	}
}

func (s *AccountPerformanceService) collectionHealth() AccountPerformanceCollectionHealth {
	if s == nil || s.recorder == nil {
		return AccountPerformanceCollectionHealth{Status: AccountPerformanceCollectionDegraded}
	}
	return s.recorder.Health()
}

func (s *AccountPerformanceService) CollectionHealth() AccountPerformanceCollectionHealth {
	return s.collectionHealth()
}

func accountPerformanceSummary(counters AccountPerformanceCounters) AccountPerformanceSummary {
	denominator := counters.AttemptCount - counters.ClientCanceledCount
	failures := denominator - counters.SuccessCount
	return AccountPerformanceSummary{
		Attempts:     counters.AttemptCount,
		Availability: accountPerformanceRatio(counters.SuccessCount, denominator),
		FailureRate:  accountPerformanceRatio(failures, denominator),
		P50TTFTMS:    counters.TTFTLatency.Percentile(0.50), P95TTFTMS: counters.TTFTLatency.Percentile(0.95),
		P95DurationMS: counters.DurationLatency.Percentile(0.95), TTFTTimeoutCount: counters.TTFTTimeoutCount,
	}
}

func accountPerformanceRatio(numerator, denominator int64) AccountPerformanceRatio {
	if denominator <= 0 {
		return AccountPerformanceRatio{Numerator: numerator, Denominator: denominator}
	}
	return AccountPerformanceRatio{Numerator: numerator, Denominator: denominator, Rate: float64(numerator) / float64(denominator)}
}
