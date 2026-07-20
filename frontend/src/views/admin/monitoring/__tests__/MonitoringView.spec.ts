import { flushPromises, mount } from '@vue/test-utils'
import { beforeEach, describe, expect, it, vi } from 'vitest'

const getOverview = vi.hoisted(() => vi.fn())
const getAccounts = vi.hoisted(() => vi.fn())
const getSettings = vi.hoisted(() => vi.fn())
const getInvestigation = vi.hoisted(() => vi.fn())

vi.mock('@/api/admin/monitoring', () => ({
  default: { getOverview, getAccounts, getSettings, getInvestigation, updateSettings: vi.fn() },
  performanceMetricsFromCounters: () => ({}),
  performanceMetricsFromTimePoint: () => ({})
}))

vi.mock('vue-i18n', async () => {
  const actual = await vi.importActual<typeof import('vue-i18n')>('vue-i18n')
  return { ...actual, useI18n: () => ({ t: (key: string) => key }) }
})

vi.mock('vue-router', async () => {
  const actual = await vi.importActual<typeof import('vue-router')>('vue-router')
  return { ...actual, useRoute: () => ({ query: {} }), useRouter: () => ({ replace: vi.fn() }) }
})

import MonitoringView from '../MonitoringView.vue'

const overviewResponse = {
  performance: {
    summary: {
      attempts: 1000,
      availability: { numerator: 990, denominator: 1000, rate: 0.99 },
      failure_rate: { numerator: 10, denominator: 1000, rate: 0.01 },
      ttft_timeout_count: 5,
      p50_ttft_ms: 800, p95_ttft_ms: 2000, p95_duration_ms: 9000
    },
    trend: [],
    collection_health: { status: 'complete', dropped_samples: 0, pending_samples: 0, last_successful_flush_at: null },
    coverage_start: '2026-07-19T00:00:00Z',
    coverage_end: '2026-07-20T00:00:00Z'
  },
  ttft: {
    summary: {
      controlled_requests: 500, client_canceled_requests: 0,
      attempt_ttft_timeout_rate: { numerator: 50, denominator: 500, rate: 0.1 },
      recovery_rate: { numerator: 40, denominator: 50, rate: 0.8 },
      final_ttft_failure_rate: { numerator: 10, denominator: 50, rate: 0.2 },
      other_final_failure_rate: { numerator: 0, denominator: 50, rate: 0 }
    },
    trend: [], other_failures: [],
    completeness: { status: 'complete', dropped_samples: 0, pending_samples: 0, last_successful_flush_at: null }
  }
}

describe('MonitoringView', () => {
  beforeEach(() => {
    getOverview.mockReset().mockResolvedValue(overviewResponse)
    getAccounts.mockReset().mockResolvedValue({ items: [], total: 0, page: 1, pages: 0 })
    getSettings.mockReset().mockResolvedValue({ saved: { enabled: true, timeout_seconds: 30 }, effective: { enabled: true, timeout_seconds: 30 }, loaded_at: '2026-07-20T00:00:00Z' })
  })

  it('loads overview, accounts and settings on mount', async () => {
    mount(MonitoringView, { global: { stubs: { AppLayout: { template: '<div><slot /></div>' }, teleport: true } } })
    await flushPromises()
    expect(getOverview).toHaveBeenCalled()
    expect(getAccounts).toHaveBeenCalled()
    expect(getSettings).toHaveBeenCalled()
  })

  it('renders the protection badge with effective timeout', async () => {
    const wrapper = mount(MonitoringView, { global: { stubs: { AppLayout: { template: '<div><slot /></div>' }, teleport: true } } })
    await flushPromises()
    expect(wrapper.get('[data-testid="protection-badge"]').exists()).toBe(true)
  })
})
