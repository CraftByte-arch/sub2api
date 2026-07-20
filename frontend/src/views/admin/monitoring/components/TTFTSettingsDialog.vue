<script setup lang="ts">
import { computed, ref, watch } from 'vue'
import { useI18n } from 'vue-i18n'
import BaseDialog from '@/components/common/BaseDialog.vue'
import Toggle from '@/components/common/Toggle.vue'
import type { FirstTokenTimeoutSettings } from '@/api/admin/monitoring'

const props = defineProps<{ open: boolean; settings: FirstTokenTimeoutSettings | null; saving: boolean; error: string }>()
const emit = defineEmits<{ close: []; save: [payload: { enabled: boolean; timeout_seconds: number }] }>()

const { t } = useI18n()

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
  <BaseDialog :show="open" :title="t('admin.monitoring.settings.title')" width="wide" @close="emit('close')">
    <p class="text-sm text-gray-500 dark:text-gray-400">{{ t('admin.monitoring.settings.description') }}</p>
    <form class="mt-4 space-y-4" @submit.prevent="save">
      <label class="flex items-center gap-3 text-sm font-medium text-gray-700 dark:text-gray-200">
        <Toggle v-model="enabled" />
        {{ t('admin.monitoring.settings.enabled') }}
      </label>
      <label class="grid gap-1 text-xs font-medium text-gray-500 dark:text-gray-400">
        <span>{{ t('admin.monitoring.settings.timeoutSeconds') }}</span>
        <input v-model.number="timeoutSeconds" type="number" min="1" max="300" inputmode="numeric" class="h-10 w-32 rounded-md border border-gray-300 bg-white px-3 text-sm text-gray-900 outline-none focus:border-primary-500 focus:ring-2 focus:ring-primary-500/20 dark:border-dark-600 dark:bg-dark-900 dark:text-white" :aria-invalid="validationError" />
      </label>
      <p v-if="validationError" class="text-sm text-red-600 dark:text-red-400">{{ t('admin.monitoring.settings.timeoutError') }}</p>
      <p v-else-if="error" class="text-sm text-red-600 dark:text-red-400">{{ error }}</p>
      <p v-else-if="settings" class="text-sm text-gray-600 dark:text-gray-300">{{ settings.effective.enabled ? t('admin.monitoring.settings.effectiveEnabled', { seconds: settings.effective.timeout_seconds }) : t('admin.monitoring.settings.effectiveDisabled') }}</p>
      <div class="flex justify-end gap-2">
        <button type="button" class="h-10 rounded-md border border-gray-300 px-4 text-sm font-medium text-gray-700 hover:bg-gray-50 dark:border-dark-600 dark:text-gray-200 dark:hover:bg-dark-800" @click="emit('close')">{{ t('common.cancel') }}</button>
        <button data-testid="ttft-settings-save" type="submit" :disabled="saving || validationError" class="h-10 rounded-md bg-primary-600 px-4 text-sm font-medium text-white hover:bg-primary-700 disabled:cursor-not-allowed disabled:opacity-60">{{ saving ? t('common.saving') : t('common.save') }}</button>
      </div>
    </form>
  </BaseDialog>
</template>
