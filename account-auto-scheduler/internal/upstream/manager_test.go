package upstream

import (
	"context"
	"encoding/json"
	"errors"
	"math"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api-account-auto-scheduler/internal/core"
	"github.com/Wei-Shaw/sub2api-account-auto-scheduler/internal/model"
	"github.com/Wei-Shaw/sub2api-account-auto-scheduler/internal/store"
)

type managerLocalCore struct {
	accounts      []model.UpstreamAccount
	listErr       error
	secrets       map[int64]string
	exportErr     error
	exportedIDs   []int64
	exportedJWT   string
	exportedAgent core.ForwardedIdentity
}

type finalCostWriterCore struct {
	*managerLocalCore
	updates []struct {
		accountID int64
		value     *float64
	}
}

func (f *managerLocalCore) ListAccounts(context.Context) ([]model.UpstreamAccount, error) {
	return append([]model.UpstreamAccount(nil), f.accounts...), f.listErr
}

func (f *managerLocalCore) ExportAPIKeySecrets(_ context.Context, ids []int64, jwt string, identity core.ForwardedIdentity) (map[int64]string, error) {
	f.exportedIDs = append([]int64(nil), ids...)
	f.exportedJWT = jwt
	f.exportedAgent = identity
	if f.exportErr != nil {
		return nil, f.exportErr
	}
	result := make(map[int64]string, len(f.secrets))
	for id, secret := range f.secrets {
		result[id] = secret
	}
	return result, nil
}

func (f *finalCostWriterCore) SetFinalCostMultiplier(_ context.Context, accountID int64, multiplier *float64) (model.UpstreamAccount, error) {
	var copied *float64
	if multiplier != nil {
		value := *multiplier
		copied = &value
	}
	f.updates = append(f.updates, struct {
		accountID int64
		value     *float64
	}{accountID: accountID, value: copied})
	for index := range f.accounts {
		if f.accounts[index].ID == accountID {
			if f.accounts[index].Extra == nil {
				f.accounts[index].Extra = make(map[string]any)
			}
			if copied == nil {
				f.accounts[index].Extra[model.FinalCostMultiplierExtraKey] = nil
			} else {
				f.accounts[index].Extra[model.FinalCostMultiplierExtraKey] = *copied
			}
			return f.accounts[index], nil
		}
	}
	return model.UpstreamAccount{}, errors.New("account not found")
}

func managerTestStore(t *testing.T) *store.Store {
	t.Helper()
	stateStore, err := store.Open(filepath.Join(t.TempDir(), "state.json"))
	if err != nil {
		t.Fatal(err)
	}
	return stateStore
}

func apiKeyAccount(id int64, name, baseURL string) model.UpstreamAccount {
	return model.UpstreamAccount{
		ID: id, Name: name, Type: "apikey", Platform: "openai", Status: "active", Schedulable: true,
		Credentials: map[string]any{"base_url": baseURL},
	}
}

func TestManagerAggregatesCandidatesAndRedactsStoredSecrets(t *testing.T) {
	stateStore := managerTestStore(t)
	box, err := NewCredentialBox(testCredentialKey())
	if err != nil {
		t.Fatal(err)
	}
	baseURL := "https://upstream.example"
	upstreamID := model.StableUpstreamID(baseURL)
	rawQuota := 1250000.0
	quotaPerUnit := 500000.0
	envelope, err := box.Encrypt(upstreamID, "identity", AuthMaterial{AccessToken: "top-secret-token"})
	if err != nil {
		t.Fatal(err)
	}
	now := time.Now().UTC()
	if err := stateStore.PutUpstream(model.ManagedUpstream{
		ID: upstreamID, Name: "Saved", BaseURL: baseURL, TypeOverride: model.UpstreamTypeSub2API,
		Detection: model.UpstreamDetection{Type: model.UpstreamTypeSub2API}, CreatedAt: now, UpdatedAt: now,
		Identities: map[string]model.UpstreamIdentity{
			"identity": {
				ID: "identity", Label: "Primary", AuthMode: model.UpstreamAuthToken, Credential: envelope,
				Status: model.IdentityStatusConnected, CreatedAt: now, UpdatedAt: now,
				Balance: &model.UpstreamBalance{
					Amount: 2.5, Unit: "USD", Source: "newapi", ObservedAt: now,
					RawQuota: &rawQuota, QuotaPerUnit: &quotaPerUnit,
				},
				Keys: map[string]model.RemoteKey{
					"remote": {ID: "remote", Name: "Remote", MaskedKey: "sk-a**********z", Fingerprint: "secret-fingerprint", MatchAvailable: true, SyncedAt: now},
				},
			},
		},
	}); err != nil {
		t.Fatal(err)
	}
	local := &managerLocalCore{accounts: []model.UpstreamAccount{
		apiKeyAccount(1, "first", "https://UPSTREAM.example:443/v1"),
		apiKeyAccount(2, "second", "https://upstream.example/api/v1"),
		{ID: 3, Name: "oauth", Type: "oauth", Credentials: map[string]any{"base_url": "https://upstream.example/v1"}},
	}}
	manager := NewManager(stateStore, local, box, 0, nil)
	result, err := manager.List(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if !result.CredentialsEnabled || len(result.Upstreams) != 1 || len(result.Upstreams[0].LocalAccounts) != 2 || len(result.Upstreams[0].Identities) != 1 {
		t.Fatalf("unexpected upstream list: %#v", result)
	}
	summary := result.Upstreams[0].BalanceSummary
	if summary.Status != "single" || summary.IdentityCount != 1 || summary.AvailableCount != 1 || summary.Balance == nil || summary.Balance.Amount != 2.5 || result.Upstreams[0].Identities[0].Balance == nil {
		t.Fatalf("unexpected balance summary: %#v", result.Upstreams[0])
	}
	raw, err := json.Marshal(result)
	if err != nil {
		t.Fatal(err)
	}
	serialized := string(raw)
	for _, forbidden := range []string{"top-secret-token", "secret-fingerprint", "ciphertext", "nonce", "cookie", "raw_response", "headers"} {
		if strings.Contains(serialized, forbidden) {
			t.Fatalf("public response leaked %q: %s", forbidden, serialized)
		}
	}
}

func TestPublicUpstreamDoesNotSumMultipleIdentityBalances(t *testing.T) {
	now := time.Now().UTC()
	view := publicUpstream(model.ManagedUpstream{
		ID: "up_multiple", Name: "Multiple", BaseURL: "https://example.com",
		Identities: map[string]model.UpstreamIdentity{
			"first": {
				ID: "first", CreatedAt: now,
				Balance: &model.UpstreamBalance{Amount: 10, Unit: "USD", Source: "sub2api", ObservedAt: now},
			},
			"second": {
				ID: "second", CreatedAt: now.Add(time.Second),
				Balance: &model.UpstreamBalance{Amount: 20, Unit: "USD", Source: "sub2api", ObservedAt: now},
			},
		},
	}, nil, true)

	if view.BalanceSummary.Status != "multiple" || view.BalanceSummary.IdentityCount != 2 || view.BalanceSummary.AvailableCount != 2 || view.BalanceSummary.Balance != nil {
		t.Fatalf("multiple balances were summarized ambiguously: %#v", view.BalanceSummary)
	}
	if len(view.Identities) != 2 || view.Identities[0].Balance == nil || view.Identities[0].Balance.Amount != 10 || view.Identities[1].Balance == nil || view.Identities[1].Balance.Amount != 20 {
		t.Fatalf("identity balances were not kept separate: %#v", view.Identities)
	}
	raw, err := json.Marshal(view.BalanceSummary)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(raw), `"amount":30`) {
		t.Fatalf("summary contained an aggregate balance: %s", raw)
	}
}

