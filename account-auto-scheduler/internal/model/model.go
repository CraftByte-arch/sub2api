package model

import (
	"encoding/json"
	"errors"
	"math"
	"strings"
	"time"
)

const (
	StateVersion                           = 6
	LegacyStateVersion                     = 1
	UpstreamStateVersion                   = 2
	ProtectionStateVersion                 = 3
	GroupProtectionDefaultStateVersion     = 4
	RemoteGroupSnapshotStateVersion        = 5
	HistoryLimit                           = 50
	MinIntervalSeconds                     = 15
	MaxIntervalSeconds                     = 86400
	MinLatencyLimitMS                      = 1000
	MaxLatencyLimitMS                      = 300000
	MaxThreshold                           = 20
	MaxBalanceAlertThreshold               = 1_000_000_000_000
	FinalCostMultiplierExtraKey            = "final_cost_multiplier"
	UpstreamBalanceQuotaManagedExtraKey    = "upstream_balance_quota_managed"
	UpstreamBalanceQuotaRemainingExtraKey  = "upstream_balance_quota_remaining"
	UpstreamBalanceQuotaObservedAtExtraKey = "upstream_balance_quota_observed_at"
	UpstreamBalanceQuotaExhaustedExtraKey  = "upstream_balance_quota_exhausted"
	UpstreamBalanceQuotaUnlimitedExtraKey  = "upstream_balance_quota_unlimited"
)

const DefaultPrompt = `Calculate and respond with ONLY the number, nothing else.

Q: 3 + 5 = ?
A: 8

Q: 12 - 7 = ?
A: 5

Q: 17 + 25 = ?
A:`

type CheckStatus string

const (
	CheckOperational CheckStatus = "operational"
	CheckDegraded    CheckStatus = "degraded"
	CheckFailed      CheckStatus = "failed"
	CheckError       CheckStatus = "error"
	CheckSkipped     CheckStatus = "skipped"
)

type CheckFailureKind string

const (
	CheckFailureBalanceInsufficient CheckFailureKind = "balance_insufficient"
)

type ProbeSource string

const (
	ProbeSourceLegacy ProbeSource = "legacy"
	ProbeSourceDirect ProbeSource = "direct"
)

type DirectProbeAuthorizationState string

const (
	DirectProbeAuthorized             DirectProbeAuthorizationState = "authorized"
	DirectProbeAuthorizationMissing   DirectProbeAuthorizationState = "authorization_missing"
	DirectProbeNeedsReauthorization   DirectProbeAuthorizationState = "needs_reauthorization"
	DirectProbeUnsupported            DirectProbeAuthorizationState = "unsupported"
	DirectProbeCredentialsUnavailable DirectProbeAuthorizationState = "credentials_unavailable"
)

type Policy struct {
	Enabled           bool   `json:"enabled"`
	IntervalSeconds   int    `json:"interval_seconds"`
	Model             string `json:"model"`
	Prompt            string `json:"prompt"`
	LatencyLimitMS    int64  `json:"latency_limit_ms"`
	FailureThreshold  int    `json:"failure_threshold"`
	RecoveryThreshold int    `json:"recovery_threshold"`
	ReasoningEffort   string `json:"reasoning_effort,omitempty"`
}

func DefaultPolicy() Policy {
	return Policy{
		Enabled:           true,
		IntervalSeconds:   60,
		Prompt:            DefaultPrompt,
		LatencyLimitMS:    45000,
		FailureThreshold:  3,
		RecoveryThreshold: 3,
	}
}

