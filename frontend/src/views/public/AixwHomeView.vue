<!-- src/views/public/AixwHomeView.vue -->
<script setup lang="ts">
import heroBackgroundUrl from '@/assets/hero-background.webp'
import logoUrl from '@/assets/aixw-logo-transparent.webp'

const pageStyle = {
  backgroundImage: `url(${heroBackgroundUrl})`
}

const bottomWords = ['RELIABLE', 'GLOBAL', 'MINIMAL', 'FAST']
const glassLineClass = 'h-px min-w-10 rounded-full bg-gradient-to-r from-transparent via-white/85 to-transparent shadow-[0_0_18px_rgba(255,255,255,0.9),0_0_2px_rgba(120,130,145,0.22)] backdrop-blur-md after:block after:h-px after:w-full after:bg-gradient-to-r after:from-transparent after:via-slate-300/30 after:to-transparent'
</script>

<template>
  <div
    class="h-screen w-screen relative overflow-hidden font-sans bg-cover bg-center bg-no-repeat"
    :style="pageStyle"
    data-testid="aixw-home-page"
  >
    <!-- Logo -->
    <div class="absolute top-7 left-8 flex items-center sm:left-12 lg:top-8">
      <img :src="logoUrl" alt="AIXW Logo" class="h-10 w-auto sm:h-12 lg:h-16" />
    </div>

    <!-- Center Content -->
    <div class="absolute left-1/2 top-[47%] z-10 flex -translate-x-1/2 -translate-y-1/2 flex-col items-center">
      <h1 class="mb-9 whitespace-nowrap text-[clamp(3.5rem,7.2vw,6.75rem)] font-light leading-none text-[#101114]">
        Move faster.
      </h1>
      <router-link 
        to="/login"
        class="flex h-14 min-w-[180px] items-center justify-center gap-4 rounded-xl border border-white/10 bg-[#111111]/95 px-8 text-lg font-medium text-white shadow-[0_26px_60px_rgba(0,0,0,0.18),inset_0_1px_0_rgba(255,255,255,0.16)] backdrop-blur-xl transition-all hover:-translate-y-0.5 hover:bg-black hover:shadow-[0_32px_70px_rgba(0,0,0,0.22),inset_0_1px_0_rgba(255,255,255,0.18)]"
      >
        Get started 
        <svg viewBox="0 0 1024 1024" class="h-4 w-4 fill-current">
          <path d="M473.9 896.5c0-9 3.4-17.9 10.3-24.7L843.9 512 484.1 152.2c-13.7-13.7-13.7-35.8 0-49.5 13.7-13.7 35.8-13.7 49.5 0l384.5 384.5c13.7 13.7 13.7 35.8 0 49.5L533.6 921.3c-13.7 13.7-35.8 13.7-49.5 0-6.8-6.9-10.2-15.8-10.2-24.8z"></path>
          <path d="M95.6 512c0-19.3 15.7-35 35-35h762.7c19.3 0 35 15.7 35 35s-15.7 35-35 35H130.6c-19.3 0-35-15.7-35-35z"></path>
        </svg>
      </router-link>
    </div>

    <!-- Bottom Navigation/Labels -->
    <div class="absolute bottom-12 left-0 w-full sm:bottom-14">
      <div class="mx-auto grid max-w-7xl grid-cols-[1fr_auto_1fr_auto_1fr_auto_1fr_auto_1fr] items-center gap-5 px-8 text-[11px] font-medium uppercase tracking-[0.38em] text-[#6d727a] sm:gap-8 sm:px-14">
        <span :class="glassLineClass"></span>
        <template v-for="word in bottomWords" :key="word">
          <span class="whitespace-nowrap">{{ word }}</span>
          <span :class="glassLineClass"></span>
        </template>
      </div>
    </div>
  </div>
</template>