func TestPublicIdentityProjectsGroupFinalMultiplierAndUnknownValues(t *testing.T) {
	now := time.Now().UTC()
	groupMultiplier := 0.8
	rechargeRate, err := model.NewUpstreamRechargeRate(model.RechargeRateUSDPerCNY, 5, now)
	if err != nil {
		t.Fatal(err)
	}
	view := publicIdentity(model.UpstreamIdentity{
		ID: "identity",
		Groups: map[string]model.RemoteGroup{
			"openai": {
				ID: "openai", Name: "OpenAI Standard", Platform: "openai", Multiplier: &groupMultiplier,
				MultiplierSource: "user_override", SyncedAt: now,
			},
			"auto": {
				ID: "auto", Name: "Auto", Platform: "grok", MultiplierSource: "dynamic", Stale: true, SyncedAt: now.Add(-time.Minute),
			},
		},
	}, &rechargeRate)
	if len(view.Groups) != 2 {
		t.Fatalf("groups = %#v", view.Groups)
	}
	openAI := view.Groups[0]
	if openAI.ID != "openai" || openAI.Platform != "openai" || openAI.Multiplier == nil || *openAI.Multiplier != 0.8 || openAI.FinalMultiplier == nil || math.Abs(*openAI.FinalMultiplier-0.16) > 1e-12 || openAI.Stale {
		t.Fatalf("unexpected projected group: %#v", openAI)
	}
	auto := view.Groups[1]
	if auto.ID != "auto" || auto.Platform != "grok" || auto.Multiplier != nil || auto.FinalMultiplier != nil || !auto.Stale || auto.MultiplierSource != "dynamic" {
		t.Fatalf("unexpected unavailable group projection: %#v", auto)
	}
}

func TestManagerMarksRemoteGroupsStaleAfterSyncFailure(t *testing.T) {
	stateStore := managerTestStore(t)
	now := time.Now().UTC()
	multiplier := 1.2
	if err := stateStore.PutUpstream(model.ManagedUpstream{
		ID: "upstream", Name: "Upstream", BaseURL: "https://upstream.example", CreatedAt: now, UpdatedAt: now,
		Identities: map[string]model.UpstreamIdentity{
			"identity": {
				ID: "identity", Groups: map[string]model.RemoteGroup{
					"group": {ID: "group", Name: "Group", Multiplier: &multiplier, SyncedAt: now},
				},
			},
		},
	}); err != nil {
		t.Fatal(err)
	}
	manager := NewManager(stateStore, &managerLocalCore{}, nil, 0, nil)
	if err := manager.recordSyncFailure("upstream", "identity", adapterError("UPSTREAM_NETWORK_ERROR", "network", model.IdentityStatusNetworkError, http.StatusBadGateway)); err == nil {
		t.Fatal("recordSyncFailure unexpectedly succeeded")
	}
	stored, err := stateStore.GetUpstream("upstream")
	if err != nil {
		t.Fatal(err)
	}
	group := stored.Identities["identity"].Groups["group"]
	if !group.Stale || group.Multiplier == nil || *group.Multiplier != 1.2 {
		t.Fatalf("failed sync did not preserve a stale snapshot: %#v", group)
	}
}

func TestManagerDisabledCredentialBoxDoesNotBlockListing(t *testing.T) {
	box, err := NewCredentialBox("")
	if err != nil {
		t.Fatal(err)
	}
	manager := NewManager(managerTestStore(t), &managerLocalCore{accounts: []model.UpstreamAccount{apiKeyAccount(1, "key", "https://example.com/v1")}}, box, 0, nil)
	result, err := manager.List(context.Background())
	if err != nil || result.CredentialsEnabled || len(result.Upstreams) != 1 {
		t.Fatalf("listing failed while credentials disabled: %#v err=%v", result, err)
	}
	_, err = manager.Connect(context.Background(), result.Upstreams[0].ID, ConnectInput{Login: LoginInput{Mode: model.UpstreamAuthToken, Token: "secret"}})
	if adapterErr := AsAdapterError(err); adapterErr.Code != "CREDENTIALS_DISABLED" {
		t.Fatalf("Connect error = %#v, want disabled", adapterErr)
	}
	if err := manager.SyncIdentity(context.Background(), "missing", "missing"); AsAdapterError(err).Code != "CREDENTIALS_DISABLED" {
		t.Fatalf("SyncIdentity error = %#v, want disabled", err)
	}
}

