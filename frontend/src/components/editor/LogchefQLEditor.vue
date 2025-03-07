<template>
  <div :style="{ height: `${editorHeight}px` }" class="editor"
    :class="{ 'border-sky-800 dark:border-sky-700': editorFocused, 'border-destructive dark:border-destructive': error }">
    <vue-monaco-editor v-model:value="code" :theme="theme" language="logchefql" :options="getDefaultMonacoOptions()"
      @mount="handleMount" @change="onChange" />
  </div>
  <div v-if="error" class="text-destructive text-xs mt-1">{{ error }}</div>
</template>

<script setup>
import { ref, computed, shallowRef, nextTick, watch } from 'vue'
import * as monaco from "monaco-editor"
import { useDark } from '@vueuse/core'
import { VueMonacoEditor } from '@guolao/vue-monaco-editor'
import { getDefaultMonacoOptions } from '@/utils/monaco'
import { isNumeric } from "@/utils/utils"

// Emit events only for changes and submit
const emit = defineEmits(['update:modelValue', 'change', 'submit'])
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
  }
})

const isDark = useDark()
const editorFocused = ref(false)
const code = ref(props.modelValue)
const editorRef = shallowRef()

// Compute editor height based on content
const editorHeight = computed(() => {
  const lines = (code.value.match(/\n/g) || []).length + 1
  return 14 + (lines * 20)
})

// Set theme based on dark mode or props
const theme = computed(() => {
  if (props.theme) {
    return props.theme
  }
  return isDark.value ? 'logchef-dark' : 'logchef'
})

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

// Get field name suggestions
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

// Analyze query for completions - simplified to prevent errors
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
    // Analyze query to determine context
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
  } catch (e) {
    // Always fall back to field suggestions on any error
    suggestions = getFieldSuggestions(range)
  }

  return {
    suggestions: suggestions,
    incomplete: incomplete,
  }
}

// Handle editor mount
const handleMount = editor => {
  // Register completion provider
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
  })

  editorRef.value = editor

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
      emit('submit')
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

  // Show suggestion details by default
  editor.getContribution("editor.contrib.suggestController").widget.value._setDetailsVisible(true);
}

// Simple change handler that doesn't do any parsing
const onChange = () => {
  emit('update:modelValue', code.value)
  emit('change', code.value)
}

// Update code when modelValue changes
watch(() => props.modelValue, (newValue) => {
  if (newValue !== code.value) {
    code.value = newValue
  }
})
</script>

<style scoped>
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