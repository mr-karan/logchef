<script setup lang="ts">
import { ref, onMounted, onBeforeUnmount, watch, computed, nextTick } from 'vue'
import type { WritableComputedRef } from 'vue'
import { useRoute, useRouter } from 'vue-router'
import { Button } from '@/components/ui/button'
import { Skeleton } from '@/components/ui/skeleton'
import { Label } from '@/components/ui/label'
import { Select, SelectContent, SelectItem, SelectTrigger, SelectValue, SelectGroup } from '@/components/ui/select'
import {
  NumberField,
  NumberFieldContent,
  NumberFieldDecrement,
  NumberFieldIncrement,
  NumberFieldInput,
} from '@/components/ui/number-field'
import { useToast } from '@/components/ui/toast'
import { useDebounceFn } from '@vueuse/core'
import type { FilterCondition } from '@/api/explore'
import { getErrorMessage } from '@/api/types'
import { TOAST_DURATION } from '@/lib/constants'
import { DateTimePicker } from '@/components/date-time-picker'
import { getLocalTimeZone, now, CalendarDateTime, type DateValue } from '@internationalized/date'
import type { DateRange } from 'radix-vue'
import { Search, Plus, ChevronRight } from 'lucide-vue-next'
import { createColumns } from './table/columns'
import DataTable from './table/data-table.vue'
import EmptyState from './EmptyState.vue'
import type { ColumnDef } from '@tanstack/vue-table'
import DataTableFilters from './table/data-table-filters.vue'
import SQLEditor from '@/components/sql-editor/SQLEditor.vue'
import { formatSourceName } from '@/utils/format'
import { useSourcesStore } from '@/stores/sources'
import { useExploreStore } from '@/stores/explore'
import { useTeamsStore } from '@/stores/teams'
import { useSqlGenerator } from '@/composables/useSqlGenerator'
import { registerSeverityField } from '@/lib/utils'
import SqlPreview from '@/components/sql-preview/SqlPreview.vue'
import { Tabs, TabsList, TabsTrigger, TabsContent } from '@/components/ui/tabs'
import { Collapsible, CollapsibleTrigger, CollapsibleContent } from '@/components/ui/collapsible'
import SmartFilterBar from '@/components/smart-filter/SmartFilterBar.vue'
import SavedQueriesDropdown from '@/components/saved-queries/SavedQueriesDropdown.vue'
import SaveQueryModal from '@/components/saved-queries/SaveQueryModal.vue'
import { useSavedQueriesStore } from '@/stores/savedQueries'
import { serializeQueryState, deserializeQueryContent } from '@/utils/querySerializer'
import type { SavedTeamQuery } from '@/api/types'
import type { CreateTeamQueryRequest } from '@/api/sources'

const router = useRouter()
const sourcesStore = useSourcesStore()
const exploreStore = useExploreStore()
const savedQueriesStore = useSavedQueriesStore()
const teamsStore = useTeamsStore()
const { toast } = useToast()
const route = useRoute()

// Saved queries state
const showSaveQueryModal = ref(false)
const queryToSave = ref('')

// Local UI state
const isLoading = computed(() => exploreStore.isLoading || sourcesStore.isLoading || teamsStore.isLoading)
const previewOpen = ref(false)
const activeTab = ref('filters')
const showSqlTab = ref(false)

// Flag to prevent unnecessary URL updates on initial load
const isInitialLoad = ref(true)

// Date state
const currentTime = now(getLocalTimeZone())

// Computed DateRange for v-model binding
const dateRange = computed({
  get: () => ({
    start: exploreStore.data.timeRange?.start || currentTime.subtract({ hours: 3 }),
    end: exploreStore.data.timeRange?.end || currentTime
  }),
  set: (newValue: DateRange | null) => {
    if (newValue?.start && newValue?.end) {
      // Create a properly typed range object
      const range: { start: DateValue; end: DateValue } = {
        start: newValue.start as DateValue,
        end: newValue.end as DateValue
      }
      exploreStore.setTimeRange(range)
      // Update SQL when time range changes
      updateSql()
    }
  }
}) as unknown as WritableComputedRef<DateRange>

// Add loading state handling
const showLoadingState = computed(() => {
  return sourcesStore.isLoading
})

// Fix the empty state computed to handle null sources from API
const showEmptyState = computed(() => {
  return !sourcesStore.isLoading &&
    (!sourcesStore.deduplicatedSources || sourcesStore.deduplicatedSources.length === 0)
})

// Get the teams for the selected source
const sourceTeams = computed(() => {
  if (!exploreStore.data.sourceId) return []
  return sourcesStore.getTeamsForSource(exploreStore.data.sourceId)
})

// Fix the selected source name computed to handle null sources
const selectedSourceName = computed(() => {
  const source = sourcesStore.sourcesWithTeams.find(s => s.id === exploreStore.data.sourceId)
  return source ? formatSourceName(source) : 'Select a source'
})

