<script setup lang="ts">
import { computed } from 'vue'

type MetricTone = 'success' | 'danger' | 'warning' | 'neutral'

const props = withDefaults(defineProps<{
  label: string
  value: string
  context: string
  tone: MetricTone
  trend?: number[]
}>(), { trend: () => [] })

const strokeClasses: Record<MetricTone, string> = {
  success: 'stroke-emerald-500 dark:stroke-emerald-300',
  danger: 'stroke-red-500 dark:stroke-red-300',
  warning: 'stroke-amber-500 dark:stroke-amber-300',
  neutral: 'stroke-gray-500 dark:stroke-gray-300'
}

const fillClasses: Record<MetricTone, string> = {
  success: 'fill-emerald-500/10 dark:fill-emerald-300/10',
  danger: 'fill-red-500/10 dark:fill-red-300/10',
  warning: 'fill-amber-500/10 dark:fill-amber-300/10',
  neutral: 'fill-gray-500/10 dark:fill-gray-300/10'
}

const hasTrend = computed(() => props.trend.length >= 2)
const normalizedTrend = computed(() => {
  const minimum = Math.min(...props.trend)
  const maximum = Math.max(...props.trend)
  const span = maximum - minimum || 1
  return props.trend.map((value, index) => `${(index / (props.trend.length - 1)) * 100},${28 - ((value - minimum) / span) * 24}`)
})
const sparklinePoints = computed(() => normalizedTrend.value.join(' '))
const sparklineArea = computed(() => `0,32 ${sparklinePoints.value} 100,32`)
const trendSummary = computed(() => {
  if (!hasTrend.value) return ''
  const first = props.trend[0]
  const last = props.trend.at(-1) ?? first
  return `${props.label}趋势：${last >= first ? '上升' : '下降'} ${Math.abs(last - first).toFixed(2)}`
})
</script>

<template>
  <article class="relative min-w-0 overflow-hidden rounded-lg border border-gray-200 bg-white p-4 dark:border-dark-700 dark:bg-dark-800">
    <div class="flex items-start justify-between gap-3">
      <div class="min-w-0">
        <p class="text-xs font-medium text-gray-500 dark:text-gray-400">{{ label }}</p>
        <p class="mt-2 truncate text-2xl font-semibold tabular-nums text-gray-900 dark:text-white">{{ value }}</p>
      </div>
    </div>
    <div class="mt-3 flex min-h-8 items-end justify-between gap-3">
      <p class="min-w-0 text-xs text-gray-500 dark:text-gray-400">{{ context }}</p>
      <svg v-if="hasTrend" data-testid="metric-trend-sparkline" class="h-8 w-24 shrink-0" viewBox="0 0 100 32" role="img" :aria-label="`${label}趋势`" preserveAspectRatio="none">
        <polygon :points="sparklineArea" :class="fillClasses[tone]" />
        <polyline :points="sparklinePoints" fill="none" stroke-width="2" stroke-linecap="round" stroke-linejoin="round" :class="strokeClasses[tone]" />
      </svg>
    </div>
    <p v-if="hasTrend" class="sr-only">{{ trendSummary }}</p>
  </article>
</template>
