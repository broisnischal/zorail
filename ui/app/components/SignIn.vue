<script setup lang="ts">
import { reactive } from 'vue'
import { Inbox, ArrowRight, Loader2 } from 'lucide-vue-next'
import { Button } from '@/components/ui/button'
import { Input } from '@/components/ui/input'
import { Label } from '@/components/ui/label'

const z = useZorail()
const { state } = z
const f = reactive({ email: '', password: '', busy: false, err: '' })

async function submit() {
  if (!f.email.includes('@') || !f.password) { f.err = 'enter your email and password'; return }
  f.busy = true; f.err = ''
  try {
    await z.login(f.email.trim(), f.password)
    z.toast('signed in')
  } catch (e) { f.err = z.errMsg(e); f.busy = false }
}
</script>

<template>
  <div class="flex min-h-screen items-center justify-center px-6 py-16">
    <div class="w-full max-w-[360px] animate-in fade-in slide-in-from-bottom-2 duration-500">
      <div class="mb-9 flex flex-col items-center text-center">
        <div class="mb-5 flex size-12 items-center justify-center rounded-2xl border bg-muted/50">
          <Inbox class="size-6" />
        </div>
        <h1 class="text-[22px] font-semibold tracking-tight">Sign in to {{ state.organization || 'Zorail' }}</h1>
        <p class="mt-2 text-[13px] text-muted-foreground">Welcome back.</p>
      </div>

      <form class="grid gap-5" @submit.prevent="submit">
        <div class="grid gap-2">
          <Label for="si-email" class="text-[12px] text-muted-foreground">Email</Label>
          <Input id="si-email" v-model="f.email" type="email" autocomplete="username" autofocus class="h-10" />
        </div>
        <div class="grid gap-2">
          <Label for="si-pw" class="text-[12px] text-muted-foreground">Password</Label>
          <Input id="si-pw" v-model="f.password" type="password" autocomplete="current-password" class="h-10" />
        </div>
        <p v-if="f.err" class="text-[12.5px] text-danger">{{ f.err }}</p>
        <Button type="submit" size="lg" class="h-10" :disabled="f.busy">
          <Loader2 v-if="f.busy" class="animate-spin" />
          <template v-else>Sign in <ArrowRight /></template>
        </Button>
      </form>
    </div>
  </div>
</template>
