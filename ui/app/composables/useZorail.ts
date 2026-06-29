import { reactive } from 'vue'

export interface InboxSummary { inbox: string; message_count: number; last_received: string }
export interface MsgMeta {
  id: string; inbox: string; from: string; env_from: string; to: string[]
  subject: string; date: string; received_at: string; size: number
}
export interface Attachment { id: string; filename: string; content_type: string; size: number }
export interface Extracted { codes: string[]; links: string[]; unsubscribe: string[] }
export interface Spam { score: number; label: string; reasons: string[] }
export interface FullMsg extends MsgMeta {
  text: string; html: string; headers: Record<string, string>
  attachments: Attachment[]; extracted: Extracted; spam: Spam
}
export interface ServerConfig { version: string; domain: string; allowed_domains: string[]; auth_required: boolean }
export interface Toast { id: number; msg: string; kind: 'ok' | 'err' }

const LS = {
  token: 'zorail_token', theme: 'zorail_theme', accent: 'zorail_accent',
  images: 'zorail_images', read: 'zorail_read', pins: 'zorail_pins', auto: 'zorail_auto',
}

const ACCENTS: Record<string, string> = {
  sky: 'oklch(0.787 0.128 230.318)',
  emerald: 'oklch(0.792 0.153 166.95)',
  amber: 'oklch(0.828 0.165 84.429)',
  coral: 'oklch(0.704 0.177 14.75)',
  violet: 'oklch(0.78 0.148 286.067)',
  magenta: 'oklch(0.78 0.15 330)',
  mono: 'oklch(0.92 0 0)',
}

const state = reactive({
  config: { version: '', domain: 'localhost', allowed_domains: [] as string[], auth_required: false } as ServerConfig,
  inboxes: [] as InboxSummary[],
  inbox: '',
  messages: [] as MsgMeta[],
  current: null as FullMsg | null,
  rawCache: '',
  token: '',
  error: '',
  autoRefresh: true,
  theme: 'dark' as 'dark' | 'light',
  accent: 'sky',
  loadImages: false,
  read: new Set<string>(),
  pins: new Set<string>(),
  toasts: [] as Toast[],
  loadingInbox: false,
  loadingMsg: false,
  searchQuery: '',
  searching: false,
})

let pollTimer: ReturnType<typeof setInterval> | null = null
let toastSeq = 0
const enc = encodeURIComponent

function headers(): Record<string, string> {
  return state.token ? { Authorization: `Bearer ${state.token}` } : {}
}
async function call<T>(path: string, opts: Record<string, unknown> = {}): Promise<T> {
  return await $fetch<T>(`/api${path}`, { ...opts, headers: { ...(opts.headers as object), ...headers() } } as never)
}
async function guard(fn: () => Promise<void>) {
  try { state.error = ''; await fn() }
  catch (e: unknown) {
    const err = e as { statusCode?: number; message?: string }
    state.error = err?.statusCode === 401 ? 'unauthorized — set your API token (press ,)' : (err?.message || 'request failed')
  }
}

function toast(msg: string, kind: 'ok' | 'err' = 'ok') {
  const id = ++toastSeq
  state.toasts.push({ id, msg, kind })
  setTimeout(() => { state.toasts = state.toasts.filter((t) => t.id !== id) }, 2600)
}

async function copy(v: string, note = 'copied') {
  if (import.meta.client && navigator.clipboard) {
    try { await navigator.clipboard.writeText(v); toast(note) } catch { toast('copy failed', 'err') }
  }
}

// ---- data ----
async function loadConfig() {
  try { state.config = await call<ServerConfig>('/config') } catch { /* keep defaults */ }
}
async function loadInboxes() {
  await guard(async () => { state.inboxes = (await call<InboxSummary[]>('/inboxes')) || [] })
}
async function loadMessages() {
  if (!state.inbox) return
  state.loadingInbox = true
  await guard(async () => { state.messages = (await call<MsgMeta[]>(`/inboxes/${enc(state.inbox)}/messages`)) || [] })
  state.loadingInbox = false
}
async function openInbox(inbox: string) {
  state.inbox = inbox.trim().toLowerCase()
  state.searchQuery = ''
  state.current = null
  pushRecent(state.inbox)
  await loadMessages()
  await loadInboxes()
}
async function openMessage(id: string) {
  state.loadingMsg = true
  await guard(async () => {
    state.current = await call<FullMsg>(`/messages/${enc(id)}`)
    markRead(id)
    state.rawCache = ''
    call<string>(`/messages/${enc(id)}/raw`).then((r) => (state.rawCache = r)).catch(() => {})
  })
  state.loadingMsg = false
}
async function deleteMessage(id: string) {
  await guard(async () => {
    await call(`/messages/${enc(id)}`, { method: 'DELETE' })
    if (state.current?.id === id) state.current = null
    await loadMessages(); await loadInboxes(); toast('message deleted')
  })
}
async function clearInbox() {
  if (!state.inbox) return
  await guard(async () => {
    const res = await call<{ deleted: number }>(`/inboxes/${enc(state.inbox)}`, { method: 'DELETE' })
    state.current = null; state.messages = []
    await loadInboxes(); toast(`cleared ${res?.deleted ?? 0} messages`)
  })
}
async function search(q: string) {
  state.searchQuery = q
  if (!q.trim()) { await loadMessages(); return }
  state.searching = true
  await guard(async () => { state.messages = (await call<MsgMeta[]>(`/search?q=${enc(q)}`)) || [] })
  state.searching = false
}

