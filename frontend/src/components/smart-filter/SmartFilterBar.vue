<script setup lang="ts">
import { ref, computed, onMounted, onBeforeUnmount, watch, nextTick } from 'vue'
import * as monaco from 'monaco-editor'
import { Button } from '@/components/ui/button'
import { Search, X, HelpCircle } from 'lucide-vue-next'
import type { FilterCondition } from '@/api/explore'
import { VueMonacoEditor } from '@guolao/vue-monaco-editor'
import { useColorMode } from '@vueuse/core'
import { getSingleLineMonacoOptions } from '@/utils/monaco'
import {
  parseFilterExpression,
  filterConditionsToExpression
} from '@/utils/logfilter-language'
import {
  registerCompletionProvider,
  showFieldSuggestions
} from '@/utils/monaco-logchefql'
import {
  Tooltip,
  TooltipContent,
  TooltipProvider,
  TooltipTrigger
} from '@/components/ui/tooltip'

// Define props and emits
interface Props {
  modelValue: FilterCondition[]
  columns?: { name: string; type: string }[]
  placeholder?: string
  showExamples?: boolean
  compactMode?: boolean
  commonComponent?: boolean // Indicates if this is used as common component for both SQL & filter
}

const props = withDefaults(defineProps<Props>(), {
  columns: () => [],
  placeholder: 'Enter query (click @ for field suggestions)',
  showExamples: true,
  compactMode: false,
  commonComponent: false
})

const emit = defineEmits<{
  (e: 'update:modelValue', value: FilterCondition[]): void
  (e: 'search'): void
}>()

// Get theme from app-wide color mode
const colorMode = useColorMode()
const theme = computed(() => colorMode.value === 'dark' ? 'logchef-dark' : 'logchef-light')

// Editor state
const code = ref('')
const editorFocused = ref(false)
const editorInstance = ref<monaco.editor.IStandaloneCodeEditor | null>(null)
const completionProvider = ref<monaco.IDisposable | null>(null)
const editorContainer = ref<HTMLElement | null>(null)
const fieldFilter = ref('')

// Filtered columns based on search
const filteredColumns = computed(() => {
  if (!fieldFilter.value.trim()) return props.columns
  
  const searchTerm = fieldFilter.value.toLowerCase().trim()
  return props.columns.filter(column => 
    column.name.toLowerCase().includes(searchTerm) || 
    (column.type && column.type.toLowerCase().includes(searchTerm))
  )
})

// Editor options - optimized for compact single-line editor appearance
const editorOptions = computed((): monaco.editor.IStandaloneEditorConstructionOptions => ({
  ...getSingleLineMonacoOptions(),
  cursorBlinking: 'smooth',
  cursorSmoothCaretAnimation: 'on',
  fontSize: 13,
  lineHeight: 20,
  // Force single-line mode for compact mode
  ...(props.compactMode ? {
    wordWrap: 'off',
    lineNumbers: 'off',
    folding: false,
    wrappingIndent: 'none',
    scrollBeyondLastLine: false,
    renderWhitespace: 'none',
    // Fixed height to prevent expansion
    fixedOverflowWidgets: true,
    // Improved scrollbar styling
    scrollbar: {
      vertical: 'hidden',
      horizontal: 'auto',
      handleMouseWheel: true,
      horizontalScrollbarSize: 4,
      useShadows: false
    },
  } : {}),
  // Improved find experience
  find: {
    seedSearchStringFromSelection: 'selection',
    addExtraSpaceOnTop: false
  },
  // Improved styling for cohesive look
  minimap: { enabled: false },
  padding: { top: 2, bottom: 2 },
  glyphMargin: false,
  lineDecorationsWidth: 0,
  lineNumbersMinChars: 0,

  // Better rendering for single line
  renderLineHighlight: 'none',
  roundedSelection: true,

  // Disable multi-line features
  guides: { bracketPairs: false, indentation: false },
  renderIndicators: false,
  // Still keep auto-closing for convenience
  matchBrackets: 'always',
  autoClosingBrackets: 'always',
  autoClosingQuotes: 'always',

  // Autocomplete settings - improve suggestions experience
  quickSuggestions: {
    other: true,
    comments: false,
    strings: true // Enable in strings for value suggestions
  },
  quickSuggestionsDelay: 0, // Show suggestions immediately
  suggestOnTriggerCharacters: true, // Always show suggestions on trigger chars
  acceptSuggestionOnEnter: "on",
  tabCompletion: "on", // Tab to accept suggestions
  wordBasedSuggestions: false, // Don't use words in document for suggestions
  snippetSuggestions: "inline", // Show snippet suggestions inline
  suggest: {
    localityBonus: true, // Prioritize nearby matches
    shareSuggestSelections: true, // Remember selected suggestions
    showIcons: true,
    maxVisibleSuggestions: 15,
    filterGraceful: true,
    showMethods: false,
    showFunctions: false,
    showVariables: true,
    preview: true, // Show preview of what will be inserted
  }
}))

