<script setup lang="ts">
import { ref, computed, watch, nextTick } from 'vue'

const emit = defineEmits<{ (e: 'settings'): void }>()
const z = useZorail()
const { state } = z

const open = ref(false)
const q = ref('')
const active = ref(0)
const inputEl = ref<HTMLInputElement | null>(null)

interface Cmd { id: string; icon: string; title: string; sub?: string; group: string; run: () => void }

const base = computed<Cmd[]>(() => {
  const cmds: Cmd[] = [
    { id: 'gen', icon: '✦', title: 'Generate disposable address', sub: '@' + (state.config.domain || 'localhost'), group: 'actions', run: genAddress },
    { id: 'refresh', icon: '⟳', title: 'Refresh', sub: 'r', group: 'actions', run: () => { z.loadInboxes(); if (state.inbox) z.loadMessages(); close() } },
    { id: 'auto', icon: '◉', title: (state.autoRefresh ? 'Disable' : 'Enable') + ' auto-refresh', group: 'actions', run: () => { z.toggleAuto(); close() } },
    { id: 'images', icon: '🖼', title: (state.loadImages ? 'Block' : 'Load') + ' remote images', group: 'actions', run: () => { z.toggleImages(); close() } },
    { id: 'theme', icon: '◐', title: 'Toggle ' + (state.theme === 'dark' ? 'light' : 'dark') + ' theme', group: 'actions', run: () => { z.setTheme(state.theme === 'dark' ? 'light' : 'dark'); close() } },
    { id: 'settings', icon: '⚙', title: 'Settings', sub: ',', group: 'actions', run: () => { close(); emit('settings') } },
  ]
  if (state.inbox) cmds.splice(1, 0, { id: 'clear', icon: '⌫', title: 'Clear inbox ' + state.inbox, group: 'actions', run: () => { z.clearInbox(); close() } })
  return cmds
})

const inboxCmds = computed<Cmd[]>(() =>
  state.inboxes.map((ib) => ({
    id: 'inbox:' + ib.inbox, icon: '✉', title: ib.inbox, sub: ib.message_count + ' msg', group: 'inboxes',
    run: () => { z.openInbox(ib.inbox); close() },
  })),
)

const all = computed(() => [...base.value, ...inboxCmds.value])

const filtered = computed(() => {
  const term = q.value.trim().toLowerCase()
  if (!term) return all.value
  // direct "type an address" affordance
  const list = all.value.filter((c) => (c.title + ' ' + (c.sub || '')).toLowerCase().includes(term))
  if (term.includes('@') || /^[a-z0-9.\-_]+$/.test(term)) {
    list.unshift({
      id: 'open:' + term, icon: '→', title: 'Open inbox ' + maybeQualify(term), group: 'open',
      run: () => { z.openInbox(maybeQualify(term)); close() },
    })
  }
  return list
})

const groups = computed(() => {
  const order = ['open', 'actions', 'inboxes']
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
  close()
}

function show() { open.value = true; q.value = ''; active.value = 0; nextTick(() => inputEl.value?.focus()) }
function close() { open.value = false }
function move(d: number) {
  const n = flat.value.length
  if (!n) return
  active.value = (active.value + d + n) % n
}
function enter() { flat.value[active.value]?.run() }

watch(filtered, () => { active.value = 0 })

defineExpose({ show, close, isOpen: () => open.value })
</script>

<template>
  <Teleport to="body">
    <div v-if="open" class="overlay" @click.self="close" @keydown.esc="close">
      <div class="palette anim-scale" role="dialog" aria-label="Command palette">
        <div class="pinput">
          <span class="mono dim">⌘</span>
          <input
            ref="inputEl"
            v-model="q"
            placeholder="jump to inbox, generate an address, or run a command…"
            @keydown.down.prevent="move(1)"
            @keydown.up.prevent="move(-1)"
            @keydown.enter.prevent="enter"
            @keydown.esc="close"
          />
        </div>
        <div class="presults">
          <div v-if="!flat.length" class="empty">no matches</div>
          <template v-for="g in groups" :key="g.name">
            <div class="pgroup label">{{ g.name }}</div>
            <button
              v-for="c in g.items"
              :key="c.id"
              class="pitem"
              :class="{ active: flat[active]?.id === c.id }"
              @mouseenter="active = flat.findIndex((x) => x.id === c.id)"
              @click="c.run()"
            >
              <span class="ico">{{ c.icon }}</span>
              <span>{{ c.title }}</span>
              <span class="spacer" />
              <span v-if="c.sub" class="sub mono">{{ c.sub }}</span>
            </button>
          </template>
        </div>
        <div class="pfoot">
          <span class="hint"><span class="kbd">↑</span><span class="kbd">↓</span> navigate</span>
          <span class="hint"><span class="kbd">↵</span> select</span>
          <span class="hint"><span class="kbd">esc</span> close</span>
        </div>
      </div>
    </div>
  </Teleport>
</template>
