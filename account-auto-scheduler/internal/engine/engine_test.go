package engine

import (
	"context"
	"encoding/json"
	"errors"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api-account-auto-scheduler/internal/core"
	"github.com/Wei-Shaw/sub2api-account-auto-scheduler/internal/model"
	"github.com/Wei-Shaw/sub2api-account-auto-scheduler/internal/store"
)

type fakeCore struct {
	mu             sync.Mutex
	account        model.UpstreamAccount
	getErr         error
	outcomes       []core.ProbeOutcome
	errors         []error
	legacyCalls    int
	directOutcomes []core.ProbeOutcome
	directErrors   []error
	directCalls    int
	setErr         error
	setCalls       []bool
	exported       core.DirectProbeExport
	exportErr      error
	exportCalls    int
	exportJWT      string
	exportIdentity core.ForwardedIdentity
}

func (f *fakeCore) ListAPIKeyAccounts(context.Context) ([]model.UpstreamAccount, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	return []model.UpstreamAccount{f.account}, nil
}

func (f *fakeCore) GetAccount(context.Context, int64) (model.UpstreamAccount, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.account, f.getErr
}

func (f *fakeCore) TestAccount(context.Context, int64, string, string) (core.ProbeOutcome, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.legacyCalls++
	var outcome core.ProbeOutcome
	var err error
	if len(f.outcomes) > 0 {
		outcome = f.outcomes[0]
		f.outcomes = f.outcomes[1:]
	}
	if len(f.errors) > 0 {
		err = f.errors[0]
		f.errors = f.errors[1:]
	}
	return outcome, err
}

func (f *fakeCore) ProbeDirect(_ context.Context, _ model.DirectProbeSnapshot, _ model.Policy) (core.ProbeOutcome, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.directCalls++
	var outcome core.ProbeOutcome
	var err error
	if len(f.directOutcomes) > 0 {
		outcome = f.directOutcomes[0]
		f.directOutcomes = f.directOutcomes[1:]
	}
	if len(f.directErrors) > 0 {
		err = f.directErrors[0]
		f.directErrors = f.directErrors[1:]
	}
	return outcome, err
}

func (f *fakeCore) ExportDirectProbeSnapshot(_ context.Context, _ int64, adminJWT string, identity core.ForwardedIdentity) (core.DirectProbeExport, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.exportCalls++
	f.exportJWT = adminJWT
	f.exportIdentity = identity
	if f.exportErr != nil {
		return core.DirectProbeExport{}, f.exportErr
	}
	return core.DirectProbeExport{Snapshot: cloneDirectSnapshot(f.exported.Snapshot)}, nil
}

func (f *fakeCore) SetSchedulable(_ context.Context, _ int64, enabled bool) (model.UpstreamAccount, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.setCalls = append(f.setCalls, enabled)
	if f.setErr != nil {
		return model.UpstreamAccount{}, f.setErr
	}
	f.account.Schedulable = enabled
	return f.account, nil
}

func TestConsecutiveFailuresSuspendAndSuccessesRecover(t *testing.T) {
	fake := healthyAPIKeyAccount()
	fake.outcomes = []core.ProbeOutcome{
		failedOutcome(), failedOutcome(), healthyOutcome(), healthyOutcome(),
	}
	scheduler := newTestEngine(t, fake)
	policy := model.DefaultPolicy()
	policy.FailureThreshold = 2
	policy.RecoveryThreshold = 2
	if _, err := scheduler.Upsert(context.Background(), fake.account.ID, policy); err != nil {
		t.Fatal(err)
	}

	scheduler.runOne(context.Background(), fake.account.ID)
	assertState(t, scheduler, 1, 0, false, true, "")
	scheduler.runOne(context.Background(), fake.account.ID)
	assertState(t, scheduler, 2, 0, true, false, "disabled")
	scheduler.runOne(context.Background(), fake.account.ID)
	assertState(t, scheduler, 0, 1, true, false, "")
	scheduler.runOne(context.Background(), fake.account.ID)
	assertState(t, scheduler, 0, 2, false, true, "restored")

	if len(fake.setCalls) != 2 || fake.setCalls[0] || !fake.setCalls[1] {
		t.Fatalf("unexpected scheduling calls: %#v", fake.setCalls)
	}
}

