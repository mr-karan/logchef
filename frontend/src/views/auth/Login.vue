<script setup lang="ts">
import { Button } from '@/components/ui/button'
import { Card, CardContent, CardDescription, CardHeader, CardTitle } from '@/components/ui/card'
import { Alert, AlertDescription, AlertTitle } from '@/components/ui/alert'
import { AlertCircle, Loader2 } from 'lucide-vue-next'
import { useAuthStore } from '@/stores/auth'
import { useRoute } from 'vue-router'
import { computed, ref } from 'vue'

const route = useRoute()
const authStore = useAuthStore()
const isLoggingIn = ref(false)

// Error message mapping
const errorMessages: Record<string, string> = {
  'UNAUTHORIZED_USER': 'Access denied. Please contact your administrator to request access.',
  'USER_INACTIVE': 'Your account is inactive. Please contact your administrator.',
  'invalid_state': 'Authentication session expired. Please try again.',
  'invalid_request': 'Invalid authentication request. Please try again.',
  'authentication_failed': 'Authentication failed. Please try again.',
}

// Get error message if present
const errorMessage = computed(() => {
  const code = route.query.error as string
  return code ? (errorMessages[code] || 'An unexpected error occurred') : null
})

async function handleLogin() {
  try {
    isLoggingIn.value = true
    // Get redirect path from query if available
    const redirectPath = route.query.redirect as string | undefined
    await authStore.startLogin(redirectPath)
  } catch (error) {
    console.error('Login initiation failed:', error)
  } finally {
    // This may not run if redirection happens immediately
    isLoggingIn.value = false
  }
}
</script>

<template>
  <div class="min-h-screen flex items-center justify-center bg-background p-4">
    <Card class="mx-auto w-full max-w-sm">
      <CardHeader>
        <CardTitle class="text-2xl text-center">
          Welcome to LogChef
        </CardTitle>
        <CardDescription class="text-center">
          Your centralized log analytics platform
        </CardDescription>
      </CardHeader>
      <CardContent class="space-y-4">
        <!-- Show error message if present -->
        <Alert v-if="errorMessage" variant="destructive">
          <AlertCircle class="h-4 w-4 mr-2" />
          <div>
            <AlertTitle>Authentication Error</AlertTitle>
            <AlertDescription>
              {{ errorMessage }}
            </AlertDescription>
          </div>
        </Alert>

        <Button 
          @click="handleLogin" 
          class="w-full" 
          :disabled="isLoggingIn"
        >
          <Loader2 v-if="isLoggingIn" class="mr-2 h-4 w-4 animate-spin" />
          Sign in with SSO
        </Button>
      </CardContent>
    </Card>
  </div>
</template>
