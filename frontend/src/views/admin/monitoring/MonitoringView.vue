<script setup lang="ts">
import { computed, onMounted, onUnmounted, ref, watch } from 'vue'
import { useI18n } from 'vue-i18n'
import { useRoute, useRouter } from 'vue-router'
import AppLayout from '@/components/layout/AppLayout.vue'
import Icon from '@/components/icons/Icon.vue'
import Select from '@/components/common/Select.vue'
import EmptyState from '@/components/common/EmptyState.vue'
import monitoringAPI, {
  performanceMetricsFromCounters,
  performanceMetricsFromTimePoint,
  type FirstTokenTimeoutSettings,
  type FirstTokenTimeoutSettingsValue,
  type MonitoringOverview,
  type MonitoringRange,
  type PerformanceAccountItem,
  type PerformanceAccountPage,
  type PerformanceInvestigation,
  type PerformanceOrder
} from '@/api/admin/monitoring'
import MetricTrendCard from './components/MetricTrendCard.vue'
import ProtectionFunnel from './components/ProtectionFunnel.vue'
import MonitoringTrendChart, { type PerformanceSeriesDefinition } from './components/MonitoringTrendChart.vue'
import FailureDistribution from './components/FailureDistribution.vue'
import AccountHealthTable from './components/AccountHealthTable.vue'
import InvestigationDrawer from './components/InvestigationDrawer.vue'
import TTFTSettingsDialog from './components/TTFTSettingsDialog.vue'

const { t } = useI18n()
const route = useRoute()
const router = useRouter()
const ranges: MonitoringRange[] = ['1h', '6h', '24h', '7d', '30d', '90d']

const range = ref<MonitoringRange>('24h')
const platform = ref('')
const model = ref('')
const settings = ref<FirstTokenTimeoutSettings | null>(null)
const overview = ref<MonitoringOverview | null>(null)
const accounts = ref<PerformanceAccountPage | null>(null)
const investigation = ref<PerformanceInvestigation | null>(null)
const selectedAccount = ref<PerformanceAccountItem | null>(null)
const settingsLoading = ref(true)
const overviewLoading = ref(true)
const accountsLoading = ref(true)
const investigationLoading = ref(false)
const settingsSaving = ref(false)
const settingsOpen = ref(false)
const settingsError = ref('')
const overviewError = ref('')
const accountsError = ref('')
const investigationError = ref('')
const hasOverviewLoaded = ref(false)
const hasAccountsLoaded = ref(false)
const accountSearch = ref('')
const accountSort = ref('health_score')
const accountOrder = ref<PerformanceOrder>('asc')
const accountPage = ref(1)
const accountPageSize = 20
let overviewGeneration = 0
let accountsGeneration = 0
let investigationGeneration = 0
let syncingRouteQuery = false
let accountSearchTimer: ReturnType<typeof setTimeout> | undefined

