package engine

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/Wei-Shaw/sub2api-account-auto-scheduler/internal/core"
	"github.com/Wei-Shaw/sub2api-account-auto-scheduler/internal/model"
	"github.com/Wei-Shaw/sub2api-account-auto-scheduler/internal/store"
)

var (
	ErrAlreadyRunning        = errors.New("account check is already running")
	ErrUnsupportedAccount    = errors.New("only API Key accounts are supported")
	ErrAccountStatusInactive = errors.New("account status is not active")
)

type CoreClient interface {
	ListAPIKeyAccounts(ctx context.Context) ([]model.UpstreamAccount, error)
	GetAccount(ctx context.Context, accountID int64) (model.UpstreamAccount, error)
	TestAccount(ctx context.Context, accountID int64, modelID, prompt string) (core.ProbeOutcome, error)
	SetSchedulable(ctx context.Context, accountID int64, schedulable bool) (model.UpstreamAccount, error)
}

type modelPricingClient interface {
	GetModelPricing(ctx context.Context, modelID string) (core.ModelPricing, error)
}

// DirectProbeCredentialBox is intentionally narrow. The engine can manage the
// lifecycle of an encrypted snapshot without knowing the encryption key or
// importing any upstream login/session behavior.
type DirectProbeCredentialBox interface {
	Enabled() bool
	EncryptDirectProbe(accountID int64, snapshot model.DirectProbeSnapshot) (model.CredentialEnvelope, error)
	DecryptDirectProbe(accountID int64, envelope model.CredentialEnvelope) (model.DirectProbeSnapshot, error)
}

type directProbeExporter interface {
	ExportDirectProbeSnapshot(ctx context.Context, accountID int64, adminJWT string, identity core.ForwardedIdentity) (core.DirectProbeExport, error)
}

type ManualProbeResult struct {
	Model   string            `json:"model"`
	Outcome core.ProbeOutcome `json:"outcome"`
	Error   string            `json:"error,omitempty"`
}

func (e *Engine) ManualProbe(ctx context.Context, accountID int64, adminJWT string, identity core.ForwardedIdentity, models []string, prompt, effort string) ([]ManualProbeResult, error) {
	if !e.DirectProbeEnabled() {
		return nil, errors.New("直连探测凭证加密未配置")
	}
	if len(models) == 0 || len(models) > 8 {
		return nil, errors.New("模型数量必须在 1 到 8 个之间")
	}
	if _, err := e.store.Get(accountID); err != nil {
		return nil, err
	}
	account, err := e.core.GetAccount(ctx, accountID)
	if err != nil {
		return nil, fmt.Errorf("读取账号失败: %w", err)
	}
	if !account.IsAPIKey() {
		return nil, errors.New("仅支持 API Key 账号")
	}
	exporter, ok := e.core.(directProbeExporter)
	if !ok {
		return nil, errors.New("当前服务未启用直连探测授权导入")
	}
	exported, err := exporter.ExportDirectProbeSnapshot(ctx, accountID, adminJWT, identity)
	if err != nil {
		return nil, err
	}
	snapshot := exported.Snapshot
	defer model.ClearDirectProbeSnapshot(&snapshot)
	if snapshot.AccountID != accountID {
		return nil, errors.New("账号导出内容与当前账号不一致")
	}
	prober, ok := e.core.(core.DirectProber)
	if !ok {
		return nil, errors.New("当前服务未启用直连上游探测")
	}
	if strings.TrimSpace(prompt) == "" {
		prompt = model.DefaultPrompt
	}
	if len(prompt) > 8000 {
		return nil, errors.New("提示词不能超过 8000 个字符")
	}
	policy := model.DefaultPolicy()
	policy.Prompt, policy.ReasoningEffort, policy.LatencyLimitMS = prompt, effort, 120000
	policy.Model = ""
	normalized, err := policy.Normalize()
	if err != nil {
		return nil, err
	}
	policy = normalized
	results := make([]ManualProbeResult, 0, len(models))
	for _, requested := range models {
		requested = strings.TrimSpace(requested)
		if requested == "" || len(requested) > 200 {
			return nil, errors.New("模型名称无效")
		}
		policy.Model = requested
		outcome, probeErr := prober.ProbeDirect(ctx, snapshot, policy)
		if probeErr != nil {
			results = append(results, ManualProbeResult{Model: requested, Outcome: outcome, Error: probeErr.Error()})
		} else {
			results = append(results, ManualProbeResult{Model: requested, Outcome: outcome})
		}
	}
	return results, nil
}

