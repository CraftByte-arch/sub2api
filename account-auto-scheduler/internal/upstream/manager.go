package upstream

import (
	"context"
	"crypto/rand"
	"crypto/subtle"
	"encoding/hex"
	"errors"
	"log/slog"
	"math"
	"net/http"
	"net/url"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/Wei-Shaw/sub2api-account-auto-scheduler/internal/core"
	"github.com/Wei-Shaw/sub2api-account-auto-scheduler/internal/model"
	"github.com/Wei-Shaw/sub2api-account-auto-scheduler/internal/store"
)

var ErrSyncInProgress = errors.New("upstream synchronization already running")

type StateStore interface {
	ListUpstreams() []model.ManagedUpstream
	GetUpstream(upstreamID string) (model.ManagedUpstream, error)
	PutUpstream(upstream model.ManagedUpstream) error
	UpdateUpstream(upstreamID string, change func(*model.ManagedUpstream) error) error
	DeleteUpstream(upstreamID string) error
}

type LocalCore interface {
	ListAccounts(ctx context.Context) ([]model.UpstreamAccount, error)
	ExportAPIKeySecrets(ctx context.Context, accountIDs []int64, adminJWT string, identity core.ForwardedIdentity) (map[int64]string, error)
}

type Manager struct {
	store        StateStore
	core         LocalCore
	box          *CredentialBox
	logger       *slog.Logger
	syncInterval time.Duration

	busyMu sync.Mutex
	busy   map[string]struct{}
}

type LocalAccountView struct {
	ID          int64  `json:"id"`
	Name        string `json:"name"`
	Platform    string `json:"platform"`
	Status      string `json:"status"`
	Schedulable bool   `json:"schedulable"`
}

type RemoteKeyView struct {
	ID                   string     `json:"id"`
	Name                 string     `json:"name"`
	MaskedKey            string     `json:"masked_key,omitempty"`
	MatchAvailable       bool       `json:"match_available"`
	Status               string     `json:"status"`
	Group                string     `json:"group,omitempty"`
	UnlimitedQuota       bool       `json:"unlimited_quota,omitempty"`
	QuotaLimit           *float64   `json:"quota_limit,omitempty"`
	QuotaUsed            *float64   `json:"quota_used,omitempty"`
	Multiplier           *float64   `json:"multiplier,omitempty"`
	FinalMultiplier      *float64   `json:"final_multiplier,omitempty"`
	MultiplierSource     string     `json:"multiplier_source,omitempty"`
	MultiplierObservedAt *time.Time `json:"multiplier_observed_at,omitempty"`
	LastUsedAt           *time.Time `json:"last_used_at,omitempty"`
	ExpiresAt            *time.Time `json:"expires_at,omitempty"`
	LocalAccountID       *int64     `json:"local_account_id,omitempty"`
	Stale                bool       `json:"stale,omitempty"`
	SyncedAt             time.Time  `json:"synced_at"`
}

type BalanceView struct {
	Amount       float64   `json:"amount"`
	Unit         string    `json:"unit"`
	Source       string    `json:"source"`
	ObservedAt   time.Time `json:"observed_at"`
	Stale        bool      `json:"stale,omitempty"`
	RawQuota     *float64  `json:"raw_quota,omitempty"`
	QuotaPerUnit *float64  `json:"quota_per_unit,omitempty"`
}

type UpstreamBalanceSummary struct {
	Status         string       `json:"status"`
	IdentityCount  int          `json:"identity_count"`
	AvailableCount int          `json:"available_count"`
	Balance        *BalanceView `json:"balance,omitempty"`
}

// LocalAccountFinalMultiplier is the final multiplier projected from a bound
// upstream key into the groups-and-accounts overview. It never contains key
// material and is calculated from the same persisted snapshot as Upstreams.
type LocalAccountFinalMultiplier struct {
	Status                string   `json:"status"`
	FinalMultiplier       *float64 `json:"final_multiplier,omitempty"`
	GroupMultiplier       *float64 `json:"group_multiplier,omitempty"`
	RechargeRateCNYPerUSD *float64 `json:"recharge_rate_cny_per_usd,omitempty"`
}

type IdentityView struct {
	ID            string                       `json:"id"`
	Label         string                       `json:"label"`
	AuthMode      model.UpstreamAuthMode       `json:"auth_mode"`
	Principal     string                       `json:"principal,omitempty"`
	Status        model.UpstreamIdentityStatus `json:"status"`
	StatusCode    string                       `json:"status_code,omitempty"`
	StatusMessage string                       `json:"status_message,omitempty"`
	HasCredential bool                         `json:"has_credential"`
	LastAttemptAt *time.Time                   `json:"last_attempt_at,omitempty"`
	LastSuccessAt *time.Time                   `json:"last_success_at,omitempty"`
	Balance       *BalanceView                 `json:"balance,omitempty"`
	Keys          []RemoteKeyView              `json:"keys"`
	CreatedAt     time.Time                    `json:"created_at"`
	UpdatedAt     time.Time                    `json:"updated_at"`
}

type UpstreamView struct {
	ID             string                      `json:"id"`
	Name           string                      `json:"name"`
	BaseURL        string                      `json:"base_url"`
	ManagementURL  string                      `json:"site_url,omitempty"`
	Type           model.UpstreamSiteType      `json:"type"`
	TypeOverride   model.UpstreamSiteType      `json:"type_override,omitempty"`
	Detection      model.UpstreamDetection     `json:"detection"`
	RechargeRate   *model.UpstreamRechargeRate `json:"recharge_rate,omitempty"`
	BalanceSummary UpstreamBalanceSummary      `json:"balance_summary"`
	LocalAccounts  []LocalAccountView          `json:"local_accounts"`
	Identities     []IdentityView              `json:"identities"`
	Persisted      bool                        `json:"persisted"`
	CreatedAt      time.Time                   `json:"created_at,omitempty"`
	UpdatedAt      time.Time                   `json:"updated_at,omitempty"`
}

type ListResponse struct {
	CredentialsEnabled bool           `json:"credentials_enabled"`
	Upstreams          []UpstreamView `json:"upstreams"`
}

type CreateInput struct {
	Name         string
	BaseURL      string
	TypeOverride model.UpstreamSiteType
}

type ConnectInput struct {
	IdentityID       string
	Label            string
	ManagementURL    string
	ManagementURLSet bool
	Login            LoginInput
}

type RechargeRateInput struct {
	Mode  model.RechargeRateInputMode
	Value float64
}

type BindingInput struct {
	IdentityID     string `json:"identity_id"`
	RemoteKeyID    string `json:"remote_key_id"`
	LocalAccountID int64  `json:"local_account_id"`
}

