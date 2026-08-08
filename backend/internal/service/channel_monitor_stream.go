package service

import (
	"bytes"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/tidwall/gjson"
)

const providerGeminiStreamPathTemplate = "/v1beta/models/%s:streamGenerateContent?alt=sse"

// defaultMonitorStream keeps create requests from older API clients streaming
// by default. A non-nil false is the explicit buffered-response opt-out.
func defaultMonitorStream(stream *bool) bool {
	return stream == nil || *stream
}

// monitorStreamEnabled keeps nil compatible with newly-created monitors and
// older API clients: they all request SSE unless false is explicitly set.
func monitorStreamEnabled(opts *CheckOptions) bool {
	return opts == nil || opts.Stream == nil || *opts.Stream
}

// prepareMonitorStreamAdapter confines stream-specific behavior to this file.
// The existing checker keeps its request flow and only calls this decorator.
func prepareMonitorStreamAdapter(adapter providerAdapter, provider, apiMode string, opts *CheckOptions) (providerAdapter, *CheckOptions) {
	stream := monitorStreamEnabled(opts)
	opts = cloneMonitorStreamOptions(opts, provider, stream)

	originalBuildPath := adapter.buildPath
	adapter.buildPath = func(model string) string {
		if provider == MonitorProviderGemini && stream {
			return fmt.Sprintf(providerGeminiStreamPathTemplate, model)
		}
		return originalBuildPath(model)
	}

	originalBuildBody := adapter.buildBody
	adapter.buildBody = func(model, prompt string) ([]byte, error) {
		body, err := originalBuildBody(model, prompt)
		if err != nil {
			return nil, err
		}
		return applyMonitorStreamRequestBody(provider, body, stream)
	}

	originalBuildHeaders := adapter.buildHeaders
	adapter.buildHeaders = func(apiKey string) map[string]string {
		return applyMonitorStreamHeaders(originalBuildHeaders(apiKey), stream)
	}

	originalExtractText := adapter.extractText
	textPath := adapter.textPath
	adapter.extractText = func(respBytes []byte) string {
		if text := extractMonitorStreamText(provider, apiMode, respBytes); strings.TrimSpace(text) != "" {
			return text
		}
		if originalExtractText != nil {
			return originalExtractText(respBytes)
		}
		return gjson.GetBytes(respBytes, textPath).String()
	}
	return adapter, opts
}

func cloneMonitorStreamOptions(opts *CheckOptions, provider string, stream bool) *CheckOptions {
	if opts == nil {
		return nil
	}

	cloned := *opts
	if opts.ExtraHeaders != nil {
		cloned.ExtraHeaders = make(map[string]string, len(opts.ExtraHeaders))
		for key, value := range opts.ExtraHeaders {
			if !strings.EqualFold(key, "Accept") {
				cloned.ExtraHeaders[key] = value
			}
		}
	}
	if opts.BodyOverride != nil && monitorStreamUsesBody(provider) {
		cloned.BodyOverride = make(map[string]any, len(opts.BodyOverride)+1)
		for key, value := range opts.BodyOverride {
			cloned.BodyOverride[key] = value
		}
		cloned.BodyOverride["stream"] = stream
	}
	return &cloned
}

func applyMonitorStreamRequestBody(provider string, payload []byte, stream bool) ([]byte, error) {
	if !monitorStreamUsesBody(provider) {
		return payload, nil
	}

	var body map[string]any
	if err := json.Unmarshal(payload, &body); err != nil {
		return nil, fmt.Errorf("unmarshal stream request body: %w", err)
	}
	body["stream"] = stream
	updated, err := json.Marshal(body)
	if err != nil {
		return nil, fmt.Errorf("marshal stream request body: %w", err)
	}
	return updated, nil
}

func monitorStreamUsesBody(provider string) bool {
	return provider == MonitorProviderOpenAI || provider == MonitorProviderGrok || provider == MonitorProviderAnthropic
}

func applyMonitorStreamHeaders(headers map[string]string, stream bool) map[string]string {
	updated := make(map[string]string, len(headers)+1)
	for key, value := range headers {
		if strings.EqualFold(key, "Accept") {
			continue
		}
		updated[key] = value
	}
	if stream {
		updated["Accept"] = "text/event-stream"
	} else {
		updated["Accept"] = "application/json"
	}
	return updated
}

