<script setup lang="ts">
import { useI18n } from "vue-i18n";

import {
  ref,
  computed,
  watch,
  onBeforeUnmount,
  nextTick,
} from "vue";
import { storeToRefs } from "pinia";
import { useRouter, onBeforeRouteLeave } from "vue-router";
import { Button } from "@/components/ui/button";
import { Info } from "lucide-vue-next";
import { useToast } from "@/composables/useToast";
import { TOAST_DURATION } from "@/lib/constants";
import { useExploreStore } from "@/stores/explore";
import { useExploreRuns } from "@/stores/exploreRuns";
import { unfilteredSortKeys } from "./sortKeyHint";
import { useTeamsStore } from "@/stores/teams";
import { useSourcesStore } from "@/stores/sources";
import { useSavedQueriesStore } from "@/stores/savedQueries";
import { usePreferencesStore } from "@/stores/preferences";
import { FieldSideBar } from "@/components/field-sidebar";
import { getErrorMessage } from "@/api/types";
import DataTable from "./table/data-table.vue";
import CompactLogList from "./table/CompactLogListSimple.vue";
import SaveQueryModal from "@/components/collections/SaveQueryModal.vue";
import QueryEditor from "@/components/query-editor/QueryEditor.vue";
import { useSavedQueries } from "@/composables/useSavedQueries";
import { useExplorerNavigator } from "@/composables/useExplorerNavigator";
import { useQuery } from "@/composables/useQuery";
import { supportsQueryLanguage, hasSourceCapability } from "@/lib/queryMetadata";

import { useContextStore } from "@/stores/context";
import { exploreApi } from "@/api/explore";
import type { ComponentPublicInstance } from "vue";
import type { SaveQueryFormData } from "@/views/explore/types";
import type { SavedQuery } from "@/api/savedQueries";
import type { FilterCondition } from "@/api/logchefql";
import { copyText } from "@/utils/clipboard";
import { epochMs } from "@/utils/timeWindow";

// Type alias for backwards compatibility
type QueryCondition = FilterCondition;

// Import refactored components
import TeamSourceSelector from "./components/TeamSourceSelector.vue";
import QueryError from "./components/QueryError.vue";
import HistogramVisualization from "./components/HistogramVisualization.vue";
import EmptyResultsState from "./components/EmptyResultsState.vue";
import ExploreTopBar from "./components/ExploreTopBar.vue";
import ResultsToolbar from "./components/ResultsToolbar.vue";
import WindowedSearchPanel from "./components/WindowedSearchPanel.vue";
import ExploreJsonResults from "./components/ExploreJsonResults.vue";
import LiveTailPanel from "./components/LiveTailPanel.vue";
import AskChefDrawer from "@/components/ask-chef/AskChefDrawer.vue";
import { ResizablePanel, ResizablePanelGroup } from "@/components/ui/resizable";

const { t } = useI18n();

// Router and stores
const router = useRouter();
const exploreStore = useExploreStore();
const runs = useExploreRuns();
const teamsStore = useTeamsStore();
const sourcesStore = useSourcesStore();
const savedQueriesStore = useSavedQueriesStore();
const preferencesStore = usePreferencesStore();
const { preferences } = storeToRefs(preferencesStore);
const { toast } = useToast();

const {
  logchefQuery,
  sqlQuery,
  activeMode,
  changeMode,
} = useQuery();
const isExecutingQuery = computed(() => runs.isRunning);
const canExecuteQuery = computed(() => exploreStore.canExecuteQuery);
const rows = computed(() => runs.results?.rows ?? []);

const nav = useExplorerNavigator();
const isInitializing = computed(() => nav.status.value === 'loading' || nav.status.value === 'error');
const initializationError = nav.error;

// Use the new clean team/source management
const contextStore = useContextStore();
// Team/source management - now centralized in sourcesStore
const availableSources = computed(() => sourcesStore.teamSources);
const sourceDetails = computed(() => sourcesStore.currentSourceDetails);
const editorSchema = computed(() => {
  const schema: Record<string, { type: string }> = {};
  for (const column of sourceDetails.value?.columns || []) {
    if (column.name && column.type) {
      schema[column.name] = { type: column.type };
    }
  }
  return schema;
});
const supportsLogchefQL = computed(() => supportsQueryLanguage(sourceDetails.value, 'logchefql'));
const supportsExports = computed(() => hasSourceCapability(sourceDetails.value, 'exports'));
const hasValidSource = computed(() => sourcesStore.hasValidCurrentSource);
const isLoadingTeamSources = computed(() => sourcesStore.isLoadingTeamSources);

// Computed properties for the clean approach
const currentTeamId = computed(() => contextStore.teamId);
const currentSourceId = computed(() => contextStore.sourceId);
const selection = computed(() => nav.pending.value ?? { teamId: currentTeamId.value, sourceId: currentSourceId.value });
const selectableSources = computed(() => selection.value.teamId === contextStore.teamId ? availableSources.value : []);
const availableTeams = computed(() => teamsStore.teams || []);
const selectedSourceName = computed(() => {
  if (!currentSourceId.value) return t('ui.selectSource');
  const source = availableSources.value.find(s => s.id === currentSourceId.value);
  return source ? source.name : t('ui.selectSource');
});

// Available fields for sidebar/autocompletion
const availableFields = computed(() => {
  if (!sourceDetails.value?.columns) return [];
  return [...sourceDetails.value.columns].sort((a, b) => a.name.localeCompare(b.name));
});

const isExporting = ref(false);
let exportAbortController: AbortController | null = null;

const {
  showSaveQueryModal,
  handleSaveQueryClick: openSaveModalFlow,
  handleSaveAsNewQueryClick: openSaveAsNewModalFlow,
  closeSaveQueryModal,
  handleSaveQuery: processSaveQueryFromComposable,
} = useSavedQueries();

// Create default empty parsed query state
const EMPTY_PARSED_QUERY = {
  success: false,
  meta: { fieldsUsed: [], conditions: [] },
};