type MatchItem struct {
	IdentityID     string `json:"identity_id"`
	RemoteKeyID    string `json:"remote_key_id"`
	RemoteKeyName  string `json:"remote_key_name"`
	LocalAccountID int64  `json:"local_account_id,omitempty"`
	Status         string `json:"status"`
}

type MatchResult struct {
	Matched     []MatchItem `json:"matched"`
	Unavailable []MatchItem `json:"unavailable"`
	Ambiguous   []MatchItem `json:"ambiguous"`
}

type SyncOutcome struct {
	UpstreamID string `json:"upstream_id"`
	IdentityID string `json:"identity_id"`
	Success    bool   `json:"success"`
	Code       string `json:"code,omitempty"`
	Message    string `json:"message,omitempty"`
}

func NewManager(stateStore StateStore, localCore LocalCore, box *CredentialBox, syncInterval time.Duration, logger *slog.Logger) *Manager {
	if logger == nil {
		logger = slog.Default()
	}
	if box == nil {
		box = &CredentialBox{}
	}
	return &Manager{
		store:        stateStore,
		core:         localCore,
		box:          box,
		logger:       logger,
		syncInterval: syncInterval,
		busy:         map[string]struct{}{},
	}
}

func (m *Manager) CredentialsEnabled() bool { return m != nil && m.box.Enabled() }

// LocalAccountFinalMultipliers projects persisted, explicitly bound upstream
// keys onto the current local API-key accounts. It performs no upstream HTTP
// calls, so the overview poll cannot trigger authentication or synchronization.
func (m *Manager) LocalAccountFinalMultipliers(accounts []model.UpstreamAccount) map[int64]LocalAccountFinalMultiplier {
	result := make(map[int64]LocalAccountFinalMultiplier)
	if m == nil {
		return result
	}

	type boundKey struct {
		upstream model.ManagedUpstream
		key      model.RemoteKey
	}

	boundKeysByAccount := make(map[int64][]boundKey)
	for _, item := range m.store.ListUpstreams() {
		baseURL, err := model.NormalizeUpstreamBaseURL(item.BaseURL)
		if err != nil || baseURL == "" {
			continue
		}
		for _, identity := range item.Identities {
			for _, remoteKey := range identity.Keys {
				if remoteKey.LocalAccountID == nil {
					continue
				}
				boundKeysByAccount[*remoteKey.LocalAccountID] = append(boundKeysByAccount[*remoteKey.LocalAccountID], boundKey{
					upstream: item,
					key:      remoteKey,
				})
			}
		}
	}

	for _, account := range accounts {
		if !account.IsAPIKey() {
			continue
		}
		projection := LocalAccountFinalMultiplier{Status: "unbound"}
		baseURL, err := model.NormalizeUpstreamBaseURL(account.BaseURL())
		if err != nil || baseURL == "" {
			projection.Status = "invalid_upstream"
			result[account.ID] = projection
			continue
		}

		activeBindings := make([]boundKey, 0, 1)
		hasStaleBinding := false
		for _, binding := range boundKeysByAccount[account.ID] {
			bindingBaseURL, normalizeErr := model.NormalizeUpstreamBaseURL(binding.upstream.BaseURL)
			if normalizeErr != nil || binding.key.Stale || bindingBaseURL != baseURL {
				hasStaleBinding = true
				continue
			}
			activeBindings = append(activeBindings, binding)
		}

		switch len(activeBindings) {
		case 0:
			if hasStaleBinding {
				projection.Status = "stale"
			}
		case 1:
			binding := activeBindings[0]
			projection.RechargeRateCNYPerUSD = rechargeRateValue(binding.upstream.RechargeRate)
			projection.GroupMultiplier = finiteMultiplierValue(binding.key.Multiplier)
			switch {
			case projection.RechargeRateCNYPerUSD == nil:
				projection.Status = "recharge_unset"
			case projection.GroupMultiplier == nil:
				projection.Status = "group_multiplier_unknown"
			default:
				projection.FinalMultiplier = deriveFinalMultiplier(binding.upstream.RechargeRate, binding.key.Multiplier)
				if projection.FinalMultiplier == nil {
					projection.Status = "group_multiplier_unknown"
				} else {
					projection.Status = "available"
				}
			}
		default:
			projection.Status = "ambiguous"
		}
		result[account.ID] = projection
	}
	return result
}

func (m *Manager) Start(ctx context.Context) {
	if m == nil || m.syncInterval <= 0 {
		return
	}
	go func() {
		ticker := time.NewTicker(m.syncInterval)
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				m.syncPersisted(ctx)
			}
		}
	}()
}

func (m *Manager) List(ctx context.Context) (ListResponse, error) {
	records, accountViews, err := m.combinedRecords(ctx)
	if err != nil {
		return ListResponse{}, err
	}
	views := make([]UpstreamView, 0, len(records))
	persistedIDs := make(map[string]struct{})
	for _, item := range m.store.ListUpstreams() {
		persistedIDs[item.ID] = struct{}{}
	}
	for _, record := range records {
		_, persisted := persistedIDs[record.ID]
		views = append(views, publicUpstream(record, accountViews[record.ID], persisted))
	}
	sort.Slice(views, func(i, j int) bool {
		left := strings.ToLower(firstNonEmpty(views[i].Name, views[i].BaseURL))
		right := strings.ToLower(firstNonEmpty(views[j].Name, views[j].BaseURL))
		if left == right {
			return views[i].ID < views[j].ID
		}
		return left < right
	})
	return ListResponse{CredentialsEnabled: m.CredentialsEnabled(), Upstreams: views}, nil
}

func (m *Manager) Create(ctx context.Context, input CreateInput) (UpstreamView, error) {
	baseURL, err := model.NormalizeUpstreamBaseURL(input.BaseURL)
	if err != nil {
		return UpstreamView{}, adapterError("INVALID_UPSTREAM_URL", err.Error(), model.IdentityStatusInvalid, http.StatusBadRequest)
	}
	name := strings.TrimSpace(input.Name)
	if len(name) > model.MaxUpstreamNameLength {
		return UpstreamView{}, adapterError("INVALID_UPSTREAM_NAME", "上游名称不能超过 120 个字符", model.IdentityStatusInvalid, http.StatusBadRequest)
	}
	if name == "" {
		name = defaultUpstreamName(baseURL)
	}
	if input.TypeOverride != "" && input.TypeOverride != model.UpstreamTypeUnknown {
		if _, err := model.ParseUpstreamSiteType(string(input.TypeOverride), false); err != nil {
			return UpstreamView{}, adapterError("INVALID_UPSTREAM_TYPE", err.Error(), model.IdentityStatusInvalid, http.StatusBadRequest)
		}
	}
	id := model.StableUpstreamID(baseURL)
	now := time.Now().UTC()
	record, err := m.store.GetUpstream(id)
	if errors.Is(err, store.ErrUpstreamNotFound) {
		record = model.ManagedUpstream{
			ID:           id,
			Name:         name,
			BaseURL:      baseURL,
			TypeOverride: input.TypeOverride,
			Detection:    model.UpstreamDetection{Type: model.UpstreamTypeUnknown},
			Identities:   map[string]model.UpstreamIdentity{},
			CreatedAt:    now,
			UpdatedAt:    now,
		}
		if err := m.store.PutUpstream(record); err != nil {
			return UpstreamView{}, err
		}
	} else if err != nil {
		return UpstreamView{}, err
	} else {
		err = m.store.UpdateUpstream(id, func(upstream *model.ManagedUpstream) error {
			upstream.Name = name
			if input.TypeOverride != "" {
				upstream.TypeOverride = input.TypeOverride
			}
			upstream.UpdatedAt = now
			return nil
		})
		if err != nil {
			return UpstreamView{}, err
		}
		record, _ = m.store.GetUpstream(id)
	}
	accounts, _ := m.accountsForBaseURL(ctx, baseURL)
	return publicUpstream(record, accounts, true), nil
}

