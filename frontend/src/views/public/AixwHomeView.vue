<!-- src/views/public/AixwHomeView.vue -->
<script setup lang="ts">
// Minimalist landing page
</script>

<template>
  <div class="h-screen w-screen bg-[#FDFDFD] relative overflow-hidden font-sans">
    <!-- Logo -->
    <div class="absolute top-8 left-12 flex items-center gap-2 font-semibold text-lg tracking-wider">
      <svg width="24" height="24" viewBox="0 0 24 24" fill="none" xmlns="http://www.w3.org/2000/svg">
        <path d="M12 2L22 22H2L12 2Z" fill="black" fill-opacity="0.8"/>
        <path d="M12 8L17 18H7L12 8Z" fill="white"/>
      </svg>
      <span>A AIXW</span>
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

    <!-- 3D Graphic (Pure Code SVG) -->
    <svg class="absolute right-0 top-1/2 -translate-y-1/2 translate-x-1/4 h-[80vh] w-auto pointer-events-none opacity-90 drop-shadow-2xl" viewBox="0 0 800 800" fill="none" xmlns="http://www.w3.org/2000/svg">
      <defs>
        <linearGradient id="grad1" x1="0%" y1="0%" x2="100%" y2="100%">
          <stop offset="0%" stop-color="#ffffff" stop-opacity="0.9" />
          <stop offset="100%" stop-color="#f0f0f0" stop-opacity="0.4" />
        </linearGradient>
        <linearGradient id="grad2" x1="100%" y1="0%" x2="0%" y2="100%">
          <stop offset="0%" stop-color="#ffffff" stop-opacity="1" />
          <stop offset="100%" stop-color="#e5e5e5" stop-opacity="0.2" />
        </linearGradient>
        <linearGradient id="grad3" x1="50%" y1="0%" x2="50%" y2="100%">
          <stop offset="0%" stop-color="#ffffff" stop-opacity="0.8" />
          <stop offset="100%" stop-color="#d4d4d4" stop-opacity="0.5" />
        </linearGradient>
      </defs>
      
      <!-- Fold 1 (Back left) -->
      <path d="M100 400 L400 100 L450 450 Z" fill="url(#grad1)" />
      <!-- Fold 2 (Front right) -->
      <path d="M400 100 L700 300 L450 450 Z" fill="url(#grad2)" />
      <!-- Fold 3 (Bottom) -->
      <path d="M100 400 L450 450 L700 750 Z" fill="url(#grad3)" />
      <!-- Subtle highlight lines -->
      <path d="M100 400 L400 100" stroke="white" stroke-width="2" stroke-opacity="0.8" />
      <path d="M400 100 L700 300" stroke="white" stroke-width="3" stroke-opacity="0.9" />
      <path d="M400 100 L450 450" stroke="white" stroke-width="1.5" stroke-opacity="0.5" />
    </svg>

    <!-- Bottom Navigation/Labels -->
    <div class="absolute bottom-12 left-0 w-full">
      <div class="max-w-6xl mx-auto px-12 relative flex justify-between items-center text-[10px] sm:text-xs text-gray-400 tracking-[0.2em] uppercase font-medium">
        <div class="absolute top-1/2 left-12 right-12 h-px bg-gray-200/50 -z-10"></div>
        <span class="bg-[#FDFDFD] px-4">RELIABLE</span>
        <span class="bg-[#FDFDFD] px-4">GLOBAL</span>
        <span class="bg-[#FDFDFD] px-4">MINIMAL</span>
        <span class="bg-[#FDFDFD] px-4">FAST</span>
      </div>
    </div>
  </div>
</template>