<script setup lang="ts">
import {
  Table,
  TableBody,
  TableCell,
  TableHead,
  TableHeader,
  TableRow
} from '@/components/ui/table'
import { ScrollArea, ScrollBar } from '@/components/ui/scroll-area'
import ColumnSelector from './ColumnSelector.vue'
import { ref, watch } from 'vue'
import type { QueryStats } from '@/api/explore'
import {
  Accordion,
  AccordionContent,
  AccordionItem,
  AccordionTrigger
} from '@/components/ui/accordion'
import { ChevronUp, Copy, Check } from 'lucide-vue-next'

interface Props {
  logs: Record<string, any>[]
  columns: { name: string; type: string }[]
  stats: QueryStats
}

const props = defineProps<Props>()

// Track expanded row
const expandedRow = ref<string | null>(null)
const copyState = ref<{ [key: string]: boolean }>({})

const toggleRow = (idx: number) => {
  expandedRow.value = expandedRow.value === `item-${idx}` ? null : `item-${idx}`
}

const copyToClipboard = async (content: string, idx: number) => {
  try {
    await navigator.clipboard.writeText(content)
    copyState.value[idx] = true
    setTimeout(() => {
      copyState.value[idx] = false
    }, 2000)
  } catch (err) {
    console.error('Failed to copy:', err)
  }
}

// Initialize selected columns with timestamp
const selectedColumns = ref<string[]>(['timestamp'])

// Watch for column changes and ensure timestamp is selected if available
watch(
  () => props.columns,
  (newColumns) => {
    if (!newColumns?.length) return
    
    // Check if timestamp exists in the schema
    const hasTimestamp = newColumns.some(col => col.name === 'timestamp')
    
    // Initialize with timestamp if available, otherwise use first column
    selectedColumns.value = hasTimestamp 
      ? ['timestamp']
      : [newColumns[0].name]
  },
  { immediate: true }
)

const formatCellValue = (value: any): string => {
  if (value === null || value === undefined) return '-'
  if (typeof value === 'object') return JSON.stringify(value, null, 2)
  return String(value)
}

const formatTimestamp = (timestamp: string): string => {
  try {
    return new Date(timestamp).toLocaleString()
  } catch (e) {
    return timestamp
  }
}
</script>

<template>
  <div class="h-full flex flex-col">
    <!-- Stats Bar -->
    <div class="flex items-center justify-between border-b pb-2">
      <ColumnSelector v-model="selectedColumns" :columns="columns" />
      <div class="flex items-center gap-3 text-sm">
        <div class="flex items-center gap-1.5">
          <div class="h-1.5 w-1.5 rounded-full bg-primary"></div>
          <span>{{ logs.length }} results</span>
        </div>
        <div class="flex items-center gap-1.5">
          <div class="h-1.5 w-1.5 rounded-full bg-primary/60"></div>
          <span>{{ stats?.execution_time_ms?.toFixed(2) ?? 0 }}ms</span>
        </div>
      </div>
    </div>

    <!-- Table Container with Fixed Height -->
    <div class="relative flex-1 min-h-0">
      <ScrollArea class="h-full rounded-md border">
        <div class="min-w-full">
          <Table>
            <TableHeader>
              <TableRow class="border-b border-b-muted-foreground/20">
                <TableHead 
                  v-for="colName in selectedColumns" 
                  :key="colName"
                  class="h-8 whitespace-nowrap p-0 pl-2 pr-4 text-xs font-medium sticky top-0 bg-background"
                >
                  {{ colName }}
                </TableHead>
              </TableRow>
            </TableHeader>
            <TableBody>
              <template v-for="(row, idx) in logs" :key="row.id || idx">
                <TableRow 
                  @click="toggleRow(idx)"
                  class="border-b border-b-muted-foreground/10 hover:bg-muted/50 cursor-pointer relative group"
                >
                  <TableCell 
                    v-for="colName in selectedColumns" 
                    :key="colName"
                    class="p-0 pl-2 pr-4 py-1 font-mono text-xs"
                  >
                    <template v-if="colName === 'timestamp'">
                      {{ formatTimestamp(row[colName]) }}
                    </template>
                    <template v-else>
                      {{ formatCellValue(row[colName]) }}
                    </template>
                  </TableCell>
                  <div v-if="expandedRow === `item-${idx}`" class="absolute right-2 top-1/2 -translate-y-1/2">
                    <ChevronUp class="h-4 w-4 text-muted-foreground" />
                  </div>
                </TableRow>
                <TableRow v-if="expandedRow === `item-${idx}`">
                  <TableCell :colspan="selectedColumns.length" class="p-0">
                    <div class="px-2 pb-2">
                      <div class="rounded bg-muted/50 p-2 font-mono text-xs whitespace-pre overflow-x-auto relative group">
                        <button 
                          @click.stop="copyToClipboard(formatCellValue(row), idx)"
                          class="absolute right-2 top-2 p-1 rounded hover:bg-background/80 transition-colors"
                          :title="copyState[idx] ? 'Copied!' : 'Copy to clipboard'"
                        >
                          <Check v-if="copyState[idx]" class="h-3 w-3 text-green-500" />
                          <Copy v-else class="h-3 w-3 text-muted-foreground" />
                        </button>
                        {{ formatCellValue(row) }}
                      </div>
                    </div>
                  </TableCell>
                </TableRow>
              </template>
            </TableBody>
          </Table>
        </div>
        <ScrollBar orientation="horizontal" />
      </ScrollArea>
    </div>
  </div>
</template>