func (m *Manager) Detect(ctx context.Context, upstreamID string) (UpstreamView, error) {
	record, err := m.ensureUpstream(ctx, upstreamID)
	if err != nil {
		return UpstreamView{}, err
	}
	result, err := Detect(ctx, record.EffectiveManagementURL())
	if err != nil {
		return UpstreamView{}, err
	}
	now := time.Now().UTC()
	if err := m.store.UpdateUpstream(record.ID, func(upstream *model.ManagedUpstream) error {
		upstream.Detection = model.UpstreamDetection{Type: result.Type, Evidence: result.Evidence, DetectedAt: &now}
		upstream.UpdatedAt = now
		return nil
	}); err != nil {
		return UpstreamView{}, err
	}
	record, _ = m.store.GetUpstream(record.ID)
	accounts, _ := m.accountsForBaseURL(ctx, record.BaseURL)
	return publicUpstream(record, accounts, true), nil
}

func (m *Manager) SetType(ctx context.Context, upstreamID string, siteType model.UpstreamSiteType) (UpstreamView, error) {
	parsed, err := model.ParseUpstreamSiteType(string(siteType), true)
	if err != nil {
		return UpstreamView{}, adapterError("INVALID_UPSTREAM_TYPE", err.Error(), model.IdentityStatusInvalid, http.StatusBadRequest)
	}
	record, err := m.ensureUpstream(ctx, upstreamID)
	if err != nil {
		return UpstreamView{}, err
	}
	now := time.Now().UTC()
	if err := m.store.UpdateUpstream(record.ID, func(upstream *model.ManagedUpstream) error {
		upstream.TypeOverride = parsed
		upstream.UpdatedAt = now
		return nil
	}); err != nil {
		return UpstreamView{}, err
	}
	record, _ = m.store.GetUpstream(record.ID)
	accounts, _ := m.accountsForBaseURL(ctx, record.BaseURL)
	return publicUpstream(record, accounts, true), nil
}

func (m *Manager) SetRechargeRate(ctx context.Context, upstreamID string, input RechargeRateInput) (UpstreamView, error) {
	now := time.Now().UTC()
	rate, err := model.NewUpstreamRechargeRate(input.Mode, input.Value, now)
	if err != nil {
		return UpstreamView{}, adapterError("INVALID_RECHARGE_RATE", err.Error(), model.IdentityStatusInvalid, http.StatusBadRequest)
	}
	record, err := m.ensureUpstream(ctx, upstreamID)
	if err != nil {
		return UpstreamView{}, err
	}
	if err := m.store.UpdateUpstream(record.ID, func(upstream *model.ManagedUpstream) error {
		upstream.RechargeRate = &rate
		upstream.UpdatedAt = now
		return nil
	}); err != nil {
		return UpstreamView{}, err
	}
	record, _ = m.store.GetUpstream(record.ID)
	accounts, _ := m.accountsForBaseURL(ctx, record.BaseURL)
	return publicUpstream(record, accounts, true), nil
}

func (m *Manager) ClearRechargeRate(ctx context.Context, upstreamID string) (UpstreamView, error) {
	record, err := m.ensureUpstream(ctx, upstreamID)
	if err != nil {
		return UpstreamView{}, err
	}
	now := time.Now().UTC()
	if err := m.store.UpdateUpstream(record.ID, func(upstream *model.ManagedUpstream) error {
		upstream.RechargeRate = nil
		upstream.UpdatedAt = now
		return nil
	}); err != nil {
		return UpstreamView{}, err
	}
	record, _ = m.store.GetUpstream(record.ID)
	accounts, _ := m.accountsForBaseURL(ctx, record.BaseURL)
	return publicUpstream(record, accounts, true), nil
}

