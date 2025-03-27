<template>
  <div class="query-editor">
    <!-- Header Bar -->
    <div class="flex items-center justify-between bg-muted/40 rounded-t-md px-3 py-1.5 border border-b-0">
      <div class="flex items-center gap-3">
        <!-- Fields Panel Toggle -->
        <button
          class="p-1 text-muted-foreground hover:text-foreground flex items-center"
          @click="$emit('toggle-fields')"
          :title="props.showFieldsPanel ? 'Hide fields panel' : 'Show fields panel'"
          aria-label="Toggle fields panel"
        >
          <PanelRightClose v-if="props.showFieldsPanel" class="h-4 w-4" />
          <PanelRightOpen v-else class="h-4 w-4" />
        </button>

        <!-- Tabs for Mode Switching -->
        <Tabs :model-value="activeMode" @update:model-value="handleTabChange" class="w-auto">
          <TabsList class="h-8">
            <TabsTrigger value="logchefql">LogchefQL</TabsTrigger>
            <TabsTrigger value="clickhouse-sql">SQL</TabsTrigger>
          </TabsList>
        </Tabs>
      </div>

      <div class="flex items-center gap-2">
        <!-- Table name indicator (SQL mode) -->
        <div v-if="activeMode === 'clickhouse-sql'" class="text-xs text-muted-foreground mr-2">
          <template v-if="props.tableName">
            <span class="mr-1">Table:</span>
            <code class="bg-muted px-1.5 py-0.5 rounded text-xs">{{ props.tableName }}</code>
          </template>
          <span v-else class="italic text-orange-500">No table selected</span>
        </div>

        <!-- Help Icon -->
        <HoverCard :open-delay="200">
          <HoverCardTrigger as-child>
            <button class="p-1 text-muted-foreground hover:text-foreground" aria-label="Show syntax help">
              <HelpCircle class="h-4 w-4" />
            </button>
          </HoverCardTrigger>
          <HoverCardContent class="w-80 backdrop-blur-md bg-card text-card-foreground border-border shadow-lg" side="bottom" align="end">
             <!-- Help Content (Keep existing template) -->
             <div class="space-y-2">
              <h4 class="text-sm font-semibold">{{ activeMode === 'logchefql' ? 'LogchefQL' : 'SQL' }} Syntax</h4>
              <div v-if="activeMode === 'logchefql'" class="text-xs space-y-1.5">
                 <!-- LogchefQL Help -->
                 <div><code class="bg-muted px-1 rounded">field="value"</code> - Exact match</div>
                 <div><code class="bg-muted px-1 rounded">field!="value"</code> - Not equal</div>
                 <div><code class="bg-muted px-1 rounded">field~"pattern"</code> - Regex match</div>
                 <div><code class="bg-muted px-1 rounded">field!~"pattern"</code> - Regex exclusion</div>
                 <div><code class="bg-muted px-1 rounded">field>100</code> - Comparison</div>
                 <div><code class="bg-muted px-1 rounded">(c1 and c2) or c3</code> - Grouping</div>
                 <div class="pt-1"><em>Example: <code class="bg-muted px-1 rounded">level="error" and status>=500</code></em></div>
              </div>
              <div v-else class="text-xs space-y-1.5">
                 <!-- SQL Help -->
                 <div><code class="bg-muted px-1 rounded">SELECT count() FROM {{ tableName || 'table' }}</code></div>
                 <div><code class="bg-muted px-1 rounded">WHERE field = 'value' AND time > now() - interval 1 hour</code></div>
                 <div><code class="bg-muted px-1 rounded">GROUP BY user ORDER BY count() DESC</code></div>
                 <div class="pt-1"><em>Time range & limit applied if not specified. Use standard ClickHouse SQL.</em></div>
              </div>
            </div>
          </HoverCardContent>
        </HoverCard>
      </div>
    </div>

    <!-- Monaco Editor -->
    <div
      :style="{ height: `${editorHeight}px` }"
      class="editor-container border rounded-b-md overflow-hidden"
      :class="{ 'ring-1 ring-primary/50 border-primary/50': editorFocused }"
    >
      <vue-monaco-editor
        v-model:value="editorContent"
        :theme="theme"
        :language="activeMode"
        :options="monacoOptions"
        @mount="handleMount"
        @update:value="handleEditorChange"
        class="h-full w-full"
      />
    </div>

    <!-- Error Message Display -->
    <div v-if="validationError" class="mt-2 p-2 text-sm text-destructive bg-destructive/10 rounded flex items-center gap-2">
      <AlertCircle class="h-4 w-4 flex-shrink-0" />
      <span>{{ validationError }}</span>
    </div>
  </div>
</template>

<script setup lang="ts">
import { ref, computed, shallowRef, watch, onMounted, onBeforeUnmount, nextTick, reactive } from "vue";
import * as monaco from "monaco-editor";
import { useDark } from "@vueuse/core";
import { VueMonacoEditor } from "@guolao/vue-monaco-editor";
import { HelpCircle, PanelRightOpen, PanelRightClose, AlertCircle } from "lucide-vue-next";
import { HoverCard, HoverCardContent, HoverCardTrigger } from '@/components/ui/hover-card';
import { Tabs, TabsList, TabsTrigger } from '@/components/ui/tabs';

import { initMonacoSetup, getDefaultMonacoOptions } from "@/utils/monaco";
import { Parser as LogchefQLParser, State as LogchefQLState, Operator as LogchefQLOperator, VALID_KEY_VALUE_OPERATORS as LogchefQLValidOperators, isNumeric } from "@/utils/logchefql";
import { parseAndTranslateLogchefQL, validateLogchefQL } from "@/utils/logchefql/api";
import { validateSQL, SQL_KEYWORDS, CLICKHOUSE_FUNCTIONS, SQL_TYPES } from "@/utils/clickhouse-sql"; // Assuming these exist and are correct
import { SQLParser } from '@/utils/clickhouse-sql/ast'; // Assuming this exists
import { useExploreStore } from '@/stores/explore';
import { QueryBuilder } from "@/utils/query-builder";

// --- Types ---
type EditorMode = "logchefql" | "clickhouse-sql";
type MonacoCompletionItem = monaco.languages.CompletionItem; // Alias for clarity
type MonacoPosition = monaco.Position;
type MonacoRange = monaco.IRange;
type EditorChangeEvent = { query: string; mode: EditorMode };

// --- Props and Emits ---
const props = defineProps({
  sourceId: { type: Number, required: true },
  schema: { type: Object as () => Record<string, { type: string }>, required: true }, // More specific type
  startTimestamp: { type: Number, required: true },
  endTimestamp: { type: Number, required: true },
  initialValue: { type: String, default: "" },
  initialTab: { type: String as () => 'logchefql' | 'sql', default: "logchefql" },
  placeholder: { type: String, default: "" },
  tsField: { type: String, default: "timestamp" },
  tableName: { type: String, required: true },
  limit: { type: Number, default: 1000 },
  showFieldsPanel: { type: Boolean, default: false }
});

const emit = defineEmits<{
  (e: "change", value: EditorChangeEvent): void;
  (e: "submit", value: EditorChangeEvent): void;
  (e: "toggle-fields"): void;
}>();

// --- State ---
const isDark = useDark();
const exploreStore = useExploreStore();
const editorRef = shallowRef<monaco.editor.IStandaloneCodeEditor | null>(null);
const editorModel = shallowRef<monaco.editor.ITextModel | null>(null);
const editorFocused = ref(false);
const validationError = ref<string | null>(null);
const fieldNames = computed(() => Object.keys(props.schema ?? {}));
const activeProviders = ref<monaco.IDisposable[]>([]); // Track completion providers

// Centralized state for editor content managed via computed property
const editorContent = ref(props.initialValue ?? "");

// Reactive state for options to allow dynamic updates
const monacoOptions = reactive(getDefaultMonacoOptions());

// Determine initial mode based on prop and store, prioritizing prop
const initialMode: EditorMode = props.initialTab === 'sql' ? 'clickhouse-sql' : 'logchefql';
const activeMode = ref<EditorMode>(initialMode);

// Flag to prevent feedback loops during programmatic updates
const isProgrammaticChange = ref(false);

// Monaco-specific types for better type checking
type MonacoCompletionItem = {
  label: string;
  kind: monaco.languages.CompletionItemKind;
  insertText: string;
  range: any;
  sortText: string;
  detail?: string;
  documentation?: string;
  command?: { id: string; title: string; snippet?: string };
};

