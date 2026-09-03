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
	accounts              []model.UpstreamAccount
	groups                []model.UpstreamGroup
	today                 map[string]model.WindowStats
	onlineUsersSummary    model.OnlineUsersSummary
	onlineUsers           model.OnlineUsersSnapshot
	groupConsumption      model.GroupUserConsumptionSnapshot
	groupConsumptionErr   error
	usage                 model.AccountUsageInfo
	consoleErr            error
	onlineUsersErr        error
	onlineUsersSummaryErr error
	usageErr              error
	cacheFallback         func(context.Context, []int64, map[string]model.WindowStats)
	cacheFallbackCalls    int
	bindingErr            error
	bindingErrByID        map[int64]error
	accountErr            error
	setErr                error
	overviewCalls         int
	groupCalls            int
	getAccountCalls       int
	setCalls              []bool
	onlineCalls           int
	onlineSummaryCalls    int
	groupConsumptionCalls int
	boundAccountID        int64
	boundGroupID          int64
	bound                 bool
	bindingCalls          []groupBindingCall
	exportErr             error
	exportErrByID         map[int64]error
	exportCalls           []int64
	exportToken           string
	exportIdentity        core.ForwardedIdentity
	testStarted           chan struct{}
	testRelease           chan struct{}
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

func (f *fakeConsoleCore) EnrichTodayAccountCacheStats(ctx context.Context, accountIDs []int64, stats map[string]model.WindowStats) {
	f.cacheFallbackCalls++
	if f.cacheFallback != nil {
		f.cacheFallback(ctx, accountIDs, stats)
	}
}

func (f *fakeConsoleCore) GetOnlineUsers(context.Context) (model.OnlineUsersSnapshot, error) {
	f.onlineCalls++
	return f.onlineUsers, f.onlineUsersErr
}

func (f *fakeConsoleCore) GetOnlineUsersSummary(context.Context) (model.OnlineUsersSummary, error) {
	f.onlineSummaryCalls++
	return f.onlineUsersSummary, f.onlineUsersSummaryErr
}

func (f *fakeConsoleCore) GetGroupUserConsumption(_ context.Context, _ int64) (model.GroupUserConsumptionSnapshot, error) {
	f.groupConsumptionCalls++
	return f.groupConsumption, f.groupConsumptionErr
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
	if f.testStarted != nil {
		f.testStarted <- struct{}{}
	}
	if f.testRelease != nil {
		<-f.testRelease
	}
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
	if !strings.Contains(response.Body.String(), `class="binding-list-header"`) ||
		!strings.Contains(response.Body.String(), "最终倍率") ||
		!strings.Contains(response.Body.String(), "可用余额") ||
		!strings.Contains(response.Body.String(), `id="metric-active-traffic"`) ||
		!strings.Contains(response.Body.String(), "真实成功率来自实际用户调用，不会主动探测") ||
		!strings.Contains(response.Body.String(), "这里仅保留余额告警设置") ||
		!strings.Contains(response.Body.String(), `id="online-users-metric"`) ||
		!strings.Contains(response.Body.String(), `id="online-users-dialog"`) ||
		!strings.Contains(response.Body.String(), "最近 10 分钟内有调用的用户") ||
		!strings.Contains(response.Body.String(), `id="scheduling-action-dialog"`) ||
		!strings.Contains(response.Body.String(), `id="scheduling-action-confirm-button"`) ||
		!strings.Contains(response.Body.String(), `id="icon-more-horizontal"`) {
		t.Fatal("binding dialog is missing metric column headings")
	}
	for _, removed := range []string{`id="add-button"`, `id="delete-dialog"`, `id="result-dialog"`, "新增检测", "删除检测配置", "最近 50 次检测状态"} {
		if strings.Contains(response.Body.String(), removed) {
			t.Fatalf("static page still exposes removed active-probe interaction %q", removed)
		}
	}
	if !strings.Contains(response.Body.String(), `id="upstream-connect-form" method="dialog" class="modal-panel" autocomplete="off"`) ||
		!strings.Contains(response.Body.String(), `id="upstream-password-input" type="password" autocomplete="new-password"`) ||
		!strings.Contains(response.Body.String(), `id="upstream-management-site-input" type="url"`) ||
		!strings.Contains(response.Body.String(), `id="upstream-captcha-fields"`) ||
		!strings.Contains(response.Body.String(), `id="upstream-captcha-image"`) ||
		!strings.Contains(response.Body.String(), `id="upstream-captcha-code-input"`) ||
		!strings.Contains(response.Body.String(), `id="upstream-captcha-refresh-button"`) ||
		!strings.Contains(response.Body.String(), "登录、身份验证、余额、Key 和倍率同步均使用此地址；留空时使用 API 地址。模型调用和本地账号关联始终保留 API 地址。") {
		t.Fatal("upstream login form must not reuse the current admin site's saved password")
	}
}

