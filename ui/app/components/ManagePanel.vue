<script setup lang="ts">
import { ref, reactive } from 'vue'
import {
  User as UserIcon, LogIn, LogOut, UserPlus, Mail, Forward, KeyRound,
  Plus, Trash2, Copy, Check, AtSign, X, Loader2, ShieldCheck, Inbox,
} from 'lucide-vue-next'
import {
  Dialog, DialogContent, DialogHeader, DialogTitle, DialogDescription,
} from '@/components/ui/dialog'
import { Tabs, TabsList, TabsTrigger, TabsContent } from '@/components/ui/tabs'
import { ToggleGroup, ToggleGroupItem } from '@/components/ui/toggle-group'
import { Button } from '@/components/ui/button'
import { Input } from '@/components/ui/input'
import { Label } from '@/components/ui/label'
import { Badge } from '@/components/ui/badge'
import { Switch } from '@/components/ui/switch'
import { Separator } from '@/components/ui/separator'
import { Skeleton } from '@/components/ui/skeleton'
import type { Address, Scope } from '@/composables/useZorail'

const z = useZorail()
const { state } = z

const open = ref(false)
const tab = ref<'account' | 'addresses' | 'keys'>('account')

function show(t?: 'account' | 'addresses' | 'keys') {
  if (t) tab.value = t
  else if (!z.isAuthed()) tab.value = 'account'
  open.value = true
  if (z.isAuthed()) { z.loadAddresses(); z.loadKeys() }
}
defineExpose({ open: show })

// ---- account ----
const mode = ref<'login' | 'register'>('login')
const acct = reactive({ email: '', password: '', busy: false, err: '' })
async function submitAccount() {
  if (!acct.email || acct.password.length < 8) { acct.err = 'email and an 8+ character password are required'; return }
  acct.busy = true; acct.err = ''
  try {
    if (mode.value === 'register') await z.register(acct.email, acct.password)
    else await z.login(acct.email, acct.password)
    z.toast(mode.value === 'register' ? 'account created' : 'signed in')
    acct.password = ''
    tab.value = 'addresses'
  } catch (e) { acct.err = z.errMsg(e) }
  finally { acct.busy = false }
}
function doLogout() { z.logout(); z.toast('signed out'); mode.value = 'login' }

// ---- addresses ----
const res = reactive({ type: 'reserved' as 'reserved' | 'forward', local: '', forwardTo: '', busy: false, err: '' })
async function reserve() {
  res.busy = true; res.err = ''
  try {
    const forward_to = res.forwardTo.split(',').map((s) => s.trim()).filter(Boolean)
    if (res.type === 'forward' && !forward_to.length) { res.err = 'add at least one destination to forward to'; res.busy = false; return }
    await z.reserveAddress({ prefix: res.local || undefined, type: res.type, forward_to })
    z.toast('address reserved'); res.local = ''; res.forwardTo = ''
  } catch (e) { res.err = z.errMsg(e) }
  finally { res.busy = false }
}
async function toggleForward(a: Address, v: boolean) {
  try { await z.updateAddress(a.address, { forward_enabled: v }) }
  catch (e) { z.toast(z.errMsg(e), 'err') }
}
const destDraft = reactive<Record<string, string>>({})
async function addDest(a: Address) {
  const d = (destDraft[a.address] || '').trim()
  if (!d) return
  try { await z.updateAddress(a.address, { forward_to: [...(a.forward_to || []), d] }); destDraft[a.address] = '' }
  catch (e) { z.toast(z.errMsg(e), 'err') }
}
async function removeDest(a: Address, dest: string) {
  try { await z.updateAddress(a.address, { forward_to: (a.forward_to || []).filter((x) => x !== dest) }) }
  catch (e) { z.toast(z.errMsg(e), 'err') }
}
async function verify(dest: string) {
  try {
    const r = await z.requestVerify(dest)
    if (r.confirm_url) z.copy(r.confirm_url, 'no mailer configured — verify link copied')
    else z.toast('verification email sent to ' + dest)
  } catch (e) { z.toast(z.errMsg(e), 'err') }
}
async function release(a: Address) {
  try { await z.releaseAddress(a.address); z.toast('address released') }
  catch (e) { z.toast(z.errMsg(e), 'err') }
}

