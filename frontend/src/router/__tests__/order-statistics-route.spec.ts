import { beforeAll, describe, expect, it, vi } from 'vitest'

import en from '@/i18n/locales/en'
import zh from '@/i18n/locales/zh'

interface CapturedRoute {
  path: string
  name?: string
  component?: unknown
  meta?: Record<string, unknown>
}

const routerHarness = vi.hoisted(() => ({
  routes: [] as CapturedRoute[],
}))

vi.mock('vue-router', () => ({
  createWebHistory: vi.fn(() => ({})),
  createRouter: vi.fn((options: { routes: CapturedRoute[] }) => {
    routerHarness.routes = options.routes
    return {
      beforeEach: vi.fn(),
      afterEach: vi.fn(),
      onError: vi.fn(),
    }
  }),
}))

vi.mock('@/stores/auth', () => ({
  useAuthStore: vi.fn(),
}))

vi.mock('@/stores/app', () => ({
  useAppStore: vi.fn(),
}))

vi.mock('@/stores/adminSettings', () => ({
  useAdminSettingsStore: vi.fn(),
}))

vi.mock('@/stores/adminCompliance', () => ({
  useAdminComplianceStore: vi.fn(),
}))

vi.mock('@/composables/useNavigationLoading', () => ({
  useNavigationLoadingState: () => ({
    startNavigation: vi.fn(),
    endNavigation: vi.fn(),
  }),
}))

vi.mock('@/composables/useRoutePrefetch', () => ({
  useRoutePrefetch: vi.fn(),
}))

vi.mock('@/api/setup', () => ({
  getSetupStatus: vi.fn(),
}))

vi.mock('@/extensions', () => ({
  extensionRoutes: [],
}))

vi.mock('@/utils/featureFlags', () => ({
  isUsageCardFeatureVisible: vi.fn(() => true),
}))

const requiredStatisticsKeys = [
  'title',
  'refresh',
  'range7',
  'range30',
  'range90',
  'startDate',
  'endDate',
  'query',
  'range.required',
  'range.invalid',
  'range.reversed',
  'range.tooLong',
  'loadError',
  'retry',
  'empty',
  'summary.totalPaid',
  'summary.orderCount',
  'summary.averagePaid',
  'byType',
  'daily',
  'openDetails',
  'types.balance',
  'types.usage_card',
  'types.subscription',
  'columns.type',
  'columns.date',
  'columns.totalPaid',
  'columns.orderCount',
  'columns.averagePaid',
  'columns.orderNo',
  'columns.paidAmount',
  'columns.status',
  'columns.paymentMethod',
  'columns.paidAt',
  'details.typeTitle',
  'details.dateTitle',
  'details.loadError',
  'details.retry',
  'details.empty',
] as const

function messageAt(root: unknown, path: string): unknown {
  return path.split('.').reduce<unknown>((node, segment) => {
    if (!node || typeof node !== 'object') return undefined
    return (node as Record<string, unknown>)[segment]
  }, root)
}

describe('order statistics route', () => {
  beforeAll(async () => {
    await import('@/router')
  })

  it('registers a lazy payment-protected user route immediately after orders', () => {
    const ordersIndex = routerHarness.routes.findIndex((route) => route.path === '/orders')
    const statisticsIndex = routerHarness.routes.findIndex(
      (route) => route.path === '/order-statistics',
    )
    const route = routerHarness.routes[statisticsIndex]

    expect(ordersIndex).toBeGreaterThanOrEqual(0)
    expect(statisticsIndex).toBe(ordersIndex + 1)
    expect(route).toMatchObject({
      name: 'OrderStatistics',
      meta: {
        requiresAuth: true,
        requiresAdmin: false,
        requiresPayment: true,
        titleKey: 'nav.orderStatistics',
      },
    })
    expect(route?.component).toEqual(expect.any(Function))
  })
})

describe('order statistics locale contract', () => {
  it.each([
    ['zh', zh],
    ['en', en],
  ] as const)('provides every statistics page message in %s', (_locale, messages) => {
    expect(messageAt(messages, 'nav.orderStatistics')).toEqual(expect.any(String))
    for (const key of requiredStatisticsKeys) {
      expect(messageAt(messages, `payment.statistics.${key}`), key).toEqual(expect.any(String))
    }
  })

  it('uses the confirmed Chinese labels for the three order types', () => {
    expect(messageAt(zh, 'payment.statistics.types')).toEqual({
      balance: '余额',
      usage_card: '余额卡',
      subscription: '订阅',
    })
  })
})
