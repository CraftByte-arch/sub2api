package upstream

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/Wei-Shaw/sub2api-account-auto-scheduler/internal/model"
)

type newAPIAdapter struct{}

func (newAPIAdapter) Type() model.UpstreamSiteType { return model.UpstreamTypeNewAPI }

type newAPIUser struct {
	ID       int             `json:"id"`
	Username string          `json:"username"`
	Email    string          `json:"email"`
	Quota    json.RawMessage `json:"quota"`
}

type newAPIToken struct {
	ID             int    `json:"id"`
	Key            string `json:"key"`
	Status         int    `json:"status"`
	Name           string `json:"name"`
	AccessedTime   int64  `json:"accessed_time"`
	ExpiredTime    int64  `json:"expired_time"`
	RemainQuota    int64  `json:"remain_quota"`
	UnlimitedQuota bool   `json:"unlimited_quota"`
	UsedQuota      int64  `json:"used_quota"`
	Group          string `json:"group"`
}

type newAPIPage[T any] struct {
	Items    []T `json:"items"`
	Total    int `json:"total"`
	Page     int `json:"page"`
	PageSize int `json:"page_size"`
}

type newAPILog struct {
	CreatedAt int64           `json:"created_at"`
	TokenID   int             `json:"token_id"`
	TokenName string          `json:"token_name"`
	Group     string          `json:"group"`
	Other     json.RawMessage `json:"other"`
}

type dynamicRatioObservation struct {
	Value      float64
	ObservedAt time.Time
}

type newAPIGroupInfo struct {
	Ratio    *float64
	Platform string
}

func (newAPIAdapter) Connect(ctx context.Context, managementURL string, input LoginInput) (LoginResult, error) {
	client, err := newRemoteClient(managementURL)
	if err != nil {
		return LoginResult{}, err
	}
	material, principal, err := newAPILoginMaterial(ctx, client, input)
	if err != nil {
		return LoginResult{}, err
	}
	user, material, err := verifyNewAPI(ctx, client, material)
	if err != nil {
		return LoginResult{}, err
	}
	if principal == "" {
		principal = firstNonEmpty(user.Username, user.Email, strconv.Itoa(user.ID))
	}
	return LoginResult{
		Material:  material,
		Principal: principal,
		Balance:   fetchNewAPIBalance(ctx, client, user),
	}, nil
}

func (newAPIAdapter) Sync(ctx context.Context, managementURL string, material AuthMaterial) (SyncResult, error) {
	client, err := newRemoteClient(managementURL)
	if err != nil {
		return SyncResult{}, err
	}
	user, material, err := verifyNewAPI(ctx, client, material)
	if err != nil {
		return SyncResult{}, err
	}
	balance := fetchNewAPIBalance(ctx, client, user)

	groupInfos, err := fetchNewAPIGroupRatios(ctx, client, material)
	if err != nil {
		return SyncResult{}, err
	}
	tokens, err := fetchNewAPITokens(ctx, client, material)
	if err != nil {
		return SyncResult{}, err
	}
	hasAuto := false
	for _, token := range tokens {
		if strings.EqualFold(strings.TrimSpace(token.Group), "auto") {
			hasAuto = true
			break
		}
	}
	dynamicRatios := map[int]dynamicRatioObservation{}
	if hasAuto {
		dynamicRatios, _ = fetchNewAPIDynamicRatios(ctx, client, material)
	}

	keys := make([]SyncedKey, 0, len(tokens))
	for _, token := range tokens {
		keys = append(keys, normalizeNewAPIKey(token, groupInfos, dynamicRatios))
	}
	return SyncResult{
		Keys:          keys,
		Groups:        normalizeNewAPIGroups(groupInfos),
		GroupsFetched: true,
		Material:      material,
		Principal:     firstNonEmpty(user.Username, user.Email, strconv.Itoa(user.ID)),
		Balance:       balance,
	}, nil
}

