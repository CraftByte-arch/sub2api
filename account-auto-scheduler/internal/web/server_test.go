package web

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api-account-auto-scheduler/internal/core"
	"github.com/Wei-Shaw/sub2api-account-auto-scheduler/internal/engine"
	"github.com/Wei-Shaw/sub2api-account-auto-scheduler/internal/model"
	"github.com/Wei-Shaw/sub2api-account-auto-scheduler/internal/store"
	"github.com/Wei-Shaw/sub2api-account-auto-scheduler/internal/upstream"
)

type fakeAdminCore struct {
	user     core.AdminUser
	err      error
	identity core.ForwardedIdentity
	token    string
}

func (f *fakeAdminCore) ValidateAdminJWT(_ context.Context, token string, identity core.ForwardedIdentity) (core.AdminUser, error) {
	f.token = token
	f.identity = identity
	return f.user, f.err
}

func (f *fakeAdminCore) RegisterAdminMenu(context.Context, string) error { return nil }

type fakeConsoleCore struct {
	fakeAdminCore
	accounts        []model.UpstreamAccount
	groups          []model.UpstreamGroup
	today           map[string]model.WindowStats
	usage           model.AccountUsageInfo
	consoleErr      error
	usageErr        error
	bindingErr      error
	bindingErrByID  map[int64]error
	accountErr      error
	setErr          error
	overviewCalls   int
	groupCalls      int
	getAccountCalls int
	setCalls        []bool
	boundAccountID  int64
	boundGroupID    int64
	bound           bool
	bindingCalls    []groupBindingCall
	exportErr       error
	exportErrByID   map[int64]error
	exportCalls     []int64
	exportToken     string
	exportIdentity  core.ForwardedIdentity
}

type groupBindingCall struct {
	accountID int64
	groupID   int64
	bound     bool
}

func (f *fakeConsoleCore) ListAccounts(context.Context) ([]model.UpstreamAccount, error) {
	f.overviewCalls++
	return f.accounts, f.consoleErr
}

func (f *fakeConsoleCore) ListGroups(context.Context) ([]model.UpstreamGroup, error) {
	f.groupCalls++
	return f.groups, f.consoleErr
}

func (f *fakeConsoleCore) GetTodayStatsBatch(context.Context, []int64) (map[string]model.WindowStats, error) {
	return f.today, f.consoleErr
}

func (f *fakeConsoleCore) GetPassiveUsage(context.Context, int64) (model.AccountUsageInfo, error) {
	return f.usage, f.usageErr
}

func (f *fakeConsoleCore) SetAccountGroup(_ context.Context, accountID, groupID int64, bound bool) (model.UpstreamAccount, error) {
	f.boundAccountID = accountID
	f.boundGroupID = groupID
	f.bound = bound
	f.bindingCalls = append(f.bindingCalls, groupBindingCall{accountID: accountID, groupID: groupID, bound: bound})
	if err := f.bindingErrByID[accountID]; err != nil {
		return model.UpstreamAccount{}, err
	}
	if f.bindingErr != nil {
		return model.UpstreamAccount{}, f.bindingErr
	}
	for index := range f.accounts {
		if f.accounts[index].ID != accountID {
			continue
		}
		if bound && !containsID(f.accounts[index].GroupIDs, groupID) {
			f.accounts[index].GroupIDs = append(f.accounts[index].GroupIDs, groupID)
		}
		if !bound {
			filtered := make([]int64, 0, len(f.accounts[index].GroupIDs))
			for _, existingID := range f.accounts[index].GroupIDs {
				if existingID != groupID {
					filtered = append(filtered, existingID)
				}
			}
			f.accounts[index].GroupIDs = filtered
		}
		return f.accounts[index], nil
	}
	return model.UpstreamAccount{ID: accountID}, nil
}

func (f *fakeConsoleCore) ListAPIKeyAccounts(context.Context) ([]model.UpstreamAccount, error) {
	return f.accounts, f.consoleErr
}

func (f *fakeConsoleCore) GetAccount(_ context.Context, accountID int64) (model.UpstreamAccount, error) {
	f.getAccountCalls++
	if f.accountErr != nil {
		return model.UpstreamAccount{}, f.accountErr
	}
	for _, account := range f.accounts {
		if account.ID == accountID {
			return account, nil
		}
	}
	return model.UpstreamAccount{}, errors.New("account not found")
}

func (f *fakeConsoleCore) TestAccount(context.Context, int64, string, string) (core.ProbeOutcome, error) {
	return core.ProbeOutcome{}, errors.New("not implemented")
}

func (f *fakeConsoleCore) SetSchedulable(_ context.Context, accountID int64, enabled bool) (model.UpstreamAccount, error) {
	f.setCalls = append(f.setCalls, enabled)
	if f.setErr != nil {
		return model.UpstreamAccount{}, f.setErr
	}
	for index := range f.accounts {
		if f.accounts[index].ID == accountID {
			f.accounts[index].Schedulable = enabled
			return f.accounts[index], nil
		}
	}
	return model.UpstreamAccount{}, errors.New("account not found")
}

