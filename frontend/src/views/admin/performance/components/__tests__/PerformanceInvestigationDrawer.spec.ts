import { mount } from '@vue/test-utils'
import { nextTick } from 'vue'
import { afterEach, describe, expect, it } from 'vitest'
import PerformanceInvestigationDrawer from '../PerformanceInvestigationDrawer.vue'

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

const account = { account_id: 42, platform: 'openai', counters, availability: 0.95, failure_rate: 0.05, health_score: 0.92, p95_ttft_ms: 2500, p95_duration_ms: 10000, low_sample: false }
const props = { open: true, account, investigation: null, loading: false, error: '' }
const global = { stubs: { PerformanceMetricCard: true, PerformanceTrendChart: true, PerformanceFailureDistribution: true } }

afterEach(() => { document.body.innerHTML = '' })

describe('PerformanceInvestigationDrawer', () => {
  it('shows an open dialog error state, retries, and closes from its close control', async () => {
    const wrapper = mount(PerformanceInvestigationDrawer, { attachTo: document.body, props: { ...props, error: '详情加载失败' }, global })
    await nextTick()

    const dialog = document.body.querySelector('[role="dialog"]')
    expect(dialog).not.toBeNull()
    expect(dialog?.textContent).toContain('详情加载失败')
    document.body.querySelector<HTMLButtonElement>('[data-testid="performance-investigation-retry"]')?.click()
    document.body.querySelector<HTMLButtonElement>('[data-testid="performance-investigation-close"]')?.click()
    await nextTick()

    expect(wrapper.emitted('retry')).toHaveLength(1)
    expect(wrapper.emitted('close')).toHaveLength(1)
    wrapper.unmount()
  })

  it('distinguishes a loading investigation from an empty investigation', async () => {
    const wrapper = mount(PerformanceInvestigationDrawer, { attachTo: document.body, props: { ...props, loading: true }, global })
    await nextTick()
    expect(document.body.textContent).toContain('正在加载账号详情')

    await wrapper.setProps({ loading: false })
    expect(document.body.textContent).toContain('暂无可供分析的性能数据')
    wrapper.unmount()
  })
})
