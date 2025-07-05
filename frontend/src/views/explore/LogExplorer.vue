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
import { Skeleton } from "@/components/ui/skeleton";
import { Button } from "@/components/ui/button";
import { useToast } from "@/components/ui/toast";
import { Share2, WandSparkles } from "lucide-vue-next";
import { TOAST_DURATION } from "@/lib/constants";
import { useExploreStore } from "@/stores/explore";
import { useTeamsStore } from "@/stores/teams";
import { useSourcesStore } from "@/stores/sources";
import { useSavedQueriesStore } from "@/stores/savedQueries";
import { useLiveLogStore } from "@/stores/liveLog"
import { FieldSideBar } from "@/components/field-sidebar";
import { getErrorMessage } from "@/api/types";
import DataTable from "./table/data-table.vue";
import SaveQueryModal from "@/components/collections/SaveQueryModal.vue";
import QueryEditor from "@/components/query-editor/QueryEditor.vue";
import { useSourceTeamManagement } from "@/composables/useSourceTeamManagement";
import { useSavedQueries } from "@/composables/useSavedQueries";
import { useExploreUrlSync } from "@/composables/useExploreUrlSync";
import { useQuery } from "@/composables/useQuery";
import { useTimeRange } from "@/composables/useTimeRange";
import type { ComponentPublicInstance } from "vue";
import type { SaveQueryFormData } from "@/views/explore/types";
import type { SavedTeamQuery } from "@/api/savedQueries";
import {
  type QueryCondition,
  parseAndTranslateLogchefQL,
} from "@/utils/logchefql/api";
import { QueryService } from "@/services/QueryService";
import { type DateValue, CalendarDate, getLocalTimeZone } from '@internationalized/date';

// Import refactored components
import TeamSourceSelector from "./components/TeamSourceSelector.vue";
import TimeRangeSelector from "./components/TimeRangeSelector.vue";
import QueryControls from "./components/QueryControls.vue";
import GroupBySelector from "./components/GroupBySelector.vue";
import QueryError from "./components/QueryError.vue";
import HistogramVisualization from "./components/HistogramVisualization.vue";
import EmptyResultsState from "./components/EmptyResultsState.vue";
// import AIQueryModal from "@/components/ai/AIQueryModal.vue"; // No longer needed

// Router and stores
const router = useRouter();
const route = useRoute();
const exploreStore = useExploreStore();
const teamsStore = useTeamsStore();
const sourcesStore = useSourcesStore();
const savedQueriesStore = useSavedQueriesStore();
const { toast } = useToast();

// Composables
const {
  isInitializing,
  initializationError,
  initializeFromUrl,
  syncUrlFromState,
  pushQueryHistoryEntry,
} = useExploreUrlSync();

const {
  logchefQuery,
  sqlQuery,
  activeMode,
  queryError,
  sqlWarnings,
  isDirty,
  dirtyReason,
  isExecutingQuery,
  canExecuteQuery,
  changeMode,
  executeQuery,
  handleTimeRangeUpdate,
  handleLimitUpdate,
} = useQuery();

const { handleHistogramTimeRangeZoom } = useTimeRange();

const {
  isProcessingTeamChange,
  isProcessingSourceChange,
  isChangingContext,
  isLoadingSourceDetails,
  currentTeamId,
  currentSourceId,
  sourceDetails,
  hasValidSource,
  availableTeams,
  availableSources,
  selectedTeamName,
  selectedSourceName,
  availableFields,
  handleTeamChange,
  handleSourceChange,
} = useSourceTeamManagement();

