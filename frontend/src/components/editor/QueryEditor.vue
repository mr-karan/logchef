<template>
  <div>
    <!-- Language toggle -->
    <div class="flex justify-end mb-2">
      <div class="flex items-center space-x-2 p-0.5 rounded-md bg-muted dark:bg-muted text-sm">
        <button class="px-2 py-1 rounded-md transition-colors"
          :class="{ 'bg-background dark:bg-background shadow-sm': activeMode === 'dsl' }" @click="setMode('dsl')">
          LogchefQL
        </button>
        <button class="px-2 py-1 rounded-md transition-colors"
          :class="{ 'bg-background dark:bg-background shadow-sm': activeMode === 'sql' }" @click="setMode('sql')">
          SQL
        </button>
      </div>
    </div>

    <!-- Editor container -->
    <div :style="{ height: `${editorHeight}px` }" class="editor"
      :class="{ 'border-sky-800 dark:border-sky-700': editorFocused, 'border-destructive dark:border-destructive': error }">
      <vue-monaco-editor v-model:value="currentCode" :theme="currentTheme" :language="currentLanguage"
        :options="editorOptions" @mount="handleMount" @change="onChange" />
    </div>

    <!-- Error display -->
    <div v-if="error" class="text-destructive text-xs mt-1">{{ error }}</div>
  </div>
</template>

<script setup>
import { ref, computed, shallowRef, nextTick, watch } from 'vue'
import * as monaco from "monaco-editor"
import { useDark } from '@vueuse/core'
import { VueMonacoEditor } from '@guolao/vue-monaco-editor'
import { getDefaultMonacoOptions } from '@/utils/monaco'
import { translateLogchefQLToSQL, parseLogchefQL } from '@/utils/logchefql/api'
import { isNumeric } from "@/utils/utils"
import {
  Parser as LogchefqlParser,
  tokenTypes as logchefqlTokenTypes,
} from "@/utils/logchefql"
import {
  Parser as ClickhouseSQLParser,
  tokenTypes as clickhouseSQLTokenTypes,
  SQL_KEYWORDS,
  SQL_TYPES,
  CLICKHOUSE_FUNCTIONS,
} from "@/utils/clickhouse-sql"

