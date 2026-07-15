<script setup lang="ts">
import type { RateMetric, TTFTOverview } from '@/api/admin/ttft'

defineProps<{ summary: TTFTOverview['summary'] }>()

function percent(metric: RateMetric) {
  return `${(Number(metric.rate ?? 0) * 100).toFixed(1)}%`
}
</script>

<template>
  <section class="grid grid-cols-1 gap-3 sm:grid-cols-2 xl:grid-cols-5" aria-label="TTFT summary metrics">
    <article class="rounded-lg border border-gray-200 bg-white p-4 dark:border-dark-700 dark:bg-dark-800">
      <h2 class="text-sm font-medium text-gray-600 dark:text-gray-300">{{ $t('admin.ttft.metrics.controlledRequests') }}</h2>
      <p class="mt-2 text-2xl font-semibold tabular-nums text-gray-900 dark:text-white">{{ summary.controlled_requests }}</p>
      <p class="mt-1 text-xs text-gray-500 dark:text-gray-400">{{ $t('admin.ttft.metrics.clientCanceled', { count: summary.client_canceled_requests }) }}</p>
    </article>
    <article v-for="metric in [
      ['attemptTimeout', summary.attempt_ttft_timeout_rate], ['recovery', summary.recovery_rate], ['finalTTFTFailure', summary.final_ttft_failure_rate], ['otherFinalFailure', summary.other_final_failure_rate]
    ]" :key="String(metric[0])" class="rounded-lg border border-gray-200 bg-white p-4 dark:border-dark-700 dark:bg-dark-800">
      <h2 class="text-sm font-medium text-gray-600 dark:text-gray-300">{{ $t(`admin.ttft.metrics.${metric[0]}`) }}</h2>
      <p class="mt-2 text-2xl font-semibold tabular-nums text-gray-900 dark:text-white">{{ percent(metric[1] as RateMetric) }}</p>
      <p class="mt-1 text-xs tabular-nums text-gray-500 dark:text-gray-400">{{ (metric[1] as RateMetric).numerator }} / {{ (metric[1] as RateMetric).denominator }}</p>
    </article>
  </section>
</template>
