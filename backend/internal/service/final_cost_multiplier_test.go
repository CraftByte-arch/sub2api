package service

import (
	"context"
	"math"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func finalCostProfitGateContext(threshold float64) context.Context {
	return context.WithValue(context.Background(), openAIProfitControlGateCtxKey{}, &openAIProfitControlGate{
		threshold: threshold,
		pricingAt: time.Now(),
	})
}

func TestFinalCostSchedulingSignalNeverMutatesBillingRate(t *testing.T) {
	billingRate := 0.9
	account := &Account{
		Platform:       PlatformOpenAI,
		Type:           AccountTypeAPIKey,
		RateMultiplier: &billingRate,
		Extra:          map[string]any{FinalCostMultiplierExtraKey: 0.16},
	}
	originalPointer := account.RateMultiplier

	signal, ok := resolveProfitSchedulingCostSignal(account)
	require.True(t, ok)
	require.Equal(t, 0.16, signal.multiplier)
	require.Equal(t, schedulingCostSourceFinalCost, signal.source)
	require.Same(t, originalPointer, account.RateMultiplier)
	require.Equal(t, 0.9, account.BillingRateMultiplier())
	require.Equal(t, 9.0, 10*account.BillingRateMultiplier(), "final cost must not affect account billing")
}

func TestFinalCostSchedulingSignalAllowsZeroAndRejectsInvalidValues(t *testing.T) {
	zero := &Account{Type: AccountTypeAPIKey, Extra: map[string]any{FinalCostMultiplierExtraKey: 0.0}}
	signal, ok := resolveFinalCostSchedulingSignal(zero)
	require.True(t, ok)
	require.Zero(t, signal.multiplier)

	for _, value := range []any{-1.0, math.NaN(), math.Inf(1), "not-a-number", nil} {
		account := &Account{Type: AccountTypeAPIKey, Extra: map[string]any{FinalCostMultiplierExtraKey: value}}
		_, ok := resolveFinalCostSchedulingSignal(account)
		require.False(t, ok, "value=%v", value)
	}
}

func TestFinalCostSchedulingSignalIgnoresOAuthAndFallsBackWithoutSidecarField(t *testing.T) {
	billingRate := 0.7
	oauth := &Account{
		Platform:       PlatformOpenAI,
		Type:           AccountTypeOAuth,
		RateMultiplier: &billingRate,
		Extra:          map[string]any{FinalCostMultiplierExtraKey: 0.1},
	}
	signal, ok := resolveProfitSchedulingCostSignal(oauth)
	require.True(t, ok)
	require.Equal(t, billingRate, signal.multiplier)
	require.Equal(t, schedulingCostSourceAccountRate, signal.source)

	legacy := &Account{Platform: PlatformOpenAI, Type: AccountTypeAPIKey, RateMultiplier: &billingRate}
	signal, ok = resolveProfitSchedulingCostSignal(legacy)
	require.True(t, ok)
	require.Equal(t, billingRate, signal.multiplier)
	require.Equal(t, schedulingCostSourceAccountRate, signal.source)
}

func TestOpenAISchedulingRatePrefersFinalCostAndFallsBackWithoutField(t *testing.T) {
	now := time.Now()
	probeCheap := upstreamCostTestAccount(1, UpstreamBillingProbeStatusOK, 0.2, now.Add(-time.Minute), 30*time.Minute)
	probeExpensive := upstreamCostTestAccount(2, UpstreamBillingProbeStatusOK, 0.8, now.Add(-time.Minute), 30*time.Minute)
	probeCheap.Extra[FinalCostMultiplierExtraKey] = 0.9
	probeExpensive.Extra[FinalCostMultiplierExtraKey] = 0.1

	cheapRate, ok := openAISchedulingRate(probeCheap, now, defaultOpenAIOAuthSchedulingRateMultiplier)
	require.True(t, ok)
	require.Equal(t, 0.9, cheapRate)
	expensiveRate, ok := openAISchedulingRate(probeExpensive, now, defaultOpenAIOAuthSchedulingRateMultiplier)
	require.True(t, ok)
	require.Equal(t, 0.1, expensiveRate)

	order := newOpenAILegacyUpstreamRateOrder([]*Account{probeCheap, probeExpensive}, now, defaultOpenAIOAuthSchedulingRateMultiplier)
	require.Negative(t, order.compare(probeExpensive, probeCheap), "lower final cost must rank first")

	legacy := upstreamCostTestAccount(3, UpstreamBillingProbeStatusOK, 0.2, now.Add(-time.Minute), 30*time.Minute)
	legacyRate, ok := openAISchedulingRate(legacy, now, defaultOpenAIOAuthSchedulingRateMultiplier)
	require.True(t, ok)
	require.Equal(t, 0.2, legacyRate, "standalone Sub2API must retain the original probe signal")
}

func TestProfitControlUsesFinalCostWithoutChangingBilling(t *testing.T) {
	billingRate := 0.1
	account := &Account{
		ID:             1,
		Platform:       PlatformOpenAI,
		Type:           AccountTypeAPIKey,
		RateMultiplier: &billingRate,
		Extra:          map[string]any{FinalCostMultiplierExtraKey: 0.8},
	}
	vetoed, reason := openAIProfitControlVetoReason(finalCostProfitGateContext(0.7), account)
	require.True(t, vetoed)
	require.Equal(t, openAIProfitFilterReasonThreshold, reason)
	require.Equal(t, 0.1, *account.RateMultiplier)

	account.Extra[FinalCostMultiplierExtraKey] = 0.0
	vetoed, _ = openAIProfitControlVetoReason(finalCostProfitGateContext(0), account)
	require.False(t, vetoed)
}

func TestProfitPreviewUsesFinalCostWithoutChangingBilling(t *testing.T) {
	now := time.Now()
	group := profitControlTestGroup(52, 0.2, 0)
	billingRate := 0.1
	account := upstreamCostTestAccount(1, UpstreamBillingProbeStatusOK, 0.9, now.Add(-time.Minute), 30*time.Minute)
	account.RateMultiplier = &billingRate
	account.Extra[FinalCostMultiplierExtraKey] = 0.8

	reports := PreviewProfitAdmission([]ProfitPreviewGroupInput{{
		Group:    group,
		Accounts: []*Account{account},
	}}, now)
	require.Len(t, reports, 1)
	require.Len(t, reports[0].Verdicts, 1)
	verdict := reports[0].Verdicts[0]
	require.Equal(t, ProfitPreviewRateSourceFinalCost, verdict.RateSource)
	require.Equal(t, 0.8, *verdict.AccountRate)
	require.Equal(t, ProfitPreviewClassAdmitted, verdict.Class)
	require.Equal(t, 0.1, *account.RateMultiplier)
}
