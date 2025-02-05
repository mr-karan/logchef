<script setup lang="ts">
import { Search } from 'lucide-vue-next'
</script>

<template>
  <div class="flex flex-col items-center justify-center h-full text-center space-y-4">
    <div class="rounded-full bg-muted p-4">
      <Search class="h-8 w-8 text-muted-foreground" />
    </div>
    <h3 class="text-lg font-medium">No logs found</h3>
    <p class="text-sm text-muted-foreground max-w-[400px]">
      Try adjusting your search query, time range, or selected source to find the logs you're looking for.
    </p>
  </div>
</template>
