package notify

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"math"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/Wei-Shaw/sub2api-account-auto-scheduler/internal/model"
	"github.com/Wei-Shaw/sub2api-account-auto-scheduler/internal/store"
	"github.com/Wei-Shaw/sub2api-account-auto-scheduler/internal/upstream"
)

const (
	credentialDomain = "bark-notification/v1"
	defaultInterval  = 30 * time.Second
	maxInterval      = 24 * time.Hour
)

type Console interface {
	ListAccounts(ctx context.Context) ([]model.UpstreamAccount, error)
	ListGroups(ctx context.Context) ([]model.UpstreamGroup, error)
}

type ProjectionSource interface {
	LocalAccountAvailableBalances(accounts []model.UpstreamAccount) map[int64]upstream.LocalAccountAvailableBalance
	LocalAccountFinalMultipliers(accounts []model.UpstreamAccount) map[int64]upstream.LocalAccountFinalMultiplier
	GroupAccountProtectionViews(accounts []model.UpstreamAccount) map[int64]map[string]upstream.GroupAccountProtectionView
	LogicalGroupIDs(accounts []model.UpstreamAccount) map[int64][]int64
}

type SettingsInput struct {
	Enabled           bool
	BarkEndpoint      string
	BarkBasicAuthUser string
	DeviceKey         string
	EncryptionKey     string
	BasicAuthPassword string
}

type SettingsView struct {
	Enabled             bool       `json:"enabled"`
	Configured          bool       `json:"configured"`
	BarkEndpoint        string     `json:"bark_endpoint,omitempty"`
	BarkBasicAuthUser   string     `json:"bark_basic_auth_user,omitempty"`
	HasDeviceKey        bool       `json:"has_device_key"`
	HasEncryptionKey    bool       `json:"has_encryption_key"`
	EncryptionAlgorithm string     `json:"encryption_algorithm"`
	LastDeliveryAt      *time.Time `json:"last_delivery_at,omitempty"`
	LastDeliveryError   string     `json:"last_delivery_error,omitempty"`
	UpdatedAt           time.Time  `json:"updated_at"`
}

type Coordinator struct {
	store       store.NotificationStore
	console     Console
	projections ProjectionSource
	box         *upstream.CredentialBox
	bark        *BarkClient
	logger      *slog.Logger
	interval    time.Duration

	mu     sync.Mutex
	ctx    context.Context
	cancel context.CancelFunc
	wg     sync.WaitGroup
	now    func() time.Time
}

func NewCoordinator(stateStore store.NotificationStore, console Console, projections ProjectionSource, box *upstream.CredentialBox, interval time.Duration, logger *slog.Logger) *Coordinator {
	if logger == nil {
		logger = slog.Default()
	}
	if interval <= 0 {
		interval = defaultInterval
	}
	if interval > maxInterval {
		interval = maxInterval
	}
	if box == nil {
		box = &upstream.CredentialBox{}
	}
	return &Coordinator{
		store:       stateStore,
		console:     console,
		projections: projections,
		box:         box,
		bark:        NewBarkClient(nil),
		logger:      logger,
		interval:    interval,
		now:         time.Now,
	}
}

func (c *Coordinator) Start(parent context.Context) {
	if c == nil || c.store == nil || c.console == nil {
		return
	}
	c.mu.Lock()
	if c.cancel != nil {
		c.mu.Unlock()
		return
	}
	c.ctx, c.cancel = context.WithCancel(parent)
	ctx := c.ctx
	c.mu.Unlock()

	c.wg.Add(1)
	go func() {
		defer c.wg.Done()
		c.Evaluate(ctx)
		ticker := time.NewTicker(c.interval)
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				c.Evaluate(ctx)
			}
		}
	}()
}

func (c *Coordinator) Stop() {
	if c == nil {
		return
	}
	c.mu.Lock()
	if c.cancel != nil {
		c.cancel()
	}
	c.mu.Unlock()
	c.wg.Wait()
}

func (c *Coordinator) Settings() SettingsView {
	if c == nil || c.store == nil {
		return SettingsView{EncryptionAlgorithm: "AES-128-CBC"}
	}
	return settingsView(c.store.GetNotificationSettings())
}

