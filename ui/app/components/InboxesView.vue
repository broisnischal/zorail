<script setup lang="ts">
import { computed, ref } from 'vue'
import { Inbox, Plus, Search, ArrowLeft, Hourglass, Loader2, Copy, Trash2, Pin, MailOpen, ArrowRight } from 'lucide-vue-next'
import { Button } from '@/components/ui/button'
import { Input } from '@/components/ui/input'
import { Skeleton } from '@/components/ui/skeleton'

const z = useZorail()
const { state } = z

const sortedInboxes = computed(() =>
  [...state.inboxes].sort((a, b) => Number(z.isPinned(b.inbox)) - Number(z.isPinned(a.inbox))),
)

function newAddress() {
  const a = z.generateAddress()
  z.copy(a, 'address generated + copied')
  z.openInbox(a)
}

// Open ANY inbox by typing it — a bare local part is qualified with the domain.
const typed = ref('')
function qualify(t: string) {
  t = t.trim().toLowerCase()
  if (!t) return ''
  return t.includes('@') ? t : `${t}@${state.config.domain || 'localhost'}`
}
function openTyped() {
  const a = qualify(typed.value)
  if (a) { z.openInbox(a); typed.value = '' }
}
</script>

<template>
  <!-- ============ LIST MODE ============ -->
  <AppPage v-if="!state.inbox" title="Inboxes" subtitle="Every address at your domain is a live, disposable inbox.">
    <template #actions>
      <Button size="sm" @click="newAddress"><Plus /> New address</Button>
    </template>

    <!-- open any inbox by typing it -->
    <div class="mb-5 flex items-center gap-2">
      <div class="relative flex-1">
        <Search class="absolute left-3 top-1/2 size-4 -translate-y-1/2 text-muted-foreground" />
        <Input
          v-model="typed" class="h-10 pl-9"
          :placeholder="`Open any inbox — type an address (e.g. qa-1 or qa-1@${state.config.domain || 'localhost'})`"
          @keydown.enter="openTyped"
        />
      </div>
      <Button variant="outline" class="h-10" :disabled="!typed.trim()" @click="openTyped">Open <ArrowRight /></Button>
    </div>

    <div v-if="state.inboxes.length" class="overflow-hidden rounded-xl border">
      <button
        v-for="ib in sortedInboxes" :key="ib.inbox"
        class="flex w-full items-center gap-3 border-b border-border-subtle px-4 py-3 text-left transition-colors last:border-b-0 hover:bg-muted/50"
        @click="z.openInbox(ib.inbox)"
      >
        <span class="flex size-8 shrink-0 items-center justify-center rounded-lg border bg-muted/50">
          <Inbox class="size-4 text-muted-foreground" />
        </span>
        <span class="min-w-0 flex-1">
          <span class="block truncate font-mono text-[13px]">{{ ib.inbox }}</span>
          <span class="block text-[11.5px] text-muted-foreground">{{ ib.message_count }} message{{ ib.message_count === 1 ? '' : 's' }}</span>
        </span>
        <Pin
          v-if="z.isPinned(ib.inbox)" class="size-3.5 text-muted-foreground" fill="currentColor"
        />
        <span class="shrink-0 text-[11.5px] tabular-nums text-muted-foreground">{{ relTime(ib.last_received) }}</span>
      </button>
    </div>

    <EmptyState
      v-else :icon="Inbox" title="No inboxes yet"
      description="Generate a disposable address, then point your test mail at it — messages show up here instantly."
    >
      <Button size="sm" @click="newAddress"><Plus /> New address</Button>
    </EmptyState>
  </AppPage>

  <!-- ============ DETAIL MODE ============ -->
  <div v-else class="flex h-full min-h-0 flex-col">
    <!-- inbox header -->
    <header class="flex h-14 flex-none items-center gap-2 border-b px-4">
      <Button variant="ghost" size="icon-sm" title="Back to inboxes" @click="z.closeInbox()"><ArrowLeft /></Button>
      <span class="truncate font-mono text-[13px]">{{ state.inbox }}</span>
      <span class="flex-1" />
      <Button
        variant="ghost" size="icon-sm"
        :title="state.waiting ? 'waiting…' : 'Wait for next message (w)'"
        :disabled="state.waiting" @click="z.waitForNext(state.inbox)"
      ><Loader2 v-if="state.waiting" class="animate-spin" /><Hourglass v-else /></Button>
      <Button variant="ghost" size="icon-sm" title="Copy address" @click="z.copy(state.inbox, 'address copied')"><Copy /></Button>
      <Button variant="ghost" size="icon-sm" class="text-danger hover:text-danger" title="Clear inbox" @click="z.clearInbox()"><Trash2 /></Button>
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
                <span class="truncate text-[13px]" :class="!z.isRead(m.id) ? 'font-semibold text-foreground' : 'text-muted-foreground'">
                  {{ parseFrom(m.from || m.env_from).name || parseFrom(m.from || m.env_from).email || '(unknown)' }}
                </span>
                <span class="shrink-0 text-[11px] tabular-nums text-muted-foreground">{{ relTime(m.received_at) }}</span>
              </div>
              <div class="truncate text-[12.5px] text-muted-foreground">{{ m.subject || '(no subject)' }}</div>
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