func TestManagerStartsLocalCaptchaWithoutPersistingIncompleteState(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/v1/settings/public":
			writeTestJSON(t, w, http.StatusOK, map[string]any{"code": 0, "data": map[string]any{"local_captcha_enabled": true}})
		case "/api/v1/auth/captcha":
			writeTestJSON(t, w, http.StatusOK, map[string]any{"code": 0, "data": map[string]any{"captcha_id": "captcha-id", "image_data": testCaptchaImage}})
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()

	stateStore := managerTestStore(t)
	now := time.Now().UTC()
	record := model.ManagedUpstream{
		ID: "up_captcha", Name: "Captcha", BaseURL: "https://model.example", TypeOverride: model.UpstreamTypeSub2API,
		CreatedAt: now, UpdatedAt: now, Identities: map[string]model.UpstreamIdentity{},
	}
	if err := stateStore.PutUpstream(record); err != nil {
		t.Fatal(err)
	}
	before, _ := stateStore.GetUpstream(record.ID)
	beforeJSON, _ := json.Marshal(before)
	box, err := NewCredentialBox(testCredentialKey())
	if err != nil {
		t.Fatal(err)
	}
	manager := NewManager(stateStore, &managerLocalCore{}, box, 0, nil)
	result, err := manager.StartLoginChallenge(context.Background(), record.ID, LoginChallengeInput{
		ManagementURL: server.URL + "/api/v1", ManagementURLSet: true,
	})
	if err != nil {
		t.Fatal(err)
	}
	if !result.Challenge.Required || result.Challenge.CaptchaID != "captcha-id" || result.ManagementURL != server.URL {
		t.Fatalf("unexpected challenge result: %#v", result)
	}
	after, _ := stateStore.GetUpstream(record.ID)
	afterJSON, _ := json.Marshal(after)
	if string(beforeJSON) != string(afterJSON) || after.ManagementURL != "" || len(after.Identities) != 0 {
		t.Fatalf("challenge preflight mutated persisted upstream: before=%s after=%s", beforeJSON, afterJSON)
	}
}

func TestManagerPersistsRechargeRateAndDerivesFinalMultiplier(t *testing.T) {
	stateStore := managerTestStore(t)
	baseURL := "https://upstream.example"
	upstreamID := model.StableUpstreamID(baseURL)
	now := time.Now().UTC()
	groupMultiplier := 0.8
	if err := stateStore.PutUpstream(model.ManagedUpstream{
		ID: upstreamID, BaseURL: baseURL, TypeOverride: model.UpstreamTypeNewAPI, CreatedAt: now, UpdatedAt: now,
		Identities: map[string]model.UpstreamIdentity{"identity": {
			ID: "identity", CreatedAt: now, UpdatedAt: now,
			Keys: map[string]model.RemoteKey{"key": {ID: "key", Name: "Key", Multiplier: &groupMultiplier, SyncedAt: now}},
		}},
	}); err != nil {
		t.Fatal(err)
	}
	manager := NewManager(stateStore, &managerLocalCore{accounts: []model.UpstreamAccount{apiKeyAccount(1, "local", baseURL+"/v1")}}, &CredentialBox{}, 0, nil)
	view, err := manager.SetRechargeRate(context.Background(), upstreamID, RechargeRateInput{Mode: model.RechargeRateUSDPerCNY, Value: 5})
	if err != nil {
		t.Fatal(err)
	}
	if view.RechargeRate == nil || math.Abs(view.RechargeRate.CNYPerUSD-0.2) > 1e-12 || len(view.Identities) != 1 || len(view.Identities[0].Keys) != 1 {
		t.Fatalf("unexpected recharge-rate view: %#v", view)
	}
	key := view.Identities[0].Keys[0]
	if key.FinalMultiplier == nil || math.Abs(*key.FinalMultiplier-0.16) > 1e-12 || key.Multiplier == nil || *key.Multiplier != 0.8 {
		t.Fatalf("unexpected derived multiplier: %#v", key)
	}
	stored, _ := stateStore.GetUpstream(upstreamID)
	if stored.RechargeRate == nil || stored.RechargeRate.CNYPerUSD != 0.2 || *stored.Identities["identity"].Keys["key"].Multiplier != 0.8 {
		t.Fatalf("unexpected persisted rate: %#v", stored)
	}

	cleared, err := manager.ClearRechargeRate(context.Background(), upstreamID)
	if err != nil {
		t.Fatal(err)
	}
	if cleared.RechargeRate != nil || cleared.Identities[0].Keys[0].FinalMultiplier != nil {
		t.Fatalf("cleared recharge rate still affects view: %#v", cleared)
	}
}

func TestDeriveFinalMultiplierRejectsNegativeGroupMultiplier(t *testing.T) {
	recharge := &model.UpstreamRechargeRate{CNYPerUSD: 0.2}
	negative := -0.8
	if got := deriveFinalMultiplier(recharge, &negative); got != nil {
		t.Fatalf("negative group multiplier produced final cost: %v", *got)
	}
}

func TestManagerReconcilesFinalCostMultiplierWithoutBillingRate(t *testing.T) {
	stateStore := managerTestStore(t)
	baseURL := "https://upstream.example"
	upstreamID := model.StableUpstreamID(baseURL)
	now := time.Now().UTC()
	groupMultiplier := 0.8
	localAccountID := int64(1)
	if err := stateStore.PutUpstream(model.ManagedUpstream{
		ID: upstreamID, BaseURL: baseURL, TypeOverride: model.UpstreamTypeSub2API, CreatedAt: now, UpdatedAt: now,
		RechargeRate: &model.UpstreamRechargeRate{CNYPerUSD: 0.2},
		Identities: map[string]model.UpstreamIdentity{"identity": {
			ID: "identity", CreatedAt: now, UpdatedAt: now,
			Keys: map[string]model.RemoteKey{"key": {ID: "key", LocalAccountID: &localAccountID, Multiplier: &groupMultiplier, SyncedAt: now}},
		}},
	}); err != nil {
		t.Fatal(err)
	}
	local := &finalCostWriterCore{managerLocalCore: &managerLocalCore{accounts: []model.UpstreamAccount{apiKeyAccount(1, "local", baseURL+"/v1")}}}
	manager := NewManager(stateStore, local, &CredentialBox{}, 0, nil)

	if _, err := manager.SetRechargeRate(context.Background(), upstreamID, RechargeRateInput{Mode: model.RechargeRateCNYPerUSD, Value: 0.2}); err != nil {
		t.Fatal(err)
	}
	if len(local.updates) != 1 || local.updates[0].value == nil || math.Abs(*local.updates[0].value-0.16) > 1e-12 {
		t.Fatalf("unexpected final cost update: %#v", local.updates)
	}
	if multiplier, ok := local.accounts[0].FinalCostMultiplier(); !ok || multiplier == nil || math.Abs(*multiplier-0.16) > 1e-12 {
		t.Fatalf("local account was not updated: %#v", local.accounts[0])
	}

	// A second reconciliation is idempotent because the local account already
	// contains the desired scheduling-only value.
	manager.reconcileFinalCostMultipliersBestEffort(context.Background())
	if len(local.updates) != 1 {
		t.Fatalf("idempotent reconciliation wrote again: %#v", local.updates)
	}

	if _, err := manager.ClearRechargeRate(context.Background(), upstreamID); err != nil {
		t.Fatal(err)
	}
	if len(local.updates) != 2 || local.updates[1].value != nil {
		t.Fatalf("expected final cost clear, got %#v", local.updates)
	}
}

