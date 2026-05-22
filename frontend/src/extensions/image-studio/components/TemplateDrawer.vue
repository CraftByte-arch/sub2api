<template>
  <Transition :name="disableTransition ? 'template-float-none' : 'template-float'" appear>
    <div
      v-if="open"
      class="template-embed-root"
      aria-label="提示词模板"
      @keydown.esc="$emit('close')"
    >
      <div class="template-modal-panel" data-testid="template-drawer" role="dialog" aria-labelledby="template-drawer-title" tabindex="-1">
        <aside class="template-drawer">
          <header class="template-drawer-header">
            <div class="template-header-title">
              <h2 id="template-drawer-title">模板中心</h2>
            </div>
            <div class="template-header-actions">
              <input
                v-model="search"
                class="template-search"
                type="search"
                data-testid="template-search"
                placeholder="搜索模板、分类或关键词"
              >
            </div>
            <div class="template-mode-tabs" role="tablist" aria-label="模板模式">
              <button
                type="button"
                :class="{ active: mode === 'text-to-image' }"
                data-testid="template-mode-generate"
                @click="$emit('update:mode', 'text-to-image')"
              >
                文生图
              </button>
              <button
                type="button"
                :class="{ active: mode === 'image-to-image' }"
                data-testid="template-mode-edit"
                @click="$emit('update:mode', 'image-to-image')"
              >
                图生图
              </button>
            </div>
            <button type="button" class="template-close-button" aria-label="关闭模板中心" @click="$emit('close')">关闭</button>
          </header>

          <div class="template-filter-rail template-category-tabs" role="tablist" aria-label="模板分类">
            <button
              type="button"
              :class="{ active: selectedCategory === '' }"
              @click="selectedCategory = ''"
            >
              全部
            </button>
            <button
              v-for="category in visibleCategories"
              :key="category"
              type="button"
              :class="{ active: selectedCategory === category }"
              @click="selectedCategory = category"
            >
              {{ category }}
            </button>
          </div>

          <div class="template-compat-category-groups" aria-hidden="true">
            <section class="template-category-group">
              <div class="template-category-tabs">
                <button
                  v-for="category in commonCategories"
                  :key="category"
                  type="button"
                  :tabindex="-1"
                  :class="{ active: selectedCategory === category }"
                  @click="selectedCategory = category"
                >
                  {{ category }}
                </button>
              </div>
            </section>
            <section class="template-category-group advanced">
              <button
                type="button"
                class="template-advanced-toggle"
                data-testid="template-advanced-toggle"
                :tabindex="-1"
                @click="advancedExpanded = !advancedExpanded"
              >
                高级模板
              </button>
              <div class="template-category-tabs">
                <button
                  v-for="category in advancedCategories"
                  :key="category"
                  type="button"
                  :tabindex="-1"
                  :class="{ active: selectedCategory === category }"
                  @click="selectedCategory = category"
                >
                  {{ category }}
                </button>
              </div>
            </section>
          </div>

          <div class="template-drawer-body">
            <section class="template-card-column">
              <div class="template-list-head">
                <div>
                  <h3>模板列表</h3>
                  <p>{{ selectedCategory || '全部分类' }}</p>
                </div>
                <span>{{ filteredTemplates.length }} 个</span>
              </div>

              <div class="template-card-list">
                <div v-if="filteredTemplates.length === 0" class="template-empty-state">
                  <h3>{{ search.trim() ? '没有匹配模板' : '当前分类暂无模板' }}</h3>
                  <p>{{ search.trim() ? '试试更短一点的关键词，或者切换到另一个模式。' : '试试切换分类，或使用搜索快速定位模板。' }}</p>
                </div>
                <button
                  v-for="template in filteredTemplates"
                  :key="template.id"
                  type="button"
                  class="template-card"
                  :class="{ active: selectedTemplate?.id === template.id, advanced: template.section === 'advanced' }"
                  data-testid="template-card"
                  @click="selectTemplate(template.id)"
                >
                  <div class="template-card-top">
                    <small>{{ template.recommendedRatios?.[0] || 'Auto' }}</small>
                  </div>
                  <h3>{{ template.title }}</h3>
                  <p>{{ template.description }}</p>
                  <div class="template-card-meta">
                    <strong>{{ template.badge || template.category }}</strong>
                    <span v-if="template.requiresReference">需参考图</span>
                    <span v-else-if="template.section === 'advanced'">高级</span>
                  </div>
                </button>
              </div>
            </section>

            <section class="template-right-column">
              <section class="template-editor-column">
                <div v-if="!selectedTemplate" class="template-empty-state editor">
                  <h3>{{ search.trim() ? '没有可编辑的模板' : '当前分类暂无模板' }}</h3>
                  <p>{{ search.trim() ? '调整关键词后，模板详情会在这里出现。' : '切换到其他模板分类，或展开高级模板继续查看。' }}</p>
                </div>

                <div v-else class="template-editor-content">
                  <section class="template-identity-card">
                    <div class="template-identity-main">
                      <div class="template-editor-summary">
                        <div class="template-editor-title-row">
                          <h3>{{ selectedTemplate.title }}</h3>
                          <div class="template-identity-meta">
                            <span>{{ selectedTemplate.category }}</span>
                            <span>{{ selectedTemplate.recommendedRatios?.[0] || 'Auto' }}</span>
                            <span>{{ selectedTemplate.recommendedModel || 'Images API' }}</span>
                            <span v-if="selectedTemplate.section === 'advanced'">高级模板</span>
                            <span v-if="selectedTemplate.requiresReference">需参考图</span>
                          </div>
                        </div>
                        <p>{{ selectedTemplate.description }}</p>
                      </div>
                      <div class="template-editor-header-actions">
                        <div class="template-action-cluster">
                          <button
                            type="button"
                            class="template-ghost-button"
                            data-testid="template-history-button"
                            :disabled="currentTemplateHistory.length === 0"
                            @click.stop="toggleTemplateHistory"
                          >
                            最近填写
                          </button>
                          <button type="button" class="template-ghost-button" @click="resetFields">重置字段</button>
                        </div>
                        <div
                          v-if="templateHistoryOpen"
                          class="template-history-popover"
                          data-testid="template-history-popover"
                          @pointerdown.stop
                        >
                          <div class="template-history-header">
                            <span>最近填写</span>
                            <button type="button" @click="clearCurrentTemplateHistory">清空</button>
                          </div>
                          <div v-if="currentTemplateHistory.length === 0" class="template-history-empty">
                            这个模板还没有填写记录。
                          </div>
                          <div v-else class="template-history-list">
                            <article
                              v-for="record in currentTemplateHistory"
                              :key="record.id"
                              class="template-history-item"
                            >
                              <div>
                                <strong>{{ formatHistoryTime(record.createdAt) }}</strong>
                                <p>{{ record.summary }}</p>
                              </div>
                              <div class="template-history-actions">
                                <button type="button" @click="applyTemplateHistoryRecord(record.id)">回填</button>
                                <button type="button" @click="deleteTemplateHistoryRecord(record.id)">删除</button>
                              </div>
                            </article>
                          </div>
                        </div>
                      </div>
                    </div>

                  </section>

                  <div class="template-parameter-scroll">
                    <section class="template-form-section template-core-section">
                      <div class="template-section-head">
                        <div>
                          <h4>核心字段</h4>
                          <p>先填最影响画面结果的 4 个变量。</p>
                        </div>
                      </div>
                      <div class="template-form-grid">
                        <label
                          v-for="field in coreFields"
                          :key="field.key"
                          class="template-field"
                          :class="{ wide: field.type === 'textarea' }"
                        >
                          <span>{{ field.label }}</span>
                          <StudioSelect
                            v-if="field.type === 'select'"
                            v-model="fieldValues[field.key]"
                            :options="field.options || []"
                            placeholder="请选择"
                            button-class="template-input"
                            @change="engageSelectedTemplate"
                          />
                          <StudioSelect
                            v-else-if="field.type === 'ratio'"
                            v-model="fieldValues[field.key]"
                            :options="ratioSelectOptions"
                            placeholder="请选择"
                            button-class="template-input"
                            @change="engageSelectedTemplate"
                          />
                          <textarea
                            v-else-if="field.type === 'textarea'"
                            v-model="fieldValues[field.key]"
                            class="template-input template-textarea"
                            :placeholder="field.placeholder || ''"
                            @input="engageSelectedTemplate"
                          ></textarea>
                          <input
                            v-else
                            v-model="fieldValues[field.key]"
                            class="template-input"
                            type="text"
                            :placeholder="field.placeholder || ''"
                            @input="engageSelectedTemplate"
                          />
                          <small v-if="field.helpText">{{ field.helpText }}</small>
                        </label>
                      </div>
                    </section>

                    <details v-if="extraFields.length > 0" class="template-advanced">
                      <summary>
                        <span>高级字段</span>
                        <small>{{ extraFields.length }} 项，可选填写</small>
                      </summary>
                      <div class="template-form-grid">
                        <label
                          v-for="field in extraFields"
                          :key="field.key"
                          class="template-field"
                          :class="{ wide: field.type === 'textarea' }"
                        >
                          <span>{{ field.label }}</span>
                          <StudioSelect
                            v-if="field.type === 'select'"
                            v-model="fieldValues[field.key]"
                            :options="field.options || []"
                            placeholder="请选择"
                            button-class="template-input"
                            @change="engageSelectedTemplate"
                          />
                          <StudioSelect
                            v-else-if="field.type === 'ratio'"
                            v-model="fieldValues[field.key]"
                            :options="ratioSelectOptions"
                            placeholder="请选择"
                            button-class="template-input"
                            @change="engageSelectedTemplate"
                          />
                          <textarea
                            v-else-if="field.type === 'textarea'"
                            v-model="fieldValues[field.key]"
                            class="template-input template-textarea"
                            :placeholder="field.placeholder || ''"
                            @input="engageSelectedTemplate"
                          ></textarea>
                          <input
                            v-else
                            v-model="fieldValues[field.key]"
                            class="template-input"
                            type="text"
                            :placeholder="field.placeholder || ''"
                            @input="engageSelectedTemplate"
                          />
                        </label>
                      </div>
                    </details>
                  </div>
                </div>
              </section>

            </section>
          </div>
        </aside>
      </div>
    </div>
  </Transition>
