<script setup lang="ts">
import { computed, onMounted, ref, watch } from 'vue'
import { useRoute, useRouter } from 'vue-router'
import AppLayout from '@/components/layout/AppLayout.vue'
import performanceAPI, { type PerformanceAccountPage, type PerformanceOverview, type PerformanceRange } from '@/api/admin/performance'

const route = useRoute()
const router = useRouter()
const ranges: PerformanceRange[] = ['1h', '6h', '24h', '7d', '30d', '90d']
const range = ref<PerformanceRange>('24h')
const overview = ref<PerformanceOverview | null>(null)
const accounts = ref<PerformanceAccountPage | null>(null)
const loading = ref(true)
const error = ref('')
const platform = ref('')
const percent = (value: number) => `${(value * 100).toFixed(2)}%`
const ms = (value: number) => value > 0 ? `${value.toLocaleString()} ms` : '--'
const degraded = computed(() => overview.value?.collection_health.status === 'degraded')

async function load() {
  loading.value = true; error.value = ''
  try {
    const params = { range: range.value, platform: platform.value || undefined }
    ;[overview.value, accounts.value] = await Promise.all([performanceAPI.getOverview(params), performanceAPI.getAccounts({ ...params, sort: 'health_score', order: 'asc', page: 1, page_size: 20 })])
  } catch { error.value = '无法加载账号性能数据' } finally { loading.value = false }
}
async function syncQuery() { await router.replace({ query: { range: range.value, ...(platform.value ? { platform: platform.value } : {}) } }) }
watch([range, platform], async () => { await syncQuery(); await load() })
onMounted(async () => { range.value = ranges.includes(route.query.range as PerformanceRange) ? route.query.range as PerformanceRange : '24h'; platform.value = typeof route.query.platform === 'string' ? route.query.platform : ''; await load() })
</script>

<template>
  <AppLayout>
    <main class="mx-auto max-w-7xl space-y-5 px-4 py-6 sm:px-6">
      <div class="flex flex-wrap items-end justify-between gap-3">
        <div><h1 class="text-xl font-semibold text-gray-900 dark:text-white">账号性能</h1><p class="mt-1 text-sm text-gray-500">可用率、失败率与上游延迟</p></div>
        <div class="flex items-center gap-2"><select v-model="range" class="rounded-md border border-gray-300 bg-white px-3 py-2 text-sm dark:border-gray-700 dark:bg-dark-800"><option v-for="item in ranges" :key="item" :value="item">{{ item }}</option></select><input v-model.trim="platform" class="w-32 rounded-md border border-gray-300 bg-white px-3 py-2 text-sm dark:border-gray-700 dark:bg-dark-800" placeholder="平台" /></div>
      </div>
      <div v-if="degraded" class="border-l-4 border-amber-500 bg-amber-50 px-4 py-3 text-sm text-amber-900 dark:bg-amber-950/30 dark:text-amber-200">采集器存在丢样，当前指标可能不完整。</div>
      <div v-if="error" class="border-l-4 border-red-500 bg-red-50 px-4 py-3 text-sm text-red-700">{{ error }}</div>
      <div v-if="loading" class="grid grid-cols-2 gap-3 lg:grid-cols-5"><div v-for="item in 5" :key="item" class="h-24 animate-pulse rounded-md bg-gray-100 dark:bg-dark-800" /></div>
      <template v-else-if="overview">
        <section class="grid grid-cols-2 gap-3 lg:grid-cols-5"><div class="border border-gray-200 bg-white p-4 dark:border-gray-800 dark:bg-dark-900"><div class="text-xs text-gray-500">可用率</div><div class="mt-2 text-xl font-semibold">{{ percent(overview.summary.availability.rate) }}</div><div class="mt-1 text-xs text-gray-500">{{ overview.summary.availability.numerator }}/{{ overview.summary.availability.denominator }}</div></div><div class="border border-gray-200 bg-white p-4 dark:border-gray-800 dark:bg-dark-900"><div class="text-xs text-gray-500">失败率</div><div class="mt-2 text-xl font-semibold">{{ percent(overview.summary.failure_rate.rate) }}</div><div class="mt-1 text-xs text-gray-500">{{ overview.summary.failure_rate.numerator }}/{{ overview.summary.failure_rate.denominator }}</div></div><div class="border border-gray-200 bg-white p-4 dark:border-gray-800 dark:bg-dark-900"><div class="text-xs text-gray-500">P50 TTFT</div><div class="mt-2 text-xl font-semibold">{{ ms(overview.summary.p50_ttft_ms) }}</div></div><div class="border border-gray-200 bg-white p-4 dark:border-gray-800 dark:bg-dark-900"><div class="text-xs text-gray-500">P95 TTFT</div><div class="mt-2 text-xl font-semibold">{{ ms(overview.summary.p95_ttft_ms) }}</div></div><div class="border border-gray-200 bg-white p-4 dark:border-gray-800 dark:bg-dark-900"><div class="text-xs text-gray-500">P95 总耗时</div><div class="mt-2 text-xl font-semibold">{{ ms(overview.summary.p95_duration_ms) }}</div></div></section>
        <section class="border border-gray-200 bg-white dark:border-gray-800 dark:bg-dark-900"><div class="border-b border-gray-200 px-4 py-3 text-sm font-medium dark:border-gray-800">账号排名</div><div class="overflow-x-auto"><table class="min-w-full text-sm"><thead class="bg-gray-50 text-left text-xs text-gray-500 dark:bg-dark-800"><tr><th class="px-4 py-3">账号</th><th class="px-4 py-3">平台</th><th class="px-4 py-3">可用率</th><th class="px-4 py-3">失败率</th><th class="px-4 py-3">P95 TTFT</th><th class="px-4 py-3">样本</th></tr></thead><tbody><tr v-for="item in accounts?.items ?? []" :key="item.account_id" class="border-t border-gray-100 dark:border-gray-800"><td class="px-4 py-3">#{{ item.account_id }}</td><td class="px-4 py-3">{{ item.platform }}</td><td class="px-4 py-3">{{ percent(item.availability) }}</td><td class="px-4 py-3">{{ percent(item.failure_rate) }}</td><td class="px-4 py-3">{{ ms(item.p95_ttft_ms) }}</td><td class="px-4 py-3">{{ item.low_sample ? '样本不足' : '已采样' }}</td></tr><tr v-if="!accounts?.items?.length"><td colspan="6" class="px-4 py-10 text-center text-gray-500">所选时间段暂无性能样本</td></tr></tbody></table></div></section>
      </template>
    </main>
  </AppLayout>
</template>
