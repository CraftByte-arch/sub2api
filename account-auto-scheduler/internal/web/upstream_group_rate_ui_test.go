package web

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestUpstreamGroupRateUIShowsIdentityScopedRatesAndPlatformLabels(t *testing.T) {
	server := NewServer(nil, &fakeAdminCore{}, Options{}, nil)

	scriptResponse := httptest.NewRecorder()
	server.Handler().ServeHTTP(scriptResponse, httptest.NewRequest(http.MethodGet, "/upstreams.js", nil))
	if scriptResponse.Code != http.StatusOK {
		t.Fatalf("upstreams.js status=%d", scriptResponse.Code)
	}
	script := scriptResponse.Body.String()
	for _, expected := range []string{
		"renderIdentityGroups", "renderRemoteGroup", "formatRemoteGroupRate", "renderRemoteGroupPlatformBadge",
		"identity.groups || []", "当前登录身份同步到的全部可用分组", "分组倍率", "最终倍率", "快照状态",
		"OpenAI", "Anthropic", "Gemini", "GROK", "未知类型", "动态分组不设统一倍率", "请设置充值倍率",
		"group.final_multiplier", "groupCountFor", "formatInteger(groupCount)",
	} {
		if !strings.Contains(script, expected) {
			t.Fatalf("upstreams.js is missing group-rate behavior %q", expected)
		}
	}

	styleResponse := httptest.NewRecorder()
	server.Handler().ServeHTTP(styleResponse, httptest.NewRequest(http.MethodGet, "/app.css", nil))
	if styleResponse.Code != http.StatusOK {
		t.Fatalf("app.css status=%d", styleResponse.Code)
	}
	style := styleResponse.Body.String()
	for _, expected := range []string{
		".identity-group-section", ".identity-group-heading", ".remote-group-header, .remote-group-row",
		".remote-platform-badge.openai", ".remote-platform-badge.anthropic", ".remote-platform-badge.gemini", ".remote-platform-badge.grok",
		".remote-group-row { grid-template-columns: repeat(2, minmax(0, 1fr));", ".remote-group-row { grid-template-columns: minmax(0, 1fr);",
	} {
		if !strings.Contains(style, expected) {
			t.Fatalf("app.css is missing responsive group-rate style %q", expected)
		}
	}
}
