package upstream

import (
	"bytes"
	"context"
	"crypto/tls"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/Wei-Shaw/sub2api-account-auto-scheduler/internal/model"
)

const (
	maxRemoteJSONBytes   = 4 << 20
	maxRemoteErrorBody   = 16 << 10
	remoteRequestTimeout = 25 * time.Second
	remoteUserAgent      = "Sub2API-Account-Auto-Scheduler/1.0"
)

type DetectionResult struct {
	Type     model.UpstreamSiteType `json:"type"`
	Evidence string                 `json:"evidence,omitempty"`
}

type LoginInput struct {
	Mode     model.UpstreamAuthMode
	Username string
	Password string
	Token    string
	Session  string
	UserID   string
}

type LoginResult struct {
	Material  AuthMaterial
	Principal string
	Balance   *model.UpstreamBalance
}

type SyncedKey struct {
	Key       model.RemoteKey
	Plaintext string
}

type SyncResult struct {
	Keys      []SyncedKey
	Material  AuthMaterial
	Principal string
	Balance   *model.UpstreamBalance
}

type Adapter interface {
	Type() model.UpstreamSiteType
	Connect(ctx context.Context, managementURL string, input LoginInput) (LoginResult, error)
	Sync(ctx context.Context, managementURL string, material AuthMaterial) (SyncResult, error)
}

type AdapterError struct {
	Code     string
	Message  string
	Status   model.UpstreamIdentityStatus
	HTTPCode int
}

func (e *AdapterError) Error() string {
	return e.Message
}

func adapterError(code, message string, status model.UpstreamIdentityStatus, httpCode int) *AdapterError {
	return &AdapterError{
		Code:     strings.TrimSpace(code),
		Message:  model.SanitizeUpstreamMessage(message),
		Status:   status,
		HTTPCode: httpCode,
	}
}

func AsAdapterError(err error) *AdapterError {
	var target *AdapterError
	if errors.As(err, &target) {
		return target
	}
	return adapterError("UPSTREAM_UNAVAILABLE", "无法连接上游，请检查网络和地址", model.IdentityStatusNetworkError, http.StatusBadGateway)
}

func AdapterFor(siteType model.UpstreamSiteType) (Adapter, error) {
	switch siteType {
	case model.UpstreamTypeSub2API:
		return sub2APIAdapter{}, nil
	case model.UpstreamTypeNewAPI:
		return newAPIAdapter{}, nil
	default:
		return nil, adapterError("UPSTREAM_TYPE_REQUIRED", "请先选择上游网站类型", model.IdentityStatusUnknownType, http.StatusBadRequest)
	}
}

func Detect(ctx context.Context, managementURL string) (DetectionResult, error) {
	client, err := newRemoteClient(managementURL)
	if err != nil {
		return DetectionResult{}, err
	}
	networkFailures := 0

	response, err := client.do(ctx, http.MethodGet, "/api/v1/settings/public", nil, AuthMaterial{})
	if err != nil {
		networkFailures++
	} else if response.StatusCode >= 200 && response.StatusCode < 300 && recognizesSub2API(response.Body) {
		return DetectionResult{Type: model.UpstreamTypeSub2API, Evidence: "/api/v1/settings/public"}, nil
	}

	response, err = client.do(ctx, http.MethodGet, "/api/status", nil, AuthMaterial{})
	if err != nil {
		networkFailures++
	} else if response.StatusCode >= 200 && response.StatusCode < 300 && recognizesNewAPI(response.Body) {
		return DetectionResult{Type: model.UpstreamTypeNewAPI, Evidence: "/api/status"}, nil
	}

	response, err = client.do(ctx, http.MethodGet, "/api/user/groups", nil, AuthMaterial{})
	if err == nil && response.StatusCode >= 200 && response.StatusCode < 300 && recognizesNewAPI(response.Body) {
		return DetectionResult{Type: model.UpstreamTypeNewAPI, Evidence: "/api/user/groups"}, nil
	}
	if err != nil {
		networkFailures++
	}
	if networkFailures == 3 {
		return DetectionResult{Type: model.UpstreamTypeUnknown, Evidence: "network_error"}, nil
	}
	return DetectionResult{Type: model.UpstreamTypeUnknown, Evidence: "not_recognized"}, nil
}

type remoteClient struct {
	root   *url.URL
	client *http.Client
}

type remoteResponse struct {
	StatusCode int
	Header     http.Header
	Body       []byte
}

