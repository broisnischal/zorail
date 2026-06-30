<script setup lang="ts">
import { computed } from 'vue'
import {
  Inbox, AtSign, Globe, KeyRound, Settings2, ChevronsUpDown, MoreHorizontal,
  LogOut, LogIn, type LucideIcon,
} from 'lucide-vue-next'
import { Menu, MenuItem } from '@/components/ui/menu'
import { Separator } from '@/components/ui/separator'
import type { Section } from '@/composables/useZorail'

const z = useZorail()
const { state } = z

const nav: { key: Section; label: string; icon: LucideIcon }[] = [
  { key: 'inboxes', label: 'Inboxes', icon: Inbox },
  { key: 'addresses', label: 'Addresses', icon: AtSign },
  { key: 'domains', label: 'Domains', icon: Globe },
  { key: 'keys', label: 'API keys', icon: KeyRound },
  { key: 'settings', label: 'Settings', icon: Settings2 },
]

const orgInitial = computed(() => (state.organization || 'Z').slice(0, 1).toUpperCase())
const userInitial = computed(() => (state.user?.email || '?').slice(0, 1).toUpperCase())
</script>

<template>
  <aside class="flex w-[256px] flex-none flex-col border-r bg-background max-[860px]:w-[64px]">
    <!-- org switcher -->
    <div class="p-3">
      <Menu align="start" class="w-full">
        <template #trigger>
          <button class="flex w-full items-center gap-2.5 rounded-lg px-2 py-2 text-left transition-colors hover:bg-accent">
            <span class="flex size-7 shrink-0 items-center justify-center rounded-md bg-gradient-to-br from-violet-500 to-violet-700 text-[12px] font-semibold text-white">{{ orgInitial }}</span>
            <span class="min-w-0 flex-1 truncate text-[13.5px] font-semibold max-[860px]:hidden">{{ state.organization || 'Zorail' }}</span>
            <ChevronsUpDown class="size-3.5 shrink-0 text-muted-foreground max-[860px]:hidden" />
          </button>
        </template>
        <div class="px-2.5 pb-1.5 pt-1">
          <div class="truncate text-[13px] font-medium">{{ state.organization || 'Zorail' }}</div>
          <div class="truncate text-[11px] text-muted-foreground">{{ z.isAuthed() ? state.user?.email : 'not signed in' }}</div>
        </div>
        <Separator class="my-1" />
        <MenuItem @click="z.setSection('settings')"><Settings2 /> Settings</MenuItem>
        <MenuItem v-if="z.isAuthed()" danger @click="z.logout()"><LogOut /> Sign out</MenuItem>
        <MenuItem v-else @click="z.openSignIn()"><LogIn /> Sign in</MenuItem>
      </Menu>
    </div>

    <!-- nav -->
    <nav class="flex-1 space-y-0.5 px-3">
      <button
        v-for="item in nav" :key="item.key"
        class="flex w-full items-center gap-3 rounded-lg px-2.5 py-2 text-[13.5px] transition-colors"
        :class="state.section === item.key
          ? 'bg-accent font-medium text-foreground'
          : 'text-muted-foreground hover:bg-accent/60 hover:text-foreground'"
        :title="item.label"
        @click="z.setSection(item.key)"
      >
        <component :is="item.icon" class="size-[18px] shrink-0" />
        <span class="max-[860px]:hidden">{{ item.label }}</span>
      </button>
    </nav>

    <!-- user footer -->
    <div class="border-t p-3">
      <Menu v-if="z.isAuthed()" align="start" class="w-full">
        <template #trigger>
          <button class="flex w-full items-center gap-2.5 rounded-lg px-2 py-1.5 text-left transition-colors hover:bg-accent">
            <span class="flex size-7 shrink-0 items-center justify-center rounded-full border bg-muted text-[11px] font-medium">{{ userInitial }}</span>
            <span class="min-w-0 flex-1 truncate text-[12.5px] text-muted-foreground max-[860px]:hidden">{{ state.user?.email }}</span>
            <MoreHorizontal class="size-4 shrink-0 text-muted-foreground max-[860px]:hidden" />
          </button>
        </template>
        <MenuItem @click="z.setSection('settings')"><Settings2 /> Settings</MenuItem>
        <Separator class="my-1" />
        <MenuItem danger @click="z.logout()"><LogOut /> Sign out</MenuItem>
      </Menu>
      <button
        v-else
        class="flex w-full items-center gap-2.5 rounded-lg px-2 py-2 text-left text-[12.5px] text-muted-foreground transition-colors hover:bg-accent hover:text-foreground"
        @click="z.openSignIn()"
      >
        <LogIn class="size-[18px] shrink-0" /> <span class="max-[860px]:hidden">Sign in</span>
      </button>
    </div>
  </aside>
</template>
