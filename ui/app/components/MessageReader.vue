<script setup lang="ts">
import { computed, ref, watch, nextTick } from 'vue'
import type { FullMsg } from '~/composables/useZorail'

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

function downloadEml() {
  if (import.meta.client) window.open(z.rawURL(props.message.id), '_blank')
}

function spamClass(label: string) {
  if (label === 'high') return 'danger'
  if (label === 'medium' || label === 'low') return 'warn'
  return 'ok'
}
</script>

<template>
  <article class="reader anim-fade">
    <div class="reader-head">
      <h1>{{ message.subject || '(no subject)' }}</h1>
      <dl class="meta">
        <div class="m">
          <dt>from</dt>
          <dd><span v-if="from.name" class="nm">{{ from.name }}</span> {{ from.email }}</dd>
        </div>
        <div class="m"><dt>to</dt><dd>{{ (message.to || []).join(', ') || message.inbox }}</dd></div>
        <div class="m"><dt>date</dt><dd :title="absTime(message.received_at)">{{ absTime(message.received_at) }} · {{ relTime(message.received_at) }} ago</dd></div>
      </dl>
    </div>

    <!-- toolbar -->
    <div class="toolbar">
      <button v-if="message.extracted.codes.length" class="btn primary" @click="z.copy(message.extracted.codes[0]!, 'code copied')">
        copy code · {{ message.extracted.codes[0] }}
      </button>
      <span v-if="from.email" class="badge" :title="'reply-to ' + from.email">
        <button class="mono" style="color:inherit" @click="z.copy(from.email, 'address copied')">{{ from.email }}</button>
      </span>
      <span class="badge" :class="spamClass(message.spam.label)" :title="message.spam.reasons.join(' · ') || 'no spam signals'">
        <span class="dot" /> spam {{ message.spam.score }}
      </span>
      <span class="sp" />
      <button class="btn" :class="{ on: state.loadImages }" v-if="hasRemoteImages" @click="z.toggleImages()">
        {{ state.loadImages ? 'images on' : 'images off' }}
      </button>
      <button class="btn" @click="downloadEml">raw ↗</button>
      <button class="btn danger" @click="emit('delete', message.id)">delete</button>
    </div>

    <!-- extracted -->
    <div v-if="message.extracted.codes.length || message.extracted.links.length || message.extracted.unsubscribe.length" class="section detected">
      <div v-if="message.extracted.codes.length" class="grp">
        <span class="label">codes</span>
        <div class="chips">
          <button v-for="c in message.extracted.codes" :key="c" class="chip code clickable" title="copy" @click="z.copy(c, 'code copied')">{{ c }}</button>
        </div>
      </div>
      <div v-if="message.extracted.links.length" class="grp">
        <span class="label">links</span>
        <div class="chips">
          <a v-for="l in message.extracted.links" :key="l" class="chip clickable" :href="l" target="_blank" rel="noopener noreferrer" :title="l">{{ hostOf(l) }}</a>
        </div>
      </div>
      <div v-if="message.extracted.unsubscribe.length" class="grp">
        <span class="label">unsub</span>
        <div class="chips">
          <a v-for="u in message.extracted.unsubscribe" :key="u" class="chip clickable" :href="u" target="_blank" rel="noopener noreferrer" :title="u">{{ hostOf(u) }}</a>
        </div>
      </div>
    </div>

    <!-- tabs -->
    <div class="tabs">
      <button class="tab" :class="{ on: tab === 'rendered' }" @click="tab = 'rendered'; nextTick(resize)">rendered</button>
      <button class="tab" :class="{ on: tab === 'text' }" @click="tab = 'text'">text</button>
      <button class="tab" :class="{ on: tab === 'headers' }" @click="tab = 'headers'">headers<span class="cbadge">{{ headerEntries.length }}</span></button>
      <button class="tab" :class="{ on: tab === 'raw' }" @click="tab = 'raw'">raw</button>
      <button class="tab" :class="{ on: tab === 'atts' }" @click="tab = 'atts'" :disabled="!atts.length">attachments<span v-if="atts.length" class="cbadge">{{ atts.length }}</span></button>
    </div>

    <div class="body-wrap">
      <template v-if="tab === 'rendered'">
        <div v-if="hasRemoteImages && !state.loadImages" class="images-bar">
          <span>🔒 Remote images blocked (privacy / tracking protection).</span>
          <button class="btn" @click="z.toggleImages()">load images</button>
        </div>
        <div class="html-card">
          <iframe
            ref="iframe"
            class="body-html"
            sandbox="allow-same-origin allow-popups allow-popups-to-escape-sandbox"
            :srcdoc="srcdoc"
            title="message body"
            @load="onload"
          />
        </div>
      </template>

      <pre v-else-if="tab === 'text'" class="body">{{ message.text || '(no plain-text part)' }}</pre>

      <div v-else-if="tab === 'headers'" class="kv">
        <div v-for="[k, v] in headerEntries" :key="k" class="m">
          <span class="key">{{ k }}</span><span class="val">{{ v }}</span>
        </div>
      </div>

      <pre v-else-if="tab === 'raw'" class="body">{{ raw || '(loading…)' }}</pre>

      <div v-else class="atts chips">
        <a v-for="a in atts" :key="a.id" class="chip clickable" :href="z.attachmentURL(message.id, a.id)">
          📎 {{ a.filename || 'attachment' }} · {{ fmtSize(a.size) }}
        </a>
      </div>
    </div>
  </article>
</template>
