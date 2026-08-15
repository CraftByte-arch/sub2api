package upstream

import (
	"context"
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api-account-auto-scheduler/internal/model"
)

type balanceQuotaUpdateCall struct {
	accountID int64
	update    model.AccountBalanceQuotaUpdate
}

type balanceQuotaWriterCore struct {
	*managerLocalCore
	updates []balanceQuotaUpdateCall
}

func (f *balanceQuotaWriterCore) SetAccountBalanceQuota(_ context.Context, accountID int64, update model.AccountBalanceQuotaUpdate) error {
	copied := update
	if update.QuotaUsed != nil {
		value := *update.QuotaUsed
		copied.QuotaUsed = &value
	}
	if update.Remaining != nil {
		value := *update.Remaining
		copied.Remaining = &value
	}
	if update.ObservedAt != nil {
		value := *update.ObservedAt
		copied.ObservedAt = &value
	}
	f.updates = append(f.updates, balanceQuotaUpdateCall{accountID: accountID, update: copied})
	for index := range f.accounts {
		if f.accounts[index].ID != accountID {
			continue
		}
		limit := update.QuotaLimit
		f.accounts[index].QuotaLimit = &limit
		if update.QuotaUsed != nil {
			used := *update.QuotaUsed
			f.accounts[index].QuotaUsed = &used
		}
		if f.accounts[index].Extra == nil {
			f.accounts[index].Extra = make(map[string]any)
		}
		f.accounts[index].Extra[model.UpstreamBalanceQuotaManagedExtraKey] = update.Managed
		f.accounts[index].Extra[model.UpstreamBalanceQuotaRemainingExtraKey] = nil
		if update.Remaining != nil {
			f.accounts[index].Extra[model.UpstreamBalanceQuotaRemainingExtraKey] = *update.Remaining
		}
		f.accounts[index].Extra[model.UpstreamBalanceQuotaExhaustedExtraKey] = update.Exhausted
		f.accounts[index].Extra[model.UpstreamBalanceQuotaUnlimitedExtraKey] = update.Unlimited
		f.accounts[index].Extra[model.UpstreamBalanceQuotaObservedAtExtraKey] = nil
		if update.ObservedAt != nil {
			f.accounts[index].Extra[model.UpstreamBalanceQuotaObservedAtExtraKey] = update.ObservedAt.UTC().Format(time.RFC3339Nano)
		}
		return nil
	}
	return nil
}

func quotaUpdateByAccount(t *testing.T, calls []balanceQuotaUpdateCall, accountID int64) model.AccountBalanceQuotaUpdate {
	t.Helper()
	for _, call := range calls {
		if call.accountID == accountID {
			return call.update
		}
	}
	t.Fatalf("missing quota update for account %d: %#v", accountID, calls)
	return model.AccountBalanceQuotaUpdate{}
}

func putBalanceQuotaUpstream(t *testing.T, stateStore StateStore, baseURL string, balance model.UpstreamBalance, bindings map[string]struct {
	accountID  int64
	multiplier *float64
	stale      bool
}) {
	t.Helper()
	now := time.Now().UTC()
	keys := make(map[string]model.RemoteKey, len(bindings))
	for keyID, binding := range bindings {
		accountID := binding.accountID
		keys[keyID] = model.RemoteKey{
			ID: keyID, LocalAccountID: &accountID, Multiplier: binding.multiplier,
			Stale: binding.stale, SyncedAt: now,
		}
	}
	upstreamID := model.StableUpstreamID(baseURL)
	if err := stateStore.PutUpstream(model.ManagedUpstream{
		ID: upstreamID, BaseURL: baseURL, CreatedAt: now, UpdatedAt: now,
		Identities: map[string]model.UpstreamIdentity{"identity": {
			ID: "identity", Balance: &balance, Keys: keys, CreatedAt: now, UpdatedAt: now,
		}},
	}); err != nil {
		t.Fatal(err)
	}
}