// Fix the selected source details computed to handle null sources with thorough null-checking
const selectedSourceDetails = computed(() => {
  // First ensure sources are loaded
  if (!sourcesStore.sourcesWithTeams || !Array.isArray(sourcesStore.sourcesWithTeams)) {
    return { database: '', table: '' };
  }

  // Check if we have a valid source ID
  if (!exploreStore.data || !exploreStore.data.sourceId) {
    return { database: '', table: '' };
  }

  // Find the source with the matching ID
  const source = sourcesStore.sourcesWithTeams.find(s => s?.id === exploreStore.data.sourceId);

  // If no source found or it doesn't have valid connection info
  if (!source || !source.connection) {
    return { database: '', table: '' };
  }

  // Return connection details with extra safety
  return {
    database: source.connection.database || '',
    table: source.connection.table_name || ''
  };
})

// Add team selection name computed
const selectedTeamName = computed(() => {
  return teamsStore.currentTeam?.name || 'Select a team'
})

// Add a computed property for the selected team ID that handles undefined correctly
const selectedTeamIdForDropdown = computed(() => {
  return teamsStore.currentTeamId ? teamsStore.currentTeamId.toString() : undefined;
});

// Function to get timestamps from store's time range with robust error handling
const getTimestamps = () => {
  try {
    // Check if store data is initialized
    if (!exploreStore.data) {
      console.log("getTimestamps: exploreStore.data is not initialized");
      return {
        start: Date.now() - 3 * 60 * 60 * 1000, // 3 hours ago
        end: Date.now()
      };
    }

    const timeRange = exploreStore.data.timeRange;

    // Check if timeRange exists and has valid start/end properties
    if (!timeRange || !timeRange.start || !timeRange.end) {
      console.log("getTimestamps: timeRange is missing or incomplete");
      return {
        start: Date.now() - 3 * 60 * 60 * 1000, // 3 hours ago
        end: Date.now()
      };
    }

    // Validate that required properties exist
    if (typeof timeRange.start.year !== 'number' ||
      typeof timeRange.start.month !== 'number' ||
      typeof timeRange.start.day !== 'number' ||
      typeof timeRange.end.year !== 'number' ||
      typeof timeRange.end.month !== 'number' ||
      typeof timeRange.end.day !== 'number') {
      console.log("getTimestamps: timeRange properties are invalid");
      return {
        start: Date.now() - 3 * 60 * 60 * 1000, // 3 hours ago
        end: Date.now()
      };
    }

    // Create start date with fallback values for time components
    let startDate;
    try {
      startDate = new Date(
        timeRange.start.year,
        timeRange.start.month - 1,
        timeRange.start.day,
        'hour' in timeRange.start ? timeRange.start.hour : 0,
        'minute' in timeRange.start ? timeRange.start.minute : 0,
        'second' in timeRange.start ? timeRange.start.second : 0
      );

      // Validate that the date is valid
      if (isNaN(startDate.getTime())) {
        throw new Error("Invalid start date");
      }
    } catch (error) {
      console.error("Error creating start date:", error);
      startDate = new Date(Date.now() - 3 * 60 * 60 * 1000); // 3 hours ago
    }

    // Create end date with fallback values for time components
    let endDate;
    try {
      endDate = new Date(
        timeRange.end.year,
        timeRange.end.month - 1,
        timeRange.end.day,
        'hour' in timeRange.end ? timeRange.end.hour : 0,
        'minute' in timeRange.end ? timeRange.end.minute : 0,
        'second' in timeRange.end ? timeRange.end.second : 0
      );

      // Validate that the date is valid
      if (isNaN(endDate.getTime())) {
        throw new Error("Invalid end date");
      }
    } catch (error) {
      console.error("Error creating end date:", error);
      endDate = new Date(); // Current time
    }

    return {
      start: startDate.getTime(),
      end: endDate.getTime()
    };
  } catch (error) {
    console.error("Critical error in getTimestamps:", error);
    // Return a safe fallback (last 3 hours)
    return {
      start: Date.now() - 3 * 60 * 60 * 1000,
      end: Date.now()
    };
  }
}

// Reactive SQL preview that properly updates when filters change
const sqlPreviewState = computed(() => {
  const defaultState = { sql: '', isValid: true, error: undefined as string | undefined };

  // Basic validations
  if (!exploreStore?.data?.sourceId ||
    !selectedSourceDetails?.value?.database ||
    !selectedSourceDetails?.value?.table) {
    return defaultState;
  }

  try {
    // The key fix: reference filterConditions in the computed dependency chain
    // This ensures the computed value recalculates when filterConditions change
    const filterConditions = exploreStore.data.filterConditions || [];
    
    // One-time SQL generation for preview
    const timestamps = getTimestamps();
    const generator = useSqlGenerator({
      database: selectedSourceDetails.value.database || '',
      table: selectedSourceDetails.value.table || '',
      start_timestamp: timestamps.start,
      end_timestamp: timestamps.end,
      limit: exploreStore.data.limit || 100,
      timestamp_field: 'timestamp'
    });

    if (!generator) return defaultState;

    // Generate SQL with the current filter conditions
    generator.generatePreviewSql(filterConditions);

    return {
      sql: generator.previewSql?.sql || '',
      isValid: generator.previewSql?.isValid ?? true,
      error: generator.previewSql?.error
    };
  } catch (error) {
    console.error("SQL preview error:", error);
    return defaultState;
  }
});

