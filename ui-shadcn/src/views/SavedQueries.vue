<script setup lang="ts">
import { Card, CardContent, CardDescription, CardHeader, CardTitle } from '@/components/ui/card'
</script>

<template>
  <div class="space-y-6">
    <div class="flex justify-between items-center">
      <h1 class="text-3xl font-bold tracking-tight">Saved Queries</h1>
    </div>
    
    <Card>
      <CardHeader>
        <CardTitle>Your Saved Queries</CardTitle>
        <CardDescription>
          View and manage your saved log queries
        </CardDescription>
      </CardHeader>
      <CardContent>
        <div class="rounded-lg border p-4">
          No saved queries yet
        </div>
      </CardContent>
    </Card>
  </div>
</template>
