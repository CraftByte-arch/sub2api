<template>
  <AppLayout>
    <div class="image-studio-page">
      <section class="image-studio-shell" aria-label="图像体验">
        <div
          class="studio-body"
          :class="{
            'history-mode': activeTab === 'history',
            'edit-mode': activeTab === 'edit',
            'composer-expanded': composerExpanded,
            'template-drawer-open': templateDrawerOpen
          }"
          :style="studioBodyStyle"
        >
          <main ref="studioMainRef" class="studio-main" aria-live="polite">
            <template v-if="activeTab === 'history'">
              <div class="history-header">
                <div>
                  <h2>最近生成</h2>
                  <p>{{ historyRecords.length }} / {{ historyLimit }} 条本地记录 · 仅保存在当前浏览器</p>
                </div>
                <div class="history-header-actions">
                  <div class="history-primary-tools">
                    <label class="history-search" aria-label="搜索历史记录">
                      <Icon name="search" size="sm" />
                      <input
                        v-model="historySearch"
                        type="search"
                        placeholder="搜索提示词 / 模型 / 尺寸"
                        data-testid="history-search"
                      >
                    </label>
                    <StudioSelect
                      v-model="historyModeFilter"
                      :options="historyModeFilterOptions"
                      button-class="history-filter"
                      aria-label="筛选历史模式"
                    />
                  </div>
                  <div class="history-secondary-tools">
                    <div class="history-limit-control" :class="{ editing: historyLimitEditing }">
                      <span>历史上限</span>
                      <strong v-if="!historyLimitEditing">{{ historyLimit }}</strong>
                      <template v-else>
                        <input
                          v-model.number="historyLimitDraft"
                          type="number"
                          min="1"
                          max="200"
                          step="1"
                          aria-label="历史记录保存上限"
                          @keydown.enter.prevent="submitHistoryLimitEdit"
                          @keydown.esc.prevent="cancelHistoryLimitEdit"
                        >
                        <button type="button" @click="submitHistoryLimitEdit">保存</button>
                        <button type="button" class="ghost" @click="cancelHistoryLimitEdit">取消</button>
                      </template>
                      <button
                        v-if="!historyLimitEditing"
                        type="button"
                        class="history-limit-edit"
                        aria-label="编辑历史记录保存上限"
                        @click="startHistoryLimitEdit"
                      >
                        修改
                      </button>
                    </div>
                    <div v-if="historyLimitConfirmOpen" class="history-limit-confirm" role="dialog" aria-modal="false">
                      <strong>确认缩减历史记录？</strong>
                      <p>
                        目标数量小于当前历史记录数量，确认后会直接删除最早时间的
                        {{ historyRecords.length - pendingHistoryLimit }} 条记录。
                      </p>
                      <div>
                        <button type="button" @click="confirmHistoryLimitReduction">确认</button>
                        <button type="button" class="ghost" @click="cancelHistoryLimitReduction">取消</button>
                      </div>
                    </div>
                    <button
                      type="button"
                      class="secondary-action"
                      data-testid="history-cache-clean-button"
                      :disabled="cleaningHistoryCache"
                      @click="clearOrphanHistoryCache"
                    >
                      <Icon name="trash" size="sm" />
                      {{ cleaningHistoryCache ? '清理中' : '清理缓存' }}
                    </button>
                    <span v-if="historyCacheMessage" class="history-cache-message">{{ historyCacheMessage }}</span>
                  </div>
                </div>
              </div>

              <div v-if="historyRecords.length === 0" class="history-empty" data-testid="history-empty">
                <Icon name="inbox" size="xl" />
                <h2>还没有生成记录</h2>
                <p>完成第一次生图后，这里会显示最近生成的缩略图和参数。</p>
              </div>

              <div v-else-if="filteredHistoryRecords.length === 0" class="history-empty compact">
                <Icon name="search" size="xl" />
                <h2>没有匹配记录</h2>
                <p>换个关键词，或者切回全部模式看看。</p>
              </div>

              <div v-else class="history-grid" data-testid="history-grid">
                <article
                  v-for="record in filteredHistoryRecords"
                  :key="record.id"
                  class="history-card"
                  :class="{ active: selectedHistoryId === record.id }"
                  data-testid="history-card"
                  @click="selectHistoryRecord(record)"
                >
                  <div v-if="record.images[0]" class="history-preview-frame">
                    <img :src="record.images[0].src" :alt="record.prompt || '生成记录'">
                    <div class="history-card-actions">
                      <button
                        type="button"
                        class="history-card-icon-button"
                        data-testid="history-preview-image"
                        aria-label="全屏查看"
                        title="全屏查看"
                        @click.stop="openHistoryLightbox(record)"
                      >
                        <svg viewBox="0 0 1024 1024" aria-hidden="true">
                          <path d="M145.066667 85.333333h153.6c25.6 0 42.666667-17.066667 42.666666-42.666666S324.266667 0 298.666667 0H34.133333C25.6 0 17.066667 8.533333 8.533333 17.066667 0 25.6 0 34.133333 0 42.666667v256c0 25.6 17.066667 42.666667 42.666667 42.666666s42.666667-17.066667 42.666666-42.666666V145.066667l230.4 230.4c17.066667 17.066667 42.666667 17.066667 59.733334 0 17.066667-17.066667 17.066667-42.666667 0-59.733334L145.066667 85.333333z m170.666666 563.2L162.133333 802.133333l-76.8 76.8V725.333333C85.333333 699.733333 68.266667 682.666667 42.666667 682.666667s-42.666667 17.066667-42.666667 42.666666v256c0 25.6 17.066667 42.666667 42.666667 42.666667h256c25.6 0 42.666667-17.066667 42.666666-42.666667s-17.066667-42.666667-42.666666-42.666666H145.066667l76.8-76.8 153.6-153.6c17.066667-17.066667 17.066667-42.666667 0-59.733334-17.066667-17.066667-42.666667-17.066667-59.733334 0z m665.6 34.133334c-25.6 0-42.666667 17.066667-42.666666 42.666666v153.6l-76.8-76.8-153.6-153.6c-17.066667-17.066667-42.666667-17.066667-59.733334 0-17.066667 17.066667-17.066667 42.666667 0 59.733334l153.6 153.6 76.8 76.8H725.333333c-25.6 0-42.666667 17.066667-42.666666 42.666666s17.066667 42.666667 42.666666 42.666667h256c25.6 0 42.666667-17.066667 42.666667-42.666667v-256c0-25.6-17.066667-42.666667-42.666667-42.666666z m0-682.666667h-256c-25.6 0-42.666667 17.066667-42.666666 42.666667s17.066667 42.666667 42.666666 42.666666h153.6l-76.8 76.8-153.6 153.6c-17.066667 17.066667-17.066667 42.666667 0 59.733334 17.066667 17.066667 42.666667 17.066667 59.733334 0l153.6-153.6 76.8-76.8v153.6c0 25.6 17.066667 42.666667 42.666666 42.666666s42.666667-17.066667 42.666667-42.666666v-256c0-25.6-17.066667-42.666667-42.666667-42.666667z" />
                        </svg>
                      </button>
                      <button
                        type="button"
                        class="history-card-icon-button danger"
                        data-testid="history-card-delete-button"
                        aria-label="删除记录"
                        title="删除记录"
                        @click.stop="deleteHistoryRecord(record)"
                      >
                        <svg viewBox="0 0 1024 1024" aria-hidden="true">
                          <path d="M254.398526 804.702412l-0.030699-4.787026C254.367827 801.546535 254.380106 803.13573 254.398526 804.702412zM614.190939 259.036661c-22.116717 0-40.047088 17.910928-40.047088 40.047088l0.37146 502.160911c0 22.097274 17.930371 40.048111 40.047088 40.048111s40.048111-17.950837 40.048111-40.048111l-0.350994-502.160911C654.259516 276.948613 636.328122 259.036661 614.190939 259.036661zM893.234259 140.105968l-318.891887 0.148379-0.178055-41.407062c0-22.13616-17.933441-40.048111-40.067554-40.048111-7.294127 0-14.126742 1.958608-20.017916 5.364171-5.894244-3.405563-12.729929-5.364171-20.031219-5.364171-22.115694 0-40.047088 17.911952-40.047088 40.048111l0.188288 41.463344-230.115981 0.106424c-3.228531-0.839111-6.613628-1.287319-10.104125-1.287319-3.502777 0-6.89913 0.452301-10.136871 1.296529l-73.067132 0.033769c-22.115694 0-40.048111 17.950837-40.048111 40.047088 0 22.13616 17.931395 40.048111 40.048111 40.048111l43.176358-0.020466 0.292666 617.902982 0.059352 0 0 42.551118c0 44.233434 35.862789 80.095199 80.095199 80.095199l40.048111 0 0 0.302899 440.523085-0.25685 0-0.046049 40.048111 0c43.663452 0 79.146595-34.95 80.054267-78.395488l-0.329505-583.369468c0-22.135136-17.930371-40.047088-40.048111-40.047088-22.115694 0-40.047088 17.911952-40.047088 40.047088l0.287549 509.324054c-1.407046 60.314691-18.594497 71.367421-79.993892 71.367421l41.575908 1.022283-454.442096 0.26606 52.398394-1.288343c-62.715367 0-79.305207-11.522428-80.0645-75.308173l0.493234 76.611865-0.543376 0-0.313132-660.818397 236.82273-0.109494c1.173732 0.103354 2.360767 0.166799 3.561106 0.166799 1.215688 0 2.416026-0.063445 3.604084-0.169869l32.639375-0.01535c1.25355 0.118704 2.521426 0.185218 3.805676 0.185218 1.299599 0 2.582825-0.067538 3.851725-0.188288l354.913289-0.163729c22.115694 0 40.050158-17.911952 40.050158-40.047088C933.283394 158.01792 915.349953 140.105968 893.234259 140.105968zM774.928806 815.294654l0.036839 65.715701-0.459464 0L774.928806 815.294654zM413.953452 259.036661c-22.116717 0-40.048111 17.910928-40.048111 40.047088l0.37146 502.160911c0 22.097274 17.931395 40.048111 40.049135 40.048111 22.115694 0 40.047088-17.950837 40.047088-40.048111l-0.37146-502.160911C454.00054 276.948613 436.069145 259.036661 413.953452 259.036661z" />
                        </svg>
                      </button>
                    </div>
                  </div>
                  <div class="history-card-body">
                    <div class="history-card-meta">
                      <span>{{ record.mode === 'edit' ? '图生图' : '文生图' }}</span>
                      <span>{{ formatHistoryTime(record.createdAt) }}</span>
                    </div>
                    <h2>{{ record.prompt || '未填写提示词' }}</h2>
                    <p>{{ record.size }} · {{ record.outputFormat.toUpperCase() }}</p>
                  </div>
                </article>
              </div>
            </template>

            <template v-else>
              <TemplateDrawer
                v-if="templateDrawerOpen"
                :style="templateDrawerInlineStyle"
                :open="templateDrawerOpen"
                :mode="templateMode"
                :has-reference-image="referenceFiles.length > 0"
                :sync-state="templateSyncState"
                :active-template-id="activeTemplateDraft?.templateId"
                :disable-transition="suppressReferenceTransition"
                @update:mode="updateTemplateMode"
                @draft-change="handleTemplateDraftChange"
                @resume-sync="resumeTemplateSync"
                @close="closeTemplateDrawer"
              >
                <template #reference-panel>
                  <ReferenceStrip
                    v-if="activeTab === 'edit'"
                    :files="referenceFiles"
                    :preview-urls="referencePreviewUrls"
                    :max-files="maxReferenceImages"
                    variant="floating"
                    interaction-mode="static"
                    @change="handleReferenceImagesChange"
                    @remove="removeReferenceImage"
                    @preview-error="handleReferencePreviewError"
                    @open-lightbox="openReferenceLightbox"
                  />
                </template>
              </TemplateDrawer>
              <ImagePreviewPanel
                v-else
                :submitting="submitting"
                :outputs="outputs"
                :ratio-label="selectedRatio.label"
                :resolution-value="selectedResolutionValue"
                :output-format="outputFormat"
                :error-message="errorMessage"
                :stream-state="generationStreamState"
                :download-file-name="downloadFileName"
                @edit-output="handleEditOutput"
                @open-lightbox="openOutputLightbox"
                @download-output="downloadOutputAsFormat"
              >
                <template #actions>
                  <nav
                    class="preview-mode-tabs"
                    :class="{ 'is-edit': activeTab === 'edit' }"
                    role="tablist"
                    aria-label="Image generation mode"
                  >
                    <span class="preview-mode-thumb" aria-hidden="true"></span>
                    <button
                      type="button"
                      class="preview-mode-tab"
                      :class="{ active: activeTab === 'generate', running: creationSessions.generate.submitting }"
                      data-testid="tab-generate"
                      @click="setActiveTab('generate')"
                    >
                      <Icon name="sparkles" size="sm" />
                      文生图
                      <span v-if="creationSessions.generate.submitting" class="mode-running-dot" aria-label="文生图生成中"></span>
                    </button>
                    <button
                      type="button"
                      class="preview-mode-tab"
                      :class="{ active: activeTab === 'edit', running: creationSessions.edit.submitting }"
                      data-testid="tab-edit"
                      @click="setActiveTab('edit')"
                    >
                      <Icon name="upload" size="sm" />
                      图生图
                      <span v-if="creationSessions.edit.submitting" class="mode-running-dot" aria-label="图生图生成中"></span>
                    </button>
                  </nav>
                </template>
              </ImagePreviewPanel>
            </template>
          </main>

          <aside class="param-panel" aria-label="Image generation controls">
            <nav
              class="studio-tabs"
              :class="{ 'history-only': activeTab !== 'history' }"
              role="tablist"
              aria-label="Image studio modes"
            >
              <button
                type="button"
                class="studio-tab"
                :class="{ active: activeTab === 'history', 'history-return-tab': activeTab === 'history' }"
                data-testid="tab-history"
                @click="toggleHistoryTab"
              >
                <Icon :name="activeTab === 'history' ? 'arrowLeft' : 'clock'" size="sm" />
                <span class="studio-tab-label">{{ historyTabLabel }}</span>
              </button>
            </nav>

            <GenerationSettingsPanel
              :active-tab="activeTab"
              :selected-history-record="selectedHistoryRecord"
              :history-download-file-name="historyDownloadFileName"
              :selected-key-value="selectedKeyValue"
              :loading-keys="loadingKeys"
              :active-keys="activeKeys"
              :model="model"
              :model-options="modelOptionsForSelection"
              :ratio-options="ratioOptions"
              :selected-ratio="selectedRatio"
              :selected-ratio-value="selectedRatioValue"
              :resolution-options="selectedResolutionOptions"
              :selected-resolution-value="selectedResolutionValue"
              :advanced-open="advancedOpen"
              :quality="quality"
              :quality-options="qualityOptions"
              :background="background"
              :background-options="backgroundOptionsForSelection"
              :output-format="outputFormat"
              :output-format-options="outputFormatOptionsForSelection"
              :advanced-json="advancedJson"
              @open-history-lightbox="openHistoryLightbox"
              @edit-history-image="handleEditHistoryImage"
              @delete-history-record="deleteHistoryRecord"
              @download-history-image="downloadHistoryImageAsFormat"
              @update:selected-key-value="selectedKeyValue = $event"
              @update:model="updateModel"
              @update:selected-ratio-value="selectedRatioValue = $event"
              @update:selected-resolution-value="updateSelectedResolutionValue"
              @update:advanced-open="advancedOpen = $event"
              @update:quality="quality = $event"
              @update:background="updateBackground"
              @update:output-format="updateOutputFormat"
              @update:advanced-json="advancedJson = $event"
            />
          </aside>

          <footer
            ref="composerPanelRef"
            class="composer-panel"
            :class="{
              history: activeTab === 'history',
              expanded: composerExpanded,
              'history-open': promptHistoryOpen,
              'reference-transition-disabled': suppressReferenceTransition
            }"
          >
            <div v-if="activeTab === 'history' && selectedHistoryRecord" class="history-reuse-bar">
              <span>已选择记录，可下载、编辑，或复用提示词继续生成。</span>
              <button type="button" class="secondary-action" data-testid="history-reuse-button" @click="reuseHistoryRecord(selectedHistoryRecord)">
                <Icon name="sync" size="sm" />
                复用到文生图
              </button>
            </div>

            <PromptComposer
              :active-tab="activeTab"
              :prompt="prompt"
              :polishing-prompt="polishingPrompt"
              :template-drawer-open="templateDrawerOpen"
              :prompt-history-open="promptHistoryOpen"
              :prompt-history-mode="promptHistoryMode"
              :prompt-history-search="promptHistorySearch"
              :filtered-prompt-history="filteredPromptHistory"
              :pending-overwrite-prompt-id="pendingOverwritePromptId"
              :composer-expanded="composerExpanded"
              :estimated-cost="estimatedCost"
              :submit-disabled="submitDisabled"
              :submitting="submitting"
              :prompt-examples="promptExamples"
              :prompt-polish-model="promptPolishModel"
              :prompt-polish-model-options="promptPolishModelOptions"
              @toggle-template="toggleTemplateDrawer"
              @polish-prompt="polishPrompt"
              @clear-prompt="clearPrompt"
              @toggle-prompt-history="togglePromptHistory"
              @update:prompt-history-mode="promptHistoryMode = $event"
              @update:prompt-history-search="promptHistorySearch = $event"
              @update:prompt-polish-model="promptPolishModel = $event"
              @clear-prompt-history="clearPromptHistoryForMode"
              @close-prompt-history="closePromptHistory"
              @append-prompt-history="appendPromptHistory"
              @request-prompt-overwrite="requestPromptOverwrite"
              @delete-prompt-history="deletePromptHistoryRecord"
              @overwrite-prompt="overwritePrompt"
              @cancel-prompt-overwrite="pendingOverwritePromptId = ''"
              @update:composer-expanded="composerExpanded = $event"
              @prompt-input="handlePromptInput"
              @submit="handleSubmit"
              @apply-example="applyPromptExample"
            />
            <Transition name="reference-strip-panel">
              <ReferenceStrip
                v-if="activeTab === 'edit'"
                :files="referenceFiles"
                :preview-urls="referencePreviewUrls"
                :max-files="maxReferenceImages"
                variant="inline"
                expand-mode="overlay"
                interaction-mode="expandable"
                @change="handleReferenceImagesChange"
                @remove="removeReferenceImage"
                @preview-error="handleReferencePreviewError"
                @open-lightbox="openReferenceLightbox"
              />
            </Transition>
          </footer>

          <ImageLightbox
            :image="lightboxImage"
            :zoom="lightboxZoom"
            :dragging="lightboxDragging"
            :image-style="lightboxImageStyle"
            @close="closeLightbox"
            @edit="editLightboxImage"
            @reset="resetLightboxTransform"
            @zoom="zoomLightbox"
            @wheel="handleLightboxWheel"
            @drag-start="startLightboxDrag"
            @drag-move="moveLightboxDrag"
            @drag-end="endLightboxDrag"
            @download="downloadLightboxImageAsFormat"
          />
        </div>
      </section>
    </div>
  </AppLayout>
