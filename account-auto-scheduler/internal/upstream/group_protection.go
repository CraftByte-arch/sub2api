package upstream

import (
	"context"
	"errors"
	"fmt"
	"math"
	"sort"
	"strings"
	"time"

	"github.com/Wei-Shaw/sub2api-account-auto-scheduler/internal/model"
	"github.com/Wei-Shaw/sub2api-account-auto-scheduler/internal/store"
)

// GroupBindingWriter is implemented by the local Sub2API client. Keeping it
// optional preserves read-only manager integrations while the production
// sidecar can still apply physical group mutations through the existing API.
type GroupBindingWriter interface {
	SetAccountGroup(ctx context.Context, accountID, groupID int64, bound bool) (model.UpstreamAccount, error)
}

type GroupAccountProtectionView struct {
	GroupID              int64                              `json:"group_id"`
	AccountID            int64                              `json:"account_id"`
	ProtectionMultiplier float64                            `json:"protection_multiplier"`
	FinalMultiplier      *float64                           `json:"final_multiplier,omitempty"`
	Status               model.GroupAccountProtectionStatus `json:"status"`
	PhysicalBound        bool                               `json:"physical_bound"`
	LastError            string                             `json:"last_error,omitempty"`
	UpdatedAt            time.Time                          `json:"updated_at"`
}

type groupAccountSource struct {
	upstream   model.ManagedUpstream
	identityID string
	remoteKey  model.RemoteKey
}

func protectionStateStore(m *Manager) (store.ProtectionStore, error) {
	if m == nil || m.store == nil {
		return nil, errors.New("protection state store is unavailable")
	}
	stateStore, ok := m.store.(store.ProtectionStore)
	if !ok {
		return nil, errors.New("protection state store is not supported")
	}
	return stateStore, nil
}

func (m *Manager) GroupAccountProtectionViews(accounts []model.UpstreamAccount) map[int64]map[string]GroupAccountProtectionView {
	result := make(map[int64]map[string]GroupAccountProtectionView)
	stateStore, err := protectionStateStore(m)
	if err != nil {
		return result
	}
	accountByID := make(map[int64]model.UpstreamAccount, len(accounts))
	for _, account := range accounts {
		accountByID[account.ID] = account
	}
	for _, record := range stateStore.ListProtections() {
		account, exists := accountByID[record.AccountID]
		view := GroupAccountProtectionView{
			GroupID:              record.GroupID,
			AccountID:            record.AccountID,
			ProtectionMultiplier: record.ProtectionMultiplier,
			Status:               record.Status,
			PhysicalBound:        exists && containsGroupID(account.GroupIDs, record.GroupID),
			LastError:            record.LastError,
			UpdatedAt:            record.UpdatedAt,
		}
		if !exists {
			view.Status = model.ProtectionUnavailable
			if view.LastError == "" {
				view.LastError = "本地账号不存在"
			}
		} else if source, final, sourceStatus := m.resolveProtectionSource(account, record); sourceStatus == "" {
			view.FinalMultiplier = final
			if final == nil {
				view.Status = model.ProtectionUnavailable
			} else if exceedsProtection(*final, record.ProtectionMultiplier) {
				view.Status = model.ProtectionExceeded
			} else if view.PhysicalBound {
				view.Status = model.ProtectionBound
			} else {
				view.Status = model.ProtectionUnbound
			}
			_ = source
		} else if view.Status == "" {
			view.Status = model.ProtectionUnavailable
			view.LastError = sourceStatus
		}
		if result[record.AccountID] == nil {
			result[record.AccountID] = map[string]GroupAccountProtectionView{}
		}
		result[record.AccountID][groupKey(record.GroupID)] = view
	}
	return result
}

// LogicalGroupIDs merges physical Sub2API membership with persisted logical
// protection membership. A protected key therefore remains visible after the
// guard physically unbinds it.
func (m *Manager) LogicalGroupIDs(accounts []model.UpstreamAccount) map[int64][]int64 {
	result := make(map[int64][]int64, len(accounts))
	projections := m.GroupAccountProtectionViews(accounts)
	for _, account := range accounts {
		ids := append([]int64(nil), account.GroupIDs...)
		seen := make(map[int64]struct{}, len(ids))
		for _, id := range ids {
			seen[id] = struct{}{}
		}
		for key := range projections[account.ID] {
			id, parseErr := parseGroupKey(key)
			if parseErr == nil {
				if _, exists := seen[id]; !exists {
					ids = append(ids, id)
				}
			}
		}
		sort.Slice(ids, func(i, j int) bool { return ids[i] < ids[j] })
		result[account.ID] = ids
	}
	return result
}

