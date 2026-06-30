<script setup lang="ts">
import { onMounted, onBeforeUnmount, ref, computed } from 'vue'
import {
  Inbox, Search, Plus, RefreshCw, Settings2, Copy, Trash2, Pin,
  MailOpen, AlertTriangle, Hourglass, Loader2,
  UserRound, KeyRound, Mail, LogIn, LogOut, CircleDot, CirclePause,
} from 'lucide-vue-next'
import { Button } from '@/components/ui/button'
import { Skeleton } from '@/components/ui/skeleton'
import { Input } from '@/components/ui/input'
import { Toaster } from '@/components/ui/sonner'
import { Menu, MenuItem } from '@/components/ui/menu'
import { Separator } from '@/components/ui/separator'

const z = useZorail()
const { state } = z

const initials = computed(() => {
  const e = state.user?.email || ''
  return e ? e.slice(0, 2).toUpperCase() : ''
})

const palette = ref<{ show: () => void; isOpen: () => boolean } | null>(null)
const settings = ref<{ open: () => void } | null>(null)
const manage = ref<{ open: (t?: 'account' | 'addresses' | 'keys') => void } | null>(null)
const searchEl = ref<HTMLInputElement | null>(null)
const sel = ref(-1) // keyboard cursor into messages

const sortedInboxes = computed(() =>
  [...state.inboxes].sort((a, b) => Number(z.isPinned(b.inbox)) - Number(z.isPinned(a.inbox))),
)

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
  const paletteOpen = palette.value?.isOpen?.()

  if ((e.metaKey || e.ctrlKey) && e.key.toLowerCase() === 'k') { e.preventDefault(); palette.value?.show(); return }
  if (paletteOpen) return
  if (e.key === 'Escape' && state.current) { state.current = null; return }
  if (typing) {
    if (e.key === 'Escape') (e.target as HTMLElement).blur()
    return
  }

  switch (e.key) {
    case '/': e.preventDefault(); searchEl.value?.focus(); break
    case 'r': e.preventDefault(); z.loadInboxes(); if (state.inbox) z.loadMessages(); break
    case ',': e.preventDefault(); settings.value?.open(); break
    case 'm': e.preventDefault(); manage.value?.open(); break
    case 'g': e.preventDefault(); newAddress(); break
    case 'w': e.preventDefault(); if (state.inbox) z.waitForNext(state.inbox); break
    case 'j': e.preventDefault(); openAt(Math.min((sel.value < 0 ? -1 : sel.value) + 1, state.messages.length - 1)); break
    case 'k': e.preventDefault(); openAt(Math.max((sel.value < 0 ? 1 : sel.value) - 1, 0)); break
    case 'Enter': if (sel.value >= 0) openAt(sel.value); break
    case 'x': case 'Delete': if (state.current) z.deleteMessage(state.current.id); break
    case '?': e.preventDefault(); settings.value?.open(); break
  }
}

onMounted(() => { z.init(); window.addEventListener('keydown', onKey) })
onBeforeUnmount(() => { z.stopPolling(); window.removeEventListener('keydown', onKey) })
</script>

