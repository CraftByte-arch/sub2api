import { flushPromises, mount } from '@vue/test-utils'
import { beforeEach, describe, expect, it, vi } from 'vitest'
import { nextTick, ref } from 'vue'

const { getSettings, updateSettings, getOverview, getAccounts } = vi.hoisted(() => ({
  getSettings: vi.fn(), updateSettings: vi.fn(), getOverview: vi.fn(), getAccounts: vi.fn()
}))
const route = ref({ query: {} as Record<string, string> })
const replace = vi.fn(async ({ query }) => { route.value.query = query })

vi.mock('vue-router', () => ({
  useRoute: () => route.value,
  useRouter: () => ({ replace })
}))

vi.mock('@/api/admin/ttft', () => ({
  default: { getSettings, updateSettings, getOverview, getAccounts }
}))

vi.mock('vue-i18n', async () => {
  const actual = await vi.importActual<typeof import('vue-i18n')>('vue-i18n')
  return { ...actual, useI18n: () => ({ t: (key: string) => key }) }
})

import FirstTokenTimeoutView from '../FirstTokenTimeoutView.vue'

const settings = {
  saved: { enabled: true, timeout_seconds: 20 },
  effective: { enabled: true, timeout_seconds: 20 },
  loaded_at: '2026-07-15T00:00:00Z'
}

const overview = {
  summary: {
    controlled_requests: 12,
    client_canceled_requests: 2,
    attempt_ttft_timeout_rate: { numerator: 3, denominator: 10, rate: 0.3 },
    recovery_rate: { numerator: 2, denominator: 3, rate: 2 / 3 },
    final_ttft_failure_rate: { numerator: 1, denominator: 12, rate: 1 / 12 },
    other_final_failure_rate: { numerator: 1, denominator: 12, rate: 1 / 12 }
  },
  trend: [],
  other_failures: [],
  completeness: { status: 'degraded', dropped_samples: 2, last_successful_flush_at: '2026-07-15T00:00:00Z', pending_samples: 1 }
}

const accounts = { items: [], total: 0, page: 1, page_size: 20, pages: 0 }

function deferred<T>() {
  let resolve!: (value: T) => void
  let reject!: (reason?: unknown) => void
  const promise = new Promise<T>((resolvePromise, rejectPromise) => { resolve = resolvePromise; reject = rejectPromise })
  return { promise, resolve, reject }
}

function mockSuccess() {
  getSettings.mockResolvedValue(settings)
  getOverview.mockResolvedValue(overview)
  getAccounts.mockResolvedValue(accounts)
}

async function mountView() {
  const wrapper = mount(FirstTokenTimeoutView, {
    global: {
      mocks: { $t: (key: string, values?: Record<string, unknown>) => values ? `${key} ${Object.values(values).join(' ')}` : key },
      stubs: {
        AppLayout: { template: '<main><slot /></main>' },
        TTFTFailureTrendChart: { template: '<div />' },
        TTFTFailureDistributionChart: { template: '<div />' }
      }
    }
  })
  await flushPromises()
  return wrapper
}

