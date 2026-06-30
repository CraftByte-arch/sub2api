import { beforeEach, describe, expect, it, vi } from 'vitest'
import { createPinia, setActivePinia } from 'pinia'
import { useUsageCardSummaryStore } from '@/stores/usageCardSummary'

const { getSummary } = vi.hoisted(() => ({
  getSummary: vi.fn(),
}))

vi.mock('@/api/usageCards', () => ({
  usageCardsAPI: {
    getSummary,
  },
}))

describe('useUsageCardSummaryStore', () => {
  beforeEach(() => {
    setActivePinia(createPinia())
    getSummary.mockReset()
  })

  it('refreshes available count and remaining USD from the API', async () => {
    getSummary.mockResolvedValue({
      data: {
        available_count: 2,
        available_remaining_usd: 7.5,
      },
    })
    const store = useUsageCardSummaryStore()

    const summary = await store.refresh()

    expect(getSummary).toHaveBeenCalledTimes(1)
    expect(summary).toEqual({
      available_count: 2,
      available_remaining_usd: 7.5,
    })
    expect(store.availableCount).toBe(2)
    expect(store.availableRemainingUSD).toBe(7.5)
    expect(store.loading).toBe(false)
    expect(store.error).toBeNull()
  })
})