</template>

<script setup lang="ts">
import { computed, ref, watch } from 'vue'
import StudioSelect from './StudioSelect.vue'
import { imagePromptTemplates } from '../templateRegistry'
import { createTemplateDefaultValues, renderTemplatePrompt } from '../templatePrompt'
import type { ImagePromptTemplate, TemplateDraftPayload, TemplateMode, TemplateSyncState } from '../templateTypes'

const props = defineProps<{
  open: boolean
  mode: TemplateMode
  hasReferenceImage: boolean
  syncState: TemplateSyncState
  activeTemplateId?: string
  disableTransition?: boolean
}>()

const emit = defineEmits<{
  'update:mode': [mode: TemplateMode]
  'draft-change': [payload: TemplateDraftPayload]
  'resume-sync': []
  close: []
}>()

const ratioOptions = ['1:1', '16:9', '9:16', '21:9', '4:3', '3:4', '3:2', '2:3', '5:4', '4:5']
const ratioSelectOptions = ratioOptions.map((ratio) => ({ value: ratio, label: ratio }))
const templateHistoryStorageKey = 'sub2api:image-studio:template-history:v1'
const templateSelectionStorageKey = 'sub2api:image-studio:template-selection:v1'

const search = ref('')
const selectedCategory = ref('')
const selectedTemplateId = ref('')
const fieldValues = ref<Record<string, string>>({})
const templateHistoryOpen = ref(false)
const advancedExpanded = ref(false)
const templateHistoryRecords = ref<TemplateHistoryRecord[]>([])
const selectionMemory = ref<Record<string, string>>({})
const draftMemory = ref<Record<string, TemplateDraftMemory>>({})
const engagedTemplateId = ref('')

