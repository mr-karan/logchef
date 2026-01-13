<script setup lang="ts">
import {
  ref,
  computed,
  watch,
  onMounted,
  onBeforeUnmount,
  nextTick,
} from "vue";
import { useRouter, useRoute } from "vue-router";
import { Button } from "@/components/ui/button";
import { useToast } from "@/composables/useToast";
import { TOAST_DURATION } from "@/lib/constants";
import { useExploreStore } from "@/stores/explore";
import { useTeamsStore } from "@/stores/teams";
import { useSourcesStore } from "@/stores/sources";
import { useSavedQueriesStore } from "@/stores/savedQueries";
import { FieldSideBar } from "@/components/field-sidebar";
import { getErrorMessage } from "@/api/types";
import DataTable from "./table/data-table.vue";
import CompactLogList from "./table/CompactLogListSimple.vue";
import SaveQueryModal from "@/components/collections/SaveQueryModal.vue";
import QueryEditor from "@/components/query-editor/QueryEditor.vue";
import { useSavedQueries } from "@/composables/useSavedQueries";
import { useUrlState } from "@/composables/useUrlState";
import { useQuery } from "@/composables/useQuery";
import { useTimeRange } from "@/composables/useTimeRange";

import { useContextStore } from "@/stores/context";
import type { ComponentPublicInstance } from "vue";
import type { SaveQueryFormData } from "@/views/explore/types";
import type { SavedTeamQuery } from "@/api/savedQueries";
import { logchefqlApi, type FilterCondition } from "@/api/logchefql";

// Type alias for backwards compatibility
type QueryCondition = FilterCondition;

// Import refactored components
import TeamSourceSelector from "./components/TeamSourceSelector.vue";
import QueryError from "./components/QueryError.vue";
import HistogramVisualization from "./components/HistogramVisualization.vue";
import EmptyResultsState from "./components/EmptyResultsState.vue";
import ExploreTopBar from "./components/ExploreTopBar.vue";
import ResultsToolbar from "./components/ResultsToolbar.vue";

// Router and stores
const router = useRouter();
const route = useRoute();
const exploreStore = useExploreStore();
const teamsStore = useTeamsStore();
const sourcesStore = useSourcesStore();
const savedQueriesStore = useSavedQueriesStore();
const { toast } = useToast();

const urlState = useUrlState();
const isInitializing = computed(() => urlState.state.value !== 'ready');
const initializationError = urlState.error;

const {
  logchefQuery,
  sqlQuery,
  activeMode,
  queryError,
  sqlWarnings: _sqlWarnings,
  isDirty,
  dirtyReason,
  isExecutingQuery,
  canExecuteQuery,
  changeMode,
  executeQuery,
  handleTimeRangeUpdate,
  handleLimitUpdate: _handleLimitUpdate,
} = useQuery();

const { handleHistogramTimeRangeZoom } = useTimeRange();

// Use the new clean team/source management
const contextStore = useContextStore();
// Team/source management - now centralized in sourcesStore
const availableSources = computed(() => sourcesStore.teamSources);
const sourceDetails = computed(() => sourcesStore.currentSourceDetails);
const hasValidSource = computed(() => sourcesStore.hasValidCurrentSource);
const isLoadingTeamSources = computed(() => sourcesStore.isLoadingTeamSources);
const isLoadingSourceDetails = computed(() => sourcesStore.isLoadingSourceDetails);
// Convenience aliases for template compatibility
const teamSources = availableSources;
const isProcessingTeamChange = isLoadingTeamSources;
const isProcessingSourceChange = isLoadingSourceDetails;

// Computed properties for the clean approach
const currentTeamId = computed(() => contextStore.teamId);
const currentSourceId = computed(() => contextStore.sourceId);
const availableTeams = computed(() => teamsStore.teams || []);
const selectedTeamName = computed(() => teamsStore.currentTeam?.name || 'Select team');
const selectedSourceName = computed(() => {
  if (!currentSourceId.value) return 'Select source';
  const source = availableSources.value.find(s => s.id === currentSourceId.value);
  return source ? source.name : 'Select source';
});

// Available fields for sidebar/autocompletion
const availableFields = computed(() => {
  if (!sourceDetails.value?.columns) return [];
  return [...sourceDetails.value.columns].sort((a, b) => a.name.localeCompare(b.name));
});