// --- Mode Switching ---
const handleTabChange = (newMode: EditorMode) => {
  if (newMode === activeMode.value) return;

  // 1. Persist current editor content to the store
  const currentQuery = editorContent.value ?? "";
  if (activeMode.value === 'logchefql') {
    exploreStore.setLogchefqlCode(currentQuery);
  } else {
    exploreStore.setRawSql(currentQuery);
  }

  // 2. Update active mode
  const previousMode = activeMode.value;
  activeMode.value = newMode;
  exploreStore.setActiveMode(newMode === 'clickhouse-sql' ? 'sql' : 'logchefql');

  // 3. Determine content for the new mode
  let newContent = "";
  if (newMode === 'clickhouse-sql') {
    // Switching TO SQL: Try generating from LogchefQL first
    const logchefqlToConvert = exploreStore.logchefqlCode;
    let conversionSuccess = false;
    if (logchefqlToConvert && validateLogchefQL(logchefqlToConvert)) {
        const result = QueryBuilder.buildSqlFromLogchefQL(logchefqlToConvert, {
            tableName: props.tableName,
            tsField: props.tsField,
            startTimestamp: props.startTimestamp,
            endTimestamp: props.endTimestamp,
            limit: props.limit,
            includeTimeFilter: true, // Include time filter when generating
            forDisplay: true
        });
        if (result.success) {
            newContent = result.sql;
            conversionSuccess = true;
        } else {
            console.warn("Failed to convert LogchefQL to SQL:", result.error);
            validationError.value = `Error converting query: ${result.error}`;
        }
    }

    // If conversion failed or no LogchefQL, use stored SQL or generate default
    if (!conversionSuccess) {
        newContent = exploreStore.rawSql ?? QueryBuilder.getDefaultSQLQuery({
            tableName: props.tableName,
            tsField: props.tsField,
            startTimestamp: props.startTimestamp,
            endTimestamp: props.endTimestamp,
            limit: props.limit,
            includeTimeFilter: true, // Include time filter in default
            forDisplay: true
        });
    }
    exploreStore.setRawSql(newContent); // Update store with the SQL we decided on

  } else {
    // Switching TO LogchefQL: Use stored value or empty
    newContent = exploreStore.logchefqlCode ?? "";
  }

  // 4. Update editor content programmatically
  runProgrammaticUpdate(newContent);

  // 5. Update Monaco language and options
  if (editorRef.value) {
    const model = editorRef.value.getModel();
    if (model) {
      monaco.editor.setModelLanguage(model, newMode);
    }
    updateMonacoOptions(); // Apply mode-specific options
  }

  // 6. Re-register completion provider for the new mode
  registerCompletionProvider();

  // 7. Clear validation error from previous mode
  validationError.value = null;

  // 8. Emit change for URL update etc.
  emit("change", { query: newContent, mode: newMode });
};

const getOperatorsSuggestions = (field: string, position: any) => {
  // Create operators as any[] type to avoid string[] conversion issues
  const operators: any[] = [
    { label: Operator.EQUALS, sortText: "a" },
    { label: Operator.NOT_EQUALS, sortText: "b" },
    { label: Operator.GREATER_THAN, sortText: "e" },
    { label: Operator.GREATER_OR_EQUALS_THAN, sortText: "f" },
    { label: Operator.LOWER_THAN, sortText: "g" },
    { label: Operator.LOWER_OR_EQUALS_THAN, sortText: "h" },
  ];

  // Only add regex operators for string fields
  const fieldType = props.schema[field]?.type || "";
  if (fieldType.includes("String") || fieldType.includes("string")) {
    operators.push(
      { label: Operator.EQUALS_REGEX, sortText: "c" },
      { label: Operator.NOT_EQUALS_REGEX, sortText: "d" }
    );
  }

  return getSuggestionsFromList({
    position: position,
    items: operators,
    kind: monaco.languages.CompletionItemKind.Operator,
  });
};

const getBooleanOperatorsSuggestions = (range: any) => {
  // Create suggestions with very distinct IDs to avoid duplicates
  return [
    {
      label: "and",
      kind: monaco.languages.CompletionItemKind.Keyword,
      insertText: "and ",
      range: range,
      sortText: "1and"
    },
    {
      label: "or",
      kind: monaco.languages.CompletionItemKind.Keyword,
      insertText: "or ",
      range: range,
      sortText: "2or"
    }
  ];
};

const getKeySuggestions = (range: any) => {
  const suggestions: MonacoCompletionItem[] = [];
  for (const name of fieldNames.value) {
    const fieldInfo = props.schema[name];
    if (fieldInfo) {
      let documentation = `Field (${fieldInfo.type})`;
      suggestions.push({
        label: name,
        kind: monaco.languages.CompletionItemKind.Field,
        range: range,
        insertText: name,
        documentation: documentation,
        sortText: name.toLowerCase(), // Add missing sortText property
        command: {
          id: "editor.action.triggerSuggest",
          title: "Trigger Suggest"
        },
      });
    }
  }
  return suggestions;
};

const prepareSuggestionValues = (items: string[], quoteChar?: string) => {
  const quoted = quoteChar !== undefined;
  const defaultQuoteChar = quoteChar === undefined ? '"' : quoteChar;
  const result = [];

  for (const item of items) {
    if (isNumeric(item)) {
      result.push({ label: item });
    } else {
      let insertText = "";
      if (!quoted) {
        insertText = defaultQuoteChar;
      }

      // Handle escaping of quotes in the value
      for (const char of item) {
        if (char === defaultQuoteChar) {
          insertText += `\\${defaultQuoteChar}`;
        } else {
          insertText += char;
        }
      }

      if (!quoted) {
        insertText += defaultQuoteChar;
      }

      result.push({ label: item, insertText });
    }
  }

  return result;
};

// Sample values for auto-completion (in real use you would get these from your backend)
const getSchemaFieldValues = (field: string): string[] => {
  // This would normally come from props or an API call
  const sampleValues: Record<string, string[]> = {
    level: ["info", "error", "warning", "debug"],
    service: ["api", "frontend", "database", "auth", "cache"],
    status: ["200", "404", "500", "403", "401"]
  };

  return sampleValues[field] || [];
};

const getValueSuggestions = async (key: string, value: string, range: any, quoteChar?: string) => {
  const result = {
    suggestions: [] as MonacoCompletionItem[],
    incomplete: false,
  };

  if (fieldNames.value.includes(key)) {
    // Get sample values for the field
    const fieldValues = getSchemaFieldValues(key);

    // Filter values based on current input
    const filteredValues = fieldValues.filter(val =>
      val.toLowerCase().includes(value.toLowerCase())
    );

    // Create suggestions with proper typing
    const items: any[] = prepareSuggestionValues(filteredValues, quoteChar);

    result.suggestions = getSuggestionsFromList({
      range: range,
      items: items,
      kind: monaco.languages.CompletionItemKind.Value,
      postfix: " ",
    });
  }

  return result;
};

// Track editor initialization state
const isEditorInitialized = ref(false);

// Add a safe dispose function to handle all editor resources
function safelyDisposeEditor() {
  if (isDisposing.value) return; // Prevent concurrent dispose operations

  isDisposing.value = true;
  console.log('QueryEditor: Starting safe disposal process');

  try {
    // First, stop any ongoing provider operations
    disposeAllProviders();

    // Next, dispose all tracked disposables
    disposeArray.value.forEach(disposable => {
      try {
        if (disposable && typeof disposable.dispose === 'function') {
          disposable.dispose();
        }
      } catch (e) {
        console.warn('Error disposing resource:', e);
      }
    });
    disposeArray.value = [];

    // Get references before nulling them
    const currentEditor = editorRef.value;
    const currentModel = editorModel.value;

    // Clear references immediately to prevent further operations
    editorRef.value = null;
    
    // Use detached Promise for model disposal to avoid blocking the UI
    if (currentModel) {
      // Mark the editor as not initialized to prevent operations during disposal
      isEditorInitialized.value = false;
      
      // First try to detach model from editor - in a try block
      if (currentEditor && typeof currentEditor.setModel === 'function') {
        try {
          console.log('QueryEditor: Detaching model from editor');
          currentEditor.setModel(null);
        } catch (err) {
          console.warn('Error detaching model from editor:', err);
        }
      }

      // Use window.setTimeout directly for more reliable cleanup
      window.setTimeout(() => {
        try {
          if (currentModel) {
            console.log('QueryEditor: Disposing model with delay');
            // Check if the model is already disposed
            if (typeof currentModel.isDisposed === 'function' && !currentModel.isDisposed()) {
              currentModel.dispose();
            } else if (!currentModel.isDisposed) {
              // Fallback for when isDisposed is not a function
              currentModel.dispose();
            }
          }
        } catch (err) {
          console.warn('Error during model disposal:', err);
        } finally {
          editorModel.value = null;
        }
      }, 150); // Use a longer delay for more reliable cleanup
    }
  } catch (e) {
    console.error('Error in safelyDisposeEditor:', e);
  } finally {
    // Use a more reliable way to reset the disposing flag
    window.setTimeout(() => {
      isDisposing.value = false;
      console.log('QueryEditor: Disposal process completed');
    }, 250);
  }
}

// Watch for changes in initialTab prop to ensure URL parameters are respected
watch(() => props.initialTab, (newTab) => {
  if (newTab) {
    setActiveTab(newTab === 'sql' ? 'clickhouse-sql' : 'logchefql');
  }
}, { immediate: true }); // immediate: true ensures it runs on component creation

// Initialize Monaco
onMounted(() => {
  initMonacoSetup();

  // No need to override exploreStore's activeMode here as it's handled by the watch
  // on props.initialTab with immediate: true
});