func TestGroupsAppUsesFinalMultiplierAndPassiveActualSuccess(t *testing.T) {
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
	for _, forbidden := range []string{
		"renderDetectedRate", "探测倍率", "promptInput", "automation-toggle", "renderHistory",
		"runNow", "direct-probe", "状态检测消耗（1 倍率）", "检测自动停止",
		"api_key:", "proxy_password:", "admin_jwt:",
	} {
		if strings.Contains(body, forbidden) {
			t.Fatalf("groups app still contains removed active-probe behavior %q", forbidden)
		}
	}
	for _, required := range []string{
		"actualSuccessByKey", "actual_success", "renderActualSuccess", "reference_24h",
		"effective_attempts", "暂无真实调用", "样本较少", "data-tooltip",
		"/api/configs/${accountID}/balance-alert", "账号余额告警设置已保存",
		"group_protections", "logical_group_ids", "设置保护倍率", "解除倍率保护", "移除绑定",
		"group_protection_defaults", "设置分组保护", "group-protection-dialog", "分组默认保护倍率",
		"binding-action-dialog", "protection-dialog", "admin_balance", "admin-balance-card",
		"renderBindingMultiplier", "renderBindingBalance", "最终倍率", "可用余额",
		"上游余额按当前分组倍率折算后的同步投影", "不限额度", "暂不可用",
		"groupBalanceSummaries", "group_balance_summaries", "renderGroupBalanceSummary", "启用余额", "未启用余额",
		"schedulableBusy", "schedulable-toggle", "schedulableState", "账号调度（全局）", "openSchedulingActionDialog",
		"expandedAccounts", "toggle-account-details", "api-key-details", "account-action-menu",
		"renderCompactBalance", "renderActualSuccessCompact", "renderCompactMultiplier",
		"`/api/accounts/${accountID}/schedulable`", "实际成功率不会自动启用或停止账号", "影响该账号所在的所有分组",
		"`/api/groups/${groupID}/accounts/${accountID}/protection`",
		"`/api/groups/${groupID}/accounts/${accountID}/binding`",
		"`/api/groups/${groupID}/protection-default`",
	} {
		if !strings.Contains(body, required) {
			t.Fatalf("groups app is missing protection UI behavior %q", required)
		}
	}

	styleResponse := httptest.NewRecorder()
	server.Handler().ServeHTTP(styleResponse, httptest.NewRequest(http.MethodGet, "/app.css", nil))
	style := styleResponse.Body.String()
	for _, required := range []string{
		".multiplier-pair", ".protection-multiplier.exceeded", ".binding-row-actions",
		".group-protection-badge", ".modal-footer-spacer",
		".admin-balance-card.insufficient", ".admin-balance-value",
		".actual-success", ".actual-success.warning", ".actual-success.danger", ".actual-success.low-sample",
		".actual-success.empty", ".actual-success.partial", ".actual-success-state", ".passive-health-note",
		".binding-list-header", ".binding-metric", ".binding-metric.insufficient", ".binding-metric-label",
		".group-balance-summary", ".group-balance-item.enabled", ".group-balance-item.disabled",
		".schedulable-control", ".schedulable-control.busy", "@media (max-width: 420px)",
		".api-key-record", ".api-key-details", ".compact-balance", ".compact-success",
		".account-action-menu", ".account-action-popover", ".button.danger-outline",
		"@media (max-width: 620px)", ".multiplier-pair { grid-template-columns: minmax(0, 1fr); }",
		".quota-list > div { grid-template-columns: 58px minmax(0, 1fr); }",
	} {
		if !strings.Contains(style, required) {
			t.Fatalf("groups stylesheet is missing protection responsive rule %q", required)
		}
	}
}

