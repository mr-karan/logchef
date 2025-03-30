<template>
  <div class="query-editor">
    <!-- Header Bar (Keep existing structure) -->
    <div class="flex items-center justify-between bg-muted/40 rounded-t-md px-3 py-1.5 border border-b-0">
      <div class="flex items-center gap-3">
        <!-- Fields Panel Toggle -->
        <button class="p-1 text-muted-foreground hover:text-foreground flex items-center"
          @click="$emit('toggle-fields')" :title="props.showFieldsPanel ? 'Hide fields panel' : 'Show fields panel'"
          aria-label="Toggle fields panel">
          <PanelRightClose v-if="props.showFieldsPanel" class="h-4 w-4" />
          <PanelRightOpen v-else class="h-4 w-4" />
        </button>

        <!-- Tabs for Mode Switching -->
        <Tabs :model-value="props.activeMode"
          @update:model-value="(value: string | number) => $emit('update:activeMode', asEditorMode(value))"
          class="w-auto">
          <TabsList class="h-8">
            <TabsTrigger value="logchefql">LogchefQL</TabsTrigger>
            <TabsTrigger value="clickhouse-sql">SQL</TabsTrigger>
          </TabsList>
        </Tabs>

        <!-- New: Active Query Indicator -->
        <div v-if="activeSavedQueryName"
          class="flex items-center bg-primary/10 text-primary text-xs font-medium px-2 py-0.5 rounded-md">
          <FileEdit class="h-3.5 w-3.5 mr-1.5" />
          <span>{{ activeSavedQueryName }}</span>
        </div>
      </div>

      <div class="flex items-center gap-2">
        <!-- New: New Query Button - Only show when editing a saved query -->
        <TooltipProvider v-if="isEditingExistingQuery">
          <Tooltip>
            <TooltipTrigger asChild>
              <Button variant="outline" size="sm" class="h-7 gap-1" @click="handleNewQueryClick">
                <FilePlus2 class="h-3.5 w-3.5" />
                <span class="text-xs">New</span>
              </Button>
            </TooltipTrigger>
            <TooltipContent side="bottom">
              <p>Create a new query</p>
            </TooltipContent>
          </Tooltip>
        </TooltipProvider>

        <!-- Saved Queries Dropdown -->
        <SavedQueriesDropdown :selected-source-id="props.sourceId" :selected-team-id="props.teamId"
          @select-saved-query="(query) => $emit('select-saved-query', query)" @save="$emit('save-query')" class="h-8" />

        <!-- Table name indicator -->
        <div v-if="props.activeMode === 'clickhouse-sql'" class="text-xs text-muted-foreground mr-2">
          <template v-if="props.tableName">
            <span class="mr-1">Table:</span>
            <code class="bg-muted px-1.5 py-0.5 rounded text-xs">{{ props.tableName }}</code>
          </template>
          <span v-else class="italic text-orange-500">No table selected</span>
        </div>

        <!-- Clear button -->
        <button class="p-1 text-muted-foreground hover:text-destructive flex items-center text-xs gap-1"
          @click="clearEditor" title="Clear editor content" aria-label="Clear editor content">
          <span>Clear</span>
          <XCircle class="h-3 w-3" />
        </button>

        <!-- Help Icon -->
        <HoverCard :open-delay="200">
          <HoverCardTrigger as-child>
            <button class="p-1 text-muted-foreground hover:text-foreground" aria-label="Show syntax help">
              <HelpCircle class="h-4 w-4" />
            </button>
          </HoverCardTrigger>
          <HoverCardContent class="w-80 backdrop-blur-md bg-card text-card-foreground border-border shadow-lg"
            side="bottom" align="end">
            <!-- Help Content (Keep existing template) -->
            <div class="space-y-2">
              <h4 class="text-sm font-semibold">{{ props.activeMode === 'logchefql' ? 'LogchefQL' : 'SQL' }} Syntax</h4>
              <div v-if="props.activeMode === 'logchefql'" class="text-xs space-y-1.5">
                <div><code class="bg-muted px-1 rounded">field="value"</code> - Exact match</div>
                <div><code class="bg-muted px-1 rounded">field!="value"</code> - Not equal</div>
                <div><code class="bg-muted px-1 rounded">field~"pattern"</code> - Regex match</div>
                <div><code class="bg-muted px-1 rounded">field!~"pattern"</code> - Regex exclusion</div>
                <div><code class="bg-muted px-1 rounded">field>100</code> - Comparison</div>
                <div><code class="bg-muted px-1 rounded">(c1 and c2) or c3</code> - Grouping</div>
                <div class="pt-1"><em>Example: <code
                      class="bg-muted px-1 rounded">level="error" and status>=500</code></em>
                </div>
              </div>
              <div v-else class="text-xs space-y-1.5">
                <div><code class="bg-muted px-1 rounded">SELECT count() FROM {{ tableName || 'table' }}</code></div>
                <div><code class="bg-muted px-1 rounded">WHERE field = 'value' AND time > now() - interval 1 hour</code>
                </div>
                <div><code class="bg-muted px-1 rounded">GROUP BY user ORDER BY count() DESC</code></div>
                <div class="pt-1"><em>Time range & limit applied if not specified. Use standard ClickHouse SQL.</em>
                </div>
              </div>
            </div>
          </HoverCardContent>
        </HoverCard>
      </div>
    </div>

    <!-- Monaco Editor Container -->
    <div class="editor-container border rounded-b-md overflow-hidden relative" :class="{
      'ring-1 ring-primary/50 border-primary/50': editorFocused,
      'is-empty': isEditorEmpty
    }" :style="{ height: `${editorHeight}px` }" :data-placeholder="currentPlaceholder">
      <!-- Monaco Editor Component -->
      <vue-monaco-editor v-model:value="editorContent" :theme="theme" :language="props.activeMode"
        :options="monacoOptions" @mount="handleMount" @update:value="handleEditorChange"
        class="h-full w-full absolute inset-0" />
    </div>

    <!-- Error Message Display -->
    <div v-if="validationError"
      class="mt-2 p-2 text-sm text-destructive bg-destructive/10 rounded flex items-center gap-2">
      <AlertCircle class="h-4 w-4 flex-shrink-0" />
      <span>{{ validationError }}</span>
    </div>
  </div>