func TestAccountBalanceQuotaFansOutFullIdentityBalancePerBoundAccount(t *testing.T) {
	stateStore := managerTestStore(t)
	baseURL := "https://balance.example"
	half := 0.5
	double := 2.0
	putBalanceQuotaUpstream(t, stateStore, baseURL, model.UpstreamBalance{
		Amount: 100, Unit: "USD", ObservedAt: time.Now().UTC(),
	}, map[string]struct {
		accountID  int64
		multiplier *float64
		stale      bool
	}{
		"half":   {accountID: 1, multiplier: &half},
		"double": {accountID: 2, multiplier: &double},
	})
	usedOne := 10.0
	usedTwo := 5.0
	local := &balanceQuotaWriterCore{managerLocalCore: &managerLocalCore{accounts: []model.UpstreamAccount{
		func() model.UpstreamAccount {
			account := apiKeyAccount(1, "one", baseURL+"/v1")
			account.QuotaUsed = &usedOne
			return account
		}(),
		func() model.UpstreamAccount {
			account := apiKeyAccount(2, "two", baseURL+"/v1")
			account.QuotaUsed = &usedTwo
			return account
		}(),
	}}}
	manager := NewManager(stateStore, local, &CredentialBox{}, 0, nil)
	if err := manager.reconcileLocalAccountState(context.Background()); err != nil {
		t.Fatal(err)
	}

	one := quotaUpdateByAccount(t, local.updates, 1)
	two := quotaUpdateByAccount(t, local.updates, 2)
	if one.Remaining == nil || *one.Remaining != 200 || one.QuotaLimit != 200 || one.QuotaUsed == nil || *one.QuotaUsed != 0 {
		t.Fatalf("account one did not receive full balance / 0.5: %#v", one)
	}
	if two.Remaining == nil || *two.Remaining != 50 || two.QuotaLimit != 50 || two.QuotaUsed == nil || *two.QuotaUsed != 0 {
		t.Fatalf("account two did not receive full balance / 2: %#v", two)
	}

	newUsedOne := 20.0
	local.accounts[0].QuotaUsed = &newUsedOne
	if err := manager.reconcileLocalAccountState(context.Background()); err != nil {
		t.Fatal(err)
	}
	if len(local.updates) != 2 {
		t.Fatalf("the same balance observation was reapplied after quota use increased: %#v", local.updates)
	}
}

