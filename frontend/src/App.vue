<script setup lang="ts">
import { Toaster } from '@/components/ui/sonner'
import 'vue-sonner/style.css'
import OuterApp from '@/layouts/OuterApp.vue'
import InnerApp from '@/layouts/InnerApp.vue'
import { computed, onMounted } from 'vue'
import { useRoute } from 'vue-router'
import { useThemeStore } from '@/stores/theme'
import { useColorMode } from '@vueuse/core'

const route = useRoute()
const themeStore = useThemeStore()
const colorMode = useColorMode()

// Initialize theme from store
onMounted(() => {
  // Set the color mode from our persisted preference
  colorMode.value = themeStore.preference
})

// Determine layout based on route meta
const layout = computed(() => {
  return route.meta.layout === 'outer' ? OuterApp : InnerApp
})
</script>

<template>
  <Toaster 
    position="top-right" 
    closeButton 
    richColors
    :visibleToasts="5"
  />
  <component :is="layout">
    <router-view />
  </component>
</template>
