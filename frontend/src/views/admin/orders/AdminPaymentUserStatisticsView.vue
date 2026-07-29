<template>
  <AppLayout>
    <div data-test="admin-payment-user-statistics" class="min-w-0 max-w-full space-y-6 overflow-x-hidden">
      <header class="flex flex-wrap items-start justify-between gap-3">
        <div>
          <h1 class="text-xl font-semibold text-gray-900 dark:text-gray-100">
            {{ t('payment.admin.aggregation.title') }}
          </h1>
          <p class="mt-1 text-sm text-gray-500 dark:text-gray-400">
            {{ appliedRange.startDate }} - {{ appliedRange.endDate }}
            <span class="mx-1 text-gray-300 dark:text-dark-600">·</span>
            {{ t('payment.admin.aggregation.timezone') }}: {{ stats?.timezone || timezoneLabel }}
          </p>
        </div>
        <button
          type="button"
          class="btn btn-secondary h-9 w-9 p-0"
          :disabled="loading"
          :title="t('common.refresh')"
          @click="refresh"
        >
          <Icon name="refresh" size="md" :class="loading ? 'animate-spin' : ''" />
        </button>
      </header>

      <section class="border-y border-gray-200 py-4 dark:border-dark-700">
        <div class="grid grid-cols-1 gap-3 sm:grid-cols-2 xl:grid-cols-6">
          <label class="min-w-0 xl:col-span-2">
            <span class="input-label">{{ t('payment.admin.aggregation.user') }}</span>
            <div class="relative mt-1">
              <Icon name="search" size="sm" class="pointer-events-none absolute left-3 top-1/2 -translate-y-1/2 text-gray-400" />
              <input
                v-model="draftFilters.user"
                type="search"
                class="input w-full pl-9"
                :placeholder="t('payment.admin.aggregation.userPlaceholder')"
                @keydown.enter="applyFilters"
              />
            </div>
          </label>

          <label class="min-w-0 xl:col-span-2">
            <span class="input-label">{{ t('payment.admin.aggregation.dateRange') }}</span>
            <DateRangePicker
              v-model:start-date="draftFilters.startDate"
              v-model:end-date="draftFilters.endDate"
              class="mt-1"
              :show-presets="false"
              :block="true"
              :max-date="today"
              @change="onDateRangeChange"
            />
          </label>

          <label class="min-w-0">
            <span class="input-label">{{ t('payment.admin.aggregation.status') }}</span>
            <Select v-model="draftFilters.status" class="mt-1 w-full" :options="statusOptions" />
          </label>

          <div class="min-w-0">
            <span class="input-label">{{ t('payment.admin.aggregation.granularity.label') }}</span>
            <div class="mt-1 inline-flex h-9 w-full overflow-hidden rounded-md border border-gray-300 dark:border-dark-600">
              <button
                v-for="option in granularityOptions"
                :key="option.value"
                type="button"
                class="min-w-0 flex-1 border-r border-gray-300 px-2 text-sm font-medium transition-colors last:border-r-0 dark:border-dark-600"
                :class="draftFilters.granularity === option.value
                  ? 'bg-primary-600 text-white'
                  : 'bg-white text-gray-700 hover:bg-gray-50 dark:bg-dark-800 dark:text-gray-200 dark:hover:bg-dark-700'"
                @click="draftFilters.granularity = option.value"
              >
                {{ option.label }}
              </button>
            </div>
          </div>
        </div>

        <div class="mt-3 flex flex-wrap items-center gap-2">
          <span class="text-xs font-medium text-gray-500 dark:text-gray-400">{{ t('payment.admin.aggregation.quickRange') }}</span>
          <button
            v-for="days in shortcutDays"
            :key="days"
            type="button"
            class="rounded-md border px-2.5 py-1 text-xs font-medium transition-colors"
            :class="activeShortcut === days
              ? 'border-primary-500 bg-primary-50 text-primary-700 dark:bg-primary-900/30 dark:text-primary-300'
              : 'border-gray-200 text-gray-600 hover:border-gray-300 hover:bg-gray-50 dark:border-dark-600 dark:text-gray-300 dark:hover:bg-dark-800'"
            @click="selectShortcut(days)"
          >
            {{ t(`payment.admin.aggregation.range${days}`) }}
          </button>
          <button type="button" class="btn btn-primary ml-auto h-9" :disabled="loading" @click="applyFilters">
            <Icon name="search" size="sm" />
            {{ t('payment.admin.aggregation.query') }}
          </button>
        </div>
        <p v-if="validationError" class="mt-2 text-sm text-red-600 dark:text-red-400">
          {{ t(`payment.admin.aggregation.range.${validationError}`) }}
        </p>
      </section>

      <div
        v-if="!stats && error"
        class="flex min-h-72 flex-col items-center justify-center gap-3 text-center"
      >
        <p class="text-sm text-red-600 dark:text-red-400">{{ t('payment.admin.aggregation.loadError') }}</p>
        <button type="button" class="btn btn-secondary" @click="refresh">
          {{ t('payment.admin.aggregation.retry') }}
        </button>
      </div>

      <template v-else-if="stats">
        <section class="grid gap-3 sm:grid-cols-2 xl:grid-cols-4" :aria-busy="loading">
          <article class="rounded-lg border border-gray-200 bg-white p-4 dark:border-dark-700 dark:bg-dark-900">
            <div class="flex items-start justify-between gap-3">
              <p class="text-sm text-gray-500 dark:text-gray-400">{{ t('payment.admin.aggregation.summary.totalPaid') }}</p>
              <Icon name="dollar" size="md" class="text-emerald-600 dark:text-emerald-400" />
            </div>
            <div class="mt-2 space-y-0.5">
              <p v-for="[currency, amount] in sortedAmounts(stats.summary.total_amount)" :key="currency" class="text-2xl font-semibold tabular-nums text-gray-900 dark:text-gray-100">
                {{ formatAmount(amount, currency) }}
              </p>
              <p v-if="!sortedAmounts(stats.summary.total_amount).length" class="text-2xl font-semibold tabular-nums text-gray-900 dark:text-gray-100">
                {{ formatAmount(0, 'CNY') }}
              </p>
            </div>
          </article>
          <article class="rounded-lg border border-gray-200 bg-white p-4 dark:border-dark-700 dark:bg-dark-900">
            <div class="flex items-start justify-between gap-3">
              <p class="text-sm text-gray-500 dark:text-gray-400">{{ t('payment.admin.aggregation.summary.orderCount') }}</p>
              <Icon name="document" size="md" class="text-blue-600 dark:text-blue-400" />
            </div>
            <p class="mt-2 text-2xl font-semibold tabular-nums text-gray-900 dark:text-gray-100">{{ formatNumber(stats.summary.order_count) }}</p>
          </article>
          <article class="rounded-lg border border-gray-200 bg-white p-4 dark:border-dark-700 dark:bg-dark-900">
            <div class="flex items-start justify-between gap-3">
              <p class="text-sm text-gray-500 dark:text-gray-400">{{ t('payment.admin.aggregation.summary.userCount') }}</p>
              <Icon name="users" size="md" class="text-violet-600 dark:text-violet-400" />
            </div>
            <p class="mt-2 text-2xl font-semibold tabular-nums text-gray-900 dark:text-gray-100">{{ formatNumber(stats.summary.user_count) }}</p>
          </article>
          <article class="rounded-lg border border-gray-200 bg-white p-4 dark:border-dark-700 dark:bg-dark-900">
            <div class="flex items-start justify-between gap-3">
              <p class="text-sm text-gray-500 dark:text-gray-400">{{ t('payment.admin.aggregation.summary.averagePaid') }}</p>
              <Icon name="chart" size="md" class="text-amber-600 dark:text-amber-400" />
            </div>
            <div class="mt-2 space-y-0.5">
              <p v-for="[currency, amount] in sortedAmounts(stats.summary.average_amount)" :key="currency" class="text-2xl font-semibold tabular-nums text-gray-900 dark:text-gray-100">
                {{ formatAmount(amount, currency) }}
              </p>
              <p v-if="!sortedAmounts(stats.summary.average_amount).length" class="text-2xl font-semibold tabular-nums text-gray-900 dark:text-gray-100">
                {{ formatAmount(0, 'CNY') }}
              </p>
            </div>
          </article>
        </section>

        <div class="grid gap-6 xl:grid-cols-[minmax(0,1fr)_20rem]">
          <AdminPaymentAggregationChart
            :data="stats.timeline"
            :granularity="stats.granularity"
            :loading="loading"
          />

          <section class="rounded-lg border border-gray-200 bg-white p-4 dark:border-dark-700 dark:bg-dark-900">
            <div class="mb-4 flex items-center justify-between gap-2">
              <h2 class="text-sm font-semibold text-gray-900 dark:text-gray-100">
                {{ t('payment.admin.aggregation.currencyBreakdown') }}
              </h2>
              <Icon name="creditCard" size="sm" class="text-gray-400" />
            </div>
            <div v-if="sortedAmounts(stats.summary.total_amount).length" class="space-y-4">
              <div v-for="[currency, amount] in sortedAmounts(stats.summary.total_amount)" :key="currency">
                <div class="flex items-baseline justify-between gap-3">
                  <span class="text-sm font-medium text-gray-700 dark:text-gray-300">{{ currency }}</span>
                  <span class="text-sm font-semibold tabular-nums text-gray-900 dark:text-gray-100">{{ formatAmount(amount, currency) }}</span>
                </div>
              </div>
            </div>
            <p v-else class="py-8 text-center text-sm text-gray-500 dark:text-gray-400">{{ t('payment.admin.aggregation.noData') }}</p>
          </section>
        </div>

        <section class="overflow-hidden rounded-lg border border-gray-200 bg-white dark:border-dark-700 dark:bg-dark-900">
          <div class="flex flex-wrap items-center justify-between gap-3 border-b border-gray-200 px-4 py-4 dark:border-dark-700">
            <div>
              <h2 class="text-sm font-semibold text-gray-900 dark:text-gray-100">{{ t('payment.admin.aggregation.usersTitle') }}</h2>
              <p class="mt-1 text-xs text-gray-500 dark:text-gray-400">{{ t('payment.admin.aggregation.resultCount', { count: formatNumber(stats.total) }) }}</p>
            </div>
            <span class="rounded-full bg-gray-100 px-2.5 py-1 text-xs font-medium text-gray-600 dark:bg-dark-800 dark:text-gray-300">
              {{ currentStatusLabel }}
            </span>
          </div>

          <div class="overflow-x-auto">
            <table class="w-full min-w-[760px] divide-y divide-gray-200 dark:divide-dark-700">
              <thead class="bg-gray-50 dark:bg-dark-800">
                <tr>
                  <th class="px-4 py-3 text-left text-xs font-medium uppercase tracking-normal text-gray-500 dark:text-gray-400">{{ t('payment.admin.aggregation.columns.user') }}</th>
                  <th class="px-4 py-3 text-left text-xs font-medium uppercase tracking-normal text-gray-500 dark:text-gray-400">{{ t('payment.admin.aggregation.columns.currency') }}</th>
                  <th class="px-4 py-3 text-right text-xs font-medium uppercase tracking-normal text-gray-500 dark:text-gray-400">{{ t('payment.admin.aggregation.columns.totalAmount') }}</th>
                  <th class="px-4 py-3 text-right text-xs font-medium uppercase tracking-normal text-gray-500 dark:text-gray-400">{{ t('payment.admin.aggregation.columns.orderCount') }}</th>
                  <th class="px-4 py-3 text-right text-xs font-medium uppercase tracking-normal text-gray-500 dark:text-gray-400">{{ t('payment.admin.aggregation.columns.averageAmount') }}</th>
                </tr>
              </thead>
              <tbody class="divide-y divide-gray-100 dark:divide-dark-800">
                <tr v-for="row in stats.users" :key="`${row.user_id}-${row.currency}`" class="transition-colors hover:bg-gray-50 dark:hover:bg-dark-800">
                  <td class="px-4 py-3">
                    <div class="flex min-w-0 items-center gap-3">
                      <span class="flex h-8 w-8 flex-shrink-0 items-center justify-center rounded-full bg-primary-50 text-xs font-semibold text-primary-700 dark:bg-primary-900/30 dark:text-primary-300">{{ initials(row) }}</span>
                      <div class="min-w-0">
                        <p class="truncate text-sm font-medium text-gray-900 dark:text-gray-100">{{ row.user_email || row.user_name || `#${row.user_id}` }}</p>
                        <p class="text-xs text-gray-500 dark:text-gray-400">#{{ row.user_id }}<span v-if="row.user_name && row.user_email" class="ml-1">· {{ row.user_name }}</span></p>
                      </div>
                    </div>
                  </td>
                  <td class="px-4 py-3 text-left"><span class="rounded bg-gray-100 px-2 py-1 text-xs font-medium text-gray-700 dark:bg-dark-800 dark:text-gray-300">{{ row.currency }}</span></td>
                  <td class="px-4 py-3 text-right text-sm font-semibold tabular-nums text-gray-900 dark:text-gray-100">{{ formatAmount(row.total_amount, row.currency) }}</td>
                  <td class="px-4 py-3 text-right text-sm tabular-nums text-gray-700 dark:text-gray-300">{{ formatNumber(row.order_count) }}</td>
                  <td class="px-4 py-3 text-right text-sm font-medium tabular-nums text-gray-900 dark:text-gray-100">{{ formatAmount(row.average_amount, row.currency) }}</td>
                </tr>
                <tr v-if="!stats.users.length">
                  <td colspan="5" class="px-4 py-12 text-center text-sm text-gray-500 dark:text-gray-400">{{ t('payment.admin.aggregation.noUsers') }}</td>
                </tr>
              </tbody>
            </table>
          </div>
          <Pagination
            v-if="stats.total > 0"
            :page="stats.page"
            :total="stats.total"
            :page-size="stats.page_size"
            @update:page="changePage"
            @update:pageSize="changePageSize"
          />
        </section>
      </template>

      <div v-else class="grid gap-3 sm:grid-cols-2 xl:grid-cols-4" aria-busy="true">
        <div v-for="index in 4" :key="index" class="h-28 animate-pulse rounded-lg bg-gray-100 dark:bg-dark-800" />
      </div>
    </div>
  </AppLayout>
