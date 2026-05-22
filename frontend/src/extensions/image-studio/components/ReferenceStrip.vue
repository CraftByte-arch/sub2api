<template>
  <div
    ref="stripRef"
    class="reference-strip"
    :class="[
      `reference-strip-${props.variant}`,
      `reference-expand-${props.expandMode}`,
      {
        expanded: (isExpandable && isVisuallyExpanded) || props.interactionMode === 'static',
        resizing: isExpandable && resizing,
        hidden: props.hidden
      }
    ]"
    :style="rootStyle"
  >
    <div class="reference-strip-header reference-compact-bar">
      <div
        v-if="isExpandable"
        class="reference-resize-handle"
        @pointerdown.stop="handleResizePointerDown"
      >
        <span class="reference-resize-handle-bar"></span>
      </div>
      <div class="reference-strip-title">
        <span>参考图</span>
        <small>{{ files.length }}/{{ maxFiles }}</small>
      </div>
      <div class="reference-compact-thumbs" aria-label="参考图缩略图">
        <figure
          v-for="(file, index) in files"
          :key="`${file.name}-compact-${index}`"
          class="reference-compact-thumb"
        >
          <button
            type="button"
            class="reference-compact-preview"
            :aria-label="`查看 ${file.name}`"
            @click.stop="handlePreviewClick(file, index)"
          >
            <img
              v-if="previewUrls[index]"
              :src="previewUrls[index]"
              alt=""
              data-testid="reference-preview"
              @error="$emit('preview-error', index)"
            >
            <Icon v-else name="upload" size="xs" />
          </button>
          <span class="reference-compact-name">{{ file.name }}</span>
          <button
            type="button"
            class="reference-compact-remove"
            :aria-label="`移除 ${file.name}`"
            @click.stop="handleRemoveClick(index)"
          >
            移除
          </button>
        </figure>
        <label
          class="reference-compact-add"
          :class="{ disabled: files.length >= maxFiles }"
          :for="fileInputId"
          data-testid="reference-upload"
          @click="handleAddClick"
        >
          <span class="reference-compact-add-icon" aria-hidden="true">
            <Icon name="plus" size="xs" />
          </span>
          <span class="reference-compact-add-text">
            <strong>添加参考图</strong>
            <small>支持粘贴或上传</small>
          </span>
        </label>
      </div>
    </div>
    <input
      :id="fileInputId"
      class="reference-hidden-input"
      type="file"
      accept="image/png,image/jpeg,image/webp"
      multiple
      :disabled="files.length >= maxFiles"
      data-testid="reference-input"
      @change="$emit('change', $event)"
    >
  </div>
</template>

<script setup lang="ts">
import { computed, onBeforeUnmount, ref, watchEffect } from 'vue'
import Icon from '@/components/icons/Icon.vue'

const props = withDefaults(defineProps<{
  files: File[]
  previewUrls: string[]
  maxFiles: number
  hidden?: boolean
  variant?: 'floating' | 'inline'
  expandMode?: 'overlay' | 'flow'
  interactionMode?: 'expandable' | 'static'
}>(), {
  hidden: false,
  variant: 'floating',
  expandMode: 'flow',
  interactionMode: 'expandable'
})

const emit = defineEmits<{
  change: [event: Event]
  remove: [index: number]
  'preview-error': [index: number]
  'open-lightbox': [file: File, index: number]
}>()

const expanded = ref(props.interactionMode === 'static')
const resizing = ref(false)
const stripRef = ref<HTMLElement | null>(null)
const liveHeight = ref<number | null>(null)
const fileInputId = `reference-input-${Math.random().toString(36).slice(2, 10)}`
const dragState = ref<{
  pointerId: number
  startY: number
  startHeight: number
  collapsedHeight: number
  expandedHeight: number
} | null>(null)

const isExpandable = computed(() => props.variant === 'inline' && props.interactionMode !== 'static')
const isVisuallyExpanded = computed(() => {
  if (!isExpandable.value) return expanded.value
  const dims = readReferenceDimensions()
  const current = liveHeight.value ?? (expanded.value ? dims.expandedHeight : dims.collapsedHeight)
  return current > dims.collapsedHeight + 2
})
const rootStyle = computed<Record<string, string> | undefined>(() => {
  if (!isExpandable.value) return undefined
  const dims = readReferenceDimensions()
  const current = liveHeight.value ?? (expanded.value ? dims.expandedHeight : dims.collapsedHeight)
  const thumb = Math.max(current - 34, dims.collapsedThumbSize)
  return {
    '--reference-current-height': `${current}px`,
    '--reference-current-thumb-size': `${thumb}px`
  }
})

