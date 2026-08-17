package web

import (
	"context"
	"crypto/sha256"
	"embed"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"log/slog"
	"math"
	"net"
	"net/http"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/Wei-Shaw/sub2api-account-auto-scheduler/internal/core"
	"github.com/Wei-Shaw/sub2api-account-auto-scheduler/internal/engine"
	"github.com/Wei-Shaw/sub2api-account-auto-scheduler/internal/model"
	"github.com/Wei-Shaw/sub2api-account-auto-scheduler/internal/notify"
	"github.com/Wei-Shaw/sub2api-account-auto-scheduler/internal/store"
	"github.com/Wei-Shaw/sub2api-account-auto-scheduler/internal/upstream"
)

//go:embed static/*
var staticFiles embed.FS

type AdminCore interface {
	ValidateAdminJWT(ctx context.Context, token string, identity core.ForwardedIdentity) (core.AdminUser, error)
	RegisterAdminMenu(ctx context.Context, publicURL string) error
}

type ConsoleCore interface {
	ListAccounts(ctx context.Context) ([]model.UpstreamAccount, error)
	ListGroups(ctx context.Context) ([]model.UpstreamGroup, error)
	GetTodayStatsBatch(ctx context.Context, accountIDs []int64) (map[string]model.WindowStats, error)
	GetPassiveUsage(ctx context.Context, accountID int64) (model.AccountUsageInfo, error)
	SetAccountGroup(ctx context.Context, accountID, groupID int64, bound bool) (model.UpstreamAccount, error)
}

type Options struct {
	UIOrigin          string
	PublicURL         string
	TrustProxyHeaders bool
	AuthCacheTTL      time.Duration
	Upstreams         UpstreamConsole
	Notifications     NotificationConsole
}

type NotificationConsole interface {
	Settings() notify.SettingsView
	SaveSettings(input notify.SettingsInput) (notify.SettingsView, error)
	ClearSettings() error
	Test(ctx context.Context) error
	GroupBalanceThresholds() map[int64]float64
	GroupBalanceThreshold(groupID int64) (float64, bool)
	SetGroupBalanceThreshold(groupID int64, threshold *float64) error
	SetAccountBalanceThreshold(accountID int64, threshold *float64) error
}

type UpstreamConsole interface {
	CredentialsEnabled() bool
	List(ctx context.Context) (upstream.ListResponse, error)
	Create(ctx context.Context, input upstream.CreateInput) (upstream.UpstreamView, error)
	Detect(ctx context.Context, upstreamID string) (upstream.UpstreamView, error)
	SetType(ctx context.Context, upstreamID string, siteType model.UpstreamSiteType) (upstream.UpstreamView, error)
	SetRechargeRate(ctx context.Context, upstreamID string, input upstream.RechargeRateInput) (upstream.UpstreamView, error)
	ClearRechargeRate(ctx context.Context, upstreamID string) (upstream.UpstreamView, error)
	Connect(ctx context.Context, upstreamID string, input upstream.ConnectInput) (upstream.IdentityView, error)
	Delete(ctx context.Context, upstreamID string) error
	DeleteIdentity(upstreamID, identityID string) error
	SyncIdentity(ctx context.Context, upstreamID, identityID string) error
	SyncUpstream(ctx context.Context, upstreamID string) []upstream.SyncOutcome
	SyncAll(ctx context.Context) []upstream.SyncOutcome
	SaveBindings(ctx context.Context, upstreamID string, bindings []upstream.BindingInput) (upstream.UpstreamView, error)
	AutoMatch(ctx context.Context, upstreamID, adminJWT string, forwarded core.ForwardedIdentity) (upstream.MatchResult, error)
}

type upstreamLoginChallengeConsole interface {
	StartLoginChallenge(ctx context.Context, upstreamID string, input upstream.LoginChallengeInput) (upstream.LoginChallengeResult, error)
}

type overviewMultiplierProjection interface {
	LocalAccountFinalMultipliers(accounts []model.UpstreamAccount) map[int64]upstream.LocalAccountFinalMultiplier
}

type groupProtectionProjection interface {
	GroupAccountProtectionViews(accounts []model.UpstreamAccount) map[int64]map[string]upstream.GroupAccountProtectionView
	LogicalGroupIDs(accounts []model.UpstreamAccount) map[int64][]int64
}

type groupProtectionDefaultProjection interface {
	GroupProtectionDefaults() map[int64]upstream.GroupProtectionDefaultView
}

type protectedGroupBindingConsole interface {
	SaveGroupBindings(ctx context.Context, groupID int64, selected []int64) ([]int64, []upstream.GroupBindingFailure, error)
	SetGroupAccountProtection(ctx context.Context, groupID, accountID int64, multiplier float64) (upstream.GroupAccountProtectionView, error)
	ReleaseGroupAccountProtection(ctx context.Context, groupID, accountID int64) (upstream.GroupAccountProtectionView, error)
	RemoveGroupAccountBinding(ctx context.Context, groupID, accountID int64) error
	SetGroupAccountBinding(ctx context.Context, groupID, accountID int64, bound bool) error
}

type groupProtectionDefaultConsole interface {
	GetGroupProtectionDefault(groupID int64) (upstream.GroupProtectionDefaultView, error)
	SetGroupProtectionDefault(ctx context.Context, groupID int64, multiplier float64) (upstream.GroupProtectionDefaultView, error)
	DeleteGroupProtectionDefault(ctx context.Context, groupID int64) error
}

type Server struct {
	engine          *engine.Engine
	core            AdminCore
	console         ConsoleCore
	options         Options
	logger          *slog.Logger
	upstreams       UpstreamConsole
	notifications   NotificationConsole
	loginChallenges *loginChallengeStore

	cacheMu   sync.Mutex
	authCache map[string]cachedSession
}

type cachedSession struct {
	User      core.AdminUser
	ExpiresAt time.Time
}

type contextKey string

const adminUserKey contextKey = "admin-user"
const adminTokenKey contextKey = "admin-token"
const forwardedIdentityKey contextKey = "forwarded-identity"

type policyRequest struct {
	AccountID         int64   `json:"account_id"`
	Enabled           *bool   `json:"enabled"`
	IntervalSeconds   *int    `json:"interval_seconds"`
	Model             *string `json:"model"`
	Prompt            *string `json:"prompt"`
	LatencyLimitMS    *int64  `json:"latency_limit_ms"`
	FailureThreshold  *int    `json:"failure_threshold"`
	RecoveryThreshold *int    `json:"recovery_threshold"`
}

type notificationSettingsRequest struct {
	Enabled           bool   `json:"enabled"`
	BarkEndpoint      string `json:"bark_endpoint"`
	BarkBasicAuthUser string `json:"bark_basic_auth_user"`
	DeviceKey         string `json:"device_key"`
	EncryptionKey     string `json:"encryption_key"`
	BasicAuthPassword string `json:"basic_auth_password"`
}

type balanceAlertRequest struct {
	Threshold *float64 `json:"threshold"`
}

type groupBindingRequest struct {
	Bound *bool `json:"bound"`
}

type protectionRequest struct {
	ProtectionMultiplier *float64 `json:"protection_multiplier"`
}

type bulkGroupBindingRequest struct {
	AccountIDs *[]int64 `json:"account_ids"`
}

type groupBindingFailure struct {
	AccountID int64  `json:"account_id"`
	Name      string `json:"name"`
	Code      string `json:"code"`
	Message   string `json:"message"`
}

type bulkGroupBindingResponse struct {
	GroupID           int64                 `json:"group_id"`
	UpdatedAccountIDs []int64               `json:"updated_account_ids"`
	Failures          []groupBindingFailure `json:"failures"`
}