func TestEnabledUpsertRestoresAdministratorStoppedAccount(t *testing.T) {
	fake := healthyAPIKeyAccount()
	fake.account.Schedulable = false
	scheduler := newTestEngine(t, fake)
	policy := model.DefaultPolicy()
	managed, err := scheduler.Upsert(context.Background(), fake.account.ID, policy)
	if err != nil {
		t.Fatal(err)
	}
	if !managed.Schedulable || managed.ManagedSuspended || !managed.Policy.Enabled || managed.NextCheckAt == nil {
		t.Fatalf("manual account was not handed to automation: %#v", managed)
	}
	if len(fake.setCalls) != 1 || !fake.setCalls[0] {
		t.Fatalf("unexpected scheduling calls: %#v", fake.setCalls)
	}
}

func TestBackgroundCheckNeverRecoversLaterAdministratorStop(t *testing.T) {
	fake := healthyAPIKeyAccount()
	fake.outcomes = []core.ProbeOutcome{healthyOutcome(), healthyOutcome()}
	scheduler := newTestEngine(t, fake)
	policy := model.DefaultPolicy()
	policy.RecoveryThreshold = 1
	if _, err := scheduler.Upsert(context.Background(), fake.account.ID, policy); err != nil {
		t.Fatal(err)
	}
	fake.account.Schedulable = false

	scheduler.runOne(context.Background(), fake.account.ID)
	scheduler.runOne(context.Background(), fake.account.ID)
	managed := scheduler.List()[0]
	if managed.ManagedSuspended || managed.Schedulable || len(fake.setCalls) != 0 {
		t.Fatalf("background checks overwrote a later manual stop: managed=%#v calls=%#v", managed, fake.setCalls)
	}
}

func TestEnabledUpsertDoesNotBypassOwnedSuspension(t *testing.T) {
	fake := healthyAPIKeyAccount()
	fake.outcomes = []core.ProbeOutcome{failedOutcome()}
	scheduler := newTestEngine(t, fake)
	policy := model.DefaultPolicy()
	policy.FailureThreshold = 1
	if _, err := scheduler.Upsert(context.Background(), fake.account.ID, policy); err != nil {
		t.Fatal(err)
	}
	scheduler.runOne(context.Background(), fake.account.ID)

	updated, err := scheduler.Upsert(context.Background(), fake.account.ID, policy)
	if err != nil {
		t.Fatal(err)
	}
	if !updated.ManagedSuspended || updated.Schedulable {
		t.Fatalf("owned suspension was bypassed: %#v", updated)
	}
	if len(fake.setCalls) != 1 || fake.setCalls[0] {
		t.Fatalf("unexpected scheduling calls: %#v", fake.setCalls)
	}
}

func TestEnabledUpsertRejectsInactiveAccount(t *testing.T) {
	fake := healthyAPIKeyAccount()
	fake.account.Status = "inactive"
	fake.account.Schedulable = false
	scheduler := newTestEngine(t, fake)

	if _, err := scheduler.Upsert(context.Background(), fake.account.ID, model.DefaultPolicy()); err == nil {
		t.Fatal("expected inactive account to be rejected")
	}
	if len(scheduler.List()) != 0 || len(fake.setCalls) != 0 {
		t.Fatalf("inactive account was mutated: configs=%#v calls=%#v", scheduler.List(), fake.setCalls)
	}
}

