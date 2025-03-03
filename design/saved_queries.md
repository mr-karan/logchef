# Saved Queries Implementation Specification

**Author**: System
**Date**: June 2023
**Status**: Draft

## Overview

This document outlines the implementation plan for adding a Saved Queries feature to LogChef. This feature will allow users to save, manage, and share query configurations within their teams, improving collaboration and efficiency for common log analysis tasks.

## Requirements

1. Users should be able to save their current query configuration
2. Saved queries should be associated with teams for shared access
3. Users should be able to load, edit, and delete saved queries
4. When loaded, a saved query should restore the exact state of the explorer
5. The URL should be updated to reflect the loaded query state
6. Saved queries should be accessible via a convenient UI component

## Data Model

We'll leverage the existing `team_queries` table in SQLite that has the following structure:

```sql
CREATE TABLE team_queries (
    id TEXT PRIMARY KEY,
    team_id TEXT NOT NULL,
    name TEXT NOT NULL,
    description TEXT,
    query_content TEXT NOT NULL, -- Will store JSON representation of query state
    created_at TEXT NOT NULL,
    updated_at TEXT NOT NULL,
    FOREIGN KEY (team_id) REFERENCES teams(id) ON DELETE CASCADE
);
```

The `query_content` field will store a JSON object with the following structure:

```json
{
  "version": 1,
  "activeTab": "filters|raw_sql", // Which tab should be active when loaded
  "sourceId": "source-uuid",
  "timeRange": {
    "absolute": {
      "start": 1740801563000, // Unix timestamp in ms
      "end": 1740812363000 // Unix timestamp in ms
    }
  },
  "limit": 100,
  "rawSql": "SELECT * FROM logs.events WHERE..."
}
```

## API Endpoints

We'll implement the following API endpoints:

### List Team Queries

- **Path**: `GET /api/teams/:teamId/queries`
- **Auth**: Requires team membership
- **Response**: Array of query objects with metadata (excluding full content)

### Get Query Detail

- **Path**: `GET /api/teams/:teamId/queries/:queryId`
- **Auth**: Requires team membership
- **Response**: Complete query object including content

### Create Query

- **Path**: `POST /api/teams/:teamId/queries`
- **Auth**: Requires team membership
- **Request Body**: Query metadata and content
- **Response**: Created query object with ID

### Update Query

- **Path**: `PUT /api/teams/:teamId/queries/:queryId`
- **Auth**: Requires team membership
- **Request Body**: Updated query metadata and content
- **Response**: Updated query object

### Delete Query

- **Path**: `DELETE /api/teams/:teamId/queries/:queryId`
- **Auth**: Requires team membership
- **Response**: Success status

## Frontend Changes

### New Components

1. **SavedQueriesDropdown.vue**

   - Displays a list of saved queries for quick access
   - Shows options to save current query
   - Located in the control bar of LogExplorer.vue

2. **SaveQueryModal.vue**

   - Modal form for saving/updating a query
   - Fields: name, description, team selection

3. **SavedQueriesView.vue**
   - Dedicated view for listing and managing saved queries for a specific team
   - Includes team selection dropdown at the top
   - Displays a table of saved queries with:
     - Query name (hyperlinked to open in new explore page)
     - Description
     - Created at timestamp
     - Updated at timestamp
   - Options for edit, delete, duplicate, etc.

### Store Changes

1. **Create new savedQueries store module**

