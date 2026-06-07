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
        <div
          v-if="image.downloadName"
          class="download-menu lightbox-download-menu"
          :class="{ open: downloadMenuOpen }"
          @pointerdown.stop
        >
          <button
            type="button"
            class="lightbox-action"
            data-testid="lightbox-download"
            aria-haspopup="menu"
            :aria-expanded="downloadMenuOpen"
            @click.stop="downloadMenuOpen = !downloadMenuOpen"
          >
            <Icon name="download" size="sm" />
            下载
          </button>
          <div class="download-menu-options" role="menu" aria-label="选择下载格式">
            <button
              v-for="format in downloadFormats"
              :key="format.value"
              type="button"
              role="menuitem"
              :data-testid="`lightbox-download-${format.value}-button`"
              @click="selectDownloadFormat(format.value)"
            >
              {{ format.label }}
            </button>
          </div>
        </div>
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
import { onBeforeUnmount, onMounted, ref } from 'vue'
import type { CSSProperties } from 'vue'
import type { ImageStudioLightboxImage } from '../types'

const props = defineProps<{
  image: ImageStudioLightboxImage | null
  zoom: number
  dragging: boolean
  imageStyle: CSSProperties
}>()

const emit = defineEmits<{
  close: []
  edit: []
  reset: []
  zoom: [delta: number]
  wheel: [event: WheelEvent]
  'drag-start': [event: PointerEvent]
  'drag-move': [event: PointerEvent]
  'drag-end': [event: PointerEvent]
  download: [image: ImageStudioLightboxImage, format: string]
}>()

const downloadFormats = [
  { value: 'jpeg', label: 'JPEG' },
  { value: 'webp', label: 'WebP' },
  { value: 'png', label: 'PNG' }
]

const downloadMenuOpen = ref(false)

onMounted(() => {
  document.addEventListener('pointerdown', closeDownloadMenu)
})

onBeforeUnmount(() => {
  document.removeEventListener('pointerdown', closeDownloadMenu)
})

function closeDownloadMenu() {
  downloadMenuOpen.value = false
}

function selectDownloadFormat(format: string) {
  if (!props.image) return
  downloadMenuOpen.value = false
  emit('download', props.image, format)
}
</script>
