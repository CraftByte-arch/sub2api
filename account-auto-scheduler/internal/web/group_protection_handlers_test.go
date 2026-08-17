package web

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api-account-auto-scheduler/internal/core"
	"github.com/Wei-Shaw/sub2api-account-auto-scheduler/internal/engine"
	"github.com/Wei-Shaw/sub2api-account-auto-scheduler/internal/model"
	"github.com/Wei-Shaw/sub2api-account-auto-scheduler/internal/store"
	"github.com/Wei-Shaw/sub2api-account-auto-scheduler/internal/upstream"
)

type fakeProtectedUpstreamConsole struct {
	*fakeUpstreamConsole
	protections    map[int64]map[string]upstream.GroupAccountProtectionView
	logical        map[int64][]int64
	setCalls       []protectionHandlerCall
	releaseCalls   []protectionHandlerCall
	removeCalls    []protectionHandlerCall
	singleCalls    []singleBindingHandlerCall
	bulkGroupID    int64
	bulkSelected   []int64
	bulkUpdated    []int64
	bulkFailures   []upstream.GroupBindingFailure
	defaults       map[int64]upstream.GroupProtectionDefaultView
	defaultSets    []protectionHandlerCall
	defaultGets    []int64
	defaultDeletes []int64
}

type protectionHandlerCall struct {
	groupID    int64
	accountID  int64
	multiplier float64
}

type singleBindingHandlerCall struct {
	groupID   int64
	accountID int64
	bound     bool
}

func (f *fakeProtectedUpstreamConsole) GroupAccountProtectionViews([]model.UpstreamAccount) map[int64]map[string]upstream.GroupAccountProtectionView {
	return f.protections
}

func (f *fakeProtectedUpstreamConsole) LogicalGroupIDs([]model.UpstreamAccount) map[int64][]int64 {
	return f.logical
}

func (f *fakeProtectedUpstreamConsole) GroupProtectionDefaults() map[int64]upstream.GroupProtectionDefaultView {
	return f.defaults
}

func (f *fakeProtectedUpstreamConsole) GetGroupProtectionDefault(groupID int64) (upstream.GroupProtectionDefaultView, error) {
	f.defaultGets = append(f.defaultGets, groupID)
	if value, ok := f.defaults[groupID]; ok {
		return value, nil
	}
	return upstream.GroupProtectionDefaultView{}, store.ErrGroupProtectionDefaultNotFound
}

func (f *fakeProtectedUpstreamConsole) SetGroupProtectionDefault(_ context.Context, groupID int64, multiplier float64) (upstream.GroupProtectionDefaultView, error) {
	f.defaultSets = append(f.defaultSets, protectionHandlerCall{groupID: groupID, multiplier: multiplier})
	if f.defaults == nil {
		f.defaults = map[int64]upstream.GroupProtectionDefaultView{}
	}
	view := upstream.GroupProtectionDefaultView{GroupID: groupID, ProtectionMultiplier: multiplier}
	f.defaults[groupID] = view
	return view, nil
}

func (f *fakeProtectedUpstreamConsole) DeleteGroupProtectionDefault(_ context.Context, groupID int64) error {
	f.defaultDeletes = append(f.defaultDeletes, groupID)
	delete(f.defaults, groupID)
	return nil
}

func (f *fakeProtectedUpstreamConsole) SaveGroupBindings(_ context.Context, groupID int64, selected []int64) ([]int64, []upstream.GroupBindingFailure, error) {
	f.bulkGroupID = groupID
	f.bulkSelected = append([]int64(nil), selected...)
	return append([]int64(nil), f.bulkUpdated...), append([]upstream.GroupBindingFailure(nil), f.bulkFailures...), nil
}

func (f *fakeProtectedUpstreamConsole) SetGroupAccountProtection(_ context.Context, groupID, accountID int64, multiplier float64) (upstream.GroupAccountProtectionView, error) {
	f.setCalls = append(f.setCalls, protectionHandlerCall{groupID: groupID, accountID: accountID, multiplier: multiplier})
	return upstream.GroupAccountProtectionView{GroupID: groupID, AccountID: accountID, ProtectionMultiplier: multiplier, Status: model.ProtectionBound, PhysicalBound: true}, nil
}

