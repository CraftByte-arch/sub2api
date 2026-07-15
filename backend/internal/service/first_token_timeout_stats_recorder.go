package service

import (
	"context"
	"math"
	"strings"
	"sync"
	"sync/atomic"
	"time"
	"unicode/utf8"
)

const (
	firstTokenStatsRecorderDefaultQueueCapacity   = 4096
	firstTokenStatsRecorderDefaultFlushUniqueKeys = 1000
	firstTokenStatsRecorderDefaultFlushInterval   = 5 * time.Second
	firstTokenStatsRecorderDefaultCleanupInterval = 24 * time.Hour
	firstTokenStatsRecorderDefaultTimeout         = 2 * time.Second
	firstTokenStatsRecorderProtocolMaxRunes       = 32

	FirstTokenStatsCompletenessComplete = "complete"
	FirstTokenStatsCompletenessDegraded = "degraded"
)

const (
	firstTokenStatsRecorderAcceptingBit uint64 = 1 << 63
	firstTokenStatsRecorderStoppedBit   uint64 = 1 << 62
	firstTokenStatsRecorderActiveMask   uint64 = firstTokenStatsRecorderStoppedBit - 1
)

type FirstTokenTimeoutStatsRecorderHealth struct {
	Status                string     `json:"status"`
	DroppedSamples        int64      `json:"dropped_samples"`
	LastSuccessfulFlushAt *time.Time `json:"last_successful_flush_at"`
	PendingSamples        int64      `json:"pending_samples"`
}

type firstTokenTimeoutStatsRecorderConfig struct {
	queueCapacity    int
	flushUniqueKeys  int
	flushInterval    time.Duration
	cleanupInterval  time.Duration
	operationTimeout time.Duration
	now              func() time.Time
}

type firstTokenStatsRecorderKey struct {
	bucketStart    time.Time
	scope          string
	accountID      int64
	protocol       string
	platform       string
	model          string
	timeoutSeconds int
	outcome        string
	failureKind    string
}

type FirstTokenTimeoutStatsRecorder struct {
	repo   FirstTokenTimeoutStatsRepository
	config firstTokenTimeoutStatsRecorderConfig
	queue  chan FirstTokenStatsDelta

	lifecycleMu sync.Mutex
	started     bool
	appCtx      context.Context
	flushTicker *time.Ticker
	cleanupTick *time.Ticker

	state          atomic.Uint64
	shutdownOnce   sync.Once
	producersOnce  sync.Once
	workerDoneOnce sync.Once
	stopCh         chan struct{}
	producersDone  chan struct{}
	workerDone     chan struct{}

	pendingSamples atomic.Int64
	droppedSamples atomic.Int64
	lastFlush      atomic.Pointer[time.Time]
}

func NewFirstTokenTimeoutStatsRecorder(repo FirstTokenTimeoutStatsRepository) *FirstTokenTimeoutStatsRecorder {
	return newFirstTokenTimeoutStatsRecorderWithConfig(repo, firstTokenTimeoutStatsRecorderConfig{})
}

func newFirstTokenTimeoutStatsRecorderWithConfig(
	repo FirstTokenTimeoutStatsRepository,
	config firstTokenTimeoutStatsRecorderConfig,
) *FirstTokenTimeoutStatsRecorder {
	config = normalizeFirstTokenTimeoutStatsRecorderConfig(config)
	return &FirstTokenTimeoutStatsRecorder{
		repo:          repo,
		config:        config,
		queue:         make(chan FirstTokenStatsDelta, config.queueCapacity),
		stopCh:        make(chan struct{}),
		producersDone: make(chan struct{}),
		workerDone:    make(chan struct{}),
	}
}

