(() => {
  'use strict'

  window.createGroupAccessWorkspace = ({ api, getGroups }) => {
    const el = Object.fromEntries(['dialog', 'target', 'source', 'search', 'refresh', 'all', 'clear', 'count',
      'list', 'message', 'confirmation', 'confirm-text', 'back', 'confirm', 'page', 'prev', 'next', 'stop',
      'submit', 'scroll'].map(name => [name, document.getElementById(`group-access-${name}`)]))
    const escape = value => String(value ?? '').replace(/[&<>"']/g, char =>
      ({ '&': '&amp;', '<': '&lt;', '>': '&gt;', '"': '&quot;', "'": '&#39;' }[char]))
    const pageSize = 20
    let target = null
    let users = []
    let selected = new Set()
    let results = new Map()
    let page = 1
    let generation = 0
    let loading = false
    let busy = false
    let ready = false
    let confirming = false
    let stopped = false
    let trigger = null
    let sourceName = ''
    let confirmedIDs = []
    const syncMode = () => Number(el.source.value) > 0
    const eligible = user => syncMode() && !user.authorized && !results.has(user.id)
    const controls = () => {
      const locked = busy || loading || confirming
      el.source.disabled = locked
      el.search.disabled = busy || confirming
      el.refresh.disabled = locked
      el.all.disabled = locked || !ready || !users.some(eligible)
      el.clear.disabled = locked || selected.size === 0
      el.submit.disabled = locked || !ready || !syncMode() || selected.size === 0
      el.submit.hidden = busy
      el.stop.hidden = !busy
      el.stop.disabled = stopped
      el.confirmation.hidden = !confirming
      el.confirm.disabled = busy || !confirming
      el.back.disabled = busy
      el.dialog.dataset.busy = String(busy)
      el.dialog.querySelector('[data-close-dialog]').disabled = busy
      el.count.textContent = syncMode()
        ? `来源共 ${users.length} 人 · 可同步 ${users.filter(eligible).length} 人 · 已选 ${selected.size} 人`
        : `已授权 ${users.length} 人`
    }
    function render() {
      const q = el.search.value.trim().toLocaleLowerCase()
      const filtered = users.filter(user => `${user.username} ${user.email} ${user.id}`.toLocaleLowerCase().includes(q))
      const pages = Math.max(1, Math.ceil(filtered.length / pageSize))
      page = Math.min(page, pages)
      controls()
      el.prev.disabled = loading || busy || confirming || page <= 1
      el.next.disabled = loading || busy || confirming || page >= pages
      el.page.textContent = `第 ${page} / ${pages} 页 · ${filtered.length} 人`
      if (loading) {
        el.list.innerHTML = '<tr><td colspan="4">正在读取完整授权名单…</td></tr>'
        return
      }
      el.list.innerHTML = filtered.slice((page - 1) * pageSize, page * pageSize).map(user => {
        const result = results.get(user.id)
        const status = user.status === 'active' ? '正常' : user.status === 'disabled' ? '已禁用' : user.status || '未知'
        return `<tr>
          <td>${syncMode() ? `<input type="checkbox" data-user-id="${user.id}" aria-label="选择用户 ${escape(user.email || user.username || user.id)}" ${selected.has(user.id) ? 'checked' : ''} ${!ready || !eligible(user) || busy || confirming ? 'disabled' : ''}>` : '—'}</td>
          <td><strong>${escape(user.username || `用户 #${user.id}`)}</strong><small>${escape(user.email)} · #${user.id}</small></td>
          <td>${escape(status)}</td>
          <td class="${result?.status === 'unknown' || result?.status === 'failed' ? 'text-danger' : ''}">${escape(result?.message || (user.authorized ? '已授权' : '可同步'))}</td>
        </tr>`
      }).join('') || `<tr><td colspan="4">${ready ? '暂无匹配用户' : '列表未就绪，请刷新重试'}</td></tr>`
    }
    async function load() {
      if (busy) return
      const request = ++generation
      users = []
      results = new Map()
      selected = new Set()
      ready = false
      loading = true
      confirming = false
      page = 1
      el.message.textContent = ''
      render()
      const id = target.id
      const source = Number(el.source.value)
      try {
        const data = await api(`/api/groups/${id}/access-users${source ? `?source_group_id=${source}` : ''}`)
        if (request !== generation || !el.dialog.open) return
        if (!Array.isArray(data.users)) throw new Error('授权名单格式无效，请重试')
        users = data.users
        sourceName = data.source_group.name
        ready = true
      } catch (error) {
        if (request === generation) el.message.textContent = error.message || '加载失败，请刷新重试'
      } finally {
        if (request === generation) { loading = false; render() }
      }
    }
    function open(id, button) {
      if (busy) return
      target = getGroups().find(group => group.id === id && group.is_exclusive)
      if (!target) return
      trigger = button
      el.target.textContent = `目标分组：${target.name} · #${target.id}`
      el.source.innerHTML = '<option value="0">查看当前分组已授权用户</option>' +
        getGroups().filter(group => group.is_exclusive && group.id !== id)
          .map(group => `<option value="${group.id}">从 ${escape(group.name)} (#${group.id}) 同步</option>`).join('')
      el.search.value = ''
      el.dialog.showModal()
      void load()
    }
    el.source.addEventListener('change', () => { el.search.value = ''; void load() })
    el.refresh.addEventListener('click', () => { void load() })
    el.search.addEventListener('input', () => { page = 1; render() })
    el.all.addEventListener('click', () => { selected = new Set(users.filter(eligible).map(user => user.id)); render() })
    el.clear.addEventListener('click', () => { selected.clear(); render() })
    el.list.addEventListener('change', event => {
      const id = Number(event.target.dataset.userId)
      if (!ready || busy || confirming || !users.some(user => user.id === id && eligible(user))) return
      if (event.target.checked) selected.add(id)
      else selected.delete(id)
      controls()
    })
    el.prev.addEventListener('click', () => { page--; render(); el.scroll.scrollTop = 0 })
    el.next.addEventListener('click', () => { page++; render(); el.scroll.scrollTop = 0 })
    el.submit.addEventListener('click', () => {
      if (!ready || busy || !selected.size) return
      confirmedIDs = users.filter(user => selected.has(user.id) && eligible(user)).map(user => user.id)
      confirming = true
      el['confirm-text'].textContent = `将 ${sourceName} 中选中的 ${confirmedIDs.length} 位用户追加授权到 ${target.name}。保留所有原有权限，不复制倍率、余额或订阅。是否确认？`
      render()
      el.confirm.focus()
    })
    el.back.addEventListener('click', () => { confirming = false; render(); el.submit.focus() })
    el.stop.addEventListener('click', () => { stopped = true; controls(); el.message.textContent = '当前批次完成后停止，已完成授权不会撤销。' })
    el.confirm.addEventListener('click', async () => {
      if (busy || !confirming || !confirmedIDs.length) return
      const ids = [...confirmedIDs]
      const source = Number(el.source.value)
      const targetID = target.id
      busy = true
      confirming = false
      stopped = false
      results = new Map()
      selected.clear()
      render()
      let completed = 0
      let interruption = ''
      try {
        for (let offset = 0; offset < ids.length && !stopped; offset += 10) {
          const batch = ids.slice(offset, offset + 10)
          el.message.textContent = `同步中 ${completed} / ${ids.length}；可停止后续批次`
          try {
            // No auth retry on a mutation: a lost response may already have
            // committed permissions. The administrator must reload to verify.
            const data = await api(`/api/groups/${targetID}/access-users/sync`,
              { method: 'POST', body: { source_group_id: source, user_ids: batch } }, false)
            if (!Array.isArray(data.results) || data.results.length !== batch.length ||
              batch.some(id => data.results.filter(row => row.user_id === id).length !== 1) ||
              data.results.some(row => !['added', 'skipped', 'failed', 'unknown'].includes(row.status))) {
              throw new Error('同步结果不完整')
            }
            for (const row of data.results) {
              results.set(row.user_id, row)
              if (row.status === 'unknown') stopped = true
            }
          } catch (error) {
            batch.forEach(id => results.set(id, { user_id: id, status: 'unknown', message: '请求结果未确认，请刷新核对权限' }))
            interruption = error.message || '连接中断'
            stopped = true
          }
          completed += batch.length
          render()
        }
      } finally {
        ids.filter(id => !results.has(id)).forEach(id => results.set(id, { user_id: id, status: 'failed', message: '未执行' }))
        busy = false
        ready = false // Require a fresh permission snapshot before another run.
        const rows = [...results.values()]
        const count = status => rows.filter(row => row.status === status).length
        el.message.textContent = `本次：成功 ${count('added')} · 跳过 ${count('skipped')} · 失败/未执行 ${count('failed')} · 结果待确认 ${count('unknown')}。${interruption ? `${interruption}。` : ''}请刷新列表核对后再操作。`
        render()
        el.refresh.focus()
      }
    })
    el.dialog.addEventListener('cancel', event => { if (busy) event.preventDefault() })
    el.dialog.addEventListener('close', () => {
      generation++
      loading = false
      users = []
      selected.clear()
      results.clear()
      trigger?.isConnected && trigger.focus()
    })
    return { open }
  }
})()
