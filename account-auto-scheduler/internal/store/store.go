package store

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/Wei-Shaw/sub2api-account-auto-scheduler/internal/model"
)

var ErrNotFound = errors.New("account configuration not found")
var ErrUpstreamNotFound = errors.New("upstream not found")
var ErrProtectionNotFound = errors.New("group-account protection not found")

type Store struct {
	mu    sync.RWMutex
	path  string
	state model.State
}

func Open(path string) (*Store, error) {
	s := &Store{
		path: path,
		state: model.State{
			Version:     model.StateVersion,
			Accounts:    map[string]model.ManagedAccount{},
			Upstreams:   map[string]model.ManagedUpstream{},
			Protections: map[string]model.GroupAccountProtection{},
		},
	}
	if err := s.load(); err != nil {
		return nil, err
	}
	return s, nil
}

func (s *Store) load() error {
	raw, err := os.ReadFile(s.path)
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil {
		return fmt.Errorf("read state: %w", err)
	}
	var state model.State
	if err := json.Unmarshal(raw, &state); err != nil {
		return fmt.Errorf("decode state: %w", err)
	}
	if state.Version != model.LegacyStateVersion && state.Version != model.UpstreamStateVersion && state.Version != model.ProtectionStateVersion && state.Version != model.StateVersion {
		return fmt.Errorf("unsupported state version %d", state.Version)
	}
	if state.Accounts == nil {
		state.Accounts = map[string]model.ManagedAccount{}
	}
	if state.Upstreams == nil {
		state.Upstreams = map[string]model.ManagedUpstream{}
	}
	if state.Protections == nil {
		state.Protections = map[string]model.GroupAccountProtection{}
	}
	for key, account := range state.Accounts {
		account.Running = false
		if len(account.History) > model.HistoryLimit {
			account.History = account.History[:model.HistoryLimit]
		}
		account.DetectionStats = cloneDetectionStats(account.DetectionStats)
		state.Accounts[key] = account
	}
	for key, upstream := range state.Upstreams {
		if upstream.Identities == nil {
			upstream.Identities = map[string]model.UpstreamIdentity{}
		}
		for identityKey, identity := range upstream.Identities {
			if identity.Keys == nil {
				identity.Keys = map[string]model.RemoteKey{}
			}
			upstream.Identities[identityKey] = identity
		}
		state.Upstreams[key] = upstream
	}
	for key, protection := range state.Protections {
		if !protection.Valid() {
			return fmt.Errorf("invalid group-account protection %q", key)
		}
		normalizedKey := protectionKey(protection.GroupID, protection.AccountID)
		if key != normalizedKey {
			delete(state.Protections, key)
			state.Protections[normalizedKey] = protection
		}
	}
	state.Version = model.StateVersion
	s.state = state
	return nil
}

// ProtectionStore is optional so older manager test doubles and integrations
// remain source-compatible. The production store implements it.
type ProtectionStore interface {
	ListProtections() []model.GroupAccountProtection
	GetProtection(groupID, accountID int64) (model.GroupAccountProtection, error)
	PutProtection(protection model.GroupAccountProtection) error
	UpdateProtection(groupID, accountID int64, change func(*model.GroupAccountProtection) error) error
	DeleteProtection(groupID, accountID int64) error
}

func protectionKey(groupID, accountID int64) string {
	return strconv.FormatInt(groupID, 10) + ":" + strconv.FormatInt(accountID, 10)
}

func (s *Store) ListProtections() []model.GroupAccountProtection {
	s.mu.RLock()
	defer s.mu.RUnlock()
	items := make([]model.GroupAccountProtection, 0, len(s.state.Protections))
	for _, item := range s.state.Protections {
		items = append(items, cloneProtection(item))
	}
	sort.Slice(items, func(i, j int) bool {
		if items[i].GroupID == items[j].GroupID {
			return items[i].AccountID < items[j].AccountID
		}
		return items[i].GroupID < items[j].GroupID
	})
	return items
}

func (s *Store) GetProtection(groupID, accountID int64) (model.GroupAccountProtection, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	item, ok := s.state.Protections[protectionKey(groupID, accountID)]
	if !ok {
		return model.GroupAccountProtection{}, ErrProtectionNotFound
	}
	return cloneProtection(item), nil
}

func (s *Store) PutProtection(protection model.GroupAccountProtection) error {
	if !protection.Valid() {
		return errors.New("invalid group-account protection")
	}
	return s.UpdateState(func(state *model.State) error {
		if state.Protections == nil {
			state.Protections = map[string]model.GroupAccountProtection{}
		}
		state.Protections[protectionKey(protection.GroupID, protection.AccountID)] = cloneProtection(protection)
		return nil
	})
}

func (s *Store) UpdateProtection(groupID, accountID int64, change func(*model.GroupAccountProtection) error) error {
	return s.UpdateState(func(state *model.State) error {
		key := protectionKey(groupID, accountID)
		item, ok := state.Protections[key]
		if !ok {
			return ErrProtectionNotFound
		}
		item = cloneProtection(item)
		if err := change(&item); err != nil {
			return err
		}
		if !item.Valid() {
			return errors.New("invalid group-account protection")
		}
		state.Protections[key] = item
		return nil
	})
}

