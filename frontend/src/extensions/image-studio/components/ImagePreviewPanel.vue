<template>
  <section class="preview-canvas">
    <div class="preview-header">
      <div>
        <p class="section-eyebrow">输出预览</p>
        <h2>{{ title }}</h2>
      </div>
      <div class="preview-header-actions">
        <slot name="actions"></slot>
        <span class="preview-meta-pill">
          <strong>{{ ratioLabel }}</strong>
          <i aria-hidden="true"></i>
          <b>{{ resolutionValue }}</b>
          <i aria-hidden="true"></i>
          <em>{{ outputFormat.toUpperCase() }}</em>
        </span>
      </div>
    </div>
    <div v-if="submitting" class="generating-preview" data-testid="generating-preview">
      <template v-if="outputs.length > 0">
        <div class="result-grid single streaming">
          <figure class="result-tile stream-result-tile">
            <div class="result-actions muted">
              <span class="stream-status-pill">
                {{ streamState.message }}
                <small v-if="streamState.detail">{{ streamState.detail }}</small>
              </span>
            </div>
            <div class="stream-image-wrap">
              <img
                :src="outputs[0].src"
                :alt="streamImageAlt"
                data-testid="stream-result-preview-image"
              >
              <div class="result-processing-overlay" data-testid="result-processing-overlay">
                <span class="result-processing-pill">
                  <span class="result-processing-spinner" aria-hidden="true"></span>
                  {{ processingOverlayText }}
                </span>
              </div>
            </div>
            <figcaption>
              <span>{{ streamCaption }}</span>
            </figcaption>
          </figure>
        </div>
      </template>
      <template v-else>
        <div class="generation-pulse">
          <Icon name="sparkles" size="xl" />
        </div>
        <h2>{{ generationTitle }}</h2>
        <p>{{ streamState.message }}</p>
        <div class="generation-skeleton" aria-hidden="true">
          <span></span>
          <span></span>
          <span></span>
        </div>
      </template>
    </div>
    <div v-else-if="errorMessage && outputs.length === 0" class="failed-preview" data-testid="failed-preview">
      <div class="failed-icon">
        <Icon name="exclamationTriangle" size="xl" />
      </div>
      <h2>生成失败</h2>
      <p>{{ errorMessage }}</p>
    </div>
    <div v-else-if="outputs.length === 0" class="empty-preview">
      <div class="empty-icon">
        <Icon name="sparkles" size="xl" />
      </div>
      <h2>还没有图片</h2>
      <p>在底部填写提示词，右侧确认参数后开始生成。</p>
    </div>
    <div v-else class="result-grid" :class="{ single: outputs.length === 1 }">
      <figure v-for="(output, index) in outputs" :key="output.id" class="result-tile">
        <div class="result-actions">
          <button
            type="button"
            class="result-edit"
            data-testid="edit-output-button"
            @click="$emit('edit-output', output, index)"
          >
            <Icon name="edit" size="sm" />
            编辑
          </button>
          <a
            class="result-download"
            :href="output.src"
            :download="downloadFileName(output, index)"
            data-testid="download-button"
          >
            <Icon name="download" size="sm" />
            下载
          </a>
        </div>
        <img
          :src="output.src"
          :alt="`Generated image ${index + 1}`"
          data-testid="result-preview-image"
          @click="$emit('open-lightbox', output, index)"
        >
      </figure>
    </div>
  </section>
</template>

<script setup lang="ts">
import { computed } from 'vue'
import Icon from '@/components/icons/Icon.vue'
import type { ImageStudioOutput, ImageStudioStreamState } from '../types'

const props = defineProps<{
  submitting: boolean
  outputs: ImageStudioOutput[]
  ratioLabel: string
  resolutionValue: string
  outputFormat: string
  errorMessage: string
  streamState: ImageStudioStreamState
  downloadFileName: (output: ImageStudioOutput, index: number) => string
}>()

defineEmits<{
  'edit-output': [output: ImageStudioOutput, index: number]
  'open-lightbox': [output: ImageStudioOutput, index: number]
}>()

const title = computed(() => {
  if (props.submitting) return '正在生成'
  return props.outputs.length > 0 ? `${props.outputs.length} 张图片` : '等待生成'
})

const generationTitle = computed(() => {
  if (props.streamState.phase === 'partial_preview') return '正在细化画面'
  if (props.streamState.phase === 'image_done') return '正在完成收尾'
  if (props.streamState.phase === 'generating') return '正在绘制画面'
  return '正在理解你的提示词'
})

const streamImageAlt = computed(() =>
  props.streamState.phase === 'partial_preview' ? '生成预览图' : '生成结果处理中'
)

const processingOverlayText = computed(() =>
  props.streamState.phase === 'partial_preview' ? '正在继续处理细节' : '正在准备最终结果'
)

const streamCaption = computed(() =>
  props.streamState.phase === 'partial_preview'
    ? `先给你看一眼当前画面，细节还在继续打磨。${props.streamState.detail ? ` ${props.streamState.detail}` : ''}`
    : '画面已经生成，正在做最后整理。'
)
</script>