// --- Computed Properties ---
const theme = computed(() => (isDark.value ? "logchef-dark" : "logchef-light"));

const currentPlaceholder = computed(() => {
    if (activeMode.value === 'logchefql') {
        return props.placeholder || 'Enter LogchefQL query (e.g., level="error" and status>400)';
    } else {
        return props.placeholder || `Enter ClickHouse SQL query for table ${props.tableName || '...'}`;
    }
});

const editorHeight = computed(() => {
  const lines = (editorContent.value?.match(/\n/g) || []).length + 1;
  const lineHeight = monacoOptions.lineHeight || 21; // Use configured line height
  const padding = (monacoOptions.padding?.top ?? 8) + (monacoOptions.padding?.bottom ?? 8);
  const minHeight = activeMode.value === 'logchefql' ? 45 : 90; // Smaller min for LogchefQL
  const calculatedHeight = padding + lines * lineHeight;
  return Math.max(minHeight, calculatedHeight);
});

// --- Lifecycle Hooks ---
onMounted(() => {
  try {
    initMonacoSetup(); // Initialize themes, languages (runs once internally)

    // Set initial content based on mode and store/props
    syncEditorContentWithStore(true);

    // Set initial mode in store if it differs
    if (exploreStore.activeMode !== activeMode.value) {
      exploreStore.setActiveMode(activeMode.value === 'clickhouse-sql' ? 'sql' : 'logchefql');
    }

  } catch (error) {
    console.error("QueryEditor: Error during mount:", error);
    validationError.value = "Failed to initialize editor.";
  }
});

onBeforeUnmount(() => {
  safelyDisposeEditor();
});

// Methods
function setActiveTab(tab: string) {
  // Guard against operations during dispose
  if (isDisposing.value) return;

  // Store current code based on current tab
  if (activeTab.value === "logchefql") {
    logchefQLCode.value = code.value || '';
  } else {
    sqlCode.value = code.value || '';
  }

  // Switch tab with transition
  if (tab === "logchefql") {
    // Store current tab before changing it
    const previousTab = activeTab.value;
    activeTab.value = "logchefql";

    // Special case: switching from SQL to LogchefQL
    if (previousTab === "clickhouse-sql") {
      // Check if we have a value in the store first
      if (exploreStore.logchefqlCode) {
        code.value = exploreStore.logchefqlCode;
        logchefQLCode.value = exploreStore.logchefqlCode;
        hasManuallyEnteredLogchefQL.value = true;
      } else if (hasManuallyEnteredLogchefQL.value) {
        // Use the previously saved LogchefQL code
        code.value = logchefQLCode.value || '';
      } else {
        // Only clear if we have no stored or previous code
        logchefQLCode.value = '';
        code.value = '';
        exploreStore.setLogchefqlCode('');
      }

      // Always emit change to update URL
      emit('change', {
        query: code.value,
        mode: 'logchefql'
      });
    } else {
      // First check if we have a value in the store
      if (exploreStore.logchefqlCode) {
        code.value = exploreStore.logchefqlCode;
        logchefQLCode.value = exploreStore.logchefqlCode;
      } else {
        // Fall back to the saved local value
        code.value = logchefQLCode.value || '';
      }

      // Update store and URL
      exploreStore.setLogchefqlCode(code.value);
      emit('change', {
        query: code.value,
        mode: 'logchefql'
      });
    }

    // Update exploreStore activeMode to ensure consistency
    exploreStore.setActiveMode('logchefql');
  } else {
    activeTab.value = "clickhouse-sql";
    // Update exploreStore activeMode to ensure consistency
    exploreStore.setActiveMode('sql');

    // CHANGED: Always prioritize generating SQL from LogchefQL if available
    if (logchefQLCode.value && logchefQLCode.value.trim()) {
      try {
        // Make sure we have a table name before generating SQL
        if (!props.tableName) {
          console.warn('No table name available when converting LogchefQL to SQL');
        }
        
        // Use our QueryBuilder to generate SQL from LogchefQL WITH time filter
        const result = QueryBuilder.buildSqlFromLogchefQL(logchefQLCode.value, {
          tableName: props.tableName || '<table_name>',  // Provide a placeholder if missing
          tsField: props.tsField,
          startTimestamp: props.startTimestamp,
          endTimestamp: props.endTimestamp,
          limit: props.limit,
          includeTimeFilter: true,
          forDisplay: true
        });

        if (result.success) {
          sqlCode.value = result.sql;
          code.value = result.sql;
          exploreStore.setRawSql(result.sql);
        } else {
          console.error('Error generating SQL:', result.error);
          // Use default SQL query as fallback
          sqlCode.value = QueryBuilder.getDefaultSQLQuery({
            tableName: props.tableName,
            tsField: props.tsField,
            startTimestamp: props.startTimestamp,
            endTimestamp: props.endTimestamp,
            limit: props.limit,
            includeTimeFilter: true,
            forDisplay: true
          });
          code.value = sqlCode.value;
          exploreStore.setRawSql(sqlCode.value);
        }
      } catch (error) {
        console.error('Error switching to SQL mode:', error);
        // Use default SQL query as fallback
        sqlCode.value = QueryBuilder.getDefaultSQLQuery({
          tableName: props.tableName,
          tsField: props.tsField,
          startTimestamp: props.startTimestamp,
          endTimestamp: props.endTimestamp,
          limit: props.limit,
          includeTimeFilter: true,
          forDisplay: true
        });
        code.value = sqlCode.value;
        exploreStore.setRawSql(sqlCode.value);
      }
    } 
    // Only use stored SQL if we don't have LogchefQL to convert
    else if (exploreStore.rawSql) {
      console.log('Using existing SQL from store:', exploreStore.rawSql);
      sqlCode.value = exploreStore.rawSql;
      code.value = exploreStore.rawSql;
    } 
    else if (!sqlCode.value) {
      // If we have a pending SQL query, use that instead of generating a default
      if (exploreStore.pendingRawSql) {
        console.log('Using pending SQL query:', exploreStore.pendingRawSql);
        sqlCode.value = exploreStore.pendingRawSql;
        code.value = exploreStore.pendingRawSql;
        exploreStore.setRawSql(exploreStore.pendingRawSql);
        exploreStore.pendingRawSql = undefined;
      } else {
        // If no LogchefQL code and no SQL code, use default query
        console.log('No SQL or LogchefQL found, generating default query for table:', props.tableName || '<MISSING>');
        
        // Only generate default query if we have a valid table name
        if (props.tableName && props.tableName.trim()) {
          sqlCode.value = QueryBuilder.getDefaultSQLQuery({
            tableName: props.tableName,
            tsField: props.tsField,
            startTimestamp: props.startTimestamp,
            endTimestamp: props.endTimestamp,
            limit: props.limit,
            includeTimeFilter: true,
            forDisplay: true
          });
          code.value = sqlCode.value;
          exploreStore.setRawSql(sqlCode.value);
        } else {
          console.warn('No table name available for SQL query generation');
          // Set a placeholder to make it obvious there's a missing table name
          sqlCode.value = `-- Please select a valid source with a table name\nSELECT * FROM <table_name>\nWHERE ${props.tsField} BETWEEN '...' AND '...'\nORDER BY ${props.tsField} DESC\nLIMIT ${props.limit}`;
          code.value = sqlCode.value;
          exploreStore.setRawSql(sqlCode.value);
        }
      }
    } else {
      // Use existing SQL code
      console.log('Using existing SQL code from local state:', sqlCode.value);
      code.value = sqlCode.value;
      exploreStore.setRawSql(sqlCode.value);
    }

    // Always emit change when switching to SQL mode
    emit('change', {
      query: code.value,
      mode: 'sql'
    });
  }

  // Update editor language - guard with additional checks
  if (editorRef.value && !isDisposing.value) {
    try {
      const model = editorRef.value.getModel();
      if (model && !model.isDisposed?.()) {
        monaco.editor.setModelLanguage(model, activeTab.value);

        // Update editor options based on the new mode
        updateEditorOptions(editorRef.value);

        // Re-register appropriate completion provider based on active tab
        if (activeTab.value === "logchefql") {
          registerLogchefQLCompletionProvider();
        } else {
          registerSQLCompletionProvider();
        }
      }
    } catch (error) {
      console.error('Error updating editor language:', error);
    }
  }
}

function validateQuery(): boolean {
  if (!code.value.trim()) {
    return true; // Empty queries are allowed and will use defaults
  }

  if (activeTab.value === 'logchefql') {
    // Validate LogchefQL
    try {
      const parser = new LogchefQLParser();
      parser.parse(code.value);
      return true;
    } catch (error) {
      errorMessage.value = error instanceof Error ? error.message : String(error);
      return false;
    }
  } else {
    // Validate SQL
    const isValid = validateSQL(code.value);
    if (!isValid) {
      errorMessage.value = "Invalid SQL query. Please check the syntax.";
    }
    return isValid;
  }
}

// Track all active providers
const activeProviders: monaco.IDisposable[] = [];

