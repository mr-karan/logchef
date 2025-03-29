<script setup lang="ts">
import { ref, computed, onMounted } from 'vue';
import { ChevronDown, Save, PlusCircle, ListTree, Pencil, Eye } from 'lucide-vue-next';
import { useRouter } from 'vue-router';
import {
  DropdownMenu,
  DropdownMenuContent,
  DropdownMenuItem,
  DropdownMenuLabel,
  DropdownMenuSeparator,
  DropdownMenuTrigger,
} from '@/components/ui/dropdown-menu';
import { Button } from '@/components/ui/button';
import { Skeleton } from '@/components/ui/skeleton';
import { useSavedQueriesStore } from '@/stores/savedQueries';
import { useTeamsStore } from '@/stores/teams';
import { useToast } from '@/components/ui/toast';
import { TOAST_DURATION } from '@/lib/constants';
import { getErrorMessage } from '@/api/types';
import { useSavedQueries } from '@/composables/useSavedQueries';

const props = defineProps<{
  selectedTeamId?: number;
}>();

const emit = defineEmits<{
  (e: 'select', queryId: string, queryData?: any): void;
  (e: 'save'): void;
  (e: 'edit', query: any): void;
}>();

const router = useRouter();
const savedQueriesStore = useSavedQueriesStore();
const teamsStore = useTeamsStore();
const { toast } = useToast();

// Local state
const isOpen = ref(false);
const currentTeamId = computed(() => props.selectedTeamId || teamsStore.currentTeamId || savedQueriesStore.data.selectedTeamId);
const isLoading = computed(() => savedQueriesStore.isLoading);
const hasQueries = computed(() => savedQueriesStore.data.queries?.length > 0);