</template>

<script setup lang="ts">
import { ref, computed, shallowRef, watch, onMounted, onBeforeUnmount, nextTick, reactive, watchEffect } from "vue";
import * as monaco from "monaco-editor";
import { useDark } from "@vueuse/core";
import { VueMonacoEditor } from "@guolao/vue-monaco-editor";
import { HelpCircle, PanelRightOpen, PanelRightClose, AlertCircle, XCircle, FileEdit, FilePlus2 } from "lucide-vue-next";
import { HoverCard, HoverCardContent, HoverCardTrigger } from '@/components/ui/hover-card';
import { Tabs, TabsList, TabsTrigger } from '@/components/ui/tabs';
import SavedQueriesDropdown from '@/components/saved-queries/SavedQueriesDropdown.vue';
import type { SavedTeamQuery } from '@/api/savedQueries';
import { useRoute, useRouter } from 'vue-router';
import { Button } from '@/components/ui/button';
import { Tooltip, TooltipContent, TooltipProvider, TooltipTrigger } from '@/components/ui/tooltip';

import { initMonacoSetup, getDefaultMonacoOptions } from "@/utils/monaco";
import { Parser as LogchefQLParser, State as LogchefQLState, Operator as LogchefQLOperator, VALID_KEY_VALUE_OPERATORS as LogchefQLValidOperators, isNumeric } from "@/utils/logchefql";
import { validateLogchefQL } from "@/utils/logchefql/api"; // Simpler validation import
import { validateSQL, SQL_KEYWORDS, CLICKHOUSE_FUNCTIONS, SQL_TYPES } from "@/utils/clickhouse-sql";
import { useExploreStore } from '@/stores/explore';
// Keep other necessary imports like types...
// --- Types ---
type EditorMode = "logchefql" | "clickhouse-sql";
type EditorChangeEvent = { query: string; mode: EditorMode };
// Monaco type aliases for clarity
type MonacoEditor = monaco.editor.IStandaloneCodeEditor;
type MonacoModel = monaco.editor.ITextModel;
type MonacoDisposable = monaco.IDisposable;
type MonacoCompletionItem = monaco.languages.CompletionItem;
type MonacoPosition = monaco.Position;
type MonacoRange = monaco.IRange;


// --- Props and Emits ---
const props = defineProps({
  sourceId: { type: Number, required: true },
  schema: { type: Object as () => Record<string, { type: string }>, required: true },
  activeMode: { type: String as () => EditorMode, required: true },
  placeholder: { type: String, default: "" },
  tsField: { type: String, default: "timestamp" },
  tableName: { type: String, required: true },
  showFieldsPanel: { type: Boolean, default: false },
  // SavedQueriesDropdown props
  teamId: { type: Number, required: true },
  useCurrentTeam: { type: Boolean, default: true }
});

