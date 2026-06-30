<script setup lang="ts">
import { reactive } from 'vue'
import { Inbox, ArrowRight, Loader2 } from 'lucide-vue-next'
import { Button } from '@/components/ui/button'
import { Input } from '@/components/ui/input'
import { Label } from '@/components/ui/label'

const z = useZorail()
const f = reactive({ org: '', email: '', password: '', busy: false, err: '' })

async function create() {
  if (!f.org.trim()) { f.err = 'name your organization'; return }
  if (!f.email.includes('@') || f.password.length < 8) { f.err = 'enter a valid email and an 8+ character password'; return }
  f.busy = true; f.err = ''
  try {
    await z.setup(f.org.trim(), f.email.trim(), f.password)
    z.toast('workspace ready')
  } catch (e) { f.err = z.errMsg(e); f.busy = false }
}
</script>

<template>
  <div class="flex min-h-screen items-center justify-center px-6 py-16">
    <div class="w-full max-w-[380px] animate-in fade-in slide-in-from-bottom-2 duration-500">
      <!-- mark -->
      <div class="mb-10 flex flex-col items-center text-center">
        <div class="mb-5 flex size-12 items-center justify-center rounded-2xl border bg-muted/50">
          <Inbox class="size-6" />
        </div>
        <h1 class="text-[22px] font-semibold tracking-tight">Welcome to Zorail</h1>
        <p class="mt-2 max-w-[300px] text-[13px] leading-relaxed text-muted-foreground">
          Create your organization and administrator account to get started. This happens once.
        </p>
      </div>

      <form class="grid gap-5" @submit.prevent="create">
        <div class="grid gap-2">
          <Label for="ob-org" class="text-[12px] text-muted-foreground">Organization</Label>
          <Input id="ob-org" v-model="f.org" placeholder="Acme Inc." autofocus class="h-10" />
        </div>
        <div class="grid gap-2">
          <Label for="ob-email" class="text-[12px] text-muted-foreground">Admin email</Label>
          <Input id="ob-email" v-model="f.email" type="email" autocomplete="username" placeholder="you@acme.com" class="h-10" />
        </div>
        <div class="grid gap-2">
          <Label for="ob-pw" class="text-[12px] text-muted-foreground">Password</Label>
          <Input id="ob-pw" v-model="f.password" type="password" autocomplete="new-password" placeholder="At least 8 characters" class="h-10" />
        </div>

        <p v-if="f.err" class="text-[12.5px] text-danger">{{ f.err }}</p>

        <Button type="submit" size="lg" class="mt-1 h-10" :disabled="f.busy">
          <Loader2 v-if="f.busy" class="animate-spin" />
          <template v-else>Create workspace <ArrowRight /></template>
        </Button>
      </form>

      <p class="mt-8 text-center text-[11.5px] leading-relaxed text-muted-foreground/70">
        Your data stays on this server. You can add forwarding, scoped API keys,
        and an MCP endpoint for agents afterwards.
      </p>
    </div>
  </div>
</template>
