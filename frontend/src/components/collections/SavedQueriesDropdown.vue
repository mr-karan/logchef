<script setup lang="ts">
import { ref, computed, watch, onMounted } from 'vue';
import { ChevronDown, Save, PlusCircle, ListTree, Pencil, Eye, Search, X, BookMarked } from 'lucide-vue-next';
import { useRouter, useRoute } from 'vue-router';
import {
  DropdownMenu,
  DropdownMenuContent,
  DropdownMenuItem,
  DropdownMenuLabel,
  DropdownMenuSeparator,
  DropdownMenuTrigger,
  DropdownMenuSub,
  DropdownMenuSubTrigger,
  DropdownMenuSubContent
} from '@/components/ui/dropdown-menu';
import { Button } from '@/components/ui/button';
import { Skeleton } from '@/components/ui/skeleton';
import { useToast } from '@/composables/useToast';
import { TOAST_DURATION } from '@/lib/constants';
import { type SavedTeamQuery } from '@/api/savedQueries';
import { useSavedQueriesStore } from '@/stores/savedQueries';
import { Input } from '@/components/ui/input';
import { useExploreStore } from '@/stores/explore';
import { useAuthStore } from '@/stores/auth';
import { useSavedQueries } from '@/composables/useSavedQueries';

const props = defineProps<{
  selectedTeamId?: number;
  selectedSourceId?: number;
}>();

const emit = defineEmits<{
  (e: 'save'): void;
  (e: 'select-saved-query', query: SavedTeamQuery): void;
  (e: 'save-as-new'): void;
}>();

const router = useRouter();
const route = useRoute();
const { toast } = useToast();
const savedQueriesStore = useSavedQueriesStore();
const exploreStore = useExploreStore();
const authStore = useAuthStore();

const {
  handleSaveQueryClick,
  loadSavedQuery: handleLoadQuery,
  createNewQuery: handleCreateNewQuery,
  isEditingExistingQuery,
  canManageCollections,
} = useSavedQueries();

// Local state
const isOpen = ref(false);
const searchQuery = ref('');

// Computed properties from store
const isLoadingQueries = computed(() => {
  if (!props.selectedTeamId || !props.selectedSourceId) return false;
  return savedQueriesStore.isLoadingOperation(`fetchTeamSourceQueries-${props.selectedTeamId}-${props.selectedSourceId}`);
});
const queries = computed(() => savedQueriesStore.queries);

// Filtered queries based on local search term
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

// Computed properties based on local filtering state
const hasQueries = computed(() => filteredQueries.value.length > 0);
const totalQueryCount = computed(() => queries.value.length); // Total count based on unfiltered store queries
const filteredQueryCount = computed(() => filteredQueries.value.length); // Count based on local filter

// Watch for changes in team/source ID
watch(
  () => [props.selectedTeamId, props.selectedSourceId],
  async ([teamId, sourceId]) => {
    if (teamId && sourceId) {
      await loadQueries(teamId, sourceId);
    } else {
      // Optionally clear store queries if team/source deselects, or let the store handle it
      // savedQueriesStore.resetQueries() // Assuming a reset action exists if needed
      // For now, the dropdown will just show 'select team/source' based on template logic
    }
  },
  { immediate: true }
);

// Load queries when dropdown opens
watch(isOpen, async (open) => {
  if (open && props.selectedTeamId && props.selectedSourceId && !queries.value.length) {
    await loadQueries(props.selectedTeamId, props.selectedSourceId);
  }

  // Clear search when dropdown is closed
  if (!open) {
    searchQuery.value = '';
  }
});

// Function to load queries using the store
async function loadQueries(teamId: number, sourceId: number) {
  if (!teamId || !sourceId) return;

  try {
    await savedQueriesStore.fetchTeamSourceQueries(teamId, sourceId);

  } catch (error) {
    console.error('Error triggering query load from store:', error);
    toast({
      title: 'Error',
      description: 'Failed to initiate loading saved queries.',
      variant: 'destructive',
      duration: TOAST_DURATION.ERROR,
    });
  }
}

// Clear search function
function clearSearch() {
  searchQuery.value = '';
}

// Handle query selection
function selectQuery(query: SavedTeamQuery) {
  try {
    emit('select-saved-query', query);
    isOpen.value = false;
  } catch (error) {
    console.error('Error selecting query:', error);
    toast({
      title: 'Error',
      description: 'Failed to open the selected query',
      variant: 'destructive',
      duration: TOAST_DURATION.ERROR,
    });
  }
}

// Handle save action
function handleSave() {
  emit('save');
  isOpen.value = false;
}

// Handle request to save as new query
function handleRequestSaveAsNew() {
  emit('save-as-new');
  isOpen.value = false;
}