func TestEnabledUpsertReturnsRestoreFailureWithoutPersisting(t *testing.T) {
	fake := healthyAPIKeyAccount()
	fake.account.Schedulable = false
	fake.setErr = errors.New("upstream unavailable")
	scheduler := newTestEngine(t, fake)

	if _, err := scheduler.Upsert(context.Background(), fake.account.ID, model.DefaultPolicy()); err == nil || !strings.Contains(err.Error(), "恢复账号调度失败") {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(scheduler.List()) != 0 || len(fake.setCalls) != 1 || !fake.setCalls[0] {
		t.Fatalf("restore failure state: configs=%#v calls=%#v", scheduler.List(), fake.setCalls)
	}
}

func TestLatencyAtOrOverLimitCountsAsFailure(t *testing.T) {
	fake := healthyAPIKeyAccount()
	fake.outcomes = []core.ProbeOutcome{{Success: true, Latency: 1000 * time.Millisecond}}
	scheduler := newTestEngine(t, fake)
	policy := model.DefaultPolicy()
	policy.LatencyLimitMS = 1000
	policy.FailureThreshold = 1
	if _, err := scheduler.Upsert(context.Background(), fake.account.ID, policy); err != nil {
		t.Fatal(err)
	}

	scheduler.runOne(context.Background(), fake.account.ID)
	managed := scheduler.List()[0]
	if managed.History[0].Status != model.CheckDegraded || !managed.ManagedSuspended || managed.History[0].Action != "disabled" {
		t.Fatalf("unexpected degraded state: %#v", managed)
	}
}

func TestAuthorizeAndRevokeDirectProbeUsesStepUpExportAndRedactedState(t *testing.T) {
	fake := healthyAPIKeyAccount()
	fake.exported = core.DirectProbeExport{Snapshot: directSnapshotForAccount(t, fake.account)}
	box := &fakeDirectBox{enabled: true}
	scheduler := newTestEngineWithDirect(t, fake, box)
	if _, err := scheduler.Upsert(context.Background(), fake.account.ID, model.DefaultPolicy()); err != nil {
		t.Fatal(err)
	}

	now := time.Date(2026, 8, 12, 9, 0, 0, 0, time.UTC)
	scheduler.now = func() time.Time { return now }
	authorized, err := scheduler.AuthorizeDirect(
		context.Background(), fake.account.ID, "browser-jwt", core.ForwardedIdentity{ClientIP: "203.0.113.8", UserAgent: "browser-agent"},
	)
	if err != nil {
		t.Fatal(err)
	}
	if authorized.EffectiveProbeSource() != model.ProbeSourceDirect || authorized.DirectProbe == nil || authorized.DirectProbe.AuthorizationState != model.DirectProbeAuthorized || authorized.DirectProbe.Credential.Ciphertext == "" || authorized.DirectProbe.ImportedAt == nil || !authorized.DirectProbe.ImportedAt.Equal(now) {
		t.Fatalf("direct authorization was not persisted: %#v", authorized)
	}
	if fake.exportCalls != 1 || fake.exportJWT != "browser-jwt" || fake.exportIdentity.ClientIP != "203.0.113.8" || fake.exportIdentity.UserAgent != "browser-agent" || box.encryptCalls != 1 {
		t.Fatalf("unexpected authorization forwarding: exports=%d jwt=%q identity=%#v encrypts=%d", fake.exportCalls, fake.exportJWT, fake.exportIdentity, box.encryptCalls)
	}

	view := authorized.PublicView()
	if view.Probe.Source != model.ProbeSourceDirect || view.Probe.AuthorizationState != model.DirectProbeAuthorized || view.Probe.ImportedAt == nil {
		t.Fatalf("direct authorization metadata is missing from public view: %#v", view.Probe)
	}
	if serialized := mustJSON(t, view); strings.Contains(serialized, "direct-api-secret") || strings.Contains(serialized, "encrypted-direct-snapshot") || strings.Contains(serialized, "routing-fingerprint") {
		t.Fatalf("public view leaked direct probe material: %s", serialized)
	}

	revoked, err := scheduler.RevokeDirect(context.Background(), fake.account.ID)
	if err != nil {
		t.Fatal(err)
	}
	if revoked.EffectiveProbeSource() != model.ProbeSourceLegacy || revoked.DirectProbe != nil || revoked.PublicView().Probe.AuthorizationState != model.DirectProbeAuthorizationMissing {
		t.Fatalf("direct authorization was not revoked: %#v", revoked)
	}
}

func TestAuthorizeDirectProbePreservesExistingSnapshotWhenExportDoesNotMatch(t *testing.T) {
	fake := healthyAPIKeyAccount()
	box := &fakeDirectBox{enabled: true}
	scheduler := newTestEngineWithDirect(t, fake, box)
	if _, err := scheduler.Upsert(context.Background(), fake.account.ID, model.DefaultPolicy()); err != nil {
		t.Fatal(err)
	}

	first := directSnapshotForAccount(t, fake.account)
	fake.exported = core.DirectProbeExport{Snapshot: first}
	if _, err := scheduler.AuthorizeDirect(context.Background(), fake.account.ID, "browser-jwt", core.ForwardedIdentity{}); err != nil {
		t.Fatal(err)
	}
	before, err := scheduler.store.Get(fake.account.ID)
	if err != nil {
		t.Fatal(err)
	}

	bad := directSnapshotForAccount(t, fake.account)
	bad.BaseURL = "https://other.example/v1"
	bad.RoutingFingerprint = model.DirectProbeRoutingFingerprint(bad)
	fake.exported = core.DirectProbeExport{Snapshot: bad}
	if _, err := scheduler.AuthorizeDirect(context.Background(), fake.account.ID, "browser-jwt", core.ForwardedIdentity{}); err == nil {
		t.Fatal("mismatched export unexpectedly replaced direct authorization")
	}
	after, err := scheduler.store.Get(fake.account.ID)
	if err != nil {
		t.Fatal(err)
	}
	if after.DirectProbe == nil || before.DirectProbe == nil || after.DirectProbe.Credential.Ciphertext != before.DirectProbe.Credential.Ciphertext || after.DirectProbe.RoutingFingerprint != before.DirectProbe.RoutingFingerprint {
		t.Fatalf("failed authorization replaced existing snapshot: before=%#v after=%#v", before.DirectProbe, after.DirectProbe)
	}
}

func TestDirectProbeFailureNeverFallsBackToLegacyTest(t *testing.T) {
	fake := healthyAPIKeyAccount()
	fake.outcomes = []core.ProbeOutcome{healthyOutcome()}
	fake.directOutcomes = []core.ProbeOutcome{failedOutcome()}
	box := &fakeDirectBox{enabled: true, snapshot: directSnapshotForAccount(t, fake.account)}
	scheduler := newTestEngineWithDirect(t, fake, box)
	putAuthorizedDirectConfig(t, scheduler, fake.account.ID, box.snapshot)

	scheduler.runOne(context.Background(), fake.account.ID)
	managed := scheduler.List()[0]
	if fake.directCalls != 1 || fake.legacyCalls != 0 || managed.History[0].Status != model.CheckFailed || managed.ConsecutiveFailures != 1 {
		t.Fatalf("direct failure fell back to legacy probe or did not count correctly: direct=%d legacy=%d managed=%#v", fake.directCalls, fake.legacyCalls, managed)
	}
}

func TestInsufficientBalanceFailureSuspendsPersistsAndHealthyCheckClearsClassification(t *testing.T) {
	fake := healthyAPIKeyAccount()
	fake.directErrors = []error{&core.DirectProbeHTTPError{
		StatusCode: 403,
		Message:    "直连上游拒绝访问 (HTTP 403 Forbidden): 用户额度不足, 剩余额度: ¥-0.000358",
	}}
	fake.directOutcomes = []core.ProbeOutcome{{}, healthyOutcome()}
	box := &fakeDirectBox{enabled: true, snapshot: directSnapshotForAccount(t, fake.account)}
	scheduler := newTestEngineWithDirect(t, fake, box)
	putAuthorizedDirectConfig(t, scheduler, fake.account.ID, box.snapshot)
	if err := scheduler.store.Update(fake.account.ID, func(account *model.ManagedAccount) error {
		account.Policy.FailureThreshold = 1
		account.Policy.RecoveryThreshold = 1
		return nil
	}); err != nil {
		t.Fatal(err)
	}

	scheduler.runOne(context.Background(), fake.account.ID)
	failed := scheduler.List()[0]
	if failed.LastFailureKind != model.CheckFailureBalanceInsufficient || failed.History[0].FailureKind != model.CheckFailureBalanceInsufficient {
		t.Fatalf("balance failure classification was not persisted: %#v", failed)
	}
	if !strings.Contains(failed.LastError, "用户额度不足") || !failed.ManagedSuspended || failed.Schedulable || failed.History[0].Action != "disabled" {
		t.Fatalf("balance failure did not use the normal suspension path: %#v calls=%#v", failed, fake.setCalls)
	}

	scheduler.runOne(context.Background(), fake.account.ID)
	recovered := scheduler.List()[0]
	if recovered.LastFailureKind != "" || recovered.LastError != "" || recovered.ManagedSuspended || !recovered.Schedulable || recovered.History[0].Action != "restored" {
		t.Fatalf("healthy recovery did not clear balance classification: %#v", recovered)
	}
	if len(fake.setCalls) != 2 || fake.setCalls[0] || !fake.setCalls[1] {
		t.Fatalf("unexpected scheduling calls: %#v", fake.setCalls)
	}
}

func TestStaleOrUnreadableDirectProbeSkipsWithoutSchedulerCountersOrFallback(t *testing.T) {
	tests := []struct {
		name    string
		prepare func(fake *fakeCore, box *fakeDirectBox)
	}{
		{
			name: "routing changed",
			prepare: func(fake *fakeCore, _ *fakeDirectBox) {
				fake.account.Credentials["base_url"] = "https://changed.example/v1"
			},
		},
		{
			name: "snapshot cannot decrypt",
			prepare: func(_ *fakeCore, box *fakeDirectBox) {
				box.decryptErr = errors.New("credential key rotated")
			},
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			fake := healthyAPIKeyAccount()
			box := &fakeDirectBox{enabled: true, snapshot: directSnapshotForAccount(t, fake.account)}
			scheduler := newTestEngineWithDirect(t, fake, box)
			putAuthorizedDirectConfig(t, scheduler, fake.account.ID, box.snapshot)
			test.prepare(fake, box)

			scheduler.runOne(context.Background(), fake.account.ID)
			managed := scheduler.List()[0]
			if fake.directCalls != 0 || fake.legacyCalls != 0 || len(fake.setCalls) != 0 || managed.History[0].Status != model.CheckSkipped || managed.ConsecutiveFailures != 0 || managed.ConsecutiveSuccesses != 0 || managed.ManagedSuspended {
				t.Fatalf("neutral direct skip changed scheduler state: direct=%d legacy=%d set=%#v managed=%#v", fake.directCalls, fake.legacyCalls, fake.setCalls, managed)
			}
			if managed.DirectProbe == nil || managed.DirectProbe.AuthorizationState != model.DirectProbeNeedsReauthorization || strings.TrimSpace(managed.DirectProbe.ActionMessage) == "" {
				t.Fatalf("stale direct authorization was not actionable: %#v", managed.DirectProbe)
			}
		})
	}
}

