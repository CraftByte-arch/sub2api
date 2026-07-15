<script setup lang="ts">
import { computed, onMounted, onUnmounted, ref, watch } from 'vue'
import { useI18n } from 'vue-i18n'
import { useRoute, useRouter } from 'vue-router'
import AppLayout from '@/components/layout/AppLayout.vue'
import ttftAPI, { type FirstTokenTimeoutSettings, type TTFTAccountsPage, type TTFTAccountsParams, type TTFTOrder, type TTFTOverview, type TTFTRange, type TTFTProtocol } from '@/api/admin/ttft'
import { formatDateTime } from '@/utils/format'
import TTFTSettingsBar from './components/TTFTSettingsBar.vue'
import TTFTSummaryMetrics from './components/TTFTSummaryMetrics.vue'
import TTFTFailureTrendChart from './components/TTFTFailureTrendChart.vue'
import TTFTFailureDistributionChart from './components/TTFTFailureDistributionChart.vue'
import TTFTAccountStatsTable from './components/TTFTAccountStatsTable.vue'

const { t } = useI18n()
const route = useRoute()
const router = useRouter()
const ranges: TTFTRange[] = ['24h', '7d', '30d', '90d']
const protocols: TTFTProtocol[] = ['responses', 'chat_completions', 'anthropic_messages']

const range = ref<TTFTRange>('24h')
const protocol = ref<TTFTProtocol | undefined>()
const model = ref('')
const settings = ref<FirstTokenTimeoutSettings | null>(null)
const overview = ref<TTFTOverview | null>(null)
const accounts = ref<TTFTAccountsPage | null>(null)
const settingsLoading = ref(true)
const overviewLoading = ref(true)
const accountsLoading = ref(true)
const settingsSaving = ref(false)
const settingsError = ref('')
const overviewError = ref('')
const accountsError = ref('')
const hasOverviewLoaded = ref(false)
const hasAccountsLoaded = ref(false)
const accountSearch = ref('')
const accountPlatform = ref('')
const accountIDInput = ref<number | string>('')
const accountSort = ref('samples')
const accountOrder = ref<TTFTOrder>('desc')
const accountPage = ref(1)
const accountPageSize = ref<10 | 20 | 50 | 100>(20)
let overviewGeneration = 0
let accountsGeneration = 0
let syncingRouteQuery = false
let accountSearchTimer: ReturnType<typeof setTimeout> | undefined

const globalParams = computed(() => ({ range: range.value, protocol: protocol.value, model: model.value.trim() || undefined }))
const accountID = computed<number | undefined>(() => {
  const raw = accountIDInput.value
  if (raw === '' || raw === null || raw === undefined) return undefined
  const value = Number(raw)
  return Number.isInteger(value) && value > 0 ? value : undefined
})
const accountIDError = computed(() => accountIDInput.value !== '' && accountID.value === undefined)
const accountsParams = computed<TTFTAccountsParams>(() => ({ ...globalParams.value, platform: accountPlatform.value.trim() || undefined, account_id: accountID.value, search: accountSearch.value.trim() || undefined, sort: accountSort.value, order: accountOrder.value, page: accountPage.value, page_size: accountPageSize.value }))
const degraded = computed(() => overview.value?.completeness.status === 'degraded')

function readQuery() {
  const query = route.query
  range.value = ranges.includes(query.range as TTFTRange) ? query.range as TTFTRange : '24h'
  protocol.value = protocols.includes(query.protocol as TTFTProtocol) ? query.protocol as TTFTProtocol : undefined
  model.value = typeof query.model === 'string' ? query.model : ''
}

async function syncQuery() {
  const query: Record<string, string> = { range: range.value }
  if (protocol.value) query.protocol = protocol.value
  if (model.value.trim()) query.model = model.value.trim()
  syncingRouteQuery = true
  try { await router.replace({ query }) } finally { syncingRouteQuery = false }
}

async function loadSettings() {
  settingsLoading.value = !settings.value
  settingsError.value = ''
  try { settings.value = await ttftAPI.getSettings() } catch { settingsError.value = t('admin.ttft.errors.settings') } finally { settingsLoading.value = false }
}

async function loadOverview() {
  const generation = ++overviewGeneration
  overviewLoading.value = !hasOverviewLoaded.value
  overviewError.value = ''
  try {
    const response = await ttftAPI.getOverview(globalParams.value)
    if (generation !== overviewGeneration) return
    overview.value = response
    hasOverviewLoaded.value = true
  } catch {
    if (generation !== overviewGeneration) return
    overviewError.value = t('admin.ttft.errors.overview')
  } finally {
    if (generation === overviewGeneration) overviewLoading.value = false
  }
}

