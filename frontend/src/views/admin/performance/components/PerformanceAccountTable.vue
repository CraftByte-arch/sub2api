<script setup lang="ts">
import Icon from '@/components/icons/Icon.vue'
import type { PerformanceAccountItem as PerformanceAccount, PerformanceAccountPage, PerformanceOrder } from '@/api/admin/performance'
import PlatformIcon from '@/components/common/PlatformIcon.vue'
import PlatformTypeBadge from '@/components/common/PlatformTypeBadge.vue'

const props = defineProps<{
  page: PerformanceAccountPage | null
  loading: boolean
  error: string
  sort: string
  order: PerformanceOrder
}>()

const emit = defineEmits<{
  retry: []
  sort: [value: string]
  page: [value: number]
  select: [account: PerformanceAccount]
}>()

type HealthStatus = 'healthy' | 'watch' | 'risk' | 'low-sample'

const healthDetails: Record<HealthStatus, { label: string; classes: string }> = {
  healthy: { label: '健康', classes: 'bg-emerald-100 text-emerald-800 dark:bg-emerald-500/15 dark:text-emerald-300' },
  watch: { label: '关注', classes: 'bg-amber-100 text-amber-800 dark:bg-amber-500/15 dark:text-amber-300' },
  risk: { label: '风险', classes: 'bg-red-100 text-red-800 dark:bg-red-500/15 dark:text-red-300' },
  'low-sample': { label: '样本不足', classes: 'bg-gray-100 text-gray-700 dark:bg-gray-700 dark:text-gray-200' }
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

function failedCalls(account: PerformanceAccount) {
  const eligible = Math.max(0, account.counters.attempt_count - account.counters.client_canceled_count)
  return Math.max(0, eligible - account.counters.success_count)
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
  <section class="min-w-0 border border-gray-200 bg-white p-4 dark:border-dark-700 dark:bg-dark-800" aria-labelledby="performance-accounts-title">
    <div class="flex items-center justify-between gap-3">
      <h2 id="performance-accounts-title" class="text-sm font-semibold text-gray-900 dark:text-white">账号表现</h2>
      <span v-if="page" class="text-xs tabular-nums text-gray-500 dark:text-gray-400">{{ page.total }} 个账号</span>
    </div>

    <div v-if="loading && !page" class="mt-4 space-y-2" aria-label="正在加载账号性能">
      <div v-for="index in 5" :key="index" class="h-11 animate-pulse bg-gray-100 dark:bg-dark-700" />
    </div>
    <div v-else-if="error && !page" class="mt-4 flex items-center gap-3 text-sm text-red-700 dark:text-red-300">
      <span>{{ error }}</span>
      <button data-testid="performance-accounts-retry" type="button" class="min-h-11 rounded-md border border-current px-3 py-2 font-medium focus:outline-none focus-visible:ring-2 focus-visible:ring-red-500 focus-visible:ring-offset-2 dark:focus-visible:ring-offset-dark-800" @click="emit('retry')">重试</button>
    </div>
    <template v-else>
      <div v-if="error" class="mt-4 flex items-center gap-3 text-sm text-red-700 dark:text-red-300">
        <span>{{ error }}</span>
        <button data-testid="performance-accounts-retry" type="button" class="min-h-11 rounded-md border border-current px-3 py-2 font-medium focus:outline-none focus-visible:ring-2 focus-visible:ring-red-500 focus-visible:ring-offset-2 dark:focus-visible:ring-offset-dark-800" @click="emit('retry')">重试</button>
      </div>
      <p v-if="!page?.items.length" class="py-10 text-center text-sm text-gray-500 dark:text-gray-400">所选时间段暂无账号性能数据</p>
      <div v-else class="mt-4 overflow-x-auto">
        <table class="w-full min-w-[1120px] border-collapse text-left text-sm">
          <thead class="border-y border-gray-200 text-xs text-gray-500 dark:border-dark-700 dark:text-gray-400">
            <tr>
              <th class="px-3 py-2 font-medium">账号</th>
              <th class="px-3 py-2 font-medium">平台</th>
              <th class="px-3 py-2 font-medium" :aria-sort="ariaSort('health_score')"><button type="button" class="inline-flex min-h-11 items-center gap-1 focus:outline-none focus-visible:ring-2 focus-visible:ring-primary-500 focus-visible:ring-offset-2 dark:focus-visible:ring-offset-dark-800" @click="emit('sort', 'health_score')">状态 <Icon name="sort" size="xs" aria-hidden="true" /></button></th>
              <th class="px-3 py-2 font-medium" :aria-sort="ariaSort('availability')"><button type="button" class="inline-flex min-h-11 items-center gap-1 focus:outline-none focus-visible:ring-2 focus-visible:ring-primary-500 focus-visible:ring-offset-2 dark:focus-visible:ring-offset-dark-800" @click="emit('sort', 'availability')">可用率 <Icon name="sort" size="xs" aria-hidden="true" /></button></th>
              <th class="px-3 py-2 font-medium" :aria-sort="ariaSort('failure_rate')"><button type="button" class="inline-flex min-h-11 items-center gap-1 focus:outline-none focus-visible:ring-2 focus-visible:ring-primary-500 focus-visible:ring-offset-2 dark:focus-visible:ring-offset-dark-800" @click="emit('sort', 'failure_rate')">失败率 <Icon name="sort" size="xs" aria-hidden="true" /></button></th>
              <th class="px-3 py-2 font-medium" :aria-sort="ariaSort('p95_ttft_ms')"><button type="button" class="inline-flex min-h-11 items-center gap-1 focus:outline-none focus-visible:ring-2 focus-visible:ring-primary-500 focus-visible:ring-offset-2 dark:focus-visible:ring-offset-dark-800" @click="emit('sort', 'p95_ttft_ms')">P95 TTFT <Icon name="sort" size="xs" aria-hidden="true" /></button></th>
              <th class="px-3 py-2 font-medium" :aria-sort="ariaSort('p95_duration_ms')"><button type="button" class="inline-flex min-h-11 items-center gap-1 focus:outline-none focus-visible:ring-2 focus-visible:ring-primary-500 focus-visible:ring-offset-2 dark:focus-visible:ring-offset-dark-800" @click="emit('sort', 'p95_duration_ms')">P95 总耗时 <Icon name="sort" size="xs" aria-hidden="true" /></button></th>
              <th class="px-3 py-2 font-medium" :aria-sort="ariaSort('success_count')"><button type="button" class="inline-flex min-h-11 items-center gap-1 focus:outline-none focus-visible:ring-2 focus-visible:ring-primary-500 focus-visible:ring-offset-2 dark:focus-visible:ring-offset-dark-800" @click="emit('sort', 'success_count')">成功调用 <Icon name="sort" size="xs" aria-hidden="true" /></button></th>
              <th class="px-3 py-2 font-medium" :aria-sort="ariaSort('failure_count')"><button type="button" class="inline-flex min-h-11 items-center gap-1 focus:outline-none focus-visible:ring-2 focus-visible:ring-primary-500 focus-visible:ring-offset-2 dark:focus-visible:ring-offset-dark-800" @click="emit('sort', 'failure_count')">失败调用 <Icon name="sort" size="xs" aria-hidden="true" /></button></th>
            </tr>
          </thead>
          <tbody class="divide-y divide-gray-100 dark:divide-dark-700">
            <tr v-for="item in page.items" :key="item.account_id" class="h-11 cursor-pointer text-gray-700 hover:bg-gray-50 dark:text-gray-200 dark:hover:bg-dark-700" @click="select(item)">
              <td class="px-3 py-3">
                <button :data-testid="`performance-account-${item.account_id}`" type="button" class="rounded text-left focus:outline-none focus-visible:ring-2 focus-visible:ring-primary-500" :aria-label="`查看账号 ${item.account_name || `#${item.account_id}`} 性能详情`" @click.stop="select(item)">
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
              <td class="px-3 py-3"><span class="inline-flex rounded-md px-2 py-1 text-xs font-medium" :class="healthDetails[healthFor(item)].classes">{{ healthDetails[healthFor(item)].label }}</span></td>
              <td class="px-3 py-3 tabular-nums">{{ percent(item.availability) }}</td>
              <td class="px-3 py-3 tabular-nums">{{ percent(item.failure_rate) }}</td>
              <td class="px-3 py-3 tabular-nums">{{ milliseconds(item.p95_ttft_ms) }}</td>
              <td class="px-3 py-3 tabular-nums">{{ milliseconds(item.p95_duration_ms) }}</td>
              <td :data-testid="`performance-success-count-${item.account_id}`" class="px-3 py-3 font-medium tabular-nums text-emerald-700 dark:text-emerald-300">{{ item.counters.success_count.toLocaleString() }}</td>
              <td :data-testid="`performance-failure-count-${item.account_id}`" class="px-3 py-3 font-medium tabular-nums text-red-700 dark:text-red-300">{{ failedCalls(item).toLocaleString() }}</td>
            </tr>
          </tbody>
        </table>
      </div>
    </template>

    <div v-if="page && page.pages > 1" class="mt-4 flex items-center justify-end gap-2 text-sm">
      <button type="button" :disabled="page.page <= 1" class="min-h-11 rounded-md border border-gray-300 px-3 py-2 disabled:opacity-50 focus:outline-none focus-visible:ring-2 focus-visible:ring-primary-500 focus-visible:ring-offset-2 dark:border-dark-600 dark:focus-visible:ring-offset-dark-800" @click="emit('page', page.page - 1)">上一页</button>
      <span class="tabular-nums text-gray-500 dark:text-gray-400">{{ page.page }} / {{ page.pages }}</span>
      <button type="button" :disabled="page.page >= page.pages" class="min-h-11 rounded-md border border-gray-300 px-3 py-2 disabled:opacity-50 focus:outline-none focus-visible:ring-2 focus-visible:ring-primary-500 focus-visible:ring-offset-2 dark:border-dark-600 dark:focus-visible:ring-offset-dark-800" @click="emit('page', page.page + 1)">下一页</button>
    </div>
  </section>
</template>
