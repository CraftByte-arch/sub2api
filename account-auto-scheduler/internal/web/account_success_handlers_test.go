package web

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api-account-auto-scheduler/internal/core"
	"github.com/Wei-Shaw/sub2api-account-auto-scheduler/internal/engine"
	"github.com/Wei-Shaw/sub2api-account-auto-scheduler/internal/model"
	"github.com/Wei-Shaw/sub2api-account-auto-scheduler/internal/store"
)

type fakeAccountSuccessConsole struct {
	snapshot model.AccountSuccessSnapshot
	err      error
	calls    int
}

func (f *fakeAccountSuccessConsole) GetAccountSuccessRates(context.Context) (model.AccountSuccessSnapshot, error) {
	f.calls++
	return f.snapshot, f.err
}

type fakeAccountPerformanceHealthCore struct {
	*fakeConsoleCore
	health    model.AccountPerformanceCollectionHealth
	healthErr error
	calls     int
}

func (f *fakeAccountPerformanceHealthCore) GetAccountPerformanceHealth(context.Context) (model.AccountPerformanceCollectionHealth, error) {
	f.calls++
	return f.health, f.healthErr
}

func TestOverviewIncludesActualSuccessAndCollectionHealth(t *testing.T) {
	now := time.Date(2026, 9, 3, 8, 15, 0, 0, time.UTC)
	backend := &fakeConsoleCore{
		fakeAdminCore: fakeAdminCore{user: core.AdminUser{ID: 1, Role: "admin"}},
		groups:        []model.UpstreamGroup{{ID: 7, Name: "Primary", Platform: "openai"}},
		accounts:      []model.UpstreamAccount{{ID: 9, Name: "key", Platform: "openai", Type: "apikey", Status: "active", Schedulable: true, GroupIDs: []int64{7}}},
		today:         map[string]model.WindowStats{},
	}
	healthCore := &fakeAccountPerformanceHealthCore{
		fakeConsoleCore: backend,
		health:          model.AccountPerformanceCollectionHealth{Status: "degraded", DroppedSamples: 3, PendingSamples: 2},
	}
	through := now.Truncate(time.Minute)
	success := &fakeAccountSuccessConsole{snapshot: model.AccountSuccessSnapshot{
		Ready: true, QueriedAt: now, DataThrough: &through, Source: "account_performance_aggregate", ActiveAccountCount: 1,
		Items: []model.GroupAccountSuccessRate{{GroupID: 7, AccountID: 9, Recent: model.AccountSuccessWindow{SuccessCount: 18, EffectiveAttempts: 20, Rate: 0.9}}},
	}}
	server := newAccountSuccessOverviewServer(t, backend, healthCore, success)

	response := httptest.NewRecorder()
	server.Handler().ServeHTTP(response, authenticatedRequest(http.MethodGet, "/api/overview", ""))
	if response.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", response.Code, response.Body.String())
	}
	var payload struct {
		Accounts      []overviewAccount            `json:"accounts"`
		ActualSuccess model.AccountSuccessSnapshot `json:"actual_success"`
	}
	if err := json.NewDecoder(response.Body).Decode(&payload); err != nil {
		t.Fatal(err)
	}
	if len(payload.Accounts) != 1 || !payload.ActualSuccess.Ready || !payload.ActualSuccess.Partial || payload.ActualSuccess.ActiveAccountCount != 1 || len(payload.ActualSuccess.Items) != 1 {
		t.Fatalf("actual success missing from otherwise valid overview: %#v", payload)
	}
	if payload.ActualSuccess.CollectionHealth == nil || payload.ActualSuccess.CollectionHealth.DroppedSamples != 3 || !strings.Contains(payload.ActualSuccess.Notice, "采集已降级") {
		t.Fatalf("collection health was not projected: %#v", payload.ActualSuccess)
	}
	if success.calls != 1 || healthCore.calls != 1 {
		t.Fatalf("unexpected aggregate calls: success=%d health=%d", success.calls, healthCore.calls)
	}
}

func TestOverviewRemainsAvailableWhenActualSuccessSourceFails(t *testing.T) {
	backend := &fakeConsoleCore{
		fakeAdminCore: fakeAdminCore{user: core.AdminUser{ID: 1, Role: "admin"}},
		groups:        []model.UpstreamGroup{{ID: 7, Name: "Primary", Platform: "openai"}},
		accounts:      []model.UpstreamAccount{{ID: 9, Name: "key", Platform: "openai", Type: "apikey", Status: "active", Schedulable: true}},
		today:         map[string]model.WindowStats{},
	}
	healthCore := &fakeAccountPerformanceHealthCore{fakeConsoleCore: backend, healthErr: errors.New("health unavailable")}
	success := &fakeAccountSuccessConsole{err: errors.New("aggregate database unavailable")}
	server := newAccountSuccessOverviewServer(t, backend, healthCore, success)

	response := httptest.NewRecorder()
	server.Handler().ServeHTTP(response, authenticatedRequest(http.MethodGet, "/api/overview", ""))
	if response.Code != http.StatusOK {
		t.Fatalf("actual-success failure broke overview: status=%d body=%s", response.Code, response.Body.String())
	}
	var payload struct {
		Accounts      []overviewAccount            `json:"accounts"`
		ActualSuccess model.AccountSuccessSnapshot `json:"actual_success"`
	}
	if err := json.NewDecoder(response.Body).Decode(&payload); err != nil {
		t.Fatal(err)
	}
	if len(payload.Accounts) != 1 || payload.ActualSuccess.Ready || len(payload.ActualSuccess.Items) != 0 || payload.ActualSuccess.Notice == "" {
		t.Fatalf("isolated failure response is misleading: %#v", payload)
	}
}

func newAccountSuccessOverviewServer(t *testing.T, engineCore *fakeConsoleCore, console AdminCore, success AccountSuccessConsole) *Server {
	t.Helper()
	stateStore, err := store.Open(filepath.Join(t.TempDir(), "state.json"))
	if err != nil {
		t.Fatal(err)
	}
	scheduler := engine.New(stateStore, engineCore, 1, nil)
	return NewServer(scheduler, console, Options{AuthCacheTTL: time.Second, AccountSuccess: success}, nil)
}
