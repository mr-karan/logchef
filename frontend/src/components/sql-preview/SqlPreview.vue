<script setup lang="ts">
import { computed, ref, watch, onMounted, onBeforeUnmount, nextTick } from 'vue';
import { Copy, Check } from 'lucide-vue-next';
import { Button } from '@/components/ui/button';
import { useToast } from '@/components/ui/toast';
import { TOAST_DURATION } from '@/lib/constants';
import * as monaco from 'monaco-editor';
import { VueMonacoEditor } from '@guolao/vue-monaco-editor';
import { useColorMode } from '@vueuse/core';
import { initMonacoSetup } from '@/utils/monaco';

const props = defineProps<{
    sql: string;
    debounce?: number;
    is_valid?: boolean;
    error?: string;
}>();

// Force our own internal SQL content to prevent stale data
const internalSql = ref('');
const monacoInstance = ref<monaco.editor.IStandaloneCodeEditor | null>(null);

onMounted(() => {
    console.log("SqlPreview component mounting", { id: Date.now().toString() });
    // Monaco is already initialized in main.ts
    internalSql.value = props.sql;
});

// Track the filters that were used to generate the current SQL
const previousFilterData = ref({
    hasNamespace: false,
    hasBody: false,
    hasRaw: false
});

// Watch for changes in the SQL and update immediately
watch(() => props.sql, (newSql) => {
    // Update our internal SQL directly without debounce - for live updates
    internalSql.value = newSql;
    
    // Force refresh Monaco editor if mounted
    if (monacoInstance.value) {
        // Use nextTick to ensure Vue has updated the DOM
        nextTick(() => {
            monacoInstance.value?.layout();
        });
    }
}, { immediate: true });

// Get theme from app-wide color mode
const colorMode = useColorMode();
const theme = computed(() => colorMode.value === 'dark' ? 'logchef-dark' : 'logchef-light');

// Monaco editor options - read-only configuration with minimal features
// to prevent performance issues
const editorOptions = computed(() => {
    return {
        readOnly: true,
        minimap: { enabled: false },
        lineNumbers: "off",
        glyphMargin: false,
        folding: false,
        lineDecorationsWidth: 0,
        lineNumbersMinChars: 0, 
        scrollBeyondLastLine: false,
        renderLineHighlight: "none",
        hideCursorInOverviewRuler: true,
        overviewRulerBorder: false,
        overviewRulerLanes: 0,
        wordWrap: "on",
        renderWhitespace: "none",
        renderControlCharacters: false,
        renderIndentGuides: false,
        
        // Disable features we don't need for read-only preview
        quickSuggestions: false,
        quickSuggestionsDelay: 1000000, // Effectively disable
        parameterHints: { enabled: false },
        suggestOnTriggerCharacters: false,
        acceptSuggestionOnEnter: "off",
        tabCompletion: "off",
        wordBasedSuggestions: "off",
        selectionHighlight: false,
        occurrencesHighlight: false,
        
        // Scrollbar config for better performance
        scrollbar: {
            vertical: "auto",
            horizontal: "auto",
            verticalScrollbarSize: 6,
            horizontalScrollbarSize: 6,
            alwaysConsumeMouseWheel: false
        },
        
        fontFamily: "ui-monospace, SFMono-Regular, Menlo, Monaco, Consolas, monospace",
        fontSize: 13,
        lineHeight: 18,
        automaticLayout: true,
        
        // Disable interactive features
        contextmenu: false,
        links: false,
        hover: false, // Disable hover widgets
        colorDecorators: false, // Disable color decorators
        
        // Disable editor services we don't need
        find: {
            addExtraSpaceOnTop: false,
            autoFindInSelection: "never",
            seedSearchStringFromSelection: "never"
        }
    };
});

// Better SQL formatter with nested WHERE conditions
const displaySql = computed(() => {
    // First clean up extra whitespace
    let cleanSql = internalSql.value.trim().replace(/\s+/g, ' ');
    
    // Extract the WHERE clause if it exists
    const whereMatch = cleanSql.match(/WHERE\s+(.*?)(?=\s+(?:ORDER BY|GROUP BY|HAVING|LIMIT|$))/i);
    
    if (whereMatch && whereMatch[1]) {
        const whereConditions = whereMatch[1];
        
        // Format each AND condition on a new line with indentation
        const formattedWhere = whereConditions.replace(/\s+AND\s+/gi, '\n  AND ');
        
        // Replace the original WHERE clause
        cleanSql = cleanSql.replace(whereMatch[0], `WHERE ${formattedWhere}`);
    }
    
    // Add main clause formatting
    cleanSql = cleanSql
        .replace(/\s*SELECT\s+/i, 'SELECT ')
        .replace(/\s*FROM\s+/i, '\nFROM ')
        .replace(/\s*WHERE\s+/i, '\nWHERE ')
        .replace(/\s*ORDER BY\s+/i, '\nORDER BY ')
        .replace(/\s*GROUP BY\s+/i, '\nGROUP BY ')
        .replace(/\s*HAVING\s+/i, '\nHAVING ')
        .replace(/\s*LIMIT\s+/i, '\nLIMIT ');
    
    return cleanSql;
});

