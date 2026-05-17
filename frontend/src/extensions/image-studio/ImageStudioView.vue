<template>
  <AppLayout>
    <div class="image-studio-page">
      <section class="image-studio-shell" aria-labelledby="image-studio-title">
        <header class="studio-tabs" role="tablist" aria-label="Image studio modes">
          <button
            type="button"
            class="studio-tab"
            :class="{ active: activeTab === 'generate' }"
            data-testid="tab-generate"
            @click="activeTab = 'generate'"
          >
            <span class="tab-icon">◇</span>
            文生图
          </button>
          <button
            type="button"
            class="studio-tab"
            :class="{ active: activeTab === 'edit' }"
            data-testid="tab-edit"
            @click="activeTab = 'edit'"
          >
            <span class="tab-icon">↥</span>
            图生图
          </button>
          <button
            type="button"
            class="studio-tab"
            :class="{ active: activeTab === 'history' }"
            data-testid="tab-history"
            @click="activeTab = 'history'"
          >
            <span class="tab-icon">◷</span>
            记录
          </button>
        </header>

        <div v-if="activeTab === 'history'" class="history-panel">
          <div class="history-empty">
            <h1 id="image-studio-title">生成记录</h1>
            <p>当前版本先保留本地会话结果。刷新页面后记录会清空，后续可扩展为服务端历史。</p>
          </div>
        </div>

        <div v-else class="studio-body">
          <aside class="studio-controls" aria-label="Image generation controls">
            <div class="control-block">
              <label class="control-label" for="studio-api-key">API 密钥</label>
              <select id="studio-api-key" v-model="selectedKeyValue" class="studio-select" data-testid="api-key-select">
                <option value="" disabled>{{ loadingKeys ? '加载中...' : '请选择 API 密钥' }}</option>
                <option
                  v-for="key in activeKeys"
                  :key="key.id"
                  :value="key.key"
                >
                  {{ key.name || `API Key #${key.id}` }}
                </option>
              </select>
              <p class="control-hint">仅显示已启用的图片生成 API 密钥。</p>
            </div>

            <div class="control-block">
              <label class="control-label" for="studio-model">模型</label>
              <select id="studio-model" v-model="model" class="studio-select">
                <option value="gpt-5.4">gpt-5.4</option>
                <option value="gpt-5.4-mini">gpt-5.4-mini</option>
                <option value="gpt-image-2">gpt-image-2</option>
                <option value="gpt-image-1.5">gpt-image-1.5</option>
              </select>
            </div>

            <div class="control-row">
              <span class="control-label">接口协议</span>
              <div class="segmented">
                <button
                  type="button"
                  :class="{ active: protocol === 'responses' }"
                  @click="protocol = 'responses'"
                >
                  Responses
                </button>
                <button
                  type="button"
                  :class="{ active: protocol === 'images' }"
                  @click="protocol = 'images'"
                >
                  Images API
                </button>
              </div>
            </div>

            <div class="control-block">
              <div class="label-row">
                <span class="control-label">画面比例</span>
                <span class="ratio-value">{{ selectedRatio.label }}</span>
              </div>
              <div class="ratio-grid">
                <button
                  v-for="ratio in ratioOptions"
                  :key="ratio.value"
                  type="button"
                  class="ratio-card"
                  :class="{ active: selectedRatioValue === ratio.value }"
                  @click="selectedRatioValue = ratio.value"
                >
                  <span class="ratio-badge">{{ ratio.tier }}</span>
                  <span class="ratio-shape" :style="{ aspectRatio: ratio.aspect }"></span>
                  <span class="ratio-label">{{ ratio.label }}</span>
                </button>
              </div>
              <p class="control-hint">不同画面比例会映射到网关支持的尺寸档位，费用可能不同。</p>
            </div>

            <div class="control-block">
              <div class="label-row">
                <label class="control-label" for="studio-count">张数</label>
                <span class="ratio-value">{{ count }}</span>
              </div>
              <input id="studio-count" v-model.number="count" class="count-slider" type="range" min="1" max="4" step="1">
            </div>

            <div v-if="activeTab === 'edit'" class="upload-grid">
              <label class="upload-tile">
                <span>上传原图</span>
                <input type="file" accept="image/*" @change="handleImageChange">
              </label>
              <label class="upload-tile">
                <span>上传蒙版</span>
                <input type="file" accept="image/*" @change="handleMaskChange">
              </label>
            </div>

            <div class="control-block">
              <label class="control-label" for="studio-prompt">提示词</label>
              <textarea
                id="studio-prompt"
                v-model="prompt"
                class="prompt-input"
                data-testid="prompt-input"
                placeholder="描述画面的主体、风格、光线、构图... 越具体效果越好"
              ></textarea>
              <div class="label-row muted">
                <span>{{ prompt.length }} 字符</span>
                <button type="button" class="link-button" @click="prompt = ''">清空</button>
              </div>
            </div>

            <details class="advanced-panel">
              <summary>高级参数</summary>
              <textarea v-model="advancedJson" spellcheck="false" placeholder='{"tool":{"background":"transparent"}}'></textarea>
            </details>

            <p v-if="errorMessage" class="error-message">{{ errorMessage }}</p>

            <button
              type="button"
              class="generate-button"
              data-testid="generate-button"
              :disabled="submitDisabled"
              @click="handleSubmit"
            >
              {{ submitting ? '生成中...' : activeTab === 'edit' ? '生成改图' : '生成图片' }}
            </button>
          </aside>

          <section class="preview-canvas" aria-live="polite">
            <div v-if="outputs.length === 0" class="empty-preview">
              <div class="empty-icon">◇</div>
              <h2>还没有图片</h2>
              <p>在左侧填写提示词和参数，点击生成图片。</p>
            </div>
            <div v-else class="result-grid">
              <figure v-for="(output, index) in outputs" :key="output.id" class="result-tile">
                <img :src="output.src" :alt="`Generated image ${index + 1}`">
                <figcaption>
                  <span>{{ output.revisedPrompt || '生成图片' }}</span>
                  <a :href="output.src" target="_blank" rel="noopener noreferrer">打开</a>
                </figcaption>
              </figure>
            </div>
          </section>
        </div>
      </section>
    </div>
  </AppLayout>