```typescript
// frontend/src/stores/savedQueries.ts
import { defineStore } from "pinia";
import { useBaseStore, handleApiCall } from "./base";
import { savedQueriesApi } from "@/api/savedQueries";

export interface SavedQuery {
  id: string;
  team_id: string;
  name: string;
  description: string;
  query_content: string; // JSON string of actual query configuration
  created_at: string;
  updated_at: string;
}

export interface SavedQueryContent {
  version: number;
  activeTab: "filters" | "raw_sql";
  sourceId: string;
  timeRange: {
    absolute: {
      start: number;
      end: number;
    };
  };
  limit: number;
  rawSql: string;
}

export interface SavedQueriesState {
  queries: SavedQuery[];
  selectedQuery: SavedQuery | null;
  teams: Array<{ id: string; name: string }>;
  selectedTeamId: string | null;
}

export const useSavedQueriesStore = defineStore("savedQueries", () => {
  // Initialize base store with default state
  const state = useBaseStore<SavedQueriesState>({
    queries: [],
    selectedQuery: null,
    teams: [],
    selectedTeamId: null,
  });

  // Actions
  async function fetchUserTeams() {
    return await state.withLoading(async () => {
      const result = await handleApiCall<Array<{ id: string; name: string }>>({
        apiCall: () => savedQueriesApi.getUserTeams(),
        onSuccess: (response) => {
          state.data.value.teams = response;
          if (response.length > 0 && !state.data.value.selectedTeamId) {
            state.data.value.selectedTeamId = response[0].id;
          }
        },
      });
      return result;
    });
  }

  function setSelectedTeam(teamId: string) {
    state.data.value.selectedTeamId = teamId;
  }

  async function fetchTeamQueries(teamId: string) {
    return await state.withLoading(async () => {
      const result = await handleApiCall<SavedQuery[]>({
        apiCall: () => savedQueriesApi.listQueries(teamId),
        onSuccess: (response) => {
          state.data.value.queries = response;
        },
      });
      return result;
    });
  }

  async function fetchQuery(teamId: string, queryId: string) {
    return await state.withLoading(async () => {
      const result = await handleApiCall<SavedQuery>({
        apiCall: () => savedQueriesApi.getQuery(teamId, queryId),
        onSuccess: (response) => {
          state.data.value.selectedQuery = response;
        },
      });
      return result;
    });
  }

  async function createQuery(
    teamId: string,
    query: Omit<SavedQuery, "id" | "created_at" | "updated_at">
  ) {
    return await state.withLoading(async () => {
      const result = await handleApiCall<SavedQuery>({
        apiCall: () => savedQueriesApi.createQuery(teamId, query),
        onSuccess: (response) => {
          state.data.value.queries.unshift(response);
          state.data.value.selectedQuery = response;
        },
      });
      return result;
    });
  }

  async function updateQuery(
    teamId: string,
    queryId: string,
    query: Partial<SavedQuery>
  ) {
    return await state.withLoading(async () => {
      const result = await handleApiCall<SavedQuery>({
        apiCall: () => savedQueriesApi.updateQuery(teamId, queryId, query),
        onSuccess: (response) => {
          const index = state.data.value.queries.findIndex(
            (q) => q.id === queryId
          );
          if (index >= 0) {
            state.data.value.queries[index] = response;
          }
          if (state.data.value.selectedQuery?.id === queryId) {
            state.data.value.selectedQuery = response;
          }
        },
      });
      return result;
    });
  }

  async function deleteQuery(teamId: string, queryId: string) {
    return await state.withLoading(async () => {
      const result = await handleApiCall<{ success: boolean }>({
        apiCall: () => savedQueriesApi.deleteQuery(teamId, queryId),
        onSuccess: () => {
          state.data.value.queries = state.data.value.queries.filter(
            (q) => q.id !== queryId
          );
          if (state.data.value.selectedQuery?.id === queryId) {
            state.data.value.selectedQuery = null;
          }
        },
      });
      return result;
    });
  }

  return {
    ...state,
    fetchUserTeams,
    setSelectedTeam,
    fetchTeamQueries,
    fetchQuery,
    createQuery,
    updateQuery,
    deleteQuery,
  };
});
```

2. **Add serialization utilities**

