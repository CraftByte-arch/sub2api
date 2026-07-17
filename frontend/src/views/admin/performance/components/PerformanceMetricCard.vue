<script setup lang="ts">
import { computed } from 'vue'
import Icon from '@/components/icons/Icon.vue'

type PerformanceTone = 'success' | 'danger' | 'warning' | 'info' | 'neutral'
type PerformanceIconName = InstanceType<typeof Icon>['$props']['name']

const props = withDefaults(defineProps<{
  label: string
  value: string
  context: string
  tone: PerformanceTone
  icon: PerformanceIconName
  trend?: number[]
  wide?: boolean
}>(), { trend: () => [], wide: false })

const toneClasses: Record<PerformanceTone, string> = {
  success: 'bg-emerald-100 text-emerald-700 dark:bg-emerald-500/15 dark:text-emerald-300',
  danger: 'bg-red-100 text-red-700 dark:bg-red-500/15 dark:text-red-300',
  warning: 'bg-amber-100 text-amber-700 dark:bg-amber-500/15 dark:text-amber-300',
  info: 'bg-sky-100 text-sky-700 dark:bg-sky-500/15 dark:text-sky-300',
  neutral: 'bg-gray-100 text-gray-700 dark:bg-gray-700 dark:text-gray-200'
}

const strokeClasses: Record<PerformanceTone, string> = {
  success: 'stroke-emerald-500 dark:stroke-emerald-300',
  danger: 'stroke-red-500 dark:stroke-red-300',
  warning: 'stroke-amber-500 dark:stroke-amber-300',
  info: 'stroke-sky-500 dark:stroke-sky-300',
  neutral: 'stroke-gray-500 dark:stroke-gray-300'
}

const fillClasses: Record<PerformanceTone, string> = {
  success: 'fill-emerald-500/10 dark:fill-emerald-300/10',
  danger: 'fill-red-500/10 dark:fill-red-300/10',
  warning: 'fill-amber-500/10 dark:fill-amber-300/10',
  info: 'fill-sky-500/10 dark:fill-sky-300/10',
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
  <article class="relative min-w-0 overflow-hidden rounded-lg border border-gray-200 bg-white p-4 dark:border-dark-700 dark:bg-dark-800" :class="wide ? 'border-primary-200 bg-primary-50/30 sm:p-5 dark:border-primary-800 dark:bg-primary-950/10' : ''">
    <div class="flex items-start justify-between gap-3">
      <div class="min-w-0">
        <p class="text-xs font-medium text-gray-500 dark:text-gray-400">{{ label }}</p>
        <p class="mt-2 truncate text-2xl font-semibold tabular-nums text-gray-900 dark:text-white" :class="wide ? 'sm:text-3xl' : ''">{{ value }}</p>
      </div>
      <span class="flex h-9 w-9 shrink-0 items-center justify-center rounded-md" :class="toneClasses[tone]" aria-hidden="true">
        <Icon :name="icon" size="md" />
      </span>
    </div>
    <div class="mt-3 flex min-h-8 items-end justify-between gap-3">
      <p class="min-w-0 text-xs text-gray-500 dark:text-gray-400">{{ context }}</p>
      <svg v-if="hasTrend" data-testid="performance-sparkline" class="h-8 w-24 shrink-0" viewBox="0 0 100 32" role="img" :aria-label="`${label}趋势`" preserveAspectRatio="none">
        <polygon :points="sparklineArea" :class="fillClasses[tone]" />
        <polyline :points="sparklinePoints" fill="none" stroke-width="2" stroke-linecap="round" stroke-linejoin="round" :class="strokeClasses[tone]" />
      </svg>
    </div>
    <p v-if="hasTrend" class="sr-only">{{ trendSummary }}</p>
  </article>
</template>
