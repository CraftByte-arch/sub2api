<script setup lang="ts">
import { computed, ref, watch } from 'vue'
import type { FirstTokenTimeoutSettings } from '@/api/admin/ttft'

const props = defineProps<{ settings: FirstTokenTimeoutSettings | null; loading: boolean; saving: boolean; error: string }>()
const emit = defineEmits<{ save: [payload: { enabled: boolean; timeout_seconds: number }] }>()

const enabled = ref(false)
const timeoutSeconds = ref(30)
const validationError = computed(() => !Number.isInteger(timeoutSeconds.value) || timeoutSeconds.value < 1 || timeoutSeconds.value > 300)

watch(
  () => props.settings,
  (settings) => {
    if (!settings) return
    enabled.value = settings.saved.enabled
    timeoutSeconds.value = settings.saved.timeout_seconds
  },
  { immediate: true }
)

function save() {
  if (validationError.value) return
  emit('save', { enabled: enabled.value, timeout_seconds: timeoutSeconds.value })
}
</script>

<template>
  <section class="border border-gray-200 bg-white p-4 dark:border-dark-700 dark:bg-dark-800" aria-labelledby="ttft-settings-title">
    <div class="flex flex-wrap items-start justify-between gap-3">
      <div class="min-w-0">
        <div class="flex flex-wrap items-center gap-2">
          <h1 id="ttft-settings-title" class="text-base font-semibold text-gray-900 dark:text-white">{{ $t('admin.ttft.settings.title') }}</h1>
          <span v-if="settings" class="rounded-full px-2 py-0.5 text-xs font-medium" :class="settings.effective.enabled ? 'bg-emerald-100 text-emerald-800 dark:bg-emerald-950/50 dark:text-emerald-300' : 'bg-gray-100 text-gray-600 dark:bg-dark-700 dark:text-gray-300'">{{ settings.effective.enabled ? $t('admin.ttft.settings.statusEnabled') : $t('admin.ttft.settings.statusDisabled') }}</span>
        </div>
        <p class="mt-1 text-sm text-gray-500 dark:text-gray-400">{{ $t('admin.ttft.settings.description') }}</p>
      </div>
      <div v-if="settings" class="pt-1 text-xs text-gray-500 dark:text-gray-400">{{ $t('admin.ttft.settings.loadedAt', { time: new Date(settings.loaded_at).toLocaleString() }) }}</div>
    </div>

    <div v-if="loading && !settings" class="mt-4 h-11 animate-pulse rounded-md bg-gray-100 dark:bg-dark-700" />
    <form v-else class="mt-4 flex flex-col gap-3 border-t border-gray-100 pt-3 sm:flex-row sm:flex-wrap sm:items-end dark:border-dark-700" @submit.prevent="save">
      <label class="flex h-10 items-center gap-2 text-sm font-medium text-gray-700 dark:text-gray-200">
        <input data-testid="ttft-enabled" v-model="enabled" type="checkbox" class="h-4 w-4 rounded border-gray-300 text-blue-600 focus:ring-blue-500" />
        {{ $t('admin.ttft.settings.enabled') }}
      </label>
      <label class="grid gap-1 text-xs font-medium text-gray-500 dark:text-gray-400">
        <span>{{ $t('admin.ttft.settings.timeoutSeconds') }}</span>
        <input v-model.number="timeoutSeconds" type="number" min="1" max="300" inputmode="numeric" class="h-10 w-32 rounded-md border border-gray-300 bg-white px-3 text-sm font-normal text-gray-900 outline-none focus:border-blue-500 focus:ring-2 focus:ring-blue-500/20 dark:border-dark-600 dark:bg-dark-900 dark:text-white" :aria-invalid="validationError" />
      </label>
      <button data-testid="ttft-save" type="submit" :disabled="saving || validationError" class="h-10 rounded-md bg-blue-600 px-4 text-sm font-medium text-white transition-colors hover:bg-blue-700 focus:outline-none focus:ring-2 focus:ring-blue-500 focus:ring-offset-2 disabled:cursor-not-allowed disabled:opacity-60 dark:focus:ring-offset-dark-950">{{ saving ? $t('common.saving') : $t('common.save') }}</button>
      <p v-if="validationError" class="pb-2 text-sm text-red-600 dark:text-red-400">{{ $t('admin.ttft.settings.timeoutError') }}</p>
      <p v-else-if="error" class="pb-2 text-sm text-red-600 dark:text-red-400">{{ error }}</p>
      <p v-else-if="settings" class="pb-2 text-sm text-gray-600 dark:text-gray-300">{{ settings.effective.enabled ? $t('admin.ttft.settings.effectiveEnabled', { seconds: settings.effective.timeout_seconds }) : $t('admin.ttft.settings.effectiveDisabled') }}</p>
    </form>
  </section>
</template>