// Clean function to dispose all existing providers
function disposeAllProviders() {
  try {
    while (activeProviders.length > 0) {
      const provider = activeProviders.pop();
      if (provider && typeof provider.dispose === 'function') {
        provider.dispose();
      }
    }
  } catch (e) {
    console.warn('Error in disposeAllProviders:', e);
  }
}

const registerSQLCompletionProvider = () => {
  // Dispose all existing providers before registering new ones
  disposeAllProviders();

  // Register a new provider
  const provider = monaco.languages.registerCompletionItemProvider("clickhouse-sql", {
    provideCompletionItems: async (model, position) => {
      const word = model.getWordUntilPosition(position);
      const range = {
        startLineNumber: position.lineNumber,
        endLineNumber: position.lineNumber,
        startColumn: word.startColumn,
        endColumn: word.endColumn,
      };

      // Get the line before the current position
      const textBeforeCursor = model.getValueInRange({
        startLineNumber: position.lineNumber,
        endLineNumber: position.lineNumber,
        startColumn: 1,
        endColumn: position.column,
      });

      // Check for specific SQL contexts - add debug info
      console.log('QueryEditor tableName prop:', props.tableName);

      // Improved regex for detecting being right after FROM keyword
      const isAfterFrom = /\bFROM\s+$/i.test(textBeforeCursor);
      const isInWhereClause = /\bWHERE\b/i.test(textBeforeCursor) && !/\bGROUP\s+BY\b|\bORDER\s+BY\b|\bLIMIT\b/i.test(textBeforeCursor);
      const isInSelectClause = /\bSELECT\b/i.test(textBeforeCursor) && !/\bFROM\b/i.test(textBeforeCursor);
      const isAfterJoin = /\b(JOIN|INNER\s+JOIN|LEFT\s+JOIN|RIGHT\s+JOIN|FULL\s+JOIN|OUTER\s+JOIN)\s+$/i.test(textBeforeCursor);
      const isInGroupByClause = /\bGROUP\s+BY\b/i.test(textBeforeCursor) && !/\bHAVING\b|\bORDER\s+BY\b|\bLIMIT\b/i.test(textBeforeCursor);
      const isInOrderByClause = /\bORDER\s+BY\b/i.test(textBeforeCursor) && !/\bLIMIT\b/i.test(textBeforeCursor);

      // Create suggestions array with proper typing
      let suggestions: monaco.languages.CompletionItem[] = [];

      // Only suggest relevant items based on context

      // After FROM or JOIN, suggest table name
      if (isAfterFrom || isAfterJoin) {
        // Suggest the table name from the parent component
        if (props.tableName) {
          suggestions.push({
            label: props.tableName,
            kind: monaco.languages.CompletionItemKind.Folder,
            insertText: props.tableName,
            range,
            sortText: '0-table',
            detail: 'Current log table',
            documentation: 'The current log table from the selected source'
          });
        }

        // Also suggest keywords that might follow FROM
        suggestions = suggestions.concat([
          {
            label: 'WHERE',
            kind: monaco.languages.CompletionItemKind.Keyword,
            insertText: 'WHERE ',
            range,
            sortText: '1-where',
            detail: 'Filter results',
            documentation: 'Add a WHERE clause to filter results'
          },
          {
            label: 'GROUP BY',
            kind: monaco.languages.CompletionItemKind.Keyword,
            insertText: 'GROUP BY ',
            range,
            sortText: '1-groupby',
            detail: 'Group results',
            documentation: 'Add a GROUP BY clause to aggregate results'
          },
          {
            label: 'ORDER BY',
            kind: monaco.languages.CompletionItemKind.Keyword,
            insertText: 'ORDER BY ',
            range,
            sortText: '1-orderby',
            detail: 'Sort results',
            documentation: 'Add an ORDER BY clause to sort results'
          },
          {
            label: 'LIMIT',
            kind: monaco.languages.CompletionItemKind.Keyword,
            insertText: 'LIMIT ',
            range,
            sortText: '1-limit',
            detail: 'Limit results',
            documentation: 'Add a LIMIT clause to limit the number of results returned'
          },
          {
            label: 'JOIN',
            kind: monaco.languages.CompletionItemKind.Keyword,
            insertText: 'JOIN ',
            range,
            sortText: '1-join',
            detail: 'Join tables',
            documentation: 'Add a JOIN clause to combine data from multiple tables'
          }
        ]);
      }
      // In WHERE, GROUP BY, ORDER BY, or SELECT clause, suggest fields
      else if (isInWhereClause || isInGroupByClause || isInOrderByClause || isInSelectClause) {
        // Add schema columns if available
        if (fieldNames.value.length > 0) {
          suggestions = suggestions.concat(
            fieldNames.value.map(field => ({
              label: field,
              kind: monaco.languages.CompletionItemKind.Field,
              range: range,
              insertText: field,
              sortText: `0-${field}`, // Sort columns first
              detail: props.schema[field]?.type || "unknown",
            }))
          );
        }

        // In WHERE clause, also suggest operators and functions
        if (isInWhereClause) {
          // Add operators
          suggestions = suggestions.concat([
            { label: '=', kind: monaco.languages.CompletionItemKind.Operator, insertText: ' = ', range, sortText: '1-eq' },
            { label: '!=', kind: monaco.languages.CompletionItemKind.Operator, insertText: ' != ', range, sortText: '1-neq' },
            { label: '>', kind: monaco.languages.CompletionItemKind.Operator, insertText: ' > ', range, sortText: '1-gt' },
            { label: '<', kind: monaco.languages.CompletionItemKind.Operator, insertText: ' < ', range, sortText: '1-lt' },
            { label: '>=', kind: monaco.languages.CompletionItemKind.Operator, insertText: ' >= ', range, sortText: '1-gte' },
            { label: '<=', kind: monaco.languages.CompletionItemKind.Operator, insertText: ' <= ', range, sortText: '1-lte' },
            { label: 'LIKE', kind: monaco.languages.CompletionItemKind.Operator, insertText: ' LIKE ', range, sortText: '1-like' },
            { label: 'IN', kind: monaco.languages.CompletionItemKind.Operator, insertText: ' IN ()', range, sortText: '1-in', command: { id: 'editor.action.triggerSuggest', title: 'Trigger Suggest' } },
            { label: 'BETWEEN', kind: monaco.languages.CompletionItemKind.Operator, insertText: ' BETWEEN ', range, sortText: '1-between' },
          ]);
        }

        // Add relevant functions
        suggestions = suggestions.concat(
          CLICKHOUSE_FUNCTIONS.map(func => ({
            label: func,
            kind: monaco.languages.CompletionItemKind.Function,
            insertText: func + "(",
            range,
            sortText: `2-${func}`,
            command: { id: "editor.action.triggerParameterHints", title: "Show Parameter Hints" }
          }))
        );
      }
      // Default suggestions for other contexts
      else {
        // Add common SQL keywords
        suggestions = suggestions.concat(
          SQL_KEYWORDS.map(keyword => ({
            label: keyword,
            kind: monaco.languages.CompletionItemKind.Keyword,
            insertText: keyword + ' ',
            range,
            sortText: `1-${keyword}`,
          }))
        );

        // Add functions for general context
        suggestions = suggestions.concat(
          CLICKHOUSE_FUNCTIONS.map(func => ({
            label: func,
            kind: monaco.languages.CompletionItemKind.Function,
            insertText: func + "(",
            range,
            sortText: `2-${func}`,
            command: { id: "editor.action.triggerParameterHints", title: "Show Parameter Hints" }
          }))
        );

        // Add SQL types for general context
        suggestions = suggestions.concat(
          SQL_TYPES.map(type => ({
            label: type,
            kind: monaco.languages.CompletionItemKind.Class,
            insertText: type,
            range,
            sortText: `3-${type}`,
          }))
        );
      }

      // Fix sortText for completion items
      suggestions.push({
        label: 'SELECT * FROM',
        kind: monaco.languages.CompletionItemKind.Field,
        range,
        insertText: 'SELECT * FROM ',
        documentation: 'Basic SELECT query template',
        command: {
          id: 'editor.action.insertSnippet',
          title: 'Insert Snippet',
          arguments: ['SELECT * FROM ${1:table} WHERE ${2:condition}']
        },
        sortText: '0001' // Add sortText property
      });

      // Return with proper typing
      return {
        suggestions: suggestions as monaco.languages.CompletionItem[],
        incomplete: false
      };
    },
    // Trigger on relevant characters
    triggerCharacters: [" ", "\n", ".", "(", ","],
  });

  // Add to the tracked providers
  activeProviders.push(provider);
  return provider;
};

