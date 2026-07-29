import { beforeEach, describe, expect, it, vi } from 'vitest'

const { get } = vi.hoisted(() => ({
  get: vi.fn(),
}))

vi.mock('@/api/client', () => ({
  apiClient: { get },
}))

import { adminPaymentAPI } from '@/api/admin/payment'

describe('admin payment api', () => {
  beforeEach(() => {
    get.mockReset()
    get.mockResolvedValue({ data: {} })
  })

  it('requests filtered user payment aggregation', async () => {
    const params = {
      user: 'alice@example.com',
      start_date: '2026-07-01',
      end_date: '2026-07-31',
      status: 'COMPLETED',
      granularity: 'week' as const,
      page: 2,
      page_size: 20,
    }

    await adminPaymentAPI.getOrderAggregation(params)

    expect(get).toHaveBeenCalledWith('/admin/payment/orders/aggregation', { params })
  })
})
