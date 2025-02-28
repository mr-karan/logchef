<script setup lang="ts">
import { ref, computed, onMounted, onBeforeUnmount, watch, nextTick } from 'vue'
import * as monaco from 'monaco-editor'
import { Button } from '@/components/ui/button'

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
  min-height: 22px !important;
}
/* Hide scrollbars but allow horizontal scrolling functionality */
.monaco-editor .scrollbar.horizontal .slider {
  height: 3px !important;
  opacity: 0.6 !important;
}
`
import { Search, X, HelpCircle } from 'lucide-vue-next'
import type { FilterCondition } from '@/api/explore'
import { VueMonacoEditor } from '@guolao/vue-monaco-editor'
import { useColorMode } from '@vueuse/core'
import { getSingleLineMonacoOptions } from '@/utils/monaco'
import {
  registerCompletionProvider,
  parseFilterExpression,
  filterConditionsToExpression,
  showFieldSuggestions
} from '@/utils/logfilter-language'
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

// Editor options - optimized for better UX
const editorOptions = computed((): monaco.editor.IStandaloneEditorConstructionOptions => ({
  ...getSingleLineMonacoOptions(),
  cursorBlinking: 'blink',
  cursorSmoothCaretAnimation: 'off',
  fontSize: 14,
  lineHeight: 22,
  wordWrap: 'off',
  lineNumbers: 'off',
  folding: false,
  wrappingIndent: 'none',
  scrollBeyondLastLine: false,
  renderWhitespace: 'none',
  scrollbar: {
    vertical: 'hidden',
    horizontal: 'auto',
    handleMouseWheel: true
  },
  find: {
    seedSearchStringFromSelection: 'never'
  },
  // Improved styling for cohesive look
  minimap: { enabled: false },
  padding: { top: 4, bottom: 4 },
  glyphMargin: false,
  lineDecorationsWidth: 0,
  lineNumbersMinChars: 0
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
    updatePlaceholder(editor);
  });

  // Support @ character directly to improve trigger experience
  // Simplify key handlers to avoid potential issues
  editor.onDidContentSizeChange(() => {
    updatePlaceholder(editor);
  });

  // Listen for model content changes directly
  // This will catch ALL changes including Ctrl+A followed by cut/delete
  editor.onDidChangeModelContent(() => {
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

  // Simple placeholder setup
  updatePlaceholder(editor);

  // Natural focus handling
  setTimeout(() => {
    editor.focus();
  }, 50);
}

// Update placeholder visibility
const updatePlaceholder = (editor: monaco.editor.IStandaloneCodeEditor) => {
  const container = editorContainer.value;
  if (!container) return;

  if (!code.value && !editorFocused.value) {
    container.setAttribute('data-placeholder', props.placeholder);
  } else {
    container.removeAttribute('data-placeholder');
  }
}

// Handle content changes
const onChange = () => {
  try {
    // Handle content changes from editor

    // First check if the editor is actually empty
    if (!code.value || code.value.trim() === '') {
      // Clear filter conditions completely
      emit('update:modelValue', []);
    } else {
      // Get filters from the expression
      const filters = parseFilterExpression(code.value);
      // Update model value with the array of filter conditions
      emit('update:modelValue', filters);
    }

    // Update placeholder if needed
    if (editorInstance.value) {
      updatePlaceholder(editorInstance.value);
    }
  } catch (error) {
    console.error("Error in onChange handler:", error);
  }
}

// Remove the special keyboard handler that's causing freezes
// We'll rely on the onChange handler which should catch all editor content changes

// Track last operation time to prevent race conditions
let lastOperationTime = 0;

// Watch for external changes to modelValue
watch(() => props.modelValue, (newVal) => {
  try {
    const newExpression = filterConditionsToExpression(newVal);
    if (newExpression !== code.value) {
      code.value = newExpression;
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

// Quick examples
const examples = [
  { label: 'Errors', expression: 'severity_text = "ERROR"' },
  { label: 'Warnings', expression: 'severity_text = "WARN" OR severity_text = "WARNING"' },
  { label: 'Recent 5xx', expression: 'status_code >= 500 AND status_code < 600' },
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

// Help tooltip content with keyboard shortcuts
const tooltipItems = [
  'Use Alt+Space to show field suggestions',
  'Use = for equality, != for inequality',
  'Use > < >= <= for numeric comparisons',
  'Use ~ for contains, !~ for not contains',
  'Combine conditions with AND/OR',
  'Use semicolon (;) to separate multiple conditions',
  'Use quotes for text values: field = "value"',
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
  <div class="w-full space-y-2">
    <!-- Filter Bar with improved UX -->
    <div class="relative flex flex-col gap-1 w-full">
      <!-- Main query bar with help on left side -->
      <div class="relative flex items-center rounded-md shadow-sm border-0 bg-background ring-1 ring-inset ring-input"
        :class="{ 'ring-primary': editorFocused }" @click="focusEditor">

        <!-- Left side controls - redesigned for a more integrated, sleek look -->
        <div class="flex items-center pl-2">
          <div class="flex h-7 rounded-lg border border-border bg-muted/30 divide-x divide-border overflow-hidden">
            <!-- Help button in unified design -->
            <TooltipProvider :delay-duration="0">
              <Tooltip>
                <TooltipTrigger asChild>
                  <button
                    class="text-muted-foreground hover:text-foreground flex items-center px-2 h-full justify-center hover:bg-accent/40 transition-colors">
                    <HelpCircle class="h-3.5 w-3.5" />
                  </button>
                </TooltipTrigger>
                <TooltipContent side="bottom" align="start" class="max-w-sm p-4">
                  <h3 class="font-medium mb-2">Filter Query Usage Tips</h3>
                  <ul class="text-sm space-y-1">
                    <li v-for="(item, index) in tooltipItems" :key="index" class="flex items-start">
                      <span class="mr-2">•</span>
                      <span>{{ item }}</span>
                    </li>
                  </ul>
                </TooltipContent>
              </Tooltip>
            </TooltipProvider>

            <!-- Fields button in integrated design -->
            <button @click.stop="insertFieldTrigger"
              class="text-muted-foreground hover:text-foreground flex items-center px-3 h-full text-xs font-mono justify-center hover:bg-accent/40 transition-colors"
              title="Show field suggestions (Alt+Space)">
              Fields
            </button>
          </div>
        </div>

        <!-- Editor Container - with improved spacing and styling -->
        <div ref="editorContainer" class="flex-1 h-9 min-h-[36px] overflow-hidden cursor-text px-2 py-1 flex items-center">
          <VueMonacoEditor v-model:value="code" :theme="theme" language="logfilter" :options="editorOptions"
            @mount="handleMount" @change="onChange" class="w-full h-full" />
        </div>

        <!-- Search button on right -->
        <div class="flex items-center pr-1">
          <Button variant="default" size="sm" @click.stop="$emit('search')" class="h-7 px-3">
            <Search class="h-4 w-4 mr-1" />
            Search
          </Button>
        </div>
      </div>
    </div>

  </div>
</template>
