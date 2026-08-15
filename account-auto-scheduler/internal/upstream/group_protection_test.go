package upstream

import (
	"context"
	"errors"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api-account-auto-scheduler/internal/core"
	"github.com/Wei-Shaw/sub2api-account-auto-scheduler/internal/model"
	"github.com/Wei-Shaw/sub2api-account-auto-scheduler/internal/store"
)

type protectionCore struct {
	mu         sync.Mutex
	accounts   []model.UpstreamAccount
	bindErrors map[bool]error
	mutations  []bool
}

func (f *protectionCore) ListAccounts(context.Context) ([]model.UpstreamAccount, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	result := make([]model.UpstreamAccount, len(f.accounts))
	copy(result, f.accounts)
	for index := range result {
		result[index].GroupIDs = append([]int64(nil), result[index].GroupIDs...)
	}
	return result, nil
}

func (f *protectionCore) ExportAPIKeySecrets(context.Context, []int64, string, core.ForwardedIdentity) (map[int64]string, error) {
	return nil, errors.New("unexpected export")
}

func (f *protectionCore) SetAccountGroup(_ context.Context, accountID, groupID int64, bound bool) (model.UpstreamAccount, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.mutations = append(f.mutations, bound)
	if err := f.bindErrors[bound]; err != nil {
		return model.UpstreamAccount{}, err
	}
	for index := range f.accounts {
		if f.accounts[index].ID != accountID {
			continue
		}
		current := containsGroupID(f.accounts[index].GroupIDs, groupID)
		if bound && !current {
			f.accounts[index].GroupIDs = append(f.accounts[index].GroupIDs, groupID)
		}
		if !bound && current {
			filtered := make([]int64, 0, len(f.accounts[index].GroupIDs))
			for _, id := range f.accounts[index].GroupIDs {
				if id != groupID {
					filtered = append(filtered, id)
				}
			}
			f.accounts[index].GroupIDs = filtered
		}
		result := f.accounts[index]
		result.GroupIDs = append([]int64(nil), result.GroupIDs...)
		return result, nil
	}
	return model.UpstreamAccount{}, errors.New("account not found")
}

func newProtectionManager(t *testing.T, finalGroupMultiplier float64) (*Manager, *store.Store, *protectionCore, string) {
	t.Helper()
	stateStore, err := store.Open(filepath.Join(t.TempDir(), "state.json"))
	if err != nil {
		t.Fatal(err)
	}
	baseURL := "https://guard.example"
	upstreamID := model.StableUpstreamID(baseURL)
	accountID := int64(7)
	now := time.Now().UTC()
	multiplier := finalGroupMultiplier
	if err := stateStore.PutUpstream(model.ManagedUpstream{
		ID: upstreamID, BaseURL: baseURL,
		RechargeRate: &model.UpstreamRechargeRate{CNYPerUSD: 0.2},
		Identities: map[string]model.UpstreamIdentity{
			"identity": {
				ID: "identity",
				Keys: map[string]model.RemoteKey{
					"remote": {ID: "remote", LocalAccountID: &accountID, Multiplier: &multiplier, SyncedAt: now},
				},
			},
		},
		CreatedAt: now, UpdatedAt: now,
	}); err != nil {
		t.Fatal(err)
	}
	local := &protectionCore{
		accounts: []model.UpstreamAccount{{
			ID: accountID, Name: "protected", Type: "apikey", Platform: "openai", Status: "active", Schedulable: true,
			GroupIDs: []int64{11}, Credentials: map[string]any{"base_url": baseURL + "/v1"},
		}},
		bindErrors: map[bool]error{},
	}
	return NewManager(stateStore, local, &CredentialBox{}, 0, nil), stateStore, local, upstreamID
}

func setRemoteMultiplier(t *testing.T, stateStore *store.Store, upstreamID string, value *float64) {
	t.Helper()
	if err := stateStore.UpdateUpstream(upstreamID, func(upstream *model.ManagedUpstream) error {
		identity := upstream.Identities["identity"]
		key := identity.Keys["remote"]
		key.Multiplier = value
		identity.Keys["remote"] = key
		upstream.Identities["identity"] = identity
		return nil
	}); err != nil {
		t.Fatal(err)
	}
}

func TestProtectionAutomaticallyUnbindsAndRebindsAtThreshold(t *testing.T) {
	manager, stateStore, local, upstreamID := newProtectionManager(t, 1)
	view, err := manager.SetGroupAccountProtection(context.Background(), 11, 7, 0.15)
	if err != nil {
		t.Fatal(err)
	}
	if view.Status != model.ProtectionExceeded || view.PhysicalBound {
		t.Fatalf("unexpected exceeded view: %#v", view)
	}
	accounts, _ := local.ListAccounts(context.Background())
	if containsGroupID(accounts[0].GroupIDs, 11) {
		t.Fatalf("account remained physically bound: %#v", accounts[0].GroupIDs)
	}
	logical := manager.LogicalGroupIDs(accounts)
	if len(logical[7]) != 1 || logical[7][0] != 11 {
		t.Fatalf("protected logical membership disappeared: %#v", logical)
	}

	// Equality is safe: 0.2 recharge × 0.75 group = 0.15 protection.
	safeMultiplier := 0.75
	setRemoteMultiplier(t, stateStore, upstreamID, &safeMultiplier)
	if err := manager.reconcileLocalAccountState(context.Background()); err != nil {
		t.Fatal(err)
	}
	accounts, _ = local.ListAccounts(context.Background())
	if !containsGroupID(accounts[0].GroupIDs, 11) {
		t.Fatalf("account was not rebound at threshold equality: %#v", accounts[0].GroupIDs)
	}
	stored, err := stateStore.GetProtection(11, 7)
	if err != nil || stored.Status != model.ProtectionBound || !stored.PhysicalBound {
		t.Fatalf("unexpected recovered protection: %#v err=%v", stored, err)
	}
}

