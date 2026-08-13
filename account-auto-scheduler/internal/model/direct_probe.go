package model

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"net/http"
	"net/url"
	"sort"
	"strconv"
	"strings"
	"time"
)

const (
	DirectProbeSnapshotVersion = 1
	MaxDirectProbeSnapshotSize = 128 << 10
)

type PublicProxy struct {
	Protocol  string     `json:"protocol"`
	Host      string     `json:"host"`
	Port      int        `json:"port"`
	Username  string     `json:"username,omitempty"`
	Status    string     `json:"status"`
	ExpiresAt *time.Time `json:"expires_at,omitempty"`
}

func IsDirectProbePlatformSupported(platform string) bool {
	switch strings.ToLower(strings.TrimSpace(platform)) {
	case "openai", "anthropic", "gemini", "grok", "antigravity":
		return true
	default:
		return false
	}
}

func NormalizeDirectProbePlatform(platform string) string {
	return strings.ToLower(strings.TrimSpace(platform))
}

func DefaultDirectProbeBaseURL(platform string) string {
	switch NormalizeDirectProbePlatform(platform) {
	case "openai":
		return "https://api.openai.com"
	case "anthropic":
		return "https://api.anthropic.com"
	case "gemini":
		return "https://generativelanguage.googleapis.com"
	case "grok":
		return "https://api.x.ai"
	default:
		return ""
	}
}

func NormalizeDirectProbeBaseURL(raw, platform string) (string, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		raw = DefaultDirectProbeBaseURL(platform)
	}
	parsed, err := url.Parse(raw)
	if err != nil || parsed.Host == "" || (parsed.Scheme != "http" && parsed.Scheme != "https") {
		return "", errors.New("上游地址必须是绝对 HTTP(S) URL")
	}
	if parsed.User != nil || parsed.RawQuery != "" || parsed.Fragment != "" {
		return "", errors.New("上游地址不能包含凭证、查询参数或片段")
	}
	parsed.Scheme = strings.ToLower(parsed.Scheme)
	parsed.Host = normalizeURLHost(parsed.Scheme, parsed.Host)
	if parsed.Host == "" {
		return "", errors.New("上游地址主机无效")
	}
	parsed.Path = strings.TrimRight(strings.TrimSpace(parsed.Path), "/")
	parsed.RawPath = ""
	return strings.TrimRight(parsed.String(), "/"), nil
}

// EffectiveDirectProbeBaseURL mirrors the API-key routing behavior used by
// Sub2API. Antigravity API-key routes live below the compatibility gateway's
// /antigravity prefix even when an administrator entered the deployment root.
func EffectiveDirectProbeBaseURL(raw, platform string) (string, error) {
	platform = NormalizeDirectProbePlatform(platform)
	baseURL, err := NormalizeDirectProbeBaseURL(raw, platform)
	if err != nil {
		return "", err
	}
	if platform != "antigravity" {
		return baseURL, nil
	}
	parsed, err := url.Parse(baseURL)
	if err != nil {
		return "", errors.New("上游地址无效")
	}
	if !strings.HasSuffix(strings.ToLower(strings.TrimRight(parsed.Path, "/")), "/antigravity") {
		parsed.Path = strings.TrimRight(parsed.Path, "/") + "/antigravity"
		parsed.RawPath = ""
	}
	return NormalizeDirectProbeBaseURL(parsed.String(), platform)
}

