package core

import (
	"bufio"
	"bytes"
	"context"
	cryptorand "crypto/rand"
	"encoding/json"
	"errors"
	"fmt"
	"html"
	"io"
	"math/big"
	"net"
	"net/http"
	"net/url"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/Wei-Shaw/sub2api-account-auto-scheduler/internal/model"
)

const (
	directProbeTimeoutGrace = 5 * time.Second
	directProbeIdleTimeout  = 25 * time.Second
	maxDirectSSELineBytes   = 256 << 10
	maxDirectStreamBytes    = 4 << 20
	maxDirectErrorBytes     = 320
)

var (
	directProbeHTMLTitlePattern           = regexp.MustCompile(`(?is)<title[^>]*>(.*?)</title>`)
	directProbeHTMLTagPattern             = regexp.MustCompile(`(?s)<[^>]*>`)
	directProbeBearerPattern              = regexp.MustCompile(`(?i)\bbearer\s+[A-Za-z0-9._~+/=-]+`)
	directProbeJWTLikePattern             = regexp.MustCompile(`\beyJ[A-Za-z0-9_-]{8,}\.[A-Za-z0-9_-]{8,}\.[A-Za-z0-9_-]{8,}\b`)
	directProbeKeyLikePattern             = regexp.MustCompile(`\b(?:sk|key)-[A-Za-z0-9][A-Za-z0-9._-]{7,}\b`)
	directProbeCredentialURLPattern       = regexp.MustCompile(`(?i)\b(https?|socks5h?)://[^@\s/]+@`)
	directProbeSensitiveAssignmentPattern = regexp.MustCompile(`(?i)(^|[\s,{;])["']?(api[_ -]?key|x-api-key|x-goog-api-key|authorization|proxy-authorization|cookie|set-cookie|password|access[_ -]?token|refresh[_ -]?token|token|secret)["']?\s*[:=]\s*(?:"[^"]*"|'[^']*'|[^\s,}\]]+)`)
)

var directProbeInsufficientBalancePhrases = []string{
	"用户额度不足",
	"额度不足",
	"余额不足",
	"余额耗尽",
	"余额已耗尽",
	"额度耗尽",
	"额度已耗尽",
	"insufficient balance",
	"insufficient quota",
	"quota exhausted",
}

// DirectProbeHTTPError retains the upstream status code while exposing only
// the already-sanitized operator-facing message.
type DirectProbeHTTPError struct {
	StatusCode int
	Message    string
}

func (e *DirectProbeHTTPError) Error() string {
	if e == nil {
		return ""
	}
	return e.Message
}

// IsDirectProbeInsufficientBalance reports a conservative balance failure:
// the error must be a real direct-probe HTTP 403 and include an explicit
// insufficient/exhausted balance phrase.
func IsDirectProbeInsufficientBalance(err error) bool {
	var statusErr *DirectProbeHTTPError
	if !errors.As(err, &statusErr) || statusErr.StatusCode != http.StatusForbidden {
		return false
	}
	message := strings.ToLower(statusErr.Message)
	for _, phrase := range directProbeInsufficientBalancePhrases {
		if strings.Contains(message, phrase) {
			return true
		}
	}
	return false
}

// DirectProber is kept separate from the legacy Sub2API account-test client so
// a direct source can never silently fall back to that route.
type DirectProber interface {
	ProbeDirect(ctx context.Context, snapshot model.DirectProbeSnapshot, policy model.Policy) (ProbeOutcome, error)
}

type directProbeProtocol string

const (
	directProbeOpenAIChat      directProbeProtocol = "openai_chat"
	directProbeOpenAIResponses directProbeProtocol = "openai_responses"
	directProbeAnthropic       directProbeProtocol = "anthropic"
	directProbeGemini          directProbeProtocol = "gemini"
)

type directPrompt struct {
	Text     string
	Expected string
}

type directStreamState struct {
	text      strings.Builder
	completed bool
	usage     *model.ProbeUsage
}

