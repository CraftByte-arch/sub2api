package service

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"strconv"
	"strings"
	"time"
)

type cachedOpenAILongContextBillingSettings struct {
	settings  OpenAILongContextBillingRuntime
	expiresAt int64
}

const (
	defaultOpenAILongContextBillingThreshold  = openAIGPT54LongContextInputThreshold
	defaultOpenAILongContextBillingMultiplier = openAIGPT54LongContextInputMultiplier
	defaultOpenAILongContextOutputMultiplier  = openAIGPT54LongContextOutputMultiplier

	openAILongContextBillingCacheTTL  = 60 * time.Second
	openAILongContextBillingErrorTTL  = 5 * time.Second
	openAILongContextBillingDBTimeout = 5 * time.Second
	openAILongContextBillingCacheKey  = "openai_long_context_billing"
)

type OpenAILongContextBillingRuntime struct {
	Enabled          bool
	Threshold        int
	InputMultiplier  float64
	OutputMultiplier float64
}

func (s *SettingService) GetOpenAILongContextBillingRuntime(ctx context.Context) OpenAILongContextBillingRuntime {
	fallback := OpenAILongContextBillingRuntime{
		Enabled:          true,
		Threshold:        defaultOpenAILongContextBillingThreshold,
		InputMultiplier:  defaultOpenAILongContextBillingMultiplier,
		OutputMultiplier: defaultOpenAILongContextOutputMultiplier,
	}
	if s == nil || s.settingRepo == nil {
		return fallback
	}
	if cached, ok := s.openAILongContextCache.Load().(*cachedOpenAILongContextBillingSettings); ok && cached != nil {
		if time.Now().UnixNano() < cached.expiresAt {
			return cached.settings
		}
	}
	result, _, _ := s.openAILongContextSF.Do(openAILongContextBillingCacheKey, func() (any, error) {
		if cached, ok := s.openAILongContextCache.Load().(*cachedOpenAILongContextBillingSettings); ok && cached != nil {
			if time.Now().UnixNano() < cached.expiresAt {
				return cached.settings, nil
			}
		}
		dbCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), openAILongContextBillingDBTimeout)
		defer cancel()
		vals, err := s.settingRepo.GetMultiple(dbCtx, []string{
			SettingKeyOpenAILongContextBillingEnabled,
			SettingKeyOpenAILongContextBillingThreshold,
			SettingKeyOpenAILongContextBillingMultiplier,
			SettingKeyOpenAILongContextOutputMultiplier,
		})
		if err != nil {
			slog.Warn("failed to get openai long context billing settings", "error", err)
			s.openAILongContextCache.Store(&cachedOpenAILongContextBillingSettings{
				settings:  fallback,
				expiresAt: time.Now().Add(openAILongContextBillingErrorTTL).UnixNano(),
			})
			return fallback, nil
		}
		runtime := OpenAILongContextBillingRuntime{
			Enabled:          !isFalseSettingValue(vals[SettingKeyOpenAILongContextBillingEnabled]),
			Threshold:        parsePositiveIntSetting(vals[SettingKeyOpenAILongContextBillingThreshold], defaultOpenAILongContextBillingThreshold),
			InputMultiplier:  parsePositiveFloatSetting(vals[SettingKeyOpenAILongContextBillingMultiplier], defaultOpenAILongContextBillingMultiplier),
			OutputMultiplier: parsePositiveFloatSetting(vals[SettingKeyOpenAILongContextOutputMultiplier], defaultOpenAILongContextOutputMultiplier),
		}
		s.openAILongContextCache.Store(&cachedOpenAILongContextBillingSettings{
			settings:  runtime,
			expiresAt: time.Now().Add(openAILongContextBillingCacheTTL).UnixNano(),
		})
		return runtime, nil
	})
	if runtime, ok := result.(OpenAILongContextBillingRuntime); ok {
		return runtime
	}
	return fallback
}

func (s *SettingService) GetDefaultUsageCards(ctx context.Context) []DefaultUsageCardSetting {
	value, err := s.settingRepo.GetValue(ctx, SettingKeyDefaultUsageCards)
	if err != nil {
		return nil
	}
	return parseDefaultUsageCards(value)
}

func (s *SettingService) validateDefaultUsageCardPlans(ctx context.Context, items []DefaultUsageCardSetting) error {
	if len(items) == 0 {
		return nil
	}
	checked := make(map[int64]struct{}, len(items))
	for _, item := range items {
		if item.PlanID <= 0 {
			continue
		}
		if _, ok := checked[item.PlanID]; ok {
			return fmt.Errorf("default usage card cannot be duplicated: %d", item.PlanID)
		}
		checked[item.PlanID] = struct{}{}
		if item.Quantity <= 0 {
			return fmt.Errorf("default usage card quantity must be positive: %d", item.PlanID)
		}
		if s.defaultUsageCardPlanReader == nil {
			continue
		}
		if _, err := s.defaultUsageCardPlanReader.GetPlanByID(ctx, item.PlanID); err != nil {
			if errors.Is(err, ErrUsageCardPlanNotFound) {
				return fmt.Errorf("default usage card plan not found: %d", item.PlanID)
			}
			return fmt.Errorf("get default usage card plan %d: %w", item.PlanID, err)
		}
	}
	return nil
}

func parseDefaultUsageCards(raw string) []DefaultUsageCardSetting {
	if strings.TrimSpace(raw) == "" {
		return nil
	}
	var items []DefaultUsageCardSetting
	if err := json.Unmarshal([]byte(raw), &items); err != nil {
		return nil
	}
	normalized := make([]DefaultUsageCardSetting, 0, len(items))
	for _, item := range items {
		if item.PlanID <= 0 {
			continue
		}
		if item.Quantity <= 0 {
			item.Quantity = 1
		}
		normalized = append(normalized, item)
	}
	return normalized
}

func parsePositiveFloatSetting(raw string, fallback float64) float64 {
	value, err := strconv.ParseFloat(strings.TrimSpace(raw), 64)
	if err != nil || value <= 0 {
		return fallback
	}
	return value
}
