<script setup lang="ts">
import { ref, onMounted, computed, watch } from "vue";
import { useRouter } from "vue-router";
import {
  ChevronDown,
  Eye,
  Pencil,
  Trash2,
  Loader2,
  Plus,
  Search,
  Star,
  Link,
} from "lucide-vue-next";
import { formatDate } from "@/utils/format";
import {
  DropdownMenu,
  DropdownMenuContent,
  DropdownMenuItem,
  DropdownMenuTrigger,
} from "@/components/ui/dropdown-menu";
import { Button } from "@/components/ui/button";
import { useToast } from "@/composables/useToast";
import { Skeleton } from "@/components/ui/skeleton";
import { Alert, AlertDescription } from "@/components/ui/alert";
import {
  Card,
  CardContent,
  CardDescription,
  CardHeader,
  CardTitle,
} from "@/components/ui/card";
import {
  Table,
  TableBody,
  TableCell,
  TableHead,
  TableHeader,
  TableRow,
} from "@/components/ui/table";
import {
  Select,
  SelectContent,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from "@/components/ui/select";
import { Input } from "@/components/ui/input";
import { Label } from "@/components/ui/label";
import { TOAST_DURATION } from "@/lib/constants";
import SaveQueryModal from "@/components/collections/SaveQueryModal.vue";
import { getErrorMessage } from "@/api/types";
import { useSourcesStore } from "@/stores/sources";
import { formatSourceName } from "@/utils/format";
import type { SavedTeamQuery } from "@/api/savedQueries";
import { useTeamsStore } from "@/stores/teams";
import { Badge } from "@/components/ui/badge";
import { useSavedQueries } from "@/composables/useSavedQueries";
import { useContextSync } from "@/composables/useContextSync";
import type { SaveQueryFormData } from "@/views/explore/types";
import { useSavedQueriesStore } from "@/stores/savedQueries";
import { useContextStore } from "@/stores/context";
import { useRoute } from "vue-router";

const router = useRouter();
const route = useRoute();
const { toast } = useToast();

const sourcesStore = useSourcesStore();
const teamsStore = useTeamsStore();
const savedQueriesStore = useSavedQueriesStore();
const contextStore = useContextStore();

const {
  isReady: contextReady,
  isLoading: contextLoading,
  error: contextError,
  teamId: contextTeamId,
  sourceId: contextSourceId,
  initialize: initializeContext,
  handleTeamChange: contextHandleTeamChange,
  handleSourceChange: contextHandleSourceChange,
} = useContextSync({ basePath: '/logs/saved' });

const localTeamQueries = ref<SavedTeamQuery[] | undefined>();
const isAllTeamsMode = ref(false);
const isAllSourcesMode = computed(() => isAllTeamsMode.value || !contextSourceId.value);

const currentSelectedSource = computed(() => {
  if (!contextSourceId.value) return undefined;
  return sourcesStore.teamSources.find(s => s.id === contextSourceId.value);
});

const getSourceName = (sourceId: number) => {
  const source = sourcesStore.teamSources.find(s => s.id === sourceId);
  return source ? formatSourceName(source) : `Source ${sourceId}`;
};

// Get saved queries composable ONCE at the top level
const {
  showSaveQueryModal,
  editingQuery,
  isLoading,
  openingQueryId,
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
  loadSourceQueries,
  handleSaveQuery: handleSaveQueryFromComposable,
  updateSavedQuery,
  canManageCollections,
} = useSavedQueries(localTeamQueries, currentSelectedSource);

const selectedSourceId = computed(() => 
  contextSourceId.value ? String(contextSourceId.value) : "all"
);

const showLoadingState = computed(() => {
  return sourcesStore.isLoading || contextLoading.value;
});

const showEmptyState = computed(() => {
  return (
    !showLoadingState.value &&
    (!sourcesStore.teamSources || sourcesStore.teamSources.length === 0)
  );
});

// Selected team name with better null handling
const selectedTeamName = computed(() => {
  if (isAllTeamsMode.value) return "All Teams";
  if (!teamsStore || !teamsStore.currentTeam) {
    return "Select a team";
  }
  return teamsStore.currentTeam.name || "Select a team";
});

const selectedSourceName = computed(() => {
  if (isAllSourcesMode.value) return "All Sources";
  if (!currentSelectedSource.value) return "Select a source";
  return formatSourceName(currentSelectedSource.value);
});

// Add this computed property near the other computed properties
const emptyStateMessage = computed(() =>
  searchQuery.value
    ? "No queries match your search."
    : "Create a query in the Explorer and save it to your collection."
);

onMounted(async () => {
  try {
    teamsStore.resetAdminTeams();
    
    // Check initial source param before initialization which might auto-select a source
    const initialSourceParam = route.query.source;
    
    await initializeContext();
    
    // If no source was specified in URL, enforce All Sources mode
    if (!initialSourceParam && contextStore.sourceId) {
      contextStore.sourceId = null;
      const q = { ...route.query };
      delete q.source;
      router.replace({ query: q });
    }
    
    if (contextError.value) {
      toast({
        title: "Error",
        description: contextError.value,
        variant: "destructive",
        duration: TOAST_DURATION.ERROR,
      });
    }
  } catch (error) {
    console.error("Error during SavedQueriesView mount:", error);
    toast({
      title: "Error",
      description: getErrorMessage(error),
      variant: "destructive",
      duration: TOAST_DURATION.ERROR,
    });
  }
});

watch(
  () => [contextReady.value, contextTeamId.value, contextSourceId.value] as const,
  async ([isReady, teamId, sourceId], oldValue) => {
    if (!isReady) return;
    if (!teamId) return;
    
    const [wasReady, , oldSourceId] = oldValue ?? [false, null, null];
    // Fetch queries when:
    // 1. Context just became ready (initial load)
    // 2. Source ID changed (user switched source)
    if (!wasReady || sourceId !== oldSourceId) {
      await fetchQueries();
    }
  },
  { immediate: true }
);

async function handleTeamChange(teamId: string) {
  try {
    // Handle "All Teams" selection
    if (teamId === "all") {
      isAllTeamsMode.value = true;
      // Clear stale team/source context so permission checks don't use the previous team
      contextStore.sourceId = null;
      localTeamQueries.value = [];
      const result = await savedQueriesStore.fetchMyCollections();
      if (result.success) {
        localTeamQueries.value = result.data ?? [];
      }
      return;
    }

    isAllTeamsMode.value = false;
    const teamIdNum = parseInt(teamId);
    if (isNaN(teamIdNum)) return;

    await contextHandleTeamChange(teamIdNum);

    // Default to All Sources when switching teams
    contextStore.sourceId = null;
    const query = { ...route.query };
    delete query.source;
    router.replace({ query });
    
    if (sourcesStore.teamSources.length === 0) {
      localTeamQueries.value = [];
    }
  } catch (error) {
    console.error("Error changing team:", error);
    toast({
      title: "Error",
      description: getErrorMessage(error),
      variant: "destructive",
      duration: TOAST_DURATION.ERROR,
    });
  }
}

async function handleSourceChange(sourceId: string) {
  try {
    // Handle All Sources selection
    if (!sourceId || sourceId === "all") {
      contextStore.sourceId = null;
      const query = { ...route.query };
      delete query.source;
      await router.replace({ query });
      
      // Manually trigger fetch if needed, though watcher should handle it
      // if sourceId was already null (e.g. clicking All Sources when already there)
      if (isAllSourcesMode.value) {
        await fetchQueries();
      }
      return;
    }

    const sourceIdNum = parseInt(sourceId);
    if (isNaN(sourceIdNum)) return;
    
    await contextHandleSourceChange(sourceIdNum);
  } catch (error) {
    console.error("Error changing source:", error);
    toast({
      title: "Error",
      description: getErrorMessage(error),
      variant: "destructive",
      duration: TOAST_DURATION.ERROR,
    });
  }
}

async function fetchQueries() {
  // All Teams mode — already fetched in handleTeamChange
  if (isAllTeamsMode.value) return;

  if (!contextTeamId.value) {
    console.warn("No team selected, cannot load queries");
    return;
  }

  // All Sources Mode
  if (isAllSourcesMode.value) {
    const result = await savedQueriesStore.fetchTeamCollections(contextTeamId.value);
    if (result.success) {
      localTeamQueries.value = result.data ?? [];
    } else {
      localTeamQueries.value = [];
    }
    return;
  }

  // Specific Source Mode
  if (!contextSourceId.value) return; // Should be covered by isAllSourcesMode check above

  const sourceExists = sourcesStore.teamSources.some(
    (source) => source.id === contextSourceId.value
  );

  if (!sourceExists) {
    console.warn(
      `Source ID ${contextSourceId.value} does not exist for team ${contextTeamId.value}, skipping query fetch`
    );
    return;
  }

  await loadSourceQueries(contextTeamId.value, contextSourceId.value);
}

// Format time using the formatDate utility
function formatTime(dateStr: string): string {
  return formatDate(dateStr);
}

// Handle delete query with refresh
async function handleDeleteQuery(query: SavedTeamQuery) {
  const result = await deleteQuery(query);
  if (result.success && contextSourceId.value) {
    await fetchQueries();
  }
}

// Handle save query modal submission - Now uses the function from the composable instance
async function handleSaveQuery(formData: SaveQueryFormData) {
  // Directly call the function obtained from the composable instance
  return await handleSaveQueryFromComposable(formData);
}

async function handleUpdateQuery(queryId: string, formData: SaveQueryFormData) {
  if (!contextTeamId.value || !contextSourceId.value) return;

  try {
    const result = await updateSavedQuery(
      contextTeamId.value!,
      contextSourceId.value!,
      queryId,
      {
        name: formData.name,
        description: formData.description,
        query_content: formData.query_content,
        query_type: formData.query_type as 'logchefql' | 'sql',
      }
    );

    if (result.success) {
      showSaveQueryModal.value = false;
      editingQuery.value = null;
      // Refresh the queries list
      await fetchQueries();
    }
  } catch (error) {
    console.error('Error updating query:', error);
  }
}

function handleCreateNewQuery() {
  createNewQuery(contextSourceId.value ?? undefined);
}

async function handleToggleBookmark(query: SavedTeamQuery) {
  const teamId = query.team_id || contextTeamId.value;
  const sourceId = query.source_id || contextSourceId.value;
  if (!teamId || !sourceId) return;

  const result = await savedQueriesStore.toggleBookmark(
    teamId,
    sourceId,
    query.id
  );

  if (result.success && result.data) {
    // Update the local query list to reflect the change
    if (localTeamQueries.value) {
      const index = localTeamQueries.value.findIndex((q) => q.id === query.id);
      if (index >= 0) {
        localTeamQueries.value[index].is_bookmarked = result.data.is_bookmarked;
      }
    }
  }
}

async function copyCollectionUrl(query: SavedTeamQuery) {
  const teamId = query.team_id || contextTeamId.value;
  const sourceId = query.source_id || contextSourceId.value;
  if (!teamId || !sourceId) return;

  const url = `${window.location.origin}/logs/collection/${teamId}/${sourceId}/${query.id}`;

  try {
    await navigator.clipboard.writeText(url);
    toast({
      title: "Link Copied",
      description: "Collection URL copied to clipboard",
      duration: TOAST_DURATION.SUCCESS,
    });
  } catch (error) {
    console.error("Failed to copy URL:", error);
    toast({
      title: "Error",
      description: "Failed to copy URL to clipboard",
      variant: "destructive",
      duration: TOAST_DURATION.ERROR,
    });
  }
}
</script>

<template>
  <div class="space-y-6">
    <Card>
      <CardHeader>
        <div class="flex flex-col gap-4 md:flex-row md:items-start md:justify-between">
          <div>
            <CardTitle>Collections</CardTitle>
            <CardDescription>
              Saved searches and SQL queries for quick reuse across your sources.
            </CardDescription>
          </div>
          <Button v-if="canManageCollections && !isAllTeamsMode" @click="handleCreateNewQuery">
            <Plus class="mr-2 h-4 w-4" />
            Add to Collection
          </Button>
        </div>
      </CardHeader>

      <CardContent class="space-y-6">
        <Alert v-if="contextError" variant="destructive">
          <AlertDescription>{{ contextError }}</AlertDescription>
        </Alert>

        <div v-if="showLoadingState" class="flex flex-col items-center justify-center py-16 gap-4">
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

        <template v-else-if="showEmptyState">
          <div class="flex flex-col gap-6">
            <div class="flex max-w-xs flex-col gap-2">
              <Label>Team</Label>
              <Select
                :model-value="contextTeamId ? contextTeamId.toString() : ''"
                @update:model-value="handleTeamChange"
              >
                <SelectTrigger>
                  <SelectValue placeholder="Select a team">
                    {{ selectedTeamName }}
                  </SelectValue>
                </SelectTrigger>
                <SelectContent>
                  <SelectItem
                    v-for="team in teamsStore.teams"
                    :key="team.id"
                    :value="team.id.toString()"
                  >
                    {{ team.name }}
                  </SelectItem>
                </SelectContent>
              </Select>
            </div>

            <div class="rounded-lg border p-6 text-center">
              <div class="mx-auto mb-4 flex h-14 w-14 items-center justify-center rounded-full bg-muted">
                <Plus class="h-6 w-6" />
              </div>
              <div class="space-y-2">
                <h2 class="text-lg font-semibold tracking-tight">
                  No Sources Found in {{ selectedTeamName }}
                </h2>
                <p class="text-muted-foreground">
                  This team does not have any log sources configured yet.
                </p>
              </div>

              <div class="mt-6 flex flex-wrap items-center justify-center gap-3">
                <Button @click="router.push({ name: 'NewSource' })">
                  <Plus class="mr-2 h-4 w-4" />
                  Add Source
                </Button>
                <Button variant="outline" v-if="teamsStore.teams.length > 1">
                  Try another team
                </Button>
              </div>
            </div>
          </div>
        </template>

        <template v-else>
          <div class="space-y-4">
            <div class="grid gap-4 xl:grid-cols-[minmax(0,220px)_minmax(0,260px)_minmax(0,1fr)] xl:items-end">
              <div class="space-y-2">
                <Label>Team</Label>
                <Select
                  :model-value="isAllTeamsMode ? 'all' : (contextTeamId ? contextTeamId.toString() : '')"
                  @update:model-value="handleTeamChange"
                  :disabled="contextLoading"
                >
                  <SelectTrigger>
                    <SelectValue placeholder="Select a team">
                      <span v-if="contextLoading">Loading...</span>
                      <span v-else>{{ selectedTeamName }}</span>
                    </SelectValue>
                  </SelectTrigger>
                  <SelectContent>
                    <SelectItem value="all">All Teams</SelectItem>
                    <SelectItem v-for="team in teamsStore.teams" :key="team.id" :value="team.id.toString()">
                      {{ team.name }}
                    </SelectItem>
                  </SelectContent>
                </Select>
              </div>

              <div class="space-y-2">
                <Label>Source</Label>
                <Select
                  :model-value="selectedSourceId"
                  @update:model-value="handleSourceChange"
                  :disabled="isAllTeamsMode || contextLoading || !contextTeamId || ((sourcesStore.teamSources || []).length === 0 && !isAllSourcesMode)"
                >
                  <SelectTrigger>
                    <span v-if="contextLoading">Loading...</span>
                    <span v-else>{{ selectedSourceName }}</span>
                  </SelectTrigger>
                  <SelectContent>
                    <SelectItem value="all">All Sources</SelectItem>
                    <SelectItem
                      v-for="source in sourcesStore.teamSources || []"
                      :key="source.id"
                      :value="String(source.id)"
                    >
                      {{ formatSourceName(source) }}
                    </SelectItem>
                  </SelectContent>
                </Select>
              </div>

              <div class="space-y-2">
                <Label>Search</Label>
                <div class="relative w-full">
                  <Search class="absolute left-3 top-2.5 h-4 w-4 text-muted-foreground" />
                  <Input
                    v-model="searchQuery"
                    type="search"
                    placeholder="Search collections by name or description..."
                    class="pl-9 pr-16"
                  />
                  <Button
                    v-if="searchQuery"
                    variant="ghost"
                    size="sm"
                    class="absolute right-1 top-1.5 h-7 px-2 text-xs"
                    @click="clearSearch"
                  >
                    Clear
                  </Button>
                </div>
              </div>
            </div>
          </div>

          <div v-if="isLoading" class="flex items-center justify-center py-10">
            <Loader2 class="h-8 w-8 animate-spin text-primary" />
            <p class="ml-2 text-muted-foreground">Loading collections...</p>
          </div>

          <div v-else-if="!hasQueries" class="rounded-lg border p-6 text-center">
            <div class="mx-auto mb-4 flex h-12 w-12 items-center justify-center rounded-full bg-muted/50">
              <Search class="h-5 w-5 text-muted-foreground" />
            </div>
            <h3 class="text-lg font-semibold mb-1">Collection is empty</h3>
            <p class="text-muted-foreground">{{ emptyStateMessage }}</p>
            <div class="mt-6 flex items-center justify-center gap-3">
              <Button v-if="searchQuery" variant="outline" @click="clearSearch">
                Clear Search
              </Button>
              <Button
                v-if="canManageCollections && !searchQuery && !isAllTeamsMode"
                @click="handleCreateNewQuery"
              >
                Add to Collection
              </Button>
            </div>
          </div>

          <div v-else class="space-y-3">
            <div class="text-sm text-muted-foreground">
              Showing {{ totalQueryCount }}
              {{ totalQueryCount === 1 ? "query" : "queries" }}
            </div>

            <div class="rounded-md border">
              <Table>
                <TableHeader>
                  <TableRow>
                    <TableHead class="w-[50px]"></TableHead>
                    <TableHead class="w-[250px]">Name</TableHead>
                    <TableHead v-if="isAllTeamsMode" class="w-[120px]">Team</TableHead>
                    <TableHead v-if="isAllSourcesMode" class="w-[150px]">Source</TableHead>
                    <TableHead>Description</TableHead>
                    <TableHead class="w-[100px]">Type</TableHead>
                    <TableHead class="w-[150px]">Created</TableHead>
                    <TableHead class="w-[150px]">Updated</TableHead>
                    <TableHead class="w-[100px] text-right">Actions</TableHead>
                  </TableRow>
                </TableHeader>
                <TableBody>
                  <TableRow
                    v-for="query in filteredQueries"
                    :key="query.id"
                    :class="{ 'bg-muted/50': openingQueryId === query.id }"
                  >
                    <TableCell class="w-[50px]">
                      <button
                        v-if="canManageCollections"
                        @click.stop="handleToggleBookmark(query)"
                        class="rounded p-1 transition-colors hover:bg-muted"
                        :title="query.is_bookmarked ? 'Remove bookmark' : 'Add bookmark'"
                      >
                        <Star
                          class="h-4 w-4 transition-transform hover:scale-110"
                          :class="query.is_bookmarked ? 'text-amber-500 fill-amber-500' : 'text-muted-foreground'"
                        />
                      </button>
                      <Star
                        v-else
                        class="h-4 w-4"
                        :class="query.is_bookmarked ? 'text-amber-500 fill-amber-500' : 'text-muted-foreground'"
                      />
                    </TableCell>
                    <TableCell class="font-medium">
                      <a
                        @click.prevent="openingQueryId === null && openQuery(query)"
                        :href="getQueryUrl(query)"
                        class="inline-flex items-center gap-2"
                        :class="[
                          openingQueryId === null
                            ? 'text-foreground hover:underline cursor-pointer'
                            : openingQueryId === query.id
                              ? 'text-foreground cursor-wait'
                              : 'text-muted-foreground cursor-not-allowed'
                        ]"
                      >
                        <Loader2
                          v-if="openingQueryId === query.id"
                          class="h-4 w-4 animate-spin"
                        />
                        {{ query.name }}
                      </a>
                    </TableCell>
                    <TableCell v-if="isAllTeamsMode">{{ query.team_name || `Team ${query.team_id}` }}</TableCell>
                    <TableCell v-if="isAllSourcesMode">{{ query.source_name || getSourceName(query.source_id) }}</TableCell>
                    <TableCell>{{ query.description || "-" }}</TableCell>
                    <TableCell>
                      <Badge :variant="query.query_type === 'logchefql' ? 'outline' : 'secondary'">
                        {{
                          query.query_type === "logchefql"
                            ? "Search"
                            : query.query_type === "sql"
                              ? "SQL"
                              : query.query_type
                        }}
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
                          <DropdownMenuItem
                            @click="openingQueryId === null && openQuery(query)"
                            :disabled="openingQueryId !== null"
                          >
                            <Loader2 v-if="openingQueryId === query.id" class="mr-2 h-4 w-4 animate-spin" />
                            <Eye v-else class="mr-2 h-4 w-4" />
                            {{ openingQueryId === query.id ? 'Opening...' : 'Open' }}
                          </DropdownMenuItem>
                          <DropdownMenuItem @click="copyCollectionUrl(query)">
                            <Link class="mr-2 h-4 w-4" />
                            Copy Link
                          </DropdownMenuItem>
                          <DropdownMenuItem v-if="canManageCollections" @click="editQuery(query)">
                            <Pencil class="mr-2 h-4 w-4" />
                            Edit
                          </DropdownMenuItem>
                          <DropdownMenuItem
                            v-if="canManageCollections"
                            @click="handleDeleteQuery(query)"
                            class="text-destructive"
                          >
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
          </div>

          <SaveQueryModal
            v-if="showSaveQueryModal && editingQuery"
            :is-open="showSaveQueryModal"
            :initial-data="editingQuery"
            :is-edit-mode="true"
            @close="showSaveQueryModal = false"
            @save="handleSaveQuery"
            @update="handleUpdateQuery"
          />
        </template>
      </CardContent>
    </Card>
  </div>
</template>
