<script setup lang="ts">
import { Button } from '@/components/ui/button'
import { Card, CardContent, CardDescription, CardHeader, CardTitle } from '@/components/ui/card'
import { Alert, AlertDescription, AlertTitle } from '@/components/ui/alert'
import { Input } from '@/components/ui/input'
import { Label } from '@/components/ui/label'
import { AlertCircle, Loader2 } from 'lucide-vue-next'
import { useAuthStore } from '@/stores/auth'
import { useMetaStore } from '@/stores/meta'
import { useRoute, useRouter } from 'vue-router'
import { computed, onMounted, ref } from 'vue'

const route = useRoute()
const router = useRouter()
const authStore = useAuthStore()
const metaStore = useMetaStore()
const isLoggingIn = ref(false)

const email = ref('')
const password = ref('')
const localError = ref<string | null>(null)

// Cold visits land here before the auth flow loads meta; fetch it so the
// local-auth form can render.
onMounted(() => {
  if (!metaStore.isInitialized) {
    metaStore.loadMeta()
  }
})

const localAuthEnabled = computed(() => metaStore.localAuthEnabled)
// Meta may not be loaded yet on a cold visit to /auth/login; OIDC stays the
// default assumption so the SSO button never flickers away for OIDC users.
const oidcEnabled = computed(() => metaStore.oidcEnabled)

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

async function handleLocalLogin() {
  if (!email.value.trim() || !password.value) {
    localError.value = 'Enter your email and password.'
    return
  }
  localError.value = null
  isLoggingIn.value = true
  try {
    const result = await authStore.localLogin(email.value.trim(), password.value)
    if (result?.success) {
      const redirectPath = (route.query.redirect as string) || '/logs/explore'
      await router.push(redirectPath)
    } else {
      localError.value = 'Invalid email or password.'
      password.value = ''
    }
  } catch (error) {
    console.error('Local login failed:', error)
    localError.value = 'Invalid email or password.'
    password.value = ''
  } finally {
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

        <form v-if="localAuthEnabled" class="space-y-3" @submit.prevent="handleLocalLogin">
          <div class="space-y-1.5">
            <Label for="login-email">Email</Label>
            <Input id="login-email" v-model="email" type="email" autocomplete="username" :disabled="isLoggingIn" />
          </div>
          <div class="space-y-1.5">
            <Label for="login-password">Password</Label>
            <Input id="login-password" v-model="password" type="password" autocomplete="current-password"
              :disabled="isLoggingIn" />
          </div>
          <p v-if="localError" class="text-sm text-destructive">{{ localError }}</p>
          <Button type="submit" class="w-full" :disabled="isLoggingIn">
            <Loader2 v-if="isLoggingIn" class="mr-2 h-4 w-4 animate-spin" />
            Sign in
          </Button>
        </form>

        <div v-if="localAuthEnabled && oidcEnabled" class="relative">
          <div class="absolute inset-0 flex items-center">
            <span class="w-full border-t" />
          </div>
          <div class="relative flex justify-center text-xs uppercase">
            <span class="bg-card px-2 text-muted-foreground">or</span>
          </div>
        </div>

        <Button v-if="oidcEnabled" @click="handleLogin" class="w-full" :variant="localAuthEnabled ? 'outline' : 'default'"
          :disabled="isLoggingIn">
          <Loader2 v-if="isLoggingIn && !localAuthEnabled" class="mr-2 h-4 w-4 animate-spin" />
          Sign in with SSO
        </Button>
      </CardContent>
    </Card>
  </div>
</template>