// Ultra-simplified: Generate SQL directly
function generateSql() {
  try {
    // Basic validation
    if (!selectedSourceDetails?.value?.database ||
      !exploreStore?.data?.sourceId) {
      return;
    }

    // Create a simple generator just for this operation
    const timestamps = getTimestamps();
    const generator = useSqlGenerator({
      database: selectedSourceDetails.value.database || '',
      table: selectedSourceDetails.value.table || '',
      start_timestamp: timestamps.start,
      end_timestamp: timestamps.end,
      limit: exploreStore.data.limit || 100,
      timestamp_field: 'timestamp'
    });

    // Generate SQL and update store
    const sql = generator.generateQuerySql(exploreStore.data.filterConditions || []);
    if (sql.isValid) {
      exploreStore.setRawSql(sql.sql);
    }
  } catch (error) {
    console.error("SQL generation error:", error);
  }
}

// Initialize state from URL parameters
function initializeFromURL() {
  // Set team from URL
  if (route.query.team && typeof route.query.team === 'string') {
    teamsStore.setCurrentTeam(parseInt(route.query.team));
  }

  // Set source from URL
  if (route.query.source && typeof route.query.source === 'string') {
    exploreStore.setSource(parseInt(route.query.source));
  }

  // Set limit from URL or use default
  if (route.query.limit && typeof route.query.limit === 'string') {
    exploreStore.setLimit(parseInt(route.query.limit))
  } else {
    exploreStore.setLimit(100) // Default limit
  }

  // Set time range from URL or use default (now - 3h)
  const currentTime = now(getLocalTimeZone())
  const defaultStart = currentTime.subtract({ hours: 3 })
  const defaultEnd = currentTime

  if (
    route.query.start_time &&
    route.query.end_time &&
    typeof route.query.start_time === 'string' &&
    typeof route.query.end_time === 'string'
  ) {
    try {
      const startValue = parseInt(route.query.start_time)
      const endValue = parseInt(route.query.end_time)

      const startDate = new Date(startValue)
      const endDate = new Date(endValue)

      const start = new CalendarDateTime(
        startDate.getFullYear(),
        startDate.getMonth() + 1,
        startDate.getDate(),
        startDate.getHours(),
        startDate.getMinutes(),
        startDate.getSeconds()
      )

      const end = new CalendarDateTime(
        endDate.getFullYear(),
        endDate.getMonth() + 1,
        endDate.getDate(),
        endDate.getHours(),
        endDate.getMinutes(),
        endDate.getSeconds()
      )

      exploreStore.setTimeRange({
        start,
        end
      })
    } catch (e) {
      console.error('Failed to parse time range from URL', e)
      // Fall back to default time range
      exploreStore.setTimeRange({
        start: defaultStart,
        end: defaultEnd
      })
    }
  } else {
    // No time range in URL, set default
    exploreStore.setTimeRange({
      start: defaultStart,
      end: defaultEnd
    })
  }

  // Set raw SQL from URL
  if (route.query.query && typeof route.query.query === 'string') {
    exploreStore.setRawSql(decodeURIComponent(route.query.query))
  }
}

// Update URL with current state
function updateURL() {
  if (isInitialLoad.value) {
    isInitialLoad.value = false
    return
  }

  const timestamps = getTimestamps()
  const query: Record<string, string> = {
    team: teamsStore.currentTeamId ? teamsStore.currentTeamId.toString() : '',
    source: exploreStore.data.sourceId ? exploreStore.data.sourceId.toString() : '',
    limit: exploreStore.data.limit.toString(),
    start_time: timestamps.start.toString(),
    end_time: timestamps.end.toString()
  }

  if (exploreStore.data.rawSql.trim()) {
    query.query = encodeURIComponent(exploreStore.data.rawSql.trim())
  }

  router.replace({ query })
}

// lifecycle handler with improvements
onMounted(() => {
  console.log("LogExplorer mounted");
});

// Simple cleanup - let child components handle their own editor cleanup
onBeforeUnmount(() => {
  // Only log in development
  if (process.env.NODE_ENV !== 'production') {
    console.log("LogExplorer unmounted");
  }

  try {
    // Just reset state variables - no Monaco cleanup as that's handled by child components
    showSqlTab.value = false;
    previewOpen.value = false;
  } catch (error) {
    console.error("Error during LogExplorer cleanup:", error);
  }
});

