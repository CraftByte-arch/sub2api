package service

import (
	"context"
	"log/slog"
	"math"
	"strings"
	"sync"
	"time"
	"unicode/utf8"
)

const (
	accountPerformanceRecorderDefaultQueueCapacity    = 4096
	accountPerformanceRecorderDefaultFlushKeys        = 1000
	accountPerformanceRecorderDefaultFlushInterval    = 5 * time.Second
	accountPerformanceRecorderDefaultRollupInterval   = 5 * time.Minute
	accountPerformanceRecorderDefaultCleanupInterval  = 24 * time.Hour
	accountPerformanceRecorderDefaultOperationTimeout = 2 * time.Second
	accountPerformanceMinuteRetention                 = 7 * 24 * time.Hour
	accountPerformanceHourlyRetention                 = 90 * 24 * time.Hour
)

type accountPerformanceRecorderConfig struct {
	queueCapacity    int
	flushKeys        int
	flushInterval    time.Duration
	rollupInterval   time.Duration
	cleanupInterval  time.Duration
	operationTimeout time.Duration
	now              func() time.Time
	logger           *slog.Logger
}

// AsyncAccountPerformanceRecorder buffers measurements and persists them in
// batches. It deliberately drops, rather than blocks, when it cannot keep up.
type AsyncAccountPerformanceRecorder struct {
	repo   AccountPerformanceRepository
	config accountPerformanceRecorderConfig
	queue  chan AccountPerformanceDelta

	lifecycleMu sync.RWMutex
	started     bool
	stopping    bool
	appCtx      context.Context
	stopCh      chan struct{}
	done        chan struct{}
	stopOnce    sync.Once

	healthMu sync.RWMutex
	health   AccountPerformanceCollectionHealth
}

func NewAccountPerformanceRecorder(repo AccountPerformanceRepository) *AsyncAccountPerformanceRecorder {
	return newAccountPerformanceRecorderWithConfig(repo, accountPerformanceRecorderConfig{})
}

func newAccountPerformanceRecorderWithConfig(repo AccountPerformanceRepository, config accountPerformanceRecorderConfig) *AsyncAccountPerformanceRecorder {
	config = normalizeAccountPerformanceRecorderConfig(config)
	return &AsyncAccountPerformanceRecorder{
		repo: repo, config: config, queue: make(chan AccountPerformanceDelta, config.queueCapacity),
		stopCh: make(chan struct{}), done: make(chan struct{}),
		health: AccountPerformanceCollectionHealth{Status: AccountPerformanceCollectionComplete},
	}
}

func normalizeAccountPerformanceRecorderConfig(config accountPerformanceRecorderConfig) accountPerformanceRecorderConfig {
	if config.queueCapacity <= 0 {
		config.queueCapacity = accountPerformanceRecorderDefaultQueueCapacity
	}
	if config.flushKeys <= 0 || config.flushKeys > accountPerformanceRecorderDefaultFlushKeys {
		config.flushKeys = accountPerformanceRecorderDefaultFlushKeys
	}
	if config.flushInterval <= 0 {
		config.flushInterval = accountPerformanceRecorderDefaultFlushInterval
	}
	if config.rollupInterval <= 0 {
		config.rollupInterval = accountPerformanceRecorderDefaultRollupInterval
	}
	if config.cleanupInterval <= 0 {
		config.cleanupInterval = accountPerformanceRecorderDefaultCleanupInterval
	}
	if config.operationTimeout <= 0 {
		config.operationTimeout = accountPerformanceRecorderDefaultOperationTimeout
	}
	if config.now == nil {
		config.now = time.Now
	}
	if config.logger == nil {
		config.logger = slog.Default()
	}
	return config
}

func (r *AsyncAccountPerformanceRecorder) Start(ctx context.Context) {
	if r == nil {
		return
	}
	if ctx == nil {
		ctx = context.Background()
	}
	r.lifecycleMu.Lock()
	if r.started || r.stopping {
		r.lifecycleMu.Unlock()
		return
	}
	r.started, r.appCtx = true, ctx
	r.lifecycleMu.Unlock()
	go r.run()
}

