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
        <span class="text-xl leading-none">-></span>
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