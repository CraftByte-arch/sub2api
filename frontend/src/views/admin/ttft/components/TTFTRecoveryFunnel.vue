<script setup lang="ts">
import { computed } from 'vue'
import type { TTFTOverview } from '@/api/admin/ttft'

const props = defineProps<{ summary: TTFTOverview['summary'] }>()

const stages = computed(() => [
  { label: '受控请求', value: props.summary.controlled_requests, tone: 'border-blue-200 bg-blue-50 text-blue-800 dark:border-blue-900/70 dark:bg-blue-950/30 dark:text-blue-200' },
  { label: '触发超时', value: props.summary.attempt_ttft_timeout_rate.numerator, tone: 'border-red-200 bg-red-50 text-red-800 dark:border-red-900/70 dark:bg-red-950/30 dark:text-red-200' },
  { label: '换号恢复', value: props.summary.recovery_rate.numerator, tone: 'border-emerald-200 bg-emerald-50 text-emerald-800 dark:border-emerald-900/70 dark:bg-emerald-950/30 dark:text-emerald-200' },
  { label: '最终 TTFT 失败', value: props.summary.final_ttft_failure_rate.numerator, tone: 'border-violet-200 bg-violet-50 text-violet-800 dark:border-violet-900/70 dark:bg-violet-950/30 dark:text-violet-200' }
])

const visible = computed(() => props.summary.controlled_requests > 0)
const accessibleSummary = computed(() => stages.value.map((stage) => `${stage.label} ${stage.value}`).join('，'))
</script>

<template>
  <section
    v-if="visible"
    data-testid="ttft-recovery-funnel"
    class="border border-gray-200 bg-white p-4 dark:border-dark-700 dark:bg-dark-800"
    :aria-label="accessibleSummary"
  >
    <div class="mb-4 flex items-center justify-between gap-3">
      <div>
        <h2 class="text-sm font-semibold text-gray-900 dark:text-white">首 Token 保护路径</h2>
        <p class="mt-1 text-xs text-gray-500 dark:text-gray-400">超时后的换号恢复与最终结果</p>
      </div>
      <span class="text-xs tabular-nums text-gray-500 dark:text-gray-400">{{ summary.controlled_requests.toLocaleString() }} 请求</span>
    </div>
    <ol class="grid gap-2 sm:grid-cols-4">
      <li v-for="(stage, index) in stages" :key="stage.label" class="relative min-w-0">
        <div class="min-h-20 border p-3" :class="stage.tone">
          <div class="text-xs font-medium">{{ stage.label }}</div>
          <div class="mt-2 text-2xl font-semibold tabular-nums">{{ stage.value.toLocaleString() }}</div>
        </div>
        <div v-if="index < stages.length - 1" class="hidden text-center text-gray-400 sm:block sm:absolute sm:-right-2 sm:top-8 sm:z-10 sm:w-4" aria-hidden="true">→</div>
      </li>
    </ol>
  </section>
</template>
