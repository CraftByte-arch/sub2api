<script setup lang="ts">
import { computed } from 'vue'
import { useI18n } from 'vue-i18n'
import BaseDialog from '@/components/common/BaseDialog.vue'
import PlatformTypeBadge from '@/components/common/PlatformTypeBadge.vue'
import { performanceMetricsFromCounters, performanceMetricsFromTimePoint, type PerformanceAccountItem as PerformanceAccount, type PerformanceInvestigation } from '@/api/admin/monitoring'
import FailureDistribution from './FailureDistribution.vue'
import MetricTrendCard from './MetricTrendCard.vue'
import MonitoringTrendChart, { type PerformanceSeriesDefinition } from './MonitoringTrendChart.vue'

const props = defineProps<{
  open: boolean
  account: PerformanceAccount | null
  investigation: PerformanceInvestigation | null
  loading: boolean
  error: string
}>()

const emit = defineEmits<{ close: []; retry: [] }>()

const { t } = useI18n()

const dialogTitle = computed(() => {
  if (!props.account) return t('admin.monitoring.drawer.title')
  const name = props.account.account_name || `#${props.account.account_id}`
  return `${name} · #${props.account.account_id}`
})

const metrics = computed(() => {
  if (!props.account) return null
  const bucketed = performanceMetricsFromCounters(props.account.counters)
  return {
    ...bucketed,
    p95_ttft_ms: props.account.p95_ttft_ms > 0 ? props.account.p95_ttft_ms : bucketed.p95_ttft_ms,
    p95_duration_ms: props.account.p95_duration_ms > 0 ? props.account.p95_duration_ms : bucketed.p95_duration_ms
  }
})

const trendSeries = computed<PerformanceSeriesDefinition[]>(() => [
  { label: t('admin.monitoring.trends.availability'), color: '#10b981', selector: (point) => performanceMetricsFromTimePoint(point).availability * 100, formatter: (value) => `${value.toFixed(2)}%` },
  { label: t('admin.monitoring.trends.failureRate'), color: '#ef4444', selector: (point) => performanceMetricsFromTimePoint(point).failure_rate * 100, formatter: (value) => `${value.toFixed(2)}%`, fill: false },
  { label: t('admin.monitoring.trends.p95Ttft'), color: '#0ea5e9', selector: (point) => performanceMetricsFromTimePoint(point).p95_ttft_ms, formatter: (value) => `${Math.round(value)} ms`, fill: false }
])

const knownOutcomes = new Set(['ttft_timeout', 'rate_limit', 'auth', 'upstream_4xx', 'upstream_5xx', 'transport', 'protocol', 'other_failure'])
const normalizeOutcome = (outcome: string) => outcome.trim().replace(/([a-z])([A-Z])/g, '$1_$2').replace(/[ -]+/g, '_').toLowerCase()
const colorForOutcome = (key: string) => {
  if (key === 'ttft_timeout') return '#ef4444'
  if (key === 'rate_limit') return '#f97316'
  return '#64748b'
}

const failureItems = computed(() => (props.investigation?.failures ?? []).map((failure) => {
  const normalized = normalizeOutcome(failure.Outcome)
  const key = knownOutcomes.has(normalized) ? normalized : 'other_failure'
  return { label: t(`admin.monitoring.failures.outcomes.${key}`), count: Number(failure.Count), color: colorForOutcome(normalized) }
}))

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

    <section v-if="account && metrics" class="grid grid-cols-2 gap-3 lg:grid-cols-4" :aria-label="t('admin.monitoring.drawer.title')">
      <MetricTrendCard :label="t('admin.monitoring.drawer.availability')" :value="percent(metrics.availability)" :context="t('admin.monitoring.drawer.successContext', { success: account.counters.success_count, total: eligibleAttempts(account) })" tone="success" />
      <MetricTrendCard :label="t('admin.monitoring.drawer.failureRate')" :value="percent(metrics.failure_rate)" :context="t('admin.monitoring.drawer.failureContext', { failure: failureCount(account), total: eligibleAttempts(account) })" tone="danger" />
      <MetricTrendCard :label="t('admin.monitoring.drawer.p95Ttft')" :value="milliseconds(metrics.p95_ttft_ms)" :context="t('admin.monitoring.drawer.ttftContext')" tone="neutral" />
      <MetricTrendCard :label="t('admin.monitoring.drawer.p95Duration')" :value="milliseconds(metrics.p95_duration_ms)" :context="t('admin.monitoring.drawer.durationContext')" tone="neutral" />
    </section>

    <div v-if="loading" class="flex min-h-64 items-center justify-center text-sm text-gray-500 dark:text-gray-400">{{ t('admin.monitoring.drawer.loading') }}</div>
    <div v-else-if="error" class="mt-5 border-l-4 border-red-500 bg-red-50 px-4 py-3 text-sm text-red-800 dark:bg-red-500/10 dark:text-red-200">
      <p>{{ error }}</p>
      <button data-testid="performance-investigation-retry" type="button" class="mt-3 min-h-11 rounded-md border border-current px-3 py-2 font-medium focus:outline-none focus-visible:ring-2 focus-visible:ring-red-500 focus-visible:ring-offset-2 dark:focus-visible:ring-offset-dark-900" @click="emit('retry')">{{ t('admin.monitoring.accounts.retry') }}</button>
    </div>
    <div v-else-if="!hasInvestigation" class="flex min-h-64 items-center justify-center text-sm text-gray-500 dark:text-gray-400">{{ t('admin.monitoring.drawer.empty') }}</div>
    <div v-else class="mt-6 space-y-8">
      <MonitoringTrendChart :title="t('admin.monitoring.drawer.trendTitle')" :points="investigation?.time_points ?? []" :series="trendSeries" />
      <FailureDistribution :title="t('admin.monitoring.drawer.failureTitle')" :failures="failureItems" />
    </div>
  </BaseDialog>
</template>