func (p Policy) Normalize() (Policy, error) {
	p.Model = strings.TrimSpace(p.Model)
	p.ReasoningEffort = strings.ToLower(strings.TrimSpace(p.ReasoningEffort))
	switch p.ReasoningEffort {
	case "", "none", "minimal", "low", "medium", "high", "xhigh":
	default:
		return Policy{}, errors.New("推理强度必须是 none、minimal、low、medium、high 或 xhigh")
	}
	// A direct probe must send an administrator's custom prompt verbatim. Keep
	// meaningful leading/trailing whitespace while still treating whitespace-only
	// input as a request for the shared default probe prompt.
	if strings.TrimSpace(p.Prompt) == "" {
		p.Prompt = DefaultPrompt
	}
	if p.IntervalSeconds < MinIntervalSeconds || p.IntervalSeconds > MaxIntervalSeconds {
		return Policy{}, errors.New("检测间隔必须在 15 秒到 24 小时之间")
	}
	if p.LatencyLimitMS < MinLatencyLimitMS || p.LatencyLimitMS > MaxLatencyLimitMS {
		return Policy{}, errors.New("耗时上限必须在 1 到 300 秒之间")
	}
	if p.FailureThreshold < 1 || p.FailureThreshold > MaxThreshold {
		return Policy{}, errors.New("连续异常次数必须在 1 到 20 之间")
	}
	if p.RecoveryThreshold < 1 || p.RecoveryThreshold > MaxThreshold {
		return Policy{}, errors.New("连续恢复次数必须在 1 到 20 之间")
	}
	if len(p.Model) > 200 {
		return Policy{}, errors.New("模型名称不能超过 200 个字符")
	}
	if len(p.Prompt) > 8000 {
		return Policy{}, errors.New("检测提示词不能超过 8000 个字符")
	}
	return p, nil
}

func NormalizeBalanceAlertThreshold(value *float64) (*float64, error) {
	if value == nil {
		return nil, nil
	}
	if math.IsNaN(*value) || math.IsInf(*value, 0) || *value < 0 || *value > MaxBalanceAlertThreshold {
		return nil, errors.New("余额告警阈值必须是 0 到 1000000000000 之间的有限数值")
	}
	copy := *value
	return &copy, nil
}

type UpstreamAccount struct {
	ID                      int64          `json:"id"`
	Name                    string         `json:"name"`
	Platform                string         `json:"platform"`
	Type                    string         `json:"type"`
	Status                  string         `json:"status"`
	ErrorMessage            string         `json:"error_message"`
	Schedulable             bool           `json:"schedulable"`
	GroupIDs                []int64        `json:"group_ids"`
	LastUsedAt              *time.Time     `json:"last_used_at"`
	ExpiresAt               *int64         `json:"expires_at"`
	AutoPauseOnExpired      bool           `json:"auto_pause_on_expired"`
	RateLimitedAt           *time.Time     `json:"rate_limited_at"`
	RateLimitResetAt        *time.Time     `json:"rate_limit_reset_at"`
	OverloadUntil           *time.Time     `json:"overload_until"`
	TempUnschedulableUntil  *time.Time     `json:"temp_unschedulable_until"`
	TempUnschedulableReason string         `json:"temp_unschedulable_reason"`
	QuotaLimit              *float64       `json:"quota_limit"`
	QuotaUsed               *float64       `json:"quota_used"`
	QuotaDailyLimit         *float64       `json:"quota_daily_limit"`
	QuotaDailyUsed          *float64       `json:"quota_daily_used"`
	QuotaWeeklyLimit        *float64       `json:"quota_weekly_limit"`
	QuotaWeeklyUsed         *float64       `json:"quota_weekly_used"`
	MixedScheduling         bool           `json:"mixed_scheduling,omitempty"`
	DetectedRate            *DetectedRate  `json:"detected_rate,omitempty"`
	ProxyID                 *int64         `json:"proxy_id,omitempty"`
	Proxy                   *PublicProxy   `json:"proxy,omitempty"`
	Credentials             map[string]any `json:"-"`
	Extra                   map[string]any `json:"-"`
}

type AccountBalanceQuotaUpdate struct {
	QuotaLimit float64
	QuotaUsed  *float64
	Managed    bool
	Remaining  *float64
	ObservedAt *time.Time
	Exhausted  bool
	Unlimited  bool
}

type DetectedRate struct {
	Status              string     `json:"status"`
	EffectiveMultiplier *float64   `json:"effective_multiplier,omitempty"`
	ObservedAt          string     `json:"observed_at,omitempty"`
	ReceivedAt          *time.Time `json:"received_at,omitempty"`
	LastAttemptAt       *time.Time `json:"last_attempt_at,omitempty"`
	LastError           string     `json:"last_error,omitempty"`
}