const emit = defineEmits<{
  (e: "change", value: EditorChangeEvent): void;
  (e: "submit", value: EditorChangeEvent): void;
  (e: "update:activeMode", value: EditorMode): void;
  (e: "toggle-fields"): void;
  // SavedQueries events
  (e: "select-saved-query", query: SavedTeamQuery): void;
  (e: "save-query"): void;
}>();

// --- Core State ---
const isDark = useDark();
const exploreStore = useExploreStore();
const editorRef = shallowRef<MonacoEditor | null>(null);
const editorContent = ref(""); // Internal state for editor value
const editorFocused = ref(false);
const validationError = ref<string | null>(null);
const isProgrammaticChange = ref(false); // Flag to prevent update loops
const isDisposing = ref(false); // Flag to prevent operations during disposal
const activeDisposables = ref<MonacoDisposable[]>([]); // Track all disposables

// --- Computed Properties ---
const theme = computed(() => (isDark.value ? "logchef-dark" : "logchef-light"));
const fieldNames = computed(() => Object.keys(props.schema ?? {}));
const isEditorEmpty = computed(() => !editorContent.value?.trim());

const currentPlaceholder = computed(() => {
  // Use prop placeholder if provided, otherwise generate default
  if (props.placeholder) return props.placeholder;

  return props.activeMode === 'logchefql'
    ? 'Enter LogchefQL query (e.g., level="error" and status>400)'
    : `Enter ClickHouse SQL query (e.g., SELECT * FROM ${props.tableName || 'your_table'} WHERE ...)`;
});

const editorHeight = computed(() => {
  const content = editorContent.value || '';
  const lines = (content.match(/\n/g) || []).length + 1;
  const baseLineHeight = 21; // Match monaco options
  const padding = 16; // Match monaco options (top + bottom)
  const minHeight = props.activeMode === 'logchefql' ? 45 : 90;
  // Calculate height based on lines, ensuring minHeight is respected
  // Add a small buffer for better spacing, especially for single line
  const calculatedHeight = padding + (lines * baseLineHeight) + (lines > 1 ? 0 : 4);
  return Math.max(minHeight, calculatedHeight);
});

// Reactive Monaco options (base + dynamic updates)
const monacoOptions = reactive(getDefaultMonacoOptions());

// --- Editor Lifecycle & Handling ---
const handleMount = (editor: MonacoEditor) => {
  if (isDisposing.value) return; // Prevent setup if disposing

  console.log("QueryEditor: Mounting Monaco instance");
  editorRef.value = editor;
  initMonacoSetup(); // Ensure themes/languages are registered (runs once internally)

  // Add Focus/Blur Listeners
  activeDisposables.value.push(
    editor.onDidFocusEditorWidget(() => editorFocused.value = true),
    editor.onDidBlurEditorWidget(() => editorFocused.value = false)
  );

  // Add Submit Shortcut
  activeDisposables.value.push(
    editor.addAction({
      id: "submit-query",
      label: "Run Query",
      keybindings: [monaco.KeyMod.CtrlCmd | monaco.KeyCode.Enter],
      run: submitQuery,
    })
  );

  // Initial setup is handled by watchEffect which runs immediately

  // Attempt initial focus
  focusEditor(true);
};

const handleEditorChange = (value: string | undefined) => {
  if (isProgrammaticChange.value || isDisposing.value) {
    return; // Prevent feedback loop or changes during disposal
  }

  const currentQuery = value ?? "";
  editorContent.value = currentQuery; // Update internal state FIRST

  // Update store based on current mode
  if (props.activeMode === 'logchefql') {
    exploreStore.setLogchefqlCode(currentQuery);
  } else {
    exploreStore.setRawSql(currentQuery);
  }

  // Clear validation errors on manual input
  validationError.value = null;

  // Emit change event
  emit("change", { query: currentQuery, mode: props.activeMode });
};

// Function to programmatically update editor content (e.g., from store or clear)
const runProgrammaticUpdate = (newValue: string) => {
  if (!editorRef.value || isDisposing.value) return;

  isProgrammaticChange.value = true;
  editorContent.value = newValue; // Update internal state
  editorRef.value.setValue(newValue); // Update Monaco instance

  // Release the flag after the update is likely processed
  nextTick(() => {
    isProgrammaticChange.value = false;
  });
};

