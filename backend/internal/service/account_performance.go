package service

import (
	"context"
	"errors"
	"math"
	"net/http"
	"strings"
	"time"
)

var ErrAccountPerformanceUnavailable = errors.New("account performance is unavailable")

const (
	AccountPerformanceOutcomeSuccess        = "success"
	AccountPerformanceOutcomeTTFTTimeout    = "ttft_timeout"
	AccountPerformanceOutcomeRateLimit      = "rate_limit"
	AccountPerformanceOutcomeAuth           = "auth"
	AccountPerformanceOutcomeUpstream4xx    = "upstream_4xx"
	AccountPerformanceOutcomeUpstream5xx    = "upstream_5xx"
	AccountPerformanceOutcomeTransport      = "transport"
	AccountPerformanceOutcomeProtocol       = "protocol"
	AccountPerformanceOutcomeOtherFailure   = "other_failure"
	AccountPerformanceOutcomeClientCanceled = "client_canceled"
)

// AccountPerformanceRepository persists bounded account attempt aggregates.
// Query callers must supply an explicit start and end time.
type AccountPerformanceRepository interface {
	UpsertMinuteBatch(ctx context.Context, deltas []AccountPerformanceDelta) error
	RollupClosedHours(ctx context.Context, before time.Time) error
	DeleteMinuteBefore(ctx context.Context, cutoff time.Time) (int64, error)
	DeleteHourlyBefore(ctx context.Context, cutoff time.Time) (int64, error)
	QueryOverview(ctx context.Context, filter AccountPerformanceOverviewFilter) (*AccountPerformanceOverview, error)
	QueryAccounts(ctx context.Context, filter AccountPerformanceAccountFilter) (*AccountPerformanceAccountPage, error)
	QueryInvestigation(ctx context.Context, filter AccountPerformanceInvestigationFilter) (*AccountPerformanceInvestigation, error)
}

const (
	AccountPerformanceCollectionComplete = "complete"
	AccountPerformanceCollectionDegraded = "degraded"
)

// AccountPerformanceRecorder accepts completed upstream-attempt measurements.
// Recording is best effort: it must never add database latency to a gateway
// request.
type AccountPerformanceRecorder interface {
	Record(AccountPerformanceDelta)
	Health() AccountPerformanceCollectionHealth
}

type AccountPerformanceCollectionHealth struct {
	Status                string     `json:"status"`
	DroppedSamples        int64      `json:"dropped_samples"`
	PendingSamples        int64      `json:"pending_samples"`
	LastSuccessfulFlushAt *time.Time `json:"last_successful_flush_at"`
}

// AccountPerformanceCounters contains raw additive metrics. Percentiles and health
// scores are deliberately derived by the caller from these stable values.
type AccountPerformanceCounters struct {
	AttemptCount        int64                              `json:"attempt_count"`
	SuccessCount        int64                              `json:"success_count"`
	ClientCanceledCount int64                              `json:"client_canceled_count"`
	TTFTTimeoutCount    int64                              `json:"ttft_timeout_count"`
	RateLimitCount      int64                              `json:"rate_limit_count"`
	AuthCount           int64                              `json:"auth_count"`
	Upstream4xxCount    int64                              `json:"upstream_4xx_count"`
	Upstream5xxCount    int64                              `json:"upstream_5xx_count"`
	TransportCount      int64                              `json:"transport_count"`
	ProtocolCount       int64                              `json:"protocol_count"`
	OtherFailureCount   int64                              `json:"other_failure_count"`
	FailoverCount       int64                              `json:"failover_count"`
	TTFTSumMS           int64                              `json:"ttft_sum_ms"`
	DurationSumMS       int64                              `json:"duration_sum_ms"`
	TTFTLatency         AccountPerformanceLatencyHistogram `json:"ttft_latency"`
	DurationLatency     AccountPerformanceLatencyHistogram `json:"duration_latency"`
}

type AccountPerformanceOverviewFilter struct {
	Start     time.Time
	End       time.Time
	Platform  string
	GroupID   int64
	Model     string
	Protocol  string
	AccountID int64
}

// AccountPerformanceExactLatency contains percentile values calculated from
// the request-level usage log. The bounded aggregate histogram remains the
// fallback when the raw-log query is unavailable or has no samples.
type AccountPerformanceExactLatency struct {
	P50TTFTMS       int64
	P95TTFTMS       int64
	P95DurationMS   int64
	TTFTSampleCount int64
	DurationSamples int64
}

