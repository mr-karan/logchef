<script setup lang="ts">
import { Button } from '@/components/ui/button'
import { CalendarIcon } from 'lucide-vue-next'

interface Props {
  hasExecutedQuery: boolean;
  canExecuteQuery: boolean;
}

defineProps<Props>()

const emit = defineEmits<{
  (e: 'runDefaultQuery'): void
  (e: 'openDatePicker'): void
}>()

const runDefaultQuery = () => emit('runDefaultQuery')
const openDatePicker = () => emit('openDatePicker')
</script>

<template>
  <!-- No Results State -->
  <template v-if="hasExecutedQuery">
    <div class="h-full flex flex-col items-center justify-center p-10 text-center">
      <svg xmlns="http://www.w3.org/2000/svg" width="36" height="36" viewBox="0 0 24 24" fill="none"
        stroke="currentColor" stroke-width="1.5" stroke-linecap="round" stroke-linejoin="round"
        class="text-muted-foreground mb-3">
        <path d="M14.5 2H6a2 2 0 0 0-2 2v16a2 2 0 0 0 2 2h12a2 2 0 0 0 2-2V7.5L14.5 2z"></path>
        <polyline points="14 2 14 8 20 8"></polyline>
        <line x1="12" x2="12" y1="18" y2="12"></line>
        <line x1="9" x2="15" y1="15" y2="15"></line>
      </svg>
      <h3 class="text-lg font-medium mb-1">No Logs Found</h3>
      <p class="text-sm text-muted-foreground max-w-md">
        Your query returned no results for the selected time range. Try adjusting the query or time.
      </p>
      <Button variant="outline" size="sm" class="mt-4 h-8" @click="openDatePicker">
        <CalendarIcon class="h-3.5 w-3.5 mr-2" />
        Adjust Timerange
      </Button>
    </div>
  </template>

  <!-- Initial State -->
  <template v-else-if="canExecuteQuery">
    <div class="h-full flex flex-col items-center justify-center p-10 text-center">
      <div class="bg-primary/5 p-6 rounded-full mb-4">
        <svg xmlns="http://www.w3.org/2000/svg" width="40" height="40" viewBox="0 0 24 24" fill="none"
          stroke="currentColor" stroke-width="1.5" stroke-linecap="round" stroke-linejoin="round"
          class="text-primary">
          <circle cx="11" cy="11" r="8"></circle>
          <line x1="21" x2="16.65" y1="21" y2="16.65"></line>
          <line x1="11" x2="11" y1="8" y2="14"></line>
          <line x1="8" x2="14" y1="11" y2="11"></line>
        </svg>
      </div>
      <h3 class="text-xl font-medium mb-2">Ready to Explore</h3>
      <p class="text-sm text-muted-foreground max-w-md mb-4">
        Enter a query or use the default, then click 'Run' to see logs.
      </p>
      <Button variant="outline" size="sm" @click="runDefaultQuery"
        class="border-primary/20 text-primary hover:bg-primary/5 hover:text-primary hover:border-primary/30">
        <svg class="h-3.5 w-3.5 mr-1.5" xmlns="http://www.w3.org/2000/svg" viewBox="0 0 24 24" fill="none"
          stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round">
          <polygon points="5 3 19 12 5 21 5 3"></polygon>
        </svg>
        Run default query
      </Button>
    </div>
  </template>
</template>