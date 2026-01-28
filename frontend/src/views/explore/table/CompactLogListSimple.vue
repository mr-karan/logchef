<script setup lang="ts">
import { computed, ref, watch } from 'vue'
import { ScrollArea } from '@/components/ui/scroll-area'
import { Button } from '@/components/ui/button'
import TableControls from './TableControls.vue'
import LogTimelineModal from '@/components/log-timeline/LogTimelineModal.vue'
import { Clock } from 'lucide-vue-next'
import { 
  useVueTable, 
  getCoreRowModel, 
  getPaginationRowModel,
  getFilteredRowModel,
  type PaginationState,
  type VisibilityState,
  type ColumnDef
} from '@tanstack/vue-table'
import { valueUpdater } from '@/lib/utils'
import type { QueryStats } from '@/api/explore'

interface Props {
  columns?: ColumnDef<Record<string, any>>[]
  data?: Record<string, any>[]  // Primary prop - same as DataTable
  stats?: QueryStats
  isLoading?: boolean
  sourceId?: string | number
  teamId?: string | number
  timestampField?: string
  severityField?: string
  timezone?: 'local' | 'utc'
  queryFields?: any[]
  regexHighlights?: Record<string, { pattern: string; isNegated: boolean }>
}

const props = withDefaults(defineProps<Props>(), {
  columns: () => [],
  data: () => [],
  stats: undefined,
  isLoading: false,
  sourceId: 0,
  teamId: 0,
  timestampField: 'timestamp',
  severityField: 'level',
  timezone: 'local',
  queryFields: () => [],
  regexHighlights: () => ({})
})

// Use 'data' prop - consistent with DataTable component
const logs = computed(() => props.data)

defineEmits<{
  'drill-down': [data: { column: string; value: any; operator: string }]
}>()

// Table state
const pagination = ref<PaginationState>({
  pageIndex: 0,
  pageSize: 100, // Default to 100 for compact view
})
const globalFilter = ref('')
const columnVisibility = ref<VisibilityState>({})
const columnOrder = ref<string[]>([])
const displayTimezone = ref<'local' | 'utc'>(props.timezone)

// Watch for external timezone prop changes
watch(() => props.timezone, (newVal) => {
  displayTimezone.value = newVal
})

// Create proper column definitions for the table - ensure all columns have proper IDs
const tableColumns = computed<ColumnDef<Record<string, any>>[]>(() => {
  if (props.columns?.length > 0) {
    // Handle both ColumnInfo objects (from store) and ColumnDef objects (from createColumns)
    return props.columns.map(col => {
      // ColumnInfo has 'name' property, ColumnDef has 'id' and 'accessorKey'
      const colAny = col as any
      const colName = colAny.name // ColumnInfo.name
      const id = col.id || colAny.accessorKey || colName || String(col.header) || 'unknown'
      
      return {
        ...col,
        id: id,
        accessorKey: colAny.accessorKey || colName || id,
        header: col.header || colName || id,
        cell: col.cell || (({ getValue }) => getValue()),
      }
    })
  }
  
  // Fallback: create basic columns from the first log entry
  if (logs.value.length > 0) {
    const firstLog = logs.value[0]
    return Object.keys(firstLog).map(key => ({
      id: key,
      accessorKey: key,
      header: key,
      cell: ({ getValue }) => getValue(),
    }))
  }
  
  return []
})

// Initialize column state based on columns
watch(tableColumns, (newColumns) => {
  if (newColumns.length > 0) {
    const newVisibility: VisibilityState = {}
    const newOrder: string[] = []
    
    newColumns.forEach(col => {
      if (col.id) {
        newVisibility[col.id] = true
        newOrder.push(col.id)
      }
    })
    
    columnVisibility.value = newVisibility
    columnOrder.value = newOrder
  }
}, { immediate: true })