func (m *Manager) SetGroupAccountProtection(ctx context.Context, groupID, accountID int64, multiplier float64) (GroupAccountProtectionView, error) {
	if groupID <= 0 || accountID <= 0 {
		return GroupAccountProtectionView{}, errors.New("group ID and account ID must be positive")
	}
	if multiplier < 0 || math.IsNaN(multiplier) || math.IsInf(multiplier, 0) {
		return GroupAccountProtectionView{}, errors.New("保护倍率必须是大于等于 0 的有限数值")
	}
	stateStore, err := protectionStateStore(m)
	if err != nil {
		return GroupAccountProtectionView{}, err
	}
	var accounts []model.UpstreamAccount
	m.protectionMu.Lock()
	err = func() error {
		var listErr error
		accounts, listErr = m.core.ListAccounts(ctx)
		if listErr != nil {
			return listErr
		}
		account, found := findAccountByID(accounts, accountID)
		if !found || !account.IsAPIKey() {
			return errors.New("仅支持 API Key 账号")
		}
		if !containsGroupID(account.GroupIDs, groupID) {
			if _, getErr := stateStore.GetProtection(groupID, accountID); getErr != nil {
				return errors.New("请先将账号绑定到当前分组")
			}
		}
		now := time.Now().UTC()
		record, getErr := stateStore.GetProtection(groupID, accountID)
		if errors.Is(getErr, store.ErrProtectionNotFound) {
			record = model.GroupAccountProtection{
				GroupID: groupID, AccountID: accountID, Status: model.ProtectionBound, CreatedAt: now,
			}
		} else if getErr != nil {
			return getErr
		}
		record.ProtectionMultiplier = multiplier
		record.PhysicalBound = containsGroupID(account.GroupIDs, groupID)
		record.UpdatedAt = now
		if source, _, _ := m.resolveProtectionSource(account, record); source != nil {
			record.UpstreamID = source.upstream.ID
			record.IdentityID = source.identityID
			record.RemoteKeyID = source.remoteKey.ID
		}
		return stateStore.PutProtection(record)
	}()
	m.protectionMu.Unlock()
	if err != nil {
		return GroupAccountProtectionView{}, err
	}
	if err := m.reconcileProtectedBindings(ctx, accounts); err != nil {
		m.logger.Warn("group-account protection reconciliation failed", "group_id", groupID, "account_id", accountID, "error", err)
	}
	return m.protectionViewFor(ctx, groupID, accountID)
}

// ReleaseGroupAccountProtection binds first and removes the protection record
// only after the physical operation succeeds.
func (m *Manager) ReleaseGroupAccountProtection(ctx context.Context, groupID, accountID int64) (GroupAccountProtectionView, error) {
	stateStore, err := protectionStateStore(m)
	if err != nil {
		return GroupAccountProtectionView{}, err
	}
	m.protectionMu.Lock()
	defer m.protectionMu.Unlock()
	if _, err := stateStore.GetProtection(groupID, accountID); err != nil {
		return GroupAccountProtectionView{}, err
	}
	writer, ok := m.core.(GroupBindingWriter)
	if !ok {
		return GroupAccountProtectionView{}, errors.New("当前 Sub2API 客户端不支持分组绑定")
	}
	updated, err := writer.SetAccountGroup(ctx, accountID, groupID, true)
	if err != nil {
		return GroupAccountProtectionView{}, fmt.Errorf("恢复分组绑定失败: %w", err)
	}
	if !containsGroupID(updated.GroupIDs, groupID) {
		return GroupAccountProtectionView{}, errors.New("恢复分组绑定失败: Sub2API 未返回已绑定状态")
	}
	if err := stateStore.DeleteProtection(groupID, accountID); err != nil && !errors.Is(err, store.ErrProtectionNotFound) {
		return GroupAccountProtectionView{}, err
	}
	return GroupAccountProtectionView{GroupID: groupID, AccountID: accountID, Status: model.ProtectionBound, PhysicalBound: true, UpdatedAt: time.Now().UTC()}, nil
}