const registerLogchefQLCompletionProvider = () => {
  // Dispose all existing providers
  disposeAllProviders();

  // Register a new provider
  const provider = monaco.languages.registerCompletionItemProvider("logchefql", {
    provideCompletionItems: async (model, position) => {
      const word = model.getWordUntilPosition(position);
      const textBeforeCursorRange = {
        startLineNumber: 1,
        endLineNumber: position.lineNumber,
        startColumn: 1,
        endColumn: position.column,
      };
      const textBeforeCursor = model.getValueInRange(textBeforeCursorRange);

      // Parse the current state
      const parser = new LogchefQLParser();
      parser.parse(textBeforeCursor, false, true);

      let suggestions: MonacoCompletionItem[] = [];
      let incomplete = false;
      const range = {
        startLineNumber: position.lineNumber,
        endLineNumber: position.lineNumber,
        startColumn: word.startColumn,
        endColumn: word.endColumn,
      };

      // Only provide suggestions for the specific state we're in
      // to prevent duplicate suggestions
      if (
        parser.state === State.KEY ||
        parser.state === State.INITIAL ||
        parser.state === State.BOOL_OP_DELIMITER
      ) {
        if (fieldNames.value.includes(word.word)) {
          suggestions = getOperatorsSuggestions(word.word, position);
        } else {
          suggestions = getKeySuggestions(range);
        }
      } else if (parser.state === State.KEY_VALUE_OPERATOR) {
        // @ts-ignore: Type compatibility issues with operator types
        if (VALID_KEY_VALUE_OPERATORS.includes(parser.keyValueOperator)) {
          range.startColumn = word.endColumn - parser.value.length;
          const result = await getValueSuggestions(parser.key, parser.value, range);
          incomplete = result.incomplete;
          suggestions = result.suggestions;
        }
      } else if (
        parser.state === State.VALUE ||
        parser.state === State.DOUBLE_QUOTED_VALUE ||
        parser.state === State.SINGLE_QUOTED_VALUE
      ) {
        range.startColumn = word.endColumn - parser.value.length;
        let quoteChar = "";
        if (parser.state === State.DOUBLE_QUOTED_VALUE) {
          quoteChar = '"';
        } else if (parser.state === State.SINGLE_QUOTED_VALUE) {
          quoteChar = "'";
        }
        const result = await getValueSuggestions(
          parser.key,
          parser.value,
          range,
          quoteChar
        );
        incomplete = result.incomplete;
        suggestions = result.suggestions;
      } else if (parser.state === State.EXPECT_BOOL_OP) {
        // Always show boolean operator suggestions when in this state
        // This state occurs after completing a filter expression like field="value"

        // Check if we're right after a completed value expression
        // We don't want to show suggestions when already typing a boolean operator
        if (word.word === '' && !textBeforeCursor.trim().endsWith('and') && !textBeforeCursor.trim().endsWith('or')) {
          suggestions = getBooleanOperatorsSuggestions(range);
        }
      }

      return {
        suggestions: suggestions as monaco.languages.CompletionItem[],
        incomplete,
      };
    },
    triggerCharacters: ["=", "!", ">", "<", "~", " ", "."],
  });

  // Add to the tracked providers
  activeProviders.push(provider);
  return provider;
};

// Editor setup with improved error handling and retries
const handleMount = (editor: any) => {
  // Guard against mount after dispose started
  if (isDisposing.value) {
    console.warn('QueryEditor: Ignoring editor mount during disposal');
    return;
  }

  console.log('QueryEditor: Monaco editor mounted');
  
  // Wait for a tick to ensure everything is ready
  nextTick(() => {
    try {
      // Store the editor reference
      editorRef.value = editor;
      
      // Store the model for proper disposal later - with better error handling
      if (editor && typeof editor.getModel === 'function') {
        try {
          const model = editor.getModel();
          if (model) {
            editorModel.value = model;
            console.log('QueryEditor: Successfully stored editor model');
          }
        } catch (err) {
          console.warn('Error getting editor model:', err);
        }
      }
  
      // Register only the completion provider for the active language
      try {
        if (activeTab.value === "logchefql") {
          registerLogchefQLCompletionProvider();
        } else {
          registerSQLCompletionProvider();
        }
      } catch (err) {
        console.warn('Error registering completion provider:', err);
      }
  
      // Set placeholder
      if (editor && typeof editor.updateOptions === 'function') {
        try {
          editor.updateOptions({
            placeholder: currentPlaceholder.value || props.placeholder || "Enter your query here..."
          });
        } catch (err) {
          console.warn('Error setting editor placeholder:', err);
        }
      }
  
      // Update editor options based on current mode
      try {
        updateEditorOptions(editor);
      } catch (err) {
        console.warn('Error updating editor options:', err);
      }
  
      // Add keyboard shortcuts with better error handling
      if (editor && typeof editor.addAction === 'function') {
        try {
          const submitAction = editor.addAction({
            id: "submit",
            label: "Run Query",
            keybindings: [
              monaco.KeyMod.CtrlCmd | monaco.KeyCode.Enter,
            ],
            run: () => {
              submitQuery();
            },
          });
          if (submitAction) disposeArray.value.push(submitAction);
        } catch (err) {
          console.warn('Error adding submit action:', err);
        }
      }
  
      // Focus handling with improved error handling
      if (editor && typeof editor.onDidFocusEditorWidget === 'function') {
        try {
          const focusListener = editor.onDidFocusEditorWidget(() => {
            editorFocused.value = true;
          });
          if (focusListener) disposeArray.value.push(focusListener);
        } catch (err) {
          console.warn('Error setting focus listener:', err);
        }
      }
  
      if (editor && typeof editor.onDidBlurEditorWidget === 'function') {
        try {
          const blurListener = editor.onDidBlurEditorWidget(() => {
            editorFocused.value = false;
          });
          if (blurListener) disposeArray.value.push(blurListener);
        } catch (err) {
          console.warn('Error setting blur listener:', err);
        }
      }
  
      // Mark initialization as complete
      isEditorInitialized.value = true;
  
      // Focus editor after everything is set up
      setTimeout(() => {
        if (editor && !isDisposing.value && typeof editor.focus === 'function') {
          try {
            editor.focus();
          } catch (err) {
            console.warn('Error focusing editor:', err);
          }
        }
      }, 50);
      
      console.log('QueryEditor: Editor initialization complete');
    } catch (error) {
      console.error('Error during editor mount:', error);
    }
  });
};

// Helper function to update editor options based on mode
function updateEditorOptions(editor: any) {
  const baseOptions = getEditorOptions();

  if (activeTab.value === 'logchefql') {
    // @ts-ignore: Type compatibility issues with editor options
    editor.updateOptions({
      ...baseOptions,
      glyphMargin: false,
      folding: false,
      lineDecorationsWidth: 0,
      lineNumbersMinChars: 0,
      wordWrap: 'off',
      padding: { top: 8, bottom: 8 },
      placeholder: currentPlaceholder.value
    });
  } else {
    // @ts-ignore: Type compatibility issues with editor options
    editor.updateOptions({
      ...baseOptions,
      folding: true,
      wordWrap: 'on',
      padding: { top: 6, bottom: 6 },
      placeholder: currentPlaceholder.value
    });
  }
}

// Watch for prop changes
watch(() => props.initialValue, (newValue) => {
  if (newValue && newValue !== code.value) {
    code.value = newValue;

    if (activeTab.value === "logchefql") {
      logchefQLCode.value = newValue;
    } else {
      sqlCode.value = newValue;
    }
  }
});

// Update the watch handler for timestamp changes to implement one-way sync
watch([() => props.startTimestamp, () => props.endTimestamp], ([newStart, newEnd], [oldStart, oldEnd]) => {
  // Only update if we're in SQL mode and the timestamps have changed
  if (activeTab.value === "clickhouse-sql" &&
    (newStart !== oldStart || newEnd !== oldEnd) &&
    sqlCode.value) {

    try {
      // Parse the current query to understand its structure
      const parsedQuery = SQLParser.parse(sqlCode.value, props.tsField);

      if (parsedQuery) {
        // Create a new timestamp condition with the updated times
        const newTimeCondition = QueryBuilder.formatTimeCondition(
          props.tsField,
          newStart * 1000,
          newEnd * 1000,
          true // Format for display
        );

        // If there's already a timestamp filter, update it
        if (parsedQuery.whereClause?.hasTimestampFilter) {
          // Replace the existing timestamp condition
          const timeFilterRegex = new RegExp(
            `${props.tsField}\\s+BETWEEN\\s+'[^']+'\\s+AND\\s+'[^']+'`,
            'i'
          );

          parsedQuery.whereClause.conditions = parsedQuery.whereClause.conditions.replace(
            timeFilterRegex,
            newTimeCondition
          );
        } else if (parsedQuery.whereClause) {
          // Add timestamp condition to existing WHERE clause
          parsedQuery.whereClause.conditions = `${newTimeCondition} AND (${parsedQuery.whereClause.conditions})`;
          parsedQuery.whereClause.hasTimestampFilter = true;
        } else {
          // Add new WHERE clause with timestamp condition
          parsedQuery.whereClause = {
            type: 'where',
            conditions: newTimeCondition,
            hasTimestampFilter: true
          };
        }

        // Convert back to SQL
        const updatedSql = SQLParser.toSQL(parsedQuery);

        // Update the state
        sqlCode.value = updatedSql;
        code.value = updatedSql;

        console.log('Updated SQL with new timestamp range:', updatedSql);
      }
    } catch (error) {
      console.error('Error updating timestamp in SQL:', error);
    }
  }
});

