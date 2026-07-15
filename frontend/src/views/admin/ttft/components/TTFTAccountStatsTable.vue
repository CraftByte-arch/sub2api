<script setup lang="ts">
import type { TTFTAccountStats, TTFTAccountsPage, TTFTOrder } from '@/api/admin/ttft'

const props = defineProps<{ page: TTFTAccountsPage | null; loading: boolean; error: string; sort: string; order: TTFTOrder }>()
const emit = defineEmits<{ retry: []; sort: [value: string]; page: [value: number] }>()

function rate(metric: TTFTAccountStats['ttft_timeout_rate']) {
  return `${(metric.rate * 100).toFixed(1)}% (${metric.numerator} / ${metric.denominator})`
}

function toggleSort(column: string) {
  emit('sort', column)
}

function ariaSort(column: string) {
  if (props.sort !== column) return 'none'
  return props.order === 'asc' ? 'ascending' : 'descending'
}
</script>

<template>
  <section class="min-w-0 border border-gray-200 bg-white p-4 dark:border-dark-700 dark:bg-dark-800" aria-labelledby="ttft-accounts-title">
    <div class="flex items-center justify-between gap-3">
      <h2 id="ttft-accounts-title" class="text-sm font-semibold text-gray-900 dark:text-white">{{ $t('admin.ttft.accounts.title') }}</h2>
      <span v-if="page" class="text-xs tabular-nums text-gray-500 dark:text-gray-400">{{ page.total }}</span>
    </div>
    <div v-if="loading && !page" class="mt-4 space-y-2" aria-label="Loading account statistics"><div v-for="index in 5" :key="index" class="h-10 animate-pulse bg-gray-100 dark:bg-dark-700" /></div>
    <div v-else-if="error && !page" class="mt-4 flex items-center gap-3 text-sm text-red-600 dark:text-red-400"><span>{{ error }}</span><button data-testid="ttft-accounts-retry" type="button" class="rounded-lg border border-current px-3 py-1.5" @click="emit('retry')">{{ $t('common.refresh') }}</button></div>
    <template v-else>
      <div v-if="error" class="mt-4 flex items-center gap-3 text-sm text-red-600 dark:text-red-400"><span>{{ error }}</span><button data-testid="ttft-accounts-retry" type="button" class="rounded-lg border border-current px-3 py-1.5" @click="emit('retry')">{{ $t('common.refresh') }}</button></div>
      <div v-if="!page?.items.length" class="py-10 text-center text-sm text-gray-500 dark:text-gray-400">{{ $t('admin.ttft.empty') }}</div>
      <div v-else class="ttft-account-table mt-4 overflow-x-auto">
      <table class="w-full min-w-[900px] border-collapse text-left text-sm">
        <thead class="border-y border-gray-200 text-xs text-gray-500 dark:border-dark-700 dark:text-gray-400">
          <tr>
            <th class="px-3 py-2 font-medium">{{ $t('admin.ttft.accounts.account') }}</th>
            <th class="px-3 py-2 font-medium" :aria-sort="ariaSort('samples')"><button type="button" @click="toggleSort('samples')">{{ $t('admin.ttft.accounts.samples') }}</button></th>
            <th class="px-3 py-2 font-medium" :aria-sort="ariaSort('success_count')"><button type="button" @click="toggleSort('success_count')">{{ $t('admin.ttft.accounts.success') }}</button></th>
            <th class="px-3 py-2 font-medium" :aria-sort="ariaSort('ttft_timeout_rate')"><button type="button" @click="toggleSort('ttft_timeout_rate')">{{ $t('admin.ttft.accounts.ttft') }}</button></th>
            <th class="px-3 py-2 font-medium" :aria-sort="ariaSort('other_failure_rate')"><button type="button" @click="toggleSort('other_failure_rate')">{{ $t('admin.ttft.accounts.other') }}</button></th>
            <th class="px-3 py-2 font-medium" :aria-sort="ariaSort('avg_ttft_ms')"><button type="button" @click="toggleSort('avg_ttft_ms')">{{ $t('admin.ttft.accounts.avgTTFT') }}</button></th>
          </tr>
        </thead>
        <tbody class="divide-y divide-gray-100 dark:divide-dark-700">
          <tr v-for="item in page.items" :key="item.account_id" class="text-gray-700 dark:text-gray-200">
            <td class="px-3 py-3"><div class="font-medium text-gray-900 dark:text-white">{{ item.account_name || item.account_id }}</div><div class="text-xs text-gray-500">{{ item.platform }}</div><span v-if="item.low_sample" class="mt-1 inline-block text-xs text-amber-700 dark:text-amber-300">{{ $t('admin.ttft.accounts.lowSample') }}</span></td>
            <td class="px-3 py-3 tabular-nums">{{ item.samples }}</td><td class="px-3 py-3 tabular-nums">{{ item.success_count }}</td><td class="px-3 py-3 tabular-nums">{{ rate(item.ttft_timeout_rate) }}</td><td class="px-3 py-3 tabular-nums">{{ rate(item.other_failure_rate) }}</td><td class="px-3 py-3 tabular-nums">{{ Math.round(item.avg_ttft_ms) }} ms</td>
          </tr>
        </tbody>
      </table>
      </div>
    </template>
    <div v-if="page && page.pages > 1" class="mt-4 flex items-center justify-end gap-2 text-sm"><button type="button" :disabled="page.page <= 1" class="rounded-lg border border-gray-300 px-3 py-1.5 disabled:opacity-50 dark:border-dark-600" @click="emit('page', page.page - 1)">{{ $t('admin.ttft.accounts.previous') }}</button><span class="tabular-nums text-gray-500 dark:text-gray-400">{{ page.page }} / {{ page.pages }}</span><button type="button" :disabled="page.page >= page.pages" class="rounded-lg border border-gray-300 px-3 py-1.5 disabled:opacity-50 dark:border-dark-600" @click="emit('page', page.page + 1)">{{ $t('admin.ttft.accounts.next') }}</button></div>
  </section>
</template>
