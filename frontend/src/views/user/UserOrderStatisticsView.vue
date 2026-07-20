<template>
  <AppLayout>
    <div data-test="order-statistics-page" class="min-w-0 max-w-full space-y-6 overflow-x-hidden">
      <header class="flex flex-wrap items-center justify-between gap-3">
        <div>
          <h1 class="text-xl font-semibold text-gray-900 dark:text-gray-100">
            {{ t('payment.statistics.title') }}
          </h1>
          <p data-test="applied-range" class="mt-1 text-sm text-gray-500 dark:text-gray-400">
            {{ appliedRange.startDate }} - {{ appliedRange.endDate }}
          </p>
        </div>
        <button
          type="button"
          class="btn btn-secondary h-9 w-9 p-0"
          :disabled="loading"
          :title="t('payment.statistics.refresh')"
          @click="refresh"
        >
          <Icon name="refresh" size="md" :class="loading ? 'animate-spin' : ''" />
        </button>
      </header>

      <section class="border-y border-gray-200 py-4 dark:border-dark-700">
        <div class="flex flex-wrap items-end gap-3">
          <div class="inline-flex h-9 overflow-hidden rounded-md border border-gray-300 dark:border-dark-600">
            <button
              v-for="days in shortcutDays"
              :key="days"
              type="button"
              :data-test="`range-${days}`"
              class="border-r border-gray-300 px-3 text-sm font-medium transition-colors last:border-r-0 dark:border-dark-600"
              :class="activeShortcut === days
                ? 'bg-primary-600 text-white'
                : 'bg-white text-gray-700 hover:bg-gray-50 dark:bg-dark-800 dark:text-gray-200 dark:hover:bg-dark-700'"
              @click="selectShortcut(days)"
            >
              {{ t(`payment.statistics.range${days}`) }}
            </button>
          </div>

          <label class="min-w-40 flex-1 sm:max-w-48">
            <span class="input-label">{{ t('payment.statistics.startDate') }}</span>
            <input
              v-model="draftRange.startDate"
              data-test="custom-start"
              type="date"
              class="input mt-1 w-full"
              :max="draftRange.endDate || today"
              @input="activeShortcut = null; validationError = null"
            />
          </label>
          <label class="min-w-40 flex-1 sm:max-w-48">
            <span class="input-label">{{ t('payment.statistics.endDate') }}</span>
            <input
              v-model="draftRange.endDate"
              data-test="custom-end"
              type="date"
              class="input mt-1 w-full"
              :min="draftRange.startDate"
              :max="today"
              @input="activeShortcut = null; validationError = null"
            />
          </label>
          <button
            data-test="custom-query"
            type="button"
            class="btn btn-primary h-9"
            :disabled="loading"
            @click="applyCustomRange"
          >
            {{ t('payment.statistics.query') }}
          </button>
        </div>
        <p v-if="validationError" data-test="range-error" class="mt-2 text-sm text-red-600 dark:text-red-400">
          {{ t(`payment.statistics.range.${validationError}`) }}
        </p>
      </section>

      <div
        v-if="!statistics && error"
        data-test="statistics-error"
        class="flex min-h-72 flex-col items-center justify-center gap-3 text-center"
      >
        <p class="text-sm text-red-600 dark:text-red-400">{{ t('payment.statistics.loadError') }}</p>
        <button data-test="statistics-retry" type="button" class="btn btn-secondary" @click="refresh">
          {{ t('payment.statistics.retry') }}
        </button>
      </div>

      <template v-else-if="statistics">
        <div
          v-if="error"
          data-test="statistics-error"
          class="flex items-center justify-between gap-3 border border-red-200 bg-red-50 px-4 py-3 text-sm text-red-700 dark:border-red-900/60 dark:bg-red-950/30 dark:text-red-300"
        >
          <span>{{ t('payment.statistics.loadError') }}</span>
          <button type="button" class="font-medium underline" @click="refresh">
            {{ t('payment.statistics.retry') }}
          </button>
        </div>

        <section class="grid min-h-28 gap-3 sm:grid-cols-3" :aria-busy="loading">
          <article class="rounded-lg border border-gray-200 bg-white p-4 dark:border-dark-700 dark:bg-dark-900">
            <p class="text-sm text-gray-500 dark:text-gray-400">{{ t('payment.statistics.summary.totalPaid') }}</p>
            <p data-test="summary-total" class="mt-2 text-2xl font-semibold tabular-nums text-gray-900 dark:text-gray-100">
              {{ formatAmount(statistics.summary.total_paid_amount) }}
            </p>
          </article>
          <article class="rounded-lg border border-gray-200 bg-white p-4 dark:border-dark-700 dark:bg-dark-900">
            <p class="text-sm text-gray-500 dark:text-gray-400">{{ t('payment.statistics.summary.orderCount') }}</p>
            <p class="mt-2 text-2xl font-semibold tabular-nums text-gray-900 dark:text-gray-100">
              {{ statistics.summary.order_count.toLocaleString(locale) }}
            </p>
          </article>
          <article class="rounded-lg border border-gray-200 bg-white p-4 dark:border-dark-700 dark:bg-dark-900">
            <p class="text-sm text-gray-500 dark:text-gray-400">{{ t('payment.statistics.summary.averagePaid') }}</p>
            <p class="mt-2 text-2xl font-semibold tabular-nums text-gray-900 dark:text-gray-100">
              {{ formatAmount(statistics.summary.average_paid_amount) }}
            </p>
          </article>
        </section>

        <p
          v-if="statistics.summary.order_count === 0"
          data-test="statistics-empty"
          class="border-y border-gray-200 py-8 text-center text-sm text-gray-500 dark:border-dark-700 dark:text-gray-400"
        >
          {{ t('payment.statistics.empty') }}
        </p>

        <section class="space-y-3">
          <h2 class="text-base font-semibold text-gray-900 dark:text-gray-100">{{ t('payment.statistics.byType') }}</h2>
          <OrderStatisticsAggregateTable
            kind="type"
            :rows="statistics.by_type"
            :currency="statistics.currency"
            @select="openDetails"
          />
        </section>

        <section class="space-y-3">
          <h2 class="text-base font-semibold text-gray-900 dark:text-gray-100">{{ t('payment.statistics.daily') }}</h2>
          <OrderStatisticsAggregateTable
            kind="daily"
            :rows="statistics.daily"
            :currency="statistics.currency"
            @select="openDetails"
          />
        </section>
      </template>

      <div v-else class="grid min-h-28 gap-3 sm:grid-cols-3" aria-busy="true">
        <div v-for="index in 3" :key="index" class="h-28 animate-pulse rounded-lg bg-gray-100 dark:bg-dark-800" />
      </div>
    </div>

    <OrderStatisticsDetailsDialog
      v-if="drilldownSelection"
      :show="true"
      :selection="drilldownSelection"
      :start-date="appliedRange.startDate"
      :end-date="appliedRange.endDate"
      @close="drilldownSelection = null"
    />
  </AppLayout>