func ResolveDirectProbeModel(snapshot DirectProbeSnapshot, requested string) string {
	modelID := strings.TrimSpace(requested)
	if modelID == "" {
		switch NormalizeDirectProbePlatform(snapshot.Platform) {
		case "openai":
			modelID = "gpt-5.4"
		case "anthropic", "antigravity":
			modelID = "claude-sonnet-4-5-20250929"
		case "gemini":
			modelID = "gemini-2.0-flash"
		case "grok":
			modelID = "grok-4-1-fast-reasoning"
		}
	}
	if len(snapshot.ModelMapping) == 0 || modelID == "" {
		return modelID
	}
	if mapped, ok := snapshot.ModelMapping[modelID]; ok && strings.TrimSpace(mapped) != "" {
		return strings.TrimSpace(mapped)
	}
	patterns := make([]string, 0, len(snapshot.ModelMapping))
	for pattern := range snapshot.ModelMapping {
		if strings.Contains(pattern, "*") {
			patterns = append(patterns, pattern)
		}
	}
	sort.Slice(patterns, func(i, j int) bool { return len(patterns[i]) > len(patterns[j]) })
	for _, pattern := range patterns {
		if directProbeWildcardMatch(pattern, modelID) && strings.TrimSpace(snapshot.ModelMapping[pattern]) != "" {
			return strings.TrimSpace(snapshot.ModelMapping[pattern])
		}
	}
	return modelID
}

func directProbeWildcardMatch(pattern, value string) bool {
	parts := strings.Split(pattern, "*")
	if len(parts) == 1 {
		return pattern == value
	}
	if !strings.HasPrefix(value, parts[0]) {
		return false
	}
	remaining := value[len(parts[0]):]
	for index := 1; index < len(parts)-1; index++ {
		part := parts[index]
		if part == "" {
			continue
		}
		position := strings.Index(remaining, part)
		if position < 0 {
			return false
		}
		remaining = remaining[position+len(part):]
	}
	return strings.HasSuffix(remaining, parts[len(parts)-1])
}

func FilterDirectProbeHeaderOverrides(enabled bool, raw any) map[string]string {
	if !enabled {
		return nil
	}
	values, ok := raw.(map[string]any)
	if !ok {
		if typed, typedOK := raw.(map[string]string); typedOK {
			values = make(map[string]any, len(typed))
			for key, value := range typed {
				values[key] = value
			}
		} else {
			return nil
		}
	}
	if len(values) > 64 {
		return nil
	}
	result := make(map[string]string, len(values))
	for rawName, rawValue := range values {
		name := http.CanonicalHeaderKey(strings.TrimSpace(rawName))
		value, ok := rawValue.(string)
		if !ok {
			continue
		}
		value = strings.TrimSpace(value)
		if name == "" || value == "" || len(name) > 200 || len(value) > 8192 || !validDirectProbeHeaderName(name) || directProbeHeaderBlocked(name) {
			continue
		}
		result[name] = value
	}
	if len(result) == 0 {
		return nil
	}
	return result
}

func validDirectProbeHeaderName(name string) bool {
	if name == "" {
		return false
	}
	for _, character := range name {
		if !(character >= '0' && character <= '9' || character >= 'a' && character <= 'z' || character >= 'A' && character <= 'Z' || strings.ContainsRune("!#$%&'*+-.^_`|~", character)) {
			return false
		}
	}
	return true
}

func directProbeHeaderBlocked(name string) bool {
	switch strings.ToLower(strings.TrimSpace(name)) {
	case "host", "content-length", "content-type", "transfer-encoding", "connection", "keep-alive", "proxy-authenticate", "proxy-authorization", "proxy-connection", "te", "trailer", "upgrade", "authorization", "x-api-key", "x-goog-api-key", "cookie", "accept-encoding", "sec-websocket-key", "sec-websocket-version", "sec-websocket-extensions", "sec-websocket-protocol", "sec-websocket-accept", "session_id", "conversation_id", "x-codex-turn-state", "x-codex-turn-metadata", "chatgpt-account-id", "x-claude-code-session-id", "x-client-request-id", "x-grok-conv-id":
		return true
	default:
		return false
	}
}

