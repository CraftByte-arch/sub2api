package upstream

import (
	"context"
	"errors"
	"math"
	"strings"
	"time"

	"github.com/Wei-Shaw/sub2api-account-auto-scheduler/internal/model"
)

const accountQuotaExhaustedSentinel = 1e-9

const (
	accountBalanceQuotaStatusUnbound            = "unbound"
	accountBalanceQuotaStatusInvalidUpstream    = "invalid_upstream"
	accountBalanceQuotaStatusStaleBinding       = "stale_binding"
	accountBalanceQuotaStatusAmbiguous          = "ambiguous"
	accountBalanceQuotaStatusBalanceUnavailable = "balance_unavailable"
	accountBalanceQuotaStatusBalanceStale       = "balance_stale"
	accountBalanceQuotaStatusBalanceUnsupported = "balance_unsupported"
	accountBalanceQuotaStatusMultiplierUnknown  = "multiplier_unknown"
	accountBalanceQuotaStatusAvailable          = "available"
)

type AccountBalanceQuotaWriter interface {
	SetAccountBalanceQuota(ctx context.Context, accountID int64, update model.AccountBalanceQuotaUpdate) error
}

type localAccountBalanceQuotaProjection struct {
	status     string
	remaining  *float64
	observedAt *time.Time
	unlimited  bool
}

type boundBalanceQuotaSource struct {
	upstream model.ManagedUpstream
	identity model.UpstreamIdentity
	key      model.RemoteKey
}

func (m *Manager) localAccountBalanceQuotaProjections(accounts []model.UpstreamAccount) map[int64]localAccountBalanceQuotaProjection {
	result := make(map[int64]localAccountBalanceQuotaProjection)
	if m == nil {
		return result
	}

	boundByAccount := make(map[int64][]boundBalanceQuotaSource)
	for _, upstream := range m.store.ListUpstreams() {
		for _, identity := range upstream.Identities {
			for _, remoteKey := range identity.Keys {
				if remoteKey.LocalAccountID == nil {
					continue
				}
				boundByAccount[*remoteKey.LocalAccountID] = append(boundByAccount[*remoteKey.LocalAccountID], boundBalanceQuotaSource{
					upstream: upstream,
					identity: identity,
					key:      remoteKey,
				})
			}
		}
	}

	for _, account := range accounts {
		if !account.IsAPIKey() {
			continue
		}
		projection := localAccountBalanceQuotaProjection{status: accountBalanceQuotaStatusUnbound}
		accountRoot, err := model.NormalizeUpstreamBaseURL(account.BaseURL())
		if err != nil || accountRoot == "" {
			projection.status = accountBalanceQuotaStatusInvalidUpstream
			result[account.ID] = projection
			continue
		}

		active := make([]boundBalanceQuotaSource, 0, 1)
		hasStaleBinding := false
		for _, source := range boundByAccount[account.ID] {
			upstreamRoot, normalizeErr := model.NormalizeUpstreamBaseURL(source.upstream.BaseURL)
			if normalizeErr != nil || source.key.Stale || upstreamRoot != accountRoot {
				hasStaleBinding = true
				continue
			}
			active = append(active, source)
		}

		switch len(active) {
		case 0:
			if hasStaleBinding {
				projection.status = accountBalanceQuotaStatusStaleBinding
			}
		case 1:
			projection = balanceQuotaProjection(active[0])
		default:
			projection.status = accountBalanceQuotaStatusAmbiguous
		}
		result[account.ID] = projection
	}
	return result
}

func balanceQuotaProjection(source boundBalanceQuotaSource) localAccountBalanceQuotaProjection {
	multiplier := source.key.Multiplier
	if multiplier == nil || *multiplier < 0 || math.IsNaN(*multiplier) || math.IsInf(*multiplier, 0) {
		return localAccountBalanceQuotaProjection{status: accountBalanceQuotaStatusMultiplierUnknown}
	}
	balance := source.identity.Balance
	if balance == nil {
		return localAccountBalanceQuotaProjection{status: accountBalanceQuotaStatusBalanceUnavailable}
	}
	if balance.Stale {
		return localAccountBalanceQuotaProjection{status: accountBalanceQuotaStatusBalanceStale}
	}
	if !strings.EqualFold(strings.TrimSpace(balance.Unit), "USD") {
		return localAccountBalanceQuotaProjection{status: accountBalanceQuotaStatusBalanceUnsupported}
	}
	if math.IsNaN(balance.Amount) || math.IsInf(balance.Amount, 0) {
		return localAccountBalanceQuotaProjection{status: accountBalanceQuotaStatusBalanceUnavailable}
	}
	observedAt := balance.ObservedAt
	if *multiplier == 0 {
		return localAccountBalanceQuotaProjection{
			status:     accountBalanceQuotaStatusAvailable,
			observedAt: &observedAt,
			unlimited:  true,
		}
	}
	remaining := math.Max(balance.Amount, 0) / *multiplier
	if math.IsNaN(remaining) || math.IsInf(remaining, 0) {
		return localAccountBalanceQuotaProjection{status: accountBalanceQuotaStatusBalanceUnavailable}
	}
	return localAccountBalanceQuotaProjection{
		status:     accountBalanceQuotaStatusAvailable,
		remaining:  &remaining,
		observedAt: &observedAt,
	}
}

func finiteQuotaValue(value *float64) float64 {
	if value == nil || *value < 0 || math.IsNaN(*value) || math.IsInf(*value, 0) {
		return 0
	}
	return *value
}