// Also watch for limit changes - with added safety checks
watch([() => props.limit], ([newLimit], [oldLimit]) => {
  // Skip if we're disposing or if there's no change
  if (isDisposing.value || newLimit === oldLimit) return;
  
  if (activeTab.value === "clickhouse-sql" &&
    sqlCode.value &&
    typeof newLimit === 'number') {

    try {
      // Safely update SQL with limit - wrapped in try/catch
      const updateSqlWithLimit = () => {
        try {
          // Parse the current SQL
          const parsedQuery = SQLParser.parse(sqlCode.value);

          if (parsedQuery) {
            // Apply new limit
            const updatedQuery = SQLParser.applyLimit(parsedQuery, newLimit);

            // Convert back to SQL
            sqlCode.value = SQLParser.toSQL(updatedQuery);

            // Update the code value if we're currently in SQL mode
            if (activeTab.value === "clickhouse-sql") {
              code.value = sqlCode.value;
            }
          } else {
            // Fallback to regex approach
            const limitRegex = /\bLIMIT\s+\d+/i;

            if (limitRegex.test(sqlCode.value)) {
              sqlCode.value = sqlCode.value.replace(
                limitRegex,
                `LIMIT ${newLimit}`
              );
            } else {
              // Add limit if not present
              sqlCode.value = `${sqlCode.value}\nLIMIT ${newLimit}`;
            }

            // Update the code value
            if (activeTab.value === "clickhouse-sql") {
              code.value = sqlCode.value;
            }
          }
        } catch (innerError) {
          console.error('Error in updateSqlWithLimit:', innerError);
        }
      };

      // Use microtask to ensure this runs after current stack is clear
      Promise.resolve().then(updateSqlWithLimit);
    } catch (error) {
      console.error('Error updating SQL limit with AST:', error);
    }
  }
});

// Clean up when component is unmounted - improved cleanup process
onBeforeUnmount(() => {
  // Use a more significant delay to ensure all operations have completed
  // This helps prevent race conditions with editor operations
  setTimeout(() => {
    safelyDisposeEditor();
  }, 50);
  
  // Set a flag to prevent any further editor operations
  isDisposing.value = true;
});

// Update the watch handler for LogchefQL to include time filter
watch(() => logchefQLCode.value, (newCode) => {
  // When LogchefQL code changes and we're in LogchefQL mode, prepare SQL for future tab switch
  if (activeTab.value === "logchefql" && newCode.trim()) {
    try {
      const parser = new LogchefQLParser();
      parser.parse(newCode);

      if (parser.root) {
        const whereConditions = translateToSQLConditions(parser.root);
        if (whereConditions && whereConditions.trim()) {
          console.log('Watch - LogchefQL changed, updating SQL conditions:', whereConditions);

          // Prepare SQL code but don't set as current editor value
          // Include time filter by default
          let baseQuery = QueryBuilder.getDefaultSQLQuery({
            tableName: props.tableName,
            tsField: props.tsField,
            startTimestamp: props.startTimestamp,
            endTimestamp: props.endTimestamp,
            limit: props.limit,
            includeTimeFilter: true, // Include time filter
            forDisplay: true // Use human-readable timestamp format for display
          });

          if (baseQuery.includes("WHERE")) {
            baseQuery = baseQuery.replace(
              /WHERE\s+([^\n]+)/i,
              `WHERE $1 AND (${whereConditions})`
            );
          } else {
            baseQuery = baseQuery.replace(
              /FROM\s+([^\n]+)/i,
              `FROM $1 WHERE ${whereConditions}`
            );
          }

          // Set sqlCode for when we switch tabs, but don't change current editor
          sqlCode.value = baseQuery;
        }
      }
    } catch (error) {
      // Ignore parsing errors during typing - we'll handle them when switching tabs
    }
  }
});

// Watch for changes in the store's rawSql and update editor when in SQL mode
watch(() => exploreStore.rawSql, (newValue) => {
  if ((activeTab.value === 'clickhouse-sql' || activeTab.value === 'sql') && newValue !== undefined) {
    // Handle case where newValue might be an object instead of a string
    const sqlString = typeof newValue === 'string'
      ? newValue
      : (typeof newValue === 'object' && newValue !== null && 'sql' in newValue)
        ? (newValue as any).sql || ''
        : '';

    // Only update if we have a non-empty value or if the current value is empty
    if (sqlString.trim() || !sqlCode.value.trim()) {
      // Store the raw SQL as is, including time filter
      sqlCode.value = sqlString;

      // Only update the display if we're in SQL mode
      if (activeTab.value === 'clickhouse-sql' || activeTab.value === 'sql') {
        code.value = sqlCode.value;
      }
    }
  }
});

// Watch for loading state from the store to disable editor during API calls
watch(() => exploreStore.isLoadingOperation('executeQuery'), (isLoading) => {
  if (editorRef.value && !isDisposing.value) {
    try {
      editorRef.value.updateOptions({ readOnly: isLoading });
    } catch (error) {
      console.warn('Error updating editor readOnly state:', error);
    }
  }
});

// Update the submitQuery function to use QueryBuilder service
const submitQuery = () => {
  try {
    if (!validateQuery()) {
      return;
    }

    // Initialize finalQuery based on code.value (may be empty)
    let finalQuery = code.value || '';

    // If the query is empty, use a default based on the active mode
    if (!finalQuery.trim()) {
      if (activeTab.value === 'clickhouse-sql') {
        // Empty SQL - will use default query when executed
        finalQuery = '';
      } else {
        // Empty LogchefQL - use simple "all" query
        finalQuery = '*';
      }
    }

    // Update the appropriate store property based on the active tab
    if (activeTab.value === 'logchefql') {
      // For LogchefQL, save the query to the store
      exploreStore.setLogchefqlCode(finalQuery);

      // Ensure the store knows we're in LogchefQL mode
      exploreStore.setActiveMode('logchefql');
    } else {
      // For SQL, save directly to the store
      exploreStore.setRawSql(finalQuery);

      // Ensure the store knows we're in SQL mode
      exploreStore.setActiveMode('sql');
    }

    // Clear any previous error
    errorMessage.value = '';
    errorText.value = '';

    // Emit the submit event with mode and query info
    emit('submit', {
      type: activeTab.value,
      query: finalQuery,
      mode: activeTab.value === 'logchefql' ? 'logchefql' : 'sql'
    });

    // Always emit change event to trigger URL update in parent
    emit('change', {
      query: finalQuery,
      mode: activeTab.value === 'logchefql' ? 'logchefql' : 'sql'
    });
  } catch (error) {
    console.error('Error submitting query:', error);
    errorMessage.value = error instanceof Error ? error.message : 'An error occurred';
    errorText.value = errorMessage.value;
  }
};

// Add onChange function definition
const onChange = (value: string) => {
  // Update local state
  code.value = value;

  // Also update the appropriate store properties to keep them in sync
  if (activeTab.value === 'logchefql') {
    logchefQLCode.value = value;
    exploreStore.setLogchefqlCode(value);

    // Set flag to indicate user has manually entered a LogchefQL query
    // Only set if the value is not empty to avoid counting empty changes
    if (value.trim()) {
      hasManuallyEnteredLogchefQL.value = true;
    }
  } else {
    sqlCode.value = value;
    exploreStore.setRawSql(value);
  }

  // Emit change event to parent with proper structure
  emit('change', {
    query: value,
    mode: activeTab.value === 'logchefql' ? 'logchefql' : 'sql'
  });
};

// --- Monaco Options and Updates ---
const updateMonacoOptions = () => {
  const editor = editorRef.value;
  if (!editor) return;

  const isSqlMode = activeMode.value === 'clickhouse-sql';

  // Update reactive options object
  Object.assign(monacoOptions, {
      ...getDefaultMonacoOptions(), // Start with base defaults
      // Mode-specific overrides:
      folding: isSqlMode,
      lineNumbers: isSqlMode ? "on" : "off",
      wordWrap: isSqlMode ? "on" : "off",
      glyphMargin: isSqlMode, // Show glyph margin only for SQL (folding)
      lineDecorationsWidth: isSqlMode ? 10 : 0,
      lineNumbersMinChars: isSqlMode ? 3 : 0,
      padding: { top: isSqlMode ? 6 : 8, bottom: isSqlMode ? 6 : 8 },
      // Update placeholder via options
      // Disabled: placeholder option doesn't work well with vue-monaco-editor's value binding
      // 'placeholder': currentPlaceholder.value,
  });

   // Apply the updated options to the editor instance
   editor.updateOptions(monacoOptions);
};

// --- Completion Providers ---
const disposeCompletionProviders = () => {
  activeProviders.value.forEach(p => p.dispose());
  activeProviders.value = activeProviders.value.filter(p =>
    !p.hasOwnProperty('provideCompletionItems') // Keep non-completion disposables
  );
};

const registerCompletionProvider = () => {
  disposeCompletionProviders(); // Dispose previous before registering new

  let provider: monaco.IDisposable | null = null;
  if (activeMode.value === 'logchefql') {
    provider = registerLogchefQLCompletionProvider();
  } else {
    provider = registerSQLCompletionProvider();
  }

  if (provider) {
    activeProviders.value.push(provider);
  }
};

