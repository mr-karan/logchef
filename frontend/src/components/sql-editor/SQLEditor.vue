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
  compactMode?: boolean
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
// Search filter for field panel
const fieldFilter = ref('')

// Filtered columns based on search (similar to SmartFilterBar)
const filteredColumns = computed(() => {
  if (!fieldFilter.value.trim()) return props.sourceColumns || []
  
  const searchTerm = fieldFilter.value.toLowerCase().trim()
  return (props.sourceColumns || []).filter(column => 
    column.name.toLowerCase().includes(searchTerm) || 
    (column.type && column.type.toLowerCase().includes(searchTerm))
  )
})

// Table definition for SQL completion
const tableDefinitions = ref<Array<{ name: string; database: string }>>([
  { name: props.sourceTable || 'logs', database: props.sourceDatabase || 'default' }
])

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

// This section is redundant as we already defined tableDefinitions above

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

// Editor options - enhanced for better user experience
const editorOptions = computed(() => {
  return {
    ...getSqlMonacoOptions(),
    fontSize: 14,
    lineHeight: 22,
    lineNumbers: "on",
    minimap: { enabled: false },
    // Allow full editor capabilities
    automaticLayout: true,
    scrollBeyondLastLine: false,
    folding: true,
    foldingStrategy: 'auto',
    // Better scrolling and visuals
    smoothScrolling: true,
    renderControlCharacters: false,
    renderIndentGuides: true,
    padding: { top: 8, bottom: 8 },
  }
})

