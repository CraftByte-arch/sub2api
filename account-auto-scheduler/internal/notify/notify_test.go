package notify

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api-account-auto-scheduler/internal/model"
	"github.com/Wei-Shaw/sub2api-account-auto-scheduler/internal/store"
	"github.com/Wei-Shaw/sub2api-account-auto-scheduler/internal/upstream"
)

type fakeNotificationConsole struct {
	mu       sync.Mutex
	accounts []model.UpstreamAccount
	groups   []model.UpstreamGroup
}

func (f *fakeNotificationConsole) ListAccounts(context.Context) ([]model.UpstreamAccount, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]model.UpstreamAccount(nil), f.accounts...), nil
}

func (f *fakeNotificationConsole) ListGroups(context.Context) ([]model.UpstreamGroup, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]model.UpstreamGroup(nil), f.groups...), nil
}

type fakeNotificationProjections struct {
	available   map[int64]upstream.LocalAccountAvailableBalance
	finals      map[int64]upstream.LocalAccountFinalMultiplier
	protections map[int64]map[string]upstream.GroupAccountProtectionView
	logical     map[int64][]int64
}

func (f *fakeNotificationProjections) LocalAccountAvailableBalances([]model.UpstreamAccount) map[int64]upstream.LocalAccountAvailableBalance {
	return f.available
}

func (f *fakeNotificationProjections) LocalAccountFinalMultipliers([]model.UpstreamAccount) map[int64]upstream.LocalAccountFinalMultiplier {
	return f.finals
}

func (f *fakeNotificationProjections) GroupAccountProtectionViews([]model.UpstreamAccount) map[int64]map[string]upstream.GroupAccountProtectionView {
	return f.protections
}

func (f *fakeNotificationProjections) LogicalGroupIDs([]model.UpstreamAccount) map[int64][]int64 {
	return f.logical
}

type capturedPush struct {
	Title string
	Body  string
}

type barkCapture struct {
	mu       sync.Mutex
	pushes   []capturedPush
	requests int
	status   int
	server   *httptest.Server
}

func newBarkCapture(t *testing.T, encryptionKey string, status int) *barkCapture {
	t.Helper()
	capture := &barkCapture{status: status}
	capture.server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		capture.mu.Lock()
		capture.requests++
		capture.mu.Unlock()
		if status >= 300 {
			w.WriteHeader(status)
			_, _ = io.WriteString(w, "delivery failed")
			return
		}
		if err := r.ParseForm(); err != nil {
			t.Fatal(err)
		}
		plaintext := decryptBarkTestPayload(t, encryptionKey, r.Form)
		var payload map[string]string
		if err := json.Unmarshal(plaintext, &payload); err != nil {
			t.Fatal(err)
		}
		capture.mu.Lock()
		capture.pushes = append(capture.pushes, capturedPush{Title: payload["title"], Body: payload["body"]})
		capture.mu.Unlock()
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, `{"code":200}`)
	}))
	return capture
}

func (c *barkCapture) Close() { c.server.Close() }

func (c *barkCapture) Snapshot() ([]capturedPush, int) {
	c.mu.Lock()
	defer c.mu.Unlock()
	return append([]capturedPush(nil), c.pushes...), c.requests
}

func testNotificationCredentialKey() string {
	return base64.StdEncoding.EncodeToString([]byte("0123456789abcdef0123456789abcdef"))
}

func setupCoordinator(t *testing.T, statePath string, console *fakeNotificationConsole, projections *fakeNotificationProjections, capture *barkCapture) (*Coordinator, *store.Store) {
	t.Helper()
	stateStore, err := store.Open(statePath)
	if err != nil {
		t.Fatal(err)
	}
	box, err := upstream.NewCredentialBox(testNotificationCredentialKey())
	if err != nil {
		t.Fatal(err)
	}
	coordinator := NewCoordinator(stateStore, console, projections, box, time.Minute, slog.New(slog.NewTextHandler(io.Discard, nil)))
	coordinator.bark = NewBarkClient(capture.server.Client())
	if _, err := coordinator.SaveSettings(SettingsInput{
		Enabled:       true,
		BarkEndpoint:  capture.server.URL,
		DeviceKey:     "device-key",
		EncryptionKey: "1234567890123456",
	}); err != nil {
		t.Fatal(err)
	}
	return coordinator, stateStore
}