func TestGroupsAppUsesSameOriginSessionRefreshAndLoginRecovery(t *testing.T) {
	server := NewServer(nil, &fakeAdminCore{}, Options{}, nil)
	response := httptest.NewRecorder()
	server.Handler().ServeHTTP(response, httptest.NewRequest(http.MethodGet, "/app.js", nil))
	if response.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", response.Code, response.Body.String())
	}
	body := response.Body.String()
	for _, required := range []string{
		"AUTH_TOKEN_KEY", "AUTH_REFRESH_TOKEN_KEY", "readStoredAccessToken", "readStorageValue(AUTH_TOKEN_KEY)",
		"window.addEventListener('storage', handleAuthStorageChange)", "AUTH_REFRESH_LOCK_NAME", "navigator.locks?.request",
		"/api/v1/auth/refresh", "refresh_token", "refreshAccessToken(true, token)",
		"SIDECAR_RETURN_PATH", "window.top.location.assign(target)", "/login?redirect=",
		"sessionStorage.removeItem('sub2api-auto-scheduler-token')",
	} {
		if !strings.Contains(body, required) {
			t.Fatalf("groups app is missing same-origin session behavior %q", required)
		}
	}
	for _, forbidden := range []string{
		"sessionStorage.setItem(TOKEN_KEY",
		"const TOKEN_KEY = 'sub2api-auto-scheduler-token'",
		"state.token = queryToken",
	} {
		if strings.Contains(body, forbidden) {
			t.Fatalf("groups app still uses legacy URL/session token behavior %q", forbidden)
		}
	}
}

func TestProjectAdminBalanceUsesManagedMetadataAndConfiguredQuotaDimensions(t *testing.T) {
	managedRemaining := 100.0
	managedLimit := 100.0
	managedUsed := 20.0
	managed := projectAdminBalance(model.UpstreamAccount{
		QuotaLimit: &managedLimit,
		QuotaUsed:  &managedUsed,
		Extra: map[string]any{
			model.UpstreamBalanceQuotaManagedExtraKey:   true,
			model.UpstreamBalanceQuotaRemainingExtraKey: managedRemaining,
		},
	})
	if !managed.Configured || !managed.Managed || managed.Insufficient || managed.Remaining == nil || *managed.Remaining != 80 {
		t.Fatalf("managed balance projection = %#v, want remaining administrator quota 80", managed)
	}

	sentinel := 1e-9
	zero := 0.0
	exhausted := projectAdminBalance(model.UpstreamAccount{
		QuotaLimit: &sentinel,
		QuotaUsed:  &sentinel,
		Extra: map[string]any{
			model.UpstreamBalanceQuotaManagedExtraKey:   true,
			model.UpstreamBalanceQuotaRemainingExtraKey: zero,
			model.UpstreamBalanceQuotaExhaustedExtraKey: true,
		},
	})
	if !exhausted.Managed || !exhausted.Insufficient || exhausted.Remaining == nil || *exhausted.Remaining != 0 || !containsString(exhausted.ExhaustedDimensions, "total") {
		t.Fatalf("managed exhausted projection = %#v", exhausted)
	}

	dailyLimit := 5.0
	dailyUsed := 5.0
	ordinary := projectAdminBalance(model.UpstreamAccount{QuotaDailyLimit: &dailyLimit, QuotaDailyUsed: &dailyUsed})
	if !ordinary.Configured || ordinary.Managed || !ordinary.Insufficient || ordinary.Remaining != nil || !containsString(ordinary.ExhaustedDimensions, "daily") {
		t.Fatalf("ordinary exhausted projection = %#v", ordinary)
	}

	unconfigured := projectAdminBalance(model.UpstreamAccount{})
	if unconfigured.Configured || unconfigured.Managed || unconfigured.Unlimited || unconfigured.Insufficient || unconfigured.Remaining != nil || len(unconfigured.ExhaustedDimensions) != 0 {
		t.Fatalf("unconfigured quota was misclassified: %#v", unconfigured)
	}
}