// Generate URL for query exploration
function getQueryUrl(query: SavedTeamQuery): string {
  try {
    const queryType = query.query_type?.toLowerCase() === 'logchefql' ? 'logchefql' : 'sql';
    let url = `/logs/explore?team=${query.team_id}&source=${query.source_id}&query_id=${query.id}&mode=${queryType}`;

    try {
      const queryContent = JSON.parse(query.query_content);
      if (queryContent.content) {
        if (queryType === 'logchefql') {
          url += `&q=${encodeURIComponent(queryContent.content)}`;
        } else {
          url += `&sql=${encodeURIComponent(queryContent.content)}`;
        }
      }
      if (queryContent.limit) {
        url += `&limit=${queryContent.limit}`;
      }
      if (queryContent.timeRange?.absolute) {
        url += `&start=${queryContent.timeRange.absolute.start}&end=${queryContent.timeRange.absolute.end}`;
      }
    } catch (error) {
      console.error('Error parsing query content:', error);
    }

    return url;
  } catch (error) {
    console.error('Error generating query URL:', error);
    return `/logs/explore?team=${query.team_id}&source=${query.source_id}&mode=${query.query_type}`;
  }
}

// Edit query - navigate to edit URL
function handleEditQuery(query: SavedTeamQuery) {
  const url = getQueryUrl(query);
  router.push(url);
  isOpen.value = false;
}

// Go to queries view
function goToQueries() {
  const query: Record<string, string | number> = {};
  if (props.selectedTeamId) query.team = props.selectedTeamId;
  if (props.selectedSourceId) query.source = props.selectedSourceId;

  // Use push instead of replace to create a proper navigation entry
  // This ensures the back button will work correctly
  router.push({
    path: '/logs/saved',
    query
  });
  isOpen.value = false;
}

const isUserAuthenticated = computed(() => authStore.isAuthenticated);
const activeSavedQueryName = computed(() => exploreStore.activeSavedQueryName);

const navigateToCollectionsView = () => {
  const query: Record<string, string | number> = {};
  if (props.selectedTeamId) query.team = props.selectedTeamId;
  if (props.selectedSourceId) query.source = props.selectedSourceId;

  router.push({
    path: '/logs/saved',
    query
  });
  isOpen.value = false;
};
</script>

<template>
  <DropdownMenu v-if="isUserAuthenticated" v-model:open="isOpen">
    <DropdownMenuTrigger as-child>
      <Button variant="outline" class="whitespace-nowrap">
        <Save class="w-4 h-4 mr-2" />
        <span>{{ activeSavedQueryName || 'Collections' }}</span>
        <ChevronDown class="w-4 h-4 ml-auto" />
      </Button>
    </DropdownMenuTrigger>
    <DropdownMenuContent class="w-64" align="end">
      <DropdownMenuItem v-if="canManageCollections" @click="handleSave">
        <Save class="w-4 h-4 mr-2" />
        <span>{{ isEditingExistingQuery ? 'Update Current Query' : 'Save Current Query to Collection...' }}</span>
      </DropdownMenuItem>
      <DropdownMenuItem v-if="canManageCollections && isEditingExistingQuery" @click="handleRequestSaveAsNew">
        <PlusCircle class="w-4 h-4 mr-2" />
        <span>Save as New Query...</span>
      </DropdownMenuItem>

      <DropdownMenuSeparator v-if="canManageCollections" />

      <DropdownMenuLabel v-if="hasQueries">Load from Collection</DropdownMenuLabel>
      <DropdownMenuSub v-if="hasQueries">
        <DropdownMenuSubTrigger>
          <ListTree class="w-4 h-4 mr-2" />
          <span>Select Query ({{ filteredQueryCount }} / {{ totalQueryCount }})</span>
        </DropdownMenuSubTrigger>
        <DropdownMenuSubContent class="max-h-96 overflow-y-auto">
          <DropdownMenuItem v-if="filteredQueries.length === 0" disabled>
            No matching queries found.
          </DropdownMenuItem>
          <DropdownMenuItem v-for="query in filteredQueries" :key="query.id" @click="() => selectQuery(query)">
            <span class="truncate" :title="query.name">{{ query.name }}</span>
          </DropdownMenuItem>
        </DropdownMenuSubContent>
      </DropdownMenuSub>

      <DropdownMenuSeparator v-if="hasQueries" />

      <DropdownMenuItem @click="navigateToCollectionsView">
        <BookMarked class="w-4 h-4 mr-2" />
        <span>View All Collections</span>
      </DropdownMenuItem>
    </DropdownMenuContent>
  </DropdownMenu>
</template>