```typescript
// frontend/src/utils/querySerializer.ts
import type { ExploreState } from "@/stores/explore";
import type { SavedQueryContent } from "@/stores/savedQueries";
import { CalendarDateTime } from "@internationalized/date";

export function serializeQueryState(state: ExploreState): SavedQueryContent {
  const timeRange = state.timeRange;

  return {
    version: 1,
    activeTab: state.rawSql ? "raw_sql" : "filters",
    sourceId: state.sourceId,
    timeRange: {
      absolute: {
        start: timeRange?.start
          ? new Date(timeRange.start.toString()).getTime()
          : 0,
        end: timeRange?.end ? new Date(timeRange.end.toString()).getTime() : 0,
      },
    },
    limit: state.limit,
    rawSql: state.rawSql,
  };
}

export function deserializeQueryContent(
  content: SavedQueryContent
): Partial<ExploreState> {
  // Convert timestamps back to DateValue objects
  const startDate = new Date(content.timeRange.absolute.start);
  const endDate = new Date(content.timeRange.absolute.end);

  // Create DateValue objects using the CalendarDateTime constructor
  const startDateValue = new CalendarDateTime(
    startDate.getFullYear(),
    startDate.getMonth() + 1,
    startDate.getDate(),
    startDate.getHours(),
    startDate.getMinutes(),
    startDate.getSeconds()
  );

  const endDateValue = new CalendarDateTime(
    endDate.getFullYear(),
    endDate.getMonth() + 1,
    endDate.getDate(),
    endDate.getHours(),
    endDate.getMinutes(),
    endDate.getSeconds()
  );

  return {
    sourceId: content.sourceId,
    limit: content.limit,
    timeRange: {
      start: startDateValue,
      end: endDateValue,
    },
    rawSql: content.rawSql,
  };
}

// Helper to properly encode query parameters for URLs
export function generateQueryURL(
  queryId: string,
  sourceId: string,
  limit: number,
  start: number,
  end: number
): string {
  const params = new URLSearchParams();
  params.set("query_id", queryId);
  params.set("source", sourceId);
  params.set("limit", limit.toString());
  params.set("start_time", start.toString());
  params.set("end_time", end.toString());

  return `/logs/explore?${params.toString()}`;
}
```

3. **Update explore store with saved query functions**

```typescript
// In frontend/src/stores/explore.ts
import {
  serializeQueryState,
  deserializeQueryContent,
} from "@/utils/querySerializer";
import type { SavedQueryContent } from "@/stores/savedQueries";

// Add these functions to the explore store
function serializeCurrentState(): SavedQueryContent {
  return serializeQueryState(state.data.value);
}

function loadSavedQuery(queryContent: string) {
  try {
    const content = JSON.parse(queryContent) as SavedQueryContent;
    const newState = deserializeQueryContent(content);

    // Update state
    if (newState.sourceId) setSource(newState.sourceId);
    if (newState.timeRange) setTimeRange(newState.timeRange);
    if (newState.limit) setLimit(newState.limit);
    if (newState.rawSql) setRawSql(newState.rawSql);

    // Set active tab
    return content.activeTab || "filters";
  } catch (error) {
    console.error("Error parsing saved query:", error);
    throw new Error("Failed to load saved query");
  }
}

// Add these functions to the returned object
return {
  // ... existing functions
  serializeCurrentState,
  loadSavedQuery,
};
```

### URL Handling

1. **Update LogExplorer.vue to handle query_id parameter**

```typescript
// In LogExplorer.vue onMounted
// Add after the existing initialization code
if (route.query.query_id && typeof route.query.query_id === "string") {
  try {
    // First get team context (assuming we have a teamsStore)
    const teamId = teamsStore.currentTeam?.id;
    if (!teamId) throw new Error("No team context available");

    // Then fetch and load the saved query
    const savedQuery = await savedQueriesStore.fetchQuery(
      teamId,
      route.query.query_id
    );
    if (savedQuery) {
      const activeTab = exploreStore.loadSavedQuery(savedQuery.query_content);
      if (activeTab) {
        this.activeTab = activeTab;
      }
      // Execute the query after loading the saved state
      await executeQuery();
    }
  } catch (error) {
    toast({
      title: "Error",
      description: `Failed to load saved query: ${getErrorMessage(error)}`,
      variant: "destructive",
      duration: TOAST_DURATION.ERROR,
    });
  }
}
```

2. **Update URL when loading a saved query**

