<script setup lang="ts">
import { computed, onMounted, onUnmounted, ref, watch } from 'vue'
import { useRoute, useRouter } from 'vue-router'
import AppLayout from '@/components/layout/AppLayout.vue'
import Icon from '@/components/icons/Icon.vue'
import performanceAPI, {
  performanceMetricsFromCounters,
  type PerformanceAccountItem,
  type PerformanceAccountPage,
  type PerformanceInvestigation,
  type PerformanceOrder,
  type PerformanceOverview,
  type PerformanceRange
} from '@/api/admin/performance'
import PerformanceAccountTable from './components/PerformanceAccountTable.vue'
import PerformanceFailureDistribution from './components/PerformanceFailureDistribution.vue'
import PerformanceInvestigationDrawer from './components/PerformanceInvestigationDrawer.vue'
import PerformanceMetricCard from './components/PerformanceMetricCard.vue'
import PerformanceTrendChart, { type PerformanceSeriesDefinition } from './components/PerformanceTrendChart.vue'

const route = useRoute()
const router = useRouter()
const ranges: PerformanceRange[] = ['1h', '6h', '24h', '7d', '30d', '90d']

const range = ref<PerformanceRange>('24h')
const platform = ref('')
const overview = ref<PerformanceOverview | null>(null)
const accounts = ref<PerformanceAccountPage | null>(null)
const investigation = ref<PerformanceInvestigation | null>(null)
const selectedAccount = ref<PerformanceAccountItem | null>(null)
const overviewLoading = ref(true)
const accountsLoading = ref(true)
const investigationLoading = ref(false)
const overviewError = ref('')
const accountsError = ref('')
const investigationError = ref('')
const hasOverviewLoaded = ref(false)
const hasAccountsLoaded = ref(false)
const refreshing = ref(false)
const accountSort = ref('health_score')
const accountOrder = ref<PerformanceOrder>('asc')
const accountPage = ref(1)
const accountPageSize = 20
let overviewGeneration = 0
let accountsGeneration = 0
let investigationGeneration = 0
let filterGeneration = 0
let loadedFilterGeneration = -1
const internalRouteWrites = new Map<string, number>()

const pageFilters = computed(() => ({ range: range.value, platform: platform.value.trim() || undefined }))
const accountParams = computed(() => ({
  ...pageFilters.value,
  sort: accountSort.value,
  order: accountOrder.value,
  page: accountPage.value,
  page_size: accountPageSize
}))
const overviewMetrics = computed(() => overview.value ? {
  availability: overview.value.summary.availability.rate,
  failureRate: overview.value.summary.failure_rate.rate,
  ttftTimeoutRate: overview.value.summary.availability.denominator > 0
    ? overview.value.summary.ttft_timeout_count / overview.value.summary.availability.denominator
    : 0,
  p50TTFT: overview.value.summary.p50_ttft_ms,
  p95TTFT: overview.value.summary.p95_ttft_ms,
  p95Duration: overview.value.summary.p95_duration_ms
} : null)
const trendMetrics = computed(() => overview.value?.trend.map((point) => performanceMetricsFromCounters(point.counters)) ?? [])
const degraded = computed(() => overview.value?.collection_health.status === 'degraded')
const coverage = computed(() => overview.value ? `${formatDate(overview.value.coverage_start)} 至 ${formatDate(overview.value.coverage_end)}` : '--')
const failureDistribution = computed(() => {
  const totals = overview.value?.trend.reduce<Record<string, number>>((result, point) => {
    const counters = point.counters
    result.ttft_timeout += counters.ttft_timeout_count
    result.rate_limit += counters.rate_limit_count
    result.auth += counters.auth_count
    result.upstream_4xx += counters.upstream_4xx_count
    result.upstream_5xx += counters.upstream_5xx_count
    result.transport += counters.transport_count
    result.protocol += counters.protocol_count
    result.other_failure += counters.other_failure_count
    return result
  }, { ttft_timeout: 0, rate_limit: 0, auth: 0, upstream_4xx: 0, upstream_5xx: 0, transport: 0, protocol: 0, other_failure: 0 }) ?? {}
  return Object.entries(totals).map(([Outcome, Count]) => ({ Outcome, Count }))
})

