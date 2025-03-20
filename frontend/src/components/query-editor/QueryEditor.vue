<template>
  <div class="query-editor">
    <!-- Combined Query Editor Header with all controls in one bar -->
    <div class="flex items-center justify-between bg-muted/40 rounded-t-md px-3 py-2">
      <div class="flex items-center gap-3">
        <!-- Fields Panel Toggle -->
        <button class="p-1 text-muted-foreground hover:text-foreground flex items-center"
          @click="$emit('toggle-fields')" :title="props.showFieldsPanel ? 'Hide fields panel' : 'Show fields panel'">
          <PanelRightClose v-if="props.showFieldsPanel" class="h-4 w-4" />
          <PanelRightOpen v-else class="h-4 w-4" />
        </button>

        <!-- Tabs for LogchefQL / SQL using shadcn Tabs -->
        <Tabs :default-value="initialTabValue" v-model="activeTabValue" class="w-auto">
          <TabsList class="h-8">
            <TabsTrigger value="logchefql">LogchefQL</TabsTrigger>
            <TabsTrigger value="clickhouse-sql">SQL</TabsTrigger>
          </TabsList>
        </Tabs>
      </div>

      <div class="flex items-center gap-2">
        <!-- Help Icon -->
        <HoverCard>
          <HoverCardTrigger>
            <button class="p-1 text-muted-foreground hover:text-foreground">
              <HelpCircle class="h-4 w-4" />
            </button>
          </HoverCardTrigger>
          <HoverCardContent class="w-80 backdrop-blur-md bg-card text-card-foreground border-border">
            <div class="space-y-2">
              <h4 class="text-sm font-semibold">{{ activeTab === 'logchefql' ? 'LogchefQL' : 'SQL' }} Syntax</h4>
              <div v-if="activeTab === 'logchefql'" class="text-xs space-y-1.5">
                <div><code class="bg-muted px-1 rounded">field="value"</code> - Exact match</div>
                <div><code class="bg-muted px-1 rounded">field!="value"</code> - Not equal</div>
                <div><code class="bg-muted px-1 rounded">field~"pattern"</code> - Partial match (regex)</div>
                <div><code class="bg-muted px-1 rounded">field!~"pattern"</code> - Exclude pattern (regex)</div>
                <div><code class="bg-muted px-1 rounded">condition1 and condition2</code> - Combine with AND</div>
                <div><code class="bg-muted px-1 rounded">condition1 or condition2</code> - Combine with OR</div>
                <div class="pt-1"><em>Example: <code
                      class="bg-muted px-1 rounded">service_name="api" and status>=400</code></em></div>
              </div>
              <div v-else class="text-xs space-y-1.5">
                <div><code class="bg-muted px-1 rounded">SELECT * FROM {{ tableName }}</code> - Basic query</div>
                <div><code class="bg-muted px-1 rounded">WHERE field = 'value'</code> - Filter by exact match</div>
                <div><code class="bg-muted px-1 rounded">WHERE field LIKE '%pattern%'</code> - Pattern matching</div>
                <div class="pt-1"><em>Time range and limit are automatically applied.</em></div>
              </div>
            </div>
          </HoverCardContent>
        </HoverCard>
      </div>
    </div>

    <!-- Monaco Editor with simplified container -->
    <div :style="{ height: `${editorHeight}px` }" class="editor border-x border-b rounded-b-md p-1"
      :class="{ 'border-primary/50 ring-1 ring-primary/20': editorFocused }">
      <!-- @ts-ignore -->
      <vue-monaco-editor v-model:value="code" :theme="theme" :language="activeTab" :options="getEditorOptions()"
        @mount="handleMount" @change="onChange" />
    </div>

    <!-- Error Message - more visible -->
    <div v-if="errorText" class="mt-2 p-2 text-sm text-destructive bg-destructive/10 rounded flex items-center">
      <AlertCircle class="h-4 w-4 mr-2" />
      {{ errorText }}
    </div>
  </div>
</template>

<script setup lang="ts">
import { ref, computed, shallowRef, nextTick, watch, onMounted, onBeforeUnmount, defineExpose } from "vue";
import * as monaco from "monaco-editor";
import { useDark } from "@vueuse/core";
import { VueMonacoEditor } from "@guolao/vue-monaco-editor";
import { HelpCircle, PanelRightOpen, PanelRightClose, AlertCircle } from "lucide-vue-next";
import {
  HoverCard,
  HoverCardContent,
  HoverCardTrigger,
} from '@/components/ui/hover-card';
import {
  Tabs,
  TabsList,
  TabsTrigger,
} from '@/components/ui/tabs'; // Import Tabs component

import { initMonacoSetup } from "@/utils/monaco";
import { Parser as LogchefQLParser, State, Operator, VALID_KEY_VALUE_OPERATORS, isNumeric } from "@/utils/logchefql";
import { translateToSQLConditions } from "@/utils/logchefql/api";
import { validateSQL } from "@/utils/clickhouse-sql/api";
import { SQL_KEYWORDS, CLICKHOUSE_FUNCTIONS, SQL_TYPES } from "@/utils/clickhouse-sql/language";
import { useExploreStore } from '@/stores/explore';
import { QueryBuilder } from "@/utils/query-builder";
import { SQLParser } from '@/utils/clickhouse-sql/ast';