// --- Synchronization and Option Updates ---
watchEffect(() => {
  // 1. Sync content from store if it differs from internal state
  const storeValue = props.activeMode === 'logchefql'
    ? exploreStore.logchefqlCode
    : exploreStore.rawSql;
  const valueToSet = storeValue ?? "";

  // Force update on edge cases
  if (editorContent.value !== valueToSet) {
    console.log(`QueryEditor: Updating content from store, mode=${props.activeMode}`);
    runProgrammaticUpdate(valueToSet);
  }

  // 2. Update editor options and language if editor instance exists
  const editor = editorRef.value;
  if (editor && !isDisposing.value) {
    let languageChanged = false; // Flag to track if mode changed
    const model = editor.getModel();
    if (model && model.getLanguageId() !== props.activeMode) {
      monaco.editor.setModelLanguage(model, props.activeMode);
      console.log(`QueryEditor: Language set to ${props.activeMode}`);
      languageChanged = true; // Set flag if language was updated
    }

    updateMonacoOptions(); // Update options like folding, line numbers, etc.
    registerCompletionProvider(); // Update syntax highlighting/completion

    // Refocus editor and move cursor *only* if the language actually changed
    if (languageChanged) {
      focusEditor(true);
    }
  }
});

// Watch for schema changes to update completion providers
watch(() => props.schema, () => {
  if (editorRef.value && !isDisposing.value) {
    console.log("QueryEditor: Schema changed, re-registering completions.");
    registerCompletionProvider();
  }
}, { deep: true });


// Watch for loading state to make editor read-only
watch(() => exploreStore.isLoadingOperation('executeQuery'), (isLoading) => {
  if (editorRef.value && !isDisposing.value) {
    editorRef.value.updateOptions({ readOnly: isLoading });
  }
});

// Add this watch effect after the existing watchEffect to position cursor when a saved query is loaded
watch(() => exploreStore.selectedQueryId, (newQueryId, oldQueryId) => {
  if (newQueryId && newQueryId !== oldQueryId) {
    // When a new saved query is loaded, position cursor at the end of content and focus editor
    console.log("QueryEditor: Saved query loaded, positioning cursor at end");
    nextTick(() => {
      // Add a slight delay to ensure content is fully updated
      setTimeout(() => {
        focusEditor(true); // true = position cursor at end
      }, 100);
    });
  }
});

// --- Monaco Options Update ---
const updateMonacoOptions = () => {
  const editor = editorRef.value;
  if (!editor || isDisposing.value) return;

  const isSqlMode = props.activeMode === 'clickhouse-sql';

  // Update reactive options object
  Object.assign(monacoOptions, {
    ...getDefaultMonacoOptions(), // Start with base defaults
    folding: isSqlMode,
    lineNumbers: isSqlMode ? "on" : "off",
    wordWrap: isSqlMode ? "on" : "off", // Enable word wrap for SQL
    glyphMargin: isSqlMode,
    lineDecorationsWidth: isSqlMode ? 10 : 0,
    lineNumbersMinChars: isSqlMode ? 3 : 0,
    padding: { top: isSqlMode ? 6 : 8, bottom: isSqlMode ? 6 : 8 },
    // Placeholder is now handled by CSS, remove from options
    // 'placeholder': currentPlaceholder.value, // NO LONGER NEEDED HERE
  });

  // Apply the updated options to the editor instance
  // Use nextTick to ensure model changes (like language) are potentially processed
  nextTick(() => {
    if (editor === editorRef.value && !isDisposing.value) {
      editor.updateOptions(monacoOptions);
      // Force layout recalculation in case options affect dimensions
      editor.layout();
    }
  });
};

// --- Completion Providers ---
const activeCompletionProvider = ref<MonacoDisposable | null>(null);

const registerCompletionProvider = () => {
  // Dispose previous provider if it exists
  if (activeCompletionProvider.value) {
    activeCompletionProvider.value.dispose();
    activeCompletionProvider.value = null;
  }

  let provider: MonacoDisposable | null = null;
  if (props.activeMode === 'logchefql') {
    provider = registerLogchefQLCompletionProvider();
  } else {
    provider = registerSQLCompletionProvider();
  }

  if (provider) {
    activeCompletionProvider.value = provider;
    // Note: We don't add this to activeDisposables because we manage it separately
    // to avoid disposing it with general listeners on unmount if not needed.
    // It will be disposed when the mode changes or on final unmount.
  }
};