func fetchNewAPIBalance(ctx context.Context, client *remoteClient, user newAPIUser) *model.UpstreamBalance {
	quota, ok := parseJSONNumber(user.Quota)
	if !ok {
		return nil
	}
	rawQuota := quota
	balance := &model.UpstreamBalance{
		Amount:     quota,
		Unit:       "quota",
		Source:     string(model.UpstreamTypeNewAPI),
		ObservedAt: time.Now().UTC(),
		RawQuota:   &rawQuota,
	}

	response, err := client.do(ctx, http.MethodGet, "/api/status", nil, AuthMaterial{})
	if err != nil {
		return balance
	}
	var status struct {
		QuotaPerUnit json.RawMessage `json:"quota_per_unit"`
	}
	if decodeNewAPIResponse(response, &status) != nil {
		return balance
	}
	quotaPerUnit, ok := parseJSONNumber(status.QuotaPerUnit)
	if !ok || quotaPerUnit <= 0 {
		return balance
	}
	amount := quota / quotaPerUnit
	if math.IsNaN(amount) || math.IsInf(amount, 0) {
		return balance
	}
	balance.Amount = amount
	balance.Unit = "USD"
	balance.QuotaPerUnit = &quotaPerUnit
	return balance
}

func newAPILoginMaterial(ctx context.Context, client *remoteClient, input LoginInput) (AuthMaterial, string, error) {
	switch input.Mode {
	case model.UpstreamAuthPassword:
		username := strings.TrimSpace(input.Username)
		if username == "" || input.Password == "" {
			return AuthMaterial{}, "", adapterError("INVALID_CREDENTIALS", "请输入上游账号和密码", model.IdentityStatusInvalid, http.StatusBadRequest)
		}
		response, err := client.do(ctx, http.MethodPost, "/api/user/login?turnstile=", map[string]string{
			"username": username, "password": input.Password,
		}, AuthMaterial{})
		if err != nil {
			return AuthMaterial{}, "", adapterError("UPSTREAM_NETWORK_ERROR", "无法连接 NewAPI 登录接口", model.IdentityStatusNetworkError, http.StatusBadGateway)
		}
		var login struct {
			Require2FA      bool       `json:"require_2fa"`
			FlowToken       string     `json:"flow_token"`
			AccessToken     string     `json:"access_token"`
			TokenType       string     `json:"token_type"`
			AccessExpiresAt int64      `json:"access_expires_at"`
			User            newAPIUser `json:"user"`
			ID              int        `json:"id"`
			Username        string     `json:"username"`
			Email           string     `json:"email"`
		}
		if err := decodeNewAPIResponse(response, &login); err != nil {
			return AuthMaterial{}, "", classifyLoginError(response.StatusCode, response.Body)
		}
		if login.Require2FA || login.FlowToken != "" {
			return AuthMaterial{}, "", adapterError("TWO_FACTOR_REQUIRED", "上游要求两步验证，请在上游完成登录后手动粘贴 Token", model.IdentityStatusTwoFactor, http.StatusConflict)
		}
		user := login.User
		if user.ID <= 0 && login.ID > 0 {
			user = newAPIUser{ID: login.ID, Username: login.Username, Email: login.Email}
		}
		material := AuthMaterial{
			AccessToken: strings.TrimSpace(login.AccessToken),
			Cookie:      joinResponseCookies(response.Header),
			UserID:      positiveNewAPIUserID(user.ID),
		}.withPasswordLogin(input)
		if material.Empty() {
			return AuthMaterial{}, "", adapterError("UPSTREAM_LOGIN_CONTRACT", "NewAPI 登录响应未包含可用会话", model.IdentityStatusInvalid, http.StatusBadGateway)
		}
		return material, firstNonEmpty(user.Username, user.Email, username), nil
	case model.UpstreamAuthToken:
		token := strings.TrimSpace(input.Token)
		if token == "" || len(token) > model.MaxUpstreamCredentialSize {
			return AuthMaterial{}, "", adapterError("INVALID_TOKEN", "请输入有效的上游访问 Token", model.IdentityStatusInvalid, http.StatusBadRequest)
		}
		userID, err := parseNewAPIUserID(input.UserID)
		if err != nil {
			return AuthMaterial{}, "", err
		}
		return AuthMaterial{AccessToken: token, UserID: userID}, "", nil
	case model.UpstreamAuthSession:
		session := strings.TrimSpace(input.Session)
		if session == "" || len(session) > model.MaxUpstreamCredentialSize || strings.ContainsAny(session, "\r\n") {
			return AuthMaterial{}, "", adapterError("INVALID_SESSION", "请输入有效的 Cookie 或会话 Token", model.IdentityStatusInvalid, http.StatusBadRequest)
		}
		userID, err := parseNewAPIUserID(input.UserID)
		if err != nil {
			return AuthMaterial{}, "", err
		}
		if strings.Contains(session, "=") {
			return AuthMaterial{Cookie: session, UserID: userID}, "", nil
		}
		return AuthMaterial{AccessToken: session, UserID: userID}, "", nil
	default:
		return AuthMaterial{}, "", adapterError("INVALID_AUTH_MODE", "不支持的登录方式", model.IdentityStatusInvalid, http.StatusBadRequest)
	}
}

