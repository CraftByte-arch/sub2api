package service

import "math"

// FinalCostMultiplierExtraKey stores an optional scheduling-only cost signal.
// It is deliberately separate from Account.RateMultiplier, which remains the
// sole account billing, quota-consumption, and usage-statistics multiplier.
const FinalCostMultiplierExtraKey = "final_cost_multiplier"

type schedulingCostSource uint8

const (
	schedulingCostSourceAccountRate schedulingCostSource = iota
	schedulingCostSourceFinalCost
)

type schedulingCostSignal struct {
	multiplier float64
	source     schedulingCostSource
}

// resolveFinalCostSchedulingSignal reads the optional API Key scheduling cost.
// It returns a scalar signal only: it never copies or mutates the account and
// therefore cannot leak a scheduling value into account billing.
func resolveFinalCostSchedulingSignal(account *Account) (schedulingCostSignal, bool) {
	if account == nil || account.Type != AccountTypeAPIKey {
		return schedulingCostSignal{}, false
	}
	value, ok := resolveAccountExtraNumber(account.Extra, FinalCostMultiplierExtraKey)
	if !ok || value < 0 || math.IsNaN(value) || math.IsInf(value, 0) {
		return schedulingCostSignal{}, false
	}
	return schedulingCostSignal{multiplier: value, source: schedulingCostSourceFinalCost}, true
}

func resolveOpenAIFinalCostSchedulingSignal(account *Account) (schedulingCostSignal, bool) {
	if account == nil || !account.IsOpenAIApiKey() {
		return schedulingCostSignal{}, false
	}
	return resolveFinalCostSchedulingSignal(account)
}

// resolveProfitSchedulingCostSignal keeps the historical account-rate fallback
// byte-for-byte equivalent in meaning when no valid final-cost field exists.
func resolveProfitSchedulingCostSignal(account *Account) (schedulingCostSignal, bool) {
	if signal, ok := resolveFinalCostSchedulingSignal(account); ok {
		return signal, true
	}
	if account == nil || account.RateMultiplier == nil ||
		math.IsNaN(*account.RateMultiplier) || math.IsInf(*account.RateMultiplier, 0) ||
		*account.RateMultiplier < 0 {
		return schedulingCostSignal{}, false
	}
	return schedulingCostSignal{
		multiplier: *account.RateMultiplier,
		source:     schedulingCostSourceAccountRate,
	}, true
}
