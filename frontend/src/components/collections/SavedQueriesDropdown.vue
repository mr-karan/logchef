<script setup lang="ts">
import { ref, computed, watch, onMounted } from 'vue';
import { ChevronDown, Save, PlusCircle, ListTree, Pencil, Eye, Search, X } from 'lucide-vue-next';
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
import { useToast } from '@/components/ui/toast';
import { TOAST_DURATION } from '@/lib/constants';
import { type SavedTeamQuery } from '@/api/savedQueries';
import { useSavedQueriesStore } from '@/stores/savedQueries';
import { Input } from '@/components/ui/input';

const props = defineProps<{
  selectedTeamId?: number;
  selectedSourceId?: number;
}>();

const emit = defineEmits<{
  (e: 'save'): void;
  (e: 'select-saved-query', query: SavedTeamQuery): void;
}>();

const router = useRouter();
const { toast } = useToast();
const savedQueriesStore = useSavedQueriesStore();

// Local state
const isOpen = ref(false);
const searchQuery = ref('');

// Computed properties from store
const isLoadingQueries = computed(() => {
  if (!props.selectedTeamId || !props.selectedSourceId) return false;
  return savedQueriesStore.isLoadingOperation(`fetchTeamSourceQueries-${props.selectedTeamId}-${props.selectedSourceId}`);
});
const queries = computed(() => savedQueriesStore.queries);

// Filtered queries based on search term
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

// Generate URL for query exploration
function getQueryUrl(query: SavedTeamQuery): string {
  try {
    const queryType = query.query_type?.toLowerCase() === 'logchefql' ? 'logchefql' : 'sql';
    let url = `/logs/explore?team=${query.team_id}&source=${query.source_id}&query_id=${query.id}&mode=${queryType}`;

    try {
      const queryContent = JSON.parse(query.query_content);
      if (queryContent.content) {
        url += `&q=${encodeURIComponent(queryContent.content)}`;
      }
      if (queryContent.limit) {
        url += `&limit=${queryContent.limit}`;
      }
      if (queryContent.timeRange?.absolute) {
        url += `&start_time=${queryContent.timeRange.absolute.start}&end_time=${queryContent.timeRange.absolute.end}`;
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
</script>

<template>
  <DropdownMenu v-model:open="isOpen">
    <DropdownMenuTrigger asChild>
      <Button variant="outline" class="w-full justify-between">
        <span class="flex items-center gap-1.5">
          <Save class="h-4 w-4 opacity-70" />
          <span>Collections</span>
        </span>
        <ChevronDown class="h-4 w-4 opacity-70" />
      </Button>
    </DropdownMenuTrigger>
    <DropdownMenuContent align="start" class="w-[280px]">
      <DropdownMenuLabel>Collections</DropdownMenuLabel>

      <!-- Search Input (New) -->
      <div v-if="queries.length > 0" class="px-2 pt-2 pb-1">
        <div class="relative">
          <Search class="absolute left-2 top-2.5 h-3.5 w-3.5 text-muted-foreground" />
          <Input v-model="searchQuery" type="search" placeholder="Search collection..." class="pl-7 h-8 text-xs"
            @click.stop />
          <button v-if="searchQuery" @click.stop="clearSearch"
            class="absolute right-2 top-2 text-muted-foreground hover:text-foreground">
            <X class="h-3.5 w-3.5" />
          </button>
        </div>
      </div>

      <!-- Loading state -->
      <div v-if="isLoadingQueries" class="px-2 py-3 flex flex-col gap-2">
        <Skeleton v-for="i in 3" :key="i" class="h-5 w-full" />
      </div>

      <!-- State for missing team/source selection -->
      <div v-else-if="!props.selectedTeamId || !props.selectedSourceId" class="px-2 py-3 text-sm text-muted-foreground">
        Select a Team and Source first.
      </div>

      <!-- No queries state -->
      <div v-else-if="queries.length === 0" class="px-2 py-3 text-sm text-muted-foreground">
        No saved queries for this source. Save one to see it here.
      </div>

      <!-- No search results state -->
      <div v-else-if="filteredQueries.length === 0" class="px-2 py-3 text-sm text-muted-foreground">
        No queries match your search.
        <button @click="clearSearch" class="text-primary hover:underline block mt-1">Clear search</button>
      </div>

      <!-- Queries list -->
      <template v-else>
        <div v-for="query in searchQuery ? filteredQueries : filteredQueries.slice(0, 5)" :key="query.id"
          class="py-2 px-2 hover:bg-accent hover:text-accent-foreground flex items-center justify-between group relative cursor-pointer"
          @click.stop="selectQuery(query)">
          <span class="font-medium flex-1 pr-2 truncate">{{ query.name }}</span>
          <div
            class="flex gap-1 opacity-0 group-hover:opacity-100 transition-opacity absolute right-2 top-1/2 -translate-y-1/2 bg-accent p-0.5 rounded"
            @click.stop>
            <button class="rounded-sm h-6 w-6 flex items-center justify-center hover:bg-accent-foreground/10"
              @click.stop="selectQuery(query)" title="Open query">
              <Eye class="h-3.5 w-3.5" />
            </button>
            <button class="rounded-sm h-6 w-6 flex items-center justify-center hover:bg-accent-foreground/10"
              @click.stop="handleEditQuery(query)" title="Edit query">
              <Pencil class="h-3.5 w-3.5" />
            </button>
          </div>
        </div>

        <!-- Show "View All" option if there are more than 5 queries and not searching -->
        <DropdownMenuSeparator v-if="!searchQuery && filteredQueries.length > 5" />
        <DropdownMenuItem v-if="!searchQuery && filteredQueries.length > 5" @click="goToQueries" class="cursor-pointer">
          <ListTree class="mr-2 h-4 w-4" />
          <span>View All Queries ({{ queries.length }})</span>
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
        <span>Manage Collections</span>
      </DropdownMenuItem>
    </DropdownMenuContent>
  </DropdownMenu>
</template>
