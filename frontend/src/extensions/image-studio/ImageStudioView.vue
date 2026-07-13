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
            <Transition name="history-guidance">
              <div
                v-if="historyGuidanceVisible && activeTab !== 'history'"
                class="history-guidance"
                data-testid="history-guidance"
                :style="historyGuidanceStyle"
                role="status"
                aria-live="polite"
              >
                <div class="history-guidance-icon" aria-hidden="true">
                  <Icon name="clock" size="sm" />
                </div>
                <div class="history-guidance-copy">
                  <strong>任务已进入历史记录</strong>
                  <span>后台正在生成，可在历史中查看排队与生成进度。</span>
                </div>
                <div class="history-guidance-actions">
                  <button
                    type="button"
                    class="history-guidance-primary"
                    data-testid="history-guidance-open"
                    @click="openGuidedHistory"
                  >
                    查看历史
                  </button>
                  <button
                    type="button"
                    class="history-guidance-close"
                    aria-label="关闭历史引导"
                    @click="dismissHistoryGuidance"
                  >
                    ×
                  </button>
                </div>
              </div>
            </Transition>
            <template v-if="activeTab === 'history'">
              <div class="history-header">
                <div>
                  <h2>最近生成</h2>
                  <p>{{ historyRecords.length }} 条任务记录 · 缩略图列表，原图按需加载</p>
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
                    <button
                      type="button"
                      class="secondary-action"
                      data-testid="history-refresh-button"
                      :disabled="refreshingHistory"
                      @click="refreshDisplayedHistoryRecords"
                    >
                      <Icon name="refresh" size="sm" />
                      {{ refreshingHistory ? '刷新中' : '刷新' }}
                    </button>
                    <button
                      type="button"
                      class="secondary-action"
                      data-testid="history-cache-clean-button"
                      :disabled="cleaningHistoryCache"
                      @click="clearHistoryImageCache"
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
                <p>完成第一次生图后，这里会显示最近任务的缩略图和参数。</p>
              </div>

              <div v-else-if="filteredHistoryRecords.length === 0" class="history-empty compact">
                <Icon name="search" size="xl" />
                <h2>没有匹配记录</h2>
                <p>换个关键词，或者切回全部模式看看。</p>
              </div>

              <div
                v-else
                ref="historyGridRef"
                class="history-grid"
                data-testid="history-grid"
                :style="{ '--history-grid-columns': displayedHistoryColumns.length }"
              >
                <div
                  v-for="(columnRecords, columnIndex) in displayedHistoryColumns"
                  :key="`history-column-${columnIndex}`"
                  class="history-grid-column"
                >
                  <article
                    v-for="record in columnRecords"
                    :key="record.id"
                    class="history-card"
                    :class="{ active: selectedHistoryId === record.id, 'is-new': highlightedHistoryId === record.id }"
                    data-testid="history-card"
                    @click="selectHistoryRecord(record)"
                  >
                    <div class="history-preview-frame">
                      <img
                        v-if="record.images[0]"
                        :src="record.images[0].src"
                        :alt="record.prompt || '生成记录'"
                        @error="handleHistoryThumbnailError(record)"
                      >
                      <div
                        v-else
                        class="history-preview-placeholder"
                        data-testid="history-preview-placeholder"
                        :style="{ '--history-preview-aspect-ratio': historyPreviewAspectRatio(record) }"
                      >
                        <span class="history-preview-placeholder-kicker">{{ historyPreviewPlaceholderKicker(record) }}</span>
                        <strong>{{ historyStatusText(record) }}</strong>
                        <span>{{ historyPreviewPlaceholderCopy(record) }}</span>
                        <button
                          v-if="canRegenerateHistoryRecord(record)"
                          type="button"
                          class="history-preview-regenerate-button"
                          data-testid="history-regenerate-button"
                          :disabled="regeneratingHistoryJobId === record.id"
                          @click.stop="regenerateHistoryRecord(record)"
                        >
                          {{ regeneratingHistoryJobId === record.id ? '重新生成中' : '重新生成' }}
                        </button>
                      </div>
                      <div class="history-card-actions">
                        <button
                          v-if="record.images[0]"
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
                      <p>{{ historyStatusText(record) }}</p>
                      <p>{{ record.size }} · {{ record.outputFormat.toUpperCase() }}</p>
                    </div>
                  </article>
                </div>
              </div>
              <div v-if="historyRecords.length > 0" class="history-pagination">
                <span>已显示 {{ historyRecords.length }} / 共 {{ historyTotal }} 条</span>
                <button
                  v-if="hasMoreHistory"
                  type="button"
                  class="secondary-action"
                  data-testid="history-load-more-button"
                  :disabled="loadingMoreHistory"
                  @click="loadMoreHistoryRecords"
                >
                  {{ loadingMoreHistory ? '加载中' : '加载更多' }}
                </button>
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
              <div
                v-if="activeTab !== 'history'"
                class="history-nav-guidance"
                data-testid="history-nav-guidance"
              >
                <div class="history-nav-guidance-copy">
                  <strong>任务进度请到历史记录查看</strong>
                  <div class="history-nav-guidance-stats">
                    <span class="history-nav-guidance-stat">
                      <b>生成中：</b>
                      <em>{{ historyStats.pendingCount }}</em>
                    </span>
                    <span class="history-nav-guidance-stat">
                      <b>失败：</b>
                      <em>{{ historyStats.failedCount }}</em>
                    </span>
                  </div>
                </div>
              </div>
              <button
                type="button"
                class="studio-tab"
                :class="{ active: activeTab === 'history', 'history-return-tab': activeTab === 'history', 'has-pending-jobs': historyStats.pendingCount > 0 || historyStats.failedCount > 0 }"
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
              :submitting="submitting || historyTransferVisible"
              :transfer-origin-active="historyTransferVisible"
              :prompt-examples="promptExamples"
              :prompt-polish-model="promptPolishModel"
              :prompt-polish-model-options="promptPolishModelOptions"
              :prompt-polish-key="promptPolishKeyValue"
              :prompt-polish-key-options="promptPolishKeyOptions"
              :prompt-polish-disabled="promptPolishDisabled"
              @toggle-template="toggleTemplateDrawer"
              @polish-prompt="polishPrompt"
              @clear-prompt="clearPrompt"
              @toggle-prompt-history="togglePromptHistory"
              @update:prompt-history-mode="promptHistoryMode = $event"
              @update:prompt-history-search="promptHistorySearch = $event"
              @update:prompt-polish-model="promptPolishModel = $event"
              @update:prompt-polish-key="promptPolishKeyValue = $event"
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
          <div
            v-if="historyTransferVisible"
            class="history-transfer-effect"
            data-testid="history-transfer-effect"
            aria-hidden="true"
            :style="historyTransferStyle"
          >
            <span class="history-transfer-aura" data-testid="history-transfer-aura"></span>
            <span class="history-transfer-orb" data-testid="history-transfer-orb"></span>
            <span class="history-transfer-arrival"></span>
          </div>
        </div>
      </section>
    </div>
  </AppLayout>
