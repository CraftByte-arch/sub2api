package web

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestManualProbeUIUsesRealModelsAndExistingDialogSystem(t *testing.T) {
	server := NewServer(nil, &fakeAdminCore{}, Options{}, nil)

	page := httptest.NewRecorder()
	server.Handler().ServeHTTP(page, httptest.NewRequest(http.MethodGet, "/", nil))
	if page.Code != http.StatusOK {
		t.Fatalf("index status=%d", page.Code)
	}
	for _, required := range []string{
		`id="manual-probe-form"`, `class="modal-panel"`, `id="manual-probe-retry"`,
		`id="manual-probe-model-status"`, `aria-labelledby="manual-probe-title"`, `<use href="#icon-x"/>`,
	} {
		if !strings.Contains(page.Body.String(), required) {
			t.Fatalf("manual probe markup is missing %q", required)
		}
	}
	if strings.Contains(page.Body.String(), `class="modal-card"`) {
		t.Fatal("manual probe still uses the undefined modal-card component")
	}

	app := httptest.NewRecorder()
	server.Handler().ServeHTTP(app, httptest.NewRequest(http.MethodGet, "/app.js", nil))
	if app.Code != http.StatusOK {
		t.Fatalf("app.js status=%d", app.Code)
	}
	for _, required := range []string{
		"/api/accounts/${account.id}/models", "正在读取账号真实模型", "loadManualProbeModels",
		"setManualProbeBusy", "formatDurationMS", "body: { models, prompt:",
	} {
		if !strings.Contains(app.Body.String(), required) {
			t.Fatalf("manual probe script is missing %q", required)
		}
	}
	for _, forbidden := range []string{"'gpt-4o-mini'", "'claude-3-5-sonnet'", "body: JSON.stringify({ models"} {
		if strings.Contains(app.Body.String(), forbidden) {
			t.Fatalf("manual probe script still contains obsolete behavior %q", forbidden)
		}
	}
	menuStart := strings.Index(app.Body.String(), "function renderAccountActionMenu(")
	menuEnd := strings.Index(app.Body.String(), "function renderAccountDetailActions(")
	if menuStart < 0 || menuEnd <= menuStart {
		t.Fatal("account action menu function boundaries were not found")
	}
	menuScript := app.Body.String()[menuStart:menuEnd]
	if !strings.Contains(menuScript, `role="menuitem" data-action="manual-probe">手动探测</button>`) {
		t.Fatal("account overflow menu is missing the manual probe action")
	}

	style := httptest.NewRecorder()
	server.Handler().ServeHTTP(style, httptest.NewRequest(http.MethodGet, "/app.css", nil))
	if style.Code != http.StatusOK {
		t.Fatalf("app.css status=%d", style.Code)
	}
	for _, required := range []string{
		".manual-probe-model-list", "overscroll-behavior: contain", "var(--border)",
		"@media (prefers-reduced-motion: reduce)", ".manual-probe-result.error",
	} {
		if !strings.Contains(style.Body.String(), required) {
			t.Fatalf("manual probe styles are missing %q", required)
		}
	}
	if strings.Contains(style.Body.String(), "var(--border-color)") {
		t.Fatal("manual probe still uses undefined border-color token")
	}
}