// --- Actions ---
const submitQuery = () => {
  const currentContent = editorContent.value;
  validationError.value = null; // Clear previous error

  try {
    let isValid = true;
    if (currentContent.trim()) { // Only validate non-empty queries
      if (props.activeMode === 'logchefql') {
        isValid = validateLogchefQL(currentContent);
        if (!isValid) validationError.value = "Invalid LogchefQL syntax.";
      } else {
        isValid = validateSQL(currentContent); // Assuming validateSQL returns boolean
        if (!isValid) validationError.value = "Invalid SQL syntax (e.g., missing SELECT/FROM or unbalanced parentheses).";
      }
    }

    if (!isValid) {
      return; // Stop if validation fails
    }

    // Update store (might be redundant if handleEditorChange already did, but ensures consistency)
    if (props.activeMode === 'logchefql') {
      if (exploreStore.logchefqlCode !== currentContent) exploreStore.setLogchefqlCode(currentContent);
    } else {
      if (exploreStore.rawSql !== currentContent) exploreStore.setRawSql(currentContent);
    }

    // Emit submit event
    emit('submit', { query: currentContent, mode: props.activeMode });

  } catch (e: any) {
    console.error("Error validating or submitting query:", e);
    validationError.value = e.message || "Error preparing query";
  }
};

const clearEditor = () => {
  // Update the store, which will trigger watchEffect to update the editor
  if (props.activeMode === 'logchefql') {
    exploreStore.setLogchefqlCode("");
  } else {
    exploreStore.setRawSql("");
  }
  validationError.value = null;

  // Focus the editor after clearing
  focusEditor();
};

const focusEditor = (revealLastPosition = false) => {
  nextTick(() => {
    // Add a slight delay to allow Monaco to fully process updates after :key change / options update
    setTimeout(() => {
      const editor = editorRef.value;
      if (editor && !isDisposing.value && document.contains(editor.getDomNode())) {
        console.log("QueryEditor: Attempting focus...");
        editor.focus();

        // Explicitly set position and reveal to ensure cursor visibility and placement
        const model = editor.getModel();
        if (model) {
          let positionToSet: monaco.Position | null = null;
          if (revealLastPosition) {
            const lineCount = model.getLineCount();
            const lastCol = model.getLineMaxColumn(lineCount);
            positionToSet = new monaco.Position(lineCount, lastCol);
          } else {
            // Try to keep current position if not revealing last
            positionToSet = editor.getPosition();
          }

          if (positionToSet) {
            try {
              editor.setPosition(positionToSet);
              // Use revealPositionInCenterIfOutsideViewport for less jarring scroll
              editor.revealPositionInCenterIfOutsideViewport(positionToSet, monaco.editor.ScrollType.Smooth);
              console.log(`QueryEditor: Focused and position set to ${positionToSet.lineNumber}:${positionToSet.column}`);
            } catch (e) {
              console.warn("QueryEditor: Error setting position/revealing:", e);
            }
          } else {
            console.log("QueryEditor: Focused, but could not determine position.");
          }
        } else {
          console.log("QueryEditor: Focused (model not available for positioning).");
        }
      } else {
        console.log("QueryEditor: Focus skipped (editor not ready, disposing, or detached).");
      }
    }, 50); // Small delay to allow Monaco to settle
  });
};


// --- Disposal ---
const safelyDisposeEditor = () => {
  if (isDisposing.value) return;
  isDisposing.value = true;
  console.log('QueryEditor: Starting disposal...');

  // Dispose the active completion provider
  if (activeCompletionProvider.value) {
    try {
      activeCompletionProvider.value.dispose();
    } catch (e) { console.warn("Error disposing completion provider:", e); }
    activeCompletionProvider.value = null;
  }

  // Dispose general listeners and actions
  [...activeDisposables.value].forEach(disposable => {
    try {
      disposable.dispose();
    } catch (e) { console.warn("Error disposing resource:", e); }
  });
  activeDisposables.value = [];

  const editor = editorRef.value;
  editorRef.value = null; // Clear ref immediately

  // Use a minimal timeout to ensure disposal happens after any pending rendering/updates
  setTimeout(() => {
    if (editor) {
      try {
        const model = editor.getModel();
        if (model && !model.isDisposed()) {
          // Detach model - might help prevent leaks but often not strictly necessary if editor is disposed
          // editor.setModel(null);
        }
        editor.dispose(); // Dispose the editor instance itself
        console.log('QueryEditor: Editor instance disposed.');
      } catch (e) {
        console.error("Error during editor instance disposal:", e);
      }
    }
    // Optionally reset isDisposing flag after a delay if needed for complex re-mount scenarios
    // setTimeout(() => { isDisposing.value = false; }, 100);
  }, 50); // Small delay
};

onBeforeUnmount(() => {
  safelyDisposeEditor();
});

