<script setup lang="ts">
defineProps<{ title: string; subtitle?: string }>()
</script>

<template>
  <div class="h-full overflow-y-auto">
    <div class="mx-auto w-full max-w-[1080px] px-7 py-6 max-[860px]:px-4">
      <div class="mb-5 flex items-center justify-between gap-4">
        <div>
          <h1 class="text-[21px] font-semibold leading-tight tracking-tight">{{ title }}</h1>
          <p v-if="subtitle" class="mt-0.5 text-[12.5px] text-muted-foreground">{{ subtitle }}</p>
        </div>
        <div class="flex items-center gap-2"><slot name="actions" /></div>
      </div>
      <slot />
    </div>
  </div>
</template>
