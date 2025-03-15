<script setup lang="ts">
import { ref, computed, onMounted } from 'vue';
import { ChevronDown, Save, PlusCircle, ListTree } from 'lucide-vue-next';
import { useRouter } from 'vue-router';
import { formatDistance } from 'date-fns';
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
import { type SavedTeamQuery } from '@/api/savedQueries';

const props = defineProps<{
  selectedTeamId?: number;
}>();

const emit = defineEmits<{
  (e: 'select', queryId: string, queryData?: any): void;
  (e: 'save'): void;
}>();

const router = useRouter();
const savedQueriesStore = useSavedQueriesStore();
const teamsStore = useTeamsStore();
const { toast } = useToast();

// Local state
const isOpen = ref(false);
const currentTeamId = computed(() => props.selectedTeamId || teamsStore.currentTeam?.id || savedQueriesStore.data.selectedTeamId);
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
    await loadTeamQueries();
  }
}

// Load team queries
async function loadTeamQueries() {
  if (!currentTeamId.value) return;

  try {
    await savedQueriesStore.fetchTeamQueries(currentTeamId.value);
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

// Handle query selection
function selectQuery(queryId: number) {
  const query = savedQueriesStore.data.queries?.find(q => q.id === queryId);
  if (query) {
    try {
      // Parse the query content
      const content = JSON.parse(query.query_content);

      // Emit the select event with the query ID and the parsed content
      emit('select', String(queryId), {
        queryType: query.query_type,
        content: content
      });
    } catch (error) {
      console.error('Error parsing query content:', error);
      // Fall back to just emitting the ID if parsing fails
      emit('select', String(queryId));
    }
  } else {
    emit('select', String(queryId));
  }
  isOpen.value = false;
}

// Get query type for display
function getQueryTypeLabel(query: SavedTeamQuery): string {
  try {
    const content = JSON.parse(query.query_content);
    return content.queryType === 'logchefql' ? 'LogchefQL' : 'SQL';
  } catch (e) {
    return 'SQL'; // Default to SQL if parsing fails
  }
}

// Handle save action
function handleSave() {
  emit('save');
  isOpen.value = false;
}

// Go to queries view
function goToQueries() {
  if (currentTeamId.value) {
    router.push(`/teams/${currentTeamId.value}/queries`);
  } else {
    router.push('/queries');
  }
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
        <DropdownMenuItem v-for="query in (savedQueriesStore.data.queries || []).slice(0, 5)" :key="query.id"
          @click="selectQuery(query.id)" class="flex flex-col items-start gap-1 py-2 cursor-pointer">
          <div class="flex items-center justify-between w-full">
            <span class="font-medium">{{ query.name }}</span>
            <div class="flex items-center gap-2">
              <span class="text-xs px-1.5 py-0.5 rounded bg-primary/10 text-primary">{{ getQueryTypeLabel(query)
                }}</span>
              <span class="text-xs text-muted-foreground">{{ formatTime(query.updated_at) }}</span>
            </div>
          </div>
          <span v-if="query.description" class="text-xs text-muted-foreground line-clamp-1">
            {{ query.description }}
          </span>
        </DropdownMenuItem>

        <!-- Show "View All" option if there are more than 5 queries -->
        <DropdownMenuSeparator v-if="savedQueriesStore.data.queries.length > 5" />
        <DropdownMenuItem v-if="savedQueriesStore.data.queries.length > 5" @click="goToQueries">
          <ListTree class="mr-2 h-4 w-4" />
          <span>View All Queries ({{ savedQueriesStore.data.queries.length }})</span>
        </DropdownMenuItem>
      </template>

      <DropdownMenuSeparator />

      <!-- Save current query -->
      <DropdownMenuItem @click="handleSave">
        <PlusCircle class="mr-2 h-4 w-4" />
        <span>Save Current Query</span>
      </DropdownMenuItem>

      <!-- Go to all queries -->
      <DropdownMenuItem @click="goToQueries">
        <ListTree class="mr-2 h-4 w-4" />
        <span>Manage Saved Queries</span>
      </DropdownMenuItem>
    </DropdownMenuContent>
  </DropdownMenu>
</template>