func TestBalanceThresholdPrecedenceEdgesAndRestartPersistence(t *testing.T) {
	statePath := filepath.Join(t.TempDir(), "state.json")
	capture := newBarkCapture(t, "1234567890123456", http.StatusOK)
	defer capture.Close()
	console := &fakeNotificationConsole{
		groups: []model.UpstreamGroup{{ID: 10, Name: "OpenAI", Status: "active"}},
		accounts: []model.UpstreamAccount{
			{ID: 1, Name: "account-one", Type: "apikey", Status: "active", Schedulable: true, GroupIDs: []int64{10}},
			{ID: 2, Name: "account-two", Type: "apikey", Status: "active", Schedulable: true, GroupIDs: []int64{10}},
		},
	}
	projections := &fakeNotificationProjections{
		available: map[int64]upstream.LocalAccountAvailableBalance{
			1: {Status: "available", Remaining: floatPointer(20)},
			2: {Status: "available", Remaining: floatPointer(20)},
		},
		logical: map[int64][]int64{1: {10}, 2: {10}},
	}
	coordinator, stateStore := setupCoordinator(t, statePath, console, projections, capture)
	if err := stateStore.PutGroupBalanceThreshold(10, floatPointer(50)); err != nil {
		t.Fatal(err)
	}
	if err := stateStore.Put(model.ManagedAccount{AccountID: 1, BalanceAlertThreshold: floatPointer(10), Policy: model.DefaultPolicy(), CreatedAt: time.Now().UTC(), UpdatedAt: time.Now().UTC()}); err != nil {
		t.Fatal(err)
	}

	coordinator.Evaluate(context.Background())
	pushes, _ := capture.Snapshot()
	if len(pushes) != 1 || !strings.Contains(pushes[0].Body, "account-two") || strings.Contains(pushes[0].Body, "account-one") {
		t.Fatalf("account threshold did not override group threshold: %#v", pushes)
	}

	projections.available[1] = upstream.LocalAccountAvailableBalance{Status: "available", Remaining: floatPointer(5)}
	coordinator.Evaluate(context.Background())
	coordinator.Evaluate(context.Background())
	pushes, _ = capture.Snapshot()
	if len(pushes) != 2 || !strings.Contains(pushes[1].Body, "account-one") || !strings.Contains(pushes[1].Title, "余额不足") {
		t.Fatalf("low balance edge was not deduplicated: %#v", pushes)
	}

	projections.available[1] = upstream.LocalAccountAvailableBalance{Status: "available", Remaining: floatPointer(12)}
	coordinator.Evaluate(context.Background())
	pushes, _ = capture.Snapshot()
	if len(pushes) != 3 || !strings.Contains(pushes[2].Title, "恢复") {
		t.Fatalf("balance recovery was not emitted: %#v", pushes)
	}

	projections.available[1] = upstream.LocalAccountAvailableBalance{Status: "available", Remaining: floatPointer(4)}
	coordinator.Evaluate(context.Background())
	pushes, _ = capture.Snapshot()
	if len(pushes) != 4 {
		t.Fatalf("later low balance transition did not alert again: %#v", pushes)
	}

	restarted, _ := setupCoordinator(t, statePath, console, projections, capture)
	restarted.Evaluate(context.Background())
	pushes, _ = capture.Snapshot()
	if len(pushes) != 4 {
		t.Fatalf("restart duplicated active balance alert: %#v", pushes)
	}
}

func TestGroupCapacityZeroOneManyStateMachine(t *testing.T) {
	statePath := filepath.Join(t.TempDir(), "state.json")
	capture := newBarkCapture(t, "1234567890123456", http.StatusOK)
	defer capture.Close()
	console := &fakeNotificationConsole{
		groups: []model.UpstreamGroup{{ID: 10, Name: "OpenAI", Status: "active"}},
		accounts: []model.UpstreamAccount{
			{ID: 1, Name: "one", Type: "apikey", Status: "active", Schedulable: true, GroupIDs: []int64{10}},
			{ID: 2, Name: "two", Type: "apikey", Status: "active", Schedulable: true, GroupIDs: []int64{10}},
		},
	}
	coordinator, _ := setupCoordinator(t, statePath, console, &fakeNotificationProjections{}, capture)
	coordinator.Evaluate(context.Background())

	console.mu.Lock()
	console.accounts[0].Schedulable = false
	console.accounts[1].Schedulable = false
	console.mu.Unlock()
	coordinator.Evaluate(context.Background())
	coordinator.Evaluate(context.Background())
	pushes, _ := capture.Snapshot()
	if len(pushes) != 1 || !strings.Contains(pushes[0].Title, "可用账号告警") {
		t.Fatalf("zero capacity alert not deduplicated: %#v", pushes)
	}

	console.mu.Lock()
	console.accounts[0].Schedulable = true
	console.mu.Unlock()
	coordinator.Evaluate(context.Background())
	pushes, _ = capture.Snapshot()
	if len(pushes) != 1 {
		t.Fatalf("one available account incorrectly recovered group: %#v", pushes)
	}

	console.mu.Lock()
	console.accounts[1].Schedulable = true
	console.mu.Unlock()
	coordinator.Evaluate(context.Background())
	coordinator.Evaluate(context.Background())
	pushes, _ = capture.Snapshot()
	if len(pushes) != 2 || !strings.Contains(pushes[1].Title, "恢复") || !strings.Contains(pushes[1].Body, "2") {
		t.Fatalf("many-account recovery not emitted once: %#v", pushes)
	}
}