// Create table
const table = useVueTable({
  get data() {
    return logs.value
  },
  get columns() {
    return tableColumns.value
  },
  state: {
    get pagination() {
      return pagination.value
    },
    get globalFilter() {
      return globalFilter.value
    },
    get columnVisibility() {
      return columnVisibility.value
    },
    get columnOrder() {
      return columnOrder.value
    },
  },
  onPaginationChange: updaterOrValue => valueUpdater(updaterOrValue, pagination),
  onGlobalFilterChange: updaterOrValue => valueUpdater(updaterOrValue, globalFilter),
  onColumnVisibilityChange: updaterOrValue => valueUpdater(updaterOrValue, columnVisibility),
  onColumnOrderChange: updaterOrValue => valueUpdater(updaterOrValue, columnOrder),
  getCoreRowModel: getCoreRowModel(),
  getPaginationRowModel: getPaginationRowModel(),
  getFilteredRowModel: getFilteredRowModel(),
})

// Format timestamp for compact view
const formatTimestamp = (timestamp: any) => {
  if (!timestamp) return ''
  
  const date = new Date(timestamp)
  if (isNaN(date.getTime())) return String(timestamp)
  
  // Use compact format: HH:mm:ss
  const options: Intl.DateTimeFormatOptions = {
    hour: '2-digit',
    minute: '2-digit',
    second: '2-digit',
    hour12: false
  }
  
  if (displayTimezone.value === 'utc') {
    options.timeZone = 'UTC'
  }
  
  return date.toLocaleString(undefined, options)
}

