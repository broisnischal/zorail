<script setup lang="ts">
import { onMounted, onBeforeUnmount, ref, computed } from 'vue'

const z = useZorail()
const { state } = z

const palette = ref<{ show: () => void; isOpen: () => boolean } | null>(null)
const settings = ref<{ open: () => void } | null>(null)
const searchEl = ref<HTMLInputElement | null>(null)
const sel = ref(-1) // keyboard cursor into messages

const sortedInboxes = computed(() =>
  [...state.inboxes].sort((a, b) => Number(z.isPinned(b.inbox)) - Number(z.isPinned(a.inbox))),
)

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
    case 'g': e.preventDefault(); { const a = z.generateAddress(); z.copy(a, 'address generated + copied'); z.openInbox(a) } break
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
  <div class="app">
    <header class="header">
      <div class="brand">
        <span class="slash">./</span><span class="name">zorail</span><span class="alpha">alpha</span>
      </div>

      <button class="btn" style="min-width:240px;justify-content:flex-start;color:var(--fg-subtle)" @click="palette?.show()">
        <span class="slash mono">/</span>
        <span>jump to inbox, generate, command…</span>
        <span style="flex:1" />
        <span class="kbd">⌘K</span>
      </button>

      <span class="grow" />

      <div class="nav">
        <span class="live" :class="{ off: !state.autoRefresh }" :title="state.autoRefresh ? 'auto-refresh on' : 'paused'">
          <span class="dot" /><span class="lbl">{{ state.autoRefresh ? 'live' : 'paused' }}</span>
        </span>
        <button class="btn primary" @click="() => { const a = z.generateAddress(); z.copy(a, 'address generated + copied'); z.openInbox(a) }">
          <span>✦</span><span class="lbl">generate</span>
        </button>
        <button class="btn icon" title="Refresh (r)" @click="z.loadInboxes(); state.inbox && z.loadMessages()">⟳</button>
        <button class="btn icon" title="Settings (,)" @click="settings?.open()">⚙</button>
      </div>
    </header>

    <div v-if="state.error" class="banner">{{ state.error }}</div>

    <div class="cols">
      <!-- inboxes -->
      <section class="col">
        <div class="col-head">
          <span class="label">inboxes</span>
          <span class="count tnum">{{ state.inboxes.length || '' }}</span>
        </div>
        <div class="col-body">
          <ul v-if="sortedInboxes.length" class="rows">
            <li
              v-for="ib in sortedInboxes"
              :key="ib.inbox"
              class="row"
              :class="{ sel: ib.inbox === state.inbox }"
              @click="z.openInbox(ib.inbox)"
            >
              <div class="r1">
                <span class="k mono">{{ ib.inbox }}</span>
                <span class="t">{{ relTime(ib.last_received) }}</span>
              </div>
              <div class="meta-line">
                <button class="pin" :class="{ active: z.isPinned(ib.inbox) }" :title="z.isPinned(ib.inbox) ? 'unpin' : 'pin'" @click.stop="z.togglePin(ib.inbox)">★</button>
                <span class="count">{{ ib.message_count }} msg</span>
              </div>
            </li>
          </ul>
          <div v-else class="empty">
            <div class="big">📭</div>
            No inboxes yet.<br>Press <span class="kbd">⌘K</span> to generate a disposable address, then send mail to it.
          </div>
        </div>
      </section>

      <!-- messages -->
      <section class="col">
        <div class="col-head">
          <div class="ttl">
            <span class="label">{{ state.inbox ? 'inbox' : 'messages' }}</span>
            <span v-if="state.inbox" class="name">{{ state.inbox }}</span>
          </div>
          <div style="display:flex;gap:6px;align-items:center">
            <button v-if="state.inbox" class="btn icon" title="Copy address" @click="z.copy(state.inbox, 'address copied')">⧉</button>
            <button v-if="state.inbox" class="btn danger icon" title="Clear inbox" @click="z.clearInbox()">⌫</button>
          </div>
        </div>
        <div class="col-tools">
          <div class="field">
            <span class="slash">/</span>
            <input
              ref="searchEl"
              :value="state.searchQuery"
              placeholder="search this inbox + all mail…"
              @input="z.search(($event.target as HTMLInputElement).value)"
            />
            <span v-if="state.searching" class="spinner" />
          </div>
        </div>
        <div class="col-body">
          <template v-if="state.loadingInbox && !state.messages.length">
            <div v-for="i in 6" :key="i" class="skel"><div class="bar" style="width:60%" /><div class="bar" style="width:85%" /></div>
          </template>
          <ul v-else-if="state.messages.length" class="rows">
            <li
              v-for="(m, i) in state.messages"
              :key="m.id"
              class="row"
              :class="{ sel: m.id === state.current?.id, unread: !z.isRead(m.id) }"
              @click="openAt(i)"
            >
              <div class="r1">
                <span class="k">{{ parseFrom(m.from || m.env_from).name || parseFrom(m.from || m.env_from).email || '(unknown)' }}</span>
                <span class="t">{{ relTime(m.received_at) }}</span>
              </div>
              <div class="r2">{{ m.subject || '(no subject)' }}</div>
              <div class="meta-line">
                <span v-if="!z.isRead(m.id)" class="udot" />
                <span v-if="state.searchQuery" class="count mono">{{ m.inbox }}</span>
              </div>
            </li>
          </ul>
          <div v-else class="empty">{{ state.searchQuery ? 'no matches' : (state.inbox ? 'this inbox is empty' : 'select or generate an inbox') }}</div>
        </div>
      </section>

      <!-- reader -->
      <section class="col">
        <div v-if="state.loadingMsg && !state.current" class="empty center"><span class="spinner" /></div>
        <MessageReader v-else-if="state.current" :message="state.current" :raw="state.rawCache" @delete="z.deleteMessage" />
        <div v-else class="empty center">
          <div class="big">✉</div>
          Select a message to read it.<br>
          <span class="muted">Codes, links and unsubscribe targets are detected automatically.</span>
        </div>
      </section>
    </div>

    <CommandPalette ref="palette" @settings="settings?.open()" />
    <SettingsDialog ref="settings" />

    <div class="toasts">
      <div v-for="t in state.toasts" :key="t.id" class="toast anim-slide" :class="{ err: t.kind === 'err' }">
        <span class="ico">{{ t.kind === 'err' ? '✕' : '✓' }}</span><span>{{ t.msg }}</span>
      </div>
    </div>
  </div>
</template>
