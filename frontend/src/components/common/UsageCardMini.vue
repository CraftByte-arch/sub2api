<template>
  <div class="relative" @mouseenter="openTooltip" @mouseleave="closeTooltip">
    <button
      type="button"
      class="flex items-center gap-1.5 rounded-xl bg-primary-50 px-2.5 py-1.5 text-primary-700 transition-colors hover:bg-primary-100 dark:bg-primary-900/20 dark:text-primary-300 dark:hover:bg-primary-900/30"
      :aria-label="t('usageCards.title')"
      :aria-expanded="tooltipOpen"
    >
      <Icon name="creditCard" size="sm" />
      <span class="hidden text-sm font-semibold sm:inline">{{ t('usageCards.title') }}</span>
      <span
        class="min-w-4 rounded-full bg-primary-100 px-1.5 text-center text-[10px] font-semibold leading-4 text-primary-700 dark:bg-primary-800/60 dark:text-primary-200"
      >
        {{ availableCount }}
      </span>
      <span class="text-sm font-semibold tabular-nums text-primary-700 dark:text-primary-200">
        ${{ availableRemainingUSD.toFixed(2) }}
      </span>
    </button>

    <transition name="dropdown">
      <div
        v-if="tooltipOpen"
        class="absolute right-0 z-50 mt-2 w-[320px] overflow-hidden rounded-xl border border-gray-200 bg-white shadow-xl dark:border-dark-700 dark:bg-dark-800"
      >
        <div class="border-b border-gray-100 px-3 py-2 dark:border-dark-700">
          <h3 class="text-sm font-semibold text-gray-900 dark:text-white">{{ t('usageCards.title') }}</h3>
          <p class="text-xs text-gray-500 dark:text-dark-400">
            {{ availableCount > 0 ? t('usageCards.availableCount', { count: availableCount }) : t('usageCards.empty') }}
          </p>
        </div>

        <div class="max-h-[360px] overflow-y-auto">
          <div v-if="loading" class="p-4 text-center text-xs text-gray-500 dark:text-dark-400">
            {{ t('usageCards.loading') }}
          </div>
          <div v-else-if="displayCards.length === 0" class="p-4 text-center text-xs text-gray-500 dark:text-dark-400">
            {{ t('usageCards.empty') }}
          </div>
          <template v-else>
            <div
              v-for="card in displayCards"
              :key="card.id"
              class="border-b border-gray-50 px-3 py-2.5 last:border-b-0 dark:border-dark-700/50"
            >
              <div class="flex items-start justify-between gap-3">
                <div class="min-w-0">
                  <p class="truncate text-sm font-medium text-gray-900 dark:text-white">
                    {{ card.name }}
                  </p>
                </div>
                <span class="badge shrink-0" :class="statusClass(effectiveStatus(card))">
                  {{ statusLabel(effectiveStatus(card)) }}
                </span>
              </div>

              <div class="mt-2">
                <div class="mb-1 flex items-center justify-between text-[10px] text-gray-400 dark:text-dark-500">
                  <span>{{ t('usageCards.remainingQuota') }}</span>
                  <span>{{ remainingPercent(card).toFixed(0) }}%</span>
                </div>
                <div class="h-1.5 overflow-hidden rounded-full bg-gray-100 dark:bg-dark-700">
                  <div
                    class="h-full rounded-full transition-all"
                    :class="progressClass(card)"
                    :style="{ width: `${remainingPercent(card)}%` }"
                  ></div>
                </div>
              </div>

              <div class="mt-2 flex items-center justify-between text-xs">
                <span class="text-gray-500 dark:text-dark-400">
                  {{ t('usageCards.remaining') }} ${{ card.remaining_usd.toFixed(4) }}
                </span>
                <span class="text-gray-400 dark:text-dark-500">
                  {{ t('usageCards.expiresAt', { time: formatMinuteDateTime(card.expires_at) }) }}
                </span>
              </div>
            </div>
          </template>
        </div>

        <div class="border-t border-gray-100 p-2 dark:border-dark-700">
          <router-link
            to="/usage-cards"
            class="block w-full py-1 text-center text-xs text-primary-600 hover:underline dark:text-primary-400"
            @click="closeTooltip"
          >
            {{ t('usageCards.viewMore') }}
          </router-link>
        </div>
      </div>
    </transition>
  </div>
</template>

<script setup lang="ts">
import { computed, onBeforeUnmount, onMounted, ref } from 'vue'
import { useI18n } from 'vue-i18n'
import Icon from '@/components/icons/Icon.vue'
import { usageCardsAPI, type UserUsageCard } from '@/api/usageCards'
import { useUsageCardSummaryStore } from '@/stores/usageCardSummary'
import { formatDateTime } from '@/utils/format'

const { t } = useI18n()
const usageCardSummaryStore = useUsageCardSummaryStore()

const loading = ref(false)
const cards = ref<UserUsageCard[]>([])
const tooltipOpen = ref(false)
let closeTimer: ReturnType<typeof setTimeout> | null = null
const availableCount = computed(() => usageCardSummaryStore.availableCount)
const availableRemainingUSD = computed(() => usageCardSummaryStore.availableRemainingUSD)

const displayCards = computed(() => {
  return [...cards.value]
    .sort((a, b) => new Date(b.created_at).getTime() - new Date(a.created_at).getTime())
    .slice(0, 3)
})

function openTooltip() {
  if (closeTimer) {
    clearTimeout(closeTimer)
    closeTimer = null
  }
  tooltipOpen.value = true
}

function closeTooltip() {
  if (closeTimer) {
    clearTimeout(closeTimer)
  }
  closeTimer = setTimeout(() => {
    tooltipOpen.value = false
    closeTimer = null
  }, 120)
}

function statusLabel(status: string) {
  return t(`usageCards.status.${status}`, status)
}

function statusClass(status: string) {
  if (status === 'active') return 'badge-success'
  if (status === 'suspended') return 'badge-warning'
  return 'badge-secondary'
}

function effectiveStatus(card: UserUsageCard) {
  if (card.status === 'active') {
    const expiresAt = new Date(card.expires_at).getTime()
    if (Number.isFinite(expiresAt) && expiresAt <= Date.now()) return 'expired'
    if (card.total_limit_usd > 0 && card.used_usd >= card.total_limit_usd) return 'exhausted'
  }
  return card.status
}

function remainingPercent(card: UserUsageCard) {
  if (card.total_limit_usd <= 0) return 0
  return Math.max(0, Math.min(100, (card.remaining_usd / card.total_limit_usd) * 100))
}

function progressClass(card: UserUsageCard) {
  const percent = remainingPercent(card)
  if (effectiveStatus(card) !== 'active') return 'bg-gray-300 dark:bg-dark-600'
  if (percent <= 10) return 'bg-red-500'
  if (percent <= 30) return 'bg-amber-500'
  return 'bg-primary-500'
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

onMounted(async () => {
  loading.value = true
  try {
    await Promise.allSettled([
      usageCardSummaryStore.refresh(),
      usageCardsAPI.listMine().then((res) => {
        cards.value = res.data
      }),
    ])
  } finally {
    loading.value = false
  }
})

onBeforeUnmount(() => {
  if (closeTimer) {
    clearTimeout(closeTimer)
  }
})
</script>

<style scoped>
.dropdown-enter-active,
.dropdown-leave-active {
  transition: all 0.2s ease;
}

.dropdown-enter-from,
.dropdown-leave-to {
  opacity: 0;
  transform: scale(0.95) translateY(-4px);
}
</style>
