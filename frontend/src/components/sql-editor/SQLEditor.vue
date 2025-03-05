<script setup lang="ts">
import { ref, computed, onMounted, onBeforeUnmount, watch, nextTick } from 'vue'
import * as monaco from 'monaco-editor'
import { Button } from '@/components/ui/button'
import { HelpCircle } from 'lucide-vue-next'
import { useToast } from '@/components/ui/toast'
import { TOAST_DURATION } from '@/lib/constants'
import { VueMonacoEditor } from '@guolao/vue-monaco-editor'
import { useColorMode } from '@vueuse/core'
import { getSqlMonacoOptions } from '@/utils/sql-language'
import { registerSqlCompletionProvider } from '@/utils/sql-language'
import {
  Tooltip,
  TooltipContent,
  TooltipProvider,
  TooltipTrigger
} from '@/components/ui/tooltip'

// Apply custom styling with CSS
const customCss = `
.monaco-editor,
.monaco-editor .monaco-editor-background {
  background-color: transparent !important;
}
.monaco-editor .margin,
.monaco-editor .inputarea.ime-input {
  background-color: transparent !important;
}
/* Remove borders that disrupt cohesive look */
.monaco-editor .overflow-guard {
  border: none !important;
}
/* Improve visual consistency with app theme */
.monaco-editor .view-line {
  align-items: center !important;
}
/* Improve scrollbar visibility for code */
.monaco-editor .scrollbar.horizontal .slider {
  height: 6px !important;
  opacity: 0.6 !important;
}
.monaco-editor .scrollbar.vertical .slider {
  width: 6px !important;
  opacity: 0.6 !important;
}
`

const { toast } = useToast()

interface Props {
  modelValue: string
  sourceDatabase?: string
  sourceTable?: string
  startTimestamp: number
  endTimestamp: number
  sourceColumns?: Array<{ name: string; type?: string }>
}

const props = defineProps<Props>()
const emit = defineEmits<{
  (e: 'update:modelValue', value: string): void
  (e: 'execute'): void
}>()

// Editor state
const code = ref('')
const editorFocused = ref(false)
const editorInstance = ref<monaco.editor.IStandaloneCodeEditor | null>(null)
const completionProvider = ref<monaco.IDisposable | null>(null)
const editorContainer = ref<HTMLElement | null>(null)

// Get theme from app-wide color mode
const colorMode = useColorMode()
const theme = computed(() => colorMode.value === 'dark' ? 'logchef-dark' : 'logchef-light')

// Get full table name with database
const fullTableName = computed(() => {
  if (!props.sourceDatabase || !props.sourceTable) return ''
  return `${props.sourceDatabase}.${props.sourceTable}`
})

// SQL templates based on source
const getSqlTemplates = computed(() => {
  if (!fullTableName.value) return []

  const startTime = props.startTimestamp / 1000 // Convert to seconds for ClickHouse
  const endTime = props.endTimestamp / 1000

  return [
    {
      label: 'Last 3 Days',
      sql: `SELECT *
FROM ${fullTableName.value}
WHERE timestamp >= today() - INTERVAL 3 DAY
ORDER BY timestamp DESC`
    },
    {
      label: 'Today Only',
      sql: `SELECT *
FROM ${fullTableName.value}
WHERE timestamp >= today()
ORDER BY timestamp DESC`
    },
    {
      label: 'Last Hour',
      sql: `SELECT *
FROM ${fullTableName.value}
WHERE timestamp >= now() - INTERVAL 1 HOUR
ORDER BY timestamp DESC`
    },
    {
      label: 'Severity Distribution (Last 24h)',
      sql: `SELECT
  severity_text,
  count(*) as count,
  min(timestamp) as first_seen,
  max(timestamp) as last_seen
FROM ${fullTableName.value}
WHERE timestamp >= now() - INTERVAL 24 HOUR
GROUP BY severity_text
ORDER BY count DESC`
    },
    {
      label: 'Recent Errors',
      sql: `SELECT *
FROM ${fullTableName.value}
WHERE timestamp >= now() - INTERVAL 6 HOUR
  AND (severity_text = 'ERROR' OR severity_text = 'FATAL')
ORDER BY timestamp DESC`
    },
    {
      label: 'Service Distribution (This Week)',
      sql: `SELECT
  service_name,
  count(*) as count,
  min(timestamp) as first_seen,
  max(timestamp) as last_seen
FROM ${fullTableName.value}
WHERE timestamp >= toStartOfWeek(now())
GROUP BY service_name
ORDER BY count DESC`
    },
    {
      label: 'Custom Time Range',
      sql: `SELECT *
FROM ${fullTableName.value}
WHERE timestamp BETWEEN toDateTime64(${startTime}, 3) AND toDateTime64(${endTime}, 3)
ORDER BY timestamp DESC`
    }
  ]
})