func (m *Manager) Connect(ctx context.Context, upstreamID string, input ConnectInput) (IdentityView, error) {
	if !m.CredentialsEnabled() {
		return IdentityView{}, adapterError("CREDENTIALS_DISABLED", "请配置 AUTO_SCHEDULER_CREDENTIAL_KEY 后再连接上游", model.IdentityStatusInvalid, http.StatusServiceUnavailable)
	}
	record, err := m.ensureUpstream(ctx, upstreamID)
	if err != nil {
		return IdentityView{}, err
	}
	adapter, err := AdapterFor(record.EffectiveType())
	if err != nil {
		return IdentityView{}, err
	}
	label := strings.TrimSpace(input.Label)
	if len(label) > model.MaxIdentityLabelLength {
		return IdentityView{}, adapterError("INVALID_IDENTITY_LABEL", "身份名称不能超过 120 个字符", model.IdentityStatusInvalid, http.StatusBadRequest)
	}
	mode, err := model.ParseUpstreamAuthMode(string(input.Login.Mode))
	if err != nil {
		return IdentityView{}, adapterError("INVALID_AUTH_MODE", err.Error(), model.IdentityStatusInvalid, http.StatusBadRequest)
	}
	input.Login.Mode = mode
	now := time.Now().UTC()
	identityID := strings.TrimSpace(input.IdentityID)
	existing, exists := record.Identities[identityID]
	if identityID == "" {
		identityID, err = randomID("identity")
		if err != nil {
			return IdentityView{}, err
		}
	} else if !exists {
		return IdentityView{}, adapterError("IDENTITY_NOT_FOUND", "上游登录身份不存在", model.IdentityStatusInvalid, http.StatusNotFound)
	}
	if label == "" {
		label = firstNonEmpty(existing.Label, input.Login.Username, "登录身份")
	}
	identity := existing
	identity.ID = identityID
	identity.Label = label
	identity.AuthMode = mode
	identity.LastAttemptAt = &now
	identity.UpdatedAt = now
	if identity.CreatedAt.IsZero() {
		identity.CreatedAt = now
	}
	if identity.Keys == nil {
		identity.Keys = map[string]model.RemoteKey{}
	}

	managementURL, persistedManagementURL, updateManagementURL, managementURLErr := resolveManagementURL(record, input)
	if managementURLErr != nil {
		return publicIdentity(identity, record.RechargeRate), managementURLErr
	}
	login, loginErr := adapter.Connect(ctx, managementURL, input.Login)
	if loginErr != nil {
		failure := AsAdapterError(loginErr)
		identity.Status = failure.Status
		identity.StatusCode = failure.Code
		identity.StatusMessage = failure.Message
		identity.Balance = staleBalance(identity.Balance)
		if err := m.saveIdentity(record.ID, identity); err != nil {
			return IdentityView{}, err
		}
		return publicIdentity(identity, record.RechargeRate), failure
	}
	envelope, err := m.box.Encrypt(record.ID, identity.ID, login.Material)
	if err != nil {
		return IdentityView{}, err
	}
	identity.Credential = envelope
	identity.Principal = strings.TrimSpace(login.Principal)
	identity.Status = model.IdentityStatusConnected
	identity.StatusCode = ""
	identity.StatusMessage = ""
	identity.LastSuccessAt = &now
	identity.Balance = freshBalance(login.Balance, now)
	if updateManagementURL {
		if err := m.saveIdentityWithManagementURL(record.ID, identity, persistedManagementURL); err != nil {
			return IdentityView{}, err
		}
	} else if err := m.saveIdentity(record.ID, identity); err != nil {
		return IdentityView{}, err
	}
	if syncErr := m.SyncIdentity(ctx, record.ID, identity.ID); syncErr != nil {
		updated, _ := m.store.GetUpstream(record.ID)
		return publicIdentity(updated.Identities[identity.ID], updated.RechargeRate), syncErr
	}
	updated, _ := m.store.GetUpstream(record.ID)
	return publicIdentity(updated.Identities[identity.ID], updated.RechargeRate), nil
}

func (m *Manager) DeleteIdentity(upstreamID, identityID string) error {
	record, err := m.store.GetUpstream(strings.TrimSpace(upstreamID))
	if err != nil {
		return err
	}
	if _, exists := record.Identities[strings.TrimSpace(identityID)]; !exists {
		return adapterError("IDENTITY_NOT_FOUND", "上游登录身份不存在", model.IdentityStatusInvalid, http.StatusNotFound)
	}
	return m.store.UpdateUpstream(record.ID, func(upstream *model.ManagedUpstream) error {
		delete(upstream.Identities, strings.TrimSpace(identityID))
		upstream.UpdatedAt = time.Now().UTC()
		return nil
	})
}

func (m *Manager) SyncIdentity(ctx context.Context, upstreamID, identityID string) error {
	if !m.CredentialsEnabled() {
		return adapterError("CREDENTIALS_DISABLED", "请配置 AUTO_SCHEDULER_CREDENTIAL_KEY 后再同步上游", model.IdentityStatusInvalid, http.StatusServiceUnavailable)
	}
	busyKey := strings.TrimSpace(upstreamID) + "\x00" + strings.TrimSpace(identityID)
	if !m.beginBusy(busyKey) {
		return ErrSyncInProgress
	}
	defer m.endBusy(busyKey)

	record, err := m.store.GetUpstream(strings.TrimSpace(upstreamID))
	if err != nil {
		return err
	}
	identity, exists := record.Identities[strings.TrimSpace(identityID)]
	if !exists {
		return adapterError("IDENTITY_NOT_FOUND", "上游登录身份不存在", model.IdentityStatusInvalid, http.StatusNotFound)
	}
	adapter, err := AdapterFor(record.EffectiveType())
	if err != nil {
		return m.recordSyncFailure(record.ID, identity.ID, err)
	}
	now := time.Now().UTC()
	_ = m.store.UpdateUpstream(record.ID, func(upstream *model.ManagedUpstream) error {
		item := upstream.Identities[identity.ID]
		item.LastAttemptAt = &now
		item.UpdatedAt = now
		upstream.Identities[identity.ID] = item
		upstream.UpdatedAt = now
		return nil
	})
	material, err := m.box.Decrypt(record.ID, identity.ID, identity.Credential)
	if err != nil {
		return m.recordSyncFailure(record.ID, identity.ID, adapterError("CREDENTIAL_DECRYPT_FAILED", "无法解密上游凭证，请重新连接", model.IdentityStatusInvalid, http.StatusConflict))
	}
	result, err := adapter.Sync(ctx, record.EffectiveManagementURL(), material)
	if err != nil {
		return m.recordSyncFailure(record.ID, identity.ID, err)
	}
	if result.Material.Empty() {
		result.Material = material
	}
	envelope := identity.Credential
	if result.Material != material {
		envelope, err = m.box.Encrypt(record.ID, identity.ID, result.Material)
		if err != nil {
			return m.recordSyncFailure(record.ID, identity.ID, err)
		}
	}
	normalizedKeys := make(map[string]model.RemoteKey, len(result.Keys))
	for index := range result.Keys {
		item := result.Keys[index]
		item.Key.ID = strings.TrimSpace(item.Key.ID)
		if item.Key.ID == "" {
			continue
		}
		item.Key.SyncedAt = now
		if item.Plaintext != "" {
			fingerprint, fingerprintErr := m.box.Fingerprint(item.Plaintext)
			item.Plaintext = ""
			result.Keys[index].Plaintext = ""
			if fingerprintErr != nil {
				return m.recordSyncFailure(record.ID, identity.ID, fingerprintErr)
			}
			item.Key.Fingerprint = fingerprint
			item.Key.MatchAvailable = true
		}
		if previous, ok := identity.Keys[item.Key.ID]; ok && previous.LocalAccountID != nil {
			accountID := *previous.LocalAccountID
			item.Key.LocalAccountID = &accountID
		}
		normalizedKeys[item.Key.ID] = item.Key
	}
	for keyID, previous := range identity.Keys {
		if previous.LocalAccountID == nil {
			continue
		}
		if _, exists := normalizedKeys[keyID]; exists {
			continue
		}
		previous.Stale = true
		normalizedKeys[keyID] = previous
	}
	return m.store.UpdateUpstream(record.ID, func(upstream *model.ManagedUpstream) error {
		item, exists := upstream.Identities[identity.ID]
		if !exists {
			return adapterError("IDENTITY_NOT_FOUND", "上游登录身份不存在", model.IdentityStatusInvalid, http.StatusNotFound)
		}
		item.Credential = envelope
		item.Keys = normalizedKeys
		item.Balance = freshBalance(result.Balance, now)
		item.Principal = firstNonEmpty(result.Principal, item.Principal)
		item.Status = model.IdentityStatusConnected
		item.StatusCode = ""
		item.StatusMessage = ""
		item.LastAttemptAt = &now
		item.LastSuccessAt = &now
		item.UpdatedAt = now
		upstream.Identities[identity.ID] = item
		upstream.UpdatedAt = now
		return nil
	})
}

