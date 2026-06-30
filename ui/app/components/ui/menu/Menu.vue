<script setup lang="ts">
import { ref } from 'vue'
import { onClickOutside, onKeyStroke } from '@vueuse/core'

// A small, self-contained popover menu — the project has no dropdown-menu
// primitive, and a focused popover keeps the chrome quiet (actions stay hidden
// until summoned, per the progressive-disclosure design).
withDefaults(defineProps<{ align?: 'start' | 'end' }>(), { align: 'end' })

const open = ref(false)
const root = ref<HTMLElement | null>(null)
onClickOutside(root, () => (open.value = false))
onKeyStroke('Escape', () => { if (open.value) open.value = false })

function toggle() { open.value = !open.value }
function close() { open.value = false }
defineExpose({ close, open: () => (open.value = true) })
</script>

<template>
  <div ref="root" class="relative inline-flex">
    <div class="contents" @click="toggle">
      <slot name="trigger" :open="open" />
    </div>
    <Transition
      enter-active-class="transition duration-150 ease-out"
      enter-from-class="opacity-0 scale-95 -translate-y-1"
      leave-active-class="transition duration-100 ease-in"
      leave-to-class="opacity-0 scale-95 -translate-y-1"
    >
      <div
        v-if="open"
        class="absolute top-[calc(100%+6px)] z-50 min-w-[220px] origin-top rounded-xl border bg-popover p-1.5 shadow-xl shadow-black/20"
        :class="align === 'end' ? 'right-0' : 'left-0'"
        role="menu"
        @click="close"
      >
        <slot />
      </div>
    </Transition>
  </div>
</template>