func TestManagerProjectsFinalMultiplierOnlyForCurrentExplicitBinding(t *testing.T) {
	stateStore := managerTestStore(t)
	baseURL := "https://upstream.example"
	otherBaseURL := "https://other.example"
	upstreamID := model.StableUpstreamID(baseURL)
	otherUpstreamID := model.StableUpstreamID(otherBaseURL)
	now := time.Now().UTC()
	groupMultiplier := 0.8
	otherGroupMultiplier := 1.5
	boundAccountID := int64(1)
	staleAccountID := int64(2)
	if err := stateStore.PutUpstream(model.ManagedUpstream{
		ID: upstreamID, BaseURL: baseURL, CreatedAt: now, UpdatedAt: now,
		RechargeRate: &model.UpstreamRechargeRate{CNYPerUSD: 0.2},
		Identities: map[string]model.UpstreamIdentity{"identity": {
			ID: "identity", Keys: map[string]model.RemoteKey{
				"bound": {ID: "bound", LocalAccountID: &boundAccountID, Multiplier: &groupMultiplier, SyncedAt: now},
				"stale": {ID: "stale", LocalAccountID: &staleAccountID, Stale: true, Multiplier: &groupMultiplier, SyncedAt: now},
			},
		}},
	}); err != nil {
		t.Fatal(err)
	}
	if err := stateStore.PutUpstream(model.ManagedUpstream{
		ID: otherUpstreamID, BaseURL: otherBaseURL, CreatedAt: now, UpdatedAt: now,
		RechargeRate: &model.UpstreamRechargeRate{CNYPerUSD: 0.5},
		Identities: map[string]model.UpstreamIdentity{"identity": {
			ID: "identity", Keys: map[string]model.RemoteKey{
				"mismatched": {ID: "mismatched", LocalAccountID: &staleAccountID, Multiplier: &otherGroupMultiplier, SyncedAt: now},
			},
		}},
	}); err != nil {
		t.Fatal(err)
	}

	manager := NewManager(stateStore, &managerLocalCore{}, &CredentialBox{}, 0, nil)
	projections := manager.LocalAccountFinalMultipliers([]model.UpstreamAccount{
		apiKeyAccount(1, "bound", baseURL+"/v1"),
		apiKeyAccount(2, "stale", baseURL+"/v1"),
		apiKeyAccount(3, "unbound", baseURL+"/v1"),
		{ID: 4, Name: "oauth", Type: "oauth", Credentials: map[string]any{"base_url": baseURL + "/v1"}},
	})

	bound := projections[1]
	if bound.Status != "available" || bound.FinalMultiplier == nil || math.Abs(*bound.FinalMultiplier-0.16) > 1e-12 || bound.GroupMultiplier == nil || *bound.GroupMultiplier != 0.8 || bound.RechargeRateCNYPerUSD == nil || *bound.RechargeRateCNYPerUSD != 0.2 {
		t.Fatalf("unexpected bound projection: %#v", bound)
	}
	if stale := projections[2]; stale.Status != "stale" || stale.FinalMultiplier != nil {
		t.Fatalf("stale or mismatched binding was used: %#v", stale)
	}
	if unbound := projections[3]; unbound.Status != "unbound" || unbound.FinalMultiplier != nil {
		t.Fatalf("unexpected unbound projection: %#v", unbound)
	}
	if _, exists := projections[4]; exists {
		t.Fatalf("OAuth account unexpectedly received a multiplier projection: %#v", projections)
	}
}

