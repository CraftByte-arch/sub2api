//go:build unit

package service

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestDefaultMonitorStream(t *testing.T) {
	if !defaultMonitorStream(nil) {
		t.Fatal("new monitors should default to streaming")
	}
	stream := false
	if defaultMonitorStream(&stream) {
		t.Fatal("an explicit false must opt into buffered responses")
	}
}

func TestRunCheckForModel_ExplicitNonStreamingRequest(t *testing.T) {
	h := &openAICaptureHandler{}
	endpoint := setupFakeOpenAI(t, h)
	stream := false

	res := runCheckForModel(context.Background(), MonitorProviderOpenAI, endpoint, "sk-openai", "gpt-test", &CheckOptions{
		Stream: &stream,
	})

	if res.Status != MonitorStatusOperational {
		t.Fatalf("non-streaming chat request should pass challenge, got status=%s message=%q", res.Status, res.Message)
	}
	if h.lastBody["stream"] != false {
		t.Errorf("explicit non-streaming body should set stream=false, got %v", h.lastBody["stream"])
	}
	if h.lastHeaders.Get("Accept") != "application/json" {
		t.Errorf("non-streaming request should accept JSON, got %q", h.lastHeaders.Get("Accept"))
	}
}

func TestRunCheckForModel_StreamModeOverridesRequestTemplate(t *testing.T) {
	h := &captureHandler{respondText: "the answer is 42"}
	endpoint := setupFakeAnthropic(t, h)
	stream := false

	res := runCheckForModel(context.Background(), MonitorProviderAnthropic, endpoint, "sk-fake", "claude-x", &CheckOptions{
		Stream:           &stream,
		BodyOverrideMode: MonitorBodyOverrideModeReplace,
		BodyOverride: map[string]any{
			"model":      "claude-x",
			"messages":   []any{map[string]any{"role": "user", "content": "hello"}},
			"max_tokens": 10,
			"stream":     true,
		},
		ExtraHeaders: map[string]string{"accept": "text/event-stream"},
	})

	if res.Status != MonitorStatusOperational {
		t.Fatalf("replace-mode response should be operational, got status=%s message=%q", res.Status, res.Message)
	}
	if h.lastBody["stream"] != false {
		t.Errorf("monitor stream setting should override body template, got %v", h.lastBody["stream"])
	}
	if h.lastHeaders.Get("Accept") != "application/json" {
		t.Errorf("monitor stream setting should override Accept header, got %q", h.lastHeaders.Get("Accept"))
	}
}

func TestExtractMonitorStreamText(t *testing.T) {
	tests := []struct {
		name     string
		provider string
		apiMode  string
		body     string
		want     string
	}{
		{
			name:     "openai chat completions",
			provider: MonitorProviderOpenAI,
			apiMode:  MonitorAPIModeChatCompletions,
			body: "data: {\"choices\":[{\"delta\":{\"content\":\"4\"}}]}\n\n" +
				"data: {\"choices\":[{\"delta\":{\"content\":\"2\"}}]}\n\n" +
				"data: [DONE]\n\n",
			want: "42",
		},
		{
			name:     "openai responses",
			provider: MonitorProviderOpenAI,
			apiMode:  MonitorAPIModeResponses,
			body: "data: {\"type\":\"response.output_text.delta\",\"delta\":\"4\"}\n\n" +
				"data: {\"type\":\"response.output_text.delta\",\"delta\":\"2\"}\n\n" +
				"data: {\"type\":\"response.completed\",\"response\":{\"output_text\":\"42\"}}\n\n",
			want: "42",
		},
		{
			name:     "anthropic messages",
			provider: MonitorProviderAnthropic,
			body: "event: content_block_delta\n" +
				"data: {\"type\":\"content_block_delta\",\"delta\":{\"type\":\"text_delta\",\"text\":\"4\"}}\n\n" +
				"data: {\"type\":\"content_block_delta\",\"delta\":{\"type\":\"text_delta\",\"text\":\"2\"}}\n\n",
			want: "42",
		},
		{
			name:     "gemini stream generate content",
			provider: MonitorProviderGemini,
			body: "data: {\"candidates\":[{\"content\":{\"parts\":[{\"text\":\"4\"}]}}]}\n\n" +
				"data: {\"candidates\":[{\"content\":{\"parts\":[{\"text\":\"2\"}]}}]}\n\n",
			want: "42",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := extractMonitorStreamText(tt.provider, tt.apiMode, []byte(tt.body)); got != tt.want {
				t.Fatalf("extractMonitorStreamText() = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestCallProvider_GeminiStreamingUsesSSEPath(t *testing.T) {
	swapMonitorHTTPClient(t)
	var path, query, accept string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		path = r.URL.Path
		query = r.URL.RawQuery
		accept = r.Header.Get("Accept")
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = w.Write([]byte("data: {\"candidates\":[{\"content\":{\"parts\":[{\"text\":\"ok\"}]}}]}\n\n"))
	}))
	t.Cleanup(srv.Close)

	text, _, status, err := callProvider(context.Background(), MonitorProviderGemini, srv.URL, "AIza-test", "gemini-test", "hello", nil)
	if err != nil {
		t.Fatalf("callProvider() error = %v", err)
	}
	if status != http.StatusOK {
		t.Fatalf("callProvider() status = %d, want %d", status, http.StatusOK)
	}
	if text != "ok" {
		t.Fatalf("callProvider() text = %q, want %q", text, "ok")
	}
	if path != "/v1beta/models/gemini-test:streamGenerateContent" || query != "alt=sse" {
		t.Fatalf("unexpected Gemini streaming endpoint path=%q query=%q", path, query)
	}
	if accept != "text/event-stream" {
		t.Fatalf("Gemini streaming request Accept = %q, want SSE", accept)
	}
}
