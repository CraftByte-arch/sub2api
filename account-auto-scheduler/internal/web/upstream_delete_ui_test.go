package web

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestUpstreamDeleteUIIsScopedToUnusedPersistedRowsAndRequiresConfirmation(t *testing.T) {
	server := NewServer(nil, &fakeAdminCore{}, Options{}, nil)

	scriptResponse := httptest.NewRecorder()
	server.Handler().ServeHTTP(scriptResponse, httptest.NewRequest(http.MethodGet, "/upstreams.js", nil))
	if scriptResponse.Code != http.StatusOK {
		t.Fatalf("upstreams.js status=%d", scriptResponse.Code)
	}
	script := scriptResponse.Body.String()
	for _, expected := range []string{
		"upstream.persisted === true && localAccounts.length === 0",
		"没有使用该地址的本地 API Key 账号",
		`data-upstream-action="delete-upstream"`,
		"openDeleteUpstream(upstream, button)",
		"elements.upstreamDeleteDialog.showModal()",
		"elements.upstreamDeleteButton.focus()",
		"elements.upstreamDeleteDialog.dataset.busy === 'true'",
		"method: 'DELETE'",
		"await load(true)",
		"error.code === 'UPSTREAM_IN_USE'",
		"restoreFocus('deleteUpstream')",
	} {
		if !strings.Contains(script, expected) {
			t.Fatalf("upstreams.js is missing guarded delete behavior %q", expected)
		}
	}

	pageResponse := httptest.NewRecorder()
	server.Handler().ServeHTTP(pageResponse, httptest.NewRequest(http.MethodGet, "/", nil))
	if pageResponse.Code != http.StatusOK {
		t.Fatalf("index status=%d", pageResponse.Code)
	}
	page := pageResponse.Body.String()
	for _, expected := range []string{
		`id="upstream-delete-dialog"`,
		`id="upstream-delete-name"`,
		`id="upstream-delete-button" class="button danger"`,
		"登录身份、加密登录材料、充值倍率、余额和 Key 快照",
		"不会删除或修改 Sub2API 账号",
		"确认删除",
	} {
		if !strings.Contains(page, expected) {
			t.Fatalf("index is missing delete confirmation behavior %q", expected)
		}
	}

	styleResponse := httptest.NewRecorder()
	server.Handler().ServeHTTP(styleResponse, httptest.NewRequest(http.MethodGet, "/app.css", nil))
	if styleResponse.Code != http.StatusOK || !strings.Contains(styleResponse.Body.String(), ".local-account-empty") {
		t.Fatalf("delete action layout styles missing: status=%d", styleResponse.Code)
	}
}
