package model

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"math"
	"net"
	"net/url"
	"path"
	"strings"
	"time"
)

const (
	MaxUpstreamNameLength     = 120
	MaxIdentityLabelLength    = 120
	MaxUpstreamResponseKeys   = 10000
	MaxUpstreamErrorLength    = 500
	MaxUpstreamCredentialSize = 64 << 10
)

type UpstreamSiteType string

const (
	UpstreamTypeUnknown UpstreamSiteType = "unknown"
	UpstreamTypeSub2API UpstreamSiteType = "sub2api"
	UpstreamTypeNewAPI  UpstreamSiteType = "newapi"
)

func ParseUpstreamSiteType(value string, allowUnknown bool) (UpstreamSiteType, error) {
	siteType := UpstreamSiteType(strings.ToLower(strings.TrimSpace(value)))
	switch siteType {
	case UpstreamTypeSub2API, UpstreamTypeNewAPI:
		return siteType, nil
	case "", UpstreamTypeUnknown:
		if allowUnknown {
			return UpstreamTypeUnknown, nil
		}
	}
	return "", errors.New("上游类型必须是 sub2api 或 newapi")
}

type UpstreamAuthMode string

const (
	UpstreamAuthPassword UpstreamAuthMode = "password"
	UpstreamAuthToken    UpstreamAuthMode = "token"
	UpstreamAuthSession  UpstreamAuthMode = "session"
)

func ParseUpstreamAuthMode(value string) (UpstreamAuthMode, error) {
	mode := UpstreamAuthMode(strings.ToLower(strings.TrimSpace(value)))
	switch mode {
	case UpstreamAuthPassword, UpstreamAuthToken, UpstreamAuthSession:
		return mode, nil
	default:
		return "", errors.New("登录方式必须是 password、token 或 session")
	}
}

type UpstreamIdentityStatus string

const (
	IdentityStatusConnected    UpstreamIdentityStatus = "connected"
	IdentityStatusDisconnected UpstreamIdentityStatus = "disconnected"
	IdentityStatusExpired      UpstreamIdentityStatus = "expired"
	IdentityStatusCaptcha      UpstreamIdentityStatus = "captcha_required"
	IdentityStatusTwoFactor    UpstreamIdentityStatus = "two_factor_required"
	IdentityStatusAccessDenied UpstreamIdentityStatus = "management_access_denied"
	IdentityStatusNetworkError UpstreamIdentityStatus = "network_error"
	IdentityStatusUnknownType  UpstreamIdentityStatus = "unknown_type"
	IdentityStatusInvalid      UpstreamIdentityStatus = "invalid"
	IdentityStatusSyncError    UpstreamIdentityStatus = "sync_error"
)

type CredentialEnvelope struct {
	Version    int    `json:"version"`
	Nonce      string `json:"nonce"`
	Ciphertext string `json:"ciphertext"`
}

type UpstreamDetection struct {
	Type       UpstreamSiteType `json:"type"`
	Evidence   string           `json:"evidence,omitempty"`
	DetectedAt *time.Time       `json:"detected_at,omitempty"`
}

type RechargeRateInputMode string

const (
	RechargeRateUSDPerCNY RechargeRateInputMode = "usd_per_cny"
	RechargeRateCNYPerUSD RechargeRateInputMode = "cny_per_usd"
)

type UpstreamRechargeRate struct {
	InputMode  RechargeRateInputMode `json:"input_mode"`
	InputValue float64               `json:"input_value"`
	CNYPerUSD  float64               `json:"cny_per_usd"`
	UpdatedAt  time.Time             `json:"updated_at"`
}

func NewUpstreamRechargeRate(mode RechargeRateInputMode, value float64, now time.Time) (UpstreamRechargeRate, error) {
	if value <= 0 || math.IsNaN(value) || math.IsInf(value, 0) {
		return UpstreamRechargeRate{}, errors.New("充值倍率必须是大于 0 的有限数值")
	}
	canonical := value
	switch mode {
	case RechargeRateUSDPerCNY:
		canonical = 1 / value
	case RechargeRateCNYPerUSD:
	default:
		return UpstreamRechargeRate{}, errors.New("充值倍率输入方式无效")
	}
	if canonical <= 0 || math.IsNaN(canonical) || math.IsInf(canonical, 0) {
		return UpstreamRechargeRate{}, errors.New("充值倍率换算结果超出有效范围")
	}
	return UpstreamRechargeRate{InputMode: mode, InputValue: value, CNYPerUSD: canonical, UpdatedAt: now.UTC()}, nil
}

type RemoteKey struct {
	ID                 string     `json:"id"`
	Name               string     `json:"name"`
	MaskedKey          string     `json:"masked_key,omitempty"`
	Fingerprint        string     `json:"fingerprint,omitempty"`
	MatchAvailable     bool       `json:"match_available"`
	Status             string     `json:"status"`
	Group              string     `json:"group,omitempty"`
	UnlimitedQuota     bool       `json:"unlimited_quota,omitempty"`
	QuotaLimit         *float64   `json:"quota_limit,omitempty"`
	QuotaUsed          *float64   `json:"quota_used,omitempty"`
	Multiplier         *float64   `json:"multiplier,omitempty"`
	MultiplierSource   string     `json:"multiplier_source,omitempty"`
	MultiplierObserved *time.Time `json:"multiplier_observed_at,omitempty"`
	LastUsedAt         *time.Time `json:"last_used_at,omitempty"`
	ExpiresAt          *time.Time `json:"expires_at,omitempty"`
	LocalAccountID     *int64     `json:"local_account_id,omitempty"`
	Stale              bool       `json:"stale,omitempty"`
	SyncedAt           time.Time  `json:"synced_at"`
}