type EngineOption func(*Engine)

// WithDirectProbeCredentials enables the explicit direct-upstream authorization
// workflow. Omitting it retains legacy account-test behavior exactly.
func WithDirectProbeCredentials(box DirectProbeCredentialBox) EngineOption {
	return func(engine *Engine) {
		engine.directBox = box
	}
}

type Engine struct {
	store  *store.Store
	core   CoreClient
	logger *slog.Logger
	sem    chan struct{}

	directBox DirectProbeCredentialBox

	mu      sync.Mutex
	running map[int64]struct{}
	ctx     context.Context
	cancel  context.CancelFunc
	wg      sync.WaitGroup
	now     func() time.Time
}

func New(stateStore *store.Store, coreClient CoreClient, maxConcurrency int, logger *slog.Logger, options ...EngineOption) *Engine {
	if logger == nil {
		logger = slog.Default()
	}
	if maxConcurrency < 1 {
		maxConcurrency = 1
	}
	engine := &Engine{
		store:   stateStore,
		core:    coreClient,
		logger:  logger,
		sem:     make(chan struct{}, maxConcurrency),
		running: map[int64]struct{}{},
		now:     time.Now,
	}
	for _, option := range options {
		if option != nil {
			option(engine)
		}
	}
	return engine
}

func (e *Engine) Start(parent context.Context) {
	e.mu.Lock()
	if e.cancel != nil {
		e.mu.Unlock()
		return
	}
	e.ctx, e.cancel = context.WithCancel(parent)
	ctx := e.ctx
	e.mu.Unlock()

	e.wg.Add(1)
	go func() {
		defer e.wg.Done()
		e.runDue()
		ticker := time.NewTicker(time.Second)
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				e.runDue()
			}
		}
	}()
}

func (e *Engine) Stop() {
	e.mu.Lock()
	if e.cancel != nil {
		e.cancel()
	}
	e.mu.Unlock()
	e.wg.Wait()
}

// EnterPassiveMode permanently disables the legacy active-probe runtime while
// preserving the serialized account records used by balance alerts and manual
// scheduling. Only suspensions explicitly owned by the probe state machine are
// restored; administrator-owned stops never carry ManagedSuspended and remain
// unchanged. The transition is idempotent and best effort per account.
func (e *Engine) EnterPassiveMode(ctx context.Context) error {
	if e == nil || e.store == nil {
		return nil
	}
	if ctx == nil {
		ctx = context.Background()
	}

	var failures []error
	for _, managed := range e.store.List() {
		var snapshot *model.UpstreamAccount
		if managed.ManagedSuspended {
			if e.core == nil {
				failures = append(failures, fmt.Errorf("restore probe-owned suspension for account %d: core client unavailable", managed.AccountID))
			} else {
				account, err := e.core.GetAccount(ctx, managed.AccountID)
				if err != nil {
					failures = append(failures, fmt.Errorf("restore probe-owned suspension for account %d: %w", managed.AccountID, err))
				} else {
					snapshot = &account
					if !account.Schedulable {
						if account.Status != "active" {
							failures = append(failures, fmt.Errorf("restore probe-owned suspension for account %d: account status is %s", managed.AccountID, account.Status))
						} else if restored, restoreErr := e.core.SetSchedulable(ctx, managed.AccountID, true); restoreErr != nil {
							failures = append(failures, fmt.Errorf("restore probe-owned suspension for account %d: %w", managed.AccountID, restoreErr))
						} else {
							snapshot = &restored
						}
					}
				}
			}
		}

		if err := e.store.Update(managed.AccountID, func(current *model.ManagedAccount) error {
			if snapshot != nil {
				current.ApplySnapshot(*snapshot)
			}
			current.Policy.Enabled = false
			current.NextCheckAt = nil
			current.Running = false
			current.ManagedSuspended = false
			current.ConsecutiveFailures = 0
			current.ConsecutiveSuccesses = 0
			current.UpdatedAt = e.now().UTC()
			return nil
		}); err != nil {
			failures = append(failures, fmt.Errorf("disable active probing for account %d: %w", managed.AccountID, err))
		}
	}
	return errors.Join(failures...)
}

func (e *Engine) List() []model.ManagedAccount {
	items := e.store.List()
	for index := range items {
		items[index].Running = e.isRunning(items[index].AccountID)
	}
	return items
}

func (e *Engine) ListViews() []model.ManagedAccountView {
	items := e.List()
	views := make([]model.ManagedAccountView, 0, len(items))
	for _, item := range items {
		views = append(views, item.PublicView())
	}
	return views
}

