<script setup lang="ts">
import type { LucideIcon } from 'lucide-vue-next'
defineProps<{ icon: LucideIcon; title: string; description?: string }>()
</script>

<template>
  <div class="flex min-h-[440px] flex-col items-center justify-center rounded-2xl border bg-gradient-to-b from-muted/30 to-transparent px-6 py-16 text-center">
    <div class="mb-5 flex size-16 items-center justify-center rounded-2xl border bg-gradient-to-br from-bg-elevated to-background shadow-inner">
      <component :is="icon" class="size-7 text-muted-foreground" />
    </div>
    <h2 class="text-[17px] font-semibold tracking-tight">{{ title }}</h2>
    <p v-if="description" class="mt-1.5 max-w-[340px] text-[13px] leading-relaxed text-muted-foreground">{{ description }}</p>
    <div class="mt-6 flex items-center gap-2"><slot /></div>
  </div>
</template>
