import { reactive } from 'vue'
import { toast as sonnerToast } from 'vue-sonner'

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

export interface User { id: string; email: string; created_at: string }
export type Scope = 'read' | 'manage' | 'admin'
export interface ApiKey {
  id: string; user_id: string; name: string; scopes: Scope[]
  inbox_prefix: string; created_at: string; last_used_at?: string
  secret?: string // present only on the create response
}
export type AddressType = 'disposable' | 'reserved' | 'forward'
export interface Address {
  address: string; type: AddressType; owner_user_id?: string; expires_at?: string
  forward_to?: string[]; forward_enabled: boolean; created_at: string
}

const LS = {
  token: 'zorail_token', theme: 'zorail_theme', accent: 'zorail_accent',
  images: 'zorail_images', read: 'zorail_read', pins: 'zorail_pins', auto: 'zorail_auto',
  user: 'zorail_user',
}

// 'neutral' is the default and intentionally monochrome — it sets no
// --accent-color, so the theme-aware CSS fallback (near-white on dark,
// near-black on light) wins. The colored options are opt-in.
const ACCENTS: Record<string, string> = {
  neutral: '',
  sky: 'oklch(0.787 0.128 230.318)',
  emerald: 'oklch(0.792 0.153 166.95)',
  amber: 'oklch(0.828 0.165 84.429)',
  coral: 'oklch(0.704 0.177 14.75)',
  violet: 'oklch(0.78 0.148 286.067)',
  magenta: 'oklch(0.78 0.15 330)',
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
  accent: 'neutral',
  loadImages: false,
  read: new Set<string>(),
  pins: new Set<string>(),
  loadingInbox: false,
  loadingMsg: false,
  searchQuery: '',
  searching: false,
  // multi-tenant
  user: null as User | null,
  keys: [] as ApiKey[],
  addresses: [] as Address[],
  loadingKeys: false,
  loadingAddresses: false,
  waiting: false,
  // first-run setup
  organization: '',
  needsSetup: false,
  setupChecked: false,
  // navigation (Resend-style sections)
  section: 'inboxes' as Section,
  // sign-in is optional after setup; this toggles the sign-in overlay
  signinOpen: false,
})

export type Section = 'inboxes' | 'addresses' | 'domains' | 'keys' | 'settings'

let pollTimer: ReturnType<typeof setInterval> | null = null
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
  if (kind === 'err') sonnerToast.error(msg)
  else sonnerToast.success(msg)
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
// sameIds reports whether two lists hold the same items in the same order.
// IDs are immutable, so this lets polling skip a reassignment (and the re-render
// flicker that comes with it) when nothing actually changed.
function sameIds<T extends { id: string }>(a: T[], b: T[]): boolean {
  if (a.length !== b.length) return false
  for (let i = 0; i < a.length; i++) if (a[i]!.id !== b[i]!.id) return false
  return true
}
function sameInboxes(a: InboxSummary[], b: InboxSummary[]): boolean {
  if (a.length !== b.length) return false
  for (let i = 0; i < a.length; i++) {
    if (a[i]!.inbox !== b[i]!.inbox || a[i]!.message_count !== b[i]!.message_count) return false
  }
  return true
}

async function loadInboxes() {
  await guard(async () => {
    const next = (await call<InboxSummary[]>('/inboxes')) || []
    if (!sameInboxes(state.inboxes, next)) state.inboxes = next
  })
}
async function loadMessages() {
  if (!state.inbox) return
  if (!state.messages.length) state.loadingInbox = true // skeleton only on first load, never on poll
  await guard(async () => {
    const next = (await call<MsgMeta[]>(`/inboxes/${enc(state.inbox)}/messages`)) || []
    if (!sameIds(state.messages, next)) state.messages = next
  })
  state.loadingInbox = false
}
// Routes own navigation now: openInbox/closeInbox/setSection just navigate, and
// the pages call enterInbox/leaveInbox to sync state to the URL.
const SECTION_PATH: Record<Section, string> = {
  inboxes: '/', addresses: '/addresses', domains: '/domains', keys: '/keys', settings: '/settings',
}
function openInbox(inbox: string) {
  return navigateTo('/inbox/' + encodeURIComponent(inbox.trim().toLowerCase()))
}
function closeInbox() {
  return navigateTo('/')
}
function setSection(s: Section) {
  return navigateTo(SECTION_PATH[s])
}

