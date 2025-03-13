<template>
  <div class="query-editor">
    <div class="flex items-center justify-between bg-muted/40 rounded-t-md px-2 mb-4">
      <div class="flex space-x-1">
        <button 
          class="relative h-9 px-4 py-1 text-sm font-medium transition-all hover:bg-muted/80 focus-visible:outline-none rounded-t-md"
          :class="{ 'bg-card text-foreground font-semibold shadow-sm': activeTab === 'logchefql', 'text-muted-foreground': activeTab !== 'logchefql' }"
          @click="setActiveTab('logchefql')"
        >
          LogchefQL
        </button>
        <button 
          class="relative h-9 px-4 py-1 text-sm font-medium transition-all hover:bg-muted/80 focus-visible:outline-none rounded-t-md"
          :class="{ 'bg-card text-foreground font-semibold shadow-sm': activeTab === 'clickhouse-sql', 'text-muted-foreground': activeTab !== 'clickhouse-sql' }"
          @click="setActiveTab('sql')"
        >
          SQL
        </button>
      </div>
      
      <HoverCard>
        <HoverCardTrigger>
          <button class="p-1 text-muted-foreground hover:text-foreground">
            <HelpCircle class="h-4 w-4" />
          </button>
        </HoverCardTrigger>
        <HoverCardContent class="w-80 backdrop-blur-md bg-black/90 text-white border-gray-800">
          <div class="space-y-2">
            <h4 class="text-sm font-semibold text-white">{{ activeTab === 'logchefql' ? 'LogchefQL' : 'SQL' }} Syntax</h4>
            <div v-if="activeTab === 'logchefql'" class="text-xs space-y-1.5">
              <div><code class="bg-white/10 px-1 rounded">field="value"</code> - Exact match</div>
              <div><code class="bg-white/10 px-1 rounded">field!="value"</code> - Not equal</div>
              <div><code class="bg-white/10 px-1 rounded">field~"pattern"</code> - Partial match (regex)</div>
              <div><code class="bg-white/10 px-1 rounded">field!~"pattern"</code> - Exclude pattern (regex)</div>
              <div><code class="bg-white/10 px-1 rounded">condition1 and condition2</code> - Combine with AND</div>
              <div><code class="bg-white/10 px-1 rounded">condition1 or condition2</code> - Combine with OR</div>
              <div class="pt-1"><em>Example: <code class="bg-white/10 px-1 rounded">service_name="api" and status>=400</code></em></div>
            </div>
            <div v-else class="text-xs space-y-1.5">
              <div><code class="bg-white/10 px-1 rounded">SELECT * FROM {{ tableName }}</code> - Basic query</div>
              <div><code class="bg-white/10 px-1 rounded">WHERE field = 'value'</code> - Filter by exact match</div>
              <div><code class="bg-white/10 px-1 rounded">WHERE field LIKE '%pattern%'</code> - Pattern matching</div>
              <div class="pt-1"><em>Time range and limit are automatically applied.</em></div>
            </div>
          </div>
        </HoverCardContent>
      </HoverCard>
    </div>

    <div :style="{ height: `${editorHeight}px` }" class="editor border rounded pl-2 pr-2 mb-2 dark:border-neutral-600"
      :class="{ 'border-sky-800 dark:border-sky-700': editorFocused }">
      <vue-monaco-editor v-model:value="code" :theme="theme" :language="activeTab" :options="getDefaultMonacoOptions()"
        @mount="handleMount" @change="onChange" />
    </div>

    <div class="editor-controls flex items-center">
      <div class="editor-status text-sm text-red-500" v-if="errorText">
        {{ errorText }}
      </div>
    </div>
  </div>
</template>

<script setup lang="ts">
import { ref, computed, shallowRef, nextTick, watch, onMounted, onBeforeUnmount } from "vue";
import * as monaco from "monaco-editor";
import { useDark } from "@vueuse/core";
import { VueMonacoEditor } from "@guolao/vue-monaco-editor";
import { HelpCircle } from "lucide-vue-next";
import {
  HoverCard,
  HoverCardContent,
  HoverCardTrigger,
} from '@/components/ui/hover-card';