</template>

<script setup lang="ts">
import { computed, onMounted, reactive, ref } from 'vue'
import { useI18n } from 'vue-i18n'
import type {
  AdminPaymentAggregationGranularity,
  AdminPaymentAggregationResponse,
  CurrencyAmounts,
} from '@/types/payment'
import { adminPaymentAPI } from '@/api/admin/payment'
import { extractI18nErrorMessage } from '@/utils/apiError'
import { useAppStore } from '@/stores/app'
import { formatPaymentAmount } from '@/components/payment/currency'
import AppLayout from '@/components/layout/AppLayout.vue'
import AdminPaymentAggregationChart from '@/components/admin/payment/AdminPaymentAggregationChart.vue'
import Icon from '@/components/icons/Icon.vue'
import Pagination from '@/components/common/Pagination.vue'
import Select from '@/components/common/Select.vue'
import DateRangePicker from '@/components/common/DateRangePicker.vue'

type RangeValidationError = 'required' | 'invalid' | 'reversed' | 'tooLong'

const { t, locale } = useI18n()
const appStore = useAppStore()

const shortcutDays = [7, 30, 90] as const
const initialRange = rangeForLastDays(30)
const draftFilters = reactive({
  user: '',
  startDate: initialRange.startDate,
  endDate: initialRange.endDate,
  status: '',
  granularity: 'day' as AdminPaymentAggregationGranularity,
})
const appliedRange = ref({ ...initialRange })
const appliedStatus = ref('')
const activeShortcut = ref<number | null>(30)
const stats = ref<AdminPaymentAggregationResponse | null>(null)
const loading = ref(false)
const error = ref(false)
const validationError = ref<RangeValidationError | null>(null)
const page = ref(1)
const pageSize = ref(20)
let requestGeneration = 0