func TestManagerFinalMultiplierProjectionExplainsMissingOperands(t *testing.T) {
	stateStore := managerTestStore(t)
	baseURL := "https://upstream.example"
	upstreamID := model.StableUpstreamID(baseURL)
	now := time.Now().UTC()
	firstAccountID := int64(1)
	secondAccountID := int64(2)
	if err := stateStore.PutUpstream(model.ManagedUpstream{
		ID: upstreamID, BaseURL: baseURL, CreatedAt: now, UpdatedAt: now,
		Identities: map[string]model.UpstreamIdentity{"identity": {
			ID: "identity", Keys: map[string]model.RemoteKey{
				"recharge": {ID: "recharge", LocalAccountID: &firstAccountID, SyncedAt: now},
				"group":    {ID: "group", LocalAccountID: &secondAccountID, SyncedAt: now},
			},
		}},
	}); err != nil {
		t.Fatal(err)
	}
	manager := NewManager(stateStore, &managerLocalCore{}, &CredentialBox{}, 0, nil)
	projections := manager.LocalAccountFinalMultipliers([]model.UpstreamAccount{
		apiKeyAccount(1, "recharge", baseURL+"/v1"),
		apiKeyAccount(2, "group", baseURL+"/v1"),
	})
	if value := projections[1]; value.Status != "recharge_unset" || value.FinalMultiplier != nil {
		t.Fatalf("missing recharge rate was not reported: %#v", value)
	}
	if value := projections[2]; value.Status != "recharge_unset" || value.FinalMultiplier != nil {
		t.Fatalf("missing recharge rate should take precedence: %#v", value)
	}

	rate := model.UpstreamRechargeRate{CNYPerUSD: 0.2}
	if err := stateStore.UpdateUpstream(upstreamID, func(record *model.ManagedUpstream) error {
		record.RechargeRate = &rate
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	projections = manager.LocalAccountFinalMultipliers([]model.UpstreamAccount{
		apiKeyAccount(1, "recharge", baseURL+"/v1"),
		apiKeyAccount(2, "group", baseURL+"/v1"),
	})
	if value := projections[1]; value.Status != "group_multiplier_unknown" || value.FinalMultiplier != nil {
		t.Fatalf("missing group multiplier was not reported: %#v", value)
	}
}

func TestManagerSyncUpstreamIsolatesFailureAndRetainsSnapshot(t *testing.T) {
	badFails := true
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		token := strings.TrimPrefix(r.Header.Get("Authorization"), "Bearer ")
		switch r.URL.Path {
		case "/api/v1/auth/me":
			balance := 25.0
			if token == "good" {
				balance = 50
			}
			writeTestJSON(t, w, http.StatusOK, map[string]any{"code": 0, "data": map[string]any{"id": 1, "email": token + "@example.com", "balance": balance}})
		case "/api/v1/groups/available":
			writeTestJSON(t, w, http.StatusOK, map[string]any{"code": 0, "data": []any{}})
		case "/api/v1/groups/rates":
			writeTestJSON(t, w, http.StatusOK, map[string]any{"code": 0, "data": map[string]any{}})
		case "/api/v1/keys":
			if token == "bad" && badFails {
				writeTestJSON(t, w, http.StatusInternalServerError, map[string]any{"code": "FAILED", "message": "secret must not escape"})
				return
			}
			writeTestJSON(t, w, http.StatusOK, map[string]any{"code": 0, "data": map[string]any{
				"items": []any{map[string]any{"id": 8, "key": "sk-good-complete", "name": "good", "status": "active"}}, "total": 1, "page": 1, "pages": 1,
			}})
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()
	box, _ := NewCredentialBox(testCredentialKey())
	stateStore := managerTestStore(t)
	baseURL, _ := model.NormalizeUpstreamBaseURL(server.URL)
	upstreamID := model.StableUpstreamID(baseURL)
	now := time.Now().UTC()
	oldObservedAt := now.Add(-time.Hour)
	identities := map[string]model.UpstreamIdentity{}
	for _, id := range []string{"good", "bad"} {
		envelope, err := box.Encrypt(upstreamID, id, AuthMaterial{AccessToken: id})
		if err != nil {
			t.Fatal(err)
		}
		identities[id] = model.UpstreamIdentity{
			ID: id, Label: id, AuthMode: model.UpstreamAuthToken, Credential: envelope, Status: model.IdentityStatusConnected,
			Balance:   &model.UpstreamBalance{Amount: 10, Unit: "USD", Source: "sub2api", ObservedAt: oldObservedAt},
			CreatedAt: now, UpdatedAt: now, Keys: map[string]model.RemoteKey{"old": {ID: "old", Name: "old", SyncedAt: now}},
		}
	}
	if err := stateStore.PutUpstream(model.ManagedUpstream{
		ID: upstreamID, Name: "Test", BaseURL: baseURL, TypeOverride: model.UpstreamTypeSub2API,
		Identities: identities, CreatedAt: now, UpdatedAt: now,
	}); err != nil {
		t.Fatal(err)
	}
	manager := NewManager(stateStore, &managerLocalCore{}, box, 0, nil)
	outcomes := manager.SyncUpstream(context.Background(), upstreamID)
	if len(outcomes) != 2 {
		t.Fatalf("outcomes = %#v", outcomes)
	}
	updated, err := stateStore.GetUpstream(upstreamID)
	if err != nil {
		t.Fatal(err)
	}
	if updated.Identities["good"].Status != model.IdentityStatusConnected || updated.Identities["good"].Keys["8"].Fingerprint == "" || updated.Identities["good"].Balance == nil || updated.Identities["good"].Balance.Amount != 50 || updated.Identities["good"].Balance.Stale {
		t.Fatalf("good identity was not synchronized: %#v", updated.Identities["good"])
	}
	bad := updated.Identities["bad"]
	if bad.Status != model.IdentityStatusSyncError || bad.Keys["old"].ID != "old" || strings.Contains(bad.StatusMessage, "secret") || bad.Balance == nil || bad.Balance.Amount != 10 || !bad.Balance.Stale || !bad.Balance.ObservedAt.Equal(oldObservedAt) {
		t.Fatalf("bad identity did not retain sanitized snapshot: %#v", bad)
	}

	badFails = false
	if err := manager.SyncIdentity(context.Background(), upstreamID, "bad"); err != nil {
		t.Fatal(err)
	}
	recovered, err := stateStore.GetUpstream(upstreamID)
	if err != nil {
		t.Fatal(err)
	}
	recoveredBalance := recovered.Identities["bad"].Balance
	if recoveredBalance == nil || recoveredBalance.Amount != 25 || recoveredBalance.Stale || !recoveredBalance.ObservedAt.After(oldObservedAt) {
		t.Fatalf("successful retry did not replace stale balance: %#v", recovered.Identities["bad"])
	}
}

func TestManagerConnectPersistsIdentityBalance(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/v1/auth/me":
			writeTestJSON(t, w, http.StatusOK, map[string]any{"code": 0, "data": map[string]any{"id": 7, "email": "operator@example.com", "balance": 0}})
		case "/api/v1/groups/available":
			writeTestJSON(t, w, http.StatusOK, map[string]any{"code": 0, "data": []any{}})
		case "/api/v1/groups/rates":
			writeTestJSON(t, w, http.StatusOK, map[string]any{"code": 0, "data": map[string]any{}})
		case "/api/v1/keys":
			writeTestJSON(t, w, http.StatusOK, map[string]any{"code": 0, "data": map[string]any{"items": []any{}, "total": 0, "page": 1, "pages": 1}})
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()

	box, _ := NewCredentialBox(testCredentialKey())
	stateStore := managerTestStore(t)
	baseURL, _ := model.NormalizeUpstreamBaseURL(server.URL)
	upstreamID := model.StableUpstreamID(baseURL)
	now := time.Now().UTC()
	if err := stateStore.PutUpstream(model.ManagedUpstream{
		ID: upstreamID, Name: "Test", BaseURL: baseURL, TypeOverride: model.UpstreamTypeSub2API,
		Identities: map[string]model.UpstreamIdentity{}, CreatedAt: now, UpdatedAt: now,
	}); err != nil {
		t.Fatal(err)
	}

	manager := NewManager(stateStore, &managerLocalCore{}, box, 0, nil)
	view, err := manager.Connect(context.Background(), upstreamID, ConnectInput{
		Label: "Primary",
		Login: LoginInput{Mode: model.UpstreamAuthToken, Token: "token"},
	})
	if err != nil {
		t.Fatal(err)
	}
	if view.Balance == nil || view.Balance.Amount != 0 || view.Balance.Unit != "USD" || view.Balance.Stale {
		t.Fatalf("connect did not expose zero balance: %#v", view)
	}
	stored, err := stateStore.GetUpstream(upstreamID)
	if err != nil {
		t.Fatal(err)
	}
	identity := stored.Identities[view.ID]
	if identity.Balance == nil || identity.Balance.Amount != 0 || identity.Balance.ObservedAt.IsZero() {
		t.Fatalf("connect did not persist balance: %#v", identity)
	}
}

func TestManagerConnectUsesManagementSiteForAllManagementOperations(t *testing.T) {
	managementServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/v1/auth/credential-key":
			http.NotFound(w, r)
		case "/api/v1/auth/login":
			writeTestJSON(t, w, http.StatusOK, map[string]any{"code": 0, "data": map[string]any{
				"access_token": "site-access", "user": map[string]any{"id": 7, "email": "operator@example.com"},
			}})
		case "/api/v1/auth/me":
			if r.Header.Get("Authorization") != "Bearer site-access" {
				t.Fatalf("management request had wrong authorization header: %q", r.Header.Get("Authorization"))
			}
			writeTestJSON(t, w, http.StatusOK, map[string]any{"code": 0, "data": map[string]any{"id": 7, "email": "operator@example.com", "balance": 2}})
		case "/api/v1/groups/available":
			writeTestJSON(t, w, http.StatusOK, map[string]any{"code": 0, "data": []any{}})
		case "/api/v1/groups/rates":
			writeTestJSON(t, w, http.StatusOK, map[string]any{"code": 0, "data": map[string]any{}})
		case "/api/v1/keys":
			writeTestJSON(t, w, http.StatusOK, map[string]any{"code": 0, "data": map[string]any{"items": []any{}, "total": 0, "page": 1, "pages": 1}})
		default:
			t.Fatalf("management server received unexpected request %s", r.URL.Path)
		}
	}))
	defer managementServer.Close()

	apiServer := httptest.NewServer(http.HandlerFunc(func(_ http.ResponseWriter, r *http.Request) {
		t.Fatalf("API/model endpoint must not receive management request: %s", r.URL.Path)
	}))
	defer apiServer.Close()

	box, _ := NewCredentialBox(testCredentialKey())
	stateStore := managerTestStore(t)
	baseURL, _ := model.NormalizeUpstreamBaseURL(apiServer.URL)
	managementURL, _ := model.NormalizeUpstreamBaseURL(managementServer.URL)
	upstreamID := model.StableUpstreamID(baseURL)
	now := time.Now().UTC()
	if err := stateStore.PutUpstream(model.ManagedUpstream{
		ID: upstreamID, BaseURL: baseURL, TypeOverride: model.UpstreamTypeSub2API,
		Identities: map[string]model.UpstreamIdentity{}, CreatedAt: now, UpdatedAt: now,
	}); err != nil {
		t.Fatal(err)
	}

	manager := NewManager(stateStore, &managerLocalCore{}, box, 0, nil)
	if _, err := manager.Connect(context.Background(), upstreamID, ConnectInput{
		ManagementURL: managementURL, ManagementURLSet: true,
		Login: LoginInput{Mode: model.UpstreamAuthPassword, Username: "operator@example.com", Password: "password"},
	}); err != nil {
		t.Fatal(err)
	}
	stored, err := stateStore.GetUpstream(upstreamID)
	if err != nil || stored.ManagementURL != managementURL || stored.BaseURL != baseURL {
		t.Fatalf("custom management site was not persisted independently: upstream=%#v err=%v", stored, err)
	}
}