// RemoveGroupAccountBinding deletes logical intent before touching Sub2API.
// This ordering is the no-rebind guarantee for a manually removed account.
func (m *Manager) RemoveGroupAccountBinding(ctx context.Context, groupID, accountID int64) error {
	stateStore, err := protectionStateStore(m)
	if err != nil {
		return err
	}
	m.protectionMu.Lock()
	defer m.protectionMu.Unlock()
	if err := stateStore.DeleteProtection(groupID, accountID); err != nil && !errors.Is(err, store.ErrProtectionNotFound) {
		return err
	}
	if accounts, listErr := m.core.ListAccounts(ctx); listErr == nil {
		if account, found := findAccountByID(accounts, accountID); found && !containsGroupID(account.GroupIDs, groupID) {
			return nil
		}
	}
	writer, ok := m.core.(GroupBindingWriter)
	if !ok {
		return errors.New("当前 Sub2API 客户端不支持分组绑定")
	}
	if _, err := writer.SetAccountGroup(ctx, accountID, groupID, false); err != nil {
		return fmt.Errorf("移除分组绑定失败: %w", err)
	}
	return nil
}

// SetGroupAccountBinding is the protection-aware single-binding entry point.
// It prevents callers of the legacy sidecar route from bypassing an active
// exceeded guard by directly requesting a physical bind.
func (m *Manager) SetGroupAccountBinding(ctx context.Context, groupID, accountID int64, bound bool) error {
	if !bound {
		return m.RemoveGroupAccountBinding(ctx, groupID, accountID)
	}
	stateStore, err := protectionStateStore(m)
	if err != nil {
		return err
	}
	writer, ok := m.core.(GroupBindingWriter)
	if !ok {
		return errors.New("当前 Sub2API 客户端不支持分组绑定")
	}
	m.protectionMu.Lock()
	defer m.protectionMu.Unlock()
	accounts, err := m.core.ListAccounts(ctx)
	if err != nil {
		return err
	}
	account, found := findAccountByID(accounts, accountID)
	if !found || !account.IsAPIKey() {
		return errors.New("仅支持 API Key 账号")
	}
	if record, getErr := stateStore.GetProtection(groupID, accountID); getErr == nil {
		_, final, sourceStatus := m.resolveProtectionSource(account, record)
		if sourceStatus != "" || final == nil {
			return fmt.Errorf("保护倍率正在生效，当前最终倍率不可用: %s", firstNonEmpty(sourceStatus, "未知原因"))
		}
		if exceedsProtection(*final, record.ProtectionMultiplier) {
			return errors.New("当前最终倍率超过保护倍率，请先使用“解除倍率保护”")
		}
	} else if !errors.Is(getErr, store.ErrProtectionNotFound) {
		return getErr
	}
	_, err = writer.SetAccountGroup(ctx, accountID, groupID, true)
	return err
}

// SaveGroupBindings applies a binding-dialog selection while honoring logical
// protected membership and deleting protection before every deselection.
func (m *Manager) SaveGroupBindings(ctx context.Context, groupID int64, selected []int64) ([]int64, []GroupBindingFailure, error) {
	stateStore, err := protectionStateStore(m)
	if err != nil {
		return nil, nil, err
	}
	writer, ok := m.core.(GroupBindingWriter)
	if !ok {
		return nil, nil, errors.New("当前 Sub2API 客户端不支持分组绑定")
	}
	accounts, err := m.core.ListAccounts(ctx)
	if err != nil {
		return nil, nil, err
	}
	selectedSet := make(map[int64]struct{}, len(selected))
	for _, accountID := range selected {
		selectedSet[accountID] = struct{}{}
	}
	m.protectionMu.Lock()
	defer m.protectionMu.Unlock()
	updated := make([]int64, 0)
	failures := make([]GroupBindingFailure, 0)
	for _, account := range accounts {
		if !account.IsAPIKey() {
			continue
		}
		_, desired := selectedSet[account.ID]
		physical := containsGroupID(account.GroupIDs, groupID)
		_, logicalErr := stateStore.GetProtection(groupID, account.ID)
		logical := logicalErr == nil || physical
		if desired == logical && (!desired || physical) {
			continue
		}
		if !desired {
			if err := stateStore.DeleteProtection(groupID, account.ID); err != nil && !errors.Is(err, store.ErrProtectionNotFound) {
				failures = append(failures, GroupBindingFailure{AccountID: account.ID, Name: account.Name, Code: "PROTECTION_DELETE_FAILED", Message: err.Error()})
				continue
			}
		}
		// A selected protected logical member may be physically unbound because
		// its multiplier exceeded the threshold. Keeping it selected in the
		// dialog must not bypass the guard; only the explicit release action may
		// bind it and remove protection.
		if desired && logicalErr == nil && !physical {
			continue
		}
		if desired && !physical {
			if _, err := writer.SetAccountGroup(ctx, account.ID, groupID, true); err != nil {
				failures = append(failures, GroupBindingFailure{AccountID: account.ID, Name: account.Name, Code: "BINDING_FAILED", Message: err.Error()})
				continue
			}
		} else if !desired && physical {
			if _, err := writer.SetAccountGroup(ctx, account.ID, groupID, false); err != nil {
				failures = append(failures, GroupBindingFailure{AccountID: account.ID, Name: account.Name, Code: "BINDING_FAILED", Message: err.Error()})
				continue
			}
		}
		updated = append(updated, account.ID)
	}
	return updated, failures, nil
}