// ProbeDirect sends a streaming health check directly to a previously
// authorized upstream. Snapshot validation is deliberately repeated here so a
// caller cannot make a credentialed request with malformed persisted state.
func (c *Client) ProbeDirect(ctx context.Context, snapshot model.DirectProbeSnapshot, policy model.Policy) (ProbeOutcome, error) {
	startedAt := time.Now()
	if err := validateDirectSnapshot(snapshot); err != nil {
		return ProbeOutcome{Latency: time.Since(startedAt)}, err
	}
	baseURL, err := model.EffectiveDirectProbeBaseURL(snapshot.BaseURL, snapshot.Platform)
	if err != nil {
		return ProbeOutcome{Latency: time.Since(startedAt)}, errors.New("直连探测上游地址无效")
	}
	snapshot.BaseURL = baseURL
	proxyURL, err := model.DirectProbeProxyURL(snapshot.Proxy, time.Now())
	if err != nil {
		return ProbeOutcome{Latency: time.Since(startedAt)}, err
	}
	prompt, err := newDirectProbePrompt(policy.Prompt)
	if err != nil {
		return ProbeOutcome{Latency: time.Since(startedAt)}, errors.New("生成默认检测题目失败")
	}
	upstreamModel := model.ResolveDirectProbeModel(snapshot, policy.Model)
	if strings.TrimSpace(upstreamModel) == "" {
		return ProbeOutcome{Latency: time.Since(startedAt)}, errors.New("直连探测模型为空")
	}

	protocol := chooseDirectProbeProtocol(snapshot, upstreamModel)
	payload, endpoint, err := buildDirectProbeRequest(snapshot, protocol, upstreamModel, prompt.Text, policy.ReasoningEffort)
	if err != nil {
		return ProbeOutcome{Latency: time.Since(startedAt)}, err
	}

	requestTimeout := directProbeRequestTimeout(policy)
	requestCtx, cancel := context.WithTimeout(ctx, requestTimeout)
	defer cancel()
	req, err := http.NewRequestWithContext(requestCtx, http.MethodPost, endpoint, bytes.NewReader(payload))
	if err != nil {
		return ProbeOutcome{Latency: time.Since(startedAt)}, errors.New("创建直连探测请求失败")
	}
	setDirectProbeHeaders(req, snapshot, protocol)

	httpClient := newDirectProbeHTTPClient(proxyURL, requestTimeout)
	resp, err := httpClient.Do(req)
	if err != nil {
		if errors.Is(err, context.DeadlineExceeded) || errors.Is(requestCtx.Err(), context.DeadlineExceeded) {
			return ProbeOutcome{Latency: time.Since(startedAt)}, context.DeadlineExceeded
		}
		if errors.Is(err, context.Canceled) || errors.Is(requestCtx.Err(), context.Canceled) {
			return ProbeOutcome{Latency: time.Since(startedAt)}, context.Canceled
		}
		detail := sanitizeDirectProbeErrorText(err.Error(), snapshot)
		if detail == "" {
			return ProbeOutcome{Latency: time.Since(startedAt)}, errors.New("直连上游网络请求失败")
		}
		return ProbeOutcome{Latency: time.Since(startedAt)}, fmt.Errorf("直连上游网络请求失败: %s", detail)
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode < http.StatusOK || resp.StatusCode >= http.StatusMultipleChoices {
		return ProbeOutcome{Latency: time.Since(startedAt)}, directProbeHTTPStatusError(resp.StatusCode, resp.Body, snapshot)
	}

	state, err := consumeDirectSSEWithIdleTimeout(requestCtx, resp.Body, protocol, requestTimeout)
	if err != nil {
		if errors.Is(err, context.DeadlineExceeded) || errors.Is(requestCtx.Err(), context.DeadlineExceeded) {
			return ProbeOutcome{Latency: time.Since(startedAt), ResponseText: state.text.String(), Usage: state.usage}, context.DeadlineExceeded
		}
		detail := sanitizeDirectProbeErrorText(err.Error(), snapshot)
		if detail == "" {
			detail = "读取直连上游流失败"
		}
		return ProbeOutcome{Latency: time.Since(startedAt), ResponseText: state.text.String(), Usage: state.usage}, errors.New(detail)
	}
	responseText := strings.TrimSpace(state.text.String())
	if !state.completed {
		return ProbeOutcome{Latency: time.Since(startedAt), ResponseText: responseText, ErrorMessage: "直连流在完成前结束", Usage: state.usage}, nil
	}
	if responseText == "" {
		return ProbeOutcome{Latency: time.Since(startedAt), ErrorMessage: "直连流未返回文本", Usage: state.usage}, nil
	}
	if prompt.Expected != "" && !directProbeAnswerMatches(responseText, prompt.Expected) {
		return ProbeOutcome{Latency: time.Since(startedAt), ResponseText: responseText, ErrorMessage: "默认算术题答案不正确", Usage: state.usage}, nil
	}
	if state.usage != nil && strings.TrimSpace(state.usage.Model) == "" {
		state.usage.Model = upstreamModel
	}
	return ProbeOutcome{Success: true, ResponseText: responseText, Latency: time.Since(startedAt), Usage: state.usage}, nil
}

// directProbeRequestTimeout keeps an untrusted upstream bounded without
// imposing a shorter fixed timeout than the administrator's configured latency
// limit. The engine also applies this bound to the full direct probe context.
func directProbeRequestTimeout(policy model.Policy) time.Duration {
	limitMS := policy.LatencyLimitMS
	if limitMS < model.MinLatencyLimitMS {
		limitMS = model.MinLatencyLimitMS
	}
	if limitMS > model.MaxLatencyLimitMS {
		limitMS = model.MaxLatencyLimitMS
	}
	return time.Duration(limitMS)*time.Millisecond + directProbeTimeoutGrace
}

func validateDirectSnapshot(snapshot model.DirectProbeSnapshot) error {
	if snapshot.Version != model.DirectProbeSnapshotVersion || snapshot.AccountID <= 0 || strings.TrimSpace(snapshot.APIKey) == "" || !model.IsDirectProbePlatformSupported(snapshot.Platform) {
		return errors.New("直连探测授权信息无效，请重新授权")
	}
	if strings.TrimSpace(snapshot.BaseURL) == "" {
		return errors.New("直连探测上游地址无效")
	}
	if fingerprint := model.DirectProbeRoutingFingerprint(snapshot); snapshot.RoutingFingerprint == "" || fingerprint != snapshot.RoutingFingerprint {
		return errors.New("直连探测授权信息已过期，请重新授权")
	}
	return nil
}

func newDirectProbePrompt(configured string) (directPrompt, error) {
	if strings.TrimSpace(configured) == "" {
		configured = model.DefaultPrompt
	}
	if configured != model.DefaultPrompt {
		return directPrompt{Text: configured}, nil
	}
	left, err := directRandomInt(11, 79)
	if err != nil {
		return directPrompt{}, err
	}
	right, err := directRandomInt(4, 39)
	if err != nil {
		return directPrompt{}, err
	}
	operator := "+"
	expected := left + right
	if left > right && left%2 == 0 {
		operator = "-"
		expected = left - right
	}
	return directPrompt{
		Text:     fmt.Sprintf("Calculate and respond with ONLY the number, nothing else.\n\nQ: %d %s %d = ?\nA:", left, operator, right),
		Expected: strconv.Itoa(expected),
	}, nil
}

func directRandomInt(minimum, maximum int) (int, error) {
	if maximum < minimum {
		return 0, errors.New("invalid random range")
	}
	value, err := cryptorand.Int(cryptorand.Reader, big.NewInt(int64(maximum-minimum+1)))
	if err != nil {
		return 0, err
	}
	return minimum + int(value.Int64()), nil
}

func directProbeAnswerMatches(response, expected string) bool {
	for _, field := range strings.FieldsFunc(response, func(character rune) bool {
		return (character < '0' || character > '9') && character != '-'
	}) {
		if strings.TrimSpace(field) == expected {
			return true
		}
	}
	return false
}

func chooseDirectProbeProtocol(snapshot model.DirectProbeSnapshot, modelID string) directProbeProtocol {
	switch model.NormalizeDirectProbePlatform(snapshot.Platform) {
	case "anthropic":
		return directProbeAnthropic
	case "gemini":
		return directProbeGemini
	case "antigravity":
		if strings.HasPrefix(strings.ToLower(strings.TrimSpace(modelID)), "gemini-") {
			return directProbeGemini
		}
		return directProbeAnthropic
	case "openai":
		if strings.TrimSpace(snapshot.OpenAIResponsesMode) == "force_responses" {
			return directProbeOpenAIResponses
		}
		return directProbeOpenAIChat
	case "grok":
		return directProbeOpenAIChat
	default:
		return directProbeOpenAIChat
	}
}

func buildDirectProbeRequest(snapshot model.DirectProbeSnapshot, protocol directProbeProtocol, modelID, prompt, effort string) ([]byte, string, error) {
	var body any
	var endpoint string
	switch protocol {
	case directProbeOpenAIChat:
		body = map[string]any{
			"model":          modelID,
			"stream":         true,
			"stream_options": map[string]bool{"include_usage": true},
			"max_tokens":     128,
			"messages": []map[string]string{{
				"role": "user", "content": prompt,
			}},
		}
		if effort != "" && effort != "none" {
			body.(map[string]any)["reasoning_effort"] = effort
		}
		endpoint = directProbeEndpoint(snapshot.BaseURL, "v1/chat/completions")
	case directProbeOpenAIResponses:
		body = map[string]any{
			"model":             modelID,
			"input":             prompt,
			"stream":            true,
			"max_output_tokens": 128,
		}
		if effort != "" && effort != "none" {
			body.(map[string]any)["reasoning"] = map[string]string{"effort": effort}
		}
		endpoint = directProbeEndpoint(snapshot.BaseURL, "v1/responses")
	case directProbeAnthropic:
		body = map[string]any{
			"model":      modelID,
			"max_tokens": 128,
			"stream":     true,
			"messages": []map[string]string{{
				"role": "user", "content": prompt,
			}},
		}
		if effort != "" && effort != "none" {
			budget := 1024
			if effort == "medium" {
				budget = 4096
			}
			if effort == "high" || effort == "xhigh" {
				budget = 8192
			}
			body.(map[string]any)["thinking"] = map[string]any{"type": "enabled", "budget_tokens": budget}
		}
		endpoint = directProbeEndpoint(snapshot.BaseURL, "v1/messages")
	case directProbeGemini:
		body = map[string]any{
			"contents": []map[string]any{{
				"role":  "user",
				"parts": []map[string]string{{"text": prompt}},
			}},
			"generationConfig": map[string]any{"maxOutputTokens": 128},
		}
		if effort != "" && effort != "none" {
			budget := 1024
			if effort == "medium" {
				budget = 4096
			}
			if effort == "high" || effort == "xhigh" {
				budget = 8192
			}
			body.(map[string]any)["generationConfig"].(map[string]any)["thinkingConfig"] = map[string]int{"thinkingBudget": budget}
		}
		endpoint = directGeminiEndpoint(snapshot.BaseURL, modelID)
	default:
		return nil, "", errors.New("直连探测协议不受支持")
	}
	if endpoint == "" {
		return nil, "", errors.New("直连探测上游地址无效")
	}
	raw, err := json.Marshal(body)
	if err != nil || len(raw) > 64<<10 {
		return nil, "", errors.New("编码直连探测请求失败")
	}
	return raw, endpoint, nil
}

func directProbeEndpoint(baseURL, suffix string) string {
	parsed, err := url.Parse(strings.TrimSpace(baseURL))
	if err != nil || parsed.Host == "" {
		return ""
	}
	basePath := strings.TrimRight(parsed.Path, "/")
	cleanSuffix := strings.TrimLeft(suffix, "/")
	for _, version := range []string{"v1", "v1beta"} {
		if strings.HasSuffix(strings.ToLower(basePath), "/"+version) && strings.HasPrefix(strings.ToLower(cleanSuffix), version+"/") {
			cleanSuffix = cleanSuffix[len(version)+1:]
			break
		}
	}
	parsed.Path = basePath + "/" + cleanSuffix
	parsed.RawPath = ""
	parsed.RawQuery = ""
	parsed.Fragment = ""
	return parsed.String()
}

func directGeminiEndpoint(baseURL, modelID string) string {
	modelID = strings.TrimPrefix(strings.TrimSpace(modelID), "models/")
	if !isSafeDirectGeminiModelID(modelID) {
		return ""
	}
	parsed, err := url.Parse(strings.TrimSpace(baseURL))
	if err != nil || parsed.Host == "" {
		return ""
	}
	basePath := strings.TrimRight(parsed.Path, "/")
	lowerPath := strings.ToLower(basePath)
	// A number of OpenAI-compatible relays are configured with /v1. Gemini
	// native endpoints live at the sibling /v1beta path, not /v1/v1beta.
	switch {
	case strings.HasSuffix(lowerPath, "/v1beta"):
		basePath = basePath[:len(basePath)-len("/v1beta")]
	case strings.HasSuffix(lowerPath, "/v1"):
		basePath = basePath[:len(basePath)-len("/v1")]
	}
	parsed.Path = basePath + "/v1beta/models/" + modelID + ":streamGenerateContent"
	parsed.RawPath = ""
	query := parsed.Query()
	query.Set("alt", "sse")
	parsed.RawQuery = query.Encode()
	parsed.Fragment = ""
	return parsed.String()
}

// Gemini model IDs become a URL path segment. Keep the accepted form narrow so
// model mappings cannot alter the upstream request path.
func isSafeDirectGeminiModelID(modelID string) bool {
	if modelID == "" || len(modelID) > 128 {
		return false
	}
	dotsOnly := true
	for index := 0; index < len(modelID); index++ {
		character := modelID[index]
		switch {
		case character >= 'a' && character <= 'z', character >= 'A' && character <= 'Z', character >= '0' && character <= '9', character == '_', character == '-', character == '.':
			if character != '.' {
				dotsOnly = false
			}
		default:
			return false
		}
	}
	return !dotsOnly
}

func setDirectProbeHeaders(req *http.Request, snapshot model.DirectProbeSnapshot, protocol directProbeProtocol) {
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "text/event-stream")
	req.Header.Set("Accept-Encoding", "identity")
	switch protocol {
	case directProbeOpenAIChat, directProbeOpenAIResponses:
		req.Header.Set("Authorization", "Bearer "+snapshot.APIKey)
	case directProbeAnthropic:
		req.Header.Set("anthropic-version", "2023-06-01")
		if model.NormalizeDirectProbePlatform(snapshot.Platform) == "antigravity" {
			req.Header.Set("Authorization", "Bearer "+snapshot.APIKey)
			req.Header.Set("x-api-key", snapshot.APIKey)
		} else if strings.TrimSpace(snapshot.AnthropicAuthMode) == "authorization_bearer" {
			req.Header.Set("Authorization", "Bearer "+snapshot.APIKey)
		} else {
			req.Header.Set("x-api-key", snapshot.APIKey)
		}
	case directProbeGemini:
		req.Header.Set("x-goog-api-key", snapshot.APIKey)
	}
	for name, value := range model.FilterDirectProbeHeaderOverrides(true, snapshot.HeaderOverrides) {
		req.Header.Set(name, value)
	}
}

