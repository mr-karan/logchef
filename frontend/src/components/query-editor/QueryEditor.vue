<template>
  <div class="query-editor">
    <!-- Header Bar (Keep existing structure) -->
    <div
      class="flex items-center justify-between bg-muted/40 rounded-t-md px-3 py-1.5 border border-b-0"
    >
      <div class="flex items-center gap-3">
        <!-- Fields Panel Toggle -->
        <button
          class="p-1 text-muted-foreground hover:text-foreground flex items-center"
          @click="$emit('toggle-fields')"
          :title="
            props.showFieldsPanel ? 'Hide fields panel' : 'Show fields panel'
          "
          aria-label="Toggle fields panel"
        >
          <PanelRightClose v-if="props.showFieldsPanel" class="h-4 w-4" />
          <PanelRightOpen v-else class="h-4 w-4" />
        </button>

        <!-- Tabs for Mode Switching -->
        <Tabs
          :model-value="props.activeMode"
          @update:model-value="(value: string | number) => $emit('update:activeMode', asEditorMode(value), true)"
          class="w-auto"
        >
          <TabsList class="grid grid-cols-2 w-fit">
            <TabsTrigger value="logchefql">
              <div class="flex-fix">
                <Search class="w-4 h-4" />
                <span>Search</span>
              </div>
            </TabsTrigger>
            <TabsTrigger value="clickhouse-sql">
              <div class="flex-fix">
                <Code2 class="w-4 h-4" />
                <span>SQL</span>
              </div>
            </TabsTrigger>
          </TabsList>
        </Tabs>

        <!-- Table name indicator (Moved & Always Visible) -->
        <div class="text-xs text-muted-foreground ml-3">
          <template v-if="props.tableName">
            <span class="mr-1">Table:</span>
            <code class="bg-muted px-1.5 py-0.5 rounded text-xs">{{
              props.tableName
            }}</code>
          </template>
          <span v-else class="italic text-orange-500">No table selected</span>
        </div>

        <!-- New: Active Query Indicator -->
        <div
          v-if="activeSavedQueryName"
          class="flex items-center bg-primary/10 text-primary text-xs font-medium px-2 py-0.5 rounded-md ml-3"
        >
          <FileEdit class="h-3.5 w-3.5 mr-1.5" />
          <span>{{ activeSavedQueryName }}</span>
        </div>
      </div>

      <div class="flex items-center gap-2">
        <!-- New: New Query Button - Only show when editing a saved query -->
        <TooltipProvider v-if="isEditingExistingQuery">
          <Tooltip>
            <TooltipTrigger asChild>
              <Button
                variant="outline"
                size="sm"
                class="h-7 gap-1"
                @click="handleNewQueryClick"
              >
                <FilePlus2 class="h-3.5 w-3.5" />
                <span class="text-xs">New</span>
              </Button>
            </TooltipTrigger>
            <TooltipContent side="bottom">
              <p>Create a new query</p>
            </TooltipContent>
          </Tooltip>
        </TooltipProvider>

        <!-- SQL Toggle Button - Only show when in SQL mode -->
        <TooltipProvider v-if="props.activeMode === 'clickhouse-sql'">
          <Tooltip>
            <TooltipTrigger asChild>
              <Button
                variant="outline"
                size="sm"
                class="h-7 gap-1"
                @click="toggleSqlEditorVisibility"
              >
                <EyeOff v-if="isEditorVisible" class="h-3.5 w-3.5" />
                <Eye v-else class="h-3.5 w-3.5" />
                <span class="text-xs"
                  >{{ isEditorVisible ? "Hide" : "Show" }} SQL</span
                >
              </Button>
            </TooltipTrigger>
            <TooltipContent side="bottom">
              <p>{{ isEditorVisible ? "Hide" : "Show" }} SQL query editor</p>
            </TooltipContent>
          </Tooltip>
        </TooltipProvider>

        <!-- Saved Queries Dropdown -->
        <SavedQueriesDropdown
          :selected-source-id="props.sourceId"
          :selected-team-id="props.teamId"
          @select-saved-query="(query: SavedTeamQuery) => $emit('select-saved-query', query)"
          @save="$emit('save-query')"
          class="h-8"
        />

        <!-- Help Icon -->
        <HoverCard :open-delay="200">
          <HoverCardTrigger as-child>
            <button
              class="p-1 text-muted-foreground hover:text-foreground"
              aria-label="Show syntax help"
            >
              <HelpCircle class="h-4 w-4" />
            </button>
          </HoverCardTrigger>
          <HoverCardContent
            class="w-80 backdrop-blur-md bg-card text-card-foreground border-border shadow-lg"
            side="bottom"
            align="end"
          >
            <!-- Help Content (Keep existing template) -->
            <div class="space-y-2">
              <h4 class="text-sm font-semibold">
                {{ props.activeMode === "logchefql" ? "LogchefQL" : "SQL" }}
                Syntax
              </h4>
              <div
                v-if="props.activeMode === 'logchefql'"
                class="text-xs space-y-1.5"
              >
                <div>
                  <code class="bg-muted px-1 rounded">field="value"</code> -
                  Exact match
                </div>
                <div>
                  <code class="bg-muted px-1 rounded">field!="value"</code> -
                  Not equal
                </div>
                <div>
                  <code class="bg-muted px-1 rounded">field~"pattern"</code> -
                  Regex match
                </div>
                <div>
                  <code class="bg-muted px-1 rounded">field!~"pattern"</code> -
                  Regex exclusion
                </div>
                <div>
                  <code class="bg-muted px-1 rounded">field>100</code> -
                  Comparison
                </div>
                <div>
                  <code class="bg-muted px-1 rounded">(c1 and c2) or c3</code> -
                  Grouping
                </div>
                <div class="pt-1">
                  <em
                    >Example:
                    <code class="bg-muted px-1 rounded"
                      >level="error" and status>=500</code
                    ></em
                  >
                </div>
              </div>
              <div v-else class="text-xs space-y-1.5">
                <div>
                  <code class="bg-muted px-1 rounded"
                    >SELECT count() FROM {{ tableName || "table" }}</code
                  >
                </div>
                <div>
                  <code class="bg-muted px-1 rounded"
                    >WHERE field = 'value' AND time > now() - interval 1
                    hour</code
                  >
                </div>
                <div>
                  <code class="bg-muted px-1 rounded"
                    >GROUP BY user ORDER BY count() DESC</code
                  >
                </div>
                <div class="pt-1">
                  <em
                    >Time range & limit applied if not specified. Use standard
                    ClickHouse SQL.</em
                  >
                </div>
              </div>
            </div>
          </HoverCardContent>
        </HoverCard>
      </div>
    </div>

    <!-- Dynamic variable list -->
    <div class="flex flex-wrap gap-2 mb-2 bg-white">
      <div v-for="variable in allVariables" :key="variable.name" class="flex items-center">
        <input
            v-model="variable.value"
            type="text"
            :placeholder="variable.label"
            @click="() => openVariableSettings(variable)"
            class="border p-2 rounded"
        />
      </div>
    </div>

    <!-- Monaco Editor Container -->
    <div
      class="editor-wrapper"
      :class="{ 'is-focused': editorFocused }"
      v-show="isEditorVisible || props.activeMode === 'logchefql'"
    >
      <div
        class="editor-container"
        :class="{
          'is-empty': isEditorEmpty,
        }"
        :style="{ height: `${editorHeight}px` }"
        :data-placeholder="currentPlaceholder"
        :data-mode="props.activeMode"
      >
        <!-- Monaco Editor Component with stable key to prevent remounting -->
        <vue-monaco-editor
          key="monaco-editor-instance"
          v-model:value="editorContent"
          :theme="theme"
          :language="props.activeMode"
          :options="monacoOptions"
          @mount="handleMount"
          @update:value="handleEditorChange"
          class="h-full w-full"
        />
      </div>
    </div>

    <!-- SQL Preview when editor is hidden -->
    <div
      v-if="
        !isEditorVisible &&
        props.activeMode === 'clickhouse-sql' &&
        !isEditorEmpty
      "
      class="sql-preview p-3 border border-border rounded-md bg-card/60 text-sm font-mono overflow-hidden cursor-pointer dark:bg-[#111522]"
      @click="isEditorVisible = true"
    >
      <div class="flex items-center justify-between">
        <div class="text-muted-foreground text-xs font-medium mb-1">
          SQL Query (collapsed)
        </div>
        <Button
          variant="ghost"
          size="sm"
          class="h-6 px-2"
          @click.stop="isEditorVisible = true"
        >
          <Eye class="h-3.5 w-3.5 mr-1" />
          <span class="text-xs">Show</span>
        </Button>
      </div>
      <div class="truncate text-xs text-muted-foreground">
        {{ editorContent }}
      </div>
    </div>

    <!-- Error Message Display -->
    <div
      v-if="validationError"
      class="mt-2 p-2 text-sm text-destructive bg-destructive/10 rounded flex items-center gap-2"
    >
      <AlertCircle class="h-4 w-4 flex-shrink-0" />
      <span>
        <span class="font-medium">Validation Error: </span>
        {{ validationError }}
        <span
          v-if="validationError?.includes('Missing boolean operator')"
          class="block mt-1 text-xs"
        >
          Hint: Use <code class="bg-muted px-1 rounded">and</code> or
          <code class="bg-muted px-1 rounded">or</code> between conditions.
          Example:
          <code class="bg-muted px-1 rounded"
            >field1="value" and field2="value"</code
          >
        </span>
      </span>
    </div>
  </div>

  <!-- Dynamic variable drawer (edit panel) -->
  <div v-if="selectedVariable" ref="drawerRef" class="drawer z-20">
    <div class="drawer-header">
      <h3>Variable Settings</h3>
      <button @click="closeDrawer">✕</button>
    </div>

    <div class="drawer-body">
      <label>Variable name</label>
      <p class="text-[#2980b9] font-bold">{{ selectedVariable.name }}</p>

      <label>Variable type</label>
      <select v-model="selectedVariable.type" @change="setDefaultValueByType">
        <option value="text">Text</option>
        <option value="number">Number</option>
        <option value="date">Date</option>
      </select>

      <label>Filter widget label</label>
      <input v-model="selectedVariable.label" placeholder="Enter label..." />
    </div>
  </div>

