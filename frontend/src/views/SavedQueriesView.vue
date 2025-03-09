<script setup lang="ts">
import { ref, onMounted, computed, watch } from 'vue';
import { useRouter } from 'vue-router';
import { formatDistance } from 'date-fns';
import { ChevronDown, ChevronRight, Eye, Pencil, Trash2 } from 'lucide-vue-next';
import {
  DropdownMenu,
  DropdownMenuContent,
  DropdownMenuItem,
  DropdownMenuTrigger,
} from '@/components/ui/dropdown-menu';
import { Button } from '@/components/ui/button';
import { useToast } from '@/components/ui/toast';
import {
  Table,
  TableBody,
  TableCell,
  TableHead,
  TableHeader,
  TableRow,
} from '@/components/ui/table';
import { Select, SelectContent, SelectItem, SelectTrigger, SelectValue } from '@/components/ui/select';
import { useSavedQueriesStore } from '@/stores/savedQueries';
import { TOAST_DURATION } from '@/lib/constants';
import SaveQueryModal from '@/components/saved-queries/SaveQueryModal.vue';
import { getErrorMessage } from '@/api/types';
import { useSourcesStore } from '@/stores/sources';
import { formatSourceName } from '@/utils/format';
import type { TeamGroupedQuery } from '@/api/sources';
import { Collapsible, CollapsibleContent, CollapsibleTrigger } from '@/components/ui/collapsible';
import { useTeamsStore } from '@/stores/teams';

const router = useRouter();
const { toast } = useToast();
const savedQueriesStore = useSavedQueriesStore();
const sourcesStore = useSourcesStore();
const teamsStore = useTeamsStore();

// Local UI state
const isLoading = computed(() => sourcesStore.isLoading || savedQueriesStore.isLoading);
const showSaveQueryModal = ref(false);
const editingQuery = ref<any>(null);
const selectedSourceId = ref<string>('');
const expandedTeams = ref<Record<string, boolean>>({});

// Keep track of which teams should be expanded by default
const teamExpansionState = computed(() => {
  const sourceQueries = sourcesStore.sourceQueriesMap[selectedSourceId.value] || [];

  // If there's only one team, expand it by default
  if (sourceQueries.length === 1) {
    return { [sourceQueries[0].team_id]: true };
  }

  // Otherwise, use the current state
  return expandedTeams.value;
});