func newDirectProbeHTTPClient(proxyURL *url.URL, responseHeaderTimeout time.Duration) *http.Client {
	transport := http.DefaultTransport.(*http.Transport).Clone()
	transport.Proxy = http.ProxyFromEnvironment
	transport.MaxIdleConns = 8
	transport.MaxIdleConnsPerHost = 2
	transport.IdleConnTimeout = 30 * time.Second
	transport.ResponseHeaderTimeout = responseHeaderTimeout
	transport.TLSHandshakeTimeout = 10 * time.Second
	transport.ExpectContinueTimeout = time.Second
	if proxyURL != nil {
		transport.Proxy = http.ProxyURL(proxyURL)
	}
	return &http.Client{
		Transport: transport,
		CheckRedirect: func(*http.Request, []*http.Request) error {
			return http.ErrUseLastResponse
		},
	}
}

func directProbeHTTPStatusError(status int, body io.Reader, snapshot model.DirectProbeSnapshot) error {
	base := "直连上游请求被拒绝"
	switch status {
	case http.StatusUnauthorized:
		base = "直连上游鉴权失败"
	case http.StatusForbidden:
		base = "直连上游拒绝访问"
	case http.StatusNotFound, http.StatusMethodNotAllowed:
		base = "直连上游不支持该探测接口"
	case http.StatusTooManyRequests:
		base = "直连上游当前限流"
	default:
		if status >= http.StatusInternalServerError {
			base = "直连上游服务异常"
		}
	}
	statusLabel := fmt.Sprintf("HTTP %d", status)
	if text := strings.TrimSpace(http.StatusText(status)); text != "" {
		statusLabel += " " + text
	}
	message := fmt.Sprintf("%s (%s)", base, statusLabel)
	raw, _ := io.ReadAll(io.LimitReader(body, maxErrorBodyBytes+1))
	if len(raw) > maxErrorBodyBytes {
		raw = raw[:maxErrorBodyBytes]
	}
	detail := sanitizeDirectProbeErrorText(directProbeErrorDetail(raw), snapshot)
	if detail != "" && !strings.EqualFold(detail, http.StatusText(status)) {
		message += ": " + detail
	}
	return &DirectProbeHTTPError{StatusCode: status, Message: message}
}

