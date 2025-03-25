<script setup lang="ts">
import { ref, onMounted, computed, watch } from 'vue';
import { useRouter } from 'vue-router';
import { ChevronDown, Eye, Pencil, Trash2, Loader2, Plus, Search } from 'lucide-vue-next';
import { formatDate } from '@/utils/format';
import {
  DropdownMenu,
  DropdownMenuContent,
  DropdownMenuItem,
  DropdownMenuTrigger,
} from '@/components/ui/dropdown-menu';
import { Button } from '@/components/ui/button';
import { useToast } from '@/components/ui/toast';
import { Skeleton } from '@/components/ui/skeleton';
import {
  Table,
  TableBody,
  TableCell,
  TableHead,
  TableHeader,
  TableRow,
} from '@/components/ui/table';
import { Select, SelectContent, SelectItem, SelectTrigger, SelectValue } from '@/components/ui/select';
import { Input } from '@/components/ui/input';
import { useSavedQueriesStore } from '@/stores/savedQueries';
import { TOAST_DURATION } from '@/lib/constants';
import SaveQueryModal from '@/components/saved-queries/SaveQueryModal.vue';
import { getErrorMessage } from '@/api/types';
import { useSourcesStore } from '@/stores/sources';
import { formatSourceName } from '@/utils/format';
import type { SavedTeamQuery } from '@/api/savedQueries';
import { useTeamsStore } from '@/stores/teams';
import { Badge } from '@/components/ui/badge';

// Initialize router and services with better error handling
const router = useRouter();
const { toast } = useToast();

// Initialize stores - ensure these are available before use
const savedQueriesStore = useSavedQueriesStore();
const sourcesStore = useSourcesStore();
const teamsStore = useTeamsStore();

// Simple check to ensure store is available
if (!teamsStore) {
  console.error("Teams store failed to initialize!");
}

// Local UI state
const isLoading = ref(false);
const showSaveQueryModal = ref(false);
const editingQuery = ref<SavedTeamQuery | null>(null);
const selectedSourceId = ref<string>('');
const searchQuery = ref('');
const queries = ref<SavedTeamQuery[]>([]);
const isChangingTeam = ref(false);
const isChangingSource = ref(false);
const urlError = ref<string | null>(null);
const isInitialLoad = ref(true); // Flag to prevent redundant API calls

// Loading and empty states
const showLoadingState = computed(() => {
  return sourcesStore.isLoading || isChangingTeam.value;
});

const showEmptyState = computed(() => {
  return !showLoadingState.value && (!sourcesStore.teamSources || sourcesStore.teamSources.length === 0);
});

// Selected team name
const selectedTeamName = computed(() => {
  return teamsStore?.currentTeam?.name || 'Select a team';
});

// Selected source name
const selectedSourceName = computed(() => {
  if (!selectedSourceId.value) return 'Select a source';
  const source = sourcesStore.teamSources?.find(s => s.id === parseInt(selectedSourceId.value));
  return source ? formatSourceName(source) : 'Select a source';
});

// Filtered queries based on search
const filteredQueries = computed(() => {
  if (!searchQuery.value.trim()) {
    return queries.value;
  }

  const search = searchQuery.value.toLowerCase();

  return queries.value.filter(query =>
    query.name.toLowerCase().includes(search) ||
    (query.description && query.description.toLowerCase().includes(search))
  );
});

// Check if we have any queries to display
const hasQueries = computed((): boolean => {
  return filteredQueries.value.length > 0;
});

// Total query count
const totalQueryCount = computed((): number => {
  return queries.value.length;
});

