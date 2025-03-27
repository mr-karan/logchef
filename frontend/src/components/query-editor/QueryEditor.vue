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

// Flag to prevent operations during disposal
const isDisposing = ref(false);

// Track if editor is initialized
const isEditorInitialized = ref(false);

// Track disposal resources
const disposeArray = ref<monaco.IDisposable[]>([]);

// Helper function for building completion suggestions
const getSuggestionsFromList = (params: any) => {
  const suggestions: MonacoCompletionItem[] = [];
  let defaultPostfix = params.postfix === undefined ? "" : params.postfix;

  const range =
    params.range === undefined
      ? {
        startLineNumber: params.position.lineNumber,
        endLineNumber: params.position.lineNumber,
        startColumn: params.position.column,
        endColumn: params.position.column,
      }
      : params.range;

  for (const item of params.items) {
    let label = null;
    let sortText = null;
    let postfix = defaultPostfix;
    if (typeof item === "string" || item instanceof String) {
      label = item;
      sortText = label;
    } else {
      label = item.label;
      sortText = item.sortText === undefined ? label : item.sortText;
      postfix = item.postfix === undefined ? defaultPostfix : item.postfix;
    }
    let insertText = item.insertText === undefined ? label : item.insertText;
    suggestions.push({
      label: label,
      kind: params.kind,
      range: range,
      sortText: sortText,
      insertText: insertText + postfix,
      command: {
        id: "editor.action.triggerSuggest",
        title: "Trigger Suggest"
      },
    });
  }
  return suggestions;
};

// --- Editor Handling ---
const handleMount = (editor: monaco.editor.IStandaloneCodeEditor) => {
  // console.log('QueryEditor: Monaco editor mounted');
  editorRef.value = editor;
  editorModel.value = editor.getModel();

  updateMonacoOptions(); // Apply initial options for the active mode
  registerCompletionProvider(); // Register for the initial mode

  // Add Submit Shortcut
  const submitAction = editor.addAction({
    id: "submit-query",
    label: "Run Query",
    keybindings: [monaco.KeyMod.CtrlCmd | monaco.KeyCode.Enter],
    run: submitQuery,
  });
  if (submitAction) activeProviders.value.push(submitAction); // Track action for disposal

  // Focus/Blur Tracking
  const focusListener = editor.onDidFocusEditorWidget(() => { editorFocused.value = true; });
  const blurListener = editor.onDidBlurEditorWidget(() => { editorFocused.value = false; });
  activeProviders.value.push(focusListener, blurListener);

  // Focus the editor shortly after mount
  setTimeout(() => editor.focus(), 50);
};

const handleEditorChange = (value: string | undefined) => {
  // If change is programmatic, ignore to prevent loops
  if (isProgrammaticChange.value) return;

  const currentQuery = value ?? "";
  editorContent.value = currentQuery; // Update local ref immediately

  // Update the store based on the current mode
  if (activeMode.value === 'logchefql') {
    exploreStore.setLogchefqlCode(currentQuery);
  } else {
    exploreStore.setRawSql(currentQuery);
  }

  // Clear validation errors on manual input
  if (validationError.value) {
    validationError.value = null;
  }

  // Emit change event
  emit("change", { query: currentQuery, mode: activeMode.value });
};

const runProgrammaticUpdate = (newValue: string) => {
    isProgrammaticChange.value = true;
    editorContent.value = newValue;
    // Also update the corresponding store value
    if (activeMode.value === 'logchefql') {
      exploreStore.setLogchefqlCode(newValue);
    } else {
      exploreStore.setRawSql(newValue);
    }
    // Wait for editor to potentially update, then release the flag
    nextTick(() => {
        isProgrammaticChange.value = false;
    });
};

const syncEditorContentWithStore = (isInitialSync = false) => {
    let storeValue: string | undefined;
    if (activeMode.value === 'logchefql') {
        storeValue = exploreStore.logchefqlCode;
    } else {
        storeValue = exploreStore.rawSql;
    }

    // On initial sync, props have higher priority if provided
    const valueToSet = (isInitialSync && props.initialValue) ? props.initialValue : (storeValue ?? "");

    if (editorContent.value !== valueToSet) {
        runProgrammaticUpdate(valueToSet);
    }
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
    { label: LogchefQLOperator.EQUALS, sortText: "a" },
    { label: LogchefQLOperator.NOT_EQUALS, sortText: "b" },
    { label: LogchefQLOperator.GREATER_THAN, sortText: "e" },
    { label: LogchefQLOperator.GREATER_OR_EQUALS_THAN, sortText: "f" },
    { label: LogchefQLOperator.LOWER_THAN, sortText: "g" },
    { label: LogchefQLOperator.LOWER_OR_EQUALS_THAN, sortText: "h" },
  ];

  // Only add regex operators for string fields
  const fieldType = props.schema[field]?.type || "";
  if (fieldType.includes("String") || fieldType.includes("string")) {
    operators.push(
      { label: LogchefQLOperator.EQUALS_REGEX, sortText: "c" },
      { label: LogchefQLOperator.NOT_EQUALS_REGEX, sortText: "d" }
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

// This is already defined as safelyDisposeEditor earlier

// This watcher is not needed - initialization is handled in onMounted

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

// This function is replaced by handleTabChange which is already defined

// This function is already redefined as validateQuery earlier

// Clean function to dispose all existing providers
function disposeAllProviders() {
  try {
    while (activeProviders.value.length > 0) {
      const provider = activeProviders.value.pop();
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
  activeProviders.value.push(provider);
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
        parser.state === LogchefQLState.KEY ||
        parser.state === LogchefQLState.INITIAL ||
        parser.state === LogchefQLState.BOOL_OP_DELIMITER
      ) {
        if (fieldNames.value.includes(word.word)) {
          suggestions = getOperatorsSuggestions(word.word, position);
        } else {
          suggestions = getKeySuggestions(range);
        }
      } else if (parser.state === LogchefQLState.KEY_VALUE_OPERATOR) {
        // @ts-ignore: Type compatibility issues with operator types
        if (VALID_KEY_VALUE_OPERATORS.includes(parser.keyValueOperator)) {
          range.startColumn = word.endColumn - parser.value.length;
          const result = await getValueSuggestions(parser.key, parser.value, range);
          incomplete = result.incomplete;
          suggestions = result.suggestions;
        }
      } else if (
        parser.state === LogchefQLState.VALUE ||
        parser.state === LogchefQLState.DOUBLE_QUOTED_VALUE ||
        parser.state === LogchefQLState.SINGLE_QUOTED_VALUE
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
      } else if (parser.state === LogchefQLState.EXPECT_BOOL_OP) {
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
  activeProviders.value.push(provider);
  return provider;
};

// Removed duplicate handleMount function

// This function is replaced by updateMonacoOptions which is already defined

// These watchers are already handled by the refactored code


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

// This watch is no longer needed as the handleTabChange function handles SQL generation

// This watch is already handled by the syncEditorContentWithStore function and watcher

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

// Remove duplicate provider registration functions since they're already defined earlier

// This function is already redefined earlier in the code

// Not needed - handleEditorChange is used for this purpose

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
    if (editorRef.value) {
        try {
            // Try to update placeholder anyway, might work in some cases
            editorRef.value.updateOptions({ 'placeholder': currentPlaceholder.value });
        } catch (err) {
            console.warn('Error updating placeholder:', err);
        }
    }
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

// This function is no longer needed - handleEditorChange and handleTabChange handle this functionality
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
