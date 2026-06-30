<script setup lang="ts">
import { onMounted, onBeforeUnmount, ref } from 'vue'
import { X } from 'lucide-vue-next'
import { Toaster } from '@/components/ui/sonner'
import { TooltipProvider } from '@/components/ui/tooltip'

const z = useZorail()
const { state } = z

useHead({ title: () => state.organization || 'Zorail' })

const palette = ref<{ show: () => void; isOpen: () => boolean } | null>(null)
const sel = ref(-1)

function newAddress() {
  const a = z.generateAddress()
  z.copy(a, 'address generated + copied')
  z.openInbox(a)
}
function openAt(i: number) {
  const m = state.messages[i]
  if (m) { sel.value = i; z.openMessage(m.id) }
}

function onKey(e: KeyboardEvent) {
  const tag = (e.target as HTMLElement)?.tagName
  const typing = tag === 'INPUT' || tag === 'TEXTAREA'
  if ((e.metaKey || e.ctrlKey) && e.key.toLowerCase() === 'k') { e.preventDefault(); palette.value?.show(); return }
  if (palette.value?.isOpen?.()) return
  if (e.key === 'Escape') {
    if (state.current) { state.current = null; return }
    if (state.inbox) { z.closeInbox(); return }
  }
  if (typing) { if (e.key === 'Escape') (e.target as HTMLElement).blur(); return }

  switch (e.key) {
    case '/': e.preventDefault(); palette.value?.show(); break
    case 'r': e.preventDefault(); z.loadInboxes(); if (state.inbox) z.loadMessages(); break
    case ',': e.preventDefault(); z.setSection('settings'); break
    case 'g': e.preventDefault(); newAddress(); break
    case 'w': e.preventDefault(); if (state.inbox) z.waitForNext(state.inbox); break
    case 'j': e.preventDefault(); openAt(Math.min((sel.value < 0 ? -1 : sel.value) + 1, state.messages.length - 1)); break
    case 'k': e.preventDefault(); openAt(Math.max((sel.value < 0 ? 1 : sel.value) - 1, 0)); break
    case 'Enter': if (sel.value >= 0) openAt(sel.value); break
    case 'x': case 'Delete': if (state.current) z.deleteMessage(state.current.id); break
  }
}

onMounted(() => { z.init(); window.addEventListener('keydown', onKey) })
onBeforeUnmount(() => { z.stopPolling(); window.removeEventListener('keydown', onKey) })
</script>

<template>
  <TooltipProvider :delay-duration="200" :skip-delay-duration="300">
    <!-- gate: setup → app (sign-in is optional, shown as an overlay) -->
    <div v-if="!state.setupChecked" class="flex min-h-screen items-center justify-center">
      <span class="size-5 animate-spin rounded-full border-2 border-border border-t-muted-foreground" />
    </div>
    <Onboarding v-else-if="state.needsSetup" />
    <template v-else>
      <NuxtLayout>
        <NuxtPage />
      </NuxtLayout>
      <CommandPalette
        ref="palette"
        @settings="z.setSection('settings')"
        @manage="(t) => z.setSection(t === 'keys' ? 'keys' : t === 'addresses' ? 'addresses' : 'settings')"
      />
    </template>

    <!-- optional sign-in (others can browse without it; needed only to manage) -->
    <Transition name="overlay">
      <div v-if="state.signinOpen" class="fixed inset-0 z-50 bg-background">
        <button
          class="absolute right-5 top-5 inline-flex size-8 items-center justify-center rounded-md text-muted-foreground transition-colors hover:bg-accent hover:text-foreground"
          title="Close" @click="z.closeSignIn()"
        ><X class="size-4" /></button>
        <SignIn />
      </div>
    </Transition>

    <Toaster position="bottom-right" :duration="2600" />
  </TooltipProvider>
</template>
