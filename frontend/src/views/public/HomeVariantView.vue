<script setup lang="ts">
import { computed } from 'vue'
import { useAppStore } from '@/stores'
import HomeView from '@/views/HomeView.vue'
import AixwHomeView from '@/views/public/AixwHomeView.vue'

const appStore = useAppStore()
const variant = computed(() =>
  appStore.cachedPublicSettings?.homepage_variant === 'aixw' ? 'aixw' : 'default'
)
</script>

<template>
  <AixwHomeView v-if="variant === 'aixw'" />
  <HomeView v-else />
</template>