type directProbeBatchRequest struct {
	AccountIDs *[]int64 `json:"account_ids"`
}

type directProbeBatchFailure struct {
	AccountID int64  `json:"account_id"`
	Code      string `json:"code"`
	Message   string `json:"message"`
}

type directProbeBatchResponse struct {
	Authorized []model.ManagedAccountView `json:"authorized"`
	Failures   []directProbeBatchFailure  `json:"failures"`
}

type overviewAccount struct {
	model.UpstreamAccount
	TodayUsage              model.WindowStats                              `json:"today_usage"`
	Config                  *model.ManagedAccountView                      `json:"config,omitempty"`
	AdminBalance            overviewAdminBalance                           `json:"admin_balance"`
	UpstreamFinalMultiplier *upstream.LocalAccountFinalMultiplier          `json:"upstream_final_multiplier,omitempty"`
	LogicalGroupIDs         []int64                                        `json:"logical_group_ids,omitempty"`
	GroupProtections        map[string]upstream.GroupAccountProtectionView `json:"group_protections,omitempty"`
}

type overviewAdminBalance struct {
	Configured          bool     `json:"configured"`
	Managed             bool     `json:"managed"`
	Unlimited           bool     `json:"unlimited"`
	Insufficient        bool     `json:"insufficient"`
	Remaining           *float64 `json:"remaining,omitempty"`
	ExhaustedDimensions []string `json:"exhausted_dimensions,omitempty"`
}

func NewServer(scheduler *engine.Engine, coreClient AdminCore, options Options, logger *slog.Logger) *Server {
	if logger == nil {
		logger = slog.Default()
	}
	server := &Server{
		engine:          scheduler,
		core:            coreClient,
		options:         options,
		logger:          logger,
		upstreams:       options.Upstreams,
		notifications:   options.Notifications,
		loginChallenges: newLoginChallengeStore(defaultLoginChallengeTTL),
		authCache:       map[string]cachedSession{},
	}
	server.console, _ = coreClient.(ConsoleCore)
	return server
}

func (s *Server) Handler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /healthz", s.handleHealth)
	mux.Handle("GET /api/session", s.requireAdmin(http.HandlerFunc(s.handleSession)))
	mux.Handle("GET /api/overview", s.requireAdmin(http.HandlerFunc(s.handleOverview)))
	mux.Handle("GET /api/accounts", s.requireAdmin(http.HandlerFunc(s.handleAccounts)))
	mux.Handle("GET /api/accounts/{accountID}/usage", s.requireAdmin(http.HandlerFunc(s.handleAccountUsage)))
	mux.Handle("GET /api/notifications", s.requireAdmin(http.HandlerFunc(s.handleNotificationSettings)))
	mux.Handle("PUT /api/notifications", s.requireAdmin(http.HandlerFunc(s.handleSaveNotificationSettings)))
	mux.Handle("DELETE /api/notifications", s.requireAdmin(http.HandlerFunc(s.handleClearNotificationSettings)))
	mux.Handle("POST /api/notifications/test", s.requireAdmin(http.HandlerFunc(s.handleTestNotification)))
	mux.Handle("PUT /api/groups/{groupID}/balance-alert", s.requireAdmin(http.HandlerFunc(s.handleSetGroupBalanceAlert)))
	mux.Handle("DELETE /api/groups/{groupID}/balance-alert", s.requireAdmin(http.HandlerFunc(s.handleClearGroupBalanceAlert)))
	mux.Handle("PUT /api/configs/{accountID}/balance-alert", s.requireAdmin(http.HandlerFunc(s.handleSetAccountBalanceAlert)))
	mux.Handle("DELETE /api/configs/{accountID}/balance-alert", s.requireAdmin(http.HandlerFunc(s.handleClearAccountBalanceAlert)))
	mux.Handle("PUT /api/groups/{groupID}/accounts", s.requireAdmin(http.HandlerFunc(s.handleBulkGroupBindings)))
	mux.Handle("PUT /api/groups/{groupID}/accounts/{accountID}", s.requireAdmin(http.HandlerFunc(s.handleGroupBinding)))
	mux.Handle("PUT /api/groups/{groupID}/accounts/{accountID}/protection", s.requireAdmin(http.HandlerFunc(s.handleSetGroupProtection)))
	mux.Handle("POST /api/groups/{groupID}/accounts/{accountID}/protection/release", s.requireAdmin(http.HandlerFunc(s.handleReleaseGroupProtection)))
	mux.Handle("DELETE /api/groups/{groupID}/accounts/{accountID}/binding", s.requireAdmin(http.HandlerFunc(s.handleRemoveGroupBinding)))
	mux.Handle("GET /api/groups/{groupID}/protection-default", s.requireAdmin(http.HandlerFunc(s.handleGetGroupProtectionDefault)))
	mux.Handle("PUT /api/groups/{groupID}/protection-default", s.requireAdmin(http.HandlerFunc(s.handleSetGroupProtectionDefault)))
	mux.Handle("DELETE /api/groups/{groupID}/protection-default", s.requireAdmin(http.HandlerFunc(s.handleDeleteGroupProtectionDefault)))
	mux.Handle("GET /api/configs", s.requireAdmin(http.HandlerFunc(s.handleListConfigs)))
	mux.Handle("POST /api/configs", s.requireAdmin(http.HandlerFunc(s.handleCreateConfig)))
	mux.Handle("PUT /api/configs/{accountID}", s.requireAdmin(http.HandlerFunc(s.handleUpdateConfig)))
	mux.Handle("DELETE /api/configs/{accountID}", s.requireAdmin(http.HandlerFunc(s.handleDeleteConfig)))
	mux.Handle("POST /api/configs/{accountID}/run", s.requireAdmin(http.HandlerFunc(s.handleRunNow)))
	mux.Handle("GET /api/configs/{accountID}/direct-probe", s.requireAdmin(http.HandlerFunc(s.handleDirectProbeStatus)))
	mux.Handle("POST /api/configs/{accountID}/direct-probe", s.requireAdmin(http.HandlerFunc(s.handleAuthorizeDirectProbe)))
	mux.Handle("DELETE /api/configs/{accountID}/direct-probe", s.requireAdmin(http.HandlerFunc(s.handleRevokeDirectProbe)))
	mux.Handle("POST /api/direct-probes/authorize", s.requireAdmin(http.HandlerFunc(s.handleBatchAuthorizeDirectProbes)))
	mux.Handle("POST /api/tab/register", s.requireAdmin(http.HandlerFunc(s.handleRegisterTab)))
	mux.Handle("GET /api/upstreams", s.requireAdmin(http.HandlerFunc(s.handleUpstreams)))
	mux.Handle("POST /api/upstreams", s.requireAdmin(http.HandlerFunc(s.handleCreateUpstream)))
	mux.Handle("DELETE /api/upstreams/{upstreamID}", s.requireAdmin(http.HandlerFunc(s.handleDeleteUpstream)))
	mux.Handle("POST /api/upstreams/sync", s.requireAdmin(http.HandlerFunc(s.handleSyncAllUpstreams)))
	mux.Handle("POST /api/upstreams/{upstreamID}/detect", s.requireAdmin(http.HandlerFunc(s.handleDetectUpstream)))
	mux.Handle("PUT /api/upstreams/{upstreamID}/type", s.requireAdmin(http.HandlerFunc(s.handleSetUpstreamType)))
	mux.Handle("PUT /api/upstreams/{upstreamID}/recharge-rate", s.requireAdmin(http.HandlerFunc(s.handleSetUpstreamRechargeRate)))
	mux.Handle("DELETE /api/upstreams/{upstreamID}/recharge-rate", s.requireAdmin(http.HandlerFunc(s.handleClearUpstreamRechargeRate)))
	mux.Handle("POST /api/upstreams/{upstreamID}/login-challenges", s.requireAdmin(http.HandlerFunc(s.handleStartUpstreamLoginChallenge)))
	mux.Handle("POST /api/upstreams/{upstreamID}/identities", s.requireAdmin(http.HandlerFunc(s.handleConnectUpstreamIdentity)))
	mux.Handle("PUT /api/upstreams/{upstreamID}/identities/{identityID}", s.requireAdmin(http.HandlerFunc(s.handleConnectUpstreamIdentity)))
	mux.Handle("DELETE /api/upstreams/{upstreamID}/identities/{identityID}", s.requireAdmin(http.HandlerFunc(s.handleDeleteUpstreamIdentity)))
	mux.Handle("POST /api/upstreams/{upstreamID}/identities/{identityID}/sync", s.requireAdmin(http.HandlerFunc(s.handleSyncUpstreamIdentity)))
	mux.Handle("POST /api/upstreams/{upstreamID}/sync", s.requireAdmin(http.HandlerFunc(s.handleSyncUpstream)))
	mux.Handle("PUT /api/upstreams/{upstreamID}/bindings", s.requireAdmin(http.HandlerFunc(s.handleSaveUpstreamBindings)))
	mux.Handle("POST /api/upstreams/{upstreamID}/auto-match", s.requireAdmin(http.HandlerFunc(s.handleAutoMatchUpstream)))
	mux.Handle("/", s.staticHandler())
	return s.securityHeaders(mux)
}