</template>

<script setup lang="ts">
import { computed, nextTick, onBeforeUnmount, onMounted, reactive, ref, watch } from 'vue'
import { keysAPI } from '@/api'
import Icon from '@/components/icons/Icon.vue'
import AppLayout from '@/components/layout/AppLayout.vue'
import GenerationSettingsPanel from './components/GenerationSettingsPanel.vue'
import ImageLightbox from './components/ImageLightbox.vue'
import ImagePreviewPanel from './components/ImagePreviewPanel.vue'
import PromptComposer from './components/PromptComposer.vue'
import ReferenceStrip from './components/ReferenceStrip.vue'
import StudioSelect from './components/StudioSelect.vue'
import TemplateDrawer from './components/TemplateDrawer.vue'
import { sendPromptPolishRequest, sendResponsesImageRequest } from './imageStudioApi'
import {
  buildResponsesEditPayload,
  buildResponsesGenerationPayload,
  parseAdvancedJson
} from './payload'
import { extractImageStudioOutputs } from './output'
import type {
  ImageStudioHistoryRecord,
  ImageStudioLightboxImage,
  ImageStudioMode,
  ImageStudioOutput,
  ImageStudioPromptHistoryRecord,
  ImageStudioStreamEvent,
  ImageStudioStreamState,
  ImageStudioSelectOption,
  StudioApiKey
} from './types'
import type { TemplateDraftPayload, TemplateMode, TemplateSyncState } from './templateTypes'

type CreationMode = Exclude<ImageStudioMode, 'history'>

interface ImageStudioCreationSession {
  submitting: boolean
  errorMessage: string
  outputs: ImageStudioOutput[]
  streamState: ImageStudioStreamState
}