func (f *fakeProtectedUpstreamConsole) ReleaseGroupAccountProtection(_ context.Context, groupID, accountID int64) (upstream.GroupAccountProtectionView, error) {
	f.releaseCalls = append(f.releaseCalls, protectionHandlerCall{groupID: groupID, accountID: accountID})
	return upstream.GroupAccountProtectionView{GroupID: groupID, AccountID: accountID, Status: model.ProtectionBound, PhysicalBound: true}, nil
}

func (f *fakeProtectedUpstreamConsole) RemoveGroupAccountBinding(_ context.Context, groupID, accountID int64) error {
	f.removeCalls = append(f.removeCalls, protectionHandlerCall{groupID: groupID, accountID: accountID})
	return nil
}

func (f *fakeProtectedUpstreamConsole) SetGroupAccountBinding(_ context.Context, groupID, accountID int64, bound bool) error {
	f.singleCalls = append(f.singleCalls, singleBindingHandlerCall{groupID: groupID, accountID: accountID, bound: bound})
	return nil
}

func TestOverviewMergesProtectedLogicalMembershipAndDetectionStats(t *testing.T) {
	coreClient := &fakeConsoleCore{
		fakeAdminCore: fakeAdminCore{user: core.AdminUser{ID: 1, Role: "admin"}},
		groups:        []model.UpstreamGroup{{ID: 11, Name: "Protected", Platform: "openai", Status: "active"}},
		accounts: []model.UpstreamAccount{{
			ID: 7, Name: "key", Type: "apikey", Platform: "openai", Status: "active", Schedulable: true,
			Credentials: map[string]any{"base_url": "https://example.com/v1"},
		}},
		today: map[string]model.WindowStats{},
	}
	stateStore, err := store.Open(filepath.Join(t.TempDir(), "state.json"))
	if err != nil {
		t.Fatal(err)
	}
	now := time.Now().UTC()
	if err := stateStore.Put(model.ManagedAccount{
		AccountID: 7, Name: "key", Platform: "openai", AccountStatus: "active", Schedulable: true,
		Policy: model.DefaultPolicy(), History: []model.CheckResult{},
		DetectionStats: model.DetectionStats{Requests: 3, InputTokens: 30, OutputTokens: 6, KnownCost: 0.012, KnownCostChecks: 2},
		CreatedAt:      now, UpdatedAt: now,
	}); err != nil {
		t.Fatal(err)
	}
	final := 0.2
	protected := &fakeProtectedUpstreamConsole{
		fakeUpstreamConsole: &fakeUpstreamConsole{finalMultipliers: map[int64]upstream.LocalAccountFinalMultiplier{7: {Status: "available", FinalMultiplier: &final}}},
		logical:             map[int64][]int64{7: {11}},
		defaults:            map[int64]upstream.GroupProtectionDefaultView{11: {GroupID: 11, ProtectionMultiplier: 0.16}},
		protections: map[int64]map[string]upstream.GroupAccountProtectionView{
			7: {"11": {GroupID: 11, AccountID: 7, ProtectionMultiplier: 0.16, Scope: model.ProtectionScopeGroup, Inherited: true, FinalMultiplier: &final, Status: model.ProtectionExceeded, PhysicalBound: false}},
		},
	}
	server := NewServer(engine.New(stateStore, coreClient, 1, nil), coreClient, Options{Upstreams: protected}, nil)
	request := httptest.NewRequest(http.MethodGet, "/api/overview", nil)
	request.Header.Set("Authorization", "Bearer valid")
	response := httptest.NewRecorder()
	server.Handler().ServeHTTP(response, request)
	if response.Code != http.StatusOK {
		t.Fatalf("overview status=%d body=%s", response.Code, response.Body.String())
	}
	var payload struct {
		GroupProtectionDefaults map[string]upstream.GroupProtectionDefaultView `json:"group_protection_defaults"`
		Accounts                []struct {
			LogicalGroupIDs  []int64                                        `json:"logical_group_ids"`
			GroupProtections map[string]upstream.GroupAccountProtectionView `json:"group_protections"`
			Config           model.ManagedAccountView                       `json:"config"`
		} `json:"accounts"`
	}
	if err := json.Unmarshal(response.Body.Bytes(), &payload); err != nil {
		t.Fatal(err)
	}
	if len(payload.Accounts) != 1 || payload.GroupProtectionDefaults["11"].ProtectionMultiplier != 0.16 || !reflect.DeepEqual(payload.Accounts[0].LogicalGroupIDs, []int64{11}) || !payload.Accounts[0].GroupProtections["11"].Inherited || payload.Accounts[0].GroupProtections["11"].Status != model.ProtectionExceeded || payload.Accounts[0].Config.DetectionStats.Requests != 3 {
		t.Fatalf("protected overview projection missing: %#v", payload)
	}
}

