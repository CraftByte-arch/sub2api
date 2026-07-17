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
const PerformanceAccountTableStub = {
  props: ['page', 'loading', 'error', 'sort', 'order'],
  emits: ['retry', 'sort', 'page', 'select'],
  template: '<button data-testid="select-account" @click="$emit(\'select\', page.items[0])">select</button><button data-testid="sort-samples" @click="$emit(\'sort\', \'samples\')">samples</button>'
}

function deferred<T>() {
  let resolve!: (value: T) => void
  const promise = new Promise<T>((resolvePromise) => { resolve = resolvePromise })
  return { promise, resolve }
}

function createView() {
  return mount(AccountPerformanceView, {
    global: {
      stubs: {
        AppLayout: { template: '<main><slot /></main>' },
        PerformanceMetricCard: { props: ['label', 'value', 'context'], template: '<div data-testid="metric-card">{{ label }} {{ value }} {{ context }}</div>' },
        PerformanceTrendChart: { props: ['points', 'series'], template: '<div data-testid="performance-trend-value">{{ points.length ? series[0].formatter(series[0].selector(points[0])) : "" }}</div>' },
        PerformanceFailureDistribution: { template: '<div />' },
        PerformanceAccountTable: PerformanceAccountTableStub,
        PerformanceInvestigationDrawer: { template: '<div />' }
      }
    }
  })
}

async function mountView() {
  const wrapper = createView()
  await flushPromises()
  return wrapper
}