// LogchefQL Completion (simplified - keep existing logic, ensure correct types)
const registerLogchefQLCompletionProvider = (): monaco.IDisposable => {
    // Use the provided completion logic, ensure types match Monaco's API
    // (Keep the existing getSuggestionsFromList, getOperatorsSuggestions, etc.)
    // Wrap the existing logic inside the Monaco provider structure.
    // ... (Implementation based on the original code, ensuring types)
    return monaco.languages.registerCompletionItemProvider('logchefql', {
        triggerCharacters: ["=", "!", ">", "<", "~", " ", ".", '"', "'"], // Add quotes
        provideCompletionItems: async (model, position) => {
            const word = model.getWordUntilPosition(position);
            const textBeforeCursor = model.getValueInRange({
                startLineNumber: 1, endLineNumber: position.lineNumber,
                startColumn: 1, endColumn: position.column
            });

            // Use the LogchefQLParser to determine context
            const parser = new LogchefQLParser();
            try {
                // Parse up to cursor, ignoring errors and EOF issues for completion
                parser.parse(textBeforeCursor, false, true);
            } catch { /* Ignore parsing errors during completion */ }

            const range: MonacoRange = {
                startLineNumber: position.lineNumber, endLineNumber: position.lineNumber,
                startColumn: word.startColumn, endColumn: word.endColumn,
            };

            let suggestions: MonacoCompletionItem[] = [];
            let incomplete = false; // For async value fetching

            // --- Context-based Suggestions ---
            const currentState = parser.state;
            const currentKey = parser.key;
            const currentOperator = parser.keyValueOperator;
            const currentValue = parser.value;

            if (currentState === LogchefQLState.INITIAL || currentState === LogchefQLState.BOOL_OP_DELIMITER) {
                // Expecting a key or '('
                suggestions = getKeySuggestions(range);
                suggestions.push({ // Add parenthesis suggestion
                     label: '()', kind: monaco.languages.CompletionItemKind.Snippet,
                     insertText: '($1) ', range: range, documentation: 'Group conditions',
                     sortText: 'zzzz', // Sort last
                     command: { id: "cursorMove", arguments: { to: "prev", value: 3 } } // Move cursor inside
                });

            } else if (currentState === LogchefQLState.KEY) {
                 // Expecting an operator
                suggestions = getOperatorsSuggestions(currentKey, position);

            } else if (currentState === LogchefQLState.KEY_VALUE_OPERATOR) {
                // Expecting start of value (quote or direct value)
                suggestions.push(
                    { label: '""', kind: monaco.languages.CompletionItemKind.Snippet, insertText: '"$1" ', range: range, documentation: 'String value', command: { id: "cursorMove", arguments: { to: "prev", value: 2 } } },
                    { label: "''", kind: monaco.languages.CompletionItemKind.Snippet, insertText: "'$1' ", range: range, documentation: 'String value', command: { id: "cursorMove", arguments: { to: "prev", value: 2 } } }
                );
                // Potentially suggest known values here too if applicable without quotes
                const valueSuggestions = await getValueSuggestions(currentKey, '', range);
                suggestions = suggestions.concat(valueSuggestions.suggestions);
                incomplete = valueSuggestions.incomplete;

            } else if (currentState === LogchefQLState.VALUE || currentState === LogchefQLState.SINGLE_QUOTED_VALUE || currentState === LogchefQLState.DOUBLE_QUOTED_VALUE) {
                // Suggesting values (potentially async)
                range.startColumn = position.column - currentValue.length; // Adjust range to cover typed value
                let quoteChar = currentState === LogchefQLState.SINGLE_QUOTED_VALUE ? "'" : currentState === LogchefQLState.DOUBLE_QUOTED_VALUE ? '"' : undefined;
                const valueSuggestions = await getValueSuggestions(currentKey, currentValue, range, quoteChar);
                suggestions = valueSuggestions.suggestions;
                incomplete = valueSuggestions.incomplete;

            } else if (currentState === LogchefQLState.EXPECT_BOOL_OP) {
                // Expecting 'and' or 'or' or ')'
                // Ensure we are *after* a space, not immediately after value/quote
                if (textBeforeCursor.endsWith(' ')) {
                   suggestions = getBooleanOperatorsSuggestions(range);
                }
                 // Also suggest closing parenthesis if applicable (check parser.nodesStack)
                 if (parser.nodesStack.length > 0) {
                     suggestions.push({ label: ')', kind: monaco.languages.CompletionItemKind.Keyword, insertText: ') ', range: range });
                 }
            }

            // --- Helper Functions (Scoped inside provider) ---
            // (Include getSuggestionsFromList, getOperatorsSuggestions, etc. here, adapting types)
            function getKeySuggestions(range: MonacoRange): MonacoCompletionItem[] { /* ... from original ... */ }
            function getOperatorsSuggestions(field: string, position: MonacoPosition): MonacoCompletionItem[] { /* ... from original ... */ }
            async function getValueSuggestions(key: string, value: string, range: MonacoRange, quoteChar?: string): Promise<{ suggestions: MonacoCompletionItem[], incomplete: boolean }> { /* ... from original ... */ }
            function getBooleanOperatorsSuggestions(range: MonacoRange): MonacoCompletionItem[] { /* ... from original ... */ }
            // --- End Helpers ---

            return { suggestions, incomplete };
        }
    });
};

// SQL Completion (simplified - keep existing logic, ensure correct types)
const registerSQLCompletionProvider = (): monaco.IDisposable => {
    // Use the provided completion logic, ensure types match Monaco's API
    // (Keep the existing logic for detecting context like FROM, WHERE, etc.)
    // Wrap the existing logic inside the Monaco provider structure.
     return monaco.languages.registerCompletionItemProvider('clickhouse-sql', {
        triggerCharacters: [" ", "\n", ".", "(", ",", "'", "`"],
        provideCompletionItems: (model, position) => {
             // ... (Implementation based on the original code)
             // Use props.tableName, fieldNames.value, SQL_KEYWORDS, CLICKHOUSE_FUNCTIONS etc.
             // Ensure all returned suggestions have `range`, `kind`, `label`, `insertText`.
            const word = model.getWordUntilPosition(position);
            const range: MonacoRange = {
                startLineNumber: position.lineNumber, endLineNumber: position.lineNumber,
                startColumn: word.startColumn, endColumn: word.endColumn,
            };
            const textBeforeCursor = model.getValueInRange({ ...range, startColumn: 1 });

            let suggestions: MonacoCompletionItem[] = [];

            // Basic Keyword Suggestions (always relevant)
            suggestions = suggestions.concat(
                SQL_KEYWORDS.map(kw => ({
                    label: kw, kind: monaco.languages.CompletionItemKind.Keyword,
                    insertText: kw + ' ', range: range, sortText: `1_${kw}`
                }))
            );

            // Function Suggestions (always relevant)
             suggestions = suggestions.concat(
                CLICKHOUSE_FUNCTIONS.map(fn => ({
                    label: fn, kind: monaco.languages.CompletionItemKind.Function,
                    insertText: `${fn}($1)`, insertTextRules: monaco.languages.CompletionItemInsertTextRule.InsertAsSnippet,
                    range: range, sortText: `2_${fn}`, documentation: 'ClickHouse Function'
                }))
            );

            // Field Suggestions (context dependent)
            const isInSelect = /\bSELECT\s+(?:[\w\(\)\*,\s]+\s+)?$/i.test(textBeforeCursor);
            const isInWhere = /\bWHERE\s+(?:.+?\s+)?$/i.test(textBeforeCursor);
            const isInGroupBy = /\bGROUP\s+BY\s+(?:.+?\s+)?$/i.test(textBeforeCursor);
            const isInOrderBy = /\bORDER\s+BY\s+(?:.+?\s+)?$/i.test(textBeforeCursor);

            if (isInSelect || isInWhere || isInGroupBy || isInOrderBy || textBeforeCursor.endsWith('.')) {
                 suggestions = suggestions.concat(
                    fieldNames.value.map(f => ({
                        label: f, kind: monaco.languages.CompletionItemKind.Field,
                        insertText: f, range: range, sortText: `0_${f}`,
                        detail: props.schema[f]?.type ?? 'unknown'
                    }))
                );
            }

            // Table Name Suggestion (after FROM/JOIN)
            const isAfterFrom = /\bFROM\s+$/i.test(textBeforeCursor);
            const isAfterJoin = /\bJOIN\s+$/i.test(textBeforeCursor);
            if ((isAfterFrom || isAfterJoin) && props.tableName) {
                 suggestions.push({
                     label: props.tableName, kind: monaco.languages.CompletionItemKind.Class, // Use Class/Module for table
                     insertText: props.tableName + ' ', range: range, sortText: `0_table`,
                     documentation: 'Current logs table'
                 });
            }

            // Basic Snippets
            suggestions.push({
                label: 'SELECT * FROM ...', kind: monaco.languages.CompletionItemKind.Snippet,
                insertText: `SELECT $1 FROM ${props.tableName || 'your_table'} WHERE $2 ORDER BY ${props.tsField || 'timestamp'} DESC LIMIT ${props.limit || 100}`,
                insertTextRules: monaco.languages.CompletionItemInsertTextRule.InsertAsSnippet,
                range: range, sortText: 'zzzz_select', documentation: 'Basic query snippet'
            });

            return { suggestions };
        }
    });
};