// Basic state
// Sidebar defaults to open, but respects user's saved preference
const showFieldsPanel = computed({
  get: () => preferences.value.fields_panel_open,
  set: (value: boolean) => {
    preferencesStore.updatePreferences({ fields_panel_open: value });
  },
});
const queryEditorRef = ref<ComponentPublicInstance<{
  focus: (revealLastPosition?: boolean) => void;
  code?: { value: string };
  toggleSqlEditorVisibility?: () => void;
}> | null>(null);
const isLoadingQuery = ref(false);
const editQueryData = ref<SavedQuery | null>(null);
const topBarRef = ref<InstanceType<typeof ExploreTopBar> | null>(null);
const sortKeysInfoOpen = ref(false); // State for sort keys info expandable section
const isHistogramVisible = ref(true);

// Display mode for table vs compact view (table is default)
const displayMode = computed({
  get: () => preferences.value.display_mode,
  set: (value: 'table' | 'compact' | 'json') => {
    preferencesStore.updatePreferences({ display_mode: value });
  },
});

// UI state computed properties
const showLoadingState = computed(
  () => isInitializing.value && !initializationError.value
);

// A location is opening. It starts its run as soon as it opens.
const isInitialQueryPending = computed(() => nav.status.value === 'navigating');

const showNoTeamsState = computed(
  () =>
    !isInitializing.value &&
    !initializationError.value &&
    (!availableTeams.value || availableTeams.value.length === 0)
);

const showNoSourcesState = computed(
  () =>
    !isInitializing.value &&
    !showNoTeamsState.value &&
    contextStore.hasTeam &&
    (!availableSources.value || availableSources.value.length === 0) &&
    !isLoadingTeamSources.value
);

// Computed property to show the "Source Not Connected" state
const showSourceNotConnectedState = computed(() => {
  // Don't show during init, if no teams/sources, or no source selected
  if (
    isInitializing.value ||
    showNoTeamsState.value ||
    showNoSourcesState.value ||
    !currentSourceId.value
  ) {
    return false;
  }
  // Show only if details *have* loaded AND the source is invalid/disconnected
  return (
    sourcesStore.currentSourceDetails?.id === currentSourceId.value &&
    !sourcesStore.hasValidCurrentSource
  );
});

// Can save or update query?
const canSaveOrUpdateQuery = computed(() => {
  return (
    !!currentTeamId.value &&
    !!currentSourceId.value &&
    hasValidSource.value &&
    (!!exploreStore.logchefqlCode?.trim() || !!exploreStore.nativeQuery?.trim())
  );
});

// Current query content as JSON for SaveQueryModal
const currentQueryContentJson = computed(() => {
  return JSON.stringify({
    sourceId: currentSourceId.value,
    limit: exploreStore.limit,
    content: exploreStore.activeMode === 'logchefql' 
      ? exploreStore.logchefqlCode 
      : exploreStore.nativeQuery,
  });
});

const parsedQuery = computed(() => {
  const compilation = runs.results?.compilation;
  if (!runs.shown?.query.text.trim() || !compilation) return EMPTY_PARSED_QUERY;
  return { success: true, meta: compilation };
});

// Use structured data for query fields
const queryFields = computed(() => {
  if (!parsedQuery.value.success) return [];
  return parsedQuery.value.meta?.fieldsUsed || [];
});

// Use structured data for regex patterns
const regexHighlights = computed(() => {
  const highlights: Record<string, { pattern: string; isNegated: boolean }> =
    {};

  if (!parsedQuery.value.success) return highlights;

  // Extract only regex conditions
  const regexConditions = (parsedQuery.value.meta?.conditions || []).filter(
    (cond: QueryCondition) => cond.is_regex
  );

  // Process each regex condition
  regexConditions.forEach((cond: QueryCondition) => {
    let pattern = cond.value;
    // Remove quotes if they exist
    if (
      (pattern.startsWith('"') && pattern.endsWith('"')) ||
      (pattern.startsWith("'") && pattern.endsWith("'"))
    ) {
      pattern = pattern.slice(1, -1);
    }

    highlights[cond.field] = {
      pattern,
      isNegated: cond.operator === "!~",
    };
  });

  return highlights;
});

