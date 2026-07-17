package service

import (
	"context"
	"time"
)

const accountPerformanceLowSampleThreshold = 20

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
	return &AccountPerformanceOverviewResult{
		Summary:          accountPerformanceSummary(overview.Counters),
		Trend:            overview.TimePoints,
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
	items := make([]AccountPerformanceAccountResult, 0, len(page.Rows))
	for _, row := range page.Rows {
		items = append(items, AccountPerformanceAccountResult{
			AccountPerformanceAccount: row,
			P95TTFTMS:                 row.Counters.TTFTLatency.Percentile(0.95), P95DurationMS: row.Counters.DurationLatency.Percentile(0.95),
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
	return &AccountPerformanceInvestigationResult{AccountPerformanceInvestigation: *result, CollectionHealth: s.collectionHealth()}, nil
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
