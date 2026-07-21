<script setup lang="ts">
import { computed, onMounted, onUnmounted, ref } from 'vue'
import { useI18n } from 'vue-i18n'
import { BarElement, CategoryScale, Chart as ChartJS, Legend, LinearScale, Tooltip, type ChartData, type ChartOptions } from 'chart.js'
import { Bar } from 'vue-chartjs'

ChartJS.register(BarElement, CategoryScale, Legend, LinearScale, Tooltip)

interface FailureItem { label: string; count: number; color: string }

const props = withDefaults(defineProps<{
  failures: FailureItem[]
  title: string
  loading?: boolean
}>(), { loading: false })

const { t } = useI18n()

const themeVersion = ref(0)
let themeObserver: MutationObserver | undefined
const dark = computed(() => {
  themeVersion.value
  return document.documentElement.classList.contains('dark')
})
const visibleFailures = computed(() => props.failures.filter((failure) => failure.count > 0))
const data = computed<ChartData<'bar', number[], string>>(() => ({
  labels: visibleFailures.value.map((failure) => failure.label),
  datasets: [{ label: t('admin.monitoring.failures.countLabel'), data: visibleFailures.value.map((failure) => failure.count), backgroundColor: visibleFailures.value.map((failure) => failure.color), borderWidth: 0, borderRadius: 3 }]
}))
const options = computed<ChartOptions<'bar'>>(() => ({
  indexAxis: 'y',
  responsive: true,
  maintainAspectRatio: false,
  plugins: { legend: { display: false }, tooltip: { callbacks: { label: (context) => t('admin.monitoring.failures.tooltipCount', { count: context.parsed.x ?? 0 }) } } },
  scales: {
    x: { beginAtZero: true, ticks: { color: dark.value ? '#9ca3af' : '#6b7280', precision: 0 }, grid: { color: dark.value ? '#374151' : '#e5e7eb' } },
    y: { ticks: { color: dark.value ? '#d1d5db' : '#4b5563', font: { size: 11 } }, grid: { display: false } }
  }
}))
const summary = computed(() => visibleFailures.value.map((failure) => `${failure.label}：${failure.count}`).join('，'))

onMounted(() => {
  themeObserver = new MutationObserver(() => { themeVersion.value++ })
  themeObserver.observe(document.documentElement, { attributes: true, attributeFilter: ['class'] })
})
onUnmounted(() => themeObserver?.disconnect())
</script>

<template>
  <section class="min-w-0" :aria-label="title">
    <h2 class="text-sm font-semibold text-gray-900 dark:text-white">{{ title }}</h2>
    <p v-if="summary" class="sr-only">{{ summary }}</p>
    <div class="mt-3 h-72">
      <p v-if="loading" class="flex h-full items-center justify-center text-sm text-gray-500 dark:text-gray-400"><slot name="loading">{{ t('admin.monitoring.failures.loading') }}</slot></p>
      <Bar v-else-if="visibleFailures.length" :data="data" :options="options" />
      <p v-else class="flex h-full items-center justify-center text-sm text-gray-500 dark:text-gray-400"><slot name="empty">{{ t('admin.monitoring.failures.empty') }}</slot></p>
    </div>
  </section>
</template>
