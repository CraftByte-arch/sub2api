package upstream

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/Wei-Shaw/sub2api-account-auto-scheduler/internal/model"
)

type sub2APIAdapter struct{}

func (sub2APIAdapter) Type() model.UpstreamSiteType { return model.UpstreamTypeSub2API }

type sub2APIPublicVerificationSettings struct {
	TurnstileEnabled      bool `json:"turnstile_enabled"`
	RecaptchaEnabled      bool `json:"recaptcha_enabled"`
	CAPEnabled            bool `json:"cap_enabled"`
	TencentCaptchaEnabled bool `json:"tencent_captcha_enabled"`
	AliyunCaptchaEnabled  bool `json:"aliyun_captcha_enabled"`
	LocalCaptchaEnabled   bool `json:"local_captcha_enabled"`
}

func (settings sub2APIPublicVerificationSettings) externalVerificationEnabled() bool {
	return settings.TurnstileEnabled ||
		settings.RecaptchaEnabled ||
		settings.CAPEnabled ||
		settings.TencentCaptchaEnabled ||
		settings.AliyunCaptchaEnabled
}

type sub2APIUser struct {
	ID       int64           `json:"id"`
	Email    string          `json:"email"`
	Username string          `json:"username"`
	Balance  json.RawMessage `json:"balance"`
}

type sub2APIKey struct {
	ID         int64      `json:"id"`
	Key        string     `json:"key"`
	Name       string     `json:"name"`
	GroupID    *int64     `json:"group_id"`
	Status     string     `json:"status"`
	Quota      float64    `json:"quota"`
	QuotaUsed  float64    `json:"quota_used"`
	LastUsedAt *time.Time `json:"last_used_at"`
	ExpiresAt  *time.Time `json:"expires_at"`
	Group      *struct {
		ID             int64   `json:"id"`
		Name           string  `json:"name"`
		RateMultiplier float64 `json:"rate_multiplier"`
	} `json:"group"`
}

type sub2APIGroup struct {
	ID             int64   `json:"id"`
	Name           string  `json:"name"`
	RateMultiplier float64 `json:"rate_multiplier"`
}

type sub2APIPage[T any] struct {
	Items    []T   `json:"items"`
	Total    int64 `json:"total"`
	Page     int   `json:"page"`
	PageSize int   `json:"page_size"`
	Pages    int   `json:"pages"`
}

func (sub2APIAdapter) StartLoginChallenge(ctx context.Context, managementURL string) (LoginChallenge, error) {
	client, err := newRemoteClient(managementURL)
	if err != nil {
		return LoginChallenge{}, err
	}
	settingsResponse, requestErr := client.do(ctx, http.MethodGet, "/api/v1/settings/public", nil, AuthMaterial{})
	if requestErr != nil || settingsResponse.StatusCode < 200 || settingsResponse.StatusCode >= 300 {
		return LoginChallenge{}, nil
	}
	var settings sub2APIPublicVerificationSettings
	if err := decodeSub2APIResponse(settingsResponse, &settings); err != nil {
		return LoginChallenge{}, nil
	}
	if settings.externalVerificationEnabled() {
		return LoginChallenge{}, adapterError(
			"CAPTCHA_REQUIRED",
			"上游启用了浏览器人机验证；当前只支持手动输入本地图片验证码，请在上游完成登录后粘贴 Token 或 Cookie/session",
			model.IdentityStatusCaptcha,
			http.StatusConflict,
		)
	}
	if !settings.LocalCaptchaEnabled {
		return LoginChallenge{}, nil
	}

	settingsCookie := joinResponseCookies(settingsResponse.Header)
	challengeResponse, err := client.do(ctx, http.MethodGet, "/api/v1/auth/captcha", nil, AuthMaterial{Cookie: settingsCookie})
	if err != nil {
		return LoginChallenge{}, adapterError("UPSTREAM_NETWORK_ERROR", "无法获取上游验证码，请稍后重试", model.IdentityStatusNetworkError, http.StatusBadGateway)
	}
	var challenge struct {
		CaptchaID string `json:"captcha_id"`
		ImageData string `json:"image_data"`
	}
	if err := decodeSub2APIResponse(challengeResponse, &challenge); err != nil {
		return LoginChallenge{}, adapterError("UPSTREAM_CAPTCHA_UNAVAILABLE", "上游验证码暂不可用，请稍后重试", model.IdentityStatusCaptcha, http.StatusBadGateway)
	}
	captchaID, err := normalizeCaptchaID(challenge.CaptchaID)
	if err != nil {
		return LoginChallenge{}, err
	}
	imageData, err := normalizeCaptchaImageData(challenge.ImageData)
	if err != nil {
		return LoginChallenge{}, err
	}
	return LoginChallenge{
		Required:  true,
		Provider:  localCaptchaProviderName,
		CaptchaID: captchaID,
		ImageData: imageData,
		Cookie: mergeCookieMaterial(
			settingsCookie,
			joinResponseCookies(challengeResponse.Header),
		),
	}, nil
}