import { getDefaultMonacoOptions, initMonacoSetup } from "@/utils/monaco";
import { Parser as LogchefQLParser, State, Operator, VALID_KEY_VALUE_OPERATORS } from "@/utils/logchefql";
import { translateToSQL, translateToSQLConditions, validateLogchefQL } from "@/utils/logchefql/api";
import { validateSQL, getDefaultSQLQuery } from "@/utils/clickhouse-sql/api";
import { isNumeric } from "@/utils/logchefql";
import { SQL_KEYWORDS, CLICKHOUSE_FUNCTIONS, SQL_TYPES } from "@/utils/clickhouse-sql/language";

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
  }
});

const emit = defineEmits(["change", "submit"]);

// State
const isDark = useDark();
const activeTab = ref(props.initialTab === "sql" ? "clickhouse-sql" : "logchefql");
const code = ref(props.initialValue || "");
const logchefQLCode = ref(props.initialValue || "");
const sqlCode = ref("");
const editorRef = shallowRef();
const editorFocused = ref(false);
const errorText = ref("");
const fieldNames = computed(() => Object.keys(props.schema));

// Initialize Monaco
onMounted(() => {
  initMonacoSetup();
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

// No longer needed - removed status text computed property

// Methods
function setActiveTab(tab: string) {
  // Store current code based on current tab
  if (activeTab.value === "logchefql") {
    logchefQLCode.value = code.value;
  } else {
    sqlCode.value = code.value;
  }

  // Switch tab with transition
  if (tab === "logchefql") {
    activeTab.value = "logchefql";
    code.value = logchefQLCode.value;
  } else {
    activeTab.value = "clickhouse-sql";

    // ALWAYS translate LogchefQL to SQL when switching to SQL mode
    if (logchefQLCode.value) {
      try {
        const parser = new LogchefQLParser();
        parser.parse(logchefQLCode.value);

        if (parser.root) {
          // Get just the WHERE conditions from LogchefQL and create a complete SQL query
          const whereConditions = translateToSQLConditions(parser.root);
          console.log('QueryEditor - conditions from LogchefQL:', whereConditions);
          
          // Use the existing getDefaultSQLQuery and add our conditions to it
          let baseQuery = getDefaultSQLQuery(
            props.tableName, 
            props.tsField,
            props.startTimestamp,
            props.endTimestamp
          );
          
          // Insert the WHERE conditions 
          if (whereConditions && whereConditions.trim()) {
            // Check if there's already a WHERE clause
            if (baseQuery.includes("WHERE")) {
              // Add our conditions with AND
              baseQuery = baseQuery.replace(
                /WHERE\s+([^\n]+)/i,
                `WHERE $1 AND (${whereConditions})`
              );
            } else {
              // Add a new WHERE clause
              baseQuery = baseQuery.replace(
                /FROM\s+([^\n]+)/i,
                `FROM $1 WHERE ${whereConditions}`
              );
            }
            
            // Update SQL code with the new query
            sqlCode.value = baseQuery;
          } else {
            // Use default query if no conditions found
            sqlCode.value = getDefaultSQLQuery(
              props.tableName, 
              props.tsField,
              props.startTimestamp,
              props.endTimestamp
            );
          }
        } else {
          sqlCode.value = getDefaultSQLQuery(
            props.tableName, 
            props.tsField,
            props.startTimestamp,
            props.endTimestamp
          );
        }
      } catch (error) {
        console.error('Error translating LogchefQL to SQL:', error);
        sqlCode.value = getDefaultSQLQuery(
          props.tableName, 
          props.tsField,
          props.startTimestamp,
          props.endTimestamp
        );
      }
    } else if (!sqlCode.value) {
      // If no LogchefQL code and no SQL code, use default query
      sqlCode.value = getDefaultSQLQuery(
        props.tableName, 
        props.tsField,
        props.startTimestamp,
        props.endTimestamp
      );
    }

    code.value = sqlCode.value;
  }

  // Update editor language
  if (editorRef.value) {
    monaco.editor.setModelLanguage(
      editorRef.value.getModel(),
      activeTab.value
    );

    // Update editor options based on the new mode
    updateEditorOptions(editorRef.value);
    
    // Re-register appropriate completion provider based on active tab
    if (activeTab.value === "logchefql") {
      registerLogchefQLCompletionProvider();
    } else {
      registerSQLCompletionProvider();
    }
  }
}

function validateQuery(): boolean {
  errorText.value = "";

  if (activeTab.value === "logchefql") {
    const isValid = validateLogchefQL(code.value);
    if (!isValid) {
      errorText.value = "Invalid LogchefQL query";
      return false;
    }
  } else {
    const isValid = validateSQL(code.value);
    if (!isValid) {
      errorText.value = "Invalid SQL query";
      return false;
    }
  }

  return true;
}

function submitQuery() {
  if (!validateQuery()) return;

  // Format the query with human-readable timestamps before submitting
  let finalQuery = code.value;
  
  // If it's SQL and contains timestamp numbers without quotes, convert to ISO format
  if (activeTab.value === "clickhouse-sql" && 
      finalQuery.includes(props.tsField) && 
      props.startTimestamp && 
      props.endTimestamp) {
    
    const startTimeIso = new Date(props.startTimestamp * 1000).toISOString();
    const endTimeIso = new Date(props.endTimestamp * 1000).toISOString();
    
    // Replace UNIX timestamps with ISO strings
    finalQuery = finalQuery.replace(
      new RegExp(`${props.tsField}\\s+BETWEEN\\s+\\d+\\s+AND\\s+\\d+`, 'i'),
      `${props.tsField} BETWEEN '${startTimeIso}' AND '${endTimeIso}'`
    );
  }

  emit("submit", {
    query: finalQuery,
    mode: activeTab.value === "logchefql" ? "logchefql" : "sql",
    sourceId: props.sourceId
  });
}

function onChange() {
  emit("change", {
    query: code.value,
    mode: activeTab.value === "logchefql" ? "logchefql" : "sql"
  });
}

// Auto-completion helpers
const getSuggestionsFromList = (params: any) => {
  const suggestions = [];
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
      },
    });
  }
  return suggestions;
};