func (s *Store) DeleteProtection(groupID, accountID int64) error {
	return s.UpdateState(func(state *model.State) error {
		key := protectionKey(groupID, accountID)
		if _, ok := state.Protections[key]; !ok {
			return ErrProtectionNotFound
		}
		delete(state.Protections, key)
		return nil
	})
}

func (s *Store) List() []model.ManagedAccount {
	s.mu.RLock()
	defer s.mu.RUnlock()

	items := make([]model.ManagedAccount, 0, len(s.state.Accounts))
	for _, account := range s.state.Accounts {
		items = append(items, cloneAccount(account))
	}
	sort.Slice(items, func(i, j int) bool {
		if items[i].CreatedAt.Equal(items[j].CreatedAt) {
			return items[i].AccountID < items[j].AccountID
		}
		return items[i].CreatedAt.Before(items[j].CreatedAt)
	})
	return items
}

func (s *Store) Get(accountID int64) (model.ManagedAccount, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	account, ok := s.state.Accounts[strconv.FormatInt(accountID, 10)]
	if !ok {
		return model.ManagedAccount{}, ErrNotFound
	}
	return cloneAccount(account), nil
}

func (s *Store) Put(account model.ManagedAccount) error {
	return s.UpdateState(func(state *model.State) error {
		state.Accounts[strconv.FormatInt(account.AccountID, 10)] = cloneAccount(account)
		return nil
	})
}

func (s *Store) Update(accountID int64, change func(*model.ManagedAccount) error) error {
	return s.UpdateState(func(state *model.State) error {
		key := strconv.FormatInt(accountID, 10)
		account, ok := state.Accounts[key]
		if !ok {
			return ErrNotFound
		}
		if err := change(&account); err != nil {
			return err
		}
		account.Running = false
		if len(account.History) > model.HistoryLimit {
			account.History = account.History[:model.HistoryLimit]
		}
		state.Accounts[key] = account
		return nil
	})
}

func (s *Store) Delete(accountID int64) error {
	return s.UpdateState(func(state *model.State) error {
		key := strconv.FormatInt(accountID, 10)
		if _, ok := state.Accounts[key]; !ok {
			return ErrNotFound
		}
		delete(state.Accounts, key)
		return nil
	})
}

func (s *Store) ListUpstreams() []model.ManagedUpstream {
	s.mu.RLock()
	defer s.mu.RUnlock()

	items := make([]model.ManagedUpstream, 0, len(s.state.Upstreams))
	for _, upstream := range s.state.Upstreams {
		items = append(items, cloneUpstream(upstream))
	}
	sort.Slice(items, func(i, j int) bool {
		if items[i].CreatedAt.Equal(items[j].CreatedAt) {
			return items[i].ID < items[j].ID
		}
		return items[i].CreatedAt.Before(items[j].CreatedAt)
	})
	return items
}

func (s *Store) GetUpstream(upstreamID string) (model.ManagedUpstream, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	upstream, ok := s.state.Upstreams[strings.TrimSpace(upstreamID)]
	if !ok {
		return model.ManagedUpstream{}, ErrUpstreamNotFound
	}
	return cloneUpstream(upstream), nil
}

func (s *Store) PutUpstream(upstream model.ManagedUpstream) error {
	return s.UpdateState(func(state *model.State) error {
		if state.Upstreams == nil {
			state.Upstreams = map[string]model.ManagedUpstream{}
		}
		state.Upstreams[upstream.ID] = cloneUpstream(upstream)
		return nil
	})
}

func (s *Store) UpdateUpstream(upstreamID string, change func(*model.ManagedUpstream) error) error {
	return s.UpdateState(func(state *model.State) error {
		upstream, ok := state.Upstreams[strings.TrimSpace(upstreamID)]
		if !ok {
			return ErrUpstreamNotFound
		}
		upstream = cloneUpstream(upstream)
		if err := change(&upstream); err != nil {
			return err
		}
		state.Upstreams[upstream.ID] = cloneUpstream(upstream)
		return nil
	})
}

func (s *Store) DeleteUpstream(upstreamID string) error {
	return s.UpdateState(func(state *model.State) error {
		key := strings.TrimSpace(upstreamID)
		if _, ok := state.Upstreams[key]; !ok {
			return ErrUpstreamNotFound
		}
		delete(state.Upstreams, key)
		return nil
	})
}

func (s *Store) UpdateState(change func(*model.State) error) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	next := cloneState(s.state)
	if err := change(&next); err != nil {
		return err
	}
	next.Version = model.StateVersion
	if err := writeAtomic(s.path, next); err != nil {
		return err
	}
	s.state = next
	return nil
}