func TestProjectGroupBalanceSummariesSeparatesEnabledAndDisabledAccounts(t *testing.T) {
	enabledLimit, enabledUsed := 10.0, 2.0
	disabledLimit, disabledUsed := 5.0, 1.0
	dailyLimit, dailyUsed := 1.0, 1.0
	enabledUnlimited := model.UpstreamAccount{
		ID: 2, Status: "active", Schedulable: true, GroupIDs: []int64{7},
		Extra: map[string]any{
			model.UpstreamBalanceQuotaManagedExtraKey:   true,
			model.UpstreamBalanceQuotaUnlimitedExtraKey: true,
		},
	}
	accounts := []overviewAccount{
		{
			UpstreamAccount: model.UpstreamAccount{ID: 1, Status: "active", Schedulable: true, GroupIDs: []int64{7}, QuotaLimit: &enabledLimit, QuotaUsed: &enabledUsed},
			AdminBalance:    projectAdminBalance(model.UpstreamAccount{QuotaLimit: &enabledLimit, QuotaUsed: &enabledUsed}),
		},
		{UpstreamAccount: enabledUnlimited, AdminBalance: projectAdminBalance(enabledUnlimited)},
		{
			UpstreamAccount: model.UpstreamAccount{ID: 3, Status: "active", Schedulable: true, GroupIDs: []int64{7}},
			AdminBalance:    projectAdminBalance(model.UpstreamAccount{}),
		},
		{
			UpstreamAccount: model.UpstreamAccount{ID: 4, Status: "active", Schedulable: false, GroupIDs: []int64{7}, QuotaLimit: &disabledLimit, QuotaUsed: &disabledUsed},
			AdminBalance:    projectAdminBalance(model.UpstreamAccount{QuotaLimit: &disabledLimit, QuotaUsed: &disabledUsed}),
		},
		{
			UpstreamAccount: model.UpstreamAccount{ID: 5, Status: "active", Schedulable: false, GroupIDs: []int64{7}, QuotaDailyLimit: &dailyLimit, QuotaDailyUsed: &dailyUsed},
			AdminBalance:    projectAdminBalance(model.UpstreamAccount{QuotaDailyLimit: &dailyLimit, QuotaDailyUsed: &dailyUsed}),
		},
	}
	summaries := projectGroupBalanceSummaries(accounts, nil, nil, time.Now().UTC())
	summary, ok := summaries["7"]
	if !ok {
		t.Fatalf("group balance summary missing: %#v", summaries)
	}
	if summary.Enabled.AccountCount != 3 || summary.Enabled.NumericCount != 1 || summary.Enabled.Remaining == nil || *summary.Enabled.Remaining != 8 || summary.Enabled.UnlimitedCount != 1 || summary.Enabled.UnavailableCount != 1 {
		t.Fatalf("enabled summary = %#v, want one numeric balance, one unlimited and one unavailable account", summary.Enabled)
	}
	if summary.Disabled.AccountCount != 2 || summary.Disabled.NumericCount != 1 || summary.Disabled.Remaining == nil || *summary.Disabled.Remaining != 4 || summary.Disabled.InsufficientCount != 1 {
		t.Fatalf("disabled summary = %#v, want one numeric and one insufficient account", summary.Disabled)
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
		"fetchConnectChallenge", "/login-challenges", "login_challenge_id", "captcha_code",
		"submittedChallenge", "refreshConnectChallenge", "LOGIN_CHALLENGE_EXPIRED", "upstreamCaptchaCodeInput",
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
		".upstream-captcha-panel", ".upstream-captcha-image-frame", ".upstream-captcha-content",
		".upstream-captcha-content { grid-template-columns: minmax(0, 1fr); }",
		".upstream-captcha-heading .button { min-height: 44px; }",
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
		GroupBalanceSummaries map[string]overviewGroupBalanceSummary `json:"group_balance_summaries"`
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
	if summary := payload.GroupBalanceSummaries["7"]; summary.Disabled.AccountCount != 1 || summary.Disabled.UnavailableCount != 1 {
		t.Fatalf("overview group balance summary is incorrect: %#v", payload.GroupBalanceSummaries)
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

func TestActiveProbeEndpointsAreGoneWithoutOutboundCalls(t *testing.T) {
	backend := &fakeConsoleCore{
		fakeAdminCore: fakeAdminCore{user: core.AdminUser{ID: 1, Role: "admin"}},
		accounts: []model.UpstreamAccount{{
			ID: 9, Name: "manual-key", Platform: "openai", Type: "apikey", Status: "active", Schedulable: false,
		}},
	}
	managed := directProbeManagedAccount(backend.accounts[0])
	managed.Policy.Enabled = true
	nextCheck := time.Now().UTC().Add(time.Minute)
	managed.NextCheckAt = &nextCheck
	server := newDirectProbeTestServer(t, backend, managed)

	unauthorized := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodPost, "/api/configs", strings.NewReader(`{"account_id":9,"enabled":true}`))
	request.Header.Set("Content-Type", "application/json")
	server.Handler().ServeHTTP(unauthorized, request)
	if unauthorized.Code != http.StatusUnauthorized {
		t.Fatalf("unauthorized status=%d body=%s", unauthorized.Code, unauthorized.Body.String())
	}

	tests := []struct {
		method string
		path   string
		body   string
	}{
		{method: http.MethodPost, path: "/api/configs", body: `{"account_id":9,"enabled":true}`},
		{method: http.MethodPut, path: "/api/configs/9", body: `{"enabled":true}`},
		{method: http.MethodDelete, path: "/api/configs/9"},
		{method: http.MethodPost, path: "/api/configs/9/run"},
		{method: http.MethodGet, path: "/api/configs/9/direct-probe"},
		{method: http.MethodPost, path: "/api/configs/9/direct-probe", body: `{}`},
		{method: http.MethodDelete, path: "/api/configs/9/direct-probe"},
		{method: http.MethodPost, path: "/api/direct-probes/authorize", body: `{"account_ids":[9]}`},
	}
	for _, test := range tests {
		response := httptest.NewRecorder()
		server.Handler().ServeHTTP(response, authenticatedRequest(test.method, test.path, test.body))
		if response.Code != http.StatusGone || !strings.Contains(response.Body.String(), `"code":"ACTIVE_PROBING_REMOVED"`) {
			t.Fatalf("%s %s status=%d body=%s", test.method, test.path, response.Code, response.Body.String())
		}
	}
	if backend.getAccountCalls != 0 || len(backend.setCalls) != 0 || len(backend.exportCalls) != 0 {
		t.Fatalf("removed endpoints reached outbound dependencies: get=%d set=%#v exports=%#v", backend.getAccountCalls, backend.setCalls, backend.exportCalls)
	}
	stored := server.engine.List()[0]
	if !stored.Policy.Enabled || stored.NextCheckAt == nil || stored.DirectProbe != nil {
		t.Fatalf("removed endpoints mutated dormant compatibility data: %#v", stored)
	}
}

func TestManualSchedulingEndpointRequiresAdminAndWorksWithoutDetectionConfig(t *testing.T) {
	backend := &fakeConsoleCore{
		fakeAdminCore: fakeAdminCore{user: core.AdminUser{ID: 1, Role: "admin"}},
		accounts: []model.UpstreamAccount{{
			ID: 9, Name: "manual-key", Platform: "openai", Type: "apikey", Status: "active", Schedulable: true,
		}},
	}
	server := newConsoleTestServer(t, backend, nil)

	unauthorized := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodPut, "/api/accounts/9/schedulable", strings.NewReader(`{"schedulable":false}`))
	request.Header.Set("Content-Type", "application/json")
	server.Handler().ServeHTTP(unauthorized, request)
	if unauthorized.Code != http.StatusUnauthorized || backend.getAccountCalls != 0 || len(backend.setCalls) != 0 {
		t.Fatalf("unauthorized update reached engine: status=%d get=%d set=%#v", unauthorized.Code, backend.getAccountCalls, backend.setCalls)
	}

	stopped := httptest.NewRecorder()
	server.Handler().ServeHTTP(stopped, authenticatedRequest(http.MethodPut, "/api/accounts/9/schedulable", `{"schedulable":false}`))
	if stopped.Code != http.StatusOK || backend.getAccountCalls != 1 || len(backend.setCalls) != 1 || backend.setCalls[0] {
		t.Fatalf("stop status=%d get=%d set=%#v body=%s", stopped.Code, backend.getAccountCalls, backend.setCalls, stopped.Body.String())
	}
	var payload struct {
		Account model.UpstreamAccount `json:"account"`
	}
	if err := json.NewDecoder(stopped.Body).Decode(&payload); err != nil {
		t.Fatal(err)
	}
	if payload.Account.Schedulable || len(server.engine.List()) != 0 {
		t.Fatalf("unexpected stop response or detection config creation: payload=%#v configs=%#v", payload, server.engine.List())
	}
}

func TestManualSchedulingEndpointClearsAutomaticSuspensionOwnership(t *testing.T) {
	backend := &fakeConsoleCore{
		fakeAdminCore: fakeAdminCore{user: core.AdminUser{ID: 1, Role: "admin"}},
		accounts: []model.UpstreamAccount{{
			ID: 9, Name: "auto-paused-key", Platform: "openai", Type: "apikey", Status: "active", Schedulable: false,
		}},
	}
	now := time.Now().UTC()
	managed := model.ManagedAccount{
		AccountID:            9,
		Name:                 "auto-paused-key",
		Platform:             "openai",
		AccountStatus:        "active",
		Schedulable:          false,
		Policy:               model.DefaultPolicy(),
		ManagedSuspended:     true,
		ConsecutiveFailures:  3,
		ConsecutiveSuccesses: 1,
		LastError:            "upstream failed",
		History:              []model.CheckResult{{ID: "previous", Status: model.CheckFailed, CheckedAt: now}},
		CreatedAt:            now,
		UpdatedAt:            now,
	}
	server := newConsoleTestServer(t, backend, &managed)
	response := httptest.NewRecorder()
	server.Handler().ServeHTTP(response, authenticatedRequest(http.MethodPut, "/api/accounts/9/schedulable", `{"schedulable":true}`))
	if response.Code != http.StatusOK || len(backend.setCalls) != 1 || !backend.setCalls[0] {
		t.Fatalf("status=%d set=%#v body=%s", response.Code, backend.setCalls, response.Body.String())
	}
	stored := server.engine.List()[0]
	if !stored.Schedulable || stored.ManagedSuspended || stored.ConsecutiveFailures != 0 || stored.ConsecutiveSuccesses != 0 || !stored.Policy.Enabled || len(stored.History) != 1 || stored.LastError != "upstream failed" {
		t.Fatalf("manual restore did not preserve detection state correctly: %#v", stored)
	}
}

func TestManualSchedulingEndpointValidatesRequestAndMapsFailures(t *testing.T) {
	tests := []struct {
		name       string
		body       string
		prepare    func(*fakeConsoleCore)
		wantStatus int
		wantCode   string
	}{
		{name: "missing schedulable", body: `{}`, wantStatus: http.StatusBadRequest, wantCode: "INVALID_REQUEST"},
		{name: "invalid schedulable type", body: `{"schedulable":"yes"}`, wantStatus: http.StatusBadRequest, wantCode: "INVALID_REQUEST"},
		{name: "unknown field", body: `{"schedulable":false,"force":true}`, wantStatus: http.StatusBadRequest, wantCode: "INVALID_REQUEST"},
		{
			name: "OAuth account",
			body: `{"schedulable":false}`,
			prepare: func(backend *fakeConsoleCore) {
				backend.accounts[0].Type = "oauth"
			},
			wantStatus: http.StatusBadRequest,
			wantCode:   "UNSUPPORTED_ACCOUNT",
		},
		{
			name: "inactive enable",
			body: `{"schedulable":true}`,
			prepare: func(backend *fakeConsoleCore) {
				backend.accounts[0].Status = "inactive"
				backend.accounts[0].Schedulable = false
			},
			wantStatus: http.StatusConflict,
			wantCode:   "ACCOUNT_NOT_ACTIVE",
		},
		{
			name: "Sub2API update failure",
			body: `{"schedulable":false}`,
			prepare: func(backend *fakeConsoleCore) {
				backend.setErr = errors.New("upstream unavailable")
			},
			wantStatus: http.StatusBadGateway,
			wantCode:   "SCHEDULING_UPDATE_FAILED",
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			backend := &fakeConsoleCore{
				fakeAdminCore: fakeAdminCore{user: core.AdminUser{ID: 1, Role: "admin"}},
				accounts: []model.UpstreamAccount{{
					ID: 9, Name: "key", Platform: "openai", Type: "apikey", Status: "active", Schedulable: true,
				}},
			}
			if test.prepare != nil {
				test.prepare(backend)
			}
			server := newConsoleTestServer(t, backend, nil)
			response := httptest.NewRecorder()
			server.Handler().ServeHTTP(response, authenticatedRequest(http.MethodPut, "/api/accounts/9/schedulable", test.body))
			if response.Code != test.wantStatus || !strings.Contains(response.Body.String(), `"code":"`+test.wantCode+`"`) {
				t.Fatalf("status=%d want=%d body=%s", response.Code, test.wantStatus, response.Body.String())
			}
		})
	}
}