func (m *Manager) SyncUpstream(ctx context.Context, upstreamID string) []SyncOutcome {
	record, err := m.store.GetUpstream(strings.TrimSpace(upstreamID))
	if err != nil {
		return []SyncOutcome{{UpstreamID: upstreamID, Success: false, Code: "UPSTREAM_NOT_FOUND", Message: "上游不存在"}}
	}
	identityIDs := make([]string, 0, len(record.Identities))
	for identityID, identity := range record.Identities {
		if identity.Credential.Ciphertext != "" {
			identityIDs = append(identityIDs, identityID)
		}
	}
	sort.Strings(identityIDs)
	outcomes := make([]SyncOutcome, 0, len(identityIDs))
	for _, identityID := range identityIDs {
		err := m.SyncIdentity(ctx, record.ID, identityID)
		outcome := SyncOutcome{UpstreamID: record.ID, IdentityID: identityID, Success: err == nil}
		if err != nil {
			failure := AsAdapterError(err)
			if errors.Is(err, ErrSyncInProgress) {
				failure = adapterError("SYNC_IN_PROGRESS", "该身份正在同步", model.IdentityStatusSyncError, http.StatusConflict)
			}
			outcome.Code = failure.Code
			outcome.Message = failure.Message
		}
		outcomes = append(outcomes, outcome)
	}
	return outcomes
}

func (m *Manager) SyncAll(ctx context.Context) []SyncOutcome {
	upstreams := m.store.ListUpstreams()
	outcomes := make([]SyncOutcome, 0)
	for _, item := range upstreams {
		outcomes = append(outcomes, m.SyncUpstream(ctx, item.ID)...)
	}
	return outcomes
}

func (m *Manager) SaveBindings(ctx context.Context, upstreamID string, bindings []BindingInput) (UpstreamView, error) {
	record, err := m.store.GetUpstream(strings.TrimSpace(upstreamID))
	if err != nil {
		return UpstreamView{}, err
	}
	accounts, err := m.core.ListAccounts(ctx)
	if err != nil {
		return UpstreamView{}, err
	}
	eligible := make(map[int64]LocalAccountView)
	for _, account := range accounts {
		if !account.IsAPIKey() {
			continue
		}
		baseURL, normalizeErr := model.NormalizeUpstreamBaseURL(account.BaseURL())
		if normalizeErr == nil && baseURL == record.BaseURL {
			eligible[account.ID] = localAccountView(account)
		}
	}
	type bindingTarget struct {
		identityID  string
		remoteKeyID string
		accountID   int64
	}
	targets := make([]bindingTarget, 0, len(bindings))
	seenRemote := make(map[string]struct{}, len(bindings))
	seenLocal := make(map[int64]struct{}, len(bindings))
	for _, binding := range bindings {
		identityID := strings.TrimSpace(binding.IdentityID)
		remoteKeyID := strings.TrimSpace(binding.RemoteKeyID)
		identity, exists := record.Identities[identityID]
		if !exists {
			return UpstreamView{}, adapterError("IDENTITY_NOT_FOUND", "绑定包含不存在的登录身份", model.IdentityStatusInvalid, http.StatusBadRequest)
		}
		if _, exists := identity.Keys[remoteKeyID]; !exists {
			return UpstreamView{}, adapterError("REMOTE_KEY_NOT_FOUND", "绑定包含不存在的上游 Key", model.IdentityStatusInvalid, http.StatusBadRequest)
		}
		if _, exists := eligible[binding.LocalAccountID]; !exists {
			return UpstreamView{}, adapterError("LOCAL_ACCOUNT_INVALID", "只能绑定当前上游关联的本地 API Key 账号", model.IdentityStatusInvalid, http.StatusBadRequest)
		}
		remoteComposite := identityID + "\x00" + remoteKeyID
		if _, exists := seenRemote[remoteComposite]; exists {
			return UpstreamView{}, adapterError("DUPLICATE_REMOTE_KEY", "同一个上游 Key 不能重复绑定", model.IdentityStatusInvalid, http.StatusBadRequest)
		}
		if _, exists := seenLocal[binding.LocalAccountID]; exists {
			return UpstreamView{}, adapterError("DUPLICATE_LOCAL_ACCOUNT", "同一个本地账号不能绑定多个上游 Key", model.IdentityStatusInvalid, http.StatusBadRequest)
		}
		seenRemote[remoteComposite] = struct{}{}
		seenLocal[binding.LocalAccountID] = struct{}{}
		targets = append(targets, bindingTarget{identityID: identityID, remoteKeyID: remoteKeyID, accountID: binding.LocalAccountID})
	}
	now := time.Now().UTC()
	if err := m.store.UpdateUpstream(record.ID, func(upstream *model.ManagedUpstream) error {
		for identityID, identity := range upstream.Identities {
			for keyID, remoteKey := range identity.Keys {
				remoteKey.LocalAccountID = nil
				identity.Keys[keyID] = remoteKey
			}
			upstream.Identities[identityID] = identity
		}
		for _, target := range targets {
			identity := upstream.Identities[target.identityID]
			remoteKey := identity.Keys[target.remoteKeyID]
			accountID := target.accountID
			remoteKey.LocalAccountID = &accountID
			identity.Keys[target.remoteKeyID] = remoteKey
			upstream.Identities[target.identityID] = identity
		}
		upstream.UpdatedAt = now
		return nil
	}); err != nil {
		return UpstreamView{}, err
	}
	updated, _ := m.store.GetUpstream(record.ID)
	return publicUpstream(updated, sortedLocalViews(eligible), true), nil
}

