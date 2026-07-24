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
import { computed } from 'vue'

import { buildStaticHtmlDocument, STATIC_HTML_SANDBOX } from '@/utils/staticHtml'

const props = defineProps<{
  source: string
  title: string
}>()

const srcdoc = computed(() => buildStaticHtmlDocument(props.source))
</script>