function createCreationSession(): ImageStudioCreationSession {
  return {
    submitting: false,
    errorMessage: '',
    outputs: [],
    streamState: {
      phase: 'idle',
      message: '等待提交生成任务'
    }
  }
}

const activeTab = ref<ImageStudioMode>('generate')
const lastCreationTab = ref<CreationMode>('generate')
const suppressReferenceTransition = ref(false)
let suppressReferenceTransitionTimer: number | undefined
const model = ref('gpt-image-2')
const selectedKeyValue = ref('')
const prompt = ref('')
const selectedRatioValue = ref('1:1')
const quality = ref('')
const background = ref('')
const outputFormat = ref('jpeg')
const advancedJson = ref('')
const advancedOpen = ref(false)
const composerExpanded = ref(false)
const loadingKeys = ref(false)
const polishingPrompt = ref(false)
const promptPolishModel = ref('gpt-5.4-mini')
const apiKeys = ref<StudioApiKey[]>([])
const creationSessions = reactive<Record<CreationMode, ImageStudioCreationSession>>({
  generate: createCreationSession(),
  edit: createCreationSession()
})
const historyRecords = ref<ImageStudioHistoryRecord[]>([])
const selectedHistoryId = ref('')
const cleaningHistoryCache = ref(false)
const historyCacheMessage = ref('')
const historySearch = ref('')
const historyModeFilter = ref<'all' | Exclude<ImageStudioMode, 'history'>>('all')
const historyLimit = ref(30)
const historyLimitDraft = ref(30)
const historyLimitEditing = ref(false)
const historyLimitConfirmOpen = ref(false)
const pendingHistoryLimit = ref(30)
const selectedResolutionValue = ref('1024x1024')
const referenceFiles = ref<File[]>([])
const referencePreviewUrls = ref<string[]>([])
const lightboxImage = ref<ImageStudioLightboxImage | null>(null)
const lightboxZoom = ref(1)
const lightboxOffset = ref({ x: 0, y: 0 })
const lightboxDragging = ref(false)
const lightboxDragStart = ref({ pointerId: -1, x: 0, y: 0, offsetX: 0, offsetY: 0 })
const promptHistoryRecords = ref<ImageStudioPromptHistoryRecord[]>([])
const promptHistoryOpen = ref(false)
const promptHistoryMode = ref<Exclude<ImageStudioMode, 'history'>>('generate')
const promptHistorySearch = ref('')
const pendingOverwritePromptId = ref('')
const templateDrawerOpen = ref(false)
const templateMode = ref<TemplateMode>('text-to-image')
const templateSyncState = ref<TemplateSyncState>('idle')
const activeTemplateDraft = ref<TemplateDraftPayload | null>(null)
const maskFile = ref<File | null>(null)
const studioMainRef = ref<HTMLElement | null>(null)
const composerPanelRef = ref<HTMLElement | null>(null)
const templateDrawerHeight = ref<number | null>(null)
const maxReferenceImages = 4
const maxReferenceImageSize = 20 * 1024 * 1024
const historyStorageKey = 'sub2api:image-studio:history:v1'
const historyLimitStorageKey = 'sub2api:image-studio:history-limit:v1'
const promptHistoryStorageKey = 'sub2api:image-studio:prompt-history:v1'
const historyDbName = 'sub2api-image-studio'
const historyDbStoreName = 'history-images'
const historyDbVersion = 1
const defaultHistoryLimit = 30
const minHistoryLimit = 1
const maxHistoryLimit = 200

const historyModeFilterOptions = [
  { value: 'all', label: '全部' },
  { value: 'generate', label: '文生图' },
  { value: 'edit', label: '图生图' }
]

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

const resolutionMap: Record<string, ImageStudioSelectOption[]> = {
  '1:1': [
    createResolutionOption('1024x1024', 'AI 绘图绝对核心标准正方形'),
    createResolutionOption('1280x1280', '高清头像 / 商品主图'),
    createResolutionOption('1536x1536', '艺术插画头像 / 专辑封面'),
    createResolutionOption('2048x2048', '极致高清正方形素材')
  ],
  '16:9': [
    createResolutionOption('1280x720', '720P 标清宽屏'),
    createResolutionOption('1536x864', '常见网页 / 大屏配图'),
    createResolutionOption('2048x1152', '高清横屏壁纸'),
    createResolutionOption('2560x1440', '2K 极清画质，标准上限'),
    createResolutionOption('3840x2160', '4K 顶级画质，极限档')
  ],
  '9:16': [
    createResolutionOption('720x1280', '手机常规海报 / 故事'),
    createResolutionOption('864x1536', '移动端高清竖屏'),
    createResolutionOption('1152x2048', '手机超清壁纸'),
    createResolutionOption('1440x2560', '2K 手机屏 / 全面屏'),
    createResolutionOption('2160x3840', '4K 竖屏极限')
  ],
  '21:9': [
    createResolutionOption('1792x768', '电影感宽画幅 / 横版 Banner'),
    createResolutionOption('2240x960', '带鱼屏游戏壁纸 / 宽景概念图'),
    createResolutionOption('2576x1104', '极宽超清场景设计'),
    createResolutionOption('3136x1344', '实验性电影级巨幕')
  ],
  '4:3': [
    createResolutionOption('1024x768', '经典平板 / iPad 基础画幅'),
    createResolutionOption('1280x960', '传统演示文稿 / 幻灯片'),
    createResolutionOption('2048x1536', '视网膜屏高清画幅'),
    createResolutionOption('2688x2016', '高画质经典插画')
  ],
  '3:4': [
    createResolutionOption('768x1024', '电子书 / 常规垂直排版'),
    createResolutionOption('960x1280', '电商主图 / 详情页备选'),
    createResolutionOption('1536x2048', '高清平板竖屏海报'),
    createResolutionOption('2016x2688', '艺术肖像 / 精细出版')
  ],
  '3:2': [
    createResolutionOption('1152x768', '单反相机原生低清预览'),
    createResolutionOption('1536x1024', '大模型黄金画幅'),
    createResolutionOption('1920x1280', '经典摄影画册 / 摄影作品'),
    createResolutionOption('3072x2048', '超高清印刷级摄影')
  ],
  '2:3': [
    createResolutionOption('768x1152', '常规书籍封面 / 海报'),
    createResolutionOption('1024x1536', '大模型竖图黄金画幅'),
    createResolutionOption('1280x1920', '艺术人像 / 街拍构图'),
    createResolutionOption('2048x3072', '极致精细艺术人像')
  ],
  '5:4': [
    createResolutionOption('1280x1024', '早期经典显示器尺寸'),
    createResolutionOption('1600x1280', '传统美术作品 / 版画'),
    createResolutionOption('1920x1536', '桌面壁纸 / 传统画幅'),
    createResolutionOption('2720x2176', '实验性高精近正方形')
  ],
  '4:5': [
    createResolutionOption('1024x1280', 'Instagram 经典长图'),
    createResolutionOption('1280x1600', '社媒艺术海报 / 杂志封面'),
    createResolutionOption('1536x1920', '社媒信息图 / 卡片流'),
    createResolutionOption('2176x2720', '极限精细垂直社媒图')
  ]
}

const promptExamples = [
  '赛博朋克城市夜景，霓虹雨夜，电影感光影，8K',
  '一只金色胖胖大柴犬坐在办公桌前，油画质感',
  '极简几何海报，蓝橙配色，主体是一只慵懒的猫',
  '童话风格蘑菇屋，黄昏光线，柔和景深'
]

const qualityOptions = [
  { value: '', label: '自动' },
  { value: 'low', label: '低' },
  { value: 'medium', label: '中' },
  { value: 'high', label: '高' }
]

const backgroundOptions = [
  { value: '', label: '自动' },
  { value: 'opaque', label: '不透明' },
  { value: 'transparent', label: '透明' }
]

const outputFormatOptions = [
  { value: 'jpeg', label: 'JPEG' },
  { value: 'webp', label: 'WebP' },
  { value: 'png', label: 'PNG' }
]

const modelOptions = [
  { value: 'gpt-image-2', label: 'GPT Image 2' },
  { value: 'gpt-image-1.5', label: 'GPT Image 1.5' },
  { value: 'gpt-image-1', label: 'GPT Image 1' }
]

const promptPolishModelOptions = [
  { value: 'gpt-5.4-mini', label: '5.4 mini' },
  { value: 'gpt-5.4', label: '5.4' },
  { value: 'gpt-5.5', label: '5.5' }
]

const activeKeys = computed(() => apiKeys.value.filter((key) => key.status === 'active'))
const selectedKey = computed(() => activeKeys.value.find((key) => key.key === selectedKeyValue.value) ?? null)
const selectedRatio = computed(() => ratioOptions.find((item) => item.value === selectedRatioValue.value) ?? ratioOptions[0])
const selectedResolutionOptions = computed(() =>
  resolutionMap[selectedRatio.value.value] ?? []
)
const selectedResolution = computed(() =>
  selectedResolutionOptions.value.find((option) => option.value === selectedResolutionValue.value) ??
  selectedResolutionOptions.value[0] ??
  null
)
const selectedHistoryRecord = computed(() =>
  historyRecords.value.find((record) => record.id === selectedHistoryId.value) ?? historyRecords.value[0] ?? null
)
const activeCreationMode = computed<CreationMode>(() => activeTab.value === 'edit' ? 'edit' : 'generate')
const activeCreationSession = computed(() => creationSessions[activeCreationMode.value])
const submitting = computed(() => activeCreationSession.value.submitting)
const outputs = computed(() => activeCreationSession.value.outputs)
const errorMessage = computed(() => activeCreationSession.value.errorMessage)
const generationStreamState = computed(() => activeCreationSession.value.streamState)
const historyTabLabel = computed(() => {
  if (activeTab.value !== 'history') return '记录'
  return lastCreationTab.value === 'edit' ? '返回图生图' : '返回文生图'
})
const filteredHistoryRecords = computed(() => {
  const query = historySearch.value.trim().toLowerCase()
  return historyRecords.value.filter((record) => {
    if (historyModeFilter.value !== 'all' && record.mode !== historyModeFilter.value) return false
    if (!query) return true
    return [
      record.prompt,
      record.model,
      record.size,
      record.outputFormat,
      record.mode === 'edit' ? '图生图' : '文生图'
    ].join(' ').toLowerCase().includes(query)
  })
})
const lightboxImageStyle = computed(() => ({
  transform: `translate3d(${lightboxOffset.value.x}px, ${lightboxOffset.value.y}px, 0) scale(${lightboxZoom.value})`
}))
const filteredPromptHistory = computed(() => {
  const query = promptHistorySearch.value.trim().toLowerCase()
  return promptHistoryRecords.value.filter((record) =>
    record.mode === promptHistoryMode.value &&
    (!query || record.prompt.toLowerCase().includes(query))
  )
})
const estimatedCost = computed(() => {
  const group = selectedKey.value?.group
  const tier = selectedResolution.value?.tier ?? selectedRatio.value.tier
  const fallbackPrice = tier === '1K' ? 1 : tier === '2K' ? 2 : 4
  const groupPrice = tier === '1K'
    ? group?.image_price_1k
    : tier === '2K'
      ? group?.image_price_2k
      : group?.image_price_4k
  const unitPrice = typeof groupPrice === 'number' && groupPrice > 0 ? groupPrice : fallbackPrice
  const multiplier = typeof group?.image_rate_multiplier === 'number' && group.image_rate_multiplier >= 0
    ? group.image_rate_multiplier
    : 1
  return `$${(unitPrice * multiplier).toFixed(2)}`
})
const submitDisabled = computed(() => {
  if (submitting.value || !selectedKeyValue.value) return true
  if (activeTab.value === 'edit') return referenceFiles.value.length === 0
  if (!prompt.value.trim()) return true
  return false
})

