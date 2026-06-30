<script setup lang="ts">
import AppSidebar from '@/components/AppSidebar.vue'
</script>

<template>
  <div class="flex h-screen overflow-hidden">
    <AppSidebar />
    <main class="flex min-w-0 flex-1 flex-col">
      <slot />
    </main>
  </div>
</template>