func (m *Manager) AutoMatch(ctx context.Context, upstreamID, adminJWT string, forwarded core.ForwardedIdentity) (MatchResult, error) {
	if !m.CredentialsEnabled() {
		return MatchResult{}, adapterError("CREDENTIALS_DISABLED", "请配置 AUTO_SCHEDULER_CREDENTIAL_KEY 后再自动匹配", model.IdentityStatusInvalid, http.StatusServiceUnavailable)
	}
	record, err := m.store.GetUpstream(strings.TrimSpace(upstreamID))
	if err != nil {
		return MatchResult{}, err
	}
	accounts, err := m.core.ListAccounts(ctx)
	if err != nil {
		return MatchResult{}, err
	}
	accountIDs := make([]int64, 0)
	for _, account := range accounts {
		if !account.IsAPIKey() {
			continue
		}
		baseURL, normalizeErr := model.NormalizeUpstreamBaseURL(account.BaseURL())
		if normalizeErr == nil && baseURL == record.BaseURL {
			accountIDs = append(accountIDs, account.ID)
		}
	}
	sort.Slice(accountIDs, func(i, j int) bool { return accountIDs[i] < accountIDs[j] })
	secrets, err := m.core.ExportAPIKeySecrets(ctx, accountIDs, adminJWT, forwarded)
	if err != nil {
		return MatchResult{}, err
	}
	defer func() {
		for accountID := range secrets {
			secrets[accountID] = ""
			delete(secrets, accountID)
		}
	}()
	localByFingerprint := make(map[string][]int64)
	localFingerprints := make(map[int64]string, len(secrets))
	for accountID, secret := range secrets {
		fingerprint, fingerprintErr := m.box.Fingerprint(secret)
		secrets[accountID] = ""
		if fingerprintErr != nil {
			return MatchResult{}, fingerprintErr
		}
		localByFingerprint[fingerprint] = append(localByFingerprint[fingerprint], accountID)
		localFingerprints[accountID] = fingerprint
	}
	type remoteRef struct {
		identityID string
		keyID      string
		key        model.RemoteKey
	}
	remoteCounts := make(map[string]int)
	refs := make([]remoteRef, 0)
	for identityID, identity := range record.Identities {
		for keyID, remoteKey := range identity.Keys {
			if remoteKey.Stale || remoteKey.LocalAccountID != nil {
				continue
			}
			refs = append(refs, remoteRef{identityID: identityID, keyID: keyID, key: remoteKey})
			if remoteKey.MatchAvailable && remoteKey.Fingerprint != "" {
				remoteCounts[remoteKey.Fingerprint]++
			}
		}
	}
	sort.Slice(refs, func(i, j int) bool {
		if refs[i].identityID == refs[j].identityID {
			return refs[i].keyID < refs[j].keyID
		}
		return refs[i].identityID < refs[j].identityID
	})
	result := MatchResult{Matched: []MatchItem{}, Unavailable: []MatchItem{}, Ambiguous: []MatchItem{}}
	assignments := make(map[string]int64)
	usedLocal := make(map[int64]struct{})
	for _, identity := range record.Identities {
		for _, remoteKey := range identity.Keys {
			if remoteKey.LocalAccountID != nil {
				usedLocal[*remoteKey.LocalAccountID] = struct{}{}
			}
		}
	}
	for _, ref := range refs {
		item := MatchItem{IdentityID: ref.identityID, RemoteKeyID: ref.keyID, RemoteKeyName: ref.key.Name}
		if !ref.key.MatchAvailable || ref.key.Fingerprint == "" {
			item.Status = "unavailable"
			result.Unavailable = append(result.Unavailable, item)
			continue
		}
		localMatches := localByFingerprint[ref.key.Fingerprint]
		if len(localMatches) != 1 || remoteCounts[ref.key.Fingerprint] != 1 {
			item.Status = "ambiguous"
			result.Ambiguous = append(result.Ambiguous, item)
			continue
		}
		accountID := localMatches[0]
		if subtle.ConstantTimeCompare([]byte(localFingerprints[accountID]), []byte(ref.key.Fingerprint)) != 1 {
			item.Status = "ambiguous"
			result.Ambiguous = append(result.Ambiguous, item)
			continue
		}
		if _, exists := usedLocal[accountID]; exists {
			item.Status = "ambiguous"
			result.Ambiguous = append(result.Ambiguous, item)
			continue
		}
		usedLocal[accountID] = struct{}{}
		assignments[ref.identityID+"\x00"+ref.keyID] = accountID
		item.Status = "matched"
		item.LocalAccountID = accountID
		result.Matched = append(result.Matched, item)
	}
	if len(assignments) > 0 {
		now := time.Now().UTC()
		if err := m.store.UpdateUpstream(record.ID, func(upstream *model.ManagedUpstream) error {
			for composite, accountID := range assignments {
				parts := strings.SplitN(composite, "\x00", 2)
				identity := upstream.Identities[parts[0]]
				remoteKey := identity.Keys[parts[1]]
				value := accountID
				remoteKey.LocalAccountID = &value
				identity.Keys[parts[1]] = remoteKey
				upstream.Identities[parts[0]] = identity
			}
			upstream.UpdatedAt = now
			return nil
		}); err != nil {
			return MatchResult{}, err
		}
	}
	return result, nil
}

func (m *Manager) ensureUpstream(ctx context.Context, upstreamID string) (model.ManagedUpstream, error) {
	upstreamID = strings.TrimSpace(upstreamID)
	if record, err := m.store.GetUpstream(upstreamID); err == nil {
		return record, nil
	} else if !errors.Is(err, store.ErrUpstreamNotFound) {
		return model.ManagedUpstream{}, err
	}
	records, _, err := m.combinedRecords(ctx)
	if err != nil {
		return model.ManagedUpstream{}, err
	}
	for _, record := range records {
		if record.ID != upstreamID {
			continue
		}
		if err := m.store.PutUpstream(record); err != nil {
			return model.ManagedUpstream{}, err
		}
		return record, nil
	}
	return model.ManagedUpstream{}, adapterError("UPSTREAM_NOT_FOUND", "上游不存在", model.IdentityStatusInvalid, http.StatusNotFound)
}

func (m *Manager) combinedRecords(ctx context.Context) ([]model.ManagedUpstream, map[string][]LocalAccountView, error) {
	accounts, err := m.core.ListAccounts(ctx)
	if err != nil {
		return nil, nil, err
	}
	now := time.Now().UTC()
	recordByID := make(map[string]model.ManagedUpstream)
	accountViews := make(map[string][]LocalAccountView)
	for _, account := range accounts {
		if !account.IsAPIKey() {
			continue
		}
		baseURL, normalizeErr := model.NormalizeUpstreamBaseURL(account.BaseURL())
		if normalizeErr != nil || baseURL == "" {
			continue
		}
		upstreamID := model.StableUpstreamID(baseURL)
		if _, exists := recordByID[upstreamID]; !exists {
			recordByID[upstreamID] = model.ManagedUpstream{
				ID:         upstreamID,
				Name:       defaultUpstreamName(baseURL),
				BaseURL:    baseURL,
				Detection:  model.UpstreamDetection{Type: model.UpstreamTypeUnknown},
				Identities: map[string]model.UpstreamIdentity{},
				CreatedAt:  now,
				UpdatedAt:  now,
			}
		}
		accountViews[upstreamID] = append(accountViews[upstreamID], localAccountView(account))
	}
	for _, record := range m.store.ListUpstreams() {
		recordByID[record.ID] = record
	}
	records := make([]model.ManagedUpstream, 0, len(recordByID))
	for id, record := range recordByID {
		accountViews[id] = sortedLocalViewsSlice(accountViews[id])
		records = append(records, record)
	}
	return records, accountViews, nil
}