</template>

<script setup lang="ts">
import { computed, onMounted, ref, watch } from 'vue'
import { keysAPI } from '@/api'
import AppLayout from '@/components/layout/AppLayout.vue'
import {
  sendImagesEditRequest,
  sendImagesGenerationRequest,
  sendResponsesImageRequest
} from './imageStudioApi'
import {
  buildImagesEditFormData,
  buildImagesGenerationPayload,
  buildResponsesEditPayload,
  buildResponsesGenerationPayload,
  parseAdvancedJson
} from './payload'
import { extractImageStudioOutputs } from './output'
import type { ImageStudioMode, ImageStudioOutput, ImageStudioProtocol } from './types'

interface StudioApiKey {
  id: number
  key: string
  name: string
  status: string
  group?: {
    platform?: string
    allow_image_generation?: boolean
  }
}

const activeTab = ref<ImageStudioMode>('generate')
const protocol = ref<ImageStudioProtocol>('responses')
const model = ref('gpt-5.4')
const selectedKeyValue = ref('')
const prompt = ref('')
const count = ref(1)
const selectedRatioValue = ref('1:1')
const advancedJson = ref('')
const loadingKeys = ref(false)
const submitting = ref(false)
const errorMessage = ref('')
const apiKeys = ref<StudioApiKey[]>([])
const outputs = ref<ImageStudioOutput[]>([])
const imageFile = ref<File | null>(null)
const maskFile = ref<File | null>(null)

