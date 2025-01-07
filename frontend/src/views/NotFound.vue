<script setup lang="ts">
import { Button } from '@/components/ui/button'
import { useRouter } from 'vue-router'

const router = useRouter()
</script>

<template>
  <div class="flex min-h-[400px] flex-col items-center justify-center space-y-4">
    <div class="space-y-2 text-center">
      <h1 class="text-4xl font-bold">404</h1>
      <h2 class="text-xl font-semibold">Page not found</h2>
      <p class="text-muted-foreground">
        The page you're looking for doesn't exist or has been moved.
      </p>
    </div>
    <Button @click="router.push('/')">
      Go to Home
    </Button>
  </div>
</template>