</template>

<script setup lang="ts">
import {
  ref,
  computed,
  shallowRef,
  watch,
  onMounted,
  onBeforeUnmount,
  onActivated,
  onDeactivated,
  nextTick,
  reactive,
  watchEffect,
} from "vue";
import * as monaco from "monaco-editor";
import { useDark } from "@vueuse/core";
import { VueMonacoEditor } from "@guolao/vue-monaco-editor";
import {
  HelpCircle,
  PanelRightOpen,
  PanelRightClose,
  AlertCircle,
  XCircle,
  FileEdit,
  FilePlus2,
  Search,
  Code2,
  Eye,
  EyeOff,
} from "lucide-vue-next";
import {
  HoverCard,
  HoverCardContent,
  HoverCardTrigger,
} from "@/components/ui/hover-card";
import { Tabs, TabsList, TabsTrigger } from "@/components/ui/tabs";
import SavedQueriesDropdown from "@/components/collections/SavedQueriesDropdown.vue";
import type { SavedTeamQuery } from "@/api/savedQueries";
import { useRoute, useRouter } from "vue-router";
import { Button } from "@/components/ui/button";
import {
  Tooltip,
  TooltipContent,
  TooltipProvider,
  TooltipTrigger,
} from "@/components/ui/tooltip";

import {
  initMonacoSetup,
  getDefaultMonacoOptions,
  getSingleLineModeOptions,
  getOrCreateModel,
  registerEditorInstance,
  unregisterEditorInstance,
  lightweightEditorDisposal,
  reactivateEditor,
} from "@/utils/monaco";
import {
  Parser as LogchefQLParser,
  State as LogchefQLState,
  Operator as LogchefQLOperator,
  VALID_KEY_VALUE_OPERATORS as LogchefQLValidOperators,
  isNumeric,
} from "@/utils/logchefql";
import { validateLogchefQLWithDetails } from "@/utils/logchefql/api"; // Import detailed validation
import {
  validateSQLWithDetails,
  SQL_KEYWORDS,
  CLICKHOUSE_FUNCTIONS,
  SQL_TYPES,
} from "@/utils/clickhouse-sql";
import { storeToRefs } from 'pinia';
import { useExploreStore } from "@/stores/explore";
import type { VariableState as VariableSetting } from '@/stores/variables';
import { useVariableStore } from '@/stores/variables';
import { QueryService } from "@/services/QueryService";
// Keep other necessary imports like types...
// --- Types ---
type EditorMode = "logchefql" | "clickhouse-sql";
type EditorChangeEvent = {
  query: string;
  mode: EditorMode;
  isUserInput?: boolean;
};
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
  schema: {
    type: Object as () => Record<string, { type: string }>,
    required: true,
  },
  activeMode: { type: String as () => EditorMode, required: true },
  value: { type: String, default: "" },
  placeholder: { type: String, default: "" },
  tsField: { type: String, default: "timestamp" },
  tableName: { type: String, required: true },
  showFieldsPanel: { type: Boolean, default: false },
  // SavedQueriesDropdown props
  teamId: { type: Number, required: true },
  useCurrentTeam: { type: Boolean, default: true },
});

