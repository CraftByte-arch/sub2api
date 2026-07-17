package service

import (
	"context"
	"errors"
	"io"
	"net"
	"net/http"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestAccountPerformanceHistogramPercentile(t *testing.T) {
	histogram := AccountPerformanceLatencyHistogram{
		Samples:   10,
		LE1000MS:  2,
		LE2500MS:  5,
		LE5000MS:  9,
		LE10000MS: 10,
	}

	p50 := histogram.Percentile(0.50)
	require.EqualValues(t, 2500, p50)

	p95 := histogram.Percentile(0.95)
	require.EqualValues(t, 10000, p95)

	require.Zero(t, AccountPerformanceLatencyHistogram{}.Percentile(0.95))
	require.Zero(t, histogram.Percentile(1.01))
}

func TestAccountPerformanceLatencyHistogramAddUsesCumulativeBuckets(t *testing.T) {
	var histogram AccountPerformanceLatencyHistogram
	histogram.Add(900)
	histogram.Add(2_000)
	histogram.Add(40_000)

	require.EqualValues(t, 3, histogram.Samples)
	require.EqualValues(t, 1, histogram.LE1000MS)
	require.EqualValues(t, 2, histogram.LE2500MS)
	require.EqualValues(t, 2, histogram.LE5000MS)
	require.EqualValues(t, 2, histogram.LE10000MS)
	require.EqualValues(t, 2, histogram.LE30000MS)
	require.EqualValues(t, 1, histogram.GT30000MS)
	require.EqualValues(t, 30001, histogram.Percentile(0.99))
}

func TestAccountPerformanceHistogramPercentileUsesThirtySecondAndOpenEndedBuckets(t *testing.T) {
	histogram := AccountPerformanceLatencyHistogram{
		Samples:   2,
		LE30000MS: 1,
		GT30000MS: 1,
	}

	require.EqualValues(t, 30000, histogram.Percentile(0.50))
	require.EqualValues(t, 30001, histogram.Percentile(0.99))
}

func TestAccountPerformanceDeltaContract(t *testing.T) {
	bucket := time.Date(2026, 7, 17, 3, 0, 0, 0, time.UTC)
	ttft := int64(125)
	duration := int64(350)
	delta := AccountPerformanceDelta{
		BucketStart:  bucket,
		AccountID:    42,
		Platform:     "openai",
		GroupID:      7,
		Model:        "gpt-5",
		Protocol:     "responses",
		Outcome:      AccountPerformanceOutcomeSuccess,
		AttemptCount: 1,
		TTFTMS:       &ttft,
		DurationMS:   &duration,
		Failover:     true,
	}

	require.Equal(t, bucket, delta.BucketStart)
	require.EqualValues(t, 42, delta.AccountID)
	require.Equal(t, "openai", delta.Platform)
	require.EqualValues(t, 7, delta.GroupID)
	require.Equal(t, "gpt-5", delta.Model)
	require.Equal(t, "responses", delta.Protocol)
	require.Equal(t, AccountPerformanceOutcomeSuccess, delta.Outcome)
	require.EqualValues(t, 1, delta.AttemptCount)
	require.Equal(t, &ttft, delta.TTFTMS)
	require.Equal(t, &duration, delta.DurationMS)
	require.True(t, delta.Failover)
}

func TestAccountPerformanceTimeoutHasNoLatencySamples(t *testing.T) {
	ttft := int64(30_000)
	duration := int64(60_000)
	delta := AccountPerformanceDelta{
		Outcome:    AccountPerformanceOutcomeTTFTTimeout,
		TTFTMS:     &ttft,
		DurationMS: &duration,
	}

	_, ok := delta.TTFTLatencySample()
	require.False(t, ok)
	_, ok = delta.DurationLatencySample()
	require.False(t, ok)
}

func TestAccountPerformanceSuccessfulLatencySamples(t *testing.T) {
	ttft := int64(125)
	duration := int64(350)
	delta := AccountPerformanceDelta{
		Outcome:    AccountPerformanceOutcomeSuccess,
		TTFTMS:     &ttft,
		DurationMS: &duration,
	}

	gotTTFT, ok := delta.TTFTLatencySample()
	require.True(t, ok)
	require.Equal(t, ttft, gotTTFT)
	gotDuration, ok := delta.DurationLatencySample()
	require.True(t, ok)
	require.Equal(t, duration, gotDuration)
}

func TestAccountPerformanceNonTimeoutFailureHasNoLatencySamples(t *testing.T) {
	ttft := int64(125)
	duration := int64(350)
	delta := AccountPerformanceDelta{
		Outcome:    AccountPerformanceOutcomeOtherFailure,
		TTFTMS:     &ttft,
		DurationMS: &duration,
	}

	_, ok := delta.TTFTLatencySample()
	require.False(t, ok)
	_, ok = delta.DurationLatencySample()
	require.False(t, ok)
}

func TestAccountPerformanceOutcomeClassification(t *testing.T) {
	canceled, cancel := context.WithCancel(context.Background())
	cancel()
	deadlineExceeded, cancelDeadline := context.WithDeadline(context.Background(), time.Now().Add(-time.Second))
	defer cancelDeadline()

	tests := []struct {
		name   string
		err    error
		parent context.Context
		want   string
	}{
		{
			name: "ttft timeout",
			err:  &UpstreamFailoverError{ErrorType: UpstreamErrorTypeFirstTokenTimeout},
			want: AccountPerformanceOutcomeTTFTTimeout,
		},
		{
			name: "ttft timeout beats status",
			err: &UpstreamFailoverError{
				StatusCode: http.StatusTooManyRequests,
				ErrorType:  UpstreamErrorTypeFirstTokenTimeout,
			},
			want: AccountPerformanceOutcomeTTFTTimeout,
		},
		{
			name:   "parent cancellation wins",
			err:    &UpstreamFailoverError{StatusCode: http.StatusTooManyRequests},
			parent: canceled,
			want:   AccountPerformanceOutcomeClientCanceled,
		},
		{
			name:   "parent deadline wins",
			err:    &UpstreamFailoverError{StatusCode: http.StatusTooManyRequests},
			parent: deadlineExceeded,
			want:   AccountPerformanceOutcomeClientCanceled,
		},
		{
			name: "rate limit",
			err:  &UpstreamFailoverError{StatusCode: http.StatusTooManyRequests},
			want: AccountPerformanceOutcomeRateLimit,
		},
		{
			name: "rate limit error type",
			err:  &UpstreamFailoverError{ErrorType: "rate_limit_error"},
			want: AccountPerformanceOutcomeRateLimit,
		},
		{
			name: "auth",
			err:  &UpstreamFailoverError{StatusCode: http.StatusForbidden},
			want: AccountPerformanceOutcomeAuth,
		},
		{
			name: "unauthorized auth",
			err:  &UpstreamFailoverError{StatusCode: http.StatusUnauthorized},
			want: AccountPerformanceOutcomeAuth,
		},
		{
			name: "auth error type",
			err:  &UpstreamFailoverError{ErrorType: "authentication_error"},
			want: AccountPerformanceOutcomeAuth,
		},
		{
			name: "upstream 4xx",
			err:  &UpstreamFailoverError{StatusCode: http.StatusTeapot},
			want: AccountPerformanceOutcomeUpstream4xx,
		},
		{
			name: "upstream 5xx",
			err:  &UpstreamFailoverError{StatusCode: http.StatusBadGateway},
			want: AccountPerformanceOutcomeUpstream5xx,
		},
		{
			name: "protocol",
			err:  &UpstreamFailoverError{ErrorType: UpstreamErrorTypeFirstTokenPreludeOverflow},
			want: AccountPerformanceOutcomeProtocol,
		},
		{
			name: "protocol sentinel",
			err:  ErrFirstTokenPreludeTooLarge,
			want: AccountPerformanceOutcomeProtocol,
		},
		{
			name: "status beats protocol type",
			err: &UpstreamFailoverError{
				StatusCode: http.StatusTooManyRequests,
				ErrorType:  UpstreamErrorTypeFirstTokenPreludeOverflow,
			},
			want: AccountPerformanceOutcomeRateLimit,
		},
		{
			name: "protocol type beats generic 4xx status",
			err: &UpstreamFailoverError{
				StatusCode: http.StatusTeapot,
				ErrorType:  UpstreamErrorTypeFirstTokenPreludeOverflow,
			},
			want: AccountPerformanceOutcomeProtocol,
		},
		{
			name: "protocol type beats generic 5xx status",
			err: &UpstreamFailoverError{
				StatusCode: http.StatusBadGateway,
				ErrorType:  UpstreamErrorTypeFirstTokenPreludeOverflow,
			},
			want: AccountPerformanceOutcomeProtocol,
		},
		{
			name: "eof transport",
			err:  io.EOF,
			want: AccountPerformanceOutcomeTransport,
		},
		{
			name: "unowned deadline transport",
			err:  context.DeadlineExceeded,
			want: AccountPerformanceOutcomeTransport,
		},
		{
			name: "network transport",
			err:  &net.DNSError{IsTimeout: true},
			want: AccountPerformanceOutcomeTransport,
		},
		{
			name: "generic",
			err:  errors.New("socket closed"),
			want: AccountPerformanceOutcomeOtherFailure,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			require.Equal(t, tt.want, ClassifyAccountPerformanceOutcome(tt.err, tt.parent))
		})
	}
}