describe('AccountPerformanceView', () => {
  beforeEach(() => {
    route.value = { query: {} }
    replace.mockReset()
    replace.mockImplementation(async ({ query }) => { route.value.query = query })
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

  it('formats canonical availability trend rates as percentages once', async () => {
    const highAvailabilityCounters = {
      ...counters,
      attempt_count: 100,
      success_count: 99,
      ttft_latency: { ...counters.ttft_latency, Samples: 100 },
      duration_latency: { ...counters.duration_latency, Samples: 100 }
    }
    getOverview.mockResolvedValueOnce({ ...overview(), trend: [{ bucket_start: '2026-07-17T00:00:00Z', counters: highAvailabilityCounters }] })
    const wrapper = await mountView()

    expect(wrapper.get('[data-testid="performance-trend-value"]').text()).toBe('99.00%')
    expect(wrapper.text()).not.toContain('9900.00%')
    wrapper.unmount()
  })

  it('uses total failovers across the selected range for the compact summary', async () => {
    getOverview.mockResolvedValueOnce({
      ...overview(20),
      trend: [
        { bucket_start: '2026-07-16T23:00:00Z', counters: { ...counters, failover_count: 2 } },
        { bucket_start: '2026-07-17T00:00:00Z', counters: { ...counters, failover_count: 0 } }
      ]
    })
    const wrapper = await mountView()

    expect(wrapper.text()).toContain('切换率 10.00%')
    wrapper.unmount()
  })

  it('shows an overview empty state instead of zero-valued metrics', async () => {
    getOverview.mockResolvedValueOnce({
      ...overview(0),
      summary: { ...overview().summary, attempts: 0, availability: { numerator: 0, denominator: 0, rate: 0 }, failure_rate: { numerator: 0, denominator: 0, rate: 0 }, ttft_timeout_count: 0 },
      trend: []
    })
    const wrapper = await mountView()

    expect(wrapper.findAll('[data-testid="metric-card"]')).toHaveLength(0)
    expect(wrapper.text()).toContain('性能样本会在部署完成并处理请求后逐步累积。')
    wrapper.unmount()
  })

  it('keeps degraded collector health visible when no samples have accumulated', async () => {
    getOverview.mockResolvedValueOnce({
      ...overview(0),
      summary: { ...overview().summary, attempts: 0, availability: { numerator: 0, denominator: 0, rate: 0 }, failure_rate: { numerator: 0, denominator: 0, rate: 0 }, ttft_timeout_count: 0 },
      trend: [],
      collection_health: { status: 'degraded', dropped_samples: 3, pending_samples: 2, last_successful_flush_at: null }
    })
    const wrapper = await mountView()

    expect(wrapper.findAll('[data-testid="metric-card"]')).toHaveLength(0)
    expect(wrapper.text()).toContain('采集健康度')
    expect(wrapper.text()).toContain('丢弃样本3')
    expect(wrapper.text()).toContain('待写入样本2')
    expect(wrapper.text()).toContain('暂无成功刷新记录')
    wrapper.unmount()
  })

  it('does not render a stale overview while a filter navigation is pending', async () => {
    const initialOverview = deferred<ReturnType<typeof overview>>()
    const initialAccounts = deferred<typeof accounts>()
    const filterNavigation = deferred<void>()
    getOverview.mockReset()
    getAccounts.mockReset()
    getOverview.mockReturnValueOnce(initialOverview.promise)
    getAccounts.mockReturnValueOnce(initialAccounts.promise)
    replace.mockImplementation(({ query }) => {
      if (query.range === '24h') {
        route.value.query = query
        return Promise.resolve()
      }
      return filterNavigation.promise.then(() => { route.value.query = query })
    })
    const wrapper = createView()
    await flushPromises()

    await wrapper.findAll('[aria-label="时间范围"] button').find((button) => button.text() === '7d')!.trigger('click')
    initialOverview.resolve(overview(10))
    initialAccounts.resolve(accounts)
    await flushPromises()

    expect(wrapper.text()).not.toContain('10 次请求')
    wrapper.unmount()
  })

  it('reloads once when an external route update changes page filters', async () => {
    const wrapper = await mountView()
    getOverview.mockClear()
    getAccounts.mockClear()

    route.value.query = { range: '7d', platform: 'openai' }
    await flushPromises()

    expect(getOverview).toHaveBeenCalledTimes(1)
    expect(getOverview).toHaveBeenCalledWith({ range: '7d', platform: 'openai' })
    expect(getAccounts).toHaveBeenCalledTimes(1)
    expect(getAccounts).toHaveBeenCalledWith(expect.objectContaining({ range: '7d', platform: 'openai' }))
    wrapper.unmount()
  })

  it('does not consume browser back navigation after a duplicate internal replace', async () => {
    route.value = { query: { range: '24h' } }
    replace.mockImplementation(async ({ query }) => {
      if (JSON.stringify(route.value.query) !== JSON.stringify(query)) route.value.query = query
    })
    const wrapper = await mountView()
    const rangeButtons = wrapper.findAll('[aria-label="时间范围"] button')

    await rangeButtons.find((button) => button.text() === '7d')!.trigger('click')
    await flushPromises()
    getOverview.mockClear()
    getAccounts.mockClear()

    route.value.query = { range: '24h' }
    await flushPromises()

    expect(getOverview).toHaveBeenCalledTimes(1)
    expect(getOverview).toHaveBeenCalledWith({ range: '24h', platform: undefined })
    expect(getAccounts).toHaveBeenCalledTimes(1)
    expect(getAccounts).toHaveBeenCalledWith(expect.objectContaining({ range: '24h', platform: undefined }))
    expect(rangeButtons.find((button) => button.text() === '24h')!.classes()).toContain('bg-primary-600')
    wrapper.unmount()
  })

  it('restores current route filters when router navigation rejects', async () => {
    const wrapper = await mountView()
    const rangeButtons = wrapper.findAll('[aria-label="时间范围"] button')
    getOverview.mockClear()
    getAccounts.mockClear()
    replace.mockRejectedValueOnce(new Error('navigation failed'))

    await rangeButtons.find((button) => button.text() === '7d')!.trigger('click')
    await flushPromises()

    expect(rangeButtons.find((button) => button.text() === '24h')!.classes()).toContain('bg-primary-600')
    expect(getOverview).not.toHaveBeenCalledWith({ range: '7d', platform: undefined })
    expect(getAccounts).not.toHaveBeenCalledWith(expect.objectContaining({ range: '7d' }))
    wrapper.unmount()
  })

  it('restores current route filters when router navigation resolves without updating the route', async () => {
    const wrapper = await mountView()
    const rangeButtons = wrapper.findAll('[aria-label="时间范围"] button')
    getOverview.mockClear()
    getAccounts.mockClear()
    replace.mockResolvedValueOnce(undefined)

    await rangeButtons.find((button) => button.text() === '7d')!.trigger('click')
    await flushPromises()

    expect(rangeButtons.find((button) => button.text() === '24h')!.classes()).toContain('bg-primary-600')
    expect(getOverview).not.toHaveBeenCalledWith({ range: '7d', platform: undefined })
    expect(getAccounts).not.toHaveBeenCalledWith(expect.objectContaining({ range: '7d' }))
    wrapper.unmount()
  })

  it('opens account investigation using the active page filters', async () => {
    const wrapper = await mountView()
    await wrapper.get('[data-testid="select-account"]').trigger('click')
    await flushPromises()

    expect(getInvestigation).toHaveBeenCalledWith({ range: '24h', platform: undefined, account_id: 42 })
    wrapper.unmount()
  })

  it('uses the supported samples key when the table requests attempt sorting', async () => {
    const wrapper = await mountView()
    getAccounts.mockClear()

    await wrapper.get('[data-testid="sort-samples"]').trigger('click')
    await flushPromises()

    expect(getAccounts).toHaveBeenCalledWith(expect.objectContaining({ sort: 'samples', order: 'asc' }))
    wrapper.unmount()
  })

  it('identifies the last successful collector flush and its missing-data fallback when degraded', async () => {
    getOverview.mockResolvedValueOnce({
      ...overview(),
      collection_health: { status: 'degraded', dropped_samples: 3, pending_samples: 2, last_successful_flush_at: '2026-07-17T00:00:00Z' }
    })
    const withFlush = await mountView()

    expect(withFlush.text()).toContain('最近一次成功刷新')
    expect(withFlush.text()).not.toContain('2026-07-17T00:00:00Z')
    withFlush.unmount()

    getOverview.mockResolvedValueOnce({
      ...overview(),
      collection_health: { status: 'degraded', dropped_samples: 3, pending_samples: 2, last_successful_flush_at: null }
    })
    const withoutFlush = await mountView()

    expect(withoutFlush.text()).toContain('暂无成功刷新记录')
    withoutFlush.unmount()
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
        PerformanceAccountTable: { props: ['page', 'loading', 'error', 'sort', 'order'], emits: ['retry', 'sort', 'page', 'select'], template: '<div />' }, PerformanceInvestigationDrawer: { template: '<div />' }
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

  it('loads final filters once when overlapping route replacements resolve out of order', async () => {
    const wrapper = await mountView()
    getOverview.mockClear()
    getAccounts.mockClear()
    const pending: Array<{ query: Record<string, string>; resolve: () => void }> = []
    replace.mockImplementation(({ query }) => {
      const navigation = deferred<void>()
      pending.push({
        query,
        resolve: () => {
          route.value.query = query
          navigation.resolve()
        }
      })
      return navigation.promise
    })

    const rangeButtons = wrapper.findAll('[aria-label="时间范围"] button')
    await rangeButtons.find((button) => button.text() === '7d')!.trigger('click')
    await rangeButtons.find((button) => button.text() === '30d')!.trigger('click')

    expect(pending.map((navigation) => navigation.query.range)).toEqual(['7d', '30d'])
    pending[1].resolve()
    await flushPromises()
    pending[0].resolve()
    await flushPromises()
    expect(pending.map((navigation) => navigation.query.range)).toEqual(['7d', '30d', '30d'])
    pending[2].resolve()
    await flushPromises()

    expect(getOverview).toHaveBeenCalledTimes(1)
    expect(getOverview).toHaveBeenCalledWith({ range: '30d', platform: undefined })
    expect(getAccounts).toHaveBeenCalledTimes(1)
    expect(getAccounts).toHaveBeenCalledWith(expect.objectContaining({ range: '30d' }))
    wrapper.unmount()
  })
})