const pageFilters = computed(() => ({ range: range.value, platform: platform.value || undefined, model: model.value.trim() || undefined }))
const degradedHealth = computed(() => {
  const perfHealth = overview.value?.performance.collection_health
  if (perfHealth?.status === 'degraded') return perfHealth
  const ttftHealth = overview.value?.ttft.completeness
  return ttftHealth?.status === 'degraded' ? ttftHealth : null
})
const degraded = computed(() => degradedHealth.value !== null)
const hasSamples = computed(() => (overview.value?.performance.summary.attempts ?? 0) > 0)
const coverage = computed(() => {
  const perf = overview.value?.performance
  if (!perf) return ''
  return t('admin.monitoring.coverage', { start: formatDate(perf.coverage_start), end: formatDate(perf.coverage_end) })
})
const protectionLabel = computed(() => {
  const effective = settings.value?.effective
  if (!effective) return ''
  return effective.enabled
    ? t('admin.monitoring.protection.enabled', { seconds: effective.timeout_seconds })
    : t('admin.monitoring.protection.disabled')
})
const ttftTimeoutRate = computed(() => {
  const summary = overview.value?.performance.summary
  if (!summary || summary.availability.denominator <= 0) return 0
  return summary.ttft_timeout_count / summary.availability.denominator
})
const trendMetrics = computed(() => overview.value?.performance.trend.map(performanceMetricsFromTimePoint) ?? [])
const kpiCards = computed(() => {
  const perf = overview.value?.performance
  if (!perf) return []
  const summary = perf.summary
  const recovery = overview.value?.ttft.summary.recovery_rate
  return [
    { label: t('admin.monitoring.kpi.availability'), value: percent(summary.availability.rate), context: t('admin.monitoring.kpi.requestsContext', { count: summary.attempts.toLocaleString() }), tone: 'success' as const, trend: trendMetrics.value.map((m) => m.availability) },
    { label: t('admin.monitoring.kpi.failureRate'), value: percent(summary.failure_rate.rate), context: t('admin.monitoring.kpi.ratioContext', { numerator: summary.failure_rate.numerator, denominator: summary.failure_rate.denominator }), tone: 'danger' as const, trend: trendMetrics.value.map((m) => m.failure_rate) },
    { label: t('admin.monitoring.kpi.ttftTimeoutRate'), value: percent(ttftTimeoutRate.value), context: t('admin.monitoring.kpi.timeoutsContext', { count: summary.ttft_timeout_count }), tone: 'warning' as const, trend: trendMetrics.value.map((m) => m.ttft_timeout_rate) },
    { label: t('admin.monitoring.kpi.recoveryRate'), value: percent(recovery?.rate ?? 0), context: t('admin.monitoring.kpi.ratioContext', { numerator: recovery?.numerator ?? 0, denominator: recovery?.denominator ?? 0 }), tone: 'success' as const, trend: overview.value?.ttft.trend.map((point) => point.recovery_rate.rate) ?? [] },
    { label: t('admin.monitoring.kpi.p95Ttft'), value: milliseconds(summary.p95_ttft_ms), context: t('admin.monitoring.kpi.p95TtftContext', { p50: milliseconds(summary.p50_ttft_ms), duration: milliseconds(summary.p95_duration_ms) }), tone: 'neutral' as const, trend: trendMetrics.value.map((m) => m.p95_ttft_ms) }
  ]
})

const ratesSeries: PerformanceSeriesDefinition[] = [
  { label: t('admin.monitoring.trends.availability'), color: '#10b981', selector: (point) => performanceMetricsFromCounters(point.counters).availability, formatter: percent },
  { label: t('admin.monitoring.trends.failureRate'), color: '#ef4444', selector: (point) => performanceMetricsFromCounters(point.counters).failure_rate, formatter: percent, fill: false },
  { label: t('admin.monitoring.trends.ttftTimeoutRate'), color: '#f59e0b', selector: (point) => performanceMetricsFromCounters(point.counters).ttft_timeout_rate, formatter: percent, fill: false }
]
const latencySeries: PerformanceSeriesDefinition[] = [
  { label: t('admin.monitoring.trends.p50Ttft'), color: '#0ea5e9', selector: (point) => performanceMetricsFromTimePoint(point).p50_ttft_ms, formatter: milliseconds },
  { label: t('admin.monitoring.trends.p95Ttft'), color: '#8b5cf6', selector: (point) => performanceMetricsFromTimePoint(point).p95_ttft_ms, formatter: milliseconds, fill: false },
  { label: t('admin.monitoring.trends.p95Duration'), color: '#f97316', selector: (point) => performanceMetricsFromTimePoint(point).p95_duration_ms, formatter: milliseconds, fill: false }
]

const FAILURE_COLORS: Record<string, string> = { ttft_timeout: '#ef4444', rate_limit: '#f97316' }
const failureItems = computed(() => {
  const totals = new Map<string, number>()
  for (const point of overview.value?.performance.trend ?? []) {
    const counters = point.counters
    totals.set('ttft_timeout', (totals.get('ttft_timeout') ?? 0) + counters.ttft_timeout_count)
    totals.set('rate_limit', (totals.get('rate_limit') ?? 0) + counters.rate_limit_count)
    totals.set('auth', (totals.get('auth') ?? 0) + counters.auth_count)
    totals.set('upstream_4xx', (totals.get('upstream_4xx') ?? 0) + counters.upstream_4xx_count)
    totals.set('upstream_5xx', (totals.get('upstream_5xx') ?? 0) + counters.upstream_5xx_count)
    totals.set('transport', (totals.get('transport') ?? 0) + counters.transport_count)
    totals.set('protocol', (totals.get('protocol') ?? 0) + counters.protocol_count)
    totals.set('other_failure', (totals.get('other_failure') ?? 0) + counters.other_failure_count)
  }
  return [...totals.entries()].map(([key, count]) => ({ label: t(`admin.monitoring.failures.outcomes.${key}`), count, color: FAILURE_COLORS[key] ?? '#64748b' }))
})

