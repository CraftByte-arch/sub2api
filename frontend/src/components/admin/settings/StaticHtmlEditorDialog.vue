<template>
  <BaseDialog
    :show="show"
    :title="title"
    width="wide"
    :close-on-click-outside="false"
    @close="requestClose"
  >
    <div class="space-y-3">
      <div class="flex flex-wrap items-center justify-between gap-3">
        <div class="inline-flex rounded-md border border-gray-200 bg-gray-50 p-0.5 dark:border-dark-600 dark:bg-dark-800">
          <button
            type="button"
            data-testid="html-source-tab"
            :class="modeButtonClass('source')"
            @click="mode = 'source'"
          >
            <Icon name="document" size="sm" />
            <span>{{ t('admin.settings.customMenu.html.source') }}</span>
          </button>
          <button
            type="button"
            data-testid="html-preview-tab"
            :class="modeButtonClass('preview')"
            @click="mode = 'preview'"
          >
            <Icon name="eye" size="sm" />
            <span>{{ t('admin.settings.customMenu.html.preview') }}</span>
          </button>
        </div>

        <div class="flex items-center gap-3">
          <span
            data-testid="html-editor-status"
            class="text-xs text-gray-500 dark:text-dark-400"
          >
            {{
              dirty
                ? t('admin.settings.customMenu.html.unsaved')
                : t('admin.settings.customMenu.html.saved')
            }}
          </span>
          <input
            ref="fileInput"
            class="sr-only"
            type="file"
            accept=".html,text/html"
            @change="onFileSelected"
          />
          <button
            type="button"
            class="btn btn-secondary inline-flex items-center gap-2"
            @click="fileInput?.click()"
          >
            <Icon name="upload" size="sm" />
            <span>{{ t('admin.settings.customMenu.html.upload') }}</span>
          </button>
        </div>
      </div>

      <p
        v-if="error"
        role="alert"
        class="rounded-md border border-red-200 bg-red-50 px-3 py-2 text-sm text-red-700 dark:border-red-900/60 dark:bg-red-950/30 dark:text-red-300"
      >
        {{ error }}
      </p>

      <textarea
        v-if="mode === 'source'"
        v-model="draft"
        class="input h-[28rem] min-h-[20rem] w-full resize-y whitespace-pre font-mono text-sm leading-6"
        :aria-label="t('admin.settings.customMenu.html.source')"
        spellcheck="false"
        @input="clearError"
      />
      <div
        v-else
        class="h-[28rem] min-h-[20rem] overflow-hidden border border-gray-200 bg-white dark:border-dark-600"
      >
        <StaticHtmlFrame class="h-full w-full" :source="draft" :title="title" />
      </div>
    </div>

    <template #footer>
      <div class="flex justify-end gap-3">
        <button type="button" class="btn btn-secondary" @click="requestClose">
          {{ t('common.cancel') }}
        </button>
        <button
          type="button"
          data-testid="html-editor-apply"
          class="btn btn-primary inline-flex items-center gap-2"
          @click="apply"
        >
          <Icon name="check" size="sm" />
          <span>{{ t('admin.settings.customMenu.html.apply') }}</span>
        </button>
      </div>
    </template>
  </BaseDialog>

  <ConfirmDialog
    :show="showDiscardConfirm"
    :title="t('admin.settings.customMenu.html.discardTitle')"
    :message="t('admin.settings.customMenu.html.discardMessage')"
    :confirm-text="t('admin.settings.customMenu.html.discardAction')"
    :cancel-text="t('common.cancel')"
    danger
    @confirm="discardAndClose"
    @cancel="showDiscardConfirm = false"
  />
</template>

<script setup lang="ts">
import { computed, ref, watch } from 'vue'
import { useI18n } from 'vue-i18n'

import Icon from '@/components/icons/Icon.vue'
import BaseDialog from '@/components/common/BaseDialog.vue'
import ConfirmDialog from '@/components/common/ConfirmDialog.vue'
import StaticHtmlFrame from '@/components/common/StaticHtmlFrame.vue'
import { hasVisibleStaticHtml } from '@/utils/staticHtml'

const MAX_HTML_BYTES = 1 << 20

const props = defineProps<{
  show: boolean
  title: string
  source: string
}>()

const emit = defineEmits<{
  (event: 'apply', source: string): void
  (event: 'close'): void
}>()

const { t } = useI18n()
const fileInput = ref<HTMLInputElement | null>(null)
const draft = ref('')
const initialSource = ref('')
const error = ref('')
const mode = ref<'source' | 'preview'>('source')
const showDiscardConfirm = ref(false)

const dirty = computed(() => draft.value !== initialSource.value)

watch(
  () => [props.show, props.source] as const,
  ([show, source]) => {
    if (!show) return
    draft.value = source
    initialSource.value = source
    error.value = ''
    mode.value = 'source'
    showDiscardConfirm.value = false
  },
  { immediate: true }
)

function modeButtonClass(value: 'source' | 'preview'): string[] {
  return [
    'inline-flex items-center gap-2 rounded px-3 py-1.5 text-sm font-medium transition-colors',
    mode.value === value
      ? 'bg-white text-gray-900 shadow-sm dark:bg-dark-700 dark:text-white'
      : 'text-gray-500 hover:text-gray-800 dark:text-dark-400 dark:hover:text-dark-100'
  ]
}

function clearError(): void {
  error.value = ''
}

function readFileBuffer(file: File): Promise<ArrayBuffer> {
  if (typeof file.arrayBuffer === 'function') return file.arrayBuffer()
  return new Promise((resolve, reject) => {
    const reader = new FileReader()
    reader.onerror = () => reject(reader.error)
    reader.onload = () => resolve(reader.result as ArrayBuffer)
    reader.readAsArrayBuffer(file)
  })
}

async function onFileSelected(event: Event): Promise<void> {
  error.value = ''
  const input = event.target as HTMLInputElement
  const file = input.files?.[0]
  if (!file) return

  try {
    if (!file.name.toLowerCase().endsWith('.html')) {
      error.value = t('admin.settings.customMenu.html.invalidFile')
      return
    }
    if (file.size > MAX_HTML_BYTES) {
      error.value = t('admin.settings.customMenu.html.tooLarge')
      return
    }
    const content = new TextDecoder('utf-8', { fatal: true }).decode(
      await readFileBuffer(file)
    )
    draft.value = content
    mode.value = 'source'
  } catch {
    error.value = t('admin.settings.customMenu.html.invalidEncoding')
  } finally {
    input.value = ''
  }
}

function apply(): void {
  error.value = ''
  if (new TextEncoder().encode(draft.value).byteLength > MAX_HTML_BYTES) {
    error.value = t('admin.settings.customMenu.html.tooLarge')
    return
  }
  if (!hasVisibleStaticHtml(draft.value)) {
    error.value = t('admin.settings.customMenu.html.emptyAfterSanitize')
    return
  }
  emit('apply', draft.value)
}

function requestClose(): void {
  if (dirty.value) {
    showDiscardConfirm.value = true
    return
  }
  emit('close')
}

function discardAndClose(): void {
  showDiscardConfirm.value = false
  emit('close')
}
</script>
