package upstream

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api-account-auto-scheduler/internal/core"
	"github.com/Wei-Shaw/sub2api-account-auto-scheduler/internal/model"
)

type startReconcileCore struct {
	accounts []model.UpstreamAccount
	listed   chan struct{}
}

func (c *startReconcileCore) ListAccounts(context.Context) ([]model.UpstreamAccount, error) {
	select {
	case <-c.listed:
	default:
		close(c.listed)
	}
	return append([]model.UpstreamAccount(nil), c.accounts...), nil
}

func (c *startReconcileCore) ExportAPIKeySecrets(context.Context, []int64, string, core.ForwardedIdentity) (map[int64]string, error) {
	return nil, errors.New("unexpected export")
}

func TestManagerListClearsRemovedAndMovedLocalBindingsButRetainsUpstream(t *testing.T) {
	stateStore := managerTestStore(t)
	baseURL := "https://old.example"
	upstreamID := model.StableUpstreamID(baseURL)
	now := time.Now().UTC()
	deletedID := int64(1)
	movedID := int64(2)
	validID := int64(3)
	oauthID := int64(4)
	invalidURLID := int64(5)
	if err := stateStore.PutUpstream(model.ManagedUpstream{
		ID: upstreamID, Name: "Persisted", BaseURL: baseURL,
		RechargeRate: &model.UpstreamRechargeRate{CNYPerUSD: 0.2},
		CreatedAt:    now, UpdatedAt: now,
		Identities: map[string]model.UpstreamIdentity{"identity": {
			ID: "identity", CreatedAt: now, UpdatedAt: now,
			Keys: map[string]model.RemoteKey{
				"deleted":     {ID: "deleted", LocalAccountID: &deletedID},
				"moved":       {ID: "moved", LocalAccountID: &movedID},
				"valid":       {ID: "valid", LocalAccountID: &validID},
				"oauth":       {ID: "oauth", LocalAccountID: &oauthID},
				"invalid-url": {ID: "invalid-url", LocalAccountID: &invalidURLID},
			},
		}},
	}); err != nil {
		t.Fatal(err)
	}

	local := &managerLocalCore{accounts: []model.UpstreamAccount{
		apiKeyAccount(movedID, "moved", "https://new.example/v1"),
		apiKeyAccount(validID, "valid", baseURL+"/v1"),
		{ID: oauthID, Name: "oauth", Type: "oauth", Credentials: map[string]any{"base_url": baseURL + "/v1"}},
		apiKeyAccount(invalidURLID, "invalid", "not-a-url"),
	}}
	manager := NewManager(stateStore, local, &CredentialBox{}, 0, nil)
	result, err := manager.List(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(result.Upstreams) != 2 {
		t.Fatalf("upstreams=%#v, want persisted old root plus derived new root", result.Upstreams)
	}
	var persisted *UpstreamView
	for index := range result.Upstreams {
		if result.Upstreams[index].ID == upstreamID {
			persisted = &result.Upstreams[index]
		}
	}
	if persisted == nil || !persisted.Persisted || len(persisted.LocalAccounts) != 1 || persisted.LocalAccounts[0].ID != validID {
		t.Fatalf("persisted upstream was not retained with current accounts: %#v", persisted)
	}

	stored, err := stateStore.GetUpstream(upstreamID)
	if err != nil {
		t.Fatal(err)
	}
	keys := stored.Identities["identity"].Keys
	if keys["deleted"].LocalAccountID != nil || keys["moved"].LocalAccountID != nil ||
		keys["oauth"].LocalAccountID != nil || keys["invalid-url"].LocalAccountID != nil {
		t.Fatalf("invalid bindings were retained: %#v", keys)
	}
	if keys["valid"].LocalAccountID == nil || *keys["valid"].LocalAccountID != validID {
		t.Fatalf("valid binding was cleared: %#v", keys["valid"])
	}
	if stored.RechargeRate == nil || stored.RechargeRate.CNYPerUSD != 0.2 || len(stored.Identities) != 1 {
		t.Fatalf("persistent upstream state was damaged: %#v", stored)
	}
}

func TestManagerStartReconcilesBindingsWhenPeriodicSyncIsDisabled(t *testing.T) {
	stateStore := managerTestStore(t)
	baseURL := "https://startup.example"
	upstreamID := model.StableUpstreamID(baseURL)
	accountID := int64(9)
	now := time.Now().UTC()
	if err := stateStore.PutUpstream(model.ManagedUpstream{
		ID: upstreamID, BaseURL: baseURL, CreatedAt: now, UpdatedAt: now,
		Identities: map[string]model.UpstreamIdentity{"identity": {
			ID: "identity", Keys: map[string]model.RemoteKey{"key": {ID: "key", LocalAccountID: &accountID}},
		}},
	}); err != nil {
		t.Fatal(err)
	}
	local := &startReconcileCore{listed: make(chan struct{})}
	manager := NewManager(stateStore, local, &CredentialBox{}, 0, nil)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	manager.Start(ctx)

	select {
	case <-local.listed:
	case <-time.After(time.Second):
		t.Fatal("startup reconciliation did not list local accounts")
	}
	deadline := time.Now().Add(time.Second)
	for {
		stored, err := stateStore.GetUpstream(upstreamID)
		if err != nil {
			t.Fatal(err)
		}
		if stored.Identities["identity"].Keys["key"].LocalAccountID == nil {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("startup reconciliation did not clear the removed account binding")
		}
		time.Sleep(10 * time.Millisecond)
	}
}

func TestManagerListClearsFinalCostAfterBindingMoves(t *testing.T) {
	stateStore := managerTestStore(t)
	baseURL := "https://old.example"
	upstreamID := model.StableUpstreamID(baseURL)
	now := time.Now().UTC()
	accountID := int64(7)
	if err := stateStore.PutUpstream(model.ManagedUpstream{
		ID: upstreamID, BaseURL: baseURL, CreatedAt: now, UpdatedAt: now,
		Identities: map[string]model.UpstreamIdentity{"identity": {
			ID: "identity", Keys: map[string]model.RemoteKey{"key": {ID: "key", LocalAccountID: &accountID}},
		}},
	}); err != nil {
		t.Fatal(err)
	}
	account := apiKeyAccount(accountID, "moved", "https://new.example/v1")
	account.Extra = map[string]any{model.FinalCostMultiplierExtraKey: 0.16}
	local := &finalCostWriterCore{managerLocalCore: &managerLocalCore{accounts: []model.UpstreamAccount{account}}}
	manager := NewManager(stateStore, local, &CredentialBox{}, 0, nil)

	if _, err := manager.List(context.Background()); err != nil {
		t.Fatal(err)
	}
	if len(local.updates) != 1 || local.updates[0].accountID != accountID || local.updates[0].value != nil {
		t.Fatalf("moved account final cost was not cleared: %#v", local.updates)
	}
}

func TestManagerListDropsUnpersistedCandidateAfterLastAccountIsRemoved(t *testing.T) {
	local := &managerLocalCore{accounts: []model.UpstreamAccount{apiKeyAccount(1, "temporary", "https://temporary.example/v1")}}
	manager := NewManager(managerTestStore(t), local, &CredentialBox{}, 0, nil)

	first, err := manager.List(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(first.Upstreams) != 1 || first.Upstreams[0].Persisted {
		t.Fatalf("unexpected derived candidate: %#v", first.Upstreams)
	}

	local.accounts = nil
	second, err := manager.List(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(second.Upstreams) != 0 {
		t.Fatalf("unpersisted candidate survived account removal: %#v", second.Upstreams)
	}
}