// Define comprehensive column names for log data
const commonColumns = ref([
  // Core fields
  { name: 'timestamp', type: 'DateTime64(3)' },
  { name: 'severity_text', type: 'String' },
  { name: 'service_name', type: 'String' },
  { name: 'message', type: 'String' },

  // System metadata
  { name: 'host', type: 'String' },
  { name: 'hostname', type: 'String' },
  { name: 'container_id', type: 'String' },
  { name: 'container_name', type: 'String' },
  { name: 'pod_name', type: 'String' },
  { name: 'namespace', type: 'String' },
  { name: 'cluster', type: 'String' },
  { name: 'environment', type: 'String' },
  { name: 'region', type: 'String' },
  { name: 'zone', type: 'String' },

  // Tracing fields
  { name: 'trace_id', type: 'String' },
  { name: 'span_id', type: 'String' },
  { name: 'parent_span_id', type: 'String' },
  { name: 'trace_flags', type: 'UInt8' },

  // HTTP/API request fields
  { name: 'body', type: 'String' },
  { name: 'method', type: 'String' },
  { name: 'path', type: 'String' },
  { name: 'url', type: 'String' },
  { name: 'status_code', type: 'UInt16' },
  { name: 'user_agent', type: 'String' },
  { name: 'referer', type: 'String' },
  { name: 'ip', type: 'String' },
  { name: 'remote_ip', type: 'String' },
  { name: 'client_ip', type: 'String' },
  { name: 'protocol', type: 'String' },
  { name: 'request_id', type: 'String' },
  { name: 'request_size', type: 'UInt32' },
  { name: 'response_size', type: 'UInt32' },

  // Performance metrics
  { name: 'duration_ms', type: 'Float64' },
  { name: 'duration', type: 'Float64' },
  { name: 'latency_ms', type: 'Float64' },
  { name: 'latency', type: 'Float64' },

  // User/authentication fields
  { name: 'user_id', type: 'String' },
  { name: 'user_email', type: 'String' },
  { name: 'user_name', type: 'String' },
  { name: 'tenant_id', type: 'String' },
  { name: 'organization_id', type: 'String' },
  { name: 'auth_method', type: 'String' },

  // Error-specific fields
  { name: 'error', type: 'String' },
  { name: 'error_message', type: 'String' },
  { name: 'error_type', type: 'String' },
  { name: 'error_stack', type: 'String' },
  { name: 'error_code', type: 'String' },

  // Application-specific fields
  { name: 'version', type: 'String' },
  { name: 'build_id', type: 'String' },
  { name: 'commit_hash', type: 'String' },
  { name: 'component', type: 'String' },
  { name: 'module', type: 'String' },
  { name: 'function', type: 'String' },
  { name: 'file', type: 'String' },
  { name: 'line', type: 'UInt32' },

  // Generic fields
  { name: '_raw', type: 'String' },
  { name: 'raw_message', type: 'String' },
  { name: 'log_source', type: 'String' },
  { name: 'level', type: 'String' }
])

// Table information for autocompletion
const tables = ref([
  { name: props.sourceTable || 'logs', database: props.sourceDatabase || 'default' }
])

// Add source's schema columns to our completions if available
const sourceColumns = computed(() => {
  // Get columns from props (will be passed from parent component)
  if (props.sourceColumns?.length) {
    return props.sourceColumns.map(col => ({
      name: col.name,
      type: col.type || 'String'
    }))
  }
  return []
})

