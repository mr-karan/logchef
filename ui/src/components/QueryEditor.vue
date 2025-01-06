<script setup lang="ts">
import { ref, onMounted, watch } from 'vue'
import { VueMonacoEditor } from '@guolao/vue-monaco-editor'
import type * as Monaco from 'monaco-editor'
import Select from 'primevue/select'
import Button from 'primevue/button'
import { configureSQLLanguage, SQL_KEYWORDS } from '@/config/languages'

type QueryMode = 'sql' | 'logql'

interface Props {
    modelValue?: string
    mode?: QueryMode
    loading?: boolean
}

const props = withDefaults(defineProps<Props>(), {
    modelValue: '',
    mode: 'sql',
    loading: false
})

const emit = defineEmits<{
    (e: 'update:modelValue', value: string): void
    (e: 'update:mode', value: QueryMode): void
    (e: 'search'): void
    (e: 'clear'): void
}>()

const queryMode = ref<QueryMode>(props.mode)
const editorValue = ref(props.modelValue)

const editorOptions = ref({
    minimap: { enabled: false },
    lineNumbers: 'off',
    wordWrap: 'on',
    scrollbar: {
        vertical: 'hidden',
        horizontal: 'hidden'
    },
    lineHeight: 24,
    padding: { top: 8, bottom: 8 },
    overviewRulerLanes: 0,
    overviewRulerBorder: false,
    hideCursorInOverviewRuler: true,
    contextmenu: true,
    fontSize: 13,
    renderLineHighlight: 'none',
    folding: false,
    glyphMargin: false,
    lineDecorationsWidth: 0,
    lineNumbersMinChars: 0,
    renderWhitespace: 'none',
    fixedOverflowWidgets: true,
    language: props.mode === 'sql' ? 'sql' : 'plaintext',
    onDidChangeModelLanguage: (e: any) => {
        console.log('🔤 Language changed:', e)
    },
    onDidChangeModelContent: (e: any) => {
        console.log('📝 Content changed:', e)
    },
    wordBasedSuggestions: true,
    quickSuggestions: true,
    formatOnType: true,
    formatOnPaste: true,
    suggestOnTriggerCharacters: true
})

// Watch for mode changes to update language
watch(() => props.mode, (newMode) => {
    console.log('🔄 Mode changed:', newMode)
    editorOptions.value = {
        ...editorOptions.value,
        language: newMode === 'sql' ? 'sql' : 'plaintext'
    }
    console.log('Updated language:', editorOptions.value.language)
})

const handleMount = (editor: Monaco.editor.IStandaloneCodeEditor, monaco: typeof Monaco) => {
    console.log('🎨 Editor mounted')

    // Set the model language directly
    const model = editor.getModel()
    if (model) {
        monaco.editor.setModelLanguage(model, 'sql')
        console.log('🔧 Set model language to SQL')
    }

    // Register completion provider
    monaco.languages.registerCompletionItemProvider('sql', {
        provideCompletionItems: () => {
            const suggestions = SQL_KEYWORDS.map(keyword => ({
                label: keyword,
                kind: monaco.languages.CompletionItemKind.Keyword,
                insertText: keyword
            }))
            console.log('💡 Providing suggestions:', suggestions)
            return { suggestions }
        }
    })
}

const handleValueChange = (value: string) => {
    console.log('📊 Value changed:', value)
    console.log('Current language:', editorOptions.value.language)
    editorValue.value = value
    emit('update:modelValue', value)
}

const handleModeChange = (event: { value: QueryMode }) => {
    const newMode = event.value
    console.log('📝 Mode change event:', newMode)
    queryMode.value = newMode

    // Update editor language immediately
    editorOptions.value = {
        ...editorOptions.value,
        language: newMode === 'sql' ? 'sql' : 'plaintext'
    }

    emit('update:mode', newMode)
}

const handleClear = () => {
    editorValue.value = ''
    emit('clear')
}
</script>

<template>
    <div class="flex items-center gap-4">
        <Select v-model="queryMode" :options="[
            { label: 'LogQL', value: 'logql' },
            { label: 'SQL', value: 'sql' }
        ]" optionLabel="label" optionValue="value" class="w-32" @change="handleModeChange" />

        <div class="flex-1 border rounded-md bg-white overflow-hidden" style="height: 40px">
            <VueMonacoEditor v-model:value="editorValue" :options="editorOptions" theme="vs" height="40px"
                @mount="handleMount" @change="handleValueChange">
                <template #default>
                    <div class="p-2 text-gray-400">Loading editor...</div>
                </template>
            </VueMonacoEditor>
        </div>

        <div class="flex items-center space-x-2">
            <Button icon="pi pi-search" label="Search" @click="$emit('search')" :loading="loading" />
            <Button v-if="modelValue" icon="pi pi-times" text @click="handleClear" />
        </div>
    </div>
</template>

<style>
.monaco-editor .inputarea {
    border: none !important;
    background: transparent !important;
    padding: 0 !important;
    outline: none !important;
    resize: none !important;
}

.monaco-editor {
    border-radius: 4px;
    padding: 4px 8px !important;
}

.monaco-editor .overflow-guard {
    border-radius: 4px;
}

.monaco-editor .cursors-layer>.cursor {
    width: 1px !important;
}

.monaco-editor .margin,
.monaco-editor .lines-decorations {
    display: none !important;
}
</style>