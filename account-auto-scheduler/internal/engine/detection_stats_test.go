package engine

import (
	"context"
	"errors"
	"math"
	"path/filepath"
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api-account-auto-scheduler/internal/core"
	"github.com/Wei-Shaw/sub2api-account-auto-scheduler/internal/model"
	"github.com/Wei-Shaw/sub2api-account-auto-scheduler/internal/store"
)

type pricingFakeCore struct {
	*fakeCore
	pricing    core.ModelPricing
	pricingErr error
	modelID    string
}

func (f *pricingFakeCore) GetModelPricing(_ context.Context, modelID string) (core.ModelPricing, error) {
	f.modelID = modelID
	return f.pricing, f.pricingErr
}

func TestProbeUsageAndKnownCostAccumulateAndSurviveRestart(t *testing.T) {
	path := filepath.Join(t.TempDir(), "state.json")
	stateStore, err := store.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	base := healthyAPIKeyAccount()
	base.outcomes = []core.ProbeOutcome{{
		Success: true, ResponseText: "42", Latency: 50 * time.Millisecond,
		Usage: &model.ProbeUsage{Model: "gpt-cost-test", InputTokens: 10, OutputTokens: 2, CacheReadTokens: 5},
	}}
	input, output, cacheRead := 0.001, 0.002, 0.0001
	client := &pricingFakeCore{fakeCore: base, pricing: core.ModelPricing{
		Found: true, InputPrice: &input, OutputPrice: &output, CacheReadPrice: &cacheRead,
	}}
	now := time.Now().UTC()
	managed := model.ManagedAccount{
		AccountID: 1, Name: "probe", Platform: "openai", AccountStatus: "active", Schedulable: true,
		Policy: model.DefaultPolicy(), History: []model.CheckResult{}, CreatedAt: now, UpdatedAt: now,
	}
	managed.Policy.Model = "configured-model"
	if err := stateStore.Put(managed); err != nil {
		t.Fatal(err)
	}
	scheduler := New(stateStore, client, 1, nil)
	scheduler.runOne(context.Background(), 1)

	stored, err := stateStore.Get(1)
	if err != nil {
		t.Fatal(err)
	}
	wantCost := 10*input + 2*output + 5*cacheRead
	stats := stored.DetectionStats
	if stats.Requests != 1 || stats.InputTokens != 10 || stats.OutputTokens != 2 || stats.CacheReadTokens != 5 || stats.KnownCostChecks != 1 || math.Abs(stats.KnownCost-wantCost) > 1e-12 {
		t.Fatalf("unexpected detection statistics: %#v want_cost=%f", stats, wantCost)
	}
	if client.modelID != "gpt-cost-test" || len(stored.History) != 1 || stored.History[0].Cost == nil || !stored.History[0].Cost.Known {
		t.Fatalf("per-check usage/cost was not retained: model=%q history=%#v", client.modelID, stored.History)
	}

	reloaded, err := store.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	restarted, err := reloaded.Get(1)
	if err != nil || restarted.DetectionStats.Requests != 1 || math.Abs(restarted.DetectionStats.KnownCost-wantCost) > 1e-12 {
		t.Fatalf("statistics were not restored after restart: %#v err=%v", restarted.DetectionStats, err)
	}
}

func TestProbeStatisticsKeepTokensWhenPricingUnavailableAndDeleteWithConfig(t *testing.T) {
	path := filepath.Join(t.TempDir(), "state.json")
	stateStore, err := store.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	base := healthyAPIKeyAccount()
	base.outcomes = []core.ProbeOutcome{{
		Success: true, ResponseText: "42", Latency: 50 * time.Millisecond,
		Usage: &model.ProbeUsage{InputTokens: 7, OutputTokens: 1},
	}}
	client := &pricingFakeCore{fakeCore: base, pricing: core.ModelPricing{Found: false}}
	now := time.Now().UTC()
	managed := model.ManagedAccount{
		AccountID: 1, Name: "probe", Platform: "openai", AccountStatus: "active", Schedulable: true,
		Policy: model.DefaultPolicy(), History: []model.CheckResult{}, CreatedAt: now, UpdatedAt: now,
	}
	managed.Policy.Model = "unknown-model"
	if err := stateStore.Put(managed); err != nil {
		t.Fatal(err)
	}
	scheduler := New(stateStore, client, 1, nil)
	scheduler.runOne(context.Background(), 1)
	stored, err := stateStore.Get(1)
	if err != nil {
		t.Fatal(err)
	}
	if stored.DetectionStats.Requests != 1 || stored.DetectionStats.InputTokens != 7 || stored.DetectionStats.OutputTokens != 1 || stored.DetectionStats.KnownCostChecks != 0 || stored.DetectionStats.KnownCost != 0 {
		t.Fatalf("unknown pricing lost tokens or fabricated cost: %#v", stored.DetectionStats)
	}
	if stored.History[0].Cost == nil || stored.History[0].Cost.Known {
		t.Fatalf("unknown per-check cost was not marked unavailable: %#v", stored.History[0].Cost)
	}
	if err := scheduler.Delete(context.Background(), 1); err != nil {
		t.Fatal(err)
	}
	if _, err := stateStore.Get(1); !errors.Is(err, store.ErrNotFound) {
		t.Fatalf("configuration and statistics survived delete: %v", err)
	}
}