func normalizeFirstTokenTimeoutStatsRecorderConfig(config firstTokenTimeoutStatsRecorderConfig) firstTokenTimeoutStatsRecorderConfig {
	if config.queueCapacity <= 0 {
		config.queueCapacity = firstTokenStatsRecorderDefaultQueueCapacity
	}
	if config.flushUniqueKeys <= 0 || config.flushUniqueKeys > firstTokenStatsRecorderDefaultFlushUniqueKeys {
		config.flushUniqueKeys = firstTokenStatsRecorderDefaultFlushUniqueKeys
	}
	if config.flushInterval <= 0 {
		config.flushInterval = firstTokenStatsRecorderDefaultFlushInterval
	}
	if config.cleanupInterval <= 0 {
		config.cleanupInterval = firstTokenStatsRecorderDefaultCleanupInterval
	}
	if config.operationTimeout <= 0 {
		config.operationTimeout = firstTokenStatsRecorderDefaultTimeout
	}
	if config.now == nil {
		config.now = time.Now
	}
	return config
}

func (r *FirstTokenTimeoutStatsRecorder) Start(ctx context.Context) {
	if r == nil {
		return
	}
	if ctx == nil {
		ctx = context.Background()
	}

	r.lifecycleMu.Lock()
	if r.started || r.state.Load()&firstTokenStatsRecorderStoppedBit != 0 {
		r.lifecycleMu.Unlock()
		return
	}
	r.started = true
	r.appCtx = ctx
	r.flushTicker = time.NewTicker(r.config.flushInterval)
	r.cleanupTick = time.NewTicker(r.config.cleanupInterval)
	r.state.Store(firstTokenStatsRecorderAcceptingBit)
	flushC := r.flushTicker.C
	cleanupC := r.cleanupTick.C
	r.lifecycleMu.Unlock()

	context.AfterFunc(ctx, r.requestShutdown)
	go r.run(flushC, cleanupC)
}

func (r *FirstTokenTimeoutStatsRecorder) Stop() {
	if r == nil {
		return
	}

	r.lifecycleMu.Lock()
	started := r.started
	if !started {
		r.stopAccepting()
		r.shutdownOnce.Do(func() { close(r.stopCh) })
		r.workerDoneOnce.Do(func() { close(r.workerDone) })
	}
	r.lifecycleMu.Unlock()
	if !started {
		return
	}

	r.requestShutdown()
	timer := time.NewTimer(r.config.operationTimeout)
	defer timer.Stop()
	select {
	case <-r.workerDone:
	case <-timer.C:
	}
}

func (r *FirstTokenTimeoutStatsRecorder) Record(delta FirstTokenStatsDelta) {
	if r == nil {
		return
	}
	weight := firstTokenStatsRecorderSafeWeight(delta)
	normalized, valid := normalizeFirstTokenStatsRecorderDelta(delta)
	if !valid {
		r.addDroppedSamples(weight)
		return
	}
	delta = normalized
	if !r.beginRecord() {
		r.addDroppedSamples(weight)
		return
	}
	defer r.endRecord()

	if !r.reservePendingSamples(delta.SampleCount) {
		r.addDroppedSamples(weight)
		return
	}
	select {
	case r.queue <- delta:
	default:
		r.pendingSamples.Add(-delta.SampleCount)
		r.addDroppedSamples(weight)
	}
}

func (r *FirstTokenTimeoutStatsRecorder) Health() FirstTokenTimeoutStatsRecorderHealth {
	if r == nil {
		return FirstTokenTimeoutStatsRecorderHealth{Status: FirstTokenStatsCompletenessComplete}
	}
	dropped := r.droppedSamples.Load()
	status := FirstTokenStatsCompletenessComplete
	if dropped > 0 {
		status = FirstTokenStatsCompletenessDegraded
	}
	var lastSuccessfulFlushAt *time.Time
	if stored := r.lastFlush.Load(); stored != nil {
		value := *stored
		lastSuccessfulFlushAt = &value
	}
	return FirstTokenTimeoutStatsRecorderHealth{
		Status:                status,
		DroppedSamples:        dropped,
		LastSuccessfulFlushAt: lastSuccessfulFlushAt,
		PendingSamples:        r.pendingSamples.Load(),
	}
}

func (r *FirstTokenTimeoutStatsRecorder) beginRecord() bool {
	for {
		state := r.state.Load()
		if state&firstTokenStatsRecorderAcceptingBit == 0 || state&firstTokenStatsRecorderActiveMask == firstTokenStatsRecorderActiveMask {
			return false
		}
		if r.state.CompareAndSwap(state, state+1) {
			return true
		}
	}
}