// Define emits and props
const emit = defineEmits(['update:modelValue', 'change', 'submit', 'modeChange'])
const props = defineProps({
  modelValue: {
    type: String,
    default: ''
  },
  placeholder: {
    type: String,
    default: 'Enter LogchefQL query (e.g. service_name=\'api\' and severity_text=\'error\')'
  },
  availableFields: {
    type: Array,
    default: () => []
  },
  height: {
    type: String,
    default: '40'
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
const dslCode = ref(props.modelValue)
const sqlCode = ref('')
const activeMode = ref('dsl')
const editorRef = shallowRef()

// Calculate editor options
const editorOptions = computed(() => {
  const options = getDefaultMonacoOptions()
  return {
    ...options,
    readOnly: false,
    placeholder: activeMode.value === 'dsl'
      ? props.placeholder
      : 'SQL query will be generated when switching to SQL mode'
  }
})

// Current language based on active mode
const currentLanguage = computed(() => {
  return activeMode.value === 'dsl' ? 'logchefql' : 'clickhouse-sql'
})

// Set theme based on dark mode and active mode
const currentTheme = computed(() => {
  if (props.theme) {
    return props.theme
  }
  return isDark.value ? 'logchef-dark' : 'logchef'
})

// Current code based on active mode
const currentCode = computed({
  get: () => (activeMode.value === 'dsl' ? dslCode.value : sqlCode.value),
  set: (val) => {
    if (activeMode.value === 'dsl') {
      dslCode.value = val
    } else {
      sqlCode.value = val
    }
    // Emit change event to parent component
    emit('change', val)
  }
})

// Compute editor height based on content
const editorHeight = computed(() => {
  const content = activeMode.value === 'dsl' ? dslCode.value : sqlCode.value
  const lines = (content.match(/\n/g) || []).length + 1
  return Math.max(40, 14 + (lines * 20)) // Minimum height of 40px
})

/**
 * Get the default SQL query to use when no query is provided
 * Simple version that will have time range and limit added automatically
 */
function getDefaultSQLQuery(tableName = 'logs.vector_logs') {
  return `SELECT * FROM ${tableName}`;
}

// Switch between DSL and SQL modes
function setMode(mode) {
  if (mode === activeMode.value) return

  if (mode === 'sql') {
    // When switching to SQL mode, generate SQL from DSL
    try {
      if (dslCode.value.trim()) {
        console.log('Translating LogchefQL to SQL:', dslCode.value);
        
        try {
          // Try to parse using the LogchefQL parser directly to better diagnose issues
          const { sql, params } = parseLogchefQL(dslCode.value);
          console.log('Raw parsed SQL components:', { sql, params });
          
          // Convert params to strings for logging
          const paramsStr = params.map(p => typeof p === 'string' ? `'${p}'` : p).join(', ');
          console.log(`SQL with params: ${sql.replace(/\?/g, () => `{param}`)}, params: [${paramsStr}]`);
        } catch (e) {
          console.error('Direct parse error:', e);
        }
        
        // For display, just show the WHERE conditions without time range and limits
        sqlCode.value = translateLogchefQLToSQL(dslCode.value, {
          table: props.tableName,
          includeTimeRange: false // Don't include time range for display
        });
        
        console.log('Translated SQL for display:', sqlCode.value);
      } else {
        sqlCode.value = getDefaultSQLQuery(props.tableName);
      }
    } catch (error) {
      console.error('Error translating to SQL:', error);
      sqlCode.value = getDefaultSQLQuery(props.tableName);
    }
  }

  activeMode.value = mode
  emit('modeChange', mode)
  
  // Emit change event with the current code after mode change
  nextTick(() => {
    // Emit the current code based on the new mode
    emit('change', activeMode.value === 'dsl' ? dslCode.value : sqlCode.value)
    
    // Focus editor after mode change
    if (editorRef.value) {
      editorRef.value.focus()
    }
  })
}

// Helper to get suggestions from a list
const getSuggestionsFromList = (params) => {
  const suggestions = []
  let defaultPostfix = (params.postfix === undefined) ? '' : params.postfix

  const range = (params.range === undefined) ? {
    startLineNumber: params.position.lineNumber,
    endLineNumber: params.position.lineNumber,
    startColumn: params.position.column,
    endColumn: params.position.column,
  } : params.range

  for (const item of params.items) {
    let label = null
    let sortText = null
    let postfix = defaultPostfix
    if (typeof item === 'string' || item instanceof String) {
      label = item
      sortText = label
    } else {
      label = item.label
      sortText = (item.sortText === undefined) ? label : item.sortText
      postfix = (item.postfix === undefined) ? defaultPostfix : item.postfix
    }
    let insertText = (item.insertText === undefined) ? label : item.insertText
    suggestions.push({
      label: label,
      kind: params.kind,
      range: range,
      sortText: sortText,
      insertText: insertText + postfix,
      command: {
        id: 'editor.action.triggerSuggest',
      }
    })
  }
  return suggestions
}

// Get operator suggestions
const getOperatorSuggestions = (field, position) => {
  let operators = [
    { label: '=', sortText: 'a' },
    { label: '!=', sortText: 'b' },
    { label: '~=', sortText: 'c' },
    { label: '!~', sortText: 'd' },
    { label: '>', sortText: 'e' },
    { label: '<', sortText: 'f' },
    { label: '>=', sortText: 'g' },
    { label: '<=', sortText: 'h' },
  ]

  return getSuggestionsFromList({
    position: position,
    items: operators,
    kind: monaco.languages.CompletionItemKind.Operator
  })
}

// Get boolean operator suggestions
const getBooleanOperatorSuggestions = (range) => {
  return getSuggestionsFromList({
    range: range,
    items: ['and', 'or'],
    kind: monaco.languages.CompletionItemKind.Keyword,
    postfix: ' '
  })
}

// Get field suggestions
const getFieldSuggestions = (range) => {
  const availableFieldNames = props.availableFields.map(field => field.name)

  return getSuggestionsFromList({
    range: range,
    items: availableFieldNames,
    kind: monaco.languages.CompletionItemKind.Field,
    postfix: ''
  })
}

// Prepare suggestion values with proper quoting
const prepareSuggestionValues = (items, quoteChar) => {
  const quoted = (quoteChar === undefined) ? false : true
  const defaultQuoteChar = (quoteChar === undefined) ? '"' : quoteChar
  const result = []

  for (const item of items) {
    if (isNumeric(item)) {
      result.push({ label: item })
    } else {
      let insertText = ''
      if (!quoted) {
        insertText = defaultQuoteChar
      }

      // Escape quotes in value if needed
      let displayValue = item
      let escapedValue = item
      if (typeof item === 'string') {
        escapedValue = item.replace(new RegExp(defaultQuoteChar, 'g'), `\\${defaultQuoteChar}`)
      }

      insertText += escapedValue
      insertText += defaultQuoteChar

      result.push({
        label: displayValue,
        insertText: insertText
      })
    }
  }

  return result
}

// Get value suggestions based on field
const getValueSuggestions = (fieldName, range) => {
  // Common values for certain fields
  const commonValues = {
    'service_name': ['api', 'frontend', 'worker', 'scheduler', 'auth'],
    'severity': ['error', 'warning', 'info', 'debug'],
    'severity_text': ['error', 'warning', 'info', 'debug'],
    'level': ['error', 'warning', 'info', 'debug'],
    'body': ['error', 'exception', 'failed', 'success', 'started', 'completed'],
  }

  // Find field type if possible
  const field = props.availableFields.find(f => f.name === fieldName)

  // Use common values if available
  const values = commonValues[fieldName] || []

  if (values.length > 0) {
    return getSuggestionsFromList({
      range: range,
      items: prepareSuggestionValues(values),
      kind: monaco.languages.CompletionItemKind.Value,
      postfix: ' '
    })
  }

  // Special handling for timestamp fields
  if (field?.isTimestamp || fieldName.includes('time') || fieldName.includes('date')) {
    return getSuggestionsFromList({
      range: range,
      items: ['now()', 'today()'],
      kind: monaco.languages.CompletionItemKind.Function,
      postfix: ' '
    })
  }

  // Generic value suggestions based on field type
  if (field) {
    if (field.type === 'string') {
      return getSuggestionsFromList({
        range: range,
        items: ['""'],
        kind: monaco.languages.CompletionItemKind.Value,
        postfix: ' '
      })
    } else if (field.type === 'number' || field.type === 'int' || field.type === 'float') {
      return getSuggestionsFromList({
        range: range,
        items: ['0', '1', '100'],
        kind: monaco.languages.CompletionItemKind.Value,
        postfix: ' '
      })
    } else if (field.type === 'boolean') {
      return getSuggestionsFromList({
        range: range,
        items: ['true', 'false'],
        kind: monaco.languages.CompletionItemKind.Value,
        postfix: ' '
      })
    }
  }

  // Default to empty quoted string
  return getSuggestionsFromList({
    range: range,
    items: prepareSuggestionValues(['']),
    kind: monaco.languages.CompletionItemKind.Value,
    postfix: ' '
  })
}

// Analyze query for completions - for LogchefQL mode
const analyzeQueryForCompletions = (text, cursorPosition) => {
  const result = {
    state: 'field', // Default to field suggestion state
    fieldName: '',
    needsOperator: false,
    needsValue: false,
    needsBooleanOperator: false
  }

  // Very basic logic to determine context
  try {
    // Find the most recent token by space
    const tokens = text.substring(0, cursorPosition).split(/\s+/)
    const lastToken = tokens[tokens.length - 1]

    // Check if we're looking at a field name
    const isFullFieldName = props.availableFields.some(field => field.name === lastToken)

    if (isFullFieldName) {
      result.state = 'operator'
      result.fieldName = lastToken
      result.needsOperator = true
    }

    // Check if we're after an operator
    const operatorRegex = /[=!<>~]+$/
    if (operatorRegex.test(text.substring(0, cursorPosition))) {
      result.state = 'value'
      result.needsValue = true

      // Try to extract the field name
      const fieldMatch = text.substring(0, cursorPosition).match(/(\w+)\s*[=!<>~]+$/)
      if (fieldMatch && fieldMatch[1]) {
        result.fieldName = fieldMatch[1]
      }
    }

    // Check if we need a boolean operator
    const valueRegex = /["'][^"']*["']\s*$/
    if (valueRegex.test(text.substring(0, cursorPosition))) {
      result.state = 'booleanOperator'
      result.needsBooleanOperator = true
    }
  } catch (e) {
    // Silently continue with default suggestions
  }

  return result
}

// Get suggestions based on editor state
const getSuggestions = async (word, position, textBeforeCursor) => {
  let incomplete = false
  let suggestions = []

  // Define suggestion range
  const range = {
    startLineNumber: position.lineNumber,
    endLineNumber: position.lineNumber,
    startColumn: word.startColumn,
    endColumn: word.endColumn,
  }

  try {
    if (activeMode.value === 'dsl') {
      // Analyze query to determine context for LogchefQL
      const analysis = analyzeQueryForCompletions(textBeforeCursor, position.column - 1)

      // Provide appropriate suggestions based on context
      if (analysis.needsOperator) {
        suggestions = getOperatorSuggestions(analysis.fieldName, position)
      } else if (analysis.needsValue) {
        suggestions = getValueSuggestions(analysis.fieldName, range)
      } else if (analysis.needsBooleanOperator) {
        suggestions = getBooleanOperatorSuggestions(range)
      } else {
        // Default to field suggestions
        suggestions = getFieldSuggestions(range)
      }
    } else if (activeMode.value === 'sql') {
      // For SQL mode, provide custom suggestions based on context
      suggestions = getSQLSuggestions(word, position, textBeforeCursor, range)
    }
  } catch (e) {
    console.error('Error getting suggestions:', e)
    // Always fall back to field suggestions on any error in DSL mode
    if (activeMode.value === 'dsl') {
      suggestions = getFieldSuggestions(range)
    }
  }

  return {
    suggestions: suggestions,
    incomplete: incomplete,
  }
}

// Get SQL suggestions based on context
const getSQLSuggestions = (word, position, textBeforeCursor, range) => {
  // Check if we're after a FROM or JOIN keyword to suggest table names
  const isAfterFromOrJoin = /\b(FROM|JOIN)\s+\w*$/.test(textBeforeCursor)
  
  // Check if we're after a table name and dot to suggest column names
  const isAfterTableDot = /\b(\w+)\.\w*$/.test(textBeforeCursor)
  
  // Check if we're in a SELECT clause to suggest column names
  const isInSelectClause = /\bSELECT\b(?!.*\bFROM\b).*$/.test(textBeforeCursor)
  
  // Check if we're in a WHERE clause to suggest column names
  const isInWhereClause = /\bWHERE\b(?!.*\b(GROUP|ORDER|LIMIT)\b).*$/.test(textBeforeCursor)
  
  // Check if we're in a GROUP BY, ORDER BY, or HAVING clause
  const isInGroupByClause = /\bGROUP\s+BY\b(?!.*\b(ORDER|LIMIT)\b).*$/.test(textBeforeCursor)
  const isInOrderByClause = /\bORDER\s+BY\b(?!.*\bLIMIT\b).*$/.test(textBeforeCursor)
  const isInHavingClause = /\bHAVING\b(?!.*\b(ORDER|LIMIT)\b).*$/.test(textBeforeCursor)
  
  // Common SQL keywords and ClickHouse functions
  let suggestions = [
    ...SQL_KEYWORDS.map(keyword => ({
      label: keyword,
      kind: monaco.languages.CompletionItemKind.Keyword,
      insertText: keyword,
      range: range,
    })),
    ...SQL_TYPES.map(type => ({
      label: type,
      kind: monaco.languages.CompletionItemKind.TypeParameter,
      insertText: type,
      range: range,
    })),
    ...CLICKHOUSE_FUNCTIONS.map(func => ({
      label: func,
      kind: monaco.languages.CompletionItemKind.Function,
      insertText: func,
      range: range,
    })),
  ]
  
  // Add table name suggestion if after FROM or JOIN
  if (isAfterFromOrJoin) {
    // Replace all suggestions with table suggestions when after FROM/JOIN
    suggestions = [
      {
        label: props.tableName,
        kind: monaco.languages.CompletionItemKind.Class,
        insertText: props.tableName,
        range: range,
        detail: 'Current log table',
        documentation: 'The main logs table containing all log entries',
        sortText: '0', // Sort to top
      },
      {
        label: `${props.tableName} AS logs`,
        kind: monaco.languages.CompletionItemKind.Class,
        insertText: `${props.tableName} AS logs`,
        range: range,
        detail: 'Table with alias',
        documentation: 'Use "logs" as an alias for better readability',
        sortText: '1',
      }
    ]
  }
  
  // Add column name suggestions for various clauses that need column names
  if (isAfterTableDot || isInSelectClause || isInWhereClause || 
      isInGroupByClause || isInOrderByClause || isInHavingClause) {
    
    // Extract table alias if present (for table.column syntax)
    let tableAlias = null
    if (isAfterTableDot) {
      const tableMatch = textBeforeCursor.match(/(\w+)\.\w*$/)
      if (tableMatch && tableMatch[1]) {
        tableAlias = tableMatch[1]
      }
    }
    
    const columnSuggestions = []
    
    props.availableFields.forEach(field => {
      // Basic field suggestion
      columnSuggestions.push({
        label: field.name,
        kind: monaco.languages.CompletionItemKind.Field,
        insertText: field.name,
        range: range,
        detail: field.type,
        documentation: field.isTimestamp ? 'Timestamp field' : 
                      field.isSeverity ? 'Severity field' : 
                      `Type: ${field.type}`,
        sortText: field.isTimestamp ? '0' : 
                  field.isSeverity ? '1' : '2', // Sort special fields to top
      })
      
      // For SELECT clause, also add common aggregations and functions for numeric fields
      if (isInSelectClause && ['int', 'float', 'number'].includes(field.type?.toLowerCase())) {
        columnSuggestions.push({
          label: `count(${field.name})`,
          kind: monaco.languages.CompletionItemKind.Function,
          insertText: `count(${field.name})`,
          range: range,
          detail: 'Count function',
          documentation: `Count occurrences of ${field.name}`,
          sortText: '3',
        })
        
        columnSuggestions.push({
          label: `avg(${field.name})`,
          kind: monaco.languages.CompletionItemKind.Function,
          insertText: `avg(${field.name})`,
          range: range,
          detail: 'Average function',
          documentation: `Calculate average of ${field.name}`,
          sortText: '3',
        })
        
        columnSuggestions.push({
          label: `sum(${field.name})`,
          kind: monaco.languages.CompletionItemKind.Function,
          insertText: `sum(${field.name})`,
          range: range,
          detail: 'Sum function',
          documentation: `Calculate sum of ${field.name}`,
          sortText: '3',
        })
      }
      
      // For timestamp fields, add date/time functions
      if (field.isTimestamp || field.name.includes('time') || field.name.includes('date')) {
        columnSuggestions.push({
          label: `toStartOfHour(${field.name})`,
          kind: monaco.languages.CompletionItemKind.Function,
          insertText: `toStartOfHour(${field.name})`,
          range: range,
          detail: 'Time function',
          documentation: 'Round timestamp to start of hour',
          sortText: '3',
        })
        
        columnSuggestions.push({
          label: `toStartOfDay(${field.name})`,
          kind: monaco.languages.CompletionItemKind.Function,
          insertText: `toStartOfDay(${field.name})`,
          range: range,
          detail: 'Time function',
          documentation: 'Round timestamp to start of day',
          sortText: '3',
        })
      }
    })
    
    // If we're in a clause that needs column names, add column suggestions
    if (isInSelectClause || isInWhereClause || isInGroupByClause || 
        isInOrderByClause || isInHavingClause) {
      suggestions = [...columnSuggestions, ...suggestions]
    } 
    // If we're after a table dot, only show column suggestions
    else if (isAfterTableDot) {
      suggestions = columnSuggestions
    }
  }
  
  return suggestions
}

/**
 * Register the logchefql language
 */
function registerLogchefql() {
  // Only register if it doesn't already exist
  if (!monaco.languages.getLanguages().some(lang => lang.id === 'logchefql')) {
    monaco.languages.register({ id: "logchefql" });

    monaco.languages.setLanguageConfiguration("logchefql", {
      autoClosingPairs: [
        { open: "(", close: ")" },
        { open: '"', close: '"' },
        { open: "'", close: "'" },
      ],
    });

    // Register semantic tokens provider
    monaco.languages.registerDocumentSemanticTokensProvider("logchefql", {
      getLegend: () => ({
        tokenTypes: logchefqlTokenTypes,
        tokenModifiers: []
      }),
      provideDocumentSemanticTokens: (model) => {
        try {
          const parser = new LogchefqlParser();
          parser.parse(model.getValue(), false);

          const data = parser.generateMonacoTokens();

          return {
            data: new Uint32Array(data),
            resultId: null,
          };
        } catch (e) {
          console.error("Error parsing LogchefQL:", e);
          return {
            data: new Uint32Array([]),
            resultId: null,
          };
        }
      },
      releaseDocumentSemanticTokens: () => { }
    });

    // Register completion provider for LogchefQL
    monaco.languages.registerCompletionItemProvider('logchefql', {
      provideCompletionItems: async (model, position) => {
        let word = model.getWordUntilPosition(position)
        const textBeforeCursorRange = {
          startLineNumber: 1,
          endLineNumber: position.lineNumber,
          startColumn: 1,
          endColumn: position.column,
        }
        const textBeforeCursor = model.getValueInRange(textBeforeCursorRange)
        return await getSuggestions(word, position, textBeforeCursor)
      },
      triggerCharacters: ["=", "!", ">", "<", "~", " "],
    });
  }
}

/**
 * Register the clickhouse-sql language
 */
function registerClickhouseSQL() {
  // Only register if it doesn't already exist
  if (!monaco.languages.getLanguages().some(lang => lang.id === 'clickhouse-sql')) {
    // Register clickhouse-sql as a language that extends the built-in SQL language
    monaco.languages.register({
      id: "clickhouse-sql",
      extensions: ['.sql'],
      aliases: ['ClickHouse SQL', 'Clickhouse SQL', 'clickhouse'],
      mimetypes: ['application/sql', 'text/x-sql'],
    });

    // Configure language features
    monaco.languages.setLanguageConfiguration("clickhouse-sql", {
      autoClosingPairs: [
        { open: "(", close: ")" },
        { open: '"', close: '"' },
        { open: "'", close: "'" },
        { open: "[", close: "]" },
        { open: "{", close: "}" },
      ],
      brackets: [
        ["{", "}"],
        ["[", "]"],
        ["(", ")"],
      ],
      comments: {
        lineComment: "--",
        blockComment: ["/*", "*/"],
      },
      wordPattern: /(-?\d*\.\d\w*)|([^\`\~\!\@\#\%\^\&\*\(\)\-\=\+\[\{\]\}\\\|\;\:\'\"\,\.\<\>\/\?\s]+)/g,
    });

    // Register tokenizer for clickhouse-sql - fallback to our parser if needed
    monaco.languages.registerDocumentSemanticTokensProvider("clickhouse-sql", {
      getLegend: () => ({
        tokenTypes: clickhouseSQLTokenTypes,
        tokenModifiers: []
      }),
      provideDocumentSemanticTokens: (model) => {
        try {
          const parser = new ClickhouseSQLParser();
          parser.parse(model.getValue(), false);

          const data = parser.generateMonacoTokens();

          return {
            data: new Uint32Array(data),
            resultId: null,
          };
        } catch (e) {
          console.error("Error parsing ClickHouse SQL:", e);
          return {
            data: new Uint32Array([]),
            resultId: null,
          };
        }
      },
      releaseDocumentSemanticTokens: () => { }
    });
    
    // Register completion provider for SQL
    monaco.languages.registerCompletionItemProvider('clickhouse-sql', {
      provideCompletionItems: async (model, position) => {
        let word = model.getWordUntilPosition(position)
        const textBeforeCursorRange = {
          startLineNumber: 1,
          endLineNumber: position.lineNumber,
          startColumn: 1,
          endColumn: position.column,
        }
        const textBeforeCursor = model.getValueInRange(textBeforeCursorRange)
        return await getSuggestions(word, position, textBeforeCursor)
      },
      triggerCharacters: [" ", ".", "(", ","],
    });
  }
}

// Handle editor mount
const handleMount = editor => {
  editorRef.value = editor

  // Register both languages in QueryEditor component
  registerLogchefql();
  registerClickhouseSQL();

  // Configure editor
  editor.updateOptions({
    placeholder: props.placeholder,
  })

  // Add keyboard shortcut for submitting query
  editor.addAction({
    id: 'submit',
    label: 'submit',
    keybindings: [
      monaco.KeyMod.chord(
        monaco.KeyMod.CtrlCmd | monaco.KeyCode.Enter,
      ),
      monaco.KeyMod.chord(
        monaco.KeyMod.Shift | monaco.KeyCode.Enter,
      )
    ],
    run: () => {
      emit('submit', activeMode.value)
    }
  })

  // Add Tab trigger for suggestions
  editor.addAction({
    id: 'triggerSugggest',
    label: 'triggerSuggest',
    keybindings: [
      monaco.KeyCode.Tab
    ],
    run: () => {
      editor.trigger('triggerSuggest', 'editor.action.triggerSuggest', {})
    }
  })

  // Disable browser find
  monaco.editor.addKeybindingRule({
    keybinding: monaco.KeyMod.CtrlCmd | monaco.KeyCode.KeyF,
    command: null
  })

  // Track editor focus state
  editor.onDidFocusEditorWidget(() => {
    editorFocused.value = true
  })

  editor.onDidBlurEditorWidget(() => {
    editorFocused.value = false
  })

  // Focus editor on mount
  nextTick(() => {
    editor.focus()
  })
}

// Change handler
const onChange = () => {
  // Update model value only when in DSL mode
  if (activeMode.value === 'dsl') {
    emit('update:modelValue', dslCode.value)
    emit('change', dslCode.value)
  } else {
    // For SQL mode, we just emit the change event but don't update modelValue
    emit('change', sqlCode.value)
  }
}

// Update code when modelValue changes
watch(() => props.modelValue, (newValue) => {
  if (newValue !== dslCode.value) {
    dslCode.value = newValue

    // If in SQL mode, regenerate SQL
    if (activeMode.value === 'sql') {
      try {
        if (dslCode.value.trim()) {
          sqlCode.value = translateLogchefQLToSQL(dslCode.value, {
            table: props.tableName
          })
        } else {
          sqlCode.value = getDefaultSQLQuery(props.tableName)
        }
      } catch (error) {
        console.error('Error translating to SQL:', error)
      }
    }
  }
})
</script>

<style scoped>
.editor {
  border: 1px solid #e2e8f0;
  border-radius: 0.375rem;
  overflow: hidden;
}

:deep(.monaco-editor) {
  background-color: transparent !important;
}

:deep(.monaco-editor .overflow-guard) {
  background-color: transparent !important;
}

:deep(.monaco-editor-background) {
  background-color: transparent !important;
}

:deep(.monaco-editor .lines-content) {
  background-color: transparent !important;
}

:deep(.monaco-editor .view-line) {
  background-color: transparent !important;
}

/* Hide scrollbars */
:deep(.monaco-scrollable-element) {
  overflow: hidden !important;
}

/* Hide line numbers */
:deep(.margin) {
  display: none !important;
}

/* Fix for cursor positioning */
:deep(.cursor) {
  height: 20px !important;
}

/* Fix for content padding */
:deep(.monaco-editor .lines-content) {
  padding-top: 2px !important;
  padding-bottom: 2px !important;
}
</style>