const platformOptions = computed(() => [
  { value: '', label: t('admin.monitoring.filters.allPlatforms') },
  ...['anthropic', 'openai', 'gemini', 'antigravity', 'grok'].map((value) => ({ value, label: value }))
])

function percent(value: number) { return `${(value * 100).toFixed(2)}%` }
function milliseconds(value: number) { return value > 0 ? `${Math.round(value).toLocaleString()} ms` : '--' }
function formatDate(value: string) {
  const date = new Date(value)
  return Number.isNaN(date.getTime()) ? '--' : date.toLocaleString([], { month: 'numeric', day: 'numeric', hour: '2-digit', minute: '2-digit' })
}

function readQuery() {
  const query = route.query
  range.value = ranges.includes(query.range as MonitoringRange) ? (query.range as MonitoringRange) : '24h'
  platform.value = typeof query.platform === 'string' ? query.platform : ''
  model.value = typeof query.model === 'string' ? query.model : ''
}

async function syncQuery() {
  const query: Record<string, string> = { range: range.value }
  if (platform.value) query.platform = platform.value
  if (model.value.trim()) query.model = model.value.trim()
  syncingRouteQuery = true
  try { await router.replace({ query }) } finally { syncingRouteQuery = false }
}

async function loadSettings() {
  settingsLoading.value = !settings.value
  settingsError.value = ''
  try { settings.value = await monitoringAPI.getSettings() } catch { settingsError.value = t('admin.monitoring.errors.settings') } finally { settingsLoading.value = false }
}

async function loadOverview() {
  const generation = ++overviewGeneration
  overviewLoading.value = !hasOverviewLoaded.value
  overviewError.value = ''
  try {
    const response = await monitoringAPI.getOverview(pageFilters.value)
    if (generation !== overviewGeneration) return
    overview.value = response
    hasOverviewLoaded.value = true
  } catch {
    if (generation !== overviewGeneration) return
    overviewError.value = t('admin.monitoring.errors.overview')
  } finally {
    if (generation === overviewGeneration) overviewLoading.value = false
  }
}

async function loadAccounts() {
  const generation = ++accountsGeneration
  accountsLoading.value = !hasAccountsLoaded.value
  accountsError.value = ''
  try {
    const response = await monitoringAPI.getAccounts({
      ...pageFilters.value,
      search: accountSearch.value.trim() || undefined,
      sort: accountSort.value,
      order: accountOrder.value,
      page: accountPage.value,
      page_size: accountPageSize
    })
    if (generation !== accountsGeneration) return
    accounts.value = response
    hasAccountsLoaded.value = true
  } catch {
    if (generation !== accountsGeneration) return
    accountsError.value = t('admin.monitoring.errors.accounts')
  } finally {
    if (generation === accountsGeneration) accountsLoading.value = false
  }
}

async function loadInvestigation() {
  if (!selectedAccount.value) return
  const generation = ++investigationGeneration
  investigationLoading.value = true
  investigationError.value = ''
  investigation.value = null
  try {
    const response = await monitoringAPI.getInvestigation({ ...pageFilters.value, account_id: selectedAccount.value.account_id })
    if (generation !== investigationGeneration) return
    investigation.value = response
  } catch {
    if (generation !== investigationGeneration) return
    investigationError.value = t('admin.monitoring.errors.investigation')
  } finally {
    if (generation === investigationGeneration) investigationLoading.value = false
  }
}

async function refreshAll() { await Promise.all([loadOverview(), loadAccounts()]) }

async function saveSettings(payload: FirstTokenTimeoutSettingsValue) {
  settingsSaving.value = true
  settingsError.value = ''
  try {
    settings.value = await monitoringAPI.updateSettings(payload)
    settingsOpen.value = false
  } catch {
    settingsError.value = t('admin.monitoring.errors.settings')
  } finally {
    settingsSaving.value = false
  }
}

async function changeGlobalFilters() {
  accountPage.value = 1
  closeInvestigation()
  await syncQuery()
  await refreshAll()
}