func (f *fakeConsoleCore) ExportDirectProbeSnapshot(_ context.Context, accountID int64, adminJWT string, identity core.ForwardedIdentity) (core.DirectProbeExport, error) {
	f.exportCalls = append(f.exportCalls, accountID)
	f.exportToken = adminJWT
	f.exportIdentity = identity
	if err := f.exportErrByID[accountID]; err != nil {
		return core.DirectProbeExport{}, err
	}
	if f.exportErr != nil {
		return core.DirectProbeExport{}, f.exportErr
	}
	for _, account := range f.accounts {
		if account.ID != accountID {
			continue
		}
		baseURL, err := model.EffectiveDirectProbeBaseURL(account.BaseURL(), account.Platform)
		if err != nil {
			return core.DirectProbeExport{}, err
		}
		snapshot := model.DirectProbeSnapshot{
			Version:   model.DirectProbeSnapshotVersion,
			AccountID: account.ID,
			Platform:  account.Platform,
			BaseURL:   baseURL,
			APIKey:    "direct-api-secret",
		}
		snapshot.RoutingFingerprint = model.DirectProbeRoutingFingerprint(snapshot)
		return core.DirectProbeExport{Snapshot: snapshot}, nil
	}
	return core.DirectProbeExport{}, errors.New("account not found")
}

func TestAdminAPIRequiresValidatedAdminJWT(t *testing.T) {
	tests := []struct {
		name       string
		authorizer *fakeAdminCore
		header     string
		wantStatus int
	}{
		{name: "missing token", authorizer: &fakeAdminCore{}, wantStatus: http.StatusUnauthorized},
		{name: "validation failure", authorizer: &fakeAdminCore{err: errors.New("invalid")}, header: "Bearer bad", wantStatus: http.StatusForbidden},
		{name: "non-admin", authorizer: &fakeAdminCore{user: core.AdminUser{Role: "user"}}, header: "Bearer user", wantStatus: http.StatusForbidden},
		{name: "admin", authorizer: &fakeAdminCore{user: core.AdminUser{ID: 1, Role: "admin"}}, header: "Bearer valid", wantStatus: http.StatusOK},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			server := NewServer(nil, test.authorizer, Options{AuthCacheTTL: time.Second}, nil)
			request := httptest.NewRequest(http.MethodGet, "/api/session", nil)
			request.RemoteAddr = "203.0.113.4:3456"
			request.Header.Set("User-Agent", "browser-agent")
			if test.header != "" {
				request.Header.Set("Authorization", test.header)
			}
			response := httptest.NewRecorder()
			server.Handler().ServeHTTP(response, request)
			if response.Code != test.wantStatus {
				t.Fatalf("status = %d, want %d; body=%s", response.Code, test.wantStatus, response.Body.String())
			}
			if test.wantStatus == http.StatusOK {
				if test.authorizer.token != "valid" || test.authorizer.identity.ClientIP != "203.0.113.4" || test.authorizer.identity.UserAgent != "browser-agent" {
					t.Fatalf("unexpected forwarded identity: token=%q identity=%#v", test.authorizer.token, test.authorizer.identity)
				}
			}
		})
	}
}

func TestTrustedProxyHeadersUseFirstValidClientIP(t *testing.T) {
	authorizer := &fakeAdminCore{user: core.AdminUser{ID: 1, Role: "admin"}}
	server := NewServer(nil, authorizer, Options{TrustProxyHeaders: true}, nil)
	request := httptest.NewRequest(http.MethodGet, "/api/session", nil)
	request.Header.Set("Authorization", "Bearer valid")
	request.Header.Set("X-Forwarded-For", "198.51.100.7, 10.0.0.2")
	response := httptest.NewRecorder()
	server.Handler().ServeHTTP(response, request)
	if response.Code != http.StatusOK || authorizer.identity.ClientIP != "198.51.100.7" {
		t.Fatalf("status=%d identity=%#v", response.Code, authorizer.identity)
	}
}

func TestStaticPageIsPublicButFramingIsRestricted(t *testing.T) {
	server := NewServer(nil, &fakeAdminCore{}, Options{UIOrigin: "https://sub2api.example.com"}, nil)
	request := httptest.NewRequest(http.MethodGet, "/", nil)
	response := httptest.NewRecorder()
	server.Handler().ServeHTTP(response, request)
	if response.Code != http.StatusOK {
		t.Fatalf("status = %d; body=%s", response.Code, response.Body.String())
	}
	csp := response.Header().Get("Content-Security-Policy")
	if !strings.Contains(csp, "frame-ancestors 'self' https://sub2api.example.com") || !strings.Contains(csp, "script-src 'self'") {
		t.Fatalf("unexpected CSP: %s", csp)
	}
	if !strings.Contains(response.Body.String(), "账号自动调度") {
		t.Fatal("static admin page was not served")
	}
	if !strings.Contains(response.Body.String(), `id="upstream-connect-form" method="dialog" class="modal-panel" autocomplete="off"`) ||
		!strings.Contains(response.Body.String(), `id="upstream-password-input" type="password" autocomplete="new-password"`) ||
		!strings.Contains(response.Body.String(), `id="upstream-management-site-input" type="url"`) ||
		!strings.Contains(response.Body.String(), "登录、身份验证、余额、Key 和倍率同步均使用此地址；留空时使用 API 地址。模型调用和本地账号关联始终保留 API 地址。") {
		t.Fatal("upstream login form must not reuse the current admin site's saved password")
	}
}

