<script setup lang="ts">
import { onMounted } from 'vue'
import { useAuthStore } from '@/stores/auth'
import { useRouter } from 'vue-router'

const authStore = useAuthStore()
const router = useRouter()

// Automatically trigger logout when this component mounts
onMounted(async () => {
    // If we're already logged out, just go to login page
    if (!authStore.isAuthenticated) {
        router.push('/')
        return
    }

    // Otherwise, perform logout
    await authStore.logout()
})
</script>

<template>
    <div class="min-h-screen flex items-center justify-center bg-background">
        <div class="text-center">
            <h2 class="text-2xl font-semibold mb-2">Logging out...</h2>
            <p class="text-muted-foreground">Please wait while we sign you out.</p>
        </div>
    </div>
</template>