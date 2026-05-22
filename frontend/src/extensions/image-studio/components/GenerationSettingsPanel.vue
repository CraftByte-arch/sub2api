<template>
  <template v-if="activeTab === 'history'">
    <section class="control-section detail-card" aria-label="记录详情">
      <div class="section-title">
        <span>记录详情</span>
        <small>Local</small>
      </div>
      <template v-if="selectedHistoryRecord">
        <button
          type="button"
          class="detail-thumb-button"
          @click="$emit('open-history-lightbox', selectedHistoryRecord)"
        >
          <img
            class="detail-thumb"
            :src="selectedHistoryRecord.images[0]?.src"
            :alt="selectedHistoryRecord.prompt || '生成记录'"
          >
        </button>
        <div class="detail-meta-line" aria-label="记录参数摘要">
          <span>{{ selectedHistoryRecord.mode === 'edit' ? '图生图' : '文生图' }}</span>
          <span>{{ selectedHistoryRecord.model }}</span>
          <span>{{ selectedHistoryRecord.size }}</span>
          <span>{{ selectedHistoryRecord.outputFormat.toUpperCase() }}</span>
        </div>
        <dl class="detail-list">
          <div>
            <dt>模式</dt>
            <dd>{{ selectedHistoryRecord.mode === 'edit' ? '图生图' : '文生图' }}</dd>
          </div>
          <div>
            <dt>模型</dt>
            <dd>{{ selectedHistoryRecord.model }}</dd>
          </div>
          <div>
            <dt>尺寸</dt>
            <dd>{{ selectedHistoryRecord.size }}</dd>
          </div>
          <div>
            <dt>格式</dt>
            <dd>{{ selectedHistoryRecord.outputFormat.toUpperCase() }}</dd>
          </div>
        </dl>
        <p class="detail-prompt">{{ selectedHistoryRecord.prompt || '未填写提示词' }}</p>
        <div v-if="selectedHistoryRecord.images[0]" class="detail-actions">
          <button
            type="button"
            class="result-edit"
            data-testid="history-edit-button"
            @click="$emit('edit-history-image', selectedHistoryRecord)"
          >
            <Icon name="edit" size="sm" />
            编辑
          </button>
          <a
            class="result-download"
            :href="selectedHistoryRecord.images[0].src"
            :download="historyDownloadFileName(selectedHistoryRecord)"
            data-testid="history-download-button"
          >
            <Icon name="download" size="sm" />
            下载
          </a>
          <button
            type="button"
            class="result-delete"
            data-testid="history-delete-button"
            aria-label="删除记录"
            title="删除记录"
            @click="$emit('delete-history-record', selectedHistoryRecord)"
          >
            <Icon name="trash" size="sm" />
          </button>
        </div>
      </template>
      <p v-else class="control-hint">选择一条记录后，可以查看参数、下载图片或继续编辑。</p>
    </section>
  </template>

  <template v-else>
    <section class="api-key-strip" aria-label="接口设置">
      <label class="api-key-strip-label" for="studio-api-key">API 密钥</label>
      <div class="api-key-strip-control">
        <StudioSelect
          :model-value="selectedKeyValue"
          :options="apiKeyOptions"
          :placeholder="loadingKeys ? '加载中...' : '请选择 API 密钥'"
          placeholder-disabled
          empty-text="没有可用于生图的 API 密钥，先去 API 密钥创建一个吧。"
          button-class="studio-select"
          data-testid="api-key-select"
          aria-label="API 密钥"
          @update:model-value="$emit('update:selected-key-value', $event)"
        />
      </div>
    </section>

    <section class="control-section" aria-label="画面设置">
      <div class="section-title">
        <span>画面设置</span>
      </div>
      <div class="control-block">
        <div class="ratio-grid">
          <button
            v-for="ratio in ratioOptions"
            :key="ratio.value"
            type="button"
            class="ratio-card"
            :class="{ active: selectedRatioValue === ratio.value }"
            :data-testid="`ratio-option-${ratio.value}`"
            @click="$emit('update:selected-ratio-value', ratio.value)"
          >
            <span class="ratio-shape-frame">
              <span class="ratio-shape" :style="ratioShapeStyle(ratio.aspect)"></span>
            </span>
            <span class="ratio-label">{{ ratio.label }}</span>
          </button>
        </div>
        <p class="control-hint">先选比例，再选该比例下的输出分辨率。</p>
      </div>

      <div class="control-block">
        <div class="label-row">
          <span class="control-label">输出分辨率</span>
        </div>
        <div class="ratio-grid resolution-grid">
          <button
            v-for="option in resolutionOptions"
            :key="option.value"
            type="button"
            class="ratio-card resolution-card"
            :class="{ active: selectedResolutionValue === option.value }"
            :data-testid="`resolution-option-${option.value}`"
            @click="$emit('update:selected-resolution-value', option.value)"
          >
            <span class="resolution-card-top">
              <span class="ratio-label">{{ option.label }}</span>
              <span
                class="resolution-tier"
                :class="{ experimental: option.status === 'experimental' }"
              >
                {{ option.tier }}
              </span>
            </span>
            <span class="resolution-description">{{ option.description }}</span>
            <span v-if="option.status === 'experimental'" class="resolution-status">实验性</span>
          </button>
        </div>
        <p class="control-hint">超过 2560×1440 的高像素尺寸会标记为实验性，可能更慢或失败率更高。</p>
      </div>

    </section>

    <section class="advanced-card" aria-label="高级参数">
      <button
        type="button"
        class="advanced-header"
        :aria-expanded="advancedOpen"
        @click="$emit('update:advanced-open', !advancedOpen)"
      >
        <span class="advanced-title">
          <Icon name="cog" size="sm" />
          高级参数
        </span>
        <Icon class="advanced-caret" :class="{ open: advancedOpen }" name="chevronUp" size="sm" />
      </button>

      <div v-if="advancedOpen" class="advanced-content">
        <div class="control-block compact">
          <label class="control-label" for="studio-quality">质量档位</label>
          <StudioSelect
            :model-value="quality"
            :options="qualityOptions"
            button-class="studio-select"
            data-testid="quality-select"
            aria-label="质量档位"
            @update:model-value="$emit('update:quality', $event)"
          />
        </div>

        <div class="control-block compact">
          <label class="control-label" for="studio-background">背景</label>
          <StudioSelect
            :model-value="background"
            :options="backgroundOptions"
            button-class="studio-select"
            data-testid="background-select"
            aria-label="背景"
            @update:model-value="$emit('update:background', $event)"
          />
          <p class="control-hint">透明背景只对 PNG/WebP 生效。</p>
        </div>

        <div class="control-block compact">
          <label class="control-label" for="studio-output-format">输出格式</label>
          <StudioSelect
            :model-value="outputFormat"
            :options="outputFormatOptions"
            button-class="studio-select"
            data-testid="output-format-select"
            aria-label="输出格式"
            placement="top"
            @update:model-value="$emit('update:output-format', $event)"
          />
          <p class="control-hint">WebP 体积更小；PNG 兼容性更好。</p>
        </div>

        <details class="advanced-json-panel">
          <summary>JSON 扩展</summary>
          <textarea
            :value="advancedJson"
            spellcheck="false"
            placeholder='{"tool":{"moderation":"low"}}'
            @input="$emit('update:advanced-json', ($event.target as HTMLTextAreaElement).value)"
          ></textarea>
        </details>
      </div>
    </section>
  </template>
