package core

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api-account-auto-scheduler/internal/model"
)

func TestProbeDirectUsesStreamingProtocolContracts(t *testing.T) {
	const prompt = "  keep this custom prompt exactly  \n"
	tests := []struct {
		name          string
		platform      string
		model         string
		responsesMode string
		path          string
		protocol      string
	}{
		{name: "openai chat", platform: "openai", model: "gpt-test", path: "/v1/chat/completions", protocol: "openai"},
		{name: "openai responses", platform: "openai", model: "gpt-test", responsesMode: "force_responses", path: "/v1/responses", protocol: "responses"},
		{name: "anthropic messages", platform: "anthropic", model: "claude-test", path: "/v1/messages", protocol: "anthropic"},
		{name: "gemini native", platform: "gemini", model: "gemini-2.0-flash", path: "/v1beta/models/gemini-2.0-flash:streamGenerateContent", protocol: "gemini"},
		{name: "grok openai compatible", platform: "grok", model: "grok-test", path: "/v1/chat/completions", protocol: "grok"},
		{name: "antigravity claude", platform: "antigravity", model: "claude-test", path: "/antigravity/v1/messages", protocol: "antigravity"},
		{name: "antigravity gemini", platform: "antigravity", model: "gemini-2.0-flash", path: "/antigravity/v1beta/models/gemini-2.0-flash:streamGenerateContent", protocol: "antigravity-gemini"},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.Method != http.MethodPost || r.URL.Path != test.path {
					t.Fatalf("unexpected request: %s %s", r.Method, r.URL.String())
				}
				if got := r.Header.Get("Content-Type"); got != "application/json" {
					t.Fatalf("content type = %q", got)
				}
				if got := r.Header.Get("Accept"); got != "text/event-stream" {
					t.Fatalf("accept = %q", got)
				}
				if test.protocol == "gemini" || test.protocol == "antigravity-gemini" {
					if r.URL.Query().Get("alt") != "sse" || r.Header.Get("x-goog-api-key") != "direct-api-secret" {
						t.Fatalf("unexpected Gemini request: query=%s headers=%#v", r.URL.RawQuery, r.Header)
					}
				} else if test.protocol == "anthropic" {
					if r.Header.Get("x-api-key") != "direct-api-secret" || r.Header.Get("Authorization") != "" || r.Header.Get("anthropic-version") != "2023-06-01" {
						t.Fatalf("unexpected Anthropic headers: %#v", r.Header)
					}
				} else if test.protocol == "antigravity" {
					if r.Header.Get("x-api-key") != "direct-api-secret" || r.Header.Get("Authorization") != "Bearer direct-api-secret" {
						t.Fatalf("unexpected Antigravity headers: %#v", r.Header)
					}
				} else if r.Header.Get("Authorization") != "Bearer direct-api-secret" {
					t.Fatalf("unexpected OpenAI-compatible headers: %#v", r.Header)
				}

				var payload map[string]any
				if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
					t.Fatal(err)
				}
				if payload["model"] != nil && payload["model"] != test.model {
					t.Fatalf("model = %#v, want %q", payload["model"], test.model)
				}
				if payload["stream"] != nil && payload["stream"] != true {
					t.Fatalf("stream = %#v, want true", payload["stream"])
				}
				if got := directPayloadPrompt(payload); got != prompt {
					t.Fatalf("prompt = %q, want %q", got, prompt)
				}

				w.Header().Set("Content-Type", "text/event-stream")
				switch test.protocol {
				case "openai", "grok":
					_, _ = fmt.Fprint(w, "data: {\"choices\":[{\"delta\":{\"content\":\"ready\"}}]}\n\ndata: [DONE]\n\n")
				case "responses":
					_, _ = fmt.Fprint(w, "event: response.output_text.delta\ndata: {\"type\":\"response.output_text.delta\",\"delta\":\"ready\"}\n\nevent: response.completed\ndata: {\"type\":\"response.completed\"}\n\n")
				case "anthropic", "antigravity":
					_, _ = fmt.Fprint(w, "event: content_block_delta\ndata: {\"type\":\"content_block_delta\",\"delta\":{\"text\":\"ready\"}}\n\nevent: message_stop\ndata: {\"type\":\"message_stop\"}\n\n")
				case "gemini", "antigravity-gemini":
					_, _ = fmt.Fprint(w, "data: {\"candidates\":[{\"content\":{\"parts\":[{\"text\":\"ready\"}]},\"finishReason\":\"STOP\"}]}\n\n")
				}
			}))
			defer server.Close()

			snapshot := directTestSnapshot(test.platform, server.URL)
			snapshot.OpenAIResponsesMode = test.responsesMode
			snapshot.RoutingFingerprint = model.DirectProbeRoutingFingerprint(snapshot)
			policy := model.DefaultPolicy()
			policy.Model = test.model
			policy.Prompt = prompt

			outcome, err := newTestClient(t, server.URL).ProbeDirect(context.Background(), snapshot, policy)
			if err != nil {
				t.Fatal(err)
			}
			if !outcome.Success || outcome.ResponseText != "ready" || outcome.ErrorMessage != "" {
				t.Fatalf("unexpected outcome: success=%v text=%q message=%q", outcome.Success, outcome.ResponseText, outcome.ErrorMessage)
			}
		})
	}
}