// UpstreamBalance is the last successful account-level balance observation
// for one authenticated upstream identity. It intentionally remains separate
// from key quota and the administrator's recharge rate.
type UpstreamBalance struct {
	Amount       float64   `json:"amount"`
	Unit         string    `json:"unit"`
	Source       string    `json:"source"`
	ObservedAt   time.Time `json:"observed_at"`
	Stale        bool      `json:"stale,omitempty"`
	RawQuota     *float64  `json:"raw_quota,omitempty"`
	QuotaPerUnit *float64  `json:"quota_per_unit,omitempty"`
}

type UpstreamIdentity struct {
	ID            string                 `json:"id"`
	Label         string                 `json:"label"`
	AuthMode      UpstreamAuthMode       `json:"auth_mode"`
	Principal     string                 `json:"principal,omitempty"`
	Credential    CredentialEnvelope     `json:"credential"`
	Status        UpstreamIdentityStatus `json:"status"`
	StatusCode    string                 `json:"status_code,omitempty"`
	StatusMessage string                 `json:"status_message,omitempty"`
	LastAttemptAt *time.Time             `json:"last_attempt_at,omitempty"`
	LastSuccessAt *time.Time             `json:"last_success_at,omitempty"`
	Balance       *UpstreamBalance       `json:"balance,omitempty"`
	Keys          map[string]RemoteKey   `json:"keys,omitempty"`
	CreatedAt     time.Time              `json:"created_at"`
	UpdatedAt     time.Time              `json:"updated_at"`
}

type ManagedUpstream struct {
	ID            string                      `json:"id"`
	Name          string                      `json:"name"`
	BaseURL       string                      `json:"base_url"`
	ManagementURL string                      `json:"site_url,omitempty"`
	TypeOverride  UpstreamSiteType            `json:"type_override,omitempty"`
	Detection     UpstreamDetection           `json:"detection"`
	RechargeRate  *UpstreamRechargeRate       `json:"recharge_rate,omitempty"`
	Identities    map[string]UpstreamIdentity `json:"identities,omitempty"`
	CreatedAt     time.Time                   `json:"created_at"`
	UpdatedAt     time.Time                   `json:"updated_at"`
}

func (u ManagedUpstream) EffectiveType() UpstreamSiteType {
	if u.TypeOverride == UpstreamTypeSub2API || u.TypeOverride == UpstreamTypeNewAPI {
		return u.TypeOverride
	}
	if u.Detection.Type == UpstreamTypeSub2API || u.Detection.Type == UpstreamTypeNewAPI {
		return u.Detection.Type
	}
	return UpstreamTypeUnknown
}

// EffectiveManagementURL returns the verified management-site address. The
// persisted JSON name stays site_url so existing state files need no migration.
// Records without an override continue to use their API address for both roles.
func (u ManagedUpstream) EffectiveManagementURL() string {
	if managementURL := strings.TrimSpace(u.ManagementURL); managementURL != "" {
		return managementURL
	}
	return strings.TrimSpace(u.BaseURL)
}

func NormalizeUpstreamBaseURL(raw string) (string, error) {
	parsed, err := url.Parse(strings.TrimSpace(raw))
	if err != nil || parsed.Host == "" || (parsed.Scheme != "http" && parsed.Scheme != "https") {
		return "", errors.New("上游地址必须是绝对 HTTP(S) URL")
	}
	if parsed.User != nil || parsed.RawQuery != "" || parsed.Fragment != "" {
		return "", errors.New("上游地址不能包含凭证、查询参数或片段")
	}

	parsed.Scheme = strings.ToLower(parsed.Scheme)
	parsed.Host = normalizeURLHost(parsed.Scheme, parsed.Host)
	if parsed.Host == "" {
		return "", errors.New("上游地址主机无效")
	}
	cleanPath := path.Clean("/" + strings.TrimSpace(parsed.Path))
	if cleanPath == "/" || cleanPath == "." {
		cleanPath = ""
	}
	lowerPath := strings.ToLower(cleanPath)
	for _, suffix := range []string{"/api/v1", "/v1"} {
		if strings.HasSuffix(lowerPath, suffix) {
			cleanPath = strings.TrimSuffix(cleanPath, cleanPath[len(cleanPath)-len(suffix):])
			cleanPath = strings.TrimRight(cleanPath, "/")
			break
		}
	}
	parsed.Path = cleanPath
	parsed.RawPath = ""
	parsed.ForceQuery = false
	return strings.TrimRight(parsed.String(), "/"), nil
}

func StableUpstreamID(baseURL string) string {
	digest := sha256.Sum256([]byte(baseURL))
	return "up_" + hex.EncodeToString(digest[:16])
}

func normalizeURLHost(scheme, host string) string {
	hostname := strings.ToLower(strings.TrimSuffix(strings.TrimSpace(host), "."))
	if hostname == "" {
		return ""
	}
	if parsedHost, port, err := net.SplitHostPort(hostname); err == nil {
		parsedHost = strings.Trim(parsedHost, "[]")
		if (scheme == "https" && port == "443") || (scheme == "http" && port == "80") {
			if strings.Contains(parsedHost, ":") {
				return "[" + parsedHost + "]"
			}
			return parsedHost
		}
		return net.JoinHostPort(parsedHost, port)
	}
	return hostname
}

func SanitizeUpstreamMessage(value string) string {
	value = strings.TrimSpace(value)
	if len(value) > MaxUpstreamErrorLength {
		value = value[:MaxUpstreamErrorLength]
	}
	return value
}