func (s *Server) RegisterTab(ctx context.Context) error {
	if strings.TrimSpace(s.options.PublicURL) == "" {
		return errors.New("AUTO_SCHEDULER_PUBLIC_URL is not configured")
	}
	return s.core.RegisterAdminMenu(ctx, s.options.PublicURL)
}

func (s *Server) handleHealth(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

func (s *Server) handleSession(w http.ResponseWriter, r *http.Request) {
	user, _ := r.Context().Value(adminUserKey).(core.AdminUser)
	writeJSON(w, http.StatusOK, map[string]any{
		"user":                    user,
		"default_policy":          model.DefaultPolicy(),
		"public_url":              s.options.PublicURL,
		"credentials_enabled":     s.upstreams != nil && s.upstreams.CredentialsEnabled(),
		"notifications_available": s.notifications != nil,
	})
}

func (s *Server) handleAccounts(w http.ResponseWriter, r *http.Request) {
	accounts, err := s.engine.AvailableAccounts(r.Context())
	if err != nil {
		writeError(w, http.StatusBadGateway, "CORE_UNAVAILABLE", err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"accounts": accounts})
}

func (s *Server) handleOverview(w http.ResponseWriter, r *http.Request) {
	if s.console == nil || s.engine == nil {
		writeError(w, http.StatusServiceUnavailable, "CONSOLE_UNAVAILABLE", "账号总览暂不可用")
		return
	}
	groups, err := s.console.ListGroups(r.Context())
	if err != nil {
		writeError(w, http.StatusBadGateway, "GROUPS_UNAVAILABLE", err.Error())
		return
	}
	accounts, err := s.console.ListAccounts(r.Context())
	if err != nil {
		writeError(w, http.StatusBadGateway, "ACCOUNTS_UNAVAILABLE", err.Error())
		return
	}
	accountIDs := make([]int64, 0, len(accounts))
	for _, account := range accounts {
		accountIDs = append(accountIDs, account.ID)
	}
	todayStats, err := s.console.GetTodayStatsBatch(r.Context(), accountIDs)
	if err != nil {
		writeError(w, http.StatusBadGateway, "USAGE_UNAVAILABLE", err.Error())
		return
	}
	configs := s.engine.List()
	configByID := make(map[int64]model.ManagedAccount, len(configs))
	for _, config := range configs {
		configByID[config.AccountID] = config
	}
	finalMultipliers := map[int64]upstream.LocalAccountFinalMultiplier{}
	logicalGroupIDs := map[int64][]int64{}
	groupProtections := map[int64]map[string]upstream.GroupAccountProtectionView{}
	groupProtectionDefaults := map[int64]upstream.GroupProtectionDefaultView{}
	groupBalanceThresholds := map[int64]float64{}
	if projections, ok := s.upstreams.(overviewMultiplierProjection); ok {
		finalMultipliers = projections.LocalAccountFinalMultipliers(accounts)
	}
	if projections, ok := s.upstreams.(groupProtectionProjection); ok {
		logicalGroupIDs = projections.LogicalGroupIDs(accounts)
		groupProtections = projections.GroupAccountProtectionViews(accounts)
	}
	if projections, ok := s.upstreams.(groupProtectionDefaultProjection); ok {
		groupProtectionDefaults = projections.GroupProtectionDefaults()
	}
	if s.notifications != nil {
		groupBalanceThresholds = s.notifications.GroupBalanceThresholds()
	}
	overviewAccounts := make([]overviewAccount, 0, len(accounts))
	for _, account := range accounts {
		// The overview uses only the administrator-configured upstream snapshot.
		// Do not expose Sub2API's independent billing-probe result here.
		account.DetectedRate = nil
		item := overviewAccount{
			UpstreamAccount: account,
			TodayUsage:      todayStats[strconv.FormatInt(account.ID, 10)],
			AdminBalance:    projectAdminBalance(account),
		}
		if account.IsAPIKey() {
			projection, exists := finalMultipliers[account.ID]
			if !exists {
				projection.Status = "unavailable"
			}
			item.UpstreamFinalMultiplier = &projection
			if ids := logicalGroupIDs[account.ID]; len(ids) > 0 {
				item.LogicalGroupIDs = append([]int64(nil), ids...)
			}
			if protections := groupProtections[account.ID]; len(protections) > 0 {
				item.GroupProtections = protections
			}
		}
		if config, ok := configByID[account.ID]; ok {
			configCopy := config.PublicView()
			item.Config = &configCopy
		}
		overviewAccounts = append(overviewAccounts, item)
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"groups":                    groups,
		"accounts":                  overviewAccounts,
		"group_protection_defaults": groupProtectionDefaults,
		"group_balance_thresholds":  groupBalanceThresholds,
	})
}

func (s *Server) handleGetGroupProtectionDefault(w http.ResponseWriter, r *http.Request) {
	manager, ok := s.upstreams.(groupProtectionDefaultConsole)
	if !ok {
		writeError(w, http.StatusServiceUnavailable, "PROTECTION_UNAVAILABLE", "分组保护功能暂不可用")
		return
	}
	groupID, err := pathPositiveID(r, "groupID")
	if err != nil {
		writeError(w, http.StatusBadRequest, "INVALID_GROUP", err.Error())
		return
	}
	setting, err := manager.GetGroupProtectionDefault(groupID)
	if errors.Is(err, store.ErrGroupProtectionDefaultNotFound) {
		writeJSON(w, http.StatusOK, map[string]any{"group_id": groupID, "configured": false})
		return
	}
	if err != nil {
		writeError(w, http.StatusBadGateway, "PROTECTION_READ_FAILED", err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"configured": true, "protection": setting})
}

