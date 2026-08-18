(() => {
  'use strict'

  const TOKEN_KEY = 'sub2api-auto-scheduler-token'
  const COLLAPSED_GROUPS_KEY = 'sub2api-auto-scheduler-collapsed-groups'
  const CHANNEL_SLOW_MS = 6000
  const DETAIL_PAGE_SIZE = 10
  const state = {
    token: '',
    user: null,
    groups: [],
    accounts: [],
    groupProtectionDefaults: {},
    groupBalanceThresholds: {},
    notificationsAvailable: false,
    notificationSettings: null,
    defaultPolicy: null,
    editingID: null,
    directProbeBusy: false,
    deletingID: null,
    search: '',
    status: 'all',
    loading: false,
    bindingGroupID: null,
    bindingOriginal: new Set(),
    bindingDraft: new Set(),
    bindingSearch: '',
    bindingSaving: false,
    bindingTrigger: null,
    detailGroupKey: null,
    detailKind: 'oauth',
    detailPage: 1,
    usageCache: new Map(),
    collapsedGroups: new Set(),
    automationBusy: new Set(),
    protectionBusy: new Set(),
    protectionTarget: null,
    groupProtectionTarget: null,
    groupBalanceAlertTarget: null,
    notificationBusy: false,
    bindingAction: null,
    activeTab: 'groups',
    upstreamWorkspace: null,
    pollTimer: null,
    toastTimer: null,
  }

  const elements = {}

  document.addEventListener('DOMContentLoaded', init)

  async function init() {
    cacheElements()
    captureEmbedContext()
    state.upstreamWorkspace = window.createUpstreamWorkspace({ api, showToast })
    bindEvents()
    if (!state.token) {
      showAuthError('未收到登录凭证，请从 Sub2API 管理后台打开此页面')
      return
    }
    try {
      const session = await api('/api/session')
      state.user = session.user
      state.defaultPolicy = session.default_policy
      state.notificationsAvailable = Boolean(session.notifications_available)
      state.upstreamWorkspace.setCredentialsEnabled(session.credentials_enabled)
      elements.registerTabButton.hidden = !session.public_url
      elements.notificationSettingsButton.hidden = !state.notificationsAvailable
      elements.authState.hidden = true
      elements.app.hidden = false
      await loadOverview()
      state.pollTimer = window.setInterval(() => {
        if (document.visibilityState === 'visible' && !document.querySelector('dialog[open]')) {
          void loadOverview(true)
          if (state.activeTab === 'upstreams') void state.upstreamWorkspace.refresh(true)
        }
      }, 10000)
    } catch (error) {
      showAuthError(error.message || '管理员身份验证失败')
    }
  }

  function cacheElements() {
    const ids = [
      'auth-state', 'auth-message', 'app', 'sync-label', 'register-tab-button', 'refresh-button', 'add-button',
      'search-input', 'status-filter', 'group-list', 'empty-state', 'no-match-state', 'metric-groups',
      'metric-accounts', 'metric-enabled', 'metric-auto-stopped', 'config-dialog', 'config-form', 'config-title',
      'account-select', 'interval-input', 'model-input', 'latency-input', 'failure-input', 'recovery-input',
      'enabled-input', 'balance-alert-input', 'prompt-input', 'form-error', 'save-button', 'binding-dialog', 'binding-group-name',
      'direct-probe-control', 'direct-probe-state', 'direct-probe-message', 'direct-probe-authorize-button', 'direct-probe-revoke-button',
      'binding-search-input', 'binding-change-count', 'binding-list', 'binding-error', 'binding-save-button',
      'account-detail-dialog', 'account-detail-title', 'account-detail-group-name', 'account-detail-list',
      'account-detail-page-label', 'account-detail-prev', 'account-detail-next', 'delete-dialog',
      'delete-account-name', 'confirm-delete-button', 'result-dialog', 'result-account-name', 'result-details', 'toast',
      'protection-dialog', 'protection-form', 'protection-account-name', 'protection-multiplier-input',
      'protection-final-preview', 'protection-error', 'protection-save-button', 'binding-action-dialog',
      'binding-action-title', 'binding-action-account-name', 'binding-action-message', 'binding-action-error',
      'binding-action-confirm-button',
      'group-protection-dialog', 'group-protection-form', 'group-protection-name', 'group-protection-multiplier-input',
      'group-protection-error', 'group-protection-save-button', 'group-protection-clear-button',
      'group-balance-alert-dialog', 'group-balance-alert-form', 'group-balance-alert-name', 'group-balance-alert-input',
      'group-balance-alert-error', 'group-balance-alert-save-button', 'group-balance-alert-clear-button',
      'notification-settings-button', 'notification-settings-dialog', 'notification-settings-form', 'notification-settings-status',
      'notification-enabled-input', 'notification-endpoint-input', 'notification-device-key-input',
      'notification-encryption-key-input', 'notification-basic-user-input', 'notification-basic-password-input',
      'notification-settings-error', 'notification-clear-button', 'notification-test-button', 'notification-save-button',
      'groups-tab', 'upstreams-tab', 'groups-panel', 'upstreams-panel', 'groups-header-actions', 'upstreams-header-actions'
    ]
    for (const id of ids) elements[toCamel(id)] = document.getElementById(id)
  }

  function captureEmbedContext() {
    const url = new URL(window.location.href)
    const queryToken = url.searchParams.get('token') || ''
    if (queryToken) sessionStorage.setItem(TOKEN_KEY, queryToken)
    state.token = queryToken || sessionStorage.getItem(TOKEN_KEY) || ''
    state.collapsedGroups = readCollapsedGroups()
    document.documentElement.dataset.theme = url.searchParams.get('theme') === 'dark' ? 'dark' : 'light'
    for (const key of ['token', 'user_id', 'src_url']) url.searchParams.delete(key)
    window.history.replaceState(null, '', url.pathname + (url.search ? url.search : ''))
  }

  function bindEvents() {
    elements.groupsTab.addEventListener('click', () => setWorkspaceTab('groups'))
    elements.upstreamsTab.addEventListener('click', () => setWorkspaceTab('upstreams'))
    for (const tab of [elements.groupsTab, elements.upstreamsTab]) tab.addEventListener('keydown', handleWorkspaceTabKeydown)
    elements.refreshButton.addEventListener('click', () => loadOverview())
    elements.addButton.addEventListener('click', () => openCreateDialog())
    elements.registerTabButton.addEventListener('click', registerTab)
    elements.notificationSettingsButton.addEventListener('click', openNotificationSettings)
    elements.searchInput.addEventListener('input', (event) => {
      state.search = event.target.value.trim().toLocaleLowerCase()
      render()
    })
    elements.statusFilter.addEventListener('change', (event) => {
      state.status = event.target.value
      render()
    })
    elements.groupList.addEventListener('click', handleGroupClick)
    elements.groupList.addEventListener('change', handleGroupChange)
    elements.configForm.addEventListener('submit', saveConfig)
    elements.directProbeAuthorizeButton.addEventListener('click', authorizeDirectProbe)
    elements.directProbeRevokeButton.addEventListener('click', revokeDirectProbe)
    elements.confirmDeleteButton.addEventListener('click', deleteConfig)
    elements.protectionForm.addEventListener('submit', saveProtection)
    elements.groupProtectionForm.addEventListener('submit', saveGroupProtection)
    elements.groupProtectionClearButton.addEventListener('click', clearGroupProtection)
    elements.groupBalanceAlertForm.addEventListener('submit', saveGroupBalanceAlert)
    elements.groupBalanceAlertClearButton.addEventListener('click', clearGroupBalanceAlert)
    elements.notificationSettingsForm.addEventListener('submit', saveNotificationSettings)
    elements.notificationTestButton.addEventListener('click', testNotificationSettings)
    elements.notificationClearButton.addEventListener('click', clearNotificationSettings)
    elements.bindingActionConfirmButton.addEventListener('click', confirmBindingAction)
    elements.bindingSearchInput.addEventListener('input', (event) => {
      state.bindingSearch = event.target.value.trim().toLocaleLowerCase()
      renderBindingList()
    })
    elements.bindingList.addEventListener('change', handleBindingChange)
    elements.bindingSaveButton.addEventListener('click', saveBindings)
    elements.accountDetailPrev.addEventListener('click', () => changeDetailPage(-1))
    elements.accountDetailNext.addEventListener('click', () => changeDetailPage(1))
    document.querySelectorAll('[data-close-dialog]').forEach((button) => {
      button.addEventListener('click', () => document.getElementById(button.dataset.closeDialog).close())
    })
    for (const dialog of document.querySelectorAll('dialog')) {
      dialog.addEventListener('click', (event) => {
        if ((dialog === elements.bindingDialog && state.bindingSaving) || dialog.dataset.busy === 'true') return
        if (event.target === dialog) dialog.close()
      })
    }
    elements.bindingDialog.addEventListener('cancel', (event) => {
      if (state.bindingSaving) event.preventDefault()
    })
    elements.bindingDialog.addEventListener('close', restoreBindingFocus)
    elements.protectionDialog.addEventListener('close', () => { state.protectionTarget = null })
    elements.groupProtectionDialog.addEventListener('close', () => { state.groupProtectionTarget = null })
    elements.groupBalanceAlertDialog.addEventListener('close', () => { state.groupBalanceAlertTarget = null })
    elements.bindingActionDialog.addEventListener('close', () => {
      if (elements.bindingActionDialog.dataset.busy !== 'true') state.bindingAction = null
    })
    document.addEventListener('keydown', (event) => {
      if (event.key !== 'Escape') return
      const dialogs = [...document.querySelectorAll('dialog[open]')]
      const activeDialog = dialogs.at(-1)
      if (!activeDialog) return
      event.preventDefault()
      if ((activeDialog === elements.bindingDialog && state.bindingSaving) || activeDialog.dataset.busy === 'true') return
      activeDialog.close()
    })
  }

  function setWorkspaceTab(tabName) {
    const upstreamsActive = tabName === 'upstreams'
    state.activeTab = upstreamsActive ? 'upstreams' : 'groups'
    elements.groupsTab.setAttribute('aria-selected', String(!upstreamsActive))
    elements.groupsTab.tabIndex = upstreamsActive ? -1 : 0
    elements.upstreamsTab.setAttribute('aria-selected', String(upstreamsActive))
    elements.upstreamsTab.tabIndex = upstreamsActive ? 0 : -1
    elements.groupsPanel.hidden = upstreamsActive
    elements.upstreamsPanel.hidden = !upstreamsActive
    elements.groupsHeaderActions.hidden = upstreamsActive
    elements.upstreamsHeaderActions.hidden = !upstreamsActive
    if (upstreamsActive) void state.upstreamWorkspace.activate()
  }

  function handleWorkspaceTabKeydown(event) {
    const tabs = [elements.groupsTab, elements.upstreamsTab]
    const index = tabs.indexOf(event.currentTarget)
    let nextIndex = index
    if (event.key === 'ArrowRight') nextIndex = (index + 1) % tabs.length
    else if (event.key === 'ArrowLeft') nextIndex = (index - 1 + tabs.length) % tabs.length
    else if (event.key === 'Home') nextIndex = 0
    else if (event.key === 'End') nextIndex = tabs.length - 1
    else return
    event.preventDefault()
    const nextTab = tabs[nextIndex]
    setWorkspaceTab(nextTab === elements.upstreamsTab ? 'upstreams' : 'groups')
    nextTab.focus()
  }

  async function loadOverview(silent = false) {
    if (state.loading) return false
    state.loading = true
    setRefreshLoading(true)
    try {
      const response = await api('/api/overview')
      state.groups = [...(response.groups || [])].sort((a, b) => (a.sort_order - b.sort_order) || (a.id - b.id))
      state.accounts = response.accounts || []
      state.groupProtectionDefaults = response.group_protection_defaults || {}
      state.groupBalanceThresholds = response.group_balance_thresholds || {}
      elements.syncLabel.textContent = `已同步 ${formatDateTime(new Date())}`
      render()
      return true
    } catch (error) {
      if (!silent) showToast(error.message || '加载失败', true)
      return false
    } finally {
      state.loading = false
      setRefreshLoading(false)
    }
  }

  function render() {
    renderMetrics()
    const views = buildGroupViews()
    const filtered = views.map(filterGroupView).filter(Boolean)
    const hasData = state.groups.length > 0 || state.accounts.length > 0
    elements.emptyState.hidden = hasData
    elements.noMatchState.hidden = !hasData || filtered.length > 0
    elements.groupList.hidden = filtered.length === 0
    elements.groupList.innerHTML = filtered.map(renderGroup).join('')
  }

  function renderMetrics() {
    elements.metricGroups.textContent = formatInteger(state.groups.length)
    elements.metricAccounts.textContent = formatInteger(state.accounts.length)
    elements.metricEnabled.textContent = formatInteger(state.accounts.filter((account) => accountState(account).key === 'enabled').length)
    elements.metricAutoStopped.textContent = formatInteger(state.accounts.filter((account) => account.config?.managed_suspended && !account.schedulable).length)
  }

  function buildGroupViews() {
    const validGroupIDs = new Set(state.groups.map((group) => group.id))
    const views = state.groups.map((group) => ({
      key: String(group.id),
      group,
      accounts: state.accounts.filter((account) => membershipIDs(account).includes(group.id)),
      synthetic: false,
    }))
    const ungrouped = state.accounts.filter((account) => !membershipIDs(account).some((id) => validGroupIDs.has(id)))
    if (ungrouped.length > 0) {
      views.push({
        key: 'ungrouped',
        group: {
          id: 0,
          name: '未分组账号',
          description: '',
          platform: 'all',
          status: 'active',
          account_count: ungrouped.length,
          active_account_count: ungrouped.filter((account) => accountState(account).key === 'enabled').length,
          rate_limited_account_count: ungrouped.filter((account) => accountState(account).key === 'limited').length,
        },
        accounts: ungrouped,
        synthetic: true,
      })
    }
    return views
  }

  function filterGroupView(view) {
    const groupHaystack = `${view.group.name || ''} ${view.group.platform || ''} ${view.group.description || ''}`.toLocaleLowerCase()
    const groupMatchesSearch = Boolean(state.search && groupHaystack.includes(state.search))
    const accounts = view.accounts.filter((account) => accountMatchesFilters(account, groupMatchesSearch, view.group.id))
    const noFilters = !state.search && state.status === 'all'
    if (!noFilters && accounts.length === 0) return null
    return { ...view, visibleAccounts: noFilters ? view.accounts : accounts }
  }

  function accountMatchesFilters(account, groupMatchesSearch = false, groupID = 0) {
    const accountView = groupAccountState(account, groupID)
    if (state.status !== 'all' && accountView.key !== state.status) return false
    if (!state.search || groupMatchesSearch) return true
    const haystack = `${account.name || ''} ${account.platform || ''} ${account.type || ''} ${account.id}`.toLocaleLowerCase()
    return haystack.includes(state.search)
  }

  function renderGroup(view) {
    const visible = sortAccounts(view.visibleAccounts || view.accounts, view.group.id)
    const apiKeys = visible.filter(isAPIKey)
    const oauthAccounts = visible.filter(isOAuthLike)
    const otherAccounts = visible.filter((account) => !isAPIKey(account) && !isOAuthLike(account))
    const total = view.accounts.length
    const enabled = view.accounts.filter((account) => groupAccountState(account, view.group.id).key === 'enabled').length
    const limited = view.accounts.filter((account) => accountState(account).key === 'limited').length
    const groupKey = String(view.key)
    const contentID = `group-content-${groupKey.replace(/[^a-zA-Z0-9_-]/g, '-')}`
    const filterActive = Boolean(state.search) || state.status !== 'all'
    const collapsed = !filterActive && state.collapsedGroups.has(groupKey)
    const toggleTitle = filterActive ? '筛选期间保持展开' : (collapsed ? '展开分组' : '收起分组')
    const groupStatus = view.group.status === 'active' ? '' : '<span class="status-tag inactive">分组停用</span>'
    const groupProtection = groupProtectionFor(view.group.id)
    const groupProtectionBadge = groupProtection
      ? `<span class="group-protection-badge" title="账号未设置账号级保护时继承此分组默认值">分组保护 ${escapeHTML(formatMultiplier(groupProtection.protection_multiplier))}x</span>`
      : ''
    const groupBalanceThreshold = groupBalanceThresholdFor(view.group.id)
    const groupBalanceBadge = Number.isFinite(groupBalanceThreshold)
      ? `<span class="group-balance-alert-badge" title="账号未设置账号级阈值时继承此分组默认值">余额告警 ${escapeHTML(formatCurrency(groupBalanceThreshold))}</span>`
      : ''
    const bindButton = view.synthetic ? '' : `
      <button class="summary-button group-manage-button" type="button" data-action="bind-group" data-group-key="${escapeAttr(view.key)}" title="管理当前分组的 API Key 账号">
        <svg class="icon"><use href="#icon-users"/></svg>
        <span>管理账号</span>
      </button>`
    const groupProtectionButton = view.synthetic ? '' : `
      <button class="summary-button group-protection-button" type="button" data-action="edit-group-protection" data-group-key="${escapeAttr(view.key)}" title="设置当前分组默认倍率保护">
        <svg class="icon"><use href="#icon-shield"/></svg>
        <span>${groupProtection ? '编辑分组保护' : '设置分组保护'}</span>
      </button>`
    const groupBalanceButton = view.synthetic ? '' : `
      <button class="summary-button group-balance-alert-button" type="button" data-action="edit-group-balance-alert" data-group-key="${escapeAttr(view.key)}" title="设置当前分组默认余额告警阈值">
        <svg class="icon"><use href="#icon-bell"/></svg>
        <span>${Number.isFinite(groupBalanceThreshold) ? '编辑余额告警' : '设置余额告警'}</span>
      </button>`
    const detailButtons = [
      oauthAccounts.length ? renderDetailButton(view.key, 'oauth', `OAuth ${formatInteger(oauthAccounts.length)}`) : '',
      otherAccounts.length ? renderDetailButton(view.key, 'other', `其他 ${formatInteger(otherAccounts.length)}`) : '',
    ].join('')
    return `
      <article class="group-section ${collapsed ? 'collapsed' : ''}" data-group-key="${escapeAttr(view.key)}">
        <header class="group-header">
          <div class="group-heading">
            <button class="icon-button group-toggle" type="button" data-action="toggle-group" data-group-key="${escapeAttr(groupKey)}" title="${escapeAttr(toggleTitle)}" aria-label="${escapeAttr(toggleTitle)}" aria-expanded="${collapsed ? 'false' : 'true'}" aria-controls="${escapeAttr(contentID)}" ${filterActive ? 'disabled' : ''}>
              <svg class="icon"><use href="#icon-chevron-right"/></svg>
            </button>
            <div class="group-identity">
              <div class="group-title-line">
                <h2>${escapeHTML(view.group.name || `分组 ${view.group.id}`)}</h2>
                <span class="platform-tag">${escapeHTML(view.group.platform || 'all')}</span>
                ${groupStatus}${groupProtectionBadge}${groupBalanceBadge}
              </div>
              <p>${escapeHTML(view.group.description || `#${view.group.id || 'ungrouped'}`)}</p>
            </div>
          </div>
          <div class="group-stats" aria-label="分组账号状态">
            <span><strong class="text-success">${formatInteger(enabled)}</strong> / ${formatInteger(total)} 可用</span>
            <span>${formatInteger(apiKeys.length)} API Key</span>
            ${limited ? `<span class="text-warning">${formatInteger(limited)} 临时受限</span>` : ''}
          </div>
          <div class="group-actions">${detailButtons}${groupBalanceButton}${groupProtectionButton}${bindButton}</div>
        </header>
        <div id="${escapeAttr(contentID)}" class="api-key-table" ${collapsed ? 'hidden' : ''}>
          <div class="api-key-header" aria-hidden="true"><span>账号状态</span><span>用量与管理员额度</span><span>检测规则与最终倍率</span><span>自动调度 / 操作</span></div>
          ${apiKeys.length ? apiKeys.map((account) => renderAPIKeyAccount(account, view.group.id)).join('') : '<div class="group-empty">暂无 API Key 账号</div>'}
        </div>
      </article>`
  }

  function renderDetailButton(groupKey, kind, label) {
    return `<button class="summary-button" type="button" data-action="open-detail" data-group-key="${escapeAttr(groupKey)}" data-kind="${escapeAttr(kind)}"><svg class="icon"><use href="#icon-users"/></svg><span>${escapeHTML(label)}</span></button>`
  }

  function renderAPIKeyAccount(account, groupID) {
    const config = account.config || null
    const protection = protectionFor(account, groupID)
    const scheduling = groupAccountState(account, groupID)
    const policy = config?.policy
    const latest = config?.history?.[0]
    const automation = automationState(account)
    const protectionKey = relationKey(groupID, account.id)
    const protectionBusy = state.protectionBusy.has(protectionKey)
    const policyText = policy
      ? `<strong>${escapeHTML(policy.model || '平台默认模型')}</strong><span>每 ${formatInterval(policy.interval_seconds)} · 上限 ${formatSeconds(policy.latency_limit_ms)}</span><span>${policy.enabled ? '自动规则启用' : '自动规则停用'} · ${policy.failure_threshold} 次暂停 / ${policy.recovery_threshold} 次恢复</span>${renderProbeSummary(config.probe)}`
      : '<strong>未配置自动调度</strong><span>开启开关后使用默认检测规则</span>'
    const actions = config ? `
      <button class="icon-button ${config.running ? 'spin' : ''}" type="button" data-action="run" title="立即检测" aria-label="立即检测" ${config.running ? 'disabled' : ''}><svg class="icon"><use href="${config.running ? '#icon-refresh' : '#icon-play'}"/></svg></button>
      <button class="icon-button" type="button" data-action="edit" title="编辑检测规则" aria-label="编辑检测规则"><svg class="icon"><use href="#icon-edit"/></svg></button>
      <button class="icon-button danger-tool" type="button" data-action="delete" title="删除检测配置" aria-label="删除检测配置" ${config.running ? 'disabled' : ''}><svg class="icon"><use href="#icon-trash"/></svg></button>` : `
      <button class="icon-button" type="button" data-action="create" title="配置状态检测" aria-label="配置状态检测"><svg class="icon"><use href="#icon-plus"/></svg></button>`
    const relationActions = groupID > 0 ? `<div class="binding-row-actions">
      <button class="button secondary relation-button" type="button" data-action="edit-protection" ${protectionBusy ? 'disabled' : ''}>${protection && !protection.inherited ? '编辑账号保护倍率' : '设置保护倍率（账号级）'}</button>
      ${protection?.status === 'rate_protected' && !protection.inherited ? `<button class="button warning relation-button" type="button" data-action="release-protection" ${protectionBusy ? 'disabled' : ''}>解除倍率保护</button>` : ''}
      <button class="button danger relation-button" type="button" data-action="remove-binding" ${protectionBusy ? 'disabled' : ''}>移除绑定</button>
    </div>` : ''
    const latestText = latest ? `${statusLabel(latest.status)} · ${formatMilliseconds(latest.latency_ms)}` : '尚未检测'
    const nextText = policy?.enabled && config.next_check_at ? `下次 ${formatRelative(config.next_check_at)}` : '—'
    return `
      <section class="api-key-row" data-account-id="${account.id}" data-group-id="${groupID}">
        <div class="account-cell">
          <div class="account-name-line"><strong>${escapeHTML(account.name || `账号 ${account.id}`)}</strong><span>#${account.id}</span></div>
          <div class="account-meta"><span class="platform-tag">${escapeHTML(account.platform || 'unknown')}</span><span>API Key</span></div>
          <div class="account-state ${escapeAttr(scheduling.tone)}"><i class="status-dot"></i><span><strong>${escapeHTML(scheduling.label)}</strong><small title="${escapeAttr(scheduling.reason)}">${escapeHTML(scheduling.reason)}</small></span></div>
        </div>
        <div class="usage-cell">${renderTodayUsage(account)}${renderDetectionStats(config)}${renderQuota(account)}</div>
        <div class="policy-cell">${policyText}${renderBalanceAlertSummary(account, groupID)}${renderFinalMultiplier(account, protection)}<span class="latest-check">${escapeHTML(latestText)} · ${escapeHTML(nextText)}</span></div>
        <div class="row-actions">
          <div class="automation-control ${automation.busy ? 'busy' : ''}" title="${escapeAttr(automation.reason)}">
            <span class="automation-copy"><strong>自动调度</strong><small>${escapeHTML(automation.label)}</small></span>
            <label class="mini-switch">
              <span class="sr-only">${escapeHTML(account.name || `账号 ${account.id}`)}自动调度</span>
              <input type="checkbox" role="switch" data-action="automation-toggle" ${automation.enabled ? 'checked' : ''} ${automation.disabled ? 'disabled' : ''} aria-label="${escapeAttr(`${account.name || `账号 ${account.id}`}自动调度`)}">
              <i aria-hidden="true"></i>
            </label>
          </div>
          <div class="row-tools">${actions}</div>
          ${relationActions}
        </div>
        <div class="history-row">
          <span class="history-label">最近 50 次</span>
          <div class="history-bars" aria-label="${escapeAttr(account.name || `账号 ${account.id}`)}最近 50 次检测状态">${renderHistory(account)}</div>
          <span class="history-range">过去 → 现在</span>
        </div>
      </section>`
  }

  function renderProbeSummary(probe) {
    if (!probe || probe.source !== 'direct') return '<span class="probe-summary legacy">来源：Sub2API 检测</span>'
    const state = String(probe.authorization_state || 'needs_reauthorization')
    const labels = {
      authorized: '来源：直连上游探测',
      needs_reauthorization: '直连授权需要更新',
      unsupported: '直连探测暂不支持',
      credentials_unavailable: '直连探测加密未配置',
      authorization_missing: '直连授权缺失',
    }
    const tone = state === 'authorized' ? 'authorized' : 'attention'
    const detail = probe.action_message || labels[state] || '直连探测需要处理'
    return `<span class="probe-summary ${tone}" title="${escapeAttr(detail)}">${escapeHTML(labels[state] || '直连探测需要处理')}</span>`
  }

  function renderTodayUsage(account) {
    const usage = account.today_usage || {}
    return `<strong>${formatInteger(usage.requests || 0)} 请求 · ${formatCompact(usage.tokens || 0)} tokens</strong><span>${formatCurrency(usage.cost || 0)} 今日成本</span>`
  }

  function renderDetectionStats(config) {
    const stats = config?.detection_stats
    if (!stats || !Number(stats.requests)) {
      return '<div class="detection-consumption"><span class="data-label">状态检测消耗（1 倍率）</span><strong>尚无检测消耗</strong></div>'
    }
    const totalTokens = ['input_tokens', 'output_tokens', 'cache_read_tokens', 'cache_write_tokens']
      .reduce((sum, key) => sum + (Number(stats[key]) || 0), 0)
    const knownChecks = Number(stats.known_cost_checks) || 0
    const requests = Number(stats.requests) || 0
    const costText = knownChecks > 0 ? formatProbeCost(stats.known_cost || 0) : '金额不可用'
    const detail = knownChecks < requests ? `其中 ${formatInteger(knownChecks)} 次取得模型定价` : '全部检测均已取得模型定价'
    return `<div class="detection-consumption" title="${escapeAttr(detail)}"><span class="data-label">状态检测消耗（1 倍率）</span><strong>${formatInteger(requests)} 次 · ${formatCompact(totalTokens)} tokens</strong><span>${escapeHTML(costText)} · ${escapeHTML(detail)}</span></div>`
  }

  function renderQuota(account) {
    const quotas = [
      quotaDimension('日额度', account.quota_daily_used, account.quota_daily_limit),
      quotaDimension('周额度', account.quota_weekly_used, account.quota_weekly_limit),
      quotaDimension('总额度', account.quota_used, account.quota_limit),
    ].filter(Boolean)
    const balance = account.admin_balance || {}
    const configured = typeof balance.configured === 'boolean' ? balance.configured : quotas.length > 0
    const totalLimit = Number(account.quota_limit)
    const totalUsed = Number(account.quota_used) || 0
    const projectedRemaining = Number(balance.remaining)
    const fallbackRemaining = Number.isFinite(totalLimit) && totalLimit > 0 ? Math.max(totalLimit - totalUsed, 0) : NaN
    const remaining = Number.isFinite(projectedRemaining) ? projectedRemaining : fallbackRemaining
    const exhausted = Array.isArray(balance.exhausted_dimensions) ? balance.exhausted_dimensions : []
    const insufficient = Boolean(balance.insufficient) || exhausted.length > 0
    const dimensionLabels = { daily: '日额度', weekly: '周额度', total: '总额度' }
    const exhaustedLabel = exhausted.map((dimension) => dimensionLabels[dimension]).filter(Boolean).join('、')
    const source = balance.managed ? '<span class="admin-balance-source">上游同步</span>' : ''
    let tone = 'neutral'
    let value = '未配置（不限）'
    let note = '当前没有管理员额度上限'
    if (insufficient) {
      tone = 'insufficient'
      value = '余额不足'
      note = exhaustedLabel ? `${exhaustedLabel}已耗尽` : '管理员配置额度已耗尽'
    } else if (balance.unlimited) {
      tone = 'available'
      value = '不限额度'
      note = balance.managed ? '上游同步为不限额度' : '管理员未配置额度上限'
    } else if (Number.isFinite(remaining)) {
      tone = 'available'
      value = formatCurrency(remaining)
      note = '剩余可用额度'
    } else if (configured) {
      tone = 'available'
      value = '额度可用'
      note = '未配置总额度，请查看周期额度'
    }
    const detail = quotas.length ? `<dl class="quota-list">${quotas.join('')}</dl>` : ''
    const label = insufficient ? `管理员余额不足，${note}` : `管理员余额：${value}，${note}`
    return `<div class="quota-summary"><div class="admin-balance-card ${tone}" aria-label="${escapeAttr(label)}"><div class="admin-balance-heading"><span>管理员余额</span>${source}</div><strong class="admin-balance-value">${escapeHTML(value)}</strong><span class="admin-balance-note">${escapeHTML(note)}</span></div>${detail}</div>`
  }

  function quotaDimension(label, used, limit) {
    if (!Number.isFinite(Number(limit)) || Number(limit) <= 0) return ''
    const normalizedUsed = Number(used) || 0
    const detail = `${label}已用 ${formatCurrency(normalizedUsed)}，管理员配置 ${formatCurrency(Number(limit))}`
    return `<div title="${escapeAttr(detail)}"><dt>${escapeHTML(label)}</dt><dd><span>${escapeHTML(formatCurrency(normalizedUsed))}</span><i aria-hidden="true">/</i><strong>${escapeHTML(formatCurrency(Number(limit)))}</strong></dd></div>`
  }

  function renderFinalMultiplier(account, protection = null) {
    const projection = account.upstream_final_multiplier || {}
    const protectedFinal = Number(protection?.final_multiplier)
    const finalMultiplier = Number.isFinite(protectedFinal) ? protectedFinal : Number(projection.final_multiplier)
    const finalAvailable = Number.isFinite(finalMultiplier) && (protection || projection.status === 'available')
    let finalBlock = ''
    if (finalAvailable) {
      const recharge = Number(projection.recharge_rate_cny_per_usd)
      const group = Number(projection.group_multiplier)
      const detail = Number.isFinite(recharge) && Number.isFinite(group)
        ? `充值 ${formatMultiplier(recharge)} × 分组 ${formatMultiplier(group)}`
        : '来自上游列表的已绑定 Key'
      finalBlock = `<div class="multiplier-item final-multiplier" title="${escapeAttr(detail)}"><span class="data-label">最终倍率</span><strong>${escapeHTML(formatMultiplier(finalMultiplier))}x</strong><span>${escapeHTML(detail)}</span></div>`
    } else {
      const status = String(projection.status || 'unavailable')
      const labels = {
        unbound: '未绑定上游 Key',
        stale: '上游 Key 绑定已失效',
        ambiguous: '存在多个有效上游 Key 绑定',
        recharge_unset: '请在上游列表设置充值倍率',
        group_multiplier_unknown: '上游尚未同步分组倍率',
        invalid_upstream: '本地上游地址无效',
        unavailable: '上游列表暂不可用',
      }
      const label = protection?.last_error || labels[status] || '最终倍率未计算'
      finalBlock = `<div class="multiplier-item final-multiplier unavailable" title="${escapeAttr(label)}"><span class="data-label">最终倍率</span><strong>未计算</strong><span>${escapeHTML(label)}</span></div>`
    }
    const protectionSource = protection?.inherited ? '分组默认' : '账号级保护'
    const protectionBlock = protection
      ? `<div class="multiplier-item protection-multiplier ${protection.status === 'rate_protected' ? 'exceeded' : ['multiplier_unavailable', 'rebind_pending'].includes(protection.status) ? 'unavailable' : ''}"><span class="data-label">保护倍率 · ${escapeHTML(protectionSource)}</span><strong>${escapeHTML(formatMultiplier(protection.protection_multiplier))}x</strong><span>${protection.status === 'rate_protected' ? '已触发保护' : protection.status === 'multiplier_unavailable' ? '最终倍率不可用，保护不生效' : protection.status === 'rebind_pending' ? '等待自动回绑' : '保护中'}</span></div>`
      : '<div class="multiplier-item protection-multiplier unset"><span class="data-label">保护倍率</span><strong>未设置</strong><span>沿用原绑定逻辑</span></div>'
    return `<div class="multiplier-pair">${finalBlock}${protectionBlock}</div>`
  }

  function automationState(account) {
    const config = account.config || null
    const busy = state.automationBusy.has(account.id)
    if (account.status !== 'active') {
      return { enabled: false, disabled: true, busy, label: account.status === 'error' ? '账号异常' : '账号未启用', reason: '仅 active 账号可以启用自动调度' }
    }
    const enabled = Boolean(config?.policy?.enabled && (account.schedulable || config.managed_suspended))
    if (busy) return { enabled, disabled: true, busy: true, label: '保存中', reason: '正在更新自动调度状态' }
    if (config?.running) return { enabled, disabled: true, busy: false, label: '检测进行中', reason: '检测结束后可修改自动调度状态' }
    if (enabled && config?.managed_suspended) {
      return { enabled: true, disabled: false, busy: false, label: '自动暂停，持续检测', reason: '健康检测已暂停调度，连续检测正常后会自动恢复' }
    }
    if (enabled) return { enabled: true, disabled: false, busy: false, label: '已接管', reason: '账号由健康检测自动管理调度状态' }
    if (!account.schedulable) {
      return { enabled: false, disabled: false, busy: false, label: '管理员停止', reason: '开启后将恢复账号调度并交给健康检测管理' }
    }
    return { enabled: false, disabled: false, busy: false, label: config ? '自动规则停用' : '未配置', reason: '开启后由健康检测自动管理调度状态' }
  }

  function renderHistory(account) {
    const history = [...(account.config?.history || [])].slice(0, 50).reverse()
    const padding = Array.from({ length: 50 - history.length }, () => '<span class="history-bar empty"></span>')
    const bars = history.map((result) => {
      const failed = result.status === 'failed' || result.status === 'error'
      const balanceFailure = result.failure_kind === 'balance_insufficient'
      const title = failed
        ? `${formatDateTime(result.checked_at)} · ${balanceFailure ? '余额不足 · ' : ''}${result.message || statusLabel(result.status)}`
        : `${formatDateTime(result.checked_at)} · 耗时 ${formatMilliseconds(result.latency_ms)}`
      const color = failed
        ? 'failed'
        : result.status === 'skipped'
          ? 'skipped'
          : (result.status === 'degraded' || Number(result.latency_ms) >= CHANNEL_SLOW_MS ? 'slow' : 'success')
      return `<button type="button" class="history-bar ${color} ${balanceFailure ? 'balance-insufficient' : ''}" data-action="result" data-result-id="${escapeAttr(result.id)}" title="${escapeAttr(title)}" aria-label="${escapeAttr(title)}"></button>`
    })
    return padding.concat(bars).join('')
  }

  function accountState(account) {
    if (account.status === 'error') {
      return { key: 'error', tone: 'danger', label: '账号异常', reason: account.error_message || '账号状态为 error' }
    }
    if (account.status !== 'active') {
      return { key: 'inactive', tone: 'neutral', label: '账号停用', reason: '账号状态由管理员设为 inactive' }
    }
    if (account.config?.last_failure_kind === 'balance_insufficient') {
      const key = account.config?.managed_suspended && !account.schedulable ? 'auto' : account.schedulable ? 'enabled' : 'manual'
      return { key, tone: 'danger', label: '余额不足导致检测失败', reason: account.config.last_error || '直连上游返回额度或余额不足' }
    }
    if (account.config?.managed_suspended && !account.schedulable) {
      return { key: 'auto', tone: 'warning', label: '检测自动停止', reason: account.config.last_error || '连续检测异常达到暂停阈值' }
    }
    if (!account.schedulable) {
      return { key: 'manual', tone: 'neutral', label: '管理员停止', reason: '管理员手动关闭账号调度' }
    }
    const now = Date.now()
    const temporary = [
      [account.temp_unschedulable_until, account.temp_unschedulable_reason || '临时不可调度'],
      [account.overload_until, '上游过载保护'],
      [account.rate_limit_reset_at, '上游速率限制'],
    ].find(([until]) => until && new Date(until).getTime() > now)
    if (temporary) {
      return { key: 'limited', tone: 'warning', label: '临时受限', reason: `${temporary[1]}，至 ${formatDateTime(temporary[0])}` }
    }
    if (account.auto_pause_on_expired && Number(account.expires_at) > 0 && Number(account.expires_at) * 1000 <= now) {
      return { key: 'limited', tone: 'warning', label: '已过期', reason: '账号凭证已到期并启用自动暂停' }
    }
    return { key: 'enabled', tone: 'success', label: '调度启用', reason: '账号当前可以参与调度' }
  }

  function groupAccountState(account, groupID) {
    const protection = protectionFor(account, groupID)
    if (protection?.status === 'rate_protected') {
      const finalValue = Number(protection.final_multiplier)
      const transition = protection.physical_bound ? (protection.last_error || '正在重试解除当前分组绑定') : '已解除当前分组绑定'
      const reason = Number.isFinite(finalValue)
        ? `最终倍率 ${formatMultiplier(finalValue)}x 超过保护倍率 ${formatMultiplier(protection.protection_multiplier)}x，${transition}`
        : `最终倍率超过保护倍率，${transition}`
      return { key: 'protected', tone: 'warning', label: '超过保护倍率', reason }
    }
    if (protection?.status === 'multiplier_unavailable') {
      return { key: 'protected', tone: 'warning', label: '保护倍率待确认', reason: protection.last_error || '当前最终倍率不可用，侧车保持上一次物理绑定状态' }
    }
    if (protection?.status === 'rebind_pending') {
      return { key: 'protected', tone: 'warning', label: '等待自动回绑', reason: protection.last_error || '最终倍率已恢复到保护范围，但实际分组绑定尚未恢复，侧车会继续重试' }
    }
    return accountState(account)
  }

  function protectionFor(account, groupID) {
    if (!account || !groupID) return null
    return account.group_protections?.[String(groupID)] || null
  }

  function groupProtectionFor(groupID) {
    if (!groupID) return null
    return state.groupProtectionDefaults?.[String(groupID)] || state.groupProtectionDefaults?.[groupID] || null
  }

  function groupBalanceThresholdFor(groupID) {
    if (!groupID) return NaN
    const value = Number(state.groupBalanceThresholds?.[String(groupID)] ?? state.groupBalanceThresholds?.[groupID])
    return Number.isFinite(value) && value >= 0 ? value : NaN
  }

  function effectiveBalanceThreshold(account, groupID) {
    const accountValue = Number(account?.config?.balance_alert_threshold)
    if (Number.isFinite(accountValue) && accountValue >= 0) return { value: accountValue, source: '账号级' }
    const groupValue = groupBalanceThresholdFor(groupID)
    if (Number.isFinite(groupValue)) return { value: groupValue, source: '分组默认' }
    return null
  }

  function renderBalanceAlertSummary(account, groupID) {
    const threshold = effectiveBalanceThreshold(account, groupID)
    if (!threshold) {
      return '<div class="balance-alert-summary unset"><span class="data-label">余额告警</span><strong>未设置</strong><span>不会推送余额阈值告警</span></div>'
    }
    return `<div class="balance-alert-summary"><span class="data-label">余额告警 · ${escapeHTML(threshold.source)}</span><strong>低于 ${escapeHTML(formatCurrency(threshold.value))}</strong><span>恢复后才会再次触发</span></div>`
  }

  function membershipIDs(account) {
    const source = Array.isArray(account.logical_group_ids) ? account.logical_group_ids : account.group_ids
    return [...new Set((source || []).map(Number).filter((id) => Number.isInteger(id) && id > 0))]
  }

  function relationKey(groupID, accountID) {
    return `${groupID}:${accountID}`
  }

  async function handleGroupClick(event) {
    const button = event.target.closest('[data-action]')
    if (!button || button.tagName === 'INPUT') return
    const action = button.dataset.action
    if (action === 'toggle-group') {
      toggleGroup(button.dataset.groupKey)
      return
    }
    if (action === 'bind-group') {
      openBindingDialog(Number(button.dataset.groupKey), button)
      return
    }
    if (action === 'edit-group-protection') {
      openGroupProtectionDialog(Number(button.dataset.groupKey))
      return
    }
    if (action === 'edit-group-balance-alert') {
      openGroupBalanceAlertDialog(Number(button.dataset.groupKey))
      return
    }
    if (action === 'open-detail') {
      openAccountDetail(button.dataset.groupKey, button.dataset.kind)
      return
    }
    const row = button.closest('[data-account-id]')
    if (!row) return
    const account = findAccount(Number(row.dataset.accountId))
    const groupID = Number(row.dataset.groupId)
    if (!account) return
    switch (action) {
      case 'create': openCreateDialog(account.id); break
      case 'run': await runNow(account); break
      case 'edit': openEditDialog(account); break
      case 'delete': openDeleteDialog(account); break
      case 'result': openResultDialog(account, button.dataset.resultId); break
      case 'edit-protection': openProtectionDialog(account, groupID); break
      case 'release-protection': openBindingActionDialog('release', account, groupID); break
      case 'remove-binding': openBindingActionDialog('remove', account, groupID); break
    }
  }

  async function handleGroupChange(event) {
    if (event.target.dataset.action !== 'automation-toggle') return
    const row = event.target.closest('[data-account-id]')
    const account = findAccount(Number(row?.dataset.accountId))
    if (!account) return
    const enabled = event.target.checked
    state.automationBusy.add(account.id)
    render()
    try {
      if (account.config) {
        await api(`/api/configs/${account.id}`, { method: 'PUT', body: { enabled } })
      } else {
        if (!enabled) return
        await api('/api/configs', { method: 'POST', body: { account_id: account.id, enabled: true } })
      }
      showToast(enabled ? '自动调度已启用' : '自动调度已停用')
      await loadOverview(true)
    } catch (error) {
      showToast(error.message || '更新失败', true)
    } finally {
      state.automationBusy.delete(account.id)
      render()
    }
  }

  function toggleGroup(groupKey) {
    if (!groupKey || state.search || state.status !== 'all') return
    if (state.collapsedGroups.has(groupKey)) state.collapsedGroups.delete(groupKey)
    else state.collapsedGroups.add(groupKey)
    persistCollapsedGroups()
    render()
  }

  function readCollapsedGroups() {
    try {
      const value = JSON.parse(localStorage.getItem(COLLAPSED_GROUPS_KEY) || '[]')
      return new Set(Array.isArray(value) ? value.map(String) : [])
    } catch {
      return new Set()
    }
  }

  function persistCollapsedGroups() {
    try {
      localStorage.setItem(COLLAPSED_GROUPS_KEY, JSON.stringify([...state.collapsedGroups]))
    } catch {
      // The page remains usable when browser storage is unavailable.
    }
  }

  function openCreateDialog(preselectedID = null) {
    const available = state.accounts.filter((account) => isAPIKey(account) && !account.config)
    state.editingID = null
    elements.configTitle.textContent = '新增状态检测'
    elements.accountSelect.disabled = false
    elements.accountSelect.innerHTML = available.length
      ? '<option value="">请选择账号</option>' + available.map(accountOption).join('')
      : '<option value="">没有可配置的 API Key 账号</option>'
    if (preselectedID && available.some((account) => account.id === preselectedID)) {
      elements.accountSelect.value = String(preselectedID)
    }
    fillPolicyForm(state.defaultPolicy, null)
    elements.formError.hidden = true
    renderDirectProbeControl(null)
    elements.saveButton.disabled = available.length === 0
    elements.configDialog.showModal()
  }

  function openEditDialog(account) {
    const config = account.config
    if (!config) return
    state.editingID = account.id
    elements.configTitle.textContent = '编辑检测规则'
    elements.accountSelect.innerHTML = accountOption(account)
    elements.accountSelect.value = String(account.id)
    elements.accountSelect.disabled = true
    fillPolicyForm(config.policy, config.balance_alert_threshold)
    elements.formError.hidden = true
    renderDirectProbeControl(config.probe || null)
    elements.saveButton.disabled = false
    elements.configDialog.showModal()
  }

  function fillPolicyForm(policy, balanceThreshold = null) {
    const normalized = policy || state.defaultPolicy
    elements.intervalInput.value = normalized.interval_seconds
    elements.modelInput.value = normalized.model || ''
    elements.latencyInput.value = (normalized.latency_limit_ms / 1000).toFixed(normalized.latency_limit_ms % 1000 ? 1 : 0)
    elements.failureInput.value = normalized.failure_threshold
    elements.recoveryInput.value = normalized.recovery_threshold
    elements.enabledInput.checked = Boolean(normalized.enabled)
    elements.balanceAlertInput.value = Number.isFinite(Number(balanceThreshold)) && balanceThreshold !== null ? String(balanceThreshold) : ''
    elements.promptInput.value = normalized.prompt || ''
  }

  function renderDirectProbeControl(probe) {
    const visible = Boolean(state.editingID)
    elements.directProbeControl.hidden = !visible
    if (!visible) return
    const source = probe?.source || 'legacy'
    const status = probe?.authorization_state || 'authorization_missing'
    const authorized = source === 'direct' && status === 'authorized'
    const labels = {
      authorization_missing: '尚未授权',
      authorized: '已授权',
      needs_reauthorization: '需要重新授权',
      unsupported: '暂不支持',
      credentials_unavailable: '加密未配置',
    }
    const heading = authorized ? '直连上游探测' : '通过 Sub2API 检测'
    const defaultMessage = authorized
      ? `已于 ${probe?.imported_at ? formatDateTime(probe.imported_at) : '此前'} 导入路由授权；检测将直接发送到该账号上游。`
      : '授权后，检测会从本服务直接流式调用该 API Key 的上游，不再通过 Sub2API 账号检测接口。'
    elements.directProbeState.textContent = heading
    elements.directProbeMessage.textContent = probe?.action_message || defaultMessage
    elements.directProbeAuthorizeButton.textContent = authorized ? '重新授权直连探测' : '授权直连探测'
    elements.directProbeAuthorizeButton.disabled = state.directProbeBusy || status === 'unsupported' || status === 'credentials_unavailable'
    elements.directProbeAuthorizeButton.hidden = status === 'unsupported' || status === 'credentials_unavailable'
    elements.directProbeRevokeButton.hidden = source !== 'direct'
    elements.directProbeRevokeButton.disabled = state.directProbeBusy
  }

  async function authorizeDirectProbe() {
    const accountID = state.editingID
    if (!accountID || state.directProbeBusy) return
    state.directProbeBusy = true
    renderDirectProbeControl(findAccount(accountID)?.config?.probe || null)
    try {
      const updated = await api(`/api/configs/${accountID}/direct-probe`, { method: 'POST', body: {} })
      const account = findAccount(accountID)
      if (account) account.config = updated
      renderDirectProbeControl(updated.probe)
      showToast('直连上游探测已授权，并已安排立即检测')
      await loadOverview(true)
      const refreshed = findAccount(accountID)
      renderDirectProbeControl(refreshed?.config?.probe || updated.probe)
    } catch (error) {
      elements.formError.textContent = error.message || '直连探测授权失败'
      elements.formError.hidden = false
    } finally {
      state.directProbeBusy = false
      renderDirectProbeControl(findAccount(accountID)?.config?.probe || null)
    }
  }

  async function revokeDirectProbe() {
    const accountID = state.editingID
    if (!accountID || state.directProbeBusy) return
    state.directProbeBusy = true
    renderDirectProbeControl(findAccount(accountID)?.config?.probe || null)
    try {
      const updated = await api(`/api/configs/${accountID}/direct-probe`, { method: 'DELETE' })
      const account = findAccount(accountID)
      if (account) account.config = updated
      renderDirectProbeControl(updated.probe)
      showToast('直连探测授权已撤销，后续将使用 Sub2API 检测')
      await loadOverview(true)
      const refreshed = findAccount(accountID)
      renderDirectProbeControl(refreshed?.config?.probe || updated.probe)
    } catch (error) {
      elements.formError.textContent = error.message || '撤销直连探测授权失败'
      elements.formError.hidden = false
    } finally {
      state.directProbeBusy = false
      renderDirectProbeControl(findAccount(accountID)?.config?.probe || null)
    }
  }

  async function saveConfig(event) {
    event.preventDefault()
    if (!elements.configForm.reportValidity()) return
    const accountID = state.editingID || Number(elements.accountSelect.value)
    const payload = {
      account_id: accountID,
      enabled: elements.enabledInput.checked,
      interval_seconds: Number(elements.intervalInput.value),
      model: elements.modelInput.value.trim(),
      latency_limit_ms: Math.round(Number(elements.latencyInput.value) * 1000),
      failure_threshold: Number(elements.failureInput.value),
      recovery_threshold: Number(elements.recoveryInput.value),
      // Keep a custom probe prompt byte-for-byte as entered. The server only
      // substitutes the shared default for whitespace-only input.
      prompt: elements.promptInput.value,
    }
    elements.saveButton.disabled = true
    elements.saveButton.textContent = '保存中'
    elements.formError.hidden = true
    try {
      await api(state.editingID ? `/api/configs/${accountID}` : '/api/configs', {
        method: state.editingID ? 'PUT' : 'POST',
        body: payload,
      })
      const thresholdRaw = elements.balanceAlertInput.value.trim()
      if (thresholdRaw) {
        const threshold = Number(thresholdRaw)
        if (!Number.isFinite(threshold) || threshold < 0) throw new Error('余额告警阈值必须是大于等于 0 的有限数值')
        await api(`/api/configs/${accountID}/balance-alert`, { method: 'PUT', body: { threshold } })
      } else if (state.editingID && findAccount(accountID)?.config?.balance_alert_threshold !== undefined) {
        await api(`/api/configs/${accountID}/balance-alert`, { method: 'DELETE' })
      }
      elements.configDialog.close()
      showToast(state.editingID ? '检测规则已更新' : '状态检测已添加')
      await loadOverview(true)
    } catch (error) {
      elements.formError.textContent = error.message || '保存失败'
      elements.formError.hidden = false
    } finally {
      elements.saveButton.disabled = false
      elements.saveButton.textContent = '保存'
    }
  }

  async function runNow(account) {
    if (!account.config) return
    account.config.running = true
    render()
    try {
      await api(`/api/configs/${account.id}/run`, { method: 'POST' })
      showToast('检测已启动')
      scheduleRunPolling(account.id)
    } catch (error) {
      account.config.running = false
      render()
      showToast(error.message || '启动检测失败', true)
    }
  }

  function scheduleRunPolling(accountID) {
    let attempts = 0
    const poll = async () => {
      attempts += 1
      await loadOverview(true)
      const current = findAccount(accountID)
      if (current?.config?.running && attempts < 90) window.setTimeout(poll, 1000)
    }
    window.setTimeout(poll, 700)
  }

  function openDeleteDialog(account) {
    if (!account.config) return
    state.deletingID = account.id
    elements.deleteAccountName.textContent = account.name || `账号 ${account.id}`
    elements.deleteDialog.showModal()
  }

  async function deleteConfig() {
    if (!state.deletingID) return
    elements.confirmDeleteButton.disabled = true
    try {
      await api(`/api/configs/${state.deletingID}`, { method: 'DELETE' })
      elements.deleteDialog.close()
      showToast('检测配置已删除')
      await loadOverview(true)
    } catch (error) {
      showToast(error.message || '删除失败', true)
    } finally {
      elements.confirmDeleteButton.disabled = false
    }
  }

  function openProtectionDialog(account, groupID) {
    if (!account || !groupID) return
    const protection = protectionFor(account, groupID)
    state.protectionTarget = { accountID: account.id, groupID }
    elements.protectionAccountName.textContent = `${account.name || `账号 ${account.id}`} · 分组 #${groupID}`
    elements.protectionMultiplierInput.value = protection && !protection.inherited ? String(protection.protection_multiplier) : ''
    const finalMultiplier = Number(protection?.final_multiplier ?? account.upstream_final_multiplier?.final_multiplier)
    elements.protectionFinalPreview.textContent = Number.isFinite(finalMultiplier) ? `${formatMultiplier(finalMultiplier)}x` : '暂不可用'
    elements.protectionError.hidden = true
    elements.protectionSaveButton.disabled = false
    elements.protectionDialog.showModal()
    window.requestAnimationFrame(() => elements.protectionMultiplierInput.focus())
  }

  function openGroupProtectionDialog(groupID) {
    const group = state.groups.find((item) => item.id === groupID)
    if (!group || groupID <= 0) return
    const protection = groupProtectionFor(groupID)
    state.groupProtectionTarget = { groupID }
    elements.groupProtectionName.textContent = `${group.name || `分组 ${groupID}`} · ${group.platform || 'all'}`
    elements.groupProtectionMultiplierInput.value = protection ? String(protection.protection_multiplier) : ''
    elements.groupProtectionClearButton.hidden = !protection
    elements.groupProtectionError.hidden = true
    elements.groupProtectionSaveButton.disabled = false
    elements.groupProtectionDialog.showModal()
    window.requestAnimationFrame(() => elements.groupProtectionMultiplierInput.focus())
  }

  async function saveProtection(event) {
    event.preventDefault()
    if (!elements.protectionForm.reportValidity() || !state.protectionTarget) return
    const multiplier = Number(elements.protectionMultiplierInput.value)
    if (!Number.isFinite(multiplier) || multiplier < 0) {
      elements.protectionError.textContent = '保护倍率必须是大于等于 0 的有限数值'
      elements.protectionError.hidden = false
      return
    }
    const { accountID, groupID } = state.protectionTarget
    const key = relationKey(groupID, accountID)
    state.protectionBusy.add(key)
    elements.protectionDialog.dataset.busy = 'true'
    document.querySelectorAll('[data-close-dialog="protection-dialog"]').forEach((button) => { button.disabled = true })
    elements.protectionSaveButton.disabled = true
    elements.protectionSaveButton.textContent = '保存中…'
    elements.protectionError.hidden = true
    try {
      await api(`/api/groups/${groupID}/accounts/${accountID}/protection`, {
        method: 'PUT',
        body: { protection_multiplier: multiplier },
      })
      elements.protectionDialog.close()
      showToast('保护倍率已保存，侧车已立即检查当前绑定状态')
      await loadOverview(true)
    } catch (error) {
      elements.protectionError.textContent = error.message || '保护倍率保存失败'
      elements.protectionError.hidden = false
    } finally {
      state.protectionBusy.delete(key)
      elements.protectionDialog.dataset.busy = 'false'
      document.querySelectorAll('[data-close-dialog="protection-dialog"]').forEach((button) => { button.disabled = false })
      elements.protectionSaveButton.disabled = false
      elements.protectionSaveButton.textContent = '保存保护倍率'
      render()
    }
  }

  async function saveGroupProtection(event) {
    event.preventDefault()
    if (!elements.groupProtectionForm.reportValidity() || !state.groupProtectionTarget) return
    const multiplier = Number(elements.groupProtectionMultiplierInput.value)
    if (!Number.isFinite(multiplier) || multiplier < 0) {
      elements.groupProtectionError.textContent = '保护倍率必须是大于等于 0 的有限数值'
      elements.groupProtectionError.hidden = false
      return
    }
    const { groupID } = state.groupProtectionTarget
    elements.groupProtectionDialog.dataset.busy = 'true'
    document.querySelectorAll('[data-close-dialog="group-protection-dialog"]').forEach((button) => { button.disabled = true })
    elements.groupProtectionSaveButton.disabled = true
    elements.groupProtectionClearButton.disabled = true
    elements.groupProtectionSaveButton.textContent = '保存中…'
    elements.groupProtectionError.hidden = true
    try {
      await api(`/api/groups/${groupID}/protection-default`, { method: 'PUT', body: { protection_multiplier: multiplier } })
      elements.groupProtectionDialog.close()
      showToast('分组默认保护倍率已保存，侧车已立即检查账号绑定状态')
      await loadOverview(true)
    } catch (error) {
      elements.groupProtectionError.textContent = error.message || '分组保护倍率保存失败'
      elements.groupProtectionError.hidden = false
    } finally {
      elements.groupProtectionDialog.dataset.busy = 'false'
      document.querySelectorAll('[data-close-dialog="group-protection-dialog"]').forEach((button) => { button.disabled = false })
      elements.groupProtectionSaveButton.disabled = false
      elements.groupProtectionClearButton.disabled = false
      elements.groupProtectionSaveButton.textContent = '保存分组保护'
      render()
    }
  }

  async function clearGroupProtection() {
    if (!state.groupProtectionTarget || elements.groupProtectionDialog.dataset.busy === 'true') return
    const { groupID } = state.groupProtectionTarget
    if (!window.confirm('清除分组保护后，继承该规则的账号将不再自动解绑；已被保护解绑的账号会尝试恢复绑定。')) return
    elements.groupProtectionDialog.dataset.busy = 'true'
    document.querySelectorAll('[data-close-dialog="group-protection-dialog"]').forEach((button) => { button.disabled = true })
    elements.groupProtectionSaveButton.disabled = true
    elements.groupProtectionClearButton.disabled = true
    elements.groupProtectionError.hidden = true
    elements.groupProtectionClearButton.textContent = '清除中…'
    try {
      await api(`/api/groups/${groupID}/protection-default`, { method: 'DELETE' })
      elements.groupProtectionDialog.close()
      showToast('分组保护已清除')
      await loadOverview(true)
    } catch (error) {
      elements.groupProtectionError.textContent = error.message || '分组保护清除失败'
      elements.groupProtectionError.hidden = false
    } finally {
      elements.groupProtectionDialog.dataset.busy = 'false'
      document.querySelectorAll('[data-close-dialog="group-protection-dialog"]').forEach((button) => { button.disabled = false })
      elements.groupProtectionSaveButton.disabled = false
      elements.groupProtectionClearButton.disabled = false
      elements.groupProtectionClearButton.textContent = '清除分组保护'
      render()
    }
  }

  function openGroupBalanceAlertDialog(groupID) {
    const group = state.groups.find((item) => item.id === groupID)
    if (!group || groupID <= 0) return
    const threshold = groupBalanceThresholdFor(groupID)
    state.groupBalanceAlertTarget = { groupID }
    elements.groupBalanceAlertName.textContent = `${group.name || `分组 ${groupID}`} · ${group.platform || 'all'}`
    elements.groupBalanceAlertInput.value = Number.isFinite(threshold) ? String(threshold) : ''
    elements.groupBalanceAlertClearButton.hidden = !Number.isFinite(threshold)
    elements.groupBalanceAlertError.hidden = true
    elements.groupBalanceAlertDialog.showModal()
    window.requestAnimationFrame(() => elements.groupBalanceAlertInput.focus())
  }

  async function saveGroupBalanceAlert(event) {
    event.preventDefault()
    if (!elements.groupBalanceAlertForm.reportValidity() || !state.groupBalanceAlertTarget) return
    const threshold = Number(elements.groupBalanceAlertInput.value)
    if (!Number.isFinite(threshold) || threshold < 0) {
      elements.groupBalanceAlertError.textContent = '余额告警阈值必须是大于等于 0 的有限数值'
      elements.groupBalanceAlertError.hidden = false
      return
    }
    const { groupID } = state.groupBalanceAlertTarget
    setGroupBalanceAlertBusy(true, '保存中…')
    try {
      await api(`/api/groups/${groupID}/balance-alert`, { method: 'PUT', body: { threshold } })
      elements.groupBalanceAlertDialog.close()
      showToast('分组余额告警阈值已保存')
      await loadOverview(true)
    } catch (error) {
      elements.groupBalanceAlertError.textContent = error.message || '分组余额告警保存失败'
      elements.groupBalanceAlertError.hidden = false
    } finally {
      setGroupBalanceAlertBusy(false, '保存余额告警')
    }
  }

  async function clearGroupBalanceAlert() {
    if (!state.groupBalanceAlertTarget || elements.groupBalanceAlertDialog.dataset.busy === 'true') return
    if (!window.confirm('清除后，没有账号级阈值的账号将不再继承此分组余额告警。')) return
    const { groupID } = state.groupBalanceAlertTarget
    setGroupBalanceAlertBusy(true, '清除中…')
    try {
      await api(`/api/groups/${groupID}/balance-alert`, { method: 'DELETE' })
      elements.groupBalanceAlertDialog.close()
      showToast('分组余额告警已清除')
      await loadOverview(true)
    } catch (error) {
      elements.groupBalanceAlertError.textContent = error.message || '清除分组余额告警失败'
      elements.groupBalanceAlertError.hidden = false
    } finally {
      setGroupBalanceAlertBusy(false, '保存余额告警')
    }
  }

  function setGroupBalanceAlertBusy(busy, label) {
    elements.groupBalanceAlertDialog.dataset.busy = busy ? 'true' : 'false'
    elements.groupBalanceAlertSaveButton.disabled = busy
    elements.groupBalanceAlertClearButton.disabled = busy
    elements.groupBalanceAlertInput.disabled = busy
    document.querySelectorAll('[data-close-dialog="group-balance-alert-dialog"]').forEach((button) => { button.disabled = busy })
    elements.groupBalanceAlertSaveButton.textContent = label
  }

  async function openNotificationSettings() {
    if (!state.notificationsAvailable || state.notificationBusy) return
    state.notificationBusy = true
    elements.notificationSettingsDialog.showModal()
    elements.notificationSettingsError.hidden = true
    setNotificationBusy(true, '读取中…')
    try {
      const response = await api('/api/notifications')
      state.notificationSettings = response.settings || null
      fillNotificationSettings(state.notificationSettings)
    } catch (error) {
      elements.notificationSettingsError.textContent = error.message || '读取通知配置失败'
      elements.notificationSettingsError.hidden = false
    } finally {
      state.notificationBusy = false
      setNotificationBusy(false, '保存通知设置')
    }
  }

  function fillNotificationSettings(settings) {
    const configured = Boolean(settings?.configured)
    elements.notificationEnabledInput.checked = Boolean(settings?.enabled)
    elements.notificationEndpointInput.value = settings?.bark_endpoint || 'https://bark.aixw.org'
    elements.notificationBasicUserInput.value = settings?.bark_basic_auth_user || ''
    elements.notificationDeviceKeyInput.value = ''
    elements.notificationEncryptionKeyInput.value = ''
    elements.notificationBasicPasswordInput.value = ''
    elements.notificationDeviceKeyInput.placeholder = configured ? '已加密保存，留空不修改' : 'Bark 设备 Key'
    elements.notificationEncryptionKeyInput.placeholder = configured ? '已加密保存，留空不修改' : '16 个 ASCII 字符'
    elements.notificationBasicPasswordInput.placeholder = configured ? '已加密保存，留空不修改' : '可选'
    elements.notificationClearButton.hidden = !configured
    elements.notificationTestButton.disabled = !configured || !settings?.enabled
    const statusClass = settings?.last_delivery_error ? 'danger' : configured ? 'success' : 'neutral'
    const statusTitle = settings?.last_delivery_error ? '最近推送失败' : configured ? (settings.enabled ? 'Bark 通知已启用' : 'Bark 配置已保存但未启用') : '尚未配置 Bark'
    const statusDetail = settings?.last_delivery_error || (settings?.last_delivery_at ? `最近发送：${formatDateTime(settings.last_delivery_at)}` : '保存后可发送加密测试通知')
    elements.notificationSettingsStatus.className = `notification-status full-width ${statusClass}`
    elements.notificationSettingsStatus.innerHTML = `<strong>${escapeHTML(statusTitle)}</strong><span>${escapeHTML(statusDetail)}</span>`
  }

  async function saveNotificationSettings(event) {
    event.preventDefault()
    if (!elements.notificationSettingsForm.reportValidity() || state.notificationBusy) return
    const configured = Boolean(state.notificationSettings?.configured)
    const deviceKey = elements.notificationDeviceKeyInput.value.trim()
    const encryptionKey = elements.notificationEncryptionKeyInput.value
    if (!configured && !deviceKey) {
      elements.notificationSettingsError.textContent = '首次配置必须填写 Bark 设备 Key'
      elements.notificationSettingsError.hidden = false
      elements.notificationDeviceKeyInput.focus()
      return
    }
    if (!configured && !encryptionKey) {
      elements.notificationSettingsError.textContent = '首次配置必须填写 16 字节推送加密 Key'
      elements.notificationSettingsError.hidden = false
      elements.notificationEncryptionKeyInput.focus()
      return
    }
    if (encryptionKey && new TextEncoder().encode(encryptionKey).length !== 16) {
      elements.notificationSettingsError.textContent = '推送加密 Key 必须正好是 16 字节，建议使用 16 个 ASCII 字符'
      elements.notificationSettingsError.hidden = false
      elements.notificationEncryptionKeyInput.focus()
      return
    }
    state.notificationBusy = true
    setNotificationBusy(true, '保存中…')
    elements.notificationSettingsError.hidden = true
    try {
      const response = await api('/api/notifications', {
        method: 'PUT',
        body: {
          enabled: elements.notificationEnabledInput.checked,
          bark_endpoint: elements.notificationEndpointInput.value.trim(),
          bark_basic_auth_user: elements.notificationBasicUserInput.value.trim(),
          device_key: deviceKey,
          encryption_key: encryptionKey,
          basic_auth_password: elements.notificationBasicPasswordInput.value,
        },
      })
      state.notificationSettings = response.settings || null
      fillNotificationSettings(state.notificationSettings)
      showToast('Bark 通知设置已保存')
    } catch (error) {
      elements.notificationSettingsError.textContent = error.message || '保存 Bark 通知设置失败'
      elements.notificationSettingsError.hidden = false
    } finally {
      state.notificationBusy = false
      setNotificationBusy(false, '保存通知设置')
    }
  }

  async function testNotificationSettings() {
    if (state.notificationBusy || !state.notificationSettings?.configured) return
    state.notificationBusy = true
    setNotificationBusy(true, '测试中…')
    elements.notificationSettingsError.hidden = true
    try {
      await api('/api/notifications/test', { method: 'POST', body: {} })
      showToast('测试通知已发送，请检查 Bark App')
      const response = await api('/api/notifications')
      state.notificationSettings = response.settings || state.notificationSettings
      fillNotificationSettings(state.notificationSettings)
    } catch (error) {
      elements.notificationSettingsError.textContent = error.message || '测试通知发送失败'
      elements.notificationSettingsError.hidden = false
    } finally {
      state.notificationBusy = false
      setNotificationBusy(false, '保存通知设置')
    }
  }

  async function clearNotificationSettings() {
    if (state.notificationBusy || !state.notificationSettings?.configured) return
    if (!window.confirm('清除后将删除 Bark 设备 Key、加密 Key 与 Basic Auth 密码；分组和账号阈值会保留。')) return
    state.notificationBusy = true
    setNotificationBusy(true, '清除中…')
    try {
      await api('/api/notifications', { method: 'DELETE' })
      state.notificationSettings = null
      fillNotificationSettings(null)
      showToast('Bark 通知配置已清除')
    } catch (error) {
      elements.notificationSettingsError.textContent = error.message || '清除 Bark 通知设置失败'
      elements.notificationSettingsError.hidden = false
    } finally {
      state.notificationBusy = false
      setNotificationBusy(false, '保存通知设置')
    }
  }

  function setNotificationBusy(busy, label) {
    elements.notificationSettingsDialog.dataset.busy = busy ? 'true' : 'false'
    elements.notificationSaveButton.disabled = busy
    elements.notificationTestButton.disabled = busy || !state.notificationSettings?.configured || !state.notificationSettings?.enabled
    elements.notificationClearButton.disabled = busy
    for (const input of elements.notificationSettingsForm.querySelectorAll('input')) input.disabled = busy
    document.querySelectorAll('[data-close-dialog="notification-settings-dialog"]').forEach((button) => { button.disabled = busy })
    elements.notificationSaveButton.textContent = label
  }

  function openBindingActionDialog(type, account, groupID) {
    if (!account || !groupID) return
    state.bindingAction = { type, accountID: account.id, groupID }
    elements.bindingActionAccountName.textContent = account.name || `账号 ${account.id}`
    elements.bindingActionError.hidden = true
    if (type === 'release') {
      elements.bindingActionTitle.textContent = '解除倍率保护'
      elements.bindingActionMessage.textContent = groupProtectionFor(groupID)
        ? '确认后会先把账号重新绑定到当前分组，再移除账号级保护倍率。该账号随后会继承分组默认保护；若最终倍率仍超过分组阈值，之后可能再次自动解绑。'
        : '确认后会先把账号重新绑定到当前分组，再移除保护倍率。即使当前最终倍率仍然较高，也不会再自动解除该绑定。'
      elements.bindingActionConfirmButton.textContent = '确认解除保护'
      elements.bindingActionConfirmButton.className = 'button warning'
    } else {
      elements.bindingActionTitle.textContent = '移除账号绑定'
      elements.bindingActionMessage.textContent = '确认后会先删除保护倍率和自动回绑关系，再解除实际分组绑定。之后即使最终倍率下降，也不会自动重新绑定。'
      elements.bindingActionConfirmButton.textContent = '确认移除绑定'
      elements.bindingActionConfirmButton.className = 'button danger'
    }
    elements.bindingActionConfirmButton.disabled = false
    elements.bindingActionDialog.showModal()
    window.requestAnimationFrame(() => elements.bindingActionConfirmButton.focus())
  }

  async function confirmBindingAction() {
    if (!state.bindingAction) return
    const { type, accountID, groupID } = state.bindingAction
    const key = relationKey(groupID, accountID)
    state.protectionBusy.add(key)
    elements.bindingActionDialog.dataset.busy = 'true'
    document.querySelectorAll('[data-close-dialog="binding-action-dialog"]').forEach((button) => { button.disabled = true })
    elements.bindingActionConfirmButton.disabled = true
    elements.bindingActionError.hidden = true
    const originalText = elements.bindingActionConfirmButton.textContent
    elements.bindingActionConfirmButton.textContent = '处理中…'
    try {
      const path = type === 'release'
        ? `/api/groups/${groupID}/accounts/${accountID}/protection/release`
        : `/api/groups/${groupID}/accounts/${accountID}/binding`
      await api(path, { method: type === 'release' ? 'POST' : 'DELETE' })
      elements.bindingActionDialog.close()
      state.bindingAction = null
      showToast(type === 'release' ? '倍率保护已解除，账号已重新绑定' : '账号绑定和倍率保护已移除')
      await loadOverview(true)
    } catch (error) {
      elements.bindingActionError.textContent = error.message || '操作失败'
      elements.bindingActionError.hidden = false
    } finally {
      state.protectionBusy.delete(key)
      elements.bindingActionDialog.dataset.busy = 'false'
      document.querySelectorAll('[data-close-dialog="binding-action-dialog"]').forEach((button) => { button.disabled = false })
      elements.bindingActionConfirmButton.disabled = false
      elements.bindingActionConfirmButton.textContent = originalText
      render()
    }
  }

  function openResultDialog(account, resultID) {
    const result = account.config?.history?.find((item) => item.id === resultID)
    if (!result) return
    elements.resultAccountName.textContent = account.name || `账号 ${account.id}`
    const rows = [
      ['状态', statusLabel(result.status)],
      ['检测时间', formatDateTime(result.checked_at)],
      ['耗时', formatMilliseconds(result.latency_ms)],
      ['调度动作', actionLabel(result.action)],
      ['信息', result.message || '—'],
    ]
    if (result.failure_kind) rows.splice(1, 0, ['失败原因', failureKindLabel(result.failure_kind)])
    if (result.usage) {
      const usageTokens = ['input_tokens', 'output_tokens', 'cache_read_tokens', 'cache_write_tokens']
        .reduce((sum, key) => sum + (Number(result.usage[key]) || 0), 0)
      rows.push(['检测 Token', `${formatInteger(usageTokens)} · 输入 ${formatInteger(result.usage.input_tokens || 0)} / 输出 ${formatInteger(result.usage.output_tokens || 0)}`])
      if (result.usage.model) rows.push(['计费模型', result.usage.model])
    }
    if (result.cost) rows.push(['检测成本（1 倍率）', result.cost.known ? formatProbeCost(result.cost.amount || 0) : '定价不可用'])
    if (result.response_text) rows.push(['响应片段', result.response_text])
    elements.resultDetails.innerHTML = rows.map(([term, detail]) => `<dt>${escapeHTML(term)}</dt><dd>${escapeHTML(detail)}</dd>`).join('')
    elements.resultDialog.showModal()
  }

  function openBindingDialog(groupID, trigger = null) {
    const group = state.groups.find((item) => item.id === groupID)
    if (!group) return
    state.bindingGroupID = groupID
    state.bindingTrigger = trigger
    state.bindingSearch = ''
    state.bindingOriginal = currentBindingIDs(groupID)
    state.bindingDraft = new Set(state.bindingOriginal)
    state.bindingSaving = false
    elements.bindingGroupName.textContent = `${group.name} · ${group.platform}`
    elements.bindingSearchInput.value = ''
    elements.bindingError.hidden = true
    renderBindingList()
    elements.bindingDialog.showModal()
    window.requestAnimationFrame(() => elements.bindingSearchInput.focus())
  }

  function renderBindingList() {
    const candidates = bindingCandidates()
    const group = bindingGroup()
    const accounts = candidates.filter((account) => {
      if (!state.bindingSearch) return true
      return `${account.name || ''} ${account.platform || ''} ${account.type || ''} ${account.id}`.toLocaleLowerCase().includes(state.bindingSearch)
    })
    elements.bindingList.innerHTML = accounts.length ? accounts.map((account) => {
      const scheduling = groupAccountState(account, state.bindingGroupID)
      const checked = state.bindingDraft.has(account.id)
      const originallyBound = state.bindingOriginal.has(account.id)
      const compatible = accountCompatibleWithGroup(account, group)
      const membershipLabel = !compatible
        ? checked ? '已绑定 · 平台不匹配' : '待解绑 · 平台不匹配'
        : originallyBound && !checked
          ? '待解绑'
          : !originallyBound && checked
            ? '待绑定'
            : originallyBound ? '已绑定' : '可绑定'
      return `<label class="binding-row">
        <input type="checkbox" data-account-id="${account.id}" ${checked ? 'checked' : ''} ${state.bindingSaving ? 'disabled' : ''}>
        <span class="binding-account"><strong>${escapeHTML(account.name || `账号 ${account.id}`)}</strong><small>${escapeHTML(account.platform || 'unknown')} · API Key · #${account.id}</small></span>
        <span class="binding-state ${escapeAttr(scheduling.tone)}"><i class="status-dot"></i>${escapeHTML(scheduling.label)}</span>
        ${renderBindingMultiplier(account)}
        ${renderBindingBalance(account)}
        <span class="binding-memberships ${compatible ? '' : 'incompatible'}"><strong>${escapeHTML(membershipLabel)}</strong><small>${formatInteger(membershipIDs(account).length)} 个分组</small></span>
      </label>`
    }).join('') : `<div class="dialog-empty">${candidates.length ? '没有匹配的 API Key 账号' : '当前分组没有可绑定的 API Key 账号'}</div>`
    const changes = bindingChanges()
    const eligibleCount = candidates.filter((account) => accountCompatibleWithGroup(account, group)).length
    const pendingLabel = changes.length ? `${formatInteger(changes.length)} 项待保存` : '无待保存变更'
    elements.bindingChangeCount.textContent = `可绑定 ${formatInteger(eligibleCount)} · 已选 ${formatInteger(state.bindingDraft.size)} · ${pendingLabel}`
    elements.bindingList.setAttribute('aria-busy', state.bindingSaving ? 'true' : 'false')
    elements.bindingSaveButton.disabled = state.bindingSaving || changes.length === 0
  }

  function renderBindingMultiplier(account) {
    const projection = account?.upstream_final_multiplier || {}
    const finalMultiplier = Number(projection.final_multiplier)
    const available = projection.status === 'available' && Number.isFinite(finalMultiplier)
    if (available) {
      const recharge = Number(projection.recharge_rate_cny_per_usd)
      const group = Number(projection.group_multiplier)
      const detail = Number.isFinite(recharge) && Number.isFinite(group)
        ? `充值 ${formatMultiplier(recharge)} × 分组 ${formatMultiplier(group)}`
        : '来自已绑定上游 Key 的当前快照'
      const accessible = `最终倍率 ${formatMultiplier(finalMultiplier)}x，${detail}`
      return `<span class="binding-metric binding-multiplier available" title="${escapeAttr(detail)}" aria-label="${escapeAttr(accessible)}"><span class="binding-metric-label">最终倍率</span><strong>${escapeHTML(formatMultiplier(finalMultiplier))}x</strong><small>${escapeHTML(detail)}</small></span>`
    }
    const labels = {
      unbound: '未绑定上游 Key',
      stale: '上游 Key 绑定已失效',
      ambiguous: '存在多个有效上游 Key 绑定',
      recharge_unset: '请在上游列表设置充值倍率',
      group_multiplier_unknown: '上游尚未同步分组倍率',
      invalid_upstream: '本地上游地址无效',
      unavailable: '上游列表暂不可用',
    }
    const reason = labels[String(projection.status || 'unavailable')] || '最终倍率未计算'
    const accessible = `最终倍率未计算，${reason}`
    return `<span class="binding-metric binding-multiplier unavailable" title="${escapeAttr(reason)}" aria-label="${escapeAttr(accessible)}"><span class="binding-metric-label">最终倍率</span><strong>未计算</strong><small>${escapeHTML(reason)}</small></span>`
  }

  function renderBindingBalance(account) {
    const balance = account?.admin_balance || {}
    const exhausted = Array.isArray(balance.exhausted_dimensions) ? balance.exhausted_dimensions : []
    const exhaustedLabels = { daily: '日额度', weekly: '周额度', total: '总额度' }
    const exhaustedText = exhausted.map((dimension) => exhaustedLabels[dimension]).filter(Boolean).join('、')
    const remaining = Number(balance.remaining)
    const zeroManagedBalance = Boolean(balance.managed) && !Boolean(balance.unlimited) && Number.isFinite(remaining) && remaining <= 0
    if (Boolean(balance.insufficient) || exhausted.length > 0 || zeroManagedBalance) {
      const reason = exhaustedText ? `${exhaustedText}已耗尽` : '同步余额已耗尽'
      return `<span class="binding-metric binding-balance insufficient" title="${escapeAttr(reason)}" aria-label="可用余额不足，${escapeAttr(reason)}"><span class="binding-metric-label">可用余额</span><strong>余额不足</strong><small>${escapeHTML(reason)}</small></span>`
    }
    if (Boolean(balance.unlimited)) {
      const detail = balance.managed ? '上游同步为不限额度' : '管理员未配置额度上限'
      return `<span class="binding-metric binding-balance unlimited" title="${escapeAttr(detail)}" aria-label="可用余额不限额度，${escapeAttr(detail)}"><span class="binding-metric-label">可用余额</span><strong>不限额度</strong><small>${escapeHTML(detail)}</small></span>`
    }
    if (Boolean(balance.managed) && Number.isFinite(remaining)) {
      const detail = '上游余额按当前分组倍率折算后的同步投影'
      const accessible = `可用余额 ${formatCurrency(remaining)}，${detail}`
      return `<span class="binding-metric binding-balance available" title="${escapeAttr(detail)}" aria-label="${escapeAttr(accessible)}"><span class="binding-metric-label">可用余额</span><strong>${escapeHTML(formatCurrency(remaining))}</strong><small>${escapeHTML(detail)}</small></span>`
    }
    const detail = '尚未取得可用的上游余额投影'
    return `<span class="binding-metric binding-balance unavailable" title="${escapeAttr(detail)}" aria-label="可用余额暂不可用，${escapeAttr(detail)}"><span class="binding-metric-label">可用余额</span><strong>暂不可用</strong><small>${escapeHTML(detail)}</small></span>`
  }

  function handleBindingChange(event) {
    if (state.bindingSaving) return
    const accountID = Number(event.target.dataset.accountId)
    if (!accountID) return
    if (event.target.checked) state.bindingDraft.add(accountID)
    else state.bindingDraft.delete(accountID)
    renderBindingList()
  }

  function bindingChanges() {
    return bindingCandidates().flatMap((account) => {
      const before = state.bindingOriginal.has(account.id)
      const after = state.bindingDraft.has(account.id)
      return before === after ? [] : [{ account, bound: after }]
    })
  }

  async function saveBindings() {
    const changes = bindingChanges()
    if (!state.bindingGroupID || state.bindingSaving || changes.length === 0) return
    const groupID = state.bindingGroupID
    const desired = new Set(state.bindingDraft)
    setBindingSaving(true)
    elements.bindingError.hidden = true
    try {
      const response = await api(`/api/groups/${groupID}/accounts`, {
        method: 'PUT',
        body: { account_ids: [...desired].sort((a, b) => a - b) },
      })
      if (!await loadOverview(true)) {
        elements.bindingError.textContent = '账号绑定结果已返回，但暂时无法刷新最新状态，请稍后重试刷新。'
        elements.bindingError.hidden = false
        return
      }
      resetBindingState(groupID, desired)
      const failures = response.failures || []
      if (failures.length) {
        const details = failures.map((failure) => `${failure.name || `账号 ${failure.account_id}`}：${failure.message || '绑定失败'}`)
        elements.bindingError.textContent = [`${formatInteger(failures.length)} 个账号未能保存：`, ...details].join('\n')
        elements.bindingError.hidden = false
        showToast(`${formatInteger(response.updated_account_ids?.length || 0)} 项已更新，${formatInteger(failures.length)} 项失败`, true)
        return
      }
      showToast(`${formatInteger(response.updated_account_ids?.length || 0)} 项账号绑定已更新`)
      setBindingSaving(false)
      elements.bindingDialog.close()
    } catch (error) {
      if (await loadOverview(true)) resetBindingState(groupID, desired)
      elements.bindingError.textContent = error.code === 'MIXED_CHANNEL_RISK'
        ? `Sub2API 已阻止混合渠道绑定：${error.message}`
        : error.code === 'INCOMPATIBLE_PLATFORM'
          ? `账号平台与当前分组不兼容：${error.message}`
          : error.message || '保存账号绑定失败'
      elements.bindingError.hidden = false
    } finally {
      if (elements.bindingDialog.open) setBindingSaving(false)
    }
  }

  function bindingGroup() {
    return state.groups.find((group) => group.id === state.bindingGroupID) || null
  }

  function currentBindingIDs(groupID) {
    return new Set(state.accounts
      .filter((account) => isAPIKey(account) && membershipIDs(account).includes(groupID))
      .map((account) => account.id))
  }

  function bindingCandidates() {
    const group = bindingGroup()
    if (!group) return []
    return sortAccounts(state.accounts.filter((account) => isAPIKey(account) && (
      accountCompatibleWithGroup(account, group) || state.bindingOriginal.has(account.id)
    )), group.id)
  }

  function accountCompatibleWithGroup(account, group) {
    if (!account || !group) return false
    const accountPlatform = String(account.platform || '').trim().toLocaleLowerCase()
    const groupPlatform = String(group.platform || '').trim().toLocaleLowerCase()
    if (!accountPlatform || !groupPlatform) return false
    if (groupPlatform === 'composite' || accountPlatform === groupPlatform) return true
    return accountPlatform === 'antigravity' && account.mixed_scheduling === true && ['anthropic', 'gemini'].includes(groupPlatform)
  }

  function resetBindingState(groupID, desired) {
    state.bindingOriginal = currentBindingIDs(groupID)
    const candidateIDs = new Set(bindingCandidates().map((account) => account.id))
    state.bindingDraft = new Set([...desired].filter((accountID) => candidateIDs.has(accountID)))
    renderBindingList()
  }

  function setBindingSaving(saving) {
    state.bindingSaving = saving
    elements.bindingSearchInput.disabled = saving
    elements.bindingSaveButton.textContent = saving ? '正在保存…' : '保存账号绑定'
    document.querySelectorAll('[data-close-dialog="binding-dialog"]').forEach((button) => { button.disabled = saving })
    renderBindingList()
  }

  function restoreBindingFocus() {
    const groupID = state.bindingGroupID
    const trigger = state.bindingTrigger
    state.bindingGroupID = null
    state.bindingTrigger = null
    state.bindingSaving = false
    window.requestAnimationFrame(() => {
      if (trigger?.isConnected) {
        trigger.focus()
        return
      }
      const replacement = [...document.querySelectorAll('[data-action="bind-group"]')]
        .find((button) => Number(button.dataset.groupKey) === groupID)
      replacement?.focus()
    })
  }

  function openAccountDetail(groupKey, kind) {
    const view = buildGroupViews().find((item) => item.key === String(groupKey))
    if (!view) return
    state.detailGroupKey = view.key
    state.detailKind = kind
    state.detailPage = 1
    elements.accountDetailTitle.textContent = kind === 'oauth' ? 'OAuth 账号' : '其他账号'
    elements.accountDetailGroupName.textContent = view.group.name
    elements.accountDetailDialog.showModal()
    renderAccountDetail()
    loadVisibleDetailUsage()
  }

  function detailAccounts() {
    const view = buildGroupViews().find((item) => item.key === state.detailGroupKey)
    if (!view) return []
    return sortAccounts(view.accounts.filter((account) => state.detailKind === 'oauth' ? isOAuthLike(account) : !isOAuthLike(account) && !isAPIKey(account)))
  }

  function currentDetailPageAccounts() {
    const accounts = detailAccounts()
    const start = (state.detailPage - 1) * DETAIL_PAGE_SIZE
    return accounts.slice(start, start + DETAIL_PAGE_SIZE)
  }

  function renderAccountDetail() {
    const accounts = detailAccounts()
    const pages = Math.max(1, Math.ceil(accounts.length / DETAIL_PAGE_SIZE))
    state.detailPage = Math.min(state.detailPage, pages)
    const visible = currentDetailPageAccounts()
    elements.accountDetailList.innerHTML = visible.length ? visible.map((account) => {
      const scheduling = accountState(account)
      return `<div class="detail-row" data-account-id="${account.id}">
        <div class="detail-account"><strong>${escapeHTML(account.name || `账号 ${account.id}`)}</strong><span>${escapeHTML(account.platform || 'unknown')} · ${escapeHTML(account.type || 'unknown')} · #${account.id}</span></div>
        <div class="account-state ${escapeAttr(scheduling.tone)}"><i class="status-dot"></i><span><strong>${escapeHTML(scheduling.label)}</strong><small title="${escapeAttr(scheduling.reason)}">${escapeHTML(scheduling.reason)}</small></span></div>
        <div class="detail-usage">${renderTodayUsage(account)}</div>
        <div class="passive-usage">${renderPassiveUsage(account)}</div>
      </div>`
    }).join('') : '<div class="dialog-empty">暂无账号</div>'
    elements.accountDetailPageLabel.textContent = `第 ${state.detailPage} / ${pages} 页 · ${formatInteger(accounts.length)} 个账号`
    elements.accountDetailPrev.disabled = state.detailPage <= 1
    elements.accountDetailNext.disabled = state.detailPage >= pages
  }

  function renderPassiveUsage(account) {
    if (!isOAuthLike(account)) return '<span class="muted">无 OAuth 额度数据</span>'
    const cached = state.usageCache.get(account.id)
    if (!cached || cached.loading) return '<span class="usage-loading">额度读取中</span>'
    if (cached.error) return `<span class="text-danger" title="${escapeAttr(cached.error)}">额度暂不可用</span>`
    const usage = cached.usage || {}
    const windows = [
      usage.five_hour ? `5 小时 ${formatPercent(usage.five_hour.utilization)}` : '',
      usage.seven_day ? `7 天 ${formatPercent(usage.seven_day.utilization)}` : '',
      usage.seven_day_sonnet ? `Sonnet ${formatPercent(usage.seven_day_sonnet.utilization)}` : '',
    ].filter(Boolean)
    if (usage.error) return `<span class="text-danger" title="${escapeAttr(usage.error)}">${escapeHTML(usage.error_code || '额度异常')}</span>`
    return windows.length ? `<strong>${escapeHTML(windows.join(' · '))}</strong><span>${usage.updated_at ? `更新于 ${escapeHTML(formatDateTime(usage.updated_at))}` : '被动快照'}</span>` : '<span class="muted">暂无额度快照</span>'
  }

  async function loadVisibleDetailUsage() {
    const accounts = currentDetailPageAccounts().filter(isOAuthLike)
    const pending = accounts.filter((account) => !state.usageCache.has(account.id))
    if (pending.length === 0) return
    for (const account of pending) state.usageCache.set(account.id, { loading: true })
    renderAccountDetail()
    await Promise.all(pending.map(async (account) => {
      try {
        const response = await api(`/api/accounts/${account.id}/usage`)
        state.usageCache.set(account.id, { usage: response.usage || {} })
      } catch (error) {
        state.usageCache.set(account.id, { error: error.message || '读取失败' })
      }
    }))
    if (elements.accountDetailDialog.open) renderAccountDetail()
  }

  function changeDetailPage(delta) {
    const pages = Math.max(1, Math.ceil(detailAccounts().length / DETAIL_PAGE_SIZE))
    const next = Math.min(pages, Math.max(1, state.detailPage + delta))
    if (next === state.detailPage) return
    state.detailPage = next
    renderAccountDetail()
    loadVisibleDetailUsage()
  }

  async function registerTab() {
    elements.registerTabButton.disabled = true
    try {
      await api('/api/tab/register', { method: 'POST' })
      elements.registerTabButton.hidden = true
      showToast('管理员 Tab 已注册')
    } catch (error) {
      showToast(error.message || '注册失败', true)
    } finally {
      elements.registerTabButton.disabled = false
    }
  }

  async function api(path, options = {}) {
    const headers = { Authorization: `Bearer ${state.token}` }
    if (options.body !== undefined) headers['Content-Type'] = 'application/json'
    const requestURL = new URL(String(path).replace(/^\//, ''), document.baseURI)
    const response = await fetch(requestURL, {
      method: options.method || 'GET',
      headers,
      body: options.body === undefined ? undefined : JSON.stringify(options.body),
      credentials: 'same-origin',
    })
    if (response.status === 204) return null
    const payload = await response.json().catch(() => ({}))
    if (!response.ok) {
      const error = new Error(payload.message || `请求失败 (${response.status})`)
      error.code = payload.code
      error.status = response.status
      error.payload = payload
      throw error
    }
    return payload
  }

  function showAuthError(message) {
    elements.authState.classList.add('error')
    elements.authState.querySelector('h1').textContent = '无权访问'
    elements.authMessage.textContent = message
  }

  function showToast(message, isError = false) {
    window.clearTimeout(state.toastTimer)
    elements.toast.textContent = message
    elements.toast.classList.toggle('error', isError)
    elements.toast.hidden = false
    state.toastTimer = window.setTimeout(() => { elements.toast.hidden = true }, 3600)
  }

  function setRefreshLoading(loading) {
    elements.refreshButton.disabled = loading
    elements.refreshButton.classList.toggle('spin', loading)
  }

  function findAccount(accountID) {
    return state.accounts.find((account) => account.id === accountID)
  }

  function sortAccounts(accounts, groupID = 0) {
    return [...accounts].sort((a, b) => {
      const availability = Number(groupAccountState(a, groupID).key !== 'enabled') - Number(groupAccountState(b, groupID).key !== 'enabled')
      return availability || String(a.name || '').localeCompare(String(b.name || ''), 'zh-CN') || a.id - b.id
    })
  }

  function isAPIKey(account) {
    return String(account.type || '').toLocaleLowerCase() === 'apikey'
  }

  function isOAuthLike(account) {
    return ['oauth', 'setup-token'].includes(String(account.type || '').toLocaleLowerCase())
  }

  function accountOption(account) {
    return `<option value="${account.id}">${escapeHTML(account.name || `账号 ${account.id}`)} · ${escapeHTML(account.platform || 'unknown')} · #${account.id}</option>`
  }

  function statusLabel(status) {
    return ({ operational: '正常', degraded: '耗时超限', failed: '调用失败', error: '检测错误', skipped: '等待授权' })[status] || '未检测'
  }

  function actionLabel(action) {
    return ({ disabled: '已关闭调度', restored: '已恢复调度', disable_failed: '关闭调度失败', restore_failed: '恢复调度失败', restore_blocked: '等待恢复' })[action] || '无'
  }

  function failureKindLabel(kind) {
    return ({ balance_insufficient: '余额不足导致检测失败' })[kind] || String(kind || '未分类')
  }

  function formatInterval(seconds) {
    if (seconds >= 3600 && seconds % 3600 === 0) return `${seconds / 3600} 小时`
    if (seconds >= 60 && seconds % 60 === 0) return `${seconds / 60} 分钟`
    return `${seconds} 秒`
  }

  function formatSeconds(milliseconds) {
    const seconds = milliseconds / 1000
    return `${Number.isInteger(seconds) ? seconds : seconds.toFixed(1)} 秒`
  }

  function formatMilliseconds(milliseconds) {
    const value = Number(milliseconds)
    return Number.isFinite(value) ? `${formatInteger(value)} ms` : '—'
  }

  function formatDateTime(value) {
    const date = value instanceof Date ? value : new Date(value)
    if (Number.isNaN(date.getTime())) return '—'
    return new Intl.DateTimeFormat('zh-CN', { month: '2-digit', day: '2-digit', hour: '2-digit', minute: '2-digit', second: '2-digit', hour12: false }).format(date)
  }

  function formatRelative(value) {
    const milliseconds = new Date(value).getTime() - Date.now()
    if (!Number.isFinite(milliseconds)) return '—'
    if (milliseconds <= 0) return '即将执行'
    const seconds = Math.ceil(milliseconds / 1000)
    if (seconds < 60) return `${seconds} 秒`
    const minutes = Math.ceil(seconds / 60)
    if (minutes < 60) return `${minutes} 分钟`
    return `${Math.ceil(minutes / 60)} 小时`
  }

  function formatInteger(value) {
    return new Intl.NumberFormat('zh-CN', { maximumFractionDigits: 0 }).format(Number(value) || 0)
  }

  function formatCompact(value) {
    return new Intl.NumberFormat('zh-CN', { notation: 'compact', maximumFractionDigits: 1 }).format(Number(value) || 0)
  }

  function formatNumber(value) {
    return new Intl.NumberFormat('zh-CN', { maximumFractionDigits: 2 }).format(Number(value) || 0)
  }

  function formatMultiplier(value) {
    const normalized = Number(Number(value).toPrecision(12))
    return new Intl.NumberFormat('zh-CN', { maximumFractionDigits: 4 }).format(normalized)
  }

  function formatCurrency(value) {
    const normalized = Number(value) || 0
    return `$${normalized.toLocaleString('en-US', { minimumFractionDigits: normalized > 0 && normalized < 0.01 ? 4 : 2, maximumFractionDigits: 4 })}`
  }

  function formatProbeCost(value) {
    const normalized = Number(value) || 0
    const digits = normalized > 0 && normalized < 0.0001 ? 8 : normalized > 0 && normalized < 0.01 ? 6 : 4
    return `$${normalized.toLocaleString('en-US', { minimumFractionDigits: 2, maximumFractionDigits: digits })}`
  }

  function formatPercent(value) {
    const normalized = Number(value)
    return Number.isFinite(normalized) ? `${normalized.toFixed(normalized % 1 ? 1 : 0)}%` : '—'
  }

  function escapeHTML(value) {
    return String(value ?? '').replace(/[&<>'"]/g, (character) => ({ '&': '&amp;', '<': '&lt;', '>': '&gt;', "'": '&#39;', '"': '&quot;' })[character])
  }

  function escapeAttr(value) { return escapeHTML(value) }
  function toCamel(value) { return value.replace(/-([a-z])/g, (_, letter) => letter.toUpperCase()) }
})()
