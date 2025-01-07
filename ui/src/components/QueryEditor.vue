<template>
  <div class="query-container flex items-center gap-3 p-2">
    <ToggleButton v-model="isSqlMode" onIcon="pi pi-database" offIcon="pi pi-code" onLabel="SQL" offLabel="LogchefQL"
      class="flex-none" />

    <div class="editor-wrapper flex-1">
      <vue-monaco-editor ref="monacoEditor" :value="modelValue" :language="currentLanguage" :options="editorOptions"
        @update:value="handleChange" @mount="handleEditorMount" />
      <span class="placeholder" v-if="!modelValue">Type your LogchefQL query here...</span>
    </div>
  </div>
</template>

<script setup lang="ts">
import { ref, computed } from 'vue'
import { VueMonacoEditor } from '@guolao/vue-monaco-editor'
import ToggleButton from 'primevue/togglebutton'
import { configureLogchefQLLanguage } from '../config/languages/logchefql'

const props = defineProps<{
  modelValue: string
  loading?: boolean
}>()

const emit = defineEmits<{
  (e: 'update:modelValue', value: string): void
  (e: 'search'): void
}>()

const isSqlMode = ref(false)
const monacoEditor = ref(null)

const currentLanguage = computed(() => isSqlMode.value ? 'sql' : 'logchefql')

const editorOptions = {
  minimap: { enabled: false },
  lineNumbers: 'off',
  lineDecorationsWidth: 0,
  folding: false,
  wordWrap: 'off',
  scrollBeyondLastLine: false,
  scrollbar: {
    vertical: 'hidden',
    horizontal: 'hidden',
    useShadows: false,
    alwaysConsumeMouseWheel: false
  },
  overviewRulerBorder: false,
  hideCursorInOverviewRuler: true,
  lineHeight: 32,
  fontSize: 14,
  padding: { top: 8, bottom: 8 },
  fontFamily: 'Monaco, Menlo, "Ubuntu Mono", Consolas, source-code-pro, monospace',
  renderLineHighlight: 'none',
  contextmenu: false,
  automaticLayout: true,
  fixedOverflowWidgets: true,
  renderWhitespace: 'none',
  quickSuggestions: {
    other: true,
    comments: false,
    strings: true
  },
  suggestOnTriggerCharacters: true,
  parameterHints: {
    enabled: true,
    cycle: true
  },
  glyphMargin: false,
  renderIndentGuides: false,
  occurrencesHighlight: false,
  selectionHighlight: false,
  links: false,
  cursorStyle: 'line',
  cursorWidth: 1,
  cursorBlinking: 'solid',
  suggest: {
    showWords: false,
    preview: true,
    showIcons: true,
    maxVisibleSuggestions: 12,
    insertMode: 'insert'
  }
}

const handleEditorMount = (editor: any, monaco: any) => {
  editor.addCommand(monaco.KeyMod.CtrlCmd | monaco.KeyCode.Enter, () => {
    emit('search')
  })
  editor.focus()

  // Configure LogchefQL language support
  configureLogchefQLLanguage(monaco)

  // Disable multiple lines
  editor.onKeyDown((e: any) => {
    if (e.keyCode === monaco.KeyCode.Enter && !e.ctrlKey && !e.metaKey) {
      e.preventDefault()
    }
  })

  // Register SQL completions if in SQL mode
  if (isSqlMode.value) {
    monaco.languages.registerCompletionItemProvider('sql', {
      provideCompletionItems: () => ({
        suggestions: [
          {
            label: 'SELECT',
            kind: monaco.languages.CompletionItemKind.Keyword,
            insertText: 'SELECT',
            detail: 'SQL SELECT keyword'
          },
          {
            label: 'FROM',
            kind: monaco.languages.CompletionItemKind.Keyword,
            insertText: 'FROM',
            detail: 'SQL FROM keyword'
          },
          {
            label: 'WHERE',
            kind: monaco.languages.CompletionItemKind.Keyword,
            insertText: 'WHERE',
            detail: 'SQL WHERE clause'
          },
          {
            label: 'GROUP BY',
            kind: monaco.languages.CompletionItemKind.Keyword,
            insertText: 'GROUP BY',
            detail: 'SQL GROUP BY clause'
          },
          {
            label: 'ORDER BY',
            kind: monaco.languages.CompletionItemKind.Keyword,
            insertText: 'ORDER BY',
            detail: 'SQL ORDER BY clause'
          },
          {
            label: 'LIMIT',
            kind: monaco.languages.CompletionItemKind.Keyword,
            insertText: 'LIMIT',
            detail: 'SQL LIMIT clause'
          }
        ]
      })
    })
  }
}

const handleChange = (value: string) => {
  // Remove any newlines to keep it single line
  const singleLine = value.replace(/[\r\n]+/g, ' ')
  emit('update:modelValue', singleLine)
}
</script>

<style>
.query-container {
  background: #ffffff;
  border: 1px solid #e2e8f0;
  border-radius: 6px;
  height: 52px;
}

.editor-wrapper {
  background: #f8fafc;
  border: 1px solid #e2e8f0;
  border-radius: 4px;
  height: 40px;
  transition: all 0.2s ease;
  position: relative;
  box-shadow: rgba(0, 0, 0, 0.05) 0px 1px 2px, rgba(0, 0, 0, 0.05) 0px 0px 0px 1px;
}

.editor-wrapper:hover {
  border-color: #cbd5e1;
  box-shadow: rgba(0, 0, 0, 0.1) 0px 1px 3px, rgba(0, 0, 0, 0.06) 0px 1px 2px;
}

.editor-wrapper:focus-within {
  border-color: #3b82f6;
  box-shadow: rgb(59 130 246 / 0.1) 0px 0px 0px 3px;
}

.placeholder {
  position: absolute;
  left: 8px;
  top: 50%;
  transform: translateY(-50%);
  color: #94a3b8;
  pointer-events: none;
  font-size: 14px;
  opacity: 0.8;
}

:deep(.monaco-editor) {
  border-radius: 4px;
  height: 40px !important;
}

:deep(.monaco-editor .overflow-guard) {
  height: 40px !important;
}

:deep(.monaco-editor .cursor) {
  width: 1px !important;
  height: 18px !important;
  margin-top: 2px;
}

:deep(.monaco-editor .suggest-widget) {
  border-radius: 6px;
  box-shadow: rgba(0, 0, 0, 0.1) 0px 4px 6px -1px, rgba(0, 0, 0, 0.06) 0px 2px 4px -1px;
  margin-top: 4px;
  border: 1px solid #e2e8f0;
  background: #ffffff !important;
}

:deep(.monaco-editor .parameter-hints-widget) {
  border-radius: 6px;
  margin-top: 4px;
  border: 1px solid #e2e8f0;
  background: #ffffff !important;
}

:deep(.monaco-editor .suggest-widget .monaco-list .monaco-list-row.focused) {
  background-color: #eff6ff !important;
}

:deep(.monaco-editor .suggest-widget .monaco-list .monaco-list-row:hover) {
  background-color: #f8fafc !important;
}
</style>