func TestManagerDetectUsesSavedManagementSite(t *testing.T) {
	var modelEndpointCalls atomic.Int64
	modelServer := httptest.NewServer(http.HandlerFunc(func(_ http.ResponseWriter, _ *http.Request) {
		modelEndpointCalls.Add(1)
	}))
	defer modelServer.Close()

	managementServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "" || r.Header.Get("Cookie") != "" {
			t.Fatalf("anonymous detection unexpectedly sent credentials: %#v", r.Header)
		}
		switch r.URL.Path {
		case "/api/v1/settings/public":
			http.NotFound(w, r)
		case "/api/status":
			writeTestJSON(t, w, http.StatusOK, map[string]any{"success": true, "data": map[string]any{"system_name": "New API"}})
		default:
			t.Fatalf("management endpoint received unexpected request %s", r.URL.Path)
		}
	}))
	defer managementServer.Close()

	stateStore := managerTestStore(t)
	baseURL, _ := model.NormalizeUpstreamBaseURL(modelServer.URL)
	managementURL, _ := model.NormalizeUpstreamBaseURL(managementServer.URL)
	upstreamID := model.StableUpstreamID(baseURL)
	now := time.Now().UTC()
	if err := stateStore.PutUpstream(model.ManagedUpstream{
		ID: upstreamID, BaseURL: baseURL, ManagementURL: managementURL,
		Detection: model.UpstreamDetection{Type: model.UpstreamTypeUnknown}, Identities: map[string]model.UpstreamIdentity{},
		CreatedAt: now, UpdatedAt: now,
	}); err != nil {
		t.Fatal(err)
	}

	view, err := NewManager(stateStore, &managerLocalCore{}, nil, 0, nil).Detect(context.Background(), upstreamID)
	if err != nil {
		t.Fatal(err)
	}
	if view.Type != model.UpstreamTypeNewAPI || view.Detection.Evidence != "/api/status" || modelEndpointCalls.Load() != 0 {
		t.Fatalf("detection routed incorrectly: view=%#v model_calls=%d", view.Detection, modelEndpointCalls.Load())
	}
}

