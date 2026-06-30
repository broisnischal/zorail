<script setup lang="ts">
import { reactive, ref } from 'vue'
import { KeyRound, Plus, Trash2, Copy, Check, X, Loader2, LogIn } from 'lucide-vue-next'
import { Button } from '@/components/ui/button'
import { Input } from '@/components/ui/input'
import { Label } from '@/components/ui/label'
import { Badge } from '@/components/ui/badge'
import { ToggleGroup, ToggleGroupItem } from '@/components/ui/toggle-group'
import type { Scope } from '@/composables/useZorail'

const z = useZorail()
const { state } = z

const nk = reactive({ name: '', scopes: ['read'] as Scope[], prefix: '', busy: false, err: '' })
const lastSecret = ref('')
async function makeKey() {
  nk.busy = true; nk.err = ''
  try {
    const k = await z.createKey(nk.name || 'key', nk.scopes.length ? nk.scopes : ['read'], nk.prefix.trim())
    lastSecret.value = k.secret || ''; nk.name = ''; nk.prefix = ''; z.toast('key created')
  } catch (e) { nk.err = z.errMsg(e) } finally { nk.busy = false }
}
</script>

<template>
  <AppPage title="API keys" subtitle="Scoped credentials for test suites, agents, and the MCP endpoint.">
    <EmptyState v-if="!z.isAuthed()" :icon="KeyRound" title="Sign in to manage API keys" description="API keys are owned by your account — sign in to mint or revoke them.">
      <Button size="sm" @click="z.openSignIn()"><LogIn /> Sign in</Button>
    </EmptyState>

    <template v-else>
    <!-- secret shown once -->
    <div v-if="lastSecret" class="mb-5 grid gap-2 rounded-xl border border-ok/40 bg-ok/10 p-4">
      <div class="flex items-center gap-2 text-[13px] font-medium"><Check class="size-4 text-ok" /> Copy this key now — it won't be shown again.</div>
      <div class="flex items-center gap-2">
        <Input :model-value="lastSecret" readonly class="h-9 font-mono text-[12px]" />
        <Button variant="secondary" size="sm" @click="z.copy(lastSecret, 'key copied')"><Copy /> copy</Button>
        <Button variant="ghost" size="icon-sm" title="dismiss" @click="lastSecret = ''"><X /></Button>
      </div>
    </div>

    <!-- create -->
    <div class="mb-6 grid gap-3 rounded-xl border p-4">
      <Label class="text-[12px] text-muted-foreground">Create a key</Label>
      <div class="grid grid-cols-2 gap-2 max-[560px]:grid-cols-1">
        <Input v-model="nk.name" class="h-9" placeholder="name (e.g. ci-pipeline)" />
        <Input v-model="nk.prefix" class="h-9" placeholder="inbox-prefix scope (optional)" />
      </div>
      <ToggleGroup type="multiple" variant="outline" :model-value="nk.scopes" @update:model-value="(v) => nk.scopes = (v as Scope[])">
        <ToggleGroupItem value="read">read</ToggleGroupItem>
        <ToggleGroupItem value="manage">manage</ToggleGroupItem>
        <ToggleGroupItem value="admin">admin</ToggleGroupItem>
      </ToggleGroup>
      <p v-if="nk.err" class="text-[12px] text-danger">{{ nk.err }}</p>
      <Button class="w-fit" size="sm" :disabled="nk.busy" @click="makeKey"><Loader2 v-if="nk.busy" class="animate-spin" /><Plus v-else /> Create key</Button>
    </div>

    <!-- list -->
    <div v-if="state.keys.length" class="grid gap-2">
      <div v-for="k in state.keys" :key="k.id" class="flex items-center gap-2.5 rounded-xl border p-4">
        <div class="min-w-0">
          <div class="flex items-center gap-2">
            <span class="truncate text-[13px] font-medium">{{ k.name || 'key' }}</span>
            <Badge v-for="s in k.scopes" :key="s" variant="secondary" class="text-[10px]">{{ s }}</Badge>
          </div>
          <div class="mt-0.5 flex items-center gap-2 text-[11px] text-muted-foreground">
            <span v-if="k.inbox_prefix" class="font-mono">prefix: {{ k.inbox_prefix }}</span>
            <span>created {{ relTime(k.created_at) }}</span>
            <span v-if="k.last_used_at">· used {{ relTime(k.last_used_at) }}</span>
          </div>
        </div>
        <span class="flex-1" />
        <Button variant="ghost" size="icon-sm" class="text-danger hover:text-danger" title="Revoke" @click="z.deleteKey(k.id)"><Trash2 /></Button>
      </div>
    </div>

    <EmptyState v-else :icon="KeyRound" title="No API keys" description="Mint a scoped key to drive Zorail from a test suite, or to authenticate an MCP agent." />
    </template>
  </AppPage>
</template>
