<script setup lang="ts">
import { onMounted, ref } from 'vue'
import { useAuthStore } from '@/stores/auth'
import { useRouter } from 'vue-router'
import { useToast } from '@/composables/useToast'
import { useI18n } from 'vue-i18n'

const authStore = useAuthStore()
const router = useRouter()
const { toast } = useToast()
const { t } = useI18n()
const error = ref<string | null>(null)

// Automatically trigger logout when this component mounts
onMounted(async () => {
    try {
        // If we're already logged out, just go to login page
        if (!authStore.isAuthenticated) {
            router.push('/')
            return
        }

        // Otherwise, perform logout
        const result = await authStore.logout()
        
        // Navigate based on result
        if (result && result.success) {
            // Already redirected by the store's logout method
        }
    } catch (err) {
        error.value = t('pages.logoutFailed')
        toast({
            title: t('pages.logoutError'),
            description: error.value,
            variant: "destructive",
        })
        
        // Force redirect to login after a short delay even if there was an error
        setTimeout(() => {
            router.push('/')
        }, 2000)
    }
})
</script>

<template>
    <div class="min-h-screen flex items-center justify-center bg-background">
        <div class="text-center">
            <h2 class="text-2xl font-semibold mb-2">
                {{ error ? t('pages.logoutError') : t('pages.loggingOut') }}
            </h2>
            <p class="text-muted-foreground">
                {{ error ? error : t('pages.pleaseWaitLogout') }}
            </p>
        </div>
    </div>
</template>
