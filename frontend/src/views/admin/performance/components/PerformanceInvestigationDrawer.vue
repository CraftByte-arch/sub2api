<script setup lang="ts">
import { computed, nextTick, onMounted, onUnmounted, ref, watch } from 'vue'
import Icon from '@/components/icons/Icon.vue'
import { performanceMetricsFromCounters, type PerformanceAccountItem as PerformanceAccount, type PerformanceInvestigation } from '@/api/admin/performance'
import { acquireModalBodyLock, releaseModalBodyLock } from '@/utils/modalBodyLock'
import PerformanceFailureDistribution from './PerformanceFailureDistribution.vue'
import PerformanceMetricCard from './PerformanceMetricCard.vue'
import PerformanceTrendChart, { type PerformanceSeriesDefinition } from './PerformanceTrendChart.vue'

const props = defineProps<{
  open: boolean
  account: PerformanceAccount | null
  investigation: PerformanceInvestigation | null
  loading: boolean
  error: string
}>()

const emit = defineEmits<{ close: []; retry: [] }>()

let previousActiveElement: HTMLElement | null = null
let hasBodyLock = false
let inertDrawerCount = 0
let appWasInert = false
const dialogRef = ref<HTMLElement | null>(null)

const focusableSelector = [
  'a[href]',
  'area[href]',
  'button:not([disabled])',
  'input:not([disabled]):not([type="hidden"])',
  'select:not([disabled])',
  'textarea:not([disabled])',
  '[contenteditable="true"]',
  '[tabindex]:not([tabindex="-1"])'
].join(',')

const metrics = computed(() => props.account ? performanceMetricsFromCounters(props.account.counters) : null)
const trendSeries: PerformanceSeriesDefinition[] = [
  { label: '可用率', color: '#10b981', selector: (point) => performanceMetricsFromCounters(point.counters).availability * 100, formatter: (value) => `${value.toFixed(2)}%` },
  { label: '失败率', color: '#ef4444', selector: (point) => performanceMetricsFromCounters(point.counters).failure_rate * 100, formatter: (value) => `${value.toFixed(2)}%`, fill: false },
  { label: 'P95 TTFT', color: '#0ea5e9', selector: (point) => performanceMetricsFromCounters(point.counters).p95_ttft_ms, formatter: (value) => `${Math.round(value)} ms`, fill: false }
]

const hasInvestigation = computed(() => Boolean(props.investigation?.time_points.length || props.investigation?.failures.length))

function percent(value: number) {
  return `${(value * 100).toFixed(2)}%`
}

function milliseconds(value: number) {
  return value > 0 ? `${Math.round(value).toLocaleString()} ms` : '--'
}

function eligibleAttempts(account: PerformanceAccount) {
  return Math.max(0, account.counters.attempt_count - account.counters.client_canceled_count)
}

function failureCount(account: PerformanceAccount) {
  return Math.max(0, eligibleAttempts(account) - account.counters.success_count)
}

function makeBackgroundInert() {
  const appRoot = document.getElementById('app') as (HTMLElement & { inert?: boolean }) | null
  if (!appRoot) return
  if (inertDrawerCount === 0) appWasInert = appRoot.hasAttribute('inert')
  inertDrawerCount += 1
  appRoot.setAttribute('inert', '')
  appRoot.inert = true
}

function restoreBackgroundInteractivity() {
  const appRoot = document.getElementById('app') as (HTMLElement & { inert?: boolean }) | null
  if (!appRoot || inertDrawerCount === 0) return
  inertDrawerCount -= 1
  if (inertDrawerCount !== 0) return
  if (appWasInert) {
    appRoot.setAttribute('inert', '')
    appRoot.inert = true
  } else {
    appRoot.removeAttribute('inert')
    appRoot.inert = false
  }
}

function focusableElements() {
  return Array.from(dialogRef.value?.querySelectorAll<HTMLElement>(focusableSelector) ?? [])
    .filter((element) => element.tabIndex >= 0 && element.getAttribute('aria-hidden') !== 'true')
}

function restorePageState() {
  if (hasBodyLock) {
    releaseModalBodyLock()
    hasBodyLock = false
  }
  restoreBackgroundInteractivity()
  previousActiveElement?.focus?.()
  previousActiveElement = null
}

function handleEscape(event: KeyboardEvent) {
  if (props.open && event.key === 'Escape') emit('close')
}

function handleTab(event: KeyboardEvent) {
  if (!props.open || event.key !== 'Tab') return
  const elements = focusableElements()
  if (!elements.length) return
  const first = elements[0]
  const last = elements.at(-1)!
  const active = document.activeElement
  if (event.shiftKey && (active === first || !dialogRef.value?.contains(active))) {
    event.preventDefault()
    last.focus()
  } else if (!event.shiftKey && (active === last || !dialogRef.value?.contains(active))) {
    event.preventDefault()
    first.focus()
  }
}

