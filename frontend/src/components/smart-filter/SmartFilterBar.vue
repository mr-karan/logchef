<script setup lang="ts">
import { ref, computed, onMounted, onBeforeUnmount, watch, nextTick } from 'vue'
import * as monaco from 'monaco-editor'
import { Button } from '@/components/ui/button'
import { Search, X, HelpCircle } from 'lucide-vue-next'
import type { FilterCondition } from '@/api/explore'
import { VueMonacoEditor } from '@guolao/vue-monaco-editor'
import { useDark } from '@vueuse/core'
import { getSingleLineMonacoOptions } from '@/utils/monaco'
import { 
  registerCompletionProvider, 
  parseFilterExpression, 
  filterConditionsToExpression 
} from '@/utils/logfilter-language'

// Define props and emits
interface Props {
  modelValue: FilterCondition[]
  columns?: { name: string; type: string }[]
  placeholder?: string
  showExamples?: boolean
}

const props = withDefaults(defineProps<Props>(), {
  columns: () => [],
  placeholder: 'Enter query (press @ for field suggestions)',
  showExamples: true
})

const emit = defineEmits<{
  (e: 'update:modelValue', value: FilterCondition[]): void
  (e: 'search'): void
}>()

// Dark mode detection
const isDark = useDark()
const theme = computed(() => isDark.value ? 'logchef-dark' : 'logchef-light')

// Editor state
const code = ref('')
const editorFocused = ref(false)
const editorInstance = ref<monaco.editor.IStandaloneCodeEditor | null>(null)
const disposables = ref<monaco.IDisposable[]>([])

// Editor options
const editorOptions = computed(() => ({
  ...getSingleLineMonacoOptions(),
  fontSize: 14,
  padding: { top: 8, bottom: 8 },
  lineHeight: 22,
  renderWhitespace: 'none',
  renderControlCharacters: false,
  renderIndentGuides: false,
  renderValidationDecorations: 'on',
  fontFamily: 'ui-monospace, SFMono-Regular, Menlo, Monaco, Consolas, monospace',
  fontLigatures: true,
  placeholderText: props.placeholder,
}))

// Handle editor mount
const handleMount = (editor: monaco.editor.IStandaloneCodeEditor) => {
  editorInstance.value = editor;
  
  // Register completion provider for field names
  disposables.value.push(
    registerCompletionProvider(props.columns)
  );

  // Handle keyboard shortcuts
  disposables.value.push(
    editor.addAction({
      id: 'submit',
      label: 'Submit Query',
      keybindings: [
        monaco.KeyMod.CtrlCmd | monaco.KeyCode.Enter
      ],
      run: () => {
        emit('search');
      }
    })
  );

  // Focus events
  disposables.value.push(
    editor.onDidFocusEditorWidget(() => {
      editorFocused.value = true;
    }),
    
    editor.onDidBlurEditorWidget(() => {
      editorFocused.value = false;
    })
  );
  
  // Set initial cursor position at the end
  editor.setPosition({ lineNumber: 1, column: code.value.length + 1 });
  
  // Add placeholder text decoration if needed
  updatePlaceholder(editor);
}

// Update placeholder text when editor is empty
const updatePlaceholder = (editor: monaco.editor.IStandaloneCodeEditor) => {
  if (!code.value && !editorFocused.value) {
    const placeholderDecoration = editor.createDecorationsCollection([{
      range: new monaco.Range(1, 1, 1, 1),
      options: {
        after: {
          content: props.placeholder,
          inlineClassName: 'monaco-placeholder-text'
        }
      }
    }]);
    
    // Store the decoration ID to remove it later
    return placeholderDecoration;
  }
  return null;
}

// Handle content changes
const onChange = () => {
  const filters = parseFilterExpression(code.value);
  emit('update:modelValue', filters);
  
  // Update placeholder if needed
  if (editorInstance.value) {
    updatePlaceholder(editorInstance.value);
  }
}

// Watch for external changes to modelValue
watch(() => props.modelValue, (newVal) => {
  const newExpression = filterConditionsToExpression(newVal);
  if (newExpression !== code.value) {
    code.value = newExpression;
  }
}, { deep: true });

// Watch for changes to columns to update completion provider
watch(() => props.columns, () => {
  // Clean up old provider
  disposables.value.forEach(d => d.dispose());
  disposables.value = [];
  
  // Register new provider if editor exists
  if (editorInstance.value) {
    disposables.value.push(
      registerCompletionProvider(props.columns)
    );
  }
}, { deep: true });

// Set initial expression from modelValue
onMounted(() => {
  if (props.modelValue.length > 0) {
    code.value = filterConditionsToExpression(props.modelValue);
  }
});

// Clean up on unmount
onBeforeUnmount(() => {
  disposables.value.forEach(d => d.dispose());
});

// Clear the filter
const clearFilter = () => {
  code.value = '';
  emit('update:modelValue', []);
  
  // Focus the editor after clearing
  nextTick(() => {
    editorInstance.value?.focus();
  });
}

