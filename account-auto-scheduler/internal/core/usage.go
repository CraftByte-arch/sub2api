package core

import (
	"encoding/json"
	"math"
	"strings"

	"github.com/Wei-Shaw/sub2api-account-auto-scheduler/internal/model"
)

// normalizeProbeUsage accepts the common usage shapes emitted by OpenAI,
// Responses, Anthropic, Gemini and compatible relays. Unknown fields are
// ignored, and an all-zero payload is treated as missing usage.
func normalizeProbeUsage(raw map[string]any, modelID string) *model.ProbeUsage {
	if len(raw) == 0 {
		return nil
	}
	usage := raw
	for _, key := range []string{"usage", "usageMetadata", "usage_metadata"} {
		if nested, ok := raw[key].(map[string]any); ok {
			usage = nested
			break
		}
	}
	result := &model.ProbeUsage{Model: strings.TrimSpace(modelID)}
	result.InputTokens = firstUsageInt(usage, "input_tokens", "prompt_tokens", "promptTokenCount", "inputTokenCount")
	result.OutputTokens = firstUsageInt(usage, "output_tokens", "completion_tokens", "candidatesTokenCount", "outputTokenCount")
	result.CacheReadTokens = firstUsageInt(usage, "cache_read_input_tokens", "cache_read_tokens", "cacheReadInputTokens", "cached_tokens")
	result.CacheWriteTokens = firstUsageInt(usage, "cache_creation_input_tokens", "cache_write_tokens", "cacheCreationInputTokens")
	for _, key := range []string{"input_tokens_details", "prompt_tokens_details"} {
		if details, ok := usage[key].(map[string]any); ok {
			cached := firstUsageInt(details, "cached_tokens", "cache_read_tokens")
			if cached > 0 {
				result.CacheReadTokens = cached
				if result.InputTokens >= cached {
					result.InputTokens -= cached
				}
			}
		}
	}
	if cached := firstUsageInt(usage, "cachedContentTokenCount"); cached > 0 {
		result.CacheReadTokens = cached
		if result.InputTokens >= cached {
			result.InputTokens -= cached
		}
	}
	if result.InputTokens == 0 && result.OutputTokens == 0 && result.CacheReadTokens == 0 && result.CacheWriteTokens == 0 {
		return nil
	}
	if result.Model == "" {
		for _, key := range []string{"model", "model_name"} {
			if value, ok := usage[key].(string); ok {
				result.Model = strings.TrimSpace(value)
				if result.Model != "" {
					break
				}
			}
		}
	}
	return result
}

func firstUsageInt(values map[string]any, keys ...string) int64 {
	for _, key := range keys {
		if value, ok := values[key]; ok {
			if parsed, ok := usageInt(value); ok && parsed >= 0 {
				return parsed
			}
		}
	}
	return 0
}

func usageInt(value any) (int64, bool) {
	switch typed := value.(type) {
	case int:
		return int64(typed), true
	case int64:
		return typed, true
	case float64:
		if math.IsNaN(typed) || math.IsInf(typed, 0) {
			return 0, false
		}
		return int64(typed), true
	case json.Number:
		parsed, err := typed.Int64()
		return parsed, err == nil
	case string:
		parsed, err := json.Number(strings.TrimSpace(typed)).Int64()
		return parsed, err == nil
	default:
		return 0, false
	}
}

func CalculateProbeCost(usage *model.ProbeUsage, pricing ModelPricing) *model.ProbeCost {
	if usage == nil || !pricing.Found || pricing.InputPrice == nil || pricing.OutputPrice == nil {
		return &model.ProbeCost{Currency: "USD", Known: false}
	}
	if (usage.CacheReadTokens > 0 && pricing.CacheReadPrice == nil) || (usage.CacheWriteTokens > 0 && pricing.CacheWritePrice == nil) {
		return &model.ProbeCost{Currency: "USD", Known: false}
	}
	values := []*float64{pricing.InputPrice, pricing.OutputPrice}
	for _, value := range []*float64{pricing.CacheReadPrice, pricing.CacheWritePrice} {
		if value != nil {
			values = append(values, value)
		}
	}
	for _, value := range values {
		if *value < 0 || math.IsNaN(*value) || math.IsInf(*value, 0) {
			return &model.ProbeCost{Currency: "USD", Known: false}
		}
	}
	amount := float64(usage.InputTokens)*(*pricing.InputPrice) + float64(usage.OutputTokens)*(*pricing.OutputPrice)
	if pricing.CacheReadPrice != nil {
		amount += float64(usage.CacheReadTokens) * (*pricing.CacheReadPrice)
	}
	if pricing.CacheWritePrice != nil {
		amount += float64(usage.CacheWriteTokens) * (*pricing.CacheWritePrice)
	}
	if math.IsNaN(amount) || math.IsInf(amount, 0) || amount < 0 {
		return &model.ProbeCost{Currency: "USD", Known: false}
	}
	return &model.ProbeCost{Amount: amount, Currency: "USD", Known: true}
}