func TestAccountBalanceQuotaZeroImmediatelyExhaustsAndPositiveBalanceRecovers(t *testing.T) {
	stateStore := managerTestStore(t)
	baseURL := "https://zero.example"
	multiplier := 1.0
	now := time.Now().UTC()
	putBalanceQuotaUpstream(t, stateStore, baseURL, model.UpstreamBalance{Amount: 0, Unit: "USD", ObservedAt: now}, map[string]struct {
		accountID  int64
		multiplier *float64
		stale      bool
	}{"key": {accountID: 1, multiplier: &multiplier}})
	local := &balanceQuotaWriterCore{managerLocalCore: &managerLocalCore{accounts: []model.UpstreamAccount{apiKeyAccount(1, "zero", baseURL+"/v1")}}}
	manager := NewManager(stateStore, local, &CredentialBox{}, 0, nil)
	if err := manager.reconcileLocalAccountState(context.Background()); err != nil {
		t.Fatal(err)
	}
	exhausted := quotaUpdateByAccount(t, local.updates, 1)
	if !exhausted.Exhausted || exhausted.QuotaUsed == nil || exhausted.QuotaLimit != accountQuotaExhaustedSentinel || *exhausted.QuotaUsed != accountQuotaExhaustedSentinel {
		t.Fatalf("zero balance was not made immediately exhausted: %#v", exhausted)
	}

	upstreamID := model.StableUpstreamID(baseURL)
	if err := stateStore.UpdateUpstream(upstreamID, func(upstream *model.ManagedUpstream) error {
		identity := upstream.Identities["identity"]
		identity.Balance = &model.UpstreamBalance{Amount: 50, Unit: "USD", ObservedAt: now.Add(time.Minute)}
		upstream.Identities["identity"] = identity
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	if err := manager.reconcileLocalAccountState(context.Background()); err != nil {
		t.Fatal(err)
	}
	recovered := local.updates[len(local.updates)-1].update
	if recovered.Exhausted || recovered.Remaining == nil || *recovered.Remaining != 50 ||
		recovered.QuotaLimit != 50 || recovered.QuotaUsed == nil || *recovered.QuotaUsed != 0 {
		t.Fatalf("positive balance did not recover account quota: %#v", recovered)
	}
}

func TestAccountBalanceQuotaZeroMultiplierIsUnlimited(t *testing.T) {
	stateStore := managerTestStore(t)
	baseURL := "https://free.example"
	zero := 0.0
	putBalanceQuotaUpstream(t, stateStore, baseURL, model.UpstreamBalance{Amount: 10, Unit: "USD", ObservedAt: time.Now().UTC()}, map[string]struct {
		accountID  int64
		multiplier *float64
		stale      bool
	}{"key": {accountID: 1, multiplier: &zero}})
	limit := 25.0
	account := apiKeyAccount(1, "free", baseURL+"/v1")
	account.QuotaLimit = &limit
	local := &balanceQuotaWriterCore{managerLocalCore: &managerLocalCore{accounts: []model.UpstreamAccount{account}}}
	manager := NewManager(stateStore, local, &CredentialBox{}, 0, nil)
	if err := manager.reconcileLocalAccountState(context.Background()); err != nil {
		t.Fatal(err)
	}
	update := quotaUpdateByAccount(t, local.updates, 1)
	if !update.Unlimited || update.QuotaLimit != 0 || update.QuotaUsed == nil || *update.QuotaUsed != 0 || update.Exhausted {
		t.Fatalf("zero multiplier was not treated as unlimited: %#v", update)
	}
}

func TestAccountBalanceQuotaSkipsNonAuthoritativeInputsAndClearsManagedUnboundLimit(t *testing.T) {
	stateStore := managerTestStore(t)
	baseURL := "https://skip.example"
	one := 1.0
	putBalanceQuotaUpstream(t, stateStore, baseURL, model.UpstreamBalance{
		Amount: 100, Unit: "quota", ObservedAt: time.Now().UTC(),
	}, map[string]struct {
		accountID  int64
		multiplier *float64
		stale      bool
	}{"raw": {accountID: 1, multiplier: &one}})
	managedLimit := 90.0
	managed := apiKeyAccount(2, "unbound", "https://other.example/v1")
	managed.QuotaLimit = &managedLimit
	managed.Extra = map[string]any{model.UpstreamBalanceQuotaManagedExtraKey: true}
	local := &balanceQuotaWriterCore{managerLocalCore: &managerLocalCore{accounts: []model.UpstreamAccount{
		apiKeyAccount(1, "raw", baseURL+"/v1"),
		managed,
	}}}
	manager := NewManager(stateStore, local, &CredentialBox{}, 0, nil)
	if err := manager.reconcileLocalAccountState(context.Background()); err != nil {
		t.Fatal(err)
	}
	if len(local.updates) != 1 || local.updates[0].accountID != 2 || local.updates[0].update.Managed || local.updates[0].update.QuotaLimit != 0 {
		t.Fatalf("raw balance was written or managed unbound quota was not cleared: %#v", local.updates)
	}
}

func TestBalanceQuotaProjectionRejectsStaleOrInvalidInputs(t *testing.T) {
	one := 1.0
	negative := -1.0
	now := time.Now().UTC()
	tests := []struct {
		name     string
		identity model.UpstreamIdentity
		key      model.RemoteKey
	}{
		{name: "missing balance", identity: model.UpstreamIdentity{}, key: model.RemoteKey{Multiplier: &one}},
		{name: "stale balance", identity: model.UpstreamIdentity{Balance: &model.UpstreamBalance{Amount: 10, Unit: "USD", Stale: true, ObservedAt: now}}, key: model.RemoteKey{Multiplier: &one}},
		{name: "raw quota", identity: model.UpstreamIdentity{Balance: &model.UpstreamBalance{Amount: 10, Unit: "quota", ObservedAt: now}}, key: model.RemoteKey{Multiplier: &one}},
		{name: "missing multiplier", identity: model.UpstreamIdentity{Balance: &model.UpstreamBalance{Amount: 10, Unit: "USD", ObservedAt: now}}, key: model.RemoteKey{}},
		{name: "negative multiplier", identity: model.UpstreamIdentity{Balance: &model.UpstreamBalance{Amount: 10, Unit: "USD", ObservedAt: now}}, key: model.RemoteKey{Multiplier: &negative}},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			projection := balanceQuotaProjection(boundBalanceQuotaSource{identity: test.identity, key: test.key})
			if projection.status == accountBalanceQuotaStatusAvailable {
				t.Fatalf("non-authoritative input produced quota: %#v", projection)
			}
		})
	}
}

func TestAccountBalanceQuotaRecordsNewObservationEvenWhenLimitIsUnchanged(t *testing.T) {
	oldObservedAt := time.Now().UTC().Add(-time.Minute)
	newObservedAt := oldObservedAt.Add(time.Minute)
	limit := 100.0
	remaining := 100.0
	account := apiKeyAccount(1, "account", "https://example.com/v1")
	account.QuotaLimit = &limit
	account.Extra = map[string]any{
		model.UpstreamBalanceQuotaManagedExtraKey:    true,
		model.UpstreamBalanceQuotaRemainingExtraKey:  remaining,
		model.UpstreamBalanceQuotaObservedAtExtraKey: oldObservedAt.Format(time.RFC3339Nano),
	}
	update := model.AccountBalanceQuotaUpdate{
		QuotaLimit: limit,
		Managed:    true,
		Remaining:  &remaining,
		ObservedAt: &newObservedAt,
	}
	if !accountBalanceQuotaUpdateNeeded(account, update) {
		t.Fatal("a new authoritative balance observation was not recorded")
	}
}