const today = computed(() => formatLocalDate(new Date()))
const timezoneLabel = Intl.DateTimeFormat().resolvedOptions().timeZone || 'Local'

const statusOptions = computed(() => [
  { value: '', label: t('payment.admin.aggregation.statusLabel.paid') },
  { value: 'ALL', label: t('payment.admin.aggregation.statusLabel.ALL') },
  { value: 'PENDING', label: t('payment.status.pending') },
  { value: 'PAID', label: t('payment.status.paid') },
  { value: 'RECHARGING', label: t('payment.status.recharging') },
  { value: 'COMPLETED', label: t('payment.status.completed') },
  { value: 'EXPIRED', label: t('payment.status.expired') },
  { value: 'CANCELLED', label: t('payment.status.cancelled') },
  { value: 'FAILED', label: t('payment.status.failed') },
  { value: 'REFUND_REQUESTED', label: t('payment.status.refund_requested') },
  { value: 'REFUNDING', label: t('payment.status.refunding') },
  { value: 'REFUND_PENDING', label: t('payment.status.refund_pending') },
  { value: 'PARTIALLY_REFUNDED', label: t('payment.status.partially_refunded') },
  { value: 'REFUNDED', label: t('payment.status.refunded') },
  { value: 'REFUND_FAILED', label: t('payment.status.refund_failed') },
])

