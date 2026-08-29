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

func TestOnlineUsersEndpointRequiresAdminAndReturnsSnapshot(t *testing.T) {
	backend := &fakeConsoleCore{
		fakeAdminCore: fakeAdminCore{user: core.AdminUser{ID: 1, Role: "admin"}},
		onlineUsers: model.OnlineUsersSnapshot{
			Count:                1,
			WindowMinutes:        10,
			QueriedAt:            time.Date(2026, 8, 29, 12, 0, 0, 0, time.UTC),
			Source:               "ops",
			GroupCounts:          map[int64]int{42: 1},
			GroupCountsAvailable: true,
			Users: []model.OnlineUser{{
				ID:          7,
				DisplayName: "alice",
				LastCallAt:  time.Date(2026, 8, 29, 11, 59, 0, 0, time.UTC),
			}},
		},
	}
	server := NewServer(nil, backend, Options{AuthCacheTTL: time.Second}, nil)

	unauthorized := httptest.NewRecorder()
	server.Handler().ServeHTTP(unauthorized, httptest.NewRequest(http.MethodGet, "/api/online-users", nil))
	if unauthorized.Code != http.StatusUnauthorized {
		t.Fatalf("unauthorized status = %d, want %d", unauthorized.Code, http.StatusUnauthorized)
	}

	request := httptest.NewRequest(http.MethodGet, "/api/online-users", nil)
	request.Header.Set("Authorization", "Bearer valid")
	response := httptest.NewRecorder()
	server.Handler().ServeHTTP(response, request)
	if response.Code != http.StatusOK {
		t.Fatalf("status = %d; body=%s", response.Code, response.Body.String())
	}
	var got model.OnlineUsersSnapshot
	if err := json.NewDecoder(response.Body).Decode(&got); err != nil {
		t.Fatal(err)
	}
	if got.Count != 1 || got.GroupCounts[42] != 1 || !got.GroupCountsAvailable || len(got.Users) != 1 || got.Users[0].DisplayName != "alice" || backend.onlineCalls != 1 {
		t.Fatalf("unexpected online response: %#v calls=%d", got, backend.onlineCalls)
	}
}

func TestOnlineUsersEndpointDoesNotAffectOverviewWhenUnavailable(t *testing.T) {
	backend := &fakeConsoleCore{
		fakeAdminCore:  fakeAdminCore{user: core.AdminUser{ID: 1, Role: "admin"}},
		onlineUsersErr: errors.New("online collection failed"),
	}
	server := NewServer(nil, backend, Options{AuthCacheTTL: time.Second}, nil)
	request := httptest.NewRequest(http.MethodGet, "/api/online-users", nil)
	request.Header.Set("Authorization", "Bearer valid")
	response := httptest.NewRecorder()
	server.Handler().ServeHTTP(response, request)
	if response.Code != http.StatusBadGateway {
		t.Fatalf("status = %d, want %d", response.Code, http.StatusBadGateway)
	}

	// The route is independent from /api/overview; this assertion documents
	// that a failing online collection is not wired into overview's response.
	if backend.overviewCalls != 0 {
		t.Fatalf("online endpoint unexpectedly called overview dependencies: %d", backend.overviewCalls)
	}
}
