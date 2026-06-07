<template>
  <AppLayout>
    <div class="mx-auto max-w-5xl space-y-6">
      <div class="flex flex-wrap items-center justify-between gap-3">
        <h1 class="text-xl font-semibold text-gray-900 dark:text-white">{{ t('usageCards.myTitle') }}</h1>
        <div class="flex items-center gap-3">
          <div class="w-36">
            <Select
              v-model="statusFilter"
              :options="statusOptions"
              :placeholder="t('usageCards.allStatus')"
              :searchable="false"
            />
          </div>
          <RouterLink to="/purchase?tab=usage_card" class="btn btn-primary">{{ t('usageCards.buy') }}</RouterLink>
        </div>
      </div>
      <div v-if="loading" class="card py-16 text-center text-gray-500 dark:text-gray-400">{{ t('usageCards.loading') }}</div>
      <div v-else-if="filteredCards.length === 0" class="card py-16 text-center text-gray-500 dark:text-gray-400">{{ t('usageCards.empty') }}</div>
      <div v-else class="grid grid-cols-1 gap-4 md:grid-cols-2">
        <div v-for="card in filteredCards" :key="card.id" class="card p-5">
          <div class="flex items-start justify-between gap-4">
            <div>
              <h2 class="font-semibold text-gray-900 dark:text-white">{{ card.name }}</h2>
              <div class="mt-1 space-y-0.5 text-xs text-gray-400 dark:text-gray-500">
                <p>{{ t('usageCards.expiresAt', { time: formatMinuteDateTime(card.expires_at) }) }}</p>
                <p>{{ t('usageCards.redeemedAt', { time: formatMinuteDateTime(card.created_at) }) }}</p>
              </div>
            </div>
            <span class="badge" :class="statusClass(card.status)">{{ statusLabel(card.status) }}</span>
          </div>
          <div class="mt-4 h-2 overflow-hidden rounded-full bg-gray-100 dark:bg-dark-700">
            <div class="h-full rounded-full bg-primary-500" :style="{ width: `${progress(card)}%` }"></div>
          </div>
          <div class="mt-3 flex justify-between text-sm">
            <span class="text-gray-500 dark:text-gray-400">{{ t('usageCards.remaining') }} ${{ card.remaining_usd.toFixed(4) }}</span>
            <span class="text-gray-900 dark:text-white">{{ t('usageCards.total') }} ${{ card.total_limit_usd.toFixed(2) }}</span>
          </div>
        </div>
      </div>
    </div>
  </AppLayout>
</template>

<script setup lang="ts">
import { computed, onMounted, ref } from 'vue'
import { useI18n } from 'vue-i18n'
import AppLayout from '@/components/layout/AppLayout.vue'
import Select from '@/components/common/Select.vue'
import { usageCardsAPI, type UserUsageCard } from '@/api/usageCards'
import { formatDateTime } from '@/utils/format'

const { t } = useI18n()

const loading = ref(true)
const cards = ref<UserUsageCard[]>([])
const statusFilter = ref('active')

const statusOptions = computed(() => [
  { value: '', label: t('usageCards.allStatus') },
  { value: 'active', label: t('usageCards.status.active') },
  { value: 'exhausted', label: t('usageCards.status.exhausted') },
  { value: 'expired', label: t('usageCards.status.expired') },
  { value: 'suspended', label: t('usageCards.status.suspended') },
  { value: 'cancelled', label: t('usageCards.status.cancelled') },
])

const filteredCards = computed(() => {
  return [...cards.value]
    .filter((card) => !statusFilter.value || card.status === statusFilter.value)
    .sort((a, b) => getRedeemedAt(b) - getRedeemedAt(a))
})

function progress(card: UserUsageCard) {
  if (card.total_limit_usd <= 0) return 0
  return Math.max(0, Math.min(100, (card.used_usd / card.total_limit_usd) * 100))
}

function statusLabel(status: string) {
  return t(`usageCards.status.${status}`, status)
}

function statusClass(status: string) {
  if (status === 'active') return 'badge-success'
  if (status === 'suspended') return 'badge-warning'
  return 'badge-secondary'
}

function formatMinuteDateTime(value: string) {
  return formatDateTime(value, {
    year: 'numeric',
    month: '2-digit',
    day: '2-digit',
    hour: '2-digit',
    minute: '2-digit',
    hour12: false
  })
}

function getRedeemedAt(card: UserUsageCard) {
  const time = new Date(card.created_at).getTime()
  return Number.isFinite(time) ? time : 0
}

onMounted(async () => {
  try {
    const res = await usageCardsAPI.listMine()
    cards.value = res.data
  } finally {
    loading.value = false
  }
})
</script>