// extractMonitorStreamText extracts generated text from the providers' SSE
// event payloads. It tolerates a regular JSON response so callers can fall
// back to the pre-existing JSON extractors.
func extractMonitorStreamText(provider, apiMode string, respBytes []byte) string {
	dataEvents := monitorSSEDataEvents(respBytes)
	if len(dataEvents) == 0 {
		return ""
	}

	deltas := make([]string, 0, len(dataEvents))
	fallbacks := make([]string, 0, 1)
	for _, event := range dataEvents {
		var delta, fallback string
		switch provider {
		case MonitorProviderOpenAI:
			if apiMode == MonitorAPIModeResponses {
				delta = extractOpenAIResponsesStreamDelta(event)
				fallback = extractOpenAIResponsesStreamFallback(event)
			} else {
				delta = extractOpenAIChatStreamDelta(event)
				fallback = gjson.GetBytes(event, "choices.0.message.content").String()
			}
		case MonitorProviderGrok:
			delta = extractOpenAIChatStreamDelta(event)
			fallback = gjson.GetBytes(event, "choices.0.message.content").String()
		case MonitorProviderAnthropic:
			delta = extractAnthropicMonitorStreamDelta(event)
			fallback = extractAnthropicMonitorText(event)
		case MonitorProviderGemini:
			// Gemini streamGenerateContent emits partial candidates in each event.
			delta = gjson.GetBytes(event, "candidates.0.content.parts.0.text").String()
			fallback = delta
		}
		if strings.TrimSpace(delta) != "" {
			deltas = append(deltas, delta)
			continue
		}
		if strings.TrimSpace(fallback) != "" {
			fallbacks = append(fallbacks, fallback)
		}
	}
	if len(deltas) > 0 {
		return strings.Join(deltas, "")
	}
	return strings.Join(fallbacks, "")
}

func monitorSSEDataEvents(respBytes []byte) [][]byte {
	var events [][]byte
	var current []byte
	flush := func() {
		if len(current) == 0 {
			return
		}
		data := bytes.TrimSpace(current)
		if len(data) > 0 && !bytes.Equal(data, []byte("[DONE]")) {
			events = append(events, data)
		}
		current = nil
	}

	for _, line := range bytes.Split(respBytes, []byte("\n")) {
		line = bytes.TrimSuffix(line, []byte("\r"))
		if len(line) == 0 {
			flush()
			continue
		}
		if !bytes.HasPrefix(line, []byte("data:")) {
			continue
		}
		data := line[len("data:"):]
		if len(data) > 0 && data[0] == ' ' {
			data = data[1:]
		}
		if len(current) > 0 {
			current = append(current, '\n')
		}
		current = append(current, data...)
	}
	flush()
	return events
}

func extractOpenAIChatStreamDelta(respBytes []byte) string {
	choices := gjson.GetBytes(respBytes, "choices")
	if !choices.IsArray() {
		return ""
	}
	parts := make([]string, 0, 1)
	choices.ForEach(func(_, choice gjson.Result) bool {
		if text := choice.Get("delta.content").String(); text != "" {
			parts = append(parts, text)
		}
		return true
	})
	return strings.Join(parts, "")
}

func extractOpenAIResponsesStreamDelta(respBytes []byte) string {
	if gjson.GetBytes(respBytes, "type").String() != "response.output_text.delta" {
		return ""
	}
	return gjson.GetBytes(respBytes, "delta").String()
}

func extractOpenAIResponsesStreamText(respBytes []byte) string {
	return extractMonitorStreamText(MonitorProviderOpenAI, MonitorAPIModeResponses, respBytes)
}

func extractOpenAIResponsesStreamFallback(respBytes []byte) string {
	if text := extractOpenAIResponsesText(respBytes); strings.TrimSpace(text) != "" {
		return text
	}
	response := gjson.GetBytes(respBytes, "response")
	if !response.Exists() {
		return ""
	}
	return extractOpenAIResponsesText([]byte(response.Raw))
}

func extractAnthropicMonitorStreamDelta(respBytes []byte) string {
	if gjson.GetBytes(respBytes, "type").String() != "content_block_delta" {
		return ""
	}
	return gjson.GetBytes(respBytes, "delta.text").String()
}