func (s *Server) handleSetGroupProtectionDefault(w http.ResponseWriter, r *http.Request) {
	manager, ok := s.upstreams.(groupProtectionDefaultConsole)
	if !ok {
		writeError(w, http.StatusServiceUnavailable, "PROTECTION_UNAVAILABLE", "分组保护功能暂不可用")
		return
	}
	groupID, err := pathPositiveID(r, "groupID")
	if err != nil {
		writeError(w, http.StatusBadRequest, "INVALID_GROUP", err.Error())
		return
	}
	if s.console != nil {
		groups, listErr := s.console.ListGroups(r.Context())
		if listErr != nil {
			writeError(w, http.StatusBadGateway, "GROUPS_UNAVAILABLE", listErr.Error())
			return
		}
		if _, found := findGroup(groups, groupID); !found {
			writeError(w, http.StatusNotFound, "GROUP_NOT_FOUND", "分组不存在")
			return
		}
	}
	request, err := decodeProtectionRequest(r)
	if err != nil {
		writeError(w, http.StatusBadRequest, "INVALID_PROTECTION", err.Error())
		return
	}
	setting, err := manager.SetGroupProtectionDefault(r.Context(), groupID, *request.ProtectionMultiplier)
	if err != nil {
		writeError(w, http.StatusBadRequest, "INVALID_PROTECTION", err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"protection": setting})
}

func (s *Server) handleDeleteGroupProtectionDefault(w http.ResponseWriter, r *http.Request) {
	manager, ok := s.upstreams.(groupProtectionDefaultConsole)
	if !ok {
		writeError(w, http.StatusServiceUnavailable, "PROTECTION_UNAVAILABLE", "分组保护功能暂不可用")
		return
	}
	groupID, err := pathPositiveID(r, "groupID")
	if err != nil {
		writeError(w, http.StatusBadRequest, "INVALID_GROUP", err.Error())
		return
	}
	if err := manager.DeleteGroupProtectionDefault(r.Context(), groupID); err != nil {
		writeError(w, http.StatusBadGateway, "PROTECTION_DELETE_FAILED", err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"group_id": groupID, "deleted": true})
}

func projectAdminBalance(account model.UpstreamAccount) overviewAdminBalance {
	managed := account.HasManagedUpstreamBalanceQuota()
	projection := overviewAdminBalance{
		Managed:   managed,
		Unlimited: managed && account.ManagedUpstreamBalanceQuotaUnlimited(),
	}
	if projection.Managed {
		projection.Configured = true
		projection.Insufficient = account.ManagedUpstreamBalanceQuotaExhausted()
		if remaining, ok := account.ManagedUpstreamBalanceQuotaRemaining(); ok {
			value := *remaining
			projection.Remaining = &value
		}
	}

	dimensions := []struct {
		name  string
		used  *float64
		limit *float64
	}{
		{name: "daily", used: account.QuotaDailyUsed, limit: account.QuotaDailyLimit},
		{name: "weekly", used: account.QuotaWeeklyUsed, limit: account.QuotaWeeklyLimit},
		{name: "total", used: account.QuotaUsed, limit: account.QuotaLimit},
	}
	for _, dimension := range dimensions {
		limit, configured := finitePositiveQuota(dimension.limit)
		if !configured {
			continue
		}
		projection.Configured = true
		used := finiteQuotaUsed(dimension.used)
		if used >= limit {
			projection.Insufficient = true
			projection.ExhaustedDimensions = append(projection.ExhaustedDimensions, dimension.name)
		}
		if dimension.name == "total" && !projection.Unlimited {
			remaining := math.Max(limit-used, 0)
			projection.Remaining = &remaining
		}
	}
	if projection.Managed && projection.Insufficient && !containsString(projection.ExhaustedDimensions, "total") {
		projection.ExhaustedDimensions = append(projection.ExhaustedDimensions, "total")
	}
	return projection
}

func finitePositiveQuota(value *float64) (float64, bool) {
	if value == nil || *value <= 0 || math.IsNaN(*value) || math.IsInf(*value, 0) {
		return 0, false
	}
	return *value, true
}

func finiteQuotaUsed(value *float64) float64 {
	if value == nil || *value < 0 || math.IsNaN(*value) || math.IsInf(*value, 0) {
		return 0
	}
	return *value
}

func containsString(values []string, target string) bool {
	for _, value := range values {
		if value == target {
			return true
		}
	}
	return false
}

func (s *Server) handleAccountUsage(w http.ResponseWriter, r *http.Request) {
	if s.console == nil {
		writeError(w, http.StatusServiceUnavailable, "CONSOLE_UNAVAILABLE", "账号用量暂不可用")
		return
	}
	accountID, err := pathAccountID(r)
	if err != nil {
		writeError(w, http.StatusBadRequest, "INVALID_ACCOUNT", err.Error())
		return
	}
	usage, err := s.console.GetPassiveUsage(r.Context(), accountID)
	if err != nil {
		writeError(w, http.StatusBadGateway, "USAGE_UNAVAILABLE", err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"usage": usage})
}

func (s *Server) handleGroupBinding(w http.ResponseWriter, r *http.Request) {
	if s.console == nil {
		writeError(w, http.StatusServiceUnavailable, "CONSOLE_UNAVAILABLE", "账号绑定暂不可用")
		return
	}
	groupID, err := pathPositiveID(r, "groupID")
	if err != nil {
		writeError(w, http.StatusBadRequest, "INVALID_GROUP", err.Error())
		return
	}
	accountID, err := pathAccountID(r)
	if err != nil {
		writeError(w, http.StatusBadRequest, "INVALID_ACCOUNT", err.Error())
		return
	}
	request, err := decodeGroupBindingRequest(r)
	if err != nil {
		writeError(w, http.StatusBadRequest, "INVALID_REQUEST", err.Error())
		return
	}
	if manager, ok := s.upstreams.(protectedGroupBindingConsole); ok {
		if err := manager.SetGroupAccountBinding(r.Context(), groupID, accountID, *request.Bound); err != nil {
			status, code, message := classifyGroupBindingError(err)
			writeError(w, status, code, message)
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{"account_id": accountID, "group_id": groupID, "bound": *request.Bound})
		return
	}
	updated, err := s.console.SetAccountGroup(r.Context(), accountID, groupID, *request.Bound)
	if err != nil {
		status, code, message := classifyGroupBindingError(err)
		writeError(w, status, code, message)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"account": updated})
}

func (s *Server) handleSetGroupProtection(w http.ResponseWriter, r *http.Request) {
	manager, ok := s.upstreams.(protectedGroupBindingConsole)
	if !ok {
		writeError(w, http.StatusServiceUnavailable, "PROTECTION_UNAVAILABLE", "倍率保护功能暂不可用")
		return
	}
	groupID, err := pathPositiveID(r, "groupID")
	if err != nil {
		writeError(w, http.StatusBadRequest, "INVALID_GROUP", err.Error())
		return
	}
	accountID, err := pathAccountID(r)
	if err != nil {
		writeError(w, http.StatusBadRequest, "INVALID_ACCOUNT", err.Error())
		return
	}
	request, err := decodeProtectionRequest(r)
	if err != nil {
		writeError(w, http.StatusBadRequest, "INVALID_PROTECTION", err.Error())
		return
	}
	view, err := manager.SetGroupAccountProtection(r.Context(), groupID, accountID, *request.ProtectionMultiplier)
	if err != nil {
		writeError(w, http.StatusBadRequest, "INVALID_PROTECTION", err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"protection": view})
}

func (s *Server) handleReleaseGroupProtection(w http.ResponseWriter, r *http.Request) {
	manager, ok := s.upstreams.(protectedGroupBindingConsole)
	if !ok {
		writeError(w, http.StatusServiceUnavailable, "PROTECTION_UNAVAILABLE", "倍率保护功能暂不可用")
		return
	}
	groupID, err := pathPositiveID(r, "groupID")
	if err != nil {
		writeError(w, http.StatusBadRequest, "INVALID_GROUP", err.Error())
		return
	}
	accountID, err := pathAccountID(r)
	if err != nil {
		writeError(w, http.StatusBadRequest, "INVALID_ACCOUNT", err.Error())
		return
	}
	view, err := manager.ReleaseGroupAccountProtection(r.Context(), groupID, accountID)
	if err != nil {
		status, code, message := classifyGroupBindingError(err)
		writeError(w, status, code, message)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"protection": view, "released": true})
}