func DirectProbeRoutingFingerprint(snapshot DirectProbeSnapshot) string {
	type fingerprintProxy struct {
		Protocol string `json:"protocol"`
		Host     string `json:"host"`
		Port     int    `json:"port"`
		Username string `json:"username"`
		Status   string `json:"status"`
		Expires  *int64 `json:"expires_at,omitempty"`
	}
	type fingerprint struct {
		Platform            string            `json:"platform"`
		BaseURL             string            `json:"base_url"`
		ModelMapping        map[string]string `json:"model_mapping,omitempty"`
		HeaderOverrides     map[string]string `json:"header_overrides,omitempty"`
		AnthropicAuthMode   string            `json:"anthropic_auth_mode,omitempty"`
		OpenAIResponsesMode string            `json:"openai_responses_mode,omitempty"`
		OpenAIResponsesOK   *bool             `json:"openai_responses_supported,omitempty"`
		Proxy               *fingerprintProxy `json:"proxy,omitempty"`
	}
	value := fingerprint{
		Platform:            NormalizeDirectProbePlatform(snapshot.Platform),
		BaseURL:             strings.TrimRight(strings.TrimSpace(snapshot.BaseURL), "/"),
		ModelMapping:        cloneStringMap(snapshot.ModelMapping),
		HeaderOverrides:     cloneStringMap(snapshot.HeaderOverrides),
		AnthropicAuthMode:   strings.TrimSpace(snapshot.AnthropicAuthMode),
		OpenAIResponsesMode: strings.TrimSpace(snapshot.OpenAIResponsesMode),
		OpenAIResponsesOK:   cloneBool(snapshot.OpenAIResponsesOK),
	}
	if snapshot.Proxy != nil {
		value.Proxy = &fingerprintProxy{
			Protocol: strings.ToLower(strings.TrimSpace(snapshot.Proxy.Protocol)),
			Host:     strings.ToLower(strings.TrimSpace(snapshot.Proxy.Host)),
			Port:     snapshot.Proxy.Port,
			Username: strings.TrimSpace(snapshot.Proxy.Username),
			Status:   strings.TrimSpace(snapshot.Proxy.Status),
			Expires:  cloneInt64(snapshot.Proxy.ExpiresAt),
		}
	}
	raw, _ := json.Marshal(value)
	digest := sha256.Sum256(raw)
	return hex.EncodeToString(digest[:])
}

func DirectProbeRoutingFingerprintForAccount(account UpstreamAccount) (string, error) {
	platform := NormalizeDirectProbePlatform(account.Platform)
	if !IsDirectProbePlatformSupported(platform) {
		return "", fmt.Errorf("暂不支持 %s 平台的直连探测", account.Platform)
	}
	baseURL, err := EffectiveDirectProbeBaseURL(account.BaseURL(), platform)
	if err != nil {
		return "", err
	}
	snapshot := DirectProbeSnapshot{
		Platform:            platform,
		BaseURL:             baseURL,
		ModelMapping:        DirectProbeModelMapping(account.Credentials),
		HeaderOverrides:     FilterDirectProbeHeaderOverrides(directProbeHeaderOverridesEnabled(account), valueFromMap(account.Credentials, "header_overrides")),
		AnthropicAuthMode:   stringFromMap(account.Extra, "anthropic_apikey_auth_scheme"),
		OpenAIResponsesMode: stringFromMap(account.Extra, "openai_responses_mode"),
		OpenAIResponsesOK:   boolPointerFromMap(account.Extra, "openai_responses_supported"),
	}
	if account.Proxy != nil {
		snapshot.Proxy = &DirectProbeProxy{
			Protocol: account.Proxy.Protocol,
			Host:     account.Proxy.Host,
			Port:     account.Proxy.Port,
			Username: account.Proxy.Username,
			Status:   account.Proxy.Status,
		}
		if account.Proxy.ExpiresAt != nil {
			unix := account.Proxy.ExpiresAt.Unix()
			snapshot.Proxy.ExpiresAt = &unix
		}
	}
	return DirectProbeRoutingFingerprint(snapshot), nil
}

func directProbeHeaderOverridesEnabled(account UpstreamAccount) bool {
	if !boolFromMap(account.Credentials, "header_override_enabled") {
		return false
	}
	switch NormalizeDirectProbePlatform(account.Platform) {
	case "openai", "anthropic", "grok":
		return account.IsAPIKey()
	default:
		return false
	}
}