</template>

<script setup lang="ts">
import Icon from '@/components/icons/Icon.vue'
import StudioSelect from './StudioSelect.vue'
import type {
  ImageStudioHistoryRecord,
  ImageStudioMode,
  ImageStudioRatioOption,
  ImageStudioSelectOption,
  StudioApiKey
} from '../types'
import { computed } from 'vue'

const props = defineProps<{
  activeTab: ImageStudioMode
  selectedHistoryRecord: ImageStudioHistoryRecord | null
  historyDownloadFileName: (record: ImageStudioHistoryRecord) => string
  selectedKeyValue: string
  loadingKeys: boolean
  activeKeys: StudioApiKey[]
  ratioOptions: ImageStudioRatioOption[]
  selectedRatio: ImageStudioRatioOption
  selectedRatioValue: string
  resolutionOptions: ImageStudioSelectOption[]
  selectedResolutionValue: string
  advancedOpen: boolean
  quality: string
  qualityOptions: ImageStudioSelectOption[]
  background: string
  backgroundOptions: ImageStudioSelectOption[]
  outputFormat: string
  outputFormatOptions: ImageStudioSelectOption[]
  advancedJson: string
}>()

const apiKeyOptions = computed(() =>
  props.activeKeys.map((key) => ({
    value: key.key,
    label: key.name || `API Key #${key.id}`
  }))
)

defineEmits<{
  'open-history-lightbox': [record: ImageStudioHistoryRecord]
  'edit-history-image': [record: ImageStudioHistoryRecord]
  'delete-history-record': [record: ImageStudioHistoryRecord]
  'update:selected-key-value': [value: string]
  'update:selected-ratio-value': [value: string]
  'update:selected-resolution-value': [value: string]
  'update:advanced-open': [value: boolean]
  'update:quality': [value: string]
  'update:background': [value: string]
  'update:output-format': [value: string]
  'update:advanced-json': [value: string]
}>()

function ratioShapeStyle(aspect: string) {
  const [rawWidth, rawHeight] = aspect.split('/').map((item) => Number(item.trim()))
  const ratio = rawWidth > 0 && rawHeight > 0 ? rawWidth / rawHeight : 1
  const maxWidth = 24
  const maxHeight = 18
  const widthLimitedHeight = maxWidth / ratio
  if (widthLimitedHeight <= maxHeight) {
    return {
      width: `${maxWidth}px`,
      height: `${Math.max(widthLimitedHeight, 4)}px`
    }
  }
  return {
    width: `${Math.max(maxHeight * ratio, 4)}px`,
    height: `${maxHeight}px`
  }
}

</script>