func (e *Engine) DirectProbeEnabled() bool {
	return e != nil && e.directBox != nil && e.directBox.Enabled()
}

// AuthorizeDirect imports one account's secret routing snapshot using the
// current browser administrator session, encrypts it before persistence, and
// schedules a direct check. No exported material leaves this method.
func (e *Engine) AuthorizeDirect(
	ctx context.Context,
	accountID int64,
	adminJWT string,
	identity core.ForwardedIdentity,
) (model.ManagedAccount, error) {
	if !e.DirectProbeEnabled() {
		return model.ManagedAccount{}, errors.New("直连探测凭证加密未配置，请设置 AUTO_SCHEDULER_CREDENTIAL_KEY")
	}
	if _, err := e.store.Get(accountID); err != nil {
		return model.ManagedAccount{}, err
	}
	account, err := e.core.GetAccount(ctx, accountID)
	if err != nil {
		return model.ManagedAccount{}, fmt.Errorf("读取账号失败: %w", err)
	}
	if !account.IsAPIKey() {
		return model.ManagedAccount{}, errors.New("only API Key accounts are supported")
	}
	if !model.IsDirectProbePlatformSupported(account.Platform) {
		return model.ManagedAccount{}, fmt.Errorf("暂不支持 %s 平台的直连探测", strings.TrimSpace(account.Platform))
	}
	exporter, ok := e.core.(directProbeExporter)
	if !ok {
		return model.ManagedAccount{}, errors.New("当前服务未启用直连探测授权导入")
	}
	exported, err := exporter.ExportDirectProbeSnapshot(ctx, accountID, adminJWT, identity)
	if err != nil {
		return model.ManagedAccount{}, err
	}
	snapshot := exported.Snapshot
	defer model.ClearDirectProbeSnapshot(&snapshot)
	if snapshot.AccountID != accountID || model.NormalizeDirectProbePlatform(snapshot.Platform) != model.NormalizeDirectProbePlatform(account.Platform) {
		return model.ManagedAccount{}, errors.New("账号导出内容与当前账号不一致")
	}
	currentFingerprint, err := model.DirectProbeRoutingFingerprintForAccount(account)
	if err != nil {
		return model.ManagedAccount{}, fmt.Errorf("读取账号直连路由失败: %w", err)
	}
	if snapshot.RoutingFingerprint == "" || snapshot.RoutingFingerprint != model.DirectProbeRoutingFingerprint(snapshot) || snapshot.RoutingFingerprint != currentFingerprint {
		return model.ManagedAccount{}, errors.New("账号导出路由与当前配置不一致，请重新授权")
	}
	envelope, err := e.directBox.EncryptDirectProbe(accountID, snapshot)
	if err != nil {
		return model.ManagedAccount{}, err
	}
	now := e.now().UTC()
	if err := e.store.Update(accountID, func(current *model.ManagedAccount) error {
		current.ApplySnapshot(account)
		current.ProbeSource = model.ProbeSourceDirect
		current.DirectProbe = &model.DirectProbeConfig{
			Credential:         envelope,
			AuthorizationState: model.DirectProbeAuthorized,
			ImportedAt:         &now,
			UpdatedAt:          &now,
			RoutingFingerprint: snapshot.RoutingFingerprint,
		}
		if current.Policy.Enabled {
			next := now
			current.NextCheckAt = &next
		}
		current.UpdatedAt = now
		return nil
	}); err != nil {
		return model.ManagedAccount{}, err
	}
	e.scheduleImmediate(accountID)
	return e.store.Get(accountID)
}

// RevokeDirect deletes the locally encrypted snapshot and intentionally moves
// only this scheduler configuration back to the existing legacy test route.
func (e *Engine) RevokeDirect(ctx context.Context, accountID int64) (model.ManagedAccount, error) {
	if _, err := e.store.Get(accountID); err != nil {
		return model.ManagedAccount{}, err
	}
	now := e.now().UTC()
	if err := e.store.Update(accountID, func(current *model.ManagedAccount) error {
		if current.DirectProbe != nil {
			current.DirectProbe.Credential.Nonce = ""
			current.DirectProbe.Credential.Ciphertext = ""
			current.DirectProbe = nil
		}
		current.ProbeSource = model.ProbeSourceLegacy
		if current.Policy.Enabled {
			next := now
			current.NextCheckAt = &next
		}
		current.UpdatedAt = now
		return nil
	}); err != nil {
		return model.ManagedAccount{}, err
	}
	e.scheduleImmediate(accountID)
	return e.store.Get(accountID)
}