func (c *Coordinator) SaveSettings(input SettingsInput) (SettingsView, error) {
	if c == nil || c.store == nil {
		return SettingsView{}, errors.New("通知服务不可用")
	}
	if c.box == nil || !c.box.Enabled() {
		return SettingsView{}, errors.New("请先配置 AUTO_SCHEDULER_CREDENTIAL_KEY，才能保存 Bark 凭证")
	}
	endpoint, err := normalizeBarkEndpoint(input.BarkEndpoint)
	if err != nil {
		return SettingsView{}, err
	}
	current := c.store.GetNotificationSettings()
	secrets, _ := c.decryptSecrets(current)
	if strings.TrimSpace(input.DeviceKey) != "" {
		secrets.DeviceKey = strings.TrimSpace(input.DeviceKey)
	}
	if strings.TrimSpace(input.EncryptionKey) != "" {
		secrets.EncryptionKey = input.EncryptionKey
	}
	if strings.TrimSpace(input.BasicAuthPassword) != "" {
		secrets.BasicAuthPassword = input.BasicAuthPassword
	}
	if strings.TrimSpace(secrets.DeviceKey) == "" {
		return SettingsView{}, errors.New("Bark 设备 Key 不能为空")
	}
	if len([]byte(secrets.EncryptionKey)) != 16 {
		return SettingsView{}, errors.New("Bark 加密 Key 必须是 16 字节（AES-128）")
	}
	encoded, err := json.Marshal(secrets)
	if err != nil {
		return SettingsView{}, fmt.Errorf("编码 Bark 凭证失败: %w", err)
	}
	envelope, err := c.box.EncryptBytes(credentialDomain, encoded)
	if err != nil {
		return SettingsView{}, err
	}
	updated := model.NotificationSettings{
		Enabled:           input.Enabled,
		BarkEndpoint:      endpoint,
		BarkBasicAuthUser: strings.TrimSpace(input.BarkBasicAuthUser),
		BarkCredentials:   envelope,
		LastDeliveryAt:    current.LastDeliveryAt,
		LastDeliveryError: current.LastDeliveryError,
		UpdatedAt:         c.now().UTC(),
	}
	if err := c.store.PutNotificationSettings(updated); err != nil {
		return SettingsView{}, err
	}
	return settingsView(updated), nil
}

func (c *Coordinator) ClearSettings() error {
	if c == nil || c.store == nil {
		return errors.New("通知服务不可用")
	}
	return c.store.PutNotificationSettings(model.NotificationSettings{UpdatedAt: c.now().UTC()})
}

func (c *Coordinator) GroupBalanceThreshold(groupID int64) (float64, bool) {
	if c == nil || c.store == nil {
		return 0, false
	}
	return c.store.GetGroupBalanceThreshold(groupID)
}

func (c *Coordinator) GroupBalanceThresholds() map[int64]float64 {
	if c == nil || c.store == nil {
		return map[int64]float64{}
	}
	return c.store.ListGroupBalanceThresholds()
}

func (c *Coordinator) SetGroupBalanceThreshold(groupID int64, threshold *float64) error {
	if c == nil || c.store == nil {
		return errors.New("通知服务不可用")
	}
	return c.store.PutGroupBalanceThreshold(groupID, threshold)
}

func (c *Coordinator) SetAccountBalanceThreshold(accountID int64, threshold *float64) error {
	if c == nil || c.store == nil {
		return errors.New("通知服务不可用")
	}
	accountStore, ok := c.store.(interface {
		Update(accountID int64, change func(*model.ManagedAccount) error) error
	})
	if !ok {
		return errors.New("账号阈值存储不可用")
	}
	if _, err := model.NormalizeBalanceAlertThreshold(threshold); err != nil {
		return err
	}
	return accountStore.Update(accountID, func(account *model.ManagedAccount) error {
		account.BalanceAlertThreshold = cloneFloat(threshold)
		account.UpdatedAt = c.now().UTC()
		return nil
	})
}

func (c *Coordinator) Test(ctx context.Context) error {
	settings := c.store.GetNotificationSettings()
	if !settings.Enabled || !configured(settings) {
		return errors.New("请先保存并启用 Bark 通知配置")
	}
	secrets, err := c.decryptSecrets(settings)
	if err != nil {
		return err
	}
	err = c.bark.Send(ctx, settings, secrets, "Sub2API 自动调度测试", "Bark 加密推送配置测试成功")
	c.recordDelivery(settings, err)
	return err
}

