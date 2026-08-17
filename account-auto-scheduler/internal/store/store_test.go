package store

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api-account-auto-scheduler/internal/model"
)

func TestStorePersistsAtomicallyWithPrivatePermissions(t *testing.T) {
	path := filepath.Join(t.TempDir(), "nested", "state.json")
	stateStore, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	account := model.ManagedAccount{AccountID: 7, Name: "test", Policy: model.DefaultPolicy(), CreatedAt: time.Now(), UpdatedAt: time.Now()}
	if err := stateStore.Put(account); err != nil {
		t.Fatal(err)
	}

	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if permissions := info.Mode().Perm(); permissions != 0o600 {
		t.Fatalf("state permissions = %o, want 600", permissions)
	}
	reloaded, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	stored, err := reloaded.Get(7)
	if err != nil {
		t.Fatal(err)
	}
	if stored.Name != "test" || stored.Policy.IntervalSeconds != 60 {
		t.Fatalf("unexpected reloaded state: %#v", stored)
	}
}

func TestFailedUpdateDoesNotMutateState(t *testing.T) {
	stateStore, err := Open(filepath.Join(t.TempDir(), "state.json"))
	if err != nil {
		t.Fatal(err)
	}
	if err := stateStore.Put(model.ManagedAccount{AccountID: 3, Name: "before"}); err != nil {
		t.Fatal(err)
	}
	expected := os.ErrPermission
	if err := stateStore.Update(3, func(account *model.ManagedAccount) error {
		account.Name = "after"
		return expected
	}); err != expected {
		t.Fatalf("Update error = %v, want %v", err, expected)
	}
	stored, err := stateStore.Get(3)
	if err != nil {
		t.Fatal(err)
	}
	if stored.Name != "before" {
		t.Fatalf("failed update mutated state: %#v", stored)
	}
}

func TestStoreMigratesVersionOneOnNextMutation(t *testing.T) {
	path := filepath.Join(t.TempDir(), "state.json")
	legacy := map[string]any{
		"version": 1,
		"accounts": map[string]any{
			"9": map[string]any{"account_id": 9, "name": "legacy", "history": []any{}},
		},
	}
	raw, err := json.Marshal(legacy)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, raw, 0o600); err != nil {
		t.Fatal(err)
	}

	stateStore, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	if account, err := stateStore.Get(9); err != nil || account.Name != "legacy" {
		t.Fatalf("legacy account was not retained: %#v, %v", account, err)
	}
	beforeMutation, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var before struct {
		Version int `json:"version"`
	}
	if err := json.Unmarshal(beforeMutation, &before); err != nil || before.Version != 1 {
		t.Fatalf("open rewrote legacy state unexpectedly: version=%d err=%v", before.Version, err)
	}

	now := time.Now().UTC()
	upstream := model.ManagedUpstream{
		ID:         "up_test",
		Name:       "Test",
		BaseURL:    "https://example.com",
		Identities: map[string]model.UpstreamIdentity{},
		CreatedAt:  now,
		UpdatedAt:  now,
	}
	if err := stateStore.PutUpstream(upstream); err != nil {
		t.Fatal(err)
	}
	afterMutation, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var migrated model.State
	if err := json.Unmarshal(afterMutation, &migrated); err != nil {
		t.Fatal(err)
	}
	if migrated.Version != model.StateVersion || migrated.Accounts["9"].Name != "legacy" || migrated.Upstreams["up_test"].BaseURL == "" {
		t.Fatalf("unexpected migrated state: %#v", migrated)
	}
}