func (r *FirstTokenTimeoutStatsRecorder) endRecord() {
	state := r.state.Add(^uint64(0))
	if state == firstTokenStatsRecorderStoppedBit {
		r.producersOnce.Do(func() { close(r.producersDone) })
	}
}

func (r *FirstTokenTimeoutStatsRecorder) requestShutdown() {
	r.shutdownOnce.Do(func() {
		r.stopAccepting()
		r.lifecycleMu.Lock()
		if r.flushTicker != nil {
			r.flushTicker.Stop()
		}
		if r.cleanupTick != nil {
			r.cleanupTick.Stop()
		}
		r.lifecycleMu.Unlock()
		close(r.stopCh)
	})
}

func (r *FirstTokenTimeoutStatsRecorder) stopAccepting() {
	for {
		state := r.state.Load()
		if state&firstTokenStatsRecorderStoppedBit != 0 {
			return
		}
		stopped := state&firstTokenStatsRecorderActiveMask | firstTokenStatsRecorderStoppedBit
		if r.state.CompareAndSwap(state, stopped) {
			if stopped == firstTokenStatsRecorderStoppedBit {
				r.producersOnce.Do(func() { close(r.producersDone) })
			}
			return
		}
	}
}

func (r *FirstTokenTimeoutStatsRecorder) run(flushC, cleanupC <-chan time.Time) {
	defer r.workerDoneOnce.Do(func() { close(r.workerDone) })

	aggregates := make(map[firstTokenStatsRecorderKey]FirstTokenStatsDelta, r.config.flushUniqueKeys)
	for {
		select {
		case delta := <-r.queue:
			if !r.mergeDelta(aggregates, delta) {
				r.addDroppedSamples(delta.SampleCount)
				r.pendingSamples.Add(-delta.SampleCount)
			}
			if len(aggregates) >= r.config.flushUniqueKeys {
				r.flush(&aggregates)
			}
		case <-flushC:
			r.flush(&aggregates)
		case <-cleanupC:
			r.cleanup()
		case <-r.stopCh:
			<-r.producersDone
			for {
				select {
				case delta := <-r.queue:
					if !r.mergeDelta(aggregates, delta) {
						r.addDroppedSamples(delta.SampleCount)
						r.pendingSamples.Add(-delta.SampleCount)
					}
					if len(aggregates) >= r.config.flushUniqueKeys {
						r.flush(&aggregates)
					}
				default:
					r.flush(&aggregates)
					return
				}
			}
		}
	}
}

func (r *FirstTokenTimeoutStatsRecorder) cleanup() {
	if r.repo == nil {
		return
	}
	baseCtx := r.appCtx
	if baseCtx == nil {
		baseCtx = context.Background()
	}
	ctx, cancel := context.WithTimeout(context.WithoutCancel(baseCtx), r.config.operationTimeout)
	defer cancel()
	cutoff := r.config.now().UTC().Add(-90 * 24 * time.Hour).Truncate(time.Hour)
	_, _ = r.repo.DeleteBefore(ctx, cutoff)
}

func (r *FirstTokenTimeoutStatsRecorder) mergeDelta(
	aggregates map[firstTokenStatsRecorderKey]FirstTokenStatsDelta,
	delta FirstTokenStatsDelta,
) bool {
	key := firstTokenStatsRecorderKey{
		bucketStart:    delta.BucketStart,
		scope:          delta.Scope,
		accountID:      delta.AccountID,
		protocol:       delta.Protocol,
		platform:       delta.Platform,
		model:          delta.Model,
		timeoutSeconds: delta.TimeoutSeconds,
		outcome:        delta.Outcome,
		failureKind:    delta.FailureKind,
	}
	existing, ok := aggregates[key]
	if !ok {
		aggregates[key] = delta
		return true
	}
	sampleCount, ok := addFirstTokenStatsRecorderCounter(existing.SampleCount, delta.SampleCount)
	if !ok {
		return false
	}
	ttftSampleCount, ok := addFirstTokenStatsRecorderCounter(existing.TTFTSampleCount, delta.TTFTSampleCount)
	if !ok {
		return false
	}
	ttftSumMS, ok := addFirstTokenStatsRecorderCounter(existing.TTFTSumMS, delta.TTFTSumMS)
	if !ok {
		return false
	}
	ttftAffectedCount, ok := addFirstTokenStatsRecorderCounter(existing.TTFTAffectedCount, delta.TTFTAffectedCount)
	if !ok {
		return false
	}
	existing.SampleCount = sampleCount
	existing.TTFTSampleCount = ttftSampleCount
	existing.TTFTSumMS = ttftSumMS
	existing.TTFTAffectedCount = ttftAffectedCount
	if delta.TTFTMaxMS > existing.TTFTMaxMS {
		existing.TTFTMaxMS = delta.TTFTMaxMS
	}
	aggregates[key] = existing
	return true
}