// Combined columns for autocompletion (both common ones and source-specific)
const allColumns = computed(() => {
  const uniqueColumns = new Map()

  // Add common columns first (lower priority)
  commonColumns.value.forEach(col => {
    uniqueColumns.set(col.name, col)
  })

  // Add source columns (higher priority - will overwrite common ones if same name)
  sourceColumns.value.forEach(col => {
    uniqueColumns.set(col.name, col)
  })

  return Array.from(uniqueColumns.values())
})

// Editor options
const editorOptions = computed(() => {
  return {
    ...getSqlMonacoOptions(),
    fontSize: 14,
    lineHeight: 22,
    lineNumbers: "on",
    minimap: { enabled: false },
  }
})

// Handle editor mount - more stable implementation
const handleMount = (editor: monaco.editor.IStandaloneCodeEditor) => {
  // Store editor reference
  editorInstance.value = editor;

  // Define a function to update completion provider, but limit how often it can run
  let lastProviderUpdate = 0;
  const updateCompletionProvider = () => {
    try {
      // Throttle updates to prevent potential loops (max once per 500ms)
      const now = Date.now();
      if (now - lastProviderUpdate < 500) {
        return;
      }
      lastProviderUpdate = now;

      // Make sure we use current table info
      tables.value = [
        { name: props.sourceTable || 'logs', database: props.sourceDatabase || 'default' }
      ];

      // Safely dispose previous provider if it exists
      if (completionProvider.value) {
        try {
          completionProvider.value.dispose();
          completionProvider.value = null;
        } catch (disposeError) {
          console.error("Error disposing completion provider:", disposeError);
        }
      }

      // Create new provider
      completionProvider.value = registerSqlCompletionProvider(allColumns.value, tables.value);
    } catch (error) {
      console.error("Failed to register SQL completion provider:", error);
    }
  };

  // Do initial registration after a short delay to ensure everything is ready
  setTimeout(() => {
    updateCompletionProvider();
  }, 100);

  // Handle keyboard shortcuts - basic implementation to avoid issues
  try {
    // Execute query shortcut
    editor.addAction({
      id: 'execute-sql',
      label: 'Execute SQL Query',
      keybindings: [
        monaco.KeyMod.CtrlCmd | monaco.KeyCode.Enter
      ],
      run: () => {
        executeQuery();
        return null;
      }
    });

    // Suggestions shortcut - simplified implementation
    editor.addAction({
      id: 'showSqlSuggestions',
      label: 'Show SQL Suggestions',
      keybindings: [
        monaco.KeyMod.Alt | monaco.KeyCode.Space
      ],
      run: () => {
        try {
          // Just trigger suggestions directly with standard API
          editor.trigger('keyboard', 'editor.action.triggerSuggest', {});
        } catch (error) {
          console.error("Error showing suggestions:", error);
        }
        return null;
      }
    });
  } catch (actionError) {
    console.error("Error setting up editor actions:", actionError);
  }

  // Focus events - basic implementation
  try {
    editor.onDidFocusEditorWidget(() => {
      editorFocused.value = true;
    });

    editor.onDidBlurEditorWidget(() => {
      editorFocused.value = false;
    });
  } catch (focusError) {
    console.error("Error setting up focus events:", focusError);
  }

  // Set initial focus after a delay - give UI time to stabilize
  setTimeout(() => {
    try {
      editor.focus();
    } catch (focusError) {
      console.error("Error setting initial focus:", focusError);
    }
  }, 100);
}

// Handle content changes
const onChange = () => {
  try {
    emit('update:modelValue', code.value);
  } catch (error) {
    console.error("Error in onChange handler:", error);
  }
}

// Watch for external changes to modelValue
watch(() => props.modelValue, (newVal) => {
  try {
    if (newVal !== code.value) {
      code.value = newVal;
    }
  } catch (error) {
    console.error("Error in modelValue watcher:", error);
  }
}, { immediate: true });

// Track last update time to prevent infinite loops
let lastTableUpdate = 0;