func TestMultiplierChangeIncludesProtectionAndInitializesSilently(t *testing.T) {
	statePath := filepath.Join(t.TempDir(), "state.json")
	capture := newBarkCapture(t, "1234567890123456", http.StatusOK)
	defer capture.Close()
	console := &fakeNotificationConsole{
		groups: []model.UpstreamGroup{{ID: 10, Name: "OpenAI", Status: "active"}},
		accounts: []model.UpstreamAccount{
			{ID: 1, Name: "rate-account", Type: "apikey", Status: "active", Schedulable: true, GroupIDs: []int64{10}},
			{ID: 2, Name: "spare", Type: "apikey", Status: "active", Schedulable: true, GroupIDs: []int64{10}},
		},
	}
	projections := &fakeNotificationProjections{
		finals:  map[int64]upstream.LocalAccountFinalMultiplier{1: {Status: "available", FinalMultiplier: floatPointer(0.16)}},
		logical: map[int64][]int64{1: {10}, 2: {10}},
	}
	coordinator, _ := setupCoordinator(t, statePath, console, projections, capture)
	coordinator.Evaluate(context.Background())
	pushes, _ := capture.Snapshot()
	if len(pushes) != 0 {
		t.Fatalf("initial multiplier observation sent a notification: %#v", pushes)
	}

	projections.finals[1] = upstream.LocalAccountFinalMultiplier{Status: "available", FinalMultiplier: floatPointer(0.2)}
	coordinator.Evaluate(context.Background())
	coordinator.Evaluate(context.Background())
	pushes, _ = capture.Snapshot()
	if len(pushes) != 1 || !strings.Contains(pushes[0].Body, "0.160000") || !strings.Contains(pushes[0].Body, "0.200000") || !strings.Contains(pushes[0].Body, "未触发") {
		t.Fatalf("multiplier change notification incorrect: %#v", pushes)
	}

	projections.protections = map[int64]map[string]upstream.GroupAccountProtectionView{
		1: {"10": {GroupID: 10, AccountID: 1, Status: model.ProtectionExceeded}},
	}
	projections.finals[1] = upstream.LocalAccountFinalMultiplier{Status: "available", FinalMultiplier: floatPointer(0.3)}
	coordinator.Evaluate(context.Background())
	pushes, _ = capture.Snapshot()
	if len(pushes) != 2 || !strings.Contains(pushes[1].Body, "已触发") {
		t.Fatalf("protection status missing from multiplier message: %#v", pushes)
	}
}

func TestDeliveryFailureIsRecordedAndNotRetriedEveryPoll(t *testing.T) {
	statePath := filepath.Join(t.TempDir(), "state.json")
	capture := newBarkCapture(t, "1234567890123456", http.StatusForbidden)
	defer capture.Close()
	console := &fakeNotificationConsole{groups: []model.UpstreamGroup{{ID: 10, Name: "Empty", Status: "active"}}}
	coordinator, stateStore := setupCoordinator(t, statePath, console, &fakeNotificationProjections{}, capture)
	coordinator.Evaluate(context.Background())
	coordinator.Evaluate(context.Background())
	_, requests := capture.Snapshot()
	if requests != 1 {
		t.Fatalf("failed transition retried repeatedly: requests=%d", requests)
	}
	settings := stateStore.GetNotificationSettings()
	if settings.LastDeliveryError == "" || strings.Contains(settings.LastDeliveryError, "device-key") {
		t.Fatalf("delivery error missing or leaked secret: %#v", settings)
	}
}

func floatPointer(value float64) *float64 { return &value }