func TestDisabledDetectionRecordsButDoesNotChangeScheduling(t *testing.T) {
	fake := healthyAPIKeyAccount()
	fake.outcomes = []core.ProbeOutcome{failedOutcome()}
	scheduler := newTestEngine(t, fake)
	policy := model.DefaultPolicy()
	policy.Enabled = false
	policy.FailureThreshold = 1
	if _, err := scheduler.Upsert(context.Background(), fake.account.ID, policy); err != nil {
		t.Fatal(err)
	}

	scheduler.runOne(context.Background(), fake.account.ID)
	managed := scheduler.List()[0]
	if len(managed.History) != 1 || managed.ManagedSuspended || len(fake.setCalls) != 0 || managed.NextCheckAt != nil {
		t.Fatalf("disabled detection changed scheduling: %#v calls=%#v", managed, fake.setCalls)
	}
}

func TestHistoryIsCappedAtFiftyResults(t *testing.T) {
	fake := healthyAPIKeyAccount()
	for range 55 {
		fake.outcomes = append(fake.outcomes, healthyOutcome())
	}
	scheduler := newTestEngine(t, fake)
	if _, err := scheduler.Upsert(context.Background(), fake.account.ID, model.DefaultPolicy()); err != nil {
		t.Fatal(err)
	}
	for range 55 {
		scheduler.runOne(context.Background(), fake.account.ID)
	}
	if history := scheduler.List()[0].History; len(history) != model.HistoryLimit {
		t.Fatalf("history length = %d, want %d", len(history), model.HistoryLimit)
	}
}