func (s *Server) handleRemoveGroupBinding(w http.ResponseWriter, r *http.Request) {
	manager, ok := s.upstreams.(protectedGroupBindingConsole)
	if !ok {
		writeError(w, http.StatusServiceUnavailable, "PROTECTION_UNAVAILABLE", "倍率保护功能暂不可用")
		return
	}
	groupID, err := pathPositiveID(r, "groupID")
	if err != nil {
		writeError(w, http.StatusBadRequest, "INVALID_GROUP", err.Error())
		return
	}
	accountID, err := pathAccountID(r)
	if err != nil {
		writeError(w, http.StatusBadRequest, "INVALID_ACCOUNT", err.Error())
		return
	}
	if err := manager.RemoveGroupAccountBinding(r.Context(), groupID, accountID); err != nil {
		status, code, message := classifyGroupBindingError(err)
		writeError(w, status, code, message)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"group_id": groupID, "account_id": accountID, "removed": true})
}

func (s *Server) handleBulkGroupBindings(w http.ResponseWriter, r *http.Request) {
	if s.console == nil {
		writeError(w, http.StatusServiceUnavailable, "CONSOLE_UNAVAILABLE", "账号绑定暂不可用")
		return
	}
	groupID, err := pathPositiveID(r, "groupID")
	if err != nil {
		writeError(w, http.StatusBadRequest, "INVALID_GROUP", err.Error())
		return
	}
	request, err := decodeBulkGroupBindingRequest(r)
	if err != nil {
		writeError(w, http.StatusBadRequest, "INVALID_REQUEST", err.Error())
		return
	}

	groups, err := s.console.ListGroups(r.Context())
	if err != nil {
		writeError(w, http.StatusBadGateway, "GROUPS_UNAVAILABLE", err.Error())
		return
	}
	group, ok := findGroup(groups, groupID)
	if !ok {
		writeError(w, http.StatusNotFound, "GROUP_NOT_FOUND", "分组不存在")
		return
	}
	accounts, err := s.console.ListAccounts(r.Context())
	if err != nil {
		writeError(w, http.StatusBadGateway, "ACCOUNTS_UNAVAILABLE", err.Error())
		return
	}

	accountByID := make(map[int64]model.UpstreamAccount, len(accounts))
	for _, account := range accounts {
		accountByID[account.ID] = account
	}
	if manager, ok := s.upstreams.(protectedGroupBindingConsole); ok {
		for _, accountID := range *request.AccountIDs {
			account, exists := accountByID[accountID]
			if !exists {
				writeError(w, http.StatusBadRequest, "INVALID_ACCOUNT", fmt.Sprintf("账号 %d 不存在", accountID))
				return
			}
			if !account.IsAPIKey() {
				writeError(w, http.StatusBadRequest, "INVALID_ACCOUNT_TYPE", fmt.Sprintf("账号 %s 不是 API Key 账号", accountDisplayName(account)))
				return
			}
			if !containsID(account.GroupIDs, groupID) && !accountCompatibleWithGroup(account, group) {
				writeError(w, http.StatusBadRequest, "INCOMPATIBLE_PLATFORM", fmt.Sprintf("账号 %s 的平台 %s 与分组平台 %s 不兼容", accountDisplayName(account), account.Platform, group.Platform))
				return
			}
		}
		updatedIDs, failures, err := manager.SaveGroupBindings(r.Context(), groupID, *request.AccountIDs)
		if err != nil {
			writeError(w, http.StatusBadGateway, "BINDING_FAILED", err.Error())
			return
		}
		response := bulkGroupBindingResponse{GroupID: groupID, UpdatedAccountIDs: updatedIDs, Failures: make([]groupBindingFailure, 0, len(failures))}
		for _, failure := range failures {
			response.Failures = append(response.Failures, groupBindingFailure{AccountID: failure.AccountID, Name: failure.Name, Code: failure.Code, Message: failure.Message})
		}
		status := http.StatusOK
		if len(response.Failures) > 0 {
			status = http.StatusMultiStatus
		}
		writeJSON(w, status, response)
		return
	}
	selected := make(map[int64]struct{}, len(*request.AccountIDs))
	for _, accountID := range *request.AccountIDs {
		account, exists := accountByID[accountID]
		if !exists {
			writeError(w, http.StatusBadRequest, "INVALID_ACCOUNT", fmt.Sprintf("账号 %d 不存在", accountID))
			return
		}
		if !account.IsAPIKey() {
			writeError(w, http.StatusBadRequest, "INVALID_ACCOUNT_TYPE", fmt.Sprintf("账号 %s 不是 API Key 账号", accountDisplayName(account)))
			return
		}
		if !containsID(account.GroupIDs, groupID) && !accountCompatibleWithGroup(account, group) {
			writeError(w, http.StatusBadRequest, "INCOMPATIBLE_PLATFORM", fmt.Sprintf("账号 %s 的平台 %s 与分组平台 %s 不兼容", accountDisplayName(account), account.Platform, group.Platform))
			return
		}
		selected[accountID] = struct{}{}
	}

	type bindingChange struct {
		account model.UpstreamAccount
		bound   bool
	}
	changes := make([]bindingChange, 0)
	for _, account := range accounts {
		if !account.IsAPIKey() {
			continue
		}
		currentlyBound := containsID(account.GroupIDs, groupID)
		_, shouldBeBound := selected[account.ID]
		if currentlyBound != shouldBeBound {
			changes = append(changes, bindingChange{account: account, bound: shouldBeBound})
		}
	}
	sort.Slice(changes, func(i, j int) bool { return changes[i].account.ID < changes[j].account.ID })

	response := bulkGroupBindingResponse{
		GroupID:           groupID,
		UpdatedAccountIDs: make([]int64, 0, len(changes)),
		Failures:          make([]groupBindingFailure, 0),
	}
	for _, change := range changes {
		if _, err := s.console.SetAccountGroup(r.Context(), change.account.ID, groupID, change.bound); err != nil {
			_, code, message := classifyGroupBindingError(err)
			response.Failures = append(response.Failures, groupBindingFailure{
				AccountID: change.account.ID,
				Name:      accountDisplayName(change.account),
				Code:      code,
				Message:   message,
			})
			continue
		}
		response.UpdatedAccountIDs = append(response.UpdatedAccountIDs, change.account.ID)
	}
	status := http.StatusOK
	if len(response.Failures) > 0 {
		status = http.StatusMultiStatus
	}
	writeJSON(w, status, response)
}

func classifyGroupBindingError(err error) (int, string, string) {
	var upstreamErr *core.HTTPError
	if !errors.As(err, &upstreamErr) {
		if strings.Contains(err.Error(), "保护倍率") {
			return http.StatusConflict, "RATE_PROTECTED", err.Error()
		}
		return http.StatusBadGateway, "BINDING_FAILED", err.Error()
	}
	switch upstreamErr.StatusCode {
	case http.StatusBadRequest:
		return http.StatusBadRequest, "BINDING_REJECTED", upstreamErr.Message
	case http.StatusNotFound:
		return http.StatusNotFound, "NOT_FOUND", upstreamErr.Message
	case http.StatusConflict:
		return http.StatusConflict, "MIXED_CHANNEL_RISK", upstreamErr.Message
	default:
		return http.StatusBadGateway, "BINDING_FAILED", upstreamErr.Message
	}
}

func findGroup(groups []model.UpstreamGroup, groupID int64) (model.UpstreamGroup, bool) {
	for _, group := range groups {
		if group.ID == groupID {
			return group, true
		}
	}
	return model.UpstreamGroup{}, false
}