func newRemoteClient(baseURL string) (*remoteClient, error) {
	normalized, err := model.NormalizeUpstreamBaseURL(baseURL)
	if err != nil {
		return nil, err
	}
	root, err := url.Parse(normalized)
	if err != nil {
		return nil, errors.New("上游地址无效")
	}
	dialer := &net.Dialer{Timeout: 8 * time.Second, KeepAlive: 30 * time.Second}
	transport := &http.Transport{
		Proxy:                 http.ProxyFromEnvironment,
		DialContext:           dialer.DialContext,
		ForceAttemptHTTP2:     true,
		MaxIdleConns:          20,
		MaxIdleConnsPerHost:   5,
		IdleConnTimeout:       60 * time.Second,
		TLSHandshakeTimeout:   8 * time.Second,
		ResponseHeaderTimeout: 12 * time.Second,
		ExpectContinueTimeout: time.Second,
		TLSClientConfig:       &tls.Config{MinVersion: tls.VersionTLS12},
	}
	client := &http.Client{
		Transport: transport,
		Timeout:   remoteRequestTimeout,
	}
	client.CheckRedirect = func(request *http.Request, via []*http.Request) error {
		if len(via) >= 3 {
			return errors.New("上游重定向次数过多")
		}
		if !strings.EqualFold(request.URL.Scheme, root.Scheme) || !strings.EqualFold(request.URL.Host, root.Host) {
			return errors.New("拒绝携带凭证跨站重定向")
		}
		return nil
	}
	return &remoteClient{root: root, client: client}, nil
}

func (c *remoteClient) endpoint(route string) string {
	endpoint := *c.root
	relative, err := url.Parse(route)
	if err != nil {
		relative = &url.URL{Path: route}
	}
	endpoint.Path = strings.TrimRight(c.root.Path, "/") + "/" + strings.TrimLeft(relative.Path, "/")
	endpoint.RawPath = ""
	endpoint.RawQuery = relative.RawQuery
	endpoint.Fragment = ""
	return endpoint.String()
}

func (c *remoteClient) do(ctx context.Context, method, route string, body any, material AuthMaterial) (remoteResponse, error) {
	var reader io.Reader
	if body != nil {
		raw, err := json.Marshal(body)
		if err != nil {
			return remoteResponse{}, err
		}
		if len(raw) > model.MaxUpstreamCredentialSize {
			return remoteResponse{}, errors.New("上游请求内容过大")
		}
		reader = bytes.NewReader(raw)
	}
	req, err := http.NewRequestWithContext(ctx, method, c.endpoint(route), reader)
	if err != nil {
		return remoteResponse{}, err
	}
	req.Header.Set("Accept", "application/json")
	req.Header.Set("User-Agent", remoteUserAgent)
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	if token := cleanHeaderValue(material.AccessToken); token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	if cookie := cleanHeaderValue(material.Cookie); cookie != "" {
		req.Header.Set("Cookie", cookie)
		req.Header.Set("Origin", c.root.Scheme+"://"+c.root.Host)
	}
	if userID := cleanHeaderValue(material.UserID); userID != "" {
		req.Header.Set("New-Api-User", userID)
	}

	resp, err := c.client.Do(req)
	if err != nil {
		return remoteResponse{}, err
	}
	defer func() { _ = resp.Body.Close() }()
	raw, err := io.ReadAll(io.LimitReader(resp.Body, maxRemoteJSONBytes+1))
	if err != nil {
		return remoteResponse{}, err
	}
	if len(raw) > maxRemoteJSONBytes {
		return remoteResponse{}, adapterError("UPSTREAM_RESPONSE_TOO_LARGE", "上游响应超过大小限制", model.IdentityStatusSyncError, http.StatusBadGateway)
	}
	return remoteResponse{StatusCode: resp.StatusCode, Header: resp.Header.Clone(), Body: raw}, nil
}

func cleanHeaderValue(value string) string {
	value = strings.TrimSpace(value)
	if value == "" || strings.ContainsAny(value, "\r\n") || len(value) > model.MaxUpstreamCredentialSize {
		return ""
	}
	return value
}

func recognizesSub2API(raw []byte) bool {
	var envelope struct {
		Code json.RawMessage `json:"code"`
		Data json.RawMessage `json:"data"`
	}
	if json.Unmarshal(raw, &envelope) != nil || len(envelope.Data) == 0 {
		return false
	}
	code := strings.Trim(strings.TrimSpace(string(envelope.Code)), `"`)
	return code == "" || code == "0"
}

func recognizesNewAPI(raw []byte) bool {
	var envelope struct {
		Success *bool           `json:"success"`
		Data    json.RawMessage `json:"data"`
	}
	return json.Unmarshal(raw, &envelope) == nil && envelope.Success != nil && len(envelope.Data) > 0
}

func maskRemoteKey(secret string) string {
	secret = strings.TrimSpace(secret)
	if secret == "" {
		return ""
	}
	if strings.Contains(secret, "*") {
		return secret
	}
	if len(secret) <= 8 {
		return strings.Repeat("*", len(secret))
	}
	return secret[:4] + strings.Repeat("*", 10) + secret[len(secret)-4:]
}

func looksLikeFullKey(value string) bool {
	value = strings.TrimSpace(value)
	return len(value) >= 8 && !strings.Contains(value, "*")
}

func joinResponseCookies(header http.Header) string {
	response := &http.Response{Header: header}
	parts := make([]string, 0)
	for _, cookie := range response.Cookies() {
		if cookie.Name == "" || cookie.Value == "" {
			continue
		}
		parts = append(parts, cookie.Name+"="+cookie.Value)
	}
	return strings.Join(parts, "; ")
}