// Define props and emits
const props = defineProps({
  sourceId: {
    type: Number,
    required: true
  },
  schema: {
    type: Object,
    required: true
  },
  startTimestamp: {
    type: Number,
    required: true
  },
  endTimestamp: {
    type: Number,
    required: true
  },
  initialValue: {
    type: String,
    default: ""
  },
  initialTab: {
    type: String,
    default: "logchefql"
  },
  placeholder: {
    type: String,
    default: ""
  },
  tsField: {
    type: String,
    default: "timestamp"
  },
  tableName: {
    type: String,
    required: true
  },
  limit: {
    type: Number,
    default: 1000
  },
  showFieldsPanel: {
    type: Boolean,
    default: false
  }
});

const emit = defineEmits(["change", "submit", "toggle-fields"]);

// State
const isDark = useDark();
const activeTab = ref(props.initialTab === "sql" ? "clickhouse-sql" : "logchefql");
const code = ref(props.initialValue || "");
// Initialize mode-specific variables based on initial tab
const logchefQLCode = ref(props.initialTab === "logchefql" ? (props.initialValue || "") : "");
const sqlCode = ref(props.initialTab === "sql" ? (props.initialValue || "") : "");
// Track if user has manually entered a LogchefQL query
const hasManuallyEnteredLogchefQL = ref(props.initialTab === "logchefql" && !!props.initialValue);
const editorRef = shallowRef();
const editorFocused = ref(false);
const errorText = ref("");
const fieldNames = computed(() => Object.keys(props.schema));
const exploreStore = useExploreStore();
const errorMessage = ref("");

// Add a computed property for the placeholder text based on activeTab
const currentPlaceholder = computed(() => {
  return activeTab.value === "logchefql"
    ? 'Enter LogchefQL query or leave empty to run SELECT * FROM table'
    : 'Enter SQL query or leave empty for default query';
});

// Track editor model and instance for proper cleanup
const editorModel = shallowRef();
const disposeArray = ref<monaco.IDisposable[]>([]);
const isDisposing = ref(false);

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

// Auto-completion helpers
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
      sortText = item.sortText;
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

// Computed properties
const editorHeight = computed(() => {
  // For LogchefQL, we use a smaller fixed height as they're typically single-line queries
  // For SQL, we calculate based on content with a larger minimum
  if (activeTab.value === 'logchefql') {
    const lines = (code.value.match(/\n/g) || []).length + 1;
    return Math.max(45, 14 + lines * 20); // Smaller height for LogchefQL
  } else {
    const lines = (code.value.match(/\n/g) || []).length + 1;
    // Provide more space for SQL queries with comments
    const hasComments = code.value.includes('--');
    const baseHeight = hasComments ? 100 : 80;
    return Math.max(baseHeight, 14 + lines * 20); // Larger minimum for SQL
  }
});

const theme = computed(() => {
  return isDark.value ? "logchef-dark" : "logchef-light";
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
        // Use our QueryBuilder to generate SQL from LogchefQL WITH time filter
        const result = QueryBuilder.buildSqlFromLogchefQL(logchefQLCode.value, {
          tableName: props.tableName,
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
        console.log('No SQL or LogchefQL found, generating default query for:', props.tableName);
        
        // Only generate default query if we have a valid table name
        if (props.tableName) {
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

      // Emit change event to trigger URL update in parent
      emit('change', {
        query: code.value || '',
        mode: 'sql'
      });
    }

    // Emit the submit event with mode and query info
    emit('submit', {
      type: activeTab.value,
      query: finalQuery,
      mode: activeTab.value === 'logchefql' ? 'logchefql' : 'sql'
    });
  } catch (error) {
    console.error('Error submitting query:', error);
    errorMessage.value = error instanceof Error ? error.message : 'An error occurred';
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

// Editor setup
function getEditorOptions() {
  return {
    readOnly: false,
    fontSize: 14,
    padding: {
      top: 10,
      bottom: 10,
    },
    contextmenu: false,
    tabCompletion: "on" as "on" | "off" | "onlySnippets",
    overviewRulerLanes: 0,
    lineNumbersMinChars: 3,
    scrollBeyondLastLine: false,
    scrollbar: {
      vertical: "auto" as const,
      horizontal: "auto" as const,
      useShadows: false,
      verticalScrollbarSize: 8,
      horizontalScrollbarSize: 8,
    },
    minimap: {
      enabled: false,
    },
    guides: {
      indentation: false,
    },
    folding: true,
    renderLineHighlight: "none" as "none" | "line" | "gutter" | "all",
    fixedOverflowWidgets: true,
    wordWrap: "on" as "on" | "off" | "wordWrapColumn" | "bounded",
    "semanticHighlighting.enabled": true,
  };
}

// Add a computed property for the initial tab value
const initialTabValue = computed(() => {
  return props.initialTab === 'sql' ? 'clickhouse-sql' : 'logchefql';
});

// Create a computed property for active tab with getter and setter
const activeTabValue = computed({
  get: () => activeTab.value,
  set: (value: string) => {
    if (value === 'clickhouse-sql') {
      setActiveTab('clickhouse-sql');
    } else {
      setActiveTab('logchefql');
    }
  }
});

// Expose necessary properties and methods to parent components
defineExpose({
  code, // Current editor content
  activeTab, // Current active tab
  submitQuery // Submit method
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
}

.editor {
  background-color: hsl(var(--card));
  transition: border-color 0.15s ease;
}

/* Additional styling for a cleaner look */
button:focus-visible {
  outline: 2px solid hsl(var(--ring));
  outline-offset: 2px;
}

.tab-button {
  transition: all 0.15s ease;
}

code {
  font-family: ui-monospace, SFMono-Regular, Menlo, Monaco, Consolas, "Liberation Mono", "Courier New", monospace;
}
</style>