</template>

<script setup lang="ts">
import { computed, onMounted, reactive, ref } from 'vue'
import { useI18n } from 'vue-i18n'
import type {
  OrderStatisticsDrilldownSelection,
  OrderStatisticsResponse,
} from '@/types/payment'
import { paymentAPI } from '@/api/payment'
import AppLayout from '@/components/layout/AppLayout.vue'
import Icon from '@/components/icons/Icon.vue'
import OrderStatisticsAggregateTable from '@/components/payment/OrderStatisticsAggregateTable.vue'
import OrderStatisticsDetailsDialog from '@/components/payment/OrderStatisticsDetailsDialog.vue'
import { formatPaymentAmount } from '@/components/payment/currency'
import {
  formatLocalDate,
  rangeForLastDays,
  validateInclusiveRange,
  type DateRangeValidationError,
  type LocalDateRange,
} from './orderStatistics'

const { t, locale } = useI18n()
const shortcutDays = [7, 30, 90] as const
const initialRange = rangeForLastDays(30)
const appliedRange = ref<LocalDateRange>({ ...initialRange })
const draftRange = reactive<LocalDateRange>({ ...initialRange })
const activeShortcut = ref<number | null>(30)
const statistics = ref<OrderStatisticsResponse | null>(null)
const loading = ref(false)
const error = ref(false)
const validationError = ref<DateRangeValidationError | null>(null)
const drilldownSelection = ref<OrderStatisticsDrilldownSelection | null>(null)
let summaryGeneration = 0

const today = computed(() => formatLocalDate(new Date()))

onMounted(() => {
  void loadRange(initialRange, { clearData: true })
})

function selectShortcut(days: number): void {
  const range = rangeForLastDays(days)
  activeShortcut.value = days
  draftRange.startDate = range.startDate
  draftRange.endDate = range.endDate
  appliedRange.value = { ...range }
  validationError.value = null
  void loadRange(range, { clearData: true })
}

function applyCustomRange(): void {
  const range = { ...draftRange }
  const invalid = validateInclusiveRange(range.startDate, range.endDate)
  validationError.value = invalid
  if (invalid) return
  activeShortcut.value = null
  void loadRange(range, { commitOnSuccess: true })
}

function refresh(): void {
  void loadRange(appliedRange.value)
}

async function loadRange(
  range: LocalDateRange,
  options: { clearData?: boolean; commitOnSuccess?: boolean } = {},
): Promise<void> {
  const generation = ++summaryGeneration
  loading.value = true
  error.value = false
  validationError.value = null
  drilldownSelection.value = null
  if (options.clearData) statistics.value = null

  try {
    const response = await paymentAPI.getOrderStatistics({
      start_date: range.startDate,
      end_date: range.endDate,
    })
    if (generation !== summaryGeneration) return
    statistics.value = response.data
    if (options.commitOnSuccess) {
      appliedRange.value = {
        startDate: response.data.start_date,
        endDate: response.data.end_date,
      }
      draftRange.startDate = response.data.start_date
      draftRange.endDate = response.data.end_date
    }
  } catch {
    if (generation !== summaryGeneration) return
    error.value = true
  } finally {
    if (generation === summaryGeneration) loading.value = false
  }
}

function openDetails(selection: OrderStatisticsDrilldownSelection): void {
  drilldownSelection.value = selection
}

function formatAmount(amount: number): string {
  return formatPaymentAmount(amount, 'CNY', locale.value)
}
</script>