func (sub2APIAdapter) Connect(ctx context.Context, managementURL string, input LoginInput) (LoginResult, error) {
	client, err := newRemoteClient(managementURL)
	if err != nil {
		return LoginResult{}, err
	}
	material, principal, err := sub2APILoginMaterial(ctx, client, input)
	if err != nil {
		return LoginResult{}, err
	}
	user, material, err := verifySub2API(ctx, client, material)
	if err != nil {
		return LoginResult{}, err
	}
	if principal == "" {
		principal = firstNonEmpty(user.Email, user.Username, strconv.FormatInt(user.ID, 10))
	}
	return LoginResult{
		Material:  material,
		Principal: principal,
		Balance:   normalizeSub2APIBalance(user, time.Now().UTC()),
	}, nil
}

func (sub2APIAdapter) Sync(ctx context.Context, managementURL string, material AuthMaterial) (SyncResult, error) {
	client, err := newRemoteClient(managementURL)
	if err != nil {
		return SyncResult{}, err
	}
	user, material, err := verifySub2API(ctx, client, material)
	if err != nil {
		return SyncResult{}, err
	}
	balance := normalizeSub2APIBalance(user, time.Now().UTC())

	groups := make(map[int64]sub2APIGroup)
	var groupList []sub2APIGroup
	if response, requestErr := client.do(ctx, http.MethodGet, "/api/v1/groups/available", nil, material); requestErr == nil {
		if decodeSub2APIResponse(response, &groupList) == nil {
			for _, group := range groupList {
				groups[group.ID] = group
			}
		}
	}
	userRates := make(map[string]float64)
	if response, requestErr := client.do(ctx, http.MethodGet, "/api/v1/groups/rates", nil, material); requestErr == nil {
		_ = decodeSub2APIResponse(response, &userRates)
	}

	keys := make([]SyncedKey, 0)
	for page := 1; page <= 100; page++ {
		query := url.Values{"page": {strconv.Itoa(page)}, "page_size": {"100"}}
		response, err := client.do(ctx, http.MethodGet, "/api/v1/keys?"+query.Encode(), nil, material)
		if err != nil {
			return SyncResult{}, adapterError("UPSTREAM_NETWORK_ERROR", "无法连接 Sub2API 上游", model.IdentityStatusNetworkError, http.StatusBadGateway)
		}
		var result sub2APIPage[sub2APIKey]
		if err := decodeSub2APIResponse(response, &result); err != nil {
			return SyncResult{}, err
		}
		for _, item := range result.Items {
			if len(keys) >= model.MaxUpstreamResponseKeys {
				return SyncResult{}, adapterError("UPSTREAM_KEYS_LIMIT", "上游 Key 数量超过同步限制", model.IdentityStatusSyncError, http.StatusBadGateway)
			}
			keys = append(keys, normalizeSub2APIKey(item, groups, userRates))
		}
		if len(result.Items) == 0 || (result.Pages > 0 && page >= result.Pages) || (result.Total > 0 && int64(len(keys)) >= result.Total) {
			break
		}
	}
	return SyncResult{
		Keys:      keys,
		Material:  material,
		Principal: firstNonEmpty(user.Email, user.Username, strconv.FormatInt(user.ID, 10)),
		Balance:   balance,
	}, nil
}

