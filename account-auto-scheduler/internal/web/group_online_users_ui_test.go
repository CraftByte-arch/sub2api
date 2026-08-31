package web

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestGroupOnlineUsersUIContract(t *testing.T) {
	server := NewServer(nil, &fakeAdminCore{}, Options{}, nil)

	app := httptest.NewRecorder()
	server.Handler().ServeHTTP(app, httptest.NewRequest(http.MethodGet, "/app.js", nil))
	if app.Code != http.StatusOK {
		t.Fatalf("app.js status=%d", app.Code)
	}
	for _, required := range []string{
		"/api/online-users/summary",
		"await loadOnlineUsersSummary(true)",
		"}, 60000)",
		"group_counts_available",
		"aggregation_lag_seconds",
		"onlineAggregateFreshnessLabel",
		"data-group-online-count",
		"renderGroupOnlineCounts",
		"renderGroupOnlineBadge",
		"最近 10 分钟分组在线人数",
		"在线 ${projection.label}",
		"Object.prototype.hasOwnProperty.call",
	} {
		if !strings.Contains(app.Body.String(), required) {
			t.Fatalf("app.js is missing %q", required)
		}
	}

	style := httptest.NewRecorder()
	server.Handler().ServeHTTP(style, httptest.NewRequest(http.MethodGet, "/app.css", nil))
	if style.Code != http.StatusOK {
		t.Fatalf("app.css status=%d", style.Code)
	}
	for _, required := range []string{
		".group-online-users",
		".group-online-dot",
		".group-online-users.unavailable",
		".group-online-users.partial",
	} {
		if !strings.Contains(style.Body.String(), required) {
			t.Fatalf("app.css is missing %q", required)
		}
	}
}