func (e *Engine) AvailableAccounts(ctx context.Context) ([]model.UpstreamAccount, error) {
	accounts, err := e.core.ListAPIKeyAccounts(ctx)
	if err != nil {
		return nil, err
	}
	e.syncSnapshots(accounts)
	return accounts, nil
}

// EnsurePassiveAccount creates the compatibility record required by account-
// level alerts without enabling any probe policy or scheduling a check.
func (e *Engine) EnsurePassiveAccount(ctx context.Context, accountID int64) (model.ManagedAccount, error) {
	if existing, err := e.store.Get(accountID); err == nil {
		return existing, nil
	} else if !errors.Is(err, store.ErrNotFound) {
		return model.ManagedAccount{}, err
	}
	if e.core == nil {
		return model.ManagedAccount{}, errors.New("core client unavailable")
	}
	account, err := e.core.GetAccount(ctx, accountID)
	if err != nil {
		return model.ManagedAccount{}, err
	}
	if !account.IsAPIKey() {
		return model.ManagedAccount{}, ErrUnsupportedAccount
	}
	now := e.now().UTC()
	policy := model.DefaultPolicy()
	policy.Enabled = false
	managed := model.ManagedAccount{AccountID: accountID, Policy: policy, History: []model.CheckResult{}, CreatedAt: now, UpdatedAt: now}
	managed.ApplySnapshot(account)
	if err := e.store.Put(managed); err != nil {
		return model.ManagedAccount{}, err
	}
	return e.store.Get(accountID)
}

func (e *Engine) Upsert(ctx context.Context, accountID int64, policy model.Policy) (model.ManagedAccount, error) {
	normalized, err := policy.Normalize()
	if err != nil {
		return model.ManagedAccount{}, err
	}
	account, err := e.core.GetAccount(ctx, accountID)
	if err != nil {
		return model.ManagedAccount{}, err
	}

	now := e.now().UTC()
	existing, getErr := e.store.Get(accountID)
	if getErr != nil && !errors.Is(getErr, store.ErrNotFound) {
		return model.ManagedAccount{}, getErr
	}
	if normalized.Enabled {
		if account.Status != "active" {
			return model.ManagedAccount{}, errors.New("账号状态不是 active，无法启用自动调度")
		}
		ownedSuspension := getErr == nil && existing.ManagedSuspended
		if !account.Schedulable && !ownedSuspension {
			restored, err := e.core.SetSchedulable(ctx, accountID, true)
			if err != nil {
				return model.ManagedAccount{}, fmt.Errorf("恢复账号调度失败: %w", err)
			}
			account = restored
		}
	}
	if getErr == nil && existing.ManagedSuspended && !normalized.Enabled {
		restored, err := e.restoreOwnedSuspension(ctx, existing, account)
		if err != nil {
			return model.ManagedAccount{}, fmt.Errorf("停用检测前恢复账号调度失败: %w", err)
		}
		account = restored
		existing.ManagedSuspended = false
		existing.ConsecutiveFailures = 0
		existing.ConsecutiveSuccesses = 0
	}

	managed := existing
	if errors.Is(getErr, store.ErrNotFound) {
		managed = model.ManagedAccount{
			AccountID: accountID,
			History:   []model.CheckResult{},
			CreatedAt: now,
		}
	}
	managed.ApplySnapshot(account)
	managed.Policy = normalized
	managed.UpdatedAt = now
	managed.Running = false
	if normalized.Enabled {
		if managed.NextCheckAt == nil || !existing.Policy.Enabled {
			next := now
			managed.NextCheckAt = &next
		}
	} else {
		managed.NextCheckAt = nil
	}
	if err := e.store.Put(managed); err != nil {
		return model.ManagedAccount{}, err
	}
	return e.store.Get(accountID)
}

