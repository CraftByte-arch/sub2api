package web

import (
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api-account-auto-scheduler/internal/core"
	"github.com/Wei-Shaw/sub2api-account-auto-scheduler/internal/model"
)

func TestGroupUserConsumptionEndpointRequiresAdminAndReturnsNamedGroup(t *testing.T) {
	cost := 1.25
	requests := int64(4)
	tokens := int64(400)
	backend := &fakeConsoleCore{
		fakeAdminCore: fakeAdminCore{user: core.AdminUser{ID: 1, Role: "admin"}},
		groups:        []model.UpstreamGroup{{ID: 42, Name: "OpenAI", Platform: "openai"}},
		groupConsumption: model.GroupUserConsumptionSnapshot{
			GroupID:   42,
			Date:      "2026-08-29",
			Limit:     20,
			QueriedAt: time.Date(2026, 8, 29, 12, 0, 0, 0, time.UTC),
			Users: []model.GroupUserConsumptionRecord{{
				UserID: 7, DisplayName: "alice@example.com", ActualCost: &cost, Requests: &requests, TotalTokens: &tokens,
			}},
		},
	}
	server := NewServer(nil, backend, Options{AuthCacheTTL: time.Second}, nil)

	unauthorized := httptest.NewRecorder()
	server.Handler().ServeHTTP(unauthorized, httptest.NewRequest(http.MethodGet, "/api/groups/42/user-consumption", nil))
	if unauthorized.Code != http.StatusUnauthorized {
		t.Fatalf("unauthorized status = %d, want %d", unauthorized.Code, http.StatusUnauthorized)
	}
	if backend.groupConsumptionCalls != 0 {
		t.Fatalf("unauthorized request reached consumption client: %d", backend.groupConsumptionCalls)
	}

	request := authenticatedRequest(http.MethodGet, "/api/groups/42/user-consumption", "")
	response := httptest.NewRecorder()
	server.Handler().ServeHTTP(response, request)
	if response.Code != http.StatusOK {
		t.Fatalf("status = %d; body=%s", response.Code, response.Body.String())
	}
	var got model.GroupUserConsumptionSnapshot
	if err := json.NewDecoder(response.Body).Decode(&got); err != nil {
		t.Fatal(err)
	}
	if got.GroupID != 42 || got.GroupName != "OpenAI" || len(got.Users) != 1 || backend.groupConsumptionCalls != 1 {
		t.Fatalf("unexpected response: %#v calls=%d", got, backend.groupConsumptionCalls)
	}
}

func TestGroupUserConsumptionEndpointRejectsSyntheticOrUnknownGroupBeforeClientQuery(t *testing.T) {
	backend := &fakeConsoleCore{
		fakeAdminCore: fakeAdminCore{user: core.AdminUser{ID: 1, Role: "admin"}},
		groups:        []model.UpstreamGroup{{ID: 42, Name: "OpenAI"}},
	}
	server := NewServer(nil, backend, Options{AuthCacheTTL: time.Second}, nil)

	for _, path := range []string{"/api/groups/0/user-consumption", "/api/groups/999/user-consumption"} {
		response := httptest.NewRecorder()
		server.Handler().ServeHTTP(response, authenticatedRequest(http.MethodGet, path, ""))
		want := http.StatusNotFound
		if path == "/api/groups/0/user-consumption" {
			want = http.StatusBadRequest
		}
		if response.Code != want {
			t.Fatalf("%s status=%d, want %d; body=%s", path, response.Code, want, response.Body.String())
		}
	}
	if backend.groupConsumptionCalls != 0 {
		t.Fatalf("invalid group reached consumption client: %d", backend.groupConsumptionCalls)
	}
}

func TestGroupUserConsumptionEndpointReturnsRetryableUpstreamError(t *testing.T) {
	backend := &fakeConsoleCore{
		fakeAdminCore:       fakeAdminCore{user: core.AdminUser{ID: 1, Role: "admin"}},
		groups:              []model.UpstreamGroup{{ID: 42, Name: "OpenAI"}},
		groupConsumptionErr: errors.New("breakdown unavailable"),
	}
	server := NewServer(nil, backend, Options{AuthCacheTTL: time.Second}, nil)
	response := httptest.NewRecorder()
	server.Handler().ServeHTTP(response, authenticatedRequest(http.MethodGet, "/api/groups/42/user-consumption", ""))
	if response.Code != http.StatusBadGateway {
		t.Fatalf("status=%d, want %d; body=%s", response.Code, http.StatusBadGateway, response.Body.String())
	}
	var payload map[string]string
	if err := json.NewDecoder(response.Body).Decode(&payload); err != nil {
		t.Fatal(err)
	}
	if payload["code"] != "GROUP_USER_CONSUMPTION_UNAVAILABLE" || payload["message"] == "" {
		t.Fatalf("unexpected error payload: %#v", payload)
	}
}

func TestGroupUserConsumptionEndpointKeepsOverviewIndependent(t *testing.T) {
	backend := &fakeConsoleCore{
		fakeAdminCore:       fakeAdminCore{user: core.AdminUser{ID: 1, Role: "admin"}},
		groups:              []model.UpstreamGroup{{ID: 42, Name: "OpenAI"}},
		groupConsumptionErr: errors.New("temporarily unavailable"),
	}
	server := NewServer(nil, backend, Options{AuthCacheTTL: time.Second}, nil)
	response := httptest.NewRecorder()
	server.Handler().ServeHTTP(response, authenticatedRequest(http.MethodGet, "/api/groups/42/user-consumption", ""))
	if response.Code != http.StatusBadGateway || backend.overviewCalls != 0 {
		t.Fatalf("consumption failure affected overview dependencies: status=%d calls=%d", response.Code, backend.overviewCalls)
	}
}