const emit = defineEmits<{
  (e: "change", value: EditorChangeEvent): void;
  (e: "submit", value: EditorChangeEvent): void;
  (e: "update:activeMode", value: EditorMode, isModeSwitchOnly?: boolean): void;
  (e: "toggle-fields"): void;
  // SavedQueries events
  (e: "select-saved-query", query: SavedTeamQuery): void;
  (e: "save-query"): void;
}>();

// --- Core State ---
const isDark = useDark();
const exploreStore = useExploreStore();
// Access variable store
const variableStore = useVariableStore();

const editorRef = shallowRef<MonacoEditor | null>(null);
const editorContent = ref(props.value || ""); // Initialize with prop value
const editorFocused = ref(false);
const validationError = ref<string | null>(null);
const isProgrammaticChange = ref(false); // Flag to prevent update loops
const isDisposing = ref(false); // Flag to prevent operations during disposal
const activeDisposables = ref<MonacoDisposable[]>([]); // Track all disposables
const isEditorVisible = ref(true); // New state for SQL editor visibility

// dynamic variables list
const { allVariables } = storeToRefs(variableStore);
// Selected variable for drawer editing
const selectedVariable = ref<VariableSetting | null>(null);
// drawerRef to hide it when user click outside of drawer
const drawerRef = ref<HTMLElement | null>(null);

// --- Computed Properties ---
const theme = computed(() => (isDark.value ? "logchef-dark" : "logchef-light"));
const fieldNames = computed(() => Object.keys(props.schema ?? {}));
const isEditorEmpty = computed(() => !editorContent.value?.trim());

const currentPlaceholder = computed(() => {
  // Use prop placeholder if provided, otherwise generate default
  if (props.placeholder) return props.placeholder;

  return props.activeMode === "logchefql"
    ? 'Enter search criteria (e.g., lvl="ERROR" and namespace~"sys")'
    : `Enter ClickHouse SQL query (e.g., SELECT * FROM ${
        props.tableName || "your_table"
      } WHERE ...)`;
});

const editorHeight = computed(() => {
  const content = editorContent.value || "";
  const lines = (content.match(/\n/g) || []).length + 1;
  const baseLineHeight = 21; // Match monaco options
  const padding = 16; // Match monaco options (top + bottom)
  const minHeight = props.activeMode === "logchefql" ? 45 : 90;
  // Calculate height based on lines, ensuring minHeight is respected
  // Add a small buffer for better spacing, especially for single line
  const calculatedHeight =
    padding + lines * baseLineHeight + (lines > 1 ? 0 : 4);
  return Math.max(minHeight, calculatedHeight);
});

// Reactive Monaco options (base + dynamic updates)
const monacoOptions = reactive(getDefaultMonacoOptions());