func normalizeSub2APIBalance(user sub2APIUser, observedAt time.Time) *model.UpstreamBalance {
	amount, ok := parseJSONNumber(user.Balance)
	if !ok {
		return nil
	}
	return &model.UpstreamBalance{
		Amount:     amount,
		Unit:       "USD",
		Source:     string(model.UpstreamTypeSub2API),
		ObservedAt: observedAt.UTC(),
	}
}

func sub2APILoginMaterial(ctx context.Context, client *remoteClient, input LoginInput) (AuthMaterial, string, error) {
	switch input.Mode {
	case model.UpstreamAuthPassword:
		email := strings.TrimSpace(input.Username)
		if email == "" || input.Password == "" {
			return AuthMaterial{}, "", adapterError("INVALID_CREDENTIALS", "请输入上游账号和密码", model.IdentityStatusInvalid, http.StatusBadRequest)
		}
		payload := make(map[string]any)
		challengeMaterial := AuthMaterial{}
		if strings.TrimSpace(input.CaptchaID) != "" || strings.TrimSpace(input.CaptchaCode) != "" || strings.TrimSpace(input.ChallengeCookie) != "" {
			captchaID, err := normalizeCaptchaID(input.CaptchaID)
			if err != nil {
				return AuthMaterial{}, "", err
			}
			captchaCode, err := normalizeCaptchaCode(input.CaptchaCode)
			if err != nil {
				return AuthMaterial{}, "", err
			}
			challengeCookie := cleanHeaderValue(input.ChallengeCookie)
			if strings.TrimSpace(input.ChallengeCookie) != "" && challengeCookie == "" {
				return AuthMaterial{}, "", adapterError("INVALID_CAPTCHA_CHALLENGE", "验证码挑战已失效，请刷新后重试", model.IdentityStatusCaptcha, http.StatusBadRequest)
			}
			payload["captcha_id"] = captchaID
			payload["captcha_code"] = captchaCode
			challengeMaterial.Cookie = challengeCookie
		}
		flow, secureFlow, err := discoverSub2APICredentialFlow(ctx, client, challengeMaterial, time.Now().UTC())
		if err != nil {
			return AuthMaterial{}, "", err
		}
		requestMaterial := challengeMaterial
		if secureFlow {
			envelope, err := buildSub2APICredentialEnvelope(flow, email, input.Password, time.Now().UTC(), nil)
			if err != nil {
				return AuthMaterial{}, "", err
			}
			payload["credential_envelope"] = envelope
			requestMaterial.Cookie = mergeCookieMaterial(requestMaterial.Cookie, flow.Cookie)
		} else {
			payload["email"] = email
			payload["password"] = input.Password
		}
		response, err := client.do(ctx, http.MethodPost, "/api/v1/auth/login", payload, requestMaterial)
		if err != nil {
			return AuthMaterial{}, "", adapterError("UPSTREAM_NETWORK_ERROR", "无法连接 Sub2API 登录接口", model.IdentityStatusNetworkError, http.StatusBadGateway)
		}
		var login struct {
			AccessToken  string      `json:"access_token"`
			RefreshToken string      `json:"refresh_token"`
			Requires2FA  bool        `json:"requires_2fa"`
			TempToken    string      `json:"temp_token"`
			User         sub2APIUser `json:"user"`
		}
		if err := decodeSub2APIResponse(response, &login); err != nil {
			return AuthMaterial{}, "", classifyLoginError(response.StatusCode, response.Body)
		}
		if login.Requires2FA || login.TempToken != "" {
			return AuthMaterial{}, "", adapterError("TWO_FACTOR_REQUIRED", "上游要求两步验证，请在上游完成登录后手动粘贴 Token", model.IdentityStatusTwoFactor, http.StatusConflict)
		}
		persistentCookie := mergeCookieMaterial(challengeMaterial.Cookie, joinResponseCookies(response.Header))
		if secureFlow {
			persistentCookie = removeCookieMaterial(persistentCookie, flow.CookieNames...)
		}
		material := AuthMaterial{
			AccessToken:  strings.TrimSpace(login.AccessToken),
			RefreshToken: strings.TrimSpace(login.RefreshToken),
			Cookie:       persistentCookie,
			UserID:       strconv.FormatInt(login.User.ID, 10),
		}.withPasswordLogin(input)
		if material.Empty() {
			return AuthMaterial{}, "", adapterError("UPSTREAM_LOGIN_CONTRACT", "Sub2API 登录响应未包含可用会话", model.IdentityStatusInvalid, http.StatusBadGateway)
		}
		return material, firstNonEmpty(login.User.Email, login.User.Username, email), nil
	case model.UpstreamAuthToken:
		token := strings.TrimSpace(input.Token)
		if token == "" || len(token) > model.MaxUpstreamCredentialSize {
			return AuthMaterial{}, "", adapterError("INVALID_TOKEN", "请输入有效的上游访问 Token", model.IdentityStatusInvalid, http.StatusBadRequest)
		}
		return AuthMaterial{AccessToken: token}, "", nil
	case model.UpstreamAuthSession:
		session := strings.TrimSpace(input.Session)
		if session == "" || len(session) > model.MaxUpstreamCredentialSize || strings.ContainsAny(session, "\r\n") {
			return AuthMaterial{}, "", adapterError("INVALID_SESSION", "请输入有效的 Cookie 或会话 Token", model.IdentityStatusInvalid, http.StatusBadRequest)
		}
		if strings.Contains(session, "=") {
			return AuthMaterial{Cookie: session}, "", nil
		}
		return AuthMaterial{AccessToken: session}, "", nil
	default:
		return AuthMaterial{}, "", adapterError("INVALID_AUTH_MODE", "不支持的登录方式", model.IdentityStatusInvalid, http.StatusBadRequest)
	}
}

