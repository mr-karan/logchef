<script setup lang="ts">
import { ref, onMounted, onBeforeUnmount, watch, computed, nextTick } from 'vue'
import { ChevronsUpDown, X } from 'lucide-vue-next'
import { Collapsible, CollapsibleContent, CollapsibleTrigger } from '@/components/ui/collapsible'
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
const activeTab = ref('filters')
const activeQueryTab = ref('filter') // 'filter' or 'sql'

// Flag to prevent unnecessary URL updates on initial load
const isInitialLoad = ref(true)

// Controls collapsible query editor section
const isQueryEditorOpen = ref(true)

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
function getTimestamps() {
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
  if (import.meta.env.MODE !== 'production') {
    console.log("LogExplorer unmounted");
  }

  try {
    // Just reset state variables - no Monaco cleanup as that's handled by child components
    activeQueryTab.value = 'filter';
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
/**
 * Apply generated SQL from filter view to the SQL editor
 * This enables seamless transition from filter to SQL editing
 */
/**
 * Switch to SQL editing mode and apply the currently generated SQL
 */
const switchToSqlMode = () => {
  try {
    // Force SQL generation to ensure we have the latest SQL
    console.log("[LogExplorer] Switching to SQL mode - forcing SQL generation");

    // Generate SQL directly
    const timestamps = getTimestamps();
    const generator = useSqlGenerator({
      database: selectedSourceDetails.value.database || '',
      table: selectedSourceDetails.value.table || '',
      start_timestamp: timestamps.start,
      end_timestamp: timestamps.end,
      limit: exploreStore.data.limit || 100,
      timestamp_field: 'timestamp'
    });

    // Get the filter conditions
    const filterConditions = exploreStore.data.filterConditions || [];
    console.log(`[LogExplorer] Generating SQL for ${filterConditions.length} conditions`);

    // Generate SQL directly
    const queryState = generator.generateQuerySql(filterConditions);

    // Set it as raw SQL, with fallback to basic query if empty
    if (queryState.isValid && queryState.sql) {
      console.log(`[LogExplorer] Generated valid SQL: ${queryState.sql.slice(0, 50)}...`);
      exploreStore.setRawSql(queryState.sql);
    } else {
      console.warn(`[LogExplorer] Generated invalid SQL or empty SQL: ${queryState.error || 'No error info'}`);
      // Use a basic query as fallback
      const fallbackSql = `SELECT *
FROM ${selectedSourceDetails.value.database || 'logs'}.${selectedSourceDetails.value.table || 'logs'}
WHERE timestamp >= toDateTime64('${new Date(timestamps.start).toISOString().replace('T', ' ').replace('Z', '')}', 3)
  AND timestamp <= toDateTime64('${new Date(timestamps.end).toISOString().replace('T', ' ').replace('Z', '')}', 3)
ORDER BY timestamp DESC
LIMIT ${exploreStore.data.limit || 100}`;
      exploreStore.setRawSql(fallbackSql);
    }

    // Switch to SQL tab
    activeQueryTab.value = 'sql';

    // Success toast
    toast({
      title: 'Switched to SQL Mode',
      description: 'Filter conditions converted to SQL query',
      duration: TOAST_DURATION.SUCCESS,
    });
  } catch (error) {
    console.error("Error switching to SQL mode:", error);
    toast({
      title: 'Error',
      description: 'Failed to convert filter to SQL. Check console for details.',
      variant: 'destructive',
      duration: TOAST_DURATION.ERROR,
    });
  }
};

// Watch for tab changes to maintain state consistency between filter and SQL modes
watch(
  () => activeQueryTab.value,
  (newTab, oldTab) => {
    if (newTab === 'sql') {
      // Generate SQL when switching to SQL mode
      generateSql();
    } else if (newTab === 'filter' && oldTab === 'sql') {
      // When switching back to filter from SQL, ensure filter conditions are preserved
      // by forcing a re-evaluation of the filter conditions

      // Ensure filter conditions are updated after tab switch
      // by making a copy of the array to trigger reactivity
      const currentFilters = [...(exploreStore.data.filterConditions || [])];

      // Force update by setting the same filters again, but as a new array reference
      // This will trigger the watcher in SmartFilterBar to update the UI
      nextTick(() => {
        exploreStore.setFilterConditions(currentFilters);
        console.log('[LogExplorer] Preserving filters after tab switch:', currentFilters.length);
      });
    }
  }
);

const updateSql = useDebounceFn(() => {
  try {
    generateSql();
  } catch (error) {
    console.error("Error updating SQL:", error);
  }
}, 250);

// Watch for filter changes with debouncing to prevent excessive SQL generation
watch(
  () => exploreStore.data?.filterConditions,
  (newFilters, oldFilters) => {
    const newLen = newFilters?.length || 0;
    const oldLen = oldFilters?.length || 0;

    // Only log changes when actual filters change (not just references)
    if (newLen !== oldLen || JSON.stringify(newFilters) !== JSON.stringify(oldFilters)) {
      console.log(`[LogExplorer] Filter conditions changed: ${newLen} filters`);
    }

    // Only update SQL if we have actual filters to use
    if (newLen > 0 || (oldLen > 0 && newLen === 0)) {
      updateSql();
    }
  },
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

// Function to trigger field suggestions in the SmartFilterBar - simplified approach
const insertFieldTrigger = () => {
  // Only trigger in filter mode
  if (activeQueryTab.value === 'filter') {
    // Find all editor instances
    const editors = document.querySelectorAll('.monaco-editor');
    if (editors && editors.length > 0) {
      // Focus the editor first (this will work for SmartFilterBar editor)
      const editor = editors[0];
      editor.click();

      // Instead of trying to trigger Alt+Space, we'll insert an '@' character
      // which is the trigger character for field suggestions in the editor
      setTimeout(() => {
        // Use the execCommand API which works reliably for inserting text
        document.execCommand('insertText', false, '@');
      }, 100);
    }
  }
};

// Insert a specific field name into the editor
const insertField = (fieldName: string) => {
  // Only allow inserting in filter mode
  if (activeQueryTab.value !== 'filter') {
    // Switch to filter mode if we're not already there
    activeQueryTab.value = 'filter';
    // Wait for the tab switch to complete
    setTimeout(() => {
      attemptFieldInsertion(fieldName);
    }, 100);
  } else {
    attemptFieldInsertion(fieldName);
  }
};

// Helper function to insert fields
const attemptFieldInsertion = (fieldName: string) => {
  // Find the SmartFilterBar component
  const filterBarComponent = document.querySelector('.smartfilter-monaco-container');
  if (filterBarComponent) {
    // Focus the editor
    filterBarComponent.dispatchEvent(new Event('click'));

    // Get the current value in the editor
    const currentContent = exploreStore.data.rawSql || '';

    // Calculate the best insertion - insert the field with appropriate operator
    setTimeout(() => {
      // Find the most recent text that would likely need a field
      const lastPart = currentContent.trim();

      // If empty or ends with operator, just insert field name
      if (!lastPart || lastPart.endsWith('AND') || lastPart.endsWith('OR') ||
        lastPart.endsWith('(') || lastPart.endsWith('=')) {
        // Just insert the field name with space after it
        document.execCommand('insertText', false, `${fieldName} `);
      } else {
        // Insert with equals operator as default
        document.execCommand('insertText', false, `${fieldName}=`);
      }
    }, 50);
  }
};

// Clear the current query based on active tab
const clearCurrentQuery = () => {
  if (activeQueryTab.value === 'filter') {
    // Clear filter conditions
    exploreStore.setFilterConditions([]);
  } else if (activeQueryTab.value === 'sql') {
    // Clear SQL query
    exploreStore.setRawSql('');
  }
};

// Function to apply example filter queries
const applyExample = (filterExpression: string) => {
  // Make sure we're in filter mode
  activeQueryTab.value = 'filter';

  try {
    // Create a simple filter condition from the expression
    // This is a simplified approach - in a real app, you might want to parse the expression
    // more carefully and create proper structured filter conditions

    // First ensure we are in filter mode
    nextTick(() => {
      // For now, we'll take the simple approach of inserting the text into the filter input
      // Find the editor element
      const filterBarComponent = document.querySelector('.smartfilter-monaco-container');

      if (filterBarComponent) {
        // Focus the editor
        filterBarComponent.dispatchEvent(new Event('click'));

        // Clear existing content first
        exploreStore.setFilterConditions([]);

        // Insert the filter expression
        setTimeout(() => {
          document.execCommand('insertText', false, filterExpression);

          // Optional: Auto-execute query after a delay
          setTimeout(() => {
            executeQuery();
          }, 300);
        }, 100);
      } else {
        console.warn('Filter editor not found for inserting example');

        // Fallback: Set a raw SQL query based on the filter expression
        const timestamps = getTimestamps();
        const database = selectedSourceDetails.value?.database || 'logs';
        const table = selectedSourceDetails.value?.table || 'logs';

        // Simple SQL generation
        const sql = `SELECT *
FROM ${database}.${table}
WHERE ${filterExpression}
  AND timestamp >= toDateTime64('${new Date(timestamps.start).toISOString().replace('T', ' ').replace('Z', '')}', 3)
  AND timestamp <= toDateTime64('${new Date(timestamps.end).toISOString().replace('T', ' ').replace('Z', '')}', 3)
ORDER BY timestamp DESC
LIMIT ${exploreStore.data.limit || 100}`;

        exploreStore.setRawSql(sql);
      }
    });

    // Show toast indicating example applied
    toast({
      title: 'Example Applied',
      description: `Filter expression "${filterExpression}" applied`,
      duration: TOAST_DURATION.SUCCESS,
    });
  } catch (error) {
    console.error('Error applying example filter:', error);
    toast({
      title: 'Error',
      description: `Failed to apply example: ${getErrorMessage(error)}`,
      variant: 'destructive',
      duration: TOAST_DURATION.ERROR,
    });
  }
};

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
    if (activeQueryTab.value === 'filter') {
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
        activeQueryTab.value = content.activeTab === 'sql' ? 'sql' : 'filter'

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
  <div v-else class="flex flex-col h-full min-w-0 gap-2">
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

    <!-- Collapsible Query Editor with Sidebar & Split Layout -->
    <Collapsible v-model:open="isQueryEditorOpen" class="rounded-md border bg-card">
      <div class="flex flex-col">
        <!-- Page section indicator with better context - also acts as collapsible trigger -->
        <CollapsibleTrigger asChild>
          <div
            class="flex items-center justify-between px-4 py-2 border-b border-border/50 bg-muted/30 cursor-pointer hover:bg-muted/40 transition-colors">
            <div class="flex items-center gap-1.5">
              <svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 20 20" fill="currentColor"
                class="w-3.5 h-3.5 text-primary">
                <path
                  d="M5 4a1 1 0 00-2 0v7.268a2 2 0 000 3.464V16a1 1 0 102 0v-1.268a2 2 0 000-3.464V4zM11 4a1 1 0 10-2 0v1.268a2 2 0 000 3.464V16a1 1 0 102 0V8.732a2 2 0 000-3.464V4zM16 3a1 1 0 011 1v7.268a2 2 0 010 3.464V16a1 1 0 11-2 0v-1.268a2 2 0 010-3.464V4a1 1 0 011-1z" />
              </svg>
              <span class="text-sm font-medium">Query Editor</span>
            </div>

            <div class="flex items-center gap-2">
              <!-- Status indicator for query performance -->
              <div v-if="exploreStore.data.queryStats" class="flex items-center gap-3 text-xs text-muted-foreground">
                <div class="flex items-center gap-1">
                  <span class="w-2 h-2 rounded-full bg-green-500"></span>
                  <span>{{ exploreStore.data.queryStats.execution_time_ms }}ms</span>
                </div>
                <div class="flex items-center gap-1">
                  <span class="w-2 h-2 rounded-full bg-blue-500"></span>
                  <span>{{ exploreStore.data.queryStats.rows_read?.toLocaleString() || 0 }} rows scanned</span>
                </div>
              </div>

              <!-- Toggle icon for collapsible -->
              <ChevronsUpDown class="h-4 w-4 text-muted-foreground transition-transform duration-200"
                :class="{ 'transform rotate-180': isQueryEditorOpen }" />
            </div>
          </div>
        </CollapsibleTrigger>

        <!-- Collapsible content with split-view editor -->
        <CollapsibleContent>
          <!-- Split-view advanced query editor with sidebar -->
          <div class="flex">
            <!-- Left sidebar for fields and metadata -->
            <div class="w-56 border-r border-border/40 max-h-[350px] overflow-auto">
              <div class="p-2">
                <!-- Tabs for sidebar sections -->
                <Tabs defaultValue="fields" class="w-full">
                  <TabsList class="w-full h-7 grid grid-cols-3">
                    <TabsTrigger value="fields" class="text-xs">Fields</TabsTrigger>
                    <TabsTrigger value="examples" class="text-xs">Examples</TabsTrigger>
                    <TabsTrigger value="history" class="text-xs">Recent</TabsTrigger>
                  </TabsList>

                  <!-- Fields Tab Content - Closed by default, opens on demand -->
                  <TabsContent value="fields" class="mt-1.5 max-h-[290px] overflow-y-auto">
                    <div class="space-y-1">
                      <div class="text-xs text-muted-foreground mb-1.5 px-0.5">Click to insert field:</div>
                      <div v-if="!exploreStore.data.columns || exploreStore.data.columns.length === 0"
                        class="text-xs text-muted-foreground p-2 italic text-center">
                        No fields available yet.<br>Run a query to see fields.
                      </div>
                      <div v-else class="space-y-0.5">
                        <button v-for="col in exploreStore.data.columns" :key="col.name" @click="insertField(col.name)"
                          class="flex items-center justify-between w-full p-1.5 rounded-sm text-left text-xs hover:bg-muted transition-colors">
                          <div class="flex items-center gap-1 truncate">
                            <!-- Icon based on field type -->
                            <svg v-if="col.type.includes('Int') || col.type.includes('Float')"
                              xmlns="http://www.w3.org/2000/svg" viewBox="0 0 20 20" fill="currentColor"
                              class="w-3 h-3 text-amber-500">
                              <path fill-rule="evenodd" d="M9.5 2a7.5 7.5 0 107.5 7.5A7.5 7.5 0 009.5 2z" />
                            </svg>
                            <svg v-else-if="col.type.includes('String')" xmlns="http://www.w3.org/2000/svg"
                              viewBox="0 0 20 20" fill="currentColor" class="w-3 h-3 text-green-500">
                              <path
                                d="M10 2a.75.75 0 01.75.75v5.59l1.95-2.1a.75.75 0 111.1 1.02l-3.25 3.5a.75.75 0 01-1.1 0L6.2 7.26a.75.75 0 111.1-1.02l1.95 2.1V2.75A.75.75 0 0110 2z" />
                            </svg>
                            <svg v-else-if="col.type.includes('DateTime')" xmlns="http://www.w3.org/2000/svg"
                              viewBox="0 0 20 20" fill="currentColor" class="w-3 h-3 text-blue-500">
                              <path fill-rule="evenodd"
                                d="M10 18a8 8 0 100-16 8 8 0 000 16zm.75-13a.75.75 0 00-1.5 0v5c0 .414.336.75.75.75h4a.75.75 0 000-1.5h-3.25V5z" />
                            </svg>
                            <svg v-else xmlns="http://www.w3.org/2000/svg" viewBox="0 0 20 20" fill="currentColor"
                              class="w-3 h-3 text-purple-500">
                              <path d="M14 6H6v8h8V6z" />
                            </svg>
                            <span class="truncate font-medium">{{ col.name }}</span>
                          </div>
                          <span class="text-[9px] text-muted-foreground opacity-70">{{ col.type.split('(')[0] }}</span>
                        </button>
                      </div>
                    </div>
                  </TabsContent>

                  <!-- Examples Tab Content -->
                  <TabsContent value="examples" class="mt-1.5 max-h-[290px] overflow-y-auto">
                    <div class="space-y-1">
                      <div class="text-xs text-muted-foreground mb-1.5 px-0.5">Example queries:</div>
                      <div class="space-y-1.5">
                        <button @click="applyExample('severity_text=\'ERROR\'')"
                          class="flex items-center w-full p-2 rounded-sm text-left text-xs hover:bg-muted transition-colors">
                          <div class="w-2 h-2 rounded-full bg-red-500 mr-2"></div>
                          <span>All errors</span>
                        </button>
                        <button @click="applyExample('severity_text=\'WARN\' OR severity_text=\'WARNING\'')"
                          class="flex items-center w-full p-2 rounded-sm text-left text-xs hover:bg-muted transition-colors">
                          <div class="w-2 h-2 rounded-full bg-amber-500 mr-2"></div>
                          <span>All warnings</span>
                        </button>
                        <button @click="applyExample('status_code>=500 AND status_code<600')"
                          class="flex items-center w-full p-2 rounded-sm text-left text-xs hover:bg-muted transition-colors">
                          <div class="w-2 h-2 rounded-full bg-red-500 mr-2"></div>
                          <span>Server errors (5xx)</span>
                        </button>
                        <button @click="applyExample('status_code>=400 AND status_code<500')"
                          class="flex items-center w-full p-2 rounded-sm text-left text-xs hover:bg-muted transition-colors">
                          <div class="w-2 h-2 rounded-full bg-amber-500 mr-2"></div>
                          <span>Client errors (4xx)</span>
                        </button>
                        <button @click="applyExample('(method=\'GET\' OR method=\'POST\') AND status_code>=400')"
                          class="flex items-center w-full p-2 rounded-sm text-left text-xs hover:bg-muted transition-colors">
                          <div class="w-2 h-2 rounded-full bg-blue-500 mr-2"></div>
                          <span>GET/POST with errors</span>
                        </button>
                        <button
                          @click="applyExample('message ~ \'exception\' OR message ~ \'error\' OR message ~ \'failed\'')"
                          class="flex items-center w-full p-2 rounded-sm text-left text-xs hover:bg-muted transition-colors">
                          <div class="w-2 h-2 rounded-full bg-purple-500 mr-2"></div>
                          <span>Exception messages</span>
                        </button>
                      </div>
                    </div>
                  </TabsContent>

                  <!-- History Tab Content -->
                  <TabsContent value="history" class="mt-1.5 max-h-[290px] overflow-y-auto">
                    <div class="text-xs text-muted-foreground p-2 italic text-center"
                      v-if="!savedQueriesStore.recentQueries?.length">
                      No recent queries.<br>Save a query to see it here.
                    </div>
                    <div v-else class="space-y-1.5">
                      <div class="text-xs text-muted-foreground mb-1.5 px-0.5">Recently used:</div>
                      <button v-for="(query, index) in savedQueriesStore.recentQueries?.slice(0, 5)" :key="index"
                        @click="loadSavedQuery(query.id)"
                        class="flex items-center w-full p-2 rounded-sm text-left text-xs hover:bg-muted transition-colors">
                        <svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 20 20" fill="currentColor"
                          class="w-3.5 h-3.5 text-primary/70 mr-1.5">
                          <path fill-rule="evenodd"
                            d="M10 2c-1.716 0-3.408.106-5.07.31C3.806 2.45 3 3.414 3 4.517V17.25a.75.75 0 001.075.676L10 15.082l5.925 2.844A.75.75 0 0017 17.25V4.517c0-1.103-.806-2.068-1.93-2.207A41.403 41.403 0 0010 2z" />
                        </svg>
                        <span class="truncate">{{ query.name }}</span>
                      </button>
                    </div>
                  </TabsContent>
                </Tabs>
              </div>
            </div>

            <!-- Right side - Main query editor and validation -->
            <div class="flex-1 flex flex-col">
              <!-- Main editor area -->
              <div class="flex-1">
                <!-- Integrated header with fixed filter/SQL tabs -->
                <div class="flex items-center justify-between px-3 py-1.5 border-b border-border/50 bg-muted/30">
                  <!-- Left side - Improved mode switcher -->
                  <div class="bg-muted inline-flex h-8 items-center justify-center rounded-md p-1">
                    <button @click="activeQueryTab = 'filter'"
                      class="inline-flex items-center justify-center whitespace-nowrap rounded-sm px-3 py-1 text-xs font-medium transition-all focus-visible:outline-none focus-visible:ring-1 focus-visible:ring-ring disabled:pointer-events-none disabled:opacity-50"
                      :class="activeQueryTab === 'filter' ? 'bg-background text-foreground shadow' : 'text-muted-foreground hover:bg-muted/60'">
                      <svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 20 20" fill="currentColor"
                        class="w-3.5 h-3.5 mr-1.5">
                        <path fill-rule="evenodd"
                          d="M2.628 1.601C5.028 1.206 7.49 1 10 1s4.973.206 7.372.601a.75.75 0 01.628.74v2.288a2.25 2.25 0 01-.659 1.59l-4.682 4.683a2.25 2.25 0 00-.659 1.59v3.037c0 .684-.31 1.33-.844 1.757l-1.937 1.55A.75.75 0 018 18.25v-5.757a2.25 2.25 0 00-.659-1.591L2.659 6.22A2.25 2.25 0 012 4.629V2.34a.75.75 0 01.628-.74z" />
                      </svg>
                      Filter Mode
                    </button>
                    <button @click="activeQueryTab = 'sql'"
                      class="inline-flex items-center justify-center whitespace-nowrap rounded-sm px-3 py-1 text-xs font-medium transition-all focus-visible:outline-none focus-visible:ring-1 focus-visible:ring-ring disabled:pointer-events-none disabled:opacity-50"
                      :class="activeQueryTab === 'sql' ? 'bg-background text-foreground shadow' : 'text-muted-foreground hover:bg-muted/60'">
                      <svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 20 20" fill="currentColor"
                        class="w-3.5 h-3.5 mr-1.5">
                        <path fill-rule="evenodd"
                          d="M4.25 2A2.25 2.25 0 002 4.25v11.5A2.25 2.25 0 004.25 18h11.5A2.25 2.25 0 0018 15.75V4.25A2.25 2.25 0 0015.75 2H4.25zm4.03 6.28a.75.75 0 00-1.06-1.06L4.97 9.47a.75.75 0 000 1.06l2.25 2.25a.75.75 0 001.06-1.06L6.56 10l1.72-1.72zm4.5-1.06a.75.75 0 10-1.06 1.06L13.44 10l-1.72 1.72a.75.75 0 101.06 1.06l2.25-2.25a.75.75 0 000-1.06l-2.25-2.25z" />
                      </svg>
                      SQL Mode
                    </button>
                  </div>

                  <!-- Right side - action buttons -->
                  <div class="flex items-center gap-2">
                    <!-- Fields button - only shows in filter mode -->
                    <Button v-show="activeQueryTab === 'filter'" @click="insertFieldTrigger" size="sm" variant="outline"
                      class="h-7 px-2.5 text-xs rounded-md flex items-center gap-1.5">
                      <svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 20 20" fill="currentColor"
                        class="w-3.5 h-3.5 text-primary">
                        <path
                          d="M10 3.75a2 2 0 10-4 0 2 2 0 004 0zM17.25 4.5a.75.75 0 000-1.5h-5.5a.75.75 0 000 1.5h5.5zM5 3.75a.75.75 0 01-.75.75h-1.5a.75.75 0 010-1.5h1.5a.75.75 0 01.75.75zM4.25 17a.75.75 0 000-1.5h-1.5a.75.75 0 000 1.5h1.5zM17.25 17a.75.75 0 000-1.5h-5.5a.75.75 0 000 1.5h5.5zM9 10a.75.75 0 01-.75.75h-5.5a.75.75 0 010-1.5h5.5A.75.75 0 019 10zM17.25 10.75a.75.75 0 000-1.5h-1.5a.75.75 0 000 1.5h1.5zM14 10a2 2 0 10-4 0 2 2 0 004 0zM10 16.25a2 2 0 10-4 0 2 2 0 004 0z" />
                      </svg>
                      Fields
                    </Button>

                    <!-- Clear button (clears current tab content) -->
                    <Button v-if="(activeQueryTab === 'filter' && exploreStore.data.filterConditions?.length > 0) ||
                      (activeQueryTab === 'sql' && exploreStore.data.rawSql?.trim())" @click="clearCurrentQuery"
                      size="sm" variant="ghost" class="h-7 px-2.5 text-xs rounded-md flex items-center gap-1.5">
                      <X class="w-3.5 h-3.5" />
                      Clear
                    </Button>

                    <!-- Execute button - always visible -->
                    <Button @click="executeQuery" size="sm" variant="secondary"
                      :disabled="isLoading || !selectedSourceDetails.database || !selectedSourceDetails.table"
                      class="h-7 px-2.5 text-xs rounded-md flex items-center gap-1.5">
                      <Search class="w-3.5 h-3.5" />
                      Run Query
                    </Button>
                  </div>
                </div>

                <!-- Content area with editors -->
                <div>
                  <!-- Visual Filter Mode -->
                  <div v-if="activeQueryTab === 'filter'" class="animate-in fade-in-50 duration-100">
                    <SmartFilterBar v-model="exploreStore.data.filterConditions" :columns="exploreStore.data.columns"
                      class="rounded-sm" compact-mode="true" />
                  </div>

                  <!-- SQL Editor Mode -->
                  <div v-else-if="activeQueryTab === 'sql'" class="animate-in fade-in-50 duration-100">
                    <Suspense>
                      <template #default>
                        <SQLEditor v-model="exploreStore.data.rawSql"
                          :source-database="selectedSourceDetails?.database || ''"
                          :source-table="selectedSourceDetails?.table || ''" :start-timestamp="getTimestamps().start"
                          :end-timestamp="getTimestamps().end" :source-columns="exploreStore.data.columns || []"
                          @execute="executeQuery" compact-mode="true" />
                      </template>
                      <template #fallback>
                        <div class="flex items-center justify-center h-28">
                          <div class="animate-pulse flex space-x-2">
                            <div class="flex-1 space-y-1.5 py-1">
                              <div class="h-2.5 bg-muted/50 rounded w-3/4"></div>
                              <div class="space-y-1">
                                <div class="h-2 bg-muted/50 rounded"></div>
                                <div class="h-2 bg-muted/50 rounded w-5/6"></div>
                              </div>
                            </div>
                          </div>
                        </div>
                      </template>
                    </Suspense>
                  </div>
                </div>
              </div>

              <!-- Enhanced footer with keyboard shortcuts and validation -->
              <div class="px-3 py-1.5 border-t border-border/50 bg-muted/20 flex justify-between items-center">
                <div class="flex items-center gap-3 text-xs text-muted-foreground">
                  <span class="flex items-center gap-1">
                    <kbd
                      class="px-1 py-0.5 bg-background/80 rounded border border-border/50 text-[10px] font-mono">Alt+Space</kbd>
                    <span>for suggestions</span>
                  </span>
                  <span class="flex items-center gap-1">
                    <kbd
                      class="px-1 py-0.5 bg-background/80 rounded border border-border/50 text-[10px] font-mono">Ctrl+Enter</kbd>
                    <span>to run query</span>
                  </span>
                </div>

                <!-- Status/validation area (shows errors or OK status) -->
                <div v-if="exploreStore.data.validationErrors?.length"
                  class="flex items-center gap-1 text-xs text-red-500">
                  <svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 20 20" fill="currentColor" class="w-3.5 h-3.5">
                    <path fill-rule="evenodd"
                      d="M18 10a8 8 0 11-16 0 8 8 0 0116 0zm-8-5a.75.75 0 01.75.75v4.5a.75.75 0 01-1.5 0v-4.5A.75.75 0 0110 5zm0 10a1 1 0 100-2 1 1 0 000 2z" />
                  </svg>
                  <span>{{ exploreStore.data.validationErrors[0] }}</span>
                </div>
                <div v-else-if="activeQueryTab === 'filter' && exploreStore.data.filterConditions?.length > 0"
                  class="text-xs text-green-600 flex items-center gap-1">
                  <svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 20 20" fill="currentColor" class="w-3.5 h-3.5">
                    <path fill-rule="evenodd"
                      d="M16.704 4.153a.75.75 0 01.143 1.052l-8 10.5a.75.75 0 01-1.127.075l-4.5-4.5a.75.75 0 011.06-1.06l3.894 3.893 7.48-9.817a.75.75 0 011.05-.143z" />
                  </svg>
                  <span>Query valid</span>
                </div>
              </div>
            </div>
          </div>
        </CollapsibleContent>
      </div>
    </Collapsible>

    <!-- Live preview of generated SQL (when in filter mode) -->
    <div v-if="activeQueryTab === 'filter' && exploreStore.data.filterConditions?.length > 0"
      class="mt-2 rounded-md border border-border/50 bg-muted/30 p-3 text-xs font-mono overflow-x-auto max-h-28">
      <div class="text-muted-foreground mb-1">Generated SQL:</div>
      <pre class="text-[11px] whitespace-pre-wrap">{{ exploreStore.data.rawSql }}</pre>
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