const getOperatorsSuggestions = (field: string, position: any) => {
  let operators = [
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
    operators = operators.concat([
      { label: Operator.EQUALS_REGEX, sortText: "c" },
      { label: Operator.NOT_EQUALS_REGEX, sortText: "d" },
    ]);
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
  const suggestions = [];
  for (const name of fieldNames.value) {
    const fieldInfo = props.schema[name];
    if (fieldInfo) {
      let documentation = `Field (${fieldInfo.type})`;
      suggestions.push({
        label: name,
        kind: monaco.languages.CompletionItemKind.Field,
        range: range,
        documentation: documentation,
        insertText: name,
        command: {
          id: "editor.action.triggerSuggest",
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
    suggestions: [],
    incomplete: false,
  };

  if (fieldNames.value.includes(key)) {
    // Get sample values for the field
    const fieldValues = getSchemaFieldValues(key);

    // Filter values based on current input
    const filteredValues = fieldValues.filter(val =>
      val.toLowerCase().includes(value.toLowerCase())
    );

    const items = prepareSuggestionValues(filteredValues, quoteChar);

    result.suggestions = getSuggestionsFromList({
      range: range,
      items: items,
      kind: monaco.languages.CompletionItemKind.Value,
      postfix: " ",
    });
  }

  return result;
};

// Track all active providers
const activeProviders: monaco.IDisposable[] = [];

// Clean function to dispose all existing providers
function disposeAllProviders() {
  while (activeProviders.length > 0) {
    const provider = activeProviders.pop();
    if (provider) {
      provider.dispose();
    }
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
      
      // Get full text for more context
      const fullText = model.getValue();

      // Check for specific SQL contexts - add debug info
      console.log('QueryEditor tableName prop:', props.tableName);
      
      // Improved regex for detecting being right after FROM keyword
      const isAfterFrom = /\bFROM\s+$/i.test(textBeforeCursor);
      const isInWhereClause = /\bWHERE\b/i.test(textBeforeCursor) && !/\bGROUP\s+BY\b|\bORDER\s+BY\b|\bLIMIT\b/i.test(textBeforeCursor);
      const isInSelectClause = /\bSELECT\b/i.test(textBeforeCursor) && !/\bFROM\b/i.test(textBeforeCursor);
      const isAfterJoin = /\b(JOIN|INNER\s+JOIN|LEFT\s+JOIN|RIGHT\s+JOIN|FULL\s+JOIN|OUTER\s+JOIN)\s+$/i.test(textBeforeCursor);
      const isInGroupByClause = /\bGROUP\s+BY\b/i.test(textBeforeCursor) && !/\bHAVING\b|\bORDER\s+BY\b|\bLIMIT\b/i.test(textBeforeCursor);
      const isInOrderByClause = /\bORDER\s+BY\b/i.test(textBeforeCursor) && !/\bLIMIT\b/i.test(textBeforeCursor);

      // Create suggestions array
      let suggestions = [];

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
          { label: 'WHERE', kind: monaco.languages.CompletionItemKind.Keyword, insertText: 'WHERE ', range, sortText: '1-where' },
          { label: 'GROUP BY', kind: monaco.languages.CompletionItemKind.Keyword, insertText: 'GROUP BY ', range, sortText: '1-groupby' },
          { label: 'ORDER BY', kind: monaco.languages.CompletionItemKind.Keyword, insertText: 'ORDER BY ', range, sortText: '1-orderby' },
          { label: 'LIMIT', kind: monaco.languages.CompletionItemKind.Keyword, insertText: 'LIMIT ', range, sortText: '1-limit' },
          { label: 'JOIN', kind: monaco.languages.CompletionItemKind.Keyword, insertText: 'JOIN ', range, sortText: '1-join' },
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
              insertText: field,
              range,
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
            { label: 'IN', kind: monaco.languages.CompletionItemKind.Operator, insertText: ' IN ()', range, sortText: '1-in', command: { id: 'editor.action.triggerSuggest' } },
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
            command: { id: "editor.action.triggerParameterHints" }
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
            command: { id: "editor.action.triggerParameterHints" }
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

      return {
        suggestions,
        incomplete: false,
      };
    },
    // Trigger on relevant characters
    triggerCharacters: [" ", ".", ",", "(", "=", ">", "<"],
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

      let suggestions = [];
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
        suggestions,
        incomplete,
      };
    },
    triggerCharacters: ["=", "!", ">", "<", "~", " ", "."],
  });
  
  // Add to the tracked providers
  activeProviders.push(provider);
  return provider;
};

// Editor setup
const handleMount = (editor: any) => {
  editorRef.value = editor;

  // Register only the completion provider for the active language
  if (activeTab.value === "logchefql") {
    registerLogchefQLCompletionProvider();
  } else {
    registerSQLCompletionProvider();
  }

  // Set placeholder
  editor.updateOptions({
    placeholder: props.placeholder || "Enter your query here..."
  });

  // Update editor options based on current mode
  updateEditorOptions(editor);

  // Add keyboard shortcuts
  editor.addAction({
    id: "submit",
    label: "Run Query",
    keybindings: [
      monaco.KeyMod.chord(monaco.KeyMod.CtrlCmd | monaco.KeyCode.Enter),
    ],
    run: () => {
      submitQuery();
    },
  });

  // Focus handling
  editor.onDidFocusEditorWidget(() => {
    editorFocused.value = true;
  });

  editor.onDidBlurEditorWidget(() => {
    editorFocused.value = false;
  });

  // Focus editor on mount
  nextTick(() => {
    editor.focus();
  });
};

// Helper function to update editor options based on mode
function updateEditorOptions(editor: any) {
  const baseOptions = getDefaultMonacoOptions();

  if (activeTab.value === 'logchefql') {
    // Single-line friendly options for LogchefQL
    editor.updateOptions({
      ...baseOptions,
      lineNumbers: 'off',
      glyphMargin: false,
      folding: false,
      lineDecorationsWidth: 0,
      lineNumbersMinChars: 0,
      wordWrap: 'off',
      padding: { top: 8, bottom: 8 },
    });
  } else {
    // More space for SQL queries
    editor.updateOptions({
      ...baseOptions,
      lineNumbers: 'on',
      folding: true,
      wordWrap: 'on',
      padding: { top: 6, bottom: 6 },
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

// Import the SQL AST parser
import { SQLParser } from '@/utils/clickhouse-sql/ast';

// Watch for LogchefQL code changes to update SQL code when appropriate
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
          let baseQuery = getDefaultSQLQuery(
            props.tableName, 
            props.tsField,
            props.startTimestamp,
            props.endTimestamp
          );
          
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

// Watch for timestamp changes to update SQL queries with new time ranges
watch([() => props.startTimestamp, () => props.endTimestamp], ([newStart, newEnd], [oldStart, oldEnd]) => {
  // Only update if we're in SQL mode and the timestamps have changed
  if (activeTab.value === "clickhouse-sql" && 
      (newStart !== oldStart || newEnd !== oldEnd) && 
      sqlCode.value) {
    
    // Use our AST parser for more robust updating
    const startTimeIso = new Date(newStart * 1000).toISOString();
    const endTimeIso = new Date(newEnd * 1000).toISOString();
    
    try {
      // Parse the current SQL
      const parsedQuery = SQLParser.parse(sqlCode.value, props.tsField);
      
      if (parsedQuery) {
        // Apply new time range
        const updatedQuery = SQLParser.applyTimeRange(
          parsedQuery, 
          props.tsField, 
          startTimeIso, 
          endTimeIso
        );
        
        // Convert back to SQL
        sqlCode.value = SQLParser.toSQL(updatedQuery);
        
        // Update the code value if we're currently in SQL mode
        if (activeTab.value === "clickhouse-sql") {
          code.value = sqlCode.value;
        }
      } else {
        // Fallback to regex approach if parsing fails
        const numericTimestampRegex = new RegExp(`${props.tsField}\\s+BETWEEN\\s+\\d+\\s+AND\\s+\\d+`, 'i');
        const stringTimestampRegex = new RegExp(`${props.tsField}\\s+BETWEEN\\s+'[^']+'\\s+AND\\s+'[^']+'`, 'i');
        
        // Update the timestamp values
        if (numericTimestampRegex.test(sqlCode.value)) {
          sqlCode.value = sqlCode.value.replace(
            numericTimestampRegex,
            `${props.tsField} BETWEEN '${startTimeIso}' AND '${endTimeIso}'`
          );
        } else if (stringTimestampRegex.test(sqlCode.value)) {
          sqlCode.value = sqlCode.value.replace(
            stringTimestampRegex,
            `${props.tsField} BETWEEN '${startTimeIso}' AND '${endTimeIso}'`
          );
        }
          
        // Update the code value if we're currently in SQL mode
        if (activeTab.value === "clickhouse-sql") {
          code.value = sqlCode.value;
        }
      }
    } catch (error) {
      console.error('Error updating SQL with AST:', error);
      // Continue with existing code if AST approach fails
    }
  }
});

// Also watch for limit changes
watch([() => props.limit], ([newLimit], [oldLimit]) => {
  if (activeTab.value === "clickhouse-sql" && 
      newLimit !== oldLimit && 
      sqlCode.value && 
      typeof newLimit === 'number') {
    
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
    } catch (error) {
      console.error('Error updating SQL limit with AST:', error);
    }
  }
});

// Clean up when component is unmounted
onBeforeUnmount(() => {
  disposeAllProviders();
});
</script>

