package service

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"sync/atomic"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/pkg/logger"
)

const (
	firstTokenTimeoutDefaultSeconds = 30
	firstTokenTimeoutMinSeconds     = 1
	firstTokenTimeoutMaxSeconds     = 300
	firstTokenTimeoutReloadInterval = time.Minute
)

type FirstTokenTimeoutSettings struct {
	Enabled        bool `json:"enabled"`
	TimeoutSeconds int  `json:"timeout_seconds"`
}

type FirstTokenTimeoutSnapshot struct {
	Enabled  bool
	Timeout  time.Duration
	LoadedAt time.Time
}

type FirstTokenTimeoutPolicyNotifier interface {
	Publish(ctx context.Context) error
	Subscribe(ctx context.Context) (<-chan struct{}, error)
}

type FirstTokenTimeoutPolicy struct {
	repo     SettingRepository
	notifier FirstTokenTimeoutPolicyNotifier
	current  atomic.Value
}

func NewFirstTokenTimeoutPolicy(repo SettingRepository, notifier FirstTokenTimeoutPolicyNotifier) *FirstTokenTimeoutPolicy {
	policy := &FirstTokenTimeoutPolicy{repo: repo, notifier: notifier}
	policy.current.Store(snapshotFromFirstTokenTimeoutSettings(defaultFirstTokenTimeoutSettings()))
	return policy
}

func (p *FirstTokenTimeoutPolicy) Snapshot() FirstTokenTimeoutSnapshot {
	return p.current.Load().(FirstTokenTimeoutSnapshot)
}

func (p *FirstTokenTimeoutPolicy) Update(ctx context.Context, in FirstTokenTimeoutSettings) error {
	if err := validateFirstTokenTimeoutSettings(in); err != nil {
		return err
	}

	encoded, err := json.Marshal(in)
	if err != nil {
		return fmt.Errorf("encode first token timeout settings: %w", err)
	}
	if err := p.repo.Set(ctx, SettingKeyFirstTokenTimeoutSettings, string(encoded)); err != nil {
		return fmt.Errorf("save first token timeout settings: %w", err)
	}

	p.current.Store(snapshotFromFirstTokenTimeoutSettings(in))
	if p.notifier != nil {
		if err := p.notifier.Publish(ctx); err != nil {
			logger.LegacyPrintf("service.first_token_timeout", "failed to publish policy invalidation; DB reload fallback remains active: %v", err)
		}
	}
	return nil
}

func (p *FirstTokenTimeoutPolicy) Reload(ctx context.Context) error {
	raw, err := p.repo.GetValue(ctx, SettingKeyFirstTokenTimeoutSettings)
	if err != nil {
		if errors.Is(err, ErrSettingNotFound) {
			p.current.Store(snapshotFromFirstTokenTimeoutSettings(defaultFirstTokenTimeoutSettings()))
			return nil
		}
		return fmt.Errorf("load first token timeout settings: %w", err)
	}

	settings, err := decodeFirstTokenTimeoutSettings(raw)
	if err != nil {
		logger.LegacyPrintf("service.first_token_timeout", "invalid first token timeout settings; using disabled default: %v", err)
		settings = defaultFirstTokenTimeoutSettings()
	}
	p.current.Store(snapshotFromFirstTokenTimeoutSettings(settings))
	return nil
}

func (p *FirstTokenTimeoutPolicy) Start(ctx context.Context) {
	if err := p.Reload(ctx); err != nil {
		logger.LegacyPrintf("service.first_token_timeout", "failed to load first token timeout settings; keeping current snapshot: %v", err)
	}
	go p.run(ctx)
}

func (p *FirstTokenTimeoutPolicy) run(ctx context.Context) {
	ticker := time.NewTicker(firstTokenTimeoutReloadInterval)
	defer ticker.Stop()

	var events <-chan struct{}
	subscribe := func() {
		if p.notifier == nil {
			return
		}
		var err error
		events, err = p.notifier.Subscribe(ctx)
		if err != nil {
			events = nil
			logger.LegacyPrintf("service.first_token_timeout", "failed to subscribe to policy invalidation; DB reload fallback remains active: %v", err)
		}
	}
	subscribe()

	for {
		select {
		case <-ctx.Done():
			return
		case _, ok := <-events:
			if !ok {
				events = nil
				continue
			}
			if err := p.Reload(ctx); err != nil {
				logger.LegacyPrintf("service.first_token_timeout", "failed to reload policy after invalidation: %v", err)
			}
		case <-ticker.C:
			if err := p.Reload(ctx); err != nil {
				logger.LegacyPrintf("service.first_token_timeout", "periodic policy reload failed: %v", err)
			}
			if events == nil {
				subscribe()
			}
		}
	}
}

func parseFirstTokenTimeoutSettings(raw string) FirstTokenTimeoutSettings {
	settings, err := decodeFirstTokenTimeoutSettings(raw)
	if err != nil {
		return defaultFirstTokenTimeoutSettings()
	}
	return settings
}

func decodeFirstTokenTimeoutSettings(raw string) (FirstTokenTimeoutSettings, error) {
	var settings FirstTokenTimeoutSettings
	if err := json.Unmarshal([]byte(raw), &settings); err != nil {
		return FirstTokenTimeoutSettings{}, fmt.Errorf("decode JSON: %w", err)
	}
	if err := validateFirstTokenTimeoutSettings(settings); err != nil {
		return FirstTokenTimeoutSettings{}, err
	}
	return settings, nil
}

func validateFirstTokenTimeoutSettings(settings FirstTokenTimeoutSettings) error {
	if settings.TimeoutSeconds < firstTokenTimeoutMinSeconds || settings.TimeoutSeconds > firstTokenTimeoutMaxSeconds {
		return fmt.Errorf("first token timeout seconds must be between %d and %d", firstTokenTimeoutMinSeconds, firstTokenTimeoutMaxSeconds)
	}
	return nil
}

func defaultFirstTokenTimeoutSettings() FirstTokenTimeoutSettings {
	return FirstTokenTimeoutSettings{Enabled: false, TimeoutSeconds: firstTokenTimeoutDefaultSeconds}
}

func snapshotFromFirstTokenTimeoutSettings(settings FirstTokenTimeoutSettings) FirstTokenTimeoutSnapshot {
	return FirstTokenTimeoutSnapshot{
		Enabled:  settings.Enabled,
		Timeout:  time.Duration(settings.TimeoutSeconds) * time.Second,
		LoadedAt: time.Now(),
	}
}
