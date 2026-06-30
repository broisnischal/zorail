<script setup lang="ts">
withDefaults(defineProps<{ danger?: boolean }>(), { danger: false })
</script>

<template>
  <button
    type="button"
    role="menuitem"
    class="flex w-full items-center gap-2.5 rounded-lg px-2.5 py-1.5 text-left text-[13px] transition-colors [&_svg]:size-4 [&_svg]:shrink-0 [&_svg]:text-muted-foreground"
    :class="danger
      ? 'text-danger hover:bg-danger/10 [&_svg]:text-danger'
      : 'text-muted-foreground hover:bg-accent hover:text-foreground'"
  >
    <slot />
  </button>
</template>