func (a *UpstreamAccount) UnmarshalJSON(data []byte) error {
	type accountAlias UpstreamAccount
	var payload struct {
		accountAlias
		Credentials map[string]any  `json:"credentials"`
		ExtraRaw    json.RawMessage `json:"extra"`
		Extra       struct {
			MixedScheduling      bool `json:"mixed_scheduling"`
			UpstreamBillingProbe *struct {
				Status string `json:"status"`
				Data   struct {
					EffectiveRateMultiplier *float64 `json:"effective_rate_multiplier"`
					ObservedAt              string   `json:"observed_at"`
				} `json:"data"`
				ReceivedAt    *time.Time `json:"received_at"`
				LastAttemptAt *time.Time `json:"last_attempt_at"`
				LastError     string     `json:"last_error"`
			} `json:"upstream_billing_probe"`
		} `json:"-"`
	}
	if err := json.Unmarshal(data, &payload); err != nil {
		return err
	}
	if len(payload.ExtraRaw) > 0 && string(payload.ExtraRaw) != "null" {
		if err := json.Unmarshal(payload.ExtraRaw, &payload.Extra); err != nil {
			return err
		}
	}
	*a = UpstreamAccount(payload.accountAlias)
	a.Credentials = payload.Credentials
	if len(payload.ExtraRaw) > 0 && string(payload.ExtraRaw) != "null" {
		if err := json.Unmarshal(payload.ExtraRaw, &a.Extra); err != nil {
			return err
		}
	}
	a.MixedScheduling = a.MixedScheduling || payload.Extra.MixedScheduling
	probe := payload.Extra.UpstreamBillingProbe
	if probe == nil {
		return nil
	}

	detected := &DetectedRate{
		Status:        strings.TrimSpace(probe.Status),
		ObservedAt:    strings.TrimSpace(probe.Data.ObservedAt),
		ReceivedAt:    probe.ReceivedAt,
		LastAttemptAt: probe.LastAttemptAt,
		LastError:     strings.TrimSpace(probe.LastError),
	}
	if multiplier := probe.Data.EffectiveRateMultiplier; multiplier != nil && !math.IsNaN(*multiplier) && !math.IsInf(*multiplier, 0) && *multiplier >= 0 {
		detected.EffectiveMultiplier = multiplier
	}
	a.DetectedRate = detected
	return nil
}

// FinalCostMultiplier reads the optional scheduling-only signal from Extra.
// It is intentionally not a serialized top-level account field and is kept
// separate from any account billing multiplier.
func (a UpstreamAccount) FinalCostMultiplier() (*float64, bool) {
	if len(a.Extra) == 0 {
		return nil, false
	}
	raw, ok := a.Extra[FinalCostMultiplierExtraKey]
	value, ok := raw.(float64)
	if pointer, pointerOK := raw.(*float64); pointerOK {
		if pointer == nil {
			return nil, false
		}
		value, ok = *pointer, true
	}
	if !ok || math.IsNaN(value) || math.IsInf(value, 0) || value < 0 {
		return nil, false
	}
	return &value, true
}

// HasFinalCostMultiplier reports whether a non-null scheduling field is
// present, including invalid values that should be cleared by reconciliation.
func (a UpstreamAccount) HasFinalCostMultiplier() bool {
	if len(a.Extra) == 0 {
		return false
	}
	raw, ok := a.Extra[FinalCostMultiplierExtraKey]
	return ok && raw != nil
}

func (a UpstreamAccount) HasManagedUpstreamBalanceQuota() bool {
	if len(a.Extra) == 0 {
		return false
	}
	managed, _ := a.Extra[UpstreamBalanceQuotaManagedExtraKey].(bool)
	return managed
}

func (a UpstreamAccount) ManagedUpstreamBalanceQuotaRemaining() (*float64, bool) {
	if len(a.Extra) == 0 {
		return nil, false
	}
	raw, ok := a.Extra[UpstreamBalanceQuotaRemainingExtraKey]
	value, ok := raw.(float64)
	if !ok || value < 0 || math.IsNaN(value) || math.IsInf(value, 0) {
		return nil, false
	}
	return &value, true
}

func (a UpstreamAccount) ManagedUpstreamBalanceQuotaObservedAt() (*time.Time, bool) {
	if len(a.Extra) == 0 {
		return nil, false
	}
	raw, ok := a.Extra[UpstreamBalanceQuotaObservedAtExtraKey].(string)
	if !ok || strings.TrimSpace(raw) == "" {
		return nil, false
	}
	parsed, err := time.Parse(time.RFC3339Nano, strings.TrimSpace(raw))
	if err != nil {
		return nil, false
	}
	return &parsed, true
}

