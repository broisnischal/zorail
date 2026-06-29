<script setup lang="ts">
import { ref } from 'vue'

const z = useZorail()
const { state } = z
const el = ref<HTMLDialogElement | null>(null)
const tokenDraft = ref('')

function open() { tokenDraft.value = state.token; el.value?.showModal() }
function close() { el.value?.close() }
function saveToken() { z.setToken(tokenDraft.value); z.toast('token saved') }
defineExpose({ open })
</script>

<template>
  <dialog ref="el" class="modal">
    <div class="mhead">
      <span>Settings</span>
      <button class="btn icon" @click="close">✕</button>
    </div>
    <div class="mbody">
      <div class="mrow">
        <span class="label">Theme</span>
        <div class="seg">
          <button :class="{ on: state.theme === 'dark' }" @click="z.setTheme('dark')">dark</button>
          <button :class="{ on: state.theme === 'light' }" @click="z.setTheme('light')">light</button>
        </div>
      </div>

      <div class="mrow">
        <span class="label">Accent</span>
        <div class="swatches">
          <button
            v-for="a in z.accentList()"
            :key="a.key"
            class="swatch"
            :class="{ sel: state.accent === a.key }"
            :style="{ background: a.value }"
            :title="a.key"
            @click="z.setAccent(a.key)"
          />
        </div>
      </div>

      <div class="mrow">
        <span class="label">Remote images</span>
        <div class="seg">
          <button :class="{ on: !state.loadImages }" @click="state.loadImages && z.toggleImages()">block</button>
          <button :class="{ on: state.loadImages }" @click="!state.loadImages && z.toggleImages()">load</button>
        </div>
      </div>

      <div class="mrow">
        <span class="label">Auto-refresh</span>
        <div class="seg">
          <button :class="{ on: state.autoRefresh }" @click="!state.autoRefresh && z.toggleAuto()">on</button>
          <button :class="{ on: !state.autoRefresh }" @click="state.autoRefresh && z.toggleAuto()">off</button>
        </div>
      </div>

      <div class="mrow">
        <span class="label">API token <span class="muted" v-if="state.config.auth_required">· required by server</span></span>
        <div class="field">
          <span class="slash">/</span>
          <input v-model="tokenDraft" type="password" placeholder="bearer token (optional)" autocomplete="off" @keydown.enter="saveToken" />
          <button class="btn" @click="saveToken">save</button>
        </div>
      </div>

      <div class="muted mono" style="font-size:11px">
        zorail v{{ state.config.version || '—' }} · domain {{ state.config.domain }}
      </div>
    </div>
  </dialog>
</template>