func TestProbeDirectPreservesCredentialHeadersAndRejectsUnsupportedProxy(t *testing.T) {
	called := false
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		called = true
		if r.Header.Get("Authorization") != "Bearer direct-api-secret" {
			t.Fatalf("authorization was overridden: %#v", r.Header)
		}
		if r.Header.Get("Cookie") != "" || r.Header.Get("X-Relay-Mode") != "enabled" {
			t.Fatalf("unsafe or missing header override: %#v", r.Header)
		}
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = fmt.Fprint(w, "data: {\"choices\":[{\"delta\":{\"content\":\"ok\"}}]}\n\ndata: [DONE]\n\n")
	}))
	defer server.Close()

	snapshot := directTestSnapshot("openai", server.URL)
	snapshot.HeaderOverrides = map[string]string{
		"Authorization": "Bearer attacker-value",
		"Cookie":        "session=attacker-value",
		"X-Relay-Mode":  "enabled",
	}
	snapshot.RoutingFingerprint = model.DirectProbeRoutingFingerprint(snapshot)
	policy := model.DefaultPolicy()
	policy.Prompt = "custom"
	if outcome, err := newTestClient(t, server.URL).ProbeDirect(context.Background(), snapshot, policy); err != nil || !outcome.Success {
		t.Fatalf("safe header probe failed: outcome=%#v err=%v", outcome, err)
	}
	if !called {
		t.Fatal("upstream was not called")
	}

	called = false
	snapshot.Proxy = &model.DirectProbeProxy{Protocol: "socks5", Host: "127.0.0.1", Port: 1080, Status: "active"}
	snapshot.RoutingFingerprint = model.DirectProbeRoutingFingerprint(snapshot)
	_, err := newTestClient(t, server.URL).ProbeDirect(context.Background(), snapshot, policy)
	if err == nil || !strings.Contains(err.Error(), "代理类型") {
		t.Fatalf("unsupported proxy error = %v", err)
	}
	if called {
		t.Fatal("unsupported proxy bypassed the proxy requirement")
	}
}