const granularityOptions = computed(() => [
  { value: 'day' as const, label: t('payment.admin.aggregation.granularity.day') },
  { value: 'week' as const, label: t('payment.admin.aggregation.granularity.week') },
  { value: 'month' as const, label: t('payment.admin.aggregation.granularity.month') },
])

const currentStatusLabel = computed(() => {
  if (!appliedStatus.value) return t('payment.admin.aggregation.statusLabel.paid')
  if (appliedStatus.value === 'ALL') return t('payment.admin.aggregation.statusLabel.ALL')
  return t(`payment.status.${appliedStatus.value.toLowerCase()}`)
})

onMounted(() => {
  void loadAggregation({ clearData: true })
})

function formatLocalDate(date: Date): string {
  const year = date.getFullYear()
  const month = String(date.getMonth() + 1).padStart(2, '0')
  const day = String(date.getDate()).padStart(2, '0')
  return `${year}-${month}-${day}`
}

function rangeForLastDays(days: number): { startDate: string; endDate: string } {
  const end = new Date()
  const start = new Date(end)
  start.setDate(start.getDate() - days + 1)
  return { startDate: formatLocalDate(start), endDate: formatLocalDate(end) }
}

function validateRange(startDate: string, endDate: string): RangeValidationError | null {
  if (!startDate || !endDate) return 'required'
  const start = Date.parse(`${startDate}T00:00:00`)
  const end = Date.parse(`${endDate}T00:00:00`)
  if (!Number.isFinite(start) || !Number.isFinite(end)) return 'invalid'
  if (end < start) return 'reversed'
  const calendarDays = Math.floor((end - start) / 86400000) + 1
  if (calendarDays > 366) return 'tooLong'
  return null
}

