<script setup lang="ts">
// A small convenience tooltip: <Tip label="Settings"><button>…</button></Tip>.
// Built on reka-ui's accessible Tooltip primitives; a single TooltipProvider
// lives at the app root. Motion is deliberately quick and subtle (Resend-style).
import { TooltipRoot, TooltipTrigger, TooltipPortal, TooltipContent } from 'reka-ui'

withDefaults(defineProps<{
  label: string
  side?: 'top' | 'right' | 'bottom' | 'left'
  disabled?: boolean
}>(), { side: 'top' })
</script>

<template>
  <TooltipRoot v-if="!disabled">
    <TooltipTrigger as-child>
      <slot />
    </TooltipTrigger>
    <TooltipPortal>
      <TooltipContent
        :side="side" :side-offset="7"
        class="z-[60] select-none rounded-lg border border-border bg-popover px-2.5 py-1.5 text-[11.5px] font-medium text-foreground shadow-lg shadow-black/30
               animate-in fade-in-0 zoom-in-95 duration-150
               data-[state=closed]:animate-out data-[state=closed]:fade-out-0 data-[state=closed]:zoom-out-95
               data-[side=top]:slide-in-from-bottom-1 data-[side=right]:slide-in-from-left-1
               data-[side=left]:slide-in-from-right-1 data-[side=bottom]:slide-in-from-top-1"
      >
        {{ label }}
      </TooltipContent>
    </TooltipPortal>
  </TooltipRoot>
  <slot v-else />
</template>