func (a UpstreamAccount) ManagedUpstreamBalanceQuotaExhausted() bool {
	value, _ := a.Extra[UpstreamBalanceQuotaExhaustedExtraKey].(bool)
	return value
}

func (a UpstreamAccount) ManagedUpstreamBalanceQuotaUnlimited() bool {
	value, _ := a.Extra[UpstreamBalanceQuotaUnlimitedExtraKey].(bool)
	return value
}

func (a UpstreamAccount) IsAPIKey() bool {
	return strings.EqualFold(strings.TrimSpace(a.Type), "apikey")
}

func (a UpstreamAccount) IsOAuthLike() bool {
	accountType := strings.ToLower(strings.TrimSpace(a.Type))
	return accountType == "oauth" || accountType == "setup-token"
}

func (a UpstreamAccount) BaseURL() string {
	if a.Credentials == nil {
		return ""
	}
	value, _ := a.Credentials["base_url"].(string)
	return strings.TrimSpace(value)
}

type UpstreamGroup struct {
	ID                      int64  `json:"id"`
	Name                    string `json:"name"`
	Description             string `json:"description"`
	Platform                string `json:"platform"`
	Status                  string `json:"status"`
	SortOrder               int    `json:"sort_order"`
	AccountCount            int64  `json:"account_count"`
	ActiveAccountCount      int64  `json:"active_account_count"`
	RateLimitedAccountCount int64  `json:"rate_limited_account_count"`
	IsExclusive             bool   `json:"is_exclusive"`
}

type GroupAccessUser struct {
	ID            int64   `json:"id"`
	Username      string  `json:"username"`
	Email         string  `json:"email"`
	Status        string  `json:"status"`
	AllowedGroups []int64 `json:"allowed_groups"`
}

type WindowStats struct {
	Requests     int64              `json:"requests"`
	Tokens       int64              `json:"tokens"`
	Cost         float64            `json:"cost"`
	StandardCost float64            `json:"standard_cost,omitempty"`
	UserCost     float64            `json:"user_cost,omitempty"`
	Cache        *AccountCacheStats `json:"cache,omitempty"`
}

// AccountCacheStats is the optional, sidecar-only daily prompt-cache
// projection. A nil projection means the existing Sub2API usage-statistics
// endpoint could not be read; a non-nil projection with PromptTokens == 0 is a
// known zero-percent result. PromptTokens uses the same cache-hit denominator
// as the Sub2API usage-record page: input plus cache-read tokens.
type AccountCacheStats struct {
	InputTokens         int64   `json:"input_tokens"`
	CacheCreationTokens int64   `json:"cache_creation_tokens"`
	CacheReadTokens     int64   `json:"cache_read_tokens"`
	PromptTokens        int64   `json:"prompt_tokens"`
	HitRate             float64 `json:"hit_rate"`
}

type UsageProgress struct {
	Utilization      float64      `json:"utilization"`
	ResetsAt         *string      `json:"resets_at"`
	RemainingSeconds int64        `json:"remaining_seconds"`
	WindowStats      *WindowStats `json:"window_stats"`
	UsedRequests     *int64       `json:"used_requests"`
	LimitRequests    *int64       `json:"limit_requests"`
}

type AccountUsageInfo struct {
	Source         string         `json:"source,omitempty"`
	UpdatedAt      *string        `json:"updated_at"`
	FiveHour       *UsageProgress `json:"five_hour"`
	SevenDay       *UsageProgress `json:"seven_day"`
	SevenDaySonnet *UsageProgress `json:"seven_day_sonnet"`
	ErrorCode      string         `json:"error_code,omitempty"`
	Error          string         `json:"error,omitempty"`
}

type CheckResult struct {
	ID           string           `json:"id"`
	Status       CheckStatus      `json:"status"`
	FailureKind  CheckFailureKind `json:"failure_kind,omitempty"`
	LatencyMS    int64            `json:"latency_ms"`
	Message      string           `json:"message"`
	ResponseText string           `json:"response_text,omitempty"`
	Action       string           `json:"action,omitempty"`
	CheckedAt    time.Time        `json:"checked_at"`
	Usage        *ProbeUsage      `json:"usage,omitempty"`
	Cost         *ProbeCost       `json:"cost,omitempty"`
}