func directProbeErrorDetail(raw []byte) string {
	raw = bytes.TrimSpace(raw)
	if len(raw) == 0 {
		return ""
	}
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.UseNumber()
	var payload any
	if decoder.Decode(&payload) == nil {
		message := firstDirectProbeJSONScalar(payload,
			[]string{"error", "message"},
			[]string{"error", "detail"},
			[]string{"message"},
			[]string{"detail"},
			[]string{"data", "message"},
			[]string{"error"},
		)
		errorType := firstDirectProbeJSONScalar(payload, []string{"error", "type"}, []string{"type"})
		code := firstDirectProbeJSONScalar(payload, []string{"error", "code"}, []string{"code"}, []string{"error", "status"}, []string{"status"})
		metadata := make([]string, 0, 2)
		if errorType != "" && !strings.EqualFold(errorType, message) && !strings.EqualFold(errorType, "error") {
			metadata = append(metadata, "type="+errorType)
		}
		if code != "" && !strings.EqualFold(code, message) && !strings.EqualFold(code, errorType) {
			metadata = append(metadata, "code="+code)
		}
		if message == "" {
			return strings.Join(metadata, ", ")
		}
		if len(metadata) > 0 {
			message += " [" + strings.Join(metadata, ", ") + "]"
		}
		return message
	}
	text := string(raw)
	if match := directProbeHTMLTitlePattern.FindStringSubmatch(text); len(match) == 2 {
		text = match[1]
	} else {
		text = directProbeHTMLTagPattern.ReplaceAllString(text, " ")
	}
	return html.UnescapeString(text)
}