const outputFormatOptionsForSelection = computed(() =>
  outputFormatOptions.map((option) => ({
    ...option,
    disabled: Boolean(outputFormatDisabledReason(option.value)),
    disabledReason: outputFormatDisabledReason(option.value)
  }))
)

const backgroundOptionsForSelection = computed(() =>
  backgroundOptions.map((option) => ({
    ...option,
    disabled: Boolean(backgroundDisabledReason(option.value)),
    disabledReason: backgroundDisabledReason(option.value)
  }))
)

const modelOptionsForSelection = computed(() =>
  modelOptions.map((option) => ({
    ...option,
    disabled: option.value === 'gpt-image-2' && background.value === 'transparent',
    disabledReason: option.value === 'gpt-image-2' && background.value === 'transparent'
      ? '透明背景暂不支持 GPT Image 2'
      : undefined
  }))
)

const studioBodyStyle = computed(() => (
  activeTab.value !== 'generate' && templateDrawerOpen.value && templateDrawerHeight.value
    ? { '--studio-edit-template-height': `${templateDrawerHeight.value}px` }
    : {}
))

const templateDrawerInlineStyle = computed(() => (
  activeTab.value !== 'generate' && templateDrawerOpen.value && templateDrawerHeight.value
    ? { height: `${templateDrawerHeight.value}px` }
    : {}
))

let layoutResizeObserver: ResizeObserver | null = null

onMounted(async () => {
  document.addEventListener('pointerdown', handleDocumentPointerDown)
  document.addEventListener('paste', handleDocumentPaste)
  window.addEventListener('resize', syncTemplateDrawerLayout)
  historyLimit.value = loadHistoryLimit()
  historyLimitDraft.value = historyLimit.value
  historyRecords.value = await loadHistoryRecords()
  promptHistoryRecords.value = loadPromptHistoryRecords()
  selectedHistoryId.value = historyRecords.value[0]?.id ?? ''
  await loadKeys()
  layoutResizeObserver = new ResizeObserver(() => {
    syncTemplateDrawerLayout()
  })
  if (studioMainRef.value) layoutResizeObserver.observe(studioMainRef.value)
  if (composerPanelRef.value) layoutResizeObserver.observe(composerPanelRef.value)
  syncTemplateDrawerLayout()
})

watch(selectedRatioValue, () => {
  syncResolutionToSelectedRatio()
}, { immediate: true })

watch(outputFormat, () => {
  if (outputFormat.value === 'jpeg' && background.value === 'transparent') {
    background.value = ''
  }
})

watch(model, () => {
  if (model.value === 'gpt-image-2' && background.value === 'transparent') {
    background.value = ''
  }
})

onBeforeUnmount(() => {
  document.removeEventListener('pointerdown', handleDocumentPointerDown)
  document.removeEventListener('paste', handleDocumentPaste)
  window.removeEventListener('resize', syncTemplateDrawerLayout)
  if (suppressReferenceTransitionTimer) window.clearTimeout(suppressReferenceTransitionTimer)
  layoutResizeObserver?.disconnect()
  revokeReferencePreviewUrls()
})

watch(
  [activeTab, templateDrawerOpen, composerExpanded],
  async () => {
    await nextTick()
    syncTemplateDrawerLayout()
  },
  { immediate: true }
)

function setActiveTab(nextTab: ImageStudioMode) {
  const previousTab = activeTab.value
  if (nextTab === 'generate' || nextTab === 'edit') {
    lastCreationTab.value = nextTab
  }
  if (previousTab === 'history' && nextTab === 'edit') {
    suppressReferenceTransition.value = true
    if (suppressReferenceTransitionTimer) window.clearTimeout(suppressReferenceTransitionTimer)
    suppressReferenceTransitionTimer = window.setTimeout(() => {
      suppressReferenceTransition.value = false
      suppressReferenceTransitionTimer = undefined
    }, 320)
  }
  activeTab.value = nextTab
  if (nextTab === 'generate') templateMode.value = 'text-to-image'
  if (nextTab === 'edit') templateMode.value = 'image-to-image'
  if (nextTab === 'history') {
    selectedHistoryId.value = selectedHistoryRecord.value?.id ?? ''
  }
}

function toggleHistoryTab() {
  if (activeTab.value === 'history') {
    setActiveTab(lastCreationTab.value)
    return
  }
  setActiveTab('history')
}

function syncTemplateDrawerLayout() {
  if (!templateDrawerOpen.value || activeTab.value === 'generate') {
    templateDrawerHeight.value = null
    return
  }
  if (activeTab.value === 'history' || suppressReferenceTransition.value) return
  const mainEl = studioMainRef.value
  const composerEl = composerPanelRef.value
  if (!mainEl || !composerEl) return
  const mainRect = mainEl.getBoundingClientRect()
  const referenceEl = composerEl.querySelector('.reference-strip-inline') as HTMLElement | null
  const boundaryRect = referenceEl?.getBoundingClientRect() ?? composerEl.getBoundingClientRect()
  const available = Math.floor(boundaryRect.top - mainRect.top)
  templateDrawerHeight.value = available > 0 ? available : null
}

async function loadKeys() {
  loadingKeys.value = true
  setSessionError(activeCreationSession.value, '')
  try {
    const response = await keysAPI.list(1, 100)
    apiKeys.value = (response.items as StudioApiKey[]).filter((key) => key.status === 'active')
    const preferred = apiKeys.value.find((key) =>
      key.group?.platform === 'openai' && key.group?.allow_image_generation !== false
    ) ?? apiKeys.value[0]
    selectedKeyValue.value = preferred?.key ?? ''
  } catch (error) {
    setSessionError(activeCreationSession.value, error instanceof Error ? error.message : '加载 API Key 失败')
  } finally {
    loadingKeys.value = false
  }
}

async function handleSubmit() {
  if (submitDisabled.value) return
  const submitMode: CreationMode = activeTab.value === 'edit' ? 'edit' : 'generate'
  const session = creationSessions[submitMode]
  templateDrawerOpen.value = false
  promptHistoryOpen.value = false
  pendingOverwritePromptId.value = ''
  session.submitting = true
  setSessionError(session, '')
  session.outputs = []
  updateGenerationStreamState(session, 'created', '正在理解你的提示词...')
  try {
    const advancedParams = parseAdvancedJson(advancedJson.value)
    const commonInput = {
      model: model.value,
      prompt: normalizedPromptForSubmit(),
      size: selectedResolutionValue.value,
      quality: quality.value,
      background: background.value,
      outputFormat: outputFormat.value,
      count: 1,
      advancedParams
    }
    const requestedOutputFormat = commonInput.outputFormat || 'jpeg'
    const requestInput = {
      ...commonInput,
      outputFormat: requestedOutputFormat
    }

    const response = submitMode === 'edit'
      ? await submitEdit(requestInput, session)
      : await submitGeneration(requestInput, session)

    updateGenerationStreamState(session, 'image_done', '画面已经生成，正在整理格式...')
    const extractedOutputs = extractImageStudioOutputs(response)
    session.outputs = await restoreOutputsToRequestedFormat(extractedOutputs, requestedOutputFormat)
    updateGenerationStreamState(session, 'completed', '生成完成')
    persistHistoryRecord(commonInput, session.outputs, submitMode)
    addPromptHistoryRecord(commonInput.prompt, submitMode, 'generated')
  } catch (error) {
    setSessionError(session, error instanceof Error ? error.message : '生成失败')
    updateGenerationStreamState(session, 'failed', session.errorMessage)
  } finally {
    session.submitting = false
  }
}

function setSessionError(session: ImageStudioCreationSession, message: string) {
  session.errorMessage = message
}

function updateGenerationStreamState(
  session: ImageStudioCreationSession,
  phase: ImageStudioStreamState['phase'],
  message: string,
  detail = ''
) {
  const now = Date.now()
  session.streamState = {
    phase,
    message,
    detail,
    startedAt: session.streamState.startedAt ?? now,
    updatedAt: now
  }
}

function handleResponsesStreamEvent(event: ImageStudioStreamEvent, session: ImageStudioCreationSession) {
  switch (event.type) {
    case 'response.created':
      updateGenerationStreamState(session, 'created', '正在理解你的提示词...')
      break
    case 'response.in_progress':
      updateGenerationStreamState(session, 'preparing', '正在整理画面方向...')
      break
    case 'response.output_item.added':
      if (event.data.item?.type === 'image_generation_call') {
        updateGenerationStreamState(session, 'image_task_created', '正在搭建画面构图...')
      }
      break
    case 'response.image_generation_call.in_progress':
      updateGenerationStreamState(session, 'image_in_progress', '正在安排主体、光线和风格...')
      break
    case 'response.image_generation_call.generating':
      updateGenerationStreamState(session, 'generating', '正在绘制画面...')
      break
    case 'keepalive':
      if (session.streamState.phase === 'generating' || session.streamState.phase === 'image_in_progress') {
        updateGenerationStreamState(session, session.streamState.phase, '正在处理更多细节...')
      }
      break
    case 'response.image_generation_call.partial_image':
      handlePartialImageEvent(event, session)
      break
    case 'response.output_item.done':
      if (event.data.item?.type === 'image_generation_call') {
        handleImageDoneEvent(event, session)
      }
      break
    case 'response.completed':
      updateGenerationStreamState(session, 'completed', '画面完成，正在呈现结果...')
      break
  }
}

