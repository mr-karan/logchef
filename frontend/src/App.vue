<script setup lang="ts">
import { Toaster } from '@/components/ui/toast'
import OuterApp from '@/layouts/OuterApp.vue'
import InnerApp from '@/layouts/InnerApp.vue'
import { computed } from 'vue'
import { useRoute } from 'vue-router'

const route = useRoute()

// Determine layout based on route meta
const layout = computed(() => {
  return route.meta.layout === 'outer' ? OuterApp : InnerApp
})
</script>

<template>
  <Toaster />
  <component :is="layout">
    <router-view />
  </component>
</template>
