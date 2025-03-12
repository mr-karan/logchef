<template>
  <div>
    <!-- Tabbed language toggle with icons -->
    <Tabs v-model="activeMode" class="w-full mb-2">
      <TabsList class="grid w-64 grid-cols-2 ml-auto shadow-sm">
        <TabsTrigger value="dsl" class="h-7 px-2 text-xs">
          <Terminal class="inline-block h-3.5 w-3.5 align-text-bottom mr-1" />LogchefQL
        </TabsTrigger>
        <TabsTrigger value="sql" class="h-7 px-2 text-xs">
          <Database class="inline-block h-3.5 w-3.5 align-text-bottom mr-1" />SQL
        </TabsTrigger>
      </TabsList>
    </Tabs>

    <!-- Editor container with minimal styling -->
    <div 
      :style="{ height: `${editorHeight}px` }" 
      class="rounded-md border dark:border-neutral-700"
      :class="{ 'border-sky-800 dark:border-sky-700': editorFocused }"
    >
      <vue-monaco-editor 
        v-model:value="code" 
        :theme="currentTheme" 
        :language="currentLanguage"
        :options="editorOptions" 
        @mount="handleMount" 
        @change="onChange" 
      />
    </div>

    <!-- Error display -->
    <div v-if="error" class="text-destructive text-xs mt-1 flex items-center">
      <svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2"
        stroke-linecap="round" stroke-linejoin="round" class="h-3.5 w-3.5 mr-1">
        <circle cx="12" cy="12" r="10" />
        <line x1="12" y1="8" x2="12" y2="12" />
        <line x1="12" y1="16" x2="12" y2="16" />
      </svg>
      {{ error }}
    </div>
  </div>
</template>

<script setup>
import { ref, computed, shallowRef, nextTick, watch, defineExpose } from 'vue'
import * as monaco from "monaco-editor"
import { useDark } from '@vueuse/core'
import { VueMonacoEditor } from '@guolao/vue-monaco-editor'
import { getDefaultMonacoOptions } from '@/utils/monaco'
import { translateLogchefQLToSQL } from '@/utils/logchefql/api'
import { Tabs, TabsList, TabsTrigger } from '@/components/ui/tabs'
import { Terminal, Database } from 'lucide-vue-next'
import { Parser, State, VALID_KEY_VALUE_OPERATORS } from "@/utils/logchefql"

// Define emits and props
const emit = defineEmits(['update:modelValue', 'change', 'submit', 'modeChange'])
const props = defineProps({
  modelValue: {
    type: String,
    default: ''
  },
  placeholder: {
    type: String,
    default: 'field="value" and another_field="value"'
  },
  availableFields: {
    type: Array,
    default: () => []
  },
  height: {
    type: [Number, String],
    default: 40,
    validator: (value) => {
      return !isNaN(Number(value));
    }
  },
  error: {
    type: String,
    default: ''
  },
  theme: {
    type: String,
    default: undefined
  },
  tableName: {
    type: String,
    default: 'logs.vector_logs'
  }
})

// State variables
const isDark = useDark()
const editorFocused = ref(false)
const code = ref(props.modelValue)
const sqlCode = ref('')
const activeMode = ref('dsl')
const editorRef = shallowRef()

// Editor options
const editorOptions = computed(() => ({
  ...getDefaultMonacoOptions(),
  placeholder: props.placeholder
}))

// Current language based on active mode
const currentLanguage = computed(() => 
  activeMode.value === 'dsl' ? 'logchefql' : 'clickhouse-sql'
)

// Set theme based on dark mode
const currentTheme = computed(() => 
  props.theme || (isDark.value ? 'logchef-dark' : 'logchef')
)

// Dynamic height calculation
const editorHeight = computed(() => {
  const lines = (code.value.match(/\n/g) || []).length + 1
  const minHeight = Number(props.height)
  return Math.max(minHeight, lines * 20)
})

/**
 * Get the default SQL query
 */
function getDefaultSQLQuery(tableName = 'logs.vector_logs') {
  return `SELECT * FROM ${tableName}`;
}

/**
 * Helper to get suggestions from a list
 */
