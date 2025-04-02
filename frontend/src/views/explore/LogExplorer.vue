<script setup lang="ts">
import { ref, computed, watch, onMounted, onBeforeUnmount, nextTick } from 'vue'
import { useRouter, useRoute } from 'vue-router'
import { Button } from '@/components/ui/button'
import { Plus, Play, RefreshCw, Share2 } from 'lucide-vue-next'
import { Skeleton } from '@/components/ui/skeleton'
import { useToast } from '@/components/ui/toast'
import { TOAST_DURATION } from '@/lib/constants'
import {
  Select,
  SelectContent,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from '@/components/ui/select'
import {
  DropdownMenu,
  DropdownMenuContent,
  DropdownMenuItem,
  DropdownMenuLabel,
  DropdownMenuSeparator,
  DropdownMenuTrigger,
} from '@/components/ui/dropdown-menu'
import { DateTimePicker } from '@/components/date-time-picker'
import { useExploreStore } from '@/stores/explore'
import { useTeamsStore } from '@/stores/teams'
import { useSourcesStore } from '@/stores/sources'
import { useSavedQueriesStore } from '@/stores/savedQueries'
import { now, getLocalTimeZone, type CalendarDateTime, type DateValue, toCalendarDateTime } from '@internationalized/date'
import { createColumns } from './table/columns'
import DataTable from './table/data-table.vue'
import { formatSourceName } from '@/utils/format'
import SaveQueryModal from '@/components/saved-queries/SaveQueryModal.vue'
import QueryEditor from '@/components/query-editor/QueryEditor.vue'
import { FieldSideBar } from '@/components/field-sidebar'
import type { ColumnDef } from '@tanstack/vue-table'
import { getErrorMessage } from '@/api/types'
import { useSourceTeamManagement } from '@/composables/useSourceTeamManagement'
import { useSavedQueries } from '@/composables/useSavedQueries'
import {
  Tooltip,
  TooltipContent,
  TooltipProvider,
  TooltipTrigger,
} from '@/components/ui/tooltip'
import type { SaveQueryFormData } from '@/views/explore/types'
import type { SavedTeamQuery } from '@/api/savedQueries'
import { useExploreUrlSync } from '@/composables/useExploreUrlSync'
import { useQuery } from '@/composables/useQuery'
import type { EditorMode } from '@/views/explore/types'

// Router and stores
const router = useRouter()
const exploreStore = useExploreStore()
const teamsStore = useTeamsStore()
const sourcesStore = useSourcesStore()
const savedQueriesStore = useSavedQueriesStore()
const { toast } = useToast()
const { isInitializing, initializationError: urlError, initializeFromUrl, syncUrlFromState } = useExploreUrlSync();

// Composables
const {
  // Query content
  logchefQuery,
  sqlQuery,
  activeMode,

  // State
  queryError,
  sqlWarnings,
  isDirty,
  isExecutingQuery,
  canExecuteQuery,

  // Actions
  changeMode,
  executeQuery,
  handleTimeRangeUpdate,
  handleLimitUpdate
} = useQuery()

const {
  isProcessingTeamChange,
  isProcessingSourceChange,
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
  handleSourceChange
} = useSourceTeamManagement()

const {
  showSaveQueryModal,
  handleSaveQueryClick,
  handleSaveQuery,
  loadSavedQuery,
  updateSavedQuery,
  loadSourceQueries
} = useSavedQueries()

// Handle updating an existing query
async function handleUpdateQuery(queryId: string, formData: SaveQueryFormData) {
  // Ensure we have the necessary IDs
  if (!currentSourceId.value || !formData.team_id) {
    toast({
      title: 'Error',
      description: 'Missing source or team ID for update.',
      variant: 'destructive',
      duration: TOAST_DURATION.ERROR
    });
    return;
  }

  try {
    // Use the correct store action: updateTeamSourceQuery
    const response = await savedQueriesStore.updateTeamSourceQuery(
      formData.team_id,
      currentSourceId.value, // Pass the current source ID
      queryId,
      {
        // Payload only includes fields allowed by updateTeamSourceQuery's type
        name: formData.name,
        description: formData.description,
        query_type: formData.query_type,
        query_content: formData.query_content
      }
    );

    if (response && response.success) {
      showSaveQueryModal.value = false;
      editQueryData.value = null; // Clear editing state

      toast({
        title: 'Success',
        description: 'Query updated successfully.',
        duration: TOAST_DURATION.SUCCESS
      });

      // Optional: Refresh the list if needed, though store should be reactive
      // await loadSourceQueries(currentTeamId.value, currentSourceId.value);

    } else if (response) {
      // Handle potential API error returned in response.error
      throw new Error(getErrorMessage(response.error) || 'Failed to update query');
    }
  } catch (error) {
    console.error("Error updating query:", error);
    toast({
      title: 'Error',
      description: getErrorMessage(error),
      variant: 'destructive',
      duration: TOAST_DURATION.ERROR
    });
  }
}

// Basic state
const showFieldsPanel = ref(false)
const tableColumns = ref<ColumnDef<Record<string, any>>[]>([])
const queryEditorRef = ref()
const isLoadingQuery = ref(false)
const editQueryData = ref<SavedTeamQuery | null>(null)

// UI state computed properties
const showLoadingState = computed(() => isInitializing.value && !urlError.value)
const showNoTeamsState = computed(() => !isInitializing.value && (!availableTeams.value || availableTeams.value.length === 0))
const showNoSourcesState = computed(() =>
  !isInitializing.value &&
  !showNoTeamsState.value &&
  currentTeamId.value !== null && currentTeamId.value > 0 &&
  (!availableSources.value || availableSources.value.length === 0)
)

// Use source details from the store
const activeSourceTableName = computed(() => sourcesStore.getCurrentSourceTableName || '');

// Better track when URL query params change
const currentRoute = useRoute();

// Track both query content and mode from URL
const lastQueryParam = ref(currentRoute.query.q);
const lastModeParam = ref(currentRoute.query.mode);

// Check if we're in edit mode (URL has query_id)
const isEditingExistingQuery = computed(() => !!currentRoute.query.query_id);
const queryIdFromUrl = computed(() => currentRoute.query.query_id as string | undefined);

// Computed property to determine if the query is savable/updatable
const canSaveOrUpdateQuery = computed(() => {
  return !!currentTeamId.value &&
    !!currentSourceId.value &&
    hasValidSource.value && // Ensure source is valid
    (!!exploreStore.logchefqlCode?.trim() || !!exploreStore.rawSql?.trim());
});

// Watch for URL query parameter changes
watch(() => [currentRoute.query.q, currentRoute.query.mode], ([newQueryParam, newModeParam]) => {
  // Handle mode change from URL
  if (newModeParam !== undefined && newModeParam !== lastModeParam.value) {
    console.log('Mode parameter changed in URL:', newModeParam);
    lastModeParam.value = newModeParam as string;

    // Update mode in store based on URL
    const mode = (newModeParam as string).toLowerCase() === 'logchefql' ? 'logchefql' : 'sql';
    if (exploreStore.activeMode !== mode) {
      console.log(`Setting mode to ${mode} based on URL parameter`);
      exploreStore.setActiveMode(mode);
    }
  }

  // Handle query content change from URL
  if (newQueryParam !== undefined && newQueryParam !== lastQueryParam.value) {
    console.log('Query parameter changed in URL:', newQueryParam);
    lastQueryParam.value = newQueryParam as string;

    const decodedQuery = decodeURIComponent(newQueryParam as string);

    // Update content based on current mode (which may have just changed)
    if (exploreStore.activeMode === 'logchefql') {
      exploreStore.setLogchefqlCode(decodedQuery);
    } else {
      exploreStore.setRawSql(decodedQuery);
    }
  }
}, { immediate: true });

// Date range computed property
const dateRange = computed({
  get() {
    return exploreStore.timeRange
  },
  set(value) {
    exploreStore.setTimeRange(value)
    // URL will be updated by the watch on exploreStore.timeRange
  }
})

// Add timezone preference for display
const displayTimezone = computed(() =>
  localStorage.getItem('logchef_timezone') === 'utc' ? 'utc' : 'local'
);


// Combined error display
const displayError = computed(() => queryError.value || exploreStore.error?.message)

// Watch for initialization completion to run initial query
watch(isInitializing, async (initializing, prevInitializing) => {
  if (prevInitializing && !initializing) {
    console.log("Initialization complete. Checking for saved query ID and if initial query should run.");

    const queryId = queryIdFromUrl.value; // Get query ID from computed property

    if (queryId) {
      console.log(`Found query_id=${queryId} in URL. Attempting to load saved query...`);
      if (!currentTeamId.value) {
        console.error("Cannot load saved query: Team ID is missing.");
        toast({
          title: 'Error',
          description: 'Cannot load saved query because the team context is missing.',
          variant: 'destructive',
          duration: TOAST_DURATION.ERROR
        });
        return; // Exit if no team ID
      }

      try {
        // 1. Fetch the query details using the store
        isLoadingQuery.value = true;
        const fetchResult = await savedQueriesStore.fetchTeamSourceQueryDetails(
          currentTeamId.value,
          currentSourceId.value, // Pass the currentSourceId
          queryId
        );
        isLoadingQuery.value = false;

        if (fetchResult.success && savedQueriesStore.selectedQuery) {
          // 2. Pass the fetched query object to loadSavedQuery
          const loadResult = await loadSavedQuery(savedQueriesStore.selectedQuery);

          if (loadResult) {
            console.log(`Successfully loaded and applied saved query ${queryId}`);
            // Sync URL after successful load and application
            syncUrlFromState();
          } else {
            // loadSavedQuery failed internally (already showed toast)
            console.error(`loadSavedQuery function failed for query ${queryId}`);
            // Potentially clear query_id from URL here?
          }
        } else {
          // Fetching the query from the store failed
          throw new Error(fetchResult.error?.message || `Failed to fetch query details for ID ${queryId}`);
        }
      } catch (error) {
        isLoadingQuery.value = false; // Ensure loading is false on error
        console.error(`Failed to load or apply saved query ${queryId}:`, error);
        toast({
          title: 'Error Loading Saved Query',
          description: getErrorMessage(error),
          variant: 'destructive',
          duration: TOAST_DURATION.ERROR
        });
        // Decide how to handle failure - maybe clear query_id from URL?
      }
    }

    // Make sure we have a valid time range before executing the query
    if (!exploreStore.timeRange || !exploreStore.timeRange.start || !exploreStore.timeRange.end) {
      console.log("Setting default time range as none was provided in saved query");
      // Set a default time range (last 24 hours)
      exploreStore.setTimeRange({
        start: now(getLocalTimeZone()).subtract({ hours: 24 }),
        end: now(getLocalTimeZone())
      });
    }

    // Now check if we can execute the query (either the one from URL or the loaded saved one)
    if (canExecuteQuery.value && (exploreStore.logchefqlCode || exploreStore.rawSql)) {
      console.log("Running initial query...")
      await executeQuery()
    } else {
      console.log("Skipping initial query (missing requirements or no query content)")
    }
  }
}, { immediate: false })

// Watch for source changes to fetch details AND saved queries
watch(
  () => currentSourceId.value, // Watch the computed property from useSourceTeamManagement
  async (newSourceId, oldSourceId) => {
    // Skip during initialization to prevent redundant calls
    if (isInitializing.value) {
      console.log(`LogExplorer: Skipping source details/queries update during initialization phase`);
      return;
    }

    if (newSourceId !== oldSourceId) {
      // Fetch Source Details (existing logic)
      if (newSourceId && newSourceId > 0) {
        console.log(`LogExplorer: Source changed to ${newSourceId}, fetching details and saved queries...`);
        // Verify source existence (using teamSources from useSourceTeamManagement)
        const sourceExists = availableSources.value.some(source => source.id === newSourceId);
        if (sourceExists) {
          // Fetch details (debounced)
          setTimeout(async () => {
            if (currentSourceId.value === newSourceId) { // Check if still the same after timeout
              await sourcesStore.loadSourceDetails(newSourceId);
            }
          }, 50);

          // Fetch Saved Queries (no debounce needed?)
          if (currentTeamId.value) { // Ensure team ID is available
            await loadSourceQueries(currentTeamId.value, newSourceId);
          } else {
            console.warn("LogExplorer: Cannot load saved queries, team ID is missing.");
          }
        } else {
          console.warn(`LogExplorer: Source ${newSourceId} not found in current team's sources`);
        }
      } else {
        // Clear saved queries if source is deselected
        // (useSavedQueries composable handles clearing its internal state if needed)
        if (currentTeamId.value) {
          console.log("LogExplorer: Source cleared, attempting to clear/load empty queries for team.");
          // Call load with invalid source ID (or maybe add a dedicated clear function to composable?)
          // For now, loading with 0 might not work as intended depending on composable logic.
          // Let's assume the composable handles the empty/invalid source case gracefully.
          await loadSourceQueries(currentTeamId.value, 0); // Or handle clearing explicitly
        } else {
          console.warn("LogExplorer: Cannot clear saved queries, team ID is missing.");
        }
      }
    }
  },
  { immediate: false } // Don't run immediately, wait for initialization
)

// Watch for changes in currentTeamId to update sources AND saved queries
watch(
  () => currentTeamId.value, // Watch the computed property from useSourceTeamManagement
  async (newTeamId, oldTeamId) => {
    // Skip during initialization
    if (isInitializing.value) {
      console.log(`LogExplorer: Skipping team change actions during initialization.`);
      return;
    }

    if (newTeamId !== oldTeamId && newTeamId) {
      console.log(`LogExplorer: Team changed to ${newTeamId}, updating sources and potentially saved queries...`)

      // Load sources for the new team (existing logic)
      const sourcesResult = await sourcesStore.loadTeamSources(newTeamId)
      let newSourceIdToLoadQueries: number | null = null;

      if (!sourcesResult.success || !sourcesResult.data || sourcesResult.data.length === 0) {
        exploreStore.setSource(0)
        newSourceIdToLoadQueries = 0; // Signal to load empty queries
      } else {
        const currentSourceExists = sourcesStore.teamSources.some(
          source => source.id === exploreStore.sourceId
        )
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
        console.log(`LogExplorer: Loading saved queries for team ${newTeamId}, source ${newSourceIdToLoadQueries}`);
        await loadSourceQueries(newTeamId, newSourceIdToLoadQueries);
      } else {
        console.warn("LogExplorer: Could not determine source ID after team change, skipping saved queries load.");
      }
    }
  },
  { immediate: false } // Don't run immediately
)

// Watch for changes in sourceDetails from the store
watch(
  () => sourceDetails.value,
  (newSourceDetails, oldSourceDetails) => {
    if (newSourceDetails?.id !== oldSourceDetails?.id) {
      console.log('Source details changed in store, availableFields will update.');
      // Handle pending SQL restoration if needed (logic might move to store or be simplified)
      // Consider if pendingRawSql is still necessary with better store sync
    }
  },
  { immediate: true }
)

// Watch for changes in exploreStore.columns to update tableColumns
watch(
  () => exploreStore.columns,
  (newColumns) => {
    if (newColumns?.length > 0) {
      console.log('LogExplorer: Creating table columns from API response:', newColumns);
      // Get display preferences
      const timezone = displayTimezone.value;
      const tsField = sourceDetails.value?._meta_ts_field || 'timestamp';
      const severityField = sourceDetails.value?._meta_severity_field || 'severity_text';

      // Create columns using the columns utility function
      tableColumns.value = createColumns(newColumns, tsField, timezone, severityField);
      console.log('LogExplorer: Table columns created:', tableColumns.value.length);
    } else {
      console.log('LogExplorer: No columns available, clearing table columns');
      tableColumns.value = [];
    }
  },
  { immediate: true }
);

// Watch time range changes and update query dirty state
watch(
  () => exploreStore.timeRange,
  (newRange, oldRange) => {
    if (!newRange || !oldRange) return;
    if (JSON.stringify(newRange) === JSON.stringify(oldRange)) return;

    // Use the query builder's handler for time range updates
    // This will set isDirty and show a notification instead of updating SQL directly
    handleTimeRangeUpdate();
  },
  { deep: true }
);

// Watch limit changes and update query dirty state
watch(
  () => exploreStore.limit,
  (newLimit, oldLimit) => {
    if (newLimit === oldLimit) return;

    // Use the query builder's handler for limit updates
    // This will set isDirty and show a notification instead of updating SQL directly
    handleLimitUpdate();
  }
);

// Function to copy current URL to clipboard
const copyUrlToClipboard = () => {
  try {
    navigator.clipboard.writeText(window.location.href);
    toast({
      title: "URL Copied",
      description: "The shareable link has been copied to your clipboard.",
      duration: TOAST_DURATION.INFO // Changed from SHORT to INFO (assuming INFO exists)
    });
  } catch (error) {
    console.error("Failed to copy URL: ", error);
    toast({
      title: "Copy Failed",
      description: "Failed to copy URL to clipboard.",
      variant: "destructive",
      duration: TOAST_DURATION.ERROR
    });
  }
}

// Component lifecycle with improved initialization sequence
onMounted(async () => {
  console.log("LogExplorer component mounting - Simplified");
  try {
    // Call the initialization function from the composable
    await initializeFromUrl();

    // Initial query execution moved to a watch effect for better reliability

  } catch (error) {
    console.error("Error during LogExplorer mount:", error);
    toast({
      title: "Explorer Error",
      description: "Error initializing the explorer. Please try refreshing the page.",
      variant: "destructive",
      duration: TOAST_DURATION.ERROR,
    });
  }
});

// --- Event Handlers for QueryEditor --- START
const updateLogchefqlValue = (newValue: string) => {
  logchefQuery.value = newValue;
};

const updateSqlValue = (newValue: string) => {
  sqlQuery.value = newValue;
};
// --- Event Handlers for QueryEditor --- END

// --- Internal Helper Functions for time conversion ---
function calendarDateTimeToTimestamp(dateTime: DateValue | null | undefined): number | null {
  if (!dateTime) return null;
  try {
    // Convert any DateValue to JS Date object using the local timezone
    const date = dateTime.toDate(getLocalTimeZone());
    return date.getTime();
  } catch (e) {
    console.error("Error converting DateValue to timestamp:", e);
    return null;
  }
}

// New handler for the Save/Update button
const handleSaveOrUpdateClick = async () => {
  const queryId = queryIdFromUrl.value;

  // Check if we can save
  if (!canSaveOrUpdateQuery.value) {
    toast({
      title: 'Cannot Save Query',
      description: 'Missing required fields (Team, Source, Query).',
      duration: TOAST_DURATION.WARNING
    });
    return;
  }

  if (queryId && currentTeamId.value && currentSourceId.value) {
    // --- Update Existing Query Flow ---
    console.log(`Attempting to update saved query ${queryId}`);

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
        editQueryData.value = existingQuery; // Pass only the valid SavedTeamQuery object
        // The modal component itself should handle getting the *current* editor content

      } else {
        throw new Error("Failed to load query details");
      }
    } catch (error) {
      console.error(`Error loading query for edit:`, error);
      toast({
        title: 'Error',
        description: 'Failed to load query details for editing.',
        variant: 'destructive',
        duration: TOAST_DURATION.ERROR
      });
    } finally {
      isLoadingQuery.value = false;
    }
  } else {
    // --- Save New Query Flow ---
    console.log("Opening save new query modal...");
    editQueryData.value = null; // Reset edit data
    handleSaveQueryClick(); // Call original function to open modal
  }
};