func verifyNewAPI(ctx context.Context, client *remoteClient, material AuthMaterial) (newAPIUser, AuthMaterial, error) {
	response, err := client.do(ctx, http.MethodGet, "/api/user/self", nil, material)
	if err != nil {
		return newAPIUser{}, material, adapterError("UPSTREAM_NETWORK_ERROR", "无法验证 NewAPI 登录状态", model.IdentityStatusNetworkError, http.StatusBadGateway)
	}
	var user newAPIUser
	verifyErr := decodeNewAPIResponse(response, &user)
	if verifyErr == nil {
		if material.UserID == "" && user.ID > 0 {
			material.UserID = strconv.Itoa(user.ID)
		}
		return user, material, nil
	}
	if AsAdapterError(verifyErr).Code != "UPSTREAM_SESSION_EXPIRED" {
		return newAPIUser{}, material, verifyErr
	}
	if strings.TrimSpace(material.Cookie) == "" {
		fallback := verifyErr
		if material.UserID == "" && response.StatusCode == http.StatusUnauthorized {
			fallback = newAPIUserIDRequiredError()
		}
		return reloginNewAPI(ctx, client, material, fallback)
	}
	refreshed, refreshErr := refreshNewAPI(ctx, client, material)
	if refreshErr != nil {
		if _, ok := material.passwordLoginInput(); ok {
			return reloginNewAPI(ctx, client, material, refreshErr)
		}
		if material.UserID == "" {
			return newAPIUser{}, material, newAPIUserIDRequiredError()
		}
		return newAPIUser{}, material, refreshErr
	}
	response, err = client.do(ctx, http.MethodGet, "/api/user/self", nil, refreshed)
	if err != nil {
		return newAPIUser{}, material, adapterError("UPSTREAM_NETWORK_ERROR", "无法验证 NewAPI 登录状态", model.IdentityStatusNetworkError, http.StatusBadGateway)
	}
	if retryErr := decodeNewAPIResponse(response, &user); retryErr != nil {
		if AsAdapterError(retryErr).Code == "UPSTREAM_SESSION_EXPIRED" {
			return reloginNewAPI(ctx, client, material, retryErr)
		}
		if refreshed.UserID == "" {
			return newAPIUser{}, material, newAPIUserIDRequiredError()
		}
		return newAPIUser{}, material, retryErr
	}
	if refreshed.UserID == "" && user.ID > 0 {
		refreshed.UserID = strconv.Itoa(user.ID)
	}
	return user, refreshed, nil
}

func parseNewAPIUserID(value string) (string, error) {
	value = strings.TrimSpace(value)
	if value == "" {
		return "", nil
	}
	parsed, err := strconv.ParseInt(value, 10, 64)
	if err != nil || parsed <= 0 {
		return "", adapterError("INVALID_NEWAPI_USER_ID", "NewAPI 用户 ID 必须是正整数", model.IdentityStatusInvalid, http.StatusBadRequest)
	}
	return strconv.FormatInt(parsed, 10), nil
}

func positiveNewAPIUserID(value int) string {
	if value <= 0 {
		return ""
	}
	return strconv.Itoa(value)
}

func newAPIUserIDRequiredError() *AdapterError {
	return adapterError("NEWAPI_USER_ID_REQUIRED", "该 NewAPI 版本需要用户 ID，请填写个人中心显示的数字用户 ID 后重试", model.IdentityStatusInvalid, http.StatusUnauthorized)
}

func refreshNewAPI(ctx context.Context, client *remoteClient, material AuthMaterial) (AuthMaterial, error) {
	response, err := client.do(ctx, http.MethodPost, "/api/user/auth/refresh", map[string]any{}, AuthMaterial{Cookie: material.Cookie})
	if err != nil {
		return material, adapterError("UPSTREAM_REFRESH_FAILED", "无法刷新 NewAPI 登录状态", model.IdentityStatusExpired, http.StatusUnauthorized)
	}
	var result struct {
		AccessToken string `json:"access_token"`
	}
	if err := decodeNewAPIResponse(response, &result); err != nil {
		return material, err
	}
	if strings.TrimSpace(result.AccessToken) == "" {
		return material, adapterError("UPSTREAM_SESSION_EXPIRED", "NewAPI 登录状态已过期，请重新连接", model.IdentityStatusExpired, http.StatusUnauthorized)
	}
	material.AccessToken = strings.TrimSpace(result.AccessToken)
	if cookie := joinResponseCookies(response.Header); cookie != "" {
		material.Cookie = cookie
	}
	return material, nil
}