// Mount handler - just store the reference
const handleEditorMount = (editor: monaco.editor.IStandaloneCodeEditor) => {
    // Only log in development
    if (process.env.NODE_ENV !== 'production') {
        console.log("SqlPreview editor mounted with ID:", editor.getId());
    }
    
    // Store the editor reference
    monacoInstance.value = editor;
}

// Minimal cleanup - just release references
onBeforeUnmount(() => {
    // Only log in development
    if (process.env.NODE_ENV !== 'production') {
        console.log("SqlPreview component unmounting");
    }
    
    // Clear content reference
    internalSql.value = '';
    
    // Clear editor reference - but DO NOT try to dispose it
    // Monaco will handle the cleanup itself when needed
    monacoInstance.value = null;
});

// Copy functionality
const { toast } = useToast();
const isCopied = ref(false);

async function copyToClipboard() {
    try {
        await navigator.clipboard.writeText(internalSql.value);
        isCopied.value = true;
        toast({
            title: 'Copied',
            description: 'SQL query copied to clipboard',
            duration: TOAST_DURATION.SUCCESS,
        });
        // Reset copy state after 2 seconds
        setTimeout(() => {
            isCopied.value = false;
        }, 2000);
    } catch (error) {
        toast({
            title: 'Error',
            description: 'Failed to copy to clipboard',
            variant: 'destructive',
            duration: TOAST_DURATION.ERROR,
        });
    }
}
</script>

<template>
    <div class="relative">
        <!-- Copy button -->
        <div class="absolute right-2 top-2 z-10">
            <Button variant="secondary" size="sm" @click.stop.prevent="copyToClipboard" :disabled="isCopied"
                class="h-6 px-2 shadow-sm transition-all duration-200 hover:shadow active:scale-95 cursor-pointer"
                :class="{
                    'bg-green-500/10 text-green-600 hover:bg-green-500/20': isCopied,
                    'hover:bg-muted': !isCopied
                }">
                <Check v-if="isCopied" class="h-3 w-3 mr-1 transition-transform duration-200 animate-in zoom-in" />
                <Copy v-else class="h-3 w-3 mr-1" />
                {{ isCopied ? 'Copied!' : 'Copy' }}
            </Button>
        </div>
        
        <!-- SQL with Monaco syntax highlighting -->
        <div class="text-sm p-3 pr-[85px] pt-10 bg-muted rounded-md mt-2 overflow-hidden max-h-[300px] relative">
            <div class="absolute left-3 top-2 text-[10px] uppercase font-sans text-muted-foreground tracking-wider">Generated Query</div>
            <!-- Monaco editor in read-only mode for consistent syntax highlighting -->
            <div class="h-[250px] w-full">
                <VueMonacoEditor
                    v-model:value="displaySql"
                    :theme="theme"
                    language="clickhouse-sql"
                    :options="editorOptions"
                    @mount="handleEditorMount"
                    class="w-full h-full"
                />
            </div>
        </div>
    </div>
</template>

<style>
/* Monaco editor customizations for the preview - scoped to this component */
:deep(.vue-monaco-editor) {
    border-radius: 4px;
    overflow: hidden;
}

/* Hide cursor in read-only mode */
:deep(.monaco-editor .cursor) {
    display: none !important;
}

/* Customize Monaco's appearance to better match our UI */
:deep(.monaco-editor),
:deep(.monaco-editor .monaco-editor-background) {
    background-color: transparent !important;
}

:deep(.monaco-editor .margin),
:deep(.monaco-editor .inputarea.ime-input) {
    background-color: transparent !important;
}

/* Remove borders that disrupt cohesive look */
:deep(.monaco-editor .overflow-guard) {
    border: none !important;
}

/* Animations for the copy button */
.animate-in {
    animation: animate-in 0.2s ease-out;
}

.zoom-in {
    transform-origin: center;
    animation: zoom-in 0.2s ease-out;
}

@keyframes animate-in {
    from {
        opacity: 0;
        transform: translateY(1px);
    }
    to {
        opacity: 1;
        transform: translateY(0);
    }
}

@keyframes zoom-in {
    from {
        opacity: 0;
        transform: scale(0.95);
    }
    to {
        opacity: 1;
        transform: scale(1);
    }
}
</style>
