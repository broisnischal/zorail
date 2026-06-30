<script setup lang="ts">
import { ref } from 'vue'
import { KeyRound, Check } from 'lucide-vue-next'
import { ToggleGroup, ToggleGroupItem } from '@/components/ui/toggle-group'
import { Button } from '@/components/ui/button'
import { Input } from '@/components/ui/input'
import { Label } from '@/components/ui/label'
import { Separator } from '@/components/ui/separator'

const z = useZorail()
const { state } = z
const tokenDraft = ref(state.token)
function saveToken() { z.setToken(tokenDraft.value); z.toast('token saved') }

function row() { return 'grid gap-2 py-4' }
</script>

<template>
  <AppPage title="Settings" subtitle="Appearance and instance preferences.">
    <div class="rounded-xl border px-5">
      <div :class="row()">
        <Label class="text-muted-foreground">Theme</Label>
        <ToggleGroup type="single" variant="outline" :model-value="state.theme" @update:model-value="(v) => v && z.setTheme(v as 'dark' | 'light')">
          <ToggleGroupItem value="dark">dark</ToggleGroupItem>
          <ToggleGroupItem value="light">light</ToggleGroupItem>
        </ToggleGroup>
      </div>
      <Separator />
      <div :class="row()">
        <Label class="text-muted-foreground">Accent</Label>
        <div class="flex flex-wrap items-center gap-2">
          <button
            v-for="a in z.accentList()" :key="a.key"
            class="size-6 rounded-md border-2 transition-transform hover:scale-110"
            :class="state.accent === a.key ? 'border-foreground' : 'border-transparent'"
            :style="{ background: a.swatch }" :title="a.key" @click="z.setAccent(a.key)"
          />
        </div>
      </div>
      <Separator />
      <div :class="row()">
        <Label class="text-muted-foreground">Remote images</Label>
        <ToggleGroup type="single" variant="outline" :model-value="state.loadImages ? 'load' : 'block'" @update:model-value="(v) => { if (v && (v === 'load') !== state.loadImages) z.toggleImages() }">
          <ToggleGroupItem value="block">block</ToggleGroupItem>
          <ToggleGroupItem value="load">load</ToggleGroupItem>
        </ToggleGroup>
      </div>
      <Separator />
      <div :class="row()">
        <Label class="text-muted-foreground">Live updates</Label>
        <ToggleGroup type="single" variant="outline" :model-value="state.autoRefresh ? 'on' : 'off'" @update:model-value="(v) => { if (v && (v === 'on') !== state.autoRefresh) z.toggleAuto() }">
          <ToggleGroupItem value="on">on</ToggleGroupItem>
          <ToggleGroupItem value="off">off</ToggleGroupItem>
        </ToggleGroup>
      </div>
      <Separator />
      <div :class="row()">
        <Label class="text-muted-foreground">API token <span class="text-muted-foreground/70">· for programmatic access</span></Label>
        <div class="flex items-center gap-2">
          <div class="relative flex-1 max-w-md">
            <KeyRound class="absolute left-3 top-1/2 size-4 -translate-y-1/2 text-muted-foreground" />
            <Input v-model="tokenDraft" type="password" autocomplete="off" placeholder="bearer token (optional)" class="h-9 pl-9" @keydown.enter="saveToken" />
          </div>
          <Button variant="secondary" size="sm" @click="saveToken"><Check /> save</Button>
        </div>
      </div>
    </div>

    <p class="mt-4 font-mono text-[11.5px] text-muted-foreground">
      zorail v{{ state.config.version || '—' }} · {{ state.organization || 'Zorail' }} · domain {{ state.config.domain }}
    </p>
  </AppPage>
</template>
