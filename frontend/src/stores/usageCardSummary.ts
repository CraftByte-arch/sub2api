import { defineStore } from 'pinia'
import { computed, ref } from 'vue'
import { usageCardsAPI, type UsageCardSummary } from '@/api/usageCards'

const emptySummary = (): UsageCardSummary => ({
  available_count: 0,
  available_remaining_usd: 0,
})

export const useUsageCardSummaryStore = defineStore('usageCardSummary', () => {
  const summary = ref<UsageCardSummary>(emptySummary())
  const loading = ref(false)
  const error = ref<unknown>(null)

  const availableCount = computed(() => summary.value.available_count)
  const availableRemainingUSD = computed(() => summary.value.available_remaining_usd)

  async function refresh(): Promise<UsageCardSummary> {
    loading.value = true
    error.value = null
    try {
      const res = await usageCardsAPI.getSummary()
      summary.value = {
        available_count: Number(res.data.available_count) || 0,
        available_remaining_usd: Number(res.data.available_remaining_usd) || 0,
      }
      return summary.value
    } catch (err) {
      error.value = err
      throw err
    } finally {
      loading.value = false
    }
  }

  function reset(): void {
    summary.value = emptySummary()
    error.value = null
    loading.value = false
  }

  return {
    summary,
    loading,
    error,
    availableCount,
    availableRemainingUSD,
    refresh,
    reset,
  }
})