func writeAtomic(path string, state model.State) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return fmt.Errorf("create state directory: %w", err)
	}
	raw, err := json.MarshalIndent(state, "", "  ")
	if err != nil {
		return fmt.Errorf("encode state: %w", err)
	}
	raw = append(raw, '\n')

	tmp, err := os.CreateTemp(filepath.Dir(path), ".state-*.tmp")
	if err != nil {
		return fmt.Errorf("create temporary state: %w", err)
	}
	tmpName := tmp.Name()
	defer func() { _ = os.Remove(tmpName) }()

	if err := tmp.Chmod(0o600); err != nil {
		_ = tmp.Close()
		return fmt.Errorf("secure temporary state: %w", err)
	}
	if _, err := tmp.Write(raw); err != nil {
		_ = tmp.Close()
		return fmt.Errorf("write temporary state: %w", err)
	}
	if err := tmp.Sync(); err != nil {
		_ = tmp.Close()
		return fmt.Errorf("sync temporary state: %w", err)
	}
	if err := tmp.Close(); err != nil {
		return fmt.Errorf("close temporary state: %w", err)
	}
	if err := os.Rename(tmpName, path); err != nil {
		return fmt.Errorf("replace state: %w", err)
	}
	return nil
}

func cloneState(state model.State) model.State {
	cloned := model.State{
		Version:     state.Version,
		Accounts:    make(map[string]model.ManagedAccount, len(state.Accounts)),
		Upstreams:   make(map[string]model.ManagedUpstream, len(state.Upstreams)),
		Protections: make(map[string]model.GroupAccountProtection, len(state.Protections)),
	}
	for key, account := range state.Accounts {
		cloned.Accounts[key] = cloneAccount(account)
	}
	for key, upstream := range state.Upstreams {
		cloned.Upstreams[key] = cloneUpstream(upstream)
	}
	for key, protection := range state.Protections {
		cloned.Protections[key] = cloneProtection(protection)
	}
	return cloned
}

func cloneAccount(account model.ManagedAccount) model.ManagedAccount {
	account.History = append([]model.CheckResult(nil), account.History...)
	for index := range account.History {
		if account.History[index].Usage != nil {
			usage := *account.History[index].Usage
			account.History[index].Usage = &usage
		}
		if account.History[index].Cost != nil {
			cost := *account.History[index].Cost
			account.History[index].Cost = &cost
		}
	}
	account.DetectionStats = cloneDetectionStats(account.DetectionStats)
	account.LastCheckAt = cloneTime(account.LastCheckAt)
	account.NextCheckAt = cloneTime(account.NextCheckAt)
	if account.DirectProbe != nil {
		direct := *account.DirectProbe
		direct.ImportedAt = cloneTime(direct.ImportedAt)
		direct.UpdatedAt = cloneTime(direct.UpdatedAt)
		// Credential is a value-only encrypted envelope. Copy it explicitly so
		// callers can never share a mutable direct-probe config with store state.
		direct.Credential = model.CredentialEnvelope{
			Version:    direct.Credential.Version,
			Nonce:      direct.Credential.Nonce,
			Ciphertext: direct.Credential.Ciphertext,
		}
		account.DirectProbe = &direct
	}
	return account
}

func cloneDetectionStats(stats model.DetectionStats) model.DetectionStats {
	if stats.LastCost != nil {
		copy := *stats.LastCost
		stats.LastCost = &copy
	}
	if stats.LastUsageAt != nil {
		copy := *stats.LastUsageAt
		stats.LastUsageAt = &copy
	}
	return stats
}

func cloneProtection(protection model.GroupAccountProtection) model.GroupAccountProtection {
	return protection
}

func cloneTime(value *time.Time) *time.Time {
	if value == nil {
		return nil
	}
	copy := *value
	return &copy
}

func cloneUpstream(upstream model.ManagedUpstream) model.ManagedUpstream {
	if upstream.RechargeRate != nil {
		rate := *upstream.RechargeRate
		upstream.RechargeRate = &rate
	}
	identities := upstream.Identities
	upstream.Identities = make(map[string]model.UpstreamIdentity, len(identities))
	for key, identity := range identities {
		if identity.Balance != nil {
			balance := *identity.Balance
			if balance.RawQuota != nil {
				rawQuota := *balance.RawQuota
				balance.RawQuota = &rawQuota
			}
			if balance.QuotaPerUnit != nil {
				quotaPerUnit := *balance.QuotaPerUnit
				balance.QuotaPerUnit = &quotaPerUnit
			}
			identity.Balance = &balance
		}
		keys := identity.Keys
		identity.Keys = make(map[string]model.RemoteKey, len(keys))
		for keyID, remoteKey := range keys {
			if remoteKey.LocalAccountID != nil {
				accountID := *remoteKey.LocalAccountID
				remoteKey.LocalAccountID = &accountID
			}
			if remoteKey.QuotaLimit != nil {
				quota := *remoteKey.QuotaLimit
				remoteKey.QuotaLimit = &quota
			}
			if remoteKey.QuotaUsed != nil {
				used := *remoteKey.QuotaUsed
				remoteKey.QuotaUsed = &used
			}
			if remoteKey.Multiplier != nil {
				multiplier := *remoteKey.Multiplier
				remoteKey.Multiplier = &multiplier
			}
			identity.Keys[keyID] = remoteKey
		}
		upstream.Identities[key] = identity
	}
	return upstream
}