func TestProtectionMutationRoutesRequireAdminAndForwardActions(t *testing.T) {
	coreClient := &fakeConsoleCore{fakeAdminCore: fakeAdminCore{user: core.AdminUser{ID: 1, Role: "admin"}}}
	protected := &fakeProtectedUpstreamConsole{fakeUpstreamConsole: &fakeUpstreamConsole{}}
	server := NewServer(nil, coreClient, Options{Upstreams: protected}, nil)

	unauthorized := httptest.NewRequest(http.MethodPut, "/api/groups/11/accounts/7/protection", strings.NewReader(`{"protection_multiplier":0.16}`))
	unauthorizedResponse := httptest.NewRecorder()
	server.Handler().ServeHTTP(unauthorizedResponse, unauthorized)
	if unauthorizedResponse.Code != http.StatusUnauthorized || len(protected.setCalls) != 0 {
		t.Fatalf("unauthorized mutation reached manager: status=%d calls=%#v", unauthorizedResponse.Code, protected.setCalls)
	}

	setRequest := httptest.NewRequest(http.MethodPut, "/api/groups/11/accounts/7/protection", strings.NewReader(`{"protection_multiplier":0.16}`))
	setRequest.Header.Set("Authorization", "Bearer valid")
	setResponse := httptest.NewRecorder()
	server.Handler().ServeHTTP(setResponse, setRequest)
	if setResponse.Code != http.StatusOK || len(protected.setCalls) != 1 || protected.setCalls[0].multiplier != 0.16 {
		t.Fatalf("set protection failed: status=%d calls=%#v body=%s", setResponse.Code, protected.setCalls, setResponse.Body.String())
	}

	releaseRequest := httptest.NewRequest(http.MethodPost, "/api/groups/11/accounts/7/protection/release", nil)
	releaseRequest.Header.Set("Authorization", "Bearer valid")
	releaseResponse := httptest.NewRecorder()
	server.Handler().ServeHTTP(releaseResponse, releaseRequest)
	if releaseResponse.Code != http.StatusOK || len(protected.releaseCalls) != 1 {
		t.Fatalf("release failed: status=%d calls=%#v body=%s", releaseResponse.Code, protected.releaseCalls, releaseResponse.Body.String())
	}

	removeRequest := httptest.NewRequest(http.MethodDelete, "/api/groups/11/accounts/7/binding", nil)
	removeRequest.Header.Set("Authorization", "Bearer valid")
	removeResponse := httptest.NewRecorder()
	server.Handler().ServeHTTP(removeResponse, removeRequest)
	if removeResponse.Code != http.StatusOK || len(protected.removeCalls) != 1 {
		t.Fatalf("remove failed: status=%d calls=%#v body=%s", removeResponse.Code, protected.removeCalls, removeResponse.Body.String())
	}
}

