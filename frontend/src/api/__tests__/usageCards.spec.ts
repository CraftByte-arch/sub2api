import { beforeEach, describe, expect, it, vi } from 'vitest'

const { get } = vi.hoisted(() => ({
  get: vi.fn(),
}))

vi.mock('@/api/client', () => ({
  apiClient: {
    get,
    post: vi.fn(),
    put: vi.fn(),
    delete: vi.fn(),
  },
}))

import { adminUsageCardsAPI } from '@/api/usageCards'

describe('admin usage cards api', () => {
  beforeEach(() => {
    get.mockReset()
  })

  it('passes pagination, filters, and cancellation through to the list request', async () => {
    const controller = new AbortController()
    get.mockResolvedValue({
      data: {
        items: [],
        total: 0,
        page: 2,
        page_size: 50,
        pages: 1,
      },
    })

    await adminUsageCardsAPI.listCards(
      {
        page: 2,
        page_size: 50,
        status: 'expired',
        user_id: 9,
        sort_by: 'expires_at',
        sort_order: 'desc',
      },
      { signal: controller.signal },
    )

    expect(get).toHaveBeenCalledWith('/admin/usage-cards', {
      params: {
        page: 2,
        page_size: 50,
        status: 'expired',
        user_id: 9,
        sort_by: 'expires_at',
        sort_order: 'desc',
      },
      signal: controller.signal,
    })
  })
})
