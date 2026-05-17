<template>
  <AppLayout>
    <div class="image-studio-placeholder">
      <h1>{{ t('imageStudio.title') }}</h1>
      <p>{{ t('imageStudio.description') }}</p>
    </div>
  </AppLayout>
</template>

<script setup lang="ts">
import { useI18n } from 'vue-i18n'
import AppLayout from '@/components/layout/AppLayout.vue'

const { t } = useI18n()
</script>