watchEffect((onCleanup) => {
  if (!isExpandable.value) return
  const element = stripRef.value
  const parent = element?.parentElement
  if (!parent) return
  const height = rootStyle.value?.['--reference-current-height']
  if (height) {
    parent.style.setProperty('--reference-current-height', height)
  }
  onCleanup(() => {
    parent.style.removeProperty('--reference-current-height')
  })
})

function handleResizePointerDown(event: PointerEvent) {
  if (!isExpandable.value) return
  event.preventDefault()
  const dims = readReferenceDimensions()
  const startHeight = liveHeight.value ?? (expanded.value ? dims.expandedHeight : dims.collapsedHeight)
  resizing.value = true
  document.body.classList.add('reference-resizing-active')
  dragState.value = {
    pointerId: event.pointerId,
    startY: event.clientY,
    startHeight,
    collapsedHeight: dims.collapsedHeight,
    expandedHeight: dims.expandedHeight
  }
  window.addEventListener('pointermove', handleResizePointerMove)
  window.addEventListener('pointerup', handleResizePointerUp)
  window.addEventListener('pointercancel', handleResizePointerUp)
}

function handleResizePointerMove(event: PointerEvent) {
  const state = dragState.value
  if (!state || event.pointerId !== state.pointerId) return
  const deltaY = event.clientY - state.startY
  const nextHeight = clampNumber(
    state.startHeight - deltaY,
    state.collapsedHeight,
    state.expandedHeight
  )
  liveHeight.value = nextHeight
  expanded.value = nextHeight > state.collapsedHeight + 2
}

function handleResizePointerUp(event: PointerEvent) {
  const state = dragState.value
  if (!state || event.pointerId !== state.pointerId) return
  const finalHeight = liveHeight.value ?? state.startHeight
  const collapsedThreshold = state.collapsedHeight + 6
  const expandedThreshold = state.expandedHeight - 6
  if (finalHeight <= collapsedThreshold) {
    expanded.value = false
    liveHeight.value = null
  } else if (finalHeight >= expandedThreshold) {
    expanded.value = true
    liveHeight.value = state.expandedHeight
  } else {
    expanded.value = true
    liveHeight.value = finalHeight
  }
  resizing.value = false
  document.body.classList.remove('reference-resizing-active')
  dragState.value = null
  window.removeEventListener('pointermove', handleResizePointerMove)
  window.removeEventListener('pointerup', handleResizePointerUp)
  window.removeEventListener('pointercancel', handleResizePointerUp)
}

function handlePreviewClick(file: File, index: number) {
  emit('open-lightbox', file, index)
}

function handleRemoveClick(index: number) {
  emit('remove', index)
}

function handleAddClick(event: MouseEvent) {
  event.stopPropagation()
  if (props.files.length >= props.maxFiles) {
    event.preventDefault()
    return
  }
}

onBeforeUnmount(() => {
  document.body.classList.remove('reference-resizing-active')
  window.removeEventListener('pointermove', handleResizePointerMove)
  window.removeEventListener('pointerup', handleResizePointerUp)
  window.removeEventListener('pointercancel', handleResizePointerUp)
})

function readReferenceDimensions() {
  const element = stripRef.value
  if (!element || typeof window === 'undefined') {
    return {
      collapsedHeight: 68,
      expandedHeight: 168,
      collapsedThumbSize: 46
    }
  }
  const styles = window.getComputedStyle(element)
  const collapsedHeight = parseCssPixels(styles.getPropertyValue('--reference-strip-height'), 68)
  const expandedHeight = parseCssPixels(styles.getPropertyValue('--reference-strip-expanded-height'), 168)
  const collapsedThumbSize = parseCssPixels(styles.getPropertyValue('--reference-thumb-size'), Math.max(collapsedHeight - 22, 40))
  return { collapsedHeight, expandedHeight, collapsedThumbSize }
}

function parseCssPixels(value: string, fallback: number) {
  const parsed = Number.parseFloat(value)
  return Number.isFinite(parsed) ? parsed : fallback
}

function clampNumber(value: number, min: number, max: number) {
  return Math.min(Math.max(value, min), max)
}
</script>
