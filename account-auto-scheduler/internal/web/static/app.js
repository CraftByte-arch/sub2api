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
      state.upstreamWorkspace.setCredentialsEnabled(session.credentials_enabled)
      elements.registerTabButton.hidden = !session.public_url
      elements.authState.hidden = true
      elements.app.hidden = false
      await loadOverview()
      state.pollTimer = window.setInterval(() => {
        if (document.visibilityState === 'visible' && !document.querySelector('dialog[open]')) {
          loadOverview(true)
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
      'enabled-input', 'prompt-input', 'form-error', 'save-button', 'binding-dialog', 'binding-group-name',
      'direct-probe-control', 'direct-probe-state', 'direct-probe-message', 'direct-probe-authorize-button', 'direct-probe-revoke-button',
      'binding-search-input', 'binding-change-count', 'binding-list', 'binding-error', 'binding-save-button',
      'account-detail-dialog', 'account-detail-title', 'account-detail-group-name', 'account-detail-list',
      'account-detail-page-label', 'account-detail-prev', 'account-detail-next', 'delete-dialog',
      'delete-account-name', 'confirm-delete-button', 'result-dialog', 'result-account-name', 'result-details', 'toast',
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
      accounts: state.accounts.filter((account) => (account.group_ids || []).includes(group.id)),
      synthetic: false,
    }))
    const ungrouped = state.accounts.filter((account) => !(account.group_ids || []).some((id) => validGroupIDs.has(id)))
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
    const accounts = view.accounts.filter((account) => accountMatchesFilters(account, groupMatchesSearch))
    const noFilters = !state.search && state.status === 'all'
    if (!noFilters && accounts.length === 0) return null
    return { ...view, visibleAccounts: noFilters ? view.accounts : accounts }
  }

  function accountMatchesFilters(account, groupMatchesSearch = false) {
    const accountView = accountState(account)
    if (state.status !== 'all' && accountView.key !== state.status) return false
    if (!state.search || groupMatchesSearch) return true
    const haystack = `${account.name || ''} ${account.platform || ''} ${account.type || ''} ${account.id}`.toLocaleLowerCase()
    return haystack.includes(state.search)
  }

  function renderGroup(view) {
    const visible = sortAccounts(view.visibleAccounts || view.accounts)
    const apiKeys = visible.filter(isAPIKey)
    const oauthAccounts = visible.filter(isOAuthLike)
    const otherAccounts = visible.filter((account) => !isAPIKey(account) && !isOAuthLike(account))
    const total = view.accounts.length
    const enabled = view.accounts.filter((account) => accountState(account).key === 'enabled').length
    const limited = view.accounts.filter((account) => accountState(account).key === 'limited').length
    const groupKey = String(view.key)
    const contentID = `group-content-${groupKey.replace(/[^a-zA-Z0-9_-]/g, '-')}`
    const filterActive = Boolean(state.search) || state.status !== 'all'
    const collapsed = !filterActive && state.collapsedGroups.has(groupKey)
    const toggleTitle = filterActive ? '筛选期间保持展开' : (collapsed ? '展开分组' : '收起分组')
    const groupStatus = view.group.status === 'active' ? '' : '<span class="status-tag inactive">分组停用</span>'
    const bindButton = view.synthetic ? '' : `
      <button class="summary-button group-manage-button" type="button" data-action="bind-group" data-group-key="${escapeAttr(view.key)}" title="管理当前分组的 API Key 账号">
        <svg class="icon"><use href="#icon-users"/></svg>
        <span>管理账号</span>
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
                ${groupStatus}
              </div>
              <p>${escapeHTML(view.group.description || `#${view.group.id || 'ungrouped'}`)}</p>
            </div>
          </div>
          <div class="group-stats" aria-label="分组账号状态">
            <span><strong class="text-success">${formatInteger(enabled)}</strong> / ${formatInteger(total)} 可用</span>
            <span>${formatInteger(apiKeys.length)} API Key</span>
            ${limited ? `<span class="text-warning">${formatInteger(limited)} 临时受限</span>` : ''}
          </div>
          <div class="group-actions">${detailButtons}${bindButton}</div>
        </header>
        <div id="${escapeAttr(contentID)}" class="api-key-table" ${collapsed ? 'hidden' : ''}>
          <div class="api-key-header" aria-hidden="true"><span>账号状态</span><span>用量与管理员额度</span><span>检测规则与最终倍率</span><span>自动调度 / 操作</span></div>
          ${apiKeys.length ? apiKeys.map(renderAPIKeyAccount).join('') : '<div class="group-empty">暂无 API Key 账号</div>'}
        </div>
      </article>`
  }

  function renderDetailButton(groupKey, kind, label) {
    return `<button class="summary-button" type="button" data-action="open-detail" data-group-key="${escapeAttr(groupKey)}" data-kind="${escapeAttr(kind)}"><svg class="icon"><use href="#icon-users"/></svg><span>${escapeHTML(label)}</span></button>`
  }

  function renderAPIKeyAccount(account) {
    const config = account.config || null
    const scheduling = accountState(account)
    const policy = config?.policy
    const latest = config?.history?.[0]
    const automation = automationState(account)
    const policyText = policy
      ? `<strong>${escapeHTML(policy.model || '平台默认模型')}</strong><span>每 ${formatInterval(policy.interval_seconds)} · 上限 ${formatSeconds(policy.latency_limit_ms)}</span><span>${policy.enabled ? '自动规则启用' : '自动规则停用'} · ${policy.failure_threshold} 次暂停 / ${policy.recovery_threshold} 次恢复</span>${renderProbeSummary(config.probe)}`
      : '<strong>未配置自动调度</strong><span>开启开关后使用默认检测规则</span>'
    const actions = config ? `
      <button class="icon-button ${config.running ? 'spin' : ''}" type="button" data-action="run" title="立即检测" aria-label="立即检测" ${config.running ? 'disabled' : ''}><svg class="icon"><use href="${config.running ? '#icon-refresh' : '#icon-play'}"/></svg></button>
      <button class="icon-button" type="button" data-action="edit" title="编辑检测规则" aria-label="编辑检测规则"><svg class="icon"><use href="#icon-edit"/></svg></button>
      <button class="icon-button danger-tool" type="button" data-action="delete" title="删除检测配置" aria-label="删除检测配置" ${config.running ? 'disabled' : ''}><svg class="icon"><use href="#icon-trash"/></svg></button>` : `
      <button class="icon-button" type="button" data-action="create" title="配置状态检测" aria-label="配置状态检测"><svg class="icon"><use href="#icon-plus"/></svg></button>`
    const latestText = latest ? `${statusLabel(latest.status)} · ${formatMilliseconds(latest.latency_ms)}` : '尚未检测'
    const nextText = policy?.enabled && config.next_check_at ? `下次 ${formatRelative(config.next_check_at)}` : '—'
    return `
      <section class="api-key-row" data-account-id="${account.id}">
        <div class="account-cell">
          <div class="account-name-line"><strong>${escapeHTML(account.name || `账号 ${account.id}`)}</strong><span>#${account.id}</span></div>
          <div class="account-meta"><span class="platform-tag">${escapeHTML(account.platform || 'unknown')}</span><span>API Key</span></div>
          <div class="account-state ${escapeAttr(scheduling.tone)}"><i class="status-dot"></i><span><strong>${escapeHTML(scheduling.label)}</strong><small title="${escapeAttr(scheduling.reason)}">${escapeHTML(scheduling.reason)}</small></span></div>
        </div>
        <div class="usage-cell">${renderTodayUsage(account)}${renderQuota(account)}</div>
        <div class="policy-cell">${policyText}${renderFinalMultiplier(account)}<span class="latest-check">${escapeHTML(latestText)} · ${escapeHTML(nextText)}</span></div>
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

  function renderQuota(account) {
    const quotas = [
      quotaDimension('日额度', account.quota_daily_used, account.quota_daily_limit),
      quotaDimension('周额度', account.quota_weekly_used, account.quota_weekly_limit),
      quotaDimension('总额度', account.quota_used, account.quota_limit),
    ].filter(Boolean)
    return quotas.length
      ? `<div class="quota-summary"><span class="data-label">管理员额度</span><dl class="quota-list">${quotas.join('')}</dl></div>`
      : '<div class="quota-summary"><span class="data-label">管理员额度</span><strong class="quota-unlimited">未配置（不限）</strong></div>'
  }

  function quotaDimension(label, used, limit) {
    if (!Number.isFinite(Number(limit)) || Number(limit) <= 0) return ''
    const normalizedUsed = Number(used) || 0
    const detail = `${label}已用 ${formatCurrency(normalizedUsed)}，管理员配置 ${formatCurrency(Number(limit))}`
    return `<div title="${escapeAttr(detail)}"><dt>${escapeHTML(label)}</dt><dd><span>${escapeHTML(formatCurrency(normalizedUsed))}</span><i aria-hidden="true">/</i><strong>${escapeHTML(formatCurrency(Number(limit)))}</strong></dd></div>`
  }

  function renderFinalMultiplier(account) {
    const projection = account.upstream_final_multiplier || {}
    const finalMultiplier = Number(projection.final_multiplier)
    if (projection.status === 'available' && Number.isFinite(finalMultiplier)) {
      const recharge = Number(projection.recharge_rate_cny_per_usd)
      const group = Number(projection.group_multiplier)
      const detail = Number.isFinite(recharge) && Number.isFinite(group)
        ? `充值 ${formatMultiplier(recharge)} × 分组 ${formatMultiplier(group)}`
        : '来自上游列表的已绑定 Key'
      return `<div class="final-multiplier" title="${escapeAttr(detail)}"><span class="data-label">最终倍率</span><strong>${escapeHTML(formatMultiplier(finalMultiplier))}x</strong><span>${escapeHTML(detail)}</span></div>`
    }
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
    const label = labels[status] || '最终倍率未计算'
    return `<div class="final-multiplier unavailable" title="${escapeAttr(label)}"><span class="data-label">最终倍率</span><strong>未计算</strong><span>${escapeHTML(label)}</span></div>`
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
      const title = failed
        ? `${formatDateTime(result.checked_at)} · ${result.message || statusLabel(result.status)}`
        : `${formatDateTime(result.checked_at)} · 耗时 ${formatMilliseconds(result.latency_ms)}`
      const color = failed
        ? 'failed'
        : result.status === 'skipped'
          ? 'skipped'
          : (result.status === 'degraded' || Number(result.latency_ms) >= CHANNEL_SLOW_MS ? 'slow' : 'success')
      return `<button type="button" class="history-bar ${color}" data-action="result" data-result-id="${escapeAttr(result.id)}" title="${escapeAttr(title)}" aria-label="${escapeAttr(title)}"></button>`
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
    if (action === 'open-detail') {
      openAccountDetail(button.dataset.groupKey, button.dataset.kind)
      return
    }
    const row = button.closest('[data-account-id]')
    if (!row) return
    const account = findAccount(Number(row.dataset.accountId))
    if (!account) return
    switch (action) {
      case 'create': openCreateDialog(account.id); break
      case 'run': await runNow(account); break
      case 'edit': openEditDialog(account); break
      case 'delete': openDeleteDialog(account); break
      case 'result': openResultDialog(account, button.dataset.resultId); break
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
    fillPolicyForm(state.defaultPolicy)
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
    fillPolicyForm(config.policy)
    elements.formError.hidden = true
    renderDirectProbeControl(config.probe || null)
    elements.saveButton.disabled = false
    elements.configDialog.showModal()
  }

  function fillPolicyForm(policy) {
    const normalized = policy || state.defaultPolicy
    elements.intervalInput.value = normalized.interval_seconds
    elements.modelInput.value = normalized.model || ''
    elements.latencyInput.value = (normalized.latency_limit_ms / 1000).toFixed(normalized.latency_limit_ms % 1000 ? 1 : 0)
    elements.failureInput.value = normalized.failure_threshold
    elements.recoveryInput.value = normalized.recovery_threshold
    elements.enabledInput.checked = Boolean(normalized.enabled)
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
      const scheduling = accountState(account)
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
        <span class="binding-memberships ${compatible ? '' : 'incompatible'}"><strong>${escapeHTML(membershipLabel)}</strong><small>${formatInteger((account.group_ids || []).length)} 个分组</small></span>
      </label>`
    }).join('') : `<div class="dialog-empty">${candidates.length ? '没有匹配的 API Key 账号' : '当前分组没有可绑定的 API Key 账号'}</div>`
    const changes = bindingChanges()
    const eligibleCount = candidates.filter((account) => accountCompatibleWithGroup(account, group)).length
    const pendingLabel = changes.length ? `${formatInteger(changes.length)} 项待保存` : '无待保存变更'
    elements.bindingChangeCount.textContent = `可绑定 ${formatInteger(eligibleCount)} · 已选 ${formatInteger(state.bindingDraft.size)} · ${pendingLabel}`
    elements.bindingList.setAttribute('aria-busy', state.bindingSaving ? 'true' : 'false')
    elements.bindingSaveButton.disabled = state.bindingSaving || changes.length === 0
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
      .filter((account) => isAPIKey(account) && (account.group_ids || []).includes(groupID))
      .map((account) => account.id))
  }

  function bindingCandidates() {
    const group = bindingGroup()
    if (!group) return []
    return sortAccounts(state.accounts.filter((account) => isAPIKey(account) && (
      accountCompatibleWithGroup(account, group) || state.bindingOriginal.has(account.id)
    )))
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

  function sortAccounts(accounts) {
    return [...accounts].sort((a, b) => {
      const availability = Number(accountState(a).key !== 'enabled') - Number(accountState(b).key !== 'enabled')
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
