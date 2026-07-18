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
    account_name: 'Codex Team',
    account_type: 'oauth',
    auth_mode: 'personalAccessToken',
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
const global = {
  stubs: {
    PlatformTypeBadge: {
      props: ['platform', 'type', 'authMode'],
      template: '<span data-testid="platform-type-badge" :data-platform="platform" :data-type="type" :data-auth-mode="authMode" />'
    },
    PlatformIcon: true,
    Icon: true
  }
}

function mountTable(overrides = {}) {
  return mount(PerformanceAccountTable, { props: { ...props, ...overrides }, global })
}

describe('PerformanceAccountTable', () => {
  it('selects through a native account button while preserving row semantics', async () => {
    const wrapper = mountTable()
    const accountButton = wrapper.get('[data-testid="performance-account-42"]')
    const row = accountButton.element.closest('tr')!

    await accountButton.trigger('click')

    expect(accountButton.element.tagName).toBe('BUTTON')
    expect(row.getAttribute('role')).toBeNull()
    expect(row.getAttribute('tabindex')).toBeNull()
    expect(wrapper.emitted('select')).toEqual([[page.items[0]]])
  })

  it('shows account identity, reuses the platform badge, and splits successful and failed calls', () => {
    const wrapper = mountTable()

    expect(wrapper.text()).toContain('Codex Team')
    expect(wrapper.text()).toContain('#42')
    expect(wrapper.get('[data-testid="platform-type-badge"]').attributes()).toMatchObject({
      'data-platform': 'openai',
      'data-type': 'oauth',
      'data-auth-mode': 'personalAccessToken'
    })
    expect(wrapper.get('[data-testid="performance-success-count-42"]').text()).toBe('114')
    expect(wrapper.get('[data-testid="performance-failure-count-42"]').text()).toBe('6')
  })

  it('excludes client cancellations and clamps negative failed calls to zero', () => {
    const withCancellations = {
      ...page,
      items: [{
        ...page.items[0],
        counters: { ...page.items[0].counters, attempt_count: 120, success_count: 90, client_canceled_count: 20 }
      }]
    }
    const cancelledWrapper = mountTable({ page: withCancellations })
    expect(cancelledWrapper.get('[data-testid="performance-failure-count-42"]').text()).toBe('10')

    const inconsistent = {
      ...page,
      items: [{
        ...page.items[0],
        counters: { ...page.items[0].counters, attempt_count: 2, success_count: 4, client_canceled_count: 0 }
      }]
    }
    const inconsistentWrapper = mountTable({ page: inconsistent })
    expect(inconsistentWrapper.get('[data-testid="performance-failure-count-42"]').text()).toBe('0')
  })

  it('keeps loaded rows visible and retries after a refresh error', async () => {
    const wrapper = mountTable({ error: '账号性能刷新失败' })

    expect(wrapper.text()).toContain('#42')
    expect(wrapper.text()).toContain('账号性能刷新失败')
    const retry = wrapper.get('[data-testid="performance-accounts-retry"]')
    expect(retry.text()).toContain('重试')

    await retry.trigger('click')

    expect(wrapper.emitted('retry')).toHaveLength(1)
  })

  it('exposes successful and failed call sorting keys', async () => {
    const wrapper = mountTable()
    const buttons = wrapper.findAll('th button')

    expect(wrapper.findAll('th')[0].find('button').exists()).toBe(false)
    expect(wrapper.findAll('th')[1].find('button').exists()).toBe(false)
    const success = buttons.find((button) => button.text().includes('成功调用'))
    const failure = buttons.find((button) => button.text().includes('失败调用'))
    expect(success).toBeDefined()
    expect(failure).toBeDefined()
    await success!.trigger('click')
    await failure!.trigger('click')

    expect(wrapper.emitted('sort')).toEqual([['success_count'], ['failure_count']])
  })
})