func firstDirectProbeJSONScalar(payload any, paths ...[]string) string {
	for _, path := range paths {
		value := payload
		valid := true
		for _, segment := range path {
			object, ok := value.(map[string]any)
			if !ok {
				valid = false
				break
			}
			value, ok = directProbeMapValue(object, segment)
			if !ok {
				valid = false
				break
			}
		}
		if valid {
			if scalar := directProbeScalarString(value); scalar != "" {
				return scalar
			}
		}
	}
	return ""
}

func directProbeMapValue(object map[string]any, key string) (any, bool) {
	for candidate, value := range object {
		if strings.EqualFold(strings.TrimSpace(candidate), key) {
			return value, true
		}
	}
	return nil, false
}

func directProbeScalarString(value any) string {
	switch typed := value.(type) {
	case string:
		return strings.TrimSpace(typed)
	case json.Number:
		return strings.TrimSpace(typed.String())
	case float64, float32, int, int64, int32, uint, uint64, uint32:
		return strings.TrimSpace(fmt.Sprint(typed))
	default:
		return ""
	}
}

func sanitizeDirectProbeErrorText(text string, snapshot model.DirectProbeSnapshot) string {
	text = strings.TrimSpace(text)
	if text == "" {
		return ""
	}
	secrets := []string{snapshot.APIKey, snapshot.BaseURL}
	if snapshot.Proxy != nil {
		secrets = append(secrets,
			snapshot.Proxy.Username,
			snapshot.Proxy.Password,
			snapshot.Proxy.Host,
			net.JoinHostPort(snapshot.Proxy.Host, strconv.Itoa(snapshot.Proxy.Port)),
		)
	}
	for _, value := range snapshot.HeaderOverrides {
		secrets = append(secrets, value)
	}
	sort.SliceStable(secrets, func(left, right int) bool { return len(secrets[left]) > len(secrets[right]) })
	for _, secret := range secrets {
		if secret = strings.TrimSpace(secret); secret != "" {
			text = strings.ReplaceAll(text, secret, "[REDACTED]")
			if escaped := url.QueryEscape(secret); escaped != secret {
				text = strings.ReplaceAll(text, escaped, "[REDACTED]")
			}
		}
	}
	text = directProbeCredentialURLPattern.ReplaceAllString(text, "$1://[REDACTED]@")
	text = directProbeSensitiveAssignmentPattern.ReplaceAllString(text, "${1}${2}=[REDACTED]")
	text = directProbeBearerPattern.ReplaceAllString(text, "Bearer [REDACTED]")
	text = directProbeJWTLikePattern.ReplaceAllString(text, "[REDACTED]")
	text = directProbeKeyLikePattern.ReplaceAllString(text, "[REDACTED]")
	text = strings.Join(strings.Fields(strings.ReplaceAll(text, "\x00", " ")), " ")
	return truncateDirectProbeError(text, maxDirectErrorBytes)
}