// enterInbox is called by the /inbox/[address] page to load a URL-addressed
// inbox; leaveInbox by the landing page to drop inbox state so polling stops.
async function enterInbox(inbox: string) {
  inbox = inbox.trim().toLowerCase()
  if (state.inbox === inbox && state.messages.length) { loadMessages(); return }
  state.inbox = inbox
  state.searchQuery = ''
  state.current = null
  pushRecent(state.inbox)
  await loadMessages()
  await loadInboxes()
}
function leaveInbox() {
  state.inbox = ''
  state.current = null
  state.messages = []
  state.searchQuery = ''
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
  if (!import.meta.client) return
  const v = ACCENTS[state.accent]
  if (v) document.documentElement.style.setProperty('--accent-color', v)
  else document.documentElement.style.removeProperty('--accent-color')
}
function setTheme(t: 'dark' | 'light') { state.theme = t; localStorage.setItem(LS.theme, t); applyTheme() }
function setAccent(a: string) { state.accent = a; localStorage.setItem(LS.accent, a); applyAccent() }
function setToken(t: string) { state.token = t.trim(); localStorage.setItem(LS.token, state.token); loadConfig(); loadInboxes() }
function toggleImages() { state.loadImages = !state.loadImages; localStorage.setItem(LS.images, String(state.loadImages)) }
function toggleAuto() { state.autoRefresh = !state.autoRefresh; localStorage.setItem(LS.auto, String(state.autoRefresh)); startPolling() }
function accentList() {
  // 'neutral' has no color of its own; show a swatch that reads as grayscale.
  return Object.entries(ACCENTS).map(([k, v]) => ({
    key: k,
    value: v,
    swatch: v || 'linear-gradient(135deg, oklch(0.97 0 0), oklch(0.45 0 0))',
  }))
}

function persistSet(key: string, set: Set<string>) {
  if (import.meta.client) localStorage.setItem(key, JSON.stringify([...set]))
}
function loadSet(key: string): Set<string> {
  if (!import.meta.client) return new Set()
  try { return new Set(JSON.parse(localStorage.getItem(key) || '[]')) } catch { return new Set() }
}

// ---- multi-tenant: auth, keys, addresses, forwarding ----

// errMsg pulls the server's {error} message out of an ofetch failure.
function errMsg(e: unknown, fallback = 'request failed'): string {
  const err = e as { data?: { error?: string }; statusCode?: number; message?: string }
  return err?.data?.error || err?.message || fallback
}

// ---- first-run setup ----
async function loadSetup() {
  try {
    const s = await call<{ needs_setup: boolean; organization: string }>('/setup')
    state.needsSetup = s.needs_setup
    state.organization = s.organization || ''
  } catch { /* leave defaults; dashboard still works in open mode */ }
  finally { state.setupChecked = true }
}

async function setup(organization: string, email: string, password: string): Promise<void> {
  const res = await call<{ user: User; token: string; organization: string }>('/setup', {
    method: 'POST', body: { organization, email, password },
  })
  setUser(res.user)
  state.token = res.token
  state.organization = res.organization
  state.needsSetup = false
  if (import.meta.client) localStorage.setItem(LS.token, res.token)
  await Promise.all([loadConfig(), loadInboxes()])
}

function setUser(u: User | null) {
  state.user = u
  if (import.meta.client) {
    if (u) localStorage.setItem(LS.user, JSON.stringify(u))
    else localStorage.removeItem(LS.user)
  }
}

// register creates an account then logs in, so the caller ends up authenticated.
async function register(email: string, password: string): Promise<void> {
  await call<User>('/auth/register', { method: 'POST', body: { email, password } })
  await login(email, password)
}

async function login(email: string, password: string): Promise<void> {
  const res = await call<{ user: User; token: string }>('/auth/login', { method: 'POST', body: { email, password } })
  setUser(res.user)
  // The login token is a manage-scoped key; store it as the active bearer.
  state.token = res.token
  if (import.meta.client) localStorage.setItem(LS.token, res.token)
  state.signinOpen = false
  await Promise.all([loadConfig(), loadInboxes(), loadAddresses(), loadKeys()])
}

function openSignIn() { state.signinOpen = true }
function closeSignIn() { state.signinOpen = false }

function logout() {
  setUser(null)
  state.token = ''
  state.keys = []
  state.addresses = []
  if (import.meta.client) localStorage.removeItem(LS.token)
  loadConfig(); loadInboxes()
}

