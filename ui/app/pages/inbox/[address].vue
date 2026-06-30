<script setup lang="ts">
import { computed, watch } from 'vue'
import { Search, ArrowLeft, Hourglass, Loader2, Copy, Trash2, MailOpen } from 'lucide-vue-next'
import { Button } from '@/components/ui/button'
import { Input } from '@/components/ui/input'
import { Skeleton } from '@/components/ui/skeleton'
import { Tip } from '@/components/ui/tooltip'
import MessageReader from '@/components/MessageReader.vue'

const z = useZorail()
const { state } = z
const route = useRoute()

// The open inbox lives in the URL. Loading the inbox is driven by the route
// param, so deep links and back/forward just work.
const address = computed(() => decodeURIComponent(String(route.params.address || '')))
watch(address, (a) => { if (a) z.enterInbox(a) }, { immediate: true })
</script>

<template>
  <div class="flex h-full min-h-0 flex-col">
    <!-- inbox header -->
    <header class="flex h-14 flex-none items-center gap-1.5 border-b px-4">
      <Tip label="Back to inboxes" side="bottom">
        <Button variant="ghost" size="icon-sm" @click="z.closeInbox()"><ArrowLeft /></Button>
      </Tip>
      <span class="ml-1 truncate font-mono text-[13px]">{{ state.inbox }}</span>
      <span class="flex-1" />
      <Tip :label="state.waiting ? 'Waiting…' : 'Wait for next message (w)'" side="bottom">
        <Button variant="ghost" size="icon-sm" :disabled="state.waiting" @click="z.waitForNext(state.inbox)">
          <Loader2 v-if="state.waiting" class="animate-spin" /><Hourglass v-else />
        </Button>
      </Tip>
      <Tip label="Copy address" side="bottom">
        <Button variant="ghost" size="icon-sm" @click="z.copy(state.inbox, 'address copied')"><Copy /></Button>
      </Tip>
      <Tip label="Clear inbox" side="bottom">
        <Button variant="ghost" size="icon-sm" class="text-danger hover:text-danger" @click="z.clearInbox()"><Trash2 /></Button>
      </Tip>
    </header>

    <div class="grid min-h-0 flex-1 grid-cols-[380px_1fr] max-[900px]:grid-cols-1">
      <!-- message list -->
      <section class="flex min-h-0 flex-col border-r">
        <div class="flex-none border-b border-border-subtle p-3">
          <div class="relative">
            <Search class="absolute left-3 top-1/2 size-3.5 -translate-y-1/2 text-muted-foreground" />
            <Input
              :model-value="state.searchQuery" placeholder="Search this inbox + all mail…" class="h-9 pl-9"
              @update:model-value="(v) => z.search(String(v))"
            />
          </div>
        </div>
        <div class="flex-1 overflow-y-auto">
          <template v-if="state.loadingInbox && !state.messages.length">
            <div v-for="i in 6" :key="i" class="border-b border-border-subtle px-4 py-3">
              <Skeleton class="h-2.5 w-3/5" /><Skeleton class="mt-2 h-2.5 w-[85%]" />
            </div>
          </template>
          <ul v-else-if="state.messages.length">
            <li
              v-for="m in state.messages" :key="m.id"
              class="relative flex cursor-pointer flex-col gap-0.5 border-b border-border-subtle px-4 py-3 transition-colors hover:bg-muted/50"
              :class="m.id === state.current?.id ? 'bg-muted before:absolute before:inset-y-0 before:left-0 before:w-0.5 before:bg-foreground' : ''"
              @click="z.openMessage(m.id)"
            >
              <div class="flex items-baseline justify-between gap-2.5">
                <span class="flex min-w-0 items-center gap-2">
                  <span v-if="!z.isRead(m.id)" class="size-1.5 shrink-0 rounded-full bg-primary" />
                  <span class="truncate text-[13px]" :class="!z.isRead(m.id) ? 'font-semibold text-foreground' : 'text-muted-foreground'">
                    {{ parseFrom(m.from || m.env_from).name || parseFrom(m.from || m.env_from).email || '(unknown)' }}
                  </span>
                </span>
                <span class="shrink-0 text-[11px] tabular-nums text-muted-foreground">{{ relTime(m.received_at) }}</span>
              </div>
              <div class="truncate text-[12.5px]" :class="!z.isRead(m.id) ? 'text-foreground/80' : 'text-muted-foreground'">{{ m.subject || '(no subject)' }}</div>
              <div v-if="state.searchQuery" class="font-mono text-[11px] text-muted-foreground/70">{{ m.inbox }}</div>
            </li>
          </ul>
          <div v-else class="px-6 py-16 text-center text-[12.5px] text-muted-foreground">
            {{ state.searchQuery ? 'No matches.' : 'This inbox is empty.' }}
          </div>
        </div>
      </section>

      <!-- reader -->
      <section class="flex min-h-0 flex-col max-[900px]:hidden">
        <div v-if="state.loadingMsg && !state.current" class="m-auto">
          <span class="block size-4 animate-spin rounded-full border-2 border-border border-t-muted-foreground" />
        </div>
        <MessageReader v-else-if="state.current" :message="state.current" :raw="state.rawCache" @delete="z.deleteMessage" />
        <div v-else class="m-auto max-w-[320px] px-6 text-center text-[12.5px] leading-relaxed text-muted-foreground">
          <MailOpen class="mx-auto mb-3 size-7 opacity-50" />
          Select a message to read it.
        </div>
      </section>
    </div>
  </div>
</template>
