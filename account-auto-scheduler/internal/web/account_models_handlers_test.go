package web

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/Wei-Shaw/sub2api-account-auto-scheduler/internal/core"
	"github.com/Wei-Shaw/sub2api-account-auto-scheduler/internal/model"
)

type fakeAccountModelsCore struct {
	fakeAdminCore
	models    []model.AccountModel
	err       error
	accountID int64
}

func (f *fakeAccountModelsCore) GetAvailableModels(_ context.Context, accountID int64) ([]model.AccountModel, error) {
	f.accountID = accountID
	return f.models, f.err
}

func TestAccountModelsRouteRequiresAdminAndReturnsRealModels(t *testing.T) {
	backend := &fakeAccountModelsCore{
		fakeAdminCore: fakeAdminCore{user: core.AdminUser{ID: 1, Role: "admin"}},
		models:        []model.AccountModel{{ID: "configured-model", DisplayName: "Configured Model"}},
	}
	request := authenticatedRequest(http.MethodGet, "/api/accounts/42/models", "")
	response := httptest.NewRecorder()
	NewServer(nil, backend, Options{}, nil).Handler().ServeHTTP(response, request)
	if response.Code != http.StatusOK || backend.accountID != 42 || response.Header().Get("Cache-Control") != "no-store" || !strings.Contains(response.Body.String(), "configured-model") {
		t.Fatalf("unexpected response: %d %s backend=%#v", response.Code, response.Body.String(), backend)
	}
}

func TestAccountModelsRouteSanitizesBackendFailure(t *testing.T) {
	backend := &fakeAccountModelsCore{fakeAdminCore: fakeAdminCore{user: core.AdminUser{ID: 1, Role: "admin"}}, err: errors.New("secret upstream detail")}
	response := httptest.NewRecorder()
	NewServer(nil, backend, Options{}, nil).Handler().ServeHTTP(response, authenticatedRequest(http.MethodGet, "/api/accounts/42/models", ""))
	if response.Code != http.StatusBadGateway || strings.Contains(response.Body.String(), "secret upstream detail") {
		t.Fatalf("backend detail leaked: %d %s", response.Code, response.Body.String())
	}
}
