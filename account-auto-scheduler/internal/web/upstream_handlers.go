package web

import (
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strings"
	"time"
	"unicode"

	"github.com/Wei-Shaw/sub2api-account-auto-scheduler/internal/core"
	"github.com/Wei-Shaw/sub2api-account-auto-scheduler/internal/model"
	"github.com/Wei-Shaw/sub2api-account-auto-scheduler/internal/store"
	"github.com/Wei-Shaw/sub2api-account-auto-scheduler/internal/upstream"
)

type createUpstreamRequest struct {
	Name         string `json:"name"`
	BaseURL      string `json:"base_url"`
	TypeOverride string `json:"type_override"`
}

type setUpstreamTypeRequest struct {
	Type string `json:"type"`
}

type setUpstreamRechargeRateRequest struct {
	Mode  string   `json:"mode"`
	Value *float64 `json:"value"`
}

type connectUpstreamRequest struct {
	Label            string  `json:"label"`
	ManagementURL    *string `json:"site_url"`
	AuthMode         string  `json:"auth_mode"`
	Username         string  `json:"username"`
	Password         string  `json:"password"`
	Token            string  `json:"token"`
	Session          string  `json:"session"`
	UserID           string  `json:"user_id"`
	LoginChallengeID string  `json:"login_challenge_id"`
	CaptchaCode      string  `json:"captcha_code"`
}

type startUpstreamLoginChallengeRequest struct {
	IdentityID    string  `json:"identity_id"`
	ManagementURL *string `json:"site_url"`
	AuthMode      string  `json:"auth_mode"`
}

type upstreamLoginChallengeResponse struct {
	Required  bool       `json:"required"`
	ID        string     `json:"id,omitempty"`
	Provider  string     `json:"provider,omitempty"`
	ImageData string     `json:"image_data,omitempty"`
	ExpiresAt *time.Time `json:"expires_at,omitempty"`
}

type saveBindingsRequest struct {
	Bindings []upstream.BindingInput `json:"bindings"`
}

func (s *Server) handleUpstreams(w http.ResponseWriter, r *http.Request) {
	if !s.requireUpstreamConsole(w) {
		return
	}
	result, err := s.upstreams.List(r.Context())
	if err != nil {
		writeUpstreamError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, result)
}

func (s *Server) handleCreateUpstream(w http.ResponseWriter, r *http.Request) {
	if !s.requireUpstreamConsole(w) {
		return
	}
	var request createUpstreamRequest
	if err := decodeLimitedJSON(r, 32<<10, &request); err != nil {
		writeError(w, http.StatusBadRequest, "INVALID_REQUEST", err.Error())
		return
	}
	siteType, err := model.ParseUpstreamSiteType(request.TypeOverride, true)
	if err != nil {
		writeError(w, http.StatusBadRequest, "INVALID_UPSTREAM_TYPE", err.Error())
		return
	}
	result, err := s.upstreams.Create(r.Context(), upstream.CreateInput{Name: request.Name, BaseURL: request.BaseURL, TypeOverride: siteType})
	if err != nil {
		writeUpstreamError(w, err)
		return
	}
	writeJSON(w, http.StatusCreated, map[string]any{"upstream": result})
}