func TestGroupsAppUsesFinalMultiplierInsteadOfProbeMultiplier(t *testing.T) {
	server := NewServer(nil, &fakeAdminCore{}, Options{}, nil)
	response := httptest.NewRecorder()
	server.Handler().ServeHTTP(response, httptest.NewRequest(http.MethodGet, "/app.js", nil))
	if response.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", response.Code, response.Body.String())
	}
	body := response.Body.String()
	if !strings.Contains(body, "renderFinalMultiplier") || !strings.Contains(body, "upstream_final_multiplier") || !strings.Contains(body, "最终倍率") {
		t.Fatalf("groups app is missing final-multiplier rendering: %s", body)
	}
	if strings.Contains(body, "renderDetectedRate") || strings.Contains(body, "探测倍率") {
		t.Fatalf("groups app still renders the billing-probe multiplier: %s", body)
	}
	if !strings.Contains(body, "prompt: elements.promptInput.value,") || strings.Contains(body, "prompt: elements.promptInput.value.trim()") {
		t.Fatal("groups app must submit the custom probe prompt without trimming it")
	}
	if !strings.Contains(body, "`/api/configs/${accountID}/direct-probe`, { method: 'POST', body: {} }") ||
		!strings.Contains(body, "`/api/configs/${accountID}/direct-probe`, { method: 'DELETE' }") {
		t.Fatal("groups app is missing the bounded direct-probe authorization or revocation request")
	}
	for _, forbidden := range []string{"api_key:", "proxy_password:", "admin_jwt:"} {
		if strings.Contains(body, forbidden) {
			t.Fatalf("groups app constructs forbidden direct-probe secret field %q", forbidden)
		}
	}
}

func TestUpstreamAssetsRenderBalanceStatesAndNarrowLayout(t *testing.T) {
	server := NewServer(nil, &fakeAdminCore{}, Options{}, nil)

	scriptResponse := httptest.NewRecorder()
	server.Handler().ServeHTTP(scriptResponse, httptest.NewRequest(http.MethodGet, "/upstreams.js", nil))
	if scriptResponse.Code != http.StatusOK {
		t.Fatalf("script status=%d body=%s", scriptResponse.Code, scriptResponse.Body.String())
	}
	script := scriptResponse.Body.String()
	for _, required := range []string{
		"renderUpstreamBalanceSummary", "renderIdentityBalance", "formatUpstreamBalance",
		"summary.status === 'single'", "summary.status === 'multiple'", "各登录账号余额分别展示，不合计",
		"站点余额", "暂不可获取", "旧数据", "原始额度", "quota_per_unit",
		"upstream-summary", "renderResourceSummary", "upstream-resource-summary", "充值倍率",
		"identity-key-section", "identity-key-heading", "上游 Key", "添加身份",
		"site_url: elements.upstreamManagementSiteInput.value.trim()", "API 地址（模型调用）：${upstream.base_url}", "管理站点", "upstream-management-site-input", "UPSTREAM_MANAGEMENT_ACCESS_DENIED",
	} {
		if !strings.Contains(script, required) {
			t.Fatalf("upstream script is missing balance state %q", required)
		}
	}

	styleResponse := httptest.NewRecorder()
	server.Handler().ServeHTTP(styleResponse, httptest.NewRequest(http.MethodGet, "/app.css", nil))
	if styleResponse.Code != http.StatusOK {
		t.Fatalf("style status=%d body=%s", styleResponse.Code, styleResponse.Body.String())
	}
	style := styleResponse.Body.String()
	for _, required := range []string{
		".upstream-balance-stat.stale", ".identity-balance", ".identity-balance.stale strong",
		"@media (max-width: 620px)", ".identity-balance, .identity-timestamps, .identity-actions { grid-column: 1; grid-row: auto; }",
		".upstream-summary", ".upstream-balance-stat strong", ".identity-key-section", ".identity-key-heading",
		".upstream-summary { grid-template-columns: minmax(0, 1fr);", ".upstream-routes", ".management-site-url > span",
	} {
		if !strings.Contains(style, required) {
			t.Fatalf("upstream stylesheet is missing responsive balance rule %q", required)
		}
	}
}

func TestOverviewRequiresAdminBeforeCallingConsole(t *testing.T) {
	backend := &fakeConsoleCore{}
	server := newConsoleTestServer(t, backend, nil)
	request := httptest.NewRequest(http.MethodGet, "/api/overview", nil)
	response := httptest.NewRecorder()
	server.Handler().ServeHTTP(response, request)
	if response.Code != http.StatusUnauthorized || backend.overviewCalls != 0 {
		t.Fatalf("status=%d calls=%d body=%s", response.Code, backend.overviewCalls, response.Body.String())
	}
}