func (r *AsyncAccountPerformanceRecorder) Stop() {
	if r == nil {
		return
	}
	r.lifecycleMu.Lock()
	if !r.started {
		r.lifecycleMu.Unlock()
		return
	}
	r.stopping = true
	r.lifecycleMu.Unlock()
	r.stopOnce.Do(func() { close(r.stopCh) })
	<-r.done
}

func (r *AsyncAccountPerformanceRecorder) Record(delta AccountPerformanceDelta) {
	if r == nil {
		return
	}
	normalized, ok := normalizeAccountPerformanceRecorderDelta(delta)
	if !ok {
		r.addDropped(1)
		return
	}
	r.lifecycleMu.RLock()
	defer r.lifecycleMu.RUnlock()
	if !r.started || r.stopping {
		r.addDropped(1)
		return
	}
	select {
	case r.queue <- normalized:
		r.addPending(1)
	default:
		r.addDropped(1)
	}
}

func (r *AsyncAccountPerformanceRecorder) Health() AccountPerformanceCollectionHealth {
	if r == nil {
		return AccountPerformanceCollectionHealth{Status: AccountPerformanceCollectionComplete}
	}
	r.healthMu.RLock()
	defer r.healthMu.RUnlock()
	result := r.health
	if r.health.LastSuccessfulFlushAt != nil {
		last := *r.health.LastSuccessfulFlushAt
		result.LastSuccessfulFlushAt = &last
	}
	return result
}

func (r *AsyncAccountPerformanceRecorder) run() {
	defer close(r.done)
	flushTicker := time.NewTicker(r.config.flushInterval)
	rollupTicker := time.NewTicker(r.config.rollupInterval)
	cleanupTicker := time.NewTicker(r.config.cleanupInterval)
	defer flushTicker.Stop()
	defer rollupTicker.Stop()
	defer cleanupTicker.Stop()

	batch := make([]AccountPerformanceDelta, 0, r.config.flushKeys)
	for {
		select {
		case delta := <-r.queue:
			batch = append(batch, delta)
			if len(batch) >= r.config.flushKeys {
				r.flush(batch)
				batch = batch[:0]
			}
		case <-flushTicker.C:
			r.flush(batch)
			batch = batch[:0]
		case <-rollupTicker.C:
			r.rollup()
		case <-cleanupTicker.C:
			r.cleanup()
		case <-r.stopCh:
			for {
				select {
				case delta := <-r.queue:
					batch = append(batch, delta)
				default:
					r.flush(batch)
					return
				}
			}
		}
	}
}

func (r *AsyncAccountPerformanceRecorder) flush(batch []AccountPerformanceDelta) {
	if len(batch) == 0 {
		return
	}
	if r.repo == nil {
		r.finishDropped(int64(len(batch)))
		return
	}
	ctx, cancel := r.operationContext()
	err := r.repo.UpsertMinuteBatch(ctx, batch)
	cancel()
	if err != nil {
		r.logFailure("flush", err)
		r.finishDropped(int64(len(batch)))
		return
	}
	r.finishSuccess(int64(len(batch)))
}

func (r *AsyncAccountPerformanceRecorder) rollup() {
	if r.repo == nil {
		return
	}
	ctx, cancel := r.operationContext()
	err := r.repo.RollupClosedHours(ctx, r.config.now().UTC())
	cancel()
	if err != nil {
		r.logFailure("rollup", err)
	}
}

func (r *AsyncAccountPerformanceRecorder) cleanup() {
	if r.repo == nil {
		return
	}
	now := r.config.now().UTC()
	ctx, cancel := r.operationContext()
	_, minuteErr := r.repo.DeleteMinuteBefore(ctx, now.Add(-accountPerformanceMinuteRetention).Truncate(time.Minute))
	if minuteErr == nil {
		_, minuteErr = r.repo.DeleteHourlyBefore(ctx, now.Add(-accountPerformanceHourlyRetention).Truncate(time.Hour))
	}
	cancel()
	if minuteErr != nil {
		r.logFailure("cleanup", minuteErr)
	}
}

