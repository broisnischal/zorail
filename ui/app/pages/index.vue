<script setup lang="ts">
import { ref, computed, onMounted } from 'vue'
import { Inbox, AtSign, ArrowRight, CornerDownLeft } from 'lucide-vue-next'
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
// The resolved address is previewed live — the address is the product.
const preview = computed(() => qualify(typed.value))

function openTyped() {
  const a = preview.value
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
  <div class="relative h-full overflow-y-auto">
    <!-- ambient dot grid, faded toward the top: quiet texture, theme-aware -->
    <div
      class="pointer-events-none absolute inset-x-0 top-0 h-[60vh]"
      style="background-image: radial-gradient(circle at 1px 1px, var(--bd) 1px, transparent 0);
             background-size: 24px 24px;
             -webkit-mask-image: radial-gradient(ellipse 70% 65% at 50% 0%, #000, transparent 72%);
             mask-image: radial-gradient(ellipse 70% 65% at 50% 0%, #000, transparent 72%);
             opacity: 0.55;"
    />

    <div class="relative mx-auto flex min-h-full w-full max-w-[560px] flex-col items-center px-6 pb-24 pt-[12vh]">
      <div class="mb-6 flex size-14 items-center justify-center rounded-2xl border bg-[var(--bg-elevated)] shadow-xl shadow-black/20 ring-1 ring-white/5 transition-transform duration-300 hover:scale-105">
        <Inbox class="size-6 text-foreground/90" />
      </div>

      <h1 class="text-center text-[28px] font-semibold tracking-[-0.02em]">Open any inbox</h1>
      <p class="mt-2.5 max-w-[410px] text-center text-[13.5px] leading-relaxed text-muted-foreground">
        Public, disposable inboxes. Type any address and read its mail instantly — no sign-up, no waiting.
      </p>

      <div class="group mt-7 flex w-full max-w-[480px] items-center gap-2">
        <div class="relative flex-1">
          <AtSign class="absolute left-3.5 top-1/2 size-4 -translate-y-1/2 text-muted-foreground transition-colors group-focus-within:text-foreground" />
          <Input
            v-model="typed" autofocus class="h-11 pl-10 pr-10 text-[14px]"
            :placeholder="`anything   ·   or   anything@${domain}`"
            @keydown.enter="openTyped"
          />
          <CornerDownLeft
            v-if="typed.trim()"
            class="absolute right-3.5 top-1/2 size-3.5 -translate-y-1/2 text-muted-foreground/60"
          />
        </div>
        <Button class="h-11" :disabled="!typed.trim()" @click="openTyped">
          Open <ArrowRight class="transition-transform group-hover:translate-x-0.5" />
        </Button>
      </div>

      <!-- one swapping line: live resolved address while typing, else the random shortcut -->
      <div class="mt-3.5 flex h-5 items-center justify-center text-[12.5px]">
        <Transition name="fade" mode="out-in">
          <p v-if="preview" key="preview" class="font-mono text-muted-foreground">
            opening <span class="text-foreground">{{ preview }}</span>
          </p>
          <button
            v-else key="random"
            class="text-muted-foreground underline-offset-4 transition-colors hover:text-foreground hover:underline"
            @click="newAddress"
          >
            or generate a random address
          </button>
        </Transition>
      </div>
    </div>
  </div>
</template>