onMounted(async () => {
  try {
    console.log("LogExplorer component mounting");

    // Load teams first
    await teamsStore.loadTeams()

    // Load sources with team information next
    await sourcesStore.loadUserSources()

    // Now initialize from URL after data is loaded
    initializeFromURL()

    // Auto-select first team if none is selected
    if (!teamsStore.currentTeamId && teamsStore.teams.length > 0) {
      teamsStore.currentTeamId = teamsStore.teams[0].id
      await nextTick()
    }

    // Auto-select first source if none is selected and we have sources
    if (!exploreStore.data.sourceId && sourcesStore.teamSources.length > 0) {
      exploreStore.setSource(sourcesStore.teamSources[0].id)
      await nextTick()
    }

    // If we have a source selected, load its saved queries for the current team
    if (exploreStore.data.sourceId && teamsStore.currentTeamId) {
      await savedQueriesStore.fetchSourceQueries(
        exploreStore.data.sourceId,
        teamsStore.currentTeamId
      )

      // Register the source's severity field for coloring
      const currentSource = sourcesStore.sourcesWithTeams.find(s => s.id === exploreStore.data.sourceId)
      if (currentSource && 'MetaSeverityField' in currentSource) {
        registerSeverityField(currentSource.MetaSeverityField as string)
      } else if (currentSource && '_meta_severity_field' in currentSource) {
        registerSeverityField((currentSource as any)._meta_severity_field as string)
      }
    }

    // Check if we have a query_id in the URL
    if (route.query.query_id && typeof route.query.query_id === "string") {
      // Try to fetch user teams first to establish team context
      if (!teamsStore.currentTeamId) {
        await savedQueriesStore.fetchUserTeams()
      }

      if (teamsStore.currentTeamId || savedQueriesStore.data.selectedTeamId) {
        // Then try to load the saved query
        await loadSavedQuery(route.query.query_id)
      } else {
        toast({
          title: 'Warning',
          description: 'Could not load saved query: No team context available',
          variant: 'destructive',
          duration: TOAST_DURATION.ERROR,
        })
      }
    } else {
      // Update the URL to ensure it's set correctly
      updateURL()

      // If we have all the necessary parameters, execute the query
      if (exploreStore.canExecuteQuery) {
        await executeQuery()
      }
    }
  } catch (error) {
    console.error('Error initializing LogExplorer:', error)
    toast({
      title: 'Error',
      description: getErrorMessage(error),
      variant: 'destructive',
      duration: TOAST_DURATION.ERROR,
    })
  }

  // Mark initial load as complete
  isInitialLoad.value = false
})

// Create debounced versions of update functions to prevent rapid firing

// One simple debounced function to generate SQL
const updateSql = useDebounceFn(() => {
  try {
    generateSql();
  } catch (error) {
    console.error("Error updating SQL:", error);
  }
}, 250);

// Simple watch for filter conditions
watch(
  () => exploreStore.data?.filterConditions,
  () => updateSql(),
  { deep: true, immediate: false }
);

// Simple watch for source changes to update SQL and register severity field
watch(
  () => exploreStore.data?.sourceId,
  (newVal) => {
    if (newVal) {
      updateSql();

      // Register severity field if available
      try {
        const currentSource = sourcesStore.sourcesWithTeams?.find(s => s?.id === newVal);
        if (currentSource && 'MetaSeverityField' in currentSource) {
          registerSeverityField(currentSource.MetaSeverityField as string);
        } else if (currentSource && '_meta_severity_field' in currentSource) {
          registerSeverityField((currentSource as any)._meta_severity_field as string);
        }
      } catch (error) {
        console.error("Error registering severity field:", error);
      }
    }
  },
  { immediate: false }
);

// Simple watch for time range changes
watch(
  () => exploreStore.data?.timeRange,
  () => updateSql(),
  { deep: true, immediate: false }
);

// Simple watch for limit changes
watch(
  () => exploreStore.data?.limit,
  () => updateSql(),
  { immediate: false }
);

// Simple query execution function
async function executeQuery() {
  if (!exploreStore.data?.sourceId || !selectedSourceDetails?.value?.database) {
    toast({
      title: 'Error',
      description: 'Please select a valid source before executing query',
      variant: 'destructive',
      duration: TOAST_DURATION.ERROR,
    })
    return
  }

  try {
    let rawSql: string

    // Either use the filters to generate SQL or use the raw SQL
    if (!showSqlTab.value) {
      // Create a temporary generator to get SQL from filters
      const timestamps = getTimestamps();
      const generator = useSqlGenerator({
        database: selectedSourceDetails.value.database || '',
        table: selectedSourceDetails.value.table || '',
        start_timestamp: timestamps.start,
        end_timestamp: timestamps.end,
        limit: exploreStore.data.limit || 100,
        timestamp_field: 'timestamp'
      });

      // Generate SQL
      const queryState = generator.generateQuerySql(exploreStore.data.filterConditions || []);

      if (!queryState.isValid) {
        throw new Error(queryState.error || 'Invalid SQL query');
      }

      rawSql = queryState.sql;
    } else {
      // Use raw SQL directly
      rawSql = exploreStore.data.rawSql;
    }

    // Execute query
    await exploreStore.executeQuery(rawSql);

    // Update URL after search
    if (!isInitialLoad.value) {
      updateURL();
    }
    isInitialLoad.value = false;
  } catch (error) {
    toast({
      title: 'Error',
      description: getErrorMessage(error),
      variant: 'destructive',
      duration: TOAST_DURATION.ERROR,
    })
  }
}

