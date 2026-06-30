<script setup lang="ts">
import { onMounted } from 'vue'
import KeysView from '@/components/KeysView.vue'
const z = useZorail()
onMounted(() => { if (z.isAuthed()) z.loadKeys() })
</script>

<template>
  <KeysView />
</template>
