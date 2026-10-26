<script setup lang="ts">
import { useI18n } from "vue-i18n";

import { ref, computed, watch } from 'vue';
import { ChevronDown, Save, PlusCircle, ListTree, BookMarked, Link } from 'lucide-vue-next';
import { useRouter } from 'vue-router';
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
import { useToast } from '@/composables/useToast';
import { TOAST_DURATION } from '@/lib/constants';
import { type SavedQuery } from '@/api/savedQueries';
import { useSavedQueriesStore } from '@/stores/savedQueries';
import { useExploreStore } from '@/stores/explore';
import { useAuthStore } from '@/stores/auth';
import { useSavedQueries } from '@/composables/useSavedQueries';

const { t } = useI18n();

const props = defineProps<{
  selectedTeamId?: number;
  selectedSourceId?: number;
}>();

const emit = defineEmits<{
  (e: 'save'): void;
  (e: 'select-saved-query', query: SavedQuery): void;
  (e: 'save-as-new'): void;
}>();

const router = useRouter();
const { toast } = useToast();
const savedQueriesStore = useSavedQueriesStore();
const exploreStore = useExploreStore();
const authStore = useAuthStore();

const {
  isEditingExistingQuery,
  canSaveQuery,
} = useSavedQueries();

const isOpen = ref(false);
const queries = computed(() => savedQueriesStore.queries);
const hasQueries = computed(() => queries.value.length > 0);

// Watch for changes in team/source ID
watch(
  () => [props.selectedTeamId, props.selectedSourceId],
  async ([teamId, sourceId]) => {
    if (teamId && sourceId) {
      await loadQueries(teamId, sourceId as number);
    }
  },
  { immediate: true }
);

// Load queries when dropdown opens
watch(isOpen, async (open) => {
  if (open && props.selectedSourceId && !queries.value.length) {
    await loadQueries(props.selectedTeamId, props.selectedSourceId);
  }
});

// Load queries for a source via the saved-queries store. teamId is no longer
// needed for the API call but is kept in the prop signature for callers that
// still pass it.
async function loadQueries(_teamId: number | undefined, sourceId: number) {
  if (!sourceId) return;

  try {
    await savedQueriesStore.list(sourceId);
  } catch (error) {
    console.error('Error triggering query load from store:', error);
    toast({
      get title() { return t('ui.error'); },
      get description() { return t('ui.failedToInitiateLoadingSavedQueries'); },
      variant: 'destructive',
      duration: TOAST_DURATION.ERROR,
    });
  }
}

// Handle query selection
function selectQuery(query: SavedQuery) {
  try {
    emit('select-saved-query', query);
    isOpen.value = false;
  } catch (error) {
    console.error('Error selecting query:', error);
    toast({
      get title() { return t('ui.error'); },
      get description() { return t('ui.failedToOpenTheSelectedQuery'); },
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

const isUserAuthenticated = computed(() => authStore.isAuthenticated);
const activeSavedQueryName = computed(() => exploreStore.activeSavedQueryName);

const navigateToCollectionsView = () => {
  router.push({
    path: '/logs/library',
    query: {},
  });
  isOpen.value = false;
};

// Copy shareable saved-query URL to clipboard
async function copyCollectionUrl(event: Event, query: SavedQuery) {
  event.stopPropagation();
  const url = new URL(router.resolve(`/logs/saved/${query.id}`).href, window.location.origin).href;

  try {
    await navigator.clipboard.writeText(url);
    toast({
      get title() { return t('ui.linkCopied'); },
      get description() { return t('ui.collectionURLCopiedToClipboard'); },
      duration: TOAST_DURATION.SUCCESS,
    });
  } catch (error) {
    console.error('Failed to copy URL:', error);
    toast({
      get title() { return t('ui.error'); },
      get description() { return t('ui.failedToCopyURLToClipboard'); },
      variant: 'destructive',
      duration: TOAST_DURATION.ERROR,
    });
  }
}
</script>

<template>
  <DropdownMenu v-if="isUserAuthenticated" v-model:open="isOpen">
    <DropdownMenuTrigger as-child>
      <Button variant="outline" size="sm" class="h-7 max-w-[200px] gap-1.5 text-xs">
        <Save class="w-3.5 h-3.5 flex-shrink-0" />
        <span class="truncate">
          {{ activeSavedQueryName || t('common.collections') }}
          <span v-if="activeSavedQueryName && exploreStore.hasDivergedFromSavedQuery" class="text-muted-foreground">{{ t('ui.unsaved') }}</span>
        </span>
        <ChevronDown class="w-3.5 h-3.5 flex-shrink-0" />
      </Button>
    </DropdownMenuTrigger>
    <DropdownMenuContent class="w-64" align="end">
      <DropdownMenuItem v-if="canSaveQuery" @click="handleSave">
        <Save class="w-4 h-4 mr-2" />
        <span>{{ isEditingExistingQuery ? t('ui.updateCurrentQuery') : t('ui.saveCurrentQueryToCollection') }}</span>
      </DropdownMenuItem>
      <DropdownMenuItem v-if="canSaveQuery && isEditingExistingQuery" @click="handleRequestSaveAsNew">
        <PlusCircle class="w-4 h-4 mr-2" />
        <span>{{ t('ui.saveAsNewQuery') }}</span>
      </DropdownMenuItem>

      <DropdownMenuSeparator v-if="canSaveQuery" />

      <DropdownMenuLabel v-if="hasQueries">{{ t('ui.loadFromCollection') }}</DropdownMenuLabel>
      <DropdownMenuSub v-if="hasQueries">
        <DropdownMenuSubTrigger>
          <ListTree class="w-4 h-4 mr-2" />
          <span>{{ t('queries.select', { count: queries.length }) }}</span>
        </DropdownMenuSubTrigger>
        <DropdownMenuSubContent class="max-h-96 overflow-y-auto">
          <DropdownMenuItem
            v-for="query in queries"
            :key="query.id"
            @click="() => selectQuery(query)"
            class="flex items-center justify-between gap-2"
          >
            <div class="flex items-center gap-2 flex-1 min-w-0">
              <span class="truncate" :title="query.name">{{ query.name }}</span>
            </div>
            <button
              @click="(e) => copyCollectionUrl(e, query)"
              class="p-0.5 rounded hover:bg-muted transition-colors flex-shrink-0"
              :title="t('ui.copyShareLink')"
            >
              <Link class="h-3.5 w-3.5 text-muted-foreground hover:text-foreground" />
            </button>
          </DropdownMenuItem>
        </DropdownMenuSubContent>
      </DropdownMenuSub>

      <DropdownMenuSeparator v-if="hasQueries" />

      <DropdownMenuItem @click="navigateToCollectionsView">
        <BookMarked class="w-4 h-4 mr-2" />
        <span>{{ t('ui.viewAllCollections') }}</span>
      </DropdownMenuItem>
    </DropdownMenuContent>
  </DropdownMenu>
</template>
