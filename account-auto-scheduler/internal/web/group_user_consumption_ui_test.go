package web

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestGroupUserConsumptionUIContract(t *testing.T) {
	server := NewServer(nil, &fakeAdminCore{}, Options{}, nil)

	page := httptest.NewRecorder()
	server.Handler().ServeHTTP(page, httptest.NewRequest(http.MethodGet, "/", nil))
	if page.Code != http.StatusOK {
		t.Fatalf("index status=%d", page.Code)
	}
	for _, required := range []string{
		`id="group-user-consumption-dialog"`,
		`id="group-user-consumption-retry-button"`,
		"请求 / Token",
	} {
		if !strings.Contains(page.Body.String(), required) {
			t.Fatalf("index is missing %q", required)
		}
	}

	app := httptest.NewRecorder()
	server.Handler().ServeHTTP(app, httptest.NewRequest(http.MethodGet, "/app.js", nil))
	if app.Code != http.StatusOK {
		t.Fatalf("app.js status=%d", app.Code)
	}
	for _, required := range []string{
		"今日用户 Top 20",
		"open-group-consumption",
		`aria-controls="group-user-consumption-dialog"`,
		"openGroupUserConsumptionDialog",
		"loadGroupUserConsumption",
		"renderGroupUserConsumption",
		"/api/groups/${groupID}/user-consumption",
		"当天暂无用户消耗记录",
		"部分字段暂不可用",
		"state.groupConsumptionTrigger",
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
		".group-consumption-summary",
		".group-consumption-row",
		".group-consumption-skeleton",
		".group-consumption-table-header { display: none; }",
	} {
		if !strings.Contains(style.Body.String(), required) {
			t.Fatalf("app.css is missing %q", required)
		}
	}
}