// --- Editor Lifecycle & Handling ---
const handleMount = (editor: MonacoEditor) => {
  if (isDisposing.value) return; // Prevent setup if disposing

  console.log("QueryEditor: Mounting editor");
  editorRef.value = editor;
  initMonacoSetup(); // Ensure themes/languages are registered (runs once internally)

  // Register editor with global management system
  registerEditorInstance(editor);

  // Create a unique model key for this editor instance
  const modelKey = `${props.activeMode}-${props.sourceId}`;
  console.log(`Creating/reusing model with key: ${modelKey}`);

  try {
    // Use cached model if available with sourceId as identifier
    const model = getOrCreateModel(
      editorContent.value,
      props.activeMode,
      props.sourceId,
      modelKey
    );

    // Set the model to the editor
    editor.setModel(model);
    console.log(`Model set successfully: ${model.id}`);
  } catch (e) {
    console.error("Error setting up editor model:", e);
    // Create a fallback model directly if cache retrieval fails
    const fallbackModel = monaco.editor.createModel(
      editorContent.value,
      props.activeMode
    );
    editor.setModel(fallbackModel);
  }

  // Check if we should be in read-only mode
  const isLoading = exploreStore.isLoadingOperation("executeQuery");
  if (isLoading) {
    editor.updateOptions({ readOnly: true });
  }

  // Add Focus/Blur Listeners
  activeDisposables.value.push(
    editor.onDidFocusEditorWidget(() => (editorFocused.value = true)),
    editor.onDidBlurEditorWidget(() => (editorFocused.value = false))
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

  // Add minimal keyboard event listener that only prevents new lines in LogchefQL mode
  const domNode = editor.getDomNode();
  if (domNode) {
    const keyHandler = (e: KeyboardEvent) => {
      // Only prevent default when:
      // 1. It's a bare Enter keypress (no modifier keys)
      // 2. We're in LogchefQL mode
      // 3. No suggestion widget is visible (to allow autocomplete acceptance)
      if (
        e.key === "Enter" &&
        !e.ctrlKey &&
        !e.metaKey &&
        !e.altKey &&
        !e.shiftKey &&
        props.activeMode === "logchefql"
      ) {
        // Check if suggestion widget is visible by looking at DOM
        const suggestWidgetVisible =
          document.querySelector(".suggest-widget.visible") !== null;

        // Only prevent new line if no suggestion widget is visible
        if (!suggestWidgetVisible) {
          e.preventDefault();
          // Don't execute query, just prevent the newline
        }
      }
    };

    domNode.addEventListener("keydown", keyHandler);

    // Add disposal for the event listener
    activeDisposables.value.push({
      dispose: () => {
        domNode.removeEventListener("keydown", keyHandler);
      },
    });
  }

  // Initial setup is handled by watchEffect which runs immediately

  // Attempt initial focus
  focusEditor(true);
};

// Handle editor changes (e.g., user typing query with {{variable}})
const handleEditorChange = (value: string | undefined) => {
  if (isProgrammaticChange.value || isDisposing.value) {
    return;
  }

  const currentQuery = value ?? "";
  editorContent.value = currentQuery;

  if (props.activeMode === "logchefql") {
    exploreStore.setLogchefqlCode(currentQuery);
  } else {
    exploreStore.setRawSql(currentQuery);
  }

  validationError.value = null;

  detectVariables(currentQuery); // detect dynamic variables and make variable list in dom

  emit("change", {
    query: currentQuery,
    mode: props.activeMode,
    isUserInput: true,
  });
};

// Function to programmatically update editor content (e.g., from store or clear)
const runProgrammaticUpdate = (newValue: string) => {
  if (!editorRef.value || isDisposing.value) return;

  isProgrammaticChange.value = true;
  editorContent.value = newValue; // Update internal state
  editorRef.value.setValue(newValue); // Update Monaco instance

  detectVariables(newValue); // detect dynamic variables and make variable list in dom

  // Release the flag after the update is likely processed
  nextTick(() => {
    isProgrammaticChange.value = false;
    // Emit change event marked as NOT user input (programmatic update)
    emit("change", {
      query: newValue,
      mode: props.activeMode,
      isUserInput: false,
    });
  });
};

const detectVariables = (value: string) => {
  if (typeof value !== 'string') return;

  // Extract dynamic variable names from query like {{ variable }}
  const matches = [...value.matchAll(/{{\s*(\w+)\s*}}/g)].map(m => m[1]);
  const uniqueVariableNames = [...new Set(matches)];

  // If allVariables is not ready, just upsert all found variables
  const currentVariables = allVariables?.value ?? [];

  const existingNames = currentVariables.map(v => v.name);

  // Remove variables that are no longer in the query
  for (const variable of currentVariables) {
    if (!uniqueVariableNames.includes(variable.name)) {
      variableStore.removeVariable(variable.name);
    }
  }

  // Add new variables that appeared in query
  for (const name of uniqueVariableNames) {
    if (!existingNames.includes(name)) {
      variableStore.upsertVariable({
        name,
        type: 'text',
        label: name,
        inputType: 'input',
        value: ''
      });
    }
  }
};

// Watch for prop value changes to update editor content
watch(
  () => props.value,
  (newValue) => {
    if (newValue !== editorContent.value) {
      runProgrammaticUpdate(newValue || "");
    }
  }
);

// --- Synchronization and Option Updates ---
watchEffect(() => {
  // 1. Sync content from store if it differs from internal state
  const storeValue =
    props.activeMode === "logchefql"
      ? exploreStore.logchefqlCode
      : exploreStore.rawSql;
  const valueToSet = storeValue ?? "";

  // Save cursor position before making changes
  const editor = editorRef.value;
  let savedPosition = editor?.getPosition() || null;
  let savedSelection = editor?.getSelection() || null;
  let shouldRestoreCursor = false;

  // Force update on edge cases
  if (editorContent.value !== valueToSet) {
    runProgrammaticUpdate(valueToSet);
    shouldRestoreCursor = true;
  }

  // 2. Update editor options and language if editor instance exists
  if (editor && !isDisposing.value) {
    let languageChanged = false; // Flag to track if mode changed
    const model = editor.getModel();
    if (model && model.getLanguageId() !== props.activeMode) {
      // Only change the language model - don't recreate the editor
      monaco.editor.setModelLanguage(model, props.activeMode);
      languageChanged = true; // Set flag if language was updated

      // Register completion provider only once per language
      if (!activeCompletionProvider.value || languageChanged) {
        registerCompletionProvider(); // Update syntax highlighting/completion
      }
    }

    // Get current loading state to ensure read-only flag is consistent
    const isLoading = exploreStore.isLoadingOperation("executeQuery");

    // Apply mode-specific options without recreating the editor
    const options = {
      ...getDefaultMonacoOptions(),
      fontSize: 13,
      lineHeight: 20,
      padding: { top: 8, bottom: 8 },
      readOnly: isLoading, // Ensure read-only state is correctly synchronized
      scrollbar: {
        vertical: "auto" as const,
        horizontal: "auto" as const,
        useShadows: false,
        verticalScrollbarSize: 8,
        horizontalScrollbarSize: 8,
      },
      minimap: { enabled: false },
      // Add mode-specific options for LogchefQL
      ...(props.activeMode === "logchefql"
        ? {
            ...getSingleLineModeOptions(),
          }
        : {
            // SQL-specific configuration - keep line numbers off
            lineNumbers: "off" as const,
            wordWrap: "on" as const,
            folding: true,
            scrollBeyondLastLine: false,
          }),
    };
    editor.updateOptions(options);

    // Refocus editor and move cursor *only* if the language actually changed
    if (languageChanged) {
      focusEditor(true);
    }
    // Otherwise, restore cursor position if we made content changes
    else if (shouldRestoreCursor && savedPosition) {
      nextTick(() => {
        if (editor && !isDisposing.value) {
          editor.setPosition(savedPosition);
          if (savedSelection) {
            editor.setSelection(savedSelection);
          }
          editor.revealPositionInCenterIfOutsideViewport(savedPosition);
        }
      });
    }
  }
});

// Watch for schema changes to update completion providers
let lastSchemaHash = "";
watch(
  () => props.schema,
  (newSchema) => {
    // Generate a simple hash to determine if schema has meaningfully changed
    const schemaKeys = Object.keys(newSchema || {})
      .sort()
      .join(",");
    if (schemaKeys === lastSchemaHash) {
      return; // Skip if schema hasn't actually changed
    }
    lastSchemaHash = schemaKeys;

    if (editorRef.value && !isDisposing.value) {
      // Use requestIdleCallback to defer non-critical operation
      if (window.requestIdleCallback) {
        window.requestIdleCallback(
          () => {
            if (editorRef.value && !isDisposing.value) {
              console.log(
                "QueryEditor: Schema changed, re-registering completions (deferred)."
              );
              registerCompletionProvider();
            }
          },
          { timeout: 1000 }
        );
      } else {
        // Fallback for browsers without requestIdleCallback
        setTimeout(() => {
          if (editorRef.value && !isDisposing.value) {
            console.log(
              "QueryEditor: Schema changed, re-registering completions (timeout)."
            );
            registerCompletionProvider();
          }
        }, 100);
      }
    }
  },
  { deep: true, flush: "post" }
);

// Watch for loading state to make editor read-only
watch(
  () => exploreStore.isLoadingOperation("executeQuery"),
  (isLoading) => {
    if (editorRef.value && !isDisposing.value) {
      // Set read-only state when query is executing
      editorRef.value.updateOptions({ readOnly: isLoading });

      // If loading is complete but editor still appears to be read-only,
      // force a refresh of the editor options
      if (!isLoading) {
        setTimeout(() => {
          if (editorRef.value && !isDisposing.value) {
            // Ensure read-only is properly reset
            editorRef.value.updateOptions({ readOnly: false });
          }
        }, 50);
      }
    }
  }
);

// Add this watch effect after the existing watchEffect to position cursor when a saved query is loaded
watch(
  () => exploreStore.selectedQueryId,
  (newQueryId, oldQueryId) => {
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
  }
);

// --- Monaco Options Update ---
watch(
  () => props.activeMode,
  (newMode) => {
    nextTick(() => {
      if (editorRef.value) {
        // Apply options based on mode
        const editor = editorRef.value;
        const options = {
          ...getDefaultMonacoOptions(),
          fontSize: 13,
          lineHeight: 20,
          padding: { top: 8, bottom: 8 },
          scrollbar: {
            vertical: "auto" as const,
            horizontal: "auto" as const,
            useShadows: false,
            verticalScrollbarSize: 8,
            horizontalScrollbarSize: 8,
          },
          minimap: { enabled: false },
        };

        editor.updateOptions(options);
        editor.layout();
      }
    });
  }
);

// --- Completion Providers ---
const activeCompletionProvider = ref<MonacoDisposable | null>(null);

const registerCompletionProvider = () => {
  // Save cursor position before making changes
  const editor = editorRef.value;
  let savedPosition = editor?.getPosition() || null;
  let savedSelection = editor?.getSelection() || null;

  // Dispose previous provider if it exists
  if (activeCompletionProvider.value) {
    activeCompletionProvider.value.dispose();
    activeCompletionProvider.value = null;
  }

  let provider: MonacoDisposable | null = null;
  if (props.activeMode === "logchefql") {
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

  // Restore cursor position and selection after completion provider is registered
  nextTick(() => {
    if (editor && savedPosition && !isDisposing.value) {
      editor.setPosition(savedPosition);
      if (savedSelection) {
        editor.setSelection(savedSelection);
      }
    }
  });
};

// --- Actions ---
// Replace dynamic variables in query before submit
const submitQuery = () => {
  const currentContent = editorContent.value;
  validationError.value = null;

  let replaceVariableContent = currentContent;

  for (const variable of allVariables.value) {
    const key = variable.name;
    const value = variable.value;
    const formattedValue =
        variable.type === 'number'
            ? value
            : variable.type === 'date'
                ? `'${new Date(value).toISOString()}'`
                : `'${value}'`;

    const regex = new RegExp(`{{\\s*${key}\\s*}}`, 'g');
    replaceVariableContent = replaceVariableContent.replace(regex, formattedValue as string);
  }

  try {
    let isValid = true;

    if (replaceVariableContent.trim()) {
      if (props.activeMode === "logchefql") {
        const validation = validateLogchefQLWithDetails(replaceVariableContent);
        isValid = validation.valid;
        if (!isValid) validationError.value = validation.error || "Invalid LogchefQL syntax.";
      } else {
        const validation = validateSQLWithDetails(replaceVariableContent);
        isValid = validation.valid;
        console.log("Invalid SQL syntax. : "+isValid);
        if (!isValid) validationError.value = validation.error || "Invalid SQL syntax.";
      }
    }

    if (!isValid) return;

    if (props.activeMode === "logchefql") {
      if (exploreStore.logchefqlCode !== currentContent) exploreStore.setLogchefqlCode(currentContent);
    } else {
      if (exploreStore.rawSql !== currentContent) exploreStore.setRawSql(currentContent);
    }

    emit("submit", {
      query: currentContent,
      mode: props.activeMode,
      isUserInput: true,
    });
  } catch (e: any) {
    console.error("Error validating or submitting query:", e);
    validationError.value = e.message || "Error preparing query";
  }
};

const focusEditor = (revealLastPosition = false) => {
  nextTick(() => {
    // Add a slight delay to allow Monaco to fully process updates after :key change / options update
    setTimeout(() => {
      const editor = editorRef.value;
      if (
        editor &&
        !isDisposing.value &&
        document.contains(editor.getDomNode())
      ) {
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
              editor.revealPositionInCenterIfOutsideViewport(
                positionToSet,
                monaco.editor.ScrollType.Smooth
              );
              console.log(
                `QueryEditor: Focused and position set to ${positionToSet.lineNumber}:${positionToSet.column}`
              );
            } catch (e) {
              console.warn("QueryEditor: Error setting position/revealing:", e);
            }
          } else {
            console.log(
              "QueryEditor: Focused, but could not determine position."
            );
          }
        } else {
          console.log(
            "QueryEditor: Focused (model not available for positioning)."
          );
        }
      } else {
        console.log(
          "QueryEditor: Focus skipped (editor not ready, disposing, or detached)."
        );
      }
    }, 50); // Small delay to allow Monaco to settle
  });
};

