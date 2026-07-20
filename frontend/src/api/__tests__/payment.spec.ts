import { beforeEach, describe, expect, it, vi } from 'vitest'

const { get, post } = vi.hoisted(() => ({
  get: vi.fn(),
  post: vi.fn(),
}))

vi.mock('@/api/client', () => ({
  apiClient: {
    get,
    post,
  },
}))

import { paymentAPI } from '@/api/payment'

describe('payment api', () => {
  beforeEach(() => {
    get.mockReset()
    post.mockReset()
    get.mockResolvedValue({ data: {} })
    post.mockResolvedValue({ data: {} })
  })

  it('keeps legacy public out_trade_no verification for upgrade compatibility', async () => {
    await paymentAPI.verifyOrderPublic('legacy-order-no')

    expect(post).toHaveBeenCalledWith('/payment/public/orders/verify', {
      out_trade_no: 'legacy-order-no',
    })
  })

  it('keeps signed public resume-token resolve endpoint', async () => {
    await paymentAPI.resolveOrderPublicByResumeToken('resume-token-123')

    expect(post).toHaveBeenCalledWith('/payment/public/orders/resolve', {
      resume_token: 'resume-token-123',
    })
  })

  it('requests the authenticated user order statistics range', async () => {
    const params = { start_date: '2026-07-01', end_date: '2026-07-20' }

    await paymentAPI.getOrderStatistics(params)

    expect(get).toHaveBeenCalledWith('/payment/orders/statistics', { params })
  })

  it('requests type and daily drilldowns with fixed selectors', async () => {
    await paymentAPI.getOrderStatisticsDetails({
      start_date: '2026-07-01',
      end_date: '2026-07-20',
      order_type: 'balance',
      page: 2,
    })
    await paymentAPI.getOrderStatisticsDetails({
      start_date: '2026-07-01',
      end_date: '2026-07-20',
      date: '2026-07-20',
      page: 1,
    })

    expect(get).toHaveBeenNthCalledWith(1, '/payment/orders/statistics/details', {
      params: {
        start_date: '2026-07-01',
        end_date: '2026-07-20',
        order_type: 'balance',
        page: 2,
      },
    })
    expect(get).toHaveBeenNthCalledWith(2, '/payment/orders/statistics/details', {
      params: {
        start_date: '2026-07-01',
        end_date: '2026-07-20',
        date: '2026-07-20',
        page: 1,
      },
    })
  })
})