// Load sources and queries on mount
onMounted(async () => {
  try {
    // Load teams first
    await teamsStore.loadTeams();

    // Check if team ID is specified in the URL query
    const teamIdFromUrl = router.currentRoute.value.query.team;

    if (teamIdFromUrl) {
      // Set the team from URL parameter
      const teamId = parseInt(teamIdFromUrl as string);
      teamsStore.setCurrentTeam(teamId);
    }
    // Set default team if none specified in URL and none selected
    else if (!teamsStore.currentTeamId && teamsStore.teams.length > 0) {
      teamsStore.setCurrentTeam(teamsStore.teams[0].id);
    }

    // Load sources for the current team
    if (teamsStore.currentTeamId) {
      await sourcesStore.loadTeamSources(teamsStore.currentTeamId);
    } else {
      console.warn("No team selected, cannot load sources");
    }

    // Get the first source if available
    if (sourcesStore.teamSources.length > 0) {
      // Check if source ID is specified in the URL query
      const sourceIdFromUrl = router.currentRoute.value.query.source;

      if (sourceIdFromUrl && sourcesStore.teamSources.some(s => s.id === parseInt(sourceIdFromUrl as string))) {
        selectedSourceId.value = sourceIdFromUrl as string;
      } else {
        selectedSourceId.value = String(sourcesStore.teamSources[0].id);
      }

      // Don't explicitly call loadSourceQueries here, 
      // the watcher on selectedSourceId will handle it
    }
  } catch (error) {
    console.error("Error during SavedQueriesView mount:", error);
    urlError.value = "Error initializing the saved queries view. Please try refreshing the page.";
    toast({
      title: 'Error',
      description: getErrorMessage(error),
      variant: 'destructive',
      duration: TOAST_DURATION.ERROR,
    });
  }
});

// Watch for source selection changes and team changes
watch(
  [() => selectedSourceId.value, () => teamsStore.currentTeamId],
  async ([newSourceId]) => {
    if (newSourceId && teamsStore.currentTeamId) {
      await loadSourceQueries();
      // Mark initial load as complete after first load
      isInitialLoad.value = false;
    }
  },
  { immediate: true } // This will trigger immediately when component mounts
);

// Handle team change
async function handleTeamChange(teamId: string) {
  try {
    isChangingTeam.value = true;
    urlError.value = null;

    // Use the improved setCurrentTeam that handles string IDs
    teamsStore.setCurrentTeam(teamId);
    
    const currentTeamId = parseInt(teamId);

    // Update URL to reflect the team change
    router.replace({
      path: '/logs/saved',
      query: { team: teamId }
    });

    // Load sources for the selected team
    const sourcesResult = await sourcesStore.loadTeamSources(currentTeamId, true);

    // Handle case where team has no sources
    if (!sourcesResult.success || !sourcesResult.data || sourcesResult.data.length === 0) {
      // Clear source selection when team has no sources
      selectedSourceId.value = '';
      queries.value = [];
      return;
    }

    // Reset source selection to the first source from the new team
    if (sourcesStore.teamSources.length > 0) {
      selectedSourceId.value = String(sourcesStore.teamSources[0].id);

      // Update URL with the new source - use currentTeamId for safety
      router.replace({
        path: '/logs/saved',
        query: { 
          team: teamId, 
          source: selectedSourceId.value 
        }
      });

      await loadSourceQueries();
    } else {
      // No sources in this team
      selectedSourceId.value = '';
      queries.value = [];
    }

  } catch (error) {
    console.error('Error changing team:', error);
    urlError.value = 'Error changing team. Please try again.';
    toast({
      title: 'Error',
      description: getErrorMessage(error),
      variant: 'destructive',
      duration: TOAST_DURATION.ERROR,
    });
  } finally {
    isChangingTeam.value = false;
  }
}

