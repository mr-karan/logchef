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
}

const props = withDefaults(defineProps<Props>(), {
  columns: () => [],
  placeholder: 'Enter query (click @ for field suggestions)',
  showExamples: true
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

// Editor options - optimized for compact single-line editor appearance
const editorOptions = computed((): monaco.editor.IStandaloneEditorConstructionOptions => ({
  ...getSingleLineMonacoOptions(),
  cursorBlinking: 'smooth',
  cursorSmoothCaretAnimation: 'on',
  fontSize: 13,
  lineHeight: 20,
  // Force single-line mode
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

// Handle editor mount
const handleMount = (editor: monaco.editor.IStandaloneCodeEditor) => {
  editorInstance.value = editor;

  // Register completion provider for field names
  try {
    completionProvider.value = registerCompletionProvider(props.columns);
  } catch (error) {
    console.error("Failed to register completion provider:", error);
  }

  // Add key listeners to detect typing
  addKeyListeners(editor);

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

// Simple change handler for better Monaco integration
const onChange = () => {
  try {
    // First check if the editor is actually empty
    if (!code.value || code.value.trim() === '') {
      // Clear filter conditions completely
      emit('update:modelValue', []);
    } else {
      // Get filters from the expression
      const filters = parseFilterExpression(code.value);

      // Always emit filter changes to keep the preview SQL updated
      emit('update:modelValue', filters);
    }

    // Update placeholder if needed
    if (editorInstance.value) {
      updatePlaceholder(editorInstance.value);

      // Force refresh completion providers to ensure they're working
      if (editorInstance.value.hasTextFocus()) {
        setTimeout(() => {
          try {
            monaco.editor.trigger('keyboard', 'editor.action.triggerSuggest', {});
          } catch (e) {
            // Ignore errors in suggestion triggering
          }
        }, 100);
      }
    }
  } catch (error) {
    console.error("Error in onChange handler:", error);
  }
}

// Remove the special keyboard handler that's causing freezes
// We'll rely on the onChange handler which should catch all editor content changes

// Preserve user typing - don't update the filter while user is actively typing
let userIsTyping = false;
let typingTimer: any = null;

const setUserTyping = () => {
  userIsTyping = true;
  clearTimeout(typingTimer);
  typingTimer = setTimeout(() => {
    userIsTyping = false;
  }, 1000); // Consider user done typing after 1 second of inactivity
};

// Handle editor keydown to detect user typing
const handleKeyDown = () => {
  setUserTyping();
};

// Add keydown handler when editor is mounted
const addKeyListeners = (editor: monaco.editor.IStandaloneCodeEditor) => {
  try {
    editor.onKeyDown(() => {
      setUserTyping();
    });
  } catch (error) {
    console.error("Error adding key listeners:", error);
  }
};

// Watch for external changes to modelValue - with typing protection
watch(() => props.modelValue, (newVal) => {
  try {
    // Skip updates while user is actively typing
    if (userIsTyping) {
      return;
    }

    const currentExpression = code.value.trim();

    // Special case: user is in the middle of typing an expression with an equals sign
    if (currentExpression && /[a-zA-Z0-9_]+\s*=\s*['"]?$/.test(currentExpression)) {
      return;
    }

    // Only update if we have a modelValue to convert and we're not actively typing
    if (newVal && newVal.length > 0) {
      const newExpression = filterConditionsToExpression(newVal);
      // Only update the code if it's different to avoid cursor jumping
      if (newExpression !== currentExpression) {
        code.value = newExpression;
      }
    } else if (currentExpression !== '' && !currentExpression.includes('=')) {
      // Don't clear incomplete expressions that are being typed
    } else if (newVal?.length === 0 && currentExpression === '') {
      // Both are empty, no change needed
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

// Set initial expression from modelValue
onMounted(() => {
  try {
    if (props.modelValue.length > 0) {
      code.value = filterConditionsToExpression(props.modelValue);
    }

    // Add style element
    const styleEl = document.createElement('style')
    styleEl.textContent = customCss
    document.head.appendChild(styleEl)
  } catch (error) {
    console.error("Error in onMounted:", error);
  }
});

// Clean up on unmount
onBeforeUnmount(() => {
  try {
    if (completionProvider.value) {
      completionProvider.value.dispose();
    }
  } catch (error) {
    console.error("Error in onBeforeUnmount:", error);
  }
});

// Show field suggestions - simplified to prevent crashes
const insertFieldTrigger = () => {
  try {
    if (editorInstance.value) {
      // Just focus and show standard suggestions
      editorInstance.value.focus();
      // Use the function from logfilter-language.ts instead of directly calling monaco
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

// Guaranteed stable field insertion with minimum Monaco API usage
const insertField = (fieldName: string) => {
  try {
    // Get current value (safest approach)
    const currentContent = code.value;

    // Calculate new content by simply appending field (most stable approach)
    const needSpace = currentContent.length > 0 &&
      !currentContent.endsWith(' ') &&
      !currentContent.endsWith(';');

    // Update value with minimal operations
    const newContent = currentContent + (needSpace ? ' ' : '') + fieldName + ' ';

    // Use separate step for focusing vs modifying content
    // to avoid race conditions

    // First set content
    code.value = newContent;

    // Then focus after UI has time to update
    window.setTimeout(() => {
      if (editorInstance.value) {
        // Basic focus only
        editorInstance.value.focus();
      }
    }, 50);

  } catch (error) {
    console.error("Error in failsafe field insertion:", error);
  }
}

// Quick examples using LogChefQL syntax
const examples = [
  { label: 'Errors', expression: "severity_text='ERROR'" },
  { label: 'Warnings', expression: "severity_text='WARN' OR severity_text='WARNING'" },
  { label: 'Recent 5xx', expression: "status_code>=500 AND status_code<600" },
  { label: 'Complex', expression: "(method='GET' OR method='POST') AND status_code>=400" },
];

const applyExample = (expression: string) => {
  code.value = expression;
  onChange();

  // Focus the editor after applying example
  nextTick(() => {
    if (editorInstance.value) {
      editorInstance.value.focus();
    }
  });
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

// Control tooltip open state for click triggering
// const isTooltipOpen = ref(false);

// Toggle tooltip on help icon click
// const toggleTooltip = () => {
//   isTooltipOpen.value = !isTooltipOpen.value;
// };

// Close tooltip when clicked elsewhere
// const closeTooltip = () => {
//   isTooltipOpen.value = false;
// };
</script>

<template>
  <div class="w-full">
    <!-- Enhanced Filter Bar with validation indicators -->
    <div class="relative flex flex-col w-full">
      <!-- Editor with improved styling and syntax highlighting -->
      <div ref="editorContainer" class="smartfilter-monaco-container px-3 py-2 overflow-hidden cursor-text w-full"
        :class="{ 'focused': editorFocused }" @click="focusEditor" style="height: 40px;">

        <!-- Help tooltip with improved content -->
        <TooltipProvider :delay-duration="300">
          <Tooltip>
            <TooltipTrigger asChild>
              <div class="absolute right-2 top-1 z-10">
                <Button size="icon" class="h-5 w-5 rounded-full" variant="ghost">
                  <HelpCircle class="h-3 w-3 text-muted-foreground/50" />
                </Button>
              </div>
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

        <!-- Monaco editor with enhanced styling -->
        <VueMonacoEditor v-model:value="code" :theme="theme" language="logchefql" :options="editorOptions"
          @mount="handleMount" @change="onChange" class="w-full h-full" />
      </div>
    </div>
  </div>
</template>