async function loadAccounts() {
  const generation = ++accountsGeneration
  accountsLoading.value = !hasAccountsLoaded.value
  accountsError.value = ''
  try {
    const response = await ttftAPI.getAccounts(accountsParams.value)
    if (generation !== accountsGeneration) return
    accounts.value = response
    hasAccountsLoaded.value = true
  } catch {
    if (generation !== accountsGeneration) return
    accountsError.value = t('admin.ttft.errors.accounts')
  } finally {
    if (generation === accountsGeneration) accountsLoading.value = false
  }
}

async function refreshAll() { await Promise.all([loadOverview(), loadAccounts()]) }

async function saveSettings(payload: { enabled: boolean; timeout_seconds: number }) {
  settingsSaving.value = true
  settingsError.value = ''
  try { settings.value = await ttftAPI.updateSettings(payload) } catch { settingsError.value = t('admin.ttft.errors.settings') } finally { settingsSaving.value = false }
}

async function changeGlobalFilters() {
  accountPage.value = 1
  await syncQuery()
  await refreshAll()
}

function searchAccounts() {
  if (accountSearchTimer) clearTimeout(accountSearchTimer)
  accountSearchTimer = setTimeout(() => {
    accountSearchTimer = undefined
    accountPage.value = 1
    void loadAccounts()
  }, 300)
}

function changeAccountSort(sort: string) {
  if (accountSort.value === sort) accountOrder.value = accountOrder.value === 'asc' ? 'desc' : 'asc'
  else { accountSort.value = sort; accountOrder.value = 'desc' }
  accountPage.value = 1
  void loadAccounts()
}

watch(accountSearch, () => { void searchAccounts() })
watch([accountPlatform, accountIDInput, accountPageSize], () => {
  accountPage.value = 1
  if (!accountIDError.value) void loadAccounts()
})
watch(
  () => route.query,
  async () => {
    if (syncingRouteQuery) return
    readQuery()
    accountPage.value = 1
    await refreshAll()
  },
  { deep: true }
)

onMounted(async () => { readQuery(); await syncQuery(); await Promise.all([loadSettings(), refreshAll()]) })
onUnmounted(() => {
  if (accountSearchTimer) clearTimeout(accountSearchTimer)
  overviewGeneration++
  accountsGeneration++
})
</script>

