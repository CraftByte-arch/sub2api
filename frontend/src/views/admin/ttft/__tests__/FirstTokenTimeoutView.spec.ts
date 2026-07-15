import { flushPromises, mount } from '@vue/test-utils'
import { beforeEach, describe, expect, it, vi } from 'vitest'
import { nextTick, ref } from 'vue'
import { readFileSync } from 'node:fs'

const { getSettings, updateSettings, getOverview, getAccounts } = vi.hoisted(() => ({
  getSettings: vi.fn(), updateSettings: vi.fn(), getOverview: vi.fn(), getAccounts: vi.fn()
}))
const route = ref({ query: {} as Record<string, string> })
const replace = vi.fn(async ({ query }) => { route.value = { query } })

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

function mockSuccess() {
  getSettings.mockResolvedValue(settings)
  getOverview.mockResolvedValue(overview)
  getAccounts.mockResolvedValue(accounts)
}

async function mountView() {
  const wrapper = mount(FirstTokenTimeoutView, {
    global: {
      mocks: { $t: (key: string) => key },
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

  it('saves settings without clearing loaded statistics', async () => {
    updateSettings.mockResolvedValue({ ...settings, effective: { enabled: false, timeout_seconds: 20 } })
    const wrapper = await mountView()
    await wrapper.get('[data-testid="ttft-enabled"]').setValue(false)
    await wrapper.get('form').trigger('submit')
    await flushPromises()

    expect(updateSettings).toHaveBeenCalledWith({ enabled: false, timeout_seconds: 20 })
    expect(wrapper.text()).toContain('3 / 10')
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

  it('exposes the admin route, sidebar label, locale key, dark styles, and a narrow table scroll container', () => {
    const routerSource = readFileSync('src/router/index.ts', 'utf8')
    const sidebarSource = readFileSync('src/components/layout/AppSidebar.vue', 'utf8')
    const enLocale = readFileSync('src/i18n/locales/en/admin/ttft.ts', 'utf8')
    const tableSource = readFileSync('src/views/admin/ttft/components/TTFTAccountStatsTable.vue', 'utf8')

    expect(routerSource).toContain("path: '/admin/ttft'")
    expect(sidebarSource).toContain("t('nav.ttftMonitoring')")
    expect(enLocale).toContain('First Token Monitoring')
    expect(tableSource).toContain('overflow-x-auto')
    expect(tableSource).toContain('dark:')
  })
})