func (m *Manager) accountsForBaseURL(ctx context.Context, baseURL string) ([]LocalAccountView, error) {
	accounts, err := m.core.ListAccounts(ctx)
	if err != nil {
		return nil, err
	}
	views := make([]LocalAccountView, 0)
	for _, account := range accounts {
		if !account.IsAPIKey() {
			continue
		}
		normalized, normalizeErr := model.NormalizeUpstreamBaseURL(account.BaseURL())
		if normalizeErr == nil && normalized == baseURL {
			views = append(views, localAccountView(account))
		}
	}
	return sortedLocalViewsSlice(views), nil
}

func (m *Manager) saveIdentity(upstreamID string, identity model.UpstreamIdentity) error {
	now := time.Now().UTC()
	return m.store.UpdateUpstream(upstreamID, func(upstream *model.ManagedUpstream) error {
		if upstream.Identities == nil {
			upstream.Identities = map[string]model.UpstreamIdentity{}
		}
		upstream.Identities[identity.ID] = identity
		upstream.UpdatedAt = now
		return nil
	})
}

func (m *Manager) saveIdentityWithManagementURL(upstreamID string, identity model.UpstreamIdentity, managementURL string) error {
	now := time.Now().UTC()
	return m.store.UpdateUpstream(upstreamID, func(upstream *model.ManagedUpstream) error {
		if upstream.Identities == nil {
			upstream.Identities = map[string]model.UpstreamIdentity{}
		}
		upstream.Identities[identity.ID] = identity
		upstream.ManagementURL = managementURL
		upstream.UpdatedAt = now
		return nil
	})
}

func resolveManagementURL(record model.ManagedUpstream, input ConnectInput) (managementURL, persistedManagementURL string, updateManagementURL bool, err error) {
	if !input.ManagementURLSet {
		return record.EffectiveManagementURL(), record.ManagementURL, false, nil
	}

	requested := strings.TrimSpace(input.ManagementURL)
	if requested == "" {
		return record.BaseURL, "", true, nil
	}
	normalized, normalizeErr := model.NormalizeUpstreamBaseURL(requested)
	if normalizeErr != nil {
		return "", "", false, adapterError("INVALID_MANAGEMENT_SITE_URL", normalizeErr.Error(), model.IdentityStatusInvalid, http.StatusBadRequest)
	}
	if normalized == record.BaseURL {
		return record.BaseURL, "", true, nil
	}
	return normalized, normalized, true, nil
}

func (m *Manager) recordSyncFailure(upstreamID, identityID string, err error) error {
	failure := AsAdapterError(err)
	now := time.Now().UTC()
	_ = m.store.UpdateUpstream(upstreamID, func(upstream *model.ManagedUpstream) error {
		identity, exists := upstream.Identities[identityID]
		if !exists {
			return nil
		}
		identity.Status = failure.Status
		identity.StatusCode = failure.Code
		identity.StatusMessage = failure.Message
		identity.LastAttemptAt = &now
		identity.Balance = staleBalance(identity.Balance)
		identity.UpdatedAt = now
		upstream.Identities[identityID] = identity
		upstream.UpdatedAt = now
		return nil
	})
	return failure
}

func (m *Manager) syncPersisted(ctx context.Context) {
	type target struct{ upstreamID, identityID string }
	targets := make([]target, 0)
	for _, upstream := range m.store.ListUpstreams() {
		for identityID, identity := range upstream.Identities {
			if identity.Credential.Ciphertext != "" {
				targets = append(targets, target{upstreamID: upstream.ID, identityID: identityID})
			}
		}
	}
	semaphore := make(chan struct{}, 3)
	var wait sync.WaitGroup
	for _, item := range targets {
		item := item
		wait.Add(1)
		go func() {
			defer wait.Done()
			select {
			case semaphore <- struct{}{}:
				defer func() { <-semaphore }()
			case <-ctx.Done():
				return
			}
			if err := m.SyncIdentity(ctx, item.upstreamID, item.identityID); err != nil && !errors.Is(err, context.Canceled) && !errors.Is(err, ErrSyncInProgress) {
				failure := AsAdapterError(err)
				m.logger.Warn("periodic upstream sync failed", "upstream_id", item.upstreamID, "identity_id", item.identityID, "code", failure.Code)
			}
		}()
	}
	wait.Wait()
}

func (m *Manager) beginBusy(key string) bool {
	m.busyMu.Lock()
	defer m.busyMu.Unlock()
	if _, exists := m.busy[key]; exists {
		return false
	}
	m.busy[key] = struct{}{}
	return true
}

func (m *Manager) endBusy(key string) {
	m.busyMu.Lock()
	delete(m.busy, key)
	m.busyMu.Unlock()
}

func publicUpstream(upstream model.ManagedUpstream, accounts []LocalAccountView, persisted bool) UpstreamView {
	identities := make([]IdentityView, 0, len(upstream.Identities))
	for _, identity := range upstream.Identities {
		identities = append(identities, publicIdentity(identity, upstream.RechargeRate))
	}
	sort.Slice(identities, func(i, j int) bool {
		if identities[i].CreatedAt.Equal(identities[j].CreatedAt) {
			return identities[i].ID < identities[j].ID
		}
		return identities[i].CreatedAt.Before(identities[j].CreatedAt)
	})
	if accounts == nil {
		accounts = []LocalAccountView{}
	}
	var rechargeRate *model.UpstreamRechargeRate
	if upstream.RechargeRate != nil {
		value := *upstream.RechargeRate
		rechargeRate = &value
	}
	return UpstreamView{
		ID:             upstream.ID,
		Name:           upstream.Name,
		BaseURL:        upstream.BaseURL,
		ManagementURL:  upstream.ManagementURL,
		Type:           upstream.EffectiveType(),
		TypeOverride:   upstream.TypeOverride,
		Detection:      upstream.Detection,
		RechargeRate:   rechargeRate,
		BalanceSummary: summarizeBalances(identities),
		LocalAccounts:  accounts,
		Identities:     identities,
		Persisted:      persisted,
		CreatedAt:      upstream.CreatedAt,
		UpdatedAt:      upstream.UpdatedAt,
	}
}