function attachmentURL(msgId: string, attId: string): string {
  const t = state.token ? `?token=${enc(state.token)}` : ''
  return `/api/messages/${enc(msgId)}/attachments/${enc(attId)}${t}`
}
function rawURL(id: string): string {
  const t = state.token ? `?token=${enc(state.token)}` : ''
  return `/api/messages/${enc(id)}/raw${t}`
}

// ---- disposable address generation ----
const ADJ = ['qa', 'test', 'dev', 'stage', 'demo', 'temp', 'probe', 'scratch', 'sandbox', 'check']
function generateAddress(): string {
  const a = ADJ[Math.floor(Math.random() * ADJ.length)]
  const n = Math.floor(1000 + Math.random() * 9000)
  const dom = state.config.domain || 'localhost'
  return `${a}-${n}@${dom}`
}

// ---- read tracking & pins (client-side, localStorage) ----
function markRead(id: string) {
  if (state.read.has(id)) return
  state.read.add(id)
  if (state.read.size > 1000) state.read = new Set([...state.read].slice(-800))
  persistSet(LS.read, state.read)
}
function isRead(id: string) { return state.read.has(id) }
function inboxUnread(inbox: string): number {
  // best-effort: only known for the open inbox's loaded messages
  if (inbox !== state.inbox) return 0
  return state.messages.filter((m) => !state.read.has(m.id)).length
}
function togglePin(inbox: string) {
  if (state.pins.has(inbox)) state.pins.delete(inbox); else state.pins.add(inbox)
  state.pins = new Set(state.pins)
  persistSet(LS.pins, state.pins)
}
function isPinned(inbox: string) { return state.pins.has(inbox) }

const recents: string[] = []
function pushRecent(inbox: string) {
  const i = recents.indexOf(inbox); if (i >= 0) recents.splice(i, 1)
  recents.unshift(inbox); recents.length = Math.min(recents.length, 8)
}
function recentInboxes(): string[] { return [...recents] }

// ---- settings ----
function applyTheme() {
  if (import.meta.client) document.documentElement.setAttribute('data-theme', state.theme)
}
function applyAccent() {
  if (import.meta.client) document.documentElement.style.setProperty('--accent-color', ACCENTS[state.accent] || ACCENTS.sky!)
}
function setTheme(t: 'dark' | 'light') { state.theme = t; localStorage.setItem(LS.theme, t); applyTheme() }
function setAccent(a: string) { state.accent = a; localStorage.setItem(LS.accent, a); applyAccent() }
function setToken(t: string) { state.token = t.trim(); localStorage.setItem(LS.token, state.token); loadConfig(); loadInboxes() }
function toggleImages() { state.loadImages = !state.loadImages; localStorage.setItem(LS.images, String(state.loadImages)) }
function toggleAuto() { state.autoRefresh = !state.autoRefresh; localStorage.setItem(LS.auto, String(state.autoRefresh)); startPolling() }
function accentList() { return Object.entries(ACCENTS).map(([k, v]) => ({ key: k, value: v })) }

function persistSet(key: string, set: Set<string>) {
  if (import.meta.client) localStorage.setItem(key, JSON.stringify([...set]))
}
function loadSet(key: string): Set<string> {
  if (!import.meta.client) return new Set()
  try { return new Set(JSON.parse(localStorage.getItem(key) || '[]')) } catch { return new Set() }
}

// ---- polling ----
function startPolling() {
  stopPolling()
  if (!state.autoRefresh) return
  pollTimer = setInterval(() => { loadInboxes(); if (state.inbox && !state.searchQuery) loadMessages() }, 8000)
}
function stopPolling() { if (pollTimer) clearInterval(pollTimer); pollTimer = null }

function init() {
  if (import.meta.client) {
    state.token = localStorage.getItem(LS.token) || ''
    state.theme = (localStorage.getItem(LS.theme) as 'dark' | 'light') || 'dark'
    state.accent = localStorage.getItem(LS.accent) || 'sky'
    state.loadImages = localStorage.getItem(LS.images) === 'true'
    state.autoRefresh = localStorage.getItem(LS.auto) !== 'false'
    state.read = loadSet(LS.read)
    state.pins = loadSet(LS.pins)
    applyTheme(); applyAccent()
  }
  loadConfig(); loadInboxes(); startPolling()
}

export function useZorail() {
  return {
    state, init,
    loadConfig, loadInboxes, loadMessages, openInbox, openMessage, deleteMessage, clearInbox, search,
    attachmentURL, rawURL, generateAddress,
    markRead, isRead, inboxUnread, togglePin, isPinned, recentInboxes,
    setTheme, setAccent, setToken, toggleImages, toggleAuto, accentList,
    toast, copy, startPolling, stopPolling,
  }
}
