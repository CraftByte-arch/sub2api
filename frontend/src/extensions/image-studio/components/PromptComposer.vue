<template>
  <section class="prompt-section" aria-label="提示词">
    <div class="prompt-main">
      <div class="prompt-head">
        <label class="control-label prompt-title-label" for="studio-prompt">
          提示词
          <span class="prompt-char-count">{{ prompt.length }} 字符</span>
        </label>
        <div class="prompt-head-actions">
          <button
            v-if="activeTab !== 'history'"
            type="button"
            class="composer-tool-button template-trigger-button"
            :class="{ active: templateDrawerOpen }"
            data-testid="template-button"
            @click="$emit('toggle-template')"
          >
            <Icon name="sparkles" size="xs" />
            模板
          </button>
          <span v-if="activeTab !== 'history'" class="prompt-polish-combo">
            <button
              type="button"
              class="composer-tool-button prompt-polish-action"
              data-testid="polish-prompt-button"
              :disabled="polishingPrompt || !prompt.trim() || promptPolishDisabled"
              @click="$emit('polish-prompt')"
            >
              <Icon name="sparkles" size="xs" />
              {{ polishingPrompt ? '润色中' : '润色' }}
            </button>
            <StudioSelect
              :model-value="promptPolishModel"
              :options="promptPolishModelOptions"
              button-class="composer-tool-button prompt-polish-model-select"
              placement="top"
              aria-label="润色模型"
              data-testid="prompt-polish-model-select"
              @update:model-value="$emit('update:prompt-polish-model', $event)"
            />
            <StudioSelect
              :model-value="promptPolishKey"
              :options="promptPolishKeyOptions"
              placeholder="选择密钥"
              placeholder-disabled
              :disabled="polishingPrompt || promptPolishDisabled"
              button-class="composer-tool-button prompt-polish-key-select"
              placement="top"
              aria-label="润色密钥"
              data-testid="prompt-polish-key-select"
              @update:model-value="$emit('update:prompt-polish-key', $event)"
            />
          </span>
          <button
            v-if="activeTab !== 'history'"
            type="button"
            class="composer-tool-button"
            data-testid="prompt-clear-button"
            :disabled="polishingPrompt || !prompt"
            @click="$emit('clear-prompt')"
          >
            清空
          </button>
          <span
            v-if="activeTab !== 'history'"
            class="prompt-history-anchor"
            :class="{ open: promptHistoryOpen }"
          >
            <button
              type="button"
              class="composer-tool-button"
              data-testid="prompt-history-button"
              :aria-expanded="promptHistoryOpen"
              @click.stop="$emit('toggle-prompt-history')"
            >
              <Icon name="clock" size="xs" />
              历史
            </button>
            <div v-if="promptHistoryOpen" class="prompt-history-popover" data-testid="prompt-history-popover" @pointerdown.stop>
              <div class="prompt-history-toolbar">
                <div class="prompt-history-tabs" role="tablist" aria-label="提示词历史模式">
                  <button
                    type="button"
                    :class="{ active: promptHistoryMode === 'generate' }"
                    @click="$emit('update:prompt-history-mode', 'generate')"
                  >
                    文生图
                  </button>
                  <button
                    type="button"
                    :class="{ active: promptHistoryMode === 'edit' }"
                    @click="$emit('update:prompt-history-mode', 'edit')"
                  >
                    图生图
                  </button>
                </div>
                <button
                  type="button"
                  class="prompt-history-clear"
                  data-testid="prompt-history-clear-button"
                  :disabled="filteredPromptHistory.length === 0"
                  @click="$emit('clear-prompt-history')"
                >
                  清空
                </button>
                <button
                  type="button"
                  class="prompt-history-close"
                  data-testid="prompt-history-close-button"
                  @click="$emit('close-prompt-history')"
                >
                  关闭
                </button>
              </div>
              <div class="prompt-history-search">
                <input
                  :value="promptHistorySearch"
                  type="search"
                  data-testid="prompt-history-search"
                  placeholder="搜索提示词历史"
                  @input="$emit('update:prompt-history-search', ($event.target as HTMLInputElement).value)"
                >
              </div>
              <div v-if="filteredPromptHistory.length === 0" class="prompt-history-empty">
                {{ promptHistorySearch.trim() ? '没有匹配的提示词历史' : '还没有提示词历史' }}
              </div>
              <div v-else class="prompt-history-list">
                <article v-for="item in filteredPromptHistory" :key="item.id" class="prompt-history-item">
                  <div>
                    <p>{{ item.prompt }}</p>
                    <span>{{ item.mode === 'edit' ? '图生图' : '文生图' }}</span>
                    <strong v-if="item.source === 'polished'">润色</strong>
                  </div>
                  <div class="prompt-history-actions">
                    <button type="button" @click="$emit('append-prompt-history', item.prompt)">追加</button>
                    <button type="button" @click="$emit('request-prompt-overwrite', item.id)">覆盖</button>
                    <button
                      type="button"
                      class="prompt-history-delete"
                      data-testid="prompt-history-delete-button"
                      @click="$emit('delete-prompt-history', item.id)"
                    >
                      删除
                    </button>
                    <div v-if="pendingOverwritePromptId === item.id" class="overwrite-confirm-tip">
                      <span>当前输入框已有内容，确定覆盖？</span>
                      <button type="button" @click="$emit('overwrite-prompt', item.prompt)">确认</button>
                      <button type="button" @click="$emit('cancel-prompt-overwrite')">取消</button>
                    </div>
                  </div>
                </article>
              </div>
            </div>
          </span>
          <button
            type="button"
            class="composer-size-button"
            :disabled="activeTab === 'history'"
            :aria-pressed="composerExpanded"
            @click="$emit('update:composer-expanded', !composerExpanded)"
          >
            <Icon :name="composerExpanded ? 'arrowDown' : 'arrowUp'" size="xs" />
            {{ composerExpanded ? '收起' : '展开' }}
          </button>
        </div>
      </div>
      <div
        class="composer-row"
        :class="{ 'transfer-origin-active': transferOriginActive }"
        data-testid="composer-transfer-anchor"
      >
        <textarea
          id="studio-prompt"
          :value="prompt"
          class="prompt-input"
          data-testid="prompt-input"
          :disabled="activeTab === 'history' || polishingPrompt"
          placeholder="描述画面的主体、风格、光线、构图... 越具体效果越好"
          @input="$emit('prompt-input', $event)"
        ></textarea>
        <div class="composer-actions">
          <section class="estimate-card" aria-label="预估费用">
            <span>预估费用</span>
            <strong>{{ estimatedCost }}</strong>
          </section>
          <button
            v-if="activeTab !== 'history'"
            type="button"
            class="generate-button"
            data-testid="generate-button"
            :disabled="submitDisabled"
            @click="$emit('submit')"
          >
            <Icon name="sparkles" size="sm" />
            {{ submitButtonLabel }}
          </button>
        </div>
      </div>
      <div class="prompt-meta-row">
        <div class="prompt-examples" aria-label="提示词示例">
          <button
            v-for="example in promptExamples"
            :key="example"
            type="button"
            class="prompt-chip"
            data-testid="prompt-example"
            :disabled="polishingPrompt"
            @click="$emit('apply-example', example)"
          >
            {{ example }}
          </button>
        </div>
      </div>
    </div>
  </section>