// --- Expose ---
defineExpose({
  submitQuery,
  clearEditor,
  focus: focusEditor, // Expose focus method
  code: computed(() => editorContent.value)
});


// --- Completion Provider Logic (Simplified stubs - keep your detailed logic) ---
// Assume getSuggestionsFromList, getSchemaFieldValues, prepareSuggestionValues remain similar

const registerLogchefQLCompletionProvider = (): MonacoDisposable | null => {
  return monaco.languages.registerCompletionItemProvider("logchefql", {
    provideCompletionItems: async (model, position) => {
      const wordInfo = model.getWordUntilPosition(position);
      const range: MonacoRange = {
        startLineNumber: position.lineNumber,
        endLineNumber: position.lineNumber,
        startColumn: wordInfo.startColumn,
        endColumn: wordInfo.endColumn
      };

      const lineStartToPosition: MonacoRange = {
        startLineNumber: position.lineNumber,
        endLineNumber: position.lineNumber,
        startColumn: 1,
        endColumn: position.column
      };
      const textBeforeCursor = model.getValueInRange(lineStartToPosition);

      const parser = new LogchefQLParser();
      parser.parse(textBeforeCursor, false, true);

      let suggestions: MonacoCompletionItem[] = [];
      let incomplete = false;

      if (parser.state === LogchefQLState.KEY ||
        parser.state === LogchefQLState.INITIAL ||
        parser.state === LogchefQLState.BOOL_OP_DELIMITER) {
        if (fieldNames.value.includes(wordInfo.word)) {
          const operatorRange: MonacoRange = {
            startLineNumber: position.lineNumber,
            endLineNumber: position.lineNumber,
            startColumn: position.column,
            endColumn: position.column
          };
          suggestions = getOperatorsSuggestions(wordInfo.word, operatorRange);
        } else {
          suggestions = getKeySuggestions(range);
        }
      } else if (parser.state === LogchefQLState.EXPECT_BOOL_OP) {
        suggestions = getBooleanOperatorsSuggestions(range);
      } else if (parser.state === LogchefQLState.KEY_VALUE_OPERATOR) {
        const valueRange: MonacoRange = {
          startLineNumber: position.lineNumber,
          endLineNumber: position.lineNumber,
          startColumn: wordInfo.startColumn,
          endColumn: wordInfo.endColumn
        };
        const valueResult = await getValueSuggestions(parser.key, parser.value, valueRange);
        suggestions = valueResult.suggestions;
        incomplete = valueResult.incomplete;
      }

      return { suggestions, incomplete };
    },
    triggerCharacters: ["=", "!", ">", "<", "~", " ", ".", '"', "'", "("]
  });
};

const registerSQLCompletionProvider = (): MonacoDisposable | null => {
  return monaco.languages.registerCompletionItemProvider("clickhouse-sql", {
    provideCompletionItems: async (model, position) => {
      const wordInfo = model.getWordUntilPosition(position);
      const range: MonacoRange = {
        startLineNumber: position.lineNumber,
        endLineNumber: position.lineNumber,
        startColumn: wordInfo.startColumn,
        endColumn: wordInfo.endColumn
      };

      const lineStartToPosition: MonacoRange = {
        startLineNumber: position.lineNumber,
        endLineNumber: position.lineNumber,
        startColumn: 1,
        endColumn: position.column
      };
      const textBeforeCursor = model.getValueInRange(lineStartToPosition);
      let suggestions: MonacoCompletionItem[] = [];

      if (/\bFROM\s+$/i.test(textBeforeCursor) && props.tableName) {
        suggestions.push({
          label: props.tableName,
          kind: monaco.languages.CompletionItemKind.Folder,
          insertText: props.tableName,
          range: range,
          detail: 'Current log table'
        });
      }

      if (fieldNames.value.length > 0 &&
        (/\bSELECT\s+(?:[\w,\s*]+\s+)?$|\bWHERE\s+(?:.+?\s+)?$/i.test(textBeforeCursor))) {
        suggestions = suggestions.concat(
          fieldNames.value.map(field => ({
            label: field,
            kind: monaco.languages.CompletionItemKind.Field,
            insertText: field,
            range: range,
            detail: props.schema[field]?.type || 'unknown'
          }))
        );
      }

      const typedPrefix = wordInfo.word.toUpperCase();
      suggestions = suggestions.concat(
        getSuggestionsFromList({
          items: SQL_KEYWORDS.filter(kw => kw.startsWith(typedPrefix))
            .map(kw => ({ label: kw, insertText: kw + ' ' })),
          kind: monaco.languages.CompletionItemKind.Keyword,
          range: range
        })
      );

      return { suggestions };
    },
    triggerCharacters: [" ", "\n", ".", "(", ","]
  });
};