watch(() => props.open, async (open) => {
  if (!open) {
    restorePageState()
    return
  }

  previousActiveElement = document.activeElement instanceof HTMLElement ? document.activeElement : null
  if (!hasBodyLock) {
    acquireModalBodyLock()
    hasBodyLock = true
  }
  makeBackgroundInert()
  await nextTick()
  focusableElements()[0]?.focus()
}, { immediate: true })

onMounted(() => {
  document.addEventListener('keydown', handleEscape)
  document.addEventListener('keydown', handleTab)
})
onUnmounted(() => {
  document.removeEventListener('keydown', handleEscape)
  document.removeEventListener('keydown', handleTab)
  restorePageState()
})
</script>

<template>
  <Teleport to="body">
    <div v-if="open" class="fixed inset-0 z-50 flex justify-end bg-gray-950/40" @click.self="emit('close')">
      <aside ref="dialogRef" role="dialog" aria-modal="true" aria-labelledby="performance-investigation-title" class="flex h-full w-full max-w-2xl flex-col bg-white shadow-xl dark:bg-dark-900">
        <header class="flex min-h-16 items-center justify-between border-b border-gray-200 px-4 dark:border-dark-700 sm:px-6">
          <div class="min-w-0">
            <h2 id="performance-investigation-title" class="truncate text-base font-semibold text-gray-900 dark:text-white">账号 #{{ account?.account_id ?? '--' }} 性能详情</h2>
            <p v-if="account" class="mt-0.5 text-sm text-gray-500 dark:text-gray-400">{{ account.platform }}</p>
          </div>
          <button data-testid="performance-investigation-close" type="button" class="flex h-11 w-11 shrink-0 items-center justify-center rounded-md text-gray-500 hover:bg-gray-100 hover:text-gray-900 focus:outline-none focus-visible:ring-2 focus-visible:ring-primary-500 focus-visible:ring-offset-2 dark:text-gray-400 dark:hover:bg-dark-800 dark:hover:text-white dark:focus-visible:ring-offset-dark-900" aria-label="关闭账号性能详情" @click="emit('close')"><Icon name="x" size="md" /></button>
        </header>

        <div class="min-h-0 flex-1 overflow-y-auto px-4 py-5 sm:px-6">
          <section v-if="account && metrics" class="grid grid-cols-2 gap-3 lg:grid-cols-4" aria-label="账号指标摘要">
            <PerformanceMetricCard label="可用率" :value="percent(metrics.availability)" :context="`${account.counters.success_count} / ${eligibleAttempts(account)} 次成功`" tone="success" icon="checkCircle" />
            <PerformanceMetricCard label="失败率" :value="percent(metrics.failure_rate)" :context="`${failureCount(account)} / ${eligibleAttempts(account)} 次失败`" tone="danger" icon="xCircle" />
            <PerformanceMetricCard label="P95 TTFT" :value="milliseconds(metrics.p95_ttft_ms)" context="首字节响应延迟" tone="info" icon="clock" />
            <PerformanceMetricCard label="P95 总耗时" :value="milliseconds(metrics.p95_duration_ms)" context="完整请求耗时" tone="neutral" icon="chart" />
          </section>

          <div v-if="loading" class="flex min-h-64 items-center justify-center text-sm text-gray-500 dark:text-gray-400">正在加载账号详情</div>
          <div v-else-if="error" class="mt-5 border-l-4 border-red-500 bg-red-50 px-4 py-3 text-sm text-red-800 dark:bg-red-500/10 dark:text-red-200">
            <p>{{ error }}</p>
            <button data-testid="performance-investigation-retry" type="button" class="mt-3 min-h-11 rounded-md border border-current px-3 py-2 font-medium focus:outline-none focus-visible:ring-2 focus-visible:ring-red-500 focus-visible:ring-offset-2 dark:focus-visible:ring-offset-dark-900" @click="emit('retry')">重试</button>
          </div>
          <div v-else-if="!hasInvestigation" class="flex min-h-64 items-center justify-center text-sm text-gray-500 dark:text-gray-400">暂无可供分析的性能数据</div>
          <div v-else class="mt-6 space-y-8">
            <PerformanceTrendChart title="性能趋势" :points="investigation?.time_points ?? []" time-range="当前时间范围" :series="trendSeries" />
            <PerformanceFailureDistribution title="失败分布" :failures="investigation?.failures ?? []" />
          </div>
        </div>
      </aside>
    </div>
  </Teleport>
</template>