func (r *FirstTokenTimeoutStatsRecorder) flush(aggregates *map[firstTokenStatsRecorderKey]FirstTokenStatsDelta) {
	if len(*aggregates) == 0 {
		return
	}
	current := *aggregates
	*aggregates = make(map[firstTokenStatsRecorderKey]FirstTokenStatsDelta, r.config.flushUniqueKeys)
	batch := make([]FirstTokenStatsDelta, 0, len(current))
	var samples int64
	for _, delta := range current {
		batch = append(batch, delta)
		samples += delta.SampleCount
	}

	baseCtx := r.appCtx
	if baseCtx == nil {
		baseCtx = context.Background()
	}
	ctx, cancel := context.WithTimeout(context.WithoutCancel(baseCtx), r.config.operationTimeout)
	var err error
	if r.repo == nil {
		err = context.Canceled
	} else {
		err = r.repo.UpsertBatch(ctx, batch)
	}
	contextErr := ctx.Err()
	cancel()
	if err != nil || contextErr != nil {
		r.addDroppedSamples(samples)
	} else {
		now := r.config.now().UTC()
		r.lastFlush.Store(&now)
	}
	r.pendingSamples.Add(-samples)
}

func firstTokenStatsRecorderSafeWeight(delta FirstTokenStatsDelta) int64 {
	if delta.SampleCount > 0 {
		return delta.SampleCount
	}
	return 1
}

func (r *FirstTokenTimeoutStatsRecorder) reservePendingSamples(samples int64) bool {
	for {
		pending := r.pendingSamples.Load()
		if pending < 0 || samples > math.MaxInt64-pending {
			return false
		}
		if r.pendingSamples.CompareAndSwap(pending, pending+samples) {
			return true
		}
	}
}

func (r *FirstTokenTimeoutStatsRecorder) addDroppedSamples(samples int64) {
	if samples <= 0 {
		samples = 1
	}
	for {
		dropped := r.droppedSamples.Load()
		if dropped >= math.MaxInt64-samples {
			if r.droppedSamples.CompareAndSwap(dropped, math.MaxInt64) {
				return
			}
			continue
		}
		if r.droppedSamples.CompareAndSwap(dropped, dropped+samples) {
			return
		}
	}
}

func addFirstTokenStatsRecorderCounter(left, right int64) (int64, bool) {
	if left < 0 || right < 0 || right > math.MaxInt64-left {
		return 0, false
	}
	return left + right, true
}

