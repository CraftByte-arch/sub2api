(() => {
  'use strict'

  const AUTH_TOKEN_KEY = 'auth_token'
  const AUTH_REFRESH_TOKEN_KEY = 'refresh_token'
  const AUTH_USER_KEY = 'auth_user'
  const AUTH_EXPIRES_AT_KEY = 'token_expires_at'
  const AUTH_REFRESH_LOCK_NAME = 'sub2api-auth-token-refresh'
  const AUTH_REFRESH_BUFFER_MS = 120000
  const AUTH_REFRESH_TIMEOUT_MS = 30000
  const SIDECAR_RETURN_PATH = '/custom/account-auto-scheduler'
  const COLLAPSED_GROUPS_KEY = 'sub2api-auto-scheduler-collapsed-groups'
  const DETAIL_PAGE_SIZE = 10
  const state = {
    token: '',
    user: null,
    groups: [],
    accounts: [],
    groupUsage: null,
    groupUsageByID: new Map(),
    groupBalanceSummaries: {},
    groupProtectionDefaults: {},
    groupBalanceThresholds: {},
    actualSuccess: null,
    actualSuccessByKey: new Map(),
    notificationsAvailable: false,
    notificationSettings: null,
    editingID: null,
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
    onlineUsersSummary: null,
    onlineUsersSummaryLoading: false,
    onlineUsersSummaryError: '',
    onlineUsers: null,
    onlineUsersLoading: false,
    onlineUsersError: '',
    onlineUsersTrigger: null,
    groupConsumption: null,
    groupConsumptionGroupKey: null,
    groupConsumptionLoading: false,
    groupConsumptionError: '',
    groupConsumptionTrigger: null,
    groupConsumptionRequestID: 0,
    collapsedGroups: new Set(),
    expandedAccounts: new Set(),
    schedulableBusy: new Set(),
    protectionBusy: new Set(),
    protectionTarget: null,
    groupProtectionTarget: null,
    groupBalanceAlertTarget: null,
    notificationBusy: false,
    bindingAction: null,
    schedulingAction: null,
    activeTab: 'groups',
    upstreamWorkspace: null,
    pollTimer: null,
    onlinePollTimer: null,
    toastTimer: null,
  }

  const elements = {}
  let groupAccessWorkspace
  let authRefreshPromise = null
  let authRedirecting = false

  document.addEventListener('DOMContentLoaded', init)

  async function init() {
    cacheElements()
    captureEmbedContext()
    state.upstreamWorkspace = window.createUpstreamWorkspace({ api, showToast })
    groupAccessWorkspace = window.createGroupAccessWorkspace({ api, getGroups: () => state.groups })
    bindEvents()
    if (!readStoredAccessToken() && !readStoredRefreshToken()) {
      redirectToLogin()
      return
    }
    try {
      const session = await api('/api/session')
      state.user = session.user
      state.notificationsAvailable = Boolean(session.notifications_available)
      state.upstreamWorkspace.setCredentialsEnabled(session.credentials_enabled)
      elements.registerTabButton.hidden = !session.public_url
      elements.notificationSettingsButton.hidden = !state.notificationsAvailable
      elements.authState.hidden = true
      elements.app.hidden = false
      await loadOverview()
      await loadOnlineUsersSummary(true)
      state.pollTimer = window.setInterval(() => {
        if (document.visibilityState === 'visible' && !document.querySelector('dialog[open]')) {
          void loadOverview(true)
          if (state.activeTab === 'upstreams') void state.upstreamWorkspace.refresh(true)
        }
      }, 10000)
      state.onlinePollTimer = window.setInterval(() => {
        if (document.visibilityState === 'visible' && !document.querySelector('dialog[open]')) {
          void loadOnlineUsersSummary(true)
        }
      }, 60000)
    } catch (error) {
      if (!authRedirecting) showAuthError(error.message || '管理员身份验证失败')
    }
  }

  function cacheElements() {
    const ids = [
      'auth-state', 'auth-message', 'app', 'sync-label', 'register-tab-button', 'refresh-button',
      'search-input', 'status-filter', 'group-list', 'empty-state', 'no-match-state', 'metric-groups',
      'metric-accounts', 'metric-enabled', 'metric-active-traffic', 'online-users-metric', 'metric-online-window', 'metric-online-users',
      'config-dialog', 'config-form', 'config-title',
      'account-select', 'balance-alert-input', 'form-error', 'save-button', 'binding-dialog', 'binding-group-name',
      'binding-search-input', 'binding-change-count', 'binding-list', 'binding-error', 'binding-save-button',
      'account-detail-dialog', 'account-detail-title', 'account-detail-group-name', 'account-detail-list',
      'account-detail-page-label', 'account-detail-prev', 'account-detail-next',
      'manual-probe-dialog', 'manual-probe-form', 'manual-probe-account', 'manual-probe-models', 'manual-probe-custom-model', 'manual-probe-prompt', 'manual-probe-effort', 'manual-probe-results', 'manual-probe-submit',
      'online-users-dialog', 'online-users-window-label', 'online-users-summary', 'online-users-list', 'online-users-retry-button',
      'group-user-consumption-dialog', 'group-user-consumption-title', 'group-user-consumption-date',
      'group-user-consumption-summary', 'group-user-consumption-list', 'group-user-consumption-retry-button',
      'toast',
      'protection-dialog', 'protection-form', 'protection-account-name', 'protection-multiplier-input',
      'protection-final-preview', 'protection-error', 'protection-save-button', 'binding-action-dialog',
      'binding-action-title', 'binding-action-account-name', 'binding-action-message', 'binding-action-error',
      'binding-action-confirm-button',
      'scheduling-action-dialog', 'scheduling-action-title', 'scheduling-action-account-name',
      'scheduling-action-message', 'scheduling-action-error', 'scheduling-action-confirm-button',
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
    state.token = readStoredAccessToken()
    try {
      // Remove the legacy sidecar token, if an older page stored one.
      sessionStorage.removeItem('sub2api-auto-scheduler-token')
    } catch {
      // Ignore storage restrictions; authenticated requests will report the real cause.
    }
    state.collapsedGroups = readCollapsedGroups()
    document.documentElement.dataset.theme = url.searchParams.get('theme') === 'dark' ? 'dark' : 'light'
    for (const key of ['token', 'refresh_token', 'user_id', 'src_url']) url.searchParams.delete(key)
    window.history.replaceState(null, '', url.pathname + (url.search ? url.search : ''))
  }

  function bindEvents() {
    elements.groupsTab.addEventListener('click', () => setWorkspaceTab('groups'))
    elements.upstreamsTab.addEventListener('click', () => setWorkspaceTab('upstreams'))
    for (const tab of [elements.groupsTab, elements.upstreamsTab]) tab.addEventListener('keydown', handleWorkspaceTabKeydown)
    elements.refreshButton.addEventListener('click', () => {
      void loadOverview()
      void loadOnlineUsersSummary()
    })
    elements.onlineUsersMetric.addEventListener('click', (event) => openOnlineUsersDialog(event.currentTarget))
    elements.onlineUsersRetryButton.addEventListener('click', () => loadOnlineUsers())
    elements.groupUserConsumptionRetryButton.addEventListener('click', () => loadGroupUserConsumption())
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
    document.addEventListener('click', closeAccountActionMenus)
    elements.configForm.addEventListener('submit', saveConfig)
    elements.protectionForm.addEventListener('submit', saveProtection)
    elements.groupProtectionForm.addEventListener('submit', saveGroupProtection)
    elements.groupProtectionClearButton.addEventListener('click', clearGroupProtection)
    elements.groupBalanceAlertForm.addEventListener('submit', saveGroupBalanceAlert)
    elements.groupBalanceAlertClearButton.addEventListener('click', clearGroupBalanceAlert)
    elements.notificationSettingsForm.addEventListener('submit', saveNotificationSettings)
    elements.notificationTestButton.addEventListener('click', testNotificationSettings)
    elements.notificationClearButton.addEventListener('click', clearNotificationSettings)
    elements.bindingActionConfirmButton.addEventListener('click', confirmBindingAction)
    elements.schedulingActionConfirmButton.addEventListener('click', confirmSchedulingAction)
    window.addEventListener('storage', handleAuthStorageChange)
    elements.bindingSearchInput.addEventListener('input', (event) => {
      state.bindingSearch = event.target.value.trim().toLocaleLowerCase()
      renderBindingList()
    })
    elements.bindingList.addEventListener('change', handleBindingChange)
    elements.bindingSaveButton.addEventListener('click', saveBindings)
    elements.manualProbeForm.addEventListener('submit', submitManualProbe)
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
    elements.schedulingActionDialog.addEventListener('close', () => {
      if (elements.schedulingActionDialog.dataset.busy !== 'true') state.schedulingAction = null
    })
    elements.onlineUsersDialog.addEventListener('close', () => {
      const trigger = state.onlineUsersTrigger
      state.onlineUsersTrigger = null
      window.requestAnimationFrame(() => trigger?.isConnected && trigger.focus())
    })
    elements.groupUserConsumptionDialog.addEventListener('close', () => {
      const trigger = state.groupConsumptionTrigger
      state.groupConsumptionTrigger = null
      state.groupConsumptionGroupKey = null
      state.groupConsumption = null
      state.groupConsumptionError = ''
      state.groupConsumptionLoading = false
      state.groupConsumptionRequestID += 1
      window.requestAnimationFrame(() => trigger?.isConnected && trigger.focus())
    })
    document.addEventListener('keydown', (event) => {
      if (event.key !== 'Escape') return
      const openAccountMenu = document.querySelector('.account-action-menu[open]')
      if (openAccountMenu) {
        event.preventDefault()
        openAccountMenu.open = false
        openAccountMenu.querySelector('summary')?.focus()
        return
      }
      const dialogs = [...document.querySelectorAll('dialog[open]')]
      const activeDialog = dialogs.at(-1)
      if (!activeDialog) return
      event.preventDefault()
      if ((activeDialog === elements.bindingDialog && state.bindingSaving) || activeDialog.dataset.busy === 'true') return
      activeDialog.close()
    })
  }

  function handleAuthStorageChange(event) {
    if (![AUTH_TOKEN_KEY, AUTH_REFRESH_TOKEN_KEY, AUTH_EXPIRES_AT_KEY].includes(event.key)) return
    state.token = readStoredAccessToken()
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
      state.groupUsage = response.group_usage || null
      state.groupUsageByID = new Map((state.groupUsage?.items || []).map((item) => [Number(item.group_id), item]))
      state.groupBalanceSummaries = response.group_balance_summaries || {}
      state.groupProtectionDefaults = response.group_protection_defaults || {}
      state.groupBalanceThresholds = response.group_balance_thresholds || {}
      state.actualSuccess = response.actual_success || null
      state.actualSuccessByKey = new Map((state.actualSuccess?.items || []).map((item) => [relationKey(item.group_id, item.account_id), item]))
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

  async function loadOnlineUsers(silent = false) {
    if (state.onlineUsersLoading) return false
    state.onlineUsersLoading = true
    renderOnlineUsers()
    try {
      const response = await api('/api/online-users')
      state.onlineUsers = response || null
      state.onlineUsersError = ''
      adoptOnlineUsersSummary(response)
      renderOnlineUsers()
      return true
    } catch (error) {
      state.onlineUsersError = error.message || '在线用户数据暂不可用'
      state.onlineUsers = null
      renderOnlineUsers()
      if (!silent && !elements.onlineUsersDialog.open) showToast(state.onlineUsersError, true)
      return false
    } finally {
      state.onlineUsersLoading = false
      renderOnlineUsers()
    }
  }

  async function loadOnlineUsersSummary(silent = false) {
    if (state.onlineUsersSummaryLoading) return false
    state.onlineUsersSummaryLoading = true
    renderOnlineUsersMetric()
    try {
      const response = await api('/api/online-users/summary')
      state.onlineUsersSummary = response || null
      state.onlineUsersSummaryError = ''
      renderOnlineUsersMetric()
      return true
    } catch (error) {
      state.onlineUsersSummaryError = error.message || '在线用户数据暂不可用'
      renderOnlineUsersMetric()
      if (!silent) showToast(state.onlineUsersSummaryError, true)
      return false
    } finally {
      state.onlineUsersSummaryLoading = false
      renderOnlineUsersMetric()
    }
  }

  function adoptOnlineUsersSummary(snapshot) {
    if (!snapshot) return
    const incoming = new Date(snapshot.queried_at || 0).getTime()
    const current = new Date(state.onlineUsersSummary?.queried_at || 0).getTime()
    if (!state.onlineUsersSummary || !Number.isFinite(current) || incoming >= current) {
      state.onlineUsersSummary = snapshot
      state.onlineUsersSummaryError = ''
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
    const activeTraffic = Number(state.actualSuccess?.active_account_count)
    elements.metricActiveTraffic.textContent = state.actualSuccess?.ready && Number.isFinite(activeTraffic) ? formatInteger(activeTraffic) : '—'
    elements.metricActiveTraffic.title = state.actualSuccess?.notice || '最近一小时内有真实上游尝试的账号数量'
    renderOnlineUsersMetric()
  }

  function renderOnlineUsersMetric() {
    const snapshot = state.onlineUsersSummary
    const count = Number(snapshot?.count)
    const ready = snapshot?.ready === true
    elements.metricOnlineUsers.textContent = state.onlineUsersSummaryLoading && !snapshot
      ? '…'
      : ready && Number.isFinite(count) ? formatInteger(count) : snapshot || state.onlineUsersSummaryError ? '不可用' : '—'
    elements.metricOnlineWindow.textContent = snapshot?.window_minutes
      ? `最近 ${formatInteger(snapshot.window_minutes)} 分钟`
      : '最近 10 分钟'
    elements.onlineUsersWindowLabel.textContent = snapshot?.window_minutes
      ? `最近 ${formatInteger(snapshot.window_minutes)} 分钟内有调用的用户`
      : '最近 10 分钟内有调用的用户'
    elements.onlineUsersMetric.classList.toggle('metric-error', Boolean((state.onlineUsersSummaryError || snapshot?.ready === false) && !ready))
    elements.onlineUsersMetric.classList.toggle('metric-loading', state.onlineUsersSummaryLoading)
    elements.onlineUsersMetric.title = state.onlineUsersSummaryError && !ready
      ? `${state.onlineUsersSummaryError}；点击查看或重试`
      : snapshot?.notice || onlineAggregateFreshnessLabel(snapshot) || '查看最近十分钟有调用的用户'
    renderGroupOnlineCounts()
  }

  function onlineUsersGroupProjection(groupID) {
    const snapshot = state.onlineUsersSummary
    if (state.onlineUsersSummaryLoading && !snapshot) {
      return {
        label: '…',
        available: false,
        partial: false,
        title: '正在读取最近 10 分钟分组在线人数',
        ariaLabel: '正在读取分组在线人数',
      }
    }
    if (!snapshot || snapshot.ready !== true || snapshot.group_counts_available !== true) {
      return {
        label: '—',
        available: false,
        partial: false,
        title: state.onlineUsersSummaryError || snapshot?.notice || '最近 10 分钟分组在线人数暂不可用',
        ariaLabel: '分组在线人数暂不可用',
      }
    }
    const groupKey = String(Number(groupID))
    const rawCount = Object.prototype.hasOwnProperty.call(snapshot.group_counts || {}, groupKey)
      ? snapshot.group_counts[groupKey]
      : 0
    const count = Number(rawCount)
    if (!Number.isFinite(count) || count < 0) {
      return {
        label: '—',
        available: false,
        partial: Boolean(snapshot.group_counts_partial),
        title: '最近 10 分钟分组在线人数暂不可用',
        ariaLabel: '分组在线人数暂不可用',
      }
    }
    const formatted = formatInteger(count)
    const partial = Boolean(snapshot.group_counts_partial || snapshot.stale)
    return {
      label: formatted,
      available: true,
      partial,
      title: partial
        ? `最近 10 分钟内有调用的用户：${formatted} 人；${snapshot.notice || '聚合数据可能延迟'}`
        : `最近 10 分钟内有调用的用户：${formatted} 人`,
      ariaLabel: partial
        ? `最近 10 分钟分组在线人数 ${formatted} 人，聚合数据可能延迟`
        : `最近 10 分钟分组在线人数 ${formatted} 人`,
    }
  }

  function renderGroupOnlineCounts() {
    const badges = document.querySelectorAll('[data-group-online-count]')
    for (const badge of badges) {
      const projection = onlineUsersGroupProjection(badge.dataset.groupOnlineCount)
      const label = badge.querySelector('[data-group-online-label]')
      if (label) label.textContent = `在线 ${projection.label}`
      badge.title = projection.title
      badge.setAttribute('aria-label', projection.ariaLabel)
      badge.classList.toggle('unavailable', !projection.available)
      badge.classList.toggle('partial', projection.partial)
    }
  }

  function renderGroupOnlineBadge(groupID) {
    const projection = onlineUsersGroupProjection(groupID)
    return `<span class="group-online-users${projection.available ? '' : ' unavailable'}${projection.partial ? ' partial' : ''}" data-group-online-count="${escapeAttr(groupID)}" title="${escapeAttr(projection.title)}" aria-label="${escapeAttr(projection.ariaLabel)}"><span class="group-online-dot" aria-hidden="true"></span><span data-group-online-label>在线 ${escapeHTML(projection.label)}</span></span>`
  }

  function groupUsageProjection(groupID) {
    const normalizedID = Number(groupID)
    if (!Number.isInteger(normalizedID) || normalizedID <= 0) return null
    const snapshot = state.groupUsage
    if (!snapshot) return null
    if (snapshot.ready !== true) {
      const notice = snapshot.notice || '分组今日消耗暂不可用'
      return { available: false, stale: false, value: '暂不可用', title: notice }
    }
    const summary = state.groupUsageByID.get(normalizedID)
    const cost = Number(summary?.today_cost)
    if (!summary || !Number.isFinite(cost)) {
      return { available: false, stale: Boolean(snapshot.stale), value: '暂不可用', title: snapshot.notice || '该分组今日消耗暂不可用' }
    }
    const stale = Boolean(snapshot.stale)
    const queriedAt = snapshot.queried_at ? `更新于 ${formatDateTime(snapshot.queried_at)}` : ''
    const titleParts = [`今日实际消耗：${formatCurrency(cost)}`]
    if (stale) titleParts.push('当前为最近一次成功结果')
    if (queriedAt) titleParts.push(queriedAt)
    if (snapshot.notice) titleParts.push(snapshot.notice)
    return { available: true, stale, value: formatCurrency(cost), title: titleParts.join('；') }
  }

  function renderGroupTodayUsage(groupID) {
    const projection = groupUsageProjection(groupID)
    if (!projection) return ''
    return `<span class="group-today-usage${projection.available ? '' : ' unavailable'}${projection.stale ? ' stale' : ''}" title="${escapeAttr(projection.title)}" aria-label="${escapeAttr(`今日消耗 ${projection.value}`)}"><span>今日消耗</span><strong>${escapeHTML(projection.value)}</strong></span>`
  }

  function renderOnlineUsers() {
    renderOnlineUsersMetric()
    if (!elements.onlineUsersDialog?.open) return
    const snapshot = state.onlineUsers
    if (state.onlineUsersLoading && !snapshot) {
      elements.onlineUsersSummary.innerHTML = '<span class="online-users-loading">正在读取最近 10 分钟的聚合数据…</span>'
      elements.onlineUsersList.innerHTML = '<div class="online-users-skeleton" aria-hidden="true"><i></i><i></i><i></i></div>'
      elements.onlineUsersRetryButton.disabled = true
      return
    }
    elements.onlineUsersRetryButton.disabled = state.onlineUsersLoading
    if (state.onlineUsersError && !snapshot) {
      elements.onlineUsersSummary.innerHTML = `<span class="online-users-error">${escapeHTML(state.onlineUsersError)}</span>`
      elements.onlineUsersList.innerHTML = '<div class="dialog-empty">在线用户暂时无法读取，请点击“刷新列表”重试。</div>'
      return
    }
    if (snapshot && snapshot.ready !== true) {
      elements.onlineUsersSummary.innerHTML = `<span class="online-users-error">${escapeHTML(snapshot.notice || '在线人数聚合数据暂不可用')}</span>`
      elements.onlineUsersList.innerHTML = '<div class="dialog-empty">当前没有足够新的聚合数据，请稍后刷新。</div>'
      return
    }
    const count = Number(snapshot?.count) || 0
    const notices = snapshot?.notice ? `<span class="online-users-notice">${escapeHTML(snapshot.notice)}</span>` : ''
    const freshness = onlineAggregateFreshnessLabel(snapshot)
    const queriedAt = snapshot?.queried_at ? `查询于 ${escapeHTML(formatDateTime(snapshot.queried_at))}` : '刚刚查询'
    elements.onlineUsersSummary.innerHTML = `<strong>${formatInteger(count)} 人在线</strong><span>${escapeHTML(freshness || queriedAt)}</span>${notices}`
    const users = Array.isArray(snapshot?.users) ? snapshot.users : []
    elements.onlineUsersList.innerHTML = users.length ? users.map(renderOnlineUser).join('') : '<div class="dialog-empty">最近 10 分钟暂无用户调用。</div>'
  }

  function renderOnlineUser(user) {
    const id = Number(user?.id)
    const displayName = user?.display_name || `用户 #${Number.isFinite(id) ? id : '—'}`
    const email = user?.email || ''
    const cost = user?.today_cost
    const tokens = user?.today_tokens
    const requests = user?.today_requests
    const costLabel = cost === null || cost === undefined
      ? '<strong class="muted">金额暂不可用</strong>'
      : `<strong>${escapeHTML(formatCurrency(cost))}</strong>`
    const usageDetails = []
    if (requests !== null && requests !== undefined) usageDetails.push(`${formatInteger(requests)} 次调用`)
    if (tokens !== null && tokens !== undefined) usageDetails.push(`${formatCompact(tokens)} tokens`)
    if (!usageDetails.length) usageDetails.push('今日统计暂不可用')
    const consumption = `${costLabel}<span>${escapeHTML(usageDetails.join(' · '))}</span>`
    const lastCall = user?.last_call_at ? formatDateTime(user.last_call_at) : '—'
    return `<div class="online-user-row">
      <div class="online-user-identity"><span class="mobile-field-label">用户</span><strong>${escapeHTML(displayName)}</strong><span>${email ? `${escapeHTML(email)} · ` : ''}#${escapeHTML(id)}</span></div>
      <div class="online-user-last-call"><span class="mobile-field-label">最后调用</span><strong>${escapeHTML(lastCall)}</strong><span>${escapeHTML(formatOnlineAge(user?.last_call_at))}</span></div>
      <div class="online-user-consumption"><span class="mobile-field-label">今日消耗</span>${consumption}</div>
    </div>`
  }

  function formatOnlineAge(value) {
    const timestamp = new Date(value).getTime()
    if (!Number.isFinite(timestamp)) return '时间未知'
    const seconds = Math.max(0, Math.floor((Date.now() - timestamp) / 1000))
    if (seconds < 60) return `${seconds} 秒前`
    const minutes = Math.floor(seconds / 60)
    if (minutes < 60) return `${minutes} 分钟前`
    return `${Math.floor(minutes / 60)} 小时前`
  }

  function onlineAggregateFreshnessLabel(snapshot) {
    if (!snapshot) return ''
    const parts = []
    if (snapshot.data_through) parts.push(`数据截至 ${formatDateTime(snapshot.data_through)}`)
    const lag = Number(snapshot.aggregation_lag_seconds)
    if (Number.isFinite(lag) && lag >= 0) {
      parts.push(lag < 60 ? `延迟 ${formatInteger(lag)} 秒` : `延迟 ${formatInteger(Math.ceil(lag / 60))} 分钟`)
    }
    return parts.join(' · ')
  }

  async function openOnlineUsersDialog(trigger = null) {
    state.onlineUsersTrigger = trigger
    if (!elements.onlineUsersDialog.open) elements.onlineUsersDialog.showModal()
    renderOnlineUsers()
    await loadOnlineUsers()
  }

  async function openGroupUserConsumptionDialog(groupKey, trigger = null) {
    const view = buildGroupViews().find((item) => item.key === String(groupKey))
    if (!view || view.synthetic || Number(view.group.id) <= 0) return
    state.groupConsumptionGroupKey = view.key
    state.groupConsumptionTrigger = trigger
    state.groupConsumptionRequestID += 1
    state.groupConsumption = null
    state.groupConsumptionError = ''
    state.groupConsumptionLoading = false
    elements.groupUserConsumptionTitle.textContent = '今日用户消耗 Top 20'
    elements.groupUserConsumptionDate.textContent = `${view.group.name || `分组 ${view.group.id}`} · 正在读取当天统计`
    if (!elements.groupUserConsumptionDialog.open) elements.groupUserConsumptionDialog.showModal()
    renderGroupUserConsumption()
    await loadGroupUserConsumption()
  }

  async function loadGroupUserConsumption() {
    const groupKey = state.groupConsumptionGroupKey
    const requestID = state.groupConsumptionRequestID
    const view = buildGroupViews().find((item) => item.key === String(groupKey))
    const groupID = Number(view?.group?.id)
    if (!groupKey || !Number.isInteger(groupID) || groupID <= 0 || state.groupConsumptionLoading) return false
    state.groupConsumptionLoading = true
    state.groupConsumptionError = ''
    renderGroupUserConsumption()
    try {
      const response = await api(`/api/groups/${groupID}/user-consumption`)
      if (state.groupConsumptionGroupKey !== groupKey || state.groupConsumptionRequestID !== requestID) return false
      state.groupConsumption = response || null
      state.groupConsumptionError = ''
      renderGroupUserConsumption()
      return true
    } catch (error) {
      if (state.groupConsumptionGroupKey !== groupKey || state.groupConsumptionRequestID !== requestID) return false
      state.groupConsumption = null
      state.groupConsumptionError = error.message || '分组用户消耗暂不可用'
      renderGroupUserConsumption()
      return false
    } finally {
      if (state.groupConsumptionRequestID === requestID) {
        state.groupConsumptionLoading = false
        if (state.groupConsumptionGroupKey === groupKey) renderGroupUserConsumption()
      }
    }
  }

  function renderGroupUserConsumption() {
    if (!elements.groupUserConsumptionDialog?.open) return
    const snapshot = state.groupConsumption
    const currentView = buildGroupViews().find((item) => item.key === String(state.groupConsumptionGroupKey))
    const groupName = snapshot?.group_name || currentView?.group?.name || '当前分组'
    const date = snapshot?.date || '当天'
    elements.groupUserConsumptionDate.textContent = `${groupName} · ${date}`
    elements.groupUserConsumptionRetryButton.disabled = state.groupConsumptionLoading

    if (state.groupConsumptionLoading && !snapshot) {
      elements.groupUserConsumptionSummary.innerHTML = '<span class="group-consumption-loading">正在读取当天用户消耗…</span>'
      elements.groupUserConsumptionList.innerHTML = '<div class="group-consumption-skeleton" aria-hidden="true"><i></i><i></i><i></i></div>'
      return
    }
    if (state.groupConsumptionError && !snapshot) {
      elements.groupUserConsumptionSummary.innerHTML = `<span class="group-consumption-error">${escapeHTML(state.groupConsumptionError)}</span>`
      elements.groupUserConsumptionList.innerHTML = '<div class="dialog-empty">统计暂时无法读取，请点击“刷新统计”重试。</div>'
      return
    }

    const users = Array.isArray(snapshot?.users) ? snapshot.users : []
    const limit = Number(snapshot?.limit) || 20
    const notices = snapshot?.notice ? `<span class="group-consumption-notice">${escapeHTML(snapshot.notice)}</span>` : ''
    const queriedAt = snapshot?.queried_at ? `更新于 ${escapeHTML(formatDateTime(snapshot.queried_at))}` : '刚刚更新'
    const partial = snapshot?.partial ? '<span class="group-consumption-partial">部分字段暂不可用</span>' : ''
    elements.groupUserConsumptionSummary.innerHTML = `<strong>前 ${formatInteger(limit)} 位</strong><span>${escapeHTML(date)} · ${queriedAt}</span>${partial}${notices}`
    elements.groupUserConsumptionList.innerHTML = users.length
      ? users.map((user, index) => renderGroupUserConsumptionRow(user, index + 1)).join('')
      : '<div class="dialog-empty">当天暂无用户消耗记录。</div>'
  }

  function renderGroupUserConsumptionRow(user, rank) {
    const id = Number(user?.user_id)
    const displayName = user?.display_name || `用户 #${Number.isFinite(id) ? id : '—'}`
    const email = user?.email || ''
    const cost = user?.actual_cost
    const requests = user?.requests
    const tokens = user?.total_tokens
    const costValue = cost === null || cost === undefined
      ? '<strong class="muted">金额暂不可用</strong>'
      : `<strong>${escapeHTML(formatCurrency(cost))}</strong>`
    const requestValue = requests === null || requests === undefined ? '请求数暂不可用' : `${formatInteger(requests)} 次调用`
    const tokenValue = tokens === null || tokens === undefined ? 'Token 数暂不可用' : `${formatCompact(tokens)} tokens`
    return `<div class="group-consumption-row">
      <div class="group-consumption-identity"><span class="mobile-field-label">用户</span><strong><span class="consumption-rank">${formatInteger(rank)}</span>${escapeHTML(displayName)}</strong><span>${email && email !== displayName ? `${escapeHTML(email)} · ` : ''}#${escapeHTML(id)}</span></div>
      <div class="group-consumption-cost"><span class="mobile-field-label">今日消耗</span>${costValue}</div>
      <div class="group-consumption-metrics"><span class="mobile-field-label">请求 / Token</span><strong>${escapeHTML(requestValue)}</strong><span>${escapeHTML(tokenValue)}</span></div>
    </div>`
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
    const groupOnlineBadge = renderGroupOnlineBadge(view.group.id)
    const groupTodayUsage = renderGroupTodayUsage(view.group.id)
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
    const groupConsumptionButton = view.synthetic ? '' : `
      <button class="summary-button group-consumption-button" type="button" data-action="open-group-consumption" data-group-key="${escapeAttr(view.key)}" aria-haspopup="dialog" aria-controls="group-user-consumption-dialog" title="查看当前分组今日用户消耗前 20 位">
        <svg class="icon"><use href="#icon-activity"/></svg>
        <span>今日用户 Top 20</span>
      </button>`
    const detailButtons = [
      oauthAccounts.length ? renderDetailButton(view.key, 'oauth', `OAuth ${formatInteger(oauthAccounts.length)}`) : '',
      otherAccounts.length ? renderDetailButton(view.key, 'other', `其他 ${formatInteger(otherAccounts.length)}`) : '',
    ].join('')
    const balanceSummary = renderGroupBalanceSummary(view)
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
                ${view.group.id > 0 ? (view.group.is_exclusive
                  ? `<button class="group-access-badge exclusive" type="button" data-action="group-access" data-group-key="${view.group.id}" aria-label="${escapeAttr(`查看 ${view.group.name} 的授权用户`)}">专属分组 · 用户</button>`
                  : '<span class="group-access-badge">公开分组</span>') : ''}
                <span class="platform-tag">${escapeHTML(view.group.platform || 'all')}</span>
                ${groupStatus}${groupProtectionBadge}${groupBalanceBadge}
              </div>
              <p>${escapeHTML(view.group.description || `#${view.group.id || 'ungrouped'}`)}</p>
            </div>
          </div>
          <div class="group-stats" aria-label="分组账号状态、在线人数与今日消耗">
            ${groupTodayUsage}
            ${groupOnlineBadge}
            <span><strong class="text-success">${formatInteger(enabled)}</strong> / ${formatInteger(total)} 可用</span>
            <span>${formatInteger(apiKeys.length)} API Key</span>
            ${limited ? `<span class="text-warning">${formatInteger(limited)} 临时受限</span>` : ''}
            ${balanceSummary}
          </div>
          <div class="group-actions">${detailButtons}${groupConsumptionButton}${groupBalanceButton}${groupProtectionButton}${bindButton}</div>
        </header>
        <div id="${escapeAttr(contentID)}" class="api-key-table" ${collapsed ? 'hidden' : ''}>
          <div class="api-key-header" aria-hidden="true"><span>账号</span><span>调度状态</span><span>今日使用</span><span>可用余额</span><span>成功率 / 倍率</span><span>操作</span></div>
          ${apiKeys.length ? apiKeys.map((account) => renderAPIKeyAccount(account, view.group.id)).join('') : '<div class="group-empty">暂无 API Key 账号</div>'}
        </div>
      </article>`
  }

  function renderGroupBalanceSummary(view) {
    const summary = state.groupBalanceSummaries?.[String(view.group.id)]
    if (!summary) return ''
    const enabled = renderGroupBalanceBucket('启用余额', summary.enabled, 'enabled')
    const disabled = renderGroupBalanceBucket('未启用余额', summary.disabled, 'disabled')
    const groupName = view.group.name || `分组 ${view.group.id}`
    return `<span class="group-balance-summary" aria-label="${escapeAttr(`${groupName}启用与未启用账号余额汇总`)}">${enabled}${disabled}</span>`
  }

  function renderGroupBalanceBucket(label, bucket = {}, tone) {
    const accountCount = Math.max(0, Number(bucket.account_count) || 0)
    const numericCount = Math.max(0, Number(bucket.numeric_count) || 0)
    const unavailableCount = Math.max(0, Number(bucket.unavailable_count) || 0)
    const unlimitedCount = Math.max(0, Number(bucket.unlimited_count) || 0)
    const insufficientCount = Math.max(0, Number(bucket.insufficient_count) || 0)
    const remaining = Number(bucket.remaining)
    let value = '—'
    if (accountCount === 0) {
      value = '—'
    } else if (numericCount > 0 && Number.isFinite(remaining)) {
      value = formatCurrency(remaining)
    } else if (unlimitedCount === accountCount) {
      value = '不限额度'
    } else if (insufficientCount === accountCount) {
      value = '余额不足'
    } else {
      value = '暂不可用'
    }
    const notes = []
    if (accountCount === 0) notes.push('无账号')
    else notes.push(`${formatInteger(accountCount)} 个账号`)
    if (unavailableCount) notes.push(`${formatInteger(unavailableCount)} 个暂不可用`)
    if (unlimitedCount) notes.push(`${formatInteger(unlimitedCount)} 个不限额度`)
    if (insufficientCount) notes.push(`${formatInteger(insufficientCount)} 个余额不足`)
    const titleParts = [`${label}：${value}`]
    if (numericCount > 0 && Number.isFinite(remaining)) titleParts.push(`已汇总 ${formatInteger(numericCount)} 个有数值余额的账号`)
    if (unavailableCount) titleParts.push(`${formatInteger(unavailableCount)} 个账号没有可用余额投影，未按 0 计入`)
    if (unlimitedCount) titleParts.push(`${formatInteger(unlimitedCount)} 个不限额度账号未计入金额`)
    if (insufficientCount) titleParts.push(`${formatInteger(insufficientCount)} 个账号余额不足`)
    return `<span class="group-balance-item ${tone}" title="${escapeAttr(titleParts.join('；'))}"><span>${escapeHTML(label)}</span><strong>${escapeHTML(value)}</strong><small>${escapeHTML(notes.join(' · '))}</small></span>`
  }

  function renderDetailButton(groupKey, kind, label) {
    return `<button class="summary-button" type="button" data-action="open-detail" data-group-key="${escapeAttr(groupKey)}" data-kind="${escapeAttr(kind)}"><svg class="icon"><use href="#icon-users"/></svg><span>${escapeHTML(label)}</span></button>`
  }

  function renderAPIKeyAccount(account, groupID) {
    const protection = protectionFor(account, groupID)
    const scheduling = groupAccountState(account, groupID)
    const schedulable = schedulableState(account)
    const protectionKey = relationKey(groupID, account.id)
    const protectionBusy = state.protectionBusy.has(protectionKey)
    const accountKey = relationKey(groupID, account.id)
    const expanded = state.expandedAccounts.has(accountKey)
    const detailsID = `account-details-${groupID}-${account.id}`
    return `
      <section class="api-key-record ${expanded ? 'expanded' : ''}" data-account-id="${account.id}" data-group-id="${groupID}">
        <div class="api-key-row">
          <div class="account-cell">
            <div class="account-name-line"><strong>${escapeHTML(account.name || `账号 ${account.id}`)}</strong><span>#${account.id}</span></div>
            <div class="account-meta"><span class="platform-tag">${escapeHTML(account.platform || 'unknown')}</span><span>API Key</span></div>
          </div>
          <div class="schedule-cell">
            <div class="account-state ${escapeAttr(scheduling.tone)}"><i class="status-dot"></i><span><strong>${escapeHTML(scheduling.label)}</strong><small title="${escapeAttr(scheduling.reason)}">${escapeHTML(scheduling.reason)}</small></span></div>
          </div>
          <div class="usage-cell compact-usage">${renderTodayUsage(account)}</div>
          <div class="balance-cell">${renderCompactBalance(account, groupID)}</div>
          <div class="health-cell">${renderActualSuccessCompact(account, groupID)}${renderCompactMultiplier(account, protection)}</div>
          <div class="row-actions">
            <button class="icon-button account-expand-button" type="button" data-action="toggle-account-details" title="${expanded ? '收起账号详情' : '展开账号详情'}" aria-label="${expanded ? '收起' : '展开'} ${escapeAttr(account.name || `账号 ${account.id}`)} 详情" aria-expanded="${expanded ? 'true' : 'false'}" aria-controls="${escapeAttr(detailsID)}"><svg class="icon"><use href="#icon-chevron-right"/></svg></button>
            ${renderAccountActionMenu(account, groupID, protection, schedulable, protectionBusy)}
          </div>
        </div>
        <div id="${escapeAttr(detailsID)}" class="api-key-details" ${expanded ? '' : 'hidden'}>
          <section class="account-detail-section schedule-detail">
            <span class="detail-heading">调度与状态</span>
            ${renderSchedulableControl(account, schedulable)}
            <p class="detail-note">${escapeHTML(scheduling.reason)}</p>
          </section>
          <section class="account-detail-section balance-detail">
            <span class="detail-heading">额度与告警</span>
            ${renderQuota(account)}
            ${renderBalanceAlertSummary(account, groupID)}
          </section>
          <section class="account-detail-section success-detail">
            <span class="detail-heading">实际成功率</span>
            ${renderActualSuccess(account, groupID)}
          </section>
          <section class="account-detail-section multiplier-detail">
            <span class="detail-heading">倍率与保护</span>
            ${renderFinalMultiplier(account, protection)}
          </section>
          <div class="account-detail-actions">
            ${renderAccountDetailActions(account, groupID, protection, protectionBusy)}
          </div>
        </div>
      </section>`
  }

  function renderSchedulableControl(account, schedulable) {
    return `<div class="schedulable-control ${schedulable.busy ? 'busy' : ''}" title="${escapeAttr(schedulable.reason)}">
      <span class="automation-copy"><strong>账号调度（全局）</strong><small>${escapeHTML(schedulable.label)}</small></span>
      <label class="mini-switch">
        <span class="sr-only">${escapeHTML(account.name || `账号 ${account.id}`)}全局调度</span>
        <input type="checkbox" role="switch" data-action="schedulable-toggle" ${schedulable.enabled ? 'checked' : ''} ${schedulable.disabled ? 'disabled' : ''} aria-label="${escapeAttr(`${account.name || `账号 ${account.id}`}全局调度`)}">
        <i aria-hidden="true"></i>
      </label>
    </div>`
  }

  function renderAccountActionMenu(account, groupID, protection, schedulable, protectionBusy) {
    const accountName = account.name || `账号 ${account.id}`
    const schedulingLabel = account.schedulable ? '停止全局调度' : '启用全局调度'
    const protectionLabel = protection && !protection.inherited ? '编辑保护倍率' : '设置保护倍率'
    const relationItems = groupID > 0 ? `
      <button type="button" role="menuitem" data-action="edit-protection" ${protectionBusy ? 'disabled' : ''}>${escapeHTML(protectionLabel)}</button>
      ${protection?.status === 'rate_protected' && !protection.inherited ? `<button type="button" role="menuitem" data-action="release-protection" ${protectionBusy ? 'disabled' : ''}>解除倍率保护</button>` : ''}
      <button class="danger-menu-item" type="button" role="menuitem" data-action="remove-binding" ${protectionBusy ? 'disabled' : ''}>移除绑定</button>` : ''
    return `<details class="account-action-menu">
      <summary class="icon-button" title="更多操作" aria-label="${escapeAttr(`${accountName}更多操作`)}"><svg class="icon"><use href="#icon-more-horizontal"/></svg></summary>
      <div class="account-action-popover" role="menu" aria-label="${escapeAttr(`${accountName}账号操作`)}">
        <button type="button" role="menuitem" data-action="request-schedulable-toggle" ${schedulable.disabled ? 'disabled' : ''}>${escapeHTML(schedulingLabel)}</button>
        <button type="button" role="menuitem" data-action="edit">编辑余额告警</button>
        ${relationItems}
      </div>
    </details>`
  }

  function renderAccountDetailActions(account, groupID, protection, protectionBusy) {
    const protectionLabel = protection && !protection.inherited ? '编辑保护倍率' : '设置保护倍率'
    const relationActions = groupID > 0 ? `
      <button class="button secondary compact-account-action" type="button" data-action="edit-protection" ${protectionBusy ? 'disabled' : ''}>${escapeHTML(protectionLabel)}</button>
      ${protection?.status === 'rate_protected' && !protection.inherited ? `<button class="button warning compact-account-action" type="button" data-action="release-protection" ${protectionBusy ? 'disabled' : ''}>解除倍率保护</button>` : ''}
      <button class="button danger-outline compact-account-action" type="button" data-action="remove-binding" ${protectionBusy ? 'disabled' : ''}>移除绑定</button>` : ''
    return `<button class="button primary compact-account-action" type="button" data-action="manual-probe">手动探测</button><button class="button secondary compact-account-action" type="button" data-action="edit">编辑余额告警</button>${relationActions}`
  }

  function renderActualSuccessCompact(account, groupID) {
    const snapshot = state.actualSuccess
    const metric = state.actualSuccessByKey.get(relationKey(groupID, account.id))
    if (!snapshot?.ready) {
      const label = snapshot?.notice || '实际成功率暂不可用'
      return `<div class="compact-success unavailable" title="${escapeAttr(label)}" aria-label="${escapeAttr(label)}"><span>最近成功率</span><strong>不可用</strong><small>${escapeHTML(label)}</small></div>`
    }
    const recent = metric?.recent || {}
    const attempts = Math.max(0, Number(recent.effective_attempts) || 0)
    const successes = Math.max(0, Number(recent.success_count) || 0)
    const reference = metric?.reference_24h || {}
    const referenceAttempts = Math.max(0, Number(reference.effective_attempts) || 0)
    const freshness = snapshot.stale ? '数据已过期' : snapshot.partial ? '数据不完整' : ''
    if (attempts === 0) {
      const referenceText = referenceAttempts > 0
        ? `24h ${formatPercent((Number(reference.rate) || 0) * 100)} · ${formatInteger(reference.success_count || 0)}/${formatInteger(referenceAttempts)}`
        : '24h 暂无样本'
      const detail = ['最近一小时暂无真实调用', referenceText, freshness, snapshot.notice].filter(Boolean).join('；')
      return `<div class="compact-success empty ${snapshot.partial ? 'partial' : ''} ${snapshot.stale ? 'stale' : ''}" title="${escapeAttr(detail)}" aria-label="${escapeAttr(detail)}"><span>最近成功率</span><strong>暂无真实调用</strong><small>${escapeHTML(referenceText)}${freshness ? ` · ${escapeHTML(freshness)}` : ''}</small></div>`
    }
    const rate = Math.max(0, Math.min(1, Number(recent.rate) || 0))
    const tone = rate >= 0.99 ? 'success' : rate >= 0.95 ? 'warning' : 'danger'
    const details = [
      `近 1 小时成功 ${formatInteger(successes)} 次，有效尝试 ${formatInteger(attempts)} 次`,
      recent.low_sample ? '样本较少' : '',
      recent.last_observed_at ? `最近调用 ${formatDateTime(recent.last_observed_at)}` : '',
      freshness,
    ].filter(Boolean).join('；')
    return `<div class="compact-success ${tone} ${recent.low_sample ? 'low-sample' : ''} ${snapshot.partial ? 'partial' : ''} ${snapshot.stale ? 'stale' : ''}" title="${escapeAttr(details)}" aria-label="${escapeAttr(details)}"><span>最近成功率</span><strong>${escapeHTML(formatPercent(rate * 100))}</strong><small>${formatInteger(successes)}/${formatInteger(attempts)} · 近 1 小时${recent.low_sample ? ' · 样本少' : ''}</small></div>`
  }

  function renderActualSuccess(account, groupID) {
    const snapshot = state.actualSuccess
    const metric = state.actualSuccessByKey.get(relationKey(groupID, account.id))
    if (!snapshot?.ready) {
      const label = snapshot?.notice || '实际成功率暂不可用'
      return `<div class="actual-success unavailable" tabindex="0" data-tooltip="${escapeAttr(label)}" aria-label="${escapeAttr(label)}"><span class="data-label">最近实际成功率</span><strong>不可用</strong><span>${escapeHTML(label)}</span></div>`
    }
    const recent = metric?.recent || {}
    const attempts = Math.max(0, Number(recent.effective_attempts) || 0)
    const successes = Math.max(0, Number(recent.success_count) || 0)
    const reference = metric?.reference_24h || {}
    const referenceAttempts = Math.max(0, Number(reference.effective_attempts) || 0)
    const notices = []
    if (snapshot.stale) notices.push('数据已过期')
    if (snapshot.partial) notices.push('数据不完整')
    if (snapshot.collection_health?.status && snapshot.collection_health.status !== 'complete') {
      notices.push(`主服务采集降级：丢弃 ${formatInteger(snapshot.collection_health.dropped_samples || 0)} 条，待写入 ${formatInteger(snapshot.collection_health.pending_samples || 0)} 条`)
    }
    if (snapshot.notice) notices.push(snapshot.notice)
    const referenceText = referenceAttempts > 0
      ? `近 24 个完整小时 ${formatPercent((Number(reference.rate) || 0) * 100)}（${formatInteger(reference.success_count || 0)}/${formatInteger(referenceAttempts)}）`
      : '近 24 个完整小时暂无样本'
    const details = [
      referenceText,
      `客户端取消 ${formatInteger((recent.client_canceled_count || 0))} 次`,
      `发生切换 ${formatInteger((recent.failover_count || 0))} 次`,
      recent.last_observed_at ? `最近一次真实调用 ${formatDateTime(recent.last_observed_at)}` : '',
      snapshot.data_through ? `近 1 小时统计截至 ${formatDateTime(snapshot.data_through)}` : '',
      snapshot.reference_through ? `24 小时统计截至 ${formatDateTime(snapshot.reference_through)}` : '',
      ...notices,
    ].filter(Boolean).join('；')
    const freshness = snapshot.stale ? '数据已过期' : snapshot.partial ? '数据不完整' : ''
    const freshnessLine = freshness ? `<span class="actual-success-state">${escapeHTML(freshness)}</span>` : ''
    if (attempts === 0) {
      return `<div class="actual-success empty ${snapshot.partial ? 'partial' : ''} ${snapshot.stale ? 'stale' : ''}" tabindex="0" data-tooltip="${escapeAttr(details)}" aria-label="最近一小时暂无真实调用；${escapeAttr(details)}"><span class="data-label">最近实际成功率</span><strong>暂无真实调用</strong><span>${escapeHTML(referenceText)}</span>${freshnessLine}</div>`
    }
    const rate = Math.max(0, Math.min(1, Number(recent.rate) || 0))
    const tone = rate >= 0.99 ? 'success' : rate >= 0.95 ? 'warning' : 'danger'
    const sampleLabel = recent.low_sample ? ' · 样本较少' : ''
    const primary = `${formatPercent(rate * 100)}${snapshot.stale ? ' · 已过期' : ''}`
    const counts = `${formatInteger(successes)} / ${formatInteger(attempts)} 次 · 近 1 小时${sampleLabel}`
    const aria = `最近一小时实际成功率 ${formatPercent(rate * 100)}，成功 ${formatInteger(successes)} 次，有效尝试 ${formatInteger(attempts)} 次${recent.low_sample ? '，样本较少' : ''}；${details}`
    return `<div class="actual-success ${tone} ${recent.low_sample ? 'low-sample' : ''} ${snapshot.partial ? 'partial' : ''} ${snapshot.stale ? 'stale' : ''}" tabindex="0" data-tooltip="${escapeAttr(details)}" aria-label="${escapeAttr(aria)}"><span class="data-label">最近实际成功率</span><strong>${escapeHTML(primary)}</strong><span>${escapeHTML(counts)}</span>${freshnessLine}</div>`
  }

  function renderTodayUsage(account) {
    const usage = account.today_usage || {}
    const cache = usage.cache
    let cacheLabel = '缓存命中 —'
    let cacheDetail = '缓存统计暂不可用；今日请求、总 tokens 和成本仍使用批量统计结果'
    if (cache && typeof cache === 'object' && Number.isFinite(Number(cache.hit_rate))) {
      const hitRate = Math.max(0, Math.min(Number(cache.hit_rate), 100))
      const inputTokens = Math.max(Number(cache.input_tokens) || 0, 0)
      const cacheCreationTokens = Math.max(Number(cache.cache_creation_tokens) || 0, 0)
      const cacheReadTokens = Math.max(Number(cache.cache_read_tokens) || 0, 0)
      const promptTokens = Math.max(Number(cache.prompt_tokens) || 0, 0)
      cacheLabel = `缓存命中 ${hitRate.toFixed(1)}%`
      cacheDetail = `缓存读取 ${formatCompact(cacheReadTokens)} / 提示词 ${formatCompact(promptTokens)} tokens；普通输入 ${formatCompact(inputTokens)}；缓存写入 ${formatCompact(cacheCreationTokens)}`
    }
    return `<strong>${formatInteger(usage.requests || 0)} 请求 · ${formatCompact(usage.tokens || 0)} tokens</strong><span title="${escapeAttr(cacheDetail)}">${formatCurrency(usage.cost || 0)} 今日成本 · ${escapeHTML(cacheLabel)}</span>`
  }

  function balancePresentation(account) {
    const dimensions = [
      { label: '日额度', used: account.quota_daily_used, limit: account.quota_daily_limit },
      { label: '周额度', used: account.quota_weekly_used, limit: account.quota_weekly_limit },
      { label: '总额度', used: account.quota_used, limit: account.quota_limit },
    ].filter((dimension) => Number.isFinite(Number(dimension.limit)) && Number(dimension.limit) > 0)
    const balance = account.admin_balance || {}
    const configured = typeof balance.configured === 'boolean' ? balance.configured : dimensions.length > 0
    const totalLimit = Number(account.quota_limit)
    const totalUsed = Number(account.quota_used) || 0
    const projectedRemaining = Number(balance.remaining)
    const fallbackRemaining = Number.isFinite(totalLimit) && totalLimit > 0 ? Math.max(totalLimit - totalUsed, 0) : NaN
    const remaining = Number.isFinite(projectedRemaining) ? projectedRemaining : fallbackRemaining
    const exhausted = Array.isArray(balance.exhausted_dimensions) ? balance.exhausted_dimensions : []
    const insufficient = Boolean(balance.insufficient) || exhausted.length > 0
    const dimensionLabels = { daily: '日额度', weekly: '周额度', total: '总额度' }
    const exhaustedLabel = exhausted.map((dimension) => dimensionLabels[dimension]).filter(Boolean).join('、')
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
    const label = insufficient ? `管理员余额不足，${note}` : `管理员余额：${value}，${note}`
    return { balance, configured, dimensions, tone, value, note, label, insufficient }
  }

  function renderCompactBalance(account, groupID) {
    const view = balancePresentation(account)
    const threshold = effectiveBalanceThreshold(account, groupID)
    const source = view.balance.managed ? '上游同步' : view.configured ? '管理员配置' : '不限额度'
    const thresholdLabel = threshold ? `阈值 ${formatCurrency(threshold.value)}` : '未设告警'
    const detail = `${view.label}；${source}；${threshold ? `${threshold.source}告警阈值 ${formatCurrency(threshold.value)}` : '未设置余额告警阈值'}`
    return `<div class="compact-balance ${escapeAttr(view.tone)}" title="${escapeAttr(detail)}" aria-label="${escapeAttr(detail)}"><strong>${escapeHTML(view.value)}</strong><span>${escapeHTML(source)} · ${escapeHTML(thresholdLabel)}</span></div>`
  }

  function renderQuota(account) {
    const view = balancePresentation(account)
    const source = view.balance.managed ? '<span class="admin-balance-source">上游同步</span>' : ''
    const detail = view.dimensions.length
      ? `<dl class="quota-list">${view.dimensions.map((dimension) => quotaDimension(dimension.label, dimension.used, dimension.limit)).join('')}</dl>`
      : ''
    return `<div class="quota-summary"><div class="admin-balance-card ${escapeAttr(view.tone)}" aria-label="${escapeAttr(view.label)}"><div class="admin-balance-heading"><span>管理员余额</span>${source}</div><strong class="admin-balance-value">${escapeHTML(view.value)}</strong><span class="admin-balance-note">${escapeHTML(view.note)}</span></div>${detail}</div>`
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

  function renderCompactMultiplier(account, protection = null) {
    const projection = account.upstream_final_multiplier || {}
    const protectedFinal = Number(protection?.final_multiplier)
    const finalMultiplier = Number.isFinite(protectedFinal) ? protectedFinal : Number(projection.final_multiplier)
    const finalAvailable = Number.isFinite(finalMultiplier) && (protection || projection.status === 'available')
    const finalLabel = finalAvailable ? `${formatMultiplier(finalMultiplier)}x` : '未计算'
    const protectionSource = protection?.inherited ? '分组默认' : '账号级'
    const protectionLabel = protection ? `${formatMultiplier(protection.protection_multiplier)}x` : '未设置'
    const protectionState = protection?.status === 'rate_protected'
      ? '已触发'
      : protection?.status === 'multiplier_unavailable'
        ? '待倍率'
        : protection?.status === 'rebind_pending'
          ? '待回绑'
          : protection ? protectionSource : '沿用绑定'
    const projectionStatus = String(projection.status || 'unavailable')
    const statusLabels = {
      unbound: '未绑定上游 Key',
      stale: '上游 Key 绑定已失效',
      ambiguous: '存在多个有效上游 Key 绑定',
      recharge_unset: '充值倍率未设置',
      group_multiplier_unknown: '分组倍率未知',
      invalid_upstream: '上游地址无效',
      unavailable: '最终倍率暂不可用',
    }
    const detail = finalAvailable
      ? `最终倍率 ${finalLabel}；保护倍率 ${protectionLabel}；${protectionState}`
      : `${protection?.last_error || statusLabels[projectionStatus] || '最终倍率未计算'}；保护倍率 ${protectionLabel}`
    const tone = protection?.status === 'rate_protected'
      ? 'warning'
      : finalAvailable ? 'available' : 'unavailable'
    return `<div class="compact-multiplier ${escapeAttr(tone)}" title="${escapeAttr(detail)}" aria-label="${escapeAttr(detail)}"><span>最终 <strong>${escapeHTML(finalLabel)}</strong></span><small>保护 ${escapeHTML(protectionLabel)} · ${escapeHTML(protectionState)}</small></div>`
  }

  function schedulableState(account) {
    const enabled = Boolean(account.schedulable)
    const busy = state.schedulableBusy.has(account.id)
    if (busy) return { enabled, disabled: true, busy: true, label: '保存中', reason: '正在更新账号的全局调度状态' }
    if (!enabled && account.status !== 'active') {
      return { enabled: false, disabled: true, busy: false, label: account.status === 'error' ? '账号异常' : '账号未启用', reason: '仅 active 账号可以启用全局调度' }
    }
    if (!enabled) {
      return { enabled: false, disabled: false, busy: false, label: '管理员停止', reason: '账号当前不参与任何分组的调度' }
    }
    if (account.status !== 'active') {
      return { enabled: true, disabled: false, busy: false, label: '异常但仍启用', reason: '账号不是 active，仍可手动停止全局调度' }
    }
    return { enabled: true, disabled: false, busy: false, label: '当前启用', reason: '账号当前可在所有已绑定分组中参与调度' }
  }

  function accountState(account) {
    if (account.status === 'error') {
      return { key: 'error', tone: 'danger', label: '账号异常', reason: account.error_message || '账号状态为 error' }
    }
    if (account.status !== 'active') {
      return { key: 'inactive', tone: 'neutral', label: '账号停用', reason: '账号状态由管理员设为 inactive' }
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
    if (action === 'group-access') {
      groupAccessWorkspace.open(Number(button.dataset.groupKey), button)
      return
    }
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
    if (action === 'open-group-consumption') {
      await openGroupUserConsumptionDialog(button.dataset.groupKey, button)
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
    button.closest('.account-action-menu')?.removeAttribute('open')
    switch (action) {
      case 'manual-probe': openManualProbe(account); break
      case 'toggle-account-details': toggleAccountDetails(account.id, groupID); break
      case 'request-schedulable-toggle': openSchedulingActionDialog(account, !account.schedulable); break
      case 'edit': openEditDialog(account); break
      case 'edit-protection': openProtectionDialog(account, groupID); break
      case 'release-protection': openBindingActionDialog('release', account, groupID); break
      case 'remove-binding': openBindingActionDialog('remove', account, groupID); break
    }
  }

  function openManualProbe(account) {
    state.manualProbeAccount = account
    elements.manualProbeAccount.textContent = account.name || `账号 ${account.id}`
    const models = [...new Set([account.model, ...(account.models || []), 'gpt-4o-mini', 'claude-3-5-sonnet'])].filter(Boolean)
    elements.manualProbeModels.innerHTML = models.map((m, i) => `<label class="manual-probe-model-option"><input type="checkbox" value="${escapeAttr(m)}" ${i === 0 ? 'checked' : ''}> ${escapeHTML(m)}</label>`).join('')
    elements.manualProbeResults.innerHTML = ''
    elements.manualProbeDialog.showModal()
  }

  async function submitManualProbe(event) {
    event.preventDefault()
    const account = state.manualProbeAccount
    if (!account) return
    const models = [...elements.manualProbeModels.querySelectorAll('input:checked')].map(x => x.value)
    const custom = elements.manualProbeCustomModel.value.trim()
    if (custom && !models.includes(custom)) models.push(custom)
    if (!models.length || models.length > 8) { elements.manualProbeResults.textContent = '请选择 1 到 8 个模型'; return }
    elements.manualProbeSubmit.disabled = true
    elements.manualProbeResults.innerHTML = '<p>探测中，请稍候…</p>'
    try {
      const data = await api(`/api/accounts/${account.id}/manual-probe`, { method: 'POST', body: JSON.stringify({ models, prompt: elements.manualProbePrompt.value, reasoning_effort: elements.manualProbeEffort.value }) })
      elements.manualProbeResults.innerHTML = data.results.map(r => `<div class="manual-probe-result ${r.outcome?.success ? 'success' : 'error'}"><strong>${escapeHTML(r.model)}</strong><span>${r.outcome?.success ? `成功 · ${escapeHTML(formatMilliseconds(r.outcome.latency))}` : escapeHTML(r.error || r.outcome?.error_message || '失败')}</span></div>`).join('')
    } catch (error) { elements.manualProbeResults.textContent = error.message || '探测失败' }
    finally { elements.manualProbeSubmit.disabled = false }
  }

  function toggleAccountDetails(accountID, groupID) {
    const key = relationKey(groupID, accountID)
    if (state.expandedAccounts.has(key)) state.expandedAccounts.delete(key)
    else state.expandedAccounts.add(key)
    render()
    window.requestAnimationFrame(() => {
      const selector = `[data-account-id="${accountID}"][data-group-id="${groupID}"] [data-action="toggle-account-details"]`
      document.querySelector(selector)?.focus()
    })
  }

  function closeAccountActionMenus(event) {
    const activeMenu = event.target.closest?.('.account-action-menu') || null
    document.querySelectorAll('.account-action-menu[open]').forEach((menu) => {
      if (menu !== activeMenu) menu.open = false
    })
  }

  async function handleGroupChange(event) {
    const action = event.target.dataset.action
    if (action !== 'schedulable-toggle') return
    const row = event.target.closest('[data-account-id]')
    const account = findAccount(Number(row?.dataset.accountId))
    if (!account) return
    const enabled = event.target.checked
    if (action === 'schedulable-toggle') {
      event.target.checked = Boolean(account.schedulable)
      if (!enabled || account.config?.managed_suspended) {
        openSchedulingActionDialog(account, enabled)
        return
      }
      try {
        await setAccountSchedulable(account.id, enabled)
        showToast('账号全局调度已启用')
      } catch (error) {
        showToast(error.message || '更新账号调度失败', true)
      }
      return
    }
  }

  async function setAccountSchedulable(accountID, schedulable) {
    state.schedulableBusy.add(accountID)
    render()
    try {
      await api(`/api/accounts/${accountID}/schedulable`, { method: 'PUT', body: { schedulable } })
      await loadOverview(true)
    } finally {
      state.schedulableBusy.delete(accountID)
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

  function openEditDialog(account) {
    if (!isAPIKey(account)) return
    state.editingID = account.id
    elements.configTitle.textContent = '账号设置'
    elements.accountSelect.innerHTML = accountOption(account)
    elements.accountSelect.value = String(account.id)
    elements.accountSelect.disabled = true
    const threshold = account.config?.balance_alert_threshold
    elements.balanceAlertInput.value = Number.isFinite(Number(threshold)) && threshold !== null ? String(threshold) : ''
    elements.formError.hidden = true
    elements.saveButton.disabled = false
    elements.saveButton.textContent = '保存设置'
    elements.configDialog.showModal()
  }

  async function saveConfig(event) {
    event.preventDefault()
    if (!elements.configForm.reportValidity()) return
    const accountID = state.editingID
    if (!accountID) return
    elements.saveButton.disabled = true
    elements.saveButton.textContent = '保存中'
    elements.formError.hidden = true
    try {
      const thresholdRaw = elements.balanceAlertInput.value.trim()
      if (thresholdRaw) {
        const threshold = Number(thresholdRaw)
        if (!Number.isFinite(threshold) || threshold < 0) throw new Error('余额告警阈值必须是大于等于 0 的有限数值')
        await api(`/api/configs/${accountID}/balance-alert`, { method: 'PUT', body: { threshold } })
      } else {
        await api(`/api/configs/${accountID}/balance-alert`, { method: 'DELETE' })
      }
      elements.configDialog.close()
      showToast('账号余额告警设置已保存')
      await loadOverview(true)
    } catch (error) {
      elements.formError.textContent = error.message || '保存失败'
      elements.formError.hidden = false
    } finally {
      elements.saveButton.disabled = false
      elements.saveButton.textContent = '保存设置'
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

  function openSchedulingActionDialog(account, schedulable) {
    if (!account) return
    state.schedulingAction = { accountID: account.id, schedulable }
    elements.schedulingActionDialog.dataset.busy = 'false'
    elements.schedulingActionAccountName.textContent = `${account.name || `账号 ${account.id}`} · 影响该账号所在的所有分组`
    elements.schedulingActionError.hidden = true
    elements.schedulingActionConfirmButton.disabled = false
    if (schedulable) {
      elements.schedulingActionTitle.textContent = '恢复账号全局调度'
      elements.schedulingActionMessage.textContent = '确认后该账号会重新参与所有已绑定分组的调度。实际成功率仅用于展示，不会自动改变调度状态。'
      elements.schedulingActionConfirmButton.textContent = '确认启用'
      elements.schedulingActionConfirmButton.className = 'button primary'
    } else {
      elements.schedulingActionTitle.textContent = '停止账号全局调度'
      elements.schedulingActionMessage.textContent = '确认后该账号会立即退出所有已绑定分组的调度，之后需要你手动恢复。实际成功率不会自动启用或停止账号。'
      elements.schedulingActionConfirmButton.textContent = '确认停止'
      elements.schedulingActionConfirmButton.className = 'button danger'
    }
    document.querySelectorAll('[data-close-dialog="scheduling-action-dialog"]').forEach((button) => { button.disabled = false })
    elements.schedulingActionDialog.showModal()
    window.requestAnimationFrame(() => elements.schedulingActionConfirmButton.focus())
  }

  async function confirmSchedulingAction() {
    if (!state.schedulingAction || elements.schedulingActionDialog.dataset.busy === 'true') return
    const { accountID, schedulable } = state.schedulingAction
    elements.schedulingActionDialog.dataset.busy = 'true'
    elements.schedulingActionConfirmButton.disabled = true
    elements.schedulingActionError.hidden = true
    document.querySelectorAll('[data-close-dialog="scheduling-action-dialog"]').forEach((button) => { button.disabled = true })
    const originalText = elements.schedulingActionConfirmButton.textContent
    elements.schedulingActionConfirmButton.textContent = '处理中…'
    try {
      await setAccountSchedulable(accountID, schedulable)
      elements.schedulingActionDialog.close()
      state.schedulingAction = null
      showToast(schedulable ? '账号全局调度已启用' : '账号全局调度已停止')
    } catch (error) {
      elements.schedulingActionError.textContent = error.message || '更新账号调度失败'
      elements.schedulingActionError.hidden = false
    } finally {
      elements.schedulingActionDialog.dataset.busy = 'false'
      elements.schedulingActionConfirmButton.disabled = false
      elements.schedulingActionConfirmButton.textContent = originalText
      document.querySelectorAll('[data-close-dialog="scheduling-action-dialog"]').forEach((button) => { button.disabled = false })
    }
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

  function readStorageValue(key) {
    try {
      return String(localStorage.getItem(key) || '').trim()
    } catch {
      return ''
    }
  }

  function readStoredAccessToken() {
    return readStorageValue(AUTH_TOKEN_KEY)
  }

  function readStoredRefreshToken() {
    return readStorageValue(AUTH_REFRESH_TOKEN_KEY)
  }

  function readStoredTokenExpiresAt() {
    const value = Number(readStorageValue(AUTH_EXPIRES_AT_KEY))
    return Number.isFinite(value) ? value : 0
  }

  function persistRefreshedTokenPair(tokens) {
    const accessToken = String(tokens?.access_token || '').trim()
    const refreshToken = String(tokens?.refresh_token || '').trim()
    const expiresIn = Number(tokens?.expires_in)
    if (!accessToken) throw new Error('刷新接口未返回 access token')
    try {
      localStorage.setItem(AUTH_TOKEN_KEY, accessToken)
      if (Number.isFinite(expiresIn) && expiresIn > 0) {
        localStorage.setItem(AUTH_EXPIRES_AT_KEY, String(Date.now() + expiresIn * 1000))
      }
      if (refreshToken) localStorage.setItem(AUTH_REFRESH_TOKEN_KEY, refreshToken)
    } catch {
      // Keep the in-memory token usable for this request even if storage is unavailable.
    }
    state.token = accessToken
    return accessToken
  }

  function clearStoredAuth() {
    try {
      for (const key of [AUTH_TOKEN_KEY, AUTH_REFRESH_TOKEN_KEY, AUTH_USER_KEY, AUTH_EXPIRES_AT_KEY]) {
        localStorage.removeItem(key)
      }
    } catch {
      // Ignore storage restrictions; navigation to login is still the recovery path.
    }
    state.token = ''
  }

  function authError(message, status = 401, code = 'TOKEN_REFRESH_FAILED') {
    const error = new Error(message)
    error.status = status
    error.code = code
    return error
  }

  function isTerminalRefreshFailure(error) {
    const status = Number(error?.status)
    return error?.code === 'NO_REFRESH_TOKEN' || [400, 401, 403].includes(status)
  }

  function handleRefreshFailure(error) {
    if (isTerminalRefreshFailure(error) || !readStoredRefreshToken()) {
      clearStoredAuth()
      redirectToLogin()
    }
  }

  async function requestAccessTokenRefresh(force = false, failedAccessToken = '') {
    const refreshToken = readStoredRefreshToken()
    if (!refreshToken) throw authError('没有可用的管理员续期凭证', 401, 'NO_REFRESH_TOKEN')

    const currentAccessToken = readStoredAccessToken()
    const expiresAt = readStoredTokenExpiresAt()
    if (currentAccessToken && failedAccessToken && currentAccessToken !== failedAccessToken) {
      state.token = currentAccessToken
      return currentAccessToken
    }
    if (!force && currentAccessToken && (!expiresAt || expiresAt > Date.now() + AUTH_REFRESH_BUFFER_MS)) {
      state.token = currentAccessToken
      return currentAccessToken
    }

    const controller = new AbortController()
    const timeoutID = window.setTimeout(() => controller.abort(), AUTH_REFRESH_TIMEOUT_MS)
    let response
    let payload
    try {
      response = await fetch(new URL('/api/v1/auth/refresh', window.location.origin), {
        method: 'POST',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify({ refresh_token: refreshToken }),
        credentials: 'same-origin',
        signal: controller.signal,
      })
      payload = await response.json().catch(() => ({}))
    } catch (error) {
      throw error?.name === 'AbortError' ? authError('管理员会话续期超时', 0, 'TOKEN_REFRESH_TIMEOUT') : error
    } finally {
      window.clearTimeout(timeoutID)
    }

    const data = payload?.data || payload
    const apiFailure = Object.prototype.hasOwnProperty.call(payload || {}, 'code') && payload.code !== 0
    if (!response.ok || apiFailure || !data?.access_token) {
      throw authError(payload?.message || `管理员会话续期失败 (${response.status})`, response.status, payload?.code || 'TOKEN_REFRESH_FAILED')
    }

    // A different tab may have rotated the one-time refresh token while this request was in flight.
    if (readStoredRefreshToken() !== refreshToken) {
      const peerToken = readStoredAccessToken()
      if (peerToken) {
        state.token = peerToken
        return peerToken
      }
      throw authError('管理员会话已在其他标签页更新', 401, 'AUTH_SESSION_CHANGED')
    }
    return persistRefreshedTokenPair(data)
  }

  async function refreshAccessToken(force = false, failedAccessToken = '') {
    if (authRefreshPromise) return authRefreshPromise
    const pending = (async () => {
      const refresh = () => requestAccessTokenRefresh(force, failedAccessToken)
      if (navigator.locks?.request) return navigator.locks.request(AUTH_REFRESH_LOCK_NAME, refresh)
      return refresh()
    })()
    authRefreshPromise = pending
    try {
      return await pending
    } finally {
      if (authRefreshPromise === pending) authRefreshPromise = null
    }
  }

  function isSidecarAuthFailure(response, payload) {
    if (response.status === 401) return true
    return response.status === 403 && ['FORBIDDEN', 'UNAUTHORIZED'].includes(String(payload?.code || '').toUpperCase())
  }

  function redirectToLogin() {
    if (authRedirecting) return
    authRedirecting = true
    const target = `/login?redirect=${encodeURIComponent(SIDECAR_RETURN_PATH)}`
    try {
      if (window.top && window.top !== window) {
        window.top.location.assign(target)
        return
      }
    } catch {
      // Fall back to navigating the current browsing context.
    }
    window.location.assign(target)
  }

  async function api(path, options = {}, allowAuthRetry = true) {
    let token = readStoredAccessToken()
    if (!token) {
      try {
        token = await refreshAccessToken()
      } catch (error) {
        handleRefreshFailure(error)
        throw error
      }
    }
    state.token = token
    const headers = { Authorization: `Bearer ${token}` }
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
      if (allowAuthRetry && isSidecarAuthFailure(response, payload)) {
        try {
          const refreshedToken = await refreshAccessToken(true, token)
          state.token = refreshedToken
          return api(path, options, false)
        } catch (refreshError) {
          handleRefreshFailure(refreshError)
          throw refreshError
        }
      }
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

  function formatDateTime(value) {
    const date = value instanceof Date ? value : new Date(value)
    if (Number.isNaN(date.getTime())) return '—'
    return new Intl.DateTimeFormat('zh-CN', { month: '2-digit', day: '2-digit', hour: '2-digit', minute: '2-digit', second: '2-digit', hour12: false }).format(date)
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