// Simple loading state for UI (replacement for isChangingContext)
const isChangingContext = computed(() => {
  console.log('Checking isChangingContext...');
  const teamLoading = sourcesStore.isLoadingTeamSources;
  const sourceLoading = sourcesStore.isLoadingSourceDetails;
  console.log(`Store loading states: team=${teamLoading}, source=${sourceLoading}`);
  const result = teamLoading || sourceLoading;
  console.log(`isChangingContext result: ${result}`);
  return result;
});

// Simple team/source change handlers using router
function handleTeamChange(teamIdStr: string) {
  const teamId = parseInt(teamIdStr);
  if (isNaN(teamId)) return;
  
  console.log(`LogExplorer: Changing team to ${teamId}`);
  router.replace({
    query: {
      ...route.query,
      team: String(teamId),
      source: undefined // Clear source when team changes
    }
  });
}

function handleSourceChange(sourceIdStr: string) {
  const sourceId = parseInt(sourceIdStr);
  if (isNaN(sourceId)) return;
  
  console.log(`LogExplorer: Changing source to ${sourceId}`);
  router.replace({
    query: {
      ...route.query,
      source: String(sourceId)
    }
  });
}

const {
  showSaveQueryModal,
  handleSaveQueryClick: openSaveModalFlow,
  handleSaveQuery: processSaveQueryFromComposable,
  loadSavedQuery,
  updateSavedQuery: _updateSavedQuery,
  loadSourceQueries,
} = useSavedQueries();

// Create default empty parsed query state
const EMPTY_PARSED_QUERY = {
  success: false,
  meta: { fieldsUsed: [], conditions: [] },
};

// Add parsed query structure to highlight columns used in search
const lastParsedQuery = ref<{
  success: boolean;
  meta?: {
    fieldsUsed: string[];
    conditions: QueryCondition[];
  };
}>(EMPTY_PARSED_QUERY);

// Basic state
// Sidebar defaults to open, but respects user's saved preference
const showFieldsPanel = ref(
  localStorage.getItem('logchef_fields_panel') !== 'closed'
);
const queryEditorRef = ref<ComponentPublicInstance<{
  focus: (revealLastPosition?: boolean) => void;
  code?: { value: string };
  toggleSqlEditorVisibility?: () => void;
}> | null>(null);
const isLoadingQuery = ref(false);
const editQueryData = ref<SavedTeamQuery | null>(null);
const topBarRef = ref<InstanceType<typeof ExploreTopBar> | null>(null);
const sortKeysInfoOpen = ref(false); // State for sort keys info expandable section
const isHistogramVisible = ref(true); // State for histogram visibility toggle

// Query execution deduplication
const executingQueryId = ref<string | null>(null);
const lastQueryTime = ref<number>(0);

// Display related refs
const displayTimezone = computed(() =>
  localStorage.getItem("logchef_timezone") === "utc" ? "utc" : "local"
);

// Display mode for table vs compact view (table is default)
const storedDisplayMode = localStorage.getItem("logchef_display_mode");
const displayMode = ref<'table' | 'compact'>(
  storedDisplayMode === 'compact' ? 'compact' : 'table'
);

// Watch display mode changes and persist to localStorage
watch(displayMode, (newMode) => {
  localStorage.setItem("logchef_display_mode", newMode);
}, { immediate: false });

// Persist fields panel preference
watch(showFieldsPanel, (isOpen) => {
  localStorage.setItem('logchef_fields_panel', isOpen ? 'open' : 'closed');
}, { immediate: false });

// UI state computed properties
const showLoadingState = computed(
  () => isInitializing.value && !initializationError.value
);

const showNoTeamsState = computed(
  () =>
    !isInitializing.value &&
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
  // Don't show while details for the *current* source are loading
  if (sourcesStore.isLoadingSourceDetailsForId(currentSourceId.value)) {
    return false;
  }
  // Show only if details *have* loaded AND the source is invalid/disconnected
  return (
    sourcesStore.currentSourceDetails?.id === currentSourceId.value &&
    !sourcesStore.hasValidCurrentSource
  );
});

const queryIdFromUrl = computed(
  () => route.query.id as string | undefined
);

// Can save or update query?
const canSaveOrUpdateQuery = computed(() => {
  return (
    !!currentTeamId.value &&
    !!currentSourceId.value &&
    hasValidSource.value &&
    (!!exploreStore.logchefqlCode?.trim() || !!exploreStore.rawSql?.trim())
  );
});

