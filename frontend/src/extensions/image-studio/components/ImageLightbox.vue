<template>
  <div
    v-if="image"
    class="image-lightbox"
    data-testid="image-lightbox"
    role="dialog"
    aria-modal="true"
    @click.self="$emit('close')"
  >
    <div class="lightbox-toolbar">
      <div>
        <p>{{ image.kind === 'reference' ? '参考图' : '图片预览' }}</p>
        <h2>{{ image.title }}</h2>
      </div>
      <div class="lightbox-actions">
        <button
          v-if="image.canEdit"
          type="button"
          class="lightbox-action"
          data-testid="lightbox-edit"
          @click="$emit('edit')"
        >
          <Icon name="edit" size="sm" />
          编辑
        </button>
        <a
          v-if="image.downloadName"
          class="lightbox-action"
          :href="image.src"
          :download="image.downloadName"
          data-testid="lightbox-download"
        >
          <Icon name="download" size="sm" />
          下载
        </a>
        <button type="button" class="lightbox-action" data-testid="lightbox-zoom-out" @click="$emit('zoom', -0.25)">
          -
        </button>
        <span class="lightbox-zoom-label">{{ Math.round(zoom * 100) }}%</span>
        <button type="button" class="lightbox-action" data-testid="lightbox-zoom-in" @click="$emit('zoom', 0.25)">
          +
        </button>
        <button type="button" class="lightbox-action" data-testid="lightbox-reset" @click="$emit('reset')">
          适应
        </button>
        <button type="button" class="lightbox-close" data-testid="lightbox-close" @click="$emit('close')">
          关闭
        </button>
      </div>
    </div>
    <div
      class="lightbox-stage"
      :class="{ dragging }"
      data-testid="lightbox-stage"
      @pointerdown="$emit('drag-start', $event)"
      @pointermove="$emit('drag-move', $event)"
      @pointerup="$emit('drag-end', $event)"
      @pointercancel="$emit('drag-end', $event)"
      @wheel.prevent="$emit('wheel', $event)"
    >
      <img
        :src="image.src"
        :alt="image.title"
        data-testid="lightbox-image"
        draggable="false"
        :style="imageStyle"
      >
    </div>
  </div>
</template>

<script setup lang="ts">
import Icon from '@/components/icons/Icon.vue'
import type { CSSProperties } from 'vue'
import type { ImageStudioLightboxImage } from '../types'

defineProps<{
  image: ImageStudioLightboxImage | null
  zoom: number
  dragging: boolean
  imageStyle: CSSProperties
}>()

defineEmits<{
  close: []
  edit: []
  reset: []
  zoom: [delta: number]
  wheel: [event: WheelEvent]
  'drag-start': [event: PointerEvent]
  'drag-move': [event: PointerEvent]
  'drag-end': [event: PointerEvent]
}>()
</script>