interface TemplateHistoryRecord {
  id: string
  templateId: string
  mode: TemplateMode
  category: string
  createdAt: string
  values: Record<string, string>
  summary: string
}

interface TemplateDraftMemory {
  templateId: string
  values: Record<string, string>
}

const modeTemplates = computed(() =>
  props.mode === 'image-to-image'
    ? imagePromptTemplates
    : imagePromptTemplates.filter((item) => item.mode === props.mode)
)
const commonCategories = computed(() => uniqueCategoriesBySection('common'))
const advancedCategories = computed(() => uniqueCategoriesBySection('advanced'))
const visibleCategories = computed(() => [...commonCategories.value, ...advancedCategories.value])
const filteredTemplates = computed(() => {
  const query = search.value.trim().toLowerCase()
  return modeTemplates.value.filter((item) => {
    if (selectedCategory.value && item.category !== selectedCategory.value) return false
    if (!query) return true
    return [item.title, item.description, item.category, ...item.tags]
      .join(' ')
      .toLowerCase()
      .includes(query)
  })
})
const selectedTemplate = computed(() =>
  filteredTemplates.value.find((item) => item.id === selectedTemplateId.value)
  ?? modeTemplates.value.find((item) => item.id === selectedTemplateId.value)
  ?? filteredTemplates.value[0]
  ?? modeTemplates.value[0]
  ?? null
)
const basicFields = computed(() => selectedTemplate.value?.fields.filter((field) => field.section !== 'advanced') ?? [])
const coreFields = computed(() => basicFields.value.slice(0, 4))
const extraFields = computed(() => [
  ...basicFields.value.slice(4),
  ...(selectedTemplate.value?.fields.filter((field) => field.section === 'advanced') ?? [])
])
const renderedPrompt = computed(() => renderTemplatePrompt(selectedTemplate.value, fieldValues.value))
const finalPrompt = computed(() => renderedPrompt.value.trim())
const currentTemplateHistory = computed(() =>
  templateHistoryRecords.value.filter((record) => record.templateId === selectedTemplate.value?.id).slice(0, 5)
)