// --- Disposal ---
const safelyDisposeEditor = (fullDisposal = false) => {
  if (isDisposing.value) return;
  isDisposing.value = true;

  // If this is a lightweight disposal (deactivation), use our optimized method
  if (!fullDisposal) {
    const editor = editorRef.value;
    if (editor) {
      console.log(
        "QueryEditor: Performing lightweight disposal (deactivation)"
      );
      lightweightEditorDisposal(editor);

      // Dispose any active disposables except the editor itself
      [...activeDisposables.value].forEach((disposable) => {
        try {
          disposable.dispose();
        } catch (e) {
          console.warn("Error disposing resource:", e);
        }
      });
      activeDisposables.value = [];

      // Release the flag after lightweight disposal
      setTimeout(() => {
        isDisposing.value = false;
      }, 50);
    }
    return;
  }

  // Full disposal for unmount
  console.log("QueryEditor: Starting full disposal...");

  // Dispose the active completion provider
  if (activeCompletionProvider.value) {
    try {
      activeCompletionProvider.value.dispose();
    } catch (e) {
      console.warn("Error disposing completion provider:", e);
    }
    activeCompletionProvider.value = null;
  }

  // Dispose general listeners and actions
  [...activeDisposables.value].forEach((disposable) => {
    try {
      disposable.dispose();
    } catch (e) {
      console.warn("Error disposing resource:", e);
    }
  });
  activeDisposables.value = [];

  const editor = editorRef.value;
  if (editor) {
    // Unregister from global tracking
    unregisterEditorInstance(editor);

    // The model is now handled by the global cache, so we don't need to dispose it
    // Just detach it from this editor instance
    editor.setModel(null);

    // Don't dispose the editor immediately to avoid race conditions
    setTimeout(() => {
      if (editor) {
        try {
          editor.dispose();
          console.log("QueryEditor: Editor instance fully disposed.");
        } catch (e) {
          console.error("Error during editor instance disposal:", e);
        }
      }
    }, 50);
  }

  editorRef.value = null; // Clear ref
};

