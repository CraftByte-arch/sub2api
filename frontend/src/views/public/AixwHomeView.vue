<!-- src/views/public/AixwHomeView.vue -->
<script setup lang="ts">
// Minimalist landing page
</script>

<template>
  <div class="h-screen w-screen relative overflow-hidden font-sans bg-cover bg-center bg-no-repeat" style="background-image: url('/src/assets/hero-background.webp')">
    <!-- Logo -->
    <div class="absolute top-8 left-12 flex items-center">
      <img src="@/assets/aixw-logo-transparent.webp" alt="AIXW Logo" class="h-8 w-auto mix-blend-darken" />
    </div>

    <!-- Center Content -->
    <div class="absolute top-1/2 left-1/2 -translate-x-1/2 -translate-y-1/2 flex flex-col items-center z-10">
      <h1 class="text-6xl md:text-7xl lg:text-8xl text-gray-900 mb-8 font-medium tracking-tight whitespace-nowrap">
        Move faster.
      </h1>
      <router-link 
        to="/login"
        class="bg-[#111111] text-white px-8 py-4 rounded-xl font-medium transition-all hover:bg-black hover:scale-105 hover:shadow-lg flex items-center gap-2"
      >
        Get started 
        <svg viewBox="0 0 1024 1024" class="w-4 h-4 fill-current">
          <path d="M473.9 896.5c0-9 3.4-17.9 10.3-24.7L843.9 512 484.1 152.2c-13.7-13.7-13.7-35.8 0-49.5 13.7-13.7 35.8-13.7 49.5 0l384.5 384.5c13.7 13.7 13.7 35.8 0 49.5L533.6 921.3c-13.7 13.7-35.8 13.7-49.5 0-6.8-6.9-10.2-15.8-10.2-24.8z"></path>
          <path d="M95.6 512c0-19.3 15.7-35 35-35h762.7c19.3 0 35 15.7 35 35s-15.7 35-35 35H130.6c-19.3 0-35-15.7-35-35z"></path>
        </svg>
      </router-link>
    </div>

    <!-- Bottom Navigation/Labels -->
    <div class="absolute bottom-12 left-0 w-full">
      <div class="max-w-6xl mx-auto px-12 relative flex justify-between items-center text-[10px] sm:text-xs text-gray-400 tracking-[0.2em] uppercase font-medium">
        <div class="absolute top-1/2 left-12 right-12 h-px bg-gray-200/50 -z-10"></div>
        <span class="px-4 bg-transparent text-gray-500">RELIABLE</span>
        <span class="px-4 bg-transparent text-gray-500">GLOBAL</span>
        <span class="px-4 bg-transparent text-gray-500">MINIMAL</span>
        <span class="px-4 bg-transparent text-gray-500">FAST</span>
      </div>
    </div>
  </div>
</template>