const ratioOptions = [
  { value: '1:1', label: '1:1', tier: '1K', size: '1024x1024', aspect: '1 / 1' },
  { value: '16:9', label: '16:9', tier: '2K', size: '1536x864', aspect: '16 / 9' },
  { value: '9:16', label: '9:16', tier: '2K', size: '864x1536', aspect: '9 / 16' },
  { value: '21:9', label: '21:9', tier: '2K', size: '1792x768', aspect: '21 / 9' },
  { value: '4:3', label: '4:3', tier: '2K', size: '1536x1152', aspect: '4 / 3' },
  { value: '3:4', label: '3:4', tier: '2K', size: '1152x1536', aspect: '3 / 4' },
  { value: '3:2', label: '3:2', tier: '2K', size: '1536x1024', aspect: '3 / 2' },
  { value: '2:3', label: '2:3', tier: '2K', size: '1024x1536', aspect: '2 / 3' },
  { value: '5:4', label: '5:4', tier: '2K', size: '1280x1024', aspect: '5 / 4' },
  { value: '4:5', label: '4:5', tier: '2K', size: '1024x1280', aspect: '4 / 5' }
]

const activeKeys = computed(() => apiKeys.value.filter((key) => key.status === 'active'))
const selectedRatio = computed(() => ratioOptions.find((item) => item.value === selectedRatioValue.value) ?? ratioOptions[0])
const submitDisabled = computed(() => {
  if (submitting.value || !selectedKeyValue.value || !prompt.value.trim()) return true
  return activeTab.value === 'edit' && !imageFile.value
})

watch(protocol, (next) => {
  model.value = next === 'images' ? 'gpt-image-2' : 'gpt-5.4'
})

onMounted(async () => {
  await loadKeys()
})

async function loadKeys() {
  loadingKeys.value = true
  errorMessage.value = ''
  try {
    const response = await keysAPI.list(1, 100)
    apiKeys.value = (response.items as StudioApiKey[]).filter((key) => key.status === 'active')
    const preferred = apiKeys.value.find((key) =>
      key.group?.platform === 'openai' && key.group?.allow_image_generation !== false
    ) ?? apiKeys.value[0]
    selectedKeyValue.value = preferred?.key ?? ''
  } catch (error) {
    errorMessage.value = error instanceof Error ? error.message : '加载 API Key 失败'
  } finally {
    loadingKeys.value = false
  }
}

async function handleSubmit() {
  if (submitDisabled.value) return
  submitting.value = true
  errorMessage.value = ''
  try {
    const advancedParams = parseAdvancedJson(advancedJson.value)
    const commonInput = {
      model: model.value,
      prompt: prompt.value.trim(),
      size: selectedRatio.value.size,
      count: count.value,
      advancedParams
    }

    const response = activeTab.value === 'edit'
      ? await submitEdit(commonInput)
      : await submitGeneration(commonInput)

    outputs.value = extractImageStudioOutputs(response)
  } catch (error) {
    errorMessage.value = error instanceof Error ? error.message : '生成失败'
  } finally {
    submitting.value = false
  }
}

async function submitGeneration(commonInput: {
  model: string
  prompt: string
  size: string
  count: number
  advancedParams: Record<string, any>
}) {
  if (protocol.value === 'images') {
    return sendImagesGenerationRequest({
      apiKey: selectedKeyValue.value,
      body: buildImagesGenerationPayload(commonInput)
    })
  }
  return sendResponsesImageRequest({
    apiKey: selectedKeyValue.value,
    body: buildResponsesGenerationPayload(commonInput)
  })
}

async function submitEdit(commonInput: {
  model: string
  prompt: string
  size: string
  count: number
  advancedParams: Record<string, any>
}) {
  if (!imageFile.value) throw new Error('请先上传原图')
  const input = { ...commonInput, image: imageFile.value, mask: maskFile.value }

  if (protocol.value === 'images') {
    return sendImagesEditRequest({
      apiKey: selectedKeyValue.value,
      body: buildImagesEditFormData(input)
    })
  }
  return sendResponsesImageRequest({
    apiKey: selectedKeyValue.value,
    body: await buildResponsesEditPayload(input)
  })
}

function handleImageChange(event: Event) {
  imageFile.value = firstFile(event)
}

