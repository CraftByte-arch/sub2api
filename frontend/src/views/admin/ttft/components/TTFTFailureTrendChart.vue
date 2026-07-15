<script setup lang="ts">
import { computed, onMounted, onUnmounted, ref } from 'vue'
import { Chart as ChartJS, CategoryScale, Legend, LineElement, LinearScale, PointElement, Tooltip, type ChartData, type ChartOptions } from 'chart.js'
import { Line } from 'vue-chartjs'
import { useI18n } from 'vue-i18n'
import type { RateMetric, TTFTTrendPoint } from '@/api/admin/ttft'

ChartJS.register(CategoryScale, Legend, LineElement, LinearScale, PointElement, Tooltip)
const props = defineProps<{ points: TTFTTrendPoint[] }>()
const { t } = useI18n()
const themeVersion = ref(0)
let themeObserver: MutationObserver | undefined
const dark = computed(() => {
  themeVersion.value
  return document.documentElement.classList.contains('dark')
})
type TrendRateKey = 'attempt_ttft_timeout_rate' | 'recovery_rate' | 'final_ttft_failure_rate' | 'other_final_failure_rate'
const definitions: Array<{ key: TrendRateKey; label: string; color: string; dash: number[] }> = [
  { key: 'attempt_ttft_timeout_rate', label: 'admin.ttft.metrics.attemptTimeout', color: '#dc2626', dash: [] },
  { key: 'recovery_rate', label: 'admin.ttft.metrics.recovery', color: '#2563eb', dash: [6, 3] },
  { key: 'final_ttft_failure_rate', label: 'admin.ttft.metrics.finalTTFTFailure', color: '#9333ea', dash: [2, 2] },
  { key: 'other_final_failure_rate', label: 'admin.ttft.metrics.otherFinalFailure', color: '#d97706', dash: [8, 3, 2, 3] }
]
const data = computed<ChartData<'line', number[], string>>(() => ({
  labels: props.points.map((point) => new Date(point.bucket_start).toLocaleString()),
  datasets: definitions.map(({ key, label, color, dash }) => ({ label: t(label), data: props.points.map((point) => Number(point[key].rate ?? 0) * 100), borderColor: color, borderDash: dash, tension: 0.25, pointRadius: 1, pointHitRadius: 8 }))
}))

function formatMetric(metric: RateMetric | undefined) {
  if (!metric) return '0.0% (0 / 0)'
  return `${(Number(metric.rate ?? 0) * 100).toFixed(1)}% (${metric.numerator} / ${metric.denominator})`
}

const options = computed<ChartOptions<'line'>>(() => ({ responsive: true, maintainAspectRatio: false, interaction: { intersect: false, mode: 'index' }, plugins: { legend: { labels: { color: dark.value ? '#d1d5db' : '#4b5563' } }, tooltip: { callbacks: { label: (context) => {
  const definition = definitions[context.datasetIndex]
  const metric = definition ? props.points[context.dataIndex]?.[definition.key] : undefined
  return `${context.dataset.label ?? ''}: ${formatMetric(metric)}`
} } } }, scales: { x: { ticks: { color: dark.value ? '#9ca3af' : '#6b7280', maxTicksLimit: 6 }, grid: { display: false } }, y: { beginAtZero: true, ticks: { color: dark.value ? '#9ca3af' : '#6b7280', callback: (value) => `${value}%` }, grid: { color: dark.value ? '#374151' : '#e5e7eb' } } } }))
const summary = computed(() => props.points.length ? definitions.map(({ key, label }) => `${t(label)}: ${formatMetric(props.points.at(-1)?.[key])}`).join(', ') : '')

onMounted(() => {
  themeObserver = new MutationObserver(() => { themeVersion.value++ })
  themeObserver.observe(document.documentElement, { attributes: true, attributeFilter: ['class'] })
})
onUnmounted(() => themeObserver?.disconnect())
</script>

<template>
  <section class="min-w-0 border border-gray-200 bg-white p-4 dark:border-dark-700 dark:bg-dark-800" aria-labelledby="ttft-trend-title">
    <h2 id="ttft-trend-title" class="text-sm font-semibold text-gray-900 dark:text-white">{{ $t('admin.ttft.charts.failureTrend') }}</h2>
    <p class="sr-only">{{ summary }}</p>
    <div class="mt-3 h-72"><Line v-if="points.length" :data="data" :options="options" /><p v-else class="flex h-full items-center justify-center text-sm text-gray-500 dark:text-gray-400">{{ $t('admin.ttft.empty') }}</p></div>
  </section>
</template>
