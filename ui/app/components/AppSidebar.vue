<script setup lang="ts">
import { computed } from 'vue'
import {
  Inbox, AtSign, Globe, KeyRound, Settings2, ChevronsUpDown, MoreHorizontal,
  LogOut, LogIn, type LucideIcon,
} from 'lucide-vue-next'
import { Menu, MenuItem } from '@/components/ui/menu'
import { Tip } from '@/components/ui/tooltip'
import { Separator } from '@/components/ui/separator'
import type { Section } from '@/composables/useZorail'

const z = useZorail()
const { state } = z

const route = useRoute()
const nav: { key: Section; label: string; icon: LucideIcon; path: string }[] = [
  { key: 'inboxes', label: 'Inboxes', icon: Inbox, path: '/' },
  { key: 'addresses', label: 'Addresses', icon: AtSign, path: '/addresses' },
  { key: 'domains', label: 'Domains', icon: Globe, path: '/domains' },
  { key: 'keys', label: 'API keys', icon: KeyRound, path: '/keys' },
  { key: 'settings', label: 'Settings', icon: Settings2, path: '/settings' },
]
function isActive(item: { key: Section; path: string }) {
  // The inbox detail route (/inbox/…) still highlights Inboxes.
  if (item.key === 'inboxes') return route.path === '/' || route.path.startsWith('/inbox')
  return route.path === item.path
}

const orgInitial = computed(() => (state.organization || 'Z').slice(0, 1).toUpperCase())
const userInitial = computed(() => (state.user?.email || '?').slice(0, 1).toUpperCase())
</script>

<template>
  <aside class="flex w-[228px] flex-none flex-col border-r bg-background max-[860px]:w-[58px]">
    <!-- org switcher -->
    <div class="p-2.5">
      <Menu align="start" class="w-full">
        <template #trigger="{ open }">
          <button class="group flex w-full items-center gap-2.5 rounded-lg px-2 py-2 text-left transition-colors hover:bg-accent active:scale-[0.99]">
            <span class="flex size-7 shrink-0 items-center justify-center rounded-md bg-gradient-to-br from-violet-500 to-violet-700 text-[12px] font-semibold text-white shadow-sm transition-transform duration-200 group-hover:scale-105">{{ orgInitial }}</span>
            <span class="min-w-0 flex-1 truncate text-[13.5px] font-semibold max-[860px]:hidden">{{ state.organization || 'Zorail' }}</span>
            <ChevronsUpDown class="size-3.5 shrink-0 text-muted-foreground transition-transform duration-200 max-[860px]:hidden" :class="open ? 'rotate-180' : ''" />
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
    <nav class="flex-1 space-y-0.5 px-2.5">
      <Tip v-for="item in nav" :key="item.key" :label="item.label" side="right">
        <button
          class="group relative flex w-full items-center gap-2.5 rounded-lg px-2.5 py-1.5 text-[13px] transition-all duration-200 active:scale-[0.98] max-[860px]:justify-center"
          :class="isActive(item)
            ? 'bg-muted font-medium text-foreground shadow-sm ring-1 ring-border-subtle'
            : 'text-muted-foreground hover:bg-muted/50 hover:text-foreground'"
          @click="navigateTo(item.path)"
        >
          <!-- animated active indicator -->
          <span
            class="absolute -left-2.5 top-1/2 h-5 w-[3px] -translate-y-1/2 rounded-r-full bg-foreground transition-all duration-300 ease-out"
            :class="isActive(item) ? 'scale-y-100 opacity-100' : 'scale-y-0 opacity-0'"
          />
          <component
            :is="item.icon"
            class="size-[18px] shrink-0 transition-transform duration-200 group-hover:scale-110 group-active:scale-90"
          />
          <span class="max-[860px]:hidden">{{ item.label }}</span>
        </button>
      </Tip>
    </nav>

    <!-- user footer -->
    <div class="border-t p-2.5">
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
