// Run against the static UI served on loopback :18769 with Playwright:
// const {chromium} = require('playwright'); const run = require('./group_access_browser_test.cjs');
// const browser = await chromium.launch(); await run(await browser.newPage()); await browser.close();
// All API traffic is mocked; this test never reads or modifies production data.
const assert = require('node:assert/strict')

module.exports = async function run(page) {
  const errors = []
  const requests = []
  let mode = 'success'
  let releaseBatch
  const groups = [
    { id: 1, name: '来源 A', is_exclusive: true, status: 'active', platform: 'openai', sort_order: 1 },
    { id: 2, name: '目标 B', is_exclusive: true, status: 'active', platform: 'openai', sort_order: 2 },
    { id: 3, name: '公开 C', is_exclusive: false, status: 'active', platform: 'openai', sort_order: 3 },
  ]
  const users = Array.from({ length: 25 }, (_, index) => ({
    id: index + 1, username: index === 0 ? '<img src=x onerror=alert(1)>' : `用户 ${index + 1}`,
    email: `user${index + 1}@example.test`, status: index === 4 ? 'disabled' : 'active',
    authorized: index === 24,
  }))
  page.on('pageerror', error => errors.push(error.message))
  await page.addInitScript(() => {
    localStorage.setItem('auth_token', 'local-ui-test')
    localStorage.setItem('token_expires_at', String(Date.now() + 3600000))
  })
  await page.route('**/api/**', async route => {
    const request = route.request()
    const url = new URL(request.url())
    const path = url.pathname
    requests.push({ path, method: request.method(), body: request.postDataJSON() })
    let body = {}
    if (path === '/api/session') body = { user: { id: 1, role: 'admin' }, credentials_enabled: false }
    else if (path === '/api/overview') body = { groups, accounts: [], group_balance_summaries: {}, group_protections: {} }
    else if (path === '/api/online-users/summary') body = { ready: true, count: 0, group_counts: {} }
    else if (path.endsWith('/access-users/sync')) {
      const data = request.postDataJSON()
      assert.equal(data.source_group_id, 1)
      assert.ok(data.user_ids.length > 0 && data.user_ids.length <= 10)
      assert.ok(!data.user_ids.includes(25))
      if (mode === 'paused') await new Promise(resolve => { releaseBatch = resolve })
      if (mode === 'unknown') {
        body = { results: data.user_ids.map(id => ({ user_id: id, status: 'unknown', message: '更新结果未确认' })) }
      } else {
        body = { results: data.user_ids.map(id => ({ user_id: id, status: 'added', message: '已追加权限' })) }
      }
    } else if (path.endsWith('/access-users')) {
      const source = url.searchParams.get('source_group_id')
      if (source && mode === 'load-error') return route.fulfill({ status: 502, json: { message: '测试加载失败' } })
      body = { group: groups[1], source_group: source ? groups[0] : groups[1],
        users: source ? users : [users[24]] }
    }
    await route.fulfill({ status: 200, json: body })
  })
  await page.goto('http://127.0.0.1:18769/')
  await page.getByRole('button', { name: '查看 目标 B 的授权用户', exact: true }).waitFor()
  assert.equal(await page.locator('.group-access-badge.exclusive').count(), 2)
  assert.equal(await page.getByText('公开分组', { exact: true }).count(), 1)
  // No eager permissions scan on overview.
  assert.equal(requests.filter(r => r.path.endsWith('/access-users')).length, 0)
  await page.getByRole('button', { name: '查看 目标 B 的授权用户', exact: true }).click()
  await page.locator('#group-access-count').filter({ hasText: '已授权 1 人' }).waitFor()
  await page.locator('#group-access-source').selectOption('1')
  await page.locator('#group-access-count').filter({ hasText: '来源共 25 人' }).waitFor()
  assert.equal(await page.locator('#group-access-source option').count(), 2)
  assert.equal(await page.locator('#group-access-list input').count(), 20)
  assert.equal(await page.locator('#group-access-list img').count(), 0)
  await page.locator('[data-user-id="1"]').check()
  await page.locator('#group-access-next').click()
  await page.locator('[data-user-id="21"]').check()
  assert.ok(await page.locator('[data-user-id="25"]').isDisabled())
  await page.locator('#group-access-search').fill('user1@')
  assert.ok((await page.locator('#group-access-count').textContent()).includes('已选 2 人'))
  await page.locator('#group-access-submit').click()
  assert.ok((await page.locator('#group-access-confirm-text').textContent()).includes('2 位用户'))
  assert.equal(requests.filter(r => r.method === 'POST').length, 0)
  await page.locator('#group-access-back').click()
  await page.locator('#group-access-search').fill('')
  await page.locator('#group-access-all').click()
  assert.ok((await page.locator('#group-access-count').textContent()).includes('已选 24 人'))
  await page.locator('#group-access-submit').click()
  await page.locator('#group-access-confirm').click()
  await page.locator('#group-access-message').filter({ hasText: '成功 24' }).waitFor()
  const writes = requests.filter(r => r.method === 'POST')
  assert.deepEqual(writes.map(r => r.body.user_ids.length), [10, 10, 4])
  assert.equal(new Set(writes.flatMap(r => r.body.user_ids)).size, 24)
  assert.ok(await page.locator('#group-access-submit').isDisabled())
  // Must refresh after outcomes; unknown writes stop the remaining batches.
  mode = 'unknown'
  await page.locator('#group-access-refresh').click()
  await page.locator('#group-access-all').waitFor({ state: 'visible' })
  await page.locator('#group-access-all').click()
  await page.locator('#group-access-submit').click()
  await page.locator('#group-access-confirm').click()
  await page.locator('#group-access-message').filter({ hasText: '结果待确认 10' }).waitFor()
  assert.equal(requests.filter(r => r.method === 'POST').length, 4)
  assert.ok((await page.locator('#group-access-message').textContent()).includes('失败/未执行 14'))
  mode = 'load-error'
  await page.locator('#group-access-refresh').click()
  await page.locator('#group-access-message').filter({ hasText: '测试加载失败' }).waitFor()
  assert.ok(await page.locator('#group-access-submit').isDisabled())
  mode = 'success'
  await page.locator('#group-access-refresh').click()
  await page.locator('#group-access-count').filter({ hasText: '来源共 25 人' }).waitFor()
  mode = 'paused'
  await page.locator('#group-access-all').click()
  await page.locator('#group-access-submit').click()
  await page.locator('#group-access-confirm').click()
  await page.waitForFunction(() => document.getElementById('group-access-dialog').dataset.busy === 'true')
  assert.ok(await page.locator('#group-access-dialog [data-close-dialog]').isDisabled())
  await page.keyboard.press('Escape')
  assert.ok(await page.locator('#group-access-dialog').evaluate(el => el.open))
  await page.locator('#group-access-stop').click()
  // The request reached the mocked server before the user can stop the batch.
  assert.equal(typeof releaseBatch, 'function')
  releaseBatch()
  await page.locator('#group-access-message').filter({ hasText: '成功 10' }).waitFor()
  assert.equal(requests.filter(r => r.method === 'POST').length, 5)
  assert.ok((await page.locator('#group-access-message').textContent()).includes('失败/未执行 14'))
  mode = 'success'
  await page.locator('#group-access-refresh').click()
  await page.locator('#group-access-count').filter({ hasText: '来源共 25 人' }).waitFor()
  // Scroll isolation and desktop/mobile layouts.
  for (const width of [1440, 375, 812]) {
    await page.setViewportSize({ width, height: width === 812 ? 375 : 900 })
    const layout = await page.evaluate(() => {
      const dialog = document.getElementById('group-access-dialog')
      const scroll = document.getElementById('group-access-scroll')
      scroll.scrollTop = scroll.scrollHeight
      return {
        width: innerWidth, rect: dialog.getBoundingClientRect().toJSON(),
        overflow: dialog.scrollWidth > dialog.clientWidth + 1,
        bodyOverflow: getComputedStyle(document.body).overflow,
        overscroll: getComputedStyle(scroll).overscrollBehaviorY,
      }
    })
    assert.ok(!layout.overflow, `dialog horizontal overflow at ${width}`)
    assert.ok(layout.rect.left >= 0 && layout.rect.right <= width + 1)
    assert.equal(layout.bodyOverflow, 'hidden')
    assert.equal(layout.overscroll, 'contain')
    const submitRect = await page.locator('#group-access-submit').boundingBox()
    const dialogRect = await page.locator('#group-access-dialog').boundingBox()
    assert.ok(submitRect.y + submitRect.height <= dialogRect.y + dialogRect.height + 1, `submit clipped at ${width}`)
  }
  await page.emulateMedia({ reducedMotion: 'reduce' })
  await page.evaluate(() => document.documentElement.dataset.theme = 'dark')
  assert.equal(await page.locator('#group-access-dialog').evaluate(el => el.scrollWidth > el.clientWidth + 1), false)
  await page.setViewportSize({ width: 1440, height: 900 })
  await page.locator('#group-access-dialog [data-close-dialog]').click()
  assert.ok(await page.getByRole('button', { name: '查看 目标 B 的授权用户', exact: true }).evaluate(el => el === document.activeElement))
  assert.deepEqual(errors, [])
  return { checks: 'badges, lazy loading, exact selection, pagination, XSS escaping, confirmation, batching, unknown stop, load retry, responsive layout, scroll lock, focus', writes: writes.length, errors }
}