function changeAccountSort(sort: string) {
  if (accountSort.value === sort) accountOrder.value = accountOrder.value === 'asc' ? 'desc' : 'asc'
  else { accountSort.value = sort; accountOrder.value = 'asc' }
  accountPage.value = 1
  void loadAccounts()
}

function selectAccount(account: PerformanceAccountItem) {
  selectedAccount.value = account
  investigation.value = null
  investigationError.value = ''
  void loadInvestigation()
}

function closeInvestigation() {
  investigationGeneration++
  selectedAccount.value = null
  investigation.value = null
  investigationError.value = ''
  investigationLoading.value = false
}

watch(accountSearch, () => {
  if (accountSearchTimer) clearTimeout(accountSearchTimer)
  accountSearchTimer = setTimeout(() => {
    accountSearchTimer = undefined
    accountPage.value = 1
    void loadAccounts()
  }, 300)
})

watch(
  () => route.query,
  async () => {
    if (syncingRouteQuery) return
    readQuery()
    accountPage.value = 1
    closeInvestigation()
    await refreshAll()
  },
  { deep: true }
)

onMounted(async () => { readQuery(); await syncQuery(); await Promise.all([loadSettings(), refreshAll()]) })
onUnmounted(() => {
  if (accountSearchTimer) clearTimeout(accountSearchTimer)
  overviewGeneration++
  accountsGeneration++
  investigationGeneration++
})
</script>

