<template>
  <div class="query-history-item group">
    <div class="flex items-center gap-2 py-2 px-3 hover:bg-muted/50 rounded transition-colors cursor-pointer"
         @click="$emit('load', entry)">
      <!-- Mode Icon -->
      <component :is="modeIcon" class="w-3 h-3 flex-shrink-0 mt-0.5"
                 :class="entry.mode === 'logchefql' ? 'text-blue-500' : 'text-green-500'" />

      <!-- Query Content (Compact) -->
      <div class="flex-1 min-w-0">
        <p class="text-xs font-mono text-foreground/70 truncate leading-tight">
          {{ truncatedQuery }}
          <span v-if="isQueryTooLong" class="text-muted-foreground">...</span>
        </p>

        <!-- Subtle metadata -->
        <div class="flex items-center gap-2 mt-0.5">
          <span class="text-[10px] text-muted-foreground/60">
            {{ formatTimeAgo(entry.lastExecuted) }}
          </span>
          <span v-if="entry.executionCount > 1" class="text-[10px] text-muted-foreground/40">
            • {{ entry.executionCount }}x
          </span>
          <span v-if="entry.title" class="text-[10px] text-muted-foreground/40 truncate">
            • {{ entry.title }}
          </span>
        </div>
      </div>

      <!-- Delete Action (Only on hover) -->
      <Button
        variant="ghost"
        size="sm"
        class="h-5 w-5 p-0 opacity-0 group-hover:opacity-100 transition-opacity text-destructive hover:text-destructive flex-shrink-0"
        @click.stop="$emit('delete', entry.id)"
      >
        <Trash2 class="w-2.5 h-2.5" />
      </Button>
    </div>
  </div>
</template>

<script setup lang="ts">
import { computed } from 'vue'
import { Button } from '@/components/ui/button'
import { Search, Code2, Trash2 } from 'lucide-vue-next'
import type { QueryHistoryEntry } from '@/services/QueryHistoryService'

interface Props {
  entry: QueryHistoryEntry
}

interface Emits {
  (e: 'load', entry: QueryHistoryEntry): void
  (e: 'delete', entryId: string): void
}

const props = defineProps<Props>()
defineEmits<Emits>()

const MAX_DISPLAY_LENGTH = 60

const modeIcon = computed(() => {
  const mode = props.entry.mode
  return mode === 'logchefql' ? Search : Code2
})
const isQueryTooLong = computed(() => props.entry.query.length > MAX_DISPLAY_LENGTH)
const truncatedQuery = computed(() => {
  if (isQueryTooLong.value) {
    return props.entry.query.substring(0, MAX_DISPLAY_LENGTH)
  }
  return props.entry.query
})

const formatTimeAgo = (timestamp: number) => {
  const now = Date.now()
  const diffMs = now - timestamp
  const diffMinutes = Math.floor(diffMs / (1000 * 60))
  const diffHours = Math.floor(diffMs / (1000 * 60 * 60))
  const diffDays = Math.floor(diffMs / (1000 * 60 * 60 * 24))

  if (diffMinutes < 1) return 'now'
  if (diffMinutes < 60) return `${diffMinutes}m`
  if (diffHours < 24) return `${diffHours}h`
  if (diffDays < 7) return `${diffDays}d`

  // For older entries, show the date
  return new Date(timestamp).toLocaleDateString()
}
</script>

<style scoped>
.query-history-item {
  width: 100%;
}

.query-history-item .text-muted-foreground {
  color: hsl(var(--muted-foreground));
}

.query-history-item .font-mono {
  font-family: ui-monospace, SFMono-Regular, "SF Mono", Consolas, "Liberation Mono", Menlo, monospace;
}
</style>