func verifySub2API(ctx context.Context, client *remoteClient, material AuthMaterial) (sub2APIUser, AuthMaterial, error) {
	response, err := client.do(ctx, http.MethodGet, "/api/v1/auth/me", nil, material)
	if err != nil {
		return sub2APIUser{}, material, adapterError("UPSTREAM_NETWORK_ERROR", "无法验证 Sub2API 登录状态", model.IdentityStatusNetworkError, http.StatusBadGateway)
	}
	var user sub2APIUser
	verifyErr := decodeSub2APIResponse(response, &user)
	if verifyErr == nil {
		return user, material, nil
	}
	if AsAdapterError(verifyErr).Code != "UPSTREAM_SESSION_EXPIRED" {
		return sub2APIUser{}, material, verifyErr
	}
	if strings.TrimSpace(material.RefreshToken) == "" {
		return reloginSub2API(ctx, client, material, verifyErr)
	}
	refreshed, refreshErr := refreshSub2API(ctx, client, material)
	if refreshErr != nil {
		return reloginSub2API(ctx, client, material, refreshErr)
	}
	response, err = client.do(ctx, http.MethodGet, "/api/v1/auth/me", nil, refreshed)
	if err != nil {
		return sub2APIUser{}, material, adapterError("UPSTREAM_NETWORK_ERROR", "无法验证 Sub2API 登录状态", model.IdentityStatusNetworkError, http.StatusBadGateway)
	}
	if retryErr := decodeSub2APIResponse(response, &user); retryErr != nil {
		if AsAdapterError(retryErr).Code == "UPSTREAM_SESSION_EXPIRED" {
			return reloginSub2API(ctx, client, material, retryErr)
		}
		return sub2APIUser{}, material, retryErr
	}
	return user, refreshed, nil
}

