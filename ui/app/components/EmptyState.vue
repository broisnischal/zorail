<script setup lang="ts">
import type { LucideIcon } from 'lucide-vue-next'
defineProps<{ icon: LucideIcon; title: string; description?: string }>()
</script>

<template>
  <div class="relative flex min-h-[460px] flex-col items-center justify-center overflow-hidden rounded-2xl border bg-gradient-to-b from-muted/25 to-transparent px-6 py-16 text-center">
    <!-- soft glow behind the icon -->
    <div
      class="pointer-events-none absolute left-1/2 top-[38%] size-72 -translate-x-1/2 -translate-y-1/2 rounded-full opacity-70 blur-3xl"
      style="background: radial-gradient(circle, var(--bg-elevated), transparent 70%)"
    />
    <div class="relative mb-5 flex size-16 items-center justify-center rounded-2xl border bg-gradient-to-br from-bg-elevated to-background shadow-lg shadow-black/30 ring-1 ring-white/5">
      <component :is="icon" class="size-7 text-foreground/80" />
    </div>
    <h2 class="relative text-[18px] font-semibold tracking-tight">{{ title }}</h2>
    <p v-if="description" class="relative mt-2 max-w-[360px] text-[13px] leading-relaxed text-muted-foreground">{{ description }}</p>
    <div class="relative mt-6 flex items-center gap-2"><slot /></div>
  </div>
</template>