func sameQuotaValue(left, right float64) bool {
	return math.Abs(left-right) <= 1e-9*math.Max(1, math.Max(math.Abs(left), math.Abs(right)))
}

func accountBalanceQuotaUpdate(account model.UpstreamAccount, projection localAccountBalanceQuotaProjection) (model.AccountBalanceQuotaUpdate, bool) {
	if projection.status != accountBalanceQuotaStatusAvailable {
		return model.AccountBalanceQuotaUpdate{}, false
	}
	used := finiteQuotaValue(account.QuotaUsed)
	update := model.AccountBalanceQuotaUpdate{
		Managed:    true,
		ObservedAt: projection.observedAt,
		Unlimited:  projection.unlimited,
	}
	if projection.unlimited {
		return update, true
	}
	remaining := 0.0
	if projection.remaining != nil {
		remaining = *projection.remaining
		value := remaining
		update.Remaining = &value
	}
	if remaining <= 0 {
		update.Exhausted = true
		if used <= 0 {
			sentinel := accountQuotaExhaustedSentinel
			update.QuotaLimit = sentinel
			update.QuotaUsed = &sentinel
			return update, true
		}
		update.QuotaLimit = used
		return update, true
	}
	limit := used + remaining
	if math.IsNaN(limit) || math.IsInf(limit, 0) {
		return model.AccountBalanceQuotaUpdate{}, false
	}
	update.QuotaLimit = limit
	return update, true
}

func clearManagedAccountBalanceQuotaUpdate() model.AccountBalanceQuotaUpdate {
	return model.AccountBalanceQuotaUpdate{Managed: false, QuotaLimit: 0}
}

func sameAppliedAccountBalanceQuotaProjection(account model.UpstreamAccount, update model.AccountBalanceQuotaUpdate) bool {
	if !update.Managed || !account.HasManagedUpstreamBalanceQuota() || update.ObservedAt == nil {
		return false
	}
	currentObservedAt, ok := account.ManagedUpstreamBalanceQuotaObservedAt()
	if !ok || !currentObservedAt.Equal(*update.ObservedAt) ||
		account.ManagedUpstreamBalanceQuotaExhausted() != update.Exhausted ||
		account.ManagedUpstreamBalanceQuotaUnlimited() != update.Unlimited {
		return false
	}
	currentRemaining, hasCurrentRemaining := account.ManagedUpstreamBalanceQuotaRemaining()
	if update.Remaining == nil {
		return !hasCurrentRemaining
	}
	return hasCurrentRemaining && sameQuotaValue(*currentRemaining, *update.Remaining)
}

func accountBalanceQuotaUpdateNeeded(account model.UpstreamAccount, update model.AccountBalanceQuotaUpdate) bool {
	if update.Managed {
		return !sameAppliedAccountBalanceQuotaProjection(account, update)
	}
	if account.HasManagedUpstreamBalanceQuota() != update.Managed {
		return true
	}
	if !sameQuotaValue(finiteQuotaValue(account.QuotaLimit), update.QuotaLimit) {
		return true
	}
	return update.QuotaUsed != nil && !sameQuotaValue(finiteQuotaValue(account.QuotaUsed), *update.QuotaUsed)
}

func shouldClearManagedAccountBalanceQuota(account model.UpstreamAccount, projection localAccountBalanceQuotaProjection) bool {
	if !account.HasManagedUpstreamBalanceQuota() {
		return false
	}
	if !account.IsAPIKey() {
		return true
	}
	return projection.status == accountBalanceQuotaStatusUnbound || projection.status == accountBalanceQuotaStatusInvalidUpstream
}

func (m *Manager) reconcileAccountBalanceQuotasForAccounts(ctx context.Context, accounts []model.UpstreamAccount) error {
	writer, ok := m.core.(AccountBalanceQuotaWriter)
	if !ok {
		return nil
	}
	projections := m.localAccountBalanceQuotaProjections(accounts)
	var firstErr error
	for _, account := range accounts {
		projection := projections[account.ID]
		update, available := accountBalanceQuotaUpdate(account, projection)
		if !available {
			if !shouldClearManagedAccountBalanceQuota(account, projection) {
				continue
			}
			update = clearManagedAccountBalanceQuotaUpdate()
		}
		if !accountBalanceQuotaUpdateNeeded(account, update) {
			continue
		}
		if err := writer.SetAccountBalanceQuota(ctx, account.ID, update); err != nil {
			if firstErr == nil {
				firstErr = err
			}
			m.logger.Warn("upstream balance quota sync failed", "account_id", account.ID, "error", err)
		}
	}
	return firstErr
}

func (m *Manager) reconcileLocalAccountStateForAccounts(ctx context.Context, accounts []model.UpstreamAccount) error {
	return errors.Join(
		m.reconcileFinalCostMultipliersForAccounts(ctx, accounts),
		m.reconcileAccountBalanceQuotasForAccounts(ctx, accounts),
	)
}

func (m *Manager) reconcileLocalAccountState(ctx context.Context) error {
	accounts, err := m.core.ListAccounts(ctx)
	if err != nil {
		return err
	}
	if _, err := m.reconcileLocalAccountBindings(accounts); err != nil {
		return err
	}
	return m.reconcileLocalAccountStateForAccounts(ctx, accounts)
}

func (m *Manager) reconcileLocalAccountStateBestEffort(ctx context.Context) {
	if err := m.reconcileLocalAccountState(ctx); err != nil && !errors.Is(err, context.Canceled) {
		m.logger.Warn("local account state reconciliation incomplete", "error", err)
	}
}