function handlePartialImageEvent(event: ImageStudioStreamEvent, session: ImageStudioCreationSession) {
  const b64 = typeof event.data.partial_image_b64 === 'string' ? event.data.partial_image_b64 : ''
  if (!b64) return
  session.outputs = [{
    id: `partial-${event.data.item_id || event.data.sequence_number || Date.now()}`,
    kind: 'b64_json',
    src: toImageDataUrl(b64, mimeTypeFromStreamOutputFormat(event.data.output_format)),
    mimeType: mimeTypeFromStreamOutputFormat(event.data.output_format),
    raw: event.data
  }]
  updateGenerationStreamState(
    session,
    'partial_preview',
    '已经有一个预览版本，正在继续打磨细节...',
    imageStreamDetail(event.data)
  )
}

function handleImageDoneEvent(event: ImageStudioStreamEvent, session: ImageStudioCreationSession) {
  const item = event.data.item
  const result = typeof item?.result === 'string' ? item.result : ''
  if (!result) return
  const mimeType = mimeTypeFromStreamOutputFormat(item.output_format)
  session.outputs = [{
    id: typeof item.id === 'string' ? item.id : `image-${Date.now()}`,
    kind: 'b64_json',
    src: toImageDataUrl(result, mimeType),
    mimeType,
    revisedPrompt: typeof item.revised_prompt === 'string' ? item.revised_prompt : undefined,
    raw: item
  }]
  updateGenerationStreamState(
    session,
    'image_done',
    '画面已经生成，正在做最后整理...',
    imageStreamDetail(item)
  )
}

function imageStreamDetail(record: Record<string, any>) {
  const parts = [
    typeof record.size === 'string' ? record.size : '',
    typeof record.quality === 'string' ? `${record.quality} quality` : '',
    typeof record.output_format === 'string' ? record.output_format.toUpperCase() : ''
  ].filter(Boolean)
  return parts.join(' · ')
}

function toImageDataUrl(value: string, mimeType: string) {
  if (value.startsWith('data:')) return value
  return `data:${mimeType};base64,${value}`
}

function mimeTypeFromStreamOutputFormat(format: unknown) {
  const normalized = typeof format === 'string' ? format.trim().toLowerCase() : ''
  if (normalized === 'webp') return 'image/webp'
  if (normalized === 'jpeg' || normalized === 'jpg') return 'image/jpeg'
  return 'image/png'
}

function normalizedPromptForSubmit(): string {
  const value = prompt.value.trim()
  if (value) return value
  return activeTab.value === 'edit'
    ? '基于参考图生成一张改图，保留主要视觉元素和构图，并提升画面质量。'
    : value
}

async function polishPrompt() {
  if (!prompt.value.trim() || !selectedKeyValue.value || polishingPrompt.value) return
  polishingPrompt.value = true
  setSessionError(activeCreationSession.value, '')
  const mode = activeTab.value === 'edit' ? 'edit' : 'generate'
  const originalPrompt = prompt.value.trim()
  try {
    const response = await sendPromptPolishRequest({
      apiKey: selectedKeyValue.value,
      body: {
        model: promptPolishModel.value,
        instructions: '你是专业图像生成提示词编辑。请在保留用户原意的基础上，把提示词润色成更清晰、具体、适合图像生成的中文提示词。只输出润色后的提示词，不要解释。',
        input: [
          {
            role: 'user',
            content: [
              {
                type: 'input_text',
                text: originalPrompt
              }
            ]
          }
        ],
        store: false,
        stream: true
      }
    })
    const polished = extractPolishedPrompt(response)
    if (!polished) throw new Error('润色失败，未返回提示词')
    setPromptValue(polished, 'external')
    addPromptHistoryRecord(originalPrompt, mode, 'polished')
  } catch (error) {
    setSessionError(activeCreationSession.value, error instanceof Error ? error.message : '润色提示词失败')
  } finally {
    polishingPrompt.value = false
  }
}

function extractPolishedPrompt(payload: unknown): string {
  if (!payload || typeof payload !== 'object') return ''
  const record = payload as Record<string, any>
  const content =
    record.output_text ??
    record.text ??
    extractResponsesOutputText(record) ??
    record.choices?.[0]?.message?.content
  return typeof content === 'string' ? content.trim().replace(/^["“]|["”]$/g, '') : ''
}

function extractResponsesOutputText(record: Record<string, any>): string {
  const output = Array.isArray(record.output) ? record.output : []
  const parts = output.flatMap((item) => {
    const content = Array.isArray(item?.content) ? item.content : []
    return content.map((part: Record<string, any>) => {
      if (typeof part?.text === 'string') return part.text
      if (typeof part?.output_text === 'string') return part.output_text
      return ''
    })
  }).filter(Boolean)
  return parts.join('').trim()
}

function setPromptValue(value: string, source: 'manual' | 'template' | 'external' = 'external') {
  prompt.value = value
  if (
    source !== 'template' &&
    templateSyncState.value === 'linked' &&
    activeTemplateDraft.value &&
    value.trim() !== activeTemplateDraft.value.prompt.trim()
  ) {
    templateSyncState.value = 'detached'
  }
}

function handlePromptInput(event: Event) {
  const nextValue = (event.target as HTMLTextAreaElement).value
  setPromptValue(nextValue, 'manual')
}

function clearPrompt() {
  setPromptValue('', 'external')
}

function appendPromptHistory(value: string) {
  const current = prompt.value.trim()
  setPromptValue(current ? `${current}\n${value}` : value, 'external')
  pendingOverwritePromptId.value = ''
}

function requestPromptOverwrite(id: string) {
  if (!prompt.value.trim()) {
    const item = promptHistoryRecords.value.find((record) => record.id === id)
    if (item) overwritePrompt(item.prompt)
    return
  }
  pendingOverwritePromptId.value = id
}

function overwritePrompt(value: string) {
  setPromptValue(value, 'external')
  pendingOverwritePromptId.value = ''
  promptHistoryOpen.value = false
}

function deletePromptHistoryRecord(id: string) {
  const nextRecords = promptHistoryRecords.value.filter((record) => record.id !== id)
  promptHistoryRecords.value = nextRecords
  if (pendingOverwritePromptId.value === id) pendingOverwritePromptId.value = ''
  savePromptHistoryRecords(nextRecords)
}

function clearPromptHistoryForMode() {
  const nextRecords = promptHistoryRecords.value.filter((record) => record.mode !== promptHistoryMode.value)
  promptHistoryRecords.value = nextRecords
  pendingOverwritePromptId.value = ''
  savePromptHistoryRecords(nextRecords)
}

async function submitGeneration(commonInput: {
  model: string
  prompt: string
  size: string
  quality: string
  background: string
  outputFormat: string
  count: number
  advancedParams: Record<string, any>
}, session: ImageStudioCreationSession) {
  return sendResponsesImageRequest({
    apiKey: selectedKeyValue.value,
    body: buildResponsesGenerationPayload(commonInput),
    onStreamEvent: (event) => handleResponsesStreamEvent(event, session)
  })
}

async function submitEdit(commonInput: {
  model: string
  prompt: string
  size: string
  quality: string
  background: string
  outputFormat: string
  count: number
  advancedParams: Record<string, any>
}, session: ImageStudioCreationSession) {
  if (referenceFiles.value.length === 0) throw new Error('请先添加参考图')
  const compressedReferences = await compressReferenceImagesForUpload(referenceFiles.value)
  const input = { ...commonInput, image: compressedReferences, mask: maskFile.value }

  return sendResponsesImageRequest({
    apiKey: selectedKeyValue.value,
    body: await buildResponsesEditPayload(input),
    onStreamEvent: (event) => handleResponsesStreamEvent(event, session)
  })
}

function handleReferenceImagesChange(event: Event) {
  const input = event.target as HTMLInputElement
  appendReferenceImages(Array.from(input.files ?? []))
  input.value = ''
}

function handleDocumentPaste(event: ClipboardEvent) {
  const clipboardFiles = Array.from(event.clipboardData?.items ?? [])
    .filter((item) => item.kind === 'file' && item.type.startsWith('image/'))
    .map((item) => item.getAsFile())
    .filter((file): file is File => Boolean(file))

  if (clipboardFiles.length > 0) {
    if (activeTab.value !== 'edit') {
      setActiveTab('edit')
    }
    const acceptedCount = appendReferenceImages(clipboardFiles)
    if (acceptedCount > 0) {
      event.preventDefault()
    }
    return
  }

  const target = event.target as HTMLElement | null
  if (target && (target.tagName === 'INPUT' || target.tagName === 'TEXTAREA' || target.isContentEditable)) {
    return
  }
}

function appendReferenceImages(incoming: File[]) {
  const availableSlots = maxReferenceImages - referenceFiles.value.length
  const accepted = incoming
    .filter((file) => file.size <= maxReferenceImageSize)
    .slice(0, availableSlots)
  for (const file of accepted) {
    appendReferenceImage(file)
  }
  return accepted.length
}

function appendReferenceImage(file: File, previewUrl = createReferencePreviewUrl(file)) {
  referenceFiles.value = [...referenceFiles.value, file]
  referencePreviewUrls.value = [...referencePreviewUrls.value, previewUrl || createReferencePreviewUrl(file)]
}

function removeReferenceImage(index: number) {
  revokeReferencePreviewUrl(referencePreviewUrls.value[index])
  const nextFiles = referenceFiles.value.filter((_, currentIndex) => currentIndex !== index)
  referenceFiles.value = nextFiles
  referencePreviewUrls.value = referencePreviewUrls.value.filter((_, currentIndex) => currentIndex !== index)
}

async function handleReferencePreviewError(index: number) {
  const file = referenceFiles.value[index]
  const brokenUrl = referencePreviewUrls.value[index]
  if (!file || brokenUrl?.startsWith('data:')) return

  try {
    const fallbackUrl = await readReferenceFileAsDataUrl(file)
    if (referenceFiles.value[index] !== file) return
    referencePreviewUrls.value = referencePreviewUrls.value.map((url, currentIndex) =>
      currentIndex === index ? fallbackUrl : url
    )
    revokeReferencePreviewUrl(brokenUrl)
  } catch {
    // Keep the original URL so the lightbox can still retry from the File object.
  }
}

function applyPromptExample(example: string) {
  if (activeTab.value === 'history' || polishingPrompt.value) return
  setPromptValue(example, 'external')
}