func (s *Server) handleDeleteUpstream(w http.ResponseWriter, r *http.Request) {
	if !s.requireUpstreamConsole(w) {
		return
	}
	if err := s.upstreams.Delete(r.Context(), r.PathValue("upstreamID")); err != nil {
		writeUpstreamError(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (s *Server) handleDetectUpstream(w http.ResponseWriter, r *http.Request) {
	if !s.requireUpstreamConsole(w) {
		return
	}
	result, err := s.upstreams.Detect(r.Context(), r.PathValue("upstreamID"))
	if err != nil {
		writeUpstreamError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"upstream": result})
}

func (s *Server) handleSetUpstreamType(w http.ResponseWriter, r *http.Request) {
	if !s.requireUpstreamConsole(w) {
		return
	}
	var request setUpstreamTypeRequest
	if err := decodeLimitedJSON(r, 8<<10, &request); err != nil {
		writeError(w, http.StatusBadRequest, "INVALID_REQUEST", err.Error())
		return
	}
	siteType, err := model.ParseUpstreamSiteType(request.Type, true)
	if err != nil {
		writeError(w, http.StatusBadRequest, "INVALID_UPSTREAM_TYPE", err.Error())
		return
	}
	result, err := s.upstreams.SetType(r.Context(), r.PathValue("upstreamID"), siteType)
	if err != nil {
		writeUpstreamError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"upstream": result})
}

func (s *Server) handleSetUpstreamRechargeRate(w http.ResponseWriter, r *http.Request) {
	if !s.requireUpstreamConsole(w) {
		return
	}
	var request setUpstreamRechargeRateRequest
	if err := decodeLimitedJSON(r, 8<<10, &request); err != nil {
		writeError(w, http.StatusBadRequest, "INVALID_REQUEST", err.Error())
		return
	}
	if request.Value == nil {
		writeError(w, http.StatusBadRequest, "INVALID_RECHARGE_RATE", "请填写充值倍率")
		return
	}
	result, err := s.upstreams.SetRechargeRate(r.Context(), r.PathValue("upstreamID"), upstream.RechargeRateInput{
		Mode: model.RechargeRateInputMode(strings.TrimSpace(request.Mode)), Value: *request.Value,
	})
	if err != nil {
		writeUpstreamError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"upstream": result})
}

func (s *Server) handleClearUpstreamRechargeRate(w http.ResponseWriter, r *http.Request) {
	if !s.requireUpstreamConsole(w) {
		return
	}
	result, err := s.upstreams.ClearRechargeRate(r.Context(), r.PathValue("upstreamID"))
	if err != nil {
		writeUpstreamError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"upstream": result})
}

func (s *Server) handleStartUpstreamLoginChallenge(w http.ResponseWriter, r *http.Request) {
	if !s.requireUpstreamConsole(w) {
		return
	}
	manager, ok := s.upstreams.(upstreamLoginChallengeConsole)
	if !ok {
		writeJSON(w, http.StatusOK, upstreamLoginChallengeResponse{Required: false})
		return
	}
	var request startUpstreamLoginChallengeRequest
	if err := decodeLimitedJSON(r, 32<<10, &request); err != nil {
		writeError(w, http.StatusBadRequest, "INVALID_REQUEST", err.Error())
		return
	}
	mode, err := model.ParseUpstreamAuthMode(request.AuthMode)
	if err != nil {
		writeError(w, http.StatusBadRequest, "INVALID_AUTH_MODE", err.Error())
		return
	}
	if mode != model.UpstreamAuthPassword {
		writeJSON(w, http.StatusOK, upstreamLoginChallengeResponse{Required: false})
		return
	}
	result, err := manager.StartLoginChallenge(r.Context(), r.PathValue("upstreamID"), upstream.LoginChallengeInput{
		ManagementURL:    optionalString(request.ManagementURL),
		ManagementURLSet: request.ManagementURL != nil,
	})
	if err != nil {
		writeUpstreamError(w, err)
		return
	}
	if !result.Challenge.Required {
		writeJSON(w, http.StatusOK, upstreamLoginChallengeResponse{Required: false})
		return
	}
	user, _ := r.Context().Value(adminUserKey).(core.AdminUser)
	handle, err := s.loginChallenges.Create(loginChallengeScope{
		AdminID:       user.ID,
		UpstreamID:    r.PathValue("upstreamID"),
		IdentityID:    request.IdentityID,
		ManagementURL: result.ManagementURL,
	}, result.Challenge)
	if err != nil {
		if errors.Is(err, errLoginChallengeCapacity) {
			writeError(w, http.StatusServiceUnavailable, "LOGIN_CHALLENGE_CAPACITY", "当前验证码请求过多，请稍后重试")
			return
		}
		writeError(w, http.StatusBadGateway, "LOGIN_CHALLENGE_FAILED", "无法创建验证码挑战，请稍后重试")
		return
	}
	expiresAt := handle.ExpiresAt
	writeJSON(w, http.StatusOK, upstreamLoginChallengeResponse{
		Required:  true,
		ID:        handle.ID,
		Provider:  handle.Provider,
		ImageData: result.Challenge.ImageData,
		ExpiresAt: &expiresAt,
	})
}