// Update the parsed query whenever a new query is executed
watch(
  () => exploreStore.lastExecutedState,
  async (newState) => {
    if (!newState) {
      lastParsedQuery.value = EMPTY_PARSED_QUERY;
      return;
    }

    if (activeMode.value === "logchefql") {
      // Check if query is empty
      if (!logchefQuery.value || logchefQuery.value.trim() === "") {
        lastParsedQuery.value = EMPTY_PARSED_QUERY;
      } else {
        // Parse the query using backend LogchefQL API
        const teamId = teamsStore.currentTeamId;
        const sourceId = currentSourceId.value;
        
        if (teamId && sourceId) {
          try {
            const response = await logchefqlApi.translate(teamId, sourceId, { query: logchefQuery.value });
            if (response.data && response.data.valid) {
              lastParsedQuery.value = {
                success: true,
                meta: {
                  fieldsUsed: response.data.fields_used || [],
                  conditions: response.data.conditions?.map((c: FilterCondition) => ({
                    field: c.field,
                    operator: c.operator,
                    value: c.value,
                    is_regex: c.is_regex
                  })) || []
                }
              };
            } else {
              lastParsedQuery.value = EMPTY_PARSED_QUERY;
            }
          } catch (error) {
            console.warn("Failed to parse query via backend:", error);
            lastParsedQuery.value = EMPTY_PARSED_QUERY;
          }
        } else {
          lastParsedQuery.value = EMPTY_PARSED_QUERY;
        }
      }
    } else {
      // Reset when in SQL mode
      lastParsedQuery.value = EMPTY_PARSED_QUERY;
    }
  },
  { immediate: true }
);