func TestProtectionKeepsPhysicalStateWhenMultiplierUnavailable(t *testing.T) {
	manager, stateStore, local, upstreamID := newProtectionManager(t, 1)
	if _, err := manager.SetGroupAccountProtection(context.Background(), 11, 7, 0.25); err != nil {
		t.Fatal(err)
	}
	setRemoteMultiplier(t, stateStore, upstreamID, nil)
	if err := manager.reconcileLocalAccountState(context.Background()); err != nil {
		t.Fatal(err)
	}
	accounts, _ := local.ListAccounts(context.Background())
	if !containsGroupID(accounts[0].GroupIDs, 11) {
		t.Fatal("unknown multiplier changed a safe physical binding")
	}
	stored, err := stateStore.GetProtection(11, 7)
	if err != nil || stored.Status != model.ProtectionUnavailable || !stored.PhysicalBound {
		t.Fatalf("unexpected unavailable state: %#v err=%v", stored, err)
	}
}

func TestManualReleaseBindsBeforeDeletingProtection(t *testing.T) {
	manager, stateStore, local, _ := newProtectionManager(t, 1)
	if _, err := manager.SetGroupAccountProtection(context.Background(), 11, 7, 0.15); err != nil {
		t.Fatal(err)
	}
	if _, err := manager.ReleaseGroupAccountProtection(context.Background(), 11, 7); err != nil {
		t.Fatal(err)
	}
	if _, err := stateStore.GetProtection(11, 7); !errors.Is(err, store.ErrProtectionNotFound) {
		t.Fatalf("protection survived release: %v", err)
	}
	accounts, _ := local.ListAccounts(context.Background())
	if !containsGroupID(accounts[0].GroupIDs, 11) {
		t.Fatal("release did not restore physical binding")
	}
}

func TestManualRemoveOnExceededRelationshipCannotRebindLater(t *testing.T) {
	manager, stateStore, local, upstreamID := newProtectionManager(t, 1)
	if _, err := manager.SetGroupAccountProtection(context.Background(), 11, 7, 0.15); err != nil {
		t.Fatal(err)
	}
	if err := manager.RemoveGroupAccountBinding(context.Background(), 11, 7); err != nil {
		t.Fatal(err)
	}
	if _, err := stateStore.GetProtection(11, 7); !errors.Is(err, store.ErrProtectionNotFound) {
		t.Fatalf("logical protection survived failed remove: %v", err)
	}

	lowerMultiplier := 0.25
	setRemoteMultiplier(t, stateStore, upstreamID, &lowerMultiplier)
	if err := manager.reconcileLocalAccountState(context.Background()); err != nil {
		t.Fatal(err)
	}
	accounts, _ := local.ListAccounts(context.Background())
	if containsGroupID(accounts[0].GroupIDs, 11) {
		t.Fatal("removed relationship was automatically rebound")
	}
}

func TestManualRemoveDeletesProtectionBeforeFailedPhysicalUnbind(t *testing.T) {
	manager, stateStore, local, _ := newProtectionManager(t, 0.5)
	if _, err := manager.SetGroupAccountProtection(context.Background(), 11, 7, 0.15); err != nil {
		t.Fatal(err)
	}
	local.bindErrors[false] = errors.New("simulated unbind failure")
	if err := manager.RemoveGroupAccountBinding(context.Background(), 11, 7); err == nil {
		t.Fatal("expected physical unbind failure")
	}
	if _, err := stateStore.GetProtection(11, 7); !errors.Is(err, store.ErrProtectionNotFound) {
		t.Fatalf("logical protection survived failed physical unbind: %v", err)
	}
}

func TestLegacySingleBindingRouteCannotBypassExceededProtection(t *testing.T) {
	manager, _, local, _ := newProtectionManager(t, 1)
	if _, err := manager.SetGroupAccountProtection(context.Background(), 11, 7, 0.15); err != nil {
		t.Fatal(err)
	}
	if err := manager.SetGroupAccountBinding(context.Background(), 11, 7, true); err == nil {
		t.Fatal("legacy bind route bypassed rate protection")
	}
	accounts, _ := local.ListAccounts(context.Background())
	if containsGroupID(accounts[0].GroupIDs, 11) {
		t.Fatal("account was physically rebound despite protection")
	}
}

func TestConcurrentProtectionReconciliationIsSerialized(t *testing.T) {
	manager, stateStore, local, upstreamID := newProtectionManager(t, 0.5)
	if _, err := manager.SetGroupAccountProtection(context.Background(), 11, 7, 0.15); err != nil {
		t.Fatal(err)
	}
	highMultiplier := 1.0
	setRemoteMultiplier(t, stateStore, upstreamID, &highMultiplier)
	var wait sync.WaitGroup
	errorsCh := make(chan error, 8)
	for index := 0; index < 8; index++ {
		wait.Add(1)
		go func() {
			defer wait.Done()
			errorsCh <- manager.reconcileLocalAccountState(context.Background())
		}()
	}
	wait.Wait()
	close(errorsCh)
	for err := range errorsCh {
		if err != nil {
			t.Fatal(err)
		}
	}
	accounts, _ := local.ListAccounts(context.Background())
	stored, err := stateStore.GetProtection(11, 7)
	if err != nil || containsGroupID(accounts[0].GroupIDs, 11) || stored.Status != model.ProtectionExceeded {
		t.Fatalf("concurrent reconciliation produced an invalid state: groups=%#v protection=%#v err=%v", accounts[0].GroupIDs, stored, err)
	}
}
