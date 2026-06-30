<script setup lang="ts">
import { ref } from 'vue'
import { KeyRound, Check } from 'lucide-vue-next'
import {
  Dialog, DialogContent, DialogHeader, DialogTitle, DialogDescription,
} from '@/components/ui/dialog'
import { ToggleGroup, ToggleGroupItem } from '@/components/ui/toggle-group'
import { Button } from '@/components/ui/button'
import { Input } from '@/components/ui/input'
import { Label } from '@/components/ui/label'

const z = useZorail()
const { state } = z
const open = ref(false)
const tokenDraft = ref('')

function show() { tokenDraft.value = state.token; open.value = true }
function saveToken() { z.setToken(tokenDraft.value); z.toast('token saved') }

defineExpose({ open: show })
</script>

<template>
  <Dialog v-model:open="open">
    <DialogContent class="sm:max-w-md">
      <DialogHeader>
        <DialogTitle>Settings</DialogTitle>
        <DialogDescription class="sr-only">Appearance, privacy and API token settings</DialogDescription>
      </DialogHeader>

      <div class="grid gap-5 py-1">
        <div class="grid gap-2">
          <Label class="text-muted-foreground">Theme</Label>
          <ToggleGroup
            type="single" variant="outline" :model-value="state.theme"
            @update:model-value="(v) => v && z.setTheme(v as 'dark' | 'light')"
          >
            <ToggleGroupItem value="dark">dark</ToggleGroupItem>
            <ToggleGroupItem value="light">light</ToggleGroupItem>
          </ToggleGroup>
        </div>

        <div class="grid gap-2">
          <Label class="text-muted-foreground">Accent</Label>
          <div class="flex flex-wrap items-center gap-2">
            <button
              v-for="a in z.accentList()"
              :key="a.key"
              class="size-6 rounded-md border-2 transition-transform hover:scale-110"
              :class="state.accent === a.key ? 'border-foreground' : 'border-transparent'"
              :style="{ background: a.swatch }"
              :title="a.key"
              @click="z.setAccent(a.key)"
            />
          </div>
        </div>

        <div class="grid gap-2">
          <Label class="text-muted-foreground">Remote images</Label>
          <ToggleGroup
            type="single" variant="outline" :model-value="state.loadImages ? 'load' : 'block'"
            @update:model-value="(v) => { if (v && (v === 'load') !== state.loadImages) z.toggleImages() }"
          >
            <ToggleGroupItem value="block">block</ToggleGroupItem>
            <ToggleGroupItem value="load">load</ToggleGroupItem>
          </ToggleGroup>
        </div>

        <div class="grid gap-2">
          <Label class="text-muted-foreground">Auto-refresh</Label>
          <ToggleGroup
            type="single" variant="outline" :model-value="state.autoRefresh ? 'on' : 'off'"
            @update:model-value="(v) => { if (v && (v === 'on') !== state.autoRefresh) z.toggleAuto() }"
          >
            <ToggleGroupItem value="on">on</ToggleGroupItem>
            <ToggleGroupItem value="off">off</ToggleGroupItem>
          </ToggleGroup>
        </div>

        <div class="grid gap-2">
          <Label class="text-muted-foreground">
            API token
            <span v-if="state.config.auth_required" class="text-muted-foreground/70">· required by server</span>
          </Label>
          <div class="flex items-center gap-2">
            <div class="relative flex-1">
              <KeyRound class="absolute left-3 top-1/2 size-4 -translate-y-1/2 text-muted-foreground" />
              <Input
                v-model="tokenDraft" type="password" autocomplete="off"
                placeholder="bearer token (optional)" class="pl-9"
                @keydown.enter="saveToken"
              />
            </div>
            <Button variant="secondary" size="sm" @click="saveToken">
              <Check /> save
            </Button>
          </div>
        </div>

        <p class="font-mono text-[11px] text-muted-foreground">
          zorail v{{ state.config.version || '—' }} · domain {{ state.config.domain }}
        </p>
      </div>
    </DialogContent>
  </Dialog>
</template>
