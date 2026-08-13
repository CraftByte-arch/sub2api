package web

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api-account-auto-scheduler/internal/core"
	"github.com/Wei-Shaw/sub2api-account-auto-scheduler/internal/model"
	"github.com/Wei-Shaw/sub2api-account-auto-scheduler/internal/upstream"
)

type fakeUpstreamConsole struct {
	credentialsEnabled bool
	listResponse       upstream.ListResponse
	listCalls          int
	syncOutcomes       []upstream.SyncOutcome
	autoResult         upstream.MatchResult
	autoErr            error
	autoJWT            string
	autoIdentity       core.ForwardedIdentity
	rechargeRateInput  upstream.RechargeRateInput
	rechargeRateClear  bool
	connectInput       upstream.ConnectInput
	finalMultipliers   map[int64]upstream.LocalAccountFinalMultiplier
}

func (f *fakeUpstreamConsole) CredentialsEnabled() bool { return f.credentialsEnabled }

func (f *fakeUpstreamConsole) LocalAccountFinalMultipliers([]model.UpstreamAccount) map[int64]upstream.LocalAccountFinalMultiplier {
	result := make(map[int64]upstream.LocalAccountFinalMultiplier, len(f.finalMultipliers))
	for accountID, multiplier := range f.finalMultipliers {
		result[accountID] = multiplier
	}
	return result
}

func (f *fakeUpstreamConsole) List(context.Context) (upstream.ListResponse, error) {
	f.listCalls++
	return f.listResponse, nil
}

func (f *fakeUpstreamConsole) Create(context.Context, upstream.CreateInput) (upstream.UpstreamView, error) {
	return upstream.UpstreamView{}, nil
}

func (f *fakeUpstreamConsole) Detect(context.Context, string) (upstream.UpstreamView, error) {
	return upstream.UpstreamView{}, nil
}

func (f *fakeUpstreamConsole) SetType(context.Context, string, model.UpstreamSiteType) (upstream.UpstreamView, error) {
	return upstream.UpstreamView{}, nil
}

func (f *fakeUpstreamConsole) SetRechargeRate(_ context.Context, _ string, input upstream.RechargeRateInput) (upstream.UpstreamView, error) {
	f.rechargeRateInput = input
	return upstream.UpstreamView{ID: "up_1"}, nil
}

func (f *fakeUpstreamConsole) ClearRechargeRate(context.Context, string) (upstream.UpstreamView, error) {
	f.rechargeRateClear = true
	return upstream.UpstreamView{ID: "up_1"}, nil
}

func (f *fakeUpstreamConsole) Connect(_ context.Context, _ string, input upstream.ConnectInput) (upstream.IdentityView, error) {
	f.connectInput = input
	return upstream.IdentityView{}, nil
}

func (f *fakeUpstreamConsole) DeleteIdentity(string, string) error { return nil }

func (f *fakeUpstreamConsole) SyncIdentity(context.Context, string, string) error { return nil }

func (f *fakeUpstreamConsole) SyncUpstream(context.Context, string) []upstream.SyncOutcome {
	return append([]upstream.SyncOutcome(nil), f.syncOutcomes...)
}

func (f *fakeUpstreamConsole) SyncAll(context.Context) []upstream.SyncOutcome {
	return append([]upstream.SyncOutcome(nil), f.syncOutcomes...)
}

func (f *fakeUpstreamConsole) SaveBindings(context.Context, string, []upstream.BindingInput) (upstream.UpstreamView, error) {
	return upstream.UpstreamView{}, nil
}

func (f *fakeUpstreamConsole) AutoMatch(_ context.Context, _ string, jwt string, identity core.ForwardedIdentity) (upstream.MatchResult, error) {
	f.autoJWT = jwt
	f.autoIdentity = identity
	return f.autoResult, f.autoErr
}