watch(
  () => props.mode,
  () => {
    search.value = ''
    templateHistoryOpen.value = false
    ensureValidCategory()
    syncTemplateForCategory()
  },
  { immediate: true }
)

watch(
  fieldValues,
  () => {
    saveCurrentTemplateDraft()
  },
  { deep: true }
)

watch(
  selectedCategory,
  (category) => {
    if (advancedCategories.value.includes(category)) {
      advancedExpanded.value = true
    }
    syncTemplateForCategory()
    templateHistoryOpen.value = false
  }
)

watch(
  [commonCategories, advancedCategories],
  () => {
    ensureValidCategory()
  },
  { immediate: true }
)

watch(
  selectedTemplate,
  (template) => {
    if (template) {
      selectedTemplateId.value = template.id
      rememberTemplateSelection(template)
      restoreTemplateDraftOrDefault(template)
      if (template.section === 'advanced') {
        advancedExpanded.value = true
      }
      if (props.activeTemplateId === template.id) {
        engagedTemplateId.value = template.id
      } else if (engagedTemplateId.value && engagedTemplateId.value !== template.id) {
        engagedTemplateId.value = ''
      }
    } else {
      fieldValues.value = {}
    }
    templateHistoryOpen.value = false
  },
  { immediate: true }
)

watch(
  () => props.open,
  (open) => {
    if (!open) {
      saveEngagedTemplateUsage()
      templateHistoryOpen.value = false
      return
    }
    templateHistoryRecords.value = loadTemplateHistoryRecords()
    selectionMemory.value = loadSelectionMemory()
    ensureValidCategory()
    syncTemplateForCategory()
  },
  { immediate: true }
)

watch(
  () => props.activeTemplateId,
  (id) => {
    if (!id) return
    selectedTemplateId.value = id
    engagedTemplateId.value = id
  },
  { immediate: true }
)

watch(
  [selectedTemplate, finalPrompt, () => props.open],
  ([template, draftPrompt, open]) => {
    if (!open || !template || !draftPrompt || engagedTemplateId.value !== template.id) return
    emit('draft-change', {
      prompt: draftPrompt,
      templateId: template.id,
      mode: props.mode,
      recommendedRatio: template.recommendedRatios?.[0]
    })
  },
  { immediate: true }
)

function uniqueCategoriesBySection(section: 'common' | 'advanced') {
  return [...new Set(modeTemplates.value.filter((item) => item.section === section).map((item) => item.category))]
}