func accountCompatibleWithGroup(account model.UpstreamAccount, group model.UpstreamGroup) bool {
	accountPlatform := strings.ToLower(strings.TrimSpace(account.Platform))
	groupPlatform := strings.ToLower(strings.TrimSpace(group.Platform))
	if accountPlatform == "" || groupPlatform == "" {
		return false
	}
	if groupPlatform == "composite" || accountPlatform == groupPlatform {
		return true
	}
	return accountPlatform == "antigravity" && account.MixedScheduling && (groupPlatform == "anthropic" || groupPlatform == "gemini")
}

func containsID(ids []int64, target int64) bool {
	for _, id := range ids {
		if id == target {
			return true
		}
	}
	return false
}

func accountDisplayName(account model.UpstreamAccount) string {
	if name := strings.TrimSpace(account.Name); name != "" {
		return name
	}
	return strconv.FormatInt(account.ID, 10)
}

func (s *Server) handleListConfigs(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, http.StatusOK, map[string]any{"configs": s.engine.ListViews()})
}

func (s *Server) handleCreateConfig(w http.ResponseWriter, r *http.Request) {
	request, err := decodePolicyRequest(r)
	if err != nil {
		writeError(w, http.StatusBadRequest, "INVALID_REQUEST", err.Error())
		return
	}
	if request.AccountID <= 0 {
		writeError(w, http.StatusBadRequest, "INVALID_ACCOUNT", "请选择 API Key 账号")
		return
	}
	policy := mergePolicy(model.DefaultPolicy(), request)
	created, err := s.engine.Upsert(r.Context(), request.AccountID, policy)
	if err != nil {
		writePolicyError(w, err)
		return
	}
	writeJSON(w, http.StatusCreated, created.PublicView())
}

func (s *Server) handleUpdateConfig(w http.ResponseWriter, r *http.Request) {
	accountID, err := pathAccountID(r)
	if err != nil {
		writeError(w, http.StatusBadRequest, "INVALID_ACCOUNT", err.Error())
		return
	}
	existing, found := findConfig(s.engine.List(), accountID)
	if !found {
		writeError(w, http.StatusNotFound, "NOT_FOUND", "账号检测配置不存在")
		return
	}
	request, err := decodePolicyRequest(r)
	if err != nil {
		writeError(w, http.StatusBadRequest, "INVALID_REQUEST", err.Error())
		return
	}
	policy := mergePolicy(existing.Policy, request)
	updated, err := s.engine.Upsert(r.Context(), accountID, policy)
	if err != nil {
		writePolicyError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, updated.PublicView())
}

func (s *Server) handleDeleteConfig(w http.ResponseWriter, r *http.Request) {
	accountID, err := pathAccountID(r)
	if err != nil {
		writeError(w, http.StatusBadRequest, "INVALID_ACCOUNT", err.Error())
		return
	}
	if err := s.engine.Delete(r.Context(), accountID); err != nil {
		switch {
		case errors.Is(err, store.ErrNotFound):
			writeError(w, http.StatusNotFound, "NOT_FOUND", "账号检测配置不存在")
		case errors.Is(err, engine.ErrAlreadyRunning):
			writeError(w, http.StatusConflict, "CHECK_RUNNING", "检测进行中，请稍后再删除")
		default:
			writeError(w, http.StatusBadGateway, "DELETE_FAILED", err.Error())
		}
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (s *Server) handleRunNow(w http.ResponseWriter, r *http.Request) {
	accountID, err := pathAccountID(r)
	if err != nil {
		writeError(w, http.StatusBadRequest, "INVALID_ACCOUNT", err.Error())
		return
	}
	if err := s.engine.Trigger(accountID); err != nil {
		switch {
		case errors.Is(err, store.ErrNotFound):
			writeError(w, http.StatusNotFound, "NOT_FOUND", "账号检测配置不存在")
		case errors.Is(err, engine.ErrAlreadyRunning):
			writeError(w, http.StatusConflict, "CHECK_RUNNING", "该账号正在检测")
		default:
			writeError(w, http.StatusServiceUnavailable, "RUN_FAILED", err.Error())
		}
		return
	}
	writeJSON(w, http.StatusAccepted, map[string]string{"status": "running"})
}

func (s *Server) handleDirectProbeStatus(w http.ResponseWriter, r *http.Request) {
	accountID, err := pathAccountID(r)
	if err != nil {
		writeError(w, http.StatusBadRequest, "INVALID_ACCOUNT", err.Error())
		return
	}
	config, found := findConfig(s.engine.List(), accountID)
	if !found {
		writeError(w, http.StatusNotFound, "NOT_FOUND", "账号检测配置不存在")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"probe": config.PublicView().Probe})
}

func (s *Server) handleAuthorizeDirectProbe(w http.ResponseWriter, r *http.Request) {
	accountID, err := pathAccountID(r)
	if err != nil {
		writeError(w, http.StatusBadRequest, "INVALID_ACCOUNT", err.Error())
		return
	}
	updated, err := s.authorizeDirectProbe(r, accountID)
	if err != nil {
		writeDirectProbeError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, updated.PublicView())
}

func (s *Server) handleBatchAuthorizeDirectProbes(w http.ResponseWriter, r *http.Request) {
	request, err := decodeDirectProbeBatchRequest(r)
	if err != nil {
		writeError(w, http.StatusBadRequest, "INVALID_REQUEST", err.Error())
		return
	}
	response := directProbeBatchResponse{
		Authorized: make([]model.ManagedAccountView, 0, len(*request.AccountIDs)),
		Failures:   make([]directProbeBatchFailure, 0),
	}
	for _, accountID := range *request.AccountIDs {
		updated, authorizeErr := s.authorizeDirectProbe(r, accountID)
		if authorizeErr != nil {
			response.Failures = append(response.Failures, directProbeBatchFailure{
				AccountID: accountID,
				Code:      directProbeErrorCode(authorizeErr),
				Message:   directProbeErrorMessage(authorizeErr),
			})
			continue
		}
		response.Authorized = append(response.Authorized, updated.PublicView())
	}
	status := http.StatusOK
	if len(response.Failures) > 0 {
		status = http.StatusMultiStatus
	}
	writeJSON(w, status, response)
}

func (s *Server) handleRevokeDirectProbe(w http.ResponseWriter, r *http.Request) {
	accountID, err := pathAccountID(r)
	if err != nil {
		writeError(w, http.StatusBadRequest, "INVALID_ACCOUNT", err.Error())
		return
	}
	updated, err := s.engine.RevokeDirect(r.Context(), accountID)
	if err != nil {
		if errors.Is(err, store.ErrNotFound) {
			writeError(w, http.StatusNotFound, "NOT_FOUND", "账号检测配置不存在")
			return
		}
		writeError(w, http.StatusBadGateway, "DIRECT_PROBE_REVOKE_FAILED", "撤销直连探测授权失败")
		return
	}
	writeJSON(w, http.StatusOK, updated.PublicView())
}

func (s *Server) authorizeDirectProbe(r *http.Request, accountID int64) (model.ManagedAccount, error) {
	if _, found := findConfig(s.engine.List(), accountID); !found {
		return model.ManagedAccount{}, store.ErrNotFound
	}
	token, _ := r.Context().Value(adminTokenKey).(string)
	identity, _ := r.Context().Value(forwardedIdentityKey).(core.ForwardedIdentity)
	return s.engine.AuthorizeDirect(r.Context(), accountID, token, identity)
}

func (s *Server) handleRegisterTab(w http.ResponseWriter, r *http.Request) {
	if err := s.RegisterTab(r.Context()); err != nil {
		writeError(w, http.StatusBadGateway, "REGISTER_FAILED", err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"status": "registered"})
}

func (s *Server) requireAdmin(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		token := bearerToken(r.Header.Get("Authorization"))
		if token == "" {
			writeError(w, http.StatusUnauthorized, "UNAUTHORIZED", "需要管理员登录")
			return
		}
		identity := core.ForwardedIdentity{
			ClientIP:  s.clientIP(r),
			UserAgent: r.UserAgent(),
		}
		cacheKey := sessionCacheKey(token, identity)
		if user, ok := s.cachedUser(cacheKey); ok {
			next.ServeHTTP(w, r.WithContext(withAdminRequestContext(r.Context(), user, token, identity)))
			return
		}

		user, err := s.core.ValidateAdminJWT(r.Context(), token, identity)
		if err != nil || user.Role != "admin" {
			s.logger.Warn("reject embedded admin session", "client_ip", identity.ClientIP, "error", err)
			writeError(w, http.StatusForbidden, "FORBIDDEN", "管理员身份验证失败")
			return
		}
		s.cacheUser(cacheKey, user)
		next.ServeHTTP(w, r.WithContext(withAdminRequestContext(r.Context(), user, token, identity)))
	})
}

