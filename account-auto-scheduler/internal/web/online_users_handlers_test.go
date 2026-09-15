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

func TestOnlineUsersEndpointsRequireAdminAndSeparateSummaryFromDetail(t *testing.T) {
	dataThrough := time.Date(2026, 8, 29, 11, 59, 0, 0, time.UTC)
	backend := &fakeConsoleCore{
		fakeAdminCore: fakeAdminCore{user: core.AdminUser{ID: 1, Role: "admin"}},
		onlineUsersSummary: model.OnlineUsersSummary{
			Ready:                 true,
			Count:                 1,
			WindowMinutes:         10,
			QueriedAt:             time.Date(2026, 8, 29, 12, 0, 0, 0, time.UTC),
			DataThrough:           &dataThrough,
			Source:                "aggregate_db",
			GroupCounts:           map[int64]int{42: 1},
			GroupCountsAvailable:  true,
			AggregationLagSeconds: 60,
		},
		onlineUsers: model.OnlineUsersSnapshot{
			Ready:                true,
			Count:                1,
			WindowMinutes:        10,
			QueriedAt:            time.Date(2026, 8, 29, 12, 0, 0, 0, time.UTC),
			DataThrough:          &dataThrough,
			Source:               "aggregate_db",
			GroupCounts:          map[int64]int{42: 1},
			GroupCountsAvailable: true,
			Users: []model.OnlineUser{{
				ID:          7,
				DisplayName: "alice",
				LastCallAt:  time.Date(2026, 8, 29, 11, 59, 0, 0, time.UTC),
			}},
		},
	}
	server := NewServer(nil, backend, Options{AuthCacheTTL: time.Second, OnlineUsers: backend}, nil)

	unauthorized := httptest.NewRecorder()
	server.Handler().ServeHTTP(unauthorized, httptest.NewRequest(http.MethodGet, "/api/online-users/summary", nil))
	if unauthorized.Code != http.StatusUnauthorized {
		t.Fatalf("unauthorized status = %d, want %d", unauthorized.Code, http.StatusUnauthorized)
	}
	if backend.onlineSummaryCalls != 0 || backend.onlineCalls != 0 {
		t.Fatalf("unauthorized request reached online service: summary=%d detail=%d", backend.onlineSummaryCalls, backend.onlineCalls)
	}

	summaryRequest := httptest.NewRequest(http.MethodGet, "/api/online-users/summary", nil)
	summaryRequest.Header.Set("Authorization", "Bearer valid")
	summaryResponse := httptest.NewRecorder()
	server.Handler().ServeHTTP(summaryResponse, summaryRequest)
	if summaryResponse.Code != http.StatusOK {
		t.Fatalf("summary status = %d; body=%s", summaryResponse.Code, summaryResponse.Body.String())
	}
	var summary model.OnlineUsersSummary
	if err := json.NewDecoder(summaryResponse.Body).Decode(&summary); err != nil {
		t.Fatal(err)
	}
	if !summary.Ready || summary.Source != "aggregate_db" || summary.Count != 1 || summary.GroupCounts[42] != 1 || backend.onlineSummaryCalls != 1 || backend.onlineCalls != 0 {
		t.Fatalf("unexpected summary response: %#v summary_calls=%d detail_calls=%d", summary, backend.onlineSummaryCalls, backend.onlineCalls)
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
	if !got.Ready || got.Source != "aggregate_db" || got.Count != 1 || got.GroupCounts[42] != 1 || !got.GroupCountsAvailable || len(got.Users) != 1 || got.Users[0].DisplayName != "alice" || backend.onlineCalls != 1 || backend.onlineSummaryCalls != 1 {
		t.Fatalf("unexpected online response: %#v calls=%d", got, backend.onlineCalls)
	}
}

func TestOnlineUsersEndpointDoesNotAffectOverviewWhenUnavailable(t *testing.T) {
	backend := &fakeConsoleCore{
		fakeAdminCore:  fakeAdminCore{user: core.AdminUser{ID: 1, Role: "admin"}},
		onlineUsersErr: errors.New("online collection failed"),
	}
	server := NewServer(nil, backend, Options{AuthCacheTTL: time.Second, OnlineUsers: backend}, nil)
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

func TestOnlineUsersNeverFallsBackToCoreHTTPClient(t *testing.T) {
	backend := &fakeConsoleCore{
		fakeAdminCore:      fakeAdminCore{user: core.AdminUser{ID: 1, Role: "admin"}},
		onlineUsersSummary: model.OnlineUsersSummary{Ready: true, Count: 99},
	}
	server := NewServer(nil, backend, Options{AuthCacheTTL: time.Second}, nil)
	request := httptest.NewRequest(http.MethodGet, "/api/online-users/summary", nil)
	request.Header.Set("Authorization", "Bearer valid")
	response := httptest.NewRecorder()
	server.Handler().ServeHTTP(response, request)
	if response.Code != http.StatusServiceUnavailable {
		t.Fatalf("status = %d, want %d; body=%s", response.Code, http.StatusServiceUnavailable, response.Body.String())
	}
	if backend.onlineSummaryCalls != 0 || backend.onlineCalls != 0 {
		t.Fatalf("core HTTP client was used as aggregate fallback: summary=%d detail=%d", backend.onlineSummaryCalls, backend.onlineCalls)
	}
}