func TestDisablingOwnedRuleRestoresSchedulingFirst(t *testing.T) {
	fake := healthyAPIKeyAccount()
	fake.outcomes = []core.ProbeOutcome{failedOutcome()}
	scheduler := newTestEngine(t, fake)
	policy := model.DefaultPolicy()
	policy.FailureThreshold = 1
	if _, err := scheduler.Upsert(context.Background(), fake.account.ID, policy); err != nil {
		t.Fatal(err)
	}
	scheduler.runOne(context.Background(), fake.account.ID)

	policy.Enabled = false
	updated, err := scheduler.Upsert(context.Background(), fake.account.ID, policy)
	if err != nil {
		t.Fatal(err)
	}
	if updated.ManagedSuspended || !updated.Schedulable || updated.NextCheckAt != nil {
		t.Fatalf("owned suspension was not restored: %#v", updated)
	}
	if len(fake.setCalls) != 2 || fake.setCalls[0] || !fake.setCalls[1] {
		t.Fatalf("unexpected scheduling calls: %#v", fake.setCalls)
	}
}

func TestStoreFailureDoesNotHideMissingConfiguration(t *testing.T) {
	fake := healthyAPIKeyAccount()
	scheduler := newTestEngine(t, fake)
	if err := scheduler.Trigger(999); !errors.Is(err, store.ErrNotFound) {
		t.Fatalf("Trigger error = %v, want ErrNotFound", err)
	}
}

