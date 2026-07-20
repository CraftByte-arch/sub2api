<script setup lang="ts">
import { useI18n } from 'vue-i18n'
import Icon from '@/components/icons/Icon.vue'
import type { PerformanceAccountItem as PerformanceAccount, PerformanceAccountPage, PerformanceOrder } from '@/api/admin/monitoring'
import Pagination from '@/components/common/Pagination.vue'
import PlatformIcon from '@/components/common/PlatformIcon.vue'
import PlatformTypeBadge from '@/components/common/PlatformTypeBadge.vue'
import SearchInput from '@/components/common/SearchInput.vue'

const props = defineProps<{
  page: PerformanceAccountPage | null
  loading: boolean
  error: string
  sort: string
  order: PerformanceOrder
  search: string
}>()

const emit = defineEmits<{
  retry: []
  sort: [value: string]
  page: [value: number]
  select: [account: PerformanceAccount]
  'update:search': [value: string]
}>()

const { t } = useI18n()

type HealthStatus = 'healthy' | 'watch' | 'risk' | 'low-sample'

const healthClasses: Record<HealthStatus, string> = {
  healthy: 'bg-emerald-100 text-emerald-800 dark:bg-emerald-500/15 dark:text-emerald-300',
  watch: 'bg-amber-100 text-amber-800 dark:bg-amber-500/15 dark:text-amber-300',
  risk: 'bg-red-100 text-red-800 dark:bg-red-500/15 dark:text-red-300',
  'low-sample': 'bg-gray-100 text-gray-700 dark:bg-gray-700 dark:text-gray-200'
}

const healthLabels: Record<HealthStatus, string> = {
  healthy: 'admin.monitoring.accounts.healthy',
  watch: 'admin.monitoring.accounts.watch',
  risk: 'admin.monitoring.accounts.risk',
  'low-sample': 'admin.monitoring.accounts.lowSample'
}

function healthFor(account: PerformanceAccount): HealthStatus {
  if (account.low_sample) return 'low-sample'
  if (account.health_score >= 0.9) return 'healthy'
  if (account.health_score >= 0.6) return 'watch'
  return 'risk'
}

function percent(value: number) {
  return `${(value * 100).toFixed(2)}%`
}

function milliseconds(value: number) {
  return value > 0 ? `${Math.round(value).toLocaleString()} ms` : '--'
}

function ttftTimeoutRate(account: PerformanceAccount) {
  const attempts = account.counters.attempt_count
  if (!attempts) return '--'
  return `${((account.counters.ttft_timeout_count / attempts) * 100).toFixed(2)}%`
}

function platformLabel(platform: PerformanceAccount['platform']) {
  return ({ anthropic: 'Anthropic', openai: 'OpenAI', gemini: 'Gemini', antigravity: 'Antigravity', grok: 'Grok' })[platform]
}

function ariaSort(column: string) {
  if (props.sort !== column) return 'none'
  return props.order === 'asc' ? 'ascending' : 'descending'
}

function select(account: PerformanceAccount) {
  emit('select', account)
}
</script>

