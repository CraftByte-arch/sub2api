<script setup lang="ts">
import Icon from '@/components/icons/Icon.vue'
import type { RateMetric, TTFTOverview } from '@/api/admin/ttft'

defineProps<{ summary: TTFTOverview['summary'] }>()

const metrics = [
  { key: 'attemptTimeout', statisticKey: 'attempt_ttft_timeout_rate', icon: 'clock', tone: 'bg-red-600' },
  { key: 'recovery', statisticKey: 'recovery_rate', icon: 'refresh', tone: 'bg-emerald-600' },
  { key: 'finalTTFTFailure', statisticKey: 'final_ttft_failure_rate', icon: 'exclamationTriangle', tone: 'bg-violet-600' },
  { key: 'otherFinalFailure', statisticKey: 'other_final_failure_rate', icon: 'chart', tone: 'bg-amber-600' }
] as const

function percent(metric: RateMetric) {
  return `${(Number(metric.rate ?? 0) * 100).toFixed(1)}%`
}
</script>

<template>
  <section data-testid="ttft-summary-metrics" class="grid grid-cols-1 gap-3 sm:grid-cols-2 xl:grid-cols-6" aria-label="TTFT summary metrics">
    <article class="border border-blue-200 bg-white p-4 dark:border-blue-900/60 dark:bg-dark-800 xl:col-span-2">
      <div class="flex items-start justify-between gap-3"><h2 class="text-sm font-medium text-gray-600 dark:text-gray-300">{{ $t('admin.ttft.metrics.controlledRequests') }}</h2><span class="flex h-9 w-9 items-center justify-center rounded-lg bg-blue-600 text-white"><Icon name="bolt" size="sm" /></span></div>
      <p class="mt-4 text-3xl font-semibold tabular-nums text-gray-900 dark:text-white">{{ summary.controlled_requests.toLocaleString() }}</p>
      <p class="mt-1 text-xs text-gray-500 dark:text-gray-400">{{ $t('admin.ttft.metrics.clientCanceled', { count: summary.client_canceled_requests }) }}</p>
    </article>
    <article v-for="metric in metrics" :key="metric.key" class="border border-gray-200 bg-white p-4 dark:border-dark-700 dark:bg-dark-800">
      <div class="flex items-start justify-between gap-3"><h2 class="text-sm font-medium text-gray-600 dark:text-gray-300">{{ $t(`admin.ttft.metrics.${metric.key}`) }}</h2><span class="flex h-8 w-8 items-center justify-center rounded-lg text-white" :class="metric.tone"><Icon :name="metric.icon" size="xs" /></span></div>
      <p class="mt-4 text-2xl font-semibold tabular-nums text-gray-900 dark:text-white">{{ percent(summary[metric.statisticKey] as RateMetric) }}</p>
      <p class="mt-1 text-xs tabular-nums text-gray-500 dark:text-gray-400">{{ (summary[metric.statisticKey] as RateMetric).numerator }} / {{ (summary[metric.statisticKey] as RateMetric).denominator }}</p>
    </article>
  </section>
</template>
