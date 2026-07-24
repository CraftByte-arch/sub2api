<template>
  <iframe
    class="block h-full w-full border-0 bg-white"
    :title="title"
    :sandbox="STATIC_HTML_SANDBOX"
    :srcdoc="srcdoc"
    referrerpolicy="no-referrer"
  />
</template>

<script setup lang="ts">
import { computed, onMounted, onUnmounted, ref } from 'vue'

import {
  buildStaticHtmlDocument,
  STATIC_HTML_SANDBOX,
  type StaticHtmlTheme,
} from '@/utils/staticHtml'

const props = defineProps<{
  source: string
  title: string
}>()

function resolveTheme(): StaticHtmlTheme {
  return typeof document !== 'undefined' && document.documentElement.classList.contains('dark')
    ? 'dark'
    : 'light'
}

const theme = ref<StaticHtmlTheme>(resolveTheme())
let themeObserver: MutationObserver | null = null

function syncTheme() {
  theme.value = resolveTheme()
}

const srcdoc = computed(() => buildStaticHtmlDocument(props.source, theme.value))

onMounted(() => {
  syncTheme()
  if (typeof MutationObserver === 'undefined') return

  themeObserver = new MutationObserver(syncTheme)
  themeObserver.observe(document.documentElement, {
    attributes: true,
    attributeFilter: ['class'],
  })
})

onUnmounted(() => {
  themeObserver?.disconnect()
  themeObserver = null
})
</script>