func safeRemoteMessage(raw []byte) (code, message string) {
	if len(raw) > maxRemoteErrorBody {
		raw = raw[:maxRemoteErrorBody]
	}
	var envelope struct {
		Code    json.RawMessage `json:"code"`
		Message string          `json:"message"`
		Error   string          `json:"error"`
	}
	if json.Unmarshal(raw, &envelope) == nil {
		code = strings.Trim(strings.TrimSpace(string(envelope.Code)), `"`)
		message = strings.TrimSpace(envelope.Message)
		if message == "" {
			message = strings.TrimSpace(envelope.Error)
		}
	}
	return code, strings.ToLower(message)
}

func classifyLoginError(statusCode int, raw []byte) *AdapterError {
	code, message := safeRemoteMessage(raw)
	combined := strings.ToLower(code + " " + message)
	switch {
	case strings.Contains(combined, "captcha"), strings.Contains(combined, "turnstile"), strings.Contains(combined, "人机"):
		return adapterError("CAPTCHA_REQUIRED", "上游要求完成人机验证，请在上游登录后手动粘贴 Token", model.IdentityStatusCaptcha, http.StatusConflict)
	case strings.Contains(combined, "2fa"), strings.Contains(combined, "totp"), strings.Contains(combined, "two-factor"), strings.Contains(combined, "two factor"):
		return adapterError("TWO_FACTOR_REQUIRED", "上游要求两步验证，请在上游完成登录后手动粘贴 Token", model.IdentityStatusTwoFactor, http.StatusConflict)
	case strings.Contains(combined, "username or password"), strings.Contains(combined, "password is incorrect"), strings.Contains(combined, "incorrect password"), strings.Contains(combined, "invalid credentials"), strings.Contains(combined, "用户名或密码"), strings.Contains(combined, "账号或密码"), strings.Contains(combined, "密码错误"):
		return adapterError("UPSTREAM_INVALID_CREDENTIALS", "上游明确拒绝了账号或密码；请手动填写该上游自己的凭据，旧版 NewAPI 优先使用站内用户名", model.IdentityStatusInvalid, http.StatusUnauthorized)
	case hasRecognizedSessionExpiry(combined):
		return adapterError("UPSTREAM_SESSION_EXPIRED", "上游登录状态已过期，请重新连接", model.IdentityStatusExpired, http.StatusUnauthorized)
	case statusCode == http.StatusForbidden:
		return adapterError("UPSTREAM_MANAGEMENT_ACCESS_DENIED", "上游管理站点拒绝了服务器请求，请检查管理站点地址或在上游放行该服务器", model.IdentityStatusAccessDenied, http.StatusForbidden)
	case statusCode == http.StatusUnauthorized:
		return adapterError("UPSTREAM_AUTH_REJECTED", "上游拒绝了登录凭证", model.IdentityStatusExpired, http.StatusUnauthorized)
	default:
		return adapterError("UPSTREAM_LOGIN_FAILED", "上游登录失败，请检查账号、凭证和网站类型", model.IdentityStatusInvalid, http.StatusBadGateway)
	}
}

func classifyRequestError(statusCode int, raw []byte) *AdapterError {
	code, message := safeRemoteMessage(raw)
	combined := strings.ToLower(code + " " + message)
	if hasRecognizedSessionExpiry(combined) {
		return adapterError("UPSTREAM_SESSION_EXPIRED", "上游登录状态已过期，请重新连接", model.IdentityStatusExpired, http.StatusUnauthorized)
	}
	if statusCode == http.StatusUnauthorized {
		return adapterError("UPSTREAM_SESSION_EXPIRED", "上游登录状态已过期，请重新连接", model.IdentityStatusExpired, http.StatusUnauthorized)
	}
	if statusCode == http.StatusForbidden {
		return adapterError("UPSTREAM_MANAGEMENT_ACCESS_DENIED", "上游管理站点拒绝了服务器请求，请检查管理站点地址或在上游放行该服务器", model.IdentityStatusAccessDenied, http.StatusForbidden)
	}
	if statusCode == http.StatusTooManyRequests {
		return adapterError("UPSTREAM_RATE_LIMITED", "上游暂时限制了同步请求，请稍后重试", model.IdentityStatusSyncError, http.StatusTooManyRequests)
	}
	return adapterError("UPSTREAM_REQUEST_FAILED", fmt.Sprintf("上游接口返回 HTTP %d", statusCode), model.IdentityStatusSyncError, http.StatusBadGateway)
}

func hasRecognizedSessionExpiry(combined string) bool {
	return strings.Contains(combined, "token_expired") ||
		strings.Contains(combined, "auth_token_expired") ||
		strings.Contains(combined, "not logged") ||
		strings.Contains(combined, "not login")
}

func parseTimeUnix(seconds int64) *time.Time {
	if seconds <= 0 {
		return nil
	}
	value := time.Unix(seconds, 0).UTC()
	return &value
}