func TestOverviewJoinsGroupsAccountsUsageAndConfig(t *testing.T) {
	backend := &fakeConsoleCore{
		fakeAdminCore: fakeAdminCore{user: core.AdminUser{ID: 1, Role: "admin"}},
		groups:        []model.UpstreamGroup{{ID: 7, Name: "Primary", AccountCount: 2, ActiveAccountCount: 1}},
		accounts: []model.UpstreamAccount{{
			ID: 9, Name: "key", Platform: "openai", Type: "apikey", Status: "active", Schedulable: false, GroupIDs: []int64{7},
			DetectedRate: &model.DetectedRate{Status: "operational", EffectiveMultiplier: float64Pointer(9.9)},
		}},
		today: map[string]model.WindowStats{"9": {Requests: 4, Tokens: 1200, Cost: 0.75}},
	}
	managed := model.ManagedAccount{
		AccountID: 9, Name: "key", ManagedSuspended: true, Policy: model.DefaultPolicy(), CreatedAt: time.Now(), UpdatedAt: time.Now(),
	}
	finalMultiplier := 0.16
	server := newConsoleTestServer(t, backend, &managed, &fakeUpstreamConsole{finalMultipliers: map[int64]upstream.LocalAccountFinalMultiplier{
		9: {Status: "available", FinalMultiplier: &finalMultiplier},
	}})
	request := authenticatedRequest(http.MethodGet, "/api/overview", "")
	response := httptest.NewRecorder()
	server.Handler().ServeHTTP(response, request)
	if response.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", response.Code, response.Body.String())
	}
	var payload struct {
		Groups   []model.UpstreamGroup `json:"groups"`
		Accounts []struct {
			ID                      int64                                 `json:"id"`
			TodayUsage              model.WindowStats                     `json:"today_usage"`
			Config                  *model.ManagedAccount                 `json:"config"`
			UpstreamFinalMultiplier *upstream.LocalAccountFinalMultiplier `json:"upstream_final_multiplier"`
			DetectedRate            *model.DetectedRate                   `json:"detected_rate"`
		} `json:"accounts"`
	}
	if err := json.NewDecoder(response.Body).Decode(&payload); err != nil {
		t.Fatal(err)
	}
	if len(payload.Groups) != 1 || len(payload.Accounts) != 1 || payload.Accounts[0].TodayUsage.Tokens != 1200 {
		t.Fatalf("unexpected overview: %#v", payload)
	}
	if payload.Accounts[0].Config == nil || !payload.Accounts[0].Config.ManagedSuspended {
		t.Fatalf("managed suspension missing: %#v", payload.Accounts[0])
	}
	if payload.Accounts[0].DetectedRate != nil || payload.Accounts[0].UpstreamFinalMultiplier == nil || payload.Accounts[0].UpstreamFinalMultiplier.FinalMultiplier == nil || *payload.Accounts[0].UpstreamFinalMultiplier.FinalMultiplier != 0.16 {
		t.Fatalf("overview multiplier projection is incorrect: %#v", payload.Accounts[0])
	}
}

func TestAccountUsageAndGroupBindingAreAdminProtected(t *testing.T) {
	backend := &fakeConsoleCore{
		fakeAdminCore: fakeAdminCore{user: core.AdminUser{ID: 1, Role: "admin"}},
		usage:         model.AccountUsageInfo{Source: "passive", FiveHour: &model.UsageProgress{Utilization: 31}},
	}
	server := newConsoleTestServer(t, backend, nil)

	usageResponse := httptest.NewRecorder()
	server.Handler().ServeHTTP(usageResponse, authenticatedRequest(http.MethodGet, "/api/accounts/9/usage", ""))
	if usageResponse.Code != http.StatusOK || !strings.Contains(usageResponse.Body.String(), `"utilization":31`) {
		t.Fatalf("usage status=%d body=%s", usageResponse.Code, usageResponse.Body.String())
	}

	bindingResponse := httptest.NewRecorder()
	server.Handler().ServeHTTP(bindingResponse, authenticatedRequest(http.MethodPut, "/api/groups/7/accounts/9", `{"bound":true}`))
	if bindingResponse.Code != http.StatusOK || backend.boundAccountID != 9 || backend.boundGroupID != 7 || !backend.bound {
		t.Fatalf("binding status=%d account=%d group=%d bound=%v body=%s", bindingResponse.Code, backend.boundAccountID, backend.boundGroupID, backend.bound, bindingResponse.Body.String())
	}
}

func TestGroupBindingSurfacesMixedChannelConflict(t *testing.T) {
	backend := &fakeConsoleCore{
		fakeAdminCore: fakeAdminCore{user: core.AdminUser{ID: 1, Role: "admin"}},
		bindingErr:    &core.HTTPError{StatusCode: http.StatusConflict, Message: "mixed_channel_warning"},
	}
	server := newConsoleTestServer(t, backend, nil)
	response := httptest.NewRecorder()
	server.Handler().ServeHTTP(response, authenticatedRequest(http.MethodPut, "/api/groups/8/accounts/4", `{"bound":true}`))
	if response.Code != http.StatusConflict || !strings.Contains(response.Body.String(), "MIXED_CHANNEL_RISK") {
		t.Fatalf("status=%d body=%s", response.Code, response.Body.String())
	}
}

func TestAccountCompatibleWithGroup(t *testing.T) {
	tests := []struct {
		name    string
		account model.UpstreamAccount
		group   model.UpstreamGroup
		want    bool
	}{
		{name: "matching platform", account: model.UpstreamAccount{Platform: "openai"}, group: model.UpstreamGroup{Platform: "OpenAI"}, want: true},
		{name: "openai excludes anthropic", account: model.UpstreamAccount{Platform: "anthropic"}, group: model.UpstreamGroup{Platform: "openai"}},
		{name: "composite accepts concrete platform", account: model.UpstreamAccount{Platform: "grok"}, group: model.UpstreamGroup{Platform: "composite"}, want: true},
		{name: "mixed antigravity supports anthropic", account: model.UpstreamAccount{Platform: "antigravity", MixedScheduling: true}, group: model.UpstreamGroup{Platform: "anthropic"}, want: true},
		{name: "mixed antigravity supports gemini", account: model.UpstreamAccount{Platform: "antigravity", MixedScheduling: true}, group: model.UpstreamGroup{Platform: "gemini"}, want: true},
		{name: "plain antigravity excluded from anthropic", account: model.UpstreamAccount{Platform: "antigravity"}, group: model.UpstreamGroup{Platform: "anthropic"}},
		{name: "empty platform is incompatible", account: model.UpstreamAccount{Platform: "openai"}, group: model.UpstreamGroup{}},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if got := accountCompatibleWithGroup(test.account, test.group); got != test.want {
				t.Fatalf("accountCompatibleWithGroup() = %v, want %v", got, test.want)
			}
		})
	}
}

