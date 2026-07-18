<script setup lang="ts">
import { computed } from 'vue'
import BaseDialog from '@/components/common/BaseDialog.vue'
import PlatformTypeBadge from '@/components/common/PlatformTypeBadge.vue'
import { performanceMetricsFromCounters, type PerformanceAccountItem as PerformanceAccount, type PerformanceInvestigation } from '@/api/admin/performance'
import PerformanceFailureDistribution from './PerformanceFailureDistribution.vue'
import PerformanceMetricCard from './PerformanceMetricCard.vue'
import PerformanceTrendChart, { type PerformanceSeriesDefinition } from './PerformanceTrendChart.vue'

const props = defineProps<{
  open: boolean
  account: PerformanceAccount | null
  investigation: PerformanceInvestigation | null
  loading: boolean
  error: string
}>()

const emit = defineEmits<{ close: []; retry: [] }>()

const dialogTitle = computed(() => {
  if (!props.account) return '账号性能详情'
  const name = props.account.account_name || `#${props.account.account_id}`
  return `${name} · #${props.account.account_id}`
})

const metrics = computed(() => props.account ? performanceMetricsFromCounters(props.account.counters) : null)
const trendSeries: PerformanceSeriesDefinition[] = [
  { label: '可用率', color: '#10b981', selector: (point) => performanceMetricsFromCounters(point.counters).availability * 100, formatter: (value) => `${value.toFixed(2)}%` },
  { label: '失败率', color: '#ef4444', selector: (point) => performanceMetricsFromCounters(point.counters).failure_rate * 100, formatter: (value) => `${value.toFixed(2)}%`, fill: false },
  { label: 'P95 TTFT', color: '#0ea5e9', selector: (point) => performanceMetricsFromCounters(point.counters).p95_ttft_ms, formatter: (value) => `${Math.round(value)} ms`, fill: false }
]

const hasInvestigation = computed(() => Boolean(props.investigation?.time_points.length || props.investigation?.failures.length))

function percent(value: number) {
  return `${(value * 100).toFixed(2)}%`
}

function milliseconds(value: number) {
  return value > 0 ? `${Math.round(value).toLocaleString()} ms` : '--'
}

function eligibleAttempts(account: PerformanceAccount) {
  return Math.max(0, account.counters.attempt_count - account.counters.client_canceled_count)
}

function failureCount(account: PerformanceAccount) {
  return Math.max(0, eligibleAttempts(account) - account.counters.success_count)
}
</script>

<template>
  <BaseDialog :show="open" :title="dialogTitle" width="extra-wide" :close-on-click-outside="true" @close="emit('close')">
    <div v-if="account" class="mb-5 flex items-center justify-between gap-3 border-b border-gray-200 pb-4 dark:border-dark-700">
      <PlatformTypeBadge v-if="account.account_type" :platform="account.platform" :type="account.account_type" :auth-mode="account.auth_mode" />
      <span v-else class="text-sm text-gray-500 dark:text-gray-400">{{ account.platform }}</span>
      <span class="font-mono text-xs text-gray-500 dark:text-gray-400">#{{ account.account_id }}</span>
    </div>

    <section v-if="account && metrics" class="grid grid-cols-2 gap-3 lg:grid-cols-4" aria-label="账号指标摘要">
      <PerformanceMetricCard label="可用率" :value="percent(metrics.availability)" :context="`${account.counters.success_count} / ${eligibleAttempts(account)} 次成功`" tone="success" icon="checkCircle" />
      <PerformanceMetricCard label="失败率" :value="percent(metrics.failure_rate)" :context="`${failureCount(account)} / ${eligibleAttempts(account)} 次失败`" tone="danger" icon="xCircle" />
      <PerformanceMetricCard label="P95 TTFT" :value="milliseconds(metrics.p95_ttft_ms)" context="首字节响应延迟" tone="info" icon="clock" />
      <PerformanceMetricCard label="P95 总耗时" :value="milliseconds(metrics.p95_duration_ms)" context="完整请求耗时" tone="neutral" icon="chart" />
    </section>

    <div v-if="loading" class="flex min-h-64 items-center justify-center text-sm text-gray-500 dark:text-gray-400">正在加载账号详情</div>
    <div v-else-if="error" class="mt-5 border-l-4 border-red-500 bg-red-50 px-4 py-3 text-sm text-red-800 dark:bg-red-500/10 dark:text-red-200">
      <p>{{ error }}</p>
      <button data-testid="performance-investigation-retry" type="button" class="mt-3 min-h-11 rounded-md border border-current px-3 py-2 font-medium focus:outline-none focus-visible:ring-2 focus-visible:ring-red-500 focus-visible:ring-offset-2 dark:focus-visible:ring-offset-dark-900" @click="emit('retry')">重试</button>
    </div>
    <div v-else-if="!hasInvestigation" class="flex min-h-64 items-center justify-center text-sm text-gray-500 dark:text-gray-400">暂无可供分析的性能数据</div>
    <div v-else class="mt-6 space-y-8">
      <PerformanceTrendChart title="性能趋势" :points="investigation?.time_points ?? []" time-range="当前时间范围" :series="trendSeries" />
      <PerformanceFailureDistribution title="失败分布" :failures="investigation?.failures ?? []" />
    </div>
  </BaseDialog>
</template>
