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

type Store struct {
	mu    sync.RWMutex
	path  string
	state model.State
}

func Open(path string) (*Store, error) {
	s := &Store{
		path: path,
		state: model.State{
			Version:   model.StateVersion,
			Accounts:  map[string]model.ManagedAccount{},
			Upstreams: map[string]model.ManagedUpstream{},
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
	if state.Version != model.LegacyStateVersion && state.Version != model.UpstreamStateVersion && state.Version != model.StateVersion {
		return fmt.Errorf("unsupported state version %d", state.Version)
	}
	if state.Accounts == nil {
		state.Accounts = map[string]model.ManagedAccount{}
	}
	if state.Upstreams == nil {
		state.Upstreams = map[string]model.ManagedUpstream{}
	}
	for key, account := range state.Accounts {
		account.Running = false
		if len(account.History) > model.HistoryLimit {
			account.History = account.History[:model.HistoryLimit]
		}
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
	state.Version = model.StateVersion
	s.state = state
	return nil
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
		Version:   state.Version,
		Accounts:  make(map[string]model.ManagedAccount, len(state.Accounts)),
		Upstreams: make(map[string]model.ManagedUpstream, len(state.Upstreams)),
	}
	for key, account := range state.Accounts {
		cloned.Accounts[key] = cloneAccount(account)
	}
	for key, upstream := range state.Upstreams {
		cloned.Upstreams[key] = cloneUpstream(upstream)
	}
	return cloned
}

func cloneAccount(account model.ManagedAccount) model.ManagedAccount {
	account.History = append([]model.CheckResult(nil), account.History...)
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
