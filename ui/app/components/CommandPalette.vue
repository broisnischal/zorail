<script setup lang="ts">
import { ref, computed, watch, nextTick } from 'vue'
import {
  Search, Sparkles, RefreshCw, CircleDot, Image, ImageOff, SunMoon,
  Settings2, Trash2, Mail, ArrowRight, CornerDownLeft, ArrowUpDown,
  UserCog, KeyRound, Hourglass, Forward,
} from 'lucide-vue-next'
import { Dialog, DialogContent, DialogTitle, DialogDescription } from '@/components/ui/dialog'

// Built on shadcn's Dialog (focus-trap, scroll-lock, animation, a11y) with a
// hand-rolled command list: reka's Command filter caches each item's text at
// mount, which can't express the dynamic "open/create any inbox" affordance, so
// filtering + keyboard nav are done here for fully predictable behavior.
const emit = defineEmits<{ (e: 'settings'): void; (e: 'manage', tab?: 'account' | 'addresses' | 'keys'): void }>()
const z = useZorail()
const { state } = z

const open = ref(false)
const q = ref('')
const active = ref(0)
const inputEl = ref<HTMLInputElement | null>(null)

interface Cmd { id: string; icon: unknown; title: string; sub?: string; group: string; run: () => void }

const base = computed<Cmd[]>(() => {
  const cmds: Cmd[] = [
    { id: 'gen', icon: Sparkles, title: 'Generate disposable address', sub: '@' + (state.config.domain || 'localhost'), group: 'actions', run: genAddress },
    { id: 'refresh', icon: RefreshCw, title: 'Refresh', sub: 'r', group: 'actions', run: () => { z.loadInboxes(); if (state.inbox) z.loadMessages() } },
    { id: 'auto', icon: CircleDot, title: (state.autoRefresh ? 'Disable' : 'Enable') + ' auto-refresh', group: 'actions', run: () => z.toggleAuto() },
    { id: 'images', icon: state.loadImages ? ImageOff : Image, title: (state.loadImages ? 'Block' : 'Load') + ' remote images', group: 'actions', run: () => z.toggleImages() },
    { id: 'theme', icon: SunMoon, title: 'Toggle ' + (state.theme === 'dark' ? 'light' : 'dark') + ' theme', group: 'actions', run: () => z.setTheme(state.theme === 'dark' ? 'light' : 'dark') },
    { id: 'account', icon: UserCog, title: z.isAuthed() ? 'Account & settings' : 'Sign in / create account', sub: 'm', group: 'manage', run: () => emit('manage', 'account') },
    { id: 'reserve', icon: Forward, title: 'Reserve or forward an address', group: 'manage', run: () => emit('manage', 'addresses') },
    { id: 'keys', icon: KeyRound, title: 'Manage API keys', group: 'manage', run: () => emit('manage', 'keys') },
    { id: 'settings', icon: Settings2, title: 'Settings', sub: ',', group: 'actions', run: () => emit('settings') },
  ]
  if (state.inbox) {
    cmds.splice(1, 0, { id: 'clear', icon: Trash2, title: 'Clear inbox ' + state.inbox, group: 'actions', run: () => z.clearInbox() })
    cmds.splice(1, 0, { id: 'wait', icon: Hourglass, title: 'Wait for next message in ' + state.inbox, sub: 'w', group: 'actions', run: () => z.waitForNext(state.inbox) })
  }
  return cmds
})

const inboxCmds = computed<Cmd[]>(() =>
  state.inboxes.map((ib) => ({
    id: 'inbox:' + ib.inbox, icon: Mail, title: ib.inbox, sub: ib.message_count + ' msg', group: 'inboxes',
    run: () => z.openInbox(ib.inbox),
  })),
)

const all = computed(() => [...base.value, ...inboxCmds.value])

const filtered = computed(() => {
  const term = q.value.trim().toLowerCase()
  if (!term) return all.value
  const list = all.value.filter((c) => (c.title + ' ' + (c.sub || '')).toLowerCase().includes(term))
  // direct "type an address" affordance — jump to (or create) any inbox
  if (term.includes('@') || /^[a-z0-9.\-_]+$/.test(term)) {
    list.unshift({
      id: 'open:' + term, icon: ArrowRight, title: 'Open inbox ' + maybeQualify(term), group: 'open',
      run: () => z.openInbox(maybeQualify(term)),
    })
  }
  return list
})