const tableColumns = ref<ColumnDef<Record<string, any>>[]>([])

watch(
  () => exploreStore.data.columns,
  (newColumns) => {
    if (newColumns) {
      tableColumns.value = createColumns(newColumns)
    }
  },
  { immediate: true }
)

// Handle filter updates
const handleFiltersUpdate = (filters: FilterCondition[]) => {
  exploreStore.setFilterConditions(filters)
}

// Handle loading saved query
async function loadSavedQuery(queryId: string) {
  try {
    // First get team context
    const teamId = savedQueriesStore.data.selectedTeamId
    if (!teamId) {
      // Try to load teams first
      await savedQueriesStore.fetchUserTeams()
      if (!savedQueriesStore.data.selectedTeamId) {
        toast({
          title: 'Error',
          description: 'No team context available',
          variant: 'destructive',
          duration: TOAST_DURATION.ERROR,
        })
        return
      }
    }

    // Fetch the query
    const queryResponse = await savedQueriesStore.fetchQuery(
      savedQueriesStore.data.selectedTeamId || 0,
      queryId
    )

    if (queryResponse.success && queryResponse.data) {
      try {
        const query = queryResponse.data
        // Parse the query content
        const content = JSON.parse(query.query_content)

        // Load the query state into the store
        if (content.sourceId) exploreStore.setSource(content.sourceId)

        // Load time range
        if (content.timeRange) {
          const startDate = new Date(content.timeRange.absolute.start)
          const endDate = new Date(content.timeRange.absolute.end)

          const start = new CalendarDateTime(
            startDate.getFullYear(),
            startDate.getMonth() + 1,
            startDate.getDate(),
            startDate.getHours(),
            startDate.getMinutes(),
            startDate.getSeconds()
          )

          const end = new CalendarDateTime(
            endDate.getFullYear(),
            endDate.getMonth() + 1,
            endDate.getDate(),
            endDate.getHours(),
            endDate.getMinutes(),
            endDate.getSeconds()
          )

          exploreStore.setTimeRange({
            start,
            end
          })
        }

        // Load limit
        if (content.limit) exploreStore.setLimit(content.limit)

        // Load raw SQL
        if (content.rawSql) exploreStore.setRawSql(content.rawSql)

        // Set active tab
        activeTab.value = content.activeTab || 'filters'

        // Update URL
        updateURL()

        // Execute the query
        await executeQuery()

        toast({
          title: 'Success',
          description: `Loaded query "${query.name}"`,
          duration: TOAST_DURATION.SUCCESS,
        })
      } catch (parseError) {
        toast({
          title: 'Error',
          description: `Failed to parse query content: ${getErrorMessage(parseError)}`,
          variant: 'destructive',
          duration: TOAST_DURATION.ERROR,
        })
      }
    }
  } catch (error) {
    toast({
      title: 'Error',
      description: `Failed to load saved query: ${getErrorMessage(error)}`,
      variant: 'destructive',
      duration: TOAST_DURATION.ERROR,
    })
  }
}

// Handle showing save query modal
function showSaveQueryForm() {
  try {
    // Serialize the current query state
    const serialized = serializeQueryState(exploreStore.data)
    queryToSave.value = JSON.stringify(serialized)
    showSaveQueryModal.value = true
  } catch (error) {
    toast({
      title: 'Error',
      description: `Failed to prepare query for saving: ${getErrorMessage(error)}`,
      variant: 'destructive',
      duration: TOAST_DURATION.ERROR,
    })
  }
}

// Handle team selection change
async function handleTeamChange(teamId: string) {
  try {
    // Set the current team directly
    teamsStore.currentTeamId = parseInt(teamId);

    // Wait for reactivity to update teamSources
    await nextTick();

    // Check if sources are available for this team
    if (sourcesStore.teamSources && sourcesStore.teamSources.length > 0) {
      // Reset source selection if current source doesn't belong to new team
      const sourceExists = sourcesStore.teamSources.some(s => s.id === exploreStore.data.sourceId);
      if (!sourceExists) {
        // Auto-select first source if current source not in team
        exploreStore.setSource(sourcesStore.teamSources[0].id);
      }
    } else {
      // No sources for this team
      exploreStore.setSource(0);
      console.log(`No sources available for team ${teamId}`);
    }

    // Update URL
    updateURL();
  } catch (error) {
    console.error('Error changing team:', error);
    toast({
      title: 'Error',
      description: `Failed to change team: ${getErrorMessage(error)}`,
      variant: 'destructive',
      duration: TOAST_DURATION.ERROR,
    });
  }
}