// Add computed property to get parsed query structure
const parsedQuery = computed(() => {
  return lastParsedQuery.value;
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

// Function to execute a query and handle URL history
// Modify the function to include a debouncingKey parameter to prevent duplicate executions
const handleQueryExecution = async (debouncingKey = "") => {
  try {
    // Get current timestamp for deduplication
    const now = Date.now();

    // Create a unique execution ID
    const executionId = `${debouncingKey || "query"}-${now}`;

    // Prevent execution if:
    // 1. A query is already executing, or
    // 2. The last query executed too recently (within 300ms) - UNLESS it's a source change
    const lastExecTime = exploreStore.lastExecutionTimestamp || 0;
    const timeSinceLastQuery = now - lastExecTime;
    const isSourceChange = debouncingKey.includes('source-change');
    const shouldDebounce = lastExecTime > 0 && timeSinceLastQuery < 300 && !isSourceChange;

    if (isExecutingQuery.value || shouldDebounce) {
      console.log(
        `LogExplorer: Skipping query execution - ${
          isExecutingQuery.value ? "already executing" : "too soon after previous query"
        }`
      );
      return;
    }

    // Log the current dirty state for debugging
    console.log(
      `LogExplorer: Executing query (${executionId}), current dirty state:`,
      isDirty.value ? "dirty" : "clean",
      "dirtyReason:",
      JSON.stringify(dirtyReason.value)
    );

    // Set executing state
    executingQueryId.value = executionId;
    lastQueryTime.value = now;

    // Execute the query using the executeQuery function from useQuery composable,
    // which now delegates to exploreStore
    console.log(`LogExplorer: Executing query with ID ${executionId}`);
    const result = await executeQuery();

    // Handle coordination errors with auto-retry
    if (result && !result.success && result.error && 'error_type' in result.error && result.error.error_type === 'CoordinationError') {
      console.log(`LogExplorer: Coordination error detected, scheduling retry in 100ms`);
      // Don't clear execution state yet, let the retry handle it
      setTimeout(() => {
        if (executingQueryId.value === executionId) {
          console.log(`LogExplorer: Retrying query after coordination error`);
          handleQueryExecution(`${debouncingKey}-retry`);
        }
      }, 100);
      return result;
    }

    if (result && result.success && !isInitializing.value) {
      urlState.pushHistoryEntry();

      // Update SQL and mark as not dirty AFTER successful execution
      if (activeMode.value === 'sql') {
        handleTimeRangeUpdate();
      }

      // Log the dirty state after execution
      console.log(
        `LogExplorer: Query executed successfully, new dirty state:`,
        isDirty.value ? "dirty" : "clean"
      );
    }

    // Clear execution state
    executingQueryId.value = null;
    return result;
  } catch (error) {
    console.error("Error during query execution:", error);
    executingQueryId.value = null;
    return {
      success: false,
      error: {
        message: error instanceof Error ? error.message : String(error),
      },
      data: null,
    };
  }
};

// Function to cancel the currently running query
const handleCancelQuery = async () => {
  console.log("LogExplorer: Cancel query requested");
  await exploreStore.cancelQuery();
  executingQueryId.value = null;
};

// Load saved queries when source changes
watch(
  () => currentSourceId.value,
  async (newSourceId, _oldSourceId) => {
    if (isInitializing.value) return;
    if (!newSourceId || !currentTeamId.value) return;
    try {
      await loadSourceQueries(currentTeamId.value, newSourceId);
    } catch (e) {
      console.error('Error loading saved queries for source:', e);
    }
  },
  { immediate: false }
)

// Keep store selection in sync with URL when team/source query params change
watch(
  () => [route.query.team, route.query.source],
  async ([teamParam, sourceParam]) => {
    if (isInitializing.value) return;
    const t = teamParam ? parseInt(teamParam as string) : null;
    const s = sourceParam ? parseInt(sourceParam as string) : null;
    if (t && t !== currentTeamId.value) {
      await handleTeamChange(t.toString());
      // If URL includes a specific source, switch to it after team change
      if (s) {
        await handleSourceChange(s.toString());
      }
    } else if (s && s !== currentSourceId.value) {
      await handleSourceChange(s.toString());
    }
  }
)

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
  exploreStore.setRawSql(newValue);
};

// New handler for the Save/Update button
const handleSaveOrUpdateClick = async () => {
  // Check if we have a query_id in the URL or in the exploreStore
  const queryId = queryIdFromUrl.value || exploreStore.selectedQueryId;

  // Check if we can save
  if (!canSaveOrUpdateQuery.value) {
    toast({
      title: "Cannot Save Query",
      variant: "destructive",
      description: "Missing required fields (Team, Source, Query).",
      duration: TOAST_DURATION.WARNING,
    });
    return;
  }

  if (queryId && currentTeamId.value && currentSourceId.value) {
    // --- Update Existing Query Flow ---
    try {
      isLoadingQuery.value = true;
      const result = await savedQueriesStore.fetchTeamSourceQueryDetails(
        currentTeamId.value,
        currentSourceId.value,
        queryId
      );

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
        title: "Error",
        description: "Failed to load query details for editing.",
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
async function handleUpdateQuery(queryId: string, formData: SaveQueryFormData) {
  // Ensure we have the necessary IDs
  if (!currentSourceId.value || !formData.team_id) {
    toast({
      title: "Error",
      description: "Missing source or team ID for update.",
      variant: "destructive",
      duration: TOAST_DURATION.ERROR,
    });
    return;
  }

  try {
    const response = await savedQueriesStore.updateTeamSourceQuery(
      formData.team_id,
      currentSourceId.value,
      queryId,
      {
        name: formData.name,
        description: formData.description,
        query_type: formData.query_type,
        query_content: formData.query_content,
      }
    );

    if (response && response.success) {
      showSaveQueryModal.value = false;
      editQueryData.value = null;
    } else if (response) {
      throw new Error(
        getErrorMessage(response.error) || "Failed to update query"
      );
    }
  } catch (error) {
    console.error("Error updating query:", error);
    toast({
      title: "Error",
      description: getErrorMessage(error),
      variant: "destructive",
      duration: TOAST_DURATION.ERROR,
    });
  }
}

const onHistogramTimeRangeZoom = (range: { start: Date; end: Date }) => {
  try {
    if (handleHistogramTimeRangeZoom(range)) {
      const zoomKey = `zoom-${Date.now()}`;
      setTimeout(() => {
        handleQueryExecution(zoomKey);
      }, 50);
    }
  } catch (e) {
    console.error("Error handling histogram time range:", e);
    toast({
      title: "Time Range Error",
      description:
        "There was an error updating the time range from chart selection.",
      variant: "destructive",
      duration: TOAST_DURATION.ERROR,
    });
  }
};

// Open the date picker programmatically
const openDatePicker = () => {
  if (topBarRef.value) {
    topBarRef.value.openDatePicker();
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

// Function to add sort key example to the query editor
const addSortKeyExample = () => {
  if (activeMode.value !== "logchefql") return;

  const exampleQuery = getSortKeyExampleQuery();
  if (!exampleQuery) return;

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
  () => exploreStore.isHistogramEligible,
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
        title: "Error",
        description: "Please select a source before using the AI Assistant",
        variant: "destructive",
        duration: TOAST_DURATION.ERROR,
      });
      return;
    }

    // Get the current query based on active mode
    let currentQuery = "";
    if (activeMode.value === "logchefql" && logchefQuery.value) {
      currentQuery = logchefQuery.value.trim();
    } else if (activeMode.value === "sql" && sqlQuery.value) {
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

// Handle adding a field filter from the sidebar
const handleAddFieldFilter = (field: string, value: string, operator: '=' | '!=') => {
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
  console.log("LogExplorer: handleRequestSaveAsNew triggered");
  editQueryData.value = null; // Ensure modal opens in "new query" mode
  openSaveModalFlow(); // Call the composable's function to open the modal
};

// Wrapper for the modal's @save event
const onSaveQueryModalSave = (formData: SaveQueryFormData) => {
  processSaveQueryFromComposable(formData);
};

// Handle saved query id changes from URL, especially when component is kept alive
watch(
  () => route.query.id,
  async (newQueryId, oldQueryId) => {
    // Skip if it's the same query ID or we're initializing
    if (newQueryId === oldQueryId || isInitializing.value) {
      return;
    }

    console.log(`LogExplorer: query id changed from ${oldQueryId} to ${newQueryId}`);

    // If query ID was removed, clear the saved query state
    if (!newQueryId && oldQueryId) {
      exploreStore.setSelectedQueryId(null);
      exploreStore.setActiveSavedQueryName(null);
      return;
    }

    // Wait for context alignment, then recompute URL params after wait
    let urlTeam = route.query.team ? parseInt(route.query.team as string) : null;
    let urlSource = route.query.source ? parseInt(route.query.source as string) : null;

    if (!urlTeam || !urlSource || urlTeam !== currentTeamId.value || urlSource !== currentSourceId.value) {
      for (let i = 0; i < 5; i++) {
        await new Promise(r => setTimeout(r, 100));
        if (
          route.query.team && route.query.source &&
          parseInt(route.query.team as string) === (currentTeamId.value ?? 0) &&
          parseInt(route.query.source as string) === (currentSourceId.value ?? 0)
        ) {
          break;
        }
      }
      // Recompute after polling to avoid stale values
      urlTeam = route.query.team ? parseInt(route.query.team as string) : null;
      urlSource = route.query.source ? parseInt(route.query.source as string) : null;
    }

    if (newQueryId && urlTeam && urlSource) {
      try {
        console.log(`LogExplorer: Loading saved query ${newQueryId}`);
        isLoadingQuery.value = true;

        const fetchResult = await savedQueriesStore.fetchTeamSourceQueryDetails(
          urlTeam,
          urlSource,
          newQueryId as string
        );

        if (fetchResult.success && savedQueriesStore.selectedQuery) {
          exploreStore.setGroupByField("__none__");

          const loadResult = await loadSavedQuery(savedQueriesStore.selectedQuery);

          if (loadResult) {
            await handleQueryExecution("query-from-url");

            nextTick(() => {
              queryEditorRef.value?.focus(true);
            });
          }
        } else {
          console.error("Failed to load query:", fetchResult.error);
          toast({
            title: "Error Loading Query",
            description: fetchResult.error?.message || "Failed to load the selected query",
            variant: "destructive",
            duration: TOAST_DURATION.ERROR,
          });
        }
      } catch (error) {
        console.error("Error loading query from URL:", error);
        toast({
          title: "Error",
          description: getErrorMessage(error),
          variant: "destructive",
          duration: TOAST_DURATION.ERROR,
        });
      } finally {
        isLoadingQuery.value = false;
      }
    }
  }
);

onMounted(async () => {
  try {
    await urlState.initialize();
  } catch (error) {
    console.error("Error during LogExplorer mount:", error);
    toast({
      title: "Explorer Error",
      description:
        "Error initializing the explorer. Please try refreshing the page.",
      variant: "destructive",
      duration: TOAST_DURATION.ERROR,
    });
  }
});

onBeforeUnmount(() => {
  if (import.meta.env.MODE !== "production") {
    console.log("LogExplorer unmounted");
  }
});
</script>

<template>
  <KeepAlive>
    <div class="log-explorer-wrapper">
      <!-- Loading State -->
      <div v-if="showLoadingState" class="flex items-center justify-center h-[calc(100vh-12rem)]">
        <p class="text-muted-foreground animate-pulse">Loading Explorer...</p>
      </div>

      <!-- No Teams State -->
      <div v-else-if="showNoTeamsState"
        class="flex flex-col items-center justify-center h-[calc(100vh-12rem)] gap-4 text-center">
        <h2 class="text-2xl font-semibold">No Teams Available</h2>
        <p class="text-muted-foreground max-w-md">
          You need to be part of a team to explore logs. Contact your
          administrator.
        </p>
        <Button variant="outline" @click="router.push({ name: 'LogExplorer' })">Go to Dashboard</Button>
      </div>

      <!-- No Sources State (Team Selected) -->
      <div v-else-if="showNoSourcesState" class="flex flex-col h-[calc(100vh-12rem)]">
        <!-- Header bar for team selection -->
        <div class="border-b py-2 px-4 flex items-center h-12">
          <TeamSourceSelector 
  :team-sources="teamSources"
  :is-loading-team-sources="isLoadingTeamSources"
  :is-processing-team-change="isProcessingTeamChange"
  :is-processing-source-change="isProcessingSourceChange"
/>
        </div>
        <!-- Empty state content -->
        <div class="flex flex-col items-center justify-center flex-1 gap-4 text-center">
          <h2 class="text-2xl font-semibold">No Log Sources Found</h2>
          <p class="text-muted-foreground max-w-md">
            The selected team '{{ selectedTeamName }}' has no sources
            configured. Add one or switch teams.
          </p>
        </div>
      </div>

      <!-- Source Not Connected State -->
      <div v-else-if="showSourceNotConnectedState" class="flex flex-col h-screen overflow-hidden">
        <!-- Filter Bar with Team/Source Selection -->
        <div class="border-b bg-background py-2 px-4 flex items-center h-12 shadow-sm">
          <TeamSourceSelector 
  :team-sources="teamSources"
  :is-loading-team-sources="isLoadingTeamSources"
  :is-processing-team-change="isProcessingTeamChange"
  :is-processing-source-change="isProcessingSourceChange"
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
            <h2 class="text-xl font-semibold mb-2">Source Not Connected</h2>
            <p class="text-muted-foreground mb-4">
              The selected source "{{ selectedSourceName }}" is not properly
              connected to the database. Please check the source configuration
              or select a different source.
            </p>

            <div class="flex items-center justify-center gap-3">
              <Button variant="outline" @click="
                router.push({
                  name: 'SourceSettings',
                  params: { sourceId: currentSourceId },
                })
                ">
                Configure Source
              </Button>
              <Button variant="default" @click="router.push({ name: 'NewSource' })">
                Add New Source
              </Button>
            </div>
          </div>
        </div>
      </div>

      <!-- Main Explorer View -->
      <div v-else class="flex flex-col h-screen overflow-hidden">
        <!-- URL Error -->
        <div v-if="initializationError"
          class="absolute top-0 left-0 right-0 bg-destructive/15 text-destructive px-4 py-2 z-10 flex items-center justify-between">
          <span class="text-sm">{{ initializationError }}</span>
          <Button variant="ghost" size="sm" @click="initializationError = null" class="h-7 px-2">Dismiss</Button>
        </div>

        <!-- Streamlined Top Bar -->
        <ExploreTopBar ref="topBarRef" />

        <!-- Main Content Area -->
        <div class="flex flex-1 min-h-0">
          <FieldSideBar 
            v-model:expanded="showFieldsPanel" 
            :fields="availableFields"
            :team-id="currentTeamId ?? undefined"
            :source-id="currentSourceId ?? undefined"
            @add-filter="handleAddFieldFilter"
            @field-click="handleFieldClick"
          />

          <div class="flex-1 flex flex-col h-full min-w-0 overflow-hidden">
            <!-- Query Editor Section -->
            <div class="px-4 py-3">
              <!-- Loading indicator during context changes -->
              <template v-if="
                isChangingContext ||
                (currentSourceId && sourcesStore.isLoadingSourceDetails)
              ">
                <div
                  class="flex items-center justify-center text-muted-foreground p-6 border rounded-md bg-card shadow-sm animate-pulse">
                  <div class="flex items-center space-x-2">
                    <svg class="animate-spin h-5 w-5 text-primary" xmlns="http://www.w3.org/2000/svg" fill="none"
                      viewBox="0 0 24 24">
                      <circle class="opacity-25" cx="12" cy="12" r="10" stroke="currentColor" stroke-width="4"></circle>
                      <path class="opacity-75" fill="currentColor"
                        d="M4 12a8 8 0 018-8V0C5.373 0 0 5.373 0 12h4zm2 5.291A7.962 7.962 0 014 12H0c0 3.042 1.135 5.824 3 7.938l3-2.647z">
                      </path>
                    </svg>
                    <span>{{
                      isChangingContext
                        ? "Loading context data..."
                        : "Loading source details..."
                    }}</span>
                  </div>
                </div>
              </template>

              <!-- Query Editor -->
              <template v-else-if="
                currentSourceId && hasValidSource && exploreStore.timeRange
              ">
                <div class="bg-card shadow-sm rounded-md overflow-hidden">
                  <QueryEditor 
                    ref="queryEditorRef" 
                    :sourceId="currentSourceId" 
                    :teamId="currentTeamId ?? 0" 
                    :schema="(sourceDetails?.columns || []).reduce((acc: Record<string, { type: string }>, col) => {
                      if (col.name && col.type) {
                        acc[col.name] = { type: col.type };
                      }
                      return acc;
                    }, {})"
                    :activeMode="exploreStore.activeMode === 'logchefql' ? 'logchefql' : 'clickhouse-sql'"
                    :value="exploreStore.activeMode === 'logchefql' ? logchefQuery : sqlQuery"
                    :placeholder="exploreStore.activeMode === 'logchefql'
                      ? 'Enter search criteria (e.g., lvl=&quot;ERROR&quot; and namespace~&quot;sys&quot;)'
                      : 'Enter SQL query...'"
                    :tsField="sourceDetails?._meta_ts_field || 'timestamp'"
                    :tableName="sourcesStore.getCurrentSourceTableName || ''" 
                    :showFieldsPanel="showFieldsPanel"
                    :isExecuting="isExecutingQuery"
                    :canExecute="canExecuteQuery"
                    :showRunButton="true"
                    :isCancelling="exploreStore.isCancellingQuery"
                    @change="(event) => event.mode === 'logchefql'
                      ? updateLogchefqlValue(event.query, event.isUserInput)
                      : updateSqlValue(event.query, event.isUserInput)"
                    @submit="() => handleQueryExecution('editor-submit')" 
                    @execute="() => handleQueryExecution('editor-run-button')"
                    @cancel-query="handleCancelQuery"
                    @update:activeMode="(mode, isModeSwitchOnly) =>
                      changeMode(mode === 'logchefql' ? 'logchefql' : 'sql', isModeSwitchOnly)"
                    @toggle-fields="showFieldsPanel = !showFieldsPanel" 
                    @select-saved-query="loadSavedQuery"
                    @save-query="handleSaveOrUpdateClick" 
                    @save-query-as-new="handleRequestSaveAsNew"
                    @generate-ai-sql="handleGenerateAISQL" 
                    class="border-0" />

                  <!-- Sort Key Optimization Hint - Inline Version -->
                  <div v-if="
                    sourceDetails?.sort_keys &&
                    (sourceDetails.sort_keys.length > 1 ||
                      (sourceDetails.sort_keys.length === 1 &&
                        sourceDetails.sort_keys[0] !== sourceDetails?._meta_ts_field))
                  " class="flex items-center gap-2 px-3 py-1.5 text-xs bg-blue-50/50 dark:bg-blue-900/20 border-t">
                    <svg class="h-3 w-3 text-blue-600 dark:text-blue-400 flex-shrink-0" xmlns="http://www.w3.org/2000/svg"
                      viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round">
                      <circle cx="12" cy="12" r="10"></circle>
                      <line x1="12" y1="16" x2="12" y2="12"></line>
                      <line x1="12" y1="8" x2="12.01" y2="8"></line>
                    </svg>
                    <span class="text-blue-700 dark:text-blue-300">
                      <span class="font-medium">Tip:</span> Filter by
                      <span v-for="(key, idx) in filteredSortKeys" :key="key">
                        <code class="px-1 bg-blue-100 dark:bg-blue-900/40 rounded text-blue-800 dark:text-blue-200">{{ key }}</code>
                        <span v-if="idx < filteredSortKeys.length - 1">, </span>
                      </span>
                      for faster queries
                    </span>
                    <button 
                      v-if="activeMode === 'logchefql'" 
                      @click="addSortKeyExample"
                      class="ml-auto px-2 py-0.5 text-xs bg-blue-600/10 hover:bg-blue-600/20 rounded transition-colors text-blue-700 dark:text-blue-300"
                    >
                      Add Example
                    </button>
                  </div>
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
                    <h3 class="text-lg font-medium mb-2">Select a Log Source</h3>
                    <p class="text-sm text-muted-foreground mb-4">
                      Choose a log source from the dropdown above to start exploring your data.
                    </p>
                    <div class="text-xs text-muted-foreground/70">
                      Need to add a new source? Click "Add Source" in the selector.
                    </div>
                  </div>
                </div>
              </template>

              <!-- Loading fallback - for any other state -->
              <template v-else>
                <div class="flex items-center justify-center p-6 border rounded-md bg-card shadow-sm">
                  <div class="text-center">
                    <p class="text-sm text-muted-foreground">
                      Loading explorer...
                    </p>
                  </div>
                </div>
              </template>

              <!-- Query Error Component -->
              <QueryError :query-error="queryError" />
            </div>

            <!-- Results Toolbar + Histogram Section -->
            <div v-if="
              !isChangingContext &&
              currentSourceId &&
              hasValidSource &&
              exploreStore.timeRange
            ">
              <!-- Unified Results Toolbar -->
              <ResultsToolbar
                :isHistogramVisible="isHistogramVisible"
                :availableFields="availableFields"
                :displayMode="displayMode"
                :logsCount="exploreStore.logs?.length || 0"
                :isLoading="isExecutingQuery"
                @toggle-histogram="toggleHistogramVisibility"
                @update:displayMode="displayMode = $event"
              />

              <!-- Histogram visualization -->
              <div v-if="isHistogramVisible" class="px-4 py-2 border-b">
                <HistogramVisualization 
                  :group-by="exploreStore.groupByField" 
                  @zoom-time-range="onHistogramTimeRangeZoom"
                  @update:timeRange="onHistogramTimeRangeZoom" 
                />
              </div>
            </div>

            <!-- Results Section -->
            <div class="flex-1 overflow-hidden flex flex-col" v-if="
              !isChangingContext &&
              currentSourceId &&
              hasValidSource &&
              exploreStore.timeRange
            ">
              <!-- Results Area -->
              <div class="flex-1 overflow-hidden relative bg-background">
                <!-- Results Table -->
                <template v-if="exploreStore.logs?.length > 0 || isExecutingQuery">
                  <!-- Render DataTable or CompactLogList based on display mode -->
                  <component
                    v-if="exploreStore.columns?.length > 0"
                    :is="displayMode === 'table' ? DataTable : CompactLogList"
                    :key="`${exploreStore.sourceId}-${exploreStore.activeMode}-${exploreStore.queryId}-${displayMode}`"
                    :columns="exploreStore.columns as any"
                    :data="exploreStore.logs"
                    :stats="exploreStore.queryStats"
                    :is-loading="isExecutingQuery"
                    :source-id="String(exploreStore.sourceId)"
                    :team-id="teamsStore.currentTeamId"
                    :timestamp-field="sourcesStore.currentSourceDetails?._meta_ts_field"
                    :severity-field="sourcesStore.currentSourceDetails?._meta_severity_field"
                    :timezone="displayTimezone"
                    :query-fields="queryFields"
                    :regex-highlights="regexHighlights"
                    :active-mode="activeMode"
                    :display-mode="displayMode"
                    @drill-down="handleDrillDown"
                    @update:display-mode="displayMode = $event"
                  />

                  <!-- Loading placeholder -->
                  <div v-else-if="isExecutingQuery"
                    class="absolute inset-0 flex items-center justify-center bg-background/70 z-10">
                    <p class="text-muted-foreground animate-pulse">
                      Loading results...
                    </p>
                  </div>
                </template>

                <!-- Empty Results State Component -->
                <EmptyResultsState v-else :has-executed-query="!!exploreStore.lastExecutedState &&
                  !exploreStore.logs?.length
                  " :can-execute-query="canExecuteQuery" @run-default-query="handleQueryExecution('default-query')"
                  @open-date-picker="openDatePicker" />
              </div>
            </div>
          </div>
        </div>

        <!-- Save Query Modal -->
        <SaveQueryModal v-if="showSaveQueryModal" :is-open="showSaveQueryModal" :query-type="exploreStore.activeMode"
          :edit-data="editQueryData" :query-content="JSON.stringify({
            sourceId: currentSourceId,
            limit: exploreStore.limit,
            content:
              exploreStore.activeMode === 'logchefql'
                ? exploreStore.logchefqlCode
                : exploreStore.rawSql,
          })
            " @close="showSaveQueryModal = false" @save="onSaveQueryModalSave" @update="handleUpdateQuery" />


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
