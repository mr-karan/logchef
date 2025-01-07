<script setup lang="ts">
import { Card, CardContent, CardDescription, CardHeader, CardTitle } from '@/components/ui/card'
</script>

<template>
  <div class="space-y-6">
    <div class="flex justify-between items-center">
      <h1 class="text-3xl font-bold tracking-tight">Query Explorer</h1>
    </div>
    
    <Card>
      <CardHeader>
        <CardTitle>New Query</CardTitle>
        <CardDescription>
          Write and execute queries to analyze your log data
        </CardDescription>
      </CardHeader>
      <CardContent>
        <div class="h-[600px] rounded-lg border border-dashed flex items-center justify-center">
          Query Editor Coming Soon
        </div>
      </CardContent>
    </Card>
  </div>
</template>