onBeforeUnmount(() => {
  if (import.meta.env.MODE !== 'production') {
    console.log("LogExplorer unmounted")
  }
})
</script>

<template>
  <!-- Loading State -->
  <div v-if="showLoadingState" class="flex items-center justify-center h-[calc(100vh-12rem)]">
    <p class="text-muted-foreground animate-pulse">Loading Explorer...</p>
  </div>

  <!-- No Teams State -->
  <div v-else-if="showNoTeamsState"
    class="flex flex-col items-center justify-center h-[calc(100vh-12rem)] gap-4 text-center">
    <h2 class="text-2xl font-semibold">No Teams Available</h2>
    <p class="text-muted-foreground max-w-md">
      You need to be part of a team to explore logs. Contact your administrator.
    </p>
    <Button variant="outline" @click="router.push({ name: 'Home' })">Go to Dashboard</Button>
  </div>

  <!-- No Sources State (Team Selected) -->
  <div v-else-if="showNoSourcesState" class="flex flex-col h-[calc(100vh-12rem)]">
    <!-- Header bar for team selection -->
    <div class="border-b py-2 px-4 flex items-center h-12">
      <div class="flex items-center space-x-3">
        <Select :model-value="currentTeamId?.toString() ?? ''" @update:model-value="handleTeamChange"
          :disabled="isProcessingTeamChange">
          <SelectTrigger class="h-8 text-sm w-48">
            <SelectValue placeholder="Select team">{{ selectedTeamName }}</SelectValue>
          </SelectTrigger>
          <SelectContent>
            <SelectItem v-for="team in availableTeams" :key="team.id" :value="team.id.toString()">
              {{ team.name }}
            </SelectItem>
          </SelectContent>
        </Select>
        <span class="text-sm text-muted-foreground italic">No sources in this team.</span>
      </div>
      <Button size="sm" class="ml-auto h-8" @click="router.push({ name: 'NewSource' })">
        Add Source
      </Button>
    </div>
    <!-- Empty state content -->
    <div class="flex flex-col items-center justify-center flex-1 gap-4 text-center">
      <h2 class="text-2xl font-semibold">No Log Sources Found</h2>
      <p class="text-muted-foreground max-w-md">
        The selected team '{{ selectedTeamName }}' has no sources configured. Add one or switch teams.
      </p>
    </div>
  </div>

  <!-- Main Explorer View -->
  <div v-else class="flex flex-col h-screen overflow-hidden">
    <!-- URL Error -->
    <div v-if="urlError"
      class="absolute top-0 left-0 right-0 bg-destructive/15 text-destructive px-4 py-2 z-10 flex items-center justify-between">
      <span class="text-sm">{{ urlError }}</span>
      <Button variant="ghost" size="sm" @click="urlError = null" class="h-7 px-2">Dismiss</Button>
    </div>

    <!-- Filter Bar -->
    <div class="border-b bg-background py-2 px-4 flex items-center h-12 shadow-sm">
      <!-- Data Source Group -->
      <div class="flex items-center space-x-2 min-w-0">
        <!-- Team Selector -->
        <Select :model-value="currentTeamId?.toString() ?? ''" @update:model-value="handleTeamChange"
          :disabled="isProcessingTeamChange">
          <SelectTrigger class="h-8 text-sm w-48">
            <SelectValue placeholder="Select team">{{ selectedTeamName }}</SelectValue>
          </SelectTrigger>
          <SelectContent>
            <SelectItem v-for="team in availableTeams" :key="team.id" :value="team.id.toString()">
              {{ team.name }}
            </SelectItem>
          </SelectContent>
        </Select>

        <!-- Source Selector -->
        <Select :model-value="currentSourceId?.toString() ?? ''" @update:model-value="handleSourceChange"
          :disabled="isProcessingSourceChange || !currentTeamId || availableSources.length === 0">
          <SelectTrigger class="h-8 text-sm w-64">
            <SelectValue placeholder="Select source">{{ selectedSourceName }}</SelectValue>
          </SelectTrigger>
          <SelectContent>
            <SelectItem v-if="!currentTeamId" value="no-team" disabled>Select a team first</SelectItem>
            <SelectItem v-else-if="availableSources.length === 0" value="no-sources" disabled>No sources available
            </SelectItem>
            <SelectItem v-for="source in availableSources" :key="source.id" :value="source.id.toString()">
              {{ formatSourceName(source) }}
            </SelectItem>
          </SelectContent>
        </Select>
      </div>

      <!-- Divider -->
      <div class="h-6 w-px bg-border mx-3"></div>

      <!-- Time Controls Group -->
      <div class="flex items-center space-x-2 flex-grow">
        <!-- Date/Time Picker -->
        <TooltipProvider v-if="activeMode === 'sql'">
          <Tooltip>
            <TooltipTrigger asChild>
              <div>
                <DateTimePicker v-model="dateRange" class="h-8" :disabled="activeMode === 'sql'" />
              </div>
            </TooltipTrigger>
            <TooltipContent>
              <p>Time range is not configurable in SQL mode. Use time filters in your query.</p>
            </TooltipContent>
          </Tooltip>
        </TooltipProvider>
        <DateTimePicker v-else v-model="dateRange" class="h-8" />

        <!-- Limit Dropdown -->
        <TooltipProvider v-if="activeMode === 'sql'">
          <Tooltip>
            <TooltipTrigger asChild>
              <div>
                <DropdownMenu>
                  <DropdownMenuTrigger asChild>
                    <Button variant="outline" size="sm" class="h-8 text-sm justify-between px-2 min-w-[90px]"
                      :disabled="activeMode === 'sql'">
                      <span>Limit:</span> {{ exploreStore.limit.toLocaleString() }}
                    </Button>
                  </DropdownMenuTrigger>
                  <DropdownMenuContent align="end">
                    <DropdownMenuLabel>Results Limit</DropdownMenuLabel>
                    <DropdownMenuSeparator />
                    <DropdownMenuItem v-for="limit in [100, 500, 1000, 2000, 5000, 10000]" :key="limit"
                      @click="exploreStore.setLimit(limit)" :disabled="exploreStore.limit === limit">
                      {{ limit.toLocaleString() }} rows
                    </DropdownMenuItem>
                  </DropdownMenuContent>
                </DropdownMenu>
              </div>
            </TooltipTrigger>
            <TooltipContent>
              <p>Limit is not configurable in SQL mode. Use LIMIT in your query.</p>
            </TooltipContent>
          </Tooltip>
        </TooltipProvider>
        <DropdownMenu v-else>
          <DropdownMenuTrigger asChild>
            <Button variant="outline" size="sm" class="h-8 text-sm justify-between px-2 min-w-[90px]">
              <span>Limit:</span> {{ exploreStore.limit.toLocaleString() }}
            </Button>
          </DropdownMenuTrigger>
          <DropdownMenuContent align="end">
            <DropdownMenuLabel>Results Limit</DropdownMenuLabel>
            <DropdownMenuSeparator />
            <DropdownMenuItem v-for="limit in [100, 500, 1000, 2000, 5000, 10000]" :key="limit"
              @click="exploreStore.setLimit(limit)" :disabled="exploreStore.limit === limit">
              {{ limit.toLocaleString() }} rows
            </DropdownMenuItem>
          </DropdownMenuContent>
        </DropdownMenu>
      </div>

      <!-- Share Button -->
      <TooltipProvider>
        <Tooltip>
          <TooltipTrigger as-child>
            <Button variant="outline" size="sm" class="h-8 ml-2" @click="copyUrlToClipboard">
              <Share2 class="h-4 w-4 mr-1.5" />
              Share
            </Button>
          </TooltipTrigger>
          <TooltipContent>
            <p>Copy shareable link</p>
          </TooltipContent>
        </Tooltip>
      </TooltipProvider>
    </div>

    <!-- Main Content Area -->
    <div class="flex flex-1 min-h-0">
      <FieldSideBar v-model:expanded="showFieldsPanel" :fields="availableFields" />

      <div class="flex-1 flex flex-col h-full min-w-0 overflow-hidden">
        <!-- Query Editor -->
        <div class="px-4 py-3">
          <template v-if="currentSourceId && hasValidSource && exploreStore.timeRange">
            <div class="bg-card shadow-sm rounded-md overflow-hidden">
              <QueryEditor ref="queryEditorRef" :sourceId="currentSourceId" :teamId="currentTeamId ?? 0"
                :schema="sourceDetails?.columns?.reduce((acc, col) => ({ ...acc, [col.name]: { type: col.type } }), {}) || {}"
                :activeMode="exploreStore.activeMode === 'logchefql' ? 'logchefql' : 'clickhouse-sql'"
                :logchefqlValue="logchefQuery" :sqlValue="sqlQuery" @update:logchefqlValue="updateLogchefqlValue"
                @update:sqlValue="updateSqlValue"
                :placeholder="exploreStore.activeMode === 'logchefql' ? 'Enter LogchefQL query...' : 'Enter SQL query...'"
                :tsField="sourceDetails?._meta_ts_field || 'timestamp'" :tableName="activeSourceTableName"
                :showFieldsPanel="showFieldsPanel" @submit="executeQuery"
                @update:activeMode="(mode) => changeMode(mode === 'logchefql' ? 'logchefql' : 'sql')"
                @toggle-fields="showFieldsPanel = !showFieldsPanel" @select-saved-query="loadSavedQuery"
                @save-query="handleSaveOrUpdateClick" class="border-0 border-b" />
            </div>
          </template>
          <template v-else-if="currentTeamId && !currentSourceId">
            <div class="flex items-center justify-center text-muted-foreground p-6 border rounded-md bg-card shadow-sm">
              <div class="text-center">
                <div class="mb-2 text-muted-foreground/70">
                  <svg xmlns="http://www.w3.org/2000/svg" width="24" height="24" viewBox="0 0 24 24" fill="none"
                    stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round"
                    class="mx-auto mb-1">
                    <path
                      d="M17.8 19.2 16 11l3.5-3.5C21 6 21.5 4 21 3c-1-.5-3 0-4.5 1.5L13 8 4.8 6.2c-.5-.1-.9.1-1.1.5l-.3.5c-.2.5-.1 1 .3 1.3L9 12l-2 3H4l-1 1 3 2 2 3 1-1v-3l3-2 3.5 5.3c.3.4.8.5 1.3.3l.5-.2c.4-.3.6-.7.5-1.2z" />
                  </svg>
                </div>
                <p class="text-sm">Please select a log source to begin exploring.</p>
              </div>
            </div>
          </template>
          <template v-else-if="currentSourceId && !hasValidSource">
            <div
              class="flex items-center justify-center text-destructive p-6 border border-destructive/30 rounded-md bg-destructive/5 shadow-sm">
              <div class="text-center">
                <div class="mb-2 text-destructive/80">
                  <svg xmlns="http://www.w3.org/2000/svg" width="24" height="24" viewBox="0 0 24 24" fill="none"
                    stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round"
                    class="mx-auto mb-1">
                    <circle cx="12" cy="12" r="10" />
                    <line x1="12" x2="12" y1="8" y2="12" />
                    <line x1="12" x2="12.01" y1="16" y2="16" />
                  </svg>
                </div>
                <p class="text-sm">Error loading source details. Please try again.</p>
              </div>
            </div>
          </template>

          <!-- Query Controls -->
          <div class="mt-3 flex items-center justify-between border-t pt-3"
            v-if="currentSourceId && hasValidSource && exploreStore.timeRange">
            <Button variant="default" class="h-9 px-4 flex items-center gap-2 shadow-sm" :class="{
              'bg-amber-500 hover:bg-amber-600 text-amber-foreground': isDirty && !isExecutingQuery,
              'bg-sky-500 hover:bg-sky-600 text-sky-foreground': isExecutingQuery
            }" :disabled="isExecutingQuery || !canExecuteQuery" @click="executeQuery">
              <Play v-if="!isExecutingQuery" class="h-4 w-4" />
              <RefreshCw v-else class="h-4 w-4 animate-spin" />
              <span>{{ isExecutingQuery ? 'Running Query...' : (isDirty ? 'Run Query*' : 'Run Query') }}</span>
            </Button>

            <!-- Query Stats Preview -->
            <div class="text-xs text-muted-foreground flex items-center gap-3"
              v-if="exploreStore.lastExecutionTimestamp">
              <span>Last successful run: {{ new Date(exploreStore.lastExecutionTimestamp).toLocaleTimeString() }}</span>
            </div>
          </div>

          <!-- Query Error -->
          <div v-if="displayError" class="mt-2 text-sm text-destructive bg-destructive/10 p-2 rounded">
            {{ displayError }}
          </div>
        </div>

        <!-- Results Section -->
        <div class="flex-1 overflow-hidden flex flex-col border-t mt-2">
          <!-- Stats Header -->
          <div v-if="exploreStore.queryStats && !isExecutingQuery"
            class="px-4 py-2 flex items-center justify-between bg-muted/30 text-xs text-muted-foreground flex-shrink-0 border-b">
            <div class="flex items-center gap-4">
              <span v-if="exploreStore.queryStats?.rows_read != null" class="flex items-center">
                <svg xmlns="http://www.w3.org/2000/svg" width="14" height="14" viewBox="0 0 24 24" fill="none"
                  stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round" class="mr-1.5">
                  <path
                    d="M8 3H7a2 2 0 0 0-2 2v16a2 2 0 0 0 2 2h12a2 2 0 0 0 2-2V7.414A2 2 0 0 0 20.414 6L18 3.586A2 2 0 0 0 16.586 3H9" />
                  <path d="M8 3v3a1 1 0 0 0 1 1h4a1 1 0 0 0 1-1V3" />
                  <path d="M10 14h8" />
                  <path d="M10 10h8" />
                  <path d="M10 18h8" />
                </svg>
                <span>
                  <strong class="font-medium">{{ exploreStore.queryStats.rows_read.toLocaleString() }}</strong> rows
                </span>
              </span>
              <span v-if="exploreStore.queryStats?.execution_time_ms != null" class="flex items-center">
                <svg xmlns="http://www.w3.org/2000/svg" width="14" height="14" viewBox="0 0 24 24" fill="none"
                  stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round" class="mr-1.5">
                  <circle cx="12" cy="12" r="10" />
                  <polyline points="12 6 12 12 16 14" />
                </svg>
                <span>
                  <strong class="font-medium">{{ (exploreStore.queryStats.execution_time_ms / 1000).toFixed(2)
                    }}</strong>s
                </span>
              </span>
              <span v-if="exploreStore.queryStats?.bytes_read != null" class="flex items-center">
                <svg xmlns="http://www.w3.org/2000/svg" width="14" height="14" viewBox="0 0 24 24" fill="none"
                  stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round" class="mr-1.5">
                  <path d="M21 15v4a2 2 0 0 1-2 2H5a2 2 0 0 1-2-2v-4" />
                  <polyline points="17 8 12 3 7 8" />
                  <line x1="12" x2="12" y1="3" y2="15" />
                </svg>
                <span>
                  <strong class="font-medium">{{ (exploreStore.queryStats.bytes_read / 1024 / 1024).toFixed(2)
                    }}</strong> MB
                </span>
              </span>
            </div>
          </div>

          <!-- Results Area -->
          <div class="flex-1 overflow-hidden relative bg-background">
            <!-- Loading State -->
            <template v-if="isExecutingQuery">
              <div class="absolute inset-0 flex items-center justify-center bg-background/70 z-10">
                <p class="text-muted-foreground animate-pulse">Loading results...</p>
              </div>
            </template>

            <!-- Results Table -->
            <template v-if="!isExecutingQuery && exploreStore.logs?.length">
              <DataTable v-if="exploreStore.logs.length > 0 && tableColumns.length > 0"
                :key="`${exploreStore.sourceId}-${exploreStore.activeMode}-${exploreStore.queryId}`"
                :columns="tableColumns" :data="exploreStore.logs" :stats="exploreStore.queryStats"
                :source-id="String(exploreStore.sourceId)" :team-id="teamsStore.currentTeamId"
                :timestamp-field="sourcesStore.currentSourceDetails?._meta_ts_field"
                :severity-field="sourcesStore.currentSourceDetails?._meta_severity_field" :timezone="displayTimezone" />
            </template>

            <!-- No Results State -->
            <template v-else-if="!isExecutingQuery && !exploreStore.logs?.length && exploreStore.lastExecutedState">
              <div class="h-full flex flex-col items-center justify-center p-10 text-center">
                <svg xmlns="http://www.w3.org/2000/svg" width="36" height="36" viewBox="0 0 24 24" fill="none"
                  stroke="currentColor" stroke-width="1.5" stroke-linecap="round" stroke-linejoin="round"
                  class="text-muted-foreground mb-3">
                  <path d="M14.5 2H6a2 2 0 0 0-2 2v16a2 2 0 0 0 2 2h12a2 2 0 0 0 2-2V7.5L14.5 2z"></path>
                  <polyline points="14 2 14 8 20 8"></polyline>
                  <line x1="12" x2="12" y1="18" y2="12"></line>
                  <line x1="9" x2="15" y1="15" y2="15"></line>
                </svg>
                <h3 class="text-lg font-medium mb-1">No Logs Found</h3>
                <p class="text-sm text-muted-foreground max-w-md">
                  Your query returned no results for the selected time range. Try adjusting the query or time.
                </p>
                <Button variant="outline" size="sm" class="mt-4 h-8" @click="exploreStore.setTimeRange({
                  start: now(getLocalTimeZone()).subtract({ hours: 24 }),
                  end: now(getLocalTimeZone())
                })">
                  Try Last 24 Hours
                </Button>
              </div>
            </template>

            <!-- Initial State -->
            <template v-else-if="!isExecutingQuery && !exploreStore.lastExecutedState && canExecuteQuery">
              <div class="h-full flex flex-col items-center justify-center p-10 text-center">
                <div class="bg-primary/5 p-6 rounded-full mb-4">
                  <svg xmlns="http://www.w3.org/2000/svg" width="40" height="40" viewBox="0 0 24 24" fill="none"
                    stroke="currentColor" stroke-width="1.5" stroke-linecap="round" stroke-linejoin="round"
                    class="text-primary">
                    <circle cx="11" cy="11" r="8"></circle>
                    <line x1="21" x2="16.65" y1="21" y2="16.65"></line>
                    <line x1="11" x2="11" y1="8" y2="14"></line>
                    <line x1="8" x2="14" y1="11" y2="11"></line>
                  </svg>
                </div>
                <h3 class="text-xl font-medium mb-2">Ready to Explore</h3>
                <p class="text-sm text-muted-foreground max-w-md mb-4">
                  Enter a query or use the default, then click 'Run' to see logs.
                </p>
                <Button variant="outline" size="sm" @click="executeQuery"
                  class="border-primary/20 text-primary hover:bg-primary/5 hover:text-primary hover:border-primary/30">
                  <Play class="h-3.5 w-3.5 mr-1.5" />
                  Run default query
                </Button>
              </div>
            </template>
          </div>
        </div>
      </div>
    </div>

    <!-- Save Query Modal -->
    <SaveQueryModal v-if="showSaveQueryModal" :is-open="showSaveQueryModal" :query-type="exploreStore.activeMode"
      :edit-data="editQueryData" :query-content="JSON.stringify({
        sourceId: currentSourceId,
        limit: exploreStore.limit,
        content: exploreStore.activeMode === 'logchefql' ? exploreStore.logchefqlCode : exploreStore.rawSql
      })" @close="showSaveQueryModal = false" @save="handleSaveQuery" @update="handleUpdateQuery" />
  </div>
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

/* Enhance table styling */
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
  background-color: hsl(var(--muted)/0.3);
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
  background-color: hsl(var(--destructive)/0.15);
  color: hsl(var(--destructive));
}

:deep(.severity-warn),
:deep(.severity-warning) {
  background-color: hsl(var(--warning)/0.15);
  color: hsl(var(--warning));
}

:deep(.severity-info) {
  background-color: hsl(var(--info)/0.15);
  color: hsl(var(--info));
}

:deep(.severity-debug) {
  background-color: hsl(var(--muted)/0.5);
  color: hsl(var(--muted-foreground));
}

/* Subtle pulse animation for dirty button state */
@keyframes subtle-pulse {

  0%,
  100% {
    box-shadow: 0 0 0 0 rgba(255, 255, 255, 0.2);
  }

  50% {
    box-shadow: 0 0 0 4px rgba(255, 255, 255, 0);
  }
}

.animate-subtle-pulse {
  animation: subtle-pulse 1.5s infinite ease-in-out;
}
</style>