// Handle editor mount with improved initialization
const handleMount = (editor: monaco.editor.IStandaloneCodeEditor) => {
  console.log('SmartFilterBar Editor mounting...', editor);
  
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

  // Register simplified completion provider for field names
  try {
    completionProvider.value = monaco.languages.registerCompletionItemProvider('logchefql', {
      triggerCharacters: ['@'],
      provideCompletionItems: () => {
        return {
          suggestions: props.columns.map(column => ({
            label: column.name,
            kind: monaco.languages.CompletionItemKind.Field,
            insertText: column.name,
            range: undefined
          }))
        };
      }
    });
  } catch (error) {
    console.error("Failed to register completion provider:", error);
  }

  // Simplified initialization - no need for complex key listeners

  // Handle keyboard shortcuts
  editor.addAction({
    id: 'submit',
    label: 'Submit Query',
    keybindings: [
      monaco.KeyMod.CtrlCmd | monaco.KeyCode.Enter
    ],
    run: () => {
      emit('search');
      return null;
    }
  });

  // Add a simpler keyboard shortcut for field suggestions (Alt+Space)
  // This version avoids custom providers which can create infinite loops
  editor.addAction({
    id: 'showFieldSuggestions',
    label: 'Show Field Suggestions',
    keybindings: [
      monaco.KeyMod.Alt | monaco.KeyCode.Space
    ],
    run: () => {
      try {
        // Focus first in case it's not focused
        editor.focus();

        // Simply show the suggestion widget
        monaco.editor.trigger('keyboard', 'editor.action.triggerSuggest', {});
      } catch (error) {
        console.error("Error showing suggestions:", error);
      }
      return null;
    }
  });

  // Focus events
  editor.onDidFocusEditorWidget(() => {
    editorFocused.value = true;
    // Ensure cursor is visible when focused
    updatePlaceholder(editor);
  });

  editor.onDidBlurEditorWidget(() => {
    editorFocused.value = false;
    // When editor loses focus, consider typing done
    userIsTyping = false;
    updatePlaceholder(editor);
  });

  // Support @ character directly to improve trigger experience
  // Simplify key handlers to avoid potential issues
  editor.onDidContentSizeChange(() => {
    updatePlaceholder(editor);
  });

  // Listen for model content changes directly
  // This will catch ALL changes including Ctrl+A followed by cut/delete
  editor.onDidChangeModelContent((e) => {
    // Mark as typing when content changes
    setUserTyping();

    // Get the raw value directly from the editor model
    const currentValue = editor.getValue().trim();

    // If the editor is now empty, force update
    if (!currentValue) {
      // First set our code to empty
      code.value = '';
      // Then force empty filters
      emit('update:modelValue', []);
    }
  });

  // For auto-completion
  editor.onDidChangeCursorSelection(() => {
    // Set typing flag when cursor changes (could be from autocomplete)
    setUserTyping();
  });

  // Simple placeholder setup
  updatePlaceholder(editor);

  // Natural focus handling
  setTimeout(() => {
    editor.focus();
  }, 50);
}

// Update placeholder visibility - removing it entirely as requested
const updatePlaceholder = (editor: monaco.editor.IStandaloneCodeEditor) => {
  // Placeholder functionality removed as requested
}