function handleMaskChange(event: Event) {
  maskFile.value = firstFile(event)
}

function firstFile(event: Event): File | null {
  const input = event.target as HTMLInputElement
  return input.files?.[0] ?? null
}
</script>

<style scoped>
.image-studio-page {
  min-height: calc(100vh - 64px);
  padding: 28px;
  background: linear-gradient(180deg, rgba(236, 253, 245, 0.58), rgba(248, 250, 252, 0.75) 210px);
}

.image-studio-shell {
  overflow: hidden;
  min-height: calc(100vh - 120px);
  border: 1px solid rgba(203, 213, 225, 0.8);
  border-radius: 18px;
  background: rgba(255, 255, 255, 0.96);
  box-shadow: 0 24px 70px rgba(15, 23, 42, 0.08);
}

.studio-tabs {
  display: flex;
  gap: 28px;
  height: 68px;
  padding: 0 28px;
  border-bottom: 1px solid #e5e7eb;
  align-items: flex-end;
}

.studio-tab {
  position: relative;
  height: 68px;
  border: 0;
  background: transparent;
  color: #64748b;
  font-size: 16px;
  font-weight: 700;
}

.studio-tab.active {
  color: #0f9f8f;
}

.studio-tab.active::after {
  position: absolute;
  right: 0;
  bottom: 0;
  left: 0;
  height: 2px;
  background: #14b8a6;
  content: '';
}

.tab-icon {
  margin-right: 8px;
}

.studio-body {
  display: grid;
  grid-template-columns: minmax(420px, 520px) minmax(0, 1fr);
  min-height: calc(100vh - 188px);
}

.studio-controls {
  padding: 24px;
  border-right: 1px solid #e5e7eb;
}

.control-block {
  margin-bottom: 22px;
}

.control-label {
  display: block;
  margin-bottom: 10px;
  color: #334155;
  font-size: 15px;
  font-weight: 700;
}

.studio-select {
  width: 100%;
  height: 56px;
  border: 1px solid #d8dee8;
  border-radius: 14px;
  padding: 0 18px;
  background: #fff;
  color: #0f172a;
  font-size: 16px;
}

.control-hint,
.label-row.muted {
  margin-top: 10px;
  color: #7c8798;
  font-size: 13px;
}

.control-row,
.label-row {
  display: flex;
  justify-content: space-between;
  align-items: center;
  gap: 12px;
}

.segmented {
  display: inline-flex;
  padding: 4px;
  border-radius: 12px;
  background: #f1f5f9;
}

.segmented button {
  height: 34px;
  border: 0;
  border-radius: 9px;
  padding: 0 12px;
  background: transparent;
  color: #64748b;
  font-weight: 700;
}

.segmented button.active {
  background: #fff;
  color: #0f9f8f;
  box-shadow: 0 1px 4px rgba(15, 23, 42, 0.1);
}

.ratio-value {
  color: #64748b;
  font-weight: 700;
}

.ratio-grid {
  display: grid;
  grid-template-columns: repeat(5, minmax(0, 1fr));
  gap: 10px;
}

.ratio-card {
  position: relative;
  display: grid;
  min-height: 88px;
  border: 1px solid #dce4ef;
  border-radius: 8px;
  background: #f8fafc;
  place-items: center;
  color: #475569;
}

.ratio-card.active {
  border-color: #14b8a6;
  background: #ccfbf1;
  color: #0f766e;
}

.ratio-badge {
  position: absolute;
  top: 8px;
  right: 8px;
  color: #f59e0b;
  font-size: 12px;
  font-weight: 800;
}

.ratio-shape {
  display: block;
  width: 34px;
  max-height: 36px;
  border-radius: 3px;
  background: currentColor;
  opacity: 0.18;
}

.ratio-card.active .ratio-shape {
  background: #14b8a6;
  opacity: 1;
}

.ratio-label {
  font-size: 13px;
  font-weight: 700;
}

.count-slider {
  width: 100%;
  accent-color: #0ea5e9;
}