// --- Helper functions for completions (Keep your existing logic) ---
const getSuggestionsFromList = (params: {
  items: any[],
  kind: monaco.languages.CompletionItemKind,
  range: MonacoRange,
  postfix?: string
}): MonacoCompletionItem[] => {
  const suggestions: MonacoCompletionItem[] = [];
  const defaultPostfix = params.postfix ?? "";
  const range = params.range;

  // Validate range
  if (!range || typeof range.startLineNumber !== 'number' ||
    typeof range.startColumn !== 'number' ||
    typeof range.endLineNumber !== 'number' ||
    typeof range.endColumn !== 'number') {
    console.error("Invalid range provided to getSuggestionsFromList:", range);
    return [];
  }

  for (const item of params.items) {
    let label: string;
    let insertText: string;
    let sortText: string;
    let documentation: string | undefined;

    if (typeof item === 'string') {
      label = item;
      insertText = item;
      sortText = item.toLowerCase();
    } else {
      label = item.label;
      insertText = item.insertText ?? label;
      sortText = item.sortText ?? label.toLowerCase();
      documentation = item.documentation;
    }

    suggestions.push({
      label,
      kind: params.kind,
      insertText: insertText + defaultPostfix,
      range: range,
      sortText,
      documentation,
      command: { id: "editor.action.triggerSuggest", title: "Trigger Suggest" },
      detail: documentation ? ' ' : undefined,
      insertTextRules: monaco.languages.CompletionItemInsertTextRule.InsertAsSnippet
    });
  }
  return suggestions;
};

const getKeySuggestions = (range: MonacoRange): MonacoCompletionItem[] => {
  return fieldNames.value.map(name => ({
    label: name,
    kind: monaco.languages.CompletionItemKind.Field,
    insertText: name,
    range: range,
    detail: props.schema[name]?.type || 'Unknown',
    sortText: `0-${name}`,
    command: { id: "editor.action.triggerSuggest", title: "Trigger Suggest" }
  }));
};

const getBooleanOperatorsSuggestions = (range: MonacoRange): MonacoCompletionItem[] => {
  return [
    {
      label: 'and',
      kind: monaco.languages.CompletionItemKind.Keyword,
      insertText: 'and ',
      range: range,
      sortText: '1-and'
    },
    {
      label: 'or',
      kind: monaco.languages.CompletionItemKind.Keyword,
      insertText: 'or ',
      range: range,
      sortText: '1-or'
    }
  ];
};

const getSchemaFieldValues = (field: string): string[] => { // Example implementation
  const sampleValues: Record<string, string[]> = {
    level: ["info", "error", "warning", "debug"],
    status: ["200", "404", "500"],
    // ... add more based on your schema/data
  };
  return sampleValues[field] || [];
};

const prepareSuggestionValues = (items: string[], quoteChar?: string): { label: string, insertText?: string }[] => {
  // ... your existing implementation ...
  const needsQuotes = quoteChar !== undefined;
  const q = quoteChar || '"'; // Default to double quotes if needed but not specified
  return items.map(item => {
    if (!needsQuotes && isNumeric(item)) {
      return { label: item }; // Numeric values don't need quotes unless context demands it
    } else {
      // Escape existing quotes within the value
      const escapedValue = item.replace(new RegExp(`\\${q}`, 'g'), `\\${q}`);
      return {
        label: item,
        insertText: `${q}${escapedValue}${q}`
      };
    }
  });
};

const getValueSuggestions = async (
  key: string,
  currentValue: string,
  range: MonacoRange,
  quoteChar?: string
): Promise<{ suggestions: MonacoCompletionItem[], incomplete: boolean }> => {
  const fieldValues = getSchemaFieldValues(key);
  const filteredValues = fieldValues.filter(v =>
    v.toLowerCase().includes(currentValue?.toLowerCase() || ''));
  const preparedItems = prepareSuggestionValues(filteredValues, quoteChar);

  const suggestions = getSuggestionsFromList({
    items: preparedItems,
    kind: monaco.languages.CompletionItemKind.Value,
    range: range,
    postfix: ' '
  });

  return { suggestions, incomplete: false };
};