func TestBulkGroupBindingRequiresAdminBeforeReadingConsole(t *testing.T) {
	backend := &fakeConsoleCore{}
	server := newConsoleTestServer(t, backend, nil)
	response := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodPut, "/api/groups/7/accounts", strings.NewReader(`{"account_ids":[]}`))
	request.Header.Set("Content-Type", "application/json")
	server.Handler().ServeHTTP(response, request)
	if response.Code != http.StatusUnauthorized || backend.groupCalls != 0 || backend.overviewCalls != 0 || len(backend.bindingCalls) != 0 {
		t.Fatalf("status=%d groups=%d accounts=%d bindings=%#v", response.Code, backend.groupCalls, backend.overviewCalls, backend.bindingCalls)
	}
}

func TestBulkGroupBindingReconcilesAPIKeysOnly(t *testing.T) {
	backend := &fakeConsoleCore{
		fakeAdminCore: fakeAdminCore{user: core.AdminUser{ID: 1, Role: "admin"}},
		groups:        []model.UpstreamGroup{{ID: 7, Name: "OpenAI", Platform: "openai"}},
		accounts: []model.UpstreamAccount{
			{ID: 1, Name: "old-key", Platform: "openai", Type: "apikey", GroupIDs: []int64{5, 7}},
			{ID: 2, Name: "new-key", Platform: "openai", Type: "apikey", GroupIDs: []int64{6}},
			{ID: 3, Name: "oauth", Platform: "openai", Type: "oauth", GroupIDs: []int64{7}},
			{ID: 4, Name: "claude-key", Platform: "anthropic", Type: "apikey"},
		},
	}
	server := newConsoleTestServer(t, backend, nil)
	response := httptest.NewRecorder()
	server.Handler().ServeHTTP(response, authenticatedRequest(http.MethodPut, "/api/groups/7/accounts", `{"account_ids":[2]}`))
	if response.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", response.Code, response.Body.String())
	}
	if len(backend.bindingCalls) != 2 || backend.bindingCalls[0] != (groupBindingCall{accountID: 1, groupID: 7, bound: false}) || backend.bindingCalls[1] != (groupBindingCall{accountID: 2, groupID: 7, bound: true}) {
		t.Fatalf("unexpected binding calls: %#v", backend.bindingCalls)
	}
	if !containsID(backend.accounts[0].GroupIDs, 5) || containsID(backend.accounts[0].GroupIDs, 7) || !containsID(backend.accounts[1].GroupIDs, 6) || !containsID(backend.accounts[1].GroupIDs, 7) {
		t.Fatalf("unrelated memberships changed: %#v", backend.accounts)
	}
	if !containsID(backend.accounts[2].GroupIDs, 7) {
		t.Fatalf("OAuth membership was changed: %#v", backend.accounts[2])
	}
	var payload bulkGroupBindingResponse
	if err := json.NewDecoder(response.Body).Decode(&payload); err != nil {
		t.Fatal(err)
	}
	if len(payload.UpdatedAccountIDs) != 2 || len(payload.Failures) != 0 {
		t.Fatalf("unexpected response: %#v", payload)
	}
}

func TestBulkGroupBindingEmptySelectionRemovesOnlyAPIKeys(t *testing.T) {
	backend := &fakeConsoleCore{
		fakeAdminCore: fakeAdminCore{user: core.AdminUser{ID: 1, Role: "admin"}},
		groups:        []model.UpstreamGroup{{ID: 7, Platform: "openai"}},
		accounts: []model.UpstreamAccount{
			{ID: 1, Platform: "openai", Type: "apikey", GroupIDs: []int64{7}},
			{ID: 2, Platform: "openai", Type: "oauth", GroupIDs: []int64{7}},
		},
	}
	server := newConsoleTestServer(t, backend, nil)
	response := httptest.NewRecorder()
	server.Handler().ServeHTTP(response, authenticatedRequest(http.MethodPut, "/api/groups/7/accounts", `{"account_ids":[]}`))
	if response.Code != http.StatusOK || len(backend.bindingCalls) != 1 || backend.bindingCalls[0].accountID != 1 || backend.bindingCalls[0].bound {
		t.Fatalf("status=%d calls=%#v body=%s", response.Code, backend.bindingCalls, response.Body.String())
	}
}