.upload-grid {
  display: grid;
  grid-template-columns: 1fr 1fr;
  gap: 12px;
  margin-bottom: 22px;
}

.upload-tile {
  display: grid;
  min-height: 92px;
  border: 1px dashed #cbd5e1;
  border-radius: 8px;
  place-items: center;
  color: #64748b;
  font-weight: 700;
}

.upload-tile input {
  max-width: 150px;
  font-size: 12px;
}

.prompt-input {
  width: 100%;
  min-height: 170px;
  border: 1px solid #14b8a6;
  border-radius: 14px;
  padding: 18px 20px;
  color: #0f172a;
  font-size: 15px;
  line-height: 1.6;
  outline: none;
  resize: vertical;
  box-shadow: 0 0 0 3px rgba(20, 184, 166, 0.12);
}

.link-button {
  border: 0;
  background: transparent;
  color: #0f9f8f;
  font-weight: 700;
}

.advanced-panel {
  margin-bottom: 18px;
  color: #475569;
  font-weight: 700;
}

.advanced-panel textarea {
  width: 100%;
  min-height: 90px;
  margin-top: 10px;
  border: 1px solid #d8dee8;
  border-radius: 8px;
  padding: 12px;
  font-family: ui-monospace, SFMono-Regular, Menlo, Monaco, Consolas, monospace;
  font-size: 13px;
}

.error-message {
  margin-bottom: 14px;
  color: #dc2626;
  font-size: 14px;
  font-weight: 700;
}

.generate-button {
  width: 100%;
  height: 48px;
  border: 0;
  border-radius: 12px;
  background: #14b8a6;
  color: #fff;
  font-size: 15px;
  font-weight: 800;
}

.generate-button:disabled {
  cursor: not-allowed;
  background: #cbd5e1;
}

.preview-canvas {
  display: grid;
  min-height: 680px;
  margin: 24px;
  border: 1px dashed #c7d5e8;
  border-radius: 18px;
  background: #fbfdff;
}

.empty-preview {
  display: grid;
  place-content: center;
  text-align: center;
  color: #64748b;
}

.empty-icon {
  display: grid;
  width: 86px;
  height: 86px;
  margin: 0 auto 24px;
  border-radius: 18px;
  background: #eef4f8;
  place-items: center;
  color: #64748b;
  font-size: 34px;
}

.empty-preview h2 {
  margin: 0 0 10px;
  color: #0f172a;
  font-size: 24px;
}

.result-grid {
  display: grid;
  grid-template-columns: repeat(auto-fill, minmax(260px, 1fr));
  gap: 18px;
  align-content: start;
  padding: 24px;
}

.result-tile {
  overflow: hidden;
  margin: 0;
  border: 1px solid #e2e8f0;
  border-radius: 8px;
  background: #fff;
}

.result-tile img {
  display: block;
  width: 100%;
  aspect-ratio: 1;
  object-fit: cover;
}

.result-tile figcaption {
  display: flex;
  min-height: 46px;
  justify-content: space-between;
  align-items: center;
  gap: 12px;
  padding: 10px 12px;
  color: #475569;
  font-size: 13px;
}

.result-tile a {
  color: #0f9f8f;
  font-weight: 800;
}

.history-panel {
  display: grid;
  min-height: 520px;
  place-items: center;
}

.history-empty {
  max-width: 520px;
  text-align: center;
  color: #64748b;
}

.history-empty h1 {
  color: #0f172a;
}

@media (max-width: 1180px) {
  .studio-body {
    grid-template-columns: 1fr;
  }

  .studio-controls {
    border-right: 0;
    border-bottom: 1px solid #e5e7eb;
  }
}

@media (max-width: 720px) {
  .image-studio-page {
    padding: 12px;
  }

  .studio-tabs {
    gap: 14px;
    padding: 0 16px;
  }

  .ratio-grid {
    grid-template-columns: repeat(3, minmax(0, 1fr));
  }

  .upload-grid {
    grid-template-columns: 1fr;
  }
}
</style>