// Ultra-simplified onChange handler to prevent freezes
const onChange = () => {
  try {
    if (!code.value || code.value.trim() === '') {
      emit('update:modelValue', []);
    } else {
      const filters = parseFilterExpression(code.value);
      emit('update:modelValue', filters);
    }
  } catch (error) {
    console.error("Error in onChange handler:", error);
    emit('update:modelValue', []);
  }
}

// Remove the special keyboard handler that's causing freezes
// We'll rely on the onChange handler which should catch all editor content changes

// Insert field at cursor position - simplified to prevent freezes
const insertFieldAtCursor = (fieldName: string) => {
  if (!editorInstance.value) return;

  const editor = editorInstance.value;
  const model = editor.getModel();
  if (!model) return;

  // Get current selection/cursor position
  const position = editor.getPosition();
  if (!position) return;

  // Insert field at cursor position
  editor.executeEdits('insert-field', [{
    range: new monaco.Range(
      position.lineNumber,
      position.column,
      position.lineNumber,
      position.column
    ),
    text: `${fieldName}=`,
    forceMoveMarkers: true
  }]);

  // Move cursor after inserted field
  const newPosition = {
    lineNumber: position.lineNumber,
    column: position.column + fieldName.length + 1
  };
  
  editor.setPosition(newPosition);
  editor.revealPositionInCenter(newPosition);
  editor.focus();
  
  // Trigger change event after insertion
  setTimeout(() => onChange(), 0);
};