const ratesSeries: PerformanceSeriesDefinition[] = [
  { label: '可用率', color: '#10b981', selector: (point) => performanceMetricsFromCounters(point.counters).availability * 100, formatter: percent },
  { label: '失败率', color: '#ef4444', selector: (point) => performanceMetricsFromCounters(point.counters).failure_rate * 100, formatter: percent, fill: false },
  { label: 'TTFT 超时率', color: '#f59e0b', selector: (point) => performanceMetricsFromCounters(point.counters).ttft_timeout_rate * 100, formatter: percent, fill: false },
  { label: '切换率', color: '#6366f1', selector: (point) => performanceMetricsFromCounters(point.counters).failover_rate * 100, formatter: percent, fill: false }
]
const latencySeries: PerformanceSeriesDefinition[] = [
  { label: 'P50 TTFT', color: '#0ea5e9', selector: (point) => performanceMetricsFromCounters(point.counters).p50_ttft_ms, formatter: milliseconds },
  { label: 'P95 TTFT', color: '#8b5cf6', selector: (point) => performanceMetricsFromCounters(point.counters).p95_ttft_ms, formatter: milliseconds, fill: false },
  { label: 'P95 总耗时', color: '#f97316', selector: (point) => performanceMetricsFromCounters(point.counters).p95_duration_ms, formatter: milliseconds, fill: false }
]

function percent(value: number) { return `${(value * 100).toFixed(2)}%` }
function milliseconds(value: number) { return value > 0 ? `${Math.round(value).toLocaleString()} ms` : '--' }
function formatDate(value: string) {
  const date = new Date(value)
  return Number.isNaN(date.getTime()) ? '--' : date.toLocaleString([], { month: 'numeric', day: 'numeric', hour: '2-digit', minute: '2-digit' })
}
function healthLabel() {
  if (!overview.value) return '等待采集'
  return degraded.value ? '采集降级' : '采集完整'
}
function healthClasses() {
  return degraded.value
    ? 'border-amber-300 bg-amber-50 text-amber-800 dark:border-amber-800 dark:bg-amber-500/10 dark:text-amber-200'
    : 'border-emerald-300 bg-emerald-50 text-emerald-800 dark:border-emerald-800 dark:bg-emerald-500/10 dark:text-emerald-200'
}

function readQuery() {
  range.value = ranges.includes(route.query.range as PerformanceRange) ? route.query.range as PerformanceRange : '24h'
  platform.value = typeof route.query.platform === 'string' ? route.query.platform : ''
}

function pageQuery() {
  const query: Record<string, string> = { range: range.value }
  if (platform.value.trim()) query.platform = platform.value.trim()
  return query
}

function querySignature(query: Record<string, unknown>) {
  return `${query.range ?? '24h'}\u0000${query.platform ?? ''}`
}

function rememberInternalRouteWrite(query: Record<string, string>) {
  const signature = querySignature(query)
  internalRouteWrites.set(signature, (internalRouteWrites.get(signature) ?? 0) + 1)
}

function discardInternalRouteWrite(query: Record<string, string>) {
  const signature = querySignature(query)
  const count = internalRouteWrites.get(signature) ?? 0
  if (count <= 1) internalRouteWrites.delete(signature)
  else internalRouteWrites.set(signature, count - 1)
}

function consumeInternalRouteWrite(query: Record<string, unknown>) {
  const signature = querySignature(query)
  const count = internalRouteWrites.get(signature) ?? 0
  if (!count) return false
  if (count === 1) internalRouteWrites.delete(signature)
  else internalRouteWrites.set(signature, count - 1)
  return true
}

function routeMatchesPageFilters() {
  return querySignature(route.query) === querySignature(pageQuery())
}

function routeMatchesQuery(query: Record<string, string>) {
  return querySignature(route.query) === querySignature(query)
}

async function refreshFiltersOnce(generation: number) {
  if (generation !== filterGeneration || generation === loadedFilterGeneration) return
  loadedFilterGeneration = generation
  await refreshAll()
}