onMounted(() => {
  // add listener to hide dynamic variable drawer
  document.addEventListener('mousedown', handleClickOutside);
});

// Handle full disposal on unmount
// to hide it when user click outside of drawer
onBeforeUnmount(() => {
  // remove listener to hide dynamic variable drawer
  document.removeEventListener('mousedown', handleClickOutside);
  safelyDisposeEditor(true); // Full disposal
});

// Handle lightweight disposal on deactivation (when kept alive)
onDeactivated(() => {
  console.log("QueryEditor: Deactivated");
  if (editorRef.value && !isDisposing.value) {
    safelyDisposeEditor(false); // Lightweight disposal
  }
});

// Handle reactivation
onActivated(() => {
  console.log("QueryEditor: Component activated");
  if (editorRef.value) {
    // Use enhanced reactivation that properly handles models
    reactivateEditor(
      editorRef.value,
      props.activeMode,
      editorContent.value,
      props.sourceId
    );

    // After reactivation, refresh the editor state
    nextTick(() => {
      if (editorRef.value) {
        // Force a layout calculation
        editorRef.value.layout();

        // Try to focus the editor
        focusEditor(false); // Don't move cursor to end
        console.log("QueryEditor: Editor reactivated and focused");
      }
    });
  } else {
    // If there's no editor reference, the component might be mounting for the first time
    console.log(
      "QueryEditor: No editor ref available on activation, will be created during mount"
    );
  }
});

// Toggle SQL editor visibility
const toggleSqlEditorVisibility = () => {
  isEditorVisible.value = !isEditorVisible.value;

  // If we're showing the editor again, focus it after a brief delay
  if (isEditorVisible.value) {
    nextTick(() => {
      setTimeout(() => {
        focusEditor(false);
      }, 50);
    });
  }
};

// Watch for mode changes to reset visibility
watch(
  () => props.activeMode,
  (newMode) => {
    // Always show editor when switching modes
    if (newMode === "logchefql") {
      isEditorVisible.value = true;
    }
  }
);

// --- Expose ---
defineExpose({
  submitQuery,
  // clearEditor is now handled by the parent
  focus: focusEditor, // Expose focus method
  code: computed(() => editorContent.value),
  toggleSqlEditorVisibility, // Expose toggle method
});

// --- Completion Provider Logic (Simplified stubs - keep your detailed logic) ---
// Assume getSuggestionsFromList, getSchemaFieldValues, prepareSuggestionValues remain similar

const registerLogchefQLCompletionProvider = (): MonacoDisposable | null => {
  return monaco.languages.registerCompletionItemProvider("logchefql", {
    provideCompletionItems: async (model, position) => {
      const wordInfo = model.getWordUntilPosition(position);

      // Save cursor position for reliable restoration
      const currentPosition = position.clone();

      const range: MonacoRange = {
        startLineNumber: position.lineNumber,
        endLineNumber: position.lineNumber,
        startColumn: wordInfo.startColumn,
        endColumn: wordInfo.endColumn,
      };

      const lineStartToPosition: MonacoRange = {
        startLineNumber: position.lineNumber,
        endLineNumber: position.lineNumber,
        startColumn: 1,
        endColumn: position.column,
      };
      const textBeforeCursor = model.getValueInRange(lineStartToPosition);

      const parser = new LogchefQLParser();
      parser.parse(textBeforeCursor, false, true);

      let suggestions: MonacoCompletionItem[] = [];
      let incomplete = false;

      // Handle "and" operator specifically
      if (textBeforeCursor.trim().endsWith("and")) {
        // Don't trigger completion after 'and' unless there's at least one more character
        // This helps prevent cursor position issues
        return { suggestions: [], incomplete: false };
      }

      if (
        parser.state === LogchefQLState.KEY ||
        parser.state === LogchefQLState.INITIAL ||
        parser.state === LogchefQLState.BOOL_OP_DELIMITER
      ) {
        if (fieldNames.value.includes(wordInfo.word)) {
          const operatorRange: MonacoRange = {
            startLineNumber: position.lineNumber,
            endLineNumber: position.lineNumber,
            startColumn: position.column,
            endColumn: position.column,
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
          endColumn: wordInfo.endColumn,
        };
        // Standard value suggestions - let user add quotes manually
        const valueResult = await getValueSuggestions(
          parser.key,
          parser.value,
          valueRange
        );
        suggestions = valueResult.suggestions;
        incomplete = valueResult.incomplete;
      }

      return { suggestions, incomplete };
    },
    // Control trigger characters more precisely
    triggerCharacters: ["=", "!", ">", "<", "~", " ", ".", '"', "'", "("],
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
        endColumn: wordInfo.endColumn,
      };

      const lineStartToPosition: MonacoRange = {
        startLineNumber: position.lineNumber,
        endLineNumber: position.lineNumber,
        startColumn: 1,
        endColumn: position.column,
      };
      const textBeforeCursor = model.getValueInRange(lineStartToPosition);
      let suggestions: MonacoCompletionItem[] = [];

      // Add table name suggestion after FROM
      if (/\bFROM\s+$/i.test(textBeforeCursor) && props.tableName) {
        suggestions.push({
          label: props.tableName,
          kind: monaco.languages.CompletionItemKind.Folder,
          insertText: props.tableName,
          range: range,
          detail: "Current log table",
        });
      }

      // Always add field name suggestions for SQL queries
      if (fieldNames.value.length > 0) {
        suggestions = suggestions.concat(
          fieldNames.value.map((field) => ({
            label: field,
            kind: monaco.languages.CompletionItemKind.Field,
            insertText: field,
            range: range,
            detail: props.schema[field]?.type || "unknown",
          }))
        );
      }

      const typedPrefix = wordInfo.word.toUpperCase();
      suggestions = suggestions.concat(
        getSuggestionsFromList({
          items: SQL_KEYWORDS.filter((kw) => kw.startsWith(typedPrefix)).map(
            (kw) => ({ label: kw, insertText: kw + " " })
          ),
          kind: monaco.languages.CompletionItemKind.Keyword,
          range: range,
        })
      );

      return { suggestions };
    },
    // Include more trigger characters for better responsiveness
    triggerCharacters: [" ", "\n", ".", "(", ",", "=", ">", "<", "!"],
  });
};