func withAdminRequestContext(ctx context.Context, user core.AdminUser, token string, identity core.ForwardedIdentity) context.Context {
	ctx = context.WithValue(ctx, adminUserKey, user)
	ctx = context.WithValue(ctx, adminTokenKey, token)
	return context.WithValue(ctx, forwardedIdentityKey, identity)
}

func (s *Server) staticHandler() http.Handler {
	root, err := fs.Sub(staticFiles, "static")
	if err != nil {
		panic(err)
	}
	fileServer := http.FileServer(http.FS(root))
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet && r.Method != http.MethodHead {
			http.NotFound(w, r)
			return
		}
		if r.URL.Path == "/" {
			w.Header().Set("Cache-Control", "no-store")
		}
		fileServer.ServeHTTP(w, r)
	})
}

func (s *Server) securityHeaders(next http.Handler) http.Handler {
	frameAncestors := "'self'"
	if origin := strings.TrimSpace(s.options.UIOrigin); origin != "" {
		frameAncestors += " " + origin
	}
	csp := "default-src 'self'; base-uri 'none'; object-src 'none'; " +
		"script-src 'self'; style-src 'self'; img-src 'self' data:; connect-src 'self'; " +
		"form-action 'self'; frame-ancestors " + frameAncestors
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Security-Policy", csp)
		w.Header().Set("Referrer-Policy", "no-referrer")
		w.Header().Set("X-Content-Type-Options", "nosniff")
		w.Header().Set("Permissions-Policy", "camera=(), microphone=(), geolocation=()")
		if strings.HasPrefix(r.URL.Path, "/api/") {
			w.Header().Set("Cache-Control", "no-store")
		}
		next.ServeHTTP(w, r)
	})
}

func (s *Server) clientIP(r *http.Request) string {
	if s.options.TrustProxyHeaders {
		for _, candidate := range strings.Split(r.Header.Get("X-Forwarded-For"), ",") {
			candidate = strings.TrimSpace(candidate)
			if net.ParseIP(candidate) != nil {
				return candidate
			}
		}
		if candidate := strings.TrimSpace(r.Header.Get("X-Real-IP")); net.ParseIP(candidate) != nil {
			return candidate
		}
	}
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err == nil && net.ParseIP(host) != nil {
		return host
	}
	if net.ParseIP(r.RemoteAddr) != nil {
		return r.RemoteAddr
	}
	return ""
}

func (s *Server) cachedUser(key string) (core.AdminUser, bool) {
	if s.options.AuthCacheTTL <= 0 {
		return core.AdminUser{}, false
	}
	s.cacheMu.Lock()
	defer s.cacheMu.Unlock()
	entry, ok := s.authCache[key]
	if !ok || time.Now().After(entry.ExpiresAt) {
		delete(s.authCache, key)
		return core.AdminUser{}, false
	}
	return entry.User, true
}

func (s *Server) cacheUser(key string, user core.AdminUser) {
	if s.options.AuthCacheTTL <= 0 {
		return
	}
	s.cacheMu.Lock()
	defer s.cacheMu.Unlock()
	if len(s.authCache) > 1000 {
		now := time.Now()
		for cacheKey, entry := range s.authCache {
			if now.After(entry.ExpiresAt) {
				delete(s.authCache, cacheKey)
			}
		}
	}
	s.authCache[key] = cachedSession{User: user, ExpiresAt: time.Now().Add(s.options.AuthCacheTTL)}
}

func decodePolicyRequest(r *http.Request) (policyRequest, error) {
	defer func() { _ = r.Body.Close() }()
	decoder := json.NewDecoder(io.LimitReader(r.Body, (64<<10)+1))
	decoder.DisallowUnknownFields()
	var request policyRequest
	if err := decoder.Decode(&request); err != nil {
		return policyRequest{}, fmt.Errorf("无效的配置: %w", err)
	}
	var extra any
	if err := decoder.Decode(&extra); !errors.Is(err, io.EOF) {
		return policyRequest{}, errors.New("无效的配置: 请求只能包含一个 JSON 对象")
	}
	return request, nil
}

func decodeGroupBindingRequest(r *http.Request) (groupBindingRequest, error) {
	defer func() { _ = r.Body.Close() }()
	decoder := json.NewDecoder(io.LimitReader(r.Body, (8<<10)+1))
	decoder.DisallowUnknownFields()
	var request groupBindingRequest
	if err := decoder.Decode(&request); err != nil {
		return groupBindingRequest{}, fmt.Errorf("无效的绑定请求: %w", err)
	}
	var extra any
	if err := decoder.Decode(&extra); !errors.Is(err, io.EOF) {
		return groupBindingRequest{}, errors.New("无效的绑定请求: 请求只能包含一个 JSON 对象")
	}
	if request.Bound == nil {
		return groupBindingRequest{}, errors.New("必须提供 bound")
	}
	return request, nil
}

func decodeProtectionRequest(r *http.Request) (protectionRequest, error) {
	defer func() { _ = r.Body.Close() }()
	decoder := json.NewDecoder(io.LimitReader(r.Body, (8<<10)+1))
	decoder.DisallowUnknownFields()
	var request protectionRequest
	if err := decoder.Decode(&request); err != nil {
		return protectionRequest{}, fmt.Errorf("无效的保护倍率: %w", err)
	}
	var extra any
	if err := decoder.Decode(&extra); !errors.Is(err, io.EOF) {
		return protectionRequest{}, errors.New("无效的保护倍率: 请求只能包含一个 JSON 对象")
	}
	if request.ProtectionMultiplier == nil || *request.ProtectionMultiplier < 0 || math.IsNaN(*request.ProtectionMultiplier) || math.IsInf(*request.ProtectionMultiplier, 0) {
		return protectionRequest{}, errors.New("保护倍率必须是大于等于 0 的有限数值")
	}
	return request, nil
}

func decodeBulkGroupBindingRequest(r *http.Request) (bulkGroupBindingRequest, error) {
	defer func() { _ = r.Body.Close() }()
	decoder := json.NewDecoder(io.LimitReader(r.Body, (256<<10)+1))
	decoder.DisallowUnknownFields()
	var request bulkGroupBindingRequest
	if err := decoder.Decode(&request); err != nil {
		return bulkGroupBindingRequest{}, fmt.Errorf("无效的批量绑定请求: %w", err)
	}
	var extra any
	if err := decoder.Decode(&extra); !errors.Is(err, io.EOF) {
		return bulkGroupBindingRequest{}, errors.New("无效的批量绑定请求: 请求只能包含一个 JSON 对象")
	}
	if request.AccountIDs == nil {
		return bulkGroupBindingRequest{}, errors.New("必须提供 account_ids")
	}
	if len(*request.AccountIDs) > 10000 {
		return bulkGroupBindingRequest{}, errors.New("account_ids 不能超过 10000 个")
	}
	seen := make(map[int64]struct{}, len(*request.AccountIDs))
	for _, accountID := range *request.AccountIDs {
		if accountID <= 0 {
			return bulkGroupBindingRequest{}, errors.New("account_ids 只能包含正整数")
		}
		if _, exists := seen[accountID]; exists {
			return bulkGroupBindingRequest{}, fmt.Errorf("account_ids 包含重复账号 %d", accountID)
		}
		seen[accountID] = struct{}{}
	}
	sort.Slice(*request.AccountIDs, func(i, j int) bool { return (*request.AccountIDs)[i] < (*request.AccountIDs)[j] })
	return request, nil
}