```typescript
// Add to LogExplorer.vue
function updateURLWithQueryId(queryId: string) {
  // Generate a properly formatted URL with all necessary parameters
  const timestamps = getTimestamps();
  const params = new URLSearchParams(route.query as Record<string, string>);

  params.set("query_id", queryId);
  params.set("source", exploreStore.data.sourceId);
  params.set("limit", exploreStore.data.limit.toString());
  params.set("start_time", timestamps.start.toString());
  params.set("end_time", timestamps.end.toString());

  if (exploreStore.data.rawSql) {
    params.set("query", encodeURIComponent(exploreStore.data.rawSql.trim()));
  } else {
    params.delete("query");
  }

  router.replace({ query: Object.fromEntries(params.entries()) });
}

// Call this when a saved query is loaded from the dropdown
```

## UI Integration

### New SavedQueriesView.vue

Create a dedicated page for browsing and managing saved queries:

````vue
<script setup lang="ts">
import { ref, onMounted, computed, watch } from 'vue';
import { useRouter } from 'vue-router';
import { formatDistance } from 'date-fns';
import { ChevronDown, Eye, Pencil, Trash2 } from 'lucide-vue-next';
import {
  DropdownMenu,
  DropdownMenuContent,
  DropdownMenuItem,
  DropdownMenuTrigger,
} from '@/components/ui/dropdown-menu';
import { Button } from '@/components/ui/button';
import { useToast } from '@/components/ui/toast';
import { useConfirm } from '@/composables/useConfirm';
import {
  Table,
  TableBody,
  TableCaption,
  TableCell,
  TableHead,
  TableHeader,
  TableRow,
} from '@/components/ui/table';
import { Select, SelectContent, SelectItem, SelectTrigger, SelectValue } from '@/components/ui/select';
import { useSavedQueriesStore } from '@/stores/savedQueries';
import { generateQueryURL } from '@/utils/querySerializer';
import { TOAST_DURATION } from '@/lib/constants';
import SaveQueryModal from '@/components/saved-queries/SaveQueryModal.vue';
import { getErrorMessage } from '@/api/types';

const router = useRouter();
const { toast } = useToast();
const { confirm } = useConfirm();
const savedQueriesStore = useSavedQueriesStore();

// Local UI state
const isLoading = computed(() => savedQueriesStore.isLoading);
const showSaveQueryModal = ref(false);
const editingQuery = ref(null);

// Load user's teams and queries
onMounted(async () => {
  try {
    await savedQueriesStore.fetchUserTeams();
    if (savedQueriesStore.data.selectedTeamId) {
      await loadTeamQueries(savedQueriesStore.data.selectedTeamId);
    }
  } catch (error) {
    toast({
      title: 'Error',
      description: getErrorMessage(error),
      variant: 'destructive',
      duration: TOAST_DURATION.ERROR,
    });
  }
});

// Watch for team selection changes
watch(
  () => savedQueriesStore.data.selectedTeamId,
  async (newTeamId) => {
    if (newTeamId) {
      await loadTeamQueries(newTeamId);
    }
  }
);

async function loadTeamQueries(teamId: string) {
  try {
    await savedQueriesStore.fetchTeamQueries(teamId);
  } catch (error) {
    toast({
      title: 'Error',
      description: getErrorMessage(error),
      variant: 'destructive',
      duration: TOAST_DURATION.ERROR,
    });
  }
}

// Format relative time
function formatTime(dateStr: string): string {
  try {
    return formatDistance(new Date(dateStr), new Date(), { addSuffix: true });
  } catch (error) {
    return dateStr;
  }
}

// Generate URL for a saved query
function getQueryUrl(query: any): string {
  try {
    const content = JSON.parse(query.query_content);
    return generateQueryURL(
      query.id,
      content.sourceId,
      content.limit,
      content.timeRange.absolute.start,
      content.timeRange.absolute.end
    );
  } catch (error) {
    console.error('Error generating query URL:', error);
    return `/logs/explore?query_id=${query.id}`;
  }
}

// Handle opening query in explorer
function openQuery(query: any) {
  const url = getQueryUrl(query);
  window.open(url, '_blank');
}

// Handle edit query
function editQuery(query: any) {
  editingQuery.value = query;
  showSaveQueryModal.value = true;
}