func (r *AsyncAccountPerformanceRecorder) operationContext() (context.Context, context.CancelFunc) {
	r.lifecycleMu.RLock()
	base := r.appCtx
	r.lifecycleMu.RUnlock()
	if base == nil {
		base = context.Background()
	}
	return context.WithTimeout(context.WithoutCancel(base), r.config.operationTimeout)
}

func (r *AsyncAccountPerformanceRecorder) addPending(samples int64) {
	r.healthMu.Lock()
	r.health.PendingSamples = saturatedAccountPerformanceCounter(r.health.PendingSamples, samples)
	r.healthMu.Unlock()
}

func (r *AsyncAccountPerformanceRecorder) addDropped(samples int64) {
	r.healthMu.Lock()
	r.health.DroppedSamples = saturatedAccountPerformanceCounter(r.health.DroppedSamples, samples)
	r.health.Status = AccountPerformanceCollectionDegraded
	r.healthMu.Unlock()
}

func (r *AsyncAccountPerformanceRecorder) finishDropped(samples int64) {
	r.healthMu.Lock()
	r.health.PendingSamples = subtractAccountPerformanceCounter(r.health.PendingSamples, samples)
	r.health.DroppedSamples = saturatedAccountPerformanceCounter(r.health.DroppedSamples, samples)
	r.health.Status = AccountPerformanceCollectionDegraded
	r.healthMu.Unlock()
}

func (r *AsyncAccountPerformanceRecorder) finishSuccess(samples int64) {
	now := r.config.now().UTC()
	r.healthMu.Lock()
	r.health.PendingSamples = subtractAccountPerformanceCounter(r.health.PendingSamples, samples)
	r.health.LastSuccessfulFlushAt = &now
	r.healthMu.Unlock()
}

func (r *AsyncAccountPerformanceRecorder) logFailure(operation string, err error) {
	if r.config.logger != nil {
		r.config.logger.Warn("account performance recorder repository operation failed", "operation", operation, "error", err)
	}
}

func normalizeAccountPerformanceRecorderDelta(delta AccountPerformanceDelta) (AccountPerformanceDelta, bool) {
	if delta.BucketStart.IsZero() || delta.AccountID <= 0 || delta.GroupID < 0 || delta.AttemptCount != 1 ||
		!validAccountPerformanceRecorderDimension(delta.Platform, 32, true) ||
		!validAccountPerformanceRecorderDimension(delta.Model, 255, false) ||
		!validAccountPerformanceRecorderDimension(delta.Protocol, 32, true) ||
		!isValidAccountPerformanceRecorderOutcome(delta.Outcome) {
		return AccountPerformanceDelta{}, false
	}
	if (delta.TTFTMS != nil && *delta.TTFTMS < 0) || (delta.DurationMS != nil && *delta.DurationMS < 0) {
		return AccountPerformanceDelta{}, false
	}
	delta.BucketStart = delta.BucketStart.UTC().Truncate(time.Minute)
	return delta, true
}

func validAccountPerformanceRecorderDimension(value string, maxRunes int, required bool) bool {
	return utf8.ValidString(value) && !strings.ContainsRune(value, '\x00') && utf8.RuneCountInString(value) <= maxRunes && (!required || strings.TrimSpace(value) != "")
}

func isValidAccountPerformanceRecorderOutcome(outcome string) bool {
	switch outcome {
	case AccountPerformanceOutcomeSuccess, AccountPerformanceOutcomeTTFTTimeout, AccountPerformanceOutcomeRateLimit, AccountPerformanceOutcomeAuth,
		AccountPerformanceOutcomeUpstream4xx, AccountPerformanceOutcomeUpstream5xx, AccountPerformanceOutcomeTransport, AccountPerformanceOutcomeProtocol,
		AccountPerformanceOutcomeOtherFailure, AccountPerformanceOutcomeClientCanceled:
		return true
	default:
		return false
	}
}

func saturatedAccountPerformanceCounter(left, right int64) int64 {
	if right <= 0 {
		return left
	}
	if left >= math.MaxInt64-right {
		return math.MaxInt64
	}
	return left + right
}

func subtractAccountPerformanceCounter(left, right int64) int64 {
	if right >= left {
		return 0
	}
	return left - right
}