// Handle source change
async function handleSourceChange(sourceId: string) {
  try {
    isChangingSource.value = true;
    urlError.value = null;

    if (!sourceId) {
      selectedSourceId.value = '';
      queries.value = [];

      // Update URL to remove source parameter
      router.replace({
        path: '/logs/saved',
        query: { team: teamsStore?.currentTeamId?.toString() || '' }
      });

      return;
    }

    const id = parseInt(sourceId);

    // Verify the source exists in the current team's sources
    const sourceExists = sourcesStore.teamSources.some(source => source.id === id);

    if (!sourceExists) {
      urlError.value = `Source with ID ${id} not found or not accessible by the selected team.`;
      // If invalid source, don't update the selection
      return;
    }

    selectedSourceId.value = sourceId;

    // Update URL with the new source
    router.replace({
      path: '/logs/saved',
      query: {
        team: teamsStore?.currentTeamId?.toString() || '',
        source: sourceId
      }
    });

    await loadSourceQueries();

  } catch (error) {
    console.error('Error changing source:', error);
    urlError.value = 'Error changing source. Please try again.';
    toast({
      title: 'Error',
      description: getErrorMessage(error),
      variant: 'destructive',
      duration: TOAST_DURATION.ERROR,
    });
  } finally {
    isChangingSource.value = false;
  }
}

async function loadSourceQueries() {
  try {
    isLoading.value = true;

    // Reset search when changing source
    searchQuery.value = '';

    // Safe check for teamsStore and currentTeamId
    const currentTeamId = teamsStore?.currentTeamId;
    
    if (!currentTeamId || !selectedSourceId.value) {
      console.warn("No team or source selected, cannot load queries");
      queries.value = [];
      return;
    }

    // Use the new store pattern with proper null handling
    const result = await savedQueriesStore.fetchTeamSourceQueries(
      currentTeamId,
      parseInt(selectedSourceId.value)
    );

    // Handle both success and error cases properly
    if (result.success) {
      // Directly use the result data which is already null-safe
      queries.value = result.data ?? [];
    } else {
      queries.value = [];
      if (result.error) {
        toast({
          title: 'Error',
          description: result.error.message,
          variant: 'destructive',
          duration: TOAST_DURATION.ERROR,
        });
      }
    }
  } catch (error) {
    queries.value = [];
    toast({
      title: 'Error',
      description: getErrorMessage(error),
      variant: 'destructive',
      duration: TOAST_DURATION.ERROR,
    });
  } finally {
    isLoading.value = false;
  }
}

// Format time using the formatDate utility
function formatTime(dateStr: string): string {
  return formatDate(dateStr);
}

// Generate URL for a saved query
function getQueryUrl(query: SavedTeamQuery): string {
  try {
    // Get query type from the saved query
    const queryType = query.query_type || 'sql';

    // Parse the query content
    const queryContent = JSON.parse(query.query_content);

    // Build the URL with the appropriate parameters
    let url = `/logs/explore?team=${query.team_id}&query_id=${query.id}`;

    // Add source ID if available
    if (query.source_id) {
      url += `&source=${query.source_id}`;
    }

    // Add mode parameter based on query type
    url += `&mode=${queryType === 'logchefql' ? 'logchefql' : 'sql'}`;

    // Add the appropriate query content based on type
    if (queryType === 'logchefql' && queryContent.logchefqlContent) {
      url += `&q=${encodeURIComponent(queryContent.logchefqlContent)}`;
    } else if (queryType === 'sql' && queryContent.rawSql) {
      url += `&q=${encodeURIComponent(queryContent.rawSql)}`;
    }

    return url;
  } catch (error) {
    console.error('Error generating query URL:', error);
    return `/logs/explore?team=${query.team_id}&query_id=${query.id}&source=${query.source_id}`;
  }
}

// Handle opening query in explorer
function openQuery(query: SavedTeamQuery) {
  const url = getQueryUrl(query);
  router.push(url);
}

// Handle edit query
function editQuery(query: SavedTeamQuery) {
  editingQuery.value = query;
  showSaveQueryModal.value = true;
}

