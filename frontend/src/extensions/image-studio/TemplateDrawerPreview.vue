<template>
  <main class="template-preview-page">
    <section class="template-preview-toolbar">
      <div>
        <strong>TemplateDrawer 样式预览</strong>
        <span>直接调 CSS，不依赖后端和生成流程。</span>
      </div>
      <div class="template-preview-actions">
        <button type="button" :class="{ active: mode === 'text-to-image' }" @click="mode = 'text-to-image'">文生图</button>
        <button type="button" :class="{ active: mode === 'image-to-image' }" @click="mode = 'image-to-image'">图生图</button>
        <button type="button" @click="toggleReference">{{ hasReferenceImage ? '清空参考图' : '模拟参考图' }}</button>
        <button type="button" @click="syncState = syncState === 'linked' ? 'detached' : 'linked'">
          {{ syncState === 'linked' ? '模拟脱离同步' : '模拟连接同步' }}
        </button>
      </div>
    </section>

    <TemplateDrawer
      open
      :mode="mode"
      :has-reference-image="hasReferenceImage"
      :sync-state="syncState"
      @update:mode="mode = $event"
      @draft-change="latestPrompt = $event.prompt"
      @resume-sync="syncState = 'linked'"
      @apply-and-submit="latestPrompt = latestPrompt || '预览提示词'"
      @close="noop"
    >
      <template #reference-panel>
        <ReferenceStrip
          :files="referenceFiles"
          :preview-urls="referencePreviewUrls"
          :max-files="4"
          variant="floating"
          interaction-mode="static"
          @change="toggleReference"
          @remove="hasReferenceImage = false"
          @preview-error="noop"
          @open-lightbox="noop"
        />
      </template>
    </TemplateDrawer>
  </main>
</template>

<script setup lang="ts">
import { computed, ref } from 'vue'
import TemplateDrawer from './components/TemplateDrawer.vue'
import ReferenceStrip from './components/ReferenceStrip.vue'
import type { TemplateMode, TemplateSyncState } from './templateTypes'

const mode = ref<TemplateMode>('text-to-image')
const syncState = ref<TemplateSyncState>('linked')
const hasReferenceImage = ref(false)
const latestPrompt = ref('')
const referenceFiles = computed(() => (
  hasReferenceImage.value
    ? [new File(['preview'], 'reference-preview.png', { type: 'image/png' })]
    : []
))
const referencePreviewUrls = computed(() => (
  hasReferenceImage.value
    ? ['data:image/svg+xml,%3Csvg xmlns=%22http://www.w3.org/2000/svg%22 width=%22160%22 height=%22100%22 viewBox=%220 0 160 100%22%3E%3Cdefs%3E%3ClinearGradient id=%22g%22 x1=%220%22 x2=%221%22 y1=%220%22 y2=%221%22%3E%3Cstop stop-color=%22%2399f6e4%22/%3E%3Cstop offset=%221%22 stop-color=%22%23bfdbfe%22/%3E%3C/linearGradient%3E%3C/defs%3E%3Crect width=%22160%22 height=%22100%22 rx=%2218%22 fill=%22url(%23g)%22/%3E%3Ctext x=%2280%22 y=%2256%22 text-anchor=%22middle%22 font-family=%22Arial%22 font-size=%2220%22 font-weight=%22700%22 fill=%22%230f766e%22%3EREF%3C/text%3E%3C/svg%3E']
    : []
))

function toggleReference() {
  mode.value = 'image-to-image'
  hasReferenceImage.value = !hasReferenceImage.value
}

function noop() {}
</script>

<style src="./ImageStudioView.css"></style>

<style scoped>
.template-preview-page {
  min-height: 100dvh;
  background:
    radial-gradient(circle at top left, rgba(20, 184, 166, 0.18), transparent 34rem),
    linear-gradient(135deg, #eef6f5, #f8fafc 45%, #edf7ff);
}

.template-preview-toolbar {
  position: fixed;
  top: 16px;
  left: 50%;
  z-index: 1200;
  display: flex;
  width: min(1120px, calc(100vw - 32px));
  border: 1px solid rgba(15, 23, 42, 0.12);
  border-radius: 16px;
  padding: 12px 14px;
  justify-content: space-between;
  align-items: center;
  gap: 16px;
  background: rgba(255, 255, 255, 0.88);
  box-shadow: 0 18px 50px rgba(15, 23, 42, 0.16);
  transform: translateX(-50%);
  backdrop-filter: blur(16px);
}

.template-preview-toolbar div:first-child {
  display: grid;
  gap: 3px;
}

.template-preview-toolbar strong {
  color: #0f172a;
  font-size: 14px;
}

.template-preview-toolbar span {
  color: #64748b;
  font-size: 12px;
}

.template-preview-actions {
  display: flex;
  flex-wrap: wrap;
  gap: 8px;
}

.template-preview-actions button {
  min-height: 32px;
  border: 1px solid #cbd5e1;
  border-radius: 999px;
  padding: 0 12px;
  background: #fff;
  color: #334155;
  cursor: pointer;
  font-size: 12px;
  font-weight: 800;
}

.template-preview-actions button.active {
  border-color: #14b8a6;
  background: #ccfbf1;
  color: #0f766e;
}
</style>
