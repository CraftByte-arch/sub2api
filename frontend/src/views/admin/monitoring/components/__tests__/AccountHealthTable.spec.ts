import { mount } from '@vue/test-utils'
import { describe, expect, it, vi } from 'vitest'
import AccountHealthTable from '../AccountHealthTable.vue'

vi.mock('vue-i18n', async () => {
  const actual = await vi.importActual<typeof import('vue-i18n')>('vue-i18n')
  return { ...actual, useI18n: () => ({ t: (key: string) => key }) }
})

const counters = {
  attempt_count: 100, success_count: 95, client_canceled_count: 0, ttft_timeout_count: 4,
  rate_limit_count: 1, auth_count: 0, upstream_4xx_count: 0, upstream_5xx_count: 0,
  transport_count: 0, protocol_count: 0, other_failure_count: 0, failover_count: 0,
  ttft_sum_ms: 0, duration_sum_ms: 0,
  ttft_latency: { Samples: 0, LE1000MS: 0, LE2500MS: 0, LE5000MS: 0, LE10000MS: 0, LE30000MS: 0, GT30000MS: 0 },
  duration_latency: { Samples: 0, LE1000MS: 0, LE2500MS: 0, LE5000MS: 0, LE10000MS: 0, LE30000MS: 0, GT30000MS: 0 }
}

const page = {
  items: [{ account_id: 7, account_name: 'prod-1', account_type: '', platform: 'openai', counters, availability: 0.95, failure_rate: 0.05, health_score: 0.95, low_sample: false, p95_ttft_ms: 1200, p95_duration_ms: 8000 }],
  total: 1, page: 1, page_size: 20, pages: 1,
  collection_health: { status: 'complete', dropped_samples: 0, pending_samples: 0, last_successful_flush_at: null }
}

describe('AccountHealthTable', () => {
  it('renders the derived ttft timeout rate column', () => {
    const wrapper = mount(AccountHealthTable, { props: { page, loading: false, error: '', sort: 'health_score', order: 'asc', search: '' } })
    expect(wrapper.text()).toContain('4.00%') // 4 / 100
  })

  it('emits select with the account on row click', async () => {
    const wrapper = mount(AccountHealthTable, { props: { page, loading: false, error: '', sort: 'health_score', order: 'asc', search: '' } })
    await wrapper.get('tbody tr').trigger('click')
    expect(wrapper.emitted('select')?.[0]?.[0]).toMatchObject({ account_id: 7 })
  })

  it('forwards search input as update:search', async () => {
    const wrapper = mount(AccountHealthTable, { props: { page, loading: false, error: '', sort: 'health_score', order: 'asc', search: '' } })
    await wrapper.get('[data-testid="account-search"] input').setValue('prod')
    expect(wrapper.emitted('update:search')?.at(-1)?.[0]).toBe('prod')
  })
})