type AccountPerformanceExactOverview struct {
	Summary AccountPerformanceExactLatency
	Trend   map[time.Time]AccountPerformanceExactLatency
}

// accountPerformanceExactLatencyRepository is intentionally optional. This
// keeps the aggregate repository contract and existing test doubles stable;
// callers fall back to bucket percentiles if a repository does not implement it.
type accountPerformanceExactLatencyRepository interface {
	QueryExactOverviewLatency(context.Context, AccountPerformanceOverviewFilter) (*AccountPerformanceExactOverview, error)
	QueryExactAccountsLatency(context.Context, AccountPerformanceAccountFilter) (map[int64]AccountPerformanceExactLatency, error)
	QueryExactInvestigationLatency(context.Context, AccountPerformanceInvestigationFilter) (map[time.Time]AccountPerformanceExactLatency, error)
}

type AccountPerformanceOverview struct {
	Counters   AccountPerformanceCounters
	TimePoints []AccountPerformanceTimePoint
}

type AccountPerformanceTimePoint struct {
	BucketStart   time.Time                  `json:"bucket_start"`
	Counters      AccountPerformanceCounters `json:"counters"`
	P50TTFTMS     int64                      `json:"p50_ttft_ms,omitempty"`
	P95TTFTMS     int64                      `json:"p95_ttft_ms,omitempty"`
	P95DurationMS int64                      `json:"p95_duration_ms,omitempty"`
}

const (
	AccountPerformanceSortHealthScore   = "health_score"
	AccountPerformanceSortAvailability  = "availability"
	AccountPerformanceSortFailureRate   = "failure_rate"
	AccountPerformanceSortP95TTFTMS     = "p95_ttft_ms"
	AccountPerformanceSortP95DurationMS = "p95_duration_ms"
	AccountPerformanceSortSamples       = "samples"
	AccountPerformanceSortSuccessCount  = "success_count"
	AccountPerformanceSortFailureCount  = "failure_count"
)

type AccountPerformanceAccountFilter struct {
	Start     time.Time
	End       time.Time
	Platform  string
	GroupID   int64
	Model     string
	Protocol  string
	AccountID int64
	Search    string
	SortBy    string
	SortOrder string
	Page      int
	PageSize  int
}

type AccountPerformanceAccount struct {
	AccountID    int64                      `json:"account_id"`
	AccountName  string                     `json:"account_name"`
	AccountType  string                     `json:"account_type"`
	AuthMode     string                     `json:"auth_mode,omitempty"`
	Platform     string                     `json:"platform"`
	Counters     AccountPerformanceCounters `json:"counters"`
	Availability float64                    `json:"availability"`
	FailureRate  float64                    `json:"failure_rate"`
	HealthScore  float64                    `json:"health_score"`
}

type AccountPerformanceAccountPage struct {
	Rows     []AccountPerformanceAccount
	Total    int64
	Page     int
	PageSize int
	Pages    int
}

type AccountPerformanceInvestigationFilter struct {
	Start     time.Time
	End       time.Time
	Platform  string
	GroupID   int64
	Model     string
	Protocol  string
	AccountID int64
}

type AccountPerformanceFailureBreakdown struct {
	Outcome string
	Count   int64
}

type AccountPerformanceInvestigation struct {
	TimePoints []AccountPerformanceTimePoint
	Failures   []AccountPerformanceFailureBreakdown
}

// AccountPerformanceDelta is a single attempt's contribution to an aggregate bucket.
// TTFTMS and DurationMS are recorded only for successful attempts.
type AccountPerformanceDelta struct {
	BucketStart  time.Time
	AccountID    int64
	Platform     string
	GroupID      int64
	Model        string
	Protocol     string
	Outcome      string
	AttemptCount int64
	TTFTMS       *int64
	DurationMS   *int64
	Failover     bool
}

// AccountPerformanceLatencyHistogram stores cumulative latency buckets used by both
// TTFT and request-duration aggregates.
type AccountPerformanceLatencyHistogram struct {
	Samples   int64
	LE1000MS  int64
	LE2500MS  int64
	LE5000MS  int64
	LE10000MS int64
	LE30000MS int64
	GT30000MS int64
}