</template>

<script setup lang="ts">
import { computed, nextTick, onBeforeUnmount, onMounted, reactive, ref, watch } from 'vue'
import { keysAPI } from '@/api'
import { getPublicSettings } from '@/api/auth'
import Icon from '@/components/icons/Icon.vue'
import AppLayout from '@/components/layout/AppLayout.vue'
import GenerationSettingsPanel from './components/GenerationSettingsPanel.vue'
import ImageLightbox from './components/ImageLightbox.vue'
import ImagePreviewPanel from './components/ImagePreviewPanel.vue'
import PromptComposer from './components/PromptComposer.vue'
import ReferenceStrip from './components/ReferenceStrip.vue'
import StudioSelect from './components/StudioSelect.vue'
import TemplateDrawer from './components/TemplateDrawer.vue'
import {
  createImageStudioJob,
  deleteImageStudioJob,
  fetchImageStudioThumbnail,
  fetchImageStudioOriginal,
  getImageStudioJobStats,
  listGatewayModels,
  listImageStudioJobs,
  sendPromptPolishRequest
} from './imageStudioApi'
import {
  clearImageStudioAssetCache,
  deleteImageStudioAssetCache,
  getCachedImageStudioAsset,
  putImageStudioAssetCache
} from './imageStudioCache'
import { estimateHistoryColumnCount, groupHistoryRecordsByVisualColumn } from './historyLayout'
import { parseAdvancedJson } from './payload'
import type {
  ImageStudioJob,
  ImageStudioHistoryRecord,
  ImageStudioLightboxImage,
  ImageStudioMode,
  ImageStudioOutput,
  ImageStudioPromptHistoryRecord,
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

interface HistoryTransferState {
  visible: boolean
  startX: number
  startY: number
  originWidth: number
  originHeight: number
  endX: number
  endY: number
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
const promptPolishKeyValue = ref('')
const apiKeys = ref<StudioApiKey[]>([])
const promptPolishApiKeys = ref<StudioApiKey[]>([])
const promptPolishModelsByGroup = ref<Record<number, string[]>>({})
const loadingPromptPolishModels = ref(false)
const imageStudioAvailableGroupIDs = ref<number[]>([])
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
const historyTotal = ref(0)
const historyPage = ref(1)
const loadingMoreHistory = ref(false)
const refreshingHistory = ref(false)
const historyStats = reactive({
  pendingCount: 0,
  failedCount: 0
})
const regeneratingHistoryJobId = ref('')
const historyGuidanceVisible = ref(false)
const historyTransferState = ref<HistoryTransferState>({
  visible: false,
  startX: 0,
  startY: 0,
  originWidth: 0,
  originHeight: 0,
  endX: 0,
  endY: 0
})
const highlightedHistoryId = ref('')
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
const historyGridRef = ref<HTMLElement | null>(null)
const historyColumnCount = ref(1)
const maxReferenceImages = 4
const maxReferenceImageSize = 20 * 1024 * 1024
const promptHistoryStorageKey = 'sub2api:image-studio:prompt-history:v1'
const historyPageSize = 50
const originalImageCache = new Map<number, { objectUrl: string, blob: Blob }>()
const thumbnailImageCache = new Map<number, string>()
let historyGuidanceTimer: number | undefined
let highlightedHistoryTimer: number | undefined
let historyTransferTimer: number | undefined

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
  { value: 'gpt-5.5', label: '5.5' },
  { value: 'gpt-5.6-sol', label: '5.6 Sol' },
  { value: 'gpt-5.6-terra', label: '5.6 Terra' },
  { value: 'gpt-5.6-luna', label: '5.6 Luna' }
]

const activeKeys = computed(() => apiKeys.value.filter((key) => key.status === 'active'))
const selectedKey = computed(() => activeKeys.value.find((key) => key.key === selectedKeyValue.value) ?? null)
const compatiblePromptPolishKeys = computed(() => promptPolishApiKeys.value.filter((key) => {
  const groupID = key.group?.id
  return typeof groupID === 'number' &&
    (promptPolishModelsByGroup.value[groupID] || []).includes(promptPolishModel.value)
}))
const promptPolishKeyOptions = computed(() => compatiblePromptPolishKeys.value.map((key) => ({
  value: key.key,
  label: `${key.name} · ${key.group?.name || '未命名分组'}`
})))
const promptPolishDisabled = computed(() =>
  loadingPromptPolishModels.value || !promptPolishKeyValue.value
)
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
  if (activeTab.value !== 'history') return '历史记录'
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
const displayedHistoryColumns = computed(() =>
  groupHistoryRecordsByVisualColumn(filteredHistoryRecords.value, historyColumnCount.value)
)
const hasMoreHistory = computed(() => historyRecords.value.length < historyTotal.value)
const historyTransferVisible = computed(() => historyTransferState.value.visible)
const historyGuidanceStyle = computed(() => {
  const viewportWidth = typeof window !== 'undefined' ? window.innerWidth : 0
  if (viewportWidth > 0 && viewportWidth <= 720) {
    return { '--history-guidance-max-width': 'calc(100vw - 24px)' }
  }

  const mainRect = studioMainRef.value?.getBoundingClientRect()
  const maxWidth = Math.min(780, Math.max(320, (mainRect?.width || viewportWidth || 780) - 40))
  const left = mainRect ? Math.round(mainRect.left + mainRect.width / 2) : Math.round((viewportWidth || maxWidth) / 2)
  const top = mainRect ? Math.round(mainRect.top + 20) : 92

  return {
    left: `${left}px`,
    top: `${top}px`,
    '--history-guidance-max-width': `${maxWidth}px`
  }
})
const historyTransferStyle = computed(() => ({
  '--transfer-start-x': `${Math.round(historyTransferState.value.startX)}px`,
  '--transfer-start-y': `${Math.round(historyTransferState.value.startY)}px`,
  '--transfer-origin-width': `${Math.round(historyTransferState.value.originWidth)}px`,
  '--transfer-origin-height': `${Math.round(historyTransferState.value.originHeight)}px`,
  '--transfer-end-x': `${Math.round(historyTransferState.value.endX)}px`,
  '--transfer-end-y': `${Math.round(historyTransferState.value.endY)}px`,
  '--transfer-dx': `${Math.round(historyTransferState.value.endX - historyTransferState.value.startX)}px`,
  '--transfer-dy': `${Math.round(historyTransferState.value.endY - historyTransferState.value.startY)}px`,
  '--transfer-angle': `${Math.atan2(
    historyTransferState.value.endY - historyTransferState.value.startY,
    historyTransferState.value.endX - historyTransferState.value.startX
  )}rad`
}))
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
  if (submitting.value || historyTransferVisible.value || !selectedKeyValue.value) return true
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
  window.addEventListener('resize', syncResponsiveLayout)
  historyRecords.value = await loadHistoryRecords()
  await refreshHistoryStats()
  promptHistoryRecords.value = loadPromptHistoryRecords()
  selectedHistoryId.value = historyRecords.value[0]?.id ?? ''
  await loadKeys()
  layoutResizeObserver = new ResizeObserver(() => {
    syncResponsiveLayout()
  })
  if (studioMainRef.value) layoutResizeObserver.observe(studioMainRef.value)
  if (composerPanelRef.value) layoutResizeObserver.observe(composerPanelRef.value)
  syncResponsiveLayout()
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

watch([promptPolishModel, compatiblePromptPolishKeys], () => {
  if (!compatiblePromptPolishKeys.value.some((key) => key.key === promptPolishKeyValue.value)) {
    promptPolishKeyValue.value = compatiblePromptPolishKeys.value[0]?.key || ''
  }
}, { immediate: true })

onBeforeUnmount(() => {
  document.removeEventListener('pointerdown', handleDocumentPointerDown)
  document.removeEventListener('paste', handleDocumentPaste)
  window.removeEventListener('resize', syncResponsiveLayout)
  if (suppressReferenceTransitionTimer) window.clearTimeout(suppressReferenceTransitionTimer)
  if (historyGuidanceTimer) window.clearTimeout(historyGuidanceTimer)
  if (highlightedHistoryTimer) window.clearTimeout(highlightedHistoryTimer)
  if (historyTransferTimer) window.clearTimeout(historyTransferTimer)
  layoutResizeObserver?.disconnect()
  revokeReferencePreviewUrls()
  revokeOriginalImageCache()
  revokeThumbnailImageCache()
})

watch(
  [activeTab, templateDrawerOpen, composerExpanded],
  async () => {
    await nextTick()
    syncResponsiveLayout()
  },
  { immediate: true }
)

watch(filteredHistoryRecords, async () => {
  await nextTick()
  syncHistoryColumnCount()
})

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
    dismissHistoryGuidance()
    void refreshHistoryRecords()
    void refreshHistoryStats()
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

function syncResponsiveLayout() {
  syncTemplateDrawerLayout()
  syncHistoryColumnCount()
}

function syncHistoryColumnCount() {
  if (activeTab.value !== 'history') return
  const gridEl = historyGridRef.value
  const width = gridEl?.clientWidth || studioMainRef.value?.clientWidth || 0
  historyColumnCount.value = estimateHistoryColumnCount(width)
}

async function loadKeys() {
  loadingKeys.value = true
  setSessionError(activeCreationSession.value, '')
  try {
    const [settings, keys] = await Promise.all([
      getPublicSettings(),
      listAllApiKeys()
    ])
    imageStudioAvailableGroupIDs.value = settings.image_studio_available_group_ids ?? []
    apiKeys.value = keys.filter(isImageStudioApiKey)
    promptPolishApiKeys.value = keys.filter(isPromptPolishApiKey)
    const preferred = apiKeys.value[0]
    selectedKeyValue.value = preferred?.key ?? ''
    await loadPromptPolishModels(promptPolishApiKeys.value)
  } catch (error) {
    setSessionError(activeCreationSession.value, error instanceof Error ? error.message : '加载 API Key 失败')
  } finally {
    loadingKeys.value = false
  }
}

async function listAllApiKeys(): Promise<StudioApiKey[]> {
  const pageSize = 1000
  const firstPage = await keysAPI.list(1, pageSize)
  const keys = [...firstPage.items] as StudioApiKey[]

  for (let page = 2; page <= firstPage.pages; page += 1) {
    const response = await keysAPI.list(page, pageSize)
    keys.push(...response.items as StudioApiKey[])
  }

  return keys
}

function isImageStudioApiKey(key: StudioApiKey) {
  const groupID = key.group?.id
  return key.status === 'active' &&
    key.group?.platform === 'openai' &&
    key.group?.allow_image_generation === true &&
    typeof groupID === 'number' &&
    imageStudioAvailableGroupIDs.value.includes(groupID)
}

function isPromptPolishApiKey(key: StudioApiKey) {
  return key.status === 'active' &&
    key.group?.platform === 'openai' &&
    typeof key.group?.id === 'number'
}

async function loadPromptPolishModels(keys: StudioApiKey[]) {
  loadingPromptPolishModels.value = true
  const representatives = new Map<number, StudioApiKey>()
  for (const key of keys) {
    const groupID = key.group?.id
    if (typeof groupID === 'number' && !representatives.has(groupID)) {
      representatives.set(groupID, key)
    }
  }

  const entries = await Promise.all([...representatives.entries()].map(async ([groupID, key]) => {
    try {
      return [groupID, await listGatewayModels(key.key)] as const
    } catch {
      return [groupID, []] as const
    }
  }))
  promptPolishModelsByGroup.value = Object.fromEntries(entries)
  loadingPromptPolishModels.value = false
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
    const requestInput = {
      ...commonInput,
      outputFormat: commonInput.outputFormat || 'jpeg'
    }

    updateGenerationStreamState(session, 'preparing', '任务已创建，正在进入队列...')
    const job = submitMode === 'edit'
      ? await submitEdit(requestInput)
      : await submitGeneration(requestInput)

    updateSessionFromJob(job, session)
    await upsertHistoryRecord(job, true)
    showHistoryGuidance(job)
    addPromptHistoryRecord(commonInput.prompt, submitMode, 'generated')
    session.outputs = []
    session.streamState = createIdleStreamState()
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

function createIdleStreamState(): ImageStudioStreamState {
  return {
    phase: 'idle',
    message: ''
  }
}

function updateSessionFromJob(job: ImageStudioJob, session: ImageStudioCreationSession) {
  if (job.status === 'queued') {
    updateGenerationStreamState(
      session,
      'preparing',
      job.attemptCount > 0 ? '任务重试排队中，等待再次处理...' : '任务已进入队列，等待处理...'
    )
    return
  }
  if (job.status === 'running') {
    updateGenerationStreamState(
      session,
      'generating',
      job.attemptCount > 0 ? `正在第 ${job.attemptCount + 1} 次尝试生成图片...` : '正在生成图片...'
    )
    return
  }
  if (job.status === 'succeeded') {
    updateGenerationStreamState(session, 'image_done', '图片生成成功，正在加载原图...')
    return
  }
  updateGenerationStreamState(session, 'failed', job.errorMessage || '生成失败')
}

async function refreshHistoryRecords(preferredJobId?: number) {
  const response = await listImageStudioJobs(1, historyPageSize)
  historyPage.value = 1
  historyTotal.value = response.total
  historyRecords.value = await Promise.all(response.items.map(mapJobToHistoryRecord))
  await refreshHistoryStats()
  if (preferredJobId) {
    selectedHistoryId.value = String(preferredJobId)
    highlightHistoryRecord(preferredJobId)
    return
  }
  if (!historyRecords.value.some((record) => record.id === selectedHistoryId.value)) {
    selectedHistoryId.value = historyRecords.value[0]?.id ?? ''
  }
}

async function refreshHistoryStats() {
  try {
    const stats = await getImageStudioJobStats()
    historyStats.pendingCount = stats.pendingCount
    historyStats.failedCount = stats.failedCount
  } catch {
    historyStats.pendingCount = 0
    historyStats.failedCount = 0
  }
}

async function upsertHistoryRecord(job: ImageStudioJob, selectRecord = false) {
  const nextRecord = await mapJobToHistoryRecord(job)
  const nextRecords = historyRecords.value.filter((record) => record.id !== nextRecord.id)
  historyRecords.value = [nextRecord, ...nextRecords]
  historyTotal.value = Math.max(historyTotal.value, historyRecords.value.length)
  if (selectRecord || !selectedHistoryId.value) {
    selectedHistoryId.value = nextRecord.id
  }
  highlightHistoryRecord(job.id)
  await refreshHistoryStats()
}

function showHistoryGuidance(job: ImageStudioJob) {
  highlightHistoryRecord(job.id)
  triggerHistoryTransfer()
  historyGuidanceVisible.value = true
  if (historyGuidanceTimer) window.clearTimeout(historyGuidanceTimer)
  historyGuidanceTimer = window.setTimeout(() => {
    historyGuidanceVisible.value = false
    historyGuidanceTimer = undefined
  }, 8000)
}

async function triggerHistoryTransfer() {
  await nextTick()
  const originEl = composerPanelRef.value?.querySelector('[data-testid="composer-transfer-anchor"]') as HTMLElement | null
  const targetEl = document.querySelector('[data-testid="tab-history"]') as HTMLElement | null
  if (!originEl || !targetEl) return

  const originRect = originEl.getBoundingClientRect()
  const targetRect = targetEl.getBoundingClientRect()
  const startX = originRect.left + originRect.width / 2
  const startY = originRect.top + originRect.height / 2
  const endX = targetRect.left + targetRect.width / 2
  const endY = targetRect.top + targetRect.height / 2

  historyTransferState.value = {
    visible: true,
    startX,
    startY,
    originWidth: originRect.width,
    originHeight: originRect.height,
    endX,
    endY
  }
  if (historyTransferTimer) window.clearTimeout(historyTransferTimer)
  historyTransferTimer = window.setTimeout(() => {
    historyTransferState.value = {
      ...historyTransferState.value,
      visible: false
    }
    historyTransferTimer = undefined
  }, 1500)
}

function dismissHistoryGuidance() {
  historyGuidanceVisible.value = false
  if (historyGuidanceTimer) {
    window.clearTimeout(historyGuidanceTimer)
    historyGuidanceTimer = undefined
  }
}

function openGuidedHistory() {
  const targetId = highlightedHistoryId.value || selectedHistoryId.value
  dismissHistoryGuidance()
  setActiveTab('history')
  if (targetId) {
    selectedHistoryId.value = targetId
    highlightHistoryRecord(Number(targetId))
  }
}

function highlightHistoryRecord(jobId: number) {
  if (!jobId) return
  highlightedHistoryId.value = String(jobId)
  if (highlightedHistoryTimer) window.clearTimeout(highlightedHistoryTimer)
  highlightedHistoryTimer = window.setTimeout(() => {
    if (highlightedHistoryId.value === String(jobId)) {
      highlightedHistoryId.value = ''
    }
    highlightedHistoryTimer = undefined
  }, 4000)
}

async function mapJobToHistoryRecord(job: ImageStudioJob): Promise<ImageStudioHistoryRecord> {
  if (job.assetsDeletedAt) {
    revokeCachedOriginal(job.id)
  }
  const thumbnailSrc = await getOrFetchThumbnailUrl(job)
  return {
    id: String(job.id),
    jobId: job.id,
    createdAt: job.queuedAt,
    mode: job.mode,
    status: job.status,
    attemptCount: job.attemptCount,
    maxAttempts: job.maxAttempts,
    nextAttemptAt: job.nextAttemptAt,
    prompt: job.prompt,
    model: job.model,
    size: job.size,
    count: 1,
    outputFormat: job.outputFormat || 'png',
    errorMessage: job.errorMessage,
    thumbnailUrl: job.thumbnailUrl,
    originalUrl: job.originalUrl,
    expiresAt: job.expiresAt,
    assetsDeletedAt: job.assetsDeletedAt,
    images: thumbnailSrc
      ? [{
        id: `job-${job.id}`,
        src: thumbnailSrc,
        mimeType: job.mimeType,
        revisedPrompt: undefined
      }]
      : []
  }
}

function normalizedPromptForSubmit(): string {
  const value = prompt.value.trim()
  if (value) return value
  return activeTab.value === 'edit'
    ? '基于参考图生成一张改图，保留主要视觉元素和构图，并提升画面质量。'
    : value
}

async function polishPrompt() {
  if (!prompt.value.trim() || !promptPolishKeyValue.value || polishingPrompt.value) return
  polishingPrompt.value = true
  setSessionError(activeCreationSession.value, '')
  const mode = activeTab.value === 'edit' ? 'edit' : 'generate'
  const originalPrompt = prompt.value.trim()
  try {
    const response = await sendPromptPolishRequest({
      apiKey: promptPolishKeyValue.value,
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
}) {
  if (!selectedKey.value) throw new Error('请选择 API 密钥')
  return createImageStudioJob({
    apiKeyId: selectedKey.value.id,
    mode: 'generate',
    prompt: commonInput.prompt,
    model: commonInput.model,
    size: commonInput.size,
    outputFormat: commonInput.outputFormat,
    quality: commonInput.quality,
    background: commonInput.background,
    style: stringValueOrEmpty(commonInput.advancedParams.style),
    moderation: stringValueOrEmpty(commonInput.advancedParams.moderation),
    outputCompression: numberValueOrUndefined(commonInput.advancedParams.output_compression)
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
}) {
  if (referenceFiles.value.length === 0) throw new Error('请先添加参考图')
  if (!selectedKey.value) throw new Error('请选择 API 密钥')
  const compressedReferences = await compressReferenceImagesForUpload(referenceFiles.value.slice(0, 1))
  const imageDataUrls = await Promise.all(compressedReferences.map((file) => readReferenceFileAsDataUrl(file)))
  const maskDataUrl = maskFile.value ? await readReferenceFileAsDataUrl(maskFile.value) : ''

  return createImageStudioJob({
    apiKeyId: selectedKey.value.id,
    mode: 'edit',
    prompt: commonInput.prompt,
    model: commonInput.model,
    size: commonInput.size,
    outputFormat: commonInput.outputFormat,
    quality: commonInput.quality,
    background: commonInput.background,
    style: stringValueOrEmpty(commonInput.advancedParams.style),
    moderation: stringValueOrEmpty(commonInput.advancedParams.moderation),
    inputFidelity: stringValueOrEmpty(commonInput.advancedParams.input_fidelity),
    outputCompression: numberValueOrUndefined(commonInput.advancedParams.output_compression),
    imageDataUrls,
    maskDataUrl: maskDataUrl || undefined
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

function historyPreviewAspectRatio(record: ImageStudioHistoryRecord) {
  const match = record.size.match(/^(\d+)\s*x\s*(\d+)$/i)
  if (!match) return '1 / 1'
  return `${match[1]} / ${match[2]}`
}

function historyPreviewPlaceholderKicker(record: ImageStudioHistoryRecord) {
  if (record.assetsDeletedAt) return '图片已过期'
  if (record.status === 'queued') return '等待生成'
  if (record.status === 'running') return '正在生成'
  if (record.status === 'failed') return '生成失败'
  return '缩略图不可用'
}

function historyPreviewPlaceholderCopy(record: ImageStudioHistoryRecord) {
  if (record.assetsDeletedAt) return '图片文件已按全站保留策略清理'
  if (record.status === 'queued') return '任务已提交，稍后刷新查看结果'
  if (record.status === 'running') return '后台正在处理，完成后会显示缩略图'
  if (record.status === 'failed') return '可直接重新生成'
  return '原图仍可按需加载'
}

function canRegenerateHistoryRecord(record: ImageStudioHistoryRecord) {
  return record.mode === 'generate' && (record.status === 'failed' || Boolean(record.assetsDeletedAt))
}

async function regenerateHistoryRecord(record: ImageStudioHistoryRecord) {
  if (!canRegenerateHistoryRecord(record) || regeneratingHistoryJobId.value) return
  if (!selectedKey.value) throw new Error('请选择 API 密钥')
  regeneratingHistoryJobId.value = record.id
  try {
    const job = await createImageStudioJob({
      apiKeyId: selectedKey.value.id,
      mode: 'generate',
      prompt: record.prompt,
      model: record.model,
      size: record.size,
      outputFormat: record.outputFormat
    })
    await refreshHistoryRecords(job.id)
  } finally {
    regeneratingHistoryJobId.value = ''
  }
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
  setActiveTab('edit')
  creationSessions.edit.outputs = []
  setSessionError(creationSessions.edit, '')
  try {
    const reference = await historyRecordToReferenceFile(record)
    if (!reference) return
    revokeReferencePreviewUrls()
    referenceFiles.value = [reference.file]
    referencePreviewUrls.value = [reference.previewUrl]
    setPromptValue(record.prompt, 'external')
  } catch (error) {
    setSessionError(creationSessions.edit, error instanceof Error ? error.message : '无法把图片加入参考图')
  }
}

async function deleteHistoryRecord(record: ImageStudioHistoryRecord) {
  await deleteImageStudioJob(record.jobId)
  const nextRecords = historyRecords.value.filter((item) => item.id !== record.id)
  historyRecords.value = nextRecords
  historyTotal.value = Math.max(0, historyTotal.value - 1)
  if (selectedHistoryId.value === record.id) {
    selectedHistoryId.value = nextRecords[0]?.id ?? ''
  }
  if (lightboxImage.value?.kind === 'history' && lightboxImage.value.record.id === record.id) {
    closeLightbox()
  }
  revokeCachedOriginal(record.jobId)
  revokeCachedThumbnail(record.jobId)
  await deleteImageStudioAssetCache(record.jobId)
  await refreshHistoryStats()
}

async function handleHistoryThumbnailError(record: ImageStudioHistoryRecord) {
  if (!record.thumbnailUrl || record.assetsDeletedAt) return
  const current = historyRecords.value.find((item) => item.id === record.id)
  if (!current?.images[0]) return

  revokeCachedThumbnail(record.jobId)

  try {
    const nextSrc = await getOrFetchThumbnailUrl({ id: record.jobId, thumbnailUrl: record.thumbnailUrl })
    if (!nextSrc) return
    historyRecords.value = historyRecords.value.map((item) => {
      if (item.id !== record.id || !item.images[0]) return item
      return {
        ...item,
        images: [
          {
            ...item.images[0],
            src: nextSrc
          }
        ]
      }
    })
  } catch {
    // Keep the broken state so the card still reflects that the thumbnail is unavailable.
  }
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

async function openHistoryLightbox(record: ImageStudioHistoryRecord) {
  const image = await resolveHistoryImage(record)
  if (!image?.src) return
  resetLightboxTransform()
  lightboxImage.value = {
    kind: 'history',
    title: record.prompt || '生成记录',
    src: image.src,
    downloadName: record.assetsDeletedAt ? null : historyDownloadFileName(record),
    canEdit: !record.assetsDeletedAt,
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
  const blob = await imageSourceToBlob(output.src)
  const mimeType = blob.type || output.mimeType || 'image/png'
  return new File([blob], `generated-reference-${index + 1}.${extensionFromMimeType(mimeType)}`, {
    type: mimeType
  })
}

async function historyRecordToReferenceFile(record: ImageStudioHistoryRecord): Promise<{ file: File; previewUrl: string } | null> {
  const image = await resolveHistoryImage(record)
  if (!image) return null
  const originalBlob = await resolveHistoryOriginalBlob(record)
  if (originalBlob) {
    const mimeType = originalBlob.type || image.mimeType || 'image/png'
    return {
      file: new File([originalBlob], `history-reference-${record.jobId}.${extensionFromMimeType(mimeType)}`, { type: mimeType }),
      previewUrl: image.src || getCachedOriginalUrl(record.jobId) || URL.createObjectURL(originalBlob)
    }
  }
  const file = await outputToReferenceFile({
    id: image.id,
    kind: image.src.startsWith('data:') ? 'b64_json' : 'url',
    src: image.src,
    mimeType: image.mimeType,
    revisedPrompt: image.revisedPrompt,
    raw: image
  }, 0)
  return {
    file,
    previewUrl: image.src || createReferencePreviewUrl(file)
  }
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

async function downloadOutputAsFormat(output: ImageStudioOutput, index: number, format: string) {
  const targetFormat = normalizeDownloadFormat(format)
  const fileName = `image-studio-${index + 1}.${extensionFromFormat(targetFormat)}`
  await downloadImageSourceAsFormat(output.src, output.mimeType, targetFormat, fileName)
}

async function downloadHistoryImageAsFormat(record: ImageStudioHistoryRecord, format: string) {
  const targetFormat = normalizeDownloadFormat(format)
  const fileName = `image-studio-history-${record.id}.${extensionFromFormat(targetFormat)}`
  const originalBlob = await resolveHistoryOriginalBlob(record)
  if (originalBlob) {
    await downloadImageBlobAsFormat(originalBlob, originalBlob.type || record.images[0]?.mimeType, targetFormat, fileName)
    return
  }

  const image = await resolveHistoryImage(record)
  if (!image?.src) return
  await downloadImageSourceAsFormat(image.src, image.mimeType, targetFormat, fileName)
}

async function downloadLightboxImageAsFormat(image: ImageStudioLightboxImage, format: string) {
  if (!image.downloadName) return
  const targetFormat = normalizeDownloadFormat(format)
  const sourceMimeType = lightboxImageMimeType(image)
  const baseName = image.downloadName.replace(/\.[^.]+$/, '') || 'image-studio'
  if (image.kind === 'history') {
    const originalBlob = await resolveHistoryOriginalBlob(image.record)
    if (originalBlob) {
      await downloadImageBlobAsFormat(
        originalBlob,
        originalBlob.type || sourceMimeType,
        targetFormat,
        `${baseName}.${extensionFromFormat(targetFormat)}`
      )
      return
    }
  }
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

async function downloadImageBlobAsFormat(
  blob: Blob,
  sourceMimeType: string | undefined,
  targetFormat: string,
  fileName: string
) {
  const targetMimeType = mimeTypeFromOutputFormat(targetFormat)
  const sourceFormat = formatFromMimeType(sourceMimeType)
  const downloadBlob = sourceFormat === targetFormat || !sourceMimeType
    ? blob
    : await renderImageBlobToFormat(blob, targetMimeType, qualityForDownloadFormat(targetFormat))
  const objectUrl = URL.createObjectURL(downloadBlob)
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

async function resolveHistoryImage(record: ImageStudioHistoryRecord) {
  const current = record.images[0]
  if (!current) return null
  if (record.assetsDeletedAt) {
    revokeCachedOriginal(record.jobId)
    return current
  }
  if (record.status !== 'succeeded') return current
  const objectUrl = getCachedOriginalUrl(record.jobId)
  if (objectUrl) {
    if (current.src !== objectUrl) current.src = objectUrl
    return current
  }
  if (!record.originalUrl) return current
  const blob = await getOrFetchOriginalBlob({ id: record.jobId, originalUrl: record.originalUrl, mimeType: current.mimeType } as ImageStudioJob)
  const nextUrl = getCachedOriginalUrl(record.jobId)
  current.src = nextUrl || current.src
  current.mimeType = current.mimeType || blob.type || undefined
  return current
}

async function resolveHistoryOriginalBlob(record: ImageStudioHistoryRecord): Promise<Blob | null> {
  if (record.assetsDeletedAt || record.status !== 'succeeded' || !record.originalUrl) return null
  return getOrFetchOriginalBlob({
    id: record.jobId,
    originalUrl: record.originalUrl,
    mimeType: record.images[0]?.mimeType
  })
}

async function getOrFetchOriginalBlob(job: Pick<ImageStudioJob, 'id' | 'originalUrl' | 'mimeType'>): Promise<Blob> {
  const cached = originalImageCache.get(job.id)
  if (cached) return cached.blob
  const cachedAsset = await getImageStudioAssetCacheSafely(job.id, 'original')
  const blob = cachedAsset?.blob || await fetchImageStudioOriginal(job.id)
  if (!cachedAsset) {
    await putImageStudioAssetCacheSafely({ jobId: job.id, kind: 'original', blob })
  }
  const objectUrl = URL.createObjectURL(blob)
  originalImageCache.set(job.id, { objectUrl, blob })
  return blob
}

function getCachedOriginalUrl(jobId: number): string {
  return originalImageCache.get(jobId)?.objectUrl || ''
}

async function getOrFetchThumbnailUrl(
  job: Pick<ImageStudioJob, 'id' | 'thumbnailUrl'> & Partial<Pick<ImageStudioJob, 'originalUrl' | 'mimeType' | 'status'>>
): Promise<string> {
  const cached = thumbnailImageCache.get(job.id)
  if (cached) return cached
  if (!job.thumbnailUrl) return ''
  try {
    const cachedAsset = await getImageStudioAssetCacheSafely(job.id, 'thumbnail')
    const blob = cachedAsset?.blob || await fetchImageStudioThumbnail(job.id)
    if (!cachedAsset) {
      await putImageStudioAssetCacheSafely({ jobId: job.id, kind: 'thumbnail', blob })
    }
    const objectUrl = URL.createObjectURL(blob)
    thumbnailImageCache.set(job.id, objectUrl)
    return objectUrl
  } catch {
    return ''
  }
}

async function getImageStudioAssetCacheSafely(
  jobId: number,
  kind: 'thumbnail' | 'original'
) {
  try {
    return await getCachedImageStudioAsset(jobId, kind)
  } catch {
    return null
  }
}

async function putImageStudioAssetCacheSafely(input: {
  jobId: number
  kind: 'thumbnail' | 'original'
  blob: Blob
}) {
  try {
    await putImageStudioAssetCache(input)
  } catch {
    // Cache failures must not block rendering images fetched from the backend.
  }
}

function revokeCachedOriginal(jobId: number) {
  const cached = originalImageCache.get(jobId)
  if (!cached) return
  URL.revokeObjectURL(cached.objectUrl)
  originalImageCache.delete(jobId)
}

function revokeCachedThumbnail(jobId: number) {
  const cached = thumbnailImageCache.get(jobId)
  if (!cached) return
  URL.revokeObjectURL(cached)
  thumbnailImageCache.delete(jobId)
}

function revokeOriginalImageCache() {
  for (const jobId of originalImageCache.keys()) {
    revokeCachedOriginal(jobId)
  }
}

function revokeThumbnailImageCache() {
  for (const objectUrl of thumbnailImageCache.values()) {
    URL.revokeObjectURL(objectUrl)
  }
  thumbnailImageCache.clear()
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

async function loadHistoryRecords(): Promise<ImageStudioHistoryRecord[]> {
  try {
    const response = await listImageStudioJobs(1, historyPageSize)
    historyPage.value = 1
    historyTotal.value = response.total
    return Promise.all(response.items.map(mapJobToHistoryRecord))
  } catch {
    historyTotal.value = 0
    return []
  }
}

async function loadMoreHistoryRecords() {
  if (loadingMoreHistory.value || !hasMoreHistory.value) return
  loadingMoreHistory.value = true
  try {
    const nextPage = historyPage.value + 1
    const response = await listImageStudioJobs(nextPage, historyPageSize)
    const nextRecords = await Promise.all(response.items.map(mapJobToHistoryRecord))
    const existingIds = new Set(historyRecords.value.map((record) => record.id))
    historyRecords.value = [
      ...historyRecords.value,
      ...nextRecords.filter((record) => !existingIds.has(record.id))
    ]
    historyPage.value = nextPage
    historyTotal.value = response.total
  } finally {
    loadingMoreHistory.value = false
  }
}

async function refreshDisplayedHistoryRecords() {
  if (refreshingHistory.value) return
  refreshingHistory.value = true
  try {
    const pageCount = Math.max(1, historyPage.value)
    const records: ImageStudioHistoryRecord[] = []
    let total = historyTotal.value
    for (let page = 1; page <= pageCount; page += 1) {
      const response = await listImageStudioJobs(page, historyPageSize)
      total = response.total
      records.push(...await Promise.all(response.items.map(mapJobToHistoryRecord)))
      if (records.length >= response.total || response.items.length === 0) break
    }
    historyRecords.value = records
    historyTotal.value = total
    historyPage.value = Math.max(1, Math.ceil(records.length / historyPageSize))
    if (!historyRecords.value.some((record) => record.id === selectedHistoryId.value)) {
      selectedHistoryId.value = historyRecords.value[0]?.id ?? ''
    }
    await refreshHistoryStats()
  } finally {
    refreshingHistory.value = false
  }
}

async function clearHistoryImageCache() {
  if (cleaningHistoryCache.value) return
  cleaningHistoryCache.value = true
  historyCacheMessage.value = ''
  try {
    const memoryCount = originalImageCache.size + thumbnailImageCache.size
    revokeOriginalImageCache()
    revokeThumbnailImageCache()
    const persistedCount = await clearImageStudioAssetCache()
    const removedCount = Math.max(memoryCount, persistedCount)
    historyCacheMessage.value = removedCount > 0 ? `已清理 ${removedCount} 张缓存图片` : '没有可清理缓存'
  } catch {
    historyCacheMessage.value = '清理失败，请稍后重试'
  } finally {
    cleaningHistoryCache.value = false
  }
}

function stringValueOrEmpty(value: unknown): string | undefined {
  const normalized = typeof value === 'string' ? value.trim() : ''
  return normalized || undefined
}

function numberValueOrUndefined(value: unknown): number | undefined {
  return typeof value === 'number' && Number.isFinite(value) ? value : undefined
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

function historyStatusText(record: ImageStudioHistoryRecord): string {
  if (record.assetsDeletedAt) return '图片已过期清理'
  if (record.status === 'queued' && record.attemptCount > 0) {
    const attemptLabel = record.maxAttempts > 0
      ? `第 ${record.attemptCount + 1}/${record.maxAttempts} 次尝试`
      : `第 ${record.attemptCount + 1} 次尝试`
    return record.nextAttemptAt
      ? `排队重试中 · ${attemptLabel}`
      : `重试排队中 · ${attemptLabel}`
  }
  if (record.status === 'running' && record.attemptCount > 0) {
    const total = record.maxAttempts > 0 ? ` / ${record.maxAttempts}` : ''
    return `重试生成中 · 第 ${record.attemptCount + 1}${total} 次尝试`
  }
  if (record.status === 'queued') return '排队中'
  if (record.status === 'running') return '生成中'
  if (record.status === 'succeeded') return '已完成'
  if (record.status === 'failed') return '失败'
  return record.status
}

function downloadFileName(output: ImageStudioOutput, index: number): string {
  return `image-studio-${index + 1}.${extensionFromMimeType(output.mimeType)}`
}

function historyDownloadFileName(record: ImageStudioHistoryRecord): string {
  return `image-studio-history-${record.id}.${extensionFromMimeType(record.images[0]?.mimeType || record.outputFormat)}`
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