function toggleTemplateDrawer() {
  if (activeTab.value === 'history') return
  templateMode.value = activeTab.value === 'edit' ? 'image-to-image' : 'text-to-image'
  promptHistoryOpen.value = false
  templateDrawerOpen.value = !templateDrawerOpen.value
}

function closeTemplateDrawer() {
  templateDrawerOpen.value = false
}

function updateTemplateMode(nextMode: TemplateMode) {
  templateMode.value = nextMode
  setActiveTab(nextMode === 'image-to-image' ? 'edit' : 'generate')
  if (activeTemplateDraft.value && activeTemplateDraft.value.mode !== nextMode) {
    templateSyncState.value = 'idle'
  }
}

function handleTemplateDraftChange(payload: TemplateDraftPayload) {
  const nextPrompt = payload.prompt.trim()
  if (!nextPrompt) return
  const isNewTemplate = activeTemplateDraft.value?.templateId !== payload.templateId
  activeTemplateDraft.value = payload
  if (payload.mode === 'image-to-image') {
    setActiveTab('edit')
  } else {
    setActiveTab('generate')
  }
  if (payload.recommendedRatio) {
    selectedRatioValue.value = payload.recommendedRatio
  }
  if (isNewTemplate) {
    templateSyncState.value = 'linked'
  }
  if (templateSyncState.value === 'detached' && !isNewTemplate) return
  templateSyncState.value = 'linked'
  setPromptValue(nextPrompt, 'template')
}

function resumeTemplateSync() {
  if (!activeTemplateDraft.value?.prompt.trim()) return
  templateSyncState.value = 'linked'
  setPromptValue(activeTemplateDraft.value.prompt.trim(), 'template')
}

function togglePromptHistory() {
  if (activeTab.value === 'history') return
  promptHistoryMode.value = activeTab.value === 'edit' ? 'edit' : 'generate'
  pendingOverwritePromptId.value = ''
  promptHistorySearch.value = ''
  templateDrawerOpen.value = false
  promptHistoryOpen.value = !promptHistoryOpen.value
}

function closePromptHistory() {
  promptHistoryOpen.value = false
  pendingOverwritePromptId.value = ''
}

function handleDocumentPointerDown(event: PointerEvent) {
  if (!promptHistoryOpen.value) return
  const target = event.target as HTMLElement | null
  if (typeof target?.closest === 'function' && target.closest('.prompt-history-anchor')) return
  closePromptHistory()
}

function addPromptHistoryRecord(
  value: string,
  mode: Exclude<ImageStudioMode, 'history'>,
  source: ImageStudioPromptHistoryRecord['source']
) {
  const normalized = value.trim()
  if (!normalized) return
  const nextRecord: ImageStudioPromptHistoryRecord = {
    id: `${Date.now()}-${Math.random().toString(36).slice(2, 8)}`,
    createdAt: new Date().toISOString(),
    mode,
    prompt: normalized,
    source
  }
  const nextRecords = [
    nextRecord,
    ...promptHistoryRecords.value.filter((record) =>
      !(record.mode === mode && record.source === source && record.prompt === normalized)
    )
  ].slice(0, 100)
  promptHistoryRecords.value = nextRecords
  savePromptHistoryRecords(nextRecords)
}

function savePromptHistoryRecords(records: ImageStudioPromptHistoryRecord[]) {
  if (typeof window === 'undefined') return
  window.localStorage.setItem(promptHistoryStorageKey, JSON.stringify(records))
}

function loadPromptHistoryRecords(): ImageStudioPromptHistoryRecord[] {
  if (typeof window === 'undefined') return []
  try {
    const parsed = JSON.parse(window.localStorage.getItem(promptHistoryStorageKey) || '[]')
    if (!Array.isArray(parsed)) return []
    return parsed.filter(isPromptHistoryRecord).slice(0, 100)
  } catch {
    return []
  }
}

function isPromptHistoryRecord(value: unknown): value is ImageStudioPromptHistoryRecord {
  if (!value || typeof value !== 'object') return false
  const record = value as Partial<ImageStudioPromptHistoryRecord>
  return (
    typeof record.id === 'string' &&
    typeof record.createdAt === 'string' &&
    (record.mode === 'generate' || record.mode === 'edit') &&
    typeof record.prompt === 'string' &&
    (record.source === 'generated' || record.source === 'polished')
  )
}

function selectHistoryRecord(record: ImageStudioHistoryRecord) {
  selectedHistoryId.value = record.id
}

function reuseHistoryRecord(record: ImageStudioHistoryRecord) {
  setPromptValue(record.prompt, 'external')
  updateModel(record.model)
  updateOutputFormat(record.outputFormat || 'jpeg')
  const matchedRatio = findRatioByResolution(record.size)
  if (matchedRatio) {
    selectedRatioValue.value = matchedRatio.value
    updateSelectedResolutionValue(record.size)
  } else {
    updateSelectedResolutionValue(selectedResolutionOptions.value[0]?.value ?? record.size)
  }
  setActiveTab('generate')
}

function findRatioByResolution(size: string) {
  return ratioOptions.find((ratio) =>
    (resolutionMap[ratio.value] ?? []).some((option) => option.value === size)
  ) ?? null
}

function updateSelectedResolutionValue(value: string) {
  selectedResolutionValue.value = value
}

function updateModel(value: string) {
  if (value === 'gpt-image-2' && background.value === 'transparent') return
  model.value = value
}

function updateBackground(value: string) {
  if (backgroundDisabledReason(value)) return
  background.value = value
}

function updateOutputFormat(value: string) {
  if (outputFormatDisabledReason(value)) return
  outputFormat.value = value
}

function backgroundDisabledReason(value: string) {
  if (value !== 'transparent') return ''
  if (model.value === 'gpt-image-2') return 'GPT Image 2 暂不支持透明背景'
  if (outputFormat.value === 'jpeg') return 'JPEG 不支持透明背景'
  return ''
}

function outputFormatDisabledReason(value: string) {
  if (value === 'jpeg' && background.value === 'transparent') {
    return '透明背景暂不支持 JPEG 输出'
  }
  return ''
}

function createResolutionOption(value: string, description: string): ImageStudioSelectOption {
  const [width, height] = value.split('x').map(Number)
  const pixels = width * height
  const standardLimit = 2560 * 1440
  const tier = pixels <= 1024 * 1024 ? '1K' : pixels <= standardLimit ? '2K' : '4K'
  const status = pixels > standardLimit ? 'experimental' : 'standard'
  return {
    value,
    label: value,
    tier,
    status,
    description
  }
}

function syncResolutionToSelectedRatio() {
  const options = selectedResolutionOptions.value.filter((option) => !option.disabled)
  if (options.length === 0) return
  if (options.some((option) => option.value === selectedResolutionValue.value)) return
  selectedResolutionValue.value = options[0].value
}

async function handleEditOutput(output: ImageStudioOutput, index: number) {
  setActiveTab('edit')
  creationSessions.edit.outputs = []
  setSessionError(creationSessions.edit, '')
  try {
    const file = await outputToReferenceFile(output, index)
    if (referenceFiles.value.length >= maxReferenceImages) {
      revokeReferencePreviewUrls()
      referenceFiles.value = [file]
      referencePreviewUrls.value = [output.src]
    } else {
      appendReferenceImage(file, output.src)
    }
  } catch (error) {
    setSessionError(creationSessions.edit, error instanceof Error ? error.message : '无法把图片加入参考图')
  }
}

async function handleEditHistoryImage(record: ImageStudioHistoryRecord) {
  const image = record.images[0]
  if (!image) return
  await handleEditOutput({
    id: image.id,
    kind: image.src.startsWith('data:') ? 'b64_json' : 'url',
    src: image.src,
    mimeType: image.mimeType,
    revisedPrompt: image.revisedPrompt,
    raw: image
  }, 0)
  setPromptValue(record.prompt, 'external')
}

function deleteHistoryRecord(record: ImageStudioHistoryRecord) {
  const nextRecords = historyRecords.value.filter((item) => item.id !== record.id)
  historyRecords.value = nextRecords
  if (selectedHistoryId.value === record.id) {
    selectedHistoryId.value = nextRecords[0]?.id ?? ''
  }
  if (lightboxImage.value?.kind === 'history' && lightboxImage.value.record.id === record.id) {
    closeLightbox()
  }
  saveHistoryIndex(nextRecords)
  void deleteHistoryImages(record)
}

function openOutputLightbox(output: ImageStudioOutput, index: number) {
  resetLightboxTransform()
  lightboxImage.value = {
    kind: 'output',
    title: `生成图片 ${index + 1}`,
    src: output.src,
    downloadName: downloadFileName(output, index),
    canEdit: true,
    output,
    index
  }
}

function openHistoryLightbox(record: ImageStudioHistoryRecord) {
  const image = record.images[0]
  if (!image?.src) return
  resetLightboxTransform()
  lightboxImage.value = {
    kind: 'history',
    title: record.prompt || '生成记录',
    src: image.src,
    downloadName: historyDownloadFileName(record),
    canEdit: true,
    record
  }
}

function openReferenceLightbox(file: File, index: number) {
  let src = referencePreviewUrls.value[index]
  if (!src) {
    src = createReferencePreviewUrl(file)
    if (src) {
      const nextSrc = src
      referencePreviewUrls.value = referencePreviewUrls.value.map((url, currentIndex) =>
        currentIndex === index ? nextSrc : url
      )
    }
  }
  if (!src) return
  resetLightboxTransform()
  lightboxImage.value = {
    kind: 'reference',
    title: file.name,
    src,
    downloadName: null,
    canEdit: false
  }
}

function closeLightbox() {
  lightboxImage.value = null
  resetLightboxTransform()
}

function resetLightboxTransform() {
  lightboxZoom.value = 1
  lightboxOffset.value = { x: 0, y: 0 }
  lightboxDragging.value = false
  lightboxDragStart.value = { pointerId: -1, x: 0, y: 0, offsetX: 0, offsetY: 0 }
}

function zoomLightbox(delta: number) {
  const nextZoom = clampLightboxZoom(lightboxZoom.value + delta)
  lightboxZoom.value = nextZoom
  if (nextZoom === 1) {
    lightboxOffset.value = { x: 0, y: 0 }
  }
}

function handleLightboxWheel(event: WheelEvent) {
  const direction = event.deltaY > 0 ? -0.12 : 0.12
  zoomLightbox(direction)
}

function startLightboxDrag(event: PointerEvent) {
  if (lightboxZoom.value <= 1) return
  const target = event.currentTarget as HTMLElement
  target.setPointerCapture?.(event.pointerId)
  lightboxDragging.value = true
  lightboxDragStart.value = {
    pointerId: event.pointerId,
    x: event.clientX,
    y: event.clientY,
    offsetX: lightboxOffset.value.x,
    offsetY: lightboxOffset.value.y
  }
}