// SetManualSchedulable applies an administrator-owned scheduling decision.
// It intentionally keeps any detection policy and history, while relinquishing
// automatic-suspension ownership so a healthy check cannot undo a manual stop.
func (e *Engine) SetManualSchedulable(ctx context.Context, accountID int64, schedulable bool) (model.UpstreamAccount, error) {
	e.mu.Lock()
	if err := e.claimAccountOperationLocked(accountID); err != nil {
		e.mu.Unlock()
		return model.UpstreamAccount{}, err
	}
	e.mu.Unlock()
	defer e.clearRunning(accountID)

	account, err := e.core.GetAccount(ctx, accountID)
	if err != nil {
		return model.UpstreamAccount{}, fmt.Errorf("读取账号失败: %w", err)
	}
	if !account.IsAPIKey() {
		return model.UpstreamAccount{}, ErrUnsupportedAccount
	}
	if schedulable && account.Status != "active" {
		return model.UpstreamAccount{}, ErrAccountStatusInactive
	}

	updated := account
	if account.Schedulable != schedulable {
		updated, err = e.core.SetSchedulable(ctx, accountID, schedulable)
		if err != nil {
			return model.UpstreamAccount{}, fmt.Errorf("更新账号调度失败: %w", err)
		}
	}

	now := e.now().UTC()
	if err := e.store.Update(accountID, func(current *model.ManagedAccount) error {
		current.ApplySnapshot(updated)
		current.ManagedSuspended = false
		current.ConsecutiveFailures = 0
		current.ConsecutiveSuccesses = 0
		current.UpdatedAt = now
		return nil
	}); err != nil && !errors.Is(err, store.ErrNotFound) {
		return model.UpstreamAccount{}, fmt.Errorf("保存账号调度状态失败: %w", err)
	}
	return updated, nil
}

func (e *Engine) Delete(ctx context.Context, accountID int64) error {
	managed, err := e.store.Get(accountID)
	if err != nil {
		return err
	}
	if e.isRunning(accountID) {
		return ErrAlreadyRunning
	}
	if managed.ManagedSuspended {
		account, err := e.core.GetAccount(ctx, accountID)
		if err != nil {
			return fmt.Errorf("删除配置前读取账号失败: %w", err)
		}
		if _, err := e.restoreOwnedSuspension(ctx, managed, account); err != nil {
			return fmt.Errorf("删除配置前恢复账号调度失败: %w", err)
		}
	}
	return e.store.Delete(accountID)
}

func (e *Engine) Trigger(accountID int64) error {
	if _, err := e.store.Get(accountID); err != nil {
		return err
	}

	e.mu.Lock()
	if err := e.claimAccountOperationLocked(accountID); err != nil {
		e.mu.Unlock()
		return err
	}
	if e.ctx == nil || e.ctx.Err() != nil {
		delete(e.running, accountID)
		e.mu.Unlock()
		return errors.New("scheduler is not running")
	}
	ctx := e.ctx
	e.wg.Add(1)
	e.mu.Unlock()

	go func() {
		defer e.wg.Done()
		select {
		case e.sem <- struct{}{}:
			defer func() { <-e.sem }()
		case <-ctx.Done():
			e.clearRunning(accountID)
			return
		}
		defer e.clearRunning(accountID)
		e.runOne(ctx, accountID)
	}()
	return nil
}

func (e *Engine) runDue() {
	now := e.now()
	for _, account := range e.store.List() {
		if !account.Policy.Enabled || account.NextCheckAt == nil || account.NextCheckAt.After(now) {
			continue
		}
		if err := e.Trigger(account.AccountID); err != nil && !errors.Is(err, ErrAlreadyRunning) {
			e.logger.Error("trigger scheduled account check", "account_id", account.AccountID, "error", err)
		}
	}
}

func (e *Engine) runOne(parent context.Context, accountID int64) {
	managed, err := e.store.Get(accountID)
	if err != nil {
		return
	}
	account, accountErr := e.core.GetAccount(parent, accountID)
	if accountErr != nil {
		status := model.CheckError
		message := truncate("读取账号失败: "+accountErr.Error(), 500)
		if managed.EffectiveProbeSource() == model.ProbeSourceDirect {
			status = model.CheckSkipped
			message = "无法读取当前账号配置，暂不执行直连探测"
			e.setDirectProbeState(accountID, model.DirectProbeNeedsReauthorization, message)
		}
		e.finishCheck(parent, managed, model.UpstreamAccount{}, model.CheckResult{
			ID:        resultID(accountID, e.now()),
			Status:    status,
			Message:   message,
			CheckedAt: e.now().UTC(),
		})
		return
	}

	if managed.EffectiveProbeSource() == model.ProbeSourceDirect {
		result, handled := e.runDirectProbe(parent, managed, account)
		if handled {
			e.finishCheck(parent, managed, account, result)
			return
		}
	}

	hardTimeout := time.Duration(managed.Policy.LatencyLimitMS)*time.Millisecond + 5*time.Second
	ctx, cancel := context.WithTimeout(parent, hardTimeout)
	outcome, probeErr := e.core.TestAccount(ctx, accountID, managed.Policy.Model, managed.Policy.Prompt)
	cancel()

	result := classifyResult(accountID, managed.Policy, outcome, probeErr, e.now().UTC())
	e.finishCheck(parent, managed, account, result)
}