<template>
  <AppLayout>
    <main class="min-w-0 space-y-5 pb-10">
      <TTFTSettingsBar :settings="settings" :loading="settingsLoading" :saving="settingsSaving" :error="settingsError" @save="saveSettings" />
      <section class="flex flex-col gap-3 border-b border-gray-200 pb-4 dark:border-dark-700 lg:flex-row lg:items-end lg:justify-between" aria-label="TTFT filters">
        <div class="flex flex-wrap gap-2" role="group" :aria-label="$t('admin.ttft.filters.range')"><button v-for="item in ranges" :key="item" type="button" :class="range === item ? 'bg-blue-600 text-white' : 'border border-gray-300 text-gray-700 dark:border-dark-600 dark:text-gray-200'" class="h-9 rounded-lg px-3 text-sm font-medium focus:outline-none focus:ring-2 focus:ring-blue-500" @click="range = item; changeGlobalFilters()">{{ item }}</button></div>
        <div class="grid gap-2 sm:grid-cols-3"><label class="grid gap-1 text-xs text-gray-500 dark:text-gray-400"><span>{{ $t('admin.ttft.filters.protocol') }}</span><select v-model="protocol" class="h-9 rounded-lg border border-gray-300 bg-white px-2 text-sm text-gray-900 dark:border-dark-600 dark:bg-dark-900 dark:text-white" @change="changeGlobalFilters"><option :value="undefined">{{ $t('common.all') }}</option><option v-for="item in protocols" :key="item" :value="item">{{ item }}</option></select></label><label class="grid gap-1 text-xs text-gray-500 dark:text-gray-400"><span>{{ $t('admin.ttft.filters.model') }}</span><input v-model="model" class="h-9 rounded-lg border border-gray-300 bg-white px-2 text-sm text-gray-900 dark:border-dark-600 dark:bg-dark-900 dark:text-white" @change="changeGlobalFilters" /></label><button type="button" class="h-9 self-end rounded-lg border border-gray-300 px-3 text-sm text-gray-700 focus:outline-none focus:ring-2 focus:ring-blue-500 dark:border-dark-600 dark:text-gray-200" @click="refreshAll">{{ $t('common.refresh') }}</button></div>
      </section>
      <aside v-if="degraded" class="border border-amber-300 bg-amber-50 px-4 py-3 text-sm text-amber-900 dark:border-amber-800 dark:bg-amber-950/30 dark:text-amber-200">
        {{ $t('admin.ttft.completeness.degraded', { dropped: overview?.completeness.dropped_samples, pending: overview?.completeness.pending_samples }) }}
        {{ overview?.completeness.last_successful_flush_at ? $t('admin.ttft.completeness.lastSuccessfulFlush', { time: formatDateTime(overview.completeness.last_successful_flush_at) }) : $t('admin.ttft.completeness.noSuccessfulFlush') }}
      </aside>
      <section v-if="overviewLoading && !hasOverviewLoaded" data-testid="ttft-skeleton" class="grid grid-cols-1 gap-3 sm:grid-cols-2 xl:grid-cols-5"><div v-for="item in 5" :key="item" class="h-32 animate-pulse rounded-lg bg-gray-100 dark:bg-dark-800" /></section>
      <section v-else-if="overviewError && !overview" class="border border-red-200 bg-red-50 p-4 text-sm text-red-700 dark:border-red-900 dark:bg-red-950/30 dark:text-red-300">{{ overviewError }} <button data-testid="ttft-overview-retry" type="button" class="ml-3 underline" @click="loadOverview">{{ $t('common.refresh') }}</button></section>
      <template v-else-if="overview"><section v-if="overviewError" class="border border-red-200 bg-red-50 p-3 text-sm text-red-700 dark:border-red-900 dark:bg-red-950/30 dark:text-red-300">{{ overviewError }} <button data-testid="ttft-overview-retry" type="button" class="ml-3 underline" @click="loadOverview">{{ $t('common.refresh') }}</button></section><TTFTSummaryMetrics :summary="overview.summary" /><section class="grid grid-cols-1 gap-4 xl:grid-cols-2"><TTFTFailureTrendChart :points="overview.trend" /><TTFTFailureDistributionChart :failures="overview.other_failures" /></section></template>
      <section class="border-t border-gray-200 pt-5 dark:border-dark-700"><div class="mb-3 grid gap-2 sm:grid-cols-2 lg:grid-cols-4"><label class="grid gap-1 text-xs text-gray-500 dark:text-gray-400"><span>{{ $t('common.search') }}</span><input data-testid="ttft-account-search" v-model="accountSearch" class="h-9 rounded-lg border border-gray-300 bg-white px-2 text-sm text-gray-900 dark:border-dark-600 dark:bg-dark-900 dark:text-white" /></label><label class="grid gap-1 text-xs text-gray-500 dark:text-gray-400"><span>{{ $t('admin.ttft.accounts.platform') }}</span><input v-model="accountPlatform" class="h-9 rounded-lg border border-gray-300 bg-white px-2 text-sm text-gray-900 dark:border-dark-600 dark:bg-dark-900 dark:text-white" /></label><label class="grid gap-1 text-xs text-gray-500 dark:text-gray-400"><span>{{ $t('admin.ttft.accounts.accountId') }}</span><input v-model.number="accountIDInput" type="number" min="1" class="h-9 rounded-lg border border-gray-300 bg-white px-2 text-sm text-gray-900 dark:border-dark-600 dark:bg-dark-900 dark:text-white" :aria-invalid="accountIDError" /><span v-if="accountIDError" class="text-xs text-red-600 dark:text-red-400">{{ $t('admin.ttft.accounts.accountIdError') }}</span></label><label class="grid gap-1 text-xs text-gray-500 dark:text-gray-400"><span>{{ $t('admin.ttft.accounts.pageSize') }}</span><select v-model.number="accountPageSize" class="h-9 rounded-lg border border-gray-300 bg-white px-2 text-sm text-gray-900 dark:border-dark-600 dark:bg-dark-900 dark:text-white"><option :value="10">10</option><option :value="20">20</option><option :value="50">50</option><option :value="100">100</option></select></label></div><TTFTAccountStatsTable :page="accounts" :loading="accountsLoading" :error="accountsError" :sort="accountSort" :order="accountOrder" @retry="loadAccounts" @sort="changeAccountSort" @page="(value) => { accountPage = value; loadAccounts() }" /></section>
    </main>
  </AppLayout>
</template>