function moveLightboxDrag(event: PointerEvent) {
  if (!lightboxDragging.value || lightboxDragStart.value.pointerId !== event.pointerId) return
  lightboxOffset.value = {
    x: lightboxDragStart.value.offsetX + event.clientX - lightboxDragStart.value.x,
    y: lightboxDragStart.value.offsetY + event.clientY - lightboxDragStart.value.y
  }
}

function endLightboxDrag(event: PointerEvent) {
  if (lightboxDragStart.value.pointerId === event.pointerId) {
    const target = event.currentTarget as HTMLElement
    target.releasePointerCapture?.(event.pointerId)
  }
  lightboxDragging.value = false
  lightboxDragStart.value = { pointerId: -1, x: 0, y: 0, offsetX: 0, offsetY: 0 }
}

function clampLightboxZoom(value: number) {
  return Math.min(5, Math.max(1, Number(value.toFixed(2))))
}

async function editLightboxImage() {
  const current = lightboxImage.value
  if (!current?.canEdit) return
  closeLightbox()
  if (current.kind === 'output') {
    await handleEditOutput(current.output, current.index)
    return
  }
  await handleEditHistoryImage(current.record)
}

async function outputToReferenceFile(output: ImageStudioOutput, index: number): Promise<File> {
  if (output.src.startsWith('data:')) {
    return dataUrlToFile(output.src, `generated-reference-${index + 1}`)
  }
  const response = await fetch(output.src)
  if (!response.ok) throw new Error('无法读取生成图片')
  const blob = await response.blob()
  const mimeType = blob.type || output.mimeType || 'image/png'
  return new File([blob], `generated-reference-${index + 1}.${extensionFromMimeType(mimeType)}`, {
    type: mimeType
  })
}

function dataUrlToFile(dataUrl: string, baseName: string): File {
  const { bytes, mimeType } = parseDataUrlBytes(dataUrl)
  return new File([bytes], `${baseName}.${extensionFromMimeType(mimeType)}`, {
    type: mimeType
  })
}

async function compressReferenceImagesForUpload(files: File[]): Promise<File[]> {
  return Promise.all(files.map((file, index) =>
    convertFileToWebp(file, `reference-${index + 1}`)
  ))
}

async function convertFileToWebp(file: File, fallbackBaseName: string): Promise<File> {
  const blob = await renderImageBlobToFormat(file, 'image/webp', 0.72)
  const baseName = file.name.replace(/\.[^.]+$/, '') || fallbackBaseName
  return new File([blob], `${baseName}.webp`, {
    type: 'image/webp',
    lastModified: Date.now()
  })
}

async function restoreOutputsToRequestedFormat(
  generatedOutputs: ImageStudioOutput[],
  requestedFormat: string
): Promise<ImageStudioOutput[]> {
  const targetMimeType = mimeTypeFromOutputFormat(requestedFormat)
  return generatedOutputs.map((output) => ({
    ...output,
    mimeType: output.mimeType || targetMimeType
  }))
}

async function downloadOutputAsFormat(output: ImageStudioOutput, index: number, format: string) {
  const targetFormat = normalizeDownloadFormat(format)
  const fileName = `image-studio-${index + 1}.${extensionFromFormat(targetFormat)}`
  await downloadImageSourceAsFormat(output.src, output.mimeType, targetFormat, fileName)
}

async function downloadHistoryImageAsFormat(record: ImageStudioHistoryRecord, format: string) {
  const image = record.images[0]
  if (!image?.src) return
  const targetFormat = normalizeDownloadFormat(format)
  const fileName = `image-studio-history-${record.id}.${extensionFromFormat(targetFormat)}`
  await downloadImageSourceAsFormat(image.src, image.mimeType, targetFormat, fileName)
}

async function downloadLightboxImageAsFormat(image: ImageStudioLightboxImage, format: string) {
  if (!image.downloadName) return
  const targetFormat = normalizeDownloadFormat(format)
  const sourceMimeType = lightboxImageMimeType(image)
  const baseName = image.downloadName.replace(/\.[^.]+$/, '') || 'image-studio'
  await downloadImageSourceAsFormat(
    image.src,
    sourceMimeType,
    targetFormat,
    `${baseName}.${extensionFromFormat(targetFormat)}`
  )
}

function lightboxImageMimeType(image: ImageStudioLightboxImage): string | undefined {
  if (image.kind === 'output') return image.output.mimeType
  if (image.kind === 'history') return image.record.images[0]?.mimeType
  return undefined
}

async function downloadImageSourceAsFormat(
  src: string,
  sourceMimeType: string | undefined,
  targetFormat: string,
  fileName: string
) {
  const targetMimeType = mimeTypeFromOutputFormat(targetFormat)
  const sourceFormat = formatFromMimeType(sourceMimeType)
  const canDirectDownload = sourceFormat === targetFormat || !sourceMimeType
  if (canDirectDownload) {
    triggerBrowserDownload(src, fileName)
    return
  }

  const sourceBlob = await imageSourceToBlob(src)
  const convertedBlob = await renderImageBlobToFormat(sourceBlob, targetMimeType, qualityForDownloadFormat(targetFormat))
  const objectUrl = URL.createObjectURL(convertedBlob)
  try {
    triggerBrowserDownload(objectUrl, fileName)
  } finally {
    window.setTimeout(() => URL.revokeObjectURL(objectUrl), 1000)
  }
}

async function imageSourceToBlob(src: string): Promise<Blob> {
  if (src.startsWith('data:')) {
    const { bytes, mimeType } = parseDataUrlBytes(src)
    return new Blob([bytes], { type: mimeType })
  }
  const response = await fetch(src)
  if (!response.ok) throw new Error('无法读取图片')
  return response.blob()
}

function triggerBrowserDownload(href: string, fileName: string) {
  const link = document.createElement('a')
  link.href = href
  link.download = fileName
  link.rel = 'noopener'
  document.body.appendChild(link)
  link.click()
  link.remove()
}

function normalizeDownloadFormat(format: string) {
  const normalized = format.trim().toLowerCase()
  if (normalized === 'webp') return 'webp'
  if (normalized === 'png') return 'png'
  return 'jpeg'
}

function qualityForDownloadFormat(format: string) {
  return format === 'png' ? undefined : 1
}

function parseDataUrlBytes(dataUrl: string): { bytes: Uint8Array, mimeType: string } {
  const match = dataUrl.match(/^data:([^;,]+)?(?:;base64)?,(.*)$/)
  if (!match) throw new Error('图片数据格式不正确')
  const mimeType = match[1] || 'image/png'
  const payload = match[2] || ''
  const bytes = dataUrl.includes(';base64,')
    ? Uint8Array.from(atob(payload), (char) => char.charCodeAt(0))
    : new TextEncoder().encode(decodeURIComponent(payload))
  return { bytes, mimeType }
}

async function renderImageBlobToFormat(blob: Blob, mimeType: string, quality?: number): Promise<Blob> {
  const image = await loadImageForCanvas(blob)
  const width = image instanceof ImageBitmap ? image.width : image.naturalWidth
  const height = image instanceof ImageBitmap ? image.height : image.naturalHeight
  const canvas = document.createElement('canvas')
  canvas.width = width
  canvas.height = height
  const context = canvas.getContext('2d')
  if (!context) throw new Error('浏览器不支持图片压缩')
  context.drawImage(image, 0, 0, width, height)
  if (image instanceof ImageBitmap) image.close()

  return canvasToBlob(canvas, mimeType, quality)
}

async function loadImageForCanvas(blob: Blob): Promise<ImageBitmap | HTMLImageElement> {
  if (typeof createImageBitmap === 'function') {
    return createImageBitmap(blob)
  }

  return new Promise((resolve, reject) => {
    const url = URL.createObjectURL(blob)
    const image = new Image()
    image.onload = () => {
      URL.revokeObjectURL(url)
      resolve(image)
    }
    image.onerror = () => {
      URL.revokeObjectURL(url)
      reject(new Error('图片读取失败'))
    }
    image.src = url
  })
}

function canvasToBlob(canvas: HTMLCanvasElement, mimeType: string, quality?: number): Promise<Blob> {
  return new Promise((resolve, reject) => {
    canvas.toBlob((blob) => {
      if (blob) {
        resolve(blob)
      } else {
        reject(new Error('图片格式转换失败'))
      }
    }, mimeType, quality)
  })
}

function mimeTypeFromOutputFormat(format?: string): string {
  const normalized = (format || '').trim().toLowerCase()
  if (normalized === 'webp') return 'image/webp'
  if (normalized === 'jpeg' || normalized === 'jpg') return 'image/jpeg'
  return 'image/png'
}

function createReferencePreviewUrl(file: File): string {
  if (typeof URL === 'undefined' || typeof URL.createObjectURL !== 'function') return ''
  return URL.createObjectURL(file)
}

function readReferenceFileAsDataUrl(file: File): Promise<string> {
  if (typeof FileReader === 'undefined') {
    return Promise.reject(new Error('FileReader is unavailable'))
  }

  return new Promise((resolve, reject) => {
    const reader = new FileReader()
    reader.onload = () => {
      const result = reader.result
      if (typeof result === 'string') {
        resolve(result)
      } else {
        reject(new Error('Invalid image preview data'))
      }
    }
    reader.onerror = () => reject(reader.error ?? new Error('Failed to read image preview'))
    reader.readAsDataURL(file)
  })
}

function revokeReferencePreviewUrl(url?: string) {
  if (!url?.startsWith('blob:') || typeof URL === 'undefined' || typeof URL.revokeObjectURL !== 'function') return
  URL.revokeObjectURL(url)
}

function revokeReferencePreviewUrls() {
  for (const url of referencePreviewUrls.value) {
    revokeReferencePreviewUrl(url)
  }
  referencePreviewUrls.value = []
}