// Add records one non-negative latency value in the cumulative histogram buckets.
func (h *AccountPerformanceLatencyHistogram) Add(milliseconds int64) {
	if milliseconds < 0 {
		return
	}

	h.Samples++
	if milliseconds <= 1000 {
		h.LE1000MS++
	}
	if milliseconds <= 2500 {
		h.LE2500MS++
	}
	if milliseconds <= 5000 {
		h.LE5000MS++
	}
	if milliseconds <= 10000 {
		h.LE10000MS++
	}
	if milliseconds <= 30000 {
		h.LE30000MS++
	} else {
		h.GT30000MS++
	}
}

// Percentile returns the upper boundary of the bucket containing the requested
// percentile. Values above 30 seconds use 30001 to represent the open-ended bucket.
// It returns zero for empty histograms or an invalid percentile.
func (h AccountPerformanceLatencyHistogram) Percentile(percentile float64) int64 {
	if percentile <= 0 || percentile > 1 {
		return 0
	}

	if h.Samples <= 0 {
		return 0
	}

	rank := int64(math.Ceil(percentile * float64(h.Samples)))
	for _, bucket := range []struct {
		upper      int64
		cumulative int64
	}{
		{upper: 1000, cumulative: h.LE1000MS},
		{upper: 2500, cumulative: h.LE2500MS},
		{upper: 5000, cumulative: h.LE5000MS},
		{upper: 10000, cumulative: h.LE10000MS},
		{upper: 30000, cumulative: h.LE30000MS},
	} {
		if rank <= bucket.cumulative {
			return bucket.upper
		}
	}

	if rank <= h.LE30000MS+h.GT30000MS {
		return 30001
	}
	return 0
}

// TTFTLatencySample returns the actual TTFT only when this is a successful attempt.
func (d AccountPerformanceDelta) TTFTLatencySample() (int64, bool) {
	if d.Outcome != AccountPerformanceOutcomeSuccess || d.TTFTMS == nil || *d.TTFTMS < 0 {
		return 0, false
	}
	return *d.TTFTMS, true
}

// DurationLatencySample returns the actual duration only when this is a successful attempt.
func (d AccountPerformanceDelta) DurationLatencySample() (int64, bool) {
	if d.Outcome != AccountPerformanceOutcomeSuccess || d.DurationMS == nil || *d.DurationMS < 0 {
		return 0, false
	}
	return *d.DurationMS, true
}

// ClassifyAccountPerformanceOutcome maps terminal attempt errors to stable metric labels.
func ClassifyAccountPerformanceOutcome(err error, parent context.Context) string {
	if parent != nil && parent.Err() != nil {
		return AccountPerformanceOutcomeClientCanceled
	}
	if err == nil {
		return AccountPerformanceOutcomeSuccess
	}
	if errors.Is(err, ErrFirstTokenTimeout) {
		return AccountPerformanceOutcomeTTFTTimeout
	}

	var failoverErr *UpstreamFailoverError
	if errors.As(err, &failoverErr) {
		errorType := strings.ToLower(strings.TrimSpace(failoverErr.ErrorType))
		switch {
		case errorType == UpstreamErrorTypeFirstTokenTimeout:
			return AccountPerformanceOutcomeTTFTTimeout
		case failoverErr.StatusCode == http.StatusTooManyRequests:
			return AccountPerformanceOutcomeRateLimit
		case failoverErr.StatusCode == http.StatusUnauthorized || failoverErr.StatusCode == http.StatusForbidden:
			return AccountPerformanceOutcomeAuth
		case isFirstTokenRateLimitErrorType(errorType):
			return AccountPerformanceOutcomeRateLimit
		case isFirstTokenAuthErrorType(errorType):
			return AccountPerformanceOutcomeAuth
		case errorType == UpstreamErrorTypeFirstTokenPreludeOverflow:
			return AccountPerformanceOutcomeProtocol
		case failoverErr.StatusCode >= http.StatusBadRequest && failoverErr.StatusCode < http.StatusInternalServerError:
			return AccountPerformanceOutcomeUpstream4xx
		case failoverErr.StatusCode >= http.StatusInternalServerError && failoverErr.StatusCode < 600:
			return AccountPerformanceOutcomeUpstream5xx
		}
	}

	if isFirstTokenTransportError(err) {
		return AccountPerformanceOutcomeTransport
	}
	if isFirstTokenProtocolError(err, failoverErr) {
		return AccountPerformanceOutcomeProtocol
	}
	return AccountPerformanceOutcomeOtherFailure
}