// runDirectProbe always returns handled=true for a direct source. A direct
// configuration must never fall through to the legacy account-test endpoint.
func (e *Engine) runDirectProbe(parent context.Context, managed model.ManagedAccount, account model.UpstreamAccount) (model.CheckResult, bool) {
	checkedAt := e.now().UTC()
	neutral := func(message string, state model.DirectProbeAuthorizationState) (model.CheckResult, bool) {
		e.setDirectProbeState(managed.AccountID, state, message)
		return model.CheckResult{
			ID:        resultID(managed.AccountID, checkedAt),
			Status:    model.CheckSkipped,
			Message:   truncate(message, 500),
			CheckedAt: checkedAt,
		}, true
	}
	if !account.IsAPIKey() {
		return neutral("当前账号已不是 API Key，直连探测需要重新授权", model.DirectProbeNeedsReauthorization)
	}
	if !model.IsDirectProbePlatformSupported(account.Platform) {
		return neutral("当前账号平台暂不支持直连探测", model.DirectProbeUnsupported)
	}
	if !e.DirectProbeEnabled() {
		return neutral("直连探测凭证加密未配置，请设置 AUTO_SCHEDULER_CREDENTIAL_KEY", model.DirectProbeCredentialsUnavailable)
	}
	if managed.DirectProbe == nil || managed.DirectProbe.Credential.Ciphertext == "" {
		return neutral("直连探测授权信息缺失，请重新授权", model.DirectProbeNeedsReauthorization)
	}
	if managed.DirectProbe.AuthorizationState != "" && managed.DirectProbe.AuthorizationState != model.DirectProbeAuthorized {
		message := strings.TrimSpace(managed.DirectProbe.ActionMessage)
		if message == "" {
			message = "直连探测需要重新授权"
		}
		return neutral(message, managed.DirectProbe.AuthorizationState)
	}
	currentFingerprint, err := model.DirectProbeRoutingFingerprintForAccount(account)
	if err != nil || currentFingerprint == "" || currentFingerprint != managed.DirectProbe.RoutingFingerprint {
		return neutral("账号上游路由或代理配置已变化，请重新授权直连探测", model.DirectProbeNeedsReauthorization)
	}
	snapshot, err := e.directBox.DecryptDirectProbe(managed.AccountID, managed.DirectProbe.Credential)
	if err != nil {
		return neutral("无法读取直连探测授权信息，请重新授权", model.DirectProbeNeedsReauthorization)
	}
	defer model.ClearDirectProbeSnapshot(&snapshot)
	if snapshot.RoutingFingerprint != managed.DirectProbe.RoutingFingerprint || model.DirectProbeRoutingFingerprint(snapshot) != managed.DirectProbe.RoutingFingerprint {
		return neutral("直连探测授权信息已过期，请重新授权", model.DirectProbeNeedsReauthorization)
	}
	if _, err := model.DirectProbeProxyURL(snapshot.Proxy, e.now()); err != nil {
		return neutral("账号配置的代理当前无法用于直连探测，请重新授权", model.DirectProbeNeedsReauthorization)
	}
	prober, ok := e.core.(core.DirectProber)
	if !ok {
		return neutral("当前服务未启用直连上游探测", model.DirectProbeCredentialsUnavailable)
	}
	hardTimeout := time.Duration(managed.Policy.LatencyLimitMS)*time.Millisecond + 5*time.Second
	ctx, cancel := context.WithTimeout(parent, hardTimeout)
	outcome, probeErr := prober.ProbeDirect(ctx, snapshot, managed.Policy)
	cancel()
	return classifyResult(managed.AccountID, managed.Policy, outcome, probeErr, checkedAt), true
}

