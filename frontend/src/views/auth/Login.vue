<script setup lang="ts">
import { Button } from '@/components/ui/button'
import { useAuthStore } from '@/stores/auth'

const authStore = useAuthStore()

function handleLogin() {
  authStore.login()
}
</script>

<template>
  <div class="flex min-h-full flex-col justify-center">
    <div class="sm:mx-auto sm:w-full sm:max-w-md">
      <h2 class="mt-6 text-center text-2xl font-bold leading-9 tracking-tight">
        Welcome to LogChef
      </h2>
      <p class="mt-2 text-center text-sm text-muted-foreground">
        Your centralized log analytics platform
      </p>
    </div>

    <div class="mt-10 sm:mx-auto sm:w-full sm:max-w-[480px]">
      <div class="bg-card px-6 py-12 shadow sm:rounded-lg sm:px-12">
        <div class="space-y-6">
          <div class="text-center">
            <Button @click="handleLogin" size="lg">
              Sign in with SSO
            </Button>
          </div>
        </div>
      </div>
    </div>
  </div>
</template>