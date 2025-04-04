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
import { TOAST_DURATION } from '@/lib/constants';
import SaveQueryModal from '@/components/collections/SaveQueryModal.vue';
import { getErrorMessage } from '@/api/types';
import { useSourcesStore } from '@/stores/sources';
import { formatSourceName } from '@/utils/format';
import type { SavedTeamQuery } from '@/api/savedQueries';
import { useTeamsStore } from '@/stores/teams';
import { Badge } from '@/components/ui/badge';
import { useSavedQueries } from '@/composables/useSavedQueries';

// Initialize router and services with better error handling
const router = useRouter();
const { toast } = useToast();

// Initialize stores with error handling
let sourcesStore = useSourcesStore();
let teamsStore = useTeamsStore();

try {
  sourcesStore = useSourcesStore();
  teamsStore = useTeamsStore();
} catch (error) {
  console.error("Error initializing stores:", error);
  // Create fallback objects with proper typing
  sourcesStore = {
    loadTeamSources: async () => ({ success: false }),
    teamSources: [],
    isLoading: false
  } as any; // Fallback to any for error case

  teamsStore = {
    loadTeams: async () => { },
    setCurrentTeam: () => { },
    currentTeamId: null,
    currentTeam: null,
    teams: [],
    resetAdminTeams: () => { }
  } as any; // Fallback to any for error case
}

// Get saved queries composable
const {
  showSaveQueryModal,
  editingQuery,
  isLoading,
  queries,
  filteredQueries,
  hasQueries,
  totalQueryCount,
  searchQuery,
  getQueryUrl,
  openQuery,
  editQuery,
  deleteQuery,
  createNewQuery,
  clearSearch,
  loadSourceQueries
} = useSavedQueries();

// Local UI state
const selectedSourceId = ref<string>('');
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

// Selected team name with better null handling
const selectedTeamName = computed(() => {
  if (!teamsStore || !teamsStore.currentTeam) {
    return 'Select a team';
  }
  return teamsStore.currentTeam.name || 'Select a team';
});

// Selected source name
const selectedSourceName = computed(() => {
  if (!selectedSourceId.value) return 'Select a source';
  const source = sourcesStore.teamSources?.find(s => s.id === parseInt(selectedSourceId.value));
  return source ? formatSourceName(source) : 'Select a source';
});

// Add this computed property near the other computed properties
const emptyStateMessage = computed(() =>
  searchQuery.value
    ? 'No queries match your search.'
    : 'Create a query in the Explorer and save it to your collection.'
);

