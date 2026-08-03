import { beforeEach, describe, expect, it, vi } from 'vitest'
import { flushPromises, mount } from '@vue/test-utils'

import BalanceOverviewView from '../BalanceOverviewView.vue'

const { getBalanceOverview } = vi.hoisted(() => ({
  getBalanceOverview: vi.fn()
}))

vi.mock('@/api/admin/balanceOverview', () => ({
  getBalanceOverview
}))

vi.mock('@/stores/app', () => ({
  useAppStore: () => ({
    showError: vi.fn()
  })
}))

vi.mock('vue-i18n', async () => {
  const actual = await vi.importActual<typeof import('vue-i18n')>('vue-i18n')
  return {
    ...actual,
    useI18n: () => ({
      t: (key: string) => key
    })
  }
})

describe('admin BalanceOverviewView', () => {
  beforeEach(() => {
    getBalanceOverview.mockReset()
    getBalanceOverview.mockResolvedValue({
      total_available_balance: 125.75,
      total_usage_card_available_balance: 38.5,
      usage_card_latest_expires_at: '2030-06-15T12:30:00Z',
      generated_at: '2030-06-01T08:00:00Z'
    })
  })

  it('renders user and usage-card balances independently', async () => {
    const wrapper = mount(BalanceOverviewView, {
      global: {
        stubs: {
          AppLayout: { template: '<div><slot /></div>' },
          LoadingSpinner: true,
          Icon: true
        }
      }
    })

    await flushPromises()

    expect(getBalanceOverview).toHaveBeenCalledTimes(1)
    expect(wrapper.text()).toContain('admin.balanceOverview.userAvailableBalance')
    expect(wrapper.text()).toContain('125.75')
    expect(wrapper.text()).toContain('admin.balanceOverview.usageCardAvailableBalance')
    expect(wrapper.text()).toContain('38.50')
    expect(wrapper.text()).toContain('admin.balanceOverview.latestUsageCardExpiry')
    expect(wrapper.text()).toContain('2030')
  })
})
