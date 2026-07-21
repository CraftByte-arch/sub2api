<script setup lang="ts">
import { computed } from 'vue'
import { useI18n } from 'vue-i18n'
import type { MonitoringTTFTSummary } from '@/api/admin/monitoring'

const props = defineProps<{ summary: MonitoringTTFTSummary }>()
const { t } = useI18n()

const visible = computed(() => props.summary.controlled_requests > 0)

const stages = computed(() => [
  { label: t('admin.monitoring.funnel.controlled'), value: props.summary.controlled_requests, rate: null as string | null, tone: 'border-blue-200 bg-blue-50 text-blue-800 dark:border-blue-900/70 dark:bg-blue-950/30 dark:text-blue-200' },
  { label: t('admin.monitoring.funnel.triggered'), value: props.summary.attempt_ttft_timeout_rate.numerator, rate: formatRate(props.summary.attempt_ttft_timeout_rate.rate), tone: 'border-red-200 bg-red-50 text-red-800 dark:border-red-900/70 dark:bg-red-950/30 dark:text-red-200' },
  { label: t('admin.monitoring.funnel.recovered'), value: props.summary.recovery_rate.numerator, rate: formatRate(props.summary.recovery_rate.rate), tone: 'border-emerald-200 bg-emerald-50 text-emerald-800 dark:border-emerald-900/70 dark:bg-emerald-950/30 dark:text-emerald-200' },
  { label: t('admin.monitoring.funnel.finalFailure'), value: props.summary.final_ttft_failure_rate.numerator, rate: formatRate(props.summary.final_ttft_failure_rate.rate), tone: 'border-amber-200 bg-amber-50 text-amber-800 dark:border-amber-900/70 dark:bg-amber-950/30 dark:text-amber-200' }
])

const accessibleSummary = computed(() => stages.value.map((stage) => `${stage.label} ${stage.value}`).join(t('admin.monitoring.funnel.summarySeparator')))

function formatRate(rate: number): string {
  return `${((Number.isFinite(rate) ? rate : 0) * 100).toFixed(1)}%`
}
</script>

<template>
  <section v-if="visible" data-testid="protection-funnel" class="rounded-lg border border-gray-200 bg-white p-4 dark:border-dark-700 dark:bg-dark-800" :aria-label="accessibleSummary">
    <div class="mb-4 flex items-center justify-between gap-3">
      <div>
        <h2 class="text-sm font-semibold text-gray-900 dark:text-white">{{ t('admin.monitoring.funnel.title') }}</h2>
        <p class="mt-1 text-xs text-gray-500 dark:text-gray-400">{{ t('admin.monitoring.funnel.subtitle') }}</p>
        <p class="mt-1 text-xs text-gray-400 dark:text-gray-500">{{ t('admin.monitoring.funnel.platformNote') }}</p>
      </div>
      <span class="text-xs tabular-nums text-gray-500 dark:text-gray-400">{{ summary.controlled_requests.toLocaleString() }}</span>
    </div>
    <ol class="grid gap-2 sm:grid-cols-4">
      <li v-for="(stage, index) in stages" :key="stage.label" class="relative min-w-0">
        <div class="min-h-20 rounded-md border p-3" :class="stage.tone">
          <div class="text-xs font-medium">{{ stage.label }}</div>
          <div class="mt-2 text-2xl font-semibold tabular-nums">{{ stage.value.toLocaleString() }}</div>
        </div>
        <div v-if="index < stages.length - 1" class="hidden text-center text-xs tabular-nums text-gray-400 sm:absolute sm:-right-2 sm:top-8 sm:z-10 sm:block sm:w-4" aria-hidden="true">→</div>
        <div v-if="stage.rate" class="mt-1 text-center text-xs tabular-nums text-gray-500 dark:text-gray-400">{{ stage.rate }}</div>
      </li>
    </ol>
  </section>
</template>
