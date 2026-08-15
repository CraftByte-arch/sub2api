package web

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestUpstreamWorkspaceReloadsOnActivationAndVisiblePoll(t *testing.T) {
	server := NewServer(nil, &fakeAdminCore{}, Options{}, nil)

	upstreamScriptResponse := httptest.NewRecorder()
	server.Handler().ServeHTTP(upstreamScriptResponse, httptest.NewRequest(http.MethodGet, "/upstreams.js", nil))
	if upstreamScriptResponse.Code != http.StatusOK {
		t.Fatalf("upstreams.js status=%d", upstreamScriptResponse.Code)
	}
	upstreamScript := upstreamScriptResponse.Body.String()
	for _, expected := range []string{
		"return load(state.loaded)",
		"refresh(silent = true)",
		"return load(silent)",
	} {
		if !strings.Contains(upstreamScript, expected) {
			t.Fatalf("upstreams.js is missing live refresh behavior %q", expected)
		}
	}

	appScriptResponse := httptest.NewRecorder()
	server.Handler().ServeHTTP(appScriptResponse, httptest.NewRequest(http.MethodGet, "/app.js", nil))
	if appScriptResponse.Code != http.StatusOK {
		t.Fatalf("app.js status=%d", appScriptResponse.Code)
	}
	appScript := appScriptResponse.Body.String()
	if !strings.Contains(appScript, "state.activeTab === 'upstreams'") ||
		!strings.Contains(appScript, "state.upstreamWorkspace.refresh(true)") {
		t.Fatal("app.js does not refresh upstream candidates while the tab is visible")
	}
}
