<script setup lang="ts">
import { computed, onMounted, onUnmounted, ref } from 'vue'
import { CategoryScale, Chart as ChartJS, Filler, Legend, LineElement, LinearScale, PointElement, Tooltip, type ChartData, type ChartOptions } from 'chart.js'
import { Line } from 'vue-chartjs'
import type { PerformanceTimePoint } from '@/api/admin/performance'

ChartJS.register(CategoryScale, Filler, Legend, LineElement, LinearScale, PointElement, Tooltip)

export interface PerformanceSeriesDefinition {
  label: string
  color: string
  selector: (point: PerformanceTimePoint) => number
  formatter: (value: number) => string
  fill?: boolean
}

const props = withDefaults(defineProps<{
  title: string
  points: PerformanceTimePoint[]
  timeRange: string
  series: PerformanceSeriesDefinition[]
  loading?: boolean
}>(), { loading: false })

const themeVersion = ref(0)
let themeObserver: MutationObserver | undefined
const dark = computed(() => {
  themeVersion.value
  return document.documentElement.classList.contains('dark')
})
const formatLabel = (bucketStart: string) => {
  const date = new Date(bucketStart)
  return props.timeRange === '1h' || props.timeRange === '6h'
    ? date.toLocaleTimeString([], { hour: '2-digit', minute: '2-digit' })
    : date.toLocaleString([], { month: 'numeric', day: 'numeric', hour: '2-digit', minute: '2-digit' })
}
const data = computed<ChartData<'line', number[], string>>(() => ({
  labels: props.points.map((point) => formatLabel(point.bucket_start)),
  datasets: props.series.map((definition) => ({
    label: definition.label,
    data: props.points.map(definition.selector),
    borderColor: definition.color,
    backgroundColor: definition.fill === false ? 'transparent' : `${definition.color}20`,
    fill: definition.fill !== false,
    tension: 0.35,
    pointRadius: 0,
    pointHitRadius: 10,
    borderWidth: 2
  }))
}))
const options = computed<ChartOptions<'line'>>(() => ({
  responsive: true,
  maintainAspectRatio: false,
  interaction: { intersect: false, mode: 'index' },
  plugins: {
    legend: { position: 'top', align: 'end', labels: { color: dark.value ? '#d1d5db' : '#4b5563', usePointStyle: true, boxWidth: 7, font: { size: 11 } } },
    tooltip: {
      backgroundColor: dark.value ? '#1f2937' : '#ffffff',
      titleColor: dark.value ? '#f3f4f6' : '#111827',
      bodyColor: dark.value ? '#d1d5db' : '#4b5563',
      borderColor: dark.value ? '#374151' : '#e5e7eb',
      borderWidth: 1,
      callbacks: { label: (context) => `${context.dataset.label ?? ''}: ${props.series[context.datasetIndex]?.formatter(Number(context.parsed.y ?? 0)) ?? context.parsed.y}` }
    }
  },
  scales: {
    x: { grid: { display: false }, ticks: { color: dark.value ? '#9ca3af' : '#6b7280', font: { size: 10 }, maxTicksLimit: 8, autoSkipPadding: 12 } },
    y: { beginAtZero: true, grid: { color: dark.value ? '#374151' : '#e5e7eb', borderDash: [4, 4] }, ticks: { color: dark.value ? '#9ca3af' : '#6b7280', font: { size: 10 } } }
  }
}))
const summary = computed(() => props.points.length ? props.series.map((series) => `${series.label}: ${series.formatter(series.selector(props.points.at(-1)!))}`).join('，') : '')

onMounted(() => {
  themeObserver = new MutationObserver(() => { themeVersion.value++ })
  themeObserver.observe(document.documentElement, { attributes: true, attributeFilter: ['class'] })
})
onUnmounted(() => themeObserver?.disconnect())
</script>

<template>
  <section class="min-w-0" :aria-label="title">
    <div class="flex items-center justify-between gap-3">
      <h2 class="text-sm font-semibold text-gray-900 dark:text-white">{{ title }}</h2>
    </div>
    <p v-if="summary" class="sr-only">{{ summary }}</p>
    <div class="mt-3 h-72">
      <div v-if="loading" class="flex h-full items-center justify-center text-sm text-gray-500 dark:text-gray-400"><slot name="loading">加载中</slot></div>
      <Line v-else-if="points.length && series.length" :data="data" :options="options" />
      <p v-else class="flex h-full items-center justify-center text-sm text-gray-500 dark:text-gray-400"><slot name="empty">所选时间段暂无性能数据</slot></p>
    </div>
  </section>
</template>
