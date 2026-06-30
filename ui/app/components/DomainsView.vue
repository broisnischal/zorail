<script setup lang="ts">
import { computed } from 'vue'
import { Globe, Copy, CheckCircle2 } from 'lucide-vue-next'
import { Badge } from '@/components/ui/badge'

const z = useZorail()
const { state } = z

const domains = computed(() => {
  const ad = state.config.allowed_domains || []
  return ad.length ? ad : (state.config.domain ? [state.config.domain] : [])
})
</script>

<template>
  <AppPage title="Domains" subtitle="Recipient domains this instance accepts mail for.">
    <div v-if="domains.length" class="grid gap-2">
      <div v-for="d in domains" :key="d" class="flex items-center gap-3 rounded-xl border p-4">
        <span class="flex size-8 shrink-0 items-center justify-center rounded-lg border bg-muted/50"><Globe class="size-4 text-muted-foreground" /></span>
        <span class="flex-1 truncate font-mono text-[13px]">{{ d }}</span>
        <Badge variant="secondary" class="gap-1"><CheckCircle2 class="size-3 text-ok" /> accepting</Badge>
        <button class="text-muted-foreground hover:text-foreground" title="copy" @click="z.copy(d, 'domain copied')"><Copy class="size-4" /></button>
      </div>
    </div>
    <EmptyState v-else :icon="Globe" title="Accepting all domains" description="No domain allow-list is set, so every recipient domain is accepted. Set ZORAIL_ALLOWED_DOMAINS in production." />

    <div class="mt-6 grid gap-3 rounded-xl border p-5 text-[12.5px] leading-relaxed text-muted-foreground">
      <p class="font-medium text-foreground">Routing mail to Zorail</p>
      <p><span class="font-medium text-foreground">Own MX:</span> point your domain's <span class="font-mono">MX</span> record at this host (public IP, port 25 reachable, DNS-only).</p>
      <p><span class="font-medium text-foreground">No port 25:</span> let Cloudflare Email Routing be the MX and have an Email Worker <span class="font-mono">POST</span> each message to <span class="font-mono">/api/ingest</span>.</p>
      <p>Domains are configured via environment variables and are read-only here.</p>
    </div>
  </AppPage>
</template>