func newTestEngine(t *testing.T, fake *fakeCore) *Engine {
	t.Helper()
	stateStore, err := store.Open(filepath.Join(t.TempDir(), "state.json"))
	if err != nil {
		t.Fatal(err)
	}
	return New(stateStore, fake, 2, nil)
}

func newTestEngineWithDirect(t *testing.T, fake *fakeCore, box *fakeDirectBox) *Engine {
	t.Helper()
	stateStore, err := store.Open(filepath.Join(t.TempDir(), "state.json"))
	if err != nil {
		t.Fatal(err)
	}
	return New(stateStore, fake, 2, nil, WithDirectProbeCredentials(box))
}

func healthyAPIKeyAccount() *fakeCore {
	return &fakeCore{account: model.UpstreamAccount{
		ID: 1, Name: "api-key-account", Platform: "openai", Type: "apikey", Status: "active", Schedulable: true,
		Credentials: map[string]any{
			"base_url": "https://relay.example/v1",
		},
	}}
}

func healthyOutcome() core.ProbeOutcome {
	return core.ProbeOutcome{Success: true, ResponseText: "42", Latency: 200 * time.Millisecond}
}

func failedOutcome() core.ProbeOutcome {
	return core.ProbeOutcome{Success: false, ErrorMessage: "upstream failed", Latency: 100 * time.Millisecond}
}

func assertState(t *testing.T, scheduler *Engine, failures, successes int, managedSuspended, schedulable bool, action string) {
	t.Helper()
	managed := scheduler.List()[0]
	if managed.ConsecutiveFailures != failures || managed.ConsecutiveSuccesses != successes ||
		managed.ManagedSuspended != managedSuspended || managed.Schedulable != schedulable {
		t.Fatalf("unexpected state: %#v", managed)
	}
	if managed.History[0].Action != action {
		t.Fatalf("action = %q, want %q", managed.History[0].Action, action)
	}
}

type fakeDirectBox struct {
	enabled      bool
	encryptErr   error
	decryptErr   error
	snapshot     model.DirectProbeSnapshot
	encryptCalls int
	decryptCalls int
}

func (b *fakeDirectBox) Enabled() bool { return b != nil && b.enabled }