func (c *Coordinator) Evaluate(ctx context.Context) {
	if c == nil || c.store == nil || c.console == nil || ctx.Err() != nil {
		return
	}
	settings := c.store.GetNotificationSettings()
	if !settings.Enabled || !configured(settings) {
		return
	}
	secrets, err := c.decryptSecrets(settings)
	if err != nil {
		c.logger.Warn("load Bark notification credentials", "error", err)
		return
	}
	accounts, err := c.console.ListAccounts(ctx)
	if err != nil {
		c.logger.Warn("load accounts for notifications", "error", err)
		return
	}
	groups, err := c.console.ListGroups(ctx)
	if err != nil {
		c.logger.Warn("load groups for notifications", "error", err)
		return
	}
	available := map[int64]upstream.LocalAccountAvailableBalance{}
	finals := map[int64]upstream.LocalAccountFinalMultiplier{}
	protections := map[int64]map[string]upstream.GroupAccountProtectionView{}
	logicalGroups := make(map[int64][]int64)
	if c.projections != nil {
		available = c.projections.LocalAccountAvailableBalances(accounts)
		finals = c.projections.LocalAccountFinalMultipliers(accounts)
		protections = c.projections.GroupAccountProtectionViews(accounts)
		logicalGroups = c.projections.LogicalGroupIDs(accounts)
	}
	managed := make(map[int64]model.ManagedAccount)
	for _, account := range c.accountConfigs() {
		managed[account.AccountID] = account
	}
	groupNames := make(map[int64]string, len(groups))
	for _, group := range groups {
		groupNames[group.ID] = group.Name
	}
	if err := c.evaluateBalances(settings, secrets, accounts, managed, groupNames, available, logicalGroups); err != nil {
		c.logger.Warn("evaluate Bark balance alerts", "error", err)
	}
	if err := c.evaluateCapacity(settings, secrets, accounts, groups, groupNames); err != nil {
		c.logger.Warn("evaluate Bark capacity alerts", "error", err)
	}
	if err := c.evaluateMultipliers(settings, secrets, accounts, groupNames, finals, protections, logicalGroups); err != nil {
		c.logger.Warn("evaluate Bark multiplier alerts", "error", err)
	}
}

func (c *Coordinator) accountConfigs() []model.ManagedAccount {
	if provider, ok := c.store.(interface{ List() []model.ManagedAccount }); ok {
		return provider.List()
	}
	return nil
}

func (c *Coordinator) decryptSecrets(settings model.NotificationSettings) (model.BarkNotificationSecrets, error) {
	if c.box == nil || !c.box.Enabled() {
		return model.BarkNotificationSecrets{}, errors.New("通知凭证加密未配置")
	}
	raw, err := c.box.DecryptBytes(credentialDomain, settings.BarkCredentials)
	if err != nil {
		return model.BarkNotificationSecrets{}, err
	}
	var secrets model.BarkNotificationSecrets
	if err := json.Unmarshal(raw, &secrets); err != nil || strings.TrimSpace(secrets.DeviceKey) == "" || len([]byte(secrets.EncryptionKey)) != 16 {
		return model.BarkNotificationSecrets{}, errors.New("Bark 凭证内容无效")
	}
	return secrets, nil
}

func settingsView(settings model.NotificationSettings) SettingsView {
	return SettingsView{
		Enabled:             settings.Enabled,
		Configured:          configured(settings),
		BarkEndpoint:        settings.BarkEndpoint,
		BarkBasicAuthUser:   settings.BarkBasicAuthUser,
		HasDeviceKey:        settings.BarkCredentials.Ciphertext != "",
		HasEncryptionKey:    settings.BarkCredentials.Ciphertext != "",
		EncryptionAlgorithm: "AES-128-CBC",
		LastDeliveryAt:      cloneTime(settings.LastDeliveryAt),
		LastDeliveryError:   settings.LastDeliveryError,
		UpdatedAt:           settings.UpdatedAt,
	}
}

func (c *Coordinator) recordDelivery(settings model.NotificationSettings, deliveryErr error) {
	now := c.now().UTC()
	settings.LastDeliveryAt = &now
	if deliveryErr != nil {
		settings.LastDeliveryError = truncate(deliveryErr.Error(), 500)
	} else {
		settings.LastDeliveryError = ""
	}
	settings.UpdatedAt = now
	if err := c.store.PutNotificationSettings(settings); err != nil {
		c.logger.Warn("persist Bark delivery status", "error", err)
	}
}

type balanceRelation struct {
	key       string
	groupID   int64
	groupName string
	account   model.UpstreamAccount
	threshold float64
}

