import { flushPromises, mount } from '@vue/test-utils'
import { beforeEach, describe, expect, it, vi } from 'vitest'
import { ref } from 'vue'

const { getOverview, getAccounts, getInvestigation } = vi.hoisted(() => ({
  getOverview: vi.fn(),
  getAccounts: vi.fn(),
  getInvestigation: vi.fn()
}))
const route = ref({ query: {} as Record<string, string> })
const replace = vi.fn(async ({ query }) => { route.value.query = query })

vi.mock('vue-router', () => ({
  useRoute: () => route.value,
  useRouter: () => ({ replace })
}))

vi.mock('@/api/admin/performance', async () => {
  const actual = await vi.importActual<typeof import('@/api/admin/performance')>('@/api/admin/performance')
  return { ...actual, default: { getOverview, getAccounts, getInvestigation } }
})

import AccountPerformanceView from '../AccountPerformanceView.vue'

const counters = {
  attempt_count: 10, success_count: 9, client_canceled_count: 0, ttft_timeout_count: 1,
  rate_limit_count: 0, auth_count: 0, upstream_4xx_count: 0, upstream_5xx_count: 0,
  transport_count: 0, protocol_count: 0, other_failure_count: 0, failover_count: 1,
  ttft_sum_ms: 5000, duration_sum_ms: 10000,
  ttft_latency: { Samples: 10, LE1000MS: 5, LE2500MS: 9, LE5000MS: 10, LE10000MS: 10, LE30000MS: 10, GT30000MS: 0 },
  duration_latency: { Samples: 10, LE1000MS: 1, LE2500MS: 5, LE5000MS: 9, LE10000MS: 10, LE30000MS: 10, GT30000MS: 0 }
}

function overview(attempts = 10) {
  return {
    summary: { attempts, availability: { numerator: 9, denominator: 10, rate: 0.9 }, failure_rate: { numerator: 1, denominator: 10, rate: 0.1 }, p50_ttft_ms: 1000, p95_ttft_ms: 2500, p95_duration_ms: 10000, ttft_timeout_count: 1 },
    trend: [{ bucket_start: '2026-07-17T00:00:00Z', counters }],
    collection_health: { status: 'complete', dropped_samples: 0, pending_samples: 0, last_successful_flush_at: '2026-07-17T00:00:00Z' },
    coverage_start: '2026-07-16T00:00:00Z', coverage_end: '2026-07-17T00:00:00Z'
  }
}

const accounts = {
  items: [{ account_id: 42, platform: 'openai', counters, availability: 0.9, failure_rate: 0.1, health_score: 0.9, p95_ttft_ms: 2500, p95_duration_ms: 10000, low_sample: false }],
  total: 1, page: 1, page_size: 20, pages: 1,
  collection_health: { status: 'complete', dropped_samples: 0, pending_samples: 0, last_successful_flush_at: '2026-07-17T00:00:00Z' }
}

const investigation = { time_points: [], failures: [], collection_health: accounts.collection_health }

function deferred<T>() {
  let resolve!: (value: T) => void
  const promise = new Promise<T>((resolvePromise) => { resolve = resolvePromise })
  return { promise, resolve }
}

async function mountView() {
  const wrapper = mount(AccountPerformanceView, {
    global: {
      stubs: {
        AppLayout: { template: '<main><slot /></main>' },
        PerformanceMetricCard: { props: ['label', 'value', 'context'], template: '<div>{{ label }} {{ value }} {{ context }}</div>' },
        PerformanceTrendChart: { template: '<div />' },
        PerformanceFailureDistribution: { template: '<div />' },
        PerformanceAccountTable: { props: ['page'], template: '<button data-testid="select-account" @click="$emit(\'select\', page.items[0])">select</button>' },
        PerformanceInvestigationDrawer: { template: '<div />' }
      }
    }
  })
  await flushPromises()
  return wrapper
}

describe('AccountPerformanceView', () => {
  beforeEach(() => {
    route.value = { query: {} }
    replace.mockClear()
    getOverview.mockReset()
    getAccounts.mockReset()
    getInvestigation.mockReset()
    getOverview.mockResolvedValue(overview())
    getAccounts.mockResolvedValue(accounts)
    getInvestigation.mockResolvedValue(investigation)
  })

  it('loads the default overview and first health-ranked account page', async () => {
    const wrapper = await mountView()

    expect(getOverview).toHaveBeenCalledWith({ range: '24h', platform: undefined })
    expect(getAccounts).toHaveBeenCalledWith({ range: '24h', platform: undefined, sort: 'health_score', order: 'asc', page: 1, page_size: 20 })
    wrapper.unmount()
  })

  it('opens account investigation using the active page filters', async () => {
    const wrapper = await mountView()
    await wrapper.get('[data-testid="select-account"]').trigger('click')
    await flushPromises()

    expect(getInvestigation).toHaveBeenCalledWith({ range: '24h', platform: undefined, account_id: 42 })
    wrapper.unmount()
  })

  it('does not allow a stale overview response to replace an explicit refresh', async () => {
    const initial = deferred<ReturnType<typeof overview>>()
    const refreshed = deferred<ReturnType<typeof overview>>()
    getOverview.mockReset()
    getOverview.mockReturnValueOnce(initial.promise).mockReturnValueOnce(refreshed.promise)
    const wrapper = mount(AccountPerformanceView, {
      global: { stubs: {
        AppLayout: { template: '<main><slot /></main>' },
        PerformanceMetricCard: { props: ['label', 'value'], template: '<div>{{ label }} {{ value }}</div>' },
        PerformanceTrendChart: { template: '<div />' }, PerformanceFailureDistribution: { template: '<div />' },
        PerformanceAccountTable: { template: '<div />' }, PerformanceInvestigationDrawer: { template: '<div />' }
      } }
    })
    await flushPromises()
    await wrapper.get('[aria-label="刷新性能数据"]').trigger('click')
    refreshed.resolve(overview(20))
    await flushPromises()
    initial.resolve(overview(10))
    await flushPromises()

    expect(wrapper.text()).toContain('20')
    expect(wrapper.text()).not.toContain('10 次请求')
    wrapper.unmount()
  })
})