const groups = computed(() => {
  const order = ['open', 'actions', 'manage', 'inboxes']
  const map: Record<string, Cmd[]> = {}
  filtered.value.forEach((c) => { (map[c.group] ||= []).push(c) })
  return order.filter((g) => map[g]?.length).map((g) => ({ name: g, items: map[g]! }))
})

const flat = computed(() => groups.value.flatMap((g) => g.items))

function maybeQualify(term: string) {
  if (term.includes('@')) return term.toLowerCase()
  return `${term.toLowerCase()}@${state.config.domain || 'localhost'}`
}
function genAddress() {
  const addr = z.generateAddress()
  z.copy(addr, 'address generated + copied')
  z.openInbox(addr)
}

function runActive() {
  const cmd = flat.value[active.value]
  if (cmd) { cmd.run(); open.value = false }
}
function move(d: number) {
  const n = flat.value.length
  if (!n) return
  active.value = (active.value + d + n) % n
}

function show() { q.value = ''; active.value = 0; open.value = true; nextTick(() => inputEl.value?.focus()) }
function close() { open.value = false }

watch(filtered, () => { active.value = 0 })

defineExpose({ show, close, isOpen: () => open.value })
</script>

<template>
  <Dialog v-model:open="open">
    <DialogContent
      :show-close-button="false"
      class="top-[12vh] block w-[min(620px,92vw)] max-w-none translate-y-0 gap-0 overflow-hidden p-0 sm:max-w-none"
    >
      <DialogTitle class="sr-only">Command palette</DialogTitle>
      <DialogDescription class="sr-only">Jump to an inbox, generate an address, or run a command.</DialogDescription>

      <div class="flex h-12 items-center gap-2.5 border-b px-3.5">
        <Search class="size-4 shrink-0 text-muted-foreground" />
        <input
          ref="inputEl"
          v-model="q"
          class="h-full flex-1 bg-transparent text-[15px] outline-none placeholder:text-muted-foreground"
          placeholder="jump to inbox, generate an address, or run a command…"
          @keydown.down.prevent="move(1)"
          @keydown.up.prevent="move(-1)"
          @keydown.enter.prevent="runActive"
        >
      </div>

      <div class="max-h-[50vh] overflow-y-auto p-1.5">
        <div v-if="!flat.length" class="py-6 text-center text-sm text-muted-foreground">No matches.</div>
        <template v-for="g in groups" :key="g.name">
          <div class="px-2 pb-1 pt-2.5 text-[10px] font-medium uppercase tracking-wide text-muted-foreground">{{ g.name }}</div>
          <button
            v-for="c in g.items"
            :key="c.id"
            class="flex w-full items-center gap-2.5 rounded-md px-2.5 py-2 text-left text-sm text-muted-foreground [&_svg]:size-4 [&_svg]:shrink-0 [&_svg]:text-muted-foreground"
            :class="flat[active]?.id === c.id ? 'bg-accent text-foreground [&_svg]:text-foreground' : ''"
            @mouseenter="active = flat.findIndex((x) => x.id === c.id)"
            @click="c.run(); open = false"
          >
            <component :is="c.icon" />
            <span :class="c.group === 'inboxes' ? 'font-mono' : ''">{{ c.title }}</span>
            <span v-if="c.sub" class="ml-auto font-mono text-[11px] text-muted-foreground">{{ c.sub }}</span>
          </button>
        </template>
      </div>

      <div class="flex items-center gap-4 border-t px-3.5 py-2 text-[11px] text-muted-foreground">
        <span class="inline-flex items-center gap-1.5"><ArrowUpDown class="size-3" /> navigate</span>
        <span class="inline-flex items-center gap-1.5"><CornerDownLeft class="size-3" /> select</span>
        <span class="inline-flex items-center gap-1.5"><kbd>esc</kbd> close</span>
      </div>
    </DialogContent>
  </Dialog>
</template>