func (b *fakeDirectBox) EncryptDirectProbe(accountID int64, snapshot model.DirectProbeSnapshot) (model.CredentialEnvelope, error) {
	if b == nil || !b.enabled {
		return model.CredentialEnvelope{}, errors.New("credential box disabled")
	}
	b.encryptCalls++
	if b.encryptErr != nil {
		return model.CredentialEnvelope{}, b.encryptErr
	}
	if snapshot.AccountID != accountID {
		return model.CredentialEnvelope{}, errors.New("unexpected account ID")
	}
	b.snapshot = cloneDirectSnapshot(snapshot)
	return model.CredentialEnvelope{Version: 1, Nonce: "encrypted-nonce", Ciphertext: "encrypted-direct-snapshot"}, nil
}

func (b *fakeDirectBox) DecryptDirectProbe(accountID int64, _ model.CredentialEnvelope) (model.DirectProbeSnapshot, error) {
	if b == nil || !b.enabled {
		return model.DirectProbeSnapshot{}, errors.New("credential box disabled")
	}
	b.decryptCalls++
	if b.decryptErr != nil {
		return model.DirectProbeSnapshot{}, b.decryptErr
	}
	if b.snapshot.AccountID != accountID {
		return model.DirectProbeSnapshot{}, errors.New("unexpected account ID")
	}
	return cloneDirectSnapshot(b.snapshot), nil
}

func directSnapshotForAccount(t *testing.T, account model.UpstreamAccount) model.DirectProbeSnapshot {
	t.Helper()
	baseURL, err := model.EffectiveDirectProbeBaseURL(account.BaseURL(), account.Platform)
	if err != nil {
		t.Fatal(err)
	}
	snapshot := model.DirectProbeSnapshot{
		Version:   model.DirectProbeSnapshotVersion,
		AccountID: account.ID,
		Platform:  account.Platform,
		BaseURL:   baseURL,
		APIKey:    "direct-api-secret",
	}
	snapshot.RoutingFingerprint = model.DirectProbeRoutingFingerprint(snapshot)
	return snapshot
}

func putAuthorizedDirectConfig(t *testing.T, scheduler *Engine, accountID int64, snapshot model.DirectProbeSnapshot) {
	t.Helper()
	now := time.Now().UTC()
	managed := model.ManagedAccount{
		AccountID:     accountID,
		Name:          "direct",
		Platform:      snapshot.Platform,
		AccountStatus: "active",
		Schedulable:   true,
		Policy:        model.DefaultPolicy(),
		ProbeSource:   model.ProbeSourceDirect,
		DirectProbe: &model.DirectProbeConfig{
			Credential:         model.CredentialEnvelope{Version: 1, Nonce: "encrypted-nonce", Ciphertext: "encrypted-direct-snapshot"},
			AuthorizationState: model.DirectProbeAuthorized,
			ImportedAt:         &now,
			UpdatedAt:          &now,
			RoutingFingerprint: snapshot.RoutingFingerprint,
		},
		History:   []model.CheckResult{},
		CreatedAt: now,
		UpdatedAt: now,
	}
	if err := scheduler.store.Put(managed); err != nil {
		t.Fatal(err)
	}
}

func cloneDirectSnapshot(snapshot model.DirectProbeSnapshot) model.DirectProbeSnapshot {
	clone := snapshot
	if snapshot.ModelMapping != nil {
		clone.ModelMapping = make(map[string]string, len(snapshot.ModelMapping))
		for key, value := range snapshot.ModelMapping {
			clone.ModelMapping[key] = value
		}
	}
	if snapshot.HeaderOverrides != nil {
		clone.HeaderOverrides = make(map[string]string, len(snapshot.HeaderOverrides))
		for key, value := range snapshot.HeaderOverrides {
			clone.HeaderOverrides[key] = value
		}
	}
	if snapshot.OpenAIResponsesOK != nil {
		value := *snapshot.OpenAIResponsesOK
		clone.OpenAIResponsesOK = &value
	}
	if snapshot.Proxy != nil {
		proxy := *snapshot.Proxy
		if snapshot.Proxy.ExpiresAt != nil {
			expiresAt := *snapshot.Proxy.ExpiresAt
			proxy.ExpiresAt = &expiresAt
		}
		clone.Proxy = &proxy
	}
	return clone
}

func mustJSON(t *testing.T, value any) string {
	t.Helper()
	raw, err := json.Marshal(value)
	if err != nil {
		t.Fatal(err)
	}
	return string(raw)
}
