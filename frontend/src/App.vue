<script setup lang="ts">
import { onMounted } from 'vue'
import { useAuthStore } from '@/stores/auth'
import { Toaster } from '@/components/ui/toast'
import OuterApp from '@/layouts/OuterApp.vue'
import InnerApp from '@/layouts/InnerApp.vue'

const authStore = useAuthStore()

// Initialize auth state when app loads
onMounted(async () => {
  await authStore.initialize()
})
</script>

<template>
  <Toaster />

  <!-- Show login page if not authenticated -->
  <template v-if="!authStore.isAuthenticated">
    <OuterApp />
  </template>

  <!-- Show main app layout if authenticated -->
  <template v-else>
    <InnerApp />
  </template>
</template>