function selectShortcut(days: number): void {
  const range = rangeForLastDays(days)
  draftFilters.startDate = range.startDate
  draftFilters.endDate = range.endDate
  activeShortcut.value = days
  validationError.value = null
  page.value = 1
  void loadAggregation({ clearData: true, range })
}

function onDateRangeChange(range: { startDate: string; endDate: string }): void {
  draftFilters.startDate = range.startDate
  draftFilters.endDate = range.endDate
  activeShortcut.value = null
  validationError.value = validateRange(range.startDate, range.endDate)
  if (validationError.value) return
  page.value = 1
  void loadAggregation({ clearData: true, range })
}

function applyFilters(): void {
  const range = { startDate: draftFilters.startDate, endDate: draftFilters.endDate }
  validationError.value = validateRange(range.startDate, range.endDate)
  if (validationError.value) return
  activeShortcut.value = null
  page.value = 1
  void loadAggregation({ clearData: true, range })
}

function refresh(): void {
  void loadAggregation({ range: appliedRange.value })
}

async function loadAggregation(options: {
  clearData?: boolean
  range?: { startDate: string; endDate: string }
} = {}): Promise<void> {
  const generation = ++requestGeneration
  loading.value = true
  error.value = false
  if (options.clearData) stats.value = null

  const range = options.range || appliedRange.value
  const requestStatus = draftFilters.status
  try {
    const response = await adminPaymentAPI.getOrderAggregation({
      start_date: range.startDate,
      end_date: range.endDate,
      user: draftFilters.user.trim() || undefined,
      status: requestStatus || undefined,
      granularity: draftFilters.granularity,
      page: page.value,
      page_size: pageSize.value,
    })
    if (generation !== requestGeneration) return
    stats.value = response.data
    appliedStatus.value = requestStatus
    appliedRange.value = { startDate: response.data.start_date, endDate: response.data.end_date }
    draftFilters.startDate = response.data.start_date
    draftFilters.endDate = response.data.end_date
    page.value = response.data.page || page.value
    pageSize.value = response.data.page_size || pageSize.value
  } catch (err: unknown) {
    if (generation !== requestGeneration) return
    error.value = true
    appStore.showError(extractI18nErrorMessage(err, t, 'payment.errors', t('common.error')))
  } finally {
    if (generation === requestGeneration) loading.value = false
  }
}

function changePage(nextPage: number): void {
  page.value = nextPage
  void loadAggregation()
}

function changePageSize(nextPageSize: number): void {
  pageSize.value = nextPageSize
  page.value = 1
  void loadAggregation()
}

function sortedAmounts(amounts: CurrencyAmounts): [string, number][] {
  return Object.entries(amounts || {}).sort(([left], [right]) => left.localeCompare(right))
}

function formatAmount(amount: number, currency: string): string {
  return formatPaymentAmount(amount, currency, locale.value)
}

function formatNumber(value: number): string {
  return new Intl.NumberFormat(locale.value).format(value || 0)
}

function initials(row: { user_email?: string; user_name?: string; user_id: number }): string {
  const label = row.user_name || row.user_email || String(row.user_id)
  const parts = label.trim().split(/\s+/).filter(Boolean)
  if (parts.length > 1) return `${parts[0][0]}${parts[1][0]}`.toUpperCase()
  return label.slice(0, 2).toUpperCase()
}
</script>
