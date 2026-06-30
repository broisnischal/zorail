<script setup lang="ts">
import { ref, computed, onMounted } from 'vue'
import { Inbox, AtSign, ArrowRight } from 'lucide-vue-next'
import { Button } from '@/components/ui/button'
import { Input } from '@/components/ui/input'

const z = useZorail()
const { state } = z
const domain = computed(() => state.config.domain || 'localhost')

const typed = ref('')
function qualify(t: string) {
  t = t.trim().toLowerCase()
  if (!t) return ''
  return t.includes('@') ? t : `${t}@${domain.value}`
}
function openTyped() {
  const a = qualify(typed.value)
  if (a) { z.openInbox(a); typed.value = '' }
}
function newAddress() {
  const a = z.generateAddress()
  z.copy(a, 'address generated + copied')
  z.openInbox(a)
}

// Landing has no open inbox — drop inbox state so polling doesn't keep fetching.
onMounted(() => z.leaveInbox())
</script>

<template>
  <div class="h-full overflow-y-auto">
    <div class="mx-auto flex min-h-full w-full max-w-[560px] flex-col items-center px-6 pb-24 pt-[11vh]">
      <div class="mb-5 flex size-12 items-center justify-center rounded-2xl border bg-gradient-to-br from-bg-elevated to-background shadow-lg shadow-black/30 ring-1 ring-white/5 transition-transform duration-300 hover:scale-105">
        <Inbox class="size-[22px] text-foreground/90" />
      </div>
      <h1 class="text-center text-[24px] font-semibold tracking-tight">Open any inbox</h1>
      <p class="mt-2 max-w-[400px] text-center text-[13px] leading-relaxed text-muted-foreground">
        Public, disposable inboxes — type any address and read its mail instantly. No sign-up.
      </p>

      <div class="group mt-6 flex w-full max-w-[460px] items-center gap-2">
        <div class="relative flex-1">
          <AtSign class="absolute left-3.5 top-1/2 size-4 -translate-y-1/2 text-muted-foreground transition-colors group-focus-within:text-foreground" />
          <Input
            v-model="typed" autofocus class="h-11 pl-10 text-[14px]"
            :placeholder="`anything   ·   or   anything@${domain}`"
            @keydown.enter="openTyped"
          />
        </div>
        <Button class="h-11" :disabled="!typed.trim()" @click="openTyped">
          Open <ArrowRight class="transition-transform group-hover:translate-x-0.5" />
        </Button>
      </div>
      <button class="mt-4 text-[12.5px] text-muted-foreground underline-offset-4 transition-colors hover:text-foreground hover:underline" @click="newAddress">
        or generate a random address
      </button>
    </div>
  </div>
</template>