function ensureValidCategory() {
  if (!selectedCategory.value || visibleCategories.value.includes(selectedCategory.value)) return
  selectedCategory.value = ''
}

function selectTemplate(id: string) {
      if (engagedTemplateId.value && engagedTemplateId.value !== id) {
        saveEngagedTemplateUsage()
      }
  selectedTemplateId.value = id
  engagedTemplateId.value = id
  if (selectedTemplate.value?.id === id && renderedPrompt.value) {
    emit('draft-change', {
      prompt: renderedPrompt.value,
      templateId: id,
      mode: props.mode,
      recommendedRatio: selectedTemplate.value.recommendedRatios?.[0]
    })
  }
}

function resetFields() {
  if (!selectedTemplate.value) return
  fieldValues.value = createTemplateDefaultValues(selectedTemplate.value)
  engageSelectedTemplate()
}

function syncTemplateForCategory() {
  const templates = filteredTemplates.value
  if (!templates.length) {
    selectedTemplateId.value = ''
    fieldValues.value = {}
    return
  }
  selectedTemplateId.value = resolveTemplateForCurrentContext(templates).id
}

function rememberTemplateSelection(template: Pick<ImagePromptTemplate, 'id' | 'mode' | 'category'>) {
  const nextMemory = {
    ...selectionMemory.value,
    [selectionMemoryKey(props.mode, template.category)]: template.id
  }
  selectionMemory.value = nextMemory
  saveSelectionMemory(nextMemory)
}

function restoreTemplateDraftOrDefault(template: ImagePromptTemplate) {
  const rememberedDraft = findRememberedDraft(template.id)
  const latest = templateHistoryRecords.value.find((record) => record.templateId === template.id)
  fieldValues.value = rememberedDraft
    ? { ...rememberedDraft.values }
    : latest
      ? { ...latest.values }
      : createTemplateDefaultValues(template)
}

function resolveTemplateForCurrentContext(templates: ImagePromptTemplate[]) {
  const activeTemplate = props.activeTemplateId
    ? templates.find((item) => item.id === props.activeTemplateId)
    : undefined
  if (activeTemplate) return activeTemplate

  const rememberedDraft = draftMemory.value[templateContextKey(props.mode, selectedCategory.value)]
  const draftTemplate = rememberedDraft
    ? templates.find((item) => item.id === rememberedDraft.templateId)
    : undefined
  if (draftTemplate) return draftTemplate

  const rememberedId = selectionMemory.value[selectionMemoryKey(props.mode, selectedCategory.value)]
  return templates.find((item) => item.id === rememberedId) ?? templates[0]
}

function saveCurrentTemplateDraft() {
  const template = selectedTemplate.value
  if (!template) return
  draftMemory.value = {
    ...draftMemory.value,
    [templateContextKey(props.mode, selectedCategory.value)]: {
      templateId: template.id,
      values: { ...fieldValues.value }
    }
  }
}

function findRememberedDraft(templateId: string) {
  const currentContextDraft = draftMemory.value[templateContextKey(props.mode, selectedCategory.value)]
  if (currentContextDraft?.templateId === templateId) return currentContextDraft
  return Object.values(draftMemory.value).find((draft) => draft.templateId === templateId)
}

function toggleTemplateHistory() {
  if (currentTemplateHistory.value.length === 0) return
  templateHistoryOpen.value = !templateHistoryOpen.value
}

function applyTemplateHistoryRecord(id: string) {
  const record = templateHistoryRecords.value.find((item) => item.id === id)
  if (!record) return
  fieldValues.value = { ...record.values }
  engagedTemplateId.value = record.templateId
  templateHistoryOpen.value = false
}

function deleteTemplateHistoryRecord(id: string) {
  const nextRecords = templateHistoryRecords.value.filter((item) => item.id !== id)
  templateHistoryRecords.value = nextRecords
  saveTemplateHistoryRecords(nextRecords)
}

function clearCurrentTemplateHistory() {
  if (!selectedTemplate.value) return
  const nextRecords = templateHistoryRecords.value.filter((item) => item.templateId !== selectedTemplate.value?.id)
  templateHistoryRecords.value = nextRecords
  saveTemplateHistoryRecords(nextRecords)
  templateHistoryOpen.value = false
}