func TestManualSchedulingEndpointConflictsWithRunningCheck(t *testing.T) {
	backend := &fakeConsoleCore{
		fakeAdminCore: fakeAdminCore{user: core.AdminUser{ID: 1, Role: "admin"}},
		accounts: []model.UpstreamAccount{{
			ID: 9, Name: "checking-key", Platform: "openai", Type: "apikey", Status: "active", Schedulable: true,
		}},
		testStarted: make(chan struct{}),
		testRelease: make(chan struct{}),
	}
	now := time.Now().UTC()
	managed := model.ManagedAccount{
		AccountID: 9, Name: "checking-key", Platform: "openai", AccountStatus: "active", Schedulable: true,
		Policy: model.DefaultPolicy(), History: []model.CheckResult{}, CreatedAt: now, UpdatedAt: now,
	}
	server := newConsoleTestServer(t, backend, &managed)
	ctx, cancel := context.WithCancel(context.Background())
	server.engine.Start(ctx)
	defer func() {
		close(backend.testRelease)
		cancel()
		server.engine.Stop()
	}()
	if err := server.engine.Trigger(9); err != nil {
		t.Fatal(err)
	}
	<-backend.testStarted

	response := httptest.NewRecorder()
	server.Handler().ServeHTTP(response, authenticatedRequest(http.MethodPut, "/api/accounts/9/schedulable", `{"schedulable":false}`))
	if response.Code != http.StatusConflict || !strings.Contains(response.Body.String(), `"code":"CHECK_RUNNING"`) || len(backend.setCalls) != 0 {
		t.Fatalf("status=%d set=%#v body=%s", response.Code, backend.setCalls, response.Body.String())
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

func TestRemovedDirectProbeAuthorizationDoesNotAttemptStepUpExport(t *testing.T) {
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

	if response.Code != http.StatusGone || !strings.Contains(response.Body.String(), `"code":"ACTIVE_PROBING_REMOVED"`) {
		t.Fatalf("status=%d body=%s", response.Code, response.Body.String())
	}
	if len(backend.exportCalls) != 0 {
		t.Fatalf("removed direct probe attempted export: %#v", backend.exportCalls)
	}
	managed := server.engine.List()[0]
	if managed.EffectiveProbeSource() != model.ProbeSourceLegacy || managed.DirectProbe != nil {
		t.Fatalf("failed step-up changed direct authorization: %#v", managed)
	}
}

func TestRemovedBatchDirectProbeAuthorizationDoesNotMutateStoredState(t *testing.T) {
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

	if response.Code != http.StatusGone || len(backend.exportCalls) != 0 || !strings.Contains(response.Body.String(), `"code":"ACTIVE_PROBING_REMOVED"`) {
		t.Fatalf("status=%d calls=%#v body=%s", response.Code, backend.exportCalls, response.Body.String())
	}
	for _, managed := range server.engine.List() {
		if managed.EffectiveProbeSource() != model.ProbeSourceLegacy || managed.DirectProbe != nil {
			t.Fatalf("removed batch authorization changed account %d: %#v", managed.AccountID, managed.DirectProbe)
		}
	}
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