const isAuthed = () => !!state.user

// API keys
async function loadKeys(): Promise<void> {
  if (!state.user) { state.keys = []; return }
  state.loadingKeys = true
  try { state.keys = (await call<ApiKey[]>('/keys')) || [] }
  catch { /* surfaced by the calling view if needed */ }
  finally { state.loadingKeys = false }
}
async function createKey(name: string, scopes: Scope[], inboxPrefix: string): Promise<ApiKey> {
  const k = await call<ApiKey>('/keys', { method: 'POST', body: { name, scopes, inbox_prefix: inboxPrefix } })
  await loadKeys()
  return k
}
async function deleteKey(id: string): Promise<void> {
  await call(`/keys/${enc(id)}`, { method: 'DELETE' })
  await loadKeys()
}

// Addresses (reserved / forwarding)
async function loadAddresses(): Promise<void> {
  if (!state.user) { state.addresses = []; return }
  state.loadingAddresses = true
  try { state.addresses = (await call<Address[]>('/addresses')) || [] }
  catch { /* surfaced by the calling view if needed */ }
  finally { state.loadingAddresses = false }
}
async function reserveAddress(body: { address?: string; prefix?: string; type: AddressType; forward_to?: string[] }): Promise<Address> {
  const a = await call<Address>('/addresses', { method: 'POST', body })
  await loadAddresses()
  return a
}
async function updateAddress(address: string, patch: { forward_to?: string[]; forward_enabled?: boolean }): Promise<void> {
  await call(`/addresses/${enc(address)}`, { method: 'PATCH', body: patch })
  await loadAddresses()
}
async function releaseAddress(address: string): Promise<void> {
  await call(`/addresses/${enc(address)}`, { method: 'DELETE' })
  await loadAddresses()
}

// Mailbox verification for forwarding destinations.
async function requestVerify(dest: string): Promise<{ dest: string; status: string; sent?: boolean; confirm_url?: string }> {
  return await call('/verify/request', { method: 'POST', body: { dest } })
}

// Long-poll: block until the next message lands in `inbox`, then open it.
async function waitForNext(inbox: string, timeoutSec = 25): Promise<boolean> {
  if (!inbox || state.waiting) return false
  state.waiting = true
  const after = state.inbox === inbox && state.messages[0] ? state.messages[0].id : ''
  try {
    const m = await call<FullMsg | null>(`/inboxes/${enc(inbox)}/wait?timeout=${timeoutSec}&after=${enc(after)}`)
    if (m && m.id) {
      if (state.inbox !== inbox) await openInbox(inbox)
      else await loadMessages()
      await openMessage(m.id)
      toast('new message arrived')
      return true
    }
    toast('no new mail within ' + timeoutSec + 's')
    return false
  } catch (e) {
    toast(errMsg(e, 'wait failed'), 'err')
    return false
  } finally {
    state.waiting = false
  }
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
    state.accent = localStorage.getItem(LS.accent) || 'neutral'
    state.loadImages = localStorage.getItem(LS.images) === 'true'
    state.autoRefresh = localStorage.getItem(LS.auto) !== 'false'
    state.read = loadSet(LS.read)
    state.pins = loadSet(LS.pins)
    try { state.user = JSON.parse(localStorage.getItem(LS.user) || 'null') } catch { state.user = null }
    applyTheme(); applyAccent()
  }
  loadSetup(); loadConfig(); loadInboxes(); startPolling()
  if (state.user) { loadAddresses(); loadKeys() }
}

export function useZorail() {
  return {
    state, init,
    loadConfig, loadInboxes, loadMessages, openInbox, closeInbox, enterInbox, leaveInbox, setSection, openMessage, deleteMessage, clearInbox, search,
    attachmentURL, rawURL, generateAddress,
    markRead, isRead, inboxUnread, togglePin, isPinned, recentInboxes,
    setTheme, setAccent, setToken, toggleImages, toggleAuto, accentList,
    toast, copy, startPolling, stopPolling,
    // multi-tenant
    loadSetup, setup,
    isAuthed, register, login, logout, openSignIn, closeSignIn,
    loadKeys, createKey, deleteKey,
    loadAddresses, reserveAddress, updateAddress, releaseAddress, requestVerify,
    waitForNext, errMsg,
  }
}