const getOperatorsSuggestions = (key: string, range: MonacoRange): MonacoCompletionItem[] => {
  const operators = [
    { label: '=', insertText: '=' },
    { label: '!=', insertText: '!=' },
    { label: '>', insertText: '>' },
    { label: '<', insertText: '<' },
    { label: '>=', insertText: '>=' },
    { label: '<=', insertText: '<=' },
    { label: '~', insertText: '~' },
    { label: '!~', insertText: '!~' }
  ];

  // Filter operators based on field type if needed
  const fieldType = props.schema[key]?.type?.toLowerCase() || '';
  const filteredOperators = operators.filter(op => {
    // For numeric fields, only allow comparison operators
    if (fieldType === 'number' || fieldType === 'integer') {
      return ['=', '!=', '>', '<', '>=', '<='].includes(op.label);
    }
    // For string fields, allow all operators
    return true;
  });

  return getSuggestionsFromList({
    items: filteredOperators.map(op => ({
      ...op,
      insertText: op.insertText + ' ', // Add space after operator
      documentation: `${key} ${op.label} value`
    })),
    kind: monaco.languages.CompletionItemKind.Operator,
    range: range,
    postfix: ''
  });
};

// Add type assertion function
const asEditorMode = (value: string | number): EditorMode => {
  if (value === 'logchefql' || value === 'clickhouse-sql') {
    return value;
  }
  console.warn(`Received unexpected value for editor mode: ${value}. Defaulting to logchefql.`);
  return 'logchefql';
};

// After the imports, add route and router
const route = useRoute();
const router = useRouter();

// Add these computed properties after other computed properties
const activeSavedQueryName = computed(() => exploreStore.activeSavedQueryName);
const isEditingExistingQuery = computed(() => !!route.query.query_id);

// Add handler for New Query button
const handleNewQueryClick = () => {
  console.log("Creating new query state...");

  // First, copy the current query parameters
  const currentQuery = { ...route.query };

  // Remove query_id from URL
  delete currentQuery.query_id;

  // Use the centralized reset function in the store
  exploreStore.resetQueryStateToDefault();

  // Wait for the state reset to complete
  nextTick(() => {
    // Explicitly remove the query_id parameter again to ensure it's gone
    const finalQuery = { ...currentQuery };
    delete finalQuery.query_id;

    // Replace URL with new parameters in router, disabling any redirect
    // This is important to prevent the URL sync from adding back query_id
    router.replace({ query: finalQuery })
      .then(() => {
        // Focus the editor and position cursor at the end after URL change is complete
        setTimeout(() => {
          focusEditor(true); // true = position cursor at end
        }, 50);
      })
      .catch(err => {
        console.error("Error updating URL:", err);
        // Still try to focus even if URL update fails
        focusEditor(true);
      });
  });
};

</script>

<style scoped>
.query-editor {
  display: flex;
  flex-direction: column;
}

.editor-container {
  background-color: hsl(var(--card));
  transition: border-color 0.15s ease, box-shadow 0.15s ease;
  min-height: 45px;
  /* Default min height */
  position: relative;
  /* Crucial for ::before positioning */
}

/* NEW: CSS Placeholder Implementation */
.editor-container.is-empty::before {
  content: attr(data-placeholder);
  /* Read placeholder text from data attribute */
  position: absolute;
  top: 8px;
  /* Adjust to match editor's top padding */
  left: 5px;
  /* Adjust to match Monaco's text starting position */
  font-family: ui-monospace, SFMono-Regular, Menlo, Monaco, Consolas, "Liberation Mono", "Courier New", monospace;
  /* Match editor font */
  font-size: 14px;
  /* Match editor font size */
  color: hsl(var(--muted-foreground));
  opacity: 0.6;
  pointer-events: none;
  /* Allow clicks to pass through */
  white-space: pre-wrap;
  /* Allow multiline placeholders */
  padding-right: 10px;
  /* Prevent text overlapping with cursor */
  z-index: 1;
  /* Ensure it's above the editor background but below text */
  max-width: calc(100% - 10px);
  /* Prevent overflow */
}

/* Ensure editor itself is positioned correctly within container */
.editor-container> :deep(.monaco-editor-container) {
  position: absolute;
  inset: 0;
  width: 100%;
  height: 100%;
}

/* Ensure cursor visibility (especially when empty) */
:deep(.monaco-editor .cursor) {
  visibility: visible !important;
  /* You might need blink animation if default doesn't work */
}

/* Shadcn focus styling */
button:focus-visible {
  outline: 2px solid hsl(var(--ring));
  outline-offset: 2px;
  border-radius: var(--radius);
}

code {
  font-family: inherit;
}
</style>