// Load saved queries when the dropdown is opened
async function onDropdownOpen() {
  if (!currentTeamId.value) {
    // Use the current team from teamsStore instead of fetching teams
    if (teamsStore.currentTeamId) {
      savedQueriesStore.setSelectedTeam(teamsStore.currentTeamId);
    } else {
      toast({
        title: 'Error',
        description: 'No team selected. Please select a team first.',
        variant: 'destructive',
        duration: TOAST_DURATION.ERROR,
      });
      return;
    }
  }

  if (!savedQueriesStore.data.queries || savedQueriesStore.data.queries.length === 0) {
    try {
      await loadTeamQueries();
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

// Load team queries
async function loadTeamQueries() {
  if (!currentTeamId.value) return;
  return savedQueriesStore.fetchTeamQueries(currentTeamId.value, true);
}

// Import the composable for consistent URL generation and editing
import { useSavedQueries } from '@/composables/useSavedQueries';
const { getQueryUrl, editQuery } = useSavedQueries();

// Handle query selection
function selectQuery(queryId: number) {
  // First check if we need to refresh the queries list
  // This helps ensure we have the most up-to-date query data
  const needsRefresh = !savedQueriesStore.data.lastFetchTime || 
                      (Date.now() - savedQueriesStore.data.lastFetchTime > 30000); // 30 seconds

  // Function to process the query selection with the current data
  const processQuerySelection = () => {
    const query = savedQueriesStore.data.queries?.find(q => q.id === queryId);
    if (query) {
      try {
        // Generate URL for navigation using the same function as in SavedQueriesView
        const url = getQueryUrl(query);
        
        // Navigate to the generated URL
        router.push(url);
        console.log(`Navigating to query ${queryId} via URL: ${url}`);
      } catch (error) {
        console.error('Error processing query selection:', error);
        toast({
          title: 'Error',
          description: 'Failed to load the selected query. Please try again.',
          variant: 'destructive',
          duration: TOAST_DURATION.ERROR,
        });
      }
    } else {
      console.warn(`Query with ID ${queryId} not found in store`);
      toast({
        title: 'Error',
        description: 'Query not found. It may have been deleted.',
        variant: 'destructive',
        duration: TOAST_DURATION.ERROR,
      });
    }
    isOpen.value = false;
  };
  
  // If we need a refresh and have a current team ID, fetch the latest queries first
  if (needsRefresh && currentTeamId.value) {
    savedQueriesStore.fetchTeamQueries(currentTeamId.value, true)
      .then(() => {
        console.log("Refreshed queries list before selection");
        processQuerySelection();
      })
      .catch(error => {
        console.error("Error refreshing queries:", error);
        // Still try to process with existing data
        processQuerySelection();
      });
  } else {
    // Use existing data
    processQuerySelection();
  }
}

// Handle save action
function handleSave() {
  emit('save');
  isOpen.value = false;
}

// Handle edit query 
function handleEditQuery(query) {
  try {
    console.log("Editing query from dropdown:", query);
    // Make sure we're passing a properly typed query object
    const typedQuery = {
      id: query.id,
      team_id: query.team_id,
      source_id: query.source_id,
      name: query.name,
      description: query.description,
      query_type: query.query_type,
      query_content: query.query_content,
      created_at: query.created_at,
      updated_at: query.updated_at
    };
    
    editQuery(typedQuery);
    isOpen.value = false;
  } catch (error) {
    console.error("Error preparing query for edit:", error);
    toast({
      title: 'Error',
      description: 'Failed to edit query. Please try again.',
      variant: 'destructive',
      duration: TOAST_DURATION.ERROR,
    });
  }
}

// Go to queries view
function goToQueries() {
  const query = currentTeamId.value ? { team: currentTeamId.value } : {};
  router.push({
    path: '/logs/saved',
    query
  });
  isOpen.value = false;
}

// Load teams on component mount if needed
onMounted(async () => {
  // Use the teams from teamsStore instead of making a separate API call
  if (teamsStore.teams.length > 0) {
    savedQueriesStore.data.teams = teamsStore.teams;
  }
});
</script>

<template>
  <DropdownMenu v-model:open="isOpen" @update:open="onDropdownOpen">
    <DropdownMenuTrigger asChild>
      <Button variant="outline" class="w-full justify-between">
        <span class="flex items-center gap-1.5">
          <Save class="h-4 w-4 opacity-70" />
          <span>Saved Queries</span>
        </span>
        <ChevronDown class="h-4 w-4 opacity-70" />
      </Button>
    </DropdownMenuTrigger>
    <DropdownMenuContent align="start" class="w-[280px]">
      <DropdownMenuLabel>Saved Queries</DropdownMenuLabel>

      <!-- Loading state -->
      <div v-if="isLoading" class="px-2 py-3 flex flex-col gap-2">
        <Skeleton v-for="i in 3" :key="i" class="h-5 w-full" />
      </div>

      <!-- No queries state -->
      <div v-else-if="!hasQueries" class="px-2 py-3 text-sm text-muted-foreground">
        No saved queries found. Save a query to see it here.
      </div>

      <!-- Queries list -->
      <template v-else>
        <div 
          v-for="query in (savedQueriesStore.data.queries || []).slice(0, 5)" 
          :key="query.id"
          class="py-2 px-2 hover:bg-accent hover:text-accent-foreground flex items-center justify-between group"
        >
          <DropdownMenuItem 
            @click="selectQuery(query.id)" 
            class="cursor-pointer p-0 focus:bg-transparent"
          >
            <span class="font-medium">{{ query.name }}</span>
          </DropdownMenuItem>
          <div class="flex gap-1 opacity-0 group-hover:opacity-100 transition-opacity">
            <button 
              class="rounded-sm h-6 w-6 flex items-center justify-center hover:bg-accent-foreground/10" 
              @click="selectQuery(query.id)"
              title="Open query"
            >
              <Eye class="h-3.5 w-3.5" />
            </button>
            <button 
              class="rounded-sm h-6 w-6 flex items-center justify-center hover:bg-accent-foreground/10" 
              @click="handleEditQuery(query)"
              title="Edit query"
            >
              <Pencil class="h-3.5 w-3.5" />
            </button>
          </div>
        </div>

        <!-- Show "View All" option if there are more than 5 queries -->
        <DropdownMenuSeparator v-if="savedQueriesStore.data.queries.length > 5" />
        <DropdownMenuItem v-if="savedQueriesStore.data.queries.length > 5" @click="goToQueries">
          <ListTree class="mr-2 h-4 w-4" />
          <span>View All Queries ({{ savedQueriesStore.data.queries.length }})</span>
        </DropdownMenuItem>
      </template>

      <DropdownMenuSeparator />

      <!-- Save current query -->
      <DropdownMenuItem @click="handleSave" class="cursor-pointer">
        <PlusCircle class="mr-2 h-4 w-4" />
        <span>Save Current Query</span>
      </DropdownMenuItem>

      <!-- Go to all queries -->
      <DropdownMenuItem @click="goToQueries" class="cursor-pointer">
        <ListTree class="mr-2 h-4 w-4" />
        <span>Manage Saved Queries</span>
      </DropdownMenuItem>
    </DropdownMenuContent>
  </DropdownMenu>
</template>