func TestProbeDirectPreservesSanitizedHTTPErrorDetails(t *testing.T) {
	tests := []struct {
		name        string
		status      int
		contentType string
		body        string
		want        []string
	}{
		{
			name:        "structured OpenAI error",
			status:      http.StatusServiceUnavailable,
			contentType: "application/json",
			body:        `{"error":{"message":"Service temporarily unavailable for direct-api-secret at BASE_URL","type":"api_error","code":"upstream_overloaded"}}`,
			want:        []string{"直连上游服务异常", "HTTP 503 Service Unavailable", "Service temporarily unavailable", "type=api_error", "code=upstream_overloaded"},
		},
		{
			name:        "flat NewAPI error",
			status:      http.StatusTooManyRequests,
			contentType: "application/json",
			body:        `{"message":"当前分组额度不足","code":"insufficient_quota"}`,
			want:        []string{"直连上游当前限流", "HTTP 429 Too Many Requests", "当前分组额度不足", "code=insufficient_quota"},
		},
		{
			name:        "HTML gateway error",
			status:      http.StatusBadGateway,
			contentType: "text/html",
			body:        `<html><head><title>Cloud gateway unavailable</title></head><body>ignored</body></html>`,
			want:        []string{"直连上游服务异常", "HTTP 502 Bad Gateway", "Cloud gateway unavailable"},
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			var server *httptest.Server
			server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				w.Header().Set("Content-Type", test.contentType)
				w.WriteHeader(test.status)
				_, _ = io.WriteString(w, strings.ReplaceAll(test.body, "BASE_URL", server.URL))
			}))
			defer server.Close()

			snapshot := directTestSnapshot("openai", server.URL)
			snapshot.HeaderOverrides = map[string]string{"X-Relay-Secret": "header-override-secret"}
			snapshot.RoutingFingerprint = model.DirectProbeRoutingFingerprint(snapshot)
			policy := model.DefaultPolicy()
			policy.Prompt = "custom"
			_, err := newTestClient(t, server.URL).ProbeDirect(context.Background(), snapshot, policy)
			if err == nil {
				t.Fatal("direct probe unexpectedly succeeded")
			}
			message := err.Error()
			for _, expected := range test.want {
				if !strings.Contains(message, expected) {
					t.Fatalf("error %q is missing %q", message, expected)
				}
			}
			for _, forbidden := range []string{"direct-api-secret", server.URL, "header-override-secret"} {
				if strings.Contains(message, forbidden) {
					t.Fatalf("error exposed %q: %s", forbidden, message)
				}
			}
		})
	}
}

func TestDirectProbeInsufficientBalanceClassificationRequiresForbiddenStatusAndPhrase(t *testing.T) {
	snapshot := directTestSnapshot("openai", "https://relay.example/v1")
	tests := []struct {
		name   string
		status int
		body   string
		want   bool
	}{
		{name: "Chinese user quota", status: http.StatusForbidden, body: `{"message":"用户额度不足, 剩余额度: ¥-0.000358"}`, want: true},
		{name: "Chinese balance exhausted", status: http.StatusForbidden, body: `{"error":{"message":"余额已耗尽"}}`, want: true},
		{name: "English insufficient quota", status: http.StatusForbidden, body: `{"message":"INSUFFICIENT QUOTA for this request"}`, want: true},
		{name: "unrelated forbidden", status: http.StatusForbidden, body: `{"message":"region policy denied this request"}`},
		{name: "same phrase on rate limit", status: http.StatusTooManyRequests, body: `{"message":"用户额度不足"}`},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			err := directProbeHTTPStatusError(test.status, strings.NewReader(test.body), snapshot)
			statusErr, ok := err.(*DirectProbeHTTPError)
			if !ok || statusErr.StatusCode != test.status {
				t.Fatalf("HTTP status was not retained: %#v", err)
			}
			if got := IsDirectProbeInsufficientBalance(fmt.Errorf("wrapped: %w", err)); got != test.want {
				t.Fatalf("classification = %v, want %v for %q", got, test.want, err)
			}
		})
	}
}