func (e *Engine) finishCheck(
	ctx context.Context,
	started model.ManagedAccount,
	account model.UpstreamAccount,
	result model.CheckResult,
) {
	latest, err := e.store.Get(started.AccountID)
	if err != nil {
		return
	}
	if account.ID != 0 {
		latest.ApplySnapshot(account)
	}

	counted := result.Status != model.CheckSkipped
	healthy := result.Status == model.CheckOperational
	if counted {
		e.attachProbeCost(ctx, started.Policy.Model, &result)
	}
	if counted {
		if healthy {
			latest.ConsecutiveSuccesses++
			latest.ConsecutiveFailures = 0
		} else {
			latest.ConsecutiveFailures++
			latest.ConsecutiveSuccesses = 0
		}
	}

	// If scheduling was re-enabled outside this service, relinquish ownership.
	if latest.ManagedSuspended && account.ID != 0 && account.Schedulable {
		latest.ManagedSuspended = false
		latest.ConsecutiveFailures = 0
	}

	if counted && latest.Policy.Enabled && healthy && latest.ManagedSuspended && latest.ConsecutiveSuccesses >= latest.Policy.RecoveryThreshold {
		if account.Status != "active" {
			result.Action = "restore_blocked"
			result.Message = appendMessage(result.Message, "账号状态不是 active，暂不恢复调度")
		} else {
			restored, actionErr := e.core.SetSchedulable(ctx, latest.AccountID, true)
			if actionErr != nil {
				result.Action = "restore_failed"
				result.Message = appendMessage(result.Message, "恢复调度失败: "+actionErr.Error())
			} else {
				latest.ApplySnapshot(restored)
				latest.ManagedSuspended = false
				result.Action = "restored"
			}
		}
	}

	if counted && latest.Policy.Enabled && !healthy && !latest.ManagedSuspended && latest.ConsecutiveFailures >= latest.Policy.FailureThreshold {
		if account.ID != 0 && account.Schedulable {
			disabled, actionErr := e.core.SetSchedulable(ctx, latest.AccountID, false)
			if actionErr != nil {
				result.Action = "disable_failed"
				result.Message = appendMessage(result.Message, "关闭调度失败: "+actionErr.Error())
			} else {
				latest.ApplySnapshot(disabled)
				latest.ManagedSuspended = true
				result.Action = "disabled"
			}
		}
	}

	checkedAt := result.CheckedAt.UTC()
	latest.LastCheckAt = &checkedAt
	latest.LastError = ""
	latest.LastFailureKind = ""
	if counted && !healthy {
		latest.LastError = result.Message
		latest.LastFailureKind = result.FailureKind
	}
	if latest.Policy.Enabled {
		next := checkedAt.Add(time.Duration(latest.Policy.IntervalSeconds) * time.Second)
		latest.NextCheckAt = &next
	} else {
		latest.NextCheckAt = nil
	}
	latest.UpdatedAt = checkedAt
	if counted {
		latest.DetectionStats.Add(result.Usage, result.Cost, checkedAt)
	}
	latest.AddHistory(result)
	if err := e.store.Put(latest); err != nil {
		e.logger.Error("persist account check", "account_id", latest.AccountID, "error", err)
	}
}

func (e *Engine) setDirectProbeState(accountID int64, state model.DirectProbeAuthorizationState, message string) {
	now := e.now().UTC()
	if err := e.store.Update(accountID, func(current *model.ManagedAccount) error {
		if current.EffectiveProbeSource() != model.ProbeSourceDirect {
			return nil
		}
		if current.DirectProbe == nil {
			current.DirectProbe = &model.DirectProbeConfig{}
		}
		current.DirectProbe.AuthorizationState = state
		current.DirectProbe.ActionMessage = truncate(message, 500)
		current.DirectProbe.UpdatedAt = &now
		current.UpdatedAt = now
		return nil
	}); err != nil {
		e.logger.Error("persist direct probe state", "account_id", accountID, "error", err)
	}
}

func (e *Engine) scheduleImmediate(accountID int64) {
	if err := e.Trigger(accountID); err != nil && !errors.Is(err, ErrAlreadyRunning) {
		e.logger.Debug("defer immediate direct probe", "account_id", accountID, "error", err)
	}
}

func classifyResult(
	accountID int64,
	policy model.Policy,
	outcome core.ProbeOutcome,
	probeErr error,
	checkedAt time.Time,
) model.CheckResult {
	result := model.CheckResult{
		ID:           resultID(accountID, checkedAt),
		LatencyMS:    outcome.Latency.Milliseconds(),
		ResponseText: truncate(outcome.ResponseText, 2000),
		CheckedAt:    checkedAt,
		Usage:        cloneProbeUsage(outcome.Usage),
	}
	if probeErr != nil {
		result.Status = model.CheckError
		if errors.Is(probeErr, context.DeadlineExceeded) {
			result.Status = model.CheckDegraded
			result.Message = fmt.Sprintf("检测超过耗时上限 %dms", policy.LatencyLimitMS)
		} else {
			result.Message = truncate(probeErr.Error(), 500)
			if core.IsDirectProbeInsufficientBalance(probeErr) {
				result.FailureKind = model.CheckFailureBalanceInsufficient
			}
		}
		return result
	}
	if !outcome.Success {
		result.Status = model.CheckFailed
		result.Message = truncate(strings.TrimSpace(outcome.ErrorMessage), 500)
		if result.Message == "" {
			result.Message = "检测调用失败"
		}
		return result
	}
	if result.LatencyMS >= policy.LatencyLimitMS {
		result.Status = model.CheckDegraded
		result.Message = fmt.Sprintf("耗时 %dms，达到或超过上限 %dms", result.LatencyMS, policy.LatencyLimitMS)
		return result
	}
	result.Status = model.CheckOperational
	result.Message = "检测正常"
	return result
}