func (s *Server) handleConnectUpstreamIdentity(w http.ResponseWriter, r *http.Request) {
	if !s.requireUpstreamConsole(w) {
		return
	}
	var request connectUpstreamRequest
	if err := decodeLimitedJSON(r, model.MaxUpstreamCredentialSize+4096, &request); err != nil {
		writeError(w, http.StatusBadRequest, "INVALID_REQUEST", err.Error())
		return
	}
	mode, err := model.ParseUpstreamAuthMode(request.AuthMode)
	if err != nil {
		writeError(w, http.StatusBadRequest, "INVALID_AUTH_MODE", err.Error())
		return
	}
	login := upstream.LoginInput{
		Mode: mode, Username: request.Username, Password: request.Password, Token: request.Token, Session: request.Session, UserID: request.UserID,
	}
	challengeID := strings.TrimSpace(request.LoginChallengeID)
	captchaCode := strings.TrimSpace(request.CaptchaCode)
	if challengeID != "" || captchaCode != "" {
		if mode != model.UpstreamAuthPassword || challengeID == "" || captchaCode == "" || len(captchaCode) > 64 || strings.IndexFunc(captchaCode, unicode.IsControl) >= 0 {
			writeError(w, http.StatusBadRequest, "INVALID_LOGIN_CHALLENGE", "验证码挑战或验证码无效，请刷新后重试")
			return
		}
		if request.ManagementURL == nil || strings.TrimSpace(*request.ManagementURL) == "" {
			writeError(w, http.StatusConflict, "LOGIN_CHALLENGE_EXPIRED", "验证码挑战已失效，请刷新后重试")
			return
		}
		managementURL, normalizeErr := model.NormalizeUpstreamBaseURL(*request.ManagementURL)
		if normalizeErr != nil {
			writeError(w, http.StatusBadRequest, "INVALID_MANAGEMENT_SITE_URL", normalizeErr.Error())
			return
		}
		user, _ := r.Context().Value(adminUserKey).(core.AdminUser)
		attempt, consumeErr := s.loginChallenges.Consume(challengeID, loginChallengeScope{
			AdminID:       user.ID,
			UpstreamID:    r.PathValue("upstreamID"),
			IdentityID:    r.PathValue("identityID"),
			ManagementURL: managementURL,
		})
		if consumeErr != nil {
			writeError(w, http.StatusConflict, "LOGIN_CHALLENGE_EXPIRED", "验证码挑战已过期或已使用，请刷新后重试")
			return
		}
		resolvedManagementURL := attempt.Scope.ManagementURL
		request.ManagementURL = &resolvedManagementURL
		login.CaptchaID = attempt.CaptchaID
		login.CaptchaCode = captchaCode
		login.ChallengeCookie = attempt.Cookie
	}
	identity, err := s.upstreams.Connect(r.Context(), r.PathValue("upstreamID"), upstream.ConnectInput{
		IdentityID:       strings.TrimSpace(r.PathValue("identityID")),
		Label:            request.Label,
		ManagementURL:    optionalString(request.ManagementURL),
		ManagementURLSet: request.ManagementURL != nil,
		Login:            login,
	})
	if err != nil {
		writeUpstreamErrorWithData(w, err, map[string]any{"identity": identity})
		return
	}
	status := http.StatusCreated
	if r.Method == http.MethodPut {
		status = http.StatusOK
	}
	writeJSON(w, status, map[string]any{"identity": identity})
}

func optionalString(value *string) string {
	if value == nil {
		return ""
	}
	return *value
}

func (s *Server) handleDeleteUpstreamIdentity(w http.ResponseWriter, r *http.Request) {
	if !s.requireUpstreamConsole(w) {
		return
	}
	if err := s.upstreams.DeleteIdentity(r.PathValue("upstreamID"), r.PathValue("identityID")); err != nil {
		writeUpstreamError(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (s *Server) handleSyncUpstreamIdentity(w http.ResponseWriter, r *http.Request) {
	if !s.requireUpstreamConsole(w) {
		return
	}
	if err := s.upstreams.SyncIdentity(r.Context(), r.PathValue("upstreamID"), r.PathValue("identityID")); err != nil {
		writeUpstreamError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"status": "synchronized"})
}

func (s *Server) handleSyncUpstream(w http.ResponseWriter, r *http.Request) {
	if !s.requireUpstreamConsole(w) {
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"outcomes": s.upstreams.SyncUpstream(r.Context(), r.PathValue("upstreamID"))})
}

func (s *Server) handleSyncAllUpstreams(w http.ResponseWriter, r *http.Request) {
	if !s.requireUpstreamConsole(w) {
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"outcomes": s.upstreams.SyncAll(r.Context())})
}

