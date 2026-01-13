<script setup lang="ts">
import { Toaster } from '@/components/ui/sonner'
import 'vue-sonner/style.css'
import OuterApp from '@/layouts/OuterApp.vue'
import InnerApp from '@/layouts/InnerApp.vue'
import { computed, onMounted } from 'vue'
import { useRoute } from 'vue-router'
import { useThemeStore } from '@/stores/theme'
import { useColorMode, useTitle } from '@vueuse/core'
import { useExploreStore } from '@/stores/explore'

const route = useRoute()
const themeStore = useThemeStore()
const colorMode = useColorMode()
const exploreStore = useExploreStore()

onMounted(() => {
  colorMode.value = themeStore.preference
})

// Dynamic page title - shows saved query name when viewing a collection
const pageTitle = computed(() => {
  const baseTitle = route.meta.title as string | undefined
  const queryName = exploreStore.activeSavedQueryName
  
  const isExplorerRoute = route.name === 'LogExplorer' || route.path.startsWith('/logs/collection')
  
  if (isExplorerRoute && queryName) {
    return `${queryName} - LogChef`
  }
  
  return baseTitle ? `${baseTitle} - LogChef` : 'LogChef'
})

useTitle(pageTitle)

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
