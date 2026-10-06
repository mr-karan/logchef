<script setup lang="ts">
import { Button } from '@/components/ui/button'
import { Card, CardContent, CardDescription, CardHeader, CardTitle } from '@/components/ui/card'
import { Alert, AlertDescription, AlertTitle } from '@/components/ui/alert'
import { Input } from '@/components/ui/input'
import { Label } from '@/components/ui/label'
import { AlertCircle, KeyRound, Loader2 } from 'lucide-vue-next'
import { useAuthStore } from '@/stores/auth'
import { useMetaStore } from '@/stores/meta'
import { prefillEmptyLoginFields } from '@/utils/demoCredentials'
import { safeRedirectPath } from '@/utils/safeRedirect'
import { useRoute, useRouter } from 'vue-router'
import { computed, onMounted, ref, watch } from 'vue'
import { useI18n } from 'vue-i18n'

const route = useRoute()
const router = useRouter()
const { t } = useI18n()
const authStore = useAuthStore()
const metaStore = useMetaStore()
const isLoggingIn = ref(false)

// Demo credentials are advertised explicitly by the running server. Keeping
// this runtime-only means disabling the server option immediately removes them
// from the login page, regardless of how the frontend image was built.
const demoCredentials = computed(() => metaStore.demoLoginCredentials)
const showDemoCredentials = computed(() => demoCredentials.value !== null)

const email = ref('')
const password = ref('')
const localError = ref<string | null>(null)

watch(demoCredentials, (credentials) => {
  if (!credentials) return
  const prefilled = prefillEmptyLoginFields(
    { email: email.value, password: password.value },
    credentials,
  )
  email.value = prefilled.email
  password.value = prefilled.password
}, { immediate: true })

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
// Get error message if present
const errorMessage = computed(() => {
  const code = route.query.error as string
  if (!code) return null
  const messages: Record<string, string> = {
    UNAUTHORIZED_USER: t('pages.loginUnauthorized'),
    USER_INACTIVE: t('pages.loginInactive'),
    invalid_state: t('pages.loginSessionExpired'),
    invalid_request: t('pages.loginInvalidRequest'),
    authentication_failed: t('pages.loginFailed'),
  }
  return messages[code] || t('pages.unexpectedError')
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
    localError.value = t('pages.enterEmailPassword')
    return
  }
  localError.value = null
  isLoggingIn.value = true
  try {
    const result = await authStore.localLogin(email.value.trim(), password.value)
    if (result?.success) {
      // Same rule as the server applies to the OIDC redirect.
      await router.push(safeRedirectPath(route.query.redirect, '/logs/explore'))
    } else {
      localError.value = t('pages.invalidEmailPassword')
      password.value = ''
    }
  } catch (error) {
    console.error('Local login failed:', error)
    localError.value = t('pages.invalidEmailPassword')
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
          {{ t('pages.welcomeToLogChef') }}
        </CardTitle>
        <CardDescription class="text-center">
          {{ t('pages.loginDescription') }}
        </CardDescription>
      </CardHeader>
      <CardContent class="space-y-4">
        <!-- Show error message if present -->
        <Alert v-if="errorMessage" variant="destructive">
          <AlertCircle class="h-4 w-4 mr-2" />
          <div>
            <AlertTitle>{{ t('pages.authenticationError') }}</AlertTitle>
            <AlertDescription>
              {{ errorMessage }}
            </AlertDescription>
          </div>
        </Alert>

        <div v-if="localAuthEnabled && showDemoCredentials"
          class="rounded-md border bg-muted/40 px-3 py-2.5 text-sm">
          <div class="mb-2 flex items-center gap-2 font-medium">
            <KeyRound class="h-4 w-4 text-muted-foreground" />
            {{ t('pages.demoCredentials') }}
          </div>
          <dl class="grid grid-cols-[auto_1fr] gap-x-3 gap-y-1 text-xs">
            <dt class="text-muted-foreground">{{ t('ui.email') }}</dt>
            <dd class="select-all font-mono">{{ demoCredentials?.email }}</dd>
            <dt class="text-muted-foreground">{{ t('sources.password') }}</dt>
            <dd class="select-all font-mono">{{ demoCredentials?.password }}</dd>
          </dl>
        </div>

        <form v-if="localAuthEnabled" class="space-y-3" @submit.prevent="handleLocalLogin">
          <div class="space-y-1.5">
            <Label for="login-email">{{ t('ui.email') }}</Label>
            <Input id="login-email" v-model="email" type="email" autocomplete="username" :disabled="isLoggingIn" />
          </div>
          <div class="space-y-1.5">
            <Label for="login-password">{{ t('sources.password') }}</Label>
            <Input id="login-password" v-model="password" type="password" autocomplete="current-password"
              :disabled="isLoggingIn" />
          </div>
          <p v-if="localError" class="text-sm text-destructive">{{ localError }}</p>
          <Button type="submit" class="w-full" :disabled="isLoggingIn">
            <Loader2 v-if="isLoggingIn" class="mr-2 h-4 w-4 animate-spin" />
            {{ t('pages.signIn') }}
          </Button>
        </form>

        <div v-if="localAuthEnabled && oidcEnabled" class="relative">
          <div class="absolute inset-0 flex items-center">
            <span class="w-full border-t" />
          </div>
          <div class="relative flex justify-center text-xs uppercase">
            <span class="bg-card px-2 text-muted-foreground">{{ t('sources.or') }}</span>
          </div>
        </div>

        <Button v-if="oidcEnabled" @click="handleLogin" class="w-full" :variant="localAuthEnabled ? 'outline' : 'default'"
          :disabled="isLoggingIn">
          <Loader2 v-if="isLoggingIn && !localAuthEnabled" class="mr-2 h-4 w-4 animate-spin" />
          {{ t('pages.signInWithSSO') }}
        </Button>
      </CardContent>
    </Card>
  </div>
</template>