func TestUpstreamListRequiresAdminBeforeReturningMetadata(t *testing.T) {
	observedAt := time.Date(2026, time.August, 13, 12, 0, 0, 0, time.UTC)
	console := &fakeUpstreamConsole{listResponse: upstream.ListResponse{
		CredentialsEnabled: false,
		Upstreams: []upstream.UpstreamView{{
			ID: "up_1", Name: "Example", BaseURL: "https://upstream.example", Type: model.UpstreamTypeSub2API,
			BalanceSummary: upstream.UpstreamBalanceSummary{
				Status: "single", IdentityCount: 1, AvailableCount: 1,
				Balance: &upstream.BalanceView{Amount: 12.5, Unit: "USD", Source: "sub2api", ObservedAt: observedAt},
			},
			Identities: []upstream.IdentityView{{
				ID: "identity", Label: "Primary", HasCredential: true,
				Balance: &upstream.BalanceView{Amount: 12.5, Unit: "USD", Source: "sub2api", ObservedAt: observedAt},
			}},
		}},
	}}
	server := NewServer(nil, &fakeAdminCore{user: core.AdminUser{ID: 1, Role: "admin"}}, Options{Upstreams: console}, nil)

	unauthorized := httptest.NewRequest(http.MethodGet, "/api/upstreams", nil)
	unauthorizedResponse := httptest.NewRecorder()
	server.Handler().ServeHTTP(unauthorizedResponse, unauthorized)
	if unauthorizedResponse.Code != http.StatusUnauthorized || console.listCalls != 0 {
		t.Fatalf("unauthorized response=%d list_calls=%d", unauthorizedResponse.Code, console.listCalls)
	}

	authorized := httptest.NewRequest(http.MethodGet, "/api/upstreams", nil)
	authorized.Header.Set("Authorization", "Bearer valid")
	authorizedResponse := httptest.NewRecorder()
	server.Handler().ServeHTTP(authorizedResponse, authorized)
	if authorizedResponse.Code != http.StatusOK || console.listCalls != 1 {
		t.Fatalf("authorized response=%d list_calls=%d body=%s", authorizedResponse.Code, console.listCalls, authorizedResponse.Body.String())
	}
	body := authorizedResponse.Body.String()
	for _, forbidden := range []string{"ciphertext", "fingerprint", `"credential":`, "api_key", "cookie", "access_token", "raw_response"} {
		if strings.Contains(body, forbidden) {
			t.Fatalf("upstream response leaked %q: %s", forbidden, body)
		}
	}
	if !strings.Contains(body, `"credentials_enabled":false`) || !strings.Contains(body, `"has_credential":true`) || !strings.Contains(body, `"amount":12.5`) || !strings.Contains(body, `"unit":"USD"`) || !strings.Contains(body, `"available_count":1`) {
		t.Fatalf("missing safe credential state: %s", body)
	}
}

func TestUpstreamRechargeRateRoutesRequireAdminAndForwardCanonicalInput(t *testing.T) {
	console := &fakeUpstreamConsole{}
	server := NewServer(nil, &fakeAdminCore{user: core.AdminUser{ID: 1, Role: "admin"}}, Options{Upstreams: console}, nil)

	unauthorized := httptest.NewRequest(http.MethodPut, "/api/upstreams/up_1/recharge-rate", strings.NewReader(`{"mode":"usd_per_cny","value":5}`))
	unauthorizedResponse := httptest.NewRecorder()
	server.Handler().ServeHTTP(unauthorizedResponse, unauthorized)
	if unauthorizedResponse.Code != http.StatusUnauthorized {
		t.Fatalf("unauthorized status = %d", unauthorizedResponse.Code)
	}

	request := httptest.NewRequest(http.MethodPut, "/api/upstreams/up_1/recharge-rate", strings.NewReader(`{"mode":"usd_per_cny","value":5}`))
	request.Header.Set("Authorization", "Bearer valid")
	response := httptest.NewRecorder()
	server.Handler().ServeHTTP(response, request)
	if response.Code != http.StatusOK || console.rechargeRateInput.Mode != model.RechargeRateUSDPerCNY || console.rechargeRateInput.Value != 5 {
		t.Fatalf("set recharge rate failed: status=%d input=%#v body=%s", response.Code, console.rechargeRateInput, response.Body.String())
	}

	clearRequest := httptest.NewRequest(http.MethodDelete, "/api/upstreams/up_1/recharge-rate", nil)
	clearRequest.Header.Set("Authorization", "Bearer valid")
	clearResponse := httptest.NewRecorder()
	server.Handler().ServeHTTP(clearResponse, clearRequest)
	if clearResponse.Code != http.StatusOK || !console.rechargeRateClear {
		t.Fatalf("clear recharge rate failed: status=%d clear=%t", clearResponse.Code, console.rechargeRateClear)
	}
}

func TestUpstreamConnectForwardsOptionalNewAPIUserIDAndManagementSite(t *testing.T) {
	console := &fakeUpstreamConsole{}
	server := NewServer(nil, &fakeAdminCore{user: core.AdminUser{ID: 1, Role: "admin"}}, Options{Upstreams: console}, nil)
	request := httptest.NewRequest(http.MethodPost, "/api/upstreams/up_1/identities", strings.NewReader(`{"label":"legacy","site_url":"https://panel.example.com","auth_mode":"session","session":"session=value","user_id":"23"}`))
	request.Header.Set("Authorization", "Bearer valid")
	response := httptest.NewRecorder()
	server.Handler().ServeHTTP(response, request)
	if response.Code != http.StatusCreated || console.connectInput.Login.UserID != "23" || console.connectInput.Login.Session != "session=value" || console.connectInput.ManagementURL != "https://panel.example.com" || !console.connectInput.ManagementURLSet {
		t.Fatalf("legacy NewAPI session and management site input were not forwarded: status=%d input=%#v body=%s", response.Code, console.connectInput, response.Body.String())
	}
}