const {
  showSaveQueryModal,
  handleSaveQueryClick: openSaveModalFlow,
  handleSaveQuery: processSaveQueryFromComposable,
  loadSavedQuery,
  updateSavedQuery,
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
const showFieldsPanel = ref(false);
const queryEditorRef = ref<ComponentPublicInstance<{
  focus: (revealLastPosition?: boolean) => void;
  code?: { value: string };
  toggleSqlEditorVisibility?: () => void;
}> | null>(null);
const isLoadingQuery = ref(false);
const editQueryData = ref<SavedTeamQuery | null>(null);
const initialQueryExecuted = ref(false);
const timeRangeSelectorRef = ref<InstanceType<typeof TimeRangeSelector> | null>(
  null
);
const sortKeysInfoOpen = ref(false); // State for sort keys info expandable section

// Query execution deduplication
const executingQueryId = ref<string | null>(null);
const lastQueryTime = ref<number>(0);
let lastExecutionKey = "";

// live log paused or not
const isPaused = ref(false);

// Display related refs
const displayTimezone = computed(() =>
  localStorage.getItem("logchef_timezone") === "utc" ? "utc" : "local"
);

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
    currentTeamId.value !== null &&
    currentTeamId.value > 0 &&
    (!availableSources.value || availableSources.value.length === 0)
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
  if (sourcesStore.isLoadingSourceDetails(currentSourceId.value)) {
    return false;
  }
  // Show only if details *have* loaded AND the source is invalid/disconnected
  return (
    sourcesStore.currentSourceDetails?.id === currentSourceId.value &&
    !sourcesStore.hasValidCurrentSource
  );
});

// Computed property to show the "Source Connected" state
const showSourceConnectedState = computed(() => {
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
  if (sourcesStore.isLoadingSourceDetails(currentSourceId.value)) {
    return false;
  }
  // Check if the source is connected
  return sourceDetails.value?.is_connected;
});

// Track when URL query params change
const lastQueryParam = ref(route.query.q);
const lastModeParam = ref(route.query.mode);