// Handle editor mount - more stable implementation with better async handling and registration
const handleMount = (editor: monaco.editor.IStandaloneCodeEditor) => {
  try {
    console.log('SQLEditor Editor mounting...', editor);
    
    // Store editor instance
    editorInstance.value = editor;
    
    // Register with global tracker if available
    if (typeof window !== 'undefined' && window.monaco && window.monaco.editor) {
      try {
        // Check if we have the utility function from monaco.ts
        const monacoUtils = window.monaco.editor._logchef_utils;
        if (monacoUtils && typeof monacoUtils.registerEditorInstance === 'function') {
          monacoUtils.registerEditorInstance(editor);
        }
      } catch (e) {
        // Ignore errors in registration
      }
    }
  
    // Define a function to update completion provider, but limit how often it can run
    let lastProviderUpdate = 0;
    const updateCompletionProvider = async () => {
      try {
        // Throttle updates to prevent potential loops (max once per 500ms)
        const now = Date.now();
        if (now - lastProviderUpdate < 500) {
          return;
        }
        lastProviderUpdate = now;
  
        // Safely dispose previous provider if it exists
        if (completionProvider.value) {
          try {
            completionProvider.value.dispose();
          } catch (disposeError) {
            console.error("Error disposing completion provider:", disposeError);
          } finally {
            completionProvider.value = null;
          }
        }
  
        // Small delay before adding new provider
        await new Promise(resolve => setTimeout(resolve, 10));
        
        // Create new provider with source columns if editor is still available
        if (editorInstance.value) {
          completionProvider.value = registerSqlCompletionProvider(
            props.sourceColumns || [], 
            tableDefinitions.value
          );
        }
      } catch (error) {
        console.error("Failed to register SQL completion provider:", error);
      }
    };
  
    // Do initial registration after a short delay to ensure everything is ready
    setTimeout(() => {
      updateCompletionProvider();
    }, 100);
  } catch (error) {
    console.error("Error in SQLEditor handleMount:", error);
  }

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
      // Update completion with current source/columns

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
          completionProvider.value = registerSqlCompletionProvider(props.sourceColumns || [], tableDefinitions.value);
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

// Enhanced cleanup with proper disposal sequence
onBeforeUnmount(() => {
  console.log('SQLEditor Unmounting...');
  try {
    // Clean up completion provider first
    if (completionProvider.value) {
      console.log('Disposing completion provider');
      try {
        completionProvider.value.dispose();
      } catch (e) {
        console.error("Error disposing completion provider:", e);
      }
      completionProvider.value = null;
    }
    
    // Remove style element
    const styleId = 'sql-editor-custom-styles';
    const styleEl = document.getElementById(styleId);
    if (styleEl && window.monaco?.editor?.getEditors().length <= 1) {
      console.log('Removing custom styles');
      styleEl.remove();
    }

    // Dispose editor instance with proper sequence
    if (editorInstance.value) {
      console.log('Disposing editor instance');
      try {
        // Get the model before disposing the editor
        const model = editorInstance.value.getModel();
        
        // Dispose the editor first
        editorInstance.value.dispose();
        
        // Then dispose the model if it exists and isn't already disposed
        if (model && !model.isDisposed()) {
          model.dispose();
        }
      } catch (e) {
        console.error("Error disposing editor:", e);
      }
      editorInstance.value = null;
    }
  } catch (error) {
    console.error("Error in SQLEditor onBeforeUnmount:", error);
  }
  console.log('SQLEditor Unmounted');
});

// Clear SQL query
const clearSql = () => {
  code.value = '';
  if (editorInstance.value) {
    editorInstance.value.focus();
  }
}

// Insert template with simplified approach
const insertTemplate = (sql: string) => {
  // Directly set the code
  code.value = sql;
  
  // Add to recent queries
  saveToRecentQueries(sql);
  
  // Let reactivity handle syncing with Monaco
  onChange();
  
  // Focus and position cursor at end
  if (editorInstance.value) {
    editorInstance.value.focus();
    
    // Position cursor at the end after a slight delay to let Monaco update
    setTimeout(() => {
      try {
        const model = editorInstance.value?.getModel();
        if (model && editorInstance.value) {
          // Get end position
          const lastLine = model.getLineCount();
          const lastColumn = model.getLineLength(lastLine) + 1;
          
          // Set cursor position
          editorInstance.value.setPosition({ lineNumber: lastLine, column: lastColumn });
          // Make sure cursor is visible
          editorInstance.value.revealPosition({ lineNumber: lastLine, column: lastColumn });
        }
      } catch (e) {
        console.error("Error positioning cursor:", e);
      }
    }, 50);
  }
}

// Focus the editor when clicking on the container
const focusEditor = () => {
  try {
    if (editorInstance.value) {
      editorInstance.value.focus();
    }
  } catch (error) {
    console.error("Error focusing editor:", error);
  }
}

// Stable field insertion using Monaco's transaction API
const insertField = (fieldName: string) => {
  if (!editorInstance.value) return;

  const model = editorInstance.value.getModel();
  if (!model) return;

  try {
    // Use transaction to ensure atomic update
    model.pushEditOperations(
      [],
      [{
        range: editorInstance.value.getSelection() || {
          startLineNumber: model.getLineCount(),
          startColumn: model.getLineLength(model.getLineCount()) + 1,
          endLineNumber: model.getLineCount(),
          endColumn: model.getLineLength(model.getLineCount()) + 1
        },
        text: fieldName + ' '
      }],
      () => null
    );

    // Focus editor
    editorInstance.value.focus();
    
    // Queue cursor positioning after layout to ensure stability
    requestAnimationFrame(() => {
      try {
        // Update our code ref to match editor content
        code.value = model.getValue();
        
        // Trigger onChange to ensure consistency
        onChange();
        
        // Position cursor at end of inserted text
        const lastLine = model.getLineCount();
        const lastColumn = model.getLineLength(lastLine) + 1;
        editorInstance.value?.setPosition({ lineNumber: lastLine, column: lastColumn });
        editorInstance.value?.revealPositionInCenter({ lineNumber: lastLine, column: lastColumn });
      } catch (e) {
        console.error("Error after field insertion:", e);
      }
    });
  } catch (error) {
    console.error("Stable field insertion failed:", error);
    
    // Fallback to direct value update if edit operation fails
    code.value = (code.value || '') + fieldName + ' ';
    onChange();
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

// Expose methods to parent component
defineExpose({
  insertField,
  focusEditor
});
</script>

<template>
  <div class="w-full h-full">
    <!-- Split layout for SQL editor similar to the filter editor -->
    <div class="flex flex-col lg:flex-row h-full">
      <!-- Left Panel: Fields (Only shown when sourceColumns are available) -->
      <div v-if="sourceColumns && sourceColumns.length > 0" 
           class="border rounded-l-md bg-card overflow-hidden flex flex-col max-h-full 
                  lg:w-1/4 lg:max-w-[240px] lg:min-w-[180px]">
        <div class="px-3 py-2 border-b bg-muted/30 text-xs font-medium">
          <span>Available Fields</span>
        </div>
        
        <!-- Search fields input -->
        <div class="px-2 py-1.5 border-b border-border/30">
          <div class="relative">
            <input 
              v-model="fieldFilter"
              type="search" 
              placeholder="Search fields..." 
              class="w-full h-7 px-2 py-1 text-xs rounded-sm bg-muted/50 border border-border/40 focus:outline-none focus:ring-1 focus:ring-primary/30"
            />
            <svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 20 20" fill="currentColor"
              class="absolute right-2 top-1/2 transform -translate-y-1/2 h-3.5 w-3.5 text-muted-foreground">
              <path fill-rule="evenodd" d="M9 3.5a5.5 5.5 0 100 11 5.5 5.5 0 000-11zM2 9a7 7 0 1112.452 4.391l3.328 3.329a.75.75 0 11-1.06 1.06l-3.329-3.328A7 7 0 012 9z" clip-rule="evenodd" />
            </svg>
          </div>
        </div>
        
        <div class="flex-1 overflow-y-auto p-2 space-y-1">
          <button 
            v-for="column in filteredColumns" 
            :key="column.name" 
            @click="insertField(column.name)"
            class="w-full text-left px-2 py-1.5 rounded-sm text-xs hover:bg-muted/80 text-foreground/90 transition-colors flex items-center gap-1.5"
          >
            <!-- Simple colored letter indicator for field type with improved type detection -->
            <span 
              class="inline-flex items-center justify-center w-4 h-4 rounded-sm text-[9px] font-medium text-white shrink-0"
              :class="{
                'bg-blue-500': column.type && (column.type.toLowerCase().includes('string') || column.type.toLowerCase().includes('text') || column.type.toLowerCase().includes('char')),
                'bg-green-500': column.type && (column.type.toLowerCase().includes('int') || column.type.toLowerCase().includes('float') || column.type.toLowerCase().includes('double') || column.type.toLowerCase().includes('decimal')),
                'bg-amber-500': column.type && (column.type.toLowerCase().includes('time') || column.type.toLowerCase().includes('date')),
                'bg-purple-500': column.type && column.type.toLowerCase().includes('bool'),
                'bg-slate-500': !column.type
              }"
            >
              {{ column.type && (column.type.toLowerCase().includes('string') || column.type.toLowerCase().includes('text') || column.type.toLowerCase().includes('char')) ? 'S' :
                 column.type && (column.type.toLowerCase().includes('int') || column.type.toLowerCase().includes('float') || column.type.toLowerCase().includes('double') || column.type.toLowerCase().includes('decimal')) ? 'N' :
                 column.type && (column.type.toLowerCase().includes('time') || column.type.toLowerCase().includes('date')) ? 'T' :
                 column.type && column.type.toLowerCase().includes('bool') ? 'B' : 'F'
              }}
            </span>
            <span class="truncate">{{ column.name }}</span>
          </button>
        </div>
      </div>

      <!-- Right Panel: Editor -->
      <div :class="[
              'border rounded-md bg-card flex-1 flex flex-col overflow-hidden',
              sourceColumns && sourceColumns.length > 0 ? 'lg:rounded-l-none lg:border-l-0' : ''
            ]">
      
        <!-- Templates row - with improved tab-like styling -->
        <div v-if="getSqlTemplates.length > 0" class="flex flex-wrap gap-1.5 px-3 py-2 border-b border-border/30 text-xs">
          <span class="font-medium text-muted-foreground mr-1">Templates:</span>
          <div class="flex flex-wrap gap-1.5">
            <button v-for="template in getSqlTemplates" :key="template.label" @click="insertTemplate(template.sql)"
              class="inline-flex items-center px-2 py-0.5 rounded-sm text-[10px] bg-muted/50 hover:bg-muted text-foreground/80 hover:text-foreground transition-colors border border-border/30">
              {{ template.label }}
            </button>
          </div>
          
          <!-- Help tooltip -->
          <div class="ml-auto">
            <TooltipProvider :delay-duration="300">
              <Tooltip>
                <TooltipTrigger asChild>
                  <Button size="icon" class="h-5 w-5 rounded-full" variant="ghost">
                    <HelpCircle class="h-3 w-3 text-muted-foreground/50" />
                  </Button>
                </TooltipTrigger>
                <TooltipContent side="left" align="center" class="max-w-sm p-3 text-xs">
                  <h3 class="font-medium mb-1.5 text-sm">SQL Editor Help</h3>
                  <ul class="space-y-1">
                    <li v-for="(item, index) in tooltipItems" :key="index" class="flex items-start">
                      <span class="mr-1.5 text-primary">•</span>
                      <span>{{ item }}</span>
                    </li>
                  </ul>
                </TooltipContent>
              </Tooltip>
            </TooltipProvider>
          </div>
        </div>

        <!-- Monaco Editor with full height -->
        <div ref="editorContainer" class="flex-1 cursor-text min-h-[80px] overflow-hidden" 
             :class="{ 'outline outline-1 outline-primary': editorFocused }" 
             @click="focusEditor">
          <VueMonacoEditor v-model:value="code" :theme="theme" language="clickhouse-sql" :options="editorOptions"
            @mount="handleMount" @change="onChange" class="w-full h-full" />
        </div>

        <!-- Footer with keyboard shortcuts -->
        <div class="px-3 py-1 border-t border-border/30 text-[10px] text-muted-foreground flex items-center gap-3">
          <span>
            <kbd class="px-1 py-0.5 bg-background/80 rounded border border-border/50 font-mono">Ctrl+Space</kbd>
            for suggestions
          </span>
          <span>
            <kbd class="px-1 py-0.5 bg-background/80 rounded border border-border/50 font-mono">Ctrl+Enter</kbd>
            to run query
          </span>
        </div>
      </div>
    </div>
  </div>
</template>