func TestProbeDirectPreservesSanitizedSSEErrorDetails(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = fmt.Fprint(w, "event: error\ndata: {\"type\":\"error\",\"error\":{\"message\":\"quota exhausted for direct-api-secret\",\"type\":\"invalid_request_error\",\"code\":\"rate_limit\"}}\n\n")
	}))
	defer server.Close()

	snapshot := directTestSnapshot("openai", server.URL)
	policy := model.DefaultPolicy()
	policy.Prompt = "custom"
	_, err := newTestClient(t, server.URL).ProbeDirect(context.Background(), snapshot, policy)
	if err == nil {
		t.Fatal("direct SSE probe unexpectedly succeeded")
	}
	message := err.Error()
	for _, expected := range []string{"直连上游流返回错误", "quota exhausted", "type=invalid_request_error", "code=rate_limit", "[REDACTED]"} {
		if !strings.Contains(message, expected) {
			t.Fatalf("error %q is missing %q", message, expected)
		}
	}
	if strings.Contains(message, "direct-api-secret") {
		t.Fatalf("SSE error exposed API key: %s", message)
	}
}

func TestSanitizeDirectProbeErrorTextRedactsCredentialShapes(t *testing.T) {
	snapshot := directTestSnapshot("openai", "https://relay.example/v1")
	snapshot.HeaderOverrides = map[string]string{"X-Relay-Secret": "header-override-secret"}
	snapshot.Proxy = &model.DirectProbeProxy{
		Protocol: "http", Host: "proxy.internal", Port: 8080,
		Username: "proxy-user", Password: "proxy-password", Status: "active",
	}
	input := `upstream overloaded at https://relay.example/v1 via http://proxy-user:proxy-password@proxy.internal:8080; ` +
		`Authorization: Bearer eyJhbGciOiJIUzI1NiJ9.abcdefghijk.abcdefghijk; ` +
		`"api_key":"sk-upstream-secret123"; Cookie=session-secret; X-Relay-Secret=header-override-secret`
	got := sanitizeDirectProbeErrorText(input, snapshot)
	for _, expected := range []string{"upstream overloaded", "Authorization=[REDACTED]", "api_key=[REDACTED]", "Cookie=[REDACTED]"} {
		if !strings.Contains(got, expected) {
			t.Fatalf("sanitized error %q is missing %q", got, expected)
		}
	}
	for _, forbidden := range []string{
		snapshot.APIKey, snapshot.BaseURL, snapshot.Proxy.Username, snapshot.Proxy.Password,
		snapshot.Proxy.Host, "header-override-secret", "sk-upstream-secret123", "eyJhbGciOiJIUzI1NiJ9",
	} {
		if strings.Contains(got, forbidden) {
			t.Fatalf("sanitized error exposed %q: %s", forbidden, got)
		}
	}
	if len(got) > maxDirectErrorBytes {
		t.Fatalf("sanitized error length = %d, want <= %d", len(got), maxDirectErrorBytes)
	}
}

func TestDirectGeminiEndpointNormalizesVersionPathAndRejectsUnsafeModels(t *testing.T) {
	tests := []struct {
		baseURL string
		model   string
		want    string
	}{
		{baseURL: "https://relay.example/v1", model: "gemini-2.0-flash", want: "https://relay.example/v1beta/models/gemini-2.0-flash:streamGenerateContent?alt=sse"},
		{baseURL: "https://relay.example/prefix/v1beta", model: "models/gemini-2.0-flash", want: "https://relay.example/prefix/v1beta/models/gemini-2.0-flash:streamGenerateContent?alt=sse"},
		{baseURL: "https://relay.example/prefix", model: "gemini-2.0-flash", want: "https://relay.example/prefix/v1beta/models/gemini-2.0-flash:streamGenerateContent?alt=sse"},
	}
	for _, test := range tests {
		if got := directGeminiEndpoint(test.baseURL, test.model); got != test.want {
			t.Fatalf("directGeminiEndpoint(%q, %q) = %q, want %q", test.baseURL, test.model, got, test.want)
		}
	}
	for _, modelID := range []string{"", ".", "../other", "gemini/other", "gemini?alt=evil", "gemini%2Fother"} {
		if got := directGeminiEndpoint("https://relay.example", modelID); got != "" {
			t.Fatalf("unsafe model %q produced endpoint %q", modelID, got)
		}
	}
}