describe('FirstTokenTimeoutView', () => {
  beforeEach(() => {
    route.value = { query: {} }
    replace.mockClear()
    getSettings.mockReset()
    updateSettings.mockReset()
    getOverview.mockReset()
    getAccounts.mockReset()
    mockSuccess()
  })

  it('defaults range to 24h and renders every rate numerator and denominator', async () => {
    const wrapper = await mountView()

    expect(getOverview).toHaveBeenCalledWith({ range: '24h', protocol: undefined, model: undefined })
    expect(wrapper.text()).toContain('3 / 10')
    expect(wrapper.text()).toContain('1 / 12')
    expect(wrapper.find('[data-testid="ttft-skeleton"]').exists()).toBe(false)
  })

  it('restores and synchronizes only global filters from the URL', async () => {
    route.value = { query: { range: '7d', protocol: 'responses', model: 'gpt-5' } }
    await mountView()

    expect(getOverview).toHaveBeenCalledWith({ range: '7d', protocol: 'responses', model: 'gpt-5' })
    expect(getAccounts).toHaveBeenCalledWith(expect.objectContaining({ range: '7d', protocol: 'responses', model: 'gpt-5' }))
    expect(replace).toHaveBeenCalledWith({ query: { range: '7d', protocol: 'responses', model: 'gpt-5' } })
  })

  it('applies an external route query change without a second URL replacement', async () => {
    const wrapper = await mountView()
    getOverview.mockClear()
    getAccounts.mockClear()
    replace.mockClear()

    route.value.query = { range: '30d', protocol: 'anthropic_messages', model: 'claude' }
    await nextTick()
    await flushPromises()

    expect(getOverview).toHaveBeenCalledWith({ range: '30d', protocol: 'anthropic_messages', model: 'claude' })
    expect(getAccounts).toHaveBeenCalledWith(expect.objectContaining({ range: '30d', protocol: 'anthropic_messages', model: 'claude' }))
    expect(replace).not.toHaveBeenCalled()
    wrapper.unmount()
  })

  it('refreshes each data region once for a local range change', async () => {
    const wrapper = await mountView()
    getOverview.mockClear()
    getAccounts.mockClear()
    replace.mockClear()
    const rangeButton = wrapper.findAll('button').find((button) => button.text() === '7d')

    await rangeButton!.trigger('click')
    await flushPromises()

    expect(replace).toHaveBeenCalledTimes(1)
    expect(getOverview).toHaveBeenCalledTimes(1)
    expect(getAccounts).toHaveBeenCalledTimes(1)
  })

  it('saves settings without clearing loaded statistics', async () => {
    updateSettings.mockResolvedValue({ ...settings, effective: { enabled: false, timeout_seconds: 20 } })
    const wrapper = await mountView()
    await wrapper.get('[data-testid="ttft-enabled"]').setValue(false)
    await wrapper.get('form').trigger('submit')
    await flushPromises()

    expect(updateSettings).toHaveBeenCalledWith({ enabled: false, timeout_seconds: 20 })
    expect(wrapper.text()).toContain('3 / 10')
  })

  it('rejects fractional timeout settings locally without sending a PUT', async () => {
    const wrapper = await mountView()
    const timeoutInput = wrapper.findAll('input[type="number"]')[0]
    await timeoutInput.setValue(1.5)
    await wrapper.get('form').trigger('submit')
    await flushPromises()

    expect(wrapper.text()).toContain('admin.ttft.settings.timeoutError')
    expect(updateSettings).not.toHaveBeenCalled()
  })

  it('keeps overview requests independent from account-local filters', async () => {
    const wrapper = await mountView()
    getOverview.mockClear()
    getAccounts.mockClear()
    await wrapper.get('[data-testid="ttft-account-search"]').setValue('alpha')
    await new Promise((resolve) => setTimeout(resolve, 320))
    await nextTick()

    expect(getOverview).not.toHaveBeenCalled()
    expect(getAccounts).toHaveBeenCalledWith(expect.objectContaining({ search: 'alpha' }))
  })

  it('cancels a pending account search debounce when the page unmounts', async () => {
    const wrapper = await mountView()
    getAccounts.mockClear()
    await wrapper.get('[data-testid="ttft-account-search"]').setValue('alpha')
    wrapper.unmount()
    await new Promise((resolve) => setTimeout(resolve, 320))

    expect(getAccounts).not.toHaveBeenCalled()
  })

  it('ignores an older overview response that settles after a newer global filter response', async () => {
    const oldOverview = deferred<typeof overview>()
    const newerOverview = { ...overview, summary: { ...overview.summary, controlled_requests: 99 } }
    getOverview.mockReset()
    getOverview.mockImplementationOnce(() => oldOverview.promise).mockResolvedValueOnce(newerOverview)
    const wrapper = await mountView()
    const rangeButton = wrapper.findAll('button').find((button) => button.text() === '7d')
    expect(rangeButton).toBeDefined()

    await rangeButton!.trigger('click')
    await flushPromises()
    oldOverview.resolve(overview)
    await flushPromises()

    expect(wrapper.findAll('article')[0].text()).toContain('99')
    expect(wrapper.findAll('article')[0].text()).not.toContain('12')
  })

  it('clears account_id from account requests without reloading the overview', async () => {
    const wrapper = await mountView()
    const accountIDInput = wrapper.findAll('input[type="number"]')[1]
    getOverview.mockClear()
    getAccounts.mockClear()

    await accountIDInput.setValue(42)
    await flushPromises()
    expect(getAccounts).toHaveBeenLastCalledWith(expect.objectContaining({ account_id: 42 }))

    getAccounts.mockClear()
    await accountIDInput.setValue('')
    await flushPromises()

    expect(getOverview).not.toHaveBeenCalled()
    expect(getAccounts).toHaveBeenLastCalledWith(expect.objectContaining({ account_id: undefined }))
  })

  it('shows a nearby error and skips account requests for a non-positive account ID', async () => {
    const wrapper = await mountView()
    const accountIDInput = wrapper.findAll('input[type="number"]')[1]
    getOverview.mockClear()
    getAccounts.mockClear()

    await accountIDInput.setValue(-1)
    await flushPromises()

    expect(wrapper.text()).toContain('admin.ttft.accounts.accountIdError')
    expect(getOverview).not.toHaveBeenCalled()
    expect(getAccounts).not.toHaveBeenCalled()
  })

  it('keeps settings visible for empty, degraded, and retriable overview states', async () => {
    getOverview.mockRejectedValueOnce(new Error('network'))
    const wrapper = await mountView()

    expect(wrapper.text()).toContain('admin.ttft.errors.overview')
    expect(wrapper.get('[data-testid="ttft-overview-retry"]').exists()).toBe(true)
    expect(wrapper.text()).toContain('admin.ttft.settings.title')
    await wrapper.get('[data-testid="ttft-overview-retry"]').trigger('click')
    await flushPromises()
    expect(wrapper.text()).toContain('admin.ttft.completeness.degraded')
  })

  it('keeps loaded overview metrics visible when a later refresh fails', async () => {
    const wrapper = await mountView()
    getOverview.mockRejectedValueOnce(new Error('network'))
    const refreshButton = wrapper.findAll('button').find((button) => button.text() === 'common.refresh')
    expect(refreshButton).toBeDefined()

    await refreshButton!.trigger('click')
    await flushPromises()

    expect(wrapper.findAll('article')[0].text()).toContain('12')
    expect(wrapper.text()).toContain('admin.ttft.errors.overview')
    expect(wrapper.get('[data-testid="ttft-overview-retry"]').exists()).toBe(true)
  })

  it('renders the last successful statistics flush in the degraded warning', async () => {
    const wrapper = await mountView()

    expect(wrapper.text()).toContain('admin.ttft.completeness.lastSuccessfulFlush')
    expect(wrapper.text()).toContain('2026')
  })

  it('states when a degraded recorder has never completed a successful flush', async () => {
    getOverview.mockResolvedValueOnce({ ...overview, completeness: { ...overview.completeness, last_successful_flush_at: null } })
    const wrapper = await mountView()

    expect(wrapper.text()).toContain('admin.ttft.completeness.noSuccessfulFlush')
  })

  it('renders a dark-mode narrow table container and keeps sorting operable', async () => {
    document.documentElement.classList.add('dark')
    getAccounts.mockResolvedValueOnce({
      ...accounts,
      total: 1,
      items: [{ account_id: 42, account_name: 'production', platform: 'openai', samples: 20, success_count: 17, ttft_timeout_count: 2, ttft_timeout_rate: { numerator: 2, denominator: 20, rate: 0.1 }, other_failure_count: 1, other_failure_rate: { numerator: 1, denominator: 20, rate: 0.05 }, avg_ttft_ms: 123, low_sample: false }]
    })
    const wrapper = await mountView()
    const table = wrapper.get('.ttft-account-table')
    wrapper.element.style.width = '375px'
    getAccounts.mockClear()

    expect(document.documentElement.classList.contains('dark')).toBe(true)
    expect(table.classes()).toContain('overflow-x-auto')
    expect(table.text()).toContain('production')
    await wrapper.get('th[aria-sort="descending"] button').trigger('click')
    await flushPromises()
    expect(getAccounts).toHaveBeenCalledWith(expect.objectContaining({ sort: 'samples', order: 'asc' }))

    document.documentElement.classList.remove('dark')
  })
})
