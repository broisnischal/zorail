<script setup lang="ts">
import { computed, ref, watch, nextTick } from 'vue'
import {
  Copy, Trash2, Download, Image, ImageOff, ShieldAlert, Paperclip, Ellipsis,
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
watch(() => props.message.id, () => { tab.value = 'rendered' })
watch(tab, (t) => { if (t === 'rendered') nextTick(resize) })

function downloadEml() {
  if (import.meta.client) window.open(z.rawURL(props.message.id), '_blank')
}

const spamBadge = computed(() => {
  const l = props.message.spam.label
  if (l === 'high') return 'border-danger/30 bg-danger/10 text-danger'
  if (l === 'medium' || l === 'low') return 'border-warn/30 bg-warn/10 text-warn'
  return 'border-ok/30 bg-ok/10 text-ok'
})
</script>

<template>
  <article class="flex flex-col overflow-y-auto animate-in fade-in duration-200">
    <header class="flex items-start gap-3 px-6 pt-6">
      <div class="min-w-0 flex-1">
        <h1 class="text-[19px] font-semibold leading-snug tracking-tight">{{ message.subject || '(no subject)' }}</h1>
        <div class="mt-1.5 flex flex-wrap items-center gap-x-2 gap-y-1 text-[12.5px] text-muted-foreground">
          <span v-if="from.name" class="font-medium text-foreground">{{ from.name }}</span>
          <span class="truncate font-mono">{{ from.email }}</span>
          <span class="text-muted-foreground/40">·</span>
          <span :title="absTime(message.received_at)">{{ relTime(message.received_at) }} ago</span>
          <span
            v-if="message.spam.label !== 'none'"
            :class="['ml-0.5 inline-flex items-center gap-1 rounded-full border px-2 py-0.5 text-[11px]', spamBadge]"
            :title="message.spam.reasons.join(' · ')"
          ><ShieldAlert class="size-3" /> spam {{ message.spam.score }}</span>
        </div>
        <div class="mt-1 truncate font-mono text-[11.5px] text-muted-foreground/70">to {{ (message.to || []).join(', ') || message.inbox }}</div>
      </div>

      <Menu align="end">
        <template #trigger>
          <button
            class="inline-flex size-8 shrink-0 items-center justify-center rounded-md text-muted-foreground transition-colors hover:bg-accent hover:text-foreground"
            title="More actions"
          ><Ellipsis class="size-4" /></button>
        </template>
        <MenuItem v-if="from.email" @click="z.copy(from.email, 'address copied')"><Copy /> Copy sender</MenuItem>
        <MenuItem v-if="hasRemoteImages" @click="z.toggleImages()">
          <component :is="state.loadImages ? ImageOff : Image" /> {{ state.loadImages ? 'Block remote images' : 'Load remote images' }}
        </MenuItem>
        <MenuItem @click="downloadEml"><Download /> Download .eml</MenuItem>
        <Separator class="my-1" />
        <MenuItem danger @click="emit('delete', message.id)"><Trash2 /> Delete message</MenuItem>
      </Menu>
    </header>

    <!-- hero: the one-time code is the job-to-be-done, so it leads -->
    <div v-if="message.extracted.codes.length" class="px-6 pt-5">
      <button
        class="group flex w-full items-center justify-between gap-4 rounded-xl border bg-muted/40 px-5 py-4 text-left transition-colors hover:border-border-hover"
        @click="z.copy(message.extracted.codes[0]!, 'code copied')"
      >
        <div class="min-w-0">
          <div class="text-[10px] uppercase tracking-wide text-muted-foreground">Verification code</div>
          <div class="mt-1 truncate font-mono text-2xl font-semibold tracking-[0.18em] text-foreground">{{ message.extracted.codes[0] }}</div>
        </div>
        <span class="inline-flex shrink-0 items-center gap-1.5 rounded-lg border bg-background px-3 py-2 text-[12px] text-muted-foreground transition-colors group-hover:text-foreground">
          <Copy class="size-3.5" /> Copy
        </span>
      </button>
      <div v-if="message.extracted.codes.length > 1" class="mt-2 flex flex-wrap gap-1.5">
        <button
          v-for="c in message.extracted.codes.slice(1)" :key="c"
          class="rounded-md border bg-muted px-2.5 py-1 font-mono text-[12px] text-muted-foreground transition-colors hover:border-border-hover hover:text-foreground"
          title="copy" @click="z.copy(c, 'code copied')"
        >{{ c }}</button>
      </div>
    </div>

    <!-- links / unsubscribe — quiet, only when present -->
    <div
      v-if="message.extracted.links.length || message.extracted.unsubscribe.length"
      class="grid gap-2.5 px-6 pt-4"
    >
      <div v-if="message.extracted.links.length" class="flex items-start gap-3">
        <span class="w-12 shrink-0 pt-1 text-[10px] uppercase tracking-wide text-muted-foreground">links</span>
        <div class="flex min-w-0 flex-wrap gap-1.5">
          <a
            v-for="l in message.extracted.links" :key="l" :href="l" target="_blank" rel="noopener noreferrer" :title="l"
            class="rounded-md border bg-muted px-2.5 py-1 font-mono text-xs text-muted-foreground transition-colors hover:border-border-hover hover:text-foreground"
          >{{ hostOf(l) }}</a>
        </div>
      </div>
      <div v-if="message.extracted.unsubscribe.length" class="flex items-start gap-3">
        <span class="w-12 shrink-0 pt-1 text-[10px] uppercase tracking-wide text-muted-foreground">unsub</span>
        <div class="flex min-w-0 flex-wrap gap-1.5">
          <a
            v-for="u in message.extracted.unsubscribe" :key="u" :href="u" target="_blank" rel="noopener noreferrer" :title="u"
            class="rounded-md border bg-muted px-2.5 py-1 font-mono text-xs text-muted-foreground transition-colors hover:border-border-hover hover:text-foreground"
          >{{ hostOf(u) }}</a>
        </div>
      </div>
    </div>

    <div class="h-5" />

    <!-- tabs -->
    <Tabs v-model="tab" class="gap-0">
      <TabsList class="sticky top-0 z-10 w-full justify-start rounded-none border-y border-border-subtle bg-background px-4">
        <TabsTrigger value="rendered">rendered</TabsTrigger>
        <TabsTrigger value="text">text</TabsTrigger>
        <TabsTrigger value="headers">headers <span class="text-[10px] text-muted-foreground">{{ headerEntries.length }}</span></TabsTrigger>
        <TabsTrigger value="raw">raw</TabsTrigger>
        <TabsTrigger value="atts" :disabled="!atts.length">attachments <span v-if="atts.length" class="text-[10px] text-muted-foreground">{{ atts.length }}</span></TabsTrigger>
      </TabsList>

      <TabsContent value="rendered" class="p-6">
        <div
          v-if="hasRemoteImages && !state.loadImages"
          class="mb-3 flex items-center justify-between gap-2.5 rounded-md border bg-muted px-3 py-2 text-xs text-muted-foreground"
        >
          <span class="inline-flex items-center gap-1.5"><ImageOff class="size-3.5" /> Remote images blocked (privacy / tracking protection).</span>
          <Button variant="outline" size="sm" @click="z.toggleImages()"><Image /> Load images</Button>
        </div>
        <div class="overflow-hidden rounded-xl border bg-white">
          <iframe
            ref="iframe"
            class="block min-h-[200px] w-full border-0 bg-white"
            sandbox="allow-same-origin allow-popups allow-popups-to-escape-sandbox"
            :srcdoc="srcdoc"
            title="message body"
            @load="onload"
          />
        </div>
      </TabsContent>

      <TabsContent value="text" class="p-6">
        <pre class="m-0 max-h-[70vh] overflow-auto whitespace-pre-wrap break-words rounded-xl border bg-muted p-4 font-mono text-xs text-muted-foreground">{{ message.text || '(no plain-text part)' }}</pre>
      </TabsContent>

      <TabsContent value="headers" class="p-6">
        <div class="grid gap-1">
          <div v-for="[k, v] in headerEntries" :key="k" class="flex gap-2.5 font-mono text-[11.5px]">
            <span class="w-[150px] shrink-0 text-muted-foreground">{{ k }}</span>
            <span class="break-all text-muted-foreground">{{ v }}</span>
          </div>
        </div>
      </TabsContent>

      <TabsContent value="raw" class="p-6">
        <pre class="m-0 max-h-[70vh] overflow-auto whitespace-pre-wrap break-words rounded-xl border bg-muted p-4 font-mono text-xs text-muted-foreground">{{ raw || '(loading…)' }}</pre>
      </TabsContent>

      <TabsContent value="atts" class="p-6">
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
