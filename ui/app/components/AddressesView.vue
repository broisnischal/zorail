<script setup lang="ts">
import { reactive } from 'vue'
import { AtSign, Forward, Inbox, Plus, Copy, Trash2, X, ShieldCheck, Loader2, LogIn } from 'lucide-vue-next'
import { Button } from '@/components/ui/button'
import { Input } from '@/components/ui/input'
import { Label } from '@/components/ui/label'
import { Badge } from '@/components/ui/badge'
import { Switch } from '@/components/ui/switch'
import { ToggleGroup, ToggleGroupItem } from '@/components/ui/toggle-group'
import type { Address } from '@/composables/useZorail'

const z = useZorail()
const { state } = z

const res = reactive({ type: 'reserved' as 'reserved' | 'forward', local: '', forwardTo: '', busy: false, err: '' })
async function reserve() {
  res.busy = true; res.err = ''
  try {
    const forward_to = res.forwardTo.split(',').map((s) => s.trim()).filter(Boolean)
    if (res.type === 'forward' && !forward_to.length) { res.err = 'add at least one destination'; res.busy = false; return }
    await z.reserveAddress({ prefix: res.local || undefined, type: res.type, forward_to })
    z.toast('address reserved'); res.local = ''; res.forwardTo = ''
  } catch (e) { res.err = z.errMsg(e) } finally { res.busy = false }
}
const destDraft = reactive<Record<string, string>>({})
async function addDest(a: Address) {
  const d = (destDraft[a.address] || '').trim(); if (!d) return
  try { await z.updateAddress(a.address, { forward_to: [...(a.forward_to || []), d] }); destDraft[a.address] = '' }
  catch (e) { z.toast(z.errMsg(e), 'err') }
}
async function removeDest(a: Address, dest: string) {
  try { await z.updateAddress(a.address, { forward_to: (a.forward_to || []).filter((x) => x !== dest) }) }
  catch (e) { z.toast(z.errMsg(e), 'err') }
}
async function verify(dest: string) {
  try { const r = await z.requestVerify(dest); r.confirm_url ? z.copy(r.confirm_url, 'no mailer — verify link copied') : z.toast('verification sent to ' + dest) }
  catch (e) { z.toast(z.errMsg(e), 'err') }
}
</script>

<template>
  <AppPage title="Addresses" subtitle="Reserve permanent inboxes, or forward to an external mailbox.">
    <EmptyState v-if="!z.isAuthed()" :icon="AtSign" title="Sign in to manage addresses" description="Browsing inboxes is open; reserving permanent and forwarding addresses needs an account.">
      <Button size="sm" @click="z.openSignIn()"><LogIn /> Sign in</Button>
    </EmptyState>

    <!-- reserve -->
    <div v-if="z.isAuthed()" class="mb-6 grid gap-3 rounded-xl border p-4">
      <Label class="text-[12px] text-muted-foreground">Reserve a new address</Label>
      <ToggleGroup
        type="single" variant="outline" :model-value="res.type"
        @update:model-value="(v) => v && (res.type = v as 'reserved' | 'forward')"
      >
        <ToggleGroupItem value="reserved"><Inbox class="size-3.5" /> Reserved</ToggleGroupItem>
        <ToggleGroupItem value="forward"><Forward class="size-3.5" /> Forwarding</ToggleGroupItem>
      </ToggleGroup>
      <div class="relative">
        <AtSign class="absolute left-3 top-1/2 size-3.5 -translate-y-1/2 text-muted-foreground" />
        <Input v-model="res.local" class="h-9 pl-9" :placeholder="`local part (optional) · @${state.config.domain}`" />
      </div>
      <Input v-if="res.type === 'forward'" v-model="res.forwardTo" class="h-9" placeholder="forward to (comma-separated): me@gmail.com" />
      <p v-if="res.err" class="text-[12px] text-danger">{{ res.err }}</p>
      <Button class="w-fit" size="sm" :disabled="res.busy" @click="reserve">
        <Loader2 v-if="res.busy" class="animate-spin" /><Plus v-else /> Reserve
      </Button>
    </div>

    <!-- list -->
    <div v-if="z.isAuthed() && state.addresses.length" class="grid gap-2">
      <div v-for="a in state.addresses" :key="a.address" class="rounded-xl border p-4">
        <div class="flex items-center gap-2.5">
          <span class="truncate font-mono text-[13px]">{{ a.address }}</span>
          <Badge :variant="a.type === 'forward' ? 'default' : 'secondary'" class="capitalize">{{ a.type }}</Badge>
          <Switch v-if="a.type === 'forward'" :model-value="a.forward_enabled" class="ml-1" title="forwarding enabled" @update:model-value="(v) => z.updateAddress(a.address, { forward_enabled: !!v })" />
          <span class="flex-1" />
          <Button variant="ghost" size="icon-sm" title="Copy" @click="z.copy(a.address, 'copied')"><Copy /></Button>
          <Button variant="ghost" size="icon-sm" class="text-danger hover:text-danger" title="Release" @click="z.releaseAddress(a.address)"><Trash2 /></Button>
        </div>
        <div v-if="a.type === 'forward'" class="mt-3 grid gap-2 border-t border-border-subtle pt-3">
          <div v-for="d in (a.forward_to || [])" :key="d" class="flex items-center gap-2 text-[12.5px]">
            <Forward class="size-3 text-muted-foreground" />
            <span class="truncate font-mono">{{ d }}</span>
            <Button variant="outline" size="sm" class="ml-auto h-6 px-2 text-[11px]" @click="verify(d)"><ShieldCheck class="size-3" /> verify</Button>
            <button class="text-muted-foreground hover:text-danger" title="remove" @click="removeDest(a, d)"><X class="size-3.5" /></button>
          </div>
          <div class="flex items-center gap-2">
            <Input v-model="destDraft[a.address]" class="h-7 text-[12px]" placeholder="add destination…" @keydown.enter="addDest(a)" />
            <Button variant="secondary" size="sm" class="h-7" @click="addDest(a)"><Plus class="size-3" /> add</Button>
          </div>
        </div>
      </div>
    </div>

    <EmptyState v-else-if="z.isAuthed()" :icon="AtSign" title="No reserved addresses" description="Reserved addresses are permanent and never swept. Forwarding addresses re-send to a verified mailbox." />
  </AppPage>
</template>