func DirectProbeModelMapping(credentials map[string]any) map[string]string {
	if credentials == nil {
		return nil
	}
	return stringMapFromAny(credentials["model_mapping"])
}

func stringMapFromAny(raw any) map[string]string {
	var source map[string]any
	switch typed := raw.(type) {
	case map[string]any:
		source = typed
	case map[string]string:
		result := make(map[string]string, len(typed))
		for key, value := range typed {
			key, value = strings.TrimSpace(key), strings.TrimSpace(value)
			if key != "" && value != "" && len(key) <= 200 && len(value) <= 200 {
				result[key] = value
			}
		}
		if len(result) == 0 {
			return nil
		}
		return result
	default:
		return nil
	}
	if len(source) > 500 {
		return nil
	}
	result := make(map[string]string, len(source))
	for key, rawValue := range source {
		value, ok := rawValue.(string)
		key, value = strings.TrimSpace(key), strings.TrimSpace(value)
		if !ok || key == "" || value == "" || len(key) > 200 || len(value) > 200 {
			continue
		}
		result[key] = value
	}
	if len(result) == 0 {
		return nil
	}
	return result
}

func boolFromMap(values map[string]any, key string) bool {
	value, ok := values[key].(bool)
	return ok && value
}

func boolPointerFromMap(values map[string]any, key string) *bool {
	value, ok := values[key].(bool)
	if !ok {
		return nil
	}
	return &value
}

func stringFromMap(values map[string]any, key string) string {
	value, _ := values[key].(string)
	return strings.TrimSpace(value)
}

func valueFromMap(values map[string]any, key string) any {
	if values == nil {
		return nil
	}
	return values[key]
}

func cloneStringMap(values map[string]string) map[string]string {
	if len(values) == 0 {
		return nil
	}
	copy := make(map[string]string, len(values))
	for key, value := range values {
		copy[key] = value
	}
	return copy
}

func cloneBool(value *bool) *bool {
	if value == nil {
		return nil
	}
	copy := *value
	return &copy
}

func cloneInt64(value *int64) *int64 {
	if value == nil {
		return nil
	}
	copy := *value
	return &copy
}

func DirectProbeProxyURL(proxy *DirectProbeProxy, now time.Time) (*url.URL, error) {
	if proxy == nil {
		return nil, nil
	}
	protocol := strings.ToLower(strings.TrimSpace(proxy.Protocol))
	if protocol != "http" && protocol != "https" {
		return nil, errors.New("账号配置的代理类型暂不支持直连探测")
	}
	if strings.TrimSpace(proxy.Status) != "active" {
		return nil, errors.New("账号配置的代理当前未启用")
	}
	if proxy.ExpiresAt != nil && !time.Unix(*proxy.ExpiresAt, 0).After(now) {
		return nil, errors.New("账号配置的代理已过期")
	}
	host := strings.TrimSpace(proxy.Host)
	if host == "" || proxy.Port < 1 || proxy.Port > 65535 {
		return nil, errors.New("账号配置的代理地址无效")
	}
	endpoint := &url.URL{Scheme: protocol, Host: net.JoinHostPort(host, strconv.Itoa(proxy.Port))}
	if proxy.Username != "" || proxy.Password != "" {
		endpoint.User = url.UserPassword(proxy.Username, proxy.Password)
	}
	return endpoint, nil
}

func ClearDirectProbeSnapshot(snapshot *DirectProbeSnapshot) {
	if snapshot == nil {
		return
	}
	snapshot.APIKey = ""
	for key := range snapshot.ModelMapping {
		delete(snapshot.ModelMapping, key)
	}
	for key := range snapshot.HeaderOverrides {
		delete(snapshot.HeaderOverrides, key)
	}
	if snapshot.Proxy != nil {
		snapshot.Proxy.Username = ""
		snapshot.Proxy.Password = ""
	}
	*snapshot = DirectProbeSnapshot{}
}