func (c *Coordinator) evaluateBalances(settings model.NotificationSettings, secrets model.BarkNotificationSecrets, accounts []model.UpstreamAccount, managed map[int64]model.ManagedAccount, groupNames map[int64]string, available map[int64]upstream.LocalAccountAvailableBalance, logicalGroups map[int64][]int64) error {
	thresholds := c.store.ListGroupBalanceThresholds()
	for _, account := range accounts {
		if !account.IsAPIKey() {
			continue
		}
		config, configured := managed[account.ID]
		accountThreshold := (*float64)(nil)
		if configured {
			accountThreshold = config.BalanceAlertThreshold
		}
		groupIDs := uniquePositiveIDs(logicalGroups[account.ID])
		if len(groupIDs) == 0 {
			groupIDs = uniquePositiveIDs(account.GroupIDs)
		}
		relations := make([]balanceRelation, 0, len(groupIDs))
		for _, groupID := range groupIDs {
			threshold, ok := thresholds[groupID]
			if accountThreshold != nil {
				threshold = *accountThreshold
				ok = true
			}
			if !ok {
				continue
			}
			relations = append(relations, balanceRelation{
				key:       fmt.Sprintf("balance:%d:%d", groupID, account.ID),
				groupID:   groupID,
				groupName: groupName(groupNames, groupID),
				account:   account,
				threshold: threshold,
			})
		}
		if len(relations) == 0 && accountThreshold != nil {
			relations = append(relations, balanceRelation{key: fmt.Sprintf("balance:ungrouped:%d", account.ID), groupName: "未分组", account: account, threshold: *accountThreshold})
		}
		for _, relation := range relations {
			projection, observable := available[account.ID]
			if !observable || projection.Unlimited || projection.Status != "available" || projection.Remaining == nil || math.IsNaN(*projection.Remaining) || math.IsInf(*projection.Remaining, 0) {
				continue
			}
			below := *projection.Remaining < relation.threshold
			state, exists := c.store.GetBalanceAlertState(relation.key)
			if !exists {
				state = model.BalanceAlertState{Configured: true, Below: below, Threshold: relation.threshold, LastAvailable: cloneFloat(projection.Remaining), UpdatedAt: c.now().UTC()}
				if err := c.store.PutBalanceAlertState(relation.key, state); err != nil {
					return err
				}
				if below {
					c.deliverBalance(settings, secrets, relation, *projection.Remaining, false, &state)
				}
				continue
			}
			if state.Below == below && sameFloat(state.Threshold, relation.threshold) && sameFloatPtr(state.LastAvailable, projection.Remaining) {
				continue
			}
			transition := state.Below != below
			state.Configured = true
			state.Below = below
			state.Threshold = relation.threshold
			state.LastAvailable = cloneFloat(projection.Remaining)
			state.LastDeliveryError = ""
			state.UpdatedAt = c.now().UTC()
			if err := c.store.PutBalanceAlertState(relation.key, state); err != nil {
				return err
			}
			if transition {
				c.deliverBalance(settings, secrets, relation, *projection.Remaining, !below, &state)
			}
		}
	}
	return nil
}

func (c *Coordinator) deliverBalance(settings model.NotificationSettings, secrets model.BarkNotificationSecrets, relation balanceRelation, value float64, recovery bool, state *model.BalanceAlertState) {
	title := "余额不足告警"
	body := fmt.Sprintf("%s-%s，可用余额 %.6f 低于 %.6f", relation.groupName, accountName(relation.account), value, relation.threshold)
	if recovery {
		title = "余额恢复通知"
		body = fmt.Sprintf("%s-%s，可用余额 %.6f 已恢复到 %.6f 以上", relation.groupName, accountName(relation.account), value, relation.threshold)
	}
	deliveryErr := c.bark.Send(context.Background(), settings, secrets, title, body)
	if deliveryErr != nil {
		state.LastDeliveryError = truncate(deliveryErr.Error(), 500)
		c.logger.Warn("send Bark balance notification", "account_id", relation.account.ID, "group_id", relation.groupID, "error", deliveryErr)
	} else {
		state.LastDeliveryError = ""
	}
	c.recordDelivery(c.store.GetNotificationSettings(), deliveryErr)
	state.UpdatedAt = c.now().UTC()
	_ = c.store.PutBalanceAlertState(relation.key, *state)
}