func publicIdentity(identity model.UpstreamIdentity, rechargeRate *model.UpstreamRechargeRate) IdentityView {
	keys := make([]RemoteKeyView, 0, len(identity.Keys))
	for _, remoteKey := range identity.Keys {
		finalMultiplier := deriveFinalMultiplier(rechargeRate, remoteKey.Multiplier)
		keys = append(keys, RemoteKeyView{
			ID:                   remoteKey.ID,
			Name:                 remoteKey.Name,
			MaskedKey:            remoteKey.MaskedKey,
			MatchAvailable:       remoteKey.MatchAvailable,
			Status:               remoteKey.Status,
			Group:                remoteKey.Group,
			UnlimitedQuota:       remoteKey.UnlimitedQuota,
			QuotaLimit:           remoteKey.QuotaLimit,
			QuotaUsed:            remoteKey.QuotaUsed,
			Multiplier:           remoteKey.Multiplier,
			FinalMultiplier:      finalMultiplier,
			MultiplierSource:     remoteKey.MultiplierSource,
			MultiplierObservedAt: remoteKey.MultiplierObserved,
			LastUsedAt:           remoteKey.LastUsedAt,
			ExpiresAt:            remoteKey.ExpiresAt,
			LocalAccountID:       remoteKey.LocalAccountID,
			Stale:                remoteKey.Stale,
			SyncedAt:             remoteKey.SyncedAt,
		})
	}
	sort.Slice(keys, func(i, j int) bool {
		if keys[i].Stale != keys[j].Stale {
			return !keys[i].Stale
		}
		left := strings.ToLower(firstNonEmpty(keys[i].Name, keys[i].ID))
		right := strings.ToLower(firstNonEmpty(keys[j].Name, keys[j].ID))
		if left == right {
			return keys[i].ID < keys[j].ID
		}
		return left < right
	})
	return IdentityView{
		ID:            identity.ID,
		Label:         identity.Label,
		AuthMode:      identity.AuthMode,
		Principal:     identity.Principal,
		Status:        identity.Status,
		StatusCode:    identity.StatusCode,
		StatusMessage: identity.StatusMessage,
		HasCredential: identity.Credential.Ciphertext != "",
		LastAttemptAt: identity.LastAttemptAt,
		LastSuccessAt: identity.LastSuccessAt,
		Balance:       publicBalance(identity.Balance),
		Keys:          keys,
		CreatedAt:     identity.CreatedAt,
		UpdatedAt:     identity.UpdatedAt,
	}
}

func publicBalance(balance *model.UpstreamBalance) *BalanceView {
	if balance == nil {
		return nil
	}
	cloned := &BalanceView{
		Amount:     balance.Amount,
		Unit:       balance.Unit,
		Source:     balance.Source,
		ObservedAt: balance.ObservedAt,
		Stale:      balance.Stale,
	}
	if balance.RawQuota != nil {
		value := *balance.RawQuota
		cloned.RawQuota = &value
	}
	if balance.QuotaPerUnit != nil {
		value := *balance.QuotaPerUnit
		cloned.QuotaPerUnit = &value
	}
	return cloned
}

func freshBalance(balance *model.UpstreamBalance, observedAt time.Time) *model.UpstreamBalance {
	if balance == nil {
		return nil
	}
	cloned := *balance
	if balance.RawQuota != nil {
		value := *balance.RawQuota
		cloned.RawQuota = &value
	}
	if balance.QuotaPerUnit != nil {
		value := *balance.QuotaPerUnit
		cloned.QuotaPerUnit = &value
	}
	cloned.Stale = false
	if cloned.ObservedAt.IsZero() {
		cloned.ObservedAt = observedAt.UTC()
	}
	return &cloned
}

func staleBalance(balance *model.UpstreamBalance) *model.UpstreamBalance {
	if balance == nil {
		return nil
	}
	cloned := freshBalance(balance, balance.ObservedAt)
	cloned.Stale = true
	return cloned
}

func summarizeBalances(identities []IdentityView) UpstreamBalanceSummary {
	summary := UpstreamBalanceSummary{
		Status:        "unavailable",
		IdentityCount: len(identities),
	}
	for _, identity := range identities {
		if identity.Balance != nil {
			summary.AvailableCount++
		}
	}
	if len(identities) == 1 && identities[0].Balance != nil {
		summary.Status = "single"
		balance := *identities[0].Balance
		if balance.RawQuota != nil {
			value := *balance.RawQuota
			balance.RawQuota = &value
		}
		if balance.QuotaPerUnit != nil {
			value := *balance.QuotaPerUnit
			balance.QuotaPerUnit = &value
		}
		summary.Balance = &balance
	} else if len(identities) > 1 && summary.AvailableCount > 0 {
		summary.Status = "multiple"
	}
	return summary
}

func rechargeRateValue(rate *model.UpstreamRechargeRate) *float64 {
	if rate == nil || rate.CNYPerUSD <= 0 || math.IsNaN(rate.CNYPerUSD) || math.IsInf(rate.CNYPerUSD, 0) {
		return nil
	}
	value := rate.CNYPerUSD
	return &value
}

func finiteMultiplierValue(multiplier *float64) *float64 {
	if multiplier == nil || math.IsNaN(*multiplier) || math.IsInf(*multiplier, 0) {
		return nil
	}
	value := *multiplier
	return &value
}

func deriveFinalMultiplier(rechargeRate *model.UpstreamRechargeRate, groupMultiplier *float64) *float64 {
	recharge := rechargeRateValue(rechargeRate)
	group := finiteMultiplierValue(groupMultiplier)
	if recharge == nil || group == nil {
		return nil
	}
	value := *recharge * *group
	if math.IsNaN(value) || math.IsInf(value, 0) {
		return nil
	}
	return &value
}

func localAccountView(account model.UpstreamAccount) LocalAccountView {
	return LocalAccountView{ID: account.ID, Name: account.Name, Platform: account.Platform, Status: account.Status, Schedulable: account.Schedulable}
}

func sortedLocalViews(items map[int64]LocalAccountView) []LocalAccountView {
	views := make([]LocalAccountView, 0, len(items))
	for _, item := range items {
		views = append(views, item)
	}
	return sortedLocalViewsSlice(views)
}

func sortedLocalViewsSlice(views []LocalAccountView) []LocalAccountView {
	sort.Slice(views, func(i, j int) bool {
		if views[i].Schedulable != views[j].Schedulable {
			return views[i].Schedulable
		}
		left := strings.ToLower(firstNonEmpty(views[i].Name, strconv.FormatInt(views[i].ID, 10)))
		right := strings.ToLower(firstNonEmpty(views[j].Name, strconv.FormatInt(views[j].ID, 10)))
		if left == right {
			return views[i].ID < views[j].ID
		}
		return left < right
	})
	return views
}

func defaultUpstreamName(baseURL string) string {
	parsed, err := url.Parse(baseURL)
	if err == nil && parsed.Host != "" {
		return parsed.Host
	}
	return baseURL
}

func randomID(prefix string) (string, error) {
	raw := make([]byte, 12)
	if _, err := rand.Read(raw); err != nil {
		return "", err
	}
	return prefix + "_" + hex.EncodeToString(raw), nil
}