// ProbeUsage is normalized usage reported by a probe. It is deliberately
// optional because legacy Sub2API test events and some upstream relays do not
// expose token accounting in their streaming terminal event.
type ProbeUsage struct {
	Model            string `json:"model,omitempty"`
	InputTokens      int64  `json:"input_tokens,omitempty"`
	OutputTokens     int64  `json:"output_tokens,omitempty"`
	CacheReadTokens  int64  `json:"cache_read_tokens,omitempty"`
	CacheWriteTokens int64  `json:"cache_write_tokens,omitempty"`
}

func (u *ProbeUsage) TotalTokens() int64 {
	if u == nil {
		return 0
	}
	return maxInt64(0, u.InputTokens) + maxInt64(0, u.OutputTokens) + maxInt64(0, u.CacheReadTokens) + maxInt64(0, u.CacheWriteTokens)
}

// ProbeCost is a sidecar-only estimate. Amount is only present when both
// usage and model pricing were available; it never affects Sub2API billing.
type ProbeCost struct {
	Amount   float64 `json:"amount"`
	Currency string  `json:"currency,omitempty"`
	Known    bool    `json:"known"`
}

type DetectionStats struct {
	Requests         int64      `json:"requests"`
	InputTokens      int64      `json:"input_tokens"`
	OutputTokens     int64      `json:"output_tokens"`
	CacheReadTokens  int64      `json:"cache_read_tokens"`
	CacheWriteTokens int64      `json:"cache_write_tokens"`
	KnownCost        float64    `json:"known_cost"`
	KnownCostChecks  int64      `json:"known_cost_checks"`
	LastCost         *ProbeCost `json:"last_cost,omitempty"`
	LastUsageAt      *time.Time `json:"last_usage_at,omitempty"`
}

func (s *DetectionStats) Add(usage *ProbeUsage, cost *ProbeCost, observedAt time.Time) {
	s.Requests++
	if usage != nil {
		s.InputTokens += maxInt64(0, usage.InputTokens)
		s.OutputTokens += maxInt64(0, usage.OutputTokens)
		s.CacheReadTokens += maxInt64(0, usage.CacheReadTokens)
		s.CacheWriteTokens += maxInt64(0, usage.CacheWriteTokens)
		s.LastUsageAt = cloneTime(&observedAt)
	}
	if cost != nil {
		copy := *cost
		s.LastCost = &copy
		if cost.Known && !math.IsNaN(cost.Amount) && !math.IsInf(cost.Amount, 0) && cost.Amount >= 0 {
			s.KnownCost += cost.Amount
			s.KnownCostChecks++
		}
	}
}

type GroupAccountProtectionStatus string

const (
	ProtectionBound       GroupAccountProtectionStatus = "bound"
	ProtectionExceeded    GroupAccountProtectionStatus = "rate_protected"
	ProtectionUnavailable GroupAccountProtectionStatus = "multiplier_unavailable"
	ProtectionUnbound     GroupAccountProtectionStatus = "rebind_pending"
)

type GroupAccountProtectionScope string

const (
	ProtectionScopeAccount GroupAccountProtectionScope = "account"
	ProtectionScopeGroup   GroupAccountProtectionScope = "group"
)

// GroupProtectionDefault is the optional threshold inherited by accounts in a
// group when they do not have an explicit account-level protection record.
type GroupProtectionDefault struct {
	GroupID              int64     `json:"group_id"`
	ProtectionMultiplier float64   `json:"protection_multiplier"`
	CreatedAt            time.Time `json:"created_at"`
	UpdatedAt            time.Time `json:"updated_at"`
}

func (p GroupProtectionDefault) Valid() bool {
	return p.GroupID > 0 && p.ProtectionMultiplier >= 0 && !math.IsNaN(p.ProtectionMultiplier) && !math.IsInf(p.ProtectionMultiplier, 0)
}