// Handle delete query
async function deleteQuery(query: any) {
  const confirmed = await confirm({
    title: 'Delete Query',
    description: `Are you sure you want to delete "${query.name}"? This action cannot be undone.`,
    confirmText: 'Delete',
    cancelText: 'Cancel',
  });

  if (confirmed) {
    try {
      await savedQueriesStore.deleteQuery(query.team_id, query.id);
      toast({
        title: 'Success',
        description: 'Query deleted successfully',
        duration: TOAST_DURATION.SUCCESS,
      });
    } catch (error) {
      toast({
        title: 'Error',
        description: getErrorMessage(error),
        variant: 'destructive',
        duration: TOAST_DURATION.ERROR,
      });
    }
  }
}

// Handle save query modal submission
async function handleSaveQuery(formData: any) {
  try {
    if (editingQuery.value) {
      await savedQueriesStore.updateQuery(formData.team_id, editingQuery.value.id, {
        name: formData.name,
        description: formData.description,
      });
      toast({
        title: 'Success',
        description: 'Query updated successfully',
        duration: TOAST_DURATION.SUCCESS,
      });
    }
    showSaveQueryModal.value = false;
    editingQuery.value = null;
  } catch (error) {
    toast({
      title: 'Error',
      description: getErrorMessage(error),
      variant: 'destructive',
      duration: TOAST_DURATION.ERROR,
    });
  }
}
</script>

<template>
  <div class="container py-6 space-y-6">
    <div class="flex justify-between items-center">
      <h1 class="text-2xl font-bold tracking-tight">Saved Queries</h1>
    </div>

    <!-- Team selector -->
    <div class="flex items-center space-x-4">
      <span class="font-medium">Team:</span>
      <Select
        v-model="savedQueriesStore.data.selectedTeamId"
        :disabled="isLoading || savedQueriesStore.data.teams.length === 0"
        class="w-[250px]"
      >
        <SelectTrigger>
          <SelectValue placeholder="Select a team" />
        </SelectTrigger>
        <SelectContent>
          <SelectItem v-for="team in savedQueriesStore.data.teams" :key="team.id" :value="team.id">
            {{ team.name }}
          </SelectItem>
        </SelectContent>
      </Select>
    </div>

    <!-- Loading state -->
    <div v-if="isLoading" class="flex justify-center items-center py-8">
      <p class="text-muted-foreground">Loading saved queries...</p>
    </div>

    <!-- Empty state -->
    <div
      v-else-if="!savedQueriesStore.data.queries.length"
      class="flex flex-col justify-center items-center py-12 space-y-4"
    >
      <p class="text-xl text-muted-foreground">No saved queries found</p>
      <p class="text-muted-foreground">
        Create a query in the Explorer and save it to access it here.
      </p>
      <Button @click="router.push('/logs/explore')">Go to Explorer</Button>
    </div>

    <!-- Queries table -->
    <Table v-else>
      <TableCaption>A list of your team's saved queries.</TableCaption>
      <TableHeader>
        <TableRow>
          <TableHead class="w-[300px]">Name</TableHead>
          <TableHead>Description</TableHead>
          <TableHead class="w-[180px]">Created</TableHead>
          <TableHead class="w-[180px]">Updated</TableHead>
          <TableHead class="w-[100px] text-right">Actions</TableHead>
        </TableRow>
      </TableHeader>
      <TableBody>
        <TableRow v-for="query in savedQueriesStore.data.queries" :key="query.id">
          <TableCell class="font-medium">
            <a
              @click.prevent="openQuery(query)"
              :href="getQueryUrl(query)"
              class="text-primary hover:underline cursor-pointer"
            >
              {{ query.name }}
            </a>
          </TableCell>
          <TableCell>{{ query.description || 'No description' }}</TableCell>
          <TableCell>{{ formatTime(query.created_at) }}</TableCell>
          <TableCell>{{ formatTime(query.updated_at) }}</TableCell>
          <TableCell class="text-right">
            <DropdownMenu>
              <DropdownMenuTrigger asChild>
                <Button variant="ghost" size="icon">
                  <ChevronDown class="h-4 w-4" />
                </Button>
              </DropdownMenuTrigger>
              <DropdownMenuContent align="end">
                <DropdownMenuItem @click="openQuery(query)">
                  <Eye class="mr-2 h-4 w-4" />
                  Open
                </DropdownMenuItem>
                <DropdownMenuItem @click="editQuery(query)">
                  <Pencil class="mr-2 h-4 w-4" />
                  Edit
                </DropdownMenuItem>
                <DropdownMenuItem @click="deleteQuery(query)" class="text-destructive">
                  <Trash2 class="mr-2 h-4 w-4" />
                  Delete
                </DropdownMenuItem>
              </DropdownMenuContent>
            </DropdownMenu>
          </TableCell>
        </TableRow>
      </TableBody>
    </Table>

    <!-- Edit query modal -->
    <SaveQueryModal
      v-if="showSaveQueryModal && editingQuery"
      :is-open="showSaveQueryModal"
      :initial-data="editingQuery"
      :is-edit-mode="true"
      @close="showSaveQueryModal = false"
      @save="handleSaveQuery"
    />
  </div>