<template>
  <Onboarding v-if="state.setupChecked && state.needsSetup" />

  <div v-else class="flex h-screen flex-col overflow-hidden">
    <!-- header -->
    <header class="flex h-14 flex-none items-center gap-3 border-b px-4">
      <div class="flex select-none items-center gap-2 pr-1">
        <Inbox class="size-[18px]" />
        <span class="max-w-[180px] truncate text-sm font-semibold tracking-tight">{{ state.organization || 'zorail' }}</span>
      </div>

      <button
        class="ml-1 inline-flex h-9 w-full max-w-[440px] items-center gap-2.5 rounded-lg border bg-muted/40 px-3 text-[13px] text-muted-foreground transition-colors hover:bg-muted hover:border-border-hover"
        title="Command palette (⌘K)" @click="palette?.show()"
      >
        <Search class="size-4" />
        <span class="truncate max-[1000px]:hidden">Search or jump to an inbox…</span>
        <span class="flex-1" />
        <kbd class="max-[1000px]:hidden">⌘K</kbd>
      </button>

      <span class="flex-1" />

      <Button size="sm" @click="newAddress">
        <Plus /> <span class="max-[680px]:hidden">New address</span>
      </Button>

      <Menu align="end">
        <template #trigger>
          <button
            class="inline-flex size-9 shrink-0 items-center justify-center rounded-full border bg-muted/40 text-[11.5px] font-medium transition-colors hover:bg-muted"
            :title="z.isAuthed() ? `Signed in as ${state.user?.email}` : 'Account'"
          >
            <span v-if="z.isAuthed()">{{ initials }}</span>
            <UserRound v-else class="size-4 text-muted-foreground" />
          </button>
        </template>

        <div class="px-2.5 pb-1.5 pt-1">
          <template v-if="z.isAuthed()">
            <div class="truncate text-[13px] font-medium text-foreground">{{ state.user?.email }}</div>
            <div class="text-[11px] text-muted-foreground">{{ state.organization || 'Zorail' }} · signed in</div>
          </template>
          <template v-else>
            <div class="text-[13px] font-medium text-foreground">Not signed in</div>
            <div class="text-[11px] text-muted-foreground">Sign in to reserve & forward</div>
          </template>
        </div>
        <Separator class="my-1" />
        <MenuItem @click="manage?.open('addresses')"><Mail /> Addresses</MenuItem>
        <MenuItem @click="manage?.open('keys')"><KeyRound /> API keys</MenuItem>
        <MenuItem @click="settings?.open()"><Settings2 /> Settings</MenuItem>
        <Separator class="my-1" />
        <MenuItem @click="z.loadInboxes(); state.inbox && z.loadMessages()"><RefreshCw /> Refresh now</MenuItem>
        <MenuItem @click="z.toggleAuto()">
          <component :is="state.autoRefresh ? CirclePause : CircleDot" />
          {{ state.autoRefresh ? 'Pause live updates' : 'Resume live updates' }}
        </MenuItem>
        <Separator class="my-1" />
        <MenuItem v-if="!z.isAuthed()" @click="manage?.open('account')"><LogIn /> Sign in</MenuItem>
        <MenuItem v-else danger @click="z.logout()"><LogOut /> Sign out</MenuItem>
      </Menu>
    </header>

    <div v-if="state.error" class="flex items-center gap-2 border-b border-danger/35 bg-danger/10 px-4 py-2 text-xs text-danger">
      <AlertTriangle class="size-3.5" />{{ state.error }}
    </div>

    <div class="grid min-h-0 flex-1 grid-cols-[288px_392px_1fr] max-[1000px]:grid-cols-1 max-[1000px]:grid-rows-[auto_auto_1fr]">
      <!-- inboxes -->
      <section class="flex min-h-0 flex-col border-r max-[1000px]:max-h-[32vh]">
        <div class="flex h-12 flex-none items-center justify-between gap-2 border-b border-border-subtle px-3.5">
          <span class="text-[10px] font-medium uppercase tracking-wide text-muted-foreground">inboxes</span>
          <span class="font-mono text-[11.5px] tabular-nums text-muted-foreground">{{ state.inboxes.length || '' }}</span>
        </div>
        <div class="flex-1 overflow-y-auto">
          <ul v-if="sortedInboxes.length">
            <li
              v-for="ib in sortedInboxes"
              :key="ib.inbox"
              class="group relative flex cursor-pointer flex-col gap-0.5 border-b border-border-subtle px-3.5 py-2.5 transition-colors hover:bg-muted/60"
              :class="ib.inbox === state.inbox ? 'bg-muted before:absolute before:inset-y-0 before:left-0 before:w-0.5 before:bg-foreground' : ''"
              @click="z.openInbox(ib.inbox)"
            >
              <div class="flex items-baseline justify-between gap-2.5">
                <span class="truncate font-mono text-[12.5px]" :class="ib.inbox === state.inbox ? 'text-foreground' : 'text-muted-foreground'">{{ ib.inbox }}</span>
                <span class="shrink-0 whitespace-nowrap text-[11px] tabular-nums text-muted-foreground">{{ relTime(ib.last_received) }}</span>
              </div>
              <div class="mt-0.5 flex items-center gap-2">
                <button
                  class="inline-flex transition-opacity"
                  :class="z.isPinned(ib.inbox) ? 'text-foreground opacity-100' : 'text-muted-foreground opacity-0 hover:text-foreground group-hover:opacity-100'"
                  :title="z.isPinned(ib.inbox) ? 'unpin' : 'pin'"
                  @click.stop="z.togglePin(ib.inbox)"
                ><Pin class="size-3" :fill="z.isPinned(ib.inbox) ? 'currentColor' : 'none'" /></button>
                <span class="text-[11.5px] tabular-nums text-muted-foreground">{{ ib.message_count }} msg</span>
              </div>
            </li>
          </ul>
          <div v-else class="px-6 py-12 text-center text-[12.5px] leading-relaxed text-muted-foreground">
            <Inbox class="mx-auto mb-3.5 size-7 opacity-50" />
            No inboxes yet.<br>Press <kbd>⌘K</kbd> to generate a disposable address, then send mail to it.
          </div>
        </div>
      </section>

      <!-- messages -->
      <section class="flex min-h-0 flex-col border-r max-[1000px]:max-h-[32vh]">
        <div class="flex h-12 flex-none items-center justify-between gap-2 border-b border-border-subtle px-3.5">
          <div class="flex min-w-0 items-center gap-2">
            <span class="text-[10px] font-medium uppercase tracking-wide text-muted-foreground">{{ state.inbox ? 'inbox' : 'messages' }}</span>
            <span v-if="state.inbox" class="truncate font-mono text-xs text-foreground">{{ state.inbox }}</span>
          </div>
          <div class="flex items-center gap-1">
            <Button
              v-if="state.inbox" variant="ghost" size="icon-sm"
              :title="state.waiting ? 'waiting for next message…' : 'Wait for next message (w)'"
              :disabled="state.waiting" @click="z.waitForNext(state.inbox)"
            >
              <Loader2 v-if="state.waiting" class="animate-spin" /><Hourglass v-else />
            </Button>
            <Button v-if="state.inbox" variant="ghost" size="icon-sm" title="Copy address" @click="z.copy(state.inbox, 'address copied')"><Copy /></Button>
            <Button v-if="state.inbox" variant="ghost" size="icon-sm" class="text-danger hover:text-danger" title="Clear inbox" @click="z.clearInbox()"><Trash2 /></Button>
          </div>
        </div>
        <div class="flex-none border-b border-border-subtle px-3 py-2.5">
          <div class="relative">
            <Search class="absolute left-3 top-1/2 size-3.5 -translate-y-1/2 text-muted-foreground" />
            <Input
              ref="searchEl"
              :model-value="state.searchQuery"
              placeholder="search this inbox + all mail…"
              class="h-8 pl-9"
              @update:model-value="(v) => z.search(String(v))"
            />
            <span v-if="state.searching" class="absolute right-3 top-1/2 size-3.5 -translate-y-1/2 animate-spin rounded-full border-2 border-border border-t-muted-foreground" />
          </div>
        </div>
        <div class="flex-1 overflow-y-auto">
          <template v-if="state.loadingInbox && !state.messages.length">
            <div v-for="i in 6" :key="i" class="border-b border-border-subtle px-3.5 py-3">
              <Skeleton class="h-2.5 w-3/5" />
              <Skeleton class="mt-2 h-2.5 w-[85%]" />
            </div>
          </template>
          <ul v-else-if="state.messages.length">
            <li
              v-for="(m, i) in state.messages"
              :key="m.id"
              class="relative flex cursor-pointer flex-col gap-0.5 border-b border-border-subtle px-3.5 py-2.5 transition-colors hover:bg-muted/60"
              :class="m.id === state.current?.id ? 'bg-muted before:absolute before:inset-y-0 before:left-0 before:w-0.5 before:bg-foreground' : ''"
              @click="openAt(i)"
            >
              <div class="flex items-baseline justify-between gap-2.5">
                <span
                  class="truncate text-[12.5px]"
                  :class="!z.isRead(m.id) ? 'font-semibold text-foreground' : (m.id === state.current?.id ? 'text-foreground' : 'text-muted-foreground')"
                >{{ parseFrom(m.from || m.env_from).name || parseFrom(m.from || m.env_from).email || '(unknown)' }}</span>
                <span class="shrink-0 whitespace-nowrap text-[11px] tabular-nums text-muted-foreground">{{ relTime(m.received_at) }}</span>
              </div>
              <div class="truncate text-xs" :class="!z.isRead(m.id) ? 'text-muted-foreground' : 'text-muted-foreground/70'">{{ m.subject || '(no subject)' }}</div>
              <div v-if="!z.isRead(m.id) || state.searchQuery" class="mt-0.5 flex items-center gap-2">
                <span v-if="!z.isRead(m.id)" class="size-1.5 rounded-full bg-foreground" />
                <span v-if="state.searchQuery" class="font-mono text-[11.5px] text-muted-foreground">{{ m.inbox }}</span>
              </div>
            </li>
          </ul>
          <div v-else class="px-6 py-12 text-center text-[12.5px] text-muted-foreground">
            {{ state.searchQuery ? 'no matches' : (state.inbox ? 'this inbox is empty' : 'select or generate an inbox') }}
          </div>
        </div>
      </section>

      <!-- reader -->
      <section class="flex min-h-0 flex-col">
        <div v-if="state.loadingMsg && !state.current" class="m-auto">
          <span class="size-4 animate-spin rounded-full border-2 border-border border-t-muted-foreground" />
        </div>
        <MessageReader v-else-if="state.current" :message="state.current" :raw="state.rawCache" @delete="z.deleteMessage" />
        <div v-else class="m-auto max-w-[340px] px-6 py-12 text-center text-[12.5px] leading-relaxed text-muted-foreground">
          <MailOpen class="mx-auto mb-3.5 size-7 opacity-50" />
          Select a message to read it.<br>
          <span class="text-muted-foreground/70">Codes, links and unsubscribe targets are detected automatically.</span>
        </div>
      </section>
    </div>

    <CommandPalette ref="palette" @settings="settings?.open()" @manage="(t) => manage?.open(t)" />
    <SettingsDialog ref="settings" />
    <ManagePanel ref="manage" />
  </div>

  <Toaster position="bottom-right" :duration="2600" />
</template>