// --- Helper functions for completions (Keep your existing logic) ---
const getSuggestionsFromList = (params: {
  items: any[];
  kind: monaco.languages.CompletionItemKind;
  range: MonacoRange;
  postfix?: string;
}): MonacoCompletionItem[] => {
  const suggestions: MonacoCompletionItem[] = [];
  const defaultPostfix = params.postfix ?? "";
  const range = params.range;

  // Validate range
  if (
    !range ||
    typeof range.startLineNumber !== "number" ||
    typeof range.startColumn !== "number" ||
    typeof range.endLineNumber !== "number" ||
    typeof range.endColumn !== "number"
  ) {
    console.error("Invalid range provided to getSuggestionsFromList:", range);
    return [];
  }

  for (const item of params.items) {
    let label: string;
    let insertText: string;
    let sortText: string;
    let documentation: string | undefined;

    if (typeof item === "string") {
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
      detail: documentation ? " " : undefined,
      insertTextRules:
        monaco.languages.CompletionItemInsertTextRule.InsertAsSnippet,
    });
  }
  return suggestions;
};

const getKeySuggestions = (range: MonacoRange): MonacoCompletionItem[] => {
  return fieldNames.value.map((name) => ({
    label: name,
    kind: monaco.languages.CompletionItemKind.Field,
    insertText: name,
    range: range,
    detail: props.schema[name]?.type || "Unknown",
    sortText: `0-${name}`,
    command: { id: "editor.action.triggerSuggest", title: "Trigger Suggest" },
  }));
};

const getBooleanOperatorsSuggestions = (
  range: MonacoRange
): MonacoCompletionItem[] => {
  return [
    {
      label: "and",
      kind: monaco.languages.CompletionItemKind.Keyword,
      insertText: "and ",
      range: range,
      sortText: "1-and",
    },
    {
      label: "or",
      kind: monaco.languages.CompletionItemKind.Keyword,
      insertText: "or ",
      range: range,
      sortText: "1-or",
    },
  ];
};

const getSchemaFieldValues = (field: string): string[] => {
  // Example implementation
  const sampleValues: Record<string, string[]> = {
    level: ["info", "error", "warning", "debug"],
    status: ["200", "404", "500"],
    // ... add more based on your schema/data
  };
  return sampleValues[field] || [];
};

const prepareSuggestionValues = (
  items: string[],
  quoteChar?: string
): { label: string; insertText?: string }[] => {
  // ... your existing implementation ...
  const needsQuotes = quoteChar !== undefined;
  const q = quoteChar || '"'; // Default to double quotes if needed but not specified
  return items.map((item) => {
    if (!needsQuotes && isNumeric(item)) {
      return { label: item }; // Numeric values don't need quotes unless context demands it
    } else {
      // Escape existing quotes within the value
      const escapedValue = item.replace(new RegExp(`\\${q}`, "g"), `\\${q}`);
      return {
        label: item,
        insertText: `${q}${escapedValue}${q}`,
      };
    }
  });
};

const getValueSuggestions = async (
  key: string,
  currentValue: string,
  range: MonacoRange,
  quoteChar?: string
): Promise<{ suggestions: MonacoCompletionItem[]; incomplete: boolean }> => {
  const fieldValues = getSchemaFieldValues(key);
  const filteredValues = fieldValues.filter((v) =>
    v.toLowerCase().includes(currentValue?.toLowerCase() || "")
  );
  const preparedItems = prepareSuggestionValues(filteredValues, quoteChar);

  const suggestions = getSuggestionsFromList({
    items: preparedItems,
    kind: monaco.languages.CompletionItemKind.Value,
    range: range,
    postfix: " ",
  });

  return { suggestions, incomplete: false };
};

const getOperatorsSuggestions = (
  key: string,
  range: MonacoRange
): MonacoCompletionItem[] => {
  // Organize operators in the desired order: =, !=, ~, !~, >=, <=, >, <
  const operators = [
    { label: "=", insertText: "=", sortText: "1=" },
    { label: "!=", insertText: "!=", sortText: "2!=" },
    { label: "~", insertText: "~", sortText: "3~" },
    { label: "!~", insertText: "!~", sortText: "4!~" },
    { label: ">=", insertText: ">=", sortText: "5>=" },
    { label: "<=", insertText: "<=", sortText: "6<=" },
    { label: ">", insertText: ">", sortText: "7>" },
    { label: "<", insertText: "<", sortText: "8<" },
  ];

  // Filter operators based on field type if needed
  const fieldType = props.schema[key]?.type?.toLowerCase() || "";
  const filteredOperators = operators.filter((op) => {
    // For numeric fields, only allow comparison operators
    if (fieldType === "number" || fieldType === "integer") {
      return ["=", "!=", ">", "<", ">=", "<="].includes(op.label);
    }
    // For string fields, allow all operators
    return true;
  });

  return getSuggestionsFromList({
    items: filteredOperators.map((op) => ({
      ...op,
      insertText: op.insertText + " ", // Just add a space after operator, no quote
      documentation: `${key} ${op.label} value`,
    })),
    kind: monaco.languages.CompletionItemKind.Operator,
    range: range,
    postfix: "",
  });
};