func TestUpstreamConnectKeepsMissingManagementSiteDistinctFromExplicitClear(t *testing.T) {
	console := &fakeUpstreamConsole{}
	server := NewServer(nil, &fakeAdminCore{user: core.AdminUser{ID: 1, Role: "admin"}}, Options{Upstreams: console}, nil)

	missing := httptest.NewRequest(http.MethodPost, "/api/upstreams/up_1/identities", strings.NewReader(`{"auth_mode":"token","token":"token"}`))
	missing.Header.Set("Authorization", "Bearer valid")
	missingResponse := httptest.NewRecorder()
	server.Handler().ServeHTTP(missingResponse, missing)
	if missingResponse.Code != http.StatusCreated || console.connectInput.ManagementURLSet {
		t.Fatalf("missing management URL unexpectedly changed routing: status=%d input=%#v", missingResponse.Code, console.connectInput)
	}

	clear := httptest.NewRequest(http.MethodPut, "/api/upstreams/up_1/identities/identity", strings.NewReader(`{"site_url":"","auth_mode":"token","token":"token"}`))
	clear.Header.Set("Authorization", "Bearer valid")
	clearResponse := httptest.NewRecorder()
	server.Handler().ServeHTTP(clearResponse, clear)
	if clearResponse.Code != http.StatusOK || !console.connectInput.ManagementURLSet || console.connectInput.ManagementURL != "" {
		t.Fatalf("explicit clear was not forwarded: status=%d input=%#v", clearResponse.Code, console.connectInput)
	}
}

func TestUpstreamSyncReturnsPerIdentityPartialOutcomes(t *testing.T) {
	console := &fakeUpstreamConsole{syncOutcomes: []upstream.SyncOutcome{
		{UpstreamID: "up_1", IdentityID: "good", Success: true},
		{UpstreamID: "up_1", IdentityID: "bad", Success: false, Code: "UPSTREAM_UNAVAILABLE", Message: "无法连接上游"},
	}}
	server := NewServer(nil, &fakeAdminCore{user: core.AdminUser{ID: 1, Role: "admin"}}, Options{Upstreams: console}, nil)
	request := httptest.NewRequest(http.MethodPost, "/api/upstreams/up_1/sync", nil)
	request.Header.Set("Authorization", "Bearer valid")
	response := httptest.NewRecorder()
	server.Handler().ServeHTTP(response, request)
	if response.Code != http.StatusOK || !strings.Contains(response.Body.String(), `"success":true`) || !strings.Contains(response.Body.String(), `"success":false`) {
		t.Fatalf("partial outcomes were not preserved: status=%d body=%s", response.Code, response.Body.String())
	}
}

func TestUpstreamAutoMatchPropagatesStepUpAndBrowserIdentity(t *testing.T) {
	console := &fakeUpstreamConsole{autoErr: &core.HTTPError{
		StatusCode: http.StatusForbidden,
		Code:       "STEP_UP_REQUIRED",
		Message:    "请先完成二次验证",
	}}
	server := NewServer(nil, &fakeAdminCore{user: core.AdminUser{ID: 1, Role: "admin"}}, Options{Upstreams: console}, nil)
	request := httptest.NewRequest(http.MethodPost, "/api/upstreams/up_1/auto-match", nil)
	request.RemoteAddr = "203.0.113.9:4567"
	request.Header.Set("Authorization", "Bearer browser-jwt")
	request.Header.Set("User-Agent", "browser-agent")
	response := httptest.NewRecorder()
	server.Handler().ServeHTTP(response, request)
	if response.Code != http.StatusForbidden || !strings.Contains(response.Body.String(), "STEP_UP_REQUIRED") {
		t.Fatalf("step-up response was not propagated: status=%d body=%s", response.Code, response.Body.String())
	}
	if console.autoJWT != "browser-jwt" || console.autoIdentity.ClientIP != "203.0.113.9" || console.autoIdentity.UserAgent != "browser-agent" {
		t.Fatalf("browser identity was not forwarded: jwt=%q identity=%#v", console.autoJWT, console.autoIdentity)
	}
}

func TestUpstreamErrorResponseNeverReturnsRawError(t *testing.T) {
	console := &fakeUpstreamConsole{autoErr: errors.New("raw secret body")}
	server := NewServer(nil, &fakeAdminCore{user: core.AdminUser{ID: 1, Role: "admin"}}, Options{Upstreams: console}, nil)
	request := httptest.NewRequest(http.MethodPost, "/api/upstreams/up_1/auto-match", nil)
	request.Header.Set("Authorization", "Bearer valid")
	response := httptest.NewRecorder()
	server.Handler().ServeHTTP(response, request)
	if response.Code != http.StatusBadGateway || strings.Contains(response.Body.String(), "raw secret body") {
		t.Fatalf("raw error escaped: status=%d body=%s", response.Code, response.Body.String())
	}
}