type GroupBindingFailure struct {
	AccountID int64  `json:"account_id"`
	Name      string `json:"name"`
	Code      string `json:"code"`
	Message   string `json:"message"`
}

func (m *Manager) protectionViewFor(ctx context.Context, groupID, accountID int64) (GroupAccountProtectionView, error) {
	accounts, err := m.core.ListAccounts(ctx)
	if err != nil {
		return GroupAccountProtectionView{}, err
	}
	views := m.GroupAccountProtectionViews(accounts)[accountID]
	view, ok := views[groupKey(groupID)]
	if !ok {
		return GroupAccountProtectionView{}, store.ErrProtectionNotFound
	}
	return view, nil
}

func (m *Manager) reconcileProtectedBindings(ctx context.Context, accounts []model.UpstreamAccount) error {
	stateStore, err := protectionStateStore(m)
	if err != nil {
		return nil
	}
	writer, writerAvailable := m.core.(GroupBindingWriter)
	m.protectionMu.Lock()
	defer m.protectionMu.Unlock()
	accountByID := make(map[int64]model.UpstreamAccount, len(accounts))
	for _, account := range accounts {
		accountByID[account.ID] = account
	}
	var firstErr error
	for _, record := range stateStore.ListProtections() {
		account, exists := accountByID[record.AccountID]
		physical := exists && containsGroupID(account.GroupIDs, record.GroupID)
		old := record
		record.PhysicalBound = physical
		record.LastError = ""
		if !exists {
			record.Status = model.ProtectionUnavailable
			record.LastError = "本地账号不存在"
			_ = m.persistProtectionIfChanged(stateStore, old, record)
			continue
		}
		source, final, sourceStatus := m.resolveProtectionSource(account, record)
		if source != nil {
			record.UpstreamID = source.upstream.ID
			record.IdentityID = source.identityID
			record.RemoteKeyID = source.remoteKey.ID
		}
		if sourceStatus != "" || final == nil {
			record.Status = model.ProtectionUnavailable
			record.LastError = firstNonEmpty(sourceStatus, "最终倍率暂不可用")
			_ = m.persistProtectionIfChanged(stateStore, old, record)
			continue
		}

		if exceedsProtection(*final, record.ProtectionMultiplier) {
			record.Status = model.ProtectionExceeded
			if physical {
				if !writerAvailable {
					record.LastError = "当前 Sub2API 客户端不支持分组解绑"
				} else if updated, bindErr := writer.SetAccountGroup(ctx, record.AccountID, record.GroupID, false); bindErr != nil {
					record.LastError = bindErr.Error()
					if firstErr == nil {
						firstErr = bindErr
					}
				} else {
					physical = containsGroupID(updated.GroupIDs, record.GroupID)
					record.PhysicalBound = physical
				}
			}
		} else {
			record.Status = model.ProtectionBound
			if !physical {
				if !writerAvailable {
					record.LastError = "当前 Sub2API 客户端不支持分组绑定"
				} else if updated, bindErr := writer.SetAccountGroup(ctx, record.AccountID, record.GroupID, true); bindErr != nil {
					record.Status = model.ProtectionUnbound
					record.LastError = bindErr.Error()
					if firstErr == nil {
						firstErr = bindErr
					}
				} else {
					physical = containsGroupID(updated.GroupIDs, record.GroupID)
					record.PhysicalBound = physical
				}
			}
		}
		record.UpdatedAt = time.Now().UTC()
		if err := m.persistProtectionIfChanged(stateStore, old, record); err != nil && firstErr == nil {
			firstErr = err
		}
	}
	return firstErr
}