// Add type assertion function
const asEditorMode = (value: string | number): EditorMode => {
  if (value === "logchefql" || value === "clickhouse-sql") {
    return value;
  }
  console.warn(
    `Received unexpected value for editor mode: ${value}. Defaulting to logchefql.`
  );
  return "logchefql";
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

  // Explicitly clear the selectedQueryId in the store
  exploreStore.setSelectedQueryId(null);

  // Explicitly clear active saved query name
  exploreStore.setActiveSavedQueryName(null);

  // Wait for the state reset to complete
  nextTick(() => {
    // Explicitly remove the query_id parameter again to ensure it's gone
    const finalQuery = { ...currentQuery };
    delete finalQuery.query_id;

    // Replace URL with new parameters in router, disabling any redirect
    // This is important to prevent the URL sync from adding back query_id
    router
      .replace({ query: finalQuery })
      .then(() => {
        // Focus the editor and position cursor at the end after URL change is complete
        setTimeout(() => {
          focusEditor(true); // true = position cursor at end
        }, 50);
      })
      .catch((err) => {
        console.error("Error updating URL:", err);
        // Still try to focus even if URL update fails
        focusEditor(true);
      });
  });
};
// Open drawer to edit selected variable
const openVariableSettings = (variable: VariableSetting) => {
  selectedVariable.value = variable;
};

// Close the drawer UI
const closeDrawer = () => {
  selectedVariable.value = null;
};

// to hide it when user click outside of drawer
const handleClickOutside = (event: MouseEvent) => {
  if (drawerRef.value && !drawerRef.value.contains(event.target as Node)) {
    closeDrawer();
  }
};

// Update default value based on variable type
const setDefaultValueByType = () => {
  if (!selectedVariable.value) return;

  switch (selectedVariable.value.type) {
    case 'text':
      selectedVariable.value.value = '';
      break;
    case 'number':
      selectedVariable.value.value = 0;
      break;
    case 'date':
      selectedVariable.value.value = new Date().toISOString();
      break;
  }
};

// Determine input type for a given variable type
const inputTypeFor = (type: string) => {
  if (type === 'number') return 'number';
  if (type === 'date') return 'datetime-local';
  return 'text';
};

</script>

<style scoped>
.query-editor {
  position: relative;
  height: 100%;
  width: 100%;
}

/* Wrapper to handle border-radius and overflow together */
.editor-wrapper {
  position: relative;
  border-radius: 0 0 6px 6px;
  border: 1px solid hsl(var(--border));
  transition: border-color 0.15s ease, box-shadow 0.15s ease;
  overflow: hidden;
}

.editor-wrapper:hover:not(.is-focused) {
  border-color: hsl(var(--border-hover, var(--border)));
}

/* Focus state - use box-shadow for a cleaner look that doesn't conflict */
.editor-wrapper.is-focused {
  border-color: hsl(var(--primary));
  box-shadow: 0 0 0 1px hsl(var(--primary) / 0.3);
}

.dark .editor-wrapper.is-focused {
  border-color: hsl(var(--primary));
  box-shadow: 0 0 0 1px hsl(var(--primary) / 0.4),
    0 0 0 2px hsl(var(--primary) / 0.15);
}

/* Clean editor container styling */
.editor-container {
  position: relative;
  width: 100%;
  height: 100%;
  background-color: transparent; /* Make transparent to let Monaco background show through */
  padding-left: 16px;
  /* Add padding to container to position cursor */
}

/* Basic placeholder implementation */
.editor-container.is-empty::before {
  content: attr(data-placeholder);
  color: hsl(var(--muted-foreground) / 0.8);
  position: absolute;
  top: 12px;
  left: 16px;
  font-size: 13px;
  pointer-events: none;
  z-index: 1;
  font-family: ui-monospace, SFMono-Regular, Menlo, Monaco, Consolas,
    "Liberation Mono", "Courier New", monospace;
}

/* Adjust placeholder position - keep it consistent in both modes */
.editor-container.is-empty[data-mode="clickhouse-sql"]::before {
  left: 16px;
}

/* Override Monaco's internal borders if any */
:deep(.monaco-editor),
:deep(.monaco-editor .overflow-guard) {
  border: none !important;
  outline: none !important;
  background-color: transparent !important; /* Ensure transparency */
}

/* Style just the margin/gutter */
:deep(.monaco-editor .margin) {
  border-radius: 0 0 0 5px;
  padding-right: 0 !important;
  background-color: transparent !important;
  width: 0 !important;
}

/* Ensure consistent content position */
:deep(.monaco-editor .monaco-scrollable-element) {
  left: 0 !important;
}

/* Reset any padding/margins that might interfere */
:deep(.monaco-editor .view-line) {
  margin-left: 0 !important;
}

/* Force flex layout for tab triggers */
:deep(.TabsTrigger) {
  display: flex !important;
  align-items: center !important;
}

.flex-fix {
  display: flex;
  flex-direction: row;
  align-items: center;
  width: 100%;
  justify-content: center;
}

.flex-fix svg {
  margin-right: 6px;
}

/* Force flex layout for tab triggers */
:deep(.tab) {
  display: block !important;
}

:deep([role="tab"]) {
  padding: 6px 12px !important;
}

/* Styles for SQL preview when editor is hidden */
.sql-preview {
  border-radius: 0 0 6px 6px;
  transition: background-color 0.2s;
}

.sql-preview:hover {
  background-color: hsl(var(--muted) / 0.6);
}

.dark .sql-preview:hover {
  background-color: #1c2536; /* Matches the bluish dark theme hover */
}

/* drawer setting for dynamic variables */
.drawer {
  position: fixed;
  top: 0;
  right: 0;
  width: 320px;
  height: 100%;
  background: #fff;
  border-left: 1px solid #ccc;
  padding: 20px;
  box-shadow: -2px 0 8px rgba(0, 0, 0, 0.1);
  display: flex;
  flex-direction: column;
}

.drawer-header {
  display: flex;
  justify-content: space-between;
  align-items: center;
}

.drawer-body {
  margin-top: 20px;
  display: flex;
  flex-direction: column;
  gap: 12px;
}
</style>
