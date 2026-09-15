package web

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestUpstreamGroupDialogShowsAllIdentityScopedRatesAndBindings(t *testing.T) {
	server := NewServer(nil, &fakeAdminCore{}, Options{}, nil)

	scriptResponse := httptest.NewRecorder()
	server.Handler().ServeHTTP(scriptResponse, httptest.NewRequest(http.MethodGet, "/upstreams.js", nil))
	if scriptResponse.Code != http.StatusOK {
		t.Fatalf("upstreams.js status=%d", scriptResponse.Code)
	}
	script := scriptResponse.Body.String()
	for _, expected := range []string{
		"groupViewsFor", "openBoundGroups", "renderBoundGroups", "renderBoundGroupIdentity", "renderBoundGroup",
		"identity.groups || []", "data-upstream-action=\"bound-groups\"", "上游分组", "未绑定本地 API Key",
		"快照未匹配", "由已绑定 Key 推断", "formatRemoteGroupRate", "renderRemoteGroupPlatformBadge",
		"OpenAI", "Anthropic", "Gemini", "GROK", "未知类型", "动态分组不设统一倍率", "请设置充值倍率",
		"groupCountFor", "restoreFocus('boundGroups')",
	} {
		if !strings.Contains(script, expected) {
			t.Fatalf("upstreams.js is missing upstream-group dialog behavior %q", expected)
		}
	}
	for _, unexpected := range []string{"function renderIdentityGroups(", "function renderRemoteGroup(", "function boundGroupsFor("} {
		if strings.Contains(script, unexpected) {
			t.Fatalf("upstreams.js still contains removed inline group behavior %q", unexpected)
		}
	}

	pageResponse := httptest.NewRecorder()
	server.Handler().ServeHTTP(pageResponse, httptest.NewRequest(http.MethodGet, "/", nil))
	if pageResponse.Code != http.StatusOK {
		t.Fatalf("index status=%d", pageResponse.Code)
	}
	for _, expected := range []string{
		"upstream-bound-groups-dialog", "upstream-bound-groups-title", "upstream-bound-groups-count",
		"upstream-bound-groups-list", "已绑定项会显示上游 Key 与本地 API Key",
	} {
		if !strings.Contains(pageResponse.Body.String(), expected) {
			t.Fatalf("index.html is missing upstream-group dialog markup %q", expected)
		}
	}

	styleResponse := httptest.NewRecorder()
	server.Handler().ServeHTTP(styleResponse, httptest.NewRequest(http.MethodGet, "/app.css", nil))
	if styleResponse.Code != http.StatusOK {
		t.Fatalf("app.css status=%d", styleResponse.Code)
	}
	style := styleResponse.Body.String()
	for _, expected := range []string{
		".bound-groups-summary", ".bound-groups-list", ".bound-group-identity", ".bound-group-table-header, .bound-group-row",
		".remote-platform-badge.openai", ".remote-platform-badge.anthropic", ".remote-platform-badge.gemini", ".remote-platform-badge.grok",
		".bound-group-row { grid-template-columns: repeat(2, minmax(0, 1fr));", ".bound-group-row { grid-template-columns: minmax(0, 1fr);",
		"html:has(dialog[open]), body:has(dialog[open])", "overscroll-behavior-y: none", "overscroll-behavior-y: contain", "touch-action: pan-y",
	} {
		if !strings.Contains(style, expected) {
			t.Fatalf("app.css is missing responsive upstream-group dialog style %q", expected)
		}
	}
	for _, unexpected := range []string{".identity-group-section", ".remote-group-header, .remote-group-row"} {
		if strings.Contains(style, unexpected) {
			t.Fatalf("app.css still contains removed inline group style %q", unexpected)
		}
	}
}