// Check if we're in edit mode (URL has query_id)
const isEditingExistingQuery = computed(
  () => !!route.query.query_id || !!exploreStore.selectedQueryId
);
const queryIdFromUrl = computed(
  () => route.query.query_id as string | undefined
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
  (newState) => {
    if (!newState) {
      lastParsedQuery.value = EMPTY_PARSED_QUERY;
      return;
    }

    if (activeMode.value === "logchefql") {
      // Check if query is empty
      if (!logchefQuery.value || logchefQuery.value.trim() === "") {
        lastParsedQuery.value = EMPTY_PARSED_QUERY;
      } else {
        // Parse the query using LogchefQL parser
        const result = parseAndTranslateLogchefQL(logchefQuery.value);
        lastParsedQuery.value = result;
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
    (cond: QueryCondition) => cond.isRegex
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
    lastExecutionKey = debouncingKey;
    lastQueryTime.value = now;

    // Execute the query using the executeQuery function from useQuery composable,
    // which now delegates to exploreStore
    console.log(`LogExplorer: Executing query with ID ${executionId}`);
    const result = await executeQuery();

    // Only push a history entry if the query executed successfully
    // But don't do it during initialization to avoid duplicate history entries
    if (result && result.success && !isInitializing.value) {
      // This will create a new browser history entry with router.push
      pushQueryHistoryEntry();

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

// Function to reset/initialize queries when switching sources
function resetQueriesForSourceChange() {
  console.log("LogExplorer: Resetting queries for source change");

  // Reset group-by selection to "No Grouping"
  exploreStore.setGroupByField("__none__");

  // Use the new method that preserves time range and limit
  exploreStore.resetQueryContentForSourceChange();
}

// Watch for source changes to fetch details AND saved queries
watch(
  () => currentSourceId.value,
  async (newSourceId, oldSourceId) => {
    // Skip during initialization to prevent redundant calls
    if (isInitializing.value) {
      return;
    }

    // Reset sort keys info panel state on source change
    sortKeysInfoOpen.value = false;

    if (newSourceId !== oldSourceId || (!oldSourceId && newSourceId)) {
      // Reset queries when source changes
      resetQueriesForSourceChange();

      // Fetch Source Details
      if (newSourceId && newSourceId > 0) {
        // Verify source existence
        const sourceExists = availableSources.value.some(
          (source) => source.id === newSourceId
        );
        if (sourceExists) {
          // Fetch details (debounced)
          setTimeout(async () => {
            if (currentSourceId.value === newSourceId) {
              // Check if still the same after timeout
              await sourcesStore.loadSourceDetails(newSourceId);
              
              // Execute query after source details are loaded if we have valid data
              if (
                sourcesStore.currentSourceDetails?.id === newSourceId &&
                sourcesStore.hasValidCurrentSource &&
                exploreStore.timeRange
              ) {
                console.log("LogExplorer: Executing query after source change and details loaded");
                // Execute query immediately after source details are confirmed loaded
                handleQueryExecution('source-change-query');
              }
            }
          }, 50);

          // Fetch Saved Queries
          if (currentTeamId.value) {
            // Ensure team ID is available
            await loadSourceQueries(currentTeamId.value, newSourceId);
          }
        }
      } else {
        // Clear saved queries if source is deselected
        if (currentTeamId.value) {
          await loadSourceQueries(currentTeamId.value, 0);
        }
      }
    }
  },
  { immediate: false } // Don't run immediately, wait for initialization
);

// Watch for changes in currentTeamId to update sources AND saved queries
watch(
  () => currentTeamId.value,
  async (newTeamId, oldTeamId) => {
    // Skip during initialization
    if (isInitializing.value) {
      return;
    }

    if (newTeamId !== oldTeamId && newTeamId) {
      // Reset queries when team changes
      resetQueriesForSourceChange();

      // Load sources for the new team
      const sourcesResult = await sourcesStore.loadTeamSources(newTeamId);
      let newSourceIdToLoadQueries: number | null = null;

      if (
        !sourcesResult.success ||
        !sourcesResult.data ||
        sourcesResult.data.length === 0
      ) {
        exploreStore.setSource(0);
        newSourceIdToLoadQueries = 0; // Signal to load empty queries
      } else {
        const currentSourceExists = sourcesStore.teamSources.some(
          (source) => source.id === exploreStore.sourceId
        );
        if (!currentSourceExists && sourcesStore.teamSources.length > 0) {
          const firstSourceId = sourcesStore.teamSources[0].id;
          exploreStore.setSource(firstSourceId);
          await sourcesStore.loadSourceDetails(firstSourceId);
          newSourceIdToLoadQueries = firstSourceId; // Load queries for the new source
        } else {
          // If current source is still valid, load its queries
          newSourceIdToLoadQueries = exploreStore.sourceId;
        }
      }

      // Load Saved Queries for the new team/source combination
      if (newSourceIdToLoadQueries !== null) {
        await loadSourceQueries(newTeamId, newSourceIdToLoadQueries);
      }
    }
  },
  { immediate: false } // Don't run immediately
);

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
const updateLogchefqlValue = (newValue: string, isUserInput = false) => {
  // Use the store's action to update LogchefQL code
  exploreStore.setLogchefqlCode(newValue);
};

const updateSqlValue = (newValue: string, isUserInput = false) => {
  // Use the store's action to update SQL
  exploreStore.setRawSql(newValue);
};

// Function to clear the query editor content
const clearQueryEditor = () => {
  // Use the store's actions to clear content
  if (exploreStore.activeMode === "logchefql") {
    exploreStore.setLogchefqlCode("");
  } else {
    exploreStore.setRawSql("");
  }

  // Clear any validation errors
  queryError.value = "";

  // Focus the editor using the ref after clearing
  nextTick(() => {
    queryEditorRef.value?.focus(true);
  });
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

      toast({
        title: "Success",
        description: "Query updated successfully.",
        duration: TOAST_DURATION.SUCCESS,
        variant: "success",
      });
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

// Handle histogram events
const onHistogramTimeRangeZoom = (range: { start: Date; end: Date }) => {
  try {
    // Use the timeRange composable function for handling
    if (handleHistogramTimeRangeZoom(range)) {
      // Generate a unique key for this zoom operation to prevent duplicates
      const zoomKey = `zoom-${Date.now()}`;

      // Execute query with the new time range
      setTimeout(() => {
        handleQueryExecution(zoomKey);
      }, 50);

      // Update URL state
      syncUrlFromState();
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
  if (timeRangeSelectorRef.value) {
    timeRangeSelectorRef.value.openDatePicker();
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

    // Show toast notification
    toast({
      title: "Sort Key Filter Applied",
      description:
        "An example filter using sort keys has been added to your query. Please customize the values as needed.",
      duration: TOAST_DURATION.INFO,
      variant: "default",
    });
  });
};

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

    const result = await exploreStore.generateAiSql(naturalLanguageQuery, currentQuery);

    if (result.success && 'data' in result && result.data) {
      // Switch to SQL mode and update the content
      changeMode('sql');
      exploreStore.setRawSql(result.data.sql_query);

      // Focus the editor
      nextTick(() => {
        queryEditorRef.value?.focus(true);
      });
    } else {
      // Show error in toast
      const errorMessage = result.error && 'message' in result.error
        ? result.error.message
        : 'Failed to generate SQL';

      toast({
        title: "AI SQL Generation Failed",
        description: errorMessage,
        variant: "destructive",
        duration: TOAST_DURATION.ERROR,
      });
    }
  } catch (error) {
    console.error("Error generating AI SQL:", error);
    toast({
      title: "AI SQL Generation Error",
      description: getErrorMessage(error),
      variant: "destructive",
      duration: TOAST_DURATION.ERROR,
    });
  }
};

// Function to copy current URL to clipboard
const copyUrlToClipboard = () => {
  try {
    navigator.clipboard.writeText(window.location.href);
    toast({
      title: "URL Copied",
      description: "The shareable link has been copied to your clipboard.",
      duration: TOAST_DURATION.INFO,
      variant: "success",
    });
  } catch (error) {
    console.error("Failed to copy URL: ", error);
    toast({
      title: "Copy Failed",
      description: "Failed to copy URL to clipboard.",
      variant: "destructive",
      duration: TOAST_DURATION.ERROR,
    });
  }
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

// handle live log's pause on / off
const liveLogPauseOnOff = (isPause: boolean) => {
  isPaused.value = isPause;
  const liveLogStore = useLiveLogStore();
  liveLogStore.setIsPause(isPause);
};

// Handle query_id changes from URL, especially when component is kept alive
watch(
  () => route.query.query_id,
  async (newQueryId, oldQueryId) => {
    // Skip if it's the same query ID or we're initializing
    if (newQueryId === oldQueryId || isInitializing.value) {
      return;
    }

    console.log(`LogExplorer: query_id changed from ${oldQueryId} to ${newQueryId}`);

    // If we have a query ID, team ID and source ID, load the query
    if (newQueryId && currentTeamId.value && currentSourceId.value) {
      try {
        console.log(`LogExplorer: Loading saved query ${newQueryId}`);
        isLoadingQuery.value = true;

        // Fetch query details
        const fetchResult = await savedQueriesStore.fetchTeamSourceQueryDetails(
          currentTeamId.value,
          currentSourceId.value,
          newQueryId as string
        );

        if (fetchResult.success && savedQueriesStore.selectedQuery) {
          // Always use no grouping by default when switching queries
          exploreStore.setGroupByField("__none__");

          // Load the saved query
          const loadResult = await loadSavedQuery(savedQueriesStore.selectedQuery);

          if (loadResult) {
            // Execute the query after loading
            await handleQueryExecution("query-from-url");

            // Focus editor after query is loaded
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

// Component lifecycle
onMounted(async () => {
  try {
    // Reset admin teams and load user teams
    teamsStore.resetAdminTeams();

    // Initialize state from URL
    await initializeFromUrl();

    // Force a reload of user teams to ensure latest membership data
    await teamsStore.loadUserTeams();

    // Skip validation if there's an initialization error - it's already handled
    if (!initializationError.value) {
      // After loading teams, verify the current teamId is still valid
      if (
        currentTeamId.value &&
        !teamsStore.userBelongsToTeam(currentTeamId.value)
      ) {
        console.log(
          `Current team ${currentTeamId.value} is no longer accessible, resetting selection`
        );

        // Select the first available team instead
        if (teamsStore.userTeams.length > 0) {
          exploreStore.setSource(0); // First clear the source
          teamsStore.setCurrentTeam(teamsStore.userTeams[0].id);
        } else {
          exploreStore.setSource(0);
          teamsStore.setCurrentTeam(0); // Clear the current team
        }
      }

      // Execute initial query if we have required parameters and no query has been executed yet
      // Wait a bit to ensure the source details are loaded
      setTimeout(async () => {
        // Only execute if we have enough parameters and no query has been executed yet
        if (
          !exploreStore.lastExecutionTimestamp &&  // No query executed yet
          exploreStore.sourceId &&
          exploreStore.timeRange &&
          sourcesStore.currentSourceDetails?.id === exploreStore.sourceId &&
          sourcesStore.hasValidCurrentSource // Ensure source is actually valid/connected
        ) {
          console.log("LogExplorer: Executing initial query on mount");
          await handleQueryExecution('initial-mount-query');
        } else {
          console.log("LogExplorer: Skipping initial query execution, missing requirements or query already executed", {
            hasLastExecution: !!exploreStore.lastExecutionTimestamp,
            sourceId: exploreStore.sourceId,
            hasTimeRange: !!exploreStore.timeRange,
            sourceDetailsLoaded: sourcesStore.currentSourceDetails?.id === exploreStore.sourceId,
            hasValidSource: sourcesStore.hasValidCurrentSource
          });
        }
      }, 500); // Increased timeout to allow more time for source details to load
    }
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
          <TeamSourceSelector />
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
          <TeamSourceSelector />
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

        <!-- Top Action Bar -->
        <div class="border-b bg-background py-2 px-4 flex items-center justify-between h-12 shadow-sm">
          <!-- Left section: Team/Source and Time Range -->
          <div class="flex items-center">
            <!-- Team/Source Selector Component -->
            <TeamSourceSelector />

            <!-- Divider -->
            <div class="h-6 w-px bg-border mx-3"></div>

            <!-- Time Range Selector Component -->
            <TimeRangeSelector ref="timeRangeSelectorRef" />
          </div>

          <!-- Right section: Share button and execution time -->
          <div class="flex items-center gap-3">
            <!-- Last run time indicator -->
            <div class="text-xs text-muted-foreground flex items-center" v-if="exploreStore.lastExecutionTimestamp">
              <span>Last run:
                {{
                  new Date(
                    exploreStore.lastExecutionTimestamp
                  ).toLocaleTimeString()
                }}</span>
            </div>

            <!-- Share Button -->
            <Button variant="outline" size="sm" class="h-8" @click="copyUrlToClipboard"
              v-if="!isChangingContext && currentSourceId && hasValidSource">
              <Share2 class="h-4 w-4 mr-1.5" />
              Share
            </Button>
          </div>
        </div>

        <!-- Main Content Area -->
        <div class="flex flex-1 min-h-0">
          <FieldSideBar v-model:expanded="showFieldsPanel" :fields="availableFields" />

          <div class="flex-1 flex flex-col h-full min-w-0 overflow-hidden">
            <!-- Query Editor Section -->
            <div class="px-4 py-3">
              <!-- Loading indicator during context changes -->
              <template v-if="
                isChangingContext ||
                (currentSourceId && isLoadingSourceDetails)
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
                  <QueryEditor ref="queryEditorRef" :sourceId="currentSourceId" :teamId="currentTeamId ?? 0" :schema="(sourceDetails?.columns || []).reduce((acc: Record<string, { type: string }>, col) => {
                    if (col.name && col.type) {
                      acc[col.name] = { type: col.type };
                    }
                    return acc;
                  }, {})
                    " :activeMode="exploreStore.activeMode === 'logchefql'
                      ? 'logchefql'
                      : 'clickhouse-sql'
                      " :value="exploreStore.activeMode === 'logchefql'
                        ? logchefQuery
                        : sqlQuery
                        " @change="
                          (event) =>
                            event.mode === 'logchefql'
                              ? updateLogchefqlValue(event.query, event.isUserInput)
                              : updateSqlValue(event.query, event.isUserInput)
                        " :placeholder="exploreStore.activeMode === 'logchefql'
                          ? 'Enter search criteria (e.g., lvl=&quot;ERROR&quot; and namespace~&quot;sys&quot;)'
                          : 'Enter SQL query...'
                          " :tsField="sourceDetails?._meta_ts_field || 'timestamp'"
                    :tableName="sourcesStore.getCurrentSourceTableName || ''" :showFieldsPanel="showFieldsPanel"
                    @submit="() => handleQueryExecution('editor-submit')" @update:activeMode="
                      (mode, isModeSwitchOnly) =>
                        changeMode(
                          mode === 'logchefql' ? 'logchefql' : 'sql',
                          isModeSwitchOnly
                        )
                    " @toggle-fields="showFieldsPanel = !showFieldsPanel" @select-saved-query="loadSavedQuery"
                    @save-query="handleSaveOrUpdateClick" @save-query-as-new="handleRequestSaveAsNew"
                    @generate-ai-sql="handleGenerateAISQL" class="border-0 border-b" />

                  <!-- Sort Key Optimization Hint - Concise Version -->
                  <div v-if="
                    sourceDetails?.sort_keys &&
                    (sourceDetails.sort_keys.length > 1 ||
                      (sourceDetails.sort_keys.length === 1 &&
                        sourceDetails.sort_keys[0] !==
                        sourceDetails?._meta_ts_field))
                  " class="border-t bg-blue-50 dark:bg-blue-900/30 px-3 py-1.5 text-xs">
                    <div class="flex items-center justify-between">
                      <button
                        class="group flex flex-wrap items-center gap-x-1.5 text-blue-700 dark:text-blue-300 focus:outline-none py-0.5"
                        @click="sortKeysInfoOpen = !sortKeysInfoOpen">
                        <svg class="h-3.5 w-3.5 flex-shrink-0 mt-0.5" xmlns="http://www.w3.org/2000/svg"
                          viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round"
                          stroke-linejoin="round">
                          <circle cx="12" cy="12" r="10"></circle>
                          <line x1="12" y1="16" x2="12" y2="12"></line>
                          <line x1="12" y1="8" x2="12.01" y2="8"></line>
                        </svg>
                        <span class="font-medium">ClickHouse Performance Tip:</span>
                        <span>Filter by</span>
                        <div class="inline-flex gap-1.5 flex-wrap items-center">
                          <span v-for="key in filteredSortKeys" :key="key"
                            class="inline-block px-1.5 bg-blue-100 dark:bg-blue-900/30 rounded text-blue-800 dark:text-blue-200 font-mono leading-relaxed">
                            {{ key }}
                          </span>
                        </div>
                        <svg
                          class="h-3.5 w-3.5 transition-transform duration-200 ml-1 mt-0.5 group-hover:text-blue-800 dark:group-hover:text-blue-200"
                          :class="{ 'rotate-180': sortKeysInfoOpen }" xmlns="http://www.w3.org/2000/svg"
                          viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round"
                          stroke-linejoin="round">
                          <polyline points="6 9 12 15 18 9"></polyline>
                        </svg>
                      </button>
                      <button v-if="activeMode === 'logchefql'" @click="addSortKeyExample"
                        class="ml-2 px-2 py-0.5 text-xs bg-blue-600/10 hover:bg-blue-600/20 dark:bg-blue-700/20 dark:hover:bg-blue-700/30 rounded transition-colors focus:outline-none text-blue-700 dark:text-blue-300 flex-shrink-0">
                        Add Example
                      </button>
                    </div>

                    <!-- Expandable Info Section -->
                    <div v-if="sortKeysInfoOpen"
                      class="mt-2 bg-white/40 dark:bg-slate-900/40 p-2 rounded border border-blue-100 dark:border-blue-900/30">
                      <p class="mb-2 text-slate-700 dark:text-slate-300">
                        ClickHouse queries perform faster when filtering by sort
                        keys in the correct order.
                      </p>

                      <div>
                        <p class="font-medium mb-1 text-slate-800 dark:text-slate-200">
                          Example query:
                        </p>
                        <div
                          class="bg-blue-50 dark:bg-blue-900/20 p-1.5 rounded font-mono border border-blue-100 dark:border-blue-900/30">
                          {{ getSortKeyExampleQuery() }}
                        </div>
                      </div>
                    </div>
                  </div>
                </div>
              </template>

              <!-- "Select source" message - only when no source selected -->
              <template v-else-if="currentTeamId && !currentSourceId">
                <div
                  class="flex items-center justify-center text-muted-foreground p-6 border rounded-md bg-card shadow-sm">
                  <div class="text-center">
                    <div class="mb-2 text-muted-foreground/70">
                      <svg xmlns="http://www.w3.org/2000/svg" width="24" height="24" viewBox="0 0 24 24" fill="none"
                        stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round"
                        class="mx-auto mb-1">
                        <path
                          d="M17.8 19.2 16 11l3.5-3.5C21 6 21.5 4 21 3c-1-.5-3 0-4.5 1.5L13 8 4.8 6.2c-.5-.1-.9.1-1.1.5l-.3.5c-.2.5-.1 1 .3 1.3L9 12l-2 3H4l-1 1 3 2 2 3 1-1v-3l3-2 3.5 5.3c.3.4.8.5 1.3.3l.5-.2c.4-.3.6-.7.5-1.2z" />
                      </svg>
                    </div>
                    <p class="text-sm">
                      Please select a log source to begin exploring.
                    </p>
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

              <!-- Query Controls -->
              <div class="mt-3 flex items-center justify-between border-t pt-3" v-if="
                !isChangingContext &&
                currentSourceId &&
                hasValidSource &&
                exploreStore.timeRange
              ">
                <QueryControls @execute="handleQueryExecution" @clear="clearQueryEditor" @liveLog="liveLogPauseOnOff" />
              </div>
            </div>

            <!-- Log Histogram Visualization with Group By -->
            <div class="px-4 pb-3" v-if="
              !isChangingContext &&
              currentSourceId &&
              hasValidSource &&
              exploreStore.timeRange
            ">
              <!-- Group By controls above histogram -->
              <div class="flex items-center justify-between mb-2">
                <div class="text-xs font-medium">Time Series Distribution</div>
                <GroupBySelector :available-fields="availableFields" />
              </div>

              <!-- Histogram visualization -->
              <HistogramVisualization :group-by="exploreStore.groupByField" @zoom-time-range="onHistogramTimeRangeZoom"
                @update:timeRange="onHistogramTimeRangeZoom" />
            </div>

            <!-- Results Section -->
            <div class="flex-1 overflow-hidden flex flex-col border-t mt-2">
              <!-- Results Area -->
              <div class="flex-1 overflow-hidden relative bg-background">
                <!-- Results Table -->
                <template v-if="exploreStore.logs?.length > 0 || isExecutingQuery">
                  <!-- Render DataTable only if columns are available -->
                  <DataTable v-if="exploreStore.columns?.length > 0"
                    :key="`${exploreStore.sourceId}-${exploreStore.activeMode}-${exploreStore.queryId}`"
                    :columns="exploreStore.columns as any" :data="exploreStore.logs" :stats="exploreStore.queryStats"
                    :is-loading="isExecutingQuery" :source-id="String(exploreStore.sourceId)"
                    :team-id="teamsStore.currentTeamId" :timestamp-field="sourcesStore.currentSourceDetails?._meta_ts_field
                      " :severity-field="sourcesStore.currentSourceDetails?._meta_severity_field
                        " :timezone="displayTimezone" :query-fields="queryFields" :regex-highlights="regexHighlights"
                    :active-mode="activeMode" @drill-down="handleDrillDown" />

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