// Load sources and queries on mount
onMounted(async () => {
  try {
    // Reset admin teams and load user teams to ensure we have the correct context
    teamsStore.resetAdminTeams();

    // Load teams first (explicitly use user teams endpoint)
    await teamsStore.loadTeams(true);

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

// Watch for source selection changes and team changes with safer access
watch(
  [
    () => selectedSourceId.value,
    () => teamsStore && teamsStore.currentTeamId ? teamsStore.currentTeamId : null
  ],
  async ([newSourceId, newTeamId], [oldSourceId, oldTeamId]) => {
    // Skip fetching if either:
    // 1. We're in the middle of a team change
    // 2. We're in the middle of a source change
    if (isChangingTeam.value || isChangingSource.value) {
      return;
    }

    // If team changed, don't fetch queries yet - wait for handleTeamChange to set appropriate source
    if (newTeamId !== oldTeamId) {
      return;
    }

    // Only proceed if we have both a valid team and source ID
    if (newSourceId && newTeamId) {
      // Verify source exists in current team before fetching
      const sourceId = parseInt(newSourceId);
      const sourceExists = sourcesStore.teamSources.some(source => source.id === sourceId);

      if (sourceExists) {
        await fetchQueries();
        // Mark initial load as complete after first load
        isInitialLoad.value = false;
      }
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
    const sourcesResult = await sourcesStore.loadTeamSources(currentTeamId);

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

      // Only fetch queries after we've properly set the selectedSourceId
      await fetchQueries();
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

    await fetchQueries();

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

async function fetchQueries() {
  if (!teamsStore?.currentTeamId || !selectedSourceId.value) {
    console.warn("No team or source selected, cannot load queries");
    return;
  }

  const sourceId = parseInt(selectedSourceId.value);

  // Double check that this source exists for the current team before making API call
  const sourceExists = sourcesStore.teamSources.some(source => source.id === sourceId);

  if (!sourceExists) {
    console.warn(`Source ID ${sourceId} does not exist for team ${teamsStore.currentTeamId}, skipping query fetch`);
    return;
  }

  await loadSourceQueries(
    teamsStore.currentTeamId,
    sourceId
  );
}

// Format time using the formatDate utility
function formatTime(dateStr: string): string {
  return formatDate(dateStr);
}

// Handle delete query with refresh
async function handleDeleteQuery(query: SavedTeamQuery) {
  const result = await deleteQuery(query);
  if (result.success && selectedSourceId.value) {
    await fetchQueries();
  }
}

// Handle save query modal submission
// Now handled by the useSavedQueries composable

// Get query type badge color
function getQueryTypeBadgeVariant(type: string): "default" | "secondary" | "destructive" | "outline" {
  return type === 'logchefql' ? 'default' : 'secondary';
}

// Create a new query with current source
function handleCreateNewQuery() {
  createNewQuery(selectedSourceId.value ? parseInt(selectedSourceId.value) : undefined);
}
</script>

<template>
  <div class="container py-6 space-y-6">
    <div class="flex justify-between items-center">
      <h1 class="text-2xl font-bold tracking-tight">Collections</h1>
      <Button @click="handleCreateNewQuery">
        <Plus class="mr-2 h-4 w-4" />
        Add to Collection
      </Button>
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
          <div class="flex items-center space-x-4 text-sm">
            <div class="flex flex-col space-y-1.5">
              <label class="text-sm font-medium leading-none">Team</label>
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
            </div>

            <span class="text-muted-foreground mt-6">→</span>

            <div class="flex flex-col space-y-1.5">
              <label class="text-sm font-medium leading-none">Source</label>
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
      </div>

      <!-- Search box -->
      <div class="my-4">
        <div class="relative">
          <Search class="absolute left-2.5 top-2.5 h-4 w-4 text-muted-foreground" />
          <Input v-model="searchQuery" type="search" placeholder="Search collection by name or description..."
            class="pl-8" />
          <Button v-if="searchQuery" variant="outline" size="sm" class="absolute right-2 top-1.5" @click="clearSearch">
            Clear
          </Button>
        </div>
      </div>

      <!-- Loading state -->
      <div v-if="isLoading" class="flex justify-center items-center py-8">
        <Loader2 class="h-8 w-8 animate-spin text-primary" />
        <p class="ml-2 text-muted-foreground">Loading collection...</p>
      </div>

      <!-- Empty state - no queries -->
      <div v-else-if="!hasQueries" class="flex flex-col justify-center items-center py-12 space-y-4">
        <div class="rounded-full bg-muted p-3">
          <Search class="h-6 w-6 text-muted-foreground" />
        </div>
        <p class="text-xl text-muted-foreground">Collection is empty</p>
        <p class="text-muted-foreground">{{ emptyStateMessage }}</p>
        <div class="flex gap-3">
          <Button v-if="searchQuery" variant="outline" @click="clearSearch">
            Clear Search
          </Button>
          <Button v-else @click="handleCreateNewQuery">
            Add to Collection
          </Button>
        </div>
      </div>

      <!-- Queries table -->
      <div v-else>
        <div class="text-sm text-muted-foreground mb-2">
          {{ totalQueryCount }} {{ totalQueryCount === 1 ? 'query' : 'queries' }} in collection
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
                    <DropdownMenuItem @click="editQuery(query)">
                      <Pencil class="mr-2 h-4 w-4" />
                      Edit
                    </DropdownMenuItem>
                    <DropdownMenuItem @click="handleDeleteQuery(query)" class="text-destructive">
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