// ---- keys ----
const nk = reactive({ name: '', scopes: ['read'] as Scope[], prefix: '', busy: false, err: '' })
const lastSecret = ref('')
async function makeKey() {
  nk.busy = true; nk.err = ''
  try {
    const k = await z.createKey(nk.name || 'key', nk.scopes.length ? nk.scopes : ['read'], nk.prefix.trim())
    lastSecret.value = k.secret || ''
    nk.name = ''; nk.prefix = ''
    z.toast('key created')
  } catch (e) { nk.err = z.errMsg(e) }
  finally { nk.busy = false }
}
async function revoke(id: string) {
  try { await z.deleteKey(id); z.toast('key revoked') }
  catch (e) { z.toast(z.errMsg(e), 'err') }
}
</script>

<template>
  <Dialog v-model:open="open">
    <DialogContent
      :show-close-button="true"
      class="block w-[min(900px,95vw)] max-w-none gap-0 overflow-hidden p-0 sm:max-w-none"
    >
      <DialogHeader class="border-b px-5 py-4">
        <DialogTitle class="text-sm font-semibold tracking-tight">Manage</DialogTitle>
        <DialogDescription class="text-[12.5px] text-muted-foreground">
          Accounts, reserved &amp; forwarding addresses, and scoped API keys.
        </DialogDescription>
      </DialogHeader>

      <Tabs v-model="tab" class="gap-0">
        <TabsList class="flex h-11 w-full flex-none items-stretch justify-start gap-1 rounded-none border-b bg-transparent px-3">
          <TabsTrigger value="account" class="gap-1.5"><UserIcon class="size-3.5" /> Account</TabsTrigger>
          <TabsTrigger value="addresses" class="gap-1.5"><Mail class="size-3.5" /> Addresses</TabsTrigger>
          <TabsTrigger value="keys" class="gap-1.5"><KeyRound class="size-3.5" /> API keys</TabsTrigger>
        </TabsList>

        <!-- ============ ACCOUNT ============ -->
        <TabsContent value="account" class="max-h-[64vh] overflow-y-auto p-5">
          <div v-if="z.isAuthed()" class="grid gap-4">
            <div class="flex items-center gap-3 rounded-lg border bg-muted/40 p-4">
              <div class="flex size-9 items-center justify-center rounded-full bg-muted"><UserIcon class="size-4" /></div>
              <div class="min-w-0">
                <div class="truncate text-sm font-medium">{{ state.user?.email }}</div>
                <div class="text-[11.5px] text-muted-foreground">signed in · session uses a manage-scoped key</div>
              </div>
              <Button variant="outline" size="sm" class="ml-auto" @click="doLogout"><LogOut /> Sign out</Button>
            </div>
            <p class="text-[12.5px] leading-relaxed text-muted-foreground">
              Your account owns reserved addresses and API keys. Use the
              <span class="font-medium text-foreground">Addresses</span> tab to reserve a permanent or
              forwarding address, and <span class="font-medium text-foreground">API keys</span> to mint
              scoped credentials for test suites and agents.
            </p>
          </div>

          <div v-else class="mx-auto grid max-w-sm gap-4 py-2">
            <ToggleGroup
              type="single" variant="outline" :model-value="mode"
              class="w-full"
              @update:model-value="(v) => v && (mode = v as 'login' | 'register')"
            >
              <ToggleGroupItem value="login" class="flex-1"><LogIn class="size-3.5" /> Sign in</ToggleGroupItem>
              <ToggleGroupItem value="register" class="flex-1"><UserPlus class="size-3.5" /> Create account</ToggleGroupItem>
            </ToggleGroup>

            <form class="grid gap-3" @submit.prevent="submitAccount">
              <div class="grid gap-1.5">
                <Label for="acct-email" class="text-muted-foreground">Email</Label>
                <Input id="acct-email" v-model="acct.email" type="email" autocomplete="username" placeholder="you@team.test" />
              </div>
              <div class="grid gap-1.5">
                <Label for="acct-pw" class="text-muted-foreground">Password</Label>
                <Input id="acct-pw" v-model="acct.password" type="password" autocomplete="current-password" placeholder="8+ characters" />
              </div>
              <p v-if="acct.err" class="text-[12px] text-danger">{{ acct.err }}</p>
              <Button type="submit" :disabled="acct.busy">
                <Loader2 v-if="acct.busy" class="animate-spin" /><component :is="mode === 'login' ? LogIn : UserPlus" v-else />
                {{ mode === 'login' ? 'Sign in' : 'Create account' }}
              </Button>
            </form>
          </div>
        </TabsContent>

        <!-- ============ ADDRESSES ============ -->
        <TabsContent value="addresses" class="max-h-[64vh] overflow-y-auto p-5">
          <div v-if="!z.isAuthed()" class="py-10 text-center">
            <Mail class="mx-auto mb-3 size-7 text-muted-foreground opacity-60" />
            <p class="text-[12.5px] text-muted-foreground">Sign in to reserve permanent &amp; forwarding addresses.</p>
            <Button variant="outline" size="sm" class="mt-4" @click="tab = 'account'"><LogIn /> Go to account</Button>
          </div>

          <div v-else class="grid gap-5">
            <!-- reserve form -->
            <div class="grid gap-3 rounded-lg border p-4">
              <Label class="text-muted-foreground">Reserve a new address</Label>
              <ToggleGroup
                type="single" variant="outline" :model-value="res.type"
                @update:model-value="(v) => v && (res.type = v as 'reserved' | 'forward')"
              >
                <ToggleGroupItem value="reserved"><Inbox class="size-3.5" /> Reserved</ToggleGroupItem>
                <ToggleGroupItem value="forward"><Forward class="size-3.5" /> Forwarding</ToggleGroupItem>
              </ToggleGroup>

              <div class="flex items-center gap-2">
                <div class="relative flex-1">
                  <AtSign class="absolute left-3 top-1/2 size-3.5 -translate-y-1/2 text-muted-foreground" />
                  <Input v-model="res.local" class="pl-9" :placeholder="`local part (optional) · @${state.config.domain}`" />
                </div>
              </div>
              <Input
                v-if="res.type === 'forward'"
                v-model="res.forwardTo"
                placeholder="forward to (comma-separated): me@gmail.com, ops@outlook.com"
              />
              <p v-if="res.type === 'forward'" class="text-[11.5px] leading-relaxed text-muted-foreground">
                Forwarding only delivers to <span class="text-foreground">verified</span> destinations — verify each below after reserving.
              </p>
              <p v-if="res.err" class="text-[12px] text-danger">{{ res.err }}</p>
              <Button class="w-fit" :disabled="res.busy" @click="reserve">
                <Loader2 v-if="res.busy" class="animate-spin" /><Plus v-else /> Reserve
              </Button>
            </div>

            <!-- list -->
            <div class="grid gap-2">
              <Label class="text-muted-foreground">Your addresses</Label>
              <template v-if="state.loadingAddresses && !state.addresses.length">
                <Skeleton v-for="i in 3" :key="i" class="h-14 w-full rounded-lg" />
              </template>
              <div v-else-if="!state.addresses.length" class="rounded-lg border border-dashed py-8 text-center text-[12.5px] text-muted-foreground">
                No reserved addresses yet.
              </div>
              <ul v-else class="grid gap-2">
                <li v-for="a in state.addresses" :key="a.address" class="rounded-lg border p-3.5">
                  <div class="flex items-center gap-2.5">
                    <span class="truncate font-mono text-[12.5px]">{{ a.address }}</span>
                    <Badge :variant="a.type === 'forward' ? 'default' : 'secondary'" class="capitalize">{{ a.type }}</Badge>
                    <Switch
                      v-if="a.type === 'forward'"
                      :model-value="a.forward_enabled"
                      class="ml-1"
                      title="forwarding enabled"
                      @update:model-value="(v) => toggleForward(a, !!v)"
                    />
                    <span class="flex-1" />
                    <Button variant="ghost" size="icon-sm" title="Copy" @click="z.copy(a.address, 'address copied')"><Copy /></Button>
                    <Button variant="ghost" size="icon-sm" class="text-danger hover:text-danger" title="Release" @click="release(a)"><Trash2 /></Button>
                  </div>

                  <div v-if="a.type === 'forward'" class="mt-3 grid gap-2 border-t border-border-subtle pt-3">
                    <div v-for="d in (a.forward_to || [])" :key="d" class="flex items-center gap-2 text-[12px]">
                      <Forward class="size-3 text-muted-foreground" />
                      <span class="truncate font-mono">{{ d }}</span>
                      <Button variant="outline" size="sm" class="ml-auto h-6 px-2 text-[11px]" title="Send verification mail" @click="verify(d)">
                        <ShieldCheck class="size-3" /> verify
                      </Button>
                      <button class="text-muted-foreground hover:text-danger" title="remove destination" @click="removeDest(a, d)"><X class="size-3.5" /></button>
                    </div>
                    <div class="flex items-center gap-2">
                      <Input
                        v-model="destDraft[a.address]"
                        class="h-7 text-[12px]"
                        placeholder="add destination…"
                        @keydown.enter="addDest(a)"
                      />
                      <Button variant="secondary" size="sm" class="h-7" @click="addDest(a)"><Plus class="size-3" /> add</Button>
                    </div>
                  </div>
                </li>
              </ul>
            </div>
          </div>
        </TabsContent>

        <!-- ============ KEYS ============ -->
        <TabsContent value="keys" class="max-h-[64vh] overflow-y-auto p-5">
          <div v-if="!z.isAuthed()" class="py-10 text-center">
            <KeyRound class="mx-auto mb-3 size-7 text-muted-foreground opacity-60" />
            <p class="text-[12.5px] text-muted-foreground">Sign in to mint scoped API keys.</p>
            <Button variant="outline" size="sm" class="mt-4" @click="tab = 'account'"><LogIn /> Go to account</Button>
          </div>

          <div v-else class="grid gap-5">
            <!-- secret shown once -->
            <div v-if="lastSecret" class="grid gap-2 rounded-lg border border-ok/40 bg-ok/10 p-4">
              <div class="flex items-center gap-2 text-[12.5px] font-medium text-foreground"><Check class="size-4 text-ok" /> Key created — copy it now, it won't be shown again.</div>
              <div class="flex items-center gap-2">
                <Input :model-value="lastSecret" readonly class="font-mono text-[12px]" />
                <Button variant="secondary" size="sm" @click="z.copy(lastSecret, 'key copied')"><Copy /> copy</Button>
                <Button variant="ghost" size="icon-sm" title="dismiss" @click="lastSecret = ''"><X /></Button>
              </div>
            </div>

            <!-- create form -->
            <div class="grid gap-3 rounded-lg border p-4">
              <Label class="text-muted-foreground">Create a key</Label>
              <div class="grid grid-cols-2 gap-2 max-[560px]:grid-cols-1">
                <Input v-model="nk.name" placeholder="name (e.g. ci-pipeline)" />
                <Input v-model="nk.prefix" placeholder="inbox-prefix scope (optional, e.g. qa-)" />
              </div>
              <div class="grid gap-1.5">
                <span class="text-[11.5px] text-muted-foreground">Scopes</span>
                <ToggleGroup type="multiple" variant="outline" :model-value="nk.scopes" @update:model-value="(v) => nk.scopes = (v as Scope[])">
                  <ToggleGroupItem value="read">read</ToggleGroupItem>
                  <ToggleGroupItem value="manage">manage</ToggleGroupItem>
                  <ToggleGroupItem value="admin">admin</ToggleGroupItem>
                </ToggleGroup>
              </div>
              <p v-if="nk.err" class="text-[12px] text-danger">{{ nk.err }}</p>
              <Button class="w-fit" :disabled="nk.busy" @click="makeKey">
                <Loader2 v-if="nk.busy" class="animate-spin" /><Plus v-else /> Create key
              </Button>
            </div>

            <!-- list -->
            <div class="grid gap-2">
              <Label class="text-muted-foreground">Your keys</Label>
              <template v-if="state.loadingKeys && !state.keys.length">
                <Skeleton v-for="i in 2" :key="i" class="h-12 w-full rounded-lg" />
              </template>
              <div v-else-if="!state.keys.length" class="rounded-lg border border-dashed py-8 text-center text-[12.5px] text-muted-foreground">
                No keys yet.
              </div>
              <ul v-else class="grid gap-2">
                <li v-for="k in state.keys" :key="k.id" class="flex items-center gap-2.5 rounded-lg border p-3.5">
                  <div class="min-w-0">
                    <div class="flex items-center gap-2">
                      <span class="truncate text-[12.5px] font-medium">{{ k.name || 'key' }}</span>
                      <Badge v-for="s in k.scopes" :key="s" variant="secondary" class="text-[10px]">{{ s }}</Badge>
                    </div>
                    <div class="mt-0.5 flex items-center gap-2 text-[11px] text-muted-foreground">
                      <span v-if="k.inbox_prefix" class="font-mono">prefix: {{ k.inbox_prefix }}</span>
                      <span>created {{ relTime(k.created_at) }}</span>
                      <span v-if="k.last_used_at">· used {{ relTime(k.last_used_at) }}</span>
                    </div>
                  </div>
                  <span class="flex-1" />
                  <Button variant="ghost" size="icon-sm" class="text-danger hover:text-danger" title="Revoke" @click="revoke(k.id)"><Trash2 /></Button>
                </li>
              </ul>
            </div>
          </div>
        </TabsContent>
      </Tabs>
    </DialogContent>
  </Dialog>
</template>