const getSuggestionsFromList = (params) => {
  // Use a Map to deduplicate suggestions by label
  const suggestionMap = new Map();
  const defaultPostfix = params.postfix || '';
  
  const range = params.range || {
    startLineNumber: params.position.lineNumber,
    endLineNumber: params.position.lineNumber,
    startColumn: params.position.column,
    endColumn: params.position.column,
  };
  
  if (!params.items || !Array.isArray(params.items) || params.items.length === 0) {
    return [];
  }
  
  for (const item of params.items) {
    let label, sortText, postfix = defaultPostfix, detail, documentation;
    
    if (typeof item === 'string') {
      label = item;
      sortText = label;
    } else {
      label = item.label;
      sortText = item.sortText || label;
      postfix = item.postfix ?? defaultPostfix;
      detail = item.detail;
      documentation = item.documentation;
    }
    
    if (!label) continue;
    
    const insertText = (item.insertText || label) + postfix;
    
    // Only add if we haven't seen this label before
    if (!suggestionMap.has(label)) {
      suggestionMap.set(label, {
        label,
        kind: params.kind,
        range,
        sortText,
        insertText,
        detail,
        documentation,
        command: { id: 'editor.action.triggerSuggest' }
      });
    }
  }
  
  // Convert Map values to array
  return Array.from(suggestionMap.values());
};

/**
 * Get operator suggestions
 */
const getOperatorSuggestions = (field, position) => {
  const operators = [
    { label: '=', sortText: 'a' },
    { label: '!=', sortText: 'b' },
    { label: '~=', sortText: 'c' },
    { label: '!~', sortText: 'd' },
    { label: '>', sortText: 'e' },
    { label: '<', sortText: 'f' },
    { label: '>=', sortText: 'g' },
    { label: '<=', sortText: 'h' },
  ];

  return getSuggestionsFromList({
    position,
    items: operators,
    kind: monaco.languages.CompletionItemKind.Operator
  });
};

/**
 * Get boolean operator suggestions
 */
const getBooleanOperatorSuggestions = (range) => {
  return getSuggestionsFromList({
    range,
    items: ['and', 'or'],
    kind: monaco.languages.CompletionItemKind.Keyword,
    postfix: ' '
  });
};

/**
 * Get field suggestions
 */
const getFieldSuggestions = (range) => {
  if (!props.availableFields || props.availableFields.length === 0) {
    return [];
  }

  const fieldItems = props.availableFields.map(field => ({
    label: field.name,
    sortText: field.isTimestamp ? '0' + field.name :
              field.isSeverity ? '1' + field.name : '2' + field.name,
    insertText: field.name,
    detail: field.type || 'unknown',
    documentation: field.isTimestamp ? 'Timestamp field' :
                  field.isSeverity ? 'Severity field' : `Field type: ${field.type || 'unknown'}`
  }));

  return getSuggestionsFromList({
    range,
    items: fieldItems,
    kind: monaco.languages.CompletionItemKind.Field
  });
};

/**
 * Get value suggestions based on field
 */
const getValueSuggestions = (fieldName, range) => {
  const field = props.availableFields.find(f => f.name === fieldName);
  
  // Timestamp field suggestions
  if (field?.isTimestamp) {
    const now = new Date();
    const formattedDateTime = now.toISOString().replace('T', ' ').substring(0, 19);
    
    return getSuggestionsFromList({
      range,
      items: [
        'now()',
        'today()',
        `"${formattedDateTime}"`
      ],
      kind: monaco.languages.CompletionItemKind.Function,
      postfix: ' '
    });
  }
  
  // Severity field suggestions
  if (field?.isSeverity) {
    return getSuggestionsFromList({
      range,
      items: ['"error"', '"warn"', '"info"', '"debug"', '"trace"'],
      kind: monaco.languages.CompletionItemKind.Value,
      postfix: ' '
    });
  }
  
  // Type-based suggestions
  if (field?.type) {
    if (/int|float|double|decimal|number/i.test(field.type)) {
      return getSuggestionsFromList({
        range,
        items: ['0', '1'],
        kind: monaco.languages.CompletionItemKind.Value,
        postfix: ' '
      });
    }
    
    if (/bool/i.test(field.type)) {
      return getSuggestionsFromList({
        range,
        items: ['true', 'false'],
        kind: monaco.languages.CompletionItemKind.Value,
        postfix: ' '
      });
    }
  }
  
  return [];
};

/**
 * Get LogchefQL suggestions
 */