func truncateDirectProbeError(text string, limit int) string {
	if limit <= 0 || len(text) <= limit {
		return text
	}
	const suffix = "..."
	if limit <= len(suffix) {
		return suffix[:limit]
	}
	end := limit - len(suffix)
	for end > 0 && text[end]&0xc0 == 0x80 {
		end--
	}
	if end == 0 {
		return suffix
	}
	return text[:end] + suffix
}

type directSSELine struct {
	line []byte
}

func consumeDirectSSE(ctx context.Context, body io.ReadCloser, protocol directProbeProtocol) (directStreamState, error) {
	return consumeDirectSSEWithIdleTimeout(ctx, body, protocol, directProbeIdleTimeout)
}

func consumeDirectSSEWithIdleTimeout(ctx context.Context, body io.ReadCloser, protocol directProbeProtocol, idleTimeout time.Duration) (directStreamState, error) {
	if idleTimeout <= 0 {
		idleTimeout = directProbeIdleTimeout
	}
	streamCtx, cancel := context.WithCancel(ctx)
	defer cancel()
	lines := make(chan directSSELine, 1)
	done := make(chan error, 1)
	go readDirectSSELines(streamCtx, body, lines, done)

	state := directStreamState{}
	var eventName string
	var readerErr error
	readerDone := false
	timer := time.NewTimer(idleTimeout)
	defer timer.Stop()
	for {
		select {
		case <-ctx.Done():
			return state, ctx.Err()
		case <-timer.C:
			return state, errors.New("直连上游流长时间无响应")
		case err := <-done:
			// The reader closes lines before publishing its result. Keep consuming
			// buffered final events instead of racing done against the last line.
			readerErr = err
			readerDone = true
			done = nil
		case item, ok := <-lines:
			if !ok {
				if !readerDone {
					readerErr = <-done
				}
				return state, readerErr
			}
			resetDirectProbeTimer(timer, idleTimeout)
			line := bytes.TrimSpace(item.line)
			if len(line) == 0 {
				eventName = ""
				continue
			}
			if bytes.HasPrefix(line, []byte("event:")) {
				eventName = strings.TrimSpace(string(bytes.TrimPrefix(line, []byte("event:"))))
				continue
			}
			if !bytes.HasPrefix(line, []byte("data:")) {
				continue
			}
			data := bytes.TrimSpace(bytes.TrimPrefix(line, []byte("data:")))
			if bytes.Equal(data, []byte("[DONE]")) {
				state.completed = true
				return state, nil
			}
			if len(data) == 0 {
				continue
			}
			text, completed, usage, err := parseDirectSSEData(protocol, eventName, data)
			if err != nil {
				return state, err
			}
			state.usage = mergeProbeUsage(state.usage, usage)
			appendLimited(&state.text, text, maxResponseTextBytes)
			if completed {
				state.completed = true
				return state, nil
			}
		}
	}
}