function saveTemplateUsage(
  template: Pick<ImagePromptTemplate, 'id' | 'mode' | 'category' | 'fields'>,
  values: Record<string, string>
) {
  const nextRecord: TemplateHistoryRecord = {
    id: `${Date.now()}-${Math.random().toString(36).slice(2, 8)}`,
    templateId: template.id,
    mode: template.mode,
    category: template.category,
    createdAt: new Date().toISOString(),
    values: { ...values },
    summary: buildTemplateHistorySummary(template.fields, values)
  }
  const nextRecords = [
    nextRecord,
    ...templateHistoryRecords.value.filter((record) =>
      !(record.templateId === template.id && shallowEqualRecordValues(record.values, values))
    )
  ].slice(0, 50)
  templateHistoryRecords.value = nextRecords
  saveTemplateHistoryRecords(nextRecords)
}

function engageSelectedTemplate() {
  if (!selectedTemplate.value) return
  engagedTemplateId.value = selectedTemplate.value.id
}

function saveEngagedTemplateUsage() {
  if (!selectedTemplate.value || !finalPrompt.value || engagedTemplateId.value !== selectedTemplate.value.id) return
  saveTemplateUsage(selectedTemplate.value, fieldValues.value)
}

function buildTemplateHistorySummary(
  fields: Array<{ key: string; section?: 'basic' | 'advanced' }>,
  values: Record<string, string>
) {
  const parts = fields
    .filter((field) => field.section !== 'advanced')
    .map((field) => (values[field.key] || '').trim())
    .filter(Boolean)
    .slice(0, 3)
    .map((item) => (item.length > 16 ? `${item.slice(0, 16)}…` : item))
  return parts.join(' / ') || '未命名模板填写'
}

function shallowEqualRecordValues(left: Record<string, string>, right: Record<string, string>) {
  const leftKeys = Object.keys(left)
  const rightKeys = Object.keys(right)
  if (leftKeys.length !== rightKeys.length) return false
  return leftKeys.every((key) => (left[key] || '') === (right[key] || ''))
}

function saveTemplateHistoryRecords(records: TemplateHistoryRecord[]) {
  if (typeof window === 'undefined') return
  window.localStorage.setItem(templateHistoryStorageKey, JSON.stringify(records))
}

function loadTemplateHistoryRecords(): TemplateHistoryRecord[] {
  if (typeof window === 'undefined') return []
  try {
    const parsed = JSON.parse(window.localStorage.getItem(templateHistoryStorageKey) || '[]')
    if (!Array.isArray(parsed)) return []
    return parsed.filter((item) =>
      item
      && typeof item === 'object'
      && typeof item.id === 'string'
      && typeof item.templateId === 'string'
      && typeof item.createdAt === 'string'
      && item.values
      && typeof item.values === 'object'
    )
  } catch {
    return []
  }
}

function saveSelectionMemory(memory: Record<string, string>) {
  if (typeof window === 'undefined') return
  window.localStorage.setItem(templateSelectionStorageKey, JSON.stringify(memory))
}

function loadSelectionMemory(): Record<string, string> {
  if (typeof window === 'undefined') return {}
  try {
    const parsed = JSON.parse(window.localStorage.getItem(templateSelectionStorageKey) || '{}')
    if (!parsed || typeof parsed !== 'object' || Array.isArray(parsed)) return {}
    return Object.entries(parsed).reduce<Record<string, string>>((acc, [key, value]) => {
      if (typeof key === 'string' && typeof value === 'string') acc[key] = value
      return acc
    }, {})
  } catch {
    return {}
  }
}

function selectionMemoryKey(mode: TemplateMode, category: string) {
  return `${mode}:${category}`
}

function templateContextKey(mode: TemplateMode, category: string) {
  return `${mode}:${category || '__all__'}`
}

function formatHistoryTime(value: string) {
  const date = new Date(value)
  if (Number.isNaN(date.getTime())) return ''
  return date.toLocaleString('zh-CN', {
    month: '2-digit',
    day: '2-digit',
    hour: '2-digit',
    minute: '2-digit'
  })
}
</script>

<style scoped src="./TemplateDrawer.css"></style>