<template>
  <section class="min-w-0 border border-gray-200 bg-white p-4 dark:border-dark-700 dark:bg-dark-800" aria-labelledby="monitoring-accounts-title">
    <div class="flex items-center justify-between gap-3">
      <h2 id="monitoring-accounts-title" class="text-sm font-semibold text-gray-900 dark:text-white">{{ t('admin.monitoring.accounts.title') }}</h2>
      <span v-if="page" class="text-xs tabular-nums text-gray-500 dark:text-gray-400">{{ t('admin.monitoring.accounts.total', { count: page.total }) }}</span>
    </div>

    <div class="mt-3 max-w-xs" data-testid="account-search">
      <SearchInput :model-value="search" :placeholder="t('admin.monitoring.accounts.searchPlaceholder')" @update:model-value="(value: string) => emit('update:search', value)" />
    </div>

    <div v-if="loading && !page" class="mt-4 space-y-2" :aria-label="t('admin.monitoring.accounts.loading')">
      <div v-for="index in 5" :key="index" class="h-11 animate-pulse bg-gray-100 dark:bg-dark-700" />
    </div>
    <div v-else-if="error && !page" class="mt-4 flex items-center gap-3 text-sm text-red-700 dark:text-red-300">
      <span>{{ error }}</span>
      <button data-testid="monitoring-accounts-retry" type="button" class="min-h-11 rounded-md border border-current px-3 py-2 font-medium focus:outline-none focus-visible:ring-2 focus-visible:ring-red-500 focus-visible:ring-offset-2 dark:focus-visible:ring-offset-dark-800" @click="emit('retry')">{{ t('admin.monitoring.accounts.retry') }}</button>
    </div>
    <template v-else>
      <div v-if="error" class="mt-4 flex items-center gap-3 text-sm text-red-700 dark:text-red-300">
        <span>{{ error }}</span>
        <button data-testid="monitoring-accounts-retry" type="button" class="min-h-11 rounded-md border border-current px-3 py-2 font-medium focus:outline-none focus-visible:ring-2 focus-visible:ring-red-500 focus-visible:ring-offset-2 dark:focus-visible:ring-offset-dark-800" @click="emit('retry')">{{ t('admin.monitoring.accounts.retry') }}</button>
      </div>
      <p v-if="!page?.items.length" class="py-10 text-center text-sm text-gray-500 dark:text-gray-400">{{ t('admin.monitoring.accounts.empty') }}</p>
      <div v-else class="mt-4 overflow-x-auto">
        <table class="w-full min-w-[960px] border-collapse text-left text-sm">
          <thead class="border-y border-gray-200 text-xs text-gray-500 dark:border-dark-700 dark:text-gray-400">
            <tr>
              <th class="px-3 py-2 font-medium">{{ t('admin.monitoring.accounts.account') }}</th>
              <th class="px-3 py-2 font-medium">{{ t('admin.monitoring.accounts.platform') }}</th>
              <th class="px-3 py-2 font-medium" :aria-sort="ariaSort('health_score')"><button type="button" class="inline-flex min-h-11 items-center gap-1 focus:outline-none focus-visible:ring-2 focus-visible:ring-primary-500 focus-visible:ring-offset-2 dark:focus-visible:ring-offset-dark-800" @click="emit('sort', 'health_score')">{{ t('admin.monitoring.accounts.status') }} <Icon name="sort" size="xs" aria-hidden="true" /></button></th>
              <th class="px-3 py-2 font-medium" :aria-sort="ariaSort('availability')"><button type="button" class="inline-flex min-h-11 items-center gap-1 focus:outline-none focus-visible:ring-2 focus-visible:ring-primary-500 focus-visible:ring-offset-2 dark:focus-visible:ring-offset-dark-800" @click="emit('sort', 'availability')">{{ t('admin.monitoring.accounts.availability') }} <Icon name="sort" size="xs" aria-hidden="true" /></button></th>
              <th class="px-3 py-2 font-medium" :aria-sort="ariaSort('failure_rate')"><button type="button" class="inline-flex min-h-11 items-center gap-1 focus:outline-none focus-visible:ring-2 focus-visible:ring-primary-500 focus-visible:ring-offset-2 dark:focus-visible:ring-offset-dark-800" @click="emit('sort', 'failure_rate')">{{ t('admin.monitoring.accounts.failureRate') }} <Icon name="sort" size="xs" aria-hidden="true" /></button></th>
              <th class="px-3 py-2 font-medium">{{ t('admin.monitoring.accounts.ttftTimeoutRate') }}</th>
              <th class="px-3 py-2 font-medium" :aria-sort="ariaSort('p95_ttft_ms')"><button type="button" class="inline-flex min-h-11 items-center gap-1 focus:outline-none focus-visible:ring-2 focus-visible:ring-primary-500 focus-visible:ring-offset-2 dark:focus-visible:ring-offset-dark-800" @click="emit('sort', 'p95_ttft_ms')">{{ t('admin.monitoring.accounts.p95Ttft') }} <Icon name="sort" size="xs" aria-hidden="true" /></button></th>
              <th class="px-3 py-2 font-medium" :aria-sort="ariaSort('samples')"><button type="button" class="inline-flex min-h-11 items-center gap-1 focus:outline-none focus-visible:ring-2 focus-visible:ring-primary-500 focus-visible:ring-offset-2 dark:focus-visible:ring-offset-dark-800" @click="emit('sort', 'samples')">{{ t('admin.monitoring.accounts.samples') }} <Icon name="sort" size="xs" aria-hidden="true" /></button></th>
            </tr>
          </thead>
          <tbody class="divide-y divide-gray-100 dark:divide-dark-700">
            <tr v-for="item in page.items" :key="item.account_id" class="h-11 cursor-pointer text-gray-700 hover:bg-gray-50 dark:text-gray-200 dark:hover:bg-dark-700" @click="select(item)">
              <td class="px-3 py-3">
                <button :data-testid="`monitoring-account-${item.account_id}`" type="button" class="rounded text-left focus:outline-none focus-visible:ring-2 focus-visible:ring-primary-500" :aria-label="t('admin.monitoring.accounts.viewDetails', { name: item.account_name || `#${item.account_id}` })" @click.stop="select(item)">
                  <span class="block font-medium text-gray-900 dark:text-white">{{ item.account_name || `#${item.account_id}` }}</span>
                  <span class="mt-0.5 block font-mono text-xs text-gray-500 dark:text-gray-400">#{{ item.account_id }}</span>
                </button>
              </td>
              <td class="px-3 py-3">
                <PlatformTypeBadge v-if="item.account_type" :platform="item.platform" :type="item.account_type" :auth-mode="item.auth_mode" />
                <span v-else class="inline-flex items-center gap-1 rounded-md bg-gray-100 px-2 py-1 text-xs font-medium text-gray-700 dark:bg-gray-700 dark:text-gray-200">
                  <PlatformIcon :platform="item.platform" size="xs" />
                  {{ platformLabel(item.platform) }}
                </span>
              </td>
              <td class="px-3 py-3"><span class="inline-flex rounded-md px-2 py-1 text-xs font-medium" :class="healthClasses[healthFor(item)]">{{ t(healthLabels[healthFor(item)]) }}</span></td>
              <td class="px-3 py-3 tabular-nums">{{ percent(item.availability) }}</td>
              <td class="px-3 py-3 tabular-nums">{{ percent(item.failure_rate) }}</td>
              <td class="px-3 py-3 tabular-nums">{{ ttftTimeoutRate(item) }}</td>
              <td class="px-3 py-3 tabular-nums">{{ milliseconds(item.p95_ttft_ms) }}</td>
              <td class="px-3 py-3 tabular-nums">{{ item.counters.attempt_count.toLocaleString() }}</td>
            </tr>
          </tbody>
        </table>
      </div>
    </template>

    <Pagination v-if="page && page.pages > 1" class="mt-4" :total="page.total" :page="page.page" :page-size="page.page_size" :show-page-size-selector="false" @update:page="(value: number) => emit('page', value)" />
  </section>
</template>