// GroupAccountProtection is a persisted logical membership record. It stays
// present when the sidecar removes the physical Sub2API group binding, so the
// account can still be displayed and automatically rebound later.
type GroupAccountProtection struct {
	GroupID              int64                        `json:"group_id"`
	AccountID            int64                        `json:"account_id"`
	ProtectionMultiplier float64                      `json:"protection_multiplier"`
	Scope                GroupAccountProtectionScope  `json:"scope,omitempty"`
	UpstreamID           string                       `json:"upstream_id,omitempty"`
	IdentityID           string                       `json:"identity_id,omitempty"`
	RemoteKeyID          string                       `json:"remote_key_id,omitempty"`
	Status               GroupAccountProtectionStatus `json:"status"`
	PhysicalBound        bool                         `json:"physical_bound"`
	LastError            string                       `json:"last_error,omitempty"`
	CreatedAt            time.Time                    `json:"created_at"`
	UpdatedAt            time.Time                    `json:"updated_at"`
}

func (p GroupAccountProtection) Valid() bool {
	return p.GroupID > 0 && p.AccountID > 0 && p.ProtectionMultiplier >= 0 && !math.IsNaN(p.ProtectionMultiplier) && !math.IsInf(p.ProtectionMultiplier, 0) && (p.Scope == "" || p.Scope == ProtectionScopeAccount || p.Scope == ProtectionScopeGroup)
}

func (p GroupAccountProtection) EffectiveScope() GroupAccountProtectionScope {
	if p.Scope == ProtectionScopeGroup {
		return ProtectionScopeGroup
	}
	return ProtectionScopeAccount
}

type ManagedAccount struct {
	AccountID             int64              `json:"account_id"`
	Name                  string             `json:"name"`
	Platform              string             `json:"platform"`
	AccountStatus         string             `json:"account_status"`
	Schedulable           bool               `json:"schedulable"`
	Policy                Policy             `json:"policy"`
	ConsecutiveFailures   int                `json:"consecutive_failures"`
	ConsecutiveSuccesses  int                `json:"consecutive_successes"`
	ManagedSuspended      bool               `json:"managed_suspended"`
	LastCheckAt           *time.Time         `json:"last_check_at,omitempty"`
	NextCheckAt           *time.Time         `json:"next_check_at,omitempty"`
	LastError             string             `json:"last_error,omitempty"`
	LastFailureKind       CheckFailureKind   `json:"last_failure_kind,omitempty"`
	History               []CheckResult      `json:"history"`
	DetectionStats        DetectionStats     `json:"detection_stats,omitempty"`
	BalanceAlertThreshold *float64           `json:"balance_alert_threshold,omitempty"`
	ProbeSource           ProbeSource        `json:"probe_source,omitempty"`
	DirectProbe           *DirectProbeConfig `json:"direct_probe,omitempty"`
	CreatedAt             time.Time          `json:"created_at"`
	UpdatedAt             time.Time          `json:"updated_at"`
	Running               bool               `json:"running"`
}

type DirectProbeProxy struct {
	Protocol  string `json:"protocol"`
	Host      string `json:"host"`
	Port      int    `json:"port"`
	Username  string `json:"username,omitempty"`
	Password  string `json:"password,omitempty"`
	Status    string `json:"status"`
	ExpiresAt *int64 `json:"expires_at,omitempty"`
}

type DirectProbeSnapshot struct {
	Version             int               `json:"version"`
	AccountID           int64             `json:"account_id"`
	Platform            string            `json:"platform"`
	BaseURL             string            `json:"base_url"`
	APIKey              string            `json:"api_key"`
	ModelMapping        map[string]string `json:"model_mapping,omitempty"`
	HeaderOverrides     map[string]string `json:"header_overrides,omitempty"`
	AnthropicAuthMode   string            `json:"anthropic_auth_mode,omitempty"`
	OpenAIResponsesMode string            `json:"openai_responses_mode,omitempty"`
	OpenAIResponsesOK   *bool             `json:"openai_responses_supported,omitempty"`
	Proxy               *DirectProbeProxy `json:"proxy,omitempty"`
	RoutingFingerprint  string            `json:"routing_fingerprint"`
}