func TestDirectProbePromptAndSSECompletion(t *testing.T) {
	custom := " \ncustom probe stays verbatim\n "
	prompt, err := newDirectProbePrompt(custom)
	if err != nil {
		t.Fatal(err)
	}
	if prompt.Text != custom || prompt.Expected != "" {
		t.Fatalf("custom prompt was changed: %#v", prompt)
	}

	defaultPrompt, err := newDirectProbePrompt(model.DefaultPrompt)
	if err != nil || defaultPrompt.Expected == "" || defaultPrompt.Text == model.DefaultPrompt {
		t.Fatalf("default prompt was not converted into a checked challenge: %#v err=%v", defaultPrompt, err)
	}
	if !directProbeAnswerMatches("The answer is "+defaultPrompt.Expected, defaultPrompt.Expected) || directProbeAnswerMatches("wrong", defaultPrompt.Expected) {
		t.Fatal("default arithmetic answer validation is incorrect")
	}

	for range 32 {
		state, err := consumeDirectSSE(
			context.Background(),
			io.NopCloser(strings.NewReader("data: {\"choices\":[{\"delta\":{\"content\":\"final\"}}]}\n\ndata: [DONE]\n\n")),
			directProbeOpenAIChat,
		)
		if err != nil || !state.completed || state.text.String() != "final" {
			t.Fatalf("final buffered SSE event was lost: state=%#v err=%v", state, err)
		}
	}
}

func TestDirectProbeRequestTimeoutFollowsPolicy(t *testing.T) {
	tests := []struct {
		name  string
		limit int64
		want  time.Duration
	}{
		{name: "minimum fallback", limit: 0, want: time.Duration(model.MinLatencyLimitMS)*time.Millisecond + directProbeTimeoutGrace},
		{name: "configured limit", limit: 45_000, want: 45*time.Second + directProbeTimeoutGrace},
		{name: "maximum clamp", limit: model.MaxLatencyLimitMS + 1, want: time.Duration(model.MaxLatencyLimitMS)*time.Millisecond + directProbeTimeoutGrace},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			policy := model.DefaultPolicy()
			policy.LatencyLimitMS = test.limit
			got := directProbeRequestTimeout(policy)
			if got != test.want {
				t.Fatalf("directProbeRequestTimeout() = %s, want %s", got, test.want)
			}
			client := newDirectProbeHTTPClient(nil, got)
			transport, ok := client.Transport.(*http.Transport)
			if !ok || transport.ResponseHeaderTimeout != got {
				t.Fatalf("response header timeout = %#v, want %s", client.Transport, got)
			}
		})
	}
}

func directTestSnapshot(platform, baseURL string) model.DirectProbeSnapshot {
	snapshot := model.DirectProbeSnapshot{
		Version:   model.DirectProbeSnapshotVersion,
		AccountID: 7,
		Platform:  platform,
		BaseURL:   baseURL,
		APIKey:    "direct-api-secret",
	}
	snapshot.RoutingFingerprint = model.DirectProbeRoutingFingerprint(snapshot)
	return snapshot
}

func directPayloadPrompt(payload map[string]any) string {
	if input, ok := payload["input"].(string); ok {
		return input
	}
	if messages, ok := payload["messages"].([]any); ok && len(messages) > 0 {
		if message, ok := messages[0].(map[string]any); ok {
			text, _ := message["content"].(string)
			return text
		}
	}
	if contents, ok := payload["contents"].([]any); ok && len(contents) > 0 {
		if content, ok := contents[0].(map[string]any); ok {
			if parts, ok := content["parts"].([]any); ok && len(parts) > 0 {
				if part, ok := parts[0].(map[string]any); ok {
					text, _ := part["text"].(string)
					return text
				}
			}
		}
	}
	return ""
}