// HTML escape utility to prevent XSS attacks
const escapeHtml = (text: string): string => {
  const htmlEscapeMap: Record<string, string> = {
    '&': '&amp;',
    '<': '&lt;',
    '>': '&gt;',
    '"': '&quot;',
    "'": '&#39;'
  }
  return text.replace(/[&<>"']/g, char => htmlEscapeMap[char])
}

// Format value for logfmt
const formatLogfmtValue = (value: any): string => {
  if (value === null || value === undefined) return 'null'
  if (typeof value === 'string') {
    // Quote strings that contain spaces or special characters
    if (value.includes(' ') || value.includes('=') || value.includes('"')) {
      return `"${value.replace(/"/g, '\\"')}"`
    }
    return value
  }
  if (typeof value === 'number' || typeof value === 'boolean') {
    return String(value)
  }
  // For objects/arrays, stringify but keep it compact
  return `"${JSON.stringify(value).replace(/"/g, '\\"')}"`
}

// Build message from log entry in logfmt format (returns raw text for tooltips/matching)
const buildMessage = (row: Record<string, any>) => {
  // Try common message fields first
  const messageFields = ['message', 'msg', 'log', 'text', 'body']
  let primaryMessage = ''
  
  for (const field of messageFields) {
    if (row[field] && typeof row[field] === 'string') {
      primaryMessage = row[field]
      break
    }
  }
  
  // Get all other fields in logfmt format
  const skipFields = new Set([props.timestampField, props.severityField, 'id', '_id'])
  const logfmtFields = Object.entries(row)
    .filter(([key, value]) => {
      // Skip timestamp/severity and already used message field
      if (skipFields.has(key)) return false
      if (primaryMessage && messageFields.includes(key)) return false
      return value !== null && value !== undefined
    })
    .map(([key, value]) => `${key}=${formatLogfmtValue(value)}`)
    .join(' ')
  
  // Combine primary message with logfmt fields
  if (primaryMessage && logfmtFields) {
    return `${primaryMessage} ${logfmtFields}`
  } else if (primaryMessage) {
    return primaryMessage
  } else if (logfmtFields) {
    return logfmtFields
  }
  
  // Fallback to basic logfmt for all fields
  return Object.entries(row)
    .filter(([key, value]) => !skipFields.has(key) && value !== null && value !== undefined)
    .map(([key, value]) => `${key}=${formatLogfmtValue(value)}`)
    .join(' ') || 'empty'
}

// Highlight text based on regex patterns (works with pre-escaped HTML from highlightLogfmt)
// Uses a safe approach that doesn't break existing HTML structure
const highlightText = (text: string, field: string) => {
  const highlight = props.regexHighlights[field]
  if (!highlight || !text) return text
  
  if (highlight.isNegated) {
    return text // Don't highlight negated patterns visually
  }
  
  try {
    // Escape the pattern to prevent regex injection, then create regex
    // The text is already HTML-escaped, so we escape the pattern for literal matching
    const escapedPattern = highlight.pattern.replace(/[.*+?^${}()|[\]\\]/g, '\\$&')
    // Also need to match the HTML-escaped version of the pattern
    const htmlEscapedPattern = escapeHtml(highlight.pattern).replace(/[.*+?^${}()|[\]\\]/g, '\\$&')
    
    // Try to match both the original pattern (for non-HTML chars) and escaped pattern
    const combinedPattern = escapedPattern === htmlEscapedPattern 
      ? escapedPattern 
      : `(?:${escapedPattern}|${htmlEscapedPattern})`
    
    const regex = new RegExp(combinedPattern, 'gi')
    
    // Only highlight text outside of HTML tags to avoid breaking existing markup
    // Split by HTML tags, highlight text parts, rejoin
    const parts = text.split(/(<[^>]+>)/g)
    const highlighted = parts.map(part => {
      // If it's an HTML tag, leave it alone
      if (part.startsWith('<') && part.endsWith('>')) {
        return part
      }
      // Otherwise, apply highlighting to text content
      return part.replace(regex, '<mark class="bg-yellow-200 dark:bg-yellow-800 px-0.5 rounded">$&</mark>')
    }).join('')
    
    return highlighted
  } catch (e) {
    // If regex is invalid, return the text as-is (already escaped by highlightLogfmt)
    return text
  }
}

// Get severity styling (border + background tint)
const getSeverityClasses = (severity: string) => {
  if (!severity) return { border: 'border-l-muted', bg: '' }
  
  const sev = severity.toLowerCase()
  switch (sev) {
    case 'error':
    case 'err':
    case 'fatal':
      return { 
        border: 'border-l-red-500', 
        bg: 'hover:bg-red-50/40 dark:hover:bg-red-900/10' 
      }
    case 'warn':
    case 'warning':
      return { 
        border: 'border-l-yellow-500', 
        bg: 'hover:bg-yellow-50/40 dark:hover:bg-yellow-900/10' 
      }
    case 'info':
    case 'information':
      return { 
        border: 'border-l-blue-500', 
        bg: 'hover:bg-blue-50/40 dark:hover:bg-blue-900/10' 
      }
    case 'debug':
    case 'trace':
      return { 
        border: 'border-l-gray-400', 
        bg: 'hover:bg-gray-50/40 dark:hover:bg-gray-900/10' 
      }
    default:
      return { border: 'border-l-muted', bg: '' }
  }
}

// Enhanced logfmt syntax highlighting (with XSS protection)
const highlightLogfmt = (text: string) => {
  if (!text) return text
  
  // First escape HTML to prevent XSS attacks
  const escapedText = escapeHtml(text)
  
  // key=value where value is either:
  //   • "quoted string with possible escaped quotes", or
  //   • bare token without spaces
  // Example handled: foo="bar baz", foo="{\"a\":\"b\"}", foo=123
  // Note: We're matching on escaped text, so HTML entities are safe
  return escapedText.replace(
    /(\w+)=(&quot;(?:\\.|[^&]|&(?!quot;))*&quot;|[^\s]+)/g,
    (_match, key, val) =>
      `<span class="text-sky-600 dark:text-sky-400">${key}</span>` +
      `<span class="text-muted-foreground">=</span>` +
      `<span class="text-amber-600 dark:text-amber-400">${val}</span>`
  )
}

// Pre-computed rows for performance - using table's paginated data
const renderedRows = computed(() => {
  // Use paginated and filtered rows from the table
  const paginatedRows = table.getRowModel().rows
  
  return paginatedRows.map((tableRow) => {
    const row = tableRow.original
    const ts = formatTimestamp(row[props.timestampField])
    const sev = row[props.severityField] || ''
    const rawMsg = buildMessage(row)
    const msg = highlightLogfmt(rawMsg)
    const severityStyles = getSeverityClasses(sev)
    
    return {
      id: `${row[props.timestampField]}-${tableRow.id}`, // Use table row ID for stability
      timestamp: ts,
      severity: sev,
      message: msg,
      rawMessage: rawMsg,
      raw: row,
      borderClass: severityStyles.border,
      bgClass: severityStyles.bg
    }
  })
})

// Handle row click interactions
const expandedRowId = ref<string | null>(null)

// Context modal state
const showContextModal = ref(false)
const contextLog = ref<Record<string, any> | null>(null)

// Open context modal for a log
const openContextModal = (log: Record<string, any>, event: Event) => {
  event.stopPropagation()
  contextLog.value = log
  showContextModal.value = true
}

// Simple click to expand/collapse (only if not selecting text)
const handleClick = (event: MouseEvent, rowId: string) => {
  // Don't toggle if user is selecting text
  const selection = window.getSelection()
  if (selection && selection.toString().length > 0) {
    return
  }

  // Don't toggle if the click was part of a text selection
  if (event.detail > 1) {
    return
  }

  expandedRowId.value = expandedRowId.value === rowId ? null : rowId
}
</script>

<template>
  <div class="h-full flex flex-col">
    <!-- Shared Table Controls -->
    <TableControls 
      v-if="table"
      :table="table"
      :stats="stats"
      :is-loading="isLoading"
      :show-column-selector="false"
      @update:timezone="displayTimezone = $event"
      @update:globalFilter="globalFilter = $event"
    />
    
    <!-- Compact log list container -->
    <ScrollArea class="flex-1 font-mono text-xs">
      <div v-if="renderedRows.length > 0" class="space-y-0">
        <!-- Log rows with enhanced layout -->
        <div
          v-for="row in renderedRows"
          :key="row.id"
          :data-row-id="row.id"
          class="group grid grid-cols-[72px_1fr] gap-2 px-2 cursor-pointer border-b border-border/20 border-l-4 transition-colors"
          :class="[
            row.borderClass, 
            row.bgClass, 
            'hover:bg-muted/30',
            expandedRowId === row.id ? 'py-1 items-start' : 'py-0.5 min-h-[22px] items-center'
          ]"
          @click="handleClick($event, row.id)"
          :title="`${new Date(row.raw[props.timestampField]).toLocaleString()} - Click to expand, select text to copy`"
        >
          <!-- Timestamp (fixed width grid column) -->
          <span 
            :class="expandedRowId === row.id 
              ? 'text-muted-foreground text-right text-xs font-mono self-start pt-0.5'
              : 'text-muted-foreground text-right text-xs font-mono self-center'"
          >
            {{ row.timestamp }}
          </span>
          
          <!-- Message with syntax highlighting -->
          <div class="flex flex-col gap-1 min-w-0">
            <span 
              :class="expandedRowId === row.id 
                ? 'whitespace-pre-wrap break-all text-foreground text-xs font-mono' 
                : 'truncate text-foreground text-xs font-mono'"
              v-html="highlightText(row.message, 'message')"
              :title="expandedRowId === row.id ? '' : row.rawMessage"
            ></span>
            <!-- Show Context button when expanded -->
            <div v-if="expandedRowId === row.id" class="flex items-center gap-2 mt-1">
              <Button 
                variant="outline" 
                size="sm" 
                class="h-6 text-xs px-2"
                @click="openContextModal(row.raw, $event)"
              >
                <Clock class="h-3 w-3 mr-1" />
                Show Context
              </Button>
            </div>
          </div>
        </div>
      </div>
      
      <!-- Empty state -->
      <div v-else class="p-4 text-center text-muted-foreground">
        <template v-if="table.getState().globalFilter">
          No logs matching "{{ table.getState().globalFilter }}"
        </template>
        <template v-else>
          No logs to display
        </template>
      </div>
    </ScrollArea>

    <!-- Log Context Modal -->
    <LogTimelineModal
      v-if="contextLog"
      :is-open="showContextModal"
      :source-id="String(props.sourceId)"
      :team-id="Number(props.teamId) || 0"
      :log="contextLog"
      :timestamp-field="props.timestampField"
      @update:is-open="showContextModal = $event"
    />
  </div>
</template>

<style scoped>
/* Ensure consistent spacing */
.font-mono {
  font-family: ui-monospace, SFMono-Regular, "SF Mono", Monaco, Consolas, "Liberation Mono", "Courier New", monospace;
}



/* Highlight styles for regex matches */
:deep(mark) {
  @apply bg-yellow-200 dark:bg-yellow-800 px-0.5 rounded;
}
</style>
