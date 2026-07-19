package service

import (
	"context"
	"errors"
	"log/slog"
	"sync"
	"sync/atomic"
	"time"

	"golang.org/x/sync/singleflight"
)

const defaultImageStudioInputStorageProbeInterval = 15 * time.Second

type ImageStudioInputStorageHealthSnapshot struct {
	Initialized    bool
	Healthy        bool
	LastTransition time.Time
	LastErrorKind  string
}

type imageStudioInputStorageHealthTicker interface {
	C() <-chan time.Time
	Stop()
}

type imageStudioInputStorageRealTicker struct {
	*time.Ticker
}

func (t imageStudioInputStorageRealTicker) C() <-chan time.Time { return t.Ticker.C }

type ImageStudioInputStorageHealth struct {
	prober            ImageStudioInputStorageProber
	interval          time.Duration
	healthy           atomic.Bool
	probeGroup        singleflight.Group
	stateMu           sync.RWMutex
	snapshot          ImageStudioInputStorageHealthSnapshot
	now               func() time.Time
	newTicker         func(time.Duration) imageStudioInputStorageHealthTicker
	observeTransition func(ImageStudioInputStorageHealthSnapshot)
}

func NewImageStudioInputStorageHealth(prober ImageStudioInputStorageProber, interval time.Duration) *ImageStudioInputStorageHealth {
	if interval <= 0 {
		interval = defaultImageStudioInputStorageProbeInterval
	}
	health := &ImageStudioInputStorageHealth{
		prober:   prober,
		interval: interval,
		now:      time.Now,
		newTicker: func(interval time.Duration) imageStudioInputStorageHealthTicker {
			return imageStudioInputStorageRealTicker{Ticker: time.NewTicker(interval)}
		},
	}
	health.observeTransition = logImageStudioInputStorageHealthTransition
	return health
}

func (h *ImageStudioInputStorageHealth) Available() bool {
	return h != nil && h.healthy.Load()
}

func (h *ImageStudioInputStorageHealth) Snapshot() ImageStudioInputStorageHealthSnapshot {
	if h == nil {
		return ImageStudioInputStorageHealthSnapshot{}
	}
	h.stateMu.RLock()
	defer h.stateMu.RUnlock()
	return h.snapshot
}

func (h *ImageStudioInputStorageHealth) Probe(ctx context.Context) error {
	if h == nil {
		return inputStorageError(errors.New("image studio input storage health is nil"))
	}
	if err := ctx.Err(); err != nil {
		return inputStorageError(err)
	}
	result, err, _ := h.probeGroup.Do("input-storage", func() (any, error) {
		if err := ctx.Err(); err != nil {
			return nil, inputStorageError(err)
		}
		var probeErr error
		if h.prober == nil {
			probeErr = inputStorageError(errors.New("image studio input storage prober is not configured"))
		} else {
			probeErr = h.prober.Probe(ctx)
			if probeErr != nil && !errors.Is(probeErr, ErrImageStudioInputStorageUnavailable) {
				probeErr = inputStorageError(probeErr)
			}
		}
		if ctx.Err() != nil && errors.Is(probeErr, context.Canceled) {
			return probeErr, nil
		}
		h.record(probeErr)
		return probeErr, nil
	})
	if err != nil {
		return err
	}
	probeErr, _ := result.(error)
	return probeErr
}

func (h *ImageStudioInputStorageHealth) Run(ctx context.Context) {
	if h == nil {
		return
	}
	ticker := h.newTicker(h.interval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C():
			_ = h.Probe(ctx)
		}
	}
}

func (h *ImageStudioInputStorageHealth) record(probeErr error) {
	healthy := probeErr == nil
	h.stateMu.Lock()
	changed := !h.snapshot.Initialized || h.snapshot.Healthy != healthy
	h.snapshot.Initialized = true
	h.snapshot.Healthy = healthy
	if probeErr != nil {
		h.snapshot.LastErrorKind = ImageStudioInputCodeStorageUnavailable
	}
	if changed {
		h.snapshot.LastTransition = h.now()
	}
	snapshot := h.snapshot
	h.stateMu.Unlock()
	h.healthy.Store(healthy)
	if changed && h.observeTransition != nil {
		h.observeTransition(snapshot)
	}
}

func logImageStudioInputStorageHealthTransition(snapshot ImageStudioInputStorageHealthSnapshot) {
	if snapshot.Healthy {
		slog.Info("image_studio_input_storage_health_transition", "healthy", true)
		return
	}
	slog.Warn(
		"image_studio_input_storage_health_transition",
		"healthy", false,
		"error_kind", ImageStudioInputCodeStorageUnavailable,
	)
}