</template>

### LogExplorer.vue Changes

Update the control bar to include the saved queries dropdown:

```vue
<!-- In the control bar of LogExplorer.vue -->
<div class="flex items-center gap-2 h-9 mb-1">
  <!-- Existing source selector -->
  <!-- ... -->

  <!-- Add saved queries dropdown after source selector -->
  <div class="w-[180px]">
    <SavedQueriesDropdown
      @select="loadSavedQuery"
      @save="showSaveQueryModal = true"
    />
  </div>

  <!-- Existing time range picker -->
  <!-- ... -->
</div>

<!-- Add save query modal -->
<SaveQueryModal
  v-if="showSaveQueryModal"
  :is-open="showSaveQueryModal"
  :initial-data="queryToSave"
  @close="showSaveQueryModal = false"
  @save="handleSaveQuery"
/>
````

## Implementation Plan

### Backend Tasks

1. Add SQL queries for team queries in `internal/sqlite/queries.sql` (already exists)
2. Create Go structs for team queries in `internal/models/team_query.go`
3. Implement query repository in `internal/repository/team_query_repository.go`
4. Implement query service in `internal/services/team_query_service.go`
5. Create API handlers in `internal/api/team_query_handlers.go`
6. Add routes to `internal/api/routes.go`
7. Add integration tests

### Frontend Tasks

1. Create API client for saved queries in `frontend/src/api/savedQueries.ts`
2. Implement saved queries store in `frontend/src/stores/savedQueries.ts`
3. Create serialization utilities in `frontend/src/utils/querySerializer.ts`
4. Update explore store with saved query functions
5. Create UI components:
   - SavedQueriesDropdown.vue
   - SaveQueryModal.vue
   - SavedQueriesView.vue
6. Add route for saved queries view at `/queries` or `/teams/:teamId/queries`
7. Modify LogExplorer.vue to integrate the saved queries dropdown
8. Update URL handling to support query_id parameter
9. Add unit tests

## Testing Strategy

1. **Unit Tests**:

   - Test serialization/deserialization functions
   - Test store actions in isolation
   - Test UI components with mocked data

2. **Integration Tests**:

   - Test API endpoints with mock database
   - Test end-to-end flow of saving and loading queries

3. **Manual Testing Scenarios**:
   - Save a query and verify it loads correctly
   - Verify URL parameters are updated correctly
   - Test with various time ranges
   - Verify team access controls work as expected
   - Verify the team selection dropdown works as expected

## Future Enhancements

1. **Favorites/Pinned Queries**: Allow users to mark frequently used queries as favorites
2. **Query Categories/Tags**: Add ability to categorize or tag queries for easier organization
3. **Query Sharing**: Generate shareable links to queries (with auth requirements)
4. **Query Versioning**: Track changes to queries over time
5. **Query Scheduling**: Allow queries to be run on a schedule with notifications/alerts
6. **Query Templates**: Create reusable query templates with parameterized values

## Conclusion

This implementation plan provides a comprehensive approach to adding Saved Queries functionality to LogChef. By leveraging the existing team-based structure, we can create a collaborative environment where users can save, share, and reuse their log analysis queries, significantly improving workflow efficiency.
