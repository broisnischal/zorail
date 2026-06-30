<script setup lang="ts">
defineProps<{ title: string; subtitle?: string }>()
</script>

<template>
  <div class="h-full overflow-y-auto">
    <div class="mx-auto w-full max-w-[1100px] px-8 py-7 max-[860px]:px-5">
      <div class="mb-6 flex items-end justify-between gap-4">
        <div>
          <h1 class="text-[26px] font-semibold tracking-tight">{{ title }}</h1>
          <p v-if="subtitle" class="mt-1 text-[13px] text-muted-foreground">{{ subtitle }}</p>
        </div>
        <div class="flex items-center gap-2"><slot name="actions" /></div>
      </div>
      <slot />
    </div>
  </div>
</template>