async function syncQuery(generation = filterGeneration) {
  const query = pageQuery()
  const tracksRouteWrite = !routeMatchesQuery(query)
  if (tracksRouteWrite) rememberInternalRouteWrite(query)
  try {
    await router.replace({ query })
  } catch {
    if (tracksRouteWrite) discardInternalRouteWrite(query)
    return
  }
  if (tracksRouteWrite && !routeMatchesQuery(query)) discardInternalRouteWrite(query)
  if (generation !== filterGeneration) {
    if (!routeMatchesPageFilters()) void syncQuery(filterGeneration)
    return
  }
  await refreshFiltersOnce(generation)
}

async function loadOverview() {
  const generation = ++overviewGeneration
  overviewLoading.value = !hasOverviewLoaded.value
  overviewError.value = ''
  try {
    const response = await performanceAPI.getOverview(pageFilters.value)
    if (generation !== overviewGeneration) return
    overview.value = response
    hasOverviewLoaded.value = true
  } catch {
    if (generation !== overviewGeneration) return
    overviewError.value = '无法加载性能概览'
  } finally {
    if (generation === overviewGeneration) overviewLoading.value = false
  }
}

async function loadAccounts() {
  const generation = ++accountsGeneration
  accountsLoading.value = !hasAccountsLoaded.value
  accountsError.value = ''
  try {
    const response = await performanceAPI.getAccounts(accountParams.value)
    if (generation !== accountsGeneration) return
    accounts.value = response
    hasAccountsLoaded.value = true
  } catch {
    if (generation !== accountsGeneration) return
    accountsError.value = '无法加载账号性能数据'
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
  const accountID = selectedAccount.value.account_id
  try {
    const response = await performanceAPI.getInvestigation({ ...pageFilters.value, account_id: accountID })
    if (generation !== investigationGeneration) return
    investigation.value = response
  } catch {
    if (generation !== investigationGeneration) return
    investigationError.value = '无法加载账号性能详情'
  } finally {
    if (generation === investigationGeneration) investigationLoading.value = false
  }
}

async function refreshAll() { await Promise.all([loadOverview(), loadAccounts()]) }

async function manualRefresh() {
  refreshing.value = true
  try { await refreshAll() } finally { refreshing.value = false }
}

async function changePageFilters() {
  const generation = ++filterGeneration
  accountPage.value = 1
  closeInvestigation()
  await syncQuery(generation)
}

function changeSort(sort: string) {
  if (accountSort.value === sort) accountOrder.value = accountOrder.value === 'asc' ? 'desc' : 'asc'
  else { accountSort.value = sort; accountOrder.value = 'asc' }
  accountPage.value = 1
  void loadAccounts()
}

function changePage(page: number) {
  accountPage.value = Math.max(1, page)
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

watch(() => route.query, async () => {
  if (consumeInternalRouteWrite(route.query)) return
  readQuery()
  const generation = ++filterGeneration
  loadedFilterGeneration = -1
  accountPage.value = 1
  closeInvestigation()
  await refreshFiltersOnce(generation)
}, { deep: true })

onMounted(async () => {
  readQuery()
  await syncQuery(filterGeneration)
})

onUnmounted(() => {
  overviewGeneration++
  accountsGeneration++
  investigationGeneration++
})
</script>

<template>
  <AppLayout>
    <main class="mx-auto min-w-0 max-w-7xl space-y-7 px-4 py-6 sm:px-6 lg:py-8">
      <header class="flex flex-col gap-5 border-b border-gray-200 pb-5 dark:border-dark-700 xl:flex-row xl:items-end xl:justify-between">
        <div class="min-w-0">
          <div class="flex flex-wrap items-center gap-3">
            <h1 class="text-2xl font-semibold text-gray-900 dark:text-white">账号性能</h1>
            <span class="inline-flex items-center rounded-md border px-2.5 py-1 text-xs font-medium" :class="healthClasses()">{{ healthLabel() }}</span>
          </div>
          <p class="mt-2 text-sm text-gray-500 dark:text-gray-400">追踪请求健康度、首字节延迟与账号级异常。</p>
          <p class="mt-1 text-xs text-gray-400 dark:text-gray-500">数据覆盖 {{ coverage }}</p>
        </div>
        <div class="flex flex-col gap-3 sm:flex-row sm:items-end">
          <div class="flex flex-wrap rounded-md border border-gray-300 p-1 dark:border-dark-600" role="group" aria-label="时间范围">
            <button v-for="item in ranges" :key="item" type="button" class="h-8 rounded px-2.5 text-xs font-medium focus:outline-none focus-visible:ring-2 focus-visible:ring-primary-500" :class="range === item ? 'bg-primary-600 text-white' : 'text-gray-600 hover:bg-gray-100 dark:text-gray-300 dark:hover:bg-dark-700'" @click="range = item; changePageFilters()">{{ item }}</button>
          </div>
          <label class="grid gap-1 text-xs text-gray-500 dark:text-gray-400">
            <span>平台</span>
            <input v-model.trim="platform" class="h-10 w-full min-w-36 rounded-md border border-gray-300 bg-white px-3 text-sm text-gray-900 outline-none focus:border-primary-500 focus:ring-2 focus:ring-primary-500/20 dark:border-dark-600 dark:bg-dark-900 dark:text-white sm:w-40" placeholder="全部平台" @change="changePageFilters" />
          </label>
          <button type="button" class="flex h-10 w-10 items-center justify-center rounded-md border border-gray-300 text-gray-600 hover:bg-gray-100 disabled:cursor-wait disabled:opacity-50 focus:outline-none focus-visible:ring-2 focus-visible:ring-primary-500 dark:border-dark-600 dark:text-gray-300 dark:hover:bg-dark-700" aria-label="刷新性能数据" title="刷新性能数据" :disabled="refreshing" @click="manualRefresh">
            <Icon name="refresh" size="md" aria-hidden="true" />
          </button>
        </div>
      </header>

      <aside v-if="degraded" class="border-l-4 border-amber-500 bg-amber-50 px-4 py-3 text-sm text-amber-900 dark:bg-amber-500/10 dark:text-amber-200">
        采集器当前降级，指标可能不完整。丢弃 {{ overview?.collection_health.dropped_samples ?? 0 }}，待写入 {{ overview?.collection_health.pending_samples ?? 0 }}。
      </aside>

      <section v-if="overviewLoading && !hasOverviewLoaded" class="grid grid-cols-1 gap-3 sm:grid-cols-2 xl:grid-cols-5" aria-label="正在加载性能概览">
        <div v-for="item in 5" :key="item" class="h-36 animate-pulse rounded-lg bg-gray-100 dark:bg-dark-800" />
      </section>
      <section v-else-if="overviewError && !overview" class="flex flex-wrap items-center gap-3 border-l-4 border-red-500 bg-red-50 px-4 py-3 text-sm text-red-800 dark:bg-red-500/10 dark:text-red-200">
        <span>{{ overviewError }}</span>
        <button data-testid="performance-overview-retry" type="button" class="min-h-9 rounded-md border border-current px-3 py-1.5 font-medium" @click="loadOverview">重试</button>
      </section>
      <template v-else-if="overview && overviewMetrics">
        <section v-if="overviewError" class="flex flex-wrap items-center gap-3 border-l-4 border-red-500 bg-red-50 px-4 py-3 text-sm text-red-800 dark:bg-red-500/10 dark:text-red-200">
          <span>{{ overviewError }}</span>
          <button data-testid="performance-overview-retry" type="button" class="min-h-9 rounded-md border border-current px-3 py-1.5 font-medium" @click="loadOverview">重试</button>
        </section>

        <section class="grid grid-cols-1 gap-3 xl:grid-cols-2" aria-label="核心性能指标">
          <PerformanceMetricCard label="请求健康" :value="percent(overviewMetrics.availability)" :context="`${overview.summary.attempts.toLocaleString()} 次请求 · ${overview.summary.availability.numerator}/${overview.summary.availability.denominator} 可用`" tone="success" icon="checkCircle" :trend="trendMetrics.map((metric) => metric.availability * 100)" wide />
          <PerformanceMetricCard label="响应延迟" :value="milliseconds(overviewMetrics.p95Duration)" :context="`P50 TTFT ${milliseconds(overviewMetrics.p50TTFT)} · P95 TTFT ${milliseconds(overviewMetrics.p95TTFT)}`" tone="info" icon="clock" :trend="trendMetrics.map((metric) => metric.p95_duration_ms)" wide />
        </section>
        <section class="grid grid-cols-1 gap-3 sm:grid-cols-3" aria-label="补充性能指标">
          <PerformanceMetricCard label="TTFT 超时率" :value="percent(overviewMetrics.ttftTimeoutRate)" :context="`${overview.summary.ttft_timeout_count} 次超时`" tone="warning" icon="clock" :trend="trendMetrics.map((metric) => metric.ttft_timeout_rate * 100)" />
          <PerformanceMetricCard label="切换率" :value="percent(trendMetrics.at(-1)?.failover_rate ?? 0)" context="上游自动切换" tone="neutral" icon="sync" :trend="trendMetrics.map((metric) => metric.failover_rate * 100)" />
          <PerformanceMetricCard label="样本数" :value="overview.summary.attempts.toLocaleString()" context="已汇总请求样本" tone="info" icon="chart" :trend="overview.trend.map((point) => point.counters.attempt_count)" />
        </section>
        <p v-if="overview.summary.attempts === 0" class="text-sm text-gray-500 dark:text-gray-400">暂无可分析样本。性能样本会在部署完成并处理请求后逐步累积。</p>

        <section class="grid grid-cols-1 gap-x-8 gap-y-7 xl:grid-cols-2" aria-label="性能趋势">
          <PerformanceTrendChart title="请求成功与失败趋势" :points="overview.trend" :time-range="range" :series="ratesSeries" :loading="overviewLoading" />
          <PerformanceTrendChart title="延迟趋势" :points="overview.trend" :time-range="range" :series="latencySeries" :loading="overviewLoading" />
        </section>

        <section class="grid grid-cols-1 gap-8 border-y border-gray-200 py-6 dark:border-dark-700 xl:grid-cols-2" aria-label="失败分布与采集健康">
          <PerformanceFailureDistribution :failures="failureDistribution" title="失败分布" :loading="overviewLoading" />
          <section class="border-l-4 px-4 py-1" :class="degraded ? 'border-amber-500' : 'border-emerald-500'" aria-labelledby="collection-health-title">
            <h2 id="collection-health-title" class="text-sm font-semibold text-gray-900 dark:text-white">采集健康度</h2>
            <p class="mt-3 text-lg font-semibold text-gray-900 dark:text-white">{{ healthLabel() }}</p>
            <dl class="mt-4 grid grid-cols-1 gap-3 text-sm sm:grid-cols-3">
              <div><dt class="text-gray-500 dark:text-gray-400">丢弃样本</dt><dd class="mt-1 font-medium tabular-nums text-gray-900 dark:text-white">{{ overview.collection_health.dropped_samples }}</dd></div>
              <div><dt class="text-gray-500 dark:text-gray-400">待写入样本</dt><dd class="mt-1 font-medium tabular-nums text-gray-900 dark:text-white">{{ overview.collection_health.pending_samples }}</dd></div>
              <div><dt class="text-gray-500 dark:text-gray-400">最近一次成功刷新</dt><dd class="mt-1 font-medium text-gray-900 dark:text-white">{{ overview.collection_health.last_successful_flush_at ? formatDate(overview.collection_health.last_successful_flush_at) : '暂无成功刷新记录' }}</dd></div>
            </dl>
          </section>
        </section>
      </template>

      <section class="border-t border-gray-200 pt-6 dark:border-dark-700">
        <p v-if="!accountsLoading && !accounts?.items.length" class="mb-3 text-sm text-gray-500 dark:text-gray-400">账号样本会在部署完成并处理请求后逐步累积。</p>
        <PerformanceAccountTable :page="accounts" :loading="accountsLoading" :error="accountsError" :sort="accountSort" :order="accountOrder" @retry="loadAccounts" @sort="changeSort" @page="changePage" @select="selectAccount" />
      </section>
    </main>
    <PerformanceInvestigationDrawer :open="Boolean(selectedAccount)" :account="selectedAccount" :investigation="investigation" :loading="investigationLoading" :error="investigationError" @close="closeInvestigation" @retry="loadInvestigation" />
  </AppLayout>
</template>
