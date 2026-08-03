<template>
  <AppLayout>
    <div class="space-y-6">
      <div class="flex items-center justify-end gap-3">
        <p v-if="stats" class="text-xs text-gray-500 dark:text-gray-400">
          {{ t('admin.balanceOverview.updatedAt') }}: {{ formatDateTimeToMinute(stats.generated_at) }}
        </p>
        <button
          type="button"
          class="btn btn-secondary btn-icon"
          :disabled="loading"
          :title="t('common.refresh')"
          :aria-label="t('common.refresh')"
          @click="loadBalanceOverview"
        >
          <Icon name="refresh" size="md" :class="loading ? 'animate-spin' : ''" />
        </button>
      </div>

      <div v-if="loading && !stats" class="flex items-center justify-center py-12">
        <LoadingSpinner />
      </div>

      <template v-else-if="stats">
        <div class="grid grid-cols-1 gap-4 md:grid-cols-2">
          <section class="card p-5">
            <div class="flex items-start gap-4">
              <div class="rounded-lg bg-cyan-100 p-2.5 dark:bg-cyan-900/30">
                <Icon name="dollar" size="lg" class="text-cyan-600 dark:text-cyan-400" :stroke-width="2" />
              </div>
              <div class="min-w-0 flex-1">
                <p class="text-sm font-medium text-gray-600 dark:text-gray-300">
                  {{ t('admin.balanceOverview.userAvailableBalance') }}
                </p>
                <p
                  class="mt-1 truncate text-2xl font-bold tabular-nums text-gray-900 dark:text-white"
                  :title="formatCurrency(stats.total_available_balance, 8)"
                >
                  {{ formatCurrency(stats.total_available_balance) }}
                </p>
                <p class="mt-2 text-xs text-gray-500 dark:text-gray-400">
                  {{ t('admin.balanceOverview.excludesFrozenBalance') }}
                </p>
              </div>
            </div>
          </section>

          <section class="card p-5">
            <div class="flex items-start gap-4">
              <div class="rounded-lg bg-orange-100 p-2.5 dark:bg-orange-900/30">
                <Icon name="creditCard" size="lg" class="text-orange-600 dark:text-orange-400" :stroke-width="2" />
              </div>
              <div class="min-w-0 flex-1">
                <p class="text-sm font-medium text-gray-600 dark:text-gray-300">
                  {{ t('admin.balanceOverview.usageCardAvailableBalance') }}
                </p>
                <p
                  class="mt-1 truncate text-2xl font-bold tabular-nums text-gray-900 dark:text-white"
                  :title="formatCurrency(stats.total_usage_card_available_balance, 8)"
                >
                  {{ formatCurrency(stats.total_usage_card_available_balance) }}
                </p>
                <p
                  v-if="stats.usage_card_latest_expires_at"
                  class="mt-2 truncate text-xs text-gray-500 dark:text-gray-400"
                  :title="formatDateTimeToMinute(stats.usage_card_latest_expires_at)"
                >
                  {{ t('admin.balanceOverview.latestUsageCardExpiry') }}:
                  {{ formatDateTimeToMinute(stats.usage_card_latest_expires_at) }}
                </p>
                <p v-else class="mt-2 text-xs text-gray-500 dark:text-gray-400">
                  {{ t('admin.balanceOverview.noAvailableUsageCards') }}
                </p>
              </div>
            </div>
          </section>
        </div>
      </template>

      <div v-else class="py-12 text-center text-sm text-gray-500 dark:text-gray-400">
        {{ t('admin.balanceOverview.failedToLoad') }}
      </div>
    </div>
  </AppLayout>
</template>

<script setup lang="ts">
import { onMounted, ref } from 'vue'
import { useI18n } from 'vue-i18n'
import { getBalanceOverview, type BalanceOverview } from '@/api/admin/balanceOverview'
import { useAppStore } from '@/stores/app'
import { formatDateTimeToMinute } from '@/utils/format'
import AppLayout from '@/components/layout/AppLayout.vue'
import Icon from '@/components/icons/Icon.vue'
import LoadingSpinner from '@/components/common/LoadingSpinner.vue'

const { t } = useI18n()
const appStore = useAppStore()
const stats = ref<BalanceOverview | null>(null)
const loading = ref(false)

function formatCurrency(value: number, maximumFractionDigits: number = 4): string {
  const amount = Number.isFinite(value) ? value : 0
  return new Intl.NumberFormat(undefined, {
    style: 'currency',
    currency: 'USD',
    minimumFractionDigits: 2,
    maximumFractionDigits
  }).format(amount)
}

async function loadBalanceOverview() {
  loading.value = true
  try {
    stats.value = await getBalanceOverview()
  } catch (error) {
    console.error('Failed to load balance overview:', error)
    appStore.showError(t('admin.balanceOverview.failedToLoad'))
  } finally {
    loading.value = false
  }
}

onMounted(() => {
  void loadBalanceOverview()
})
</script>