func TestBulkGroupBindingRejectsInvalidSelectionBeforeMutation(t *testing.T) {
	tests := []struct {
		name string
		body string
	}{
		{name: "missing IDs", body: `{}`},
		{name: "duplicate ID", body: `{"account_ids":[2,2]}`},
		{name: "unknown ID", body: `{"account_ids":[99]}`},
		{name: "OAuth account", body: `{"account_ids":[3]}`},
		{name: "incompatible platform", body: `{"account_ids":[4]}`},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			backend := &fakeConsoleCore{
				fakeAdminCore: fakeAdminCore{user: core.AdminUser{ID: 1, Role: "admin"}},
				groups:        []model.UpstreamGroup{{ID: 7, Platform: "openai"}},
				accounts: []model.UpstreamAccount{
					{ID: 2, Platform: "openai", Type: "apikey"},
					{ID: 3, Platform: "openai", Type: "oauth"},
					{ID: 4, Platform: "anthropic", Type: "apikey"},
				},
			}
			server := newConsoleTestServer(t, backend, nil)
			response := httptest.NewRecorder()
			server.Handler().ServeHTTP(response, authenticatedRequest(http.MethodPut, "/api/groups/7/accounts", test.body))
			if response.Code != http.StatusBadRequest || len(backend.bindingCalls) != 0 {
				t.Fatalf("status=%d calls=%#v body=%s", response.Code, backend.bindingCalls, response.Body.String())
			}
		})
	}
}

func TestBulkGroupBindingAllowsRemovalOfLegacyIncompatibleMembership(t *testing.T) {
	backend := &fakeConsoleCore{
		fakeAdminCore: fakeAdminCore{user: core.AdminUser{ID: 1, Role: "admin"}},
		groups:        []model.UpstreamGroup{{ID: 7, Platform: "openai"}},
		accounts:      []model.UpstreamAccount{{ID: 4, Name: "legacy", Platform: "anthropic", Type: "apikey", GroupIDs: []int64{7}}},
	}
	server := newConsoleTestServer(t, backend, nil)

	unchanged := httptest.NewRecorder()
	server.Handler().ServeHTTP(unchanged, authenticatedRequest(http.MethodPut, "/api/groups/7/accounts", `{"account_ids":[4]}`))
	if unchanged.Code != http.StatusOK || len(backend.bindingCalls) != 0 {
		t.Fatalf("retain status=%d calls=%#v body=%s", unchanged.Code, backend.bindingCalls, unchanged.Body.String())
	}

	removed := httptest.NewRecorder()
	server.Handler().ServeHTTP(removed, authenticatedRequest(http.MethodPut, "/api/groups/7/accounts", `{"account_ids":[]}`))
	if removed.Code != http.StatusOK || len(backend.bindingCalls) != 1 || backend.bindingCalls[0].accountID != 4 || backend.bindingCalls[0].bound {
		t.Fatalf("remove status=%d calls=%#v body=%s", removed.Code, backend.bindingCalls, removed.Body.String())
	}
}

func TestBulkGroupBindingReturnsStructuredPartialFailures(t *testing.T) {
	backend := &fakeConsoleCore{
		fakeAdminCore: fakeAdminCore{user: core.AdminUser{ID: 1, Role: "admin"}},
		groups:        []model.UpstreamGroup{{ID: 7, Platform: "openai"}},
		accounts: []model.UpstreamAccount{
			{ID: 1, Name: "remove-me", Platform: "openai", Type: "apikey", GroupIDs: []int64{7}},
			{ID: 2, Name: "blocked", Platform: "openai", Type: "apikey"},
		},
		bindingErrByID: map[int64]error{2: &core.HTTPError{StatusCode: http.StatusConflict, Message: "mixed_channel_warning"}},
	}
	server := newConsoleTestServer(t, backend, nil)
	response := httptest.NewRecorder()
	server.Handler().ServeHTTP(response, authenticatedRequest(http.MethodPut, "/api/groups/7/accounts", `{"account_ids":[2]}`))
	if response.Code != http.StatusMultiStatus {
		t.Fatalf("status=%d body=%s", response.Code, response.Body.String())
	}
	var payload bulkGroupBindingResponse
	if err := json.NewDecoder(response.Body).Decode(&payload); err != nil {
		t.Fatal(err)
	}
	if len(payload.UpdatedAccountIDs) != 1 || payload.UpdatedAccountIDs[0] != 1 || len(payload.Failures) != 1 || payload.Failures[0].AccountID != 2 || payload.Failures[0].Name != "blocked" || payload.Failures[0].Code != "MIXED_CHANNEL_RISK" {
		t.Fatalf("unexpected partial response: %#v", payload)
	}
}