func decodeDirectProbeBatchRequest(r *http.Request) (directProbeBatchRequest, error) {
	defer func() { _ = r.Body.Close() }()
	decoder := json.NewDecoder(io.LimitReader(r.Body, (64<<10)+1))
	decoder.DisallowUnknownFields()
	var request directProbeBatchRequest
	if err := decoder.Decode(&request); err != nil {
		return directProbeBatchRequest{}, fmt.Errorf("无效的直连探测授权请求: %w", err)
	}
	var extra any
	if err := decoder.Decode(&extra); !errors.Is(err, io.EOF) {
		return directProbeBatchRequest{}, errors.New("无效的直连探测授权请求: 请求只能包含一个 JSON 对象")
	}
	if request.AccountIDs == nil || len(*request.AccountIDs) == 0 {
		return directProbeBatchRequest{}, errors.New("必须提供至少一个 account_ids")
	}
	if len(*request.AccountIDs) > 100 {
		return directProbeBatchRequest{}, errors.New("account_ids 不能超过 100 个")
	}
	seen := make(map[int64]struct{}, len(*request.AccountIDs))
	for _, accountID := range *request.AccountIDs {
		if accountID <= 0 {
			return directProbeBatchRequest{}, errors.New("account_ids 只能包含正整数")
		}
		if _, exists := seen[accountID]; exists {
			return directProbeBatchRequest{}, fmt.Errorf("account_ids 包含重复账号 %d", accountID)
		}
		seen[accountID] = struct{}{}
	}
	sort.Slice(*request.AccountIDs, func(i, j int) bool { return (*request.AccountIDs)[i] < (*request.AccountIDs)[j] })
	return request, nil
}

func mergePolicy(base model.Policy, request policyRequest) model.Policy {
	if request.Enabled != nil {
		base.Enabled = *request.Enabled
	}
	if request.IntervalSeconds != nil {
		base.IntervalSeconds = *request.IntervalSeconds
	}
	if request.Model != nil {
		base.Model = *request.Model
	}
	if request.Prompt != nil {
		base.Prompt = *request.Prompt
	}
	if request.LatencyLimitMS != nil {
		base.LatencyLimitMS = *request.LatencyLimitMS
	}
	if request.FailureThreshold != nil {
		base.FailureThreshold = *request.FailureThreshold
	}
	if request.RecoveryThreshold != nil {
		base.RecoveryThreshold = *request.RecoveryThreshold
	}
	return base
}

func pathAccountID(r *http.Request) (int64, error) {
	accountID, err := pathPositiveID(r, "accountID")
	if err != nil {
		return 0, errors.New("无效的账号 ID")
	}
	return accountID, nil
}

func pathPositiveID(r *http.Request, name string) (int64, error) {
	value, err := strconv.ParseInt(r.PathValue(name), 10, 64)
	if err != nil || value <= 0 {
		return 0, errors.New("无效的 ID")
	}
	return value, nil
}

func findConfig(configs []model.ManagedAccount, accountID int64) (model.ManagedAccount, bool) {
	for _, config := range configs {
		if config.AccountID == accountID {
			return config, true
		}
	}
	return model.ManagedAccount{}, false
}

func bearerToken(header string) string {
	parts := strings.SplitN(strings.TrimSpace(header), " ", 2)
	if len(parts) != 2 || !strings.EqualFold(parts[0], "Bearer") {
		return ""
	}
	return strings.TrimSpace(parts[1])
}

func sessionCacheKey(token string, identity core.ForwardedIdentity) string {
	digest := sha256.Sum256([]byte(token + "\x00" + identity.ClientIP + "\x00" + identity.UserAgent))
	return hex.EncodeToString(digest[:])
}

func writePolicyError(w http.ResponseWriter, err error) {
	message := err.Error()
	if strings.Contains(message, "only API Key accounts") {
		writeError(w, http.StatusBadRequest, "UNSUPPORTED_ACCOUNT", "仅支持 API Key 添加的账号")
		return
	}
	if strings.Contains(message, "恢复账号调度失败") {
		writeError(w, http.StatusBadGateway, "SCHEDULING_UPDATE_FAILED", message)
		return
	}
	writeError(w, http.StatusBadRequest, "INVALID_POLICY", message)
}

func writeDirectProbeError(w http.ResponseWriter, err error) {
	if errors.Is(err, store.ErrNotFound) {
		writeError(w, http.StatusNotFound, "NOT_FOUND", "账号检测配置不存在")
		return
	}
	var upstreamErr *core.HTTPError
	if errors.As(err, &upstreamErr) {
		status := upstreamErr.StatusCode
		if status == http.StatusUnauthorized || status == http.StatusForbidden {
			writeError(w, status, firstNonEmptyCode(upstreamErr.Code, "STEP_UP_REQUIRED"), directProbeErrorMessage(err))
			return
		}
	}
	code := directProbeErrorCode(err)
	status := http.StatusBadRequest
	if code == "DIRECT_PROBE_CREDENTIALS_DISABLED" || code == "DIRECT_PROBE_IMPORT_UNAVAILABLE" {
		status = http.StatusServiceUnavailable
	}
	writeError(w, status, code, directProbeErrorMessage(err))
}

func directProbeErrorCode(err error) string {
	var upstreamErr *core.HTTPError
	if errors.As(err, &upstreamErr) && strings.TrimSpace(upstreamErr.Code) != "" {
		return upstreamErr.Code
	}
	message := strings.ToLower(err.Error())
	switch {
	case strings.Contains(message, "credential") || strings.Contains(message, "加密未配置"):
		return "DIRECT_PROBE_CREDENTIALS_DISABLED"
	case strings.Contains(message, "未启用直连探测授权导入"):
		return "DIRECT_PROBE_IMPORT_UNAVAILABLE"
	case strings.Contains(message, "only api key"):
		return "UNSUPPORTED_ACCOUNT"
	case strings.Contains(message, "暂不支持"):
		return "UNSUPPORTED_PLATFORM"
	default:
		return "DIRECT_PROBE_AUTHORIZATION_FAILED"
	}
}

func directProbeErrorMessage(err error) string {
	var upstreamErr *core.HTTPError
	if errors.As(err, &upstreamErr) && (upstreamErr.StatusCode == http.StatusUnauthorized || upstreamErr.StatusCode == http.StatusForbidden) {
		if message := strings.TrimSpace(upstreamErr.Message); message != "" {
			return message
		}
		return "请先在 Sub2API 完成管理员二次验证后再授权"
	}
	message := strings.TrimSpace(err.Error())
	if message == "" {
		return "直连探测授权失败"
	}
	return message
}

func firstNonEmptyCode(value, fallback string) string {
	if strings.TrimSpace(value) != "" {
		return value
	}
	return fallback
}

func writeError(w http.ResponseWriter, status int, code, message string) {
	writeJSON(w, status, map[string]string{"code": code, "message": message})
}

func writeJSON(w http.ResponseWriter, status int, value any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(value)
}
