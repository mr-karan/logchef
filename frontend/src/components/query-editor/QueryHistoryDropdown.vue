<template>
  <Popover v-model:open="isOpen">
    <PopoverTrigger asChild>
      <Button variant="outline" size="sm" class="h-7 gap-1.5">
        <History class="w-3.5 h-3.5" />
        <span class="text-xs font-medium hidden sm:inline">History</span>
      </Button>
    </PopoverTrigger>
    <PopoverContent class="w-96 p-0" align="end">
      <div class="flex flex-col max-h-96">
        <!-- Header -->
        <div class="flex items-center justify-between p-3 border-b">
          <h3 class="text-sm font-medium">Query History</h3>
          <div class="flex items-center gap-2">
            <Button
              v-if="history.length > 0"
              variant="ghost"
              size="sm"
              class="h-6 text-xs"
              @click="clearHistory"
            >
              Clear All
            </Button>
            <Button
              variant="ghost"
              size="sm"
              class="h-6 w-6 p-0"
              @click="isOpen = false"
            >
              <X class="w-3 h-3" />
            </Button>
          </div>
        </div>

        <!-- Content -->
        <div v-if="isLoading" class="flex items-center justify-center p-8">
          <div class="flex items-center gap-2 text-sm text-muted-foreground">
            <Loader2 class="w-4 h-4 animate-spin" />
            Loading history...
          </div>
        </div>

        <div v-else-if="history.length === 0" class="flex flex-col items-center justify-center p-8 text-center">
          <History class="w-8 h-8 text-muted-foreground mb-2" />
          <p class="text-sm text-muted-foreground mb-1">No query history yet</p>
          <p class="text-xs text-muted-foreground">
            Execute queries to see them appear here
          </p>
        </div>

        <ScrollArea v-else class="flex-1">
          <div class="p-1">
            <QueryHistoryItem
              v-for="entry in history"
              :key="entry.id"
              :entry="entry"
              @load="handleLoadQuery"
              @delete="handleDeleteQuery"
            />
          </div>
        </ScrollArea>

        <!-- Footer with stats -->
        <div v-if="history.length > 0" class="flex items-center justify-between p-3 border-t text-xs text-muted-foreground">
          <span>{{ history.length }} recent queries</span>
          <span v-if="storageStats.totalEntries > 0">
            {{ storageStats.totalEntries }} total stored
          </span>
        </div>
      </div>
    </PopoverContent>
  </Popover>
</template>

<script setup lang="ts">
import { ref, watch } from 'vue'
import { Popover, PopoverContent, PopoverTrigger } from '@/components/ui/popover'
import { Button } from '@/components/ui/button'
import { ScrollArea } from '@/components/ui/scroll-area'
import { History, X, Loader2 } from 'lucide-vue-next'
import { queryHistoryService, type QueryHistoryEntry } from '@/services/QueryHistoryService'
import QueryHistoryItem from './QueryHistoryItem.vue'
import { useToast } from '@/composables/useToast'

interface Props {
  teamId: number | null
  sourceId: number | null
}

const props = defineProps<Props>()
const emit = defineEmits<{
  (e: 'loadQuery', mode: 'logchefql' | 'sql', query: string): void
}>()

const isOpen = ref(false)
const isLoading = ref(false)
const history = ref<QueryHistoryEntry[]>([])
const storageStats = ref<{ totalEntries: number; teamSourceGroups: number; oldestEntry?: number }>({
  totalEntries: 0,
  teamSourceGroups: 0
})

const { toast } = useToast()

// Load history when popover opens and team/source context changes
watch([isOpen, () => props.teamId, () => props.sourceId], async ([newIsOpen]) => {
  if (newIsOpen && props.teamId && props.sourceId) {
    await loadHistory()
  }
}, { immediate: true })

const loadHistory = async () => {
  if (!props.teamId || !props.sourceId) return

  isLoading.value = true
  try {
    history.value = queryHistoryService.getQueryHistory(props.teamId, props.sourceId)
    storageStats.value = queryHistoryService.getStorageStats()
  } catch (error) {
    console.error('Failed to load query history:', error)
    toast({
      title: 'Error',
      description: 'Failed to load query history',
      variant: 'destructive',
    })
  } finally {
    isLoading.value = false
  }
}

const handleLoadQuery = (entry: QueryHistoryEntry) => {
  emit('loadQuery', entry.mode, entry.query)
  isOpen.value = false
}

const handleDeleteQuery = async (entryId: string) => {
  try {
    queryHistoryService.deleteQueryEntry(entryId)
    await loadHistory() // Refresh the list
  } catch (error) {
    console.error('Failed to delete query from history:', error)
    toast({
      title: 'Error',
      description: 'Failed to remove query from history',
      variant: 'destructive',
    })
  }
}

const clearHistory = async () => {
  if (!props.teamId || !props.sourceId) return

  try {
    queryHistoryService.clearTeamSourceHistory(props.teamId, props.sourceId)
    history.value = []
    storageStats.value = queryHistoryService.getStorageStats()
  } catch (error) {
    console.error('Failed to clear query history:', error)
    toast({
      title: 'Error',
      description: 'Failed to clear query history',
      variant: 'destructive',
    })
  }
}
</script>

<style scoped>
/* Add any component-specific styles here */
</style>