// Handle delete query
async function deleteQuery(query: SavedTeamQuery) {
  if (window.confirm(`Are you sure you want to delete "${query.name}"? This action cannot be undone.`)) {
    try {
      await savedQueriesStore.deleteQuery(query.team_id, query.id.toString());

      // Refresh the source queries to update the UI
      if (selectedSourceId.value) {
        await loadSourceQueries();
      }

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
      await savedQueriesStore.updateQuery(
        formData.team_id,
        editingQuery.value.id.toString(),
        {
          name: formData.name,
          description: formData.description,
        }
      );

      // Refresh the source queries to update the UI
      if (selectedSourceId.value) {
        await loadSourceQueries();
      }

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

// Get query type badge color
function getQueryTypeBadgeVariant(type: string): "default" | "secondary" | "destructive" | "outline" {
  return type === 'logchefql' ? 'default' : 'secondary';
}

// Create a new query in the explorer
function createNewQuery() {
  if (selectedSourceId.value) {
    router.push(`/logs/explore?source=${selectedSourceId.value}`);
  } else {
    router.push('/logs/explore');
  }
}

// Clear search
function clearSearch() {
  searchQuery.value = '';
}
</script>

<template>
  <div class="container py-6 space-y-6">
    <div class="flex justify-between items-center">
      <h1 class="text-2xl font-bold tracking-tight">Saved Queries</h1>
      <Button @click="createNewQuery">Create New Query</Button>
    </div>

    <!-- Error Alert -->
    <div v-if="urlError" class="bg-destructive/15 text-destructive px-4 py-2 rounded-md mb-2 flex items-center">
      <span class="text-sm">{{ urlError }}</span>
    </div>

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
    <div v-else-if="showEmptyState" class="flex flex-col h-[calc(100vh-12rem)]">
      <!-- Team selector bar -->
      <div class="border-b pb-3 mb-2">
        <div class="flex items-center justify-between">
          <div class="flex items-center space-x-2 text-sm">
            <Select :model-value="teamsStore.currentTeamId ? teamsStore.currentTeamId.toString() : ''"
              @update:model-value="handleTeamChange" class="h-8 min-w-[160px]">
              <SelectTrigger>
                <SelectValue placeholder="Select a team">{{ selectedTeamName }}</SelectValue>
              </SelectTrigger>
              <SelectContent>
                <SelectItem v-for="team in teamsStore.teams" :key="team.id" :value="team.id.toString()">
                  {{ team.name }}
                </SelectItem>
              </SelectContent>
            </Select>
          </div>
        </div>
      </div>

      <!-- Empty state content -->
      <div class="flex flex-col items-center justify-center flex-1 gap-4">
        <div class="text-center space-y-2">
          <h2 class="text-2xl font-semibold tracking-tight">No Sources Found in {{ selectedTeamName }}</h2>
          <p class="text-muted-foreground">
            This team doesn't have any log sources configured. You can add a source or switch to another team.
          </p>
        </div>
        <div class="flex gap-3">
          <Button @click="router.push({ name: 'NewSource' })">
            <Plus class="mr-2 h-4 w-4" />
            Add Source
          </Button>
          <Button variant="outline" v-if="teamsStore.teams.length > 1">
            Try switching teams using the selector above
          </Button>
        </div>
      </div>
    </div>

    <div v-else>
      <!-- Team and Source selectors -->
      <div class="border-b pb-3 mb-2">
        <div class="flex items-center justify-between">
          <div class="flex items-center space-x-2 text-sm">
            <Select :model-value="teamsStore.currentTeamId ? teamsStore.currentTeamId.toString() : ''"
              @update:model-value="handleTeamChange" class="h-8 min-w-[160px]" :disabled="isChangingTeam">
              <SelectTrigger>
                <SelectValue placeholder="Select a team">
                  <span v-if="isChangingTeam">Loading...</span>
                  <span v-else>{{ selectedTeamName }}</span>
                </SelectValue>
              </SelectTrigger>
              <SelectContent>
                <SelectItem v-for="team in teamsStore.teams" :key="team.id" :value="team.id.toString()">
                  {{ team.name }}
                </SelectItem>
              </SelectContent>
            </Select>

            <span class="text-muted-foreground">→</span>

            <Select :model-value="selectedSourceId" @update:model-value="handleSourceChange"
              :disabled="isChangingSource || !teamsStore.currentTeamId || (sourcesStore.teamSources || []).length === 0"
              class="h-8 min-w-[200px]">
              <SelectTrigger>
                <SelectValue placeholder="Select a source">
                  <span v-if="isChangingSource">Loading...</span>
                  <span v-else>{{ selectedSourceName }}</span>
                </SelectValue>
              </SelectTrigger>
              <SelectContent>
                <SelectItem v-for="source in sourcesStore.teamSources || []" :key="source.id"
                  :value="String(source.id)">
                  {{ formatSourceName(source) }}
                </SelectItem>
              </SelectContent>
            </Select>
          </div>
        </div>
      </div>

      <!-- Search box -->
      <div class="my-4">
        <div class="relative">
          <Search class="absolute left-2.5 top-2.5 h-4 w-4 text-muted-foreground" />
          <Input v-model="searchQuery" type="search" placeholder="Search by name or description..." class="pl-8" />
          <Button v-if="searchQuery" variant="outline" size="sm" class="absolute right-2 top-1.5" @click="clearSearch">
            Clear
          </Button>
        </div>
      </div>

      <!-- Loading state -->
      <div v-if="isLoading" class="flex justify-center items-center py-8">
        <Loader2 class="h-8 w-8 animate-spin text-primary" />
        <p class="ml-2 text-muted-foreground">Loading saved queries...</p>
      </div>

      <!-- Empty state - no queries -->
      <div v-else-if="!hasQueries" class="flex flex-col justify-center items-center py-12 space-y-4">
        <div class="rounded-full bg-muted p-3">
          <Search class="h-6 w-6 text-muted-foreground" />
        </div>
        <p class="text-xl text-muted-foreground">No saved queries found</p>
        <p class="text-muted-foreground">
          {{ searchQuery ? 'No queries match your search.' : 'Create a query in the Explorer and save it to access it here.' }}
        </p>
        <div class="flex gap-3">
          <Button v-if="searchQuery" variant="outline" @click="clearSearch">
            Clear Search
          </Button>
          <Button v-else @click="createNewQuery">
            Create New Query
          </Button>
        </div>
      </div>

      <!-- Queries table -->
      <div v-else>
        <div class="text-sm text-muted-foreground mb-2">
          {{ totalQueryCount }} {{ totalQueryCount === 1 ? 'query' : 'queries' }} found
        </div>

        <Table class="font-sans">
          <TableHeader>
            <TableRow>
              <TableHead class="w-[250px] font-sans">Name</TableHead>
              <TableHead class="font-sans">Description</TableHead>
              <TableHead class="w-[100px] font-sans">Type</TableHead>
              <TableHead class="w-[150px] font-sans">Created</TableHead>
              <TableHead class="w-[150px] font-sans">Updated</TableHead>
              <TableHead class="w-[100px] text-right font-sans">Actions</TableHead>
            </TableRow>
          </TableHeader>
          <TableBody>
            <TableRow v-for="query in filteredQueries" :key="query.id">
              <TableCell class="font-medium font-sans">
                <a @click.prevent="openQuery(query)" :href="getQueryUrl(query)"
                  class="text-primary hover:underline cursor-pointer">
                  {{ query.name }}
                </a>
              </TableCell>
              <TableCell>{{ query.description || '-' }}</TableCell>
              <TableCell>
                <Badge :variant="getQueryTypeBadgeVariant(query.query_type)">
                  {{ query.query_type.toUpperCase() }}
                </Badge>
              </TableCell>
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
      </div>

      <!-- Edit query modal -->
      <SaveQueryModal v-if="showSaveQueryModal && editingQuery" :is-open="showSaveQueryModal"
        :initial-data="editingQuery" :is-edit-mode="true" @close="showSaveQueryModal = false" @save="handleSaveQuery" />
    </div>
  </div>
</template>

<!-- Custom styles removed in favor of Tailwind classes -->