func TestManagerFailedLoginDoesNotReplaceSavedManagementSite(t *testing.T) {
	apiServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		t.Fatalf("API server must not receive password login request: %s", r.URL.Path)
	}))
	defer apiServer.Close()
	failingLoginServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/api/v1/auth/credential-key" {
			http.NotFound(w, r)
			return
		}
		if r.URL.Path != "/api/v1/auth/login" {
			t.Fatalf("unexpected login route %s", r.URL.Path)
		}
		writeTestJSON(t, w, http.StatusUnauthorized, map[string]any{"code": "INVALID", "message": "invalid credentials"})
	}))
	defer failingLoginServer.Close()

	box, _ := NewCredentialBox(testCredentialKey())
	stateStore := managerTestStore(t)
	baseURL, _ := model.NormalizeUpstreamBaseURL(apiServer.URL)
	previousManagementURL := "https://known-good.example"
	failingManagementURL, _ := model.NormalizeUpstreamBaseURL(failingLoginServer.URL)
	upstreamID := model.StableUpstreamID(baseURL)
	now := time.Now().UTC()
	if err := stateStore.PutUpstream(model.ManagedUpstream{
		ID: upstreamID, BaseURL: baseURL, ManagementURL: previousManagementURL, TypeOverride: model.UpstreamTypeSub2API,
		Identities: map[string]model.UpstreamIdentity{}, CreatedAt: now, UpdatedAt: now,
	}); err != nil {
		t.Fatal(err)
	}

	manager := NewManager(stateStore, &managerLocalCore{}, box, 0, nil)
	_, err := manager.Connect(context.Background(), upstreamID, ConnectInput{
		ManagementURL: failingManagementURL, ManagementURLSet: true,
		Login: LoginInput{Mode: model.UpstreamAuthPassword, Username: "operator@example.com", Password: "wrong"},
	})
	if err == nil {
		t.Fatal("failing login unexpectedly succeeded")
	}
	stored, getErr := stateStore.GetUpstream(upstreamID)
	if getErr != nil || stored.ManagementURL != previousManagementURL {
		t.Fatalf("failed login replaced saved management URL: upstream=%#v err=%v", stored, getErr)
	}
}

func TestManagerSuccessfulEmptyManagementSiteClearsOverride(t *testing.T) {
	apiServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/v1/auth/me":
			writeTestJSON(t, w, http.StatusOK, map[string]any{"code": 0, "data": map[string]any{"id": 7, "email": "operator@example.com"}})
		case "/api/v1/groups/available":
			writeTestJSON(t, w, http.StatusOK, map[string]any{"code": 0, "data": []any{}})
		case "/api/v1/groups/rates":
			writeTestJSON(t, w, http.StatusOK, map[string]any{"code": 0, "data": map[string]any{}})
		case "/api/v1/keys":
			writeTestJSON(t, w, http.StatusOK, map[string]any{"code": 0, "data": map[string]any{"items": []any{}, "total": 0, "page": 1, "pages": 1}})
		default:
			t.Fatalf("unexpected API request %s", r.URL.Path)
		}
	}))
	defer apiServer.Close()

	box, _ := NewCredentialBox(testCredentialKey())
	stateStore := managerTestStore(t)
	baseURL, _ := model.NormalizeUpstreamBaseURL(apiServer.URL)
	upstreamID := model.StableUpstreamID(baseURL)
	now := time.Now().UTC()
	if err := stateStore.PutUpstream(model.ManagedUpstream{
		ID: upstreamID, BaseURL: baseURL, ManagementURL: "https://panel.example.com", TypeOverride: model.UpstreamTypeSub2API,
		Identities: map[string]model.UpstreamIdentity{}, CreatedAt: now, UpdatedAt: now,
	}); err != nil {
		t.Fatal(err)
	}

	manager := NewManager(stateStore, &managerLocalCore{}, box, 0, nil)
	if _, err := manager.Connect(context.Background(), upstreamID, ConnectInput{
		ManagementURLSet: true,
		Login:            LoginInput{Mode: model.UpstreamAuthToken, Token: "token"},
	}); err != nil {
		t.Fatal(err)
	}
	stored, err := stateStore.GetUpstream(upstreamID)
	if err != nil || stored.ManagementURL != "" || stored.EffectiveManagementURL() != baseURL {
		t.Fatalf("successful explicit clear did not restore API management fallback: upstream=%#v err=%v", stored, err)
	}
}

