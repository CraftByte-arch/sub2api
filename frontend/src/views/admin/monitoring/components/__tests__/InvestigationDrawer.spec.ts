import { mount } from '@vue/test-utils'
import { nextTick } from 'vue'
import { afterEach, describe, expect, it, vi } from 'vitest'
import BaseDialog from '@/components/common/BaseDialog.vue'
import InvestigationDrawer from '../InvestigationDrawer.vue'

vi.mock('vue-i18n', async () => {
  const actual = await vi.importActual<typeof import('vue-i18n')>('vue-i18n')
  return {
    ...actual,
    useI18n: () => ({ t: (key: string, params?: Record<string, unknown>) => (params ? `${key} ${JSON.stringify(params)}` : key) })
  }
})

const counters = {
  attempt_count: 120,
  success_count: 114,
  client_canceled_count: 0,
  ttft_timeout_count: 2,
  rate_limit_count: 1,
  auth_count: 0,
  upstream_4xx_count: 0,
  upstream_5xx_count: 2,
  transport_count: 1,
  protocol_count: 0,
  other_failure_count: 0,
  failover_count: 3,
  ttft_sum_ms: 12000,
  duration_sum_ms: 36000,
  ttft_latency: { Samples: 120, LE1000MS: 80, LE2500MS: 110, LE5000MS: 118, LE10000MS: 120, LE30000MS: 120, GT30000MS: 0 },
  duration_latency: { Samples: 120, LE1000MS: 30, LE2500MS: 80, LE5000MS: 110, LE10000MS: 118, LE30000MS: 120, GT30000MS: 0 }
}

const account = { account_id: 42, account_name: 'Codex Team', account_type: 'oauth', auth_mode: 'personalAccessToken', platform: 'openai', counters, availability: 0.95, failure_rate: 0.05, health_score: 0.92, p95_ttft_ms: 2500, p95_duration_ms: 10000, low_sample: false }
const props = { open: true, account, investigation: null, loading: false, error: '' }
const global = {
  stubs: {
    MetricTrendCard: true,
    MonitoringTrendChart: true,
    FailureDistribution: true,
    PlatformTypeBadge: {
      props: ['platform', 'type', 'authMode'],
      template: '<span data-testid="platform-type-badge" />'
    }
  }
}

afterEach(() => {
  document.body.innerHTML = ''
  document.body.style.overflow = ''
})

