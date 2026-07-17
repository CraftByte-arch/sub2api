import { mount } from '@vue/test-utils'
import { describe, expect, it } from 'vitest'
import PerformanceAccountTable from '../PerformanceAccountTable.vue'

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

const page = {
  items: [{
    account_id: 42,
    platform: 'openai',
    counters,
    availability: 0.95,
    failure_rate: 0.05,
    health_score: 0.92,
    p95_ttft_ms: 2500,
    p95_duration_ms: 10000,
    low_sample: false
  }],
  total: 1,
  page: 1,
  page_size: 20,
  pages: 1,
  collection_health: { status: 'complete', dropped_samples: 0, pending_samples: 0, last_successful_flush_at: null }
}

const props = { page, loading: false, error: '', sort: 'health_score', order: 'asc' as const }

describe('PerformanceAccountTable', () => {
  it('selects an account when its row is activated by keyboard', async () => {
    const wrapper = mount(PerformanceAccountTable, { props })
    const row = wrapper.get('[data-testid="performance-account-42"]')

    await row.trigger('keydown.enter')
    await row.trigger('keydown.space')

    expect(wrapper.emitted('select')).toEqual([[page.items[0]], [page.items[0]]])
  })

  it('keeps loaded rows visible and offers retry after a refresh error', () => {
    const wrapper = mount(PerformanceAccountTable, { props: { ...props, error: '账号性能刷新失败' } })

    expect(wrapper.text()).toContain('#42')
    expect(wrapper.text()).toContain('账号性能刷新失败')
    expect(wrapper.get('[data-testid="performance-accounts-retry"]').text()).toContain('重试')
  })
})