</template>

<script setup lang="ts">
import Icon from '@/components/icons/Icon.vue'
import { computed } from 'vue'
import StudioSelect from './StudioSelect.vue'
import type { ImageStudioMode, ImageStudioPromptHistoryRecord, ImageStudioSelectOption } from '../types'

const props = defineProps<{
  activeTab: ImageStudioMode
  prompt: string
  polishingPrompt: boolean
  templateDrawerOpen: boolean
  promptHistoryOpen: boolean
  promptHistoryMode: Exclude<ImageStudioMode, 'history'>
  promptHistorySearch: string
  filteredPromptHistory: ImageStudioPromptHistoryRecord[]
  pendingOverwritePromptId: string
  composerExpanded: boolean
  estimatedCost: string
  submitDisabled: boolean
  submitting: boolean
  transferOriginActive?: boolean
  promptExamples: string[]
  promptPolishModel: string
  promptPolishModelOptions: ImageStudioSelectOption[]
  promptPolishKey: string
  promptPolishKeyOptions: ImageStudioSelectOption[]
  promptPolishDisabled: boolean
}>()

defineEmits<{
  'toggle-template': []
  'polish-prompt': []
  'clear-prompt': []
  'toggle-prompt-history': []
  'update:prompt-history-mode': [mode: Exclude<ImageStudioMode, 'history'>]
  'update:prompt-history-search': [value: string]
  'update:prompt-polish-model': [value: string]
  'update:prompt-polish-key': [value: string]
  'clear-prompt-history': []
  'close-prompt-history': []
  'append-prompt-history': [prompt: string]
  'request-prompt-overwrite': [id: string]
  'delete-prompt-history': [id: string]
  'overwrite-prompt': [prompt: string]
  'cancel-prompt-overwrite': []
  'update:composer-expanded': [expanded: boolean]
  'prompt-input': [event: Event]
  submit: []
  'apply-example': [example: string]
}>()

const submitButtonLabel = computed(() => {
  if (props.submitting) return '生成中...'
  if (props.activeTab === 'edit') return '生成改图'
  return '生成图片'
})
</script>