// --- Watchers ---
// Watch for external changes to sync editor (e.g., URL change updates store)
watch(() => [exploreStore.logchefqlCode, exploreStore.rawSql, exploreStore.activeMode], () => {
    // Avoid sync if a programmatic change is in progress
    if (!isProgrammaticChange.value) {
        syncEditorContentWithStore();
    }
});

// Watch relevant props to regenerate SQL if needed (timestamp, limit, table)
// Debounce this to avoid excessive updates during rapid prop changes
let sqlUpdateTimeout: number | null = null;
watch([() => props.startTimestamp, () => props.endTimestamp, () => props.limit, () => props.tableName, () => props.tsField],
  () => {
    if (sqlUpdateTimeout) clearTimeout(sqlUpdateTimeout);
    sqlUpdateTimeout = window.setTimeout(() => {
        // Only update SQL if currently in SQL mode AND the SQL likely contains generated parts
        if (activeMode.value === 'clickhouse-sql' && editorContent.value) {
            // More robust: check if the current SQL seems to be generated/default
            // This is tricky. For now, let's update if AST parsing works.
            try {
                 const parsed = SQLParser.parse(editorContent.value, props.tsField);
                 if (parsed) {
                     // Re-generate the query using current props IF it was likely generated
                     // Heuristic: Does it have the standard time filter and limit?
                     const needsUpdate = parsed.whereClause?.hasTimestampFilter || parsed.limit === oldLimitValue; // Need to store oldLimitValue

                     // Or simpler: Always try to update if parseable
                     let updatedSql = editorContent.value;

                     // Update Time Filter
                     if (parsed.whereClause?.hasTimestampFilter) {
                         const timeFilterRegex = new RegExp(
                             `${props.tsField}\\s+BETWEEN\\s+'[^']+'\\s+AND\\s+'[^']+'`, 'i'
                         );
                         const newTimeCondition = QueryBuilder.formatTimeCondition(
                             props.tsField, props.startTimestamp * 1000, props.endTimestamp * 1000, true
                         );
                         updatedSql = updatedSql.replace(timeFilterRegex, newTimeCondition);
                     } // Else: Don't add time filter if user removed it

                     // Update Limit
                     const limitRegex = /\bLIMIT\s+\d+/i;
                     const newLimitClause = `LIMIT ${props.limit}`;
                     if (parsed.limit && limitRegex.test(updatedSql)) {
                         updatedSql = updatedSql.replace(limitRegex, newLimitClause);
                     } else if (!parsed.limit) {
                         // Add limit if it was missing
                         updatedSql = SQLParser.toSQL(SQLParser.applyLimit(parsed, props.limit));
                     }

                     if (updatedSql !== editorContent.value) {
                        runProgrammaticUpdate(updatedSql);
                        // Optionally notify user?
                     }
                 }
            } catch(e) {
                console.warn("Could not parse existing SQL for auto-update:", e)
            }
        }
        // Store current limit for next comparison
        oldLimitValue = props.limit;

    }, 300); // Debounce updates
});
let oldLimitValue = props.limit; // Store initial limit

// Update placeholder when it changes
watch(currentPlaceholder, () => {
    // Placeholder update via options is unreliable with v-model
    // Consider adding a visual placeholder element behind the editor if needed
});

// Watch for loading state to make editor read-only
watch(() => exploreStore.isLoadingOperation('executeQuery'), (isLoading) => {
  if (editorRef.value) {
    editorRef.value.updateOptions({ readOnly: isLoading });
  }
});


// --- Validation ---
const validateQuery = (): boolean => {
  const query = editorContent.value ?? "";
  validationError.value = null; // Clear previous error

  if (!query.trim()) {
    return true; // Empty query is considered valid (will run default)
  }

  let isValid = false;
  let errorMsg: string | null = null;

  try {
    if (activeMode.value === 'logchefql') {
      isValid = validateLogchefQL(query);
      if (!isValid) errorMsg = "Invalid LogchefQL syntax.";
    } else {
      // Use a more specific validation if available
      isValid = validateSQL(query); // Assuming validateSQL returns boolean
      if (!isValid) errorMsg = "Invalid SQL syntax.";
      // Consider using the CH parser here for more detailed errors
    }
  } catch (error: any) {
     console.error("Validation Error:", error);
     isValid = false;
     errorMsg = error.message || "Query validation failed.";
  }

  if (!isValid) {
    validationError.value = errorMsg;
  }
  return isValid;
};

// --- Actions ---
const submitQuery = () => {
  if (!validateQuery()) {
    return;
  }
  const queryToSubmit = editorContent.value ?? "";

   // Update store just before submitting to ensure it's current
  if (activeMode.value === 'logchefql') {
    exploreStore.setLogchefqlCode(queryToSubmit);
  } else {
    exploreStore.setRawSql(queryToSubmit);
  }

  emit("submit", { query: queryToSubmit, mode: activeMode.value });
};

// --- Disposal ---
const safelyDisposeEditor = () => {
    console.log('QueryEditor: Starting disposal');
    // Dispose Monaco resources (listeners, providers, actions)
    activeProviders.value.forEach(disposable => {
        try {
            disposable.dispose();
        } catch (e) { console.warn("Error disposing resource:", e); }
    });
    activeProviders.value = [];

    // Dispose editor instance and model
    const editor = editorRef.value;
    const model = editorModel.value;

    // Nullify refs immediately
    editorRef.value = null;
    editorModel.value = null;

    if (editor) {
        try {
            // It's generally recommended to dispose the model associated with the editor *first*
            // or let the editor dispose its current model upon its own disposal.
            // Disposing the editor instance should handle its model.
            editor.dispose();
            console.log('QueryEditor: Editor instance disposed');
        } catch (e) {
            console.warn("Error disposing editor instance:", e);
        }
    }
     // Models might be shared, but if we got it via getModel(), dispose it *if* the editor didn't
     // However, editor.dispose() *should* handle the model it holds. Explicit model disposal
     // here can cause issues if the model is used elsewhere or already disposed.
     // Rely on editor.dispose() for cleanup.
    console.log('QueryEditor: Disposal finished');
};

// --- Expose ---
defineExpose({
  submitQuery,
  setActiveMode: handleTabChange, // Expose function to allow parent control
  getCurrentQuery: () => ({ query: editorContent.value ?? "", mode: activeMode.value })
});

const handleQueryChange = (data: any) => {
  // Ensure data is an object before destructuring
  if (!data || typeof data !== 'object') {
    console.warn('Invalid data received in handleQueryChange:', data);
    return;
  }

  const { query: inputQuery, mode } = data;
  // Ensure queryText is always a string, even if inputQuery is undefined
  const queryText = inputQuery || '';

  // Map the mode from editor to store format
  const mappedMode = mode === 'logchefql' ? 'logchefql' : 'sql';

  // Update the mode if it's changing
  if (activeTab.value !== mappedMode) {
    activeTab.value = mappedMode;
    exploreStore.setActiveMode(mappedMode);
  }

  // Simply update the appropriate store and local state
  if (mappedMode === 'logchefql') {
    logchefQLCode.value = queryText;
    exploreStore.setLogchefqlCode(queryText);
    hasManuallyEnteredLogchefQL.value = !!queryText.trim();
  } else {
    sqlCode.value = queryText;
    exploreStore.setRawSql(queryText);
  }

  // Update editor content
  code.value = queryText;

  // Clear any error messages when the user is typing
  if (errorText.value) {
    errorText.value = '';
  }

  // Emit change event to update URL
  emit('change', {
    query: queryText,
    mode: mappedMode
  });
}
</script>

<style scoped>
.query-editor {
  display: flex;
  flex-direction: column;
  position: relative; /* For potential placeholder positioning */
}

.editor-container {
  background-color: hsl(var(--card));
  transition: border-color 0.15s ease, box-shadow 0.15s ease;
  min-height: 45px; /* Ensure container has a minimum height */
}

/* Optional: Style for visual placeholder if needed */
.editor-placeholder {
  position: absolute;
  top: calc(theme(padding.2) + theme(spacing[1.5])); /* Adjust based on editor padding */
  left: calc(theme(padding.3) + 5px); /* Adjust based on line number width/padding */
  color: hsl(var(--muted-foreground));
  opacity: 0.6;
  pointer-events: none;
  font-size: 14px; /* Match editor font size */
  font-family: ui-monospace, SFMono-Regular, Menlo, Monaco, Consolas, "Liberation Mono", "Courier New", monospace;
}

/* Shadcn focus styling */
button:focus-visible {
  outline: 2px solid hsl(var(--ring));
  outline-offset: 2px;
  border-radius: var(--radius);
}

code {
  font-family: inherit; /* Inherit from editor/UI */
}
</style>