// Watch for changes to table information to update completion - with throttling
watch(() => [props.sourceTable, props.sourceDatabase], () => {
  try {
    // Throttle updates to prevent potential infinite loops (max once per second)
    const now = Date.now();
    if (now - lastTableUpdate < 1000) {
      return;
    }
    lastTableUpdate = now;

    if (editorInstance.value) {
      // Update tables
      tables.value = [
        { name: props.sourceTable || 'logs', database: props.sourceDatabase || 'default' }
      ];

      // Safe disposal of existing provider
      if (completionProvider.value) {
        try {
          completionProvider.value.dispose();
          completionProvider.value = null;
        } catch (disposeError) {
          console.error("Error disposing completion provider:", disposeError);
        }
      }

      // Register new provider after a short delay to ensure everything is settled
      setTimeout(() => {
        try {
          completionProvider.value = registerSqlCompletionProvider(allColumns.value, tables.value);
        } catch (error) {
          console.error("Error creating new completion provider:", error);
        }
      }, 100);
    }
  } catch (error) {
    console.error("Error updating SQL completion provider:", error);
  }
}, { immediate: false });

// Set initial value - Monaco is already initialized in main.ts
onMounted(() => {
  try {
    // Only log in development
    if (process.env.NODE_ENV !== 'production') {
      console.log("SQLEditor component mounting");
    }

    // Set initial code value
    code.value = props.modelValue || '';

    // Add style element for Monaco editor - with unique ID to prevent duplication
    const styleId = 'sql-editor-custom-styles';
    if (!document.getElementById(styleId)) {
      const styleEl = document.createElement('style');
      styleEl.id = styleId;
      styleEl.textContent = customCss;
      document.head.appendChild(styleEl);
    }
  } catch (error) {
    console.error("Error in SQLEditor onMounted:", error);
  }
});

// Minimal cleanup - just release references
onBeforeUnmount(() => {
  try {
    // Only log in development
    if (process.env.NODE_ENV !== 'production') {
      console.log("SQLEditor component unmounting");
    }

    // Remove style element if it exists and no other SQLEditor components are active
    const styleId = 'sql-editor-custom-styles';
    const styleEl = document.getElementById(styleId);
    if (styleEl && window.monaco?.editor?.getEditors().length <= 1) {
      styleEl.remove();
    }

    // Clear references
    code.value = '';
    completionProvider.value = null;
    editorInstance.value = null;
  } catch (error) {
    console.error("Error in SQLEditor onBeforeUnmount:", error);
  }
});

// Clear SQL query
const clearSql = () => {
  code.value = '';
  if (editorInstance.value) {
    editorInstance.value.focus();
  }
}

// Insert template
const insertTemplate = (sql: string) => {
  code.value = sql;

  // Focus the editor after applying the template
  nextTick(() => {
    if (editorInstance.value) {
      editorInstance.value.focus();
    }
  });
}

// Focus the editor when clicking on the container
const focusEditor = () => {
  if (editorInstance.value) {
    editorInstance.value.focus();
  }
}

// Execute query with validation
const executeQuery = () => {
  const sql = code.value.trim().toLowerCase();
  if (!sql) {
    toast({
      title: 'Error',
      description: 'SQL query cannot be empty',
      variant: 'destructive',
      duration: TOAST_DURATION.ERROR,
    });
    return;
  }

  if (!sql.startsWith('select')) {
    toast({
      title: 'Error',
      description: 'Only SELECT queries are allowed',
      variant: 'destructive',
      duration: TOAST_DURATION.ERROR,
    });
    return;
  }

  // Save to recent queries
  saveToRecentQueries(code.value);

  // Emit execute event
  emit('execute');
}

// Recent queries history (stored in memory)
const recentQueries = ref<Array<{ sql: string; timestamp: number }>>([]);

// Maximum number of recent queries to keep
const MAX_RECENT_QUERIES = 5;

// Save query to history after execution
const saveToRecentQueries = (sql: string) => {
  // Skip empty or very short queries
  if (!sql || sql.trim().length < 10) return;

  // Avoid duplicates - remove existing entry with the same SQL
  const existingIndex = recentQueries.value.findIndex(q => q.sql === sql);
  if (existingIndex !== -1) {
    recentQueries.value.splice(existingIndex, 1);
  }

  // Add to the beginning of the array
  recentQueries.value.unshift({
    sql,
    timestamp: Date.now()
  });

  // Trim to maximum size
  if (recentQueries.value.length > MAX_RECENT_QUERIES) {
    recentQueries.value = recentQueries.value.slice(0, MAX_RECENT_QUERIES);
  }
}

