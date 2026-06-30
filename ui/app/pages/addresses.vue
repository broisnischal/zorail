<script setup lang="ts">
import { onMounted } from 'vue'
import AddressesView from '@/components/AddressesView.vue'
const z = useZorail()
onMounted(() => { if (z.isAuthed()) z.loadAddresses() })
</script>

<template>
  <AddressesView />
</template>
