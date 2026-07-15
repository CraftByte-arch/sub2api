<script setup lang="ts">
import { computed } from 'vue'
import { BarElement, CategoryScale, Chart as ChartJS, Legend, LinearScale, Tooltip, type ChartData, type ChartOptions } from 'chart.js'
import { Bar } from 'vue-chartjs'
import type { TTFTOverview } from '@/api/admin/ttft'

ChartJS.register(BarElement, CategoryScale, Legend, LinearScale, Tooltip)
const props = defineProps<{ failures: TTFTOverview['other_failures'] }>()
const dark = computed(() => document.documentElement.classList.contains('dark'))
const data = computed<ChartData<'bar', number[], string>>(() => ({ labels: props.failures.map((item) => item.failure_kind), datasets: [{ label: 'Count', data: props.failures.map((item) => item.sample_count), backgroundColor: '#64748b', borderWidth: 0 }] }))
const options = computed<ChartOptions<'bar'>>(() => ({ indexAxis: 'y', responsive: true, maintainAspectRatio: false, plugins: { legend: { display: false }, tooltip: { callbacks: { label: (context) => String(context.parsed.x ?? 0) } } }, scales: { x: { ticks: { color: dark.value ? '#9ca3af' : '#6b7280', precision: 0 }, grid: { color: dark.value ? '#374151' : '#e5e7eb' } }, y: { ticks: { color: dark.value ? '#d1d5db' : '#4b5563' }, grid: { display: false } } } }))
</script>

<template>
  <section class="min-w-0 border border-gray-200 bg-white p-4 dark:border-dark-700 dark:bg-dark-800" aria-labelledby="ttft-distribution-title">
    <h2 id="ttft-distribution-title" class="text-sm font-semibold text-gray-900 dark:text-white">{{ $t('admin.ttft.charts.failureDistribution') }}</h2>
    <div class="mt-3 h-72"><Bar v-if="failures.length" :data="data" :options="options" /><p v-else class="flex h-full items-center justify-center text-sm text-gray-500 dark:text-gray-400">{{ $t('admin.ttft.empty') }}</p></div>
  </section>
</template>