// Function to handle drill-down from DataTable to add a filter condition
const handleDrillDown = (data: {
  column: string;
  value: any;
  operator: string;
}) => {
  // Only handle in LogchefQL mode
  if (activeMode.value !== "logchefql") return;

  const { column, value, operator } = data;

  // Create a new condition based on the column and value
  let newCondition = "";
  let formattedValue = "";

  // Format value appropriately
  if (value === null || value === undefined) {
    formattedValue = "null";
  } else if (typeof value === "string") {
    // Escape quotes in the string value
    const escapedValue = value.replace(/"/g, '\\"');
    formattedValue = `"${escapedValue}"`;
  } else if (typeof value === "number" || typeof value === "boolean") {
    formattedValue = String(value);
  } else {
    // Convert objects to string representation
    try {
      formattedValue = `"${JSON.stringify(value).replace(/"/g, '\\"')}"`;
    } catch (e) {
      formattedValue = `"${String(value).replace(/"/g, '\\"')}"`;
    }
  }

  // Create the condition based on the operator
  newCondition = `${column}${operator}${formattedValue}`;

  // Get the current query
  let currentQuery = logchefQuery.value?.trim() || "";

  // If there's already a query, append the new condition with "and"
  if (currentQuery) {
    // Check if we need to wrap existing query in parentheses
    if (currentQuery.includes(" or ") && !currentQuery.startsWith("(")) {
      currentQuery = `(${currentQuery})`;
    }
    currentQuery = `${currentQuery} and ${newCondition}`;
  } else {
    currentQuery = newCondition;
  }

  // Update the query
  logchefQuery.value = currentQuery;

  // Focus the editor and move cursor to the end of the query
  nextTick(() => {
    queryEditorRef.value?.focus(true);
  });
};

// Event Handlers for QueryEditor
const updateLogchefqlValue = (newValue: string, _isUserInput = false) => {
  // Use the store's action to update LogchefQL code
  exploreStore.setLogchefqlCode(newValue);
};

const updateSqlValue = (newValue: string, _isUserInput = false) => {
  // Use the store's action to update SQL
  exploreStore.setNativeQuery(newValue);
};

// New handler for the Save/Update button
const handleSaveOrUpdateClick = async () => {
  const queryId = exploreStore.selectedQueryId;

  // Check if we can save
  if (!canSaveOrUpdateQuery.value) {
    toast({
      get title() { return t('ui.cannotSaveQuery'); },
      variant: "destructive",
      get description() { return t('ui.missingRequiredFieldsTeamSourceQuery'); },
      duration: TOAST_DURATION.WARNING,
    });
    return;
  }

  if (queryId) {
    // --- Update Existing Query Flow ---
    try {
      isLoadingQuery.value = true;
      const result = await savedQueriesStore.fetchById(queryId);

      if (result.success && savedQueriesStore.selectedQuery) {
        const existingQuery = savedQueriesStore.selectedQuery;

        // Open the edit modal with the existing query's details
        showSaveQueryModal.value = true;
        editQueryData.value = existingQuery;
      } else {
        throw new Error("Failed to load query details");
      }
    } catch (error) {
      console.error(`Error loading query for edit:`, error);
      toast({
        get title() { return t('ui.error'); },
        get description() { return t('ui.failedToLoadQueryDetailsForEditing'); },
        variant: "destructive",
        duration: TOAST_DURATION.ERROR,
      });
    } finally {
      isLoadingQuery.value = false;
    }
  } else {
    // --- Save New Query Flow ---
    editQueryData.value = null; // Reset edit data
    openSaveModalFlow(); // Call the composable's function to open the modal
  }
};

// Handle updating an existing query
async function handleUpdateQuery(queryId: string, formData: SaveQueryFormData, done?: () => void) {
  try {
    const saved = await nav.recordAfter(async () => {
      const response = await savedQueriesStore.update(queryId, {
        name: formData.name,
        description: formData.description,
        query_language: formData.query_language,
        editor_mode: formData.editor_mode,
        query_content: formData.query_content,
      });
      if (!response.success) throw new Error(getErrorMessage(response.error) || "Failed to update query");
      return response.data ?? null;
    }, exploreStore.markSavedQuery);
    if (saved) {
      showSaveQueryModal.value = false;
      editQueryData.value = null;
    }
  } catch (error) {
    console.error("Error updating query:", error);
    toast({
      get title() { return t('ui.error'); },
      description: getErrorMessage(error),
      variant: "destructive",
      duration: TOAST_DURATION.ERROR,
    });
  } finally {
    done?.();
  }
}

const onHistogramTimeRangeZoom = (range: { start: Date; end: Date }) => {
  const current = runs.shown?.resolved;
  const start = epochMs(range.start);
  const end = epochMs(range.end);
  if (current && Math.abs(current.start - start) < 1000 && Math.abs(current.end - end) < 1000) return;
  nav.setWindow({ kind: 'absolute', start, end });
};

// Open the date picker programmatically
const openDatePicker = () => {
  if (topBarRef.value) {
    topBarRef.value.openDatePicker();
  }
};

const sleep = (ms: number) => new Promise(resolve => setTimeout(resolve, ms));

const triggerBrowserDownload = (downloadUrl: string, fileName?: string) => {
  const link = document.createElement("a");
  link.href = downloadUrl;
  if (fileName) {
    link.download = fileName;
  }
  document.body.appendChild(link);
  link.click();
  link.remove();
};

class ExportAbortedError extends Error {
  constructor() {
    super("Export polling aborted");
    this.name = "ExportAbortedError";
  }
}

const waitForExportCompletion = async (exportId: string, timeoutSeconds: number, signal: AbortSignal) => {
  if (!currentTeamId.value || !currentSourceId.value) {
    throw new Error("Team and source are required for export");
  }

  const deadline = Date.now() + (timeoutSeconds + 60) * 1000;
  while (Date.now() < deadline) {
    if (signal.aborted) throw new ExportAbortedError();
    const response = await exploreApi.getExportJob(currentSourceId.value, exportId, currentTeamId.value);
    if (signal.aborted) throw new ExportAbortedError();
    const job = response.data;
    if (!job) {
      throw new Error("Export status is unavailable");
    }
    if (job.status === "complete") {
      return job;
    }
    if (job.status === "failed") {
      throw new Error(job.error_message || "Export failed");
    }
    await sleep(1000);
  }

  throw new Error("Export is taking longer than expected. Please try again.");
};

const handleExport = async () => {
  if (!currentTeamId.value || !currentSourceId.value) return;

  const shown = runs.shown;
  const sql = shown?.results?.sql;
  if (!shown || !sql?.trim()) {
    toast({
      get title() { return t('ui.cannotDownload'); },
      get description() { return t('ui.runTheQueryBeforeDownloadingResults'); },
      variant: "destructive",
      duration: TOAST_DURATION.WARNING,
    });
    return;
  }

  exportAbortController?.abort();
  exportAbortController = new AbortController();
  const signal = exportAbortController.signal;

  try {
    isExporting.value = true;
    const queryTimeout = Math.max(exploreStore.queryTimeout, 120);
    const response = await exploreApi.createExportJob(currentSourceId.value, {
      query_text: sql,
      format: "csv",
      query_timeout: queryTimeout,
      variables: shown.variables,
    }, currentTeamId.value);
    const job = response.data;
    if (!job?.id) {
      throw new Error("Export job was not created");
    }

    const completedJob = await waitForExportCompletion(job.id, queryTimeout, signal);
    if (!completedJob.download_url) {
      throw new Error("Export download is unavailable");
    }

    triggerBrowserDownload(completedJob.download_url, completedJob.file_name);
  } catch (error: any) {
    if (error instanceof ExportAbortedError) return;
    toast({
      get title() { return t('ui.downloadFailed'); },
      description: getErrorMessage(error),
      variant: "destructive",
      duration: TOAST_DURATION.ERROR,
    });
  } finally {
    if (exportAbortController?.signal === signal) {
      exportAbortController = null;
    }
    isExporting.value = false;
  }
};

onBeforeUnmount(() => {
  exportAbortController?.abort();
  exportAbortController = null;
  // Abort any in-flight live tail stream when the explorer unmounts.
  exploreStore.stopLiveTail();
});

// Leaving the explore route must also abort the tail (unmount may lag a
// keep-alive transition, and this guarantees the fetch is cancelled).
onBeforeRouteLeave(() => {
  exploreStore.stopLiveTail();
});

const handleShare = async () => {
  try {
    const share = await nav.recordAfter(() => exploreStore.createQueryShare(), exploreStore.markShare);
    if (!share?.share_url) return;

    await copyText(share.share_url);

    // An unedited saved query keeps its id in the URL.
    const recorded = router.currentRoute.value.query.share === share.token;
    toast({
      get title() { return t('ui.shareLinkCopied'); },
      get description() { return recorded ? t('ui.uRLUpdatedToTheShareLinkAndCopiedToClipboard') : undefined; },
      duration: TOAST_DURATION.SUCCESS,
    });
  } catch (error: any) {
    toast({
      get title() { return t('ui.shareFailed'); },
      description: error?.message || getErrorMessage(error),
      variant: "destructive",
      duration: TOAST_DURATION.ERROR,
    });
  }
};

// Function to generate example query based on sort keys
const getSortKeyExampleQuery = (): string => {
  if (!sourceDetails.value?.sort_keys?.length) return "";

  // Filter out timestamp field if it's the last sort key
  const relevantKeys: string[] = [];
  const sortKeys = sourceDetails.value.sort_keys;
  const metaTsField = sourceDetails.value._meta_ts_field;

  sortKeys.forEach((key, index) => {
    const isLastKey = index === sortKeys.length - 1;
    const isTimestampField = key === metaTsField;

    // Only exclude if it's both the timestamp field AND the last key
    if (!(isTimestampField && isLastKey)) {
      relevantKeys.push(key);
    }
  });

  // Generate query example with the keys
  if (relevantKeys.length === 0) return "";

  return relevantKeys.map((key) => `${key}="example"`).join(" and ");
};

// After a run, nudge toward the sort keys only while the query filters on none of them.
const missingSortKeyFilters = computed(() => {
  if (!runs.results || !sourceDetails.value?.sort_keys?.length) return [];
  const mode = activeMode.value === "logchefql" ? "logchefql" : "native";
  const query = (mode === "logchefql" ? logchefQuery.value : sqlQuery.value) ?? "";
  return unfilteredSortKeys(query, mode, sourceDetails.value.sort_keys, sourceDetails.value._meta_ts_field);
});

const addSortKeyFilter = (field: string) => {
  if (activeMode.value !== "logchefql") return;
  appendLogchefqlCondition(`${field}="example"`);
};

// Function to add sort key example to the query editor
const addSortKeyExample = () => {
  if (activeMode.value !== "logchefql") return;

  const exampleQuery = getSortKeyExampleQuery();
  if (!exampleQuery) return;
  appendLogchefqlCondition(exampleQuery);
};

const appendLogchefqlCondition = (exampleQuery: string) => {

  // Get current query
  let currentQuery = logchefQuery.value?.trim() || "";

  // If there's already a query, append the new condition with "and"
  if (currentQuery) {
    // Check if we need to wrap existing query in parentheses
    if (currentQuery.includes(" or ") && !currentQuery.startsWith("(")) {
      currentQuery = `(${currentQuery})`;
    }
    currentQuery = `${currentQuery} and ${exampleQuery}`;
  } else {
    currentQuery = exampleQuery;
  }

  // Update the query through the store
  exploreStore.setLogchefqlCode(currentQuery);

  // Focus the editor and move cursor to the end
  nextTick(() => {
    queryEditorRef.value?.focus(true);

    // Expand the sort keys info panel
    sortKeysInfoOpen.value = true;
  });
};

// Histogram visibility toggle
const toggleHistogramVisibility = () => {
  isHistogramVisible.value = !isHistogramVisible.value;
};

// Auto-hide histogram when it's not eligible (e.g., in SQL mode)
watch(
  () => runs.isHistogramEligible,
  (isEligible) => {
    if (!isEligible && isHistogramVisible.value) {
      isHistogramVisible.value = false;
    }
  }
);

// AI SQL generation handler (now handled inline in QueryEditor)
const handleGenerateAISQL = async ({ naturalLanguageQuery }: { naturalLanguageQuery: string }) => {
  try {
    if (!currentSourceId.value) {
      toast({
        get title() { return t('ui.error'); },
        get description() { return t('ui.pleaseSelectASourceBeforeUsingTheAIAssistant'); },
        variant: "destructive",
        duration: TOAST_DURATION.ERROR,
      });
      return;
    }

    // Get the current query based on active mode
    let currentQuery = "";
    if (activeMode.value === "logchefql" && logchefQuery.value) {
      currentQuery = logchefQuery.value.trim();
    } else if (activeMode.value === "native" && sqlQuery.value) {
      currentQuery = sqlQuery.value.trim();
    }

    // Generate AI SQL and store result for the QueryEditor to access
    await exploreStore.generateAiSql(naturalLanguageQuery, currentQuery);

    // The AI dialog in QueryEditor will handle success/error display and insertion
  } catch (error) {
    console.error("Error generating AI SQL:", error);
    // The store will have the error state that the AI dialog can display
  }
};

const formatLogsqlValue = (value: string) => {
  if (/^-?(?:\d+|\d*\.\d+)$/.test(value) || /^(?:true|false)$/i.test(value)) {
    return value;
  }
  return `"${value.replace(/\\/g, '\\\\').replace(/"/g, '\\"')}"`;
};

const appendLogsqlExpression = (query: string, expression: string) => {
  const trimmed = query.trim();
  if (!trimmed) {
    return expression;
  }

  const pipeIndex = trimmed.indexOf('|');
  if (pipeIndex === -1) {
    return `${trimmed} ${expression}`;
  }

  const base = trimmed.slice(0, pipeIndex).trimEnd();
  const pipeSection = trimmed.slice(pipeIndex).trimStart();
  const nextBase = base ? `${base} ${expression}` : expression;
  return `${nextBase} ${pipeSection}`;
};

// Handle adding a field filter from the sidebar
const handleAddFieldFilter = (field: string, value: string, operator: '=' | '!=') => {
  if (!supportsLogchefQL.value) {
    const exactFilter = `${field}:=${formatLogsqlValue(value)}`;
    const filterExpression = operator === '!=' ? `NOT ${exactFilter}` : exactFilter;
    const currentQuery = exploreStore.nativeQuery?.trim() || '';

    exploreStore.setNativeQuery(appendLogsqlExpression(currentQuery, filterExpression));

    if (activeMode.value !== 'native') {
      changeMode('native');
    }

    nextTick(() => {
      queryEditorRef.value?.focus(true);
    });
    return;
  }

  // Build the filter expression
  const needsQuotes = !/^\d+$/.test(value); // Only numbers don't need quotes
  const quotedValue = needsQuotes ? `"${value.replace(/"/g, '\\"')}"` : value;
  const filterExpression = `${field}${operator}${quotedValue}`;
  
  // Get current query
  const currentQuery = exploreStore.logchefqlCode?.trim() || '';
  
  // Build new query - append with 'and' if there's existing content
  let newQuery: string;
  if (currentQuery) {
    newQuery = `${currentQuery} and ${filterExpression}`;
  } else {
    newQuery = filterExpression;
  }
  
  // Update the store
  exploreStore.setLogchefqlCode(newQuery);
  
  // Switch to LogchefQL mode if not already
  if (activeMode.value !== 'logchefql') {
    changeMode('logchefql');
  }
  
  // Focus the editor
  nextTick(() => {
    queryEditorRef.value?.focus(true);
  });
};

// Handle field name click from sidebar - inserts field name into query
const handleFieldClick = (fieldName: string) => {
  if (!supportsLogchefQL.value) {
    const currentQuery = exploreStore.nativeQuery?.trim() || '';
    exploreStore.setNativeQuery(appendLogsqlExpression(currentQuery, `${fieldName}:=`));

    if (activeMode.value !== 'native') {
      changeMode('native');
    }

    nextTick(() => {
      queryEditorRef.value?.focus(true);
    });
    return;
  }

  // Get current query
  const currentQuery = exploreStore.logchefqlCode?.trim() || '';
  
  // Append field name with equals operator, ready for user to type value
  let newQuery: string;
  if (currentQuery) {
    newQuery = `${currentQuery} and ${fieldName}=`;
  } else {
    newQuery = `${fieldName}=`;
  }
  
  // Update the store
  exploreStore.setLogchefqlCode(newQuery);
  
  // Switch to LogchefQL mode if not already
  if (activeMode.value !== 'logchefql') {
    changeMode('logchefql');
  }
  
  // Focus the editor
  nextTick(() => {
    queryEditorRef.value?.focus(true);
  });
};

// Filtered sort keys computed property
const filteredSortKeys = computed(() => {
  if (!sourceDetails.value?.sort_keys) return [];
  return sourceDetails.value.sort_keys.filter(
    (k, i) => k !== sourceDetails.value?._meta_ts_field || i === 0
  );
});

// New handler for save-as-new request from QueryEditor
const handleRequestSaveAsNew = () => {
  editQueryData.value = null; // Ensure modal opens in "new query" mode
  openSaveAsNewModalFlow(); // Keep the open saved query from selecting update mode.
};

// Wrapper for the modal's @save event
const onSaveQueryModalSave = async (formData: SaveQueryFormData, done?: () => void) => {
  try {
    await nav.recordAfter(() => processSaveQueryFromComposable(formData), exploreStore.markSavedQuery);
  } finally {
    done?.();
  }
};

</script>

<template>
  <KeepAlive>
    <div class="log-explorer-wrapper">
      <!-- Loading State -->
      <div v-if="showLoadingState" class="flex items-center justify-center h-[calc(100vh-12rem)]">
        <p class='text-muted-foreground animate-pulse'>{{ t('ui.loadingExplorer') }}</p>
      </div>

      <!-- Initialization Error State -->
      <div v-else-if="initializationError"
        class="flex flex-col items-center justify-center h-[calc(100vh-12rem)] gap-4 text-center px-4">
        <div class="w-16 h-16 rounded-full bg-muted flex items-center justify-center">
          <svg xmlns="http://www.w3.org/2000/svg" width="32" height="32" viewBox="0 0 24 24" fill="none"
            stroke="currentColor" stroke-width="1.5" stroke-linecap="round" stroke-linejoin="round"
            class="text-muted-foreground">
            <circle cx="12" cy="12" r="10"></circle>
            <line x1="12" y1="8" x2="12" y2="12"></line>
            <line x1="12" y1="16" x2="12.01" y2="16"></line>
          </svg>
        </div>
        <h2 class='text-xl font-semibold'>{{ t('ui.unableToLoadExplorer') }}</h2>
        <p class="text-muted-foreground max-w-md">{{ initializationError }}</p>
        <div class="flex gap-3">
          <Button variant="outline" @click="nav.retry()">
            <svg xmlns="http://www.w3.org/2000/svg" width="16" height="16" viewBox="0 0 24 24" fill="none"
              stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round" class="mr-2">
              <path d="M21 12a9 9 0 0 0-9-9 9.75 9.75 0 0 0-6.74 2.74L3 8"></path>
              <path d="M3 3v5h5"></path>
              <path d="M3 12a9 9 0 0 0 9 9 9.75 9.75 0 0 0 6.74-2.74L21 16"></path>
              <path d="M16 21h5v-5"></path>
            </svg>
            {{ t('ui.retry') }}
          </Button>
        </div>
      </div>

      <!-- No Teams State -->
      <div v-else-if="showNoTeamsState"
        class="flex flex-col items-center justify-center h-[calc(100vh-12rem)] gap-4 text-center px-4">
        <div class="w-16 h-16 rounded-full bg-muted flex items-center justify-center">
          <svg xmlns="http://www.w3.org/2000/svg" width="32" height="32" viewBox="0 0 24 24" fill="none"
            stroke="currentColor" stroke-width="1.5" stroke-linecap="round" stroke-linejoin="round"
            class="text-muted-foreground">
            <path d="M16 21v-2a4 4 0 0 0-4-4H6a4 4 0 0 0-4 4v2"></path>
            <circle cx="9" cy="7" r="4"></circle>
            <path d="M22 21v-2a4 4 0 0 0-3-3.87"></path>
            <path d="M16 3.13a4 4 0 0 1 0 7.75"></path>
          </svg>
        </div>
        <h2 class='text-xl font-semibold'>{{ t('ui.noTeamsAvailable') }}</h2>
        <p class="text-muted-foreground max-w-md">
          {{ t('ui.youNeedToBePartOfATeamToExploreLogsContact') }}
        </p>
      </div>

      <!-- No Sources State (Team Selected) -->
      <div v-else-if="showNoSourcesState" class="flex flex-col h-[calc(100vh-12rem)]">
        <!-- Header bar for team selection -->
        <div class="border-b py-2 px-4 flex items-center h-12">
          <TeamSourceSelector 
            :current-team-id="selection.teamId"
            :current-source-id="selection.sourceId"
            :available-teams="availableTeams"
            :available-sources="selectableSources"
            @update:team="nav.selectTeam"
            @update:source="nav.selectSource"
          />
        </div>
        <!-- Empty state content -->
        <div class="flex flex-col items-center justify-center flex-1 gap-4 text-center">
          <h2 class='text-2xl font-semibold'>{{ t('ui.noLogSourcesFound') }}</h2>
          <p class="text-muted-foreground max-w-md">
            {{ t('explore.noTeamSources') }}
          </p>
        </div>
      </div>

      <!-- Source Not Connected State -->
      <div v-else-if="showSourceNotConnectedState" class="flex flex-col h-screen overflow-hidden">
        <!-- Filter Bar with Team/Source Selection -->
        <div class="border-b bg-background py-2 px-4 flex items-center h-12 shadow-sm">
          <TeamSourceSelector 
            :current-team-id="selection.teamId"
            :current-source-id="selection.sourceId"
            :available-teams="availableTeams"
            :available-sources="selectableSources"
            @update:team="nav.selectTeam"
            @update:source="nav.selectSource"
          />
        </div>

        <!-- Source Not Connected Message -->
        <div class="flex-1 flex flex-col items-center justify-center p-8">
          <div class="max-w-xl w-full bg-destructive/10 border border-destructive/20 rounded-lg p-6 text-center">
            <svg xmlns="http://www.w3.org/2000/svg" width="48" height="48" viewBox="0 0 24 24" fill="none"
              stroke="currentColor" stroke-width="1.5" stroke-linecap="round" stroke-linejoin="round"
              class="mx-auto mb-4 text-destructive">
              <path d="M18 6 6 18"></path>
              <path d="m6 6 12 12"></path>
            </svg>
            <h2 class='text-xl font-semibold mb-2'>{{ t('ui.sourceNotConnected') }}</h2>
            <p class="text-muted-foreground mb-4">
              {{ t('explore.disconnectedSource', { name: selectedSourceName }) }}
            </p>

            <div class="flex items-center justify-center gap-3">
              <Button variant="outline" @click="
                router.push({
                  name: 'SourceSettings',
                  params: { sourceId: currentSourceId },
                })
                ">
                {{ t('ui.configureSource') }}
              </Button>
              <Button variant="default" @click="router.push({ name: 'NewSource' })">
                {{ t('ui.addNewSource') }}
              </Button>
            </div>
          </div>
        </div>
      </div>

      <!-- Main Explorer View -->
      <div v-else class="flex flex-col h-screen overflow-hidden">
        <h1 class="sr-only">{{ t('routes.logExplorer') }}</h1>
        <!-- Streamlined Top Bar -->
        <ExploreTopBar
          ref="topBarRef"
          :selection="selection"
          :available-teams="availableTeams"
          :available-sources="selectableSources"
          :selected-source="sourceDetails"
          @update:team="nav.selectTeam"
          @update:source="nav.selectSource"
          @update:window="nav.setWindow"
          @update:limit="nav.setLimit"
          @share="handleShare"
        />

        <!-- Main Content Area -->
        <div class="flex flex-1 min-h-0">
          <FieldSideBar 
            v-model:expanded="showFieldsPanel" 
            :fields="availableFields"
            :team-id="currentTeamId ?? undefined"
            :source-id="currentSourceId ?? undefined"
            :source="sourceDetails"
            @add-filter="handleAddFieldFilter"
            @field-click="handleFieldClick"
          />

          <ResizablePanelGroup direction="horizontal" auto-save-id="explore-ask-chef" class="min-w-0 flex-1">
          <ResizablePanel id="explore-main" :order="1" class="flex flex-col h-full min-w-0 overflow-hidden">
            <!-- Query Editor Section -->
            <div>
              <!-- Query Editor -->
              <template v-if="
                currentSourceId && hasValidSource && exploreStore.hasWindow
              ">
                <div class="bg-card border-b">
                  <QueryEditor 
                    ref="queryEditorRef" 
                    :sourceId="currentSourceId" 
                    :sourceType="sourceDetails?.source_type || 'clickhouse'"
                    :queryLanguages="sourceDetails?.query_languages || []"
                    :capabilities="sourceDetails?.capabilities || []"
                    :teamId="currentTeamId ?? 0" 
                    :schema="editorSchema"
                    :activeMode="exploreStore.activeMode === 'logchefql' ? 'logchefql' : 'clickhouse-sql'"
                    :value="exploreStore.activeMode === 'logchefql' ? logchefQuery : sqlQuery"
                    :tsField="sourceDetails?._meta_ts_field || 'timestamp'"
                    :tableName="sourcesStore.getCurrentSourceTableName || ''" 
                    :showFieldsPanel="showFieldsPanel"
                    :isExecuting="isExecutingQuery"
                    :canExecute="canExecuteQuery"
                    :showRunButton="true"
                    @change="(event) => event.mode === 'logchefql'
                      ? updateLogchefqlValue(event.query, event.isUserInput)
                      : updateSqlValue(event.query, event.isUserInput)"
                    @execute="nav.run"
                    @new-query="nav.newQuery"
                    @cancel-query="nav.cancel()"
                    @update:activeMode="(mode, isModeSwitchOnly) =>
                      changeMode(mode === 'logchefql' ? 'logchefql' : 'native', isModeSwitchOnly)"
                    @toggle-fields="showFieldsPanel = !showFieldsPanel" 
                    @select-saved-query="nav.openSavedQuery"
                    @save-query="handleSaveOrUpdateClick" 
                    @save-query-as-new="handleRequestSaveAsNew"
                    @generate-ai-sql="handleGenerateAISQL" 
                    class="border-0">
                    <template v-if="sourceDetails?.sort_keys?.some((key) => key !== sourceDetails?._meta_ts_field)" #hint>
                      <div class="flex items-center gap-1.5 text-xs text-muted-foreground">
                        <Info class="h-3 w-3 flex-shrink-0 text-blue-600 dark:text-blue-400" />
                        <span>
                          <span class="font-medium text-blue-700 dark:text-blue-300">{{ t('ui.tip') }}</span>
                          {{ ' ' }}
                          <i18n-t keypath="explore.filterHint" scope="global">
                            <template #fields>
                              <span v-for="(key, idx) in filteredSortKeys" :key="key">
                                <code class="px-1 bg-muted rounded text-foreground/80">{{ key }}</code>
                                <span v-if="idx < filteredSortKeys.length - 1">, </span>
                              </span>
                            </template>
                          </i18n-t>
                        </span>
                        <button
                          v-if="activeMode === 'logchefql'"
                          @click="addSortKeyExample"
                          class="pointer-events-auto flex-shrink-0 whitespace-nowrap px-1.5 py-0.5 rounded font-medium text-blue-700 hover:bg-blue-600/10 dark:text-blue-300 transition-colors"
                        >
                          {{ t('ui.addExample') }}
                        </button>
                      </div>
                    </template>
                  </QueryEditor>
                </div>
              </template>

              <!-- "Select source" message - only when no source selected -->
              <template v-else-if="currentTeamId && !currentSourceId">
                <div class="flex items-center justify-center min-h-[400px]">
                  <div class="text-center max-w-md mx-auto">
                    <div class="w-16 h-16 mx-auto mb-4 rounded-full bg-muted/50 flex items-center justify-center">
                      <svg xmlns="http://www.w3.org/2000/svg" width="32" height="32" viewBox="0 0 24 24" fill="none"
                        stroke="currentColor" stroke-width="1.5" stroke-linecap="round" stroke-linejoin="round"
                        class="text-muted-foreground/70">
                        <path d="M3 7v10a2 2 0 0 0 2 2h14a2 2 0 0 0 2-2V9a2 2 0 0 0-2-2H5a2 2 0 0 0-2 2z" />
                        <polyline points="3,7 12,13 21,7" />
                      </svg>
                    </div>
                    <h3 class='text-lg font-medium mb-2'>{{ t('ui.selectALogSource') }}</h3>
                    <p class="text-sm text-muted-foreground mb-4">
                      {{ t('ui.chooseALogSourceFromTheDropdownAboveToStartExploringYour') }}
                    </p>
                    <div class="text-xs text-muted-foreground/70">
                      {{ t('ui.needToAddANewSourceClickAddSourceInTheSelector') }}
                    </div>
                  </div>
                </div>
              </template>

              <!-- Loading fallback - for any other state -->
              <template v-else>
                <div class="flex items-center justify-center p-6 m-3 border rounded-md bg-card shadow-sm">
                  <div class="text-center">
                    <p class="text-sm text-muted-foreground">
                      {{ t('ui.loadingExplorer2') }}
                    </p>
                  </div>
                </div>
              </template>

              <!-- Query Error Component -->
              <QueryError />
            </div>

            <!-- Results Toolbar + Histogram Section -->
            <div v-if="
              currentSourceId &&
              hasValidSource &&
              exploreStore.hasWindow
            ">
              <!-- Unified Results Toolbar -->
              <WindowedSearchPanel @stop="nav.cancel()" @page="nav.page" />
              <ResultsToolbar
                :isHistogramVisible="isHistogramVisible"
                :availableFields="availableFields"
                :displayMode="displayMode"
                :logsCount="rows.length"
                :isLoading="isExecutingQuery || isInitialQueryPending"
                :isExporting="isExporting"
                :canExport="supportsExports"
                :fastFilterFields="missingSortKeyFilters"
                :canAddFastFilter="activeMode === 'logchefql'"
                @add-fast-filter="addSortKeyFilter"
                @toggle-histogram="toggleHistogramVisibility"
                @update:displayMode="displayMode = $event"
                @export="handleExport"
              />

              <!-- Histogram visualization - paused during live tail -->
              <div v-if="isHistogramVisible && !exploreStore.isLive" class="px-3 py-0.5 border-b">
                <HistogramVisualization @zoom-time-range="onHistogramTimeRangeZoom" />
              </div>
            </div>

            <!-- Results Section -->
            <div class="flex-1 overflow-hidden flex flex-col" v-if="
              currentSourceId &&
              hasValidSource &&
              exploreStore.hasWindow
            ">
              <!-- Results Area -->
              <div class="flex-1 overflow-hidden relative bg-background">
                <!-- Live Tail Panel - takes over the results area while live -->
                <LiveTailPanel
                  v-if="exploreStore.isLive"
                  :rows="exploreStore.liveRows"
                  :status="exploreStore.liveStatus"
                  :notice="exploreStore.liveNotice"
                  :dropped-count="exploreStore.liveDroppedCount"
                  :end-reason="exploreStore.liveEndReason"
                  :end-message="exploreStore.liveEndMessage"
                  :error="exploreStore.liveError"
                  :timestamp-field="sourceDetails?._meta_ts_field"
                  @stop="exploreStore.stopLiveTail()"
                  @resume="exploreStore.startLiveTail()"
                />

                <!-- Results Table -->
                <template v-else-if="rows.length > 0 || isExecutingQuery || isInitialQueryPending">
                  <ExploreJsonResults
                    v-if="displayMode === 'json'"
                    :key="`${exploreStore.sourceId}-${exploreStore.activeMode}-${runs.shown?.id}-${displayMode}`"
                    :data="rows"
                    :is-loading="runs.showsLoading || isInitialQueryPending"
                  />

                  <component
                    v-else-if="runs.results?.columns.length"
                    :is="displayMode === 'table' ? DataTable : CompactLogList"
                    :key="`${exploreStore.sourceId}-${exploreStore.activeMode}-${runs.shown?.id}-${displayMode}`"
                    :columns="runs.results.columns as any"
                    :data="rows"
                    :stats="runs.results.stats"
                    :is-loading="runs.showsLoading || isInitialQueryPending"
                    :source-id="String(exploreStore.sourceId)"
                    :team-id="teamsStore.currentTeamId"
                    :source="sourceDetails"
                    :timestamp-field="sourcesStore.currentSourceDetails?._meta_ts_field"
                    :severity-field="sourcesStore.currentSourceDetails?._meta_severity_field"
                    :query-fields="queryFields"
                    :regex-highlights="regexHighlights"
                    :active-mode="activeMode"
                    :display-mode="displayMode"
                    @drill-down="handleDrillDown"
                    @update:display-mode="displayMode = $event"
                  />

                  <!-- Loading placeholder -->
                  <div v-else-if="isExecutingQuery || isInitialQueryPending"
                    class="absolute inset-0 flex items-center justify-center bg-background/70 z-10">
                    <p class="text-muted-foreground animate-pulse">
                      {{ t('ui.loadingResults') }}
                    </p>
                  </div>
                </template>

                <!-- Empty Results State Component -->
                <EmptyResultsState v-else :has-executed-query="runs.shown !== null"
                  :can-execute-query="canExecuteQuery" @run-default-query="nav.run"
                  @open-date-picker="openDatePicker" />
              </div>
            </div>
          </ResizablePanel>

          <AskChefDrawer />
          </ResizablePanelGroup>
        </div>

        <!-- Save Query Modal -->
        <SaveQueryModal v-if="showSaveQueryModal" :is-open="showSaveQueryModal" :query-type="exploreStore.activeMode"
          :edit-data="editQueryData" :query-content="currentQueryContentJson"
          @close="closeSaveQueryModal" @save="onSaveQueryModalSave" @update="handleUpdateQuery" />


      </div>
    </div>
  </KeepAlive>
</template>

<style scoped>
.required::after {
  content: " *";
  color: hsl(var(--destructive));
}

/* Add fade transition for the SQL preview */
.fade-enter-active,
.fade-leave-active {
  transition: opacity 0.15s ease;
}

.fade-enter-from,
.fade-leave-to {
  opacity: 0;
}

/* Improved table height handling */
.h-full {
  height: 100% !important;
}

/* Enhanced flex layout for proper table expansion */
.flex.flex-1.min-h-0 {
  display: flex;
  width: 100%;
  min-height: 0;
  flex: 1 1 auto !important;
  max-height: 100%;
}

.flex.flex-1.min-h-0>div:last-child {
  flex: 1 1 auto;
  min-width: 0;
  width: 100%;
  display: flex;
  flex-direction: column;
}

/* Fix for main content area to ensure full height expansion */
.flex-1.flex.flex-col.h-full.min-w-0 {
  flex: 1 1 auto !important;
  display: flex;
  flex-direction: column;
  min-height: 0;
  height: 100% !important;
  max-height: 100%;
}

/* Fix padding for main content area to eliminate y-scroll bar */
.log-explorer-wrapper {
  margin: -0.75rem;
}

/* Force the results section to expand fully */
.flex-1.overflow-hidden.flex.flex-col.border-t {
  flex: 1 1 auto !important;
  display: flex;
  flex-direction: column;
  min-height: 0;
  height: 100% !important;
}

/* Fix DataTable height issues */
:deep(.datatable-wrapper) {
  height: 100%;
  display: flex;
  flex-direction: column;
}

:deep(.datatable-container) {
  flex: 1;
  overflow: auto;
}

/* Table styling */
:deep(.table) {
  border-collapse: separate;
  border-spacing: 0;
}

:deep(.table th) {
  background-color: hsl(var(--muted));
  font-weight: 500;
  text-align: left;
  font-size: 0.85rem;
  color: hsl(var(--muted-foreground));
  padding: 0.75rem 1rem;
}

:deep(.table td) {
  padding: 0.65rem 1rem;
  border-bottom: 1px solid hsl(var(--border));
  font-size: 0.9rem;
}

:deep(.table tr:hover td) {
  background-color: hsl(var(--muted) / 0.3);
}

/* Severity label styling */
:deep(.severity-label) {
  border-radius: 4px;
  padding: 2px 6px;
  font-size: 0.75rem;
  font-weight: 500;
  display: inline-block;
}

:deep(.severity-error) {
  background-color: hsl(var(--destructive) / 0.15);
  color: hsl(var(--destructive));
}

:deep(.severity-warn),
:deep(.severity-warning) {
  background-color: hsl(var(--warning) / 0.15);
  color: hsl(var(--warning));
}

:deep(.severity-info) {
  background-color: hsl(var(--info) / 0.15);
  color: hsl(var(--info));
}

:deep(.severity-debug) {
  background-color: hsl(var(--muted) / 0.5);
  color: hsl(var(--muted-foreground));
}
</style>