type DirectProbeConfig struct {
	Credential         CredentialEnvelope            `json:"credential"`
	AuthorizationState DirectProbeAuthorizationState `json:"authorization_state"`
	ImportedAt         *time.Time                    `json:"imported_at,omitempty"`
	UpdatedAt          *time.Time                    `json:"updated_at,omitempty"`
	ActionMessage      string                        `json:"action_message,omitempty"`
	RoutingFingerprint string                        `json:"routing_fingerprint,omitempty"`
}

type DirectProbeView struct {
	Source             ProbeSource                   `json:"source"`
	AuthorizationState DirectProbeAuthorizationState `json:"authorization_state"`
	ImportedAt         *time.Time                    `json:"imported_at,omitempty"`
	UpdatedAt          *time.Time                    `json:"updated_at,omitempty"`
	ActionMessage      string                        `json:"action_message,omitempty"`
}

type ManagedAccountView struct {
	AccountID             int64            `json:"account_id"`
	Name                  string           `json:"name"`
	Platform              string           `json:"platform"`
	AccountStatus         string           `json:"account_status"`
	Schedulable           bool             `json:"schedulable"`
	Policy                Policy           `json:"policy"`
	ConsecutiveFailures   int              `json:"consecutive_failures"`
	ConsecutiveSuccesses  int              `json:"consecutive_successes"`
	ManagedSuspended      bool             `json:"managed_suspended"`
	LastCheckAt           *time.Time       `json:"last_check_at,omitempty"`
	NextCheckAt           *time.Time       `json:"next_check_at,omitempty"`
	LastError             string           `json:"last_error,omitempty"`
	LastFailureKind       CheckFailureKind `json:"last_failure_kind,omitempty"`
	History               []CheckResult    `json:"history"`
	DetectionStats        DetectionStats   `json:"detection_stats,omitempty"`
	BalanceAlertThreshold *float64         `json:"balance_alert_threshold,omitempty"`
	Probe                 DirectProbeView  `json:"probe"`
	CreatedAt             time.Time        `json:"created_at"`
	UpdatedAt             time.Time        `json:"updated_at"`
	Running               bool             `json:"running"`
}

func (m ManagedAccount) EffectiveProbeSource() ProbeSource {
	if m.ProbeSource == ProbeSourceDirect {
		return ProbeSourceDirect
	}
	return ProbeSourceLegacy
}

func (m ManagedAccount) PublicView() ManagedAccountView {
	view := ManagedAccountView{
		AccountID:             m.AccountID,
		Name:                  m.Name,
		Platform:              m.Platform,
		AccountStatus:         m.AccountStatus,
		Schedulable:           m.Schedulable,
		Policy:                m.Policy,
		ConsecutiveFailures:   m.ConsecutiveFailures,
		ConsecutiveSuccesses:  m.ConsecutiveSuccesses,
		ManagedSuspended:      m.ManagedSuspended,
		LastCheckAt:           cloneTime(m.LastCheckAt),
		NextCheckAt:           cloneTime(m.NextCheckAt),
		LastError:             m.LastError,
		LastFailureKind:       m.LastFailureKind,
		History:               append([]CheckResult(nil), m.History...),
		DetectionStats:        cloneDetectionStats(m.DetectionStats),
		BalanceAlertThreshold: cloneFloat64(m.BalanceAlertThreshold),
		CreatedAt:             m.CreatedAt,
		UpdatedAt:             m.UpdatedAt,
		Running:               m.Running,
		Probe: DirectProbeView{
			Source:             m.EffectiveProbeSource(),
			AuthorizationState: DirectProbeAuthorizationMissing,
		},
	}
	if view.Probe.Source == ProbeSourceLegacy {
		view.Probe.AuthorizationState = DirectProbeAuthorizationMissing
		return view
	}
	if m.DirectProbe == nil {
		view.Probe.AuthorizationState = DirectProbeNeedsReauthorization
		view.Probe.ActionMessage = "直连探测授权信息缺失，请重新授权"
		return view
	}
	view.Probe.AuthorizationState = m.DirectProbe.AuthorizationState
	view.Probe.ImportedAt = cloneTime(m.DirectProbe.ImportedAt)
	view.Probe.UpdatedAt = cloneTime(m.DirectProbe.UpdatedAt)
	view.Probe.ActionMessage = m.DirectProbe.ActionMessage
	return view
}

func cloneTime(value *time.Time) *time.Time {
	if value == nil {
		return nil
	}
	copy := *value
	return &copy
}