describe('InvestigationDrawer', () => {
  it('shows an open dialog error state, retries, and closes from its close control', async () => {
    const wrapper = mount(InvestigationDrawer, { attachTo: document.body, props: { ...props, error: '详情加载失败' }, global })
    await nextTick()

    const dialog = document.body.querySelector('[role="dialog"]')
    expect(dialog).not.toBeNull()
    expect(dialog?.textContent).toContain('详情加载失败')
    document.body.querySelector<HTMLButtonElement>('[data-testid="performance-investigation-retry"]')?.click()
    document.body.querySelector<HTMLButtonElement>('button[aria-label="Close modal"]')?.click()
    await nextTick()

    expect(wrapper.emitted('retry')).toHaveLength(1)
    expect(wrapper.emitted('close')).toHaveLength(1)
    wrapper.unmount()
  })

  it('distinguishes a loading investigation from an empty investigation', async () => {
    const wrapper = mount(InvestigationDrawer, { attachTo: document.body, props: { ...props, loading: true }, global })
    await nextTick()
    expect(document.body.textContent).toContain('admin.monitoring.drawer.loading')

    await wrapper.setProps({ loading: false })
    expect(document.body.textContent).toContain('admin.monitoring.drawer.empty')
    wrapper.unmount()
  })

  it('uses the shared dialog lifecycle and restores the app background and prior focus after close', async () => {
    const appRoot = document.createElement('div')
    appRoot.id = 'app'
    const trigger = document.createElement('button')
    trigger.textContent = '打开详情'
    appRoot.append(trigger)
    document.body.append(appRoot)
    trigger.focus()

    const wrapper = mount(InvestigationDrawer, { attachTo: appRoot, props: { ...props, error: '详情加载失败' }, global })
    await nextTick()
    const dialog = wrapper.getComponent(BaseDialog)

    expect((appRoot as HTMLElement & { inert: boolean }).inert).toBe(true)
    expect(dialog.props('title')).toContain('Codex Team')
    expect(dialog.props('title')).toContain('#42')

    await wrapper.setProps({ open: false })
    await nextTick()
    expect((appRoot as HTMLElement & { inert: boolean }).inert).toBe(false)
    expect(document.body.style.overflow).toBe('')
    expect(document.activeElement).toBe(trigger)
    wrapper.unmount()
  })

  it('cleans every shared lock across rapid reopen and unmount', async () => {
    const appRoot = document.createElement('div')
    appRoot.id = 'app'
    document.body.append(appRoot)

    const wrapper = mount(InvestigationDrawer, { attachTo: appRoot, props, global })
    await nextTick()
    await wrapper.setProps({ open: false })
    await wrapper.setProps({ open: true })
    await wrapper.setProps({ open: false })
    await nextTick()
    wrapper.unmount()

    expect(appRoot.hasAttribute('inert')).toBe(false)
    expect(document.body.style.overflow).toBe('')
  })

  it('uses non-cancelled attempts for availability and failure metric contexts', async () => {
    const cancelledAccount = {
      ...account,
      counters: { ...counters, success_count: 90, client_canceled_count: 20 }
    }
    const wrapper = mount(InvestigationDrawer, {
      attachTo: document.body,
      props: { ...props, account: cancelledAccount },
      global: { stubs: { ...global.stubs, MetricTrendCard: { props: ['context'], template: '<div>{{ context }}</div>' } } }
    })
    await nextTick()

    expect(document.body.textContent).toContain('admin.monitoring.drawer.successContext {"success":90,"total":100}')
    expect(document.body.textContent).toContain('admin.monitoring.drawer.failureContext {"failure":10,"total":100}')
    wrapper.unmount()
  })

  it('maps raw failure outcomes to labeled, colored distribution items', async () => {
    const investigation = {
      time_points: [],
      failures: [
        { Outcome: 'ttftTimeout', Count: 2 },
        { Outcome: 'rate limit', Count: 1 },
        { Outcome: 'Mystery-Thing', Count: 5 }
      ]
    }
    const wrapper = mount(InvestigationDrawer, { attachTo: document.body, props: { ...props, investigation }, global })
    await nextTick()

    const distribution = wrapper.getComponent({ name: 'FailureDistribution' })
    expect(distribution.props('failures')).toEqual([
      { label: 'admin.monitoring.failures.outcomes.ttft_timeout', count: 2, color: '#ef4444' },
      { label: 'admin.monitoring.failures.outcomes.rate_limit', count: 1, color: '#f97316' },
      { label: 'admin.monitoring.failures.outcomes.other_failure', count: 5, color: '#64748b' }
    ])
    expect(distribution.props('title')).toBe('admin.monitoring.drawer.failureTitle')
    wrapper.unmount()
  })

  it('keeps the app inert until the last open drawer closes', async () => {
    const appRoot = document.createElement('div')
    appRoot.id = 'app'
    document.body.append(appRoot)
    const first = mount(InvestigationDrawer, { attachTo: appRoot, props: { ...props, error: '详情加载失败' }, global })
    const second = mount(InvestigationDrawer, { attachTo: appRoot, props: { ...props, error: '详情加载失败' }, global })
    await nextTick()

    expect(appRoot.hasAttribute('inert')).toBe(true)
    await first.setProps({ open: false })
    first.unmount()
    expect(appRoot.hasAttribute('inert')).toBe(true)

    await second.setProps({ open: false })
    expect(appRoot.hasAttribute('inert')).toBe(false)
    second.unmount()
  })
})