// Handle save query submission with specific typing
async function handleSaveQuery(formData: any) {
  try {
    // Convert to the expected type
    const queryRequest: CreateTeamQueryRequest = {
      team_id: formData.team_id ? parseInt(formData.team_id) : (teamsStore.currentTeamId || 0),
      name: formData.name,
      description: formData.description,
      query_content: formData.query_content
    };

    // Use the source-centric API to create the query
    const result = await sourcesStore.createSourceQuery(
      exploreStore.data.sourceId.toString(),
      queryRequest
    );

    if (result.success && result.data) {
      toast({
        title: 'Success',
        description: 'Query saved successfully',
        duration: TOAST_DURATION.SUCCESS,
      });

      // Update URL with query_id
      const timestamps = getTimestamps();
      const params = new URLSearchParams(route.query as Record<string, string>);

      // Safely access id from data
      params.set('query_id', result.data.id.toString());
      params.set('team', teamsStore.currentTeamId ? teamsStore.currentTeamId.toString() : '');
      params.set('source', exploreStore.data.sourceId.toString());
      params.set('limit', exploreStore.data.limit.toString());
      params.set('start_time', timestamps.start.toString());
      params.set('end_time', timestamps.end.toString());

      if (exploreStore.data.rawSql) {
        params.set('query', encodeURIComponent(exploreStore.data.rawSql.trim()));
      } else {
        params.delete('query');
      }

      router.replace({ query: Object.fromEntries(params.entries()) });
    }

    showSaveQueryModal.value = false;
  } catch (error) {
    toast({
      title: 'Error',
      description: `Failed to save query: ${getErrorMessage(error)}`,
      variant: 'destructive',
      duration: TOAST_DURATION.ERROR,
    });
  }
}
</script>