func (m *ManagedAccount) ApplySnapshot(account UpstreamAccount) {
	m.Name = account.Name
	m.Platform = account.Platform
	m.AccountStatus = account.Status
	m.Schedulable = account.Schedulable
}

func (m *ManagedAccount) AddHistory(result CheckResult) {
	m.History = append([]CheckResult{result}, m.History...)
	if len(m.History) > HistoryLimit {
		m.History = m.History[:HistoryLimit]
	}
}

type State struct {
	Version                 int                               `json:"version"`
	Accounts                map[string]ManagedAccount         `json:"accounts"`
	Upstreams               map[string]ManagedUpstream        `json:"upstreams,omitempty"`
	Protections             map[string]GroupAccountProtection `json:"group_account_protections,omitempty"`
	GroupProtectionDefaults map[string]GroupProtectionDefault `json:"group_protection_defaults,omitempty"`
	Notification            *NotificationSettings             `json:"notification,omitempty"`
	GroupBalanceThresholds  map[string]float64                `json:"group_balance_thresholds,omitempty"`
	BalanceAlertStates      map[string]BalanceAlertState      `json:"balance_alert_states,omitempty"`
	CapacityAlertStates     map[string]CapacityAlertState     `json:"capacity_alert_states,omitempty"`
	MultiplierAlertStates   map[string]MultiplierAlertState   `json:"multiplier_alert_states,omitempty"`
}

// NotificationSettings contains only non-secret Bark metadata and an encrypted
// envelope for the device key, encryption key, and optional Basic Auth secret.
type NotificationSettings struct {
	Enabled           bool               `json:"enabled"`
	BarkEndpoint      string             `json:"bark_endpoint,omitempty"`
	BarkBasicAuthUser string             `json:"bark_basic_auth_user,omitempty"`
	BarkCredentials   CredentialEnvelope `json:"bark_credentials,omitempty"`
	LastDeliveryAt    *time.Time         `json:"last_delivery_at,omitempty"`
	LastDeliveryError string             `json:"last_delivery_error,omitempty"`
	UpdatedAt         time.Time          `json:"updated_at"`
}

type BalanceAlertState struct {
	Configured        bool      `json:"configured"`
	Below             bool      `json:"below"`
	Threshold         float64   `json:"threshold,omitempty"`
	LastAvailable     *float64  `json:"last_available,omitempty"`
	LastDeliveryError string    `json:"last_delivery_error,omitempty"`
	UpdatedAt         time.Time `json:"updated_at"`
}

type CapacityAlertBand string

const (
	CapacityBandUnknown CapacityAlertBand = "unknown"
	CapacityBandZero    CapacityAlertBand = "zero"
	CapacityBandOne     CapacityAlertBand = "one"
	CapacityBandMany    CapacityAlertBand = "many"
)

type CapacityAlertState struct {
	Band              CapacityAlertBand `json:"band"`
	LastCount         int               `json:"last_count"`
	LastDeliveryError string            `json:"last_delivery_error,omitempty"`
	UpdatedAt         time.Time         `json:"updated_at"`
}

type MultiplierAlertState struct {
	Initialized         bool      `json:"initialized"`
	LastFinalMultiplier *float64  `json:"last_final_multiplier,omitempty"`
	ProtectionTriggered bool      `json:"protection_triggered"`
	LastDeliveryError   string    `json:"last_delivery_error,omitempty"`
	UpdatedAt           time.Time `json:"updated_at"`
}

type BarkNotificationSecrets struct {
	DeviceKey         string `json:"device_key"`
	EncryptionKey     string `json:"encryption_key"`
	BasicAuthPassword string `json:"basic_auth_password,omitempty"`
}

func maxInt64(value, minimum int64) int64 {
	if value < minimum {
		return minimum
	}
	return value
}

func cloneFloat64(value *float64) *float64 {
	if value == nil {
		return nil
	}
	copy := *value
	return &copy
}

func cloneDetectionStats(stats DetectionStats) DetectionStats {
	if stats.LastCost != nil {
		copy := *stats.LastCost
		stats.LastCost = &copy
	}
	stats.LastUsageAt = cloneTime(stats.LastUsageAt)
	return stats
}
