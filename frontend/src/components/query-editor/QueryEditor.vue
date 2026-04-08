<template>
  <div :class="['query-editor', props.class]">
    <!-- Header Bar (Keep existing structure) -->
    <div class="flex items-center justify-between bg-muted/40 rounded-t-md px-3 py-1.5 border border-b-0">
      <div class="flex items-center gap-3">
        <!-- Fields Panel Toggle -->
        <button class="p-1 text-muted-foreground hover:text-foreground flex items-center"
          @click="$emit('toggle-fields')" :title="props.showFieldsPanel ? 'Hide fields panel' : 'Show fields panel'
            " aria-label="Toggle fields panel">
          <PanelRightClose v-if="props.showFieldsPanel" class="h-4 w-4" />
          <PanelRightOpen v-else class="h-4 w-4" />
        </button>

        <!-- Tabs for Mode Switching -->
        <Tabs :model-value="props.activeMode"
          @update:model-value="(value: string | number) => $emit('update:activeMode', asEditorMode(value), true)"
          class="w-auto">
          <TabsList :class="['grid w-fit', supportsLogchefQL ? 'grid-cols-2' : 'grid-cols-1']">
            <TabsTrigger v-if="supportsLogchefQL" value="logchefql">
              <div class="flex-fix">
                <Search class="w-4 h-4" />
                <span>Search</span>
              </div>
            </TabsTrigger>
            <TabsTrigger value="clickhouse-sql">
              <div class="flex-fix">
                <Code2 class="w-4 h-4" />
                <span>{{ nativeEditorLabel }}</span>
              </div>
            </TabsTrigger>
          </TabsList>
        </Tabs>

        <!-- AI Assistant Button -->
        <TooltipProvider v-if="supportsAiAssistant">
          <Tooltip>
            <TooltipTrigger asChild>
              <Button variant="outline" size="sm" class="h-7 gap-1.5" @click="showAiDialog = true">
                <Wand2 class="h-3.5 w-3.5 text-purple-600" />
                <span class="text-xs font-medium hidden sm:inline">AI Assistant</span>
              </Button>
            </TooltipTrigger>
            <TooltipContent side="bottom">
              <p>Generate SQL using natural language</p>
            </TooltipContent>
          </Tooltip>
        </TooltipProvider>

        <!-- Query History Button -->
        <QueryHistoryDropdown
          :team-id="props.teamId"
          :source-id="props.sourceId"
          @load-query="handleLoadQueryFromHistory"
        />

        <!-- Table name indicator - hidden on small screens -->
        <div class="text-xs text-muted-foreground ml-3 hidden md:block">
          <template v-if="props.tableName">
            <span class="mr-1">Table:</span>
            <code class="bg-muted px-1.5 py-0.5 rounded text-xs">{{
              props.tableName
            }}</code>
          </template>
          <template v-else-if="isVictoriaLogsSource">
            <span class="mr-1">Datasource:</span>
            <code class="bg-muted px-1.5 py-0.5 rounded text-xs">VictoriaLogs</code>
          </template>
          <span v-else class="italic text-orange-500">No table selected</span>
        </div>

      </div>

      <div class="flex items-center gap-2">
        <!-- New: New Query Button - Only show when editing a saved query -->
        <TooltipProvider v-if="isEditingExistingQuery">
          <Tooltip>
            <TooltipTrigger asChild>
              <Button variant="outline" size="sm" class="h-7 gap-1" @click="handleNewQueryClick">
                <FilePlus2 class="h-3.5 w-3.5" />
                <span class="text-xs hidden sm:inline">New</span>
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
              <Button variant="outline" size="sm" class="h-7 gap-1" @click="toggleSqlEditorVisibility">
                <EyeOff v-if="isEditorVisible" class="h-3.5 w-3.5" />
                <Eye v-else class="h-3.5 w-3.5" />
                <span class="text-xs hidden sm:inline">{{ isEditorVisible ? "Hide" : "Show" }} {{ nativeEditorLabel }}</span>
              </Button>
            </TooltipTrigger>
            <TooltipContent side="bottom">
              <p>{{ isEditorVisible ? "Hide" : "Show" }} {{ nativeEditorLabel }} query editor</p>
            </TooltipContent>
          </Tooltip>
        </TooltipProvider>

        <!-- Saved Queries Dropdown -->
        <SavedQueriesDropdown :selected-source-id="props.sourceId" :selected-team-id="props.teamId"
          @select-saved-query="(query: SavedTeamQuery) => $emit('select-saved-query', query)"
          @save="$emit('save-query')" class="h-8" />

        <!-- Run Query Button - Integrated -->
        <TooltipProvider v-if="props.showRunButton && !props.isExecuting">
          <Tooltip>
            <TooltipTrigger asChild>
              <Button 
                variant="default" 
                size="sm" 
                class="h-7 gap-1.5 px-3 bg-emerald-600 hover:bg-emerald-700 text-white shadow-sm"
                :disabled="!props.canExecute"
                @click="$emit('execute')"
              >
                <Play class="h-3.5 w-3.5" />
                <span class="font-medium">Run</span>
                <kbd class="ml-1 text-[10px] bg-emerald-700/50 px-1 py-0.5 rounded hidden sm:inline">⌘↵</kbd>
              </Button>
            </TooltipTrigger>
            <TooltipContent side="bottom">
              <p class="text-xs">Execute query (Ctrl+Enter)</p>
            </TooltipContent>
          </Tooltip>
        </TooltipProvider>

        <!-- Cancel Query Button - Shows when executing -->
        <TooltipProvider v-if="props.showRunButton && props.isExecuting">
          <Tooltip>
            <TooltipTrigger asChild>
              <Button 
                variant="destructive" 
                size="sm" 
                class="h-7 gap-1.5 px-3 shadow-sm"
                :disabled="props.isCancelling"
                @click="$emit('cancel-query')"
              >
                <template v-if="props.isCancelling">
                  <RefreshCw class="h-3.5 w-3.5 animate-spin" />
                  <span class="font-medium">Cancelling...</span>
                </template>
                <template v-else>
                  <Square class="h-3.5 w-3.5" />
                  <span class="font-medium">Cancel</span>
                  <kbd class="ml-1 text-[10px] bg-red-700/50 px-1 py-0.5 rounded hidden sm:inline">Esc</kbd>
                </template>
              </Button>
            </TooltipTrigger>
            <TooltipContent side="bottom">
              <p class="text-xs">Cancel running query (Escape)</p>
            </TooltipContent>
          </Tooltip>
        </TooltipProvider>

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
              <h4 class="text-sm font-semibold">
                {{ props.activeMode === "logchefql" ? "LogchefQL" : nativeEditorLabel }}
                Syntax
              </h4>
              <div v-if="props.activeMode === 'logchefql'" class="text-xs space-y-1.5">
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
                  <em>Example:
                    <code class="bg-muted px-1 rounded">level="error" and status>=500</code></em>
                </div>
              </div>
              <div v-else-if="isVictoriaLogsSource" class="text-xs space-y-1.5">
                <div>
                  <code class="bg-muted px-1 rounded">level:="error"</code> -
                  Exact match
                </div>
                <div>
                  <code class="bg-muted px-1 rounded">*timeout*</code> -
                  Message substring search
                </div>
                <div>
                  <code class="bg-muted px-1 rounded">service:="api" level:="error"</code> -
                  Combine filters
                </div>
                <div>
                  <code class="bg-muted px-1 rounded">| stats by (level) count()</code> -
                  Pipe operators
                </div>
                <div class="pt-1">
                  <em>Use native VictoriaLogs LogsQL. Time range is applied separately from the picker.</em>
                </div>
              </div>
              <div v-else class="text-xs space-y-1.5">
                <div>
                  <code class="bg-muted px-1 rounded">SELECT count() FROM {{ tableName || "table" }}</code>
                </div>
                <div>
                  <code class="bg-muted px-1 rounded">WHERE field = 'value' AND time > now() - interval 1
              hour</code>
                </div>
                <div>
                  <code class="bg-muted px-1 rounded">GROUP BY user ORDER BY count() DESC</code>
                </div>
                <div class="pt-1">
                  <em>Time range & limit applied if not specified. Use standard
                    ClickHouse SQL.</em>
                </div>
              </div>
            </div>
          </HoverCardContent>
        </HoverCard>
      </div>
    </div>

    <!-- Compact Variable Editor -->
    <VariablesPanel @open-config="showVariablesConfig = true" />

    <!-- Query Editor Container -->
    <div class="editor-wrapper" :class="{ 'is-focused': editorFocused }"
      v-show="isEditorVisible || props.activeMode === 'logchefql'">
      <div
        class="editor-container"
        :class="{ 'is-empty': isEditorEmpty }"
        :style="{ height: `${editorHeight}px` }"
        :data-placeholder="currentPlaceholder"
        :data-mode="isVictoriaLogsSource ? 'logsql' : 'clickhouse-sql'"
      >
        <div v-if="sqlEditorLoadError" class="sql-editor-error">
          <p class="sql-editor-error__title">Unable to load SQL editor</p>
          <p class="sql-editor-error__description">{{ sqlEditorLoadError }}</p>
          <Button size="sm" variant="outline" @click="retrySqlEditorLoad">
            Retry
          </Button>
        </div>

        <component
          :is="SqlMonacoEditorComponent"
          v-else-if="SqlMonacoEditorComponent"
          ref="sqlEditorRef"
          :value="editorContent"
          :theme="theme"
          :language="props.activeMode"
          :schema="props.schema"
          :team-id="props.teamId"
          :source-id="props.sourceId"
          :table-name="props.tableName"
          :is-executing="props.isExecuting"
          :visible="isEditorVisible"
          class="h-full w-full"
          @change="handleSqlEditorChange"
          @submit="submitQuery"
          @ready="handleSqlEditorReady"
          @focus-change="editorFocused = $event"
        />

        <SqlMonacoSkeleton v-else-if="isSqlEditorLoading" />
      </div>
    </div>

    <!-- SQL Preview when editor is hidden -->
    <div v-if="
      !isEditorVisible &&
      props.activeMode === 'clickhouse-sql' &&
      !isEditorEmpty
    " class="sql-preview p-3 border border-border rounded-md bg-card/60 text-sm font-mono overflow-hidden cursor-pointer dark:bg-[#111522]"
      @click="isEditorVisible = true">
      <div class="flex items-center justify-between">
        <div class="text-muted-foreground text-xs font-medium mb-1">
          {{ nativeEditorLabel }} Query (collapsed)
        </div>
        <Button variant="ghost" size="sm" class="h-6 px-2" @click.stop="isEditorVisible = true">
          <Eye class="h-3.5 w-3.5 mr-1" />
          <span class="text-xs">Show</span>
        </Button>
      </div>
      <div class="truncate text-xs text-muted-foreground">
        {{ editorContent }}
      </div>
    </div>

    <!-- Error Message Display -->
    <div v-if="validationError"
      class="mt-2 p-2 text-sm text-destructive bg-destructive/10 rounded flex items-center gap-2">
      <AlertCircle class="h-4 w-4 flex-shrink-0" />
      <span>
        <span class="font-medium">Validation Error: </span>
        {{ validationError }}
        <span v-if="validationError?.includes('Missing boolean operator')" class="block mt-1 text-xs">
          Hint: Use <code class="bg-muted px-1 rounded">and</code> or
          <code class="bg-muted px-1 rounded">or</code> between conditions.
          Example:
          <code class="bg-muted px-1 rounded">field1="value" and field2="value"</code>
        </span>
      </span>
    </div>
  </div>

  <!-- Variable Configuration Sheet -->
  <VariableConfigSheet
    :open="showVariablesConfig"
    @update:open="showVariablesConfig = $event"
  />

  <!-- AI SQL Assistant Dialog -->
  <AiSqlDialog
    :open="showAiDialog"
    :is-generating="isGeneratingAi"
    :error="aiError"
    :generated-sql="generatedSql"
    @update:open="showAiDialog = $event"
    @submit="handleAiDialogSubmit"
    @insert="handleAiInsert"
  />


</template>

<script setup lang="ts">
import {
  type Component,
  computed,
  nextTick,
  onBeforeUnmount,
  onMounted,
  markRaw,
  ref,
  shallowRef,
  watch,
  type ComponentPublicInstance,
} from "vue";
import { useDark } from "@vueuse/core";
import {
  HelpCircle,
  PanelRightOpen,
  PanelRightClose,
  AlertCircle,
  FilePlus2,
  Search,
  Code2,
  Eye,
  EyeOff,
  Wand2,
  Play,
  RefreshCw,
  Square,
} from "lucide-vue-next";
import {
  HoverCard,
  HoverCardContent,
  HoverCardTrigger,
} from "@/components/ui/hover-card";
import { Tabs, TabsList, TabsTrigger } from "@/components/ui/tabs";
import SavedQueriesDropdown from "@/components/collections/SavedQueriesDropdown.vue";
import QueryHistoryDropdown from "./QueryHistoryDropdown.vue";
import AiSqlDialog from "./AiSqlDialog.vue";
import VariableConfigSheet from "./VariableConfigSheet.vue";
import VariablesPanel from "./VariablesPanel.vue";
import type { SavedTeamQuery } from "@/api/savedQueries";
import { useRoute, useRouter } from "vue-router";
import { Button } from "@/components/ui/button";
import {
  Tooltip,
  TooltipContent,
  TooltipProvider,
  TooltipTrigger,
} from "@/components/ui/tooltip";
import { logchefqlApi } from "@/api/logchefql";
import { storeToRefs } from 'pinia';
import { useExploreStore } from "@/stores/explore";
import { useTeamsStore } from "@/stores/teams";
import { useVariableStore } from '@/stores/variables';
import { useVariables, extractVariablesWithOptional, extractVariableNames } from "@/composables/useVariables.ts";
import SqlMonacoSkeleton from "./SqlMonacoSkeleton.vue";

type EditorMode = "logchefql" | "clickhouse-sql";
type EditorChangeEvent = {
  query: string;
  mode: EditorMode;
  isUserInput?: boolean;
};

type SqlEditorPublicInstance = ComponentPublicInstance<{
  focus: (revealLastPosition?: boolean) => void;
}>;

const sqlEditorModules = import.meta.glob("./SqlMonacoEditor.vue");

interface QueryEditorProps {
  sourceId: number
  sourceType?: string
  schema: Record<string, { type: string }>
  activeMode: EditorMode
  tableName: string
  teamId: number
  value?: string
  placeholder?: string
  tsField?: string
  showFieldsPanel?: boolean
  useCurrentTeam?: boolean
  class?: string
  isExecuting?: boolean
  canExecute?: boolean
  showRunButton?: boolean
  isCancelling?: boolean
}

const props = withDefaults(defineProps<QueryEditorProps>(), {
  value: '',
  sourceType: 'clickhouse',
  placeholder: '',
  tsField: 'timestamp',
  showFieldsPanel: false,
  useCurrentTeam: true,
  class: '',
  isExecuting: false,
  canExecute: true,
  showRunButton: true,
  isCancelling: false,
});

const emit = defineEmits<{
  (e: "change", value: EditorChangeEvent): void;
  (e: "submit", value: EditorChangeEvent): void;
  (e: "update:activeMode", value: EditorMode, isModeSwitchOnly?: boolean): void;
  (e: "toggle-fields"): void;
  // SavedQueries events
  (e: "select-saved-query", query: SavedTeamQuery): void;
  (e: "save-query"): void;
  // Additional emits to prevent Vue warnings
  (e: "saveQueryAsNew"): void;
  (e: "generateAiSql", payload: any): void;
  // Run button
  (e: "execute"): void;
  // Cancel button
  (e: "cancel-query"): void;
}>();

const isDark = useDark();
const exploreStore = useExploreStore();
const variableStore = useVariableStore();
const teamsStore = useTeamsStore();
const { convertVariables } = useVariables();

const editorContent = ref(props.value || "");
const editorFocused = ref(false);
const validationError = ref<string | null>(null);
const isEditorVisible = ref(true);
const pendingSqlFocus = ref<boolean | null>(null);
const isSqlEditorLoading = ref(false);
const sqlEditorLoadError = ref<string | null>(null);
const sqlEditorRef = ref<SqlEditorPublicInstance | null>(null);
const SqlMonacoEditorComponent = shallowRef<Component | null>(null);

const { allVariables } = storeToRefs(variableStore);
const showVariablesConfig = ref(false);
const showAiDialog = ref(false);
const isGeneratingAi = computed(() => exploreStore.isGeneratingAISQL);
const aiError = computed(() => exploreStore.aiSqlError);
const generatedSql = computed(() => exploreStore.generatedAiSql);

const theme = computed(() => (isDark.value ? "logchef-dark" : "logchef-light"));
const isEditorEmpty = computed(() => !editorContent.value?.trim());
const isVictoriaLogsSource = computed(() => props.sourceType === "victorialogs");
const supportsLogchefQL = computed(() => !isVictoriaLogsSource.value);
const supportsAiAssistant = computed(() => !isVictoriaLogsSource.value);
const nativeEditorLabel = computed(() => (isVictoriaLogsSource.value ? "LogsQL" : "SQL"));

const currentPlaceholder = computed(() => {
  if (props.placeholder) return props.placeholder;

  return props.activeMode === "logchefql"
    ? 'Enter search criteria (e.g., lvl="ERROR" and namespace~"sys")'
    : isVictoriaLogsSource.value
      ? 'Enter LogsQL query (e.g., level:="error" service:="api")'
      : `Enter ClickHouse SQL query (e.g., SELECT * FROM ${props.tableName || "your_table"} WHERE ...)`;
});

const editorHeight = computed(() => {
  const content = editorContent.value || "";
  const lines = (content.match(/\n/g) || []).length + 1;
  const baseLineHeight = 20; // Must match Monaco lineHeight
  const padding = 16; // Monaco top+bottom padding (8+8)
  const minHeight = props.activeMode === "logchefql" ? 52 : 90;
  const maxHeight = 300;
  const calculatedHeight = padding + lines * baseLineHeight + (lines > 1 ? 0 : 4);
  return Math.min(maxHeight, Math.max(minHeight, calculatedHeight));
});

const handleEditorChange = (value: string | undefined) => {
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

const detectVariables = (value: string) => {
  if (typeof value !== 'string') return;

  const isSqlMode = props.activeMode === 'clickhouse-sql';
  const extractedVars = isSqlMode 
    ? extractVariablesWithOptional(value)
    : extractVariableNames(value).map(name => ({ name, isOptional: false }));
  
  const currentVariables = allVariables?.value ?? [];
  const extractedNames = extractedVars.map(v => v.name);

  for (const variable of currentVariables) {
    if (!extractedNames.includes(variable.name)) {
      variableStore.removeVariable(variable.name);
    }
  }

  for (const { name, isOptional } of extractedVars) {
    const existing = variableStore.getVariableByName(name);
    if (!existing) {
      variableStore.upsertVariable({
        name,
        type: 'text',
        label: name,
        inputType: 'input',
        value: '',
        isOptional
      });
    } else if (existing.isOptional !== isOptional) {
      variableStore.upsertVariable({ ...existing, isOptional });
    }
  }
};

// Watch for prop value changes to update editor content
watch(
  () => props.value,
  (newValue) => {
    const normalizedValue = newValue || "";
    if (normalizedValue !== editorContent.value) {
      editorContent.value = normalizedValue;
    }
  }
);

// Watch for editor content changes to detect variables immediately
watch(
  () => editorContent.value,
  (newValue) => {
    detectVariables(newValue ?? "");
  },
  { immediate: true }
);

watch(
  () => props.activeMode,
  (newMode, oldMode) => {
    validationError.value = null;

    if (newMode === "clickhouse-sql") {
      void ensureSqlEditorLoaded();
    } else {
      isEditorVisible.value = true;
    }

    if (oldMode && oldMode !== newMode) {
      nextTick(() => focusEditor(false));
    }
  },
  { immediate: true }
);

watch(
  () => exploreStore.selectedQueryId,
  (newQueryId, oldQueryId) => {
    if (newQueryId && newQueryId !== oldQueryId) {
      nextTick(() => {
        setTimeout(() => {
          focusEditor(true);
        }, 100);
      });
    }
  }
);

const submitQuery = async () => {
  const currentContent = editorContent.value;
  validationError.value = null;

  let queryForValidation = currentContent;
  if (props.activeMode === "logchefql") {
    queryForValidation = currentContent.replace(/{{(\w+)}}/g, '"placeholder"');
  } else {
    queryForValidation = convertVariables(currentContent);
  }

  try {
    let isValid = true;

      if (queryForValidation.trim()) {
        if (props.activeMode === "logchefql") {
          try {
            const currentTeamId = teamsStore.currentTeamId;
            if (currentTeamId && props.sourceId) {
              const response = await logchefqlApi.validate(currentTeamId, props.sourceId, queryForValidation);
              if (response.data) {
                isValid = response.data.valid;
                if (!isValid) validationError.value = response.data.error?.message || "Invalid LogchefQL syntax.";
              }
            }
          } catch (validationErr) {
            console.warn("LogchefQL validation API error:", validationErr);
            isValid = true;
          }
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
  pendingSqlFocus.value = revealLastPosition;
  void ensureSqlEditorLoaded();

  nextTick(() => {
    if (!sqlEditorRef.value) {
      return;
    }

    sqlEditorRef.value.focus(revealLastPosition);
    pendingSqlFocus.value = null;
  });
};

const handleEscapeKey = (e: KeyboardEvent) => {
  if (e.key === 'Escape' && props.isExecuting && !props.isCancelling) {
    e.preventDefault();
    emit('cancel-query');
  }
};

async function ensureSqlEditorLoaded() {
  if (SqlMonacoEditorComponent.value || isSqlEditorLoading.value) {
    return;
  }

  const loadSqlEditor = sqlEditorModules["./SqlMonacoEditor.vue"];
  if (!loadSqlEditor) {
    return;
  }

  isSqlEditorLoading.value = true;
  sqlEditorLoadError.value = null;

  try {
    const module = (await loadSqlEditor()) as { default: Component };
    SqlMonacoEditorComponent.value = markRaw(module.default);
  } catch (error) {
    sqlEditorLoadError.value =
      error instanceof Error
        ? error.message
        : "The SQL editor chunk could not be loaded. Please retry.";
  } finally {
    isSqlEditorLoading.value = false;
  }
}

function retrySqlEditorLoad() {
  SqlMonacoEditorComponent.value = null;
  void ensureSqlEditorLoaded();
}

const handleSqlEditorChange = (value: string) => {
  handleEditorChange(value);
};

const handleSqlEditorReady = () => {
  if (pendingSqlFocus.value === null || !sqlEditorRef.value) {
    return;
  }

  const revealLastPosition = pendingSqlFocus.value;
  pendingSqlFocus.value = null;
  sqlEditorRef.value.focus(revealLastPosition);
};

onMounted(() => {
  document.addEventListener('keydown', handleEscapeKey);
  void ensureSqlEditorLoaded();
});

onBeforeUnmount(() => {
  document.removeEventListener('keydown', handleEscapeKey);
});

const toggleSqlEditorVisibility = () => {
  isEditorVisible.value = !isEditorVisible.value;

  if (isEditorVisible.value) {
    nextTick(() => focusEditor(false));
  }
};

defineExpose({
  submitQuery,
  focus: focusEditor,
  code: computed(() => editorContent.value),
  toggleSqlEditorVisibility,
});

const asEditorMode = (value: string | number): EditorMode => {
  if (value === "logchefql" || value === "clickhouse-sql") {
    return value;
  }
  return "logchefql";
};

const route = useRoute();
const router = useRouter();

const isEditingExistingQuery = computed(() => !!route.query.id);

const handleLoadQueryFromHistory = (mode: 'logchefql' | 'sql', query: string) => {
  const editorMode = mode === 'logchefql' ? 'logchefql' : 'clickhouse-sql';

  emit('change', {
    query: query,
    mode: editorMode,
    isUserInput: false,
  });

  if (editorMode !== props.activeMode) {
    nextTick(() => {
      emit('update:activeMode', editorMode, true);
    });
  }
};

const handleNewQueryClick = () => {
  const currentQuery = { ...route.query };
  delete currentQuery.id;

  exploreStore.resetQueryToDefaults();
  exploreStore.setSelectedQueryId(null);
  exploreStore.setActiveSavedQueryName(null);

  nextTick(() => {
    const finalQuery = { ...currentQuery };
    delete finalQuery.id;

    router
      .replace({ query: finalQuery })
      .then(() => {
        setTimeout(() => {
          focusEditor(true);
        }, 50);
      })
      .catch((err) => {
        console.error("Error updating URL:", err);
        focusEditor(true);
      });
  });
};

const handleAiDialogSubmit = (payload: { naturalLanguageQuery: string; currentQuery: string }) => {
  emit('generateAiSql', {
    naturalLanguageQuery: payload.naturalLanguageQuery,
    currentQuery: editorContent.value || '',
  });
};

const handleAiInsert = (sql: string) => {
  editorContent.value = sql;
  handleEditorChange(sql);
  showAiDialog.value = false;
  exploreStore.clearAiSqlState();
  nextTick(() => {
    focusEditor(true);
  });
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
  background-color: transparent;
  padding-left: 16px;
}

.query-input {
  width: 100%;
  border: none;
  outline: none;
  resize: none;
  background: transparent;
  padding: 12px 16px;
  font-size: 13px;
  line-height: 21px;
  color: hsl(var(--foreground));
  font-family: ui-monospace, SFMono-Regular, Menlo, Monaco, Consolas,
    "Liberation Mono", "Courier New", monospace;
}

.query-input::placeholder {
  color: hsl(var(--muted-foreground) / 0.8);
}

.sql-editor-error {
  display: flex;
  flex-direction: column;
  align-items: flex-start;
  justify-content: center;
  gap: 0.75rem;
  width: 100%;
  height: 100%;
  padding: 1rem;
}

.sql-editor-error__title {
  font-size: 0.875rem;
  font-weight: 600;
}

.sql-editor-error__description {
  font-size: 0.75rem;
  color: hsl(var(--muted-foreground));
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

/* Adjust placeholder position - keep it consistent in native query modes */
.editor-container.is-empty[data-mode="clickhouse-sql"]::before,
.editor-container.is-empty[data-mode="logsql"]::before {
  left: 16px;
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
  background-color: #1c2536;
  /* Matches the bluish dark theme hover */
}

/* Remove old drawer styles as we're using shadcn-ui Sheet component now */
</style>
