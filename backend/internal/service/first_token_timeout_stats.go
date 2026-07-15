package service

import (
	"context"
	"time"
)

const (
	FirstTokenStatsScopeAttempt = "attempt"
	FirstTokenStatsScopeRequest = "request"

	FirstTokenStatsProtocolMaxRunes = 32
	FirstTokenStatsModelMaxRunes    = 255
	FirstTokenStatsPlatformMaxRunes = 32
	FirstTokenStatsSearchMaxRunes   = 255

	FirstTokenStatsAttemptSuccess        = "success"
	FirstTokenStatsAttemptTTFTTimeout    = "ttft_timeout"
	FirstTokenStatsAttemptClientCanceled = "client_canceled"
	FirstTokenStatsAttemptOtherFailure   = "other_failure"

	FirstTokenStatsRequestSuccess            = "success"
	FirstTokenStatsRequestRecoveredAfterTTFT = "recovered_after_ttft"
	FirstTokenStatsRequestTTFTExhausted      = "ttft_exhausted"
	FirstTokenStatsRequestClientCanceled     = "client_canceled"
	FirstTokenStatsRequestOtherFailure       = "other_failure"

	FirstTokenStatsFailureRateLimit         = "rate_limit"
	FirstTokenStatsFailureAuth              = "auth"
	FirstTokenStatsFailureUpstream4xx       = "upstream_4xx"
	FirstTokenStatsFailureUpstream5xx       = "upstream_5xx"
	FirstTokenStatsFailureTransport         = "transport"
	FirstTokenStatsFailureStreamIdleTimeout = "stream_idle_timeout"
	FirstTokenStatsFailureProtocol          = "protocol"
	FirstTokenStatsFailureOther             = "other"
)

type FirstTokenStatsDelta struct {
	BucketStart       time.Time
	Scope             string
	AccountID         int64
	Protocol          string
	Platform          string
	Model             string
	TimeoutSeconds    int
	Outcome           string
	FailureKind       string
	SampleCount       int64
	TTFTSampleCount   int64
	TTFTSumMS         int64
	TTFTMaxMS         int64
	TTFTAffectedCount int64
}

type FirstTokenStatsRange string

const (
	FirstTokenStatsRange24Hours FirstTokenStatsRange = "24h"
	FirstTokenStatsRange7Days   FirstTokenStatsRange = "7d"
	FirstTokenStatsRange30Days  FirstTokenStatsRange = "30d"
	FirstTokenStatsRange90Days  FirstTokenStatsRange = "90d"
)

type FirstTokenStatsOverviewFilter struct {
	Range    FirstTokenStatsRange
	End      time.Time
	Protocol string
	Model    string
}

type FirstTokenStatsRatio struct {
	Numerator   int64
	Denominator int64
	Rate        float64
}

type FirstTokenStatsSummary struct {
	ControlledRequests     int64
	ClientCanceledRequests int64
	AttemptTTFTTimeoutRate FirstTokenStatsRatio
	RecoveryRate           FirstTokenStatsRatio
	FinalTTFTFailureRate   FirstTokenStatsRatio
	OtherFinalFailureRate  FirstTokenStatsRatio
}

type FirstTokenStatsTrendPoint struct {
	BucketStart            time.Time
	AttemptTTFTTimeoutRate FirstTokenStatsRatio
	RecoveryRate           FirstTokenStatsRatio
	FinalTTFTFailureRate   FirstTokenStatsRatio
	OtherFinalFailureRate  FirstTokenStatsRatio
}

type FirstTokenStatsFailureDistribution struct {
	FailureKind string
	SampleCount int64
}

type FirstTokenStatsOverview struct {
	Summary       FirstTokenStatsSummary
	Trend         []FirstTokenStatsTrendPoint
	OtherFailures []FirstTokenStatsFailureDistribution
}

const (
	FirstTokenStatsAccountSortSamples           = "samples"
	FirstTokenStatsAccountSortSuccess           = "success"
	FirstTokenStatsAccountSortTTFTTimeoutCount  = "ttft_timeout_count"
	FirstTokenStatsAccountSortTTFTTimeoutRate   = "ttft_timeout_rate"
	FirstTokenStatsAccountSortOtherFailureCount = "other_failure_count"
	FirstTokenStatsAccountSortOtherFailureRate  = "other_failure_rate"
	FirstTokenStatsAccountSortAvgTTFTMS         = "avg_ttft_ms"
)

type FirstTokenStatsAccountFilter struct {
	Range     FirstTokenStatsRange
	End       time.Time
	Protocol  string
	Model     string
	Platform  string
	AccountID int64
	Search    string
	SortBy    string
	SortOrder string
	Page      int
	PageSize  int
}

type FirstTokenStatsAccount struct {
	AccountID         int64
	AccountName       string
	Platform          string
	Samples           int64
	SuccessCount      int64
	TTFTTimeoutCount  int64
	TTFTTimeoutRate   FirstTokenStatsRatio
	OtherFailureCount int64
	OtherFailureRate  FirstTokenStatsRatio
	AvgTTFTMS         float64
	LowSample         bool
}

type FirstTokenStatsAccountPage struct {
	Items    []FirstTokenStatsAccount
	Total    int64
	Page     int
	PageSize int
	Pages    int
}

type FirstTokenTimeoutStatsRepository interface {
	UpsertBatch(ctx context.Context, deltas []FirstTokenStatsDelta) error
	QueryOverview(ctx context.Context, filter FirstTokenStatsOverviewFilter) (*FirstTokenStatsOverview, error)
	QueryAccounts(ctx context.Context, filter FirstTokenStatsAccountFilter) (*FirstTokenStatsAccountPage, error)
	DeleteBefore(ctx context.Context, cutoff time.Time) (int64, error)
}
