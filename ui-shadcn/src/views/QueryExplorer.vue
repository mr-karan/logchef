<script setup lang="ts">
import { Card, CardContent, CardDescription, CardHeader, CardTitle } from '@/components/ui/card'
</script>

<template>
  <div class="space-y-4">
    <div class="flex justify-between items-center">
      <h1 class="text-3xl font-bold tracking-tight">Query Explorer</h1>
    </div>

    <!-- Render nested route components -->
    <RouterView />
  </div>
</template>