// Load sources and queries on mount
onMounted(async () => {
  try {
    // Load teams first
    await teamsStore.loadTeams();

    // Set default team if none selected
    if (!teamsStore.currentTeamId && teamsStore.teams.length > 0) {
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
      selectedSourceId.value = String(sourcesStore.teamSources[0].id);
      // Load queries for the first source
      await loadSourceQueries(selectedSourceId.value);
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

// Watch for source selection changes
watch(
  () => selectedSourceId.value,
  async (newSourceId) => {
    if (newSourceId) {
      await loadSourceQueries(newSourceId);
    }
  }
);

async function loadSourceQueries(sourceId: string) {
  try {
    await sourcesStore.loadSourceQueries(sourceId);
  } catch (error) {
    toast({
      title: 'Error',
      description: getErrorMessage(error),
      variant: 'destructive',
      duration: TOAST_DURATION.ERROR,
    });
  }
}

// Toggle team expansion
function toggleTeamExpansion(teamId: number | string) {
  const teamIdStr = String(teamId);
  expandedTeams.value = {
    ...expandedTeams.value,
    [teamIdStr]: !expandedTeams.value[teamIdStr]
  };
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
    JSON.parse(query.query_content); // Parse but don't use the content
    return `/logs/explore?query_id=${query.id}`;
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
  if (window.confirm(`Are you sure you want to delete "${query.name}"? This action cannot be undone.`)) {
    try {
      await savedQueriesStore.deleteQuery(query.team_id, query.id);

      // Also refresh the source queries to update the UI
      if (selectedSourceId.value) {
        await loadSourceQueries(selectedSourceId.value);
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
      await savedQueriesStore.updateQuery(formData.team_id, editingQuery.value.id, {
        name: formData.name,
        description: formData.description,
      });

      // Refresh the source queries to update the UI
      if (selectedSourceId.value) {
        await loadSourceQueries(selectedSourceId.value);
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

// Get source queries grouped by team
const sourceQueries = computed((): TeamGroupedQuery[] => {
  if (!selectedSourceId.value) return [];
  return sourcesStore.sourceQueriesMap[selectedSourceId.value] || [];
});

// Check if source has any queries
const hasQueries = computed((): boolean => {
  return sourceQueries.value.some(teamGroup => teamGroup.queries.length > 0);
});
</script>

<template>
  <div class="container py-6 space-y-6">
    <div class="flex justify-between items-center">
      <h1 class="text-2xl font-bold tracking-tight">Saved Queries</h1>
    </div>

    <!-- Source selector -->
    <div class="flex items-center space-x-4">
      <span class="font-medium">Source:</span>
      <Select v-model="selectedSourceId" :disabled="isLoading || !sourcesStore.teamSources.length" class="w-[300px]">
        <SelectTrigger>
          <SelectValue placeholder="Select a source" />
        </SelectTrigger>
        <SelectContent>
          <SelectItem v-for="source in sourcesStore.teamSources" :key="source.id" :value="String(source.id)">
            {{ formatSourceName(source) }}
          </SelectItem>
        </SelectContent>
      </Select>
    </div>

    <!-- Loading state -->
    <div v-if="isLoading" class="flex justify-center items-center py-8">
      <p class="text-muted-foreground">Loading saved queries...</p>
    </div>

    <!-- Empty state - no source selected -->
    <div v-else-if="!selectedSourceId" class="flex flex-col justify-center items-center py-12 space-y-4">
      <p class="text-xl text-muted-foreground">No source selected</p>
      <p class="text-muted-foreground">
        Please select a source to view its saved queries.
      </p>
    </div>

    <!-- Empty state - no queries for source -->
    <div v-else-if="!hasQueries" class="flex flex-col justify-center items-center py-12 space-y-4">
      <p class="text-xl text-muted-foreground">No saved queries found</p>
      <p class="text-muted-foreground">
        Create a query in the Explorer and save it to access it here.
      </p>
      <Button @click="router.push('/logs/explore?source=' + selectedSourceId)">Go to Explorer</Button>
    </div>

    <!-- Queries grouped by team -->
    <div v-else class="space-y-6">
      <div v-for="teamGroup in sourceQueries" :key="teamGroup.team_id" class="border rounded-md shadow-sm">
        <Collapsible :open="teamExpansionState[teamGroup.team_id]"
          @update:open="val => expandedTeams[teamGroup.team_id] = val">
          <CollapsibleTrigger class="flex justify-between items-center w-full p-4 cursor-pointer hover:bg-muted/50"
            @click="toggleTeamExpansion(teamGroup.team_id)">
            <div class="flex items-center">
              <ChevronRight class="h-5 w-5 mr-2 transition-transform duration-200"
                :class="{ 'rotate-90': teamExpansionState[teamGroup.team_id] }" />
              <h3 class="text-lg font-medium">{{ teamGroup.team_name }}</h3>
              <div class="ml-2 text-sm text-muted-foreground">
                ({{ teamGroup.queries.length }} {{ teamGroup.queries.length === 1 ? 'query' : 'queries' }})
              </div>
            </div>
          </CollapsibleTrigger>

          <CollapsibleContent>
            <Table>
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
                <TableRow v-for="query in teamGroup.queries" :key="query.id">
                  <TableCell class="font-medium">
                    <a @click.prevent="openQuery(query)" :href="getQueryUrl(query)"
                      class="text-primary hover:underline cursor-pointer">
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
          </CollapsibleContent>
        </Collapsible>
      </div>
    </div>

    <!-- Edit query modal -->
    <SaveQueryModal v-if="showSaveQueryModal && editingQuery" :is-open="showSaveQueryModal" :initial-data="editingQuery"
      :is-edit-mode="true" @close="showSaveQueryModal = false" @save="handleSaveQuery" />
  </div>
</template>