func readDirectSSELines(ctx context.Context, body io.Reader, lines chan<- directSSELine, done chan<- error) {
	var result error
	defer func() {
		// Close first so consumers can drain every queued event before they act
		// on EOF or a reader error.
		close(lines)
		done <- result
	}()
	scanner := bufio.NewScanner(body)
	scanner.Buffer(make([]byte, 16<<10), maxDirectSSELineBytes)
	total := 0
	for scanner.Scan() {
		line := append([]byte(nil), scanner.Bytes()...)
		total += len(line)
		if total > maxDirectStreamBytes {
			result = errors.New("直连上游流响应过大")
			return
		}
		select {
		case lines <- directSSELine{line: line}:
		case <-ctx.Done():
			return
		}
	}
	if err := scanner.Err(); err != nil {
		result = errors.New("读取直连上游流失败")
		return
	}
}

func resetDirectProbeTimer(timer *time.Timer, timeout time.Duration) {
	if !timer.Stop() {
		select {
		case <-timer.C:
		default:
		}
	}
	timer.Reset(timeout)
}

func parseDirectSSEData(protocol directProbeProtocol, eventName string, data []byte) (string, bool, *model.ProbeUsage, error) {
	var payload map[string]any
	if err := json.Unmarshal(data, &payload); err != nil {
		return "", false, nil, errors.New("直连上游返回无效流事件")
	}
	if hasDirectStreamError(payload, eventName) {
		return "", false, nil, directProbeStreamPayloadError(payload)
	}
	usage := directProbeUsageFromPayload(payload)
	var text string
	var completed bool
	var err error
	switch protocol {
	case directProbeOpenAIChat:
		text, completed, err = parseOpenAIChatStreamData(payload)
	case directProbeOpenAIResponses:
		text, completed, err = parseOpenAIResponsesStreamData(payload)
	case directProbeAnthropic:
		text, completed, err = parseAnthropicStreamData(payload)
	case directProbeGemini:
		text, completed, err = parseGeminiStreamData(payload)
	default:
		return "", false, nil, errors.New("直连探测协议不受支持")
	}
	return text, completed, usage, err
}

func directProbeUsageFromPayload(payload map[string]any) *model.ProbeUsage {
	modelID := strings.TrimSpace(stringFromAny(payload["model"]))
	usagePayload := payload
	if response, ok := payload["response"].(map[string]any); ok {
		usagePayload = response
		if modelID == "" {
			modelID = strings.TrimSpace(stringFromAny(response["model"]))
		}
	}
	if message, ok := payload["message"].(map[string]any); ok {
		usagePayload = message
		if modelID == "" {
			modelID = strings.TrimSpace(stringFromAny(message["model"]))
		}
	}
	if modelID == "" {
		modelID = strings.TrimSpace(stringFromAny(payload["modelVersion"]))
	}
	for _, key := range []string{"usage", "usageMetadata", "usage_metadata"} {
		if usage, ok := usagePayload[key].(map[string]any); ok {
			return normalizeProbeUsage(usage, modelID)
		}
		if usage, ok := payload[key].(map[string]any); ok {
			return normalizeProbeUsage(usage, modelID)
		}
	}
	return nil
}

