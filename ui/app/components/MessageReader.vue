<script setup lang="ts">
import { computed, ref, watch, nextTick } from 'vue'
import {
  Copy, Trash2, Download, Image, ImageOff, ShieldAlert, Paperclip, Ellipsis, Link2, FileCode2,
} from 'lucide-vue-next'
import type { FullMsg } from '~/composables/useZorail'
import { Button } from '@/components/ui/button'
import { Tabs, TabsList, TabsTrigger, TabsContent } from '@/components/ui/tabs'
import { Menu, MenuItem } from '@/components/ui/menu'
import { Separator } from '@/components/ui/separator'

const props = defineProps<{ message: FullMsg; raw: string }>()
const emit = defineEmits<{ (e: 'delete', id: string): void }>()

const z = useZorail()
const { state } = z

type Tab = 'rendered' | 'text' | 'headers' | 'raw' | 'atts'
const tab = ref<Tab>('rendered')
const iframe = ref<HTMLIFrameElement | null>(null)

const from = computed(() => parseFrom(props.message.from || props.message.env_from))
const hasHTML = computed(() => !!props.message.html)
const headerEntries = computed(() => Object.entries(props.message.headers || {}))
const atts = computed(() => props.message.attachments || [])

// Links/unsubscribe are collapsed by default — most mail has a pile of CDN /
// social / tracking URLs that just clutter the reader. The code (above) leads.
const codeList = computed(() => props.message.extracted.codes || [])
const links = computed(() => props.message.extracted.links || [])
const unsub = computed(() => props.message.extracted.unsubscribe || [])
const hasExtras = computed(() => links.value.length > 0 || unsub.value.length > 0)
const extrasOpen = ref(false)