<template>
  <!-- Show loading state -->
  <div v-if="showLoadingState" class="flex flex-col items-center justify-center h-[calc(100vh-12rem)] gap-4">
    <div class="space-y-4 p-4 animate-pulse">
      <div class="flex space-x-2">
        <Skeleton class="h-4 w-32" />
      </div>
      <div class="space-y-2">
        <Skeleton class="h-4 w-48" />
        <Skeleton class="h-4 w-40" />
      </div>
    </div>
  </div>

  <!-- Show empty state when no sources are available -->
  <div v-else-if="showEmptyState" class="flex flex-col items-center justify-center h-[calc(100vh-12rem)] gap-4">
    <div class="text-center space-y-2">
      <h2 class="text-2xl font-semibold tracking-tight">No Sources Found</h2>
      <p class="text-muted-foreground">You need to configure a log source before you can explore logs.</p>
    </div>
    <Button @click="router.push({ name: 'NewSource' })">
      <Plus class="mr-2 h-4 w-4" />
      Add Your First Source
    </Button>
  </div>

  <!-- Main explorer UI when sources are available -->
  <div v-else class="flex flex-col h-full min-w-0 gap-3">
    <!-- Updated Control Bar - Team First -->
    <div class="flex items-center gap-2 h-9 mb-1">
      <!-- Team Selector -->
      <div class="w-[220px]">
        <Select :model-value="teamsStore.currentTeamId ? teamsStore.currentTeamId.toString() : ''"
          @update:model-value="handleTeamChange" class="h-7">
          <SelectTrigger class="h-7 py-0 text-xs">
            <SelectValue placeholder="Select a team">
              {{ selectedTeamName }}
            </SelectValue>
          </SelectTrigger>
          <SelectContent>
            <SelectItem v-for="team in teamsStore.teams" :key="team.id" :value="team.id.toString()">
              {{ team.name }}
            </SelectItem>
          </SelectContent>
        </Select>
      </div>

      <!-- Source Selector (filtered by team) -->
      <div class="w-[220px]">
        <Select :model-value="exploreStore.data.sourceId ? exploreStore.data.sourceId.toString() : ''"
          @update:model-value="(val) => exploreStore.setSource(parseInt(val))" class="h-7"
          :disabled="!teamsStore.currentTeamId || (sourcesStore.teamSources || []).length === 0">
          <SelectTrigger class="h-7 py-0 text-xs">
            <SelectValue placeholder="Select a source">
              {{ selectedSourceName }}
            </SelectValue>
          </SelectTrigger>
          <SelectContent>
            <SelectItem v-for="source in sourcesStore.teamSources || []" :key="source.id" :value="source.id.toString()">
              {{ formatSourceName(source) }}
            </SelectItem>
          </SelectContent>
        </Select>
      </div>

      <!-- Divider -->
      <div class="h-5 w-px bg-border self-center"></div>

      <!-- Time Range Picker - Increased width -->
      <div class="w-[340px]">
        <DateTimePicker v-model="dateRange" class="h-7" buttonClass="py-0 h-7 text-xs" />
      </div>

      <!-- Spacer -->
      <div class="flex-1"></div>

      <!-- Saved Queries Dropdown (now with team context) -->
      <div class="w-[180px]">
        <SavedQueriesDropdown :selected-team-id="teamsStore.currentTeamId || undefined" @select="loadSavedQuery"
          @save="showSaveQueryForm" />
      </div>

      <!-- Limit Control - Changed to dropdown -->
      <div class="flex items-center gap-1 mr-2">
        <span class="text-xs text-muted-foreground whitespace-nowrap">Limit:</span>
        <Select :model-value="exploreStore.data.limit.toString()"
          @update:model-value="(val: string) => exploreStore.setLimit(parseInt(val))" class="w-[80px]">
          <SelectTrigger class="h-7 py-0 text-xs">
            <SelectValue placeholder="Limit" />
          </SelectTrigger>
          <SelectContent>
            <SelectItem value="100">100</SelectItem>
            <SelectItem value="500">500</SelectItem>
            <SelectItem value="1000">1,000</SelectItem>
            <SelectItem value="5000">5,000</SelectItem>
            <SelectItem value="10000">10,000</SelectItem>
          </SelectContent>
        </Select>
      </div>

      <!-- Search Button -->
      <Button @click="executeQuery"
        :disabled="isLoading || !selectedSourceDetails.database || !selectedSourceDetails.table"
        class="h-7 px-3 py-0 text-xs">
        <Search class="mr-2 h-3 w-3" />
        Search
      </Button>
    </div>

    <!-- Modern Query Builder with Streamlined UI -->
    <div class="rounded-md border bg-card">
      <div class="flex flex-col">
        <!-- Sleek Toggle Switch Header -->
        <div class="flex items-center px-4 py-2 border-b">
          <div class="flex-1">
            <h3 class="text-sm font-medium flex items-center gap-1.5">
              <svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 20 20" fill="currentColor"
                class="w-4 h-4 text-primary">
                <path d="M14 6H6v8h8V6z" />
                <path fill-rule="evenodd"
                  d="M9.25 3V1.75a.75.75 0 011.5 0V3h1.5v-.25a.75.75 0 011.5 0V3h.5A2.75 2.75 0 0117 5.75v.5h1.25a.75.75 0 010 1.5H17v1.5h.25a.75.75 0 010 1.5H17v1.5h.25a.75.75 0 010 1.5H17v.5A2.75 2.75 0 0114.25 17h-.5v1.25a.75.75 0 01-1.5 0V17h-1.5v.25a.75.75 0 01-1.5 0V17h-1.5v.25a.75.75 0 01-1.5 0V17h-.5A2.75 2.75 0 013 14.25v-.5H1.75a.75.75 0 010-1.5H3v-1.5h-.25a.75.75 0 010-1.5H3v-1.5h-.25a.75.75 0 010-1.5H3v-.5A2.75 2.75 0 015.75 3h.5V1.75a.75.75 0 011.5 0V3h1.5zM4.5 5.75c0-.69.56-1.25 1.25-1.25h8.5c.69 0 1.25.56 1.25 1.25v8.5c0 .69-.56 1.25-1.25 1.25h-8.5c-.69 0-1.25-.56-1.25-1.25v-8.5z"
                  clip-rule="evenodd" />
              </svg>
              Query Editor
            </h3>
          </div>
          <div class="relative rounded-full bg-muted p-0.5 flex shadow-sm">
            <button @click="showSqlTab = false"
              class="relative flex items-center gap-1.5 px-3 py-1.5 text-xs rounded-full transition-colors duration-200 focus:outline-none"
              :class="!showSqlTab
                ? 'bg-primary text-primary-foreground shadow-sm'
                : 'text-muted-foreground hover:text-foreground'">
              <span class="relative z-10 flex items-center gap-1.5">
                <svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 20 20" fill="currentColor" class="w-3.5 h-3.5">
                  <path fill-rule="evenodd"
                    d="M2.628 1.601C5.028 1.206 7.49 1 10 1s4.973.206 7.372.601a.75.75 0 01.628.74v2.288a2.25 2.25 0 01-.659 1.59l-4.682 4.683a2.25 2.25 0 00-.659 1.59v3.037c0 .684-.31 1.33-.844 1.757l-1.937 1.55A.75.75 0 018 18.25v-5.757a2.25 2.25 0 00-.659-1.591L2.659 6.22A2.25 2.25 0 012 4.629V2.34a.75.75 0 01.628-.74z"
                    clip-rule="evenodd" />
                </svg>
                Visual Filter
              </span>
            </button>
            <button @click="showSqlTab = true"
              class="relative flex items-center gap-1.5 px-3 py-1.5 text-xs rounded-full transition-colors duration-200 focus:outline-none"
              :class="showSqlTab
                ? 'bg-primary text-primary-foreground shadow-sm'
                : 'text-muted-foreground hover:text-foreground'">
              <span class="relative z-10 flex items-center gap-1.5">
                <svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 20 20" fill="currentColor" class="w-3.5 h-3.5">
                  <path fill-rule="evenodd"
                    d="M4.25 2A2.25 2.25 0 002 4.25v11.5A2.25 2.25 0 004.25 18h11.5A2.25 2.25 0 0018 15.75V4.25A2.25 2.25 0 0015.75 2H4.25zm4.03 6.28a.75.75 0 00-1.06-1.06L4.97 9.47a.75.75 0 000 1.06l2.25 2.25a.75.75 0 001.06-1.06L6.56 10l1.72-1.72zm4.5-1.06a.75.75 0 10-1.06 1.06L13.44 10l-1.72 1.72a.75.75 0 101.06 1.06l2.25-2.25a.75.75 0 000-1.06l-2.25-2.25z"
                    clip-rule="evenodd" />
                </svg>
                SQL Query
              </span>
            </button>
          </div>
        </div>

        <!-- Content Area -->
        <div class="p-4">
          <!-- Basic tab content - Visual Filter or SQL Editor -->
          <div v-if="!showSqlTab" class="space-y-3">
            <!-- Visual Filter Builder -->
            <SmartFilterBar v-model="exploreStore.data.filterConditions" :columns="exploreStore.data.columns"
              class="border rounded-md bg-card shadow-inner p-2" />

            <!-- SQL Preview -->
            <div class="mt-4">
              <button @click="previewOpen = !previewOpen"
                class="inline-flex items-center gap-1.5 text-xs font-medium text-primary hover:text-primary/80 transition-colors">
                <svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 20 20" fill="currentColor"
                  class="w-4 h-4 transition-transform duration-200" :class="{ 'rotate-90': previewOpen }">
                  <path fill-rule="evenodd"
                    d="M7.21 14.77a.75.75 0 01.02-1.06L11.168 10 7.23 6.29a.75.75 0 111.04-1.08l4.5 4.25a.75.75 0 010 1.08l-4.5 4.25a.75.75 0 01-1.06-.02z"
                    clip-rule="evenodd" />
                </svg>
                Preview Generated SQL
              </button>

              <transition name="fade">
                <div v-show="previewOpen" class="mt-2">
                  <div class="border rounded-md bg-muted/20 p-3 overflow-hidden">
                    <SqlPreview :sql="(sqlPreviewState as any).sql || ''" :is-valid="!!(sqlPreviewState as any).isValid"
                      :error="(sqlPreviewState as any).error" />
                  </div>
                </div>
              </transition>
            </div>
          </div>

          <!-- SQL Query Editor - Simple version -->
          <div v-else class="border rounded-md bg-card shadow-inner">
            <Suspense>
              <template #default>
                <SQLEditor v-model="exploreStore.data.rawSql" :source-database="selectedSourceDetails?.database || ''"
                  :source-table="selectedSourceDetails?.table || ''" :start-timestamp="getTimestamps().start"
                  :end-timestamp="getTimestamps().end" :source-columns="exploreStore.data.columns || []"
                  @execute="executeQuery" />
              </template>
              <template #fallback>
                <div class="p-4 flex items-center justify-center h-48">
                  <div class="animate-pulse flex space-x-4">
                    <div class="flex-1 space-y-4 py-1">
                      <div class="h-4 bg-muted/50 rounded w-3/4"></div>
                      <div class="space-y-2">
                        <div class="h-4 bg-muted/50 rounded"></div>
                        <div class="h-4 bg-muted/50 rounded w-5/6"></div>
                      </div>
                    </div>
                  </div>
                </div>
              </template>
            </Suspense>
          </div>
        </div>
      </div>
    </div>

    <!-- Results Area -->
    <div class="flex-1 min-h-0">
      <!-- Show Skeleton when loading -->
      <div v-if="isLoading && !sourcesStore.sourcesWithTeams.length" class="flex-1 flex justify-center items-center">
        <div class="flex flex-col items-center gap-4 text-center">
          <Skeleton class="h-12 w-12 rounded-full" />
          <div class="space-y-2">
            <Skeleton class="h-4 w-[250px]" />
            <Skeleton class="h-4 w-[200px]" />
          </div>
        </div>
      </div>
      <div v-else-if="isLoading" class="border rounded-md bg-card p-3 space-y-3">
        <!-- Simulate Table Header Skeleton -->
        <div class="flex gap-3">
          <Skeleton class="h-4 w-1/4" />
          <Skeleton class="h-4 w-1/4" />
          <Skeleton class="h-4 w-1/4" />
          <Skeleton class="h-4 w-1/4" />
        </div>
        <!-- Simulate Table Rows Skeleton -->
        <div class="space-y-2">
          <div v-for="n in 5" :key="n" class="flex gap-3">
            <Skeleton class="h-4 w-1/4" />
            <Skeleton class="h-4 w-1/4" />
            <Skeleton class="h-4 w-1/4" />
            <Skeleton class="h-4 w-1/4" />
          </div>
        </div>
      </div>
      <!-- Show Results when data is loaded -->
      <div v-else-if="exploreStore.data.logs?.length > 0" class="border rounded-md bg-card">
        <DataTable :columns="tableColumns" :data="exploreStore.data.logs" :stats="exploreStore.data.queryStats"
          :source-id="exploreStore.data.sourceId.toString()" />
      </div>
      <EmptyState v-else class="border rounded-md bg-card" />
    </div>

    <!-- Save Query Modal (now with team context) -->
    <SaveQueryModal v-if="showSaveQueryModal" :is-open="showSaveQueryModal" :query-content="queryToSave"
      @close="showSaveQueryModal = false" @save="handleSaveQuery" />
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
  transition: opacity 0.2s ease-in-out;
}

.fade-enter-from,
.fade-leave-to {
  opacity: 0;
}
</style>