<template>
  <AppLayout>
    <main class="mx-auto min-w-0 max-w-7xl space-y-6 px-4 py-6 sm:px-6">
      <header class="flex flex-col gap-4 border-b border-gray-200 pb-5 dark:border-dark-700 xl:flex-row xl:items-end xl:justify-between">
        <div class="min-w-0">
          <div class="flex flex-wrap items-center gap-2">
            <h1 class="text-2xl font-semibold text-gray-900 dark:text-white">{{ t('admin.monitoring.title') }}</h1>
            <span
              class="inline-flex items-center rounded-md border px-2.5 py-1 text-xs font-medium"
              :class="degraded ? 'border-amber-300 bg-amber-50 text-amber-800 dark:border-amber-800 dark:bg-amber-500/10 dark:text-amber-200' : 'border-emerald-300 bg-emerald-50 text-emerald-800 dark:border-emerald-800 dark:bg-emerald-500/10 dark:text-emerald-200'"
            >{{ degraded ? t('admin.monitoring.health.degraded') : t('admin.monitoring.health.complete') }}</span>
            <button
              v-if="settings"
              data-testid="protection-badge"
              type="button"
              class="inline-flex items-center gap-1 rounded-md border border-primary-300 bg-primary-50 px-2.5 py-1 text-xs font-medium text-primary-700 hover:bg-primary-100 dark:border-primary-800 dark:bg-primary-500/10 dark:text-primary-300 dark:hover:bg-primary-500/20"
              @click="settingsOpen = true"
            >{{ protectionLabel }} · {{ t('admin.monitoring.protection.adjust') }}</button>
          </div>
          <p class="mt-2 text-sm text-gray-500 dark:text-gray-400">{{ t('admin.monitoring.description') }}</p>
          <p v-if="coverage" class="mt-1 text-xs text-gray-400 dark:text-gray-500">{{ coverage }}</p>
        </div>
        <div class="flex flex-col gap-2 sm:flex-row sm:items-end">
          <div class="flex flex-wrap rounded-md border border-gray-300 p-1 dark:border-dark-600" role="group" :aria-label="t('admin.monitoring.filters.range')">
            <button
              v-for="item in ranges"
              :key="item"
              type="button"
              class="h-8 rounded px-2.5 text-xs font-medium focus:outline-none focus-visible:ring-2 focus-visible:ring-primary-500"
              :class="range === item ? 'bg-primary-600 text-white' : 'text-gray-600 hover:bg-gray-100 dark:text-gray-300 dark:hover:bg-dark-700'"
              @click="range = item; changeGlobalFilters()"
            >{{ item }}</button>
          </div>
          <label class="grid gap-1 text-xs text-gray-500 dark:text-gray-400">
            <span>{{ t('admin.monitoring.filters.platform') }}</span>
            <Select v-model="platform" :options="platformOptions" class="sm:w-40" @change="changeGlobalFilters" />
          </label>
          <label class="grid gap-1 text-xs text-gray-500 dark:text-gray-400">
            <span>{{ t('admin.monitoring.filters.model') }}</span>
            <input v-model="model" :placeholder="t('admin.monitoring.filters.modelPlaceholder')" class="h-10 min-w-36 rounded-md border border-gray-300 bg-white px-3 text-sm text-gray-900 outline-none focus:border-primary-500 focus:ring-2 focus:ring-primary-500/20 dark:border-dark-600 dark:bg-dark-900 dark:text-white" @change="changeGlobalFilters" />
          </label>
          <button type="button" class="flex h-10 w-10 items-center justify-center rounded-md border border-gray-300 text-gray-600 hover:bg-gray-100 focus:outline-none focus-visible:ring-2 focus-visible:ring-primary-500 dark:border-dark-600 dark:text-gray-300 dark:hover:bg-dark-700" :aria-label="t('common.refresh')" :title="t('common.refresh')" @click="refreshAll">
            <Icon name="refresh" size="md" aria-hidden="true" />
          </button>
        </div>
      </header>

      <aside v-if="degraded && degradedHealth" class="border-l-4 border-amber-500 bg-amber-50 px-4 py-3 text-sm text-amber-900 dark:bg-amber-500/10 dark:text-amber-200">
        {{ t('admin.monitoring.degradedBanner', { dropped: degradedHealth.dropped_samples, pending: degradedHealth.pending_samples }) }}
      </aside>

      <section v-if="overviewLoading && !hasOverviewLoaded" data-testid="monitoring-skeleton" class="grid grid-cols-2 gap-3 lg:grid-cols-3 xl:grid-cols-5" aria-label="loading">
        <div v-for="item in 5" :key="item" class="h-32 animate-pulse rounded-lg bg-gray-100 dark:bg-dark-800" />
      </section>
      <section v-else-if="overviewError && !overview" class="flex flex-wrap items-center gap-3 border-l-4 border-red-500 bg-red-50 px-4 py-3 text-sm text-red-800 dark:bg-red-500/10 dark:text-red-200">
        <span>{{ overviewError }}</span>
        <button data-testid="monitoring-overview-retry" type="button" class="min-h-9 rounded-md border border-current px-3 py-1.5 font-medium" @click="loadOverview">{{ t('common.refresh') }}</button>
      </section>
      <template v-else-if="overview">
        <EmptyState v-if="!hasSamples" :title="t('admin.monitoring.empty.title')" :description="t('admin.monitoring.empty.description')" />
        <template v-else>
          <section class="grid grid-cols-2 gap-3 lg:grid-cols-3 xl:grid-cols-5" :aria-label="t('admin.monitoring.title')">
            <MetricTrendCard v-for="card in kpiCards" :key="card.label" v-bind="card" />
          </section>
          <ProtectionFunnel :summary="overview.ttft.summary" />
          <section class="grid grid-cols-1 gap-6 xl:grid-cols-2" :aria-label="t('admin.monitoring.trends.rates')">
            <MonitoringTrendChart :title="t('admin.monitoring.trends.rates')" :points="overview.performance.trend" :time-range="range" :series="ratesSeries" :loading="overviewLoading" value-format="percent" />
            <MonitoringTrendChart :title="t('admin.monitoring.trends.latency')" :points="overview.performance.trend" :time-range="range" :series="latencySeries" :loading="overviewLoading" />
          </section>
        </template>
      </template>

      <AccountHealthTable
        :page="accounts"
        :loading="accountsLoading"
        :error="accountsError"
        :sort="accountSort"
        :order="accountOrder"
        :search="accountSearch"
        @update:search="(value: string) => (accountSearch = value)"
        @retry="loadAccounts"
        @sort="changeAccountSort"
        @page="(value: number) => { accountPage = value; loadAccounts() }"
        @select="selectAccount"
      />

      <FailureDistribution v-if="overview && hasSamples" :failures="failureItems" :title="t('admin.monitoring.failures.title')" :loading="overviewLoading" />
    </main>
    <InvestigationDrawer :open="Boolean(selectedAccount)" :account="selectedAccount" :investigation="investigation" :loading="investigationLoading" :error="investigationError" @close="closeInvestigation" @retry="loadInvestigation" />
    <TTFTSettingsDialog :open="settingsOpen" :settings="settings" :saving="settingsSaving" :error="settingsError" @close="settingsOpen = false" @save="saveSettings" />
  </AppLayout>
</template>
