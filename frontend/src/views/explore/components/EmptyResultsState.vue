<script setup lang="ts">
import { Button } from '@/components/ui/button'
import { EmptyState } from '@/components/layout'
import { CalendarIcon, FileSearch, Play, Search } from 'lucide-vue-next'

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
    <EmptyState
      :icon="FileSearch"
      title="No Logs Found"
      description="Your query returned no results for the selected time range. Try adjusting the query or time."
      class="h-full"
    >
      <template #action>
      <Button variant="outline" size="sm" class="mt-4 h-8" @click="openDatePicker">
        <CalendarIcon class="h-3.5 w-3.5 mr-2" />
        Adjust Timerange
      </Button>
      </template>
    </EmptyState>
  </template>

  <!-- Initial State -->
  <template v-else-if="canExecuteQuery">
    <EmptyState
      :icon="Search"
      title="Ready to Explore"
      description="Enter a query or use the default, then click 'Run' to see logs."
      class="h-full"
    >
      <template #action>
      <Button variant="outline" size="sm" @click="runDefaultQuery"
        class="border-primary/20 text-primary hover:bg-primary/5 hover:text-primary hover:border-primary/30">
        <Play class="h-3.5 w-3.5 mr-1.5" />
        Run default query
      </Button>
      </template>
    </EmptyState>
  </template>
</template>
