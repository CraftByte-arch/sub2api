<script setup lang="ts">
import { computed, onMounted, onUnmounted, ref } from 'vue'
import { BarElement, CategoryScale, Chart as ChartJS, Legend, LinearScale, Tooltip, type ChartData, type ChartOptions } from 'chart.js'
import { Bar } from 'vue-chartjs'

ChartJS.register(BarElement, CategoryScale, Legend, LinearScale, Tooltip)

interface FailureItem { Outcome: string; Count: number }

const props = withDefaults(defineProps<{
  failures: FailureItem[]
  title?: string
  loading?: boolean
}>(), { title: '失败分布', loading: false })

const outcomeLabels: Record<string, string> = {
  ttft_timeout: 'TTFT 超时',
  ttfttimeout: 'TTFT 超时',
  rate_limit: '限流',
  ratelimit: '限流',
  auth: '鉴权',
  upstream_4xx: '上游 4xx',
  upstream4xx: '上游 4xx',
  upstream_5xx: '上游 5xx',
  upstream5xx: '上游 5xx',
  transport: '网络传输',
  protocol: '协议',
  other_failure: '其他失败',
  otherfailure: '其他失败'
}
const normalizeOutcome = (outcome: string) => outcome.trim().replace(/([a-z])([A-Z])/g, '$1_$2').replace(/[ -]+/g, '_').toLowerCase()
const readableOutcome = (outcome: string) => outcomeLabels[normalizeOutcome(outcome)] ?? outcomeLabels.other_failure
const colorForOutcome = (outcome: string) => {
  const key = normalizeOutcome(outcome)
  if (key === 'ttft_timeout' || key === 'ttfttimeout') return '#ef4444'
  if (key === 'rate_limit' || key === 'ratelimit') return '#f97316'
  return '#64748b'
}

const themeVersion = ref(0)
let themeObserver: MutationObserver | undefined
const dark = computed(() => {
  themeVersion.value
  return document.documentElement.classList.contains('dark')
})
const visibleFailures = computed(() => props.failures.filter((failure) => Number(failure.Count) > 0))
const data = computed<ChartData<'bar', number[], string>>(() => ({
  labels: visibleFailures.value.map((failure) => readableOutcome(failure.Outcome)),
  datasets: [{ label: '失败次数', data: visibleFailures.value.map((failure) => Number(failure.Count)), backgroundColor: visibleFailures.value.map((failure) => colorForOutcome(failure.Outcome)), borderWidth: 0, borderRadius: 3 }]
}))
const options = computed<ChartOptions<'bar'>>(() => ({
  indexAxis: 'y',
  responsive: true,
  maintainAspectRatio: false,
  plugins: { legend: { display: false }, tooltip: { callbacks: { label: (context) => `${context.parsed.x ?? 0} 次` } } },
  scales: {
    x: { beginAtZero: true, ticks: { color: dark.value ? '#9ca3af' : '#6b7280', precision: 0 }, grid: { color: dark.value ? '#374151' : '#e5e7eb' } },
    y: { ticks: { color: dark.value ? '#d1d5db' : '#4b5563', font: { size: 11 } }, grid: { display: false } }
  }
}))
const summary = computed(() => visibleFailures.value.map((failure) => `${readableOutcome(failure.Outcome)}：${failure.Count} 次`).join('，'))

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
      <p v-if="loading" class="flex h-full items-center justify-center text-sm text-gray-500 dark:text-gray-400">加载中</p>
      <Bar v-else-if="visibleFailures.length" :data="data" :options="options" />
      <p v-else class="flex h-full items-center justify-center text-sm text-gray-500 dark:text-gray-400">暂无失败记录</p>
    </div>
  </section>
</template>