func fetchNewAPIGroupRatios(ctx context.Context, client *remoteClient, material AuthMaterial) (map[string]newAPIGroupInfo, error) {
	response, err := client.do(ctx, http.MethodGet, "/api/user/self/groups", nil, material)
	if err != nil {
		return nil, adapterError("UPSTREAM_NETWORK_ERROR", "无法读取 NewAPI 分组倍率", model.IdentityStatusNetworkError, http.StatusBadGateway)
	}
	var raw map[string]struct {
		Ratio    json.RawMessage `json:"ratio"`
		Platform string          `json:"platform"`
		Type     string          `json:"type"`
	}
	if err := decodeNewAPIResponse(response, &raw); err != nil {
		return nil, err
	}
	groups := make(map[string]newAPIGroupInfo, len(raw))
	for group, value := range raw {
		group = strings.TrimSpace(group)
		if group == "" {
			continue
		}
		info := newAPIGroupInfo{Platform: model.NormalizeRemoteGroupPlatform(firstNonEmpty(value.Platform, value.Type))}
		if ratio, ok := parseJSONNumber(value.Ratio); ok {
			info.Ratio = floatPointer(ratio)
		}
		groups[group] = info
	}
	return groups, nil
}

func fetchNewAPITokens(ctx context.Context, client *remoteClient, material AuthMaterial) ([]newAPIToken, error) {
	tokens := make([]newAPIToken, 0)
	for page := 1; page <= 100; page++ {
		query := url.Values{"p": {strconv.Itoa(page)}, "size": {"100"}}
		response, err := client.do(ctx, http.MethodGet, "/api/token/?"+query.Encode(), nil, material)
		if err != nil {
			return nil, adapterError("UPSTREAM_NETWORK_ERROR", "无法读取 NewAPI Key", model.IdentityStatusNetworkError, http.StatusBadGateway)
		}
		var result newAPIPage[newAPIToken]
		if err := decodeNewAPIResponse(response, &result); err != nil {
			return nil, err
		}
		if len(tokens)+len(result.Items) > model.MaxUpstreamResponseKeys {
			return nil, adapterError("UPSTREAM_KEYS_LIMIT", "上游 Key 数量超过同步限制", model.IdentityStatusSyncError, http.StatusBadGateway)
		}
		tokens = append(tokens, result.Items...)
		if len(result.Items) == 0 || (result.Total > 0 && len(tokens) >= result.Total) || len(result.Items) < 100 {
			break
		}
	}
	return tokens, nil
}

func fetchNewAPIDynamicRatios(ctx context.Context, client *remoteClient, material AuthMaterial) (map[int]dynamicRatioObservation, error) {
	query := url.Values{"p": {"1"}, "size": {"100"}, "type": {"2"}}
	response, err := client.do(ctx, http.MethodGet, "/api/log/self?"+query.Encode(), nil, material)
	if err != nil {
		return nil, err
	}
	var result newAPIPage[newAPILog]
	if err := decodeNewAPIResponse(response, &result); err != nil {
		return nil, err
	}
	observations := make(map[int]dynamicRatioObservation)
	for _, item := range result.Items {
		if item.TokenID <= 0 {
			continue
		}
		ratio, ok := parseNewAPILogRatio(item.Other)
		if !ok {
			continue
		}
		observedAt := time.Unix(item.CreatedAt, 0).UTC()
		if existing, exists := observations[item.TokenID]; !exists || observedAt.After(existing.ObservedAt) {
			observations[item.TokenID] = dynamicRatioObservation{Value: ratio, ObservedAt: observedAt}
		}
	}
	return observations, nil
}

func decodeNewAPIResponse(response remoteResponse, target any) error {
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		return classifyRequestError(response.StatusCode, response.Body)
	}
	var envelope struct {
		Success *bool           `json:"success"`
		Message string          `json:"message"`
		Data    json.RawMessage `json:"data"`
	}
	if err := json.Unmarshal(response.Body, &envelope); err != nil || envelope.Success == nil {
		return adapterError("UPSTREAM_INVALID_RESPONSE", "NewAPI 返回了无法解析的响应", model.IdentityStatusSyncError, http.StatusBadGateway)
	}
	if !*envelope.Success {
		return classifyRequestError(response.StatusCode, response.Body)
	}
	if len(envelope.Data) == 0 || string(envelope.Data) == "null" {
		return errors.New("NewAPI 响应缺少 data")
	}
	if err := json.Unmarshal(envelope.Data, target); err != nil {
		return adapterError("UPSTREAM_INVALID_RESPONSE", "NewAPI 数据结构不受支持", model.IdentityStatusSyncError, http.StatusBadGateway)
	}
	return nil
}

