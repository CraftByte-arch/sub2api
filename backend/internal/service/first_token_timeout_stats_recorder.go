package service

import (
	"context"
	"log/slog"
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

type firstTokenTimeoutStatsRecorderHealthState struct {
	droppedSamples         int64
	pendingSamples         int64
	lastSuccessfulFlushAt  time.Time
	hasLastSuccessfulFlush bool
	closed                 bool
}

type firstTokenTimeoutStatsRecorderConfig struct {
	queueCapacity    int
	flushUniqueKeys  int
	flushInterval    time.Duration
	cleanupInterval  time.Duration
	operationTimeout time.Duration
	now              func() time.Time
	logger           *slog.Logger
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

	lifecycleMu    sync.Mutex
	started        bool
	appCtx         context.Context
	shutdownCtx    context.Context
	shutdownCancel context.CancelFunc
	flushTicker    *time.Ticker
	cleanupTick    *time.Ticker

	state          atomic.Uint64
	shutdownOnce   sync.Once
	producersOnce  sync.Once
	workerDoneOnce sync.Once
	stopCh         chan struct{}
	producersDone  chan struct{}
	workerDone     chan struct{}
	repoCallGate   chan struct{}
	logInFlight    atomic.Bool

	health atomic.Pointer[firstTokenTimeoutStatsRecorderHealthState]
}

func NewFirstTokenTimeoutStatsRecorder(repo FirstTokenTimeoutStatsRepository) *FirstTokenTimeoutStatsRecorder {
	return newFirstTokenTimeoutStatsRecorderWithConfig(repo, firstTokenTimeoutStatsRecorderConfig{})
}

func newFirstTokenTimeoutStatsRecorderWithConfig(
	repo FirstTokenTimeoutStatsRepository,
	config firstTokenTimeoutStatsRecorderConfig,
) *FirstTokenTimeoutStatsRecorder {
	config = normalizeFirstTokenTimeoutStatsRecorderConfig(config)
	recorder := &FirstTokenTimeoutStatsRecorder{
		repo:          repo,
		config:        config,
		queue:         make(chan FirstTokenStatsDelta, config.queueCapacity),
		stopCh:        make(chan struct{}),
		producersDone: make(chan struct{}),
		workerDone:    make(chan struct{}),
		repoCallGate:  make(chan struct{}, 1),
	}
	recorder.repoCallGate <- struct{}{}
	recorder.health.Store(&firstTokenTimeoutStatsRecorderHealthState{closed: true})
	return recorder
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
	if config.logger == nil {
		config.logger = slog.Default()
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
	r.transitionHealthClosed(false)
	flushC := r.flushTicker.C
	cleanupC := r.cleanupTick.C
	r.lifecycleMu.Unlock()

	context.AfterFunc(ctx, func() { r.requestShutdown() })
	go r.run(flushC, cleanupC)
}

func (r *FirstTokenTimeoutStatsRecorder) Stop() {
	if r == nil {
		return
	}

	r.lifecycleMu.Lock()
	started := r.started
	r.lifecycleMu.Unlock()
	shutdownCtx := r.requestShutdown()
	if !started {
		r.finishWorker()
		return
	}

	select {
	case <-r.workerDone:
	case <-shutdownCtx.Done():
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
		r.transitionPendingDrop(delta.SampleCount, weight)
	}
}

func (r *FirstTokenTimeoutStatsRecorder) Health() FirstTokenTimeoutStatsRecorderHealth {
	if r == nil {
		return FirstTokenTimeoutStatsRecorderHealth{Status: FirstTokenStatsCompletenessComplete}
	}
	state := r.loadHealthState()
	dropped := state.droppedSamples
	status := FirstTokenStatsCompletenessComplete
	if dropped > 0 {
		status = FirstTokenStatsCompletenessDegraded
	}
	var lastSuccessfulFlushAt *time.Time
	if state.hasLastSuccessfulFlush {
		value := state.lastSuccessfulFlushAt
		lastSuccessfulFlushAt = &value
	}
	return FirstTokenTimeoutStatsRecorderHealth{
		Status:                status,
		DroppedSamples:        dropped,
		LastSuccessfulFlushAt: lastSuccessfulFlushAt,
		PendingSamples:        state.pendingSamples,
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

func (r *FirstTokenTimeoutStatsRecorder) requestShutdown() context.Context {
	r.shutdownOnce.Do(func() {
		r.stopAccepting()
		r.transitionHealthClosed(true)
		r.lifecycleMu.Lock()
		baseCtx := r.appCtx
		if baseCtx == nil {
			baseCtx = context.Background()
		}
		r.shutdownCtx, r.shutdownCancel = context.WithTimeout(context.WithoutCancel(baseCtx), r.config.operationTimeout)
		if r.flushTicker != nil {
			r.flushTicker.Stop()
		}
		if r.cleanupTick != nil {
			r.cleanupTick.Stop()
		}
		r.lifecycleMu.Unlock()
		close(r.stopCh)
	})
	r.lifecycleMu.Lock()
	shutdownCtx := r.shutdownCtx
	r.lifecycleMu.Unlock()
	return shutdownCtx
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
	defer r.finishWorker()

	aggregates := make(map[firstTokenStatsRecorderKey]FirstTokenStatsDelta, r.config.flushUniqueKeys)
	for {
		if r.shutdownRequested() {
			r.shutdown(&aggregates)
			return
		}
		select {
		case delta := <-r.queue:
			if !r.mergeDelta(aggregates, delta) {
				r.transitionFlushFailure(delta.SampleCount)
			}
			if r.shutdownRequested() {
				r.shutdown(&aggregates)
				return
			}
			if len(aggregates) >= r.config.flushUniqueKeys {
				r.flush(&aggregates)
			}
		case <-flushC:
			if r.shutdownRequested() {
				r.shutdown(&aggregates)
				return
			}
			r.flush(&aggregates)
		case <-cleanupC:
			if r.shutdownRequested() {
				r.shutdown(&aggregates)
				return
			}
			r.cleanup()
		case <-r.stopCh:
			r.shutdown(&aggregates)
			return
		}
	}
}

func (r *FirstTokenTimeoutStatsRecorder) shutdownRequested() bool {
	select {
	case <-r.stopCh:
		return true
	default:
		return false
	}
}

func (r *FirstTokenTimeoutStatsRecorder) finishWorker() {
	r.lifecycleMu.Lock()
	shutdownCancel := r.shutdownCancel
	r.lifecycleMu.Unlock()
	if shutdownCancel != nil {
		defer shutdownCancel()
	}
	r.workerDoneOnce.Do(func() { close(r.workerDone) })
}

func (r *FirstTokenTimeoutStatsRecorder) shutdown(aggregates *map[firstTokenStatsRecorderKey]FirstTokenStatsDelta) {
	r.lifecycleMu.Lock()
	shutdownCtx := r.shutdownCtx
	r.lifecycleMu.Unlock()
	if shutdownCtx == nil {
		shutdownCtx = context.Background()
	}

	select {
	case <-r.producersDone:
	case <-shutdownCtx.Done():
		r.dropAllPendingSamples()
		return
	}

	for {
		if shutdownCtx.Err() != nil {
			r.dropAllPendingSamples()
			return
		}
		select {
		case delta := <-r.queue:
			if !r.mergeDelta(*aggregates, delta) {
				r.transitionFlushFailure(delta.SampleCount)
			}
			if len(*aggregates) >= r.config.flushUniqueKeys {
				r.flushWithContext(aggregates, shutdownCtx)
			}
		default:
			r.flushWithContext(aggregates, shutdownCtx)
			if shutdownCtx.Err() != nil {
				r.dropAllPendingSamples()
			}
			return
		}
	}
}

func (r *FirstTokenTimeoutStatsRecorder) cleanup() {
	if r.repo == nil {
		return
	}
	ctx, cancel := r.operationContext()
	defer cancel()
	cutoff := r.config.now().UTC().Add(-90 * 24 * time.Hour).Truncate(time.Hour)
	if _, err := r.deleteBefore(ctx, cutoff); err != nil {
		r.logRepositoryFailure(
			"operation", "cleanup",
			"error", err,
		)
	}
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
	ctx, cancel := r.operationContext()
	defer cancel()
	r.flushWithContext(aggregates, ctx)
}

func (r *FirstTokenTimeoutStatsRecorder) operationContext() (context.Context, context.CancelFunc) {
	r.lifecycleMu.Lock()
	defer r.lifecycleMu.Unlock()
	if r.shutdownCtx != nil {
		return r.shutdownCtx, func() {}
	}
	baseCtx := r.appCtx
	if baseCtx == nil {
		baseCtx = context.Background()
	}
	return context.WithTimeout(context.WithoutCancel(baseCtx), r.config.operationTimeout)
}

func (r *FirstTokenTimeoutStatsRecorder) flushWithContext(
	aggregates *map[firstTokenStatsRecorderKey]FirstTokenStatsDelta,
	ctx context.Context,
) {
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

	err := r.upsertBatch(ctx, batch)
	if err != nil {
		r.logRepositoryFailure(
			"operation", "flush",
			"sample_count", samples,
			"error", err,
		)
		r.transitionFlushFailure(samples)
	} else {
		now := r.config.now().UTC()
		r.transitionFlushSuccess(samples, now)
	}
}

func (r *FirstTokenTimeoutStatsRecorder) logRepositoryFailure(attrs ...any) {
	logger := r.config.logger
	if logger == nil || !r.logInFlight.CompareAndSwap(false, true) {
		return
	}
	go func() {
		defer r.logInFlight.Store(false)
		logger.Warn("first token stats recorder repository operation failed", attrs...)
	}()
}

func (r *FirstTokenTimeoutStatsRecorder) upsertBatch(ctx context.Context, batch []FirstTokenStatsDelta) error {
	if r.repo == nil {
		return context.Canceled
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	if !r.acquireRepositoryCall(ctx) {
		return ctx.Err()
	}
	result := make(chan error, 1)
	go func() {
		err := r.repo.UpsertBatch(ctx, batch)
		r.releaseRepositoryCall()
		result <- err
	}()
	select {
	case err := <-result:
		if err != nil {
			return err
		}
		return ctx.Err()
	case <-ctx.Done():
		return ctx.Err()
	}
}

func (r *FirstTokenTimeoutStatsRecorder) deleteBefore(ctx context.Context, cutoff time.Time) (int64, error) {
	if r.repo == nil {
		return 0, context.Canceled
	}
	if err := ctx.Err(); err != nil {
		return 0, err
	}
	if !r.acquireRepositoryCall(ctx) {
		return 0, ctx.Err()
	}
	type result struct {
		deleted int64
		err     error
	}
	results := make(chan result, 1)
	go func() {
		deleted, err := r.repo.DeleteBefore(ctx, cutoff)
		r.releaseRepositoryCall()
		results <- result{deleted: deleted, err: err}
	}()
	select {
	case result := <-results:
		if result.err != nil {
			return 0, result.err
		}
		if err := ctx.Err(); err != nil {
			return 0, err
		}
		return result.deleted, nil
	case <-ctx.Done():
		return 0, ctx.Err()
	}
}

func (r *FirstTokenTimeoutStatsRecorder) acquireRepositoryCall(ctx context.Context) bool {
	select {
	case <-ctx.Done():
		return false
	case <-r.repoCallGate:
		if ctx.Err() != nil {
			r.releaseRepositoryCall()
			return false
		}
		return true
	}
}

func (r *FirstTokenTimeoutStatsRecorder) releaseRepositoryCall() {
	r.repoCallGate <- struct{}{}
}

func (r *FirstTokenTimeoutStatsRecorder) dropAllPendingSamples() {
	for {
		state := r.loadHealthState()
		if state.pendingSamples == 0 {
			return
		}
		next := *state
		next.droppedSamples = addFirstTokenStatsRecorderSaturated(state.droppedSamples, state.pendingSamples)
		next.pendingSamples = 0
		if r.health.CompareAndSwap(state, &next) {
			return
		}
	}
}

func firstTokenStatsRecorderSafeWeight(delta FirstTokenStatsDelta) int64 {
	if delta.SampleCount > 0 {
		return delta.SampleCount
	}
	return 1
}

func (r *FirstTokenTimeoutStatsRecorder) reservePendingSamples(samples int64) bool {
	for {
		state := r.loadHealthState()
		if state.closed || state.pendingSamples < 0 || samples > math.MaxInt64-state.pendingSamples {
			return false
		}
		next := *state
		next.pendingSamples += samples
		if r.health.CompareAndSwap(state, &next) {
			return true
		}
	}
}

func (r *FirstTokenTimeoutStatsRecorder) addDroppedSamples(samples int64) {
	if samples <= 0 {
		samples = 1
	}
	for {
		state := r.loadHealthState()
		next := *state
		next.droppedSamples = addFirstTokenStatsRecorderSaturated(state.droppedSamples, samples)
		if r.health.CompareAndSwap(state, &next) {
			return
		}
	}
}

func (r *FirstTokenTimeoutStatsRecorder) transitionPendingDrop(pending, dropped int64) {
	for {
		state := r.loadHealthState()
		next := *state
		if pending >= next.pendingSamples {
			next.pendingSamples = 0
		} else {
			next.pendingSamples -= pending
		}
		next.droppedSamples = addFirstTokenStatsRecorderSaturated(next.droppedSamples, dropped)
		if r.health.CompareAndSwap(state, &next) {
			return
		}
	}
}

func (r *FirstTokenTimeoutStatsRecorder) transitionFlushFailure(samples int64) {
	r.transitionPendingDrop(samples, samples)
}

func (r *FirstTokenTimeoutStatsRecorder) transitionFlushSuccess(samples int64, flushedAt time.Time) {
	for {
		state := r.loadHealthState()
		next := *state
		if samples >= next.pendingSamples {
			next.pendingSamples = 0
		} else {
			next.pendingSamples -= samples
		}
		next.lastSuccessfulFlushAt = flushedAt
		next.hasLastSuccessfulFlush = true
		if r.health.CompareAndSwap(state, &next) {
			return
		}
	}
}

func (r *FirstTokenTimeoutStatsRecorder) transitionHealthClosed(closed bool) {
	for {
		state := r.loadHealthState()
		if state.closed == closed {
			return
		}
		next := *state
		next.closed = closed
		if r.health.CompareAndSwap(state, &next) {
			return
		}
	}
}

func (r *FirstTokenTimeoutStatsRecorder) loadHealthState() *firstTokenTimeoutStatsRecorderHealthState {
	if state := r.health.Load(); state != nil {
		return state
	}
	initial := &firstTokenTimeoutStatsRecorderHealthState{closed: true}
	if r.health.CompareAndSwap(nil, initial) {
		return initial
	}
	return r.health.Load()
}

func addFirstTokenStatsRecorderSaturated(left, right int64) int64 {
	if right <= 0 {
		return left
	}
	if left >= math.MaxInt64-right {
		return math.MaxInt64
	}
	return left + right
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
		// Recorder events must already use the canonical request sentinels.
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
	// A recorder event represents at least one observed sample, unlike generic repository deltas.
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
