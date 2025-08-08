<script setup lang="ts">
import { computed, ref } from 'vue'
import { ScrollArea } from '@/components/ui/scroll-area'
import { useToast } from '@/composables/useToast'
import { TOAST_DURATION } from '@/lib/constants'

interface Props {
  columns?: any[]
  data?: Record<string, any>[]
  logs: Record<string, any>[]
  stats?: any
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
  logs: () => [],
  stats: () => ({}),
  isLoading: false,
  sourceId: 0,
  teamId: 0,
  timestampField: 'timestamp',
  severityField: 'level',
  timezone: 'local',
  queryFields: () => [],
  regexHighlights: () => ({})
})

const emit = defineEmits<{
  'drill-down': [data: { column: string; value: any; operator: string }]
}>()

const { toast } = useToast()

// Format timestamp for compact view
const formatTimestamp = (timestamp: any, timezone: string) => {
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
  
  if (timezone === 'utc') {
    options.timeZone = 'UTC'
  }
  
  return date.toLocaleString(undefined, options)
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

// Build message from log entry in logfmt format
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

// Highlight text based on regex patterns
const highlightText = (text: string, field: string) => {
  const highlight = props.regexHighlights[field]
  if (!highlight || !text) return text
  
  try {
    const regex = new RegExp(highlight.pattern, 'gi')
    if (highlight.isNegated) {
      return text // Don't highlight negated patterns visually
    }
    return text.replace(regex, '<mark class="bg-yellow-200 dark:bg-yellow-800 px-0.5 rounded">$&</mark>')
  } catch (e) {
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

// Enhanced logfmt syntax highlighting
const highlightLogfmt = (text: string) => {
  if (!text) return text
  
  // key=value where value is either:
  //   • "quoted string with possible escaped quotes", or
  //   • bare token without spaces
  // Example handled: foo="bar baz", foo="{\"a\":\"b\"}", foo=123
  return text.replace(
    /(\w+)=("(?:\\.|[^"])*"|[^\s]+)/g,
    (_match, key, val) =>
      `<span class="text-sky-600 dark:text-sky-400">${key}</span>` +
      `<span class="text-muted-foreground">=</span>` +
      `<span class="text-amber-600 dark:text-amber-400">${val}</span>`
  )
}

// Pre-computed rows for performance
const renderedRows = computed(() => 
  props.logs.map((row, index) => {
    const ts = formatTimestamp(row[props.timestampField], props.timezone)
    const sev = row[props.severityField] || ''
    const rawMsg = buildMessage(row)
    const msg = highlightLogfmt(rawMsg)
    const severityStyles = getSeverityClasses(sev)
    
    return {
      id: `${row[props.timestampField]}-${index}`, // More stable key
      timestamp: ts,
      severity: sev,
      message: msg,
      rawMessage: rawMsg,
      raw: row,
      borderClass: severityStyles.border,
      bgClass: severityStyles.bg
    }
  })
)

// Handle row click interactions
const expandedRowId = ref<string | null>(null)

// Simple click to expand/collapse (only if not selecting text)
const handleClick = (event: Event, rowId: string) => {
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
          <span 
            :class="expandedRowId === row.id 
              ? 'whitespace-pre-wrap break-all text-foreground text-xs font-mono' 
              : 'truncate text-foreground text-xs font-mono'"
            v-html="highlightText(row.message, 'message')"
            :title="expandedRowId === row.id ? '' : row.rawMessage"
          ></span>
        </div>
      </div>
      
      <!-- Empty state -->
      <div v-else class="p-4 text-center text-muted-foreground">
        No logs to display
      </div>
    </ScrollArea>
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