func refreshSub2API(ctx context.Context, client *remoteClient, material AuthMaterial) (AuthMaterial, error) {
	response, err := client.do(ctx, http.MethodPost, "/api/v1/auth/refresh", map[string]string{"refresh_token": material.RefreshToken}, AuthMaterial{})
	if err != nil {
		return material, adapterError("UPSTREAM_REFRESH_FAILED", "无法刷新 Sub2API 登录状态", model.IdentityStatusExpired, http.StatusUnauthorized)
	}
	var result struct {
		AccessToken  string `json:"access_token"`
		RefreshToken string `json:"refresh_token"`
	}
	if err := decodeSub2APIResponse(response, &result); err != nil {
		return material, err
	}
	if strings.TrimSpace(result.AccessToken) == "" {
		return material, adapterError("UPSTREAM_SESSION_EXPIRED", "Sub2API 登录状态已过期，请重新连接", model.IdentityStatusExpired, http.StatusUnauthorized)
	}
	material.AccessToken = strings.TrimSpace(result.AccessToken)
	if strings.TrimSpace(result.RefreshToken) != "" {
		material.RefreshToken = strings.TrimSpace(result.RefreshToken)
	}
	return material, nil
}

func decodeSub2APIResponse(response remoteResponse, target any) error {
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		return classifyRequestError(response.StatusCode, response.Body)
	}
	var envelope struct {
		Code    json.RawMessage `json:"code"`
		Message string          `json:"message"`
		Data    json.RawMessage `json:"data"`
	}
	if err := json.Unmarshal(response.Body, &envelope); err != nil {
		return adapterError("UPSTREAM_INVALID_RESPONSE", "Sub2API 返回了无法解析的响应", model.IdentityStatusSyncError, http.StatusBadGateway)
	}
	code := strings.Trim(strings.TrimSpace(string(envelope.Code)), `"`)
	if code != "" && code != "0" {
		return classifyRequestError(response.StatusCode, response.Body)
	}
	if len(envelope.Data) == 0 || string(envelope.Data) == "null" {
		return errors.New("Sub2API 响应缺少 data")
	}
	if err := json.Unmarshal(envelope.Data, target); err != nil {
		return adapterError("UPSTREAM_INVALID_RESPONSE", "Sub2API 数据结构不受支持", model.IdentityStatusSyncError, http.StatusBadGateway)
	}
	return nil
}

func normalizeSub2APIKey(item sub2APIKey, groups map[int64]sub2APIGroup, userRates map[string]float64) SyncedKey {
	groupID := item.GroupID
	if groupID == nil && item.Group != nil {
		value := item.Group.ID
		groupID = &value
	}
	groupName := ""
	var multiplier *float64
	source := ""
	if groupID != nil {
		if value, ok := userRates[strconv.FormatInt(*groupID, 10)]; ok {
			multiplier = floatPointer(value)
			source = "user_override"
		}
		if group, ok := groups[*groupID]; ok {
			groupName = group.Name
			if multiplier == nil {
				multiplier = floatPointer(group.RateMultiplier)
				source = "group"
			}
		}
	}
	if item.Group != nil {
		if groupName == "" {
			groupName = item.Group.Name
		}
		if multiplier == nil {
			multiplier = floatPointer(item.Group.RateMultiplier)
			source = "group"
		}
	}
	plain := ""
	if looksLikeFullKey(item.Key) {
		plain = strings.TrimSpace(item.Key)
	}
	return SyncedKey{
		Key: model.RemoteKey{
			ID:               strconv.FormatInt(item.ID, 10),
			Name:             strings.TrimSpace(item.Name),
			MaskedKey:        maskRemoteKey(item.Key),
			MatchAvailable:   plain != "",
			Status:           strings.TrimSpace(item.Status),
			Group:            groupName,
			UnlimitedQuota:   item.Quota == 0,
			QuotaLimit:       floatPointer(item.Quota),
			QuotaUsed:        floatPointer(item.QuotaUsed),
			Multiplier:       multiplier,
			MultiplierSource: source,
			LastUsedAt:       item.LastUsedAt,
			ExpiresAt:        item.ExpiresAt,
		},
		Plaintext: plain,
	}
}

func floatPointer(value float64) *float64 {
	copy := value
	return &copy
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if value = strings.TrimSpace(value); value != "" {
			return value
		}
	}
	return ""
}

func unexpectedContract(family string) error {
	return fmt.Errorf("%s response contract is unsupported", family)
}
