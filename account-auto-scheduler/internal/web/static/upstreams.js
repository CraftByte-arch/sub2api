(() => {
  'use strict'

  const EXPANDED_UPSTREAMS_KEY = 'sub2api-auto-scheduler-expanded-upstreams'
  const ATTENTION_STATUSES = new Set([
    'expired', 'captcha_required', 'two_factor_required', 'management_access_denied', 'network_error', 'unknown_type', 'invalid', 'sync_error'
  ])

  const IDENTITY_LABELS = {
    connected: '已连接',
    disconnected: '未连接',
    expired: '登录已过期',
    captcha_required: '需要人机验证',
    two_factor_required: '需要二次验证',
    management_access_denied: '管理访问被拒绝',
    network_error: '网络异常',
    unknown_type: '类型未识别',
    invalid: '凭证无效',
    sync_error: '同步失败',
  }

  function createUpstreamWorkspace({ api, showToast, credentialsEnabled = false }) {
    const state = {
      loaded: false,
      loading: false,
      error: '',
      credentialsEnabled: Boolean(credentialsEnabled),
      upstreams: [],
      search: '',
      status: 'all',
      expanded: readExpanded(),
      busy: new Set(),
      createTrigger: null,
      connectTrigger: null,
      connectUpstreamID: '',
      connectIdentityID: '',
      connectChallenge: null,
      rechargeTrigger: null,
      rechargeUpstreamID: '',
      bindingTrigger: null,
      bindingUpstreamID: '',
      bindingRows: [],
      bindingDraft: [],
      bindingOriginal: [],
      deleteTrigger: null,
      deleteUpstreamID: '',
      deleteIdentityID: '',
      deleteUpstreamTrigger: null,
      deleteRecordUpstreamID: '',
    }

    const elements = cacheElements()
    bindEvents()
    render()

    return {
      activate() {
        if (state.loading) return Promise.resolve(false)
        return load(state.loaded)
      },
      refresh(silent = true) {
        return load(silent)
      },
      setCredentialsEnabled(value) {
        state.credentialsEnabled = Boolean(value)
        render()
      },
    }

    function cacheElements() {
      const ids = [
        'upstream-count-badge', 'upstream-refresh-button', 'add-upstream-button', 'upstream-credentials-notice',
        'upstream-metric-sites', 'upstream-metric-identities', 'upstream-metric-keys', 'upstream-metric-attention',
        'upstream-search-input', 'upstream-status-filter', 'upstream-loading-state', 'upstream-error-state',
        'upstream-error-message', 'upstream-retry-button', 'upstream-list', 'upstream-empty-state',
        'upstream-empty-add-button', 'upstream-no-match-state', 'upstream-create-dialog', 'upstream-create-form',
        'upstream-url-input', 'upstream-name-input', 'upstream-create-type', 'upstream-create-error',
        'upstream-create-save-button', 'upstream-connect-dialog', 'upstream-connect-form', 'upstream-connect-title',
        'upstream-connect-url', 'upstream-detection-summary', 'upstream-connect-type', 'upstream-identity-label', 'upstream-management-site-input',
        'upstream-password-fields', 'upstream-username-input', 'upstream-password-input', 'upstream-password-toggle',
        'upstream-captcha-fields', 'upstream-captcha-expiry', 'upstream-captcha-refresh-button',
        'upstream-captcha-image', 'upstream-captcha-code-input',
        'upstream-token-fields', 'upstream-token-input', 'upstream-session-fields', 'upstream-session-input',
        'upstream-newapi-user-id-field', 'upstream-newapi-user-id-input',
        'upstream-connect-error', 'upstream-connect-save-button', 'upstream-binding-dialog', 'upstream-binding-name',
        'upstream-binding-count', 'upstream-auto-match-button', 'upstream-step-up-notice', 'upstream-binding-list',
        'upstream-binding-error', 'upstream-binding-save-button', 'upstream-delete-identity-dialog',
        'upstream-delete-identity-name', 'upstream-delete-identity-button', 'upstream-recharge-dialog',
        'upstream-delete-dialog', 'upstream-delete-name', 'upstream-delete-button',
        'upstream-recharge-form', 'upstream-recharge-name', 'upstream-recharge-prefix', 'upstream-recharge-value',
        'upstream-recharge-suffix', 'upstream-recharge-preview', 'upstream-recharge-formula', 'upstream-recharge-error',
        'upstream-recharge-clear-button', 'upstream-recharge-save-button'
      ]
      const result = {}
      for (const id of ids) result[toCamel(id)] = document.getElementById(id)
      return result
    }

    function bindEvents() {
      elements.upstreamRefreshButton.addEventListener('click', syncAll)
      elements.addUpstreamButton.addEventListener('click', (event) => openCreate(event.currentTarget))
      elements.upstreamEmptyAddButton.addEventListener('click', (event) => openCreate(event.currentTarget))
      elements.upstreamRetryButton.addEventListener('click', () => load())
      elements.upstreamSearchInput.addEventListener('input', (event) => {
        state.search = event.target.value.trim().toLocaleLowerCase()
        render()
      })
      elements.upstreamStatusFilter.addEventListener('change', (event) => {
        state.status = event.target.value
        render()
      })
      elements.upstreamList.addEventListener('click', handleListClick)
      elements.upstreamCreateForm.addEventListener('submit', saveCreate)
      elements.upstreamConnectForm.addEventListener('submit', saveConnection)
      elements.upstreamConnectForm.addEventListener('change', (event) => {
        if (event.target.name === 'upstream-auth-mode' || event.target === elements.upstreamConnectType) {
          clearConnectChallenge()
          renderAuthMode()
        }
      })
      elements.upstreamManagementSiteInput.addEventListener('input', clearConnectChallenge)
      elements.upstreamPasswordToggle.addEventListener('click', togglePassword)
      elements.upstreamCaptchaRefreshButton.addEventListener('click', refreshConnectChallenge)
      elements.upstreamRechargeForm.addEventListener('submit', saveRechargeRate)
      elements.upstreamRechargeForm.addEventListener('change', (event) => {
        if (event.target.name === 'upstream-recharge-mode') renderRechargePreview()
      })
      elements.upstreamRechargeValue.addEventListener('input', renderRechargePreview)
      elements.upstreamRechargeClearButton.addEventListener('click', clearRechargeRate)
      elements.upstreamBindingList.addEventListener('change', changeBinding)
      elements.upstreamBindingSaveButton.addEventListener('click', saveBindings)
      elements.upstreamAutoMatchButton.addEventListener('click', autoMatch)
      elements.upstreamDeleteIdentityButton.addEventListener('click', deleteIdentity)
      elements.upstreamDeleteButton.addEventListener('click', deleteUpstream)
      elements.upstreamCreateDialog.addEventListener('close', () => restoreFocus('create'))
      elements.upstreamConnectDialog.addEventListener('close', () => {
        clearConnectChallenge()
        restoreFocus('connect')
      })
      elements.upstreamRechargeDialog.addEventListener('close', () => restoreFocus('recharge'))
      elements.upstreamBindingDialog.addEventListener('close', () => restoreFocus('binding'))
      elements.upstreamDeleteIdentityDialog.addEventListener('close', () => restoreFocus('delete'))
      elements.upstreamDeleteDialog.addEventListener('close', () => restoreFocus('deleteUpstream'))
      for (const dialog of [elements.upstreamCreateDialog, elements.upstreamConnectDialog, elements.upstreamRechargeDialog, elements.upstreamBindingDialog, elements.upstreamDeleteIdentityDialog, elements.upstreamDeleteDialog]) {
        dialog.addEventListener('cancel', (event) => {
          if (dialog.dataset.busy === 'true') event.preventDefault()
        })
      }
    }

    async function load(silent = false) {
      if (state.loading) return false
      state.loading = true
      state.error = ''
      render()
      try {
        const response = await api('/api/upstreams')
        state.credentialsEnabled = Boolean(response.credentials_enabled)
        state.upstreams = Array.isArray(response.upstreams) ? response.upstreams : []
        state.loaded = true
        render()
        return true
      } catch (error) {
        state.error = error.message || '上游列表加载失败'
        if (silent) showToast(state.error, true)
        render()
        return false
      } finally {
        state.loading = false
        render()
      }
    }

    function render() {
      const filtered = state.upstreams.filter(matchesFilters)
      const identityCount = state.upstreams.reduce((total, item) => total + (item.identities || []).length, 0)
      const keyCount = state.upstreams.reduce((total, item) => total + keyCountFor(item), 0)
      const attentionCount = state.upstreams.filter((item) => siteState(item).key === 'attention').length

      elements.upstreamMetricSites.textContent = formatInteger(state.upstreams.length)
      elements.upstreamMetricIdentities.textContent = formatInteger(identityCount)
      elements.upstreamMetricKeys.textContent = formatInteger(keyCount)
      elements.upstreamMetricAttention.textContent = formatInteger(attentionCount)
      elements.upstreamCredentialsNotice.hidden = state.credentialsEnabled
      elements.upstreamCountBadge.hidden = !state.loaded
      elements.upstreamCountBadge.textContent = formatInteger(state.upstreams.length)
      elements.upstreamRefreshButton.disabled = state.loading || !state.credentialsEnabled || identityCount === 0
      elements.upstreamRefreshButton.classList.toggle('spin', state.busy.has('sync-all'))
      elements.upstreamRefreshButton.title = state.credentialsEnabled ? '同步全部上游' : '需先配置上游凭证加密密钥'

      const initialLoading = state.loading && !state.loaded
      elements.upstreamLoadingState.hidden = !initialLoading
      elements.upstreamErrorState.hidden = !state.error || state.loaded
      elements.upstreamErrorMessage.textContent = state.error || '上游列表加载失败'
      elements.upstreamList.hidden = initialLoading || Boolean(state.error && !state.loaded) || filtered.length === 0
      elements.upstreamList.innerHTML = filtered.map(renderUpstream).join('')
      elements.upstreamEmptyState.hidden = initialLoading || Boolean(state.error) || !state.loaded || state.upstreams.length > 0
      elements.upstreamNoMatchState.hidden = initialLoading || Boolean(state.error) || state.upstreams.length === 0 || filtered.length > 0
    }

    function matchesFilters(upstream) {
      const currentState = siteState(upstream).key
      if (state.status !== 'all' && currentState !== state.status) return false
      if (!state.search) return true
      const localText = (upstream.local_accounts || []).map((account) => `${account.name || ''} ${account.platform || ''} ${account.id}`).join(' ')
      const identityText = (upstream.identities || []).map((identity) => {
        const keys = (identity.keys || []).map((key) => `${key.name || ''} ${key.masked_key || ''} ${key.group || ''}`).join(' ')
        const groups = (identity.groups || []).map((group) => `${group.id || ''} ${group.name || ''} ${group.platform || ''} ${group.multiplier_source || ''}`).join(' ')
        return `${identity.label || ''} ${identity.principal || ''} ${keys} ${groups}`
      }).join(' ')
      return `${upstream.name || ''} ${upstream.base_url || ''} ${upstream.type || ''} ${localText} ${identityText}`.toLocaleLowerCase().includes(state.search)
    }

    function renderUpstream(upstream) {
      const identities = upstream.identities || []
      const localAccounts = [...(upstream.local_accounts || [])].sort(sortLocalAccounts)
      const keys = keyCountFor(upstream)
      const groups = groupCountFor(upstream)
      const currentState = siteState(upstream)
      const expanded = Boolean(state.search) || state.expanded.has(upstream.id)
      const contentID = `upstream-content-${safeID(upstream.id)}`
      const disabledReason = state.credentialsEnabled ? '' : '需先配置 AUTO_SCHEDULER_CREDENTIAL_KEY'
      const rowBusy = state.busy.has(`upstream:${upstream.id}`)
      const connectLabel = identities.length ? '添加身份' : '登录'
      const connectTitle = identities.length ? '添加登录身份' : '登录上游'
      const canDelete = upstream.persisted === true && localAccounts.length === 0
      return `<article class="upstream-row ${expanded ? 'expanded' : ''}" data-upstream-id="${escapeAttr(upstream.id)}">
        <header class="upstream-row-header">
          <div class="upstream-heading">
            <button class="icon-button upstream-toggle" type="button" data-upstream-action="toggle" title="${expanded ? '收起上游' : '展开上游'}" aria-label="${expanded ? '收起上游' : '展开上游'}" aria-expanded="${expanded}" aria-controls="${escapeAttr(contentID)}">
              <svg class="icon"><use href="#icon-chevron-right"/></svg>
            </button>
            <div class="upstream-identity">
              <div class="upstream-title-line"><h2>${escapeHTML(upstream.name || upstream.base_url)}</h2>${renderTypeBadge(upstream)}${renderStatusPill(currentState.label, currentState.tone)}</div>
              <div class="upstream-routes">
                <a class="upstream-url" href="${escapeAttr(upstream.base_url)}" target="_blank" rel="noopener noreferrer"><span>API 地址（模型调用）</span>${escapeHTML(upstream.base_url)}</a>
                ${upstream.site_url ? `<a class="upstream-url management-site-url" href="${escapeAttr(upstream.site_url)}" target="_blank" rel="noopener noreferrer"><span>管理站点</span>${escapeHTML(upstream.site_url)}</a>` : ''}
              </div>
            </div>
          </div>
          <div class="upstream-summary">
            ${renderUpstreamBalanceSummary(upstream)}
            <div class="upstream-secondary-summary">
              ${renderResourceSummary(localAccounts.length, identities.length, keys, groups)}
              ${renderRechargeSummary(upstream)}
            </div>
          </div>
          <div class="upstream-row-actions">
            <button class="icon-button ${state.busy.has(`detect:${upstream.id}`) ? 'spin' : ''}" type="button" data-upstream-action="detect" title="匿名探测网站类型" aria-label="匿名探测网站类型" ${state.busy.has(`detect:${upstream.id}`) ? 'disabled' : ''}><svg class="icon"><use href="#icon-activity"/></svg></button>
            <button class="button secondary" type="button" data-upstream-action="recharge-rate"><svg class="icon"><use href="#icon-edit"/></svg><span>充值倍率</span></button>
            <button class="button secondary" type="button" data-upstream-action="bind" ${keys === 0 ? 'disabled' : ''}><svg class="icon"><use href="#icon-link"/></svg><span>绑定账号</span></button>
            <button class="button secondary" type="button" data-upstream-action="sync" title="${escapeAttr(disabledReason || '同步当前上游')}" ${!state.credentialsEnabled || identities.length === 0 || rowBusy ? 'disabled' : ''}><svg class="icon ${rowBusy ? 'spin' : ''}"><use href="#icon-refresh"/></svg><span>同步</span></button>
            <button class="button primary" type="button" data-upstream-action="connect" title="${escapeAttr(disabledReason || connectTitle)}" ${!state.credentialsEnabled ? 'disabled' : ''}><svg class="icon"><use href="#icon-log-in"/></svg><span>${connectLabel}</span></button>
          </div>
        </header>
        <div id="${escapeAttr(contentID)}" class="upstream-detail" ${expanded ? '' : 'hidden'}>
          ${renderLocalAccounts(upstream, localAccounts, canDelete)}
          <div class="identity-section-header"><div><h3>登录身份 <span class="section-count">${formatInteger(identities.length)}</span></h3><span>${identities.length ? `共 ${formatInteger(keys)} 个上游 Key 快照` : '尚未添加登录身份'}</span></div></div>
          ${identities.length ? identities.map((identity) => renderIdentity(upstream, identity)).join('') : '<div class="identity-empty">点击“登录”添加账号密码、Token 或 Cookie/session 身份</div>'}
        </div>
      </article>`
    }

    function renderLocalAccounts(upstream, accounts, canDelete) {
      const content = accounts.length ? accounts.map((account) => {
        const active = account.status === 'active' && account.schedulable
        const label = account.status !== 'active' ? '账号停用' : (account.schedulable ? '调度启用' : '调度停止')
        return `<span class="local-account-chip"><i class="status-dot ${active ? 'success' : 'neutral'}"></i><strong>${escapeHTML(account.name || `账号 ${account.id}`)}</strong><span>#${account.id} · ${escapeHTML(label)}</span></span>`
      }).join('') : `<span class="local-account-empty"><span class="muted">没有使用该地址的本地 API Key 账号</span>${canDelete ? `<button class="button danger compact" type="button" data-upstream-action="delete-upstream" title="删除未使用的上游" aria-label="删除上游 ${escapeAttr(upstream.name || upstream.base_url)}"><svg class="icon"><use href="#icon-trash"/></svg><span>删除上游</span></button>` : ''}</span>`
      return `<div class="local-account-band"><span class="band-label">关联本地账号 <b class="section-count">${formatInteger(accounts.length)}</b></span><div class="local-account-list">${content}</div></div>`
    }

    function renderIdentity(upstream, identity) {
      const status = identityStatus(identity.status)
      const keys = identity.keys || []
      const groups = identity.groups || []
      const busy = state.busy.has(`identity:${upstream.id}:${identity.id}`)
      const identityLabel = identity.label || '登录身份'
      const principal = String(identity.principal || '').trim()
      const identityMeta = [principal && principal !== identityLabel ? principal : '', authModeLabel(identity.auth_mode)].filter(Boolean).join(' · ')
      const statusMessage = identity.status_message ? `<p class="identity-error">${escapeHTML(identity.status_message)}</p>` : ''
      return `<section class="identity-block" data-identity-id="${escapeAttr(identity.id)}">
        <div class="identity-summary">
          <div class="identity-name"><div class="identity-title-line"><strong>${escapeHTML(identityLabel)}</strong>${renderStatusPill(status.label, status.tone)}</div><span>${escapeHTML(identityMeta || '已保存登录身份')}</span></div>
          ${renderIdentityBalance(identity)}
          <div class="identity-timestamps"><strong>上次成功 ${escapeHTML(formatDateTime(identity.last_success_at))}</strong><span>上次尝试 ${escapeHTML(formatDateTime(identity.last_attempt_at))}</span></div>
          <div class="identity-actions">
            <button class="button secondary" type="button" data-upstream-action="sync-identity" title="同步该身份" ${!state.credentialsEnabled || busy || !identity.has_credential ? 'disabled' : ''}><svg class="icon ${busy ? 'spin' : ''}"><use href="#icon-refresh"/></svg><span>同步</span></button>
            <button class="icon-button" type="button" data-upstream-action="reconnect" title="重新登录" aria-label="重新登录"><svg class="icon"><use href="#icon-log-in"/></svg></button>
            <button class="icon-button danger-tool" type="button" data-upstream-action="delete-identity" title="删除身份" aria-label="删除身份"><svg class="icon"><use href="#icon-trash"/></svg></button>
          </div>
        </div>
        ${statusMessage}
        ${renderIdentityGroups(upstream, identity, groups)}
        <section class="identity-key-section" aria-label="${escapeAttr(`${identity.label || '登录身份'} 的上游 Key`)}">
          <div class="identity-key-heading"><h4>上游 Key <span class="section-count">${formatInteger(keys.length)}</span></h4><span>${keys.length ? '该登录身份的最近 Key 快照' : '尚无 Key 快照'}</span></div>
          ${keys.length ? `<div class="remote-key-header" aria-hidden="true"><span>Key</span><span>状态与分组</span><span>用量 / 额度</span><span>最终倍率</span><span>本地绑定 / 同步</span></div>${keys.map((key) => renderRemoteKey(upstream, identity, key)).join('')}` : '<div class="identity-empty">请同步该登录身份以获取 Key 快照</div>'}
        </section>
      </section>`
    }

    function renderIdentityGroups(upstream, identity, groups) {
      const identityLabel = identity.label || '登录身份'
      return `<section class="identity-group-section" aria-label="${escapeAttr(`${identityLabel} 的可用分组`)}">
        <div class="identity-group-heading"><h4>可用分组 <span class="section-count">${formatInteger(groups.length)}</span></h4><span>${groups.length ? '当前登录身份同步到的全部可用分组' : '同步该登录身份后获取可用分组'}</span></div>
        ${groups.length ? `<div class="remote-group-header" aria-hidden="true"><span>分组</span><span>类型</span><span>分组倍率</span><span>最终倍率</span><span>快照状态</span></div>${groups.map((group) => renderRemoteGroup(upstream, group)).join('')}` : '<div class="identity-empty">尚无可用分组快照</div>'}
      </section>`
    }

    function renderRemoteGroup(upstream, group) {
      const rate = formatRemoteGroupRate(group, upstream)
      const snapshot = group.stale ? { label: '旧快照', tone: 'warning' } : { label: '已同步', tone: 'success' }
      return `<div class="remote-group-row">
        <div class="remote-group-cell"><span class="mobile-field-label">分组</span><strong>${escapeHTML(group.name || group.id || '未命名分组')}</strong><span>${escapeHTML(group.id || '上游未返回分组标识')}</span></div>
        <div class="remote-group-cell"><span class="mobile-field-label">类型</span>${renderRemoteGroupPlatformBadge(group.platform)}</div>
        <div class="remote-group-cell"><span class="mobile-field-label">分组倍率</span><strong class="rate-value">${escapeHTML(rate.groupPrimary)}</strong><span>${escapeHTML(rate.groupSecondary)}</span></div>
        <div class="remote-group-cell"><span class="mobile-field-label">最终倍率</span><strong class="rate-value">${escapeHTML(rate.finalPrimary)}</strong><span>${escapeHTML(rate.finalSecondary)}</span></div>
        <div class="remote-group-cell"><span class="mobile-field-label">快照状态</span>${renderStatusPill(snapshot.label, snapshot.tone)}<span>${escapeHTML(formatDateTime(group.synced_at))}</span></div>
      </div>`
    }

    function renderRemoteKey(upstream, identity, key) {
      const local = (upstream.local_accounts || []).find((account) => account.id === key.local_account_id)
      const binding = key.local_account_id
        ? `${local ? local.name || `账号 ${local.id}` : `账号 #${key.local_account_id}`} ${key.stale ? '· 失效绑定' : '· 已绑定'}`
        : '未绑定'
      const rate = formatRemoteRate(key, upstream)
      const quota = formatRemoteQuota(key)
      const keyStatus = remoteKeyStatus(key.status, key.stale)
      return `<div class="remote-key-row">
        <div class="remote-key-cell"><span class="mobile-field-label">Key</span><strong>${escapeHTML(key.name || `Key ${key.id}`)}</strong><span>${escapeHTML(key.masked_key || '上游未返回掩码')}</span></div>
        <div class="remote-key-cell"><span class="mobile-field-label">状态与分组</span>${renderStatusPill(keyStatus.label, keyStatus.tone)}<span>${escapeHTML(key.group || '默认分组')}</span></div>
        <div class="remote-key-cell"><span class="mobile-field-label">用量 / 额度</span><strong>${escapeHTML(quota.primary)}</strong><span>${escapeHTML(quota.secondary)}</span></div>
        <div class="remote-key-cell"><span class="mobile-field-label">最终倍率</span><strong class="rate-value">${escapeHTML(rate.primary)}</strong><span>${escapeHTML(rate.secondary)}</span><span>${escapeHTML(rate.detail)}</span></div>
        <div class="remote-key-cell"><span class="mobile-field-label">本地绑定 / 同步</span><strong>${escapeHTML(binding)}</strong><span>${escapeHTML(formatDateTime(key.synced_at))}</span></div>
      </div>`
    }

    async function handleListClick(event) {
      const button = event.target.closest('[data-upstream-action]')
      if (!button) return
      const row = button.closest('[data-upstream-id]')
      const upstream = getUpstream(row?.dataset.upstreamId)
      if (!upstream) return
      const identityID = button.closest('[data-identity-id]')?.dataset.identityId || ''
      switch (button.dataset.upstreamAction) {
        case 'toggle': toggleUpstream(upstream.id); break
        case 'detect': await detectUpstream(upstream.id); break
        case 'recharge-rate': openRechargeRate(upstream, button); break
        case 'connect': openConnect(upstream, null, button); break
        case 'reconnect': openConnect(upstream, findIdentity(upstream, identityID), button); break
        case 'sync': await syncUpstream(upstream.id); break
        case 'sync-identity': await syncIdentity(upstream.id, identityID); break
        case 'bind': openBindings(upstream, button); break
        case 'delete-identity': openDeleteIdentity(upstream, findIdentity(upstream, identityID), button); break
        case 'delete-upstream': openDeleteUpstream(upstream, button); break
      }
    }

    function toggleUpstream(upstreamID) {
      if (state.search) return
      if (state.expanded.has(upstreamID)) state.expanded.delete(upstreamID)
      else state.expanded.add(upstreamID)
      persistExpanded()
      render()
    }

    async function detectUpstream(upstreamID, { forDialog = false } = {}) {
      const busyKey = `detect:${upstreamID}`
      if (state.busy.has(busyKey)) return null
      state.busy.add(busyKey)
      if (forDialog) renderDetectionSummary(true)
      render()
      try {
        const response = await api(`/api/upstreams/${encodeURIComponent(upstreamID)}/detect`, { method: 'POST' })
        replaceUpstream(response.upstream)
        if (forDialog) {
          const detected = response.upstream?.type
          if (detected && detected !== 'unknown' && !elements.upstreamConnectType.value) elements.upstreamConnectType.value = detected
        } else {
          showToast(response.upstream?.type === 'unknown' ? '未识别网站类型，请手动选择' : `已识别为 ${siteTypeLabel(response.upstream?.type)}`)
        }
        return response.upstream
      } catch (error) {
        if (forDialog) showConnectError(error)
        else showToast(error.message || '网站类型探测失败', true)
        return null
      } finally {
        state.busy.delete(busyKey)
        render()
        if (forDialog && elements.upstreamConnectDialog.open) renderDetectionSummary(false)
      }
    }

    async function syncAll() {
      if (!state.credentialsEnabled || state.busy.has('sync-all')) return
      state.busy.add('sync-all')
      render()
      try {
        const response = await api('/api/upstreams/sync', { method: 'POST' })
        await load(true)
        showOutcomeToast(response.outcomes || [], '全部上游已同步')
      } catch (error) {
        showToast(error.message || '同步全部上游失败', true)
      } finally {
        state.busy.delete('sync-all')
        render()
      }
    }

    async function syncUpstream(upstreamID) {
      const busyKey = `upstream:${upstreamID}`
      if (state.busy.has(busyKey)) return
      state.busy.add(busyKey)
      render()
      try {
        const response = await api(`/api/upstreams/${encodeURIComponent(upstreamID)}/sync`, { method: 'POST' })
        await load(true)
        showOutcomeToast(response.outcomes || [], '上游已同步')
      } catch (error) {
        showToast(error.message || '同步上游失败', true)
      } finally {
        state.busy.delete(busyKey)
        render()
      }
    }

    async function syncIdentity(upstreamID, identityID) {
      const busyKey = `identity:${upstreamID}:${identityID}`
      if (state.busy.has(busyKey)) return
      state.busy.add(busyKey)
      render()
      try {
        await api(`/api/upstreams/${encodeURIComponent(upstreamID)}/identities/${encodeURIComponent(identityID)}/sync`, { method: 'POST' })
        await load(true)
        showToast('登录身份已同步')
      } catch (error) {
        await load(true)
        showToast(error.message || '同步登录身份失败', true)
      } finally {
        state.busy.delete(busyKey)
        render()
      }
    }

    function showOutcomeToast(outcomes, successMessage) {
      const failures = outcomes.filter((item) => !item.success)
      if (!failures.length) {
        showToast(successMessage)
        return
      }
      showToast(`${formatInteger(outcomes.length - failures.length)} 个身份成功，${formatInteger(failures.length)} 个失败`, true)
    }

    function openCreate(trigger) {
      state.createTrigger = trigger
      elements.upstreamCreateForm.reset()
      elements.upstreamCreateType.value = 'unknown'
      elements.upstreamCreateError.hidden = true
      elements.upstreamCreateDialog.showModal()
      window.requestAnimationFrame(() => elements.upstreamUrlInput.focus())
    }

    async function saveCreate(event) {
      event.preventDefault()
      if (!elements.upstreamCreateForm.reportValidity()) return
      setDialogBusy(elements.upstreamCreateDialog, elements.upstreamCreateSaveButton, true, '添加中')
      elements.upstreamCreateError.hidden = true
      try {
        const selectedType = elements.upstreamCreateType.value
        const response = await api('/api/upstreams', {
          method: 'POST',
          body: {
            base_url: elements.upstreamUrlInput.value.trim(),
            name: elements.upstreamNameInput.value.trim(),
            type_override: selectedType,
          },
        })
        state.expanded.add(response.upstream.id)
        persistExpanded()
        replaceUpstream(response.upstream)
        let detected = response.upstream
        if (selectedType === 'unknown') detected = await detectUpstream(response.upstream.id) || response.upstream
        await load(true)
        elements.upstreamCreateDialog.close()
        showToast(detected?.type === 'unknown' ? '上游已添加，网站类型需要手动选择' : `上游已添加：${siteTypeLabel(detected?.type)}`)
      } catch (error) {
        elements.upstreamCreateError.textContent = error.message || '添加上游失败'
        elements.upstreamCreateError.hidden = false
      } finally {
        setDialogBusy(elements.upstreamCreateDialog, elements.upstreamCreateSaveButton, false, '添加')
      }
    }

    function openConnect(upstream, identity, trigger) {
      state.connectTrigger = trigger
      state.connectUpstreamID = upstream.id
      state.connectIdentityID = identity?.id || ''
      elements.upstreamConnectForm.reset()
      clearConnectChallenge()
      elements.upstreamConnectTitle.textContent = identity ? '重新连接登录身份' : '连接上游'
      elements.upstreamConnectUrl.textContent = `API 地址（模型调用）：${upstream.base_url}`
      elements.upstreamConnectType.value = upstream.type && upstream.type !== 'unknown' ? upstream.type : ''
      elements.upstreamIdentityLabel.value = identity?.label || ''
      elements.upstreamManagementSiteInput.value = upstream.site_url || upstream.base_url || ''
      const mode = identity?.auth_mode || 'password'
      const modeInput = elements.upstreamConnectForm.querySelector(`input[name="upstream-auth-mode"][value="${mode}"]`)
      if (modeInput) modeInput.checked = true
      elements.upstreamConnectError.hidden = true
      renderAuthMode()
      renderDetectionSummary(false)
      elements.upstreamConnectDialog.showModal()
      window.requestAnimationFrame(() => (elements.upstreamConnectType.value ? elements.upstreamIdentityLabel : elements.upstreamConnectType).focus())
      if (!upstream.detection?.detected_at || upstream.type === 'unknown') void detectUpstream(upstream.id, { forDialog: true })
    }

    function renderDetectionSummary(detecting) {
      const upstream = getUpstream(state.connectUpstreamID)
      if (!upstream) return
      if (detecting) {
        elements.upstreamDetectionSummary.innerHTML = '<div><strong>正在匿名探测网站类型</strong><span>不会发送账号、密码、Token 或 Cookie</span></div><svg class="icon spin"><use href="#icon-refresh"/></svg>'
        return
      }
      const detectedType = upstream.detection?.type || 'unknown'
      const evidence = upstream.detection?.evidence || '尚未探测'
      const recognized = detectedType !== 'unknown'
      elements.upstreamDetectionSummary.innerHTML = `<div><strong>${recognized ? `自动识别：${escapeHTML(siteTypeLabel(detectedType))}` : '自动探测未识别'}</strong><span>${escapeHTML(recognized ? `依据 ${evidence}，可在下方手动覆盖` : '请在下方选择 Sub2API 或 NewAPI')}</span></div>${renderTypeBadge(upstream)}`
    }

    function renderAuthMode() {
      const mode = selectedAuthMode()
      const needsNewAPIUserID = elements.upstreamConnectType.value === 'newapi' && mode !== 'password'
      elements.upstreamPasswordFields.hidden = mode !== 'password'
      elements.upstreamTokenFields.hidden = mode !== 'token'
      elements.upstreamSessionFields.hidden = mode !== 'session'
      elements.upstreamNewapiUserIdField.hidden = !needsNewAPIUserID
      elements.upstreamUsernameInput.required = mode === 'password'
      elements.upstreamPasswordInput.required = mode === 'password'
      elements.upstreamTokenInput.required = mode === 'token'
      elements.upstreamSessionInput.required = mode === 'session'
      renderConnectChallenge()
    }

    function clearConnectChallenge() {
      state.connectChallenge = null
      if (!elements.upstreamCaptchaFields) return
      elements.upstreamCaptchaCodeInput.value = ''
      elements.upstreamCaptchaCodeInput.required = false
      elements.upstreamCaptchaImage.removeAttribute('src')
      elements.upstreamCaptchaFields.hidden = true
      if (elements.upstreamConnectDialog.dataset.busy !== 'true') {
        const labelNode = elements.upstreamConnectSaveButton.querySelector('span')
        if (labelNode) labelNode.textContent = '验证并连接'
      }
    }

    function renderConnectChallenge() {
      const challenge = state.connectChallenge
      const visible = selectedAuthMode() === 'password' && Boolean(challenge?.id && challenge?.image_data)
      elements.upstreamCaptchaFields.hidden = !visible
      elements.upstreamCaptchaCodeInput.required = visible
      elements.upstreamCaptchaRefreshButton.disabled = elements.upstreamConnectDialog.dataset.busy === 'true'
      if (!visible) {
        elements.upstreamCaptchaImage.removeAttribute('src')
        return
      }
      if (elements.upstreamCaptchaImage.getAttribute('src') !== challenge.image_data) {
        elements.upstreamCaptchaImage.src = challenge.image_data
      }
      elements.upstreamCaptchaExpiry.textContent = challenge.expires_at
        ? `验证码有效至 ${formatDateTime(challenge.expires_at)}`
        : '验证码将在 5 分钟内失效'
    }

    function connectSubmitLabel() {
      return state.connectChallenge ? '提交验证码并连接' : '验证并连接'
    }

    function selectedAuthMode() {
      return elements.upstreamConnectForm.querySelector('input[name="upstream-auth-mode"]:checked')?.value || 'password'
    }

    function togglePassword() {
      const visible = elements.upstreamPasswordInput.type === 'text'
      elements.upstreamPasswordInput.type = visible ? 'password' : 'text'
      elements.upstreamPasswordToggle.title = visible ? '显示密码' : '隐藏密码'
      elements.upstreamPasswordToggle.setAttribute('aria-label', elements.upstreamPasswordToggle.title)
      elements.upstreamPasswordToggle.querySelector('use').setAttribute('href', visible ? '#icon-eye' : '#icon-eye-off')
      elements.upstreamPasswordInput.focus()
    }

    async function fetchConnectChallenge(upstream) {
      const response = await api(`/api/upstreams/${encodeURIComponent(upstream.id)}/login-challenges`, {
        method: 'POST',
        body: {
          identity_id: state.connectIdentityID,
          site_url: elements.upstreamManagementSiteInput.value.trim(),
          auth_mode: 'password',
        },
      })
      if (!response?.required) {
        clearConnectChallenge()
        return false
      }
      if (!response.id || !response.image_data) {
        const error = new Error('上游验证码响应无效，请稍后重试')
        error.code = 'LOGIN_CHALLENGE_FAILED'
        throw error
      }
      state.connectChallenge = response
      elements.upstreamCaptchaCodeInput.value = ''
      renderConnectChallenge()
      return true
    }

    async function refreshConnectChallenge() {
      const upstream = getUpstream(state.connectUpstreamID)
      if (!upstream || selectedAuthMode() !== 'password' || elements.upstreamConnectDialog.dataset.busy === 'true') return
      setDialogBusy(elements.upstreamConnectDialog, elements.upstreamConnectSaveButton, true, '获取验证码')
      elements.upstreamConnectError.hidden = true
      try {
        const required = await fetchConnectChallenge(upstream)
        if (!required) {
          elements.upstreamConnectError.textContent = '该上游当前未要求本地图片验证码，可直接登录'
          elements.upstreamConnectError.hidden = false
          return
        }
        elements.upstreamCaptchaCodeInput.focus()
      } catch (error) {
        clearConnectChallenge()
        showConnectError(error)
      } finally {
        setDialogBusy(elements.upstreamConnectDialog, elements.upstreamConnectSaveButton, false, connectSubmitLabel())
      }
    }

    async function saveConnection(event) {
      event.preventDefault()
      if (!elements.upstreamConnectForm.reportValidity()) return
      const upstream = getUpstream(state.connectUpstreamID)
      if (!upstream) return
      const siteType = elements.upstreamConnectType.value
      if (!siteType) {
        showConnectError({ message: '自动探测未识别网站类型，请手动选择 Sub2API 或 NewAPI' })
        elements.upstreamConnectType.focus()
        return
      }
      const mode = selectedAuthMode()
      const submittedChallenge = mode === 'password' ? state.connectChallenge : null
      setDialogBusy(elements.upstreamConnectDialog, elements.upstreamConnectSaveButton, true, submittedChallenge ? '登录中' : '验证中')
      elements.upstreamConnectError.hidden = true
      try {
        if (upstream.type !== siteType || upstream.type_override !== siteType) {
          const typeResponse = await api(`/api/upstreams/${encodeURIComponent(upstream.id)}/type`, { method: 'PUT', body: { type: siteType } })
          replaceUpstream(typeResponse.upstream)
        }
        if (mode === 'password' && !submittedChallenge) {
          const required = await fetchConnectChallenge(upstream)
          if (required) {
            elements.upstreamCaptchaCodeInput.focus()
            return
          }
        }
        const body = {
          label: elements.upstreamIdentityLabel.value.trim(),
          site_url: elements.upstreamManagementSiteInput.value.trim(),
          auth_mode: mode,
          username: mode === 'password' ? elements.upstreamUsernameInput.value.trim() : '',
          password: mode === 'password' ? elements.upstreamPasswordInput.value : '',
          token: mode === 'token' ? elements.upstreamTokenInput.value.trim() : '',
          session: mode === 'session' ? elements.upstreamSessionInput.value.trim() : '',
          user_id: siteType === 'newapi' && mode !== 'password' ? elements.upstreamNewapiUserIdInput.value.trim() : '',
          login_challenge_id: submittedChallenge?.id || '',
          captcha_code: submittedChallenge ? elements.upstreamCaptchaCodeInput.value.trim() : '',
        }
        const identityPath = state.connectIdentityID ? `/${encodeURIComponent(state.connectIdentityID)}` : ''
        await api(`/api/upstreams/${encodeURIComponent(upstream.id)}/identities${identityPath}`, {
          method: state.connectIdentityID ? 'PUT' : 'POST', body,
        })
        state.expanded.add(upstream.id)
        persistExpanded()
        await load(true)
        clearConnectChallenge()
        elements.upstreamConnectDialog.close()
        showToast(state.connectIdentityID ? '登录身份已重新连接' : '上游登录成功并已同步 Key')
      } catch (error) {
        if (submittedChallenge) {
          clearConnectChallenge()
          try {
            await fetchConnectChallenge(upstream)
          } catch {
            clearConnectChallenge()
          }
        }
        await load(true)
        showConnectError(error)
      } finally {
        setDialogBusy(elements.upstreamConnectDialog, elements.upstreamConnectSaveButton, false, connectSubmitLabel())
      }
    }

    function showConnectError(error) {
      const action = {
        CAPTCHA_REQUIRED: '上游要求浏览器人机验证。当前自动弹窗只支持本地图片验证码，请在上游网站完成验证后改用 Cookie / Session。',
        LOGIN_CHALLENGE_EXPIRED: '验证码已过期或已使用，已为你刷新，请重新输入。',
        INVALID_LOGIN_CHALLENGE: '验证码挑战无效，请刷新验证码后重试。',
        INVALID_CAPTCHA_CODE: '请输入有效的验证码。',
        UPSTREAM_CAPTCHA_UNAVAILABLE: '上游验证码暂不可用，请稍后换一张。',
        UPSTREAM_CAPTCHA_IMAGE_INVALID: '上游返回的验证码图片无效，无法安全显示。',
        TWO_FACTOR_REQUIRED: '上游要求二次验证。请在上游完成验证后粘贴 Token 或 Cookie/session。',
        UPSTREAM_INVALID_CREDENTIALS: '上游明确拒绝了账号或密码。请手动填写该上游自己的凭据；旧版 NewAPI 优先使用站内用户名。',
        UPSTREAM_SESSION_EXPIRED: '登录材料已过期，请重新复制 Token 或 Cookie/session。',
        UPSTREAM_MANAGEMENT_ACCESS_DENIED: '上游管理站点拒绝了服务器请求。请检查管理站点地址，或在上游侧放行该服务器；这不表示账号密码或登录材料已过期。',
        NEWAPI_USER_ID_REQUIRED: '该 NewAPI 版本还需要数字用户 ID。请在上游个人中心查看后填写。',
        INVALID_NEWAPI_USER_ID: 'NewAPI 用户 ID 必须是正整数。',
        CREDENTIALS_DISABLED: '调度侧车尚未配置上游凭证加密密钥。',
      }[error.code]
      elements.upstreamConnectError.textContent = action || error.message || '连接上游失败'
      elements.upstreamConnectError.hidden = false
    }

    function openRechargeRate(upstream, trigger) {
      state.rechargeTrigger = trigger
      state.rechargeUpstreamID = upstream.id
      elements.upstreamRechargeForm.reset()
      elements.upstreamRechargeName.textContent = upstream.name || upstream.base_url
      const configured = upstream.recharge_rate || null
      const mode = configured?.input_mode === 'cny_per_usd' ? 'cny_per_usd' : 'usd_per_cny'
      const modeInput = elements.upstreamRechargeForm.querySelector(`input[name="upstream-recharge-mode"][value="${mode}"]`)
      if (modeInput) modeInput.checked = true
      elements.upstreamRechargeValue.value = configured?.input_value ?? ''
      elements.upstreamRechargeClearButton.hidden = !configured
      elements.upstreamRechargeError.hidden = true
      renderRechargePreview()
      elements.upstreamRechargeDialog.showModal()
      window.requestAnimationFrame(() => elements.upstreamRechargeValue.focus())
    }

    function selectedRechargeMode() {
      return elements.upstreamRechargeForm.querySelector('input[name="upstream-recharge-mode"]:checked')?.value || 'usd_per_cny'
    }

    function renderRechargePreview() {
      const mode = selectedRechargeMode()
      const isUSDPerCNY = mode === 'usd_per_cny'
      elements.upstreamRechargePrefix.textContent = isUSDPerCNY ? '1 CNY =' : ''
      elements.upstreamRechargePrefix.hidden = !isUSDPerCNY
      elements.upstreamRechargeSuffix.textContent = isUSDPerCNY ? 'USD' : 'CNY = 1 USD'
      const value = Number(elements.upstreamRechargeValue.value)
      if (!Number.isFinite(value) || value <= 0) {
        elements.upstreamRechargePreview.textContent = '— CNY/USD'
        elements.upstreamRechargeFormula.textContent = '最终倍率将在充值倍率有效后计算'
        return
      }
      const canonical = isUSDPerCNY ? 1 / value : value
      elements.upstreamRechargePreview.textContent = `${formatMultiplier(canonical)} CNY/USD`
      elements.upstreamRechargeFormula.textContent = `最终倍率 = ${formatMultiplier(canonical)} × 分组倍率`
    }

    async function saveRechargeRate(event) {
      event.preventDefault()
      if (!elements.upstreamRechargeForm.reportValidity()) return
      const upstream = getUpstream(state.rechargeUpstreamID)
      if (!upstream) return
      setRechargeBusy(true, '保存中')
      elements.upstreamRechargeError.hidden = true
      try {
        const response = await api(`/api/upstreams/${encodeURIComponent(upstream.id)}/recharge-rate`, {
          method: 'PUT',
          body: { mode: selectedRechargeMode(), value: Number(elements.upstreamRechargeValue.value) },
        })
        replaceUpstream(response.upstream)
        elements.upstreamRechargeDialog.close()
        showToast('充值倍率已保存，最终倍率已重新计算')
      } catch (error) {
        elements.upstreamRechargeError.textContent = error.message || '保存充值倍率失败'
        elements.upstreamRechargeError.hidden = false
      } finally {
        setRechargeBusy(false, '保存')
      }
    }

    async function clearRechargeRate() {
      const upstream = getUpstream(state.rechargeUpstreamID)
      if (!upstream?.recharge_rate) return
      setRechargeBusy(true, '清除中')
      elements.upstreamRechargeError.hidden = true
      try {
        const response = await api(`/api/upstreams/${encodeURIComponent(upstream.id)}/recharge-rate`, { method: 'DELETE' })
        replaceUpstream(response.upstream)
        elements.upstreamRechargeDialog.close()
        showToast('充值倍率配置已清除')
      } catch (error) {
        elements.upstreamRechargeError.textContent = error.message || '清除充值倍率失败'
        elements.upstreamRechargeError.hidden = false
      } finally {
        setRechargeBusy(false, '保存')
      }
    }

    function setRechargeBusy(busy, label) {
      setDialogBusy(elements.upstreamRechargeDialog, elements.upstreamRechargeSaveButton, busy, label)
      elements.upstreamRechargeClearButton.disabled = busy
      elements.upstreamRechargeValue.disabled = busy
      elements.upstreamRechargeForm.querySelectorAll('input[name="upstream-recharge-mode"]').forEach((input) => { input.disabled = busy })
    }

    function openBindings(upstream, trigger) {
      state.bindingTrigger = trigger
      state.bindingUpstreamID = upstream.id
      const candidateIDs = new Set((upstream.local_accounts || []).map((account) => account.id))
      state.bindingRows = flattenKeys(upstream)
      state.bindingOriginal = state.bindingRows.map((row) => candidateIDs.has(row.key.local_account_id) ? String(row.key.local_account_id) : '')
      state.bindingDraft = [...state.bindingOriginal]
      elements.upstreamBindingName.textContent = upstream.name || upstream.base_url
      elements.upstreamBindingError.hidden = true
      elements.upstreamStepUpNotice.hidden = true
      renderBindings()
      elements.upstreamBindingDialog.showModal()
      window.requestAnimationFrame(() => elements.upstreamBindingList.querySelector('select')?.focus() || elements.upstreamAutoMatchButton.focus())
    }

    function renderBindings() {
      const upstream = getUpstream(state.bindingUpstreamID)
      if (!upstream) return
      const accounts = [...(upstream.local_accounts || [])].sort(sortLocalAccounts)
      elements.upstreamBindingCount.textContent = `${formatInteger(state.bindingRows.length)} 个上游 Key · ${formatInteger(accounts.length)} 个本地账号 · ${formatInteger(bindingChangeCount())} 项待保存`
      elements.upstreamBindingList.innerHTML = state.bindingRows.length ? state.bindingRows.map((row, index) => {
        const keyStatus = remoteKeyStatus(row.key.status, row.key.stale)
        const options = ['<option value="">不绑定本地账号</option>', ...accounts.map((account) => `<option value="${account.id}" ${state.bindingDraft[index] === String(account.id) ? 'selected' : ''}>${escapeHTML(account.name || `账号 ${account.id}`)} · #${account.id} · ${account.schedulable ? '调度启用' : '调度停止'}</option>`)].join('')
        return `<div class="remote-binding-row" data-binding-index="${index}">
          <div class="remote-binding-key"><strong>${escapeHTML(row.key.name || `Key ${row.key.id}`)}</strong><span>${escapeHTML(row.key.masked_key || '无掩码')} · ${escapeHTML(row.key.group || '默认分组')}${row.key.stale ? ' · 快照已失效' : ''}</span></div>
          <div class="remote-binding-identity"><strong>${escapeHTML(row.identity.label || '登录身份')}</strong><span>${escapeHTML(keyStatus.label)} · ${escapeHTML(formatDateTime(row.key.synced_at))}</span></div>
          <label class="field"><span class="sr-only">为 ${escapeHTML(row.key.name || row.key.id)} 选择本地账号</span><select data-binding-index="${index}" ${elements.upstreamBindingDialog.dataset.busy === 'true' ? 'disabled' : ''}>${options}</select></label>
        </div>`
      }).join('') : '<div class="dialog-empty">尚无可绑定的上游 Key，请先同步登录身份</div>'
      const canMatch = state.credentialsEnabled && state.bindingRows.some((row) => row.key.match_available && !row.key.stale)
      elements.upstreamAutoMatchButton.disabled = elements.upstreamBindingDialog.dataset.busy === 'true' || !canMatch
      elements.upstreamBindingSaveButton.disabled = elements.upstreamBindingDialog.dataset.busy === 'true' || bindingChangeCount() === 0
    }

    function changeBinding(event) {
      const index = Number(event.target.dataset.bindingIndex)
      if (!Number.isInteger(index) || index < 0 || index >= state.bindingDraft.length) return
      const selected = event.target.value
      if (selected) {
        state.bindingDraft = state.bindingDraft.map((value, currentIndex) => currentIndex !== index && value === selected ? '' : value)
      }
      state.bindingDraft[index] = selected
      renderBindings()
      elements.upstreamBindingList.querySelector(`select[data-binding-index="${index}"]`)?.focus()
    }

    function bindingChangeCount() {
      return state.bindingDraft.reduce((total, value, index) => total + Number(value !== state.bindingOriginal[index]), 0)
    }

    async function saveBindings() {
      const upstream = getUpstream(state.bindingUpstreamID)
      if (!upstream || bindingChangeCount() === 0) return
      setBindingBusy(true, 'save', '保存中')
      elements.upstreamBindingError.hidden = true
      try {
        const bindings = state.bindingRows.flatMap((row, index) => state.bindingDraft[index] ? [{
          identity_id: row.identity.id,
          remote_key_id: row.key.id,
          local_account_id: Number(state.bindingDraft[index]),
        }] : [])
        await api(`/api/upstreams/${encodeURIComponent(upstream.id)}/bindings`, { method: 'PUT', body: { bindings } })
        await load(true)
        elements.upstreamBindingDialog.close()
        showToast('上游 Key 与本地账号绑定已保存')
      } catch (error) {
        elements.upstreamBindingError.textContent = error.message || '保存绑定失败'
        elements.upstreamBindingError.hidden = false
      } finally {
        setBindingBusy(false, 'save', '保存绑定')
      }
    }

    async function autoMatch() {
      const upstream = getUpstream(state.bindingUpstreamID)
      if (!upstream || !state.credentialsEnabled) return
      setBindingBusy(true, 'match', '匹配中')
      elements.upstreamBindingError.hidden = true
      elements.upstreamStepUpNotice.hidden = true
      try {
        const result = await api(`/api/upstreams/${encodeURIComponent(upstream.id)}/auto-match`, { method: 'POST' })
        await load(true)
        const updated = getUpstream(upstream.id)
        if (updated) {
          const candidateIDs = new Set((updated.local_accounts || []).map((account) => account.id))
          state.bindingRows = flattenKeys(updated)
          state.bindingOriginal = state.bindingRows.map((row) => candidateIDs.has(row.key.local_account_id) ? String(row.key.local_account_id) : '')
          state.bindingDraft = [...state.bindingOriginal]
        }
        renderBindings()
        const matched = result.matched?.length || 0
        const unavailable = result.unavailable?.length || 0
        const ambiguous = result.ambiguous?.length || 0
        showToast(`精确匹配完成：${matched} 个已绑定，${unavailable} 个不可匹配，${ambiguous} 个不唯一`, unavailable > 0 || ambiguous > 0)
      } catch (error) {
        if (error.code === 'STEP_UP_REQUIRED') {
          elements.upstreamStepUpNotice.textContent = 'Sub2API 要求管理员二次验证。请先在管理后台完成敏感操作验证，再点击“自动精确匹配”。'
          elements.upstreamStepUpNotice.hidden = false
        } else {
          elements.upstreamBindingError.textContent = error.message || '自动匹配失败'
          elements.upstreamBindingError.hidden = false
        }
      } finally {
        setBindingBusy(false, 'match', '自动精确匹配')
      }
    }

    function setBindingBusy(busy, action, label) {
      elements.upstreamBindingDialog.dataset.busy = busy ? 'true' : 'false'
      const button = action === 'match' ? elements.upstreamAutoMatchButton : elements.upstreamBindingSaveButton
      const labelNode = button.querySelector('span')
      if (labelNode) labelNode.textContent = label
      else button.textContent = label
      button.setAttribute('aria-busy', busy ? 'true' : 'false')
      document.querySelectorAll('[data-close-dialog="upstream-binding-dialog"]').forEach((button) => { button.disabled = busy })
      renderBindings()
    }

    function openDeleteIdentity(upstream, identity, trigger) {
      if (!identity) return
      state.deleteTrigger = trigger
      state.deleteUpstreamID = upstream.id
      state.deleteIdentityID = identity.id
      elements.upstreamDeleteIdentityName.textContent = `${identity.label || '登录身份'} · ${upstream.name || upstream.base_url}`
      elements.upstreamDeleteIdentityDialog.showModal()
      window.requestAnimationFrame(() => elements.upstreamDeleteIdentityButton.focus())
    }

    async function deleteIdentity() {
      if (!state.deleteUpstreamID || !state.deleteIdentityID) return
      setDialogBusy(elements.upstreamDeleteIdentityDialog, elements.upstreamDeleteIdentityButton, true, '删除中')
      try {
        await api(`/api/upstreams/${encodeURIComponent(state.deleteUpstreamID)}/identities/${encodeURIComponent(state.deleteIdentityID)}`, { method: 'DELETE' })
        await load(true)
        elements.upstreamDeleteIdentityDialog.close()
        showToast('登录身份已删除')
      } catch (error) {
        showToast(error.message || '删除登录身份失败', true)
      } finally {
        setDialogBusy(elements.upstreamDeleteIdentityDialog, elements.upstreamDeleteIdentityButton, false, '删除身份')
      }
    }

    function openDeleteUpstream(upstream, trigger) {
      if (!upstream?.persisted || (upstream.local_accounts || []).length !== 0) return
      state.deleteUpstreamTrigger = trigger
      state.deleteRecordUpstreamID = upstream.id
      elements.upstreamDeleteName.textContent = `${upstream.name || upstream.base_url} · ${upstream.base_url}`
      elements.upstreamDeleteDialog.showModal()
      window.requestAnimationFrame(() => elements.upstreamDeleteButton.focus())
    }

    async function deleteUpstream() {
      const upstreamID = state.deleteRecordUpstreamID
      if (!upstreamID || elements.upstreamDeleteDialog.dataset.busy === 'true') return
      setDialogBusy(elements.upstreamDeleteDialog, elements.upstreamDeleteButton, true, '删除中')
      try {
        await api(`/api/upstreams/${encodeURIComponent(upstreamID)}`, { method: 'DELETE' })
        state.expanded.delete(upstreamID)
        persistExpanded()
        await load(true)
        elements.upstreamDeleteDialog.close()
        showToast('上游已删除；后续同地址 API Key 账号出现时会自动重新添加')
      } catch (error) {
        await load(true)
        const refreshed = getUpstream(upstreamID)
        if (error.code === 'UPSTREAM_IN_USE' || !refreshed || (refreshed.local_accounts || []).length > 0) {
          elements.upstreamDeleteDialog.close()
        }
        showToast(error.message || '删除上游失败', true)
      } finally {
        setDialogBusy(elements.upstreamDeleteDialog, elements.upstreamDeleteButton, false, '确认删除')
      }
    }

    function setDialogBusy(dialog, button, busy, label) {
      dialog.dataset.busy = busy ? 'true' : 'false'
      button.disabled = busy
      const labelNode = button.querySelector('span')
      if (labelNode) labelNode.textContent = label
      else button.textContent = label
      dialog.querySelectorAll('[data-close-dialog]').forEach((item) => { item.disabled = busy })
      if (dialog === elements.upstreamConnectDialog) renderConnectChallenge()
    }

    function restoreFocus(kind) {
      const triggerKey = `${kind}Trigger`
      const trigger = state[triggerKey]
      state[triggerKey] = null
      window.requestAnimationFrame(() => {
        if (trigger?.isConnected) {
          trigger.focus()
          return
        }
        if (kind === 'create') elements.addUpstreamButton.focus()
        else if (kind === 'connect') findActionButton(state.connectUpstreamID, 'connect')?.focus()
        else if (kind === 'recharge') findActionButton(state.rechargeUpstreamID, 'recharge-rate')?.focus()
        else if (kind === 'binding') findActionButton(state.bindingUpstreamID, 'bind')?.focus()
        else if (kind === 'deleteUpstream') {
          const fallback = findActionButton(state.deleteRecordUpstreamID, 'delete-upstream') || findActionButton(state.deleteRecordUpstreamID, 'toggle') || elements.upstreamRefreshButton
          fallback?.focus()
        }
      })
    }

    function findActionButton(upstreamID, action) {
      return [...elements.upstreamList.querySelectorAll(`[data-upstream-action="${action}"]`)]
        .find((button) => button.closest('[data-upstream-id]')?.dataset.upstreamId === upstreamID)
    }

    function getUpstream(upstreamID) {
      return state.upstreams.find((item) => item.id === upstreamID) || null
    }

    function replaceUpstream(upstream) {
      if (!upstream?.id) return
      const index = state.upstreams.findIndex((item) => item.id === upstream.id)
      if (index >= 0) state.upstreams[index] = upstream
      else state.upstreams.push(upstream)
      state.upstreams.sort((a, b) => String(a.name || a.base_url).localeCompare(String(b.name || b.base_url), 'zh-CN'))
      render()
    }

    function findIdentity(upstream, identityID) {
      return (upstream.identities || []).find((item) => item.id === identityID) || null
    }

    function flattenKeys(upstream) {
      return (upstream.identities || []).flatMap((identity) => (identity.keys || []).map((key) => ({ identity, key })))
    }

    function keyCountFor(upstream) {
      return (upstream.identities || []).reduce((total, identity) => total + (identity.keys || []).length, 0)
    }

    function groupCountFor(upstream) {
      return (upstream.identities || []).reduce((total, identity) => total + (identity.groups || []).length, 0)
    }

    function siteState(upstream) {
      const identities = upstream.identities || []
      if (!identities.length) return { key: 'unconnected', label: '未连接', tone: 'neutral' }
      if (identities.some((identity) => ATTENTION_STATUSES.has(identity.status))) return { key: 'attention', label: '需处理', tone: 'warning' }
      if (identities.some((identity) => identity.status === 'connected')) return { key: 'connected', label: '已连接', tone: 'success' }
      return { key: 'unconnected', label: '未连接', tone: 'neutral' }
    }

    function identityStatus(status) {
      return {
        label: IDENTITY_LABELS[status] || '状态未知',
        tone: status === 'connected' ? 'success' : (ATTENTION_STATUSES.has(status) ? (status === 'management_access_denied' || status === 'network_error' || status === 'sync_error' ? 'danger' : 'warning') : 'neutral'),
      }
    }

    function remoteKeyStatus(status, stale) {
      if (stale) return { label: '快照失效', tone: 'warning' }
      const normalized = String(status || '').toLocaleLowerCase()
      if (['active', 'enabled', 'normal'].includes(normalized)) return { label: '启用', tone: 'success' }
      if (['expired', 'error', 'invalid'].includes(normalized)) return { label: normalized === 'expired' ? '已过期' : '异常', tone: 'danger' }
      if (['disabled', 'inactive'].includes(normalized)) return { label: '停用', tone: 'neutral' }
      return { label: status || '未知', tone: 'neutral' }
    }

    function renderTypeBadge(upstream) {
      const label = siteTypeLabel(upstream.type)
      const suffix = upstream.type_override && upstream.type_override !== 'unknown' ? ' · 手动' : ''
      return `<span class="type-badge">${escapeHTML(label + suffix)}</span>`
    }

    function renderRechargeSummary(upstream) {
      const rate = numberOrNull(upstream.recharge_rate?.cny_per_usd)
      if (rate === null) return '<div class="recharge-stat"><span class="upstream-summary-label">充值倍率</span><strong>未设置</strong></div>'
      return `<div class="recharge-stat"><span class="upstream-summary-label">充值倍率</span><strong>${escapeHTML(formatMultiplier(rate))} CNY/USD</strong></div>`
    }

    function renderResourceSummary(localAccountCount, identityCount, keyCount, groupCount) {
      return `<div class="upstream-resource-summary">
        <span class="upstream-summary-label">资源</span>
        <div class="resource-summary-values">
          <span><strong>${formatInteger(localAccountCount)}</strong> 本地账号</span>
          <span><strong>${formatInteger(identityCount)}</strong> 登录身份</span>
          <span><strong>${formatInteger(groupCount)}</strong> 分组</span>
          <span><strong>${formatInteger(keyCount)}</strong> Key</span>
        </div>
      </div>`
    }

    function renderUpstreamBalanceSummary(upstream) {
      const summary = upstream.balance_summary || {}
      if (summary.status === 'single' && summary.balance) {
        const balance = formatUpstreamBalance(summary.balance)
        return `<div class="upstream-balance-stat ${summary.balance.stale ? 'stale' : 'current'}" title="${escapeAttr(balance.detail)}">
          <span class="upstream-summary-label">站点余额${summary.balance.stale ? ' · 旧数据' : ''}</span>
          <strong>${escapeHTML(balance.primary)}</strong>
          <span class="upstream-summary-detail">${escapeHTML(balance.secondary)}</span>
        </div>`
      }
      const identityCount = Math.max(0, Number(summary.identity_count) || (upstream.identities || []).length)
      const availableCount = Math.max(0, Number(summary.available_count) || 0)
      if (summary.status === 'multiple' && availableCount > 0) {
        return `<div class="upstream-balance-stat multiple" title="各登录账号余额分别展示，不合计">
          <span class="upstream-summary-label">账号余额</span>
          <strong>${formatInteger(availableCount)} 个账号余额</strong>
          <span class="upstream-summary-detail">${availableCount < identityCount ? `${formatInteger(identityCount)} 个身份中已获取 ${formatInteger(availableCount)} 个` : '各登录账号余额分别展示，不合计'}</span>
        </div>`
      }
      return '<div class="upstream-balance-stat unavailable"><span class="upstream-summary-label">站点余额</span><strong>待同步</strong><span class="upstream-summary-detail">登录并同步后刷新</span></div>'
    }

    function renderIdentityBalance(identity) {
      if (!identity.balance) {
        const hint = identity.has_credential ? '同步该身份后刷新' : '登录后获取'
        return `<div class="identity-balance unavailable"><span class="identity-balance-label">站点余额</span><strong>暂不可获取</strong><span>${escapeHTML(hint)}</span></div>`
      }
      const balance = formatUpstreamBalance(identity.balance)
      return `<div class="identity-balance ${identity.balance.stale ? 'stale' : 'current'}" title="${escapeAttr(balance.detail)}">
        <span class="identity-balance-label">站点余额${identity.balance.stale ? renderStatusPill('旧数据', 'warning') : ''}</span>
        <strong>${escapeHTML(balance.primary)}</strong>
        <span>${escapeHTML(balance.secondary)}</span>
      </div>`
    }

    function formatUpstreamBalance(balance) {
      const amount = numberOrNull(balance?.amount)
      if (amount === null) return { primary: '暂不可获取', secondary: '余额数据无效', detail: '上游未返回有效余额' }
      const unit = balance?.unit === 'USD' ? 'USD' : (balance?.unit === 'quota' ? 'quota' : String(balance?.unit || ''))
      const primary = `${formatBalanceAmount(amount)}${unit ? ` ${unit}` : ''}`
      const source = balance?.source === 'sub2api' ? 'Sub2API' : (balance?.source === 'newapi' ? 'NewAPI' : '上游')
      const observed = formatDateTime(balance?.observed_at)
      const secondary = `${unit === 'quota' ? '原始额度' : source} · ${balance?.stale ? '上次获取' : '更新'} ${observed}`
      let detail = `${source} 站点余额，${balance?.stale ? '数据已过期，' : ''}${balance?.stale ? '上次获取' : '更新'}于 ${observed}`
      const rawQuota = numberOrNull(balance?.raw_quota)
      const quotaPerUnit = numberOrNull(balance?.quota_per_unit)
      if (unit === 'USD' && rawQuota !== null && quotaPerUnit !== null && quotaPerUnit > 0) {
        detail += `；按 ${formatBalanceAmount(rawQuota)} quota ÷ ${formatBalanceAmount(quotaPerUnit)} quota/USD 换算`
      } else if (unit === 'quota') {
        detail += '；上游未提供有效 quota_per_unit，未换算货币'
      }
      return { primary, secondary, detail }
    }

    function renderStatusPill(label, tone) {
      return `<span class="status-pill ${escapeAttr(tone)}"><i class="status-dot"></i>${escapeHTML(label)}</span>`
    }

    function siteTypeLabel(type) {
      return ({ sub2api: 'Sub2API', newapi: 'NewAPI', unknown: '待识别' })[type] || '待识别'
    }

    function authModeLabel(mode) {
      return ({ password: '账号密码', token: 'Token', session: 'Cookie / Session' })[mode] || '未知方式'
    }

    function formatRemoteQuota(key) {
      if (key.unlimited_quota) return { primary: '无限额', secondary: `已用 ${formatNumber(key.quota_used)}` }
      const used = numberOrNull(key.quota_used)
      const limit = numberOrNull(key.quota_limit)
      if (limit !== null) return { primary: `${formatNumber(used || 0)} / ${formatNumber(limit)}`, secondary: limit > 0 ? `已用 ${formatPercent((used || 0) / limit * 100)}` : '额度为 0' }
      if (used !== null) return { primary: formatNumber(used), secondary: '上游仅返回已用量' }
      return { primary: '暂无数据', secondary: '上游未返回额度' }
    }

    function formatRemoteRate(key, upstream) {
      const groupMultiplier = numberOrNull(key.multiplier)
      const rechargeMultiplier = numberOrNull(upstream.recharge_rate?.cny_per_usd)
      const finalMultiplier = numberOrNull(key.final_multiplier)
      const source = String(key.multiplier_source || '')
      const sourceLabel = source === 'dynamic' ? '动态日志观测' : (source === 'user_override' ? '用户有效倍率' : (source === 'group' ? '分组固定倍率' : '上游倍率'))
      const groupLabel = groupMultiplier === null ? '未知' : formatMultiplier(groupMultiplier)
      const rechargeLabel = rechargeMultiplier === null ? '未设置' : formatMultiplier(rechargeMultiplier)
      let detail = sourceLabel
      if (key.multiplier_observed_at) detail += ` · ${formatDateTime(key.multiplier_observed_at)}`
      else if (groupMultiplier === null && (source === 'dynamic' || String(key.group || '').toLocaleLowerCase() === 'auto')) detail = '动态分组暂无近期日志观测'
      else if (groupMultiplier === null) detail = '上游未返回分组倍率'
      if (rechargeMultiplier === null) detail += ' · 请设置充值倍率'
      return {
        primary: finalMultiplier === null ? '未计算' : `${formatMultiplier(finalMultiplier)}x`,
        secondary: `分组 ${groupLabel} × 充值 ${rechargeLabel}`,
        detail,
      }
    }

    function renderRemoteGroupPlatformBadge(platform) {
      const normalized = String(platform || '').trim().toLocaleLowerCase()
      const metadata = {
        openai: { label: 'OpenAI', tone: 'openai' },
        anthropic: { label: 'Anthropic', tone: 'anthropic' },
        gemini: { label: 'Gemini', tone: 'gemini' },
        grok: { label: 'GROK', tone: 'grok' },
      }[normalized] || { label: '未知类型', tone: 'unknown' }
      return `<span class="remote-platform-badge ${metadata.tone}">${escapeHTML(metadata.label)}</span>`
    }

    function formatRemoteGroupRate(group, upstream) {
      const groupMultiplier = numberOrNull(group.multiplier)
      const rechargeMultiplier = numberOrNull(upstream.recharge_rate?.cny_per_usd)
      const finalMultiplier = numberOrNull(group.final_multiplier)
      const source = String(group.multiplier_source || '')
      const sourceLabel = source === 'dynamic' ? '动态分组' : (source === 'user_override' ? '用户有效倍率' : (source === 'group' ? '分组固定倍率' : '上游未提供倍率'))
      const groupPrimary = groupMultiplier === null ? (source === 'dynamic' ? '动态' : '未提供') : `${formatMultiplier(groupMultiplier)}x`
      let finalSecondary
      if (finalMultiplier !== null) {
        finalSecondary = `分组 ${formatMultiplier(groupMultiplier)} × 充值 ${formatMultiplier(rechargeMultiplier)}`
      } else if (source === 'dynamic') {
        finalSecondary = '动态分组不设统一倍率'
      } else if (groupMultiplier === null) {
        finalSecondary = '上游未返回分组倍率'
      } else {
        finalSecondary = '请设置充值倍率'
      }
      return {
        groupPrimary,
        groupSecondary: sourceLabel,
        finalPrimary: finalMultiplier === null ? '未计算' : `${formatMultiplier(finalMultiplier)}x`,
        finalSecondary,
      }
    }

    function sortLocalAccounts(left, right) {
      if (left.schedulable !== right.schedulable) return left.schedulable ? -1 : 1
      return String(left.name || '').localeCompare(String(right.name || ''), 'zh-CN') || left.id - right.id
    }

    function formatDateTime(value) {
      if (!value) return '从未'
      const date = new Date(value)
      if (Number.isNaN(date.getTime())) return '未知'
      return new Intl.DateTimeFormat('zh-CN', { month: '2-digit', day: '2-digit', hour: '2-digit', minute: '2-digit', hour12: false }).format(date)
    }

    function formatInteger(value) {
      return new Intl.NumberFormat('zh-CN', { maximumFractionDigits: 0 }).format(Number(value) || 0)
    }

    function formatNumber(value) {
      const number = Number(value)
      if (!Number.isFinite(number)) return '—'
      return new Intl.NumberFormat('zh-CN', { notation: Math.abs(number) >= 100000 ? 'compact' : 'standard', maximumFractionDigits: 2 }).format(number)
    }

    function formatBalanceAmount(value) {
      const number = Number(value)
      if (!Number.isFinite(number)) return '—'
      return new Intl.NumberFormat('zh-CN', { maximumFractionDigits: 6 }).format(number)
    }

    function formatMultiplier(value) {
      const normalized = Number(Number(value).toPrecision(12))
      return new Intl.NumberFormat('zh-CN', { maximumFractionDigits: 4 }).format(normalized)
    }

    function formatPercent(value) {
      const normalized = Number(value)
      return Number.isFinite(normalized) ? `${normalized.toFixed(normalized % 1 ? 1 : 0)}%` : '—'
    }

    function numberOrNull(value) {
      const number = Number(value)
      return value === null || value === undefined || value === '' || !Number.isFinite(number) ? null : number
    }

    function readExpanded() {
      try {
        const value = JSON.parse(localStorage.getItem(EXPANDED_UPSTREAMS_KEY) || '[]')
        return new Set(Array.isArray(value) ? value.map(String) : [])
      } catch {
        return new Set()
      }
    }

    function persistExpanded() {
      try {
        localStorage.setItem(EXPANDED_UPSTREAMS_KEY, JSON.stringify([...state.expanded]))
      } catch {
        // Expanded state is optional when browser storage is unavailable.
      }
    }

    function safeID(value) {
      return String(value || '').replace(/[^a-zA-Z0-9_-]/g, '-')
    }

    function escapeHTML(value) {
      return String(value ?? '').replace(/[&<>'"]/g, (character) => ({ '&': '&amp;', '<': '&lt;', '>': '&gt;', "'": '&#39;', '"': '&quot;' })[character])
    }

    function escapeAttr(value) { return escapeHTML(value) }
    function toCamel(value) { return value.replace(/-([a-z])/g, (_, letter) => letter.toUpperCase()) }
  }

  window.createUpstreamWorkspace = createUpstreamWorkspace
})()