const getLogchefQLSuggestions = async (word, position, textBeforeCursor) => {
  const range = {
    startLineNumber: position.lineNumber,
    endLineNumber: position.lineNumber,
    startColumn: word.startColumn,
    endColumn: word.endColumn,
  };
  
  try {
    const parser = new Parser();
    parser.parse(textBeforeCursor, false, true);
    const context = parser.getCompletionContext();
    
    // Field suggestions
    if ([State.KEY, State.INITIAL, State.BOOL_OP_DELIMITER].includes(context.state)) {
      if (props.availableFields.some(field => field.name === word.word)) {
        const suggestions = getOperatorSuggestions(word.word, position);
        if (suggestions && suggestions.length > 0) {
          return {
            suggestions,
            incomplete: false
          };
        }
      } else {
        const suggestions = getFieldSuggestions(range);
        if (suggestions && suggestions.length > 0) {
          return {
            suggestions,
            incomplete: false
          };
        }
      }
    } 
    // Value suggestions
    else if (context.state === State.KEY_VALUE_OPERATOR && 
             VALID_KEY_VALUE_OPERATORS.includes(context.keyValueOperator)) {
      const suggestions = getValueSuggestions(context.key, range);
      if (suggestions && suggestions.length > 0) {
        return {
          suggestions,
          incomplete: false
        };
      }
    }
    // Boolean operator suggestions
    else if (context.state === State.EXPECT_BOOL_OP) {
      const lastExpressionMatch = textBeforeCursor.match(/\w+\s*[=!<>~]+\s*(['"]?)[^'"]*\1\s*$/);
      
      if (lastExpressionMatch) {
        const suggestions = getBooleanOperatorSuggestions(range);
        if (suggestions && suggestions.length > 0) {
          return {
            suggestions,
            incomplete: false
          };
        }
      }
    }
    
    return undefined;
  } catch (e) {
    console.error('Error getting LogchefQL suggestions:', e);
    return undefined;
  }
};

/**
 * Get SQL suggestions
 */
const getSQLSuggestions = async (word, position, textBeforeCursor, range) => {
  try {
    // Check context patterns
    const isAfterFromOrJoin = /\b(FROM|JOIN)\s+\w*$/i.test(textBeforeCursor);
    const isInSelectClause = /\bSELECT\b(?!.*\bFROM\b)/i.test(textBeforeCursor);
    const isInWhereClause = /\bWHERE\b(?!.*\b(GROUP|ORDER|LIMIT)\b)/i.test(textBeforeCursor);
    const isAfterOperator = /\b(AND|OR|WHERE|=|!=|<>|>|<|>=|<=|LIKE|NOT LIKE|IN|BETWEEN)\s+\w*$/i.test(textBeforeCursor);
    
    const suggestions = [];
    
    // Table suggestions
    if (isAfterFromOrJoin) {
      suggestions.push({
        label: props.tableName,
        kind: monaco.languages.CompletionItemKind.Class,
        insertText: props.tableName,
        range,
        detail: 'Current log table',
        documentation: 'The main logs table containing all log entries',
        sortText: '0'
      });
      
      return { suggestions, incomplete: false };
    }
    
    // Field suggestions for SELECT/WHERE
    if (isInSelectClause || isInWhereClause || isAfterOperator) {
      // Add field suggestions
      if (props.availableFields && props.availableFields.length > 0) {
        props.availableFields.forEach(field => {
          suggestions.push({
            label: field.name,
            kind: monaco.languages.CompletionItemKind.Field,
            insertText: field.name,
            range,
            detail: field.type,
            documentation: field.isTimestamp ? 'Timestamp field' :
                          field.isSeverity ? 'Severity field' : `Type: ${field.type}`,
            sortText: field.isTimestamp ? '0' : field.isSeverity ? '1' : '2'
          });
        });
      } else {
        // Default * suggestion
        suggestions.push({
          label: '*',
          kind: monaco.languages.CompletionItemKind.Field,
          insertText: '*',
          range,
          detail: 'All columns',
          documentation: 'Select all columns',
          sortText: '0'
        });
      }
      
      // Add SQL keywords
      const sqlKeywords = [
        'SELECT', 'FROM', 'WHERE', 'GROUP BY', 'ORDER BY', 'LIMIT',
        'HAVING', 'JOIN', 'LEFT JOIN', 'RIGHT JOIN', 'INNER JOIN',
        'UNION', 'DISTINCT', 'COUNT', 'SUM', 'AVG', 'MIN', 'MAX',
        'AND', 'OR', 'NOT', 'IN', 'BETWEEN', 'LIKE', 'IS NULL', 'IS NOT NULL'
      ];
      
      // Only add keywords in appropriate contexts
      if (!isAfterOperator) {
        sqlKeywords.forEach(keyword => {
          suggestions.push({
            label: keyword,
            kind: monaco.languages.CompletionItemKind.Keyword,
            insertText: keyword,
            range,
            sortText: '9' + keyword // Sort after fields
          });
        });
      }
      
      return { suggestions, incomplete: false };
    }
    
    // Default suggestions for empty context
    if (!isInSelectClause && !isInWhereClause && !isAfterFromOrJoin) {
      const basicKeywords = [
        'SELECT', 'FROM', 'WHERE', 'GROUP BY', 'ORDER BY', 'LIMIT'
      ];
      
      basicKeywords.forEach(keyword => {
        suggestions.push({
          label: keyword,
          kind: monaco.languages.CompletionItemKind.Keyword,
          insertText: keyword + ' ',
          range,
          sortText: keyword
        });
      });
      
      // Add common query templates
      suggestions.push({
        label: 'SELECT * FROM table',
        kind: monaco.languages.CompletionItemKind.Snippet,
        insertText: `SELECT * FROM ${props.tableName}`,
        range,
        detail: 'Basic query template',
        sortText: '0template'
      });
      
      return { suggestions, incomplete: false };
    }
    
    return { suggestions: [], incomplete: false };
  } catch (e) {
    console.error('Error getting SQL suggestions:', e);
    return { suggestions: [], incomplete: false };
  }
};

// Handle mode switching
watch(activeMode, (newMode, oldMode) => {
  if (newMode === oldMode) return;

  if (newMode === 'sql') {
    // Generate SQL from LogchefQL
    try {
      if (code.value.trim()) {
        // Store the current LogchefQL query before switching to SQL
        const logchefqlQuery = code.value;
        
        // Generate SQL from LogchefQL
        sqlCode.value = translateLogchefQLToSQL(logchefqlQuery, {
          table: props.tableName,
          includeTimeRange: false
        });
        
        // Update editor with SQL
        code.value = sqlCode.value;
      } else {
        code.value = getDefaultSQLQuery(props.tableName);
      }
    } catch (error) {
      console.error('Error translating to SQL:', error);
      code.value = getDefaultSQLQuery(props.tableName);
    }
  } else {
    // When switching back to LogchefQL
    // Store current SQL code for later
    sqlCode.value = code.value;
    
    // Restore original LogchefQL code
    code.value = props.modelValue || '';
  }

  emit('modeChange', newMode);

  // Update editor model language
  nextTick(() => {
    emit('change', code.value);
    
    if (editorRef.value) {
      const model = editorRef.value.getModel();
      if (model) {
        monaco.editor.setModelLanguage(model, currentLanguage.value);
        
        // Wait a bit before registering completion providers to ensure
        // the language change has taken effect
        setTimeout(() => {
          // Register new providers for the current language
          completionProviders.value = registerCompletionProviders();
          
          // Position cursor at the end
          const lastLine = model.getLineCount();
          const lastColumn = model.getLineMaxColumn(lastLine);
          editorRef.value.setPosition({ lineNumber: lastLine, column: lastColumn });
          editorRef.value.focus();
        }, 100);
      }
    }
  });
});

// Store providers for disposal
let logchefProvider = null;
let sqlProvider = null;

// Register completion providers
function registerCompletionProviders() {
  // Store references to providers so we can dispose them later
  const providers = [];
  
  // Dispose existing providers if they exist
  if (logchefProvider) {
    try {
      logchefProvider.dispose();
    } catch (e) {
      console.error('Error disposing logchefQL provider:', e);
    }
  }
  
  if (sqlProvider) {
    try {
      sqlProvider.dispose();
    } catch (e) {
      console.error('Error disposing SQL provider:', e);
    }
  }
  
  // LogchefQL completion provider - only register one provider per language
  logchefProvider = monaco.languages.registerCompletionItemProvider('logchefql', {
    triggerCharacters: ["=", "!", ">", "<", "~", " ", "."],
    provideCompletionItems: async (model, position) => {
      const word = model.getWordUntilPosition(position);
      const textBeforeCursor = model.getValueInRange({
        startLineNumber: 1,
        endLineNumber: position.lineNumber,
        startColumn: 1,
        endColumn: position.column,
      });
      
      try {
        const result = await getLogchefQLSuggestions(word, position, textBeforeCursor);
        return result;
      } catch (e) {
        console.error('Error in LogchefQL completion provider:', e);
        return undefined;
      }
    }
  });
  providers.push(logchefProvider);

  // SQL completion provider - only register one provider per language
  sqlProvider = monaco.languages.registerCompletionItemProvider('clickhouse-sql', {
    triggerCharacters: [" ", ".", "(", ","],
    provideCompletionItems: async (model, position) => {
      const word = model.getWordUntilPosition(position);
      const textBeforeCursor = model.getValueInRange({
        startLineNumber: 1,
        endLineNumber: position.lineNumber,
        startColumn: 1,
        endColumn: position.column,
      });
      
      const range = {
        startLineNumber: position.lineNumber,
        endLineNumber: position.lineNumber,
        startColumn: word.startColumn,
        endColumn: word.endColumn,
      };
      
      try {
        const result = await getSQLSuggestions(word, position, textBeforeCursor, range);
        return result;
      } catch (e) {
        console.error('Error in SQL completion provider:', e);
        return undefined;
      }
    }
  });
  providers.push(sqlProvider);
  
  // Return the providers array so they can be disposed later if needed
  return providers;
}

// Store providers for disposal
const completionProviders = ref([]);

// Handle editor mount
const handleMount = editor => {
  editorRef.value = editor;
  
  // Set placeholder and other editor options
  editor.updateOptions({ 
    placeholder: props.placeholder,
    suggest: {
      showStatusBar: false,
      showInlineDetails: true,
      filterGraceful: true,
      snippetsPreventQuickSuggestions: false,
      localityBonus: true,
      shareSuggestSelections: true,
      showIcons: true,
      maxVisibleSuggestions: 12
    }
  });
  
  // Register completion providers after a short delay to ensure
  // the editor is fully initialized
  setTimeout(() => {
    completionProviders.value = registerCompletionProviders();
  }, 100);
  
  // Add keyboard shortcuts
  editor.addAction({
    id: 'submit',
    label: 'submit',
    keybindings: [
      monaco.KeyMod.chord(monaco.KeyMod.CtrlCmd | monaco.KeyCode.Enter),
      monaco.KeyMod.chord(monaco.KeyMod.Shift | monaco.KeyCode.Enter)
    ],
    run: () => emit('submit', activeMode.value)
  });
  
  editor.addAction({
    id: 'triggerSuggest',
    label: 'triggerSuggest',
    keybindings: [monaco.KeyCode.Tab],
    run: () => editor.trigger('keyboard', 'editor.action.triggerSuggest', {})
  });
  
  // Disable browser find
  monaco.editor.addKeybindingRule({
    keybinding: monaco.KeyMod.CtrlCmd | monaco.KeyCode.KeyF,
    command: null
  });
  
  // Track focus state
  editor.onDidFocusEditorWidget(() => { editorFocused.value = true });
  editor.onDidBlurEditorWidget(() => { editorFocused.value = false });
  
  // Auto-suggest on content change
  editor.onDidChangeModelContent(() => {
    if (editorFocused.value && props.availableFields?.length > 0) {
      setTimeout(() => editor.trigger('content', 'editor.action.triggerSuggest', {}), 100);
    }
  });
  
  // Configure editor to hide empty suggestions
  editor.updateOptions({
    suggest: {
      showStatusBar: false,
      showInlineDetails: true,
      filterGraceful: true
    }
  });
  
  // Focus editor on mount
  nextTick(() => editor.focus());
};

// Handle content changes
const onChange = () => {
  emit('update:modelValue', code.value);
  emit('change', code.value);
};

// Watch for model value changes
watch(() => props.modelValue, (newValue) => {
  if (newValue !== code.value && activeMode.value === 'dsl') {
    code.value = newValue;
  }
});

// Expose methods to parent component
defineExpose({
  editorRef,
  registerCompletionProviders: () => {
    // Register new providers
    completionProviders.value = registerCompletionProviders();
    
    // Trigger suggestions if editor is available
    if (editorRef.value) {
      setTimeout(() => {
        editorRef.value.trigger('keyboard', 'editor.action.triggerSuggest', {});
      }, 100);
    }
  },
  setMode: (mode) => {
    activeMode.value = mode;
  },
  getMode: () => activeMode.value
});
</script>

<style scoped>
/* Clean, minimal editor styling */
:deep(.margin) {
  display: none !important;
}

:deep(.monaco-editor .placeholder) {
  color: hsl(var(--muted-foreground)) !important;
}

:deep(.monaco-editor-background),
:deep(.monaco-editor) .margin,
:deep(.monaco-editor) .overflow-guard {
  background-color: transparent !important;
}
</style>