func TestStoreMigratesVersionTwoToLegacyDirectProbeDefaults(t *testing.T) {
	path := filepath.Join(t.TempDir(), "state.json")
	legacy := map[string]any{
		"version": 2,
		"accounts": map[string]any{
			"11": map[string]any{
				"account_id": 11,
				"name":       "v2 account",
				"history":    []any{},
				"policy":     model.DefaultPolicy(),
			},
		},
		"upstreams": map[string]any{
			"up_existing": map[string]any{"id": "up_existing", "name": "existing", "identities": map[string]any{}},
		},
	}
	raw, err := json.Marshal(legacy)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, raw, 0o600); err != nil {
		t.Fatal(err)
	}

	stateStore, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	account, err := stateStore.Get(11)
	if err != nil {
		t.Fatal(err)
	}
	if account.EffectiveProbeSource() != model.ProbeSourceLegacy || account.DirectProbe != nil {
		t.Fatalf("v2 state did not initialize direct probe as legacy: %#v", account)
	}
	if account.PublicView().Probe.AuthorizationState != model.DirectProbeAuthorizationMissing {
		t.Fatalf("v2 public probe status is not legacy: %#v", account.PublicView().Probe)
	}
	if _, err := stateStore.GetUpstream("up_existing"); err != nil {
		t.Fatalf("v2 upstream state was not retained: %v", err)
	}

	if err := stateStore.Update(11, func(current *model.ManagedAccount) error {
		current.Name = "migrated"
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	updatedRaw, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var updated model.State
	if err := json.Unmarshal(updatedRaw, &updated); err != nil {
		t.Fatal(err)
	}
	if updated.Version != model.StateVersion || updated.Accounts["11"].Name != "migrated" || updated.Upstreams["up_existing"].Name != "existing" {
		t.Fatalf("unexpected v3 migration result: %#v", updated)
	}
}

func TestStoreDeepCopiesDirectProbeConfig(t *testing.T) {
	stateStore, err := Open(filepath.Join(t.TempDir(), "state.json"))
	if err != nil {
		t.Fatal(err)
	}
	now := time.Now().UTC()
	managed := model.ManagedAccount{
		AccountID:   19,
		Name:        "direct",
		Policy:      model.DefaultPolicy(),
		ProbeSource: model.ProbeSourceDirect,
		DirectProbe: &model.DirectProbeConfig{
			Credential:         model.CredentialEnvelope{Version: 1, Nonce: "original-nonce", Ciphertext: "original-ciphertext"},
			AuthorizationState: model.DirectProbeAuthorized,
			ImportedAt:         &now,
			UpdatedAt:          &now,
			RoutingFingerprint: "original-fingerprint",
		},
		CreatedAt: now,
		UpdatedAt: now,
	}
	if err := stateStore.Put(managed); err != nil {
		t.Fatal(err)
	}

	first, err := stateStore.Get(19)
	if err != nil {
		t.Fatal(err)
	}
	first.DirectProbe.Credential.Ciphertext = "mutated"
	first.DirectProbe.ImportedAt = nil
	first.DirectProbe.RoutingFingerprint = "mutated"

	second, err := stateStore.Get(19)
	if err != nil {
		t.Fatal(err)
	}
	if second.DirectProbe == nil || second.DirectProbe.Credential.Ciphertext != "original-ciphertext" || second.DirectProbe.ImportedAt == nil || second.DirectProbe.RoutingFingerprint != "original-fingerprint" {
		t.Fatalf("store state was mutated through direct-probe clone: %#v", second.DirectProbe)
	}
}

func TestStoreMigratesVersionThreeWithEmptyProtectionState(t *testing.T) {
	path := filepath.Join(t.TempDir(), "state.json")
	raw, err := json.Marshal(map[string]any{
		"version": 3,
		"accounts": map[string]any{
			"31": map[string]any{"account_id": 31, "name": "v3", "history": []any{}},
		},
		"upstreams": map[string]any{},
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, raw, 0o600); err != nil {
		t.Fatal(err)
	}

	stateStore, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	if protections := stateStore.ListProtections(); len(protections) != 0 {
		t.Fatalf("legacy v3 protections = %#v, want empty", protections)
	}
	now := time.Now().UTC()
	protection := model.GroupAccountProtection{
		GroupID: 9, AccountID: 31, ProtectionMultiplier: 0.16,
		Status: model.ProtectionBound, PhysicalBound: true, CreatedAt: now, UpdatedAt: now,
	}
	if err := stateStore.PutProtection(protection); err != nil {
		t.Fatal(err)
	}
	reloaded, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	stored, err := reloaded.GetProtection(9, 31)
	if err != nil || stored.ProtectionMultiplier != 0.16 || !stored.PhysicalBound || stored.EffectiveScope() != model.ProtectionScopeAccount {
		t.Fatalf("protection was not persisted: %#v err=%v", stored, err)
	}
}

func TestStoreMigratesVersionFourAndPersistsNotificationState(t *testing.T) {
	path := filepath.Join(t.TempDir(), "state.json")
	raw, err := json.Marshal(map[string]any{
		"version": 4,
		"accounts": map[string]any{
			"41": map[string]any{"account_id": 41, "name": "v4", "history": []any{}},
		},
		"upstreams":                 map[string]any{},
		"group_account_protections": map[string]any{},
		"group_protection_defaults": map[string]any{},
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, raw, 0o600); err != nil {
		t.Fatal(err)
	}
	stateStore, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := stateStore.GetGroupBalanceThreshold(9); ok {
		t.Fatal("legacy v4 state unexpectedly contained a balance threshold")
	}
	threshold := 50.0
	if err := stateStore.PutGroupBalanceThreshold(9, &threshold); err != nil {
		t.Fatal(err)
	}
	now := time.Now().UTC()
	settings := model.NotificationSettings{
		Enabled:           true,
		BarkEndpoint:      "https://bark.example.com",
		BarkBasicAuthUser: "bark-user",
		BarkCredentials:   model.CredentialEnvelope{Version: 1, Nonce: "nonce", Ciphertext: "ciphertext"},
		LastDeliveryAt:    &now,
		UpdatedAt:         now,
	}
	if err := stateStore.PutNotificationSettings(settings); err != nil {
		t.Fatal(err)
	}
	if err := stateStore.PutBalanceAlertState("balance:9:41", model.BalanceAlertState{Configured: true, Below: true, Threshold: 50, LastAvailable: float64Pointer(10), UpdatedAt: now}); err != nil {
		t.Fatal(err)
	}

	reloaded, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	if got, ok := reloaded.GetGroupBalanceThreshold(9); !ok || got != 50 {
		t.Fatalf("group threshold = %v, configured=%v", got, ok)
	}
	loadedSettings := reloaded.GetNotificationSettings()
	if !loadedSettings.Enabled || loadedSettings.BarkCredentials.Ciphertext != "ciphertext" || loadedSettings.LastDeliveryAt == nil {
		t.Fatalf("notification settings were not retained: %#v", loadedSettings)
	}
	loadedSettings.BarkCredentials.Ciphertext = "mutated"
	loadedSettings.LastDeliveryAt = nil
	again := reloaded.GetNotificationSettings()
	if again.BarkCredentials.Ciphertext != "ciphertext" || again.LastDeliveryAt == nil {
		t.Fatalf("notification settings were not deep-copied: %#v", again)
	}
	stateRaw, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var persisted model.State
	if err := json.Unmarshal(stateRaw, &persisted); err != nil {
		t.Fatal(err)
	}
	if persisted.Version != model.StateVersion {
		t.Fatalf("persisted version = %d, want %d", persisted.Version, model.StateVersion)
	}
}

func float64Pointer(value float64) *float64 { return &value }

func TestStorePersistsGroupProtectionDefaultsAndMigratesMissingField(t *testing.T) {
	path := filepath.Join(t.TempDir(), "state.json")
	legacy, err := json.Marshal(map[string]any{
		"version":   4,
		"accounts":  map[string]any{},
		"upstreams": map[string]any{},
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, legacy, 0o600); err != nil {
		t.Fatal(err)
	}
	stateStore, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	if defaults := stateStore.ListGroupProtectionDefaults(); len(defaults) != 0 {
		t.Fatalf("missing group defaults should load as empty: %#v", defaults)
	}
	now := time.Now().UTC()
	setting := model.GroupProtectionDefault{GroupID: 17, ProtectionMultiplier: 0.24, CreatedAt: now, UpdatedAt: now}
	if err := stateStore.PutGroupProtectionDefault(setting); err != nil {
		t.Fatal(err)
	}
	got, err := stateStore.GetGroupProtectionDefault(17)
	if err != nil || got.ProtectionMultiplier != 0.24 {
		t.Fatalf("group default was not persisted: %#v err=%v", got, err)
	}
	if err := stateStore.DeleteGroupProtectionDefault(17); err != nil {
		t.Fatal(err)
	}
	if _, err := stateStore.GetGroupProtectionDefault(17); !errors.Is(err, ErrGroupProtectionDefaultNotFound) {
		t.Fatalf("deleted group default still exists: %v", err)
	}
}

func TestStorePersistsAndDeepCopiesDetectionStatistics(t *testing.T) {
	path := filepath.Join(t.TempDir(), "state.json")
	stateStore, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	now := time.Now().UTC()
	knownCost := &model.ProbeCost{Amount: 0.0012, Currency: "USD", Known: true}
	account := model.ManagedAccount{
		AccountID:       42,
		LastFailureKind: model.CheckFailureBalanceInsufficient,
		History: []model.CheckResult{{
			ID: "check", CheckedAt: now, FailureKind: model.CheckFailureBalanceInsufficient,
			Usage: &model.ProbeUsage{Model: "gpt-test", InputTokens: 10, OutputTokens: 2},
			Cost:  knownCost,
		}},
		DetectionStats: model.DetectionStats{Requests: 1, InputTokens: 10, OutputTokens: 2, KnownCost: 0.0012, KnownCostChecks: 1, LastCost: knownCost, LastUsageAt: &now},
		CreatedAt:      now, UpdatedAt: now,
	}
	if err := stateStore.Put(account); err != nil {
		t.Fatal(err)
	}
	first, err := stateStore.Get(42)
	if err != nil {
		t.Fatal(err)
	}
	first.History[0].Usage.InputTokens = 999
	first.History[0].Cost.Amount = 999
	first.DetectionStats.LastCost.Amount = 999
	second, err := stateStore.Get(42)
	if err != nil {
		t.Fatal(err)
	}
	if second.History[0].Usage.InputTokens != 10 || second.History[0].Cost.Amount != 0.0012 || second.DetectionStats.LastCost.Amount != 0.0012 {
		t.Fatalf("store detection state was mutated through a clone: %#v", second)
	}
	reloaded, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	stored, err := reloaded.Get(42)
	if err != nil || stored.DetectionStats.Requests != 1 || stored.DetectionStats.KnownCost != 0.0012 ||
		stored.LastFailureKind != model.CheckFailureBalanceInsufficient || stored.History[0].FailureKind != model.CheckFailureBalanceInsufficient {
		t.Fatalf("detection state was not restored: %#v err=%v", stored, err)
	}
}

func TestUpstreamStoreReturnsDeepCopies(t *testing.T) {
	stateStore, err := Open(filepath.Join(t.TempDir(), "state.json"))
	if err != nil {
		t.Fatal(err)
	}
	accountID := int64(17)
	quota := 20.0
	rawBalance := 500000.0
	quotaPerUnit := 500000.0
	rechargeRate := &model.UpstreamRechargeRate{InputMode: model.RechargeRateUSDPerCNY, InputValue: 5, CNYPerUSD: 0.2}
	upstream := model.ManagedUpstream{
		ID:           "up_copy",
		BaseURL:      "https://example.com",
		RechargeRate: rechargeRate,
		Identities: map[string]model.UpstreamIdentity{
			"identity": {
				ID: "identity",
				Balance: &model.UpstreamBalance{
					Amount: 1, Unit: "USD", Source: "newapi", ObservedAt: time.Now().UTC(),
					RawQuota: &rawBalance, QuotaPerUnit: &quotaPerUnit,
				},
				Keys: map[string]model.RemoteKey{
					"key": {ID: "key", LocalAccountID: &accountID, QuotaLimit: &quota},
				},
			},
		},
	}
	if err := stateStore.PutUpstream(upstream); err != nil {
		t.Fatal(err)
	}
	first, err := stateStore.GetUpstream("up_copy")
	if err != nil {
		t.Fatal(err)
	}
	key := first.Identities["identity"].Keys["key"]
	*key.LocalAccountID = 99
	*key.QuotaLimit = 99
	first.RechargeRate.CNYPerUSD = 99
	identity := first.Identities["identity"]
	identity.Balance.Amount = 99
	*identity.Balance.RawQuota = 99
	*identity.Balance.QuotaPerUnit = 99
	identity.Keys["key"] = key
	first.Identities["identity"] = identity

	second, err := stateStore.GetUpstream("up_copy")
	if err != nil {
		t.Fatal(err)
	}
	stored := second.Identities["identity"].Keys["key"]
	balance := second.Identities["identity"].Balance
	if *stored.LocalAccountID != 17 || *stored.QuotaLimit != 20 || second.RechargeRate == nil || second.RechargeRate.CNYPerUSD != 0.2 || balance == nil || balance.Amount != 1 || *balance.RawQuota != 500000 || *balance.QuotaPerUnit != 500000 {
		t.Fatalf("stored upstream was mutated through clone: %#v", stored)
	}
}

func TestStoreLoadsVersionThreeStateWithoutIdentityBalance(t *testing.T) {
	path := filepath.Join(t.TempDir(), "state.json")
	legacyV3 := map[string]any{
		"version":  3,
		"accounts": map[string]any{},
		"upstreams": map[string]any{
			"up_existing": map[string]any{
				"id": "up_existing", "name": "existing", "base_url": "https://example.com",
				"identities": map[string]any{
					"identity": map[string]any{
						"id": "identity", "label": "Primary", "status": "connected",
						"keys": map[string]any{"key": map[string]any{"id": "key", "name": "Existing"}},
					},
				},
			},
		},
	}
	raw, err := json.Marshal(legacyV3)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, raw, 0o600); err != nil {
		t.Fatal(err)
	}

	stateStore, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	upstream, err := stateStore.GetUpstream("up_existing")
	if err != nil {
		t.Fatal(err)
	}
	identity := upstream.Identities["identity"]
	if identity.Balance != nil || identity.Keys["key"].Name != "Existing" {
		t.Fatalf("legacy v3 identity was not preserved: %#v", identity)
	}
	if upstream.ManagementURL != "" || upstream.EffectiveManagementURL() != upstream.BaseURL {
		t.Fatalf("legacy upstream management fallback changed: %#v", upstream)
	}
}

func TestUpstreamStorePersistsManagementSiteURLWithLegacyJSONField(t *testing.T) {
	path := filepath.Join(t.TempDir(), "state.json")
	stateStore, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	if err := stateStore.PutUpstream(model.ManagedUpstream{
		ID: "up_sites", BaseURL: "https://api.example.com", ManagementURL: "https://panel.example.com",
		Identities: map[string]model.UpstreamIdentity{},
	}); err != nil {
		t.Fatal(err)
	}

	reloaded, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	upstream, err := reloaded.GetUpstream("up_sites")
	if err != nil || upstream.ManagementURL != "https://panel.example.com" || upstream.EffectiveManagementURL() != "https://panel.example.com" {
		t.Fatalf("management site URL was not persisted: upstream=%#v err=%v", upstream, err)
	}
}