func (s *Server) handleSaveUpstreamBindings(w http.ResponseWriter, r *http.Request) {
	if !s.requireUpstreamConsole(w) {
		return
	}
	var request saveBindingsRequest
	if err := decodeLimitedJSON(r, 1<<20, &request); err != nil {
		writeError(w, http.StatusBadRequest, "INVALID_REQUEST", err.Error())
		return
	}
	if len(request.Bindings) > 10000 {
		writeError(w, http.StatusBadRequest, "INVALID_REQUEST", "bindings 不能超过 10000 个")
		return
	}
	result, err := s.upstreams.SaveBindings(r.Context(), r.PathValue("upstreamID"), request.Bindings)
	if err != nil {
		writeUpstreamError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"upstream": result})
}

func (s *Server) handleAutoMatchUpstream(w http.ResponseWriter, r *http.Request) {
	if !s.requireUpstreamConsole(w) {
		return
	}
	result, err := s.upstreams.AutoMatch(r.Context(), r.PathValue("upstreamID"), bearerToken(r.Header.Get("Authorization")), core.ForwardedIdentity{
		ClientIP: s.clientIP(r), UserAgent: r.UserAgent(),
	})
	if err != nil {
		writeUpstreamError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, result)
}

func (s *Server) requireUpstreamConsole(w http.ResponseWriter) bool {
	if s.upstreams != nil {
		return true
	}
	writeError(w, http.StatusServiceUnavailable, "UPSTREAMS_UNAVAILABLE", "上游管理暂不可用")
	return false
}

func decodeLimitedJSON(r *http.Request, limit int64, target any) error {
	defer func() { _ = r.Body.Close() }()
	decoder := json.NewDecoder(io.LimitReader(r.Body, limit+1))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(target); err != nil {
		return errors.New("请求内容无效")
	}
	var extra any
	if err := decoder.Decode(&extra); !errors.Is(err, io.EOF) {
		return errors.New("请求只能包含一个 JSON 对象")
	}
	return nil
}

func writeUpstreamError(w http.ResponseWriter, err error) {
	writeUpstreamErrorWithData(w, err, nil)
}

func writeUpstreamErrorWithData(w http.ResponseWriter, err error, data map[string]any) {
	status := http.StatusBadGateway
	code := "UPSTREAM_OPERATION_FAILED"
	message := "上游操作失败"
	var adapterErr *upstream.AdapterError
	var coreErr *core.HTTPError
	switch {
	case errors.As(err, &adapterErr):
		status = adapterErr.HTTPCode
		code = adapterErr.Code
		message = adapterErr.Message
	case errors.As(err, &coreErr):
		status = coreErr.StatusCode
		code = firstNonEmptyWeb(coreErr.Code, "SUB2API_REQUEST_FAILED")
		message = model.SanitizeUpstreamMessage(coreErr.Message)
	case errors.Is(err, store.ErrUpstreamNotFound):
		status = http.StatusNotFound
		code = "UPSTREAM_NOT_FOUND"
		message = "上游不存在"
	case errors.Is(err, upstream.ErrSyncInProgress):
		status = http.StatusConflict
		code = "SYNC_IN_PROGRESS"
		message = "该身份正在同步"
	case errors.Is(err, upstream.ErrCredentialsDisabled):
		status = http.StatusServiceUnavailable
		code = "CREDENTIALS_DISABLED"
		message = "请配置 AUTO_SCHEDULER_CREDENTIAL_KEY 后重试"
	}
	payload := map[string]any{"code": code, "message": message}
	for key, value := range data {
		payload[key] = value
	}
	writeJSON(w, status, payload)
}

func firstNonEmptyWeb(values ...string) string {
	for _, value := range values {
		if value = strings.TrimSpace(value); value != "" {
			return value
		}
	}
	return ""
}
