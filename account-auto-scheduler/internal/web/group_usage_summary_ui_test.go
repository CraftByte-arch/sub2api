package web

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestGroupUsageSummaryUIContract(t *testing.T) {
	server := NewServer(nil, &fakeAdminCore{}, Options{}, nil)

	app := httptest.NewRecorder()
	server.Handler().ServeHTTP(app, httptest.NewRequest(http.MethodGet, "/app.js", nil))
	if app.Code != http.StatusOK {
		t.Fatalf("app.js status=%d", app.Code)
	}
	for _, required := range []string{
		"group_usage",
		"groupUsageByID",
		"groupUsageProjection",
		"renderGroupTodayUsage",
		"今日消耗",
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
		".group-today-usage",
		".group-today-usage.unavailable",
		".group-today-usage.stale",
	} {
		if !strings.Contains(style.Body.String(), required) {
			t.Fatalf("app.css is missing %q", required)
		}
	}
}