// Insert @ character to trigger field suggestions
const insertFieldTrigger = () => {
  if (editorInstance.value) {
    const position = editorInstance.value.getPosition();
    if (position) {
      editorInstance.value.executeEdits('', [
        {
          range: {
            startLineNumber: position.lineNumber,
            startColumn: position.column,
            endLineNumber: position.lineNumber,
            endColumn: position.column
          },
          text: '@'
        }
      ]);
      // Trigger suggestions
      editorInstance.value.trigger('keyboard', 'editor.action.triggerSuggest', {});
    }
  }
}

// Quick examples
const examples = [
  { label: 'Errors', expression: 'severity_text="ERROR"' },
  { label: 'Warnings', expression: 'severity_text="WARN"' },
  { label: 'Recent 5xx', expression: 'status_code>="500";status_code<"600"' },
];

const applyExample = (expression: string) => {
  code.value = expression;
  onChange();
  
  // Focus the editor after applying example
  nextTick(() => {
    editorInstance.value?.focus();
  });
}

// Show help tooltip
const showHelp = ref(false);
</script>

<template>
  <div class="w-full space-y-1">
    <!-- Filter Bar -->
    <div class="relative flex items-center rounded-md shadow-sm border-0 bg-background ring-1 ring-inset ring-input"
      :class="{ 'ring-primary': editorFocused }">
      <!-- Field trigger button -->
      <button 
        @click="insertFieldTrigger" 
        class="flex items-center justify-center h-9 px-2 text-muted-foreground hover:text-foreground"
        title="Insert field (@)">
        @
      </button>
      
      <!-- Editor Container -->
      <div class="flex-1 h-9 min-h-[36px] overflow-hidden">
        <VueMonacoEditor 
          v-model:value="code" 
          :theme="theme" 
          language="logfilter" 
          :options="editorOptions"
          @mount="handleMount" 
          @change="onChange" 
        />
      </div>

      <!-- Actions -->
      <div class="flex items-center pr-1">
        <Button 
          v-if="code" 
          variant="ghost" 
          size="icon" 
          @click="clearFilter" 
          class="h-7 w-7 p-0 mr-1 text-muted-foreground"
          title="Clear filter">
          <X class="h-4 w-4" />
        </Button>
        <Button 
          variant="default" 
          size="sm" 
          @click="$emit('search')" 
          class="h-7 px-3">
          <Search class="h-4 w-4 mr-1" />
          Search
        </Button>
      </div>
    </div>

    <!-- Quick Examples and Help -->
    <div v-if="showExamples" class="flex items-center justify-between">
      <div class="flex flex-wrap gap-2">
        <Button 
          v-for="example in examples" 
          :key="example.label" 
          variant="outline" 
          size="sm" 
          class="h-6 text-xs px-2 py-0"
          @click="applyExample(example.expression)">
          {{ example.label }}
        </Button>
      </div>
      
      <div class="relative">
        <Button 
          variant="ghost" 
          size="icon" 
          class="h-6 w-6 p-0 text-muted-foreground"
          @click="showHelp = !showHelp"
          title="Show syntax help">
          <HelpCircle class="h-4 w-4" />
        </Button>
        
        <div v-if="showHelp" class="absolute right-0 top-full mt-1 p-3 z-10 bg-popover text-popover-foreground rounded-md shadow-md text-xs w-64">
          <h4 class="font-medium mb-1">Query Syntax:</h4>
          <ul class="space-y-1 list-disc pl-4">
            <li><code>field=value</code> - Exact match</li>
            <li><code>field!=value</code> - Not equal</li>
            <li><code>field>value</code> - Greater than</li>
            <li><code>field~value</code> - Contains</li>
            <li><code>field1=value;field2=value</code> - Multiple conditions</li>
          </ul>
          <p class="mt-2 text-muted-foreground">Press <kbd>@</kbd> for field suggestions</p>
          <p class="text-muted-foreground">Press <kbd>Ctrl+Enter</kbd> to search</p>
        </div>
      </div>
    </div>
  </div>
</template>

<style>
/* Custom styles for editor container */
.vue-monaco-editor {
  border-radius: 0;
  overflow: hidden;
}

/* Remove editor border */
.monaco-editor {
  border: none !important;
}

/* Style for placeholder text */
.monaco-placeholder-text {
  color: var(--muted-foreground, #9ca3af);
  font-style: italic;
  opacity: 0.8;
}

/* Improve suggestion widget appearance */
.monaco-editor .suggest-widget {
  border-radius: 6px;
  overflow: hidden;
  box-shadow: 0 4px 6px -1px rgba(0, 0, 0, 0.1), 0 2px 4px -1px rgba(0, 0, 0, 0.06);
}

.monaco-editor .suggest-widget .monaco-list .monaco-list-row.focused {
  background-color: var(--primary, #2563eb);
  color: white;
}

.monaco-editor .suggest-widget .monaco-list .monaco-list-row:hover:not(.focused) {
  background-color: var(--accent, #f3f4f6);
}
</style>