func (e *Engine) attachProbeCost(ctx context.Context, configuredModel string, result *model.CheckResult) {
	if result == nil {
		return
	}
	result.Cost = &model.ProbeCost{Currency: "USD", Known: false}
	if result.Usage == nil {
		return
	}
	modelID := strings.TrimSpace(result.Usage.Model)
	if modelID == "" {
		modelID = strings.TrimSpace(configuredModel)
		result.Usage.Model = modelID
	}
	pricingClient, ok := e.core.(modelPricingClient)
	if !ok || modelID == "" {
		return
	}
	pricingCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	pricing, err := pricingClient.GetModelPricing(pricingCtx, modelID)
	if err != nil {
		e.logger.Debug("probe pricing unavailable", "model", modelID, "error", err)
		return
	}
	result.Cost = core.CalculateProbeCost(result.Usage, pricing)
}

func cloneProbeUsage(usage *model.ProbeUsage) *model.ProbeUsage {
	if usage == nil {
		return nil
	}
	copy := *usage
	return &copy
}

func (e *Engine) syncSnapshots(accounts []model.UpstreamAccount) {
	byID := make(map[int64]model.UpstreamAccount, len(accounts))
	for _, account := range accounts {
		byID[account.ID] = account
	}
	for _, managed := range e.store.List() {
		account, ok := byID[managed.AccountID]
		if !ok {
			continue
		}
		if managed.Name == account.Name && managed.Platform == account.Platform &&
			managed.AccountStatus == account.Status && managed.Schedulable == account.Schedulable {
			continue
		}
		if err := e.store.Update(managed.AccountID, func(current *model.ManagedAccount) error {
			current.ApplySnapshot(account)
			if current.ManagedSuspended && account.Schedulable {
				current.ManagedSuspended = false
				current.ConsecutiveFailures = 0
			}
			current.UpdatedAt = e.now().UTC()
			return nil
		}); err != nil {
			e.logger.Error("sync account snapshot", "account_id", managed.AccountID, "error", err)
		}
	}
}

func (e *Engine) restoreOwnedSuspension(
	ctx context.Context,
	managed model.ManagedAccount,
	account model.UpstreamAccount,
) (model.UpstreamAccount, error) {
	if !managed.ManagedSuspended || account.Schedulable {
		return account, nil
	}
	if account.Status != "active" {
		return model.UpstreamAccount{}, errors.New("账号状态不是 active")
	}
	return e.core.SetSchedulable(ctx, account.ID, true)
}

func (e *Engine) isRunning(accountID int64) bool {
	e.mu.Lock()
	defer e.mu.Unlock()
	_, exists := e.running[accountID]
	return exists
}

// claimAccountOperationLocked reserves one account for either a detection run
// or a manual scheduling update. The caller must hold e.mu.
func (e *Engine) claimAccountOperationLocked(accountID int64) error {
	if _, exists := e.running[accountID]; exists {
		return ErrAlreadyRunning
	}
	e.running[accountID] = struct{}{}
	return nil
}

func (e *Engine) clearRunning(accountID int64) {
	e.mu.Lock()
	delete(e.running, accountID)
	e.mu.Unlock()
}

func resultID(accountID int64, now time.Time) string {
	return strconv.FormatInt(accountID, 10) + "-" + strconv.FormatInt(now.UnixNano(), 10)
}

func truncate(value string, max int) string {
	value = strings.TrimSpace(value)
	if len(value) <= max {
		return value
	}
	return value[:max]
}

func appendMessage(current, extra string) string {
	current = strings.TrimSpace(current)
	extra = strings.TrimSpace(extra)
	if current == "" {
		return truncate(extra, 500)
	}
	return truncate(current+"; "+extra, 500)
}