func TestGroupProtectionDefaultRoutesRequireAdminAndValidateGroup(t *testing.T) {
	coreClient := &fakeConsoleCore{
		fakeAdminCore: fakeAdminCore{user: core.AdminUser{ID: 1, Role: "admin"}},
		groups:        []model.UpstreamGroup{{ID: 11, Name: "Protected", Platform: "openai", Status: "active"}},
	}
	protected := &fakeProtectedUpstreamConsole{fakeUpstreamConsole: &fakeUpstreamConsole{}, defaults: map[int64]upstream.GroupProtectionDefaultView{}}
	server := NewServer(nil, coreClient, Options{Upstreams: protected}, nil)

	unauthorized := httptest.NewRequest(http.MethodPut, "/api/groups/11/protection-default", strings.NewReader(`{"protection_multiplier":0.16}`))
	unauthorizedResponse := httptest.NewRecorder()
	server.Handler().ServeHTTP(unauthorizedResponse, unauthorized)
	if unauthorizedResponse.Code != http.StatusUnauthorized || len(protected.defaultSets) != 0 {
		t.Fatalf("unauthorized group default reached manager: status=%d calls=%#v", unauthorizedResponse.Code, protected.defaultSets)
	}

	invalid := httptest.NewRequest(http.MethodPut, "/api/groups/11/protection-default", strings.NewReader(`{"protection_multiplier":-1}`))
	invalid.Header.Set("Authorization", "Bearer valid")
	invalidResponse := httptest.NewRecorder()
	server.Handler().ServeHTTP(invalidResponse, invalid)
	if invalidResponse.Code != http.StatusBadRequest || len(protected.defaultSets) != 0 {
		t.Fatalf("invalid group default was accepted: status=%d calls=%#v body=%s", invalidResponse.Code, protected.defaultSets, invalidResponse.Body.String())
	}

	setRequest := httptest.NewRequest(http.MethodPut, "/api/groups/11/protection-default", strings.NewReader(`{"protection_multiplier":0.16}`))
	setRequest.Header.Set("Authorization", "Bearer valid")
	setResponse := httptest.NewRecorder()
	server.Handler().ServeHTTP(setResponse, setRequest)
	if setResponse.Code != http.StatusOK || len(protected.defaultSets) != 1 || protected.defaultSets[0].multiplier != 0.16 {
		t.Fatalf("group default set failed: status=%d calls=%#v body=%s", setResponse.Code, protected.defaultSets, setResponse.Body.String())
	}

	getRequest := httptest.NewRequest(http.MethodGet, "/api/groups/11/protection-default", nil)
	getRequest.Header.Set("Authorization", "Bearer valid")
	getResponse := httptest.NewRecorder()
	server.Handler().ServeHTTP(getResponse, getRequest)
	if getResponse.Code != http.StatusOK || !strings.Contains(getResponse.Body.String(), `"protection_multiplier":0.16`) {
		t.Fatalf("group default get failed: status=%d body=%s", getResponse.Code, getResponse.Body.String())
	}

	deleteRequest := httptest.NewRequest(http.MethodDelete, "/api/groups/11/protection-default", nil)
	deleteRequest.Header.Set("Authorization", "Bearer valid")
	deleteResponse := httptest.NewRecorder()
	server.Handler().ServeHTTP(deleteResponse, deleteRequest)
	if deleteResponse.Code != http.StatusOK || len(protected.defaultDeletes) != 1 {
		t.Fatalf("group default delete failed: status=%d calls=%#v body=%s", deleteResponse.Code, protected.defaultDeletes, deleteResponse.Body.String())
	}
}

func TestBulkBindingUsesProtectionAwareLogicalSelection(t *testing.T) {
	coreClient := &fakeConsoleCore{
		fakeAdminCore: fakeAdminCore{user: core.AdminUser{ID: 1, Role: "admin"}},
		groups:        []model.UpstreamGroup{{ID: 11, Name: "Protected", Platform: "openai", Status: "active"}},
		accounts:      []model.UpstreamAccount{{ID: 7, Name: "key", Type: "apikey", Platform: "openai"}},
	}
	protected := &fakeProtectedUpstreamConsole{fakeUpstreamConsole: &fakeUpstreamConsole{}, bulkUpdated: []int64{7}}
	server := NewServer(nil, coreClient, Options{Upstreams: protected}, nil)
	request := httptest.NewRequest(http.MethodPut, "/api/groups/11/accounts", strings.NewReader(`{"account_ids":[7]}`))
	request.Header.Set("Authorization", "Bearer valid")
	response := httptest.NewRecorder()
	server.Handler().ServeHTTP(response, request)
	if response.Code != http.StatusOK || protected.bulkGroupID != 11 || !reflect.DeepEqual(protected.bulkSelected, []int64{7}) {
		t.Fatalf("bulk selection did not reach protection manager: status=%d group=%d selected=%#v body=%s", response.Code, protected.bulkGroupID, protected.bulkSelected, response.Body.String())
	}
}
