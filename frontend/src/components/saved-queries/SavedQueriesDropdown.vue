<script setup lang="ts">
import { ref, computed } from 'vue';
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
import { useTeamsStore } from '@/stores/teams';
import { useToast } from '@/components/ui/toast';
import { TOAST_DURATION } from '@/lib/constants';
import { getErrorMessage } from '@/api/types';
import { useSavedQueries } from '@/composables/useSavedQueries';
import type { SavedTeamQuery } from '@/api/savedQueries';

const props = defineProps<{
  selectedTeamId?: number;
  selectedSourceId?: number;
}>();

const emit = defineEmits<{
  (e: 'save'): void;
}>();

const router = useRouter();
const teamsStore = useTeamsStore();
const { toast } = useToast();

// Use state and actions from the composable
const {
  isLoading,         // Use composable's loading state
  queries,           // Use composable's raw queries list
  filteredQueries,   // Use composable's filtered list (if searching needed later)
  hasQueries,        // Use composable's computed property
  getQueryUrl,       // For navigation
  openQuery,         // Use composable's open function
  editQuery          // Use composable's edit function
} = useSavedQueries();

// Local UI state
const isOpen = ref(false);

// Computed properties based on props
const currentTeamId = computed(() => props.selectedTeamId);
const currentSourceId = computed(() => props.selectedSourceId);

// Handle query selection using composable's function
function selectQuery(query: SavedTeamQuery) {
  try {
    openQuery(query);
    isOpen.value = false; // Close dropdown after selection
  } catch (error) {
    console.error('Error selecting query:', error);
    toast({
      title: 'Error',
      description: 'Failed to open the selected query. Please try again.',
      variant: 'destructive',
      duration: TOAST_DURATION.ERROR,
    });
  }
}

// Handle save action (emit event)
function handleSave() {
  emit('save');
  isOpen.value = false;
}

// Handle edit query using composable's function
function handleEditQuery(query: SavedTeamQuery) {
  try {
    console.log("Editing query from dropdown:", query);
    editQuery(query);
    isOpen.value = false; // Close dropdown after edit trigger
  } catch (error) {
    // Error handling is within editQuery, but catch here if needed
    console.error("Error triggering edit from dropdown:", error);
  }
}

// Go to queries view
function goToQueries() {
  const query: Record<string, string | number> = {};
  if (currentTeamId.value) query.team = currentTeamId.value;
  if (currentSourceId.value) query.source = currentSourceId.value; // Include source if available

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
          <span>Saved Queries</span>
        </span>
        <ChevronDown class="h-4 w-4 opacity-70" />
      </Button>
    </DropdownMenuTrigger>
    <DropdownMenuContent align="start" class="w-[280px]">
      <DropdownMenuLabel>Saved Queries</DropdownMenuLabel>

      <!-- Loading state from composable -->
      <div v-if="isLoading" class="px-2 py-3 flex flex-col gap-2">
        <Skeleton v-for="i in 3" :key="i" class="h-5 w-full" />
      </div>

      <!-- State for missing team/source selection -->
      <div v-else-if="!currentTeamId || !currentSourceId" class="px-2 py-3 text-sm text-muted-foreground">
        Select a Team and Source first.
      </div>

      <!-- No queries state from composable -->
      <div v-else-if="!hasQueries" class="px-2 py-3 text-sm text-muted-foreground">
        No saved queries for this source. Save one to see it here.
      </div>

      <!-- Queries list from composable (using 'queries' for simplicity, can switch to filteredQueries if search added) -->
      <template v-else>
        <div v-for="query in queries.slice(0, 5)" :key="query.id"
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

        <!-- Show "View All" option if there are more than 5 queries -->
        <DropdownMenuSeparator v-if="queries.length > 5" />
        <DropdownMenuItem v-if="queries.length > 5" @click="goToQueries" class="cursor-pointer">
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
        <span>Manage Saved Queries</span>
      </DropdownMenuItem>
    </DropdownMenuContent>
  </DropdownMenu>
</template>