func (c *Coordinator) evaluateCapacity(settings model.NotificationSettings, secrets model.BarkNotificationSecrets, accounts []model.UpstreamAccount, groups []model.UpstreamGroup, groupNames map[int64]string) error {
	for _, group := range groups {
		if group.ID <= 0 || strings.ToLower(strings.TrimSpace(group.Status)) != "active" {
			continue
		}
		count := 0
		for _, account := range accounts {
			if accountUsable(account, c.now()) && containsID(account.GroupIDs, group.ID) {
				count++
			}
		}
		band := capacityBand(count)
		key := fmt.Sprintf("capacity:%d", group.ID)
		state, exists := c.store.GetCapacityAlertState(key)
		if !exists {
			state = model.CapacityAlertState{Band: band, LastCount: count, UpdatedAt: c.now().UTC()}
			if err := c.store.PutCapacityAlertState(key, state); err != nil {
				return err
			}
			if band == model.CapacityBandZero {
				c.deliverCapacity(settings, secrets, group, count, false, &state)
			}
			continue
		}
		if state.Band == band && state.LastCount == count {
			continue
		}
		// A single usable account is an intermediate state, not a recovery.
		// Keep the persisted alert band at zero so a later return to zero does
		// not emit a duplicate and recovery still requires more than one.
		if state.Band == model.CapacityBandZero && band == model.CapacityBandOne {
			state.LastCount = count
			state.UpdatedAt = c.now().UTC()
			if err := c.store.PutCapacityAlertState(key, state); err != nil {
				return err
			}
			continue
		}
		previous := state.Band
		state.Band = band
		state.LastCount = count
		state.LastDeliveryError = ""
		state.UpdatedAt = c.now().UTC()
		if err := c.store.PutCapacityAlertState(key, state); err != nil {
			return err
		}
		if previous != model.CapacityBandZero && band == model.CapacityBandZero {
			c.deliverCapacity(settings, secrets, group, count, false, &state)
		} else if previous == model.CapacityBandZero && band == model.CapacityBandMany {
			c.deliverCapacity(settings, secrets, group, count, true, &state)
		}
	}
	return nil
}

func (c *Coordinator) deliverCapacity(settings model.NotificationSettings, secrets model.BarkNotificationSecrets, group model.UpstreamGroup, count int, recovery bool, state *model.CapacityAlertState) {
	title := "分组可用账号告警"
	body := fmt.Sprintf("分组 %s 当前可用 API Key 账号数为 0", group.Name)
	if recovery {
		title = "分组可用账号恢复"
		body = fmt.Sprintf("分组 %s 当前可用 API Key 账号数已恢复为 %d", group.Name, count)
	}
	deliveryErr := c.bark.Send(context.Background(), settings, secrets, title, body)
	if deliveryErr != nil {
		state.LastDeliveryError = truncate(deliveryErr.Error(), 500)
		c.logger.Warn("send Bark capacity notification", "group_id", group.ID, "error", deliveryErr)
	} else {
		state.LastDeliveryError = ""
	}
	c.recordDelivery(c.store.GetNotificationSettings(), deliveryErr)
	state.UpdatedAt = c.now().UTC()
	_ = c.store.PutCapacityAlertState(fmt.Sprintf("capacity:%d", group.ID), *state)
}