// remote-image blocking: a CSP meta governs whether the iframe may load remote
// images. data: URIs are always allowed; remote http(s) only when the user opts in.
const imgPolicy = computed(() => (state.loadImages ? "img-src data: https: http:;" : "img-src data:;"))
const hasRemoteImages = computed(() => /<img[^>]+src=["']?https?:/i.test(props.message.html))

const srcdoc = computed(() => {
  const csp = `default-src 'none'; ${imgPolicy.value} style-src 'unsafe-inline'; font-src data:;`
  const head =
    `<meta charset="utf-8">` +
    `<meta http-equiv="Content-Security-Policy" content="${csp}">` +
    `<base target="_blank">` +
    `<style>html,body{margin:0}body{font:14px/1.6 -apple-system,system-ui,Segoe UI,Roboto,sans-serif;color:#111;background:#fff;padding:16px;word-wrap:break-word}a{color:#0b66c3}img{max-width:100%;height:auto}</style>`
  const body = hasHTML.value
    ? props.message.html
    : `<pre style="font:13px/1.6 ui-monospace,Menlo,monospace;white-space:pre-wrap;margin:0">${escapeHTML(props.message.text || '(no body)')}</pre>`
  return `<!doctype html><html><head>${head}</head><body>${body}</body></html>`
})

function escapeHTML(s: string) {
  return s.replace(/[&<>]/g, (c) => ({ '&': '&amp;', '<': '&lt;', '>': '&gt;' }[c] || c))
}

// Auto-size the iframe to its content. sandbox includes allow-same-origin (but
// NOT allow-scripts), so email JS can't run yet the parent can measure height.
function resize() {
  const el = iframe.value
  const doc = el?.contentDocument
  if (!el || !doc) return
  const h = Math.max(doc.documentElement?.scrollHeight || 0, doc.body?.scrollHeight || 0)
  if (h > 0) el.style.height = `${h + 8}px`
}
function onload() {
  resize()
  // images/late layout can change height after initial load
  setTimeout(resize, 120)
  setTimeout(resize, 500)
}
watch(() => props.message.id, () => { tab.value = 'rendered'; extrasOpen.value = false })
watch(tab, (t) => { if (t === 'rendered') nextTick(resize) })

// View the raw RFC 5322 source in a new tab.
function viewRaw() {
  if (import.meta.client) window.open(z.rawURL(props.message.id), '_blank')
}
// Save the message as an .eml file.
function downloadEml() {
  if (!import.meta.client) return
  const a = document.createElement('a')
  a.href = z.rawURL(props.message.id)
  a.download = `${props.message.id}.eml`
  a.click()
}

const spamBadge = computed(() => {
  const l = props.message.spam.label
  if (l === 'high') return 'border-danger/30 bg-danger/10 text-danger'
  if (l === 'medium' || l === 'low') return 'border-warn/30 bg-warn/10 text-warn'
  return 'border-ok/30 bg-ok/10 text-ok'
})
</script>

<template>
  <article class="flex h-full min-h-0 flex-col animate-in fade-in duration-200">
    <!-- compact header band: subject + meta + code chip + actions, one row -->
    <header class="flex items-start gap-3 border-b border-border-subtle px-5 py-3">
      <div class="min-w-0 flex-1">
        <h1 class="truncate text-[15px] font-semibold leading-tight tracking-tight">{{ message.subject || '(no subject)' }}</h1>
        <div class="mt-1 flex flex-wrap items-center gap-x-2 gap-y-0.5 text-[12px] text-muted-foreground">
          <span v-if="from.name" class="font-medium text-foreground">{{ from.name }}</span>
          <span class="truncate font-mono">{{ from.email }}</span>
          <span class="text-muted-foreground/40">·</span>
          <span :title="absTime(message.received_at)">{{ relTime(message.received_at) }} ago</span>
          <span class="text-muted-foreground/40">·</span>
          <span class="truncate font-mono text-muted-foreground/70">to {{ (message.to || []).join(', ') || message.inbox }}</span>
          <span
            v-if="message.spam.label !== 'none'"
            :class="['inline-flex items-center gap-1 rounded-full border px-1.5 text-[10.5px]', spamBadge]"
            :title="message.spam.reasons.join(' · ')"
          ><ShieldAlert class="size-3" /> spam {{ message.spam.score }}</span>
          <button
            v-if="hasExtras"
            class="inline-flex items-center gap-1 rounded-md px-1.5 py-0.5 text-[11.5px] transition-colors hover:bg-accent hover:text-foreground"
            :class="extrasOpen ? 'text-foreground' : ''"
            @click="extrasOpen = !extrasOpen"
          ><Link2 class="size-3" /> {{ links.length }} link{{ links.length === 1 ? '' : 's' }}<template v-if="unsub.length"> · {{ unsub.length }} unsub</template></button>
        </div>
      </div>

      <!-- verification code — compact, copyable chip -->
      <button
        v-if="codeList.length"
        class="group flex shrink-0 items-center gap-2 rounded-lg border bg-muted px-2.5 py-1.5 transition-colors hover:border-border-hover"
        title="Copy code" @click="z.copy(codeList[0]!, 'code copied')"
      >
        <span class="font-mono text-[15px] font-semibold tracking-[0.14em] text-foreground">{{ codeList[0] }}</span>
        <Copy class="size-3.5 text-muted-foreground transition-colors group-hover:text-foreground" />
      </button>

      <Menu align="end">
        <template #trigger>
          <button
            class="inline-flex size-8 shrink-0 items-center justify-center rounded-md text-muted-foreground transition-colors hover:bg-accent hover:text-foreground"
            title="More actions"
          ><Ellipsis class="size-4" /></button>
        </template>
        <MenuItem v-if="from.email" @click="z.copy(from.email, 'address copied')"><Copy /> Copy sender</MenuItem>
        <MenuItem v-if="codeList.length" @click="z.copy(codeList[0]!, 'code copied')"><Copy /> Copy code</MenuItem>
        <MenuItem v-if="hasRemoteImages" @click="z.toggleImages()">
          <component :is="state.loadImages ? ImageOff : Image" /> {{ state.loadImages ? 'Block remote images' : 'Load remote images' }}
        </MenuItem>
        <MenuItem @click="viewRaw"><FileCode2 /> View raw source</MenuItem>
        <MenuItem @click="downloadEml"><Download /> Download .eml</MenuItem>
        <Separator class="my-1" />
        <MenuItem danger @click="emit('delete', message.id)"><Trash2 /> Delete message</MenuItem>
      </Menu>
    </header>

    <!-- expanded links / unsubscribe — only when toggled, so it never steals space -->
    <div v-if="hasExtras && extrasOpen" class="flex flex-none flex-wrap gap-1.5 border-b border-border-subtle px-5 py-2.5">
      <a
        v-for="l in links" :key="l" :href="l" target="_blank" rel="noopener noreferrer" :title="l"
        class="inline-block max-w-[220px] truncate rounded-md border bg-muted px-2.5 py-1 font-mono text-[11.5px] text-muted-foreground transition-colors hover:border-border-hover hover:text-foreground"
      >{{ hostOf(l) }}</a>
      <a
        v-for="u in unsub" :key="u" :href="u" target="_blank" rel="noopener noreferrer" :title="u"
        class="inline-flex max-w-[220px] items-center gap-1 truncate rounded-md border border-warn/30 bg-warn/5 px-2.5 py-1 font-mono text-[11.5px] text-warn/90 transition-colors hover:border-warn/50"
      >unsub · {{ hostOf(u) }}</a>
    </div>

    <!-- tabs fill the remaining height -->
    <Tabs v-model="tab" class="flex min-h-0 flex-1 flex-col gap-0">
      <div class="flex-none border-b border-border-subtle px-5 py-2">
        <TabsList class="h-8">
          <TabsTrigger value="rendered" class="px-3">Rendered</TabsTrigger>
          <TabsTrigger value="text" class="px-3">Text</TabsTrigger>
          <TabsTrigger value="headers" class="px-3">Headers <span class="text-[10px] opacity-60">{{ headerEntries.length }}</span></TabsTrigger>
          <TabsTrigger value="raw" class="px-3">Raw</TabsTrigger>
          <TabsTrigger value="atts" :disabled="!atts.length" class="px-3">Attachments <span v-if="atts.length" class="text-[10px] opacity-60">{{ atts.length }}</span></TabsTrigger>
        </TabsList>
      </div>

      <!-- rendered: full-bleed white surface that fills the whole reader -->
      <TabsContent value="rendered" class="relative min-h-0 flex-1 overflow-auto bg-white p-0">
        <div
          v-if="hasRemoteImages && !state.loadImages"
          class="sticky top-0 z-10 flex items-center justify-between gap-2.5 border-b border-black/10 bg-white/95 px-4 py-2 text-xs text-black/60 backdrop-blur"
        >
          <span class="inline-flex items-center gap-1.5"><ImageOff class="size-3.5" /> Remote images blocked.</span>
          <button
            class="inline-flex items-center gap-1.5 rounded-md border border-black/15 px-2 py-1 text-black/70 transition-colors hover:bg-black/5"
            @click="z.toggleImages()"
          ><Image class="size-3.5" /> Load images</button>
        </div>
        <iframe
          ref="iframe"
          class="block w-full border-0 bg-white"
          sandbox="allow-same-origin allow-popups allow-popups-to-escape-sandbox"
          :srcdoc="srcdoc"
          title="message body"
          @load="onload"
        />
      </TabsContent>

      <TabsContent value="text" class="min-h-0 flex-1 overflow-auto p-5">
        <pre class="m-0 whitespace-pre-wrap break-words rounded-xl border bg-muted p-4 font-mono text-xs text-muted-foreground">{{ message.text || '(no plain-text part)' }}</pre>
      </TabsContent>

      <TabsContent value="headers" class="min-h-0 flex-1 overflow-auto p-5">
        <div class="grid gap-1">
          <div v-for="[k, v] in headerEntries" :key="k" class="flex gap-2.5 font-mono text-[11.5px]">
            <span class="w-[150px] shrink-0 text-muted-foreground">{{ k }}</span>
            <span class="break-all text-muted-foreground">{{ v }}</span>
          </div>
        </div>
      </TabsContent>

      <TabsContent value="raw" class="min-h-0 flex-1 overflow-auto p-5">
        <pre class="m-0 whitespace-pre-wrap break-words rounded-xl border bg-muted p-4 font-mono text-xs text-muted-foreground">{{ raw || '(loading…)' }}</pre>
      </TabsContent>

      <TabsContent value="atts" class="min-h-0 flex-1 overflow-auto p-5">
        <div class="flex flex-wrap gap-1.5">
          <a
            v-for="a in atts" :key="a.id" :href="z.attachmentURL(message.id, a.id)"
            class="inline-flex items-center gap-1.5 rounded-md border bg-muted px-2.5 py-1 font-mono text-xs text-muted-foreground transition-colors hover:border-border-hover hover:text-foreground"
          >
            <Paperclip class="size-3.5" /> {{ a.filename || 'attachment' }} · {{ fmtSize(a.size) }}
          </a>
        </div>
      </TabsContent>
    </Tabs>
  </article>
</template>