function persistHistoryRecord(
  input: {
    model: string
    prompt: string
    size: string
    count: number
    outputFormat: string
  },
  generatedOutputs: ImageStudioOutput[],
  mode: Exclude<ImageStudioMode, 'history'>
) {
  if (typeof window === 'undefined' || generatedOutputs.length === 0) return
  const record: ImageStudioHistoryRecord = {
    id: `${Date.now()}-${Math.random().toString(36).slice(2, 8)}`,
    createdAt: new Date().toISOString(),
    mode,
    prompt: input.prompt,
    model: input.model,
    size: input.size,
    count: input.count,
    outputFormat: input.outputFormat || 'jpeg',
    images: generatedOutputs.map((output) => ({
      id: output.id,
      src: output.src,
      mimeType: output.mimeType,
      revisedPrompt: output.revisedPrompt
    }))
  }
  const allRecords = [record, ...historyRecords.value]
  const nextRecords = allRecords.slice(0, historyLimit.value)
  const overflowRecords = allRecords.slice(historyLimit.value)
  historyRecords.value = nextRecords
  selectedHistoryId.value = record.id
  saveHistoryIndex(nextRecords)
  void saveHistoryImages(record)
  void Promise.all(overflowRecords.map((overflowRecord) => deleteHistoryImages(overflowRecord)))
}

function saveHistoryIndex(records: ImageStudioHistoryRecord[]) {
  if (typeof window === 'undefined') return
  const compactRecords = records.map((record) => ({
    ...record,
    images: record.images.map((image) => ({
      ...image,
      src: image.src.startsWith('data:') ? '' : image.src
    }))
  }))
  window.localStorage.setItem(historyStorageKey, JSON.stringify(compactRecords))
}

async function loadHistoryRecords(): Promise<ImageStudioHistoryRecord[]> {
  if (typeof window === 'undefined') return []
  try {
    const parsed = JSON.parse(window.localStorage.getItem(historyStorageKey) || '[]')
    if (!Array.isArray(parsed)) return []
    const records = parsed
      .filter(isHistoryRecord)
      .slice(0, historyLimit.value)
    await hydrateHistoryImages(records)
    return records
  } catch {
    return []
  }
}

function loadHistoryLimit(): number {
  if (typeof window === 'undefined') return defaultHistoryLimit
  const raw = window.localStorage.getItem(historyLimitStorageKey)
  return normalizeHistoryLimitValue(Number(raw || defaultHistoryLimit))
}

function saveHistoryLimit(limit: number) {
  if (typeof window === 'undefined') return
  window.localStorage.setItem(historyLimitStorageKey, String(limit))
}

function startHistoryLimitEdit() {
  historyLimitDraft.value = historyLimit.value
  historyLimitConfirmOpen.value = false
  historyLimitEditing.value = true
}

function cancelHistoryLimitEdit() {
  historyLimitDraft.value = historyLimit.value
  historyLimitEditing.value = false
  historyLimitConfirmOpen.value = false
}

function submitHistoryLimitEdit() {
  const nextLimit = normalizeHistoryLimitValue(historyLimitDraft.value)
  historyLimitDraft.value = nextLimit
  if (nextLimit < historyRecords.value.length) {
    pendingHistoryLimit.value = nextLimit
    historyLimitConfirmOpen.value = true
    return
  }
  applyHistoryLimit(nextLimit)
}

function confirmHistoryLimitReduction() {
  applyHistoryLimit(pendingHistoryLimit.value)
  historyLimitConfirmOpen.value = false
}

function cancelHistoryLimitReduction() {
  pendingHistoryLimit.value = historyLimit.value
  historyLimitConfirmOpen.value = false
}

function applyHistoryLimit(limit: number) {
  const normalizedLimit = normalizeHistoryLimitValue(limit)
  historyLimit.value = normalizedLimit
  historyLimitDraft.value = normalizedLimit
  historyLimitEditing.value = false
  saveHistoryLimit(normalizedLimit)
  enforceHistoryLimit(normalizedLimit)
}

function normalizeHistoryLimitValue(value: number): number {
  if (!Number.isFinite(value)) return defaultHistoryLimit
  return Math.min(maxHistoryLimit, Math.max(minHistoryLimit, Math.floor(value)))
}

function enforceHistoryLimit(limit = historyLimit.value) {
  const nextRecords = historyRecords.value.slice(0, limit)
  const overflowRecords = historyRecords.value.slice(limit)
  if (overflowRecords.length === 0) {
    saveHistoryIndex(nextRecords)
    return
  }
  historyRecords.value = nextRecords
  if (selectedHistoryId.value && !nextRecords.some((record) => record.id === selectedHistoryId.value)) {
    selectedHistoryId.value = nextRecords[0]?.id ?? ''
  }
  saveHistoryIndex(nextRecords)
  void Promise.all(overflowRecords.map((record) => deleteHistoryImages(record)))
}

async function saveHistoryImages(record: ImageStudioHistoryRecord) {
  const entries = record.images
    .filter((image) => image.src.startsWith('data:'))
    .map((image) => ({
      id: `${record.id}:${image.id}`,
      src: image.src
    }))
  if (entries.length === 0) return
  try {
    const db = await openHistoryDb()
    await new Promise<void>((resolve, reject) => {
      const transaction = db.transaction(historyDbStoreName, 'readwrite')
      const store = transaction.objectStore(historyDbStoreName)
      for (const entry of entries) {
        store.put(entry)
      }
      transaction.oncomplete = () => resolve()
      transaction.onerror = () => reject(transaction.error)
    })
  } catch {
    // IndexedDB is best-effort local history storage. Visible outputs remain usable even if persistence fails.
  }
}

async function deleteHistoryImages(record: ImageStudioHistoryRecord) {
  try {
    const db = await openHistoryDb()
    await new Promise<void>((resolve, reject) => {
      const transaction = db.transaction(historyDbStoreName, 'readwrite')
      const store = transaction.objectStore(historyDbStoreName)
      for (const image of record.images) {
        store.delete(`${record.id}:${image.id}`)
      }
      transaction.oncomplete = () => resolve()
      transaction.onerror = () => reject(transaction.error)
    })
  } catch {
    // Deleting local thumbnails is best-effort; the index removal above is the user-visible source of truth.
  }
}

async function clearOrphanHistoryCache() {
  if (cleaningHistoryCache.value) return
  cleaningHistoryCache.value = true
  historyCacheMessage.value = ''
  try {
    const removedCount = await deleteOrphanHistoryImages(historyRecords.value)
    historyCacheMessage.value = removedCount > 0 ? `已清理 ${removedCount} 张缓存图片` : '没有可清理缓存'
  } catch {
    historyCacheMessage.value = '清理失败，请稍后重试'
  } finally {
    cleaningHistoryCache.value = false
  }
}

function getVisibleHistoryImageKeys(records: ImageStudioHistoryRecord[]) {
  return new Set(records.flatMap((record) =>
    record.images.map((image) => `${record.id}:${image.id}`)
  ))
}

async function deleteOrphanHistoryImages(records: ImageStudioHistoryRecord[]): Promise<number> {
  const visibleKeys = getVisibleHistoryImageKeys(records)
  const db = await openHistoryDb()
  return new Promise((resolve, reject) => {
    const transaction = db.transaction(historyDbStoreName, 'readwrite')
    const store = transaction.objectStore(historyDbStoreName)
    let removedCount = 0
    const request = store.openCursor()
    request.onsuccess = () => {
      const cursor = request.result
      if (!cursor) return
      const key = typeof cursor.primaryKey === 'string' ? cursor.primaryKey : String(cursor.primaryKey)
      if (!visibleKeys.has(key)) {
        cursor.delete()
        removedCount += 1
      }
      cursor.continue()
    }
    request.onerror = () => reject(request.error)
    transaction.oncomplete = () => resolve(removedCount)
    transaction.onerror = () => reject(transaction.error)
  })
}

async function hydrateHistoryImages(records: ImageStudioHistoryRecord[]) {
  const missing = records.flatMap((record) =>
    record.images
      .filter((image) => !image.src)
      .map((image) => ({ record, image }))
  )
  if (missing.length === 0) return
  try {
    const db = await openHistoryDb()
    await Promise.all(missing.map(({ image, record }) =>
      new Promise<void>((resolve) => {
        const request = db
          .transaction(historyDbStoreName, 'readonly')
          .objectStore(historyDbStoreName)
          .get(`${record.id}:${image.id}`)
        request.onsuccess = () => {
          const value = request.result as { src?: string } | undefined
          if (value?.src) image.src = value.src
          resolve()
        }
        request.onerror = () => resolve()
      })
    ))
  } catch {
    // Leave missing local-only images empty when IndexedDB is unavailable.
  }
}

function openHistoryDb(): Promise<IDBDatabase> {
  return new Promise((resolve, reject) => {
    if (typeof indexedDB === 'undefined') {
      reject(new Error('IndexedDB unavailable'))
      return
    }
    const request = indexedDB.open(historyDbName, historyDbVersion)
    request.onupgradeneeded = () => {
      const db = request.result
      if (!db.objectStoreNames.contains(historyDbStoreName)) {
        db.createObjectStore(historyDbStoreName, { keyPath: 'id' })
      }
    }
    request.onsuccess = () => resolve(request.result)
    request.onerror = () => reject(request.error)
  })
}

function isHistoryRecord(value: unknown): value is ImageStudioHistoryRecord {
  if (!value || typeof value !== 'object') return false
  const record = value as Partial<ImageStudioHistoryRecord>
  return (
    typeof record.id === 'string' &&
    typeof record.createdAt === 'string' &&
    (record.mode === 'generate' || record.mode === 'edit') &&
    typeof record.prompt === 'string' &&
    typeof record.model === 'string' &&
    typeof record.size === 'string' &&
    typeof record.count === 'number' &&
    typeof record.outputFormat === 'string' &&
    Array.isArray(record.images)
  )
}

function formatHistoryTime(value: string): string {
  const date = new Date(value)
  if (Number.isNaN(date.getTime())) return ''
  return date.toLocaleString('zh-CN', {
    month: '2-digit',
    day: '2-digit',
    hour: '2-digit',
    minute: '2-digit'
  })
}

function downloadFileName(output: ImageStudioOutput, index: number): string {
  return `image-studio-${index + 1}.${extensionFromMimeType(output.mimeType)}`
}

function historyDownloadFileName(record: ImageStudioHistoryRecord): string {
  return `image-studio-history-${record.id}.${extensionFromMimeType(record.images[0]?.mimeType)}`
}

function extensionFromFormat(format: string): string {
  return format === 'jpeg' ? 'jpg' : format
}

function formatFromMimeType(mimeType?: string): string {
  const normalized = (mimeType || '').toLowerCase()
  if (normalized.includes('webp')) return 'webp'
  if (normalized.includes('jpeg') || normalized.includes('jpg')) return 'jpeg'
  if (normalized.includes('png')) return 'png'
  return ''
}

function extensionFromMimeType(mimeType?: string): string {
  return extensionFromFormat(formatFromMimeType(mimeType) || 'png')
}
</script>

<style src="./ImageStudioView.css"></style>