func (c *Coordinator) evaluateMultipliers(settings model.NotificationSettings, secrets model.BarkNotificationSecrets, accounts []model.UpstreamAccount, groupNames map[int64]string, finals map[int64]upstream.LocalAccountFinalMultiplier, protections map[int64]map[string]upstream.GroupAccountProtectionView, logicalGroups map[int64][]int64) error {
	for _, account := range accounts {
		if !account.IsAPIKey() {
			continue
		}
		projection, ok := finals[account.ID]
		if !ok || projection.Status != "available" || projection.FinalMultiplier == nil || math.IsNaN(*projection.FinalMultiplier) || math.IsInf(*projection.FinalMultiplier, 0) {
			continue
		}
		groupIDs := uniquePositiveIDs(logicalGroups[account.ID])
		if len(groupIDs) == 0 {
			groupIDs = uniquePositiveIDs(account.GroupIDs)
		}
		for _, groupID := range groupIDs {
			protection, hasProtection := protections[account.ID][strconv.FormatInt(groupID, 10)]
			protected := hasProtection && protection.Status == model.ProtectionExceeded
			key := fmt.Sprintf("multiplier:%d:%d", groupID, account.ID)
			state, exists := c.store.GetMultiplierAlertState(key)
			if !exists || !state.Initialized || state.LastFinalMultiplier == nil {
				state = model.MultiplierAlertState{Initialized: true, LastFinalMultiplier: cloneFloat(projection.FinalMultiplier), ProtectionTriggered: protected, UpdatedAt: c.now().UTC()}
				if err := c.store.PutMultiplierAlertState(key, state); err != nil {
					return err
				}
				continue
			}
			if sameFloat(*state.LastFinalMultiplier, *projection.FinalMultiplier) {
				if state.ProtectionTriggered != protected {
					state.ProtectionTriggered = protected
					state.UpdatedAt = c.now().UTC()
					if err := c.store.PutMultiplierAlertState(key, state); err != nil {
						return err
					}
				}
				continue
			}
			old := *state.LastFinalMultiplier
			state.LastFinalMultiplier = cloneFloat(projection.FinalMultiplier)
			state.ProtectionTriggered = protected
			state.LastDeliveryError = ""
			state.UpdatedAt = c.now().UTC()
			if err := c.store.PutMultiplierAlertState(key, state); err != nil {
				return err
			}
			body := fmt.Sprintf("%s-%s，最终倍率由 %.6f 调整为 %.6f；倍率保护：%s", groupName(groupNames, groupID), accountName(account), old, *projection.FinalMultiplier, protectionLabel(protected))
			deliveryErr := c.bark.Send(context.Background(), settings, secrets, "上游最终倍率变化", body)
			if deliveryErr != nil {
				state.LastDeliveryError = truncate(deliveryErr.Error(), 500)
				c.logger.Warn("send Bark multiplier notification", "account_id", account.ID, "group_id", groupID, "error", deliveryErr)
			}
			c.recordDelivery(c.store.GetNotificationSettings(), deliveryErr)
			state.UpdatedAt = c.now().UTC()
			_ = c.store.PutMultiplierAlertState(key, state)
		}
	}
	return nil
}

func configured(s model.NotificationSettings) bool {
	return strings.TrimSpace(s.BarkEndpoint) != "" && s.BarkCredentials.Ciphertext != ""
}

func capacityBand(count int) model.CapacityAlertBand {
	switch {
	case count <= 0:
		return model.CapacityBandZero
	case count == 1:
		return model.CapacityBandOne
	default:
		return model.CapacityBandMany
	}
}

func groupName(groups map[int64]string, groupID int64) string {
	if value := strings.TrimSpace(groups[groupID]); value != "" {
		return value
	}
	return "分组 " + strconv.FormatInt(groupID, 10)
}

func accountName(account model.UpstreamAccount) string {
	if value := strings.TrimSpace(account.Name); value != "" {
		return value
	}
	return "账号 " + strconv.FormatInt(account.ID, 10)
}

func protectionLabel(triggered bool) string {
	if triggered {
		return "已触发"
	}
	return "未触发"
}

func uniquePositiveIDs(values []int64) []int64 {
	seen := make(map[int64]struct{}, len(values))
	result := make([]int64, 0, len(values))
	for _, value := range values {
		if value <= 0 {
			continue
		}
		if _, exists := seen[value]; exists {
			continue
		}
		seen[value] = struct{}{}
		result = append(result, value)
	}
	sort.Slice(result, func(i, j int) bool { return result[i] < result[j] })
	return result
}

func containsID(values []int64, target int64) bool {
	for _, value := range values {
		if value == target {
			return true
		}
	}
	return false
}

func accountUsable(account model.UpstreamAccount, now time.Time) bool {
	if !account.IsAPIKey() || account.Status != "active" || !account.Schedulable {
		return false
	}
	for _, until := range []*time.Time{account.TempUnschedulableUntil, account.OverloadUntil, account.RateLimitResetAt} {
		if until != nil && until.After(now) {
			return false
		}
	}
	return !(account.AutoPauseOnExpired && account.ExpiresAt != nil && *account.ExpiresAt > 0 && time.Unix(*account.ExpiresAt, 0).Before(now))
}

func cloneFloat(value *float64) *float64 {
	if value == nil {
		return nil
	}
	copy := *value
	return &copy
}

func cloneTime(value *time.Time) *time.Time {
	if value == nil {
		return nil
	}
	copy := *value
	return &copy
}

func sameFloat(left, right float64) bool {
	return math.Abs(left-right) <= 1e-12*math.Max(1, math.Max(math.Abs(left), math.Abs(right)))
}

func sameFloatPtr(left, right *float64) bool {
	if left == nil || right == nil {
		return left == nil && right == nil
	}
	return sameFloat(*left, *right)
}

func truncate(value string, max int) string {
	value = strings.TrimSpace(value)
	if len(value) <= max {
		return value
	}
	return value[:max]
}