// Help tooltip content with keyboard shortcuts
const tooltipItems = [
  'Use Alt+Space to show SQL suggestions',
  'Use Ctrl+Space for standard code completion',
  'Press Tab to accept the current suggestion',
  'Press Ctrl+Enter (Cmd+Enter on Mac) to execute query',
  'Only SELECT queries are allowed',
  'Click Quick Templates to insert sample queries',
  'Click on a recent query to reuse it'
];
</script>

<template>
  <div class="w-full">
    <!-- Simplified and more consistent with the filter editor - no clear button -->
    <div class="flex flex-col gap-2">

      <!-- Templates row - with improved tab-like styling -->
      <div v-if="getSqlTemplates.length > 0" class="flex flex-wrap gap-1.5 px-3 py-1.5 border-b border-border/30">
        <span class="text-[10px] font-medium text-muted-foreground mr-1">Templates:</span>
        <div class="flex flex-wrap gap-1.5">
          <button v-for="template in getSqlTemplates" :key="template.label" @click="insertTemplate(template.sql)"
            class="inline-flex items-center px-2 py-0.5 rounded-sm text-[10px] bg-muted/50 hover:bg-muted text-foreground/80 hover:text-foreground transition-colors border border-border/30">
            {{ template.label }}
          </button>
        </div>
      </div>

      <!-- Monaco Editor with compact styling -->
      <div ref="editorContainer" class="relative px-3 pt-1 pb-2" style="height: 80px;" @click="focusEditor">
        <!-- Help tooltip -->
        <TooltipProvider :delay-duration="300">
          <Tooltip>
            <TooltipTrigger asChild>
              <div class="absolute right-2 top-2 z-10">
                <Button size="icon" class="h-6 w-6 rounded-full" variant="ghost">
                  <HelpCircle class="h-3.5 w-3.5 text-muted-foreground/50" />
                </Button>
              </div>
            </TooltipTrigger>
            <TooltipContent side="left" align="center" class="max-w-sm p-3 text-xs">
              <h3 class="font-medium mb-1.5 text-sm">SQL Editor Help</h3>
              <ul class="space-y-1">
                <li v-for="(item, index) in tooltipItems" :key="index" class="flex items-start">
                  <span class="mr-1.5">•</span>
                  <span>{{ item }}</span>
                </li>
              </ul>
            </TooltipContent>
          </Tooltip>
        </TooltipProvider>

        <VueMonacoEditor v-model:value="code" :theme="theme" language="clickhouse-sql" :options="editorOptions"
          @mount="handleMount" @change="onChange" class="w-full h-full" />
      </div>

      <!-- Simple footer area -->
      <div class="flex items-center justify-between px-3 pb-1">
        <div class="flex items-center">
          <span class="text-[10px] text-muted-foreground/70">
            <kbd
              class="px-1 py-0.5 bg-background/80 rounded border border-border/50 font-mono text-[10px]">Ctrl+Enter</kbd>
            to run
          </span>
        </div>

        <!-- Compact Recent Queries button -->
        <div v-if="recentQueries.length > 0" class="relative group">
          <button class="flex items-center gap-0.5 text-[10px] text-primary/80 hover:text-primary font-medium">
            Recent
            <svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 20 20" fill="currentColor" class="w-3 h-3">
              <path fill-rule="evenodd"
                d="M5.23 7.21a.75.75 0 011.06.02L10 11.168l3.71-3.938a.75.75 0 111.08 1.04l-4.25 4.5a.75.75 0 01-1.08 0l-4.25-4.5a.75.75 0 01.02-1.06z"
                clip-rule="evenodd" />
            </svg>
          </button>

          <!-- Dropdown Panel -->
          <div
            class="absolute right-0 z-10 w-72 mt-1 hidden group-hover:block bg-popover rounded-md shadow-lg border border-border overflow-hidden">
            <div class="p-1 max-h-40 overflow-y-auto">
              <button v-for="query in recentQueries" :key="query.timestamp"
                class="w-full px-2 py-1.5 text-left text-xs font-mono hover:bg-muted/50 rounded-sm transition-colors truncate"
                @click="insertTemplate(query.sql)">
                {{ query.sql.length > 60 ? query.sql.substring(0, 60) + '...' : query.sql }}
              </button>
            </div>
          </div>
        </div>
      </div>
    </div>
  </div>
</template>