func normalizeFirstTokenStatsRecorderDelta(delta FirstTokenStatsDelta) (FirstTokenStatsDelta, bool) {
	if delta.BucketStart.IsZero() ||
		!validFirstTokenStatsRecorderDimension(delta.Protocol, firstTokenStatsRecorderProtocolMaxRunes, true) ||
		!validFirstTokenStatsRecorderDimension(delta.Platform, firstTokenStatsPlatformMaxRunes, false) ||
		!validFirstTokenStatsRecorderDimension(delta.Model, firstTokenStatsModelMaxRunes, false) {
		return FirstTokenStatsDelta{}, false
	}
	delta.BucketStart = delta.BucketStart.UTC().Truncate(time.Hour)

	switch delta.Scope {
	case FirstTokenStatsScopeAttempt:
		if delta.AccountID <= 0 || strings.TrimSpace(delta.Platform) == "" {
			return FirstTokenStatsDelta{}, false
		}
	case FirstTokenStatsScopeRequest:
		if delta.AccountID != 0 || delta.Platform != "" {
			return FirstTokenStatsDelta{}, false
		}
	default:
		return FirstTokenStatsDelta{}, false
	}
	if delta.TimeoutSeconds < firstTokenTimeoutMinSeconds || delta.TimeoutSeconds > firstTokenTimeoutMaxSeconds ||
		!validFirstTokenStatsRecorderOutcome(delta.Scope, delta.Outcome) {
		return FirstTokenStatsDelta{}, false
	}
	if delta.Outcome == FirstTokenStatsAttemptOtherFailure {
		if !validFirstTokenStatsRecorderFailureKind(delta.FailureKind) {
			return FirstTokenStatsDelta{}, false
		}
	} else if delta.FailureKind != "" {
		return FirstTokenStatsDelta{}, false
	}
	if delta.SampleCount <= 0 || delta.TTFTSampleCount < 0 || delta.TTFTSumMS < 0 ||
		delta.TTFTMaxMS < 0 || delta.TTFTMaxMS > math.MaxInt32 || delta.TTFTAffectedCount < 0 ||
		delta.TTFTSampleCount > delta.SampleCount ||
		(delta.TTFTSampleCount == 0 && (delta.TTFTSumMS != 0 || delta.TTFTMaxMS != 0)) ||
		delta.TTFTMaxMS > delta.TTFTSumMS {
		return FirstTokenStatsDelta{}, false
	}

	switch delta.Scope {
	case FirstTokenStatsScopeAttempt:
		if delta.TTFTAffectedCount != 0 ||
			(delta.Outcome == FirstTokenStatsAttemptTTFTTimeout && delta.TTFTSampleCount != 0) {
			return FirstTokenStatsDelta{}, false
		}
	case FirstTokenStatsScopeRequest:
		if delta.TTFTSampleCount != 0 || delta.TTFTSumMS != 0 || delta.TTFTMaxMS != 0 ||
			delta.TTFTAffectedCount > delta.SampleCount {
			return FirstTokenStatsDelta{}, false
		}
		switch delta.Outcome {
		case FirstTokenStatsRequestSuccess:
			if delta.TTFTAffectedCount != 0 {
				return FirstTokenStatsDelta{}, false
			}
		case FirstTokenStatsRequestRecoveredAfterTTFT, FirstTokenStatsRequestTTFTExhausted:
			if delta.TTFTAffectedCount != delta.SampleCount {
				return FirstTokenStatsDelta{}, false
			}
		}
	}
	return delta, true
}

func validFirstTokenStatsRecorderDimension(value string, maxRunes int, required bool) bool {
	if !utf8.ValidString(value) || strings.ContainsRune(value, '\x00') || utf8.RuneCountInString(value) > maxRunes {
		return false
	}
	return !required || strings.TrimSpace(value) != ""
}

func validFirstTokenStatsRecorderOutcome(scope, outcome string) bool {
	switch scope {
	case FirstTokenStatsScopeAttempt:
		switch outcome {
		case FirstTokenStatsAttemptSuccess,
			FirstTokenStatsAttemptTTFTTimeout,
			FirstTokenStatsAttemptClientCanceled,
			FirstTokenStatsAttemptOtherFailure:
			return true
		}
	case FirstTokenStatsScopeRequest:
		switch outcome {
		case FirstTokenStatsRequestSuccess,
			FirstTokenStatsRequestRecoveredAfterTTFT,
			FirstTokenStatsRequestTTFTExhausted,
			FirstTokenStatsRequestClientCanceled,
			FirstTokenStatsRequestOtherFailure:
			return true
		}
	}
	return false
}

func validFirstTokenStatsRecorderFailureKind(failureKind string) bool {
	switch failureKind {
	case FirstTokenStatsFailureRateLimit,
		FirstTokenStatsFailureAuth,
		FirstTokenStatsFailureUpstream4xx,
		FirstTokenStatsFailureUpstream5xx,
		FirstTokenStatsFailureTransport,
		FirstTokenStatsFailureStreamIdleTimeout,
		FirstTokenStatsFailureProtocol,
		FirstTokenStatsFailureOther:
		return true
	default:
		return false
	}
}
