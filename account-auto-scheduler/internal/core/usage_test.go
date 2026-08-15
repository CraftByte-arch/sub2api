package core

import (
	"encoding/json"
	"math"
	"testing"

	"github.com/Wei-Shaw/sub2api-account-auto-scheduler/internal/model"
)

func TestNormalizeProbeUsageAcrossProviderShapes(t *testing.T) {
	openAI := normalizeProbeUsage(map[string]any{
		"prompt_tokens":     float64(100),
		"completion_tokens": float64(20),
		"prompt_tokens_details": map[string]any{
			"cached_tokens": float64(40),
		},
	}, "gpt-test")
	if openAI == nil || openAI.InputTokens != 60 || openAI.OutputTokens != 20 || openAI.CacheReadTokens != 40 || openAI.Model != "gpt-test" {
		t.Fatalf("unexpected OpenAI usage: %#v", openAI)
	}

	anthropic := normalizeProbeUsage(map[string]any{
		"input_tokens":                float64(12),
		"output_tokens":               float64(3),
		"cache_read_input_tokens":     float64(5),
		"cache_creation_input_tokens": float64(7),
	}, "claude-test")
	if anthropic == nil || anthropic.InputTokens != 12 || anthropic.OutputTokens != 3 || anthropic.CacheReadTokens != 5 || anthropic.CacheWriteTokens != 7 {
		t.Fatalf("unexpected Anthropic usage: %#v", anthropic)
	}

	gemini := normalizeProbeUsage(map[string]any{
		"promptTokenCount":        float64(30),
		"candidatesTokenCount":    float64(4),
		"cachedContentTokenCount": float64(10),
	}, "gemini-test")
	if gemini == nil || gemini.InputTokens != 20 || gemini.OutputTokens != 4 || gemini.CacheReadTokens != 10 {
		t.Fatalf("unexpected Gemini usage: %#v", gemini)
	}
}

func TestCalculateProbeCostUsesOneTimesModelPricing(t *testing.T) {
	input, output, cacheRead, cacheWrite := 0.001, 0.002, 0.0001, 0.00125
	cost := CalculateProbeCost(&modelProbeUsageFixture, ModelPricing{
		Found: true, InputPrice: &input, OutputPrice: &output, CacheReadPrice: &cacheRead, CacheWritePrice: &cacheWrite,
	})
	want := 10*input + 2*output + 5*cacheRead + 4*cacheWrite
	if cost == nil || !cost.Known || math.Abs(cost.Amount-want) > 1e-12 {
		t.Fatalf("cost = %#v, want %.12f", cost, want)
	}
	unknown := CalculateProbeCost(&modelProbeUsageFixture, ModelPricing{Found: false})
	if unknown == nil || unknown.Known || unknown.Amount != 0 {
		t.Fatalf("missing pricing fabricated a cost: %#v", unknown)
	}
	missingCachePrice := CalculateProbeCost(&modelProbeUsageFixture, ModelPricing{Found: true, InputPrice: &input, OutputPrice: &output})
	if missingCachePrice == nil || missingCachePrice.Known {
		t.Fatalf("missing cache pricing fabricated a cost: %#v", missingCachePrice)
	}
}

var modelProbeUsageFixture = model.ProbeUsage{
	Model: "test", InputTokens: 10, OutputTokens: 2, CacheReadTokens: 5, CacheWriteTokens: 4,
}

func TestDirectStreamParsersRetainTerminalUsage(t *testing.T) {
	finishPayload, _ := json.Marshal(map[string]any{
		"model":   "gpt-test",
		"choices": []any{map[string]any{"delta": map[string]any{}, "finish_reason": "stop"}},
	})
	_, completed, usage, err := parseDirectSSEData(directProbeOpenAIChat, "", finishPayload)
	if err != nil || completed || usage != nil {
		t.Fatalf("finish_reason stopped before usage: completed=%t usage=%#v err=%v", completed, usage, err)
	}
	usagePayload, _ := json.Marshal(map[string]any{
		"model":   "gpt-test",
		"choices": []any{},
		"usage": map[string]any{
			"prompt_tokens": 8, "completion_tokens": 2,
		},
	})
	_, completed, usage, err = parseDirectSSEData(directProbeOpenAIChat, "", usagePayload)
	if err != nil || !completed || usage == nil || usage.InputTokens != 8 || usage.OutputTokens != 2 || usage.Model != "gpt-test" {
		t.Fatalf("terminal usage was not retained: completed=%t usage=%#v err=%v", completed, usage, err)
	}

	anthropicPayload, _ := json.Marshal(map[string]any{
		"type":  "message_delta",
		"usage": map[string]any{"output_tokens": 3, "cache_read_input_tokens": 5},
	})
	_, _, usage, err = parseDirectSSEData(directProbeAnthropic, "", anthropicPayload)
	if err != nil || usage == nil || usage.OutputTokens != 3 || usage.CacheReadTokens != 5 {
		t.Fatalf("Anthropic usage was not retained: %#v err=%v", usage, err)
	}

	geminiPayload, _ := json.Marshal(map[string]any{
		"modelVersion":  "gemini-test",
		"usageMetadata": map[string]any{"promptTokenCount": 12, "candidatesTokenCount": 4},
		"candidates":    []any{map[string]any{"finishReason": "STOP"}},
	})
	_, completed, usage, err = parseDirectSSEData(directProbeGemini, "", geminiPayload)
	if err != nil || !completed || usage == nil || usage.InputTokens != 12 || usage.OutputTokens != 4 || usage.Model != "gemini-test" {
		t.Fatalf("Gemini usage was not retained: completed=%t usage=%#v err=%v", completed, usage, err)
	}
}