func mergeProbeUsage(current, next *model.ProbeUsage) *model.ProbeUsage {
	if current == nil {
		if next == nil {
			return nil
		}
		copy := *next
		return &copy
	}
	if next == nil {
		return current
	}
	if strings.TrimSpace(next.Model) != "" {
		current.Model = strings.TrimSpace(next.Model)
	}
	current.InputTokens = maxUsageInt(current.InputTokens, next.InputTokens)
	current.OutputTokens = maxUsageInt(current.OutputTokens, next.OutputTokens)
	current.CacheReadTokens = maxUsageInt(current.CacheReadTokens, next.CacheReadTokens)
	current.CacheWriteTokens = maxUsageInt(current.CacheWriteTokens, next.CacheWriteTokens)
	return current
}

func maxUsageInt(left, right int64) int64 {
	if right > left {
		return right
	}
	return left
}

func hasDirectStreamError(payload map[string]any, eventName string) bool {
	if strings.EqualFold(strings.TrimSpace(eventName), "error") || strings.EqualFold(strings.TrimSpace(stringFromAny(payload["type"])), "error") {
		return true
	}
	_, exists := payload["error"]
	return exists
}

func parseOpenAIChatStreamData(payload map[string]any) (string, bool, error) {
	choices, _ := payload["choices"].([]any)
	for _, rawChoice := range choices {
		choice, _ := rawChoice.(map[string]any)
		if choice == nil {
			continue
		}
		if delta, ok := choice["delta"].(map[string]any); ok {
			if text := openAIContentText(delta["content"]); text != "" {
				return text, false, nil
			}
		}
		// With stream_options.include_usage, the usage-only terminal chunk is
		// emitted after finish_reason and before [DONE]. Do not stop early or the
		// sidecar would miss the tokens it is meant to account for.
	}
	if len(choices) == 0 {
		if _, ok := payload["usage"].(map[string]any); ok {
			return "", true, nil
		}
	}
	return "", false, nil
}

func parseOpenAIResponsesStreamData(payload map[string]any) (string, bool, error) {
	eventType := strings.TrimSpace(stringFromAny(payload["type"]))
	switch eventType {
	case "response.output_text.delta", "response.reasoning_text.delta":
		return stringFromAny(payload["delta"]), false, nil
	case "response.completed":
		return "", true, nil
	case "response.failed", "error":
		return "", false, directProbeStreamPayloadError(payload)
	}
	return "", false, nil
}

func parseAnthropicStreamData(payload map[string]any) (string, bool, error) {
	eventType := strings.TrimSpace(stringFromAny(payload["type"]))
	switch eventType {
	case "content_block_delta":
		if delta, ok := payload["delta"].(map[string]any); ok {
			return stringFromAny(delta["text"]), false, nil
		}
	case "message_stop":
		return "", true, nil
	case "error":
		return "", false, directProbeStreamPayloadError(payload)
	}
	return "", false, nil
}

func directProbeStreamPayloadError(payload map[string]any) error {
	raw, err := json.Marshal(payload)
	if err != nil {
		return errors.New("直连上游流返回错误")
	}
	detail := strings.TrimSpace(directProbeErrorDetail(raw))
	if detail == "" {
		return errors.New("直连上游流返回错误")
	}
	return fmt.Errorf("直连上游流返回错误: %s", detail)
}

func parseGeminiStreamData(payload map[string]any) (string, bool, error) {
	completed := false
	var text strings.Builder
	candidates, _ := payload["candidates"].([]any)
	for _, rawCandidate := range candidates {
		candidate, _ := rawCandidate.(map[string]any)
		if candidate == nil {
			continue
		}
		if content, ok := candidate["content"].(map[string]any); ok {
			parts, _ := content["parts"].([]any)
			for _, rawPart := range parts {
				part, _ := rawPart.(map[string]any)
				appendLimited(&text, stringFromAny(part["text"]), maxResponseTextBytes)
			}
		}
		if strings.TrimSpace(stringFromAny(candidate["finishReason"])) != "" {
			completed = true
		}
	}
	return text.String(), completed, nil
}

func openAIContentText(value any) string {
	switch typed := value.(type) {
	case string:
		return typed
	case []any:
		var text strings.Builder
		for _, item := range typed {
			part, _ := item.(map[string]any)
			appendLimited(&text, stringFromAny(part["text"]), maxResponseTextBytes)
		}
		return text.String()
	default:
		return ""
	}
}

func stringFromAny(value any) string {
	text, _ := value.(string)
	return strings.TrimSpace(text)
}