func TestManagerBindingsValidateCandidatesAndPreserveStaleBoundKey(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/v1/auth/me":
			writeTestJSON(t, w, http.StatusOK, map[string]any{"code": 0, "data": map[string]any{"id": 1}})
		case "/api/v1/groups/available":
			writeTestJSON(t, w, http.StatusOK, map[string]any{"code": 0, "data": []any{}})
		case "/api/v1/groups/rates":
			writeTestJSON(t, w, http.StatusOK, map[string]any{"code": 0, "data": map[string]any{}})
		case "/api/v1/keys":
			writeTestJSON(t, w, http.StatusOK, map[string]any{"code": 0, "data": map[string]any{"items": []any{}, "total": 0, "page": 1, "pages": 1}})
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()
	box, _ := NewCredentialBox(testCredentialKey())
	stateStore := managerTestStore(t)
	baseURL, _ := model.NormalizeUpstreamBaseURL(server.URL)
	upstreamID := model.StableUpstreamID(baseURL)
	envelope, _ := box.Encrypt(upstreamID, "identity", AuthMaterial{AccessToken: "token"})
	now := time.Now().UTC()
	if err := stateStore.PutUpstream(model.ManagedUpstream{
		ID: upstreamID, BaseURL: baseURL, TypeOverride: model.UpstreamTypeSub2API, CreatedAt: now, UpdatedAt: now,
		Identities: map[string]model.UpstreamIdentity{"identity": {
			ID: "identity", Credential: envelope, Status: model.IdentityStatusConnected, CreatedAt: now, UpdatedAt: now,
			Keys: map[string]model.RemoteKey{"remote": {ID: "remote", Name: "Remote", SyncedAt: now}},
		}},
	}); err != nil {
		t.Fatal(err)
	}
	local := &managerLocalCore{accounts: []model.UpstreamAccount{
		apiKeyAccount(7, "eligible", server.URL+"/v1"),
		apiKeyAccount(8, "other", "https://other.example/v1"),
	}}
	manager := NewManager(stateStore, local, box, 0, nil)
	if _, err := manager.SaveBindings(context.Background(), upstreamID, []BindingInput{{IdentityID: "identity", RemoteKeyID: "remote", LocalAccountID: 8}}); err == nil {
		t.Fatal("cross-upstream binding unexpectedly succeeded")
	}
	view, err := manager.SaveBindings(context.Background(), upstreamID, []BindingInput{{IdentityID: "identity", RemoteKeyID: "remote", LocalAccountID: 7}})
	if err != nil || view.Identities[0].Keys[0].LocalAccountID == nil || *view.Identities[0].Keys[0].LocalAccountID != 7 {
		t.Fatalf("valid binding failed: %#v err=%v", view, err)
	}
	if err := manager.SyncIdentity(context.Background(), upstreamID, "identity"); err != nil {
		t.Fatal(err)
	}
	updated, _ := stateStore.GetUpstream(upstreamID)
	stale := updated.Identities["identity"].Keys["remote"]
	if !stale.Stale || stale.LocalAccountID == nil || *stale.LocalAccountID != 7 {
		t.Fatalf("bound removed key was not retained as stale: %#v", stale)
	}
}

func TestManagerAutoMatchPropagatesStepUpAndMatchesUniqueFingerprints(t *testing.T) {
	box, _ := NewCredentialBox(testCredentialKey())
	stateStore := managerTestStore(t)
	baseURL := "https://upstream.example"
	upstreamID := model.StableUpstreamID(baseURL)
	fingerprint, _ := box.Fingerprint("sk-exact")
	now := time.Now().UTC()
	if err := stateStore.PutUpstream(model.ManagedUpstream{
		ID: upstreamID, BaseURL: baseURL, TypeOverride: model.UpstreamTypeSub2API, CreatedAt: now, UpdatedAt: now,
		Identities: map[string]model.UpstreamIdentity{"identity": {
			ID: "identity", Keys: map[string]model.RemoteKey{"remote": {
				ID: "remote", Name: "Remote", Fingerprint: fingerprint, MatchAvailable: true, SyncedAt: now,
			}},
		}},
	}); err != nil {
		t.Fatal(err)
	}
	local := &managerLocalCore{
		accounts:  []model.UpstreamAccount{apiKeyAccount(4, "local", baseURL+"/v1")},
		exportErr: &core.HTTPError{StatusCode: http.StatusForbidden, Code: "STEP_UP_REQUIRED", Message: "verification required"},
	}
	manager := NewManager(stateStore, local, box, 0, nil)
	forwarded := core.ForwardedIdentity{ClientIP: "203.0.113.9", UserAgent: "browser"}
	_, err := manager.AutoMatch(context.Background(), upstreamID, "admin-jwt", forwarded)
	var httpErr *core.HTTPError
	if !errors.As(err, &httpErr) || httpErr.Code != "STEP_UP_REQUIRED" {
		t.Fatalf("step-up error was not propagated: %T %v", err, err)
	}

	local.exportErr = nil
	local.secrets = map[int64]string{4: "sk-exact"}
	result, err := manager.AutoMatch(context.Background(), upstreamID, "admin-jwt", forwarded)
	if err != nil {
		t.Fatal(err)
	}
	if len(result.Matched) != 1 || result.Matched[0].LocalAccountID != 4 || local.exportedJWT != "admin-jwt" || local.exportedAgent != forwarded {
		t.Fatalf("unexpected match: %#v local=%#v", result, local)
	}
	stored, _ := stateStore.GetUpstream(upstreamID)
	bound := stored.Identities["identity"].Keys["remote"].LocalAccountID
	if bound == nil || *bound != 4 {
		t.Fatalf("match was not persisted: %#v", stored)
	}
}

func TestManagerAutoMatchDoesNotReuseAlreadyBoundLocalAccount(t *testing.T) {
	box, _ := NewCredentialBox(testCredentialKey())
	stateStore := managerTestStore(t)
	baseURL := "https://upstream.example"
	upstreamID := model.StableUpstreamID(baseURL)
	fingerprint, _ := box.Fingerprint("sk-exact")
	now := time.Now().UTC()
	boundAccountID := int64(4)
	if err := stateStore.PutUpstream(model.ManagedUpstream{
		ID: upstreamID, BaseURL: baseURL, TypeOverride: model.UpstreamTypeSub2API, CreatedAt: now, UpdatedAt: now,
		Identities: map[string]model.UpstreamIdentity{
			"first": {ID: "first", Keys: map[string]model.RemoteKey{
				"bound": {ID: "bound", Name: "Bound", LocalAccountID: &boundAccountID, SyncedAt: now},
			}},
			"second": {ID: "second", Keys: map[string]model.RemoteKey{
				"candidate": {ID: "candidate", Name: "Candidate", Fingerprint: fingerprint, MatchAvailable: true, SyncedAt: now},
			}},
		},
	}); err != nil {
		t.Fatal(err)
	}
	local := &managerLocalCore{
		accounts: []model.UpstreamAccount{apiKeyAccount(4, "local", baseURL+"/v1")},
		secrets:  map[int64]string{4: "sk-exact"},
	}
	manager := NewManager(stateStore, local, box, 0, nil)
	result, err := manager.AutoMatch(context.Background(), upstreamID, "admin-jwt", core.ForwardedIdentity{})
	if err != nil {
		t.Fatal(err)
	}
	if len(result.Matched) != 0 || len(result.Ambiguous) != 1 || result.Ambiguous[0].RemoteKeyID != "candidate" {
		t.Fatalf("already-bound account was reused: %#v", result)
	}
}