func (m *Manager) persistProtectionIfChanged(stateStore store.ProtectionStore, old, next model.GroupAccountProtection) error {
	if old.ProtectionMultiplier == next.ProtectionMultiplier && old.UpstreamID == next.UpstreamID && old.IdentityID == next.IdentityID && old.RemoteKeyID == next.RemoteKeyID && old.Status == next.Status && old.PhysicalBound == next.PhysicalBound && old.LastError == next.LastError {
		return nil
	}
	return stateStore.PutProtection(next)
}

func (m *Manager) resolveProtectionSource(account model.UpstreamAccount, record model.GroupAccountProtection) (*groupAccountSource, *float64, string) {
	accountRoot, err := model.NormalizeUpstreamBaseURL(account.BaseURL())
	if err != nil || accountRoot == "" {
		return nil, nil, "本地账号上游地址无效"
	}
	sources := make([]groupAccountSource, 0)
	for _, upstream := range m.store.ListUpstreams() {
		upstreamRoot, normalizeErr := model.NormalizeUpstreamBaseURL(upstream.BaseURL)
		if normalizeErr != nil || upstreamRoot != accountRoot {
			continue
		}
		for identityID, identity := range upstream.Identities {
			for _, remoteKey := range identity.Keys {
				if remoteKey.Stale || remoteKey.LocalAccountID == nil || *remoteKey.LocalAccountID != account.ID {
					continue
				}
				if record.UpstreamID != "" && record.UpstreamID != upstream.ID {
					continue
				}
				if record.IdentityID != "" && record.IdentityID != identityID {
					continue
				}
				if record.RemoteKeyID != "" && record.RemoteKeyID != remoteKey.ID {
					continue
				}
				sources = append(sources, groupAccountSource{upstream: upstream, identityID: identityID, remoteKey: remoteKey})
			}
		}
	}
	if len(sources) == 0 && (record.UpstreamID != "" || record.IdentityID != "" || record.RemoteKeyID != "") {
		return nil, nil, "保护倍率来源 Key 已不存在"
	}
	if len(sources) == 0 {
		for _, upstream := range m.store.ListUpstreams() {
			upstreamRoot, normalizeErr := model.NormalizeUpstreamBaseURL(upstream.BaseURL)
			if normalizeErr != nil || upstreamRoot != accountRoot {
				continue
			}
			for identityID, identity := range upstream.Identities {
				for _, remoteKey := range identity.Keys {
					if !remoteKey.Stale && remoteKey.LocalAccountID != nil && *remoteKey.LocalAccountID == account.ID {
						sources = append(sources, groupAccountSource{upstream: upstream, identityID: identityID, remoteKey: remoteKey})
					}
				}
			}
		}
	}
	if len(sources) != 1 {
		if len(sources) > 1 {
			return nil, nil, "存在多个有效上游 Key，无法确定保护倍率来源"
		}
		return nil, nil, "未找到有效上游 Key"
	}
	source := sources[0]
	final := deriveFinalMultiplier(source.upstream.RechargeRate, source.remoteKey.Multiplier)
	if final == nil {
		return &source, nil, "充值倍率或分组倍率尚未同步"
	}
	return &source, final, ""
}

func findAccountByID(accounts []model.UpstreamAccount, accountID int64) (model.UpstreamAccount, bool) {
	for _, account := range accounts {
		if account.ID == accountID {
			return account, true
		}
	}
	return model.UpstreamAccount{}, false
}

func containsGroupID(ids []int64, target int64) bool {
	for _, id := range ids {
		if id == target {
			return true
		}
	}
	return false
}

func groupKey(groupID int64) string { return fmt.Sprintf("%d", groupID) }

func parseGroupKey(value string) (int64, error) {
	var groupID int64
	if _, err := fmt.Sscanf(strings.TrimSpace(value), "%d", &groupID); err != nil || groupID <= 0 {
		return 0, errors.New("invalid group key")
	}
	return groupID, nil
}

func exceedsProtection(finalMultiplier, protectionMultiplier float64) bool {
	tolerance := 1e-12 * math.Max(1, math.Max(math.Abs(finalMultiplier), math.Abs(protectionMultiplier)))
	return finalMultiplier-protectionMultiplier > tolerance
}