// Watch for external changes to modelValue - simplified to prevent freezes
watch(() => props.modelValue, (newVal) => {
  try {
    const currentExpression = code.value.trim();

    // Only update if we have a modelValue to convert
    if (newVal && newVal.length > 0) {
      const newExpression = filterConditionsToExpression(newVal);
      // Only update the code if it's different to avoid cursor jumping
      if (newExpression !== currentExpression) {
        code.value = newExpression;
      }
    } else if (newVal?.length === 0 && currentExpression !== '') {
      // Clear only if we're not in the middle of typing a field name
      if (!currentExpression.match(/[a-zA-Z0-9_]+\s*=\s*['"]?$/)) {
        code.value = '';
      }
    }
  } catch (error) {
    console.error("Error in modelValue watcher:", error);
  }
}, { deep: true });

// Watch for changes to columns to update completion provider
watch(() => props.columns, () => {
  try {
    // Clean up old provider
    if (completionProvider.value) {
      completionProvider.value.dispose();
    }

    // Register new provider if editor exists
    if (editorInstance.value) {
      completionProvider.value = registerCompletionProvider(props.columns);
    }
  } catch (error) {
    console.error("Error updating completion provider:", error);
  }
}, { deep: true });

// Add custom CSS
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

// Set initial expression from modelValue
onMounted(() => {
  try {
    if (props.modelValue.length > 0) {
      code.value = filterConditionsToExpression(props.modelValue);
    }

    // Add style element with unique ID to prevent duplication
    const styleId = 'smart-filter-custom-styles';
    if (!document.getElementById(styleId)) {
      const styleEl = document.createElement('style');
      styleEl.id = styleId;
      styleEl.textContent = customCss;
      document.head.appendChild(styleEl);
    }
  } catch (error) {
    console.error("Error in onMounted:", error);
  }
});

// Simplified cleanup on unmount
onBeforeUnmount(() => {
  console.log('SmartFilterBar Unmounting...');
  
  try {
    
    // Create strong references to what we need to dispose
    // This prevents loss of references during disposal process
    const editorToDispose = editorInstance.value;
    const providerToDispose = completionProvider.value;
    
    // Nullify references first to prevent circular references
    editorInstance.value = null;
    completionProvider.value = null;
    
    // Dispose completion provider
    if (providerToDispose) {
      console.log('Disposing completion provider');
      setTimeout(() => {
        try {
          providerToDispose.dispose();
        } catch (e) {
          console.error("Error disposing completion provider:", e);
        }
      }, 0);
    }
    
    // Remove custom styles if no other filter components are active
    setTimeout(() => {
      try {
        const styleId = 'smart-filter-custom-styles';
        const styleEl = document.getElementById(styleId);
        if (styleEl && (!window.monaco?.editor?.getEditors || window.monaco?.editor?.getEditors().length <= 1)) {
          console.log('Removing custom styles');
          styleEl.remove();
        }
      } catch (e) {
        console.error("Error removing styles:", e);
      }
    }, 10);

    // Dispose editor instance - use the utility function from monaco.ts for better cleanup
    if (editorToDispose) {
      console.log('Disposing editor instance');
      setTimeout(() => {
        try {
          // Use utility if available
          if (window.monaco?.editor?._logchef_utils?.disposeMonacoEditorInstance) {
            window.monaco.editor._logchef_utils.disposeMonacoEditorInstance(editorToDispose);
          } else {
            // Fallback to manual disposal
            try {
              const model = editorToDispose.getModel();
              editorToDispose.dispose();
              if (model && !model.isDisposed()) {
                model.dispose();
              }
            } catch (e) {
              console.error("Error in fallback disposal:", e);
            }
          }
        } catch (e) {
          console.error("Error disposing editor:", e);
        }
      }, 20);
    }
    
    // Clear the code value to release any large strings
    code.value = '';
    
    // Run garbage collection hint if available
    setTimeout(() => {
      try {
        if (window.gc) window.gc();
      } catch (e) {
        // Ignore gc errors
      }
    }, 100);
    
  } catch (error) {
    console.error("Error in onBeforeUnmount:", error);
  }
  
  console.log('SmartFilterBar Unmounted');
});

// Show field suggestions - exposed for parent component
const insertFieldTrigger = () => {
  try {
    if (editorInstance.value) {
      // Focus editor first
      editorInstance.value.focus();
      
      // Use helper function to show field suggestions
      showFieldSuggestions(editorInstance.value);
    }
  } catch (error) {
    console.error("Error showing field suggestions:", error);
  }
}

// Focus the editor when clicking on the container
const focusEditor = () => {
  if (editorInstance.value) {
    editorInstance.value.focus();
  }
}

// Simple field insertion that appends to current value
const insertField = (fieldName: string) => {
  const currentValue = code.value.trim();
  code.value = currentValue ? `${currentValue} ${fieldName}=` : `${fieldName}=`;
  onChange();
}

// Quick examples using LogChefQL syntax
const examples = [
  { label: 'Errors', expression: "severity_text='ERROR'" },
  { label: 'Warnings', expression: "severity_text='WARN' OR severity_text='WARNING'" },
  { label: 'Recent 5xx', expression: "status_code>=500 AND status_code<600" },
  { label: 'Complex', expression: "(method='GET' OR method='POST') AND status_code>=400" },
];

// Apply example expression - simplified version
const applyExample = (expression: string) => {
  try {
    if (editorInstance.value) {
      const model = editorInstance.value.getModel();
      if (!model) return;
      
      // Replace entire content with new expression
      editorInstance.value.executeEdits('applyExample', [{
        range: model.getFullModelRange(),
        text: expression
      }]);
      
      // Place cursor at the end
      const lastLine = model.getLineCount();
      const lastColumn = model.getLineLength(lastLine) + 1;
      editorInstance.value.setPosition({ lineNumber: lastLine, column: lastColumn });
      editorInstance.value.focus();
      
      // Trigger change event
      onChange();
    } else {
      code.value = expression;
      onChange();
    }
  } catch (error) {
    console.error("Error applying example:", error);
  }
}

// Help tooltip content with keyboard shortcuts and LogChefQL syntax
const tooltipItems = [
  'Use Alt+Space to show field suggestions',
  'Use = for equality, != for inequality',
  'Use > < >= <= for numeric comparisons',
  'Use ~ for contains, !~ for not contains',
  'Combine conditions with AND/OR (case insensitive)',
  'Group expressions with parentheses: (expr1 OR expr2) AND expr3',
  'Use quotes for text values: field = "value" or field = \'value\'',
  'Use Ctrl+Enter to execute search'
];

// Expose methods to parent component
defineExpose({
  insertFieldTrigger,
  insertField,
  focusEditor
});
</script>

<template>
  <div class="w-full h-full">
    <!-- Split Editor Layout -->
    <div class="flex flex-col lg:flex-row h-full">
      <!-- Left Panel: Fields - Fixed version -->
      <div v-if="columns && columns.length > 0" class="border rounded-l-md bg-card overflow-hidden flex flex-col max-h-full 
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
            <Search class="absolute right-2 top-1/2 transform -translate-y-1/2 h-3.5 w-3.5 text-muted-foreground" />
          </div>
        </div>
        
        <div class="flex-1 overflow-y-auto p-2 space-y-1">
          <button 
            v-for="column in filteredColumns" 
            :key="column.name" 
            @click="insertFieldAtCursor(column.name)"
            class="w-full text-left px-2 py-1.5 rounded-sm text-xs hover:bg-muted/80 text-foreground/90 transition-colors flex items-center justify-between"
          >
            <div class="flex items-center gap-1.5 truncate">
              <!-- Field type icon with improved type detection -->
              <span 
                class="inline-flex items-center justify-center w-5 h-5 rounded-sm text-white shrink-0"
                :class="{
                  'bg-blue-500': column.type && (column.type.toLowerCase().includes('string') || column.type.toLowerCase().includes('text') || column.type.toLowerCase().includes('char')),
                  'bg-green-500': column.type && (column.type.toLowerCase().includes('int') || column.type.toLowerCase().includes('float') || column.type.toLowerCase().includes('double') || column.type.toLowerCase().includes('decimal')),
                  'bg-amber-500': column.type && (column.type.toLowerCase().includes('time') || column.type.toLowerCase().includes('date')),
                  'bg-purple-500': column.type && column.type.toLowerCase().includes('bool'),
                  'bg-slate-500': !column.type
                }"
              >
                <svg v-if="column.type && (column.type.toLowerCase().includes('string') || column.type.toLowerCase().includes('text') || column.type.toLowerCase().includes('char'))" 
                     xmlns="http://www.w3.org/2000/svg" viewBox="0 0 20 20" fill="currentColor" class="w-3 h-3">
                  <path d="M10 9a3 3 0 100-6 3 3 0 000 6zM6 8a2 2 0 11-4 0 2 2 0 014 0zM1.49 15.326a.78.78 0 01-.358-.442 3 3 0 014.308-3.516 6.484 6.484 0 00-1.905 3.959c-.023.222-.014.442.025.654a4.97 4.97 0 01-2.07-.655zM16.44 15.98a4.97 4.97 0 002.07-.654.78.78 0 00.357-.442 3 3 0 00-4.308-3.517 6.484 6.484 0 011.907 3.96 2.32 2.32 0 01-.026.654zM18 8a2 2 0 11-4 0 2 2 0 014 0zM5.304 16.19a.844.844 0 01-.277-.71 5 5 0 019.947 0 .843.843 0 01-.277.71A6.975 6.975 0 0110 18a6.974 6.974 0 01-4.696-1.81z" />
                </svg>
                <svg v-else-if="column.type && (column.type.toLowerCase().includes('int') || column.type.toLowerCase().includes('float') || column.type.toLowerCase().includes('double') || column.type.toLowerCase().includes('decimal'))" 
                     xmlns="http://www.w3.org/2000/svg" viewBox="0 0 20 20" fill="currentColor" class="w-3 h-3">
                  <path fill-rule="evenodd" d="M5 2a2 2 0 00-2 2v14l3.5-2 3.5 2 3.5-2 3.5 2V4a2 2 0 00-2-2H5zm4.707 3.707a1 1 0 00-1.414-1.414l-3 3a1 1 0 000 1.414l3 3a1 1 0 001.414-1.414L8.414 9H10a3 3 0 013 3v1a1 1 0 102 0v-1a5 5 0 00-5-5H8.414l1.293-1.293z" clip-rule="evenodd" />
                </svg>
                <svg v-else-if="column.type && (column.type.toLowerCase().includes('time') || column.type.toLowerCase().includes('date'))" 
                     xmlns="http://www.w3.org/2000/svg" viewBox="0 0 20 20" fill="currentColor" class="w-3 h-3">
                  <path fill-rule="evenodd" d="M10 18a8 8 0 100-16 8 8 0 000 16zm1-12a1 1 0 10-2 0v4a1 1 0 00.293.707l2.828 2.829a1 1 0 101.415-1.415L11 9.586V6z" clip-rule="evenodd" />
                </svg>
                <svg v-else-if="column.type && column.type.toLowerCase().includes('bool')" 
                     xmlns="http://www.w3.org/2000/svg" viewBox="0 0 20 20" fill="currentColor" class="w-3 h-3">
                  <path fill-rule="evenodd" d="M3 4a1 1 0 011-1h12a1 1 0 110 2H4a1 1 0 01-1-1zm0 4a1 1 0 011-1h12a1 1 0 110 2H4a1 1 0 01-1-1zm0 4a1 1 0 011-1h12a1 1 0 110 2H4a1 1 0 01-1-1zm0 4a1 1 0 011-1h12a1 1 0 110 2H4a1 1 0 01-1-1z" clip-rule="evenodd" />
                </svg>
                <svg v-else
                     xmlns="http://www.w3.org/2000/svg" viewBox="0 0 20 20" fill="currentColor" class="w-3 h-3">
                  <path fill-rule="evenodd" d="M4 3a2 2 0 00-2 2v10a2 2 0 002 2h12a2 2 0 002-2V5a2 2 0 00-2-2H4zm12 12H4a.5.5 0 01-.5-.5v-9a.5.5 0 01.5-.5h12a.5.5 0 01.5.5v9a.5.5 0 01-.5.5z" clip-rule="evenodd" />
                </svg>
              </span>
              <span class="truncate">{{ column.name }}</span>
            </div>
            <!-- Column type indicator -->
            <span class="text-[10px] text-muted-foreground ml-1">{{ column.type }}</span>
          </button>
        </div>
      </div>

      <!-- Right Panel: Editor -->
      <div :class="[
              'border rounded-md bg-card flex-1 flex flex-col overflow-hidden',
              columns && columns.length > 0 ? 'lg:rounded-l-none lg:border-l-0' : ''
            ]">
        <!-- Examples header -->
        <div v-if="showExamples" class="flex items-center gap-1.5 px-3 py-2 border-b border-border/30 text-xs">
          <span class="font-medium text-muted-foreground mr-1">Examples:</span>
          <div class="flex flex-wrap gap-1.5">
            <button v-for="example in examples" :key="example.label" 
                    @click="applyExample(example.expression)"
                    class="inline-flex items-center px-2 py-0.5 rounded-sm text-[10px] bg-muted/50 hover:bg-muted text-foreground/80 hover:text-foreground transition-colors border border-border/30">
              {{ example.label }}
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
                  <h3 class="font-medium mb-1.5 text-sm">Filter Query Syntax</h3>
                  <div class="mb-2">
                    <div class="text-primary text-[11px] font-medium mb-1">Common Patterns:</div>
                    <code class="text-[10px] bg-muted/50 p-1 rounded block mb-1">field = 'value'</code>
                    <code class="text-[10px] bg-muted/50 p-1 rounded block mb-1">field > 100 AND field < 200</code>
                    <code class="text-[10px] bg-muted/50 p-1 rounded block">message ~ 'error' OR severity = 'ERROR'</code>
                  </div>
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
        
        <!-- Editor container -->
        <div 
            ref="editorContainer" 
            class="flex-1 min-h-[80px] cursor-text smart-filter-bar-component overflow-hidden" 
            :class="[
              { 'outline outline-1 outline-primary': editorFocused },
              compactMode ? 'px-3 py-2' : ''
            ]" 
            @click="focusEditor">
          <!-- Monaco editor -->
          <VueMonacoEditor 
              v-model:value="code" 
              :theme="theme" 
              language="logchefql" 
              :options="editorOptions"
              @mount="handleMount" 
              @change="onChange" 
              class="w-full h-full" />
        </div>
        
        <!-- Footer with keyboard shortcuts -->
        <div class="px-3 py-1 border-t border-border/30 text-[10px] text-muted-foreground flex items-center gap-3">
          <span>
            <kbd class="px-1 py-0.5 bg-background/80 rounded border border-border/50 font-mono">Alt+Space</kbd>
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

<style scoped>
.smart-filter-bar-component {
  /* Prevent clipping of Monaco editor content */
  position: relative;
}

.smartfilter-monaco-container {
  /* Custom styling for compact mode */
  position: relative;
  border-radius: theme('borderRadius.md');
  transition: all 0.2s ease;
}

.smartfilter-monaco-container.focused {
  outline: 1px solid hsl(var(--primary));
}
</style>