func TestAutomationConfigMutationsRequireAdminAndRestoreManualStops(t *testing.T) {
	backend := &fakeConsoleCore{
		fakeAdminCore: fakeAdminCore{user: core.AdminUser{ID: 1, Role: "admin"}},
		accounts: []model.UpstreamAccount{{
			ID: 9, Name: "manual-key", Platform: "openai", Type: "apikey", Status: "active", Schedulable: false,
		}},
	}
	server := newConsoleTestServer(t, backend, nil)

	unauthorized := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodPost, "/api/configs", strings.NewReader(`{"account_id":9,"enabled":true}`))
	request.Header.Set("Content-Type", "application/json")
	server.Handler().ServeHTTP(unauthorized, request)
	if unauthorized.Code != http.StatusUnauthorized || backend.getAccountCalls != 0 || len(backend.setCalls) != 0 {
		t.Fatalf("unauthorized mutation reached engine: status=%d get=%d set=%#v", unauthorized.Code, backend.getAccountCalls, backend.setCalls)
	}

	created := httptest.NewRecorder()
	server.Handler().ServeHTTP(created, authenticatedRequest(http.MethodPost, "/api/configs", `{"account_id":9,"enabled":true}`))
	if created.Code != http.StatusCreated || backend.getAccountCalls != 1 || len(backend.setCalls) != 1 || !backend.setCalls[0] {
		t.Fatalf("create status=%d get=%d set=%#v body=%s", created.Code, backend.getAccountCalls, backend.setCalls, created.Body.String())
	}
	var createdConfig model.ManagedAccount
	if err := json.NewDecoder(created.Body).Decode(&createdConfig); err != nil {
		t.Fatal(err)
	}
	if !createdConfig.Policy.Enabled || createdConfig.NextCheckAt == nil || !createdConfig.Schedulable {
		t.Fatalf("unexpected created automation config: %#v", createdConfig)
	}

	backend.accounts[0].Schedulable = false
	updated := httptest.NewRecorder()
	server.Handler().ServeHTTP(updated, authenticatedRequest(http.MethodPut, "/api/configs/9", `{"enabled":true}`))
	if updated.Code != http.StatusOK || backend.getAccountCalls != 2 || len(backend.setCalls) != 2 || !backend.setCalls[1] {
		t.Fatalf("update status=%d get=%d set=%#v body=%s", updated.Code, backend.getAccountCalls, backend.setCalls, updated.Body.String())
	}
}

func TestAutomationConfigSurfacesSchedulingRestoreFailure(t *testing.T) {
	backend := &fakeConsoleCore{
		fakeAdminCore: fakeAdminCore{user: core.AdminUser{ID: 1, Role: "admin"}},
		accounts: []model.UpstreamAccount{{
			ID: 9, Name: "manual-key", Platform: "openai", Type: "apikey", Status: "active", Schedulable: false,
		}},
		setErr: errors.New("upstream unavailable"),
	}
	server := newConsoleTestServer(t, backend, nil)
	response := httptest.NewRecorder()
	server.Handler().ServeHTTP(response, authenticatedRequest(http.MethodPost, "/api/configs", `{"account_id":9,"enabled":true}`))
	if response.Code != http.StatusBadGateway || !strings.Contains(response.Body.String(), "SCHEDULING_UPDATE_FAILED") {
		t.Fatalf("status=%d body=%s", response.Code, response.Body.String())
	}
}

func TestDirectProbeAuthorizationRequiresAdminBeforeExport(t *testing.T) {
	account := directProbeTestAccount(9)
	backend := &fakeConsoleCore{
		fakeAdminCore: fakeAdminCore{user: core.AdminUser{ID: 2, Role: "user"}},
		accounts:      []model.UpstreamAccount{account},
	}
	server := newDirectProbeTestServer(t, backend, directProbeManagedAccount(account))
	request := httptest.NewRequest(http.MethodPost, "/api/configs/9/direct-probe", strings.NewReader(`{}`))
	request.Header.Set("Authorization", "Bearer user-session")
	request.Header.Set("Content-Type", "application/json")
	response := httptest.NewRecorder()

	server.Handler().ServeHTTP(response, request)

	if response.Code != http.StatusForbidden || len(backend.exportCalls) != 0 || backend.getAccountCalls != 0 {
		t.Fatalf("status=%d exports=%#v account_reads=%d body=%s", response.Code, backend.exportCalls, backend.getAccountCalls, response.Body.String())
	}
}

func TestDirectProbeAuthorizationPropagatesStepUpFailure(t *testing.T) {
	account := directProbeTestAccount(9)
	backend := &fakeConsoleCore{
		fakeAdminCore: fakeAdminCore{user: core.AdminUser{ID: 1, Role: "admin"}},
		accounts:      []model.UpstreamAccount{account},
		exportErr: &core.HTTPError{
			StatusCode: http.StatusForbidden,
			Code:       "STEP_UP_REQUIRED",
			Message:    "请先完成管理员二次验证",
		},
	}
	server := newDirectProbeTestServer(t, backend, directProbeManagedAccount(account))
	request := authenticatedRequest(http.MethodPost, "/api/configs/9/direct-probe", `{}`)
	request.RemoteAddr = "203.0.113.9:4321"
	request.Header.Set("User-Agent", "probe-browser")
	response := httptest.NewRecorder()

	server.Handler().ServeHTTP(response, request)

	if response.Code != http.StatusForbidden || !strings.Contains(response.Body.String(), `"code":"STEP_UP_REQUIRED"`) || !strings.Contains(response.Body.String(), "请先完成管理员二次验证") {
		t.Fatalf("status=%d body=%s", response.Code, response.Body.String())
	}
	if len(backend.exportCalls) != 1 || backend.exportCalls[0] != 9 || backend.exportToken != "valid" || backend.exportIdentity.ClientIP != "203.0.113.9" || backend.exportIdentity.UserAgent != "probe-browser" {
		t.Fatalf("unexpected export context: calls=%#v token=%q identity=%#v", backend.exportCalls, backend.exportToken, backend.exportIdentity)
	}
	managed := server.engine.List()[0]
	if managed.EffectiveProbeSource() != model.ProbeSourceLegacy || managed.DirectProbe != nil {
		t.Fatalf("failed step-up changed direct authorization: %#v", managed)
	}
}