func normalizeNewAPIKey(item newAPIToken, groups map[string]newAPIGroupInfo, observations map[int]dynamicRatioObservation) SyncedKey {
	group := strings.TrimSpace(item.Group)
	var multiplier *float64
	source := ""
	var observedAt *time.Time
	if strings.EqualFold(group, "auto") {
		source = "dynamic"
		if observation, ok := observations[item.ID]; ok {
			multiplier = floatPointer(observation.Value)
			value := observation.ObservedAt
			observedAt = &value
		}
	} else if info, ok := groups[group]; ok && info.Ratio != nil {
		multiplier = floatPointer(*info.Ratio)
		source = "group"
	}
	plain := ""
	if looksLikeFullKey(item.Key) {
		plain = strings.TrimSpace(item.Key)
	}
	status := newAPITokenStatus(item.Status, item.ExpiredTime)
	var quotaLimit *float64
	if !item.UnlimitedQuota {
		quotaLimit = floatPointer(float64(item.RemainQuota + item.UsedQuota))
	}
	return SyncedKey{
		Key: model.RemoteKey{
			ID:                 strconv.Itoa(item.ID),
			Name:               strings.TrimSpace(item.Name),
			MaskedKey:          maskRemoteKey(item.Key),
			MatchAvailable:     plain != "",
			Status:             status,
			Group:              group,
			UnlimitedQuota:     item.UnlimitedQuota,
			QuotaLimit:         quotaLimit,
			QuotaUsed:          floatPointer(float64(item.UsedQuota)),
			Multiplier:         multiplier,
			MultiplierSource:   source,
			MultiplierObserved: observedAt,
			LastUsedAt:         parseTimeUnix(item.AccessedTime),
			ExpiresAt:          parseTimeUnix(item.ExpiredTime),
		},
		Plaintext: plain,
	}
}

func normalizeNewAPIGroups(groups map[string]newAPIGroupInfo) []model.RemoteGroup {
	snapshots := make([]model.RemoteGroup, 0, len(groups))
	for groupID, info := range groups {
		groupID = strings.TrimSpace(groupID)
		if groupID == "" {
			continue
		}
		group := model.RemoteGroup{
			ID:       groupID,
			Name:     groupID,
			Platform: info.Platform,
		}
		if strings.EqualFold(groupID, "auto") {
			group.MultiplierSource = "dynamic"
		} else if info.Ratio != nil {
			group.Multiplier = floatPointer(*info.Ratio)
			group.MultiplierSource = "group"
		}
		snapshots = append(snapshots, group)
	}
	return snapshots
}

func newAPITokenStatus(status int, expiresAt int64) string {
	if expiresAt > 0 && time.Now().Unix() >= expiresAt {
		return "expired"
	}
	switch status {
	case 1:
		return "enabled"
	case 2:
		return "disabled"
	case 3:
		return "expired"
	default:
		return fmt.Sprintf("status_%d", status)
	}
}

func parseJSONNumber(raw json.RawMessage) (float64, bool) {
	if len(raw) == 0 || bytes.Equal(bytes.TrimSpace(raw), []byte("null")) {
		return 0, false
	}
	var number float64
	if json.Unmarshal(raw, &number) == nil {
		return number, !math.IsNaN(number) && !math.IsInf(number, 0)
	}
	var text string
	if json.Unmarshal(raw, &text) != nil {
		return 0, false
	}
	value, err := strconv.ParseFloat(strings.TrimSpace(text), 64)
	return value, err == nil && !math.IsNaN(value) && !math.IsInf(value, 0)
}

func parseNewAPILogRatio(raw json.RawMessage) (float64, bool) {
	if len(raw) == 0 || string(raw) == "null" {
		return 0, false
	}
	var object map[string]json.RawMessage
	if raw[0] == '"' {
		var text string
		if json.Unmarshal(raw, &text) != nil || json.Unmarshal([]byte(text), &object) != nil {
			return 0, false
		}
	} else if json.Unmarshal(raw, &object) != nil {
		return 0, false
	}
	return parseJSONNumber(object["group_ratio"])
}
