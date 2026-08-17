package upstream

import (
	"context"
	"errors"
	"net/http"
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api-account-auto-scheduler/internal/model"
	"github.com/Wei-Shaw/sub2api-account-auto-scheduler/internal/store"
)

func TestManagerDeleteUnusedUpstreamAndRediscoverReusedAddress(t *testing.T) {
	stateStore := managerTestStore(t)
	baseURL := "https://unused.example"
	upstreamID := model.StableUpstreamID(baseURL)
	now := time.Now().UTC()
	if err := stateStore.PutUpstream(model.ManagedUpstream{
		ID: upstreamID, Name: "Saved upstream", BaseURL: baseURL,
		TypeOverride: model.UpstreamTypeSub2API,
		RechargeRate: &model.UpstreamRechargeRate{
			InputMode: model.RechargeRateUSDPerCNY, InputValue: 5, CNYPerUSD: 0.2, UpdatedAt: now,
		},
		Identities: map[string]model.UpstreamIdentity{"identity": {
			ID: "identity", Label: "Primary", Credential: model.CredentialEnvelope{Version: 1, Nonce: "nonce", Ciphertext: "ciphertext"},
			Keys: map[string]model.RemoteKey{"key": {ID: "key", Name: "Remote key"}}, CreatedAt: now, UpdatedAt: now,
		}},
		CreatedAt: now, UpdatedAt: now,
	}); err != nil {
		t.Fatal(err)
	}
	local := &managerLocalCore{}
	manager := NewManager(stateStore, local, &CredentialBox{}, 0, nil)

	if err := manager.Delete(context.Background(), upstreamID); err != nil {
		t.Fatal(err)
	}
	if _, err := stateStore.GetUpstream(upstreamID); !errors.Is(err, store.ErrUpstreamNotFound) {
		t.Fatalf("deleted upstream still persisted: %v", err)
	}
	withoutAccount, err := manager.List(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(withoutAccount.Upstreams) != 0 {
		t.Fatalf("deleted unused upstream still listed: %#v", withoutAccount.Upstreams)
	}

	local.accounts = []model.UpstreamAccount{apiKeyAccount(17, "reused", baseURL+"/v1")}
	reappeared, err := manager.List(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(reappeared.Upstreams) != 1 {
		t.Fatalf("reused address did not reappear: %#v", reappeared.Upstreams)
	}
	view := reappeared.Upstreams[0]
	if view.ID != upstreamID || view.Persisted || len(view.LocalAccounts) != 1 || len(view.Identities) != 0 || view.RechargeRate != nil {
		t.Fatalf("reappeared upstream restored deleted sidecar state: %#v", view)
	}
}

func TestManagerDeleteRejectsAddressUsedByLocalAPIKeyAccount(t *testing.T) {
	stateStore := managerTestStore(t)
	baseURL := "https://active.example"
	upstreamID := model.StableUpstreamID(baseURL)
	now := time.Now().UTC()
	if err := stateStore.PutUpstream(model.ManagedUpstream{
		ID: upstreamID, Name: "Active upstream", BaseURL: baseURL,
		RechargeRate: &model.UpstreamRechargeRate{CNYPerUSD: 0.2},
		Identities:   map[string]model.UpstreamIdentity{"identity": {ID: "identity", Keys: map[string]model.RemoteKey{}}},
		CreatedAt:    now, UpdatedAt: now,
	}); err != nil {
		t.Fatal(err)
	}
	local := &managerLocalCore{accounts: []model.UpstreamAccount{apiKeyAccount(9, "active", baseURL+"/v1")}}
	manager := NewManager(stateStore, local, &CredentialBox{}, 0, nil)

	err := manager.Delete(context.Background(), upstreamID)
	failure := AsAdapterError(err)
	if failure.Code != "UPSTREAM_IN_USE" || failure.HTTPCode != http.StatusConflict {
		t.Fatalf("delete error=%#v, want UPSTREAM_IN_USE conflict", failure)
	}
	stored, err := stateStore.GetUpstream(upstreamID)
	if err != nil || stored.Name != "Active upstream" || stored.RechargeRate == nil || len(stored.Identities) != 1 {
		t.Fatalf("in-use upstream was changed: upstream=%#v err=%v", stored, err)
	}
}

func TestManagerDeleteFailsClosedWhenAccountsCannotBeListed(t *testing.T) {
	stateStore := managerTestStore(t)
	baseURL := "https://unknown.example"
	upstreamID := model.StableUpstreamID(baseURL)
	now := time.Now().UTC()
	if err := stateStore.PutUpstream(model.ManagedUpstream{
		ID: upstreamID, Name: "Unknown", BaseURL: baseURL, Identities: map[string]model.UpstreamIdentity{}, CreatedAt: now, UpdatedAt: now,
	}); err != nil {
		t.Fatal(err)
	}
	listErr := errors.New("account listing unavailable")
	manager := NewManager(stateStore, &managerLocalCore{listErr: listErr}, &CredentialBox{}, 0, nil)

	if err := manager.Delete(context.Background(), upstreamID); !errors.Is(err, listErr) {
		t.Fatalf("delete error=%v, want account-list failure", err)
	}
	if _, err := stateStore.GetUpstream(upstreamID); err != nil {
		t.Fatalf("upstream was deleted after account-list failure: %v", err)
	}
}