func TestBatchDirectProbeAuthorizationReturnsRedactedViews(t *testing.T) {
	first := directProbeTestAccount(9)
	second := directProbeTestAccount(10)
	second.Name = "second-key"
	backend := &fakeConsoleCore{
		fakeAdminCore: fakeAdminCore{user: core.AdminUser{ID: 1, Role: "admin"}},
		accounts:      []model.UpstreamAccount{first, second},
	}
	server := newDirectProbeTestServer(t, backend, directProbeManagedAccount(first), directProbeManagedAccount(second))
	response := httptest.NewRecorder()

	server.Handler().ServeHTTP(response, authenticatedRequest(http.MethodPost, "/api/direct-probes/authorize", `{"account_ids":[9,10]}`))

	if response.Code != http.StatusOK || len(backend.exportCalls) != 2 || backend.exportCalls[0] != 9 || backend.exportCalls[1] != 10 {
		t.Fatalf("status=%d calls=%#v body=%s", response.Code, backend.exportCalls, response.Body.String())
	}
	var payload directProbeBatchResponse
	if err := json.NewDecoder(response.Body).Decode(&payload); err != nil {
		t.Fatal(err)
	}
	if len(payload.Authorized) != 2 || len(payload.Failures) != 0 {
		t.Fatalf("unexpected batch response: %#v", payload)
	}
	assertDirectProbeResponseRedacted(t, response.Body.String())
	for _, managed := range server.engine.List() {
		if managed.EffectiveProbeSource() != model.ProbeSourceDirect || managed.DirectProbe == nil || managed.DirectProbe.AuthorizationState != model.DirectProbeAuthorized || managed.DirectProbe.Credential.Ciphertext == "" {
			t.Fatalf("account %d was not stored as encrypted direct authorization: %#v", managed.AccountID, managed.DirectProbe)
		}
		if strings.Contains(managed.DirectProbe.Credential.Ciphertext, "direct-api-secret") {
			t.Fatalf("account %d stored plaintext API key", managed.AccountID)
		}
	}

	statusResponse := httptest.NewRecorder()
	server.Handler().ServeHTTP(statusResponse, authenticatedRequest(http.MethodGet, "/api/configs/9/direct-probe", ""))
	if statusResponse.Code != http.StatusOK || !strings.Contains(statusResponse.Body.String(), `"authorization_state":"authorized"`) {
		t.Fatalf("status=%d body=%s", statusResponse.Code, statusResponse.Body.String())
	}
	assertDirectProbeResponseRedacted(t, statusResponse.Body.String())
}

func newConsoleTestServer(t *testing.T, backend *fakeConsoleCore, managed *model.ManagedAccount, upstreams ...UpstreamConsole) *Server {
	t.Helper()
	stateStore, err := store.Open(filepath.Join(t.TempDir(), "state.json"))
	if err != nil {
		t.Fatal(err)
	}
	if managed != nil {
		if err := stateStore.Put(*managed); err != nil {
			t.Fatal(err)
		}
	}
	scheduler := engine.New(stateStore, backend, 1, nil)
	var upstreamConsole UpstreamConsole
	if len(upstreams) > 0 {
		upstreamConsole = upstreams[0]
	}
	return NewServer(scheduler, backend, Options{AuthCacheTTL: time.Second, Upstreams: upstreamConsole}, nil)
}

func newDirectProbeTestServer(t *testing.T, backend *fakeConsoleCore, managed ...model.ManagedAccount) *Server {
	t.Helper()
	stateStore, err := store.Open(filepath.Join(t.TempDir(), "state.json"))
	if err != nil {
		t.Fatal(err)
	}
	for _, config := range managed {
		if err := stateStore.Put(config); err != nil {
			t.Fatal(err)
		}
	}
	box, err := upstream.NewCredentialBox(strings.Repeat("11", 32))
	if err != nil {
		t.Fatal(err)
	}
	scheduler := engine.New(stateStore, backend, 1, nil, engine.WithDirectProbeCredentials(box))
	return NewServer(scheduler, backend, Options{AuthCacheTTL: time.Second}, nil)
}

func directProbeTestAccount(accountID int64) model.UpstreamAccount {
	return model.UpstreamAccount{
		ID:          accountID,
		Name:        "direct-key",
		Platform:    "openai",
		Type:        "apikey",
		Status:      "active",
		Schedulable: true,
		Credentials: map[string]any{"base_url": "https://relay.example/v1"},
	}
}

func directProbeManagedAccount(account model.UpstreamAccount) model.ManagedAccount {
	now := time.Now().UTC()
	return model.ManagedAccount{
		AccountID:     account.ID,
		Name:          account.Name,
		Platform:      account.Platform,
		AccountStatus: account.Status,
		Schedulable:   account.Schedulable,
		Policy:        model.DefaultPolicy(),
		History:       []model.CheckResult{},
		CreatedAt:     now,
		UpdatedAt:     now,
	}
}

func assertDirectProbeResponseRedacted(t *testing.T, body string) {
	t.Helper()
	for _, forbidden := range []string{"direct-api-secret", `"credential"`, `"routing_fingerprint"`, `"api_key"`, `"base_url"`} {
		if strings.Contains(body, forbidden) {
			t.Fatalf("direct-probe response exposed %q: %s", forbidden, body)
		}
	}
}

func float64Pointer(value float64) *float64 { return &value }

func authenticatedRequest(method, path, body string) *http.Request {
	request := httptest.NewRequest(method, path, strings.NewReader(body))
	request.Header.Set("Authorization", "Bearer valid")
	if body != "" {
		request.Header.Set("Content-Type", "application/json")
	}
	return request
}
