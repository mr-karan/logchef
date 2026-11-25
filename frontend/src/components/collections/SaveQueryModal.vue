<script setup lang="ts">
import { useI18n } from "vue-i18n";
import { getErrorMessage } from '@/api/types';
import { isAbortError } from '@/api/error-handler';

import { ref, computed, onMounted, watch } from 'vue';
import { SaveIcon, Pencil, ChevronDown, ChevronRight, UserPlus, ExternalLink, Loader2 } from 'lucide-vue-next';
import { Button } from '@/components/ui/button';
import {
  Dialog,
  DialogContent,
  DialogDescription,
  DialogHeader,
  DialogTitle,
} from '@/components/ui/dialog';
import { Input } from '@/components/ui/input';
import { Textarea } from '@/components/ui/textarea';
import { Checkbox } from '@/components/ui/checkbox';
import { Label } from '@/components/ui/label';
import {
  Select,
  SelectContent,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from '@/components/ui/select';
import { SearchableSelect } from '@/components/ui/searchable-select';
import { useSavedQueriesStore } from '@/stores/savedQueries';
import { useCollectionsStore } from '@/stores/collections';
import { useAuthStore } from '@/stores/auth';
import { useTeamsStore } from '@/stores/teams';
import { useSourcesStore } from '@/stores/sources';
import { asClickHouseConnection } from '@/api/sources';
import { useExploreStore } from '@/stores/explore';
import { useVariableStore } from '@/stores/variables';
import { useCollectionInvitableUsers } from '@/composables/useCollectionInvitableUsers';
import { TOAST_DURATION } from '@/lib/constants';
import { useToast } from '@/composables/useToast';
import { storeToRefs } from "pinia";
import { getExploreModeForQueryLanguage, getQueryLanguageLabel, resolveSavedQueryMetadata, type ExploreMode } from '@/lib/queryMetadata';
import type { SavedQueryTimeRange } from '@/api/savedQueries';
import { encodeStoredRange } from '@/utils/timeWindowCodec';
import { basePath } from '@/utils/basePath';

const { t } = useI18n();

const props = defineProps<{
  isOpen: boolean;
  editData?: any;    // For editing an existing query
  queryContent?: string;
  queryType?: ExploreMode;
}>();

const emit = defineEmits<{
  (e: 'close'): void;
  (e: 'save', data: any, done?: () => void): void;
  (e: 'update', queryId: string, data: any, done?: () => void): void;
}>();

const savedQueriesStore = useSavedQueriesStore();
const teamsStore = useTeamsStore();
const sourcesStore = useSourcesStore();
const exploreStore = useExploreStore();
const variableStore = useVariableStore();
const collectionsStore = useCollectionsStore();
const authStore = useAuthStore();
const { toast } = useToast();
const {
  canListAllUsers,
  canInviteToCollection,
  isLoading: isLoadingCandidates,
  loadCandidates,
  availableUserItems,
} = useCollectionInvitableUsers();

// Form state
const name = ref('');
const description = ref('');
const saveTimestamp = ref(true);
// Tracks whether the user actually interacted with the "Save current
// timestamp" checkbox during this modal session, as opposed to it merely
// reflecting the initial auto-derived state from the query being edited.
// When editing an existing query purely to change its name/description, we
// must not silently overwrite the saved time range/limit with whatever the
// (unrelated) live explore store currently happens to hold - only do that
// when the user explicitly opted in by touching this control.
const saveTimestampUserModified = ref(false);
function onToggleSaveTimestamp(value: boolean | 'indeterminate') {
  saveTimestamp.value = value === true;
  saveTimestampUserModified.value = true;
}

// Progressive disclosure state
const showQueryPreview = ref(false);
const showMoreOptions = ref(false);
const showManageEditors = ref(false);
const newEditorUserId = ref('');
const isAddingEditor = ref(false);

// Inline save-to-collection (new queries only); defaults to the personal collection.
const selectedCollectionId = ref<string>(
  collectionsStore.personalCollection ? String(collectionsStore.personalCollection.id) : '',
);
const collectionOptions = computed(() => {
  const personal = collectionsStore.personalCollection;
  // The store holds only collections the caller takes part in, and any
  // participant may pin.
  const shared = collectionsStore.sharedCollections;
  return personal ? [personal, ...shared] : shared;
});
const selectedCollection = computed(() =>
  collectionsStore.collections.find((c) => String(c.id) === selectedCollectionId.value) ?? null,
);
// The saver is the query's creator, so a shared collection may get a managed
// link, which delegates edit to the collection's owners and editors. On by
// default; a personal collection takes pins only and shows no toggle.
const editorsCanEdit = ref(true);
const isEditing = computed(() => !!props.editData);
const showsEditorsToggle = computed(() => !isEditing.value && !!selectedCollection.value && !selectedCollection.value.is_personal);
const isSelectedCollectionOwner = computed(() =>
  !!selectedCollection.value && !selectedCollection.value.is_personal && selectedCollection.value.caller_role === 'owner',
);
const canInviteToSelected = computed(() => canInviteToCollection(selectedCollection.value));
const selectedCollectionMembers = computed(() =>
  selectedCollection.value ? (collectionsStore.data?.members?.[selectedCollection.value.id] ?? []) : [],
);
const selectedCollectionEditors = computed(() =>
  selectedCollectionMembers.value.filter((m) => m.role === 'owner' || m.role === 'editor'),
);
const hasOtherEditors = computed(() => {
  const me = authStore.user?.id;
  return selectedCollectionEditors.value.some((m) => me == null || String(m.user_id) !== String(me));
});
const eligibleEditorItems = computed(() => availableUserItems(selectedCollectionMembers.value));
const isLoadingEditors = computed(() =>
  !!selectedCollection.value && (
    collectionsStore.isLoadingOperation?.(`listMembers-${selectedCollection.value.id}`) || isLoadingCandidates.value
  ),
);
const selectedCollectionMembersUrl = computed(() => {
  if (!selectedCollection.value) return `${basePath}logs/library`;
  return `${basePath}logs/library/${selectedCollection.value.id}?section=members`;
});

watch(selectedCollectionId, () => {
  showManageEditors.value = false;
  newEditorUserId.value = '';
});

async function toggleManageEditors() {
  if (!selectedCollection.value || !isSelectedCollectionOwner.value) return;
  showManageEditors.value = !showManageEditors.value;
  if (!showManageEditors.value) return;
  const id = selectedCollection.value.id;
  const tasks: Promise<unknown>[] = [];
  if (typeof collectionsStore.fetchMembers === 'function') {
    tasks.push(collectionsStore.fetchMembers(id));
  }
  if (canInviteToSelected.value) {
    tasks.push(loadCandidates());
  }
  if (tasks.length > 0) {
    await Promise.allSettled(tasks);
  }
}

async function handleAddEditor() {
  if (!selectedCollection.value || !newEditorUserId.value || isAddingEditor.value) return;
  const userId = Number(newEditorUserId.value);
  if (!userId) return;
  isAddingEditor.value = true;
  try {
    const result = await collectionsStore.addMember(selectedCollection.value.id, {
      user_id: userId,
      role: 'editor',
    });
    if (result?.success) {
      newEditorUserId.value = '';
    }
  } finally {
    isAddingEditor.value = false;
  }
}

function translateRole(role: string) {
  switch (role) {
    case 'owner': return t('library.roleOwner');
    case 'editor': return t('library.roleEditor');
    default: return t('library.roleMember');
  }
}

const isSubmitting = ref(false);
const queryId = ref('');
const { allVariables } = storeToRefs(variableStore);

const currentTeamId = computed(() => {
  return teamsStore.currentTeamId;
});

// Get the current source ID
const currentSourceId = computed(() => {
  // Try to get from explore store
  if (exploreStore.sourceId) {
    return exploreStore.sourceId;
  }

  // If query content is provided, try to parse it
  if (props.queryContent) {
    try {
      const content = JSON.parse(props.queryContent);
      if (content.sourceId) {
        return content.sourceId;
      }
    } catch (e) {
      console.error("Failed to parse query content", e);
    }
  }

  return '';
});

// Get source name for display
const sourceName = computed(() => {
  if (props.editData?.source_name) return props.editData.source_name;
  if (!currentSourceId.value) return '';

  // Find the source in the sources list
  const source = sourcesStore.teamSources.find(s => s.id === currentSourceId.value);
  // Return the source name if available, otherwise fallback to table_name (ClickHouse only)
  return source ? (source.name || asClickHouseConnection(source.connection)?.table_name || '') : '';
});

const currentSourceDescriptor = computed(() => {
  const currentSource = sourcesStore.teamSources.find(s => s.id === currentSourceId.value);
  return currentSource || sourcesStore.currentSourceDetails || null;
});

// Form validation
const isValid = computed(() => {
  return !!name.value.trim();
});

const resolvedQueryMetadata = computed(() =>
  resolveSavedQueryMetadata({
    active_mode: props.queryType,
    query_language: props.editData?.query_language,
    editor_mode: props.editData?.editor_mode,
    source_type: currentSourceDescriptor.value?.source_type,
    query_languages: currentSourceDescriptor.value?.query_languages,
    saved_query_editor_modes: currentSourceDescriptor.value?.saved_query_editor_modes,
  })
);

const displayQueryMode = computed(() => {
  if (props.queryType) {
    return props.queryType;
  }
  return getExploreModeForQueryLanguage(resolvedQueryMetadata.value.queryLanguage);
});

const displayQueryLanguageLabel = computed(() => getQueryLanguageLabel(resolvedQueryMetadata.value.queryLanguage));

const headerContextLabel = computed(() =>
  sourceName.value
    ? t('library.saveQueryContext', { source: sourceName.value, language: displayQueryLanguageLabel.value })
    : displayQueryLanguageLabel.value,
);

const moreOptionsSummary = computed(() => {
  if (!saveTimestamp.value) return t('library.moreOptionsTimeRangeOff');
  return t('library.moreOptionsSummary');
});

// Get the query content for display - prioritize props then editData
const displayQueryContent = computed(() => {
  // First, try to get content from queryContent prop if provided (e.g. from Explorer)
  // This is the CURRENT editor content, so it takes priority over saved data
  if (props.queryContent) {
    try {
      const content = JSON.parse(props.queryContent);
      if (content.content !== undefined) {
        return content.content;
      }
    } catch (e) {
      console.error("Failed to parse queryContent prop for display", e);
    }
  }

  // Then, try to get content from editData (when editing existing query from Collections view)
  if (props.editData?.query_content) {
    try {
      const content = JSON.parse(props.editData.query_content);
      if (content.content !== undefined) {
        return content.content;
      }
    } catch (e) {
      console.error("Failed to parse editData query content for display", e);
    }
  }
  
  // Fall back to exploreStore (when creating new query from Explorer and props not passed)
  const activeMode = exploreStore.activeMode || "native";
  return activeMode === "logchefql" 
    ? exploreStore.logchefqlCode || ""
    : exploreStore.nativeQuery || "";
});

// Load teams and sources on mount if needed
onMounted(async () => {
  const promises = [];

  if (!savedQueriesStore.data.teams.length) {
    promises.push(savedQueriesStore.fetchUserTeams());
  }

  if (!collectionsStore.collections.length) {
    promises.push(collectionsStore.fetchCollections());
  }

  if (currentSourceId.value && !sourcesStore.teamSources.length) {
    // Load teams first if needed
    if (!teamsStore.teams.length) {
      promises.push(teamsStore.loadTeams());
    }

    // Then load sources for the current team
    if (currentTeamId.value) {
      promises.push(sourcesStore.loadTeamSources(currentTeamId.value));
    }
  }

  if (promises.length > 0) {
    const results = await Promise.allSettled(promises);
    const failure = results.find(result => result.status === 'rejected' && !isAbortError(result.reason));
    if (failure?.status === 'rejected') {
      toast({ title: t('ui.error'), description: getErrorMessage(failure.reason), variant: 'destructive', duration: TOAST_DURATION.ERROR });
    }
  }

  // Default the collection picker to the user's personal collection.
  if (!selectedCollectionId.value && collectionsStore.personalCollection) {
    selectedCollectionId.value = String(collectionsStore.personalCollection.id);
  }
});

function initFromProps() {
  const data = props.editData;
  name.value = data?.name || '';
  description.value = data?.description || '';
  queryId.value = isEditing.value ? data?.id?.toString() || '' : '';
  saveTimestampUserModified.value = false;
  saveTimestamp.value = !data;
  if (data?.query_content) {
    try {
      const content = JSON.parse(data.query_content);
      saveTimestamp.value = !!(content.timeRange?.absolute || content.timeRange?.relative);
    } catch (error) {
      console.error("Failed to parse saved query content", error);
    }
  }
}

watch(
  () => props.editData,
  initFromProps,
  { immediate: true },
);

// Prepare query content with proper structure
function prepareQueryContent(saveTimestamp: boolean): string {
  try {
    // Use displayQueryMode to get the correct mode (from editData when editing)
    const activeMode = displayQueryMode.value;

    // Get initial content if available
    let content: Record<string, any> = {};
    if (props.queryContent) {
      try {
        content = JSON.parse(props.queryContent);
      } catch (e) {
        console.error("Failed to parse provided query content", e);
      }
    } else if (props.editData?.query_content) {
      try {
        content = JSON.parse(props.editData.query_content);
      } catch (e) {
        console.error("Failed to parse edit query content", e);
      }
    }

    // `content` above resolves from props.queryContent FIRST when present -
    // and the Explorer always passes queryContent (the live editor state,
    // which has no timeRange/limit of its own). That makes `content.timeRange`
    // /`content.limit` useless for "what was actually persisted" once a query
    // is loaded in the Explorer. The metadata-only path below needs the
    // *actual saved record*, so parse editData directly here,
    // independent of the queryContent prop.
    let originalSavedContent: Record<string, any> = {};
    if (props.editData?.query_content) {
      try {
        originalSavedContent = JSON.parse(props.editData.query_content);
      } catch (e) {
        console.error("Failed to parse edit query content", e);
      }
    }

    // Use displayQueryContent which handles fallback from editData
    const queryContent = displayQueryContent.value;

    if (!queryContent.trim()) {
      throw new Error(`${activeMode === 'logchefql' ? 'LogchefQL' : displayQueryLanguageLabel.value} content is required`);
    }

    // Metadata-only update path: when editing an existing query, only pull
    // time range/limit from the live explore store if the user explicitly
    // opted in via the "Save current timestamp" checkbox this session.
    // Otherwise (e.g. just fixing the name/description), keep whatever was
    // already saved untouched - the live explore store may hold an unrelated
    // or stale time range/limit that has nothing to do with this edit.
    const useLiveExploreState = !isEditing.value || saveTimestampUserModified.value;

    // A preserved range keeps the version it was written with, so v1
    // browser-field ms are never read back as instants.
    let stored: { version: number; timeRange: SavedQueryTimeRange | null } = { version: 2, timeRange: null };
    if (saveTimestamp) {
      if (useLiveExploreState && exploreStore.hasWindow) {
        // Use current explore store time range (user is editing from Explorer)
        stored = encodeStoredRange(exploreStore.window);
      } else {
        // Keep the originally saved time range: editing from Collections, or a
        // metadata-only edit (from the persisted record, not the live-editor `content`).
        const original = useLiveExploreState ? content : originalSavedContent;
        if (original.timeRange) {
          stored = { version: typeof original.version === 'number' ? original.version : 1, timeRange: original.timeRange };
        }
      }
    }

    const limitValue = useLiveExploreState
      ? (exploreStore.limit || content.limit || 100)
      : (typeof originalSavedContent.limit === 'number' ? originalSavedContent.limit : 100);

    const simplifiedContent = {
      version: stored.version,
      sourceId: content.sourceId || currentSourceId.value,
      timeRange: stored.timeRange,
      limit: limitValue,
      content: queryContent,
      variables: allVariables.value?.length ? allVariables.value : (content.variables || []),
    };

    return JSON.stringify(simplifiedContent);
  } catch (error) {
    console.error('Error preparing query content:', error);

    const fallback = saveTimestamp && exploreStore.hasWindow ? encodeStoredRange(exploreStore.window) : { version: 2, timeRange: null };

    return JSON.stringify({
      version: fallback.version,
      sourceId: currentSourceId.value,
      timeRange: fallback.timeRange,
      limit: exploreStore.limit || 100,
      content: displayQueryContent.value || '',
      variables: allVariables?.value || []
    });
  }
}

// Handle form submission
async function handleSubmit(event: Event) {
  event.preventDefault();

  if (isSubmitting.value || !isValid.value) {
    return;
  }

  try {
    isSubmitting.value = true;
    const done = () => {
      isSubmitting.value = false;
    };

    const queryMetadata = resolvedQueryMetadata.value;

    try {
      // Prepare the query content with the proper structure
      const preparedContent = prepareQueryContent(saveTimestamp.value);

      const payload = {
        source_id: currentSourceId.value,
        created_from_team_id: currentTeamId.value ?? null,
        name: name.value,
        description: description.value,
        query_content: preparedContent,
        query_language: queryMetadata.queryLanguage,
        editor_mode: queryMetadata.editorMode,
        save_timestamp: saveTimestamp.value,
        // Pin new queries to the chosen collection in one step. Omitted on edit.
        collection_id: !isEditing.value && selectedCollectionId.value ? Number(selectedCollectionId.value) : null,
        collection_kind: showsEditorsToggle.value && editorsCanEdit.value ? 'managed' : 'pin',
      };


      if (isEditing.value && queryId.value) {
        // We're updating an existing query
        emit('update', queryId.value, payload, done);
      } else {
        // We're creating a new query
        emit('save', payload, done);
      }
    } catch (contentError) {
      console.error('Error preparing query content:', contentError);
      toast({
        get title() { return t('ui.error'); },
        get description() { return t('ui.failedToPrepareQueryContent'); },
        variant: 'destructive',
        duration: TOAST_DURATION.ERROR
      });
      emit('close');
      isSubmitting.value = false;
      throw contentError;
    }
  } catch (error) {
    // The parent component will handle showing the error toast
  }
}

// Close the modal
function handleClose() {
  emit('close');
}
</script>

<template>
  <Dialog :open="isOpen" @update:open="(val) => !val && handleClose()">
    <DialogContent class="sm:max-w-[460px] max-h-[calc(100dvh-2rem)] flex flex-col gap-0 p-0 overflow-hidden">
      <DialogHeader class="shrink-0 border-b px-6 pt-5 pb-3.5 pr-12 text-left">
        <DialogTitle>
          <span v-if="isEditing" class="flex items-center">
            <Pencil class="h-4 w-4 mr-2" />
            {{ t('ui.editSavedQuery') }}
          </span>
          <span v-else class="flex items-center">
            <SaveIcon class="h-4 w-4 mr-2" />
            {{ t('ui.saveQuery') }}
          </span>
        </DialogTitle>
        <div class="mt-1 flex items-center justify-between gap-2">
          <DialogDescription class="truncate text-xs text-muted-foreground">
            {{ headerContextLabel }}
          </DialogDescription>
          <button
            v-if="displayQueryContent"
            type="button"
            data-testid="toggle-query-preview"
            :aria-expanded="showQueryPreview"
            class="inline-flex shrink-0 items-center gap-1 rounded px-1.5 py-0.5 text-xs text-muted-foreground transition-colors hover:bg-muted hover:text-foreground"
            @click="showQueryPreview = !showQueryPreview"
          >
            {{ showQueryPreview ? t('library.hideQuery') : t('library.previewQuery') }}
            <ChevronDown class="h-3.5 w-3.5 transition-transform" :class="showQueryPreview && 'rotate-180'" />
          </button>
        </div>
      </DialogHeader>

      <form @submit="handleSubmit" class="flex min-h-0 flex-1 flex-col">
        <div class="flex-1 min-h-0 overflow-y-auto px-6 py-4 space-y-4">
          <!-- Expandable Query Preview -->
          <pre
            v-if="showQueryPreview && displayQueryContent"
            data-testid="query-preview"
            class="max-h-28 overflow-auto rounded-md border bg-muted/50 p-2.5 font-mono text-xs whitespace-pre-wrap break-all"
          >{{ displayQueryContent }}</pre>

          <!-- Query Name -->
          <div class="grid gap-1.5">
            <Label for="name" class="required">{{ t('ui.name') }}</Label>
            <Input id="name" v-model="name" :placeholder="t('ui.enterADescriptiveName')" required autofocus />
          </div>

          <!-- Collection picker (new queries only) -->
          <div v-if="!isEditing" class="grid gap-2">
            <div class="grid gap-1.5">
              <Label for="collection">{{ t('library.collection') }}</Label>
              <Select v-model="selectedCollectionId">
                <SelectTrigger id="collection" data-testid="collection-select">
                  <SelectValue :placeholder="t('ui.chooseACollection')" />
                </SelectTrigger>
                <SelectContent>
                  <SelectItem v-for="c in collectionOptions" :key="c.id" :value="String(c.id)">
                    {{ c.name }}<span v-if="c.is_personal" class="ml-1 text-xs text-muted-foreground">{{ t('ui.personal') }}</span>
                  </SelectItem>
                </SelectContent>
              </Select>
            </div>

            <!-- Shared-collection editing permission + inline owner editor management -->
            <div v-if="showsEditorsToggle" class="rounded-md border bg-muted/25 px-3 py-2.5 space-y-2.5">
              <div class="flex items-start justify-between gap-2">
                <div class="flex items-start gap-2.5 min-w-0">
                  <Checkbox
                    id="editors_can_edit"
                    class="mt-0.5"
                    :model-value="editorsCanEdit"
                    @update:model-value="(value) => (editorsCanEdit = value === true)"
                  />
                  <div class="space-y-0.5 leading-snug">
                    <Label for="editors_can_edit" class="cursor-pointer text-sm font-medium">
                      {{ t('library.editorsCanEdit') }}
                    </Label>
                    <p class="text-xs text-muted-foreground">
                      <template v-if="editorsCanEdit">
                        {{ isSelectedCollectionOwner
                          ? t('library.editorsCanEditOwnerHint')
                          : t('library.editorsCanEditParticipantHint') }}
                      </template>
                      <template v-else>
                        {{ t('library.editorsCannotEditHint') }}
                      </template>
                    </p>
                  </div>
                </div>
                <Button
                  v-if="isSelectedCollectionOwner"
                  type="button"
                  variant="ghost"
                  size="sm"
                  class="h-7 shrink-0 gap-1 px-2 text-xs text-muted-foreground hover:text-foreground"
                  data-testid="manage-editors-toggle"
                  :aria-expanded="showManageEditors"
                  @click="toggleManageEditors"
                >
                  <UserPlus class="h-3.5 w-3.5" />
                  {{ t('library.manageEditors') }}
                </Button>
              </div>

              <!-- Inline editor roster & quick add for collection owners -->
              <div
                v-if="isSelectedCollectionOwner && showManageEditors"
                data-testid="manage-editors-panel"
                class="border-t pt-2.5 space-y-2.5"
              >
                <div class="space-y-1.5">
                  <p class="text-xs font-medium text-foreground">{{ t('library.currentEditors') }}</p>
                  <div v-if="isLoadingEditors" class="flex items-center gap-1.5 text-xs text-muted-foreground">
                    <Loader2 class="h-3.5 w-3.5 animate-spin" />
                    <span>{{ t('library.loadingEditors') }}</span>
                  </div>
                  <template v-else>
                    <ul v-if="selectedCollectionEditors.length" class="flex flex-wrap gap-1.5">
                      <li
                        v-for="m in selectedCollectionEditors"
                        :key="m.user_id"
                        class="inline-flex items-center gap-1 rounded-md border bg-background px-2 py-0.5 text-xs"
                      >
                        <span class="truncate max-w-[160px] font-medium">{{ m.full_name || m.email || t('pages.userNumber', { id: m.user_id }) }}</span>
                        <span class="text-muted-foreground">· {{ translateRole(m.role) }}</span>
                      </li>
                    </ul>
                    <p v-if="!hasOtherEditors" class="text-xs text-muted-foreground">
                      {{ t('library.onlyYouCanEdit') }}
                    </p>
                  </template>
                </div>

                <div v-if="canInviteToSelected" class="space-y-1.5">
                  <div class="flex items-center gap-2">
                    <SearchableSelect
                      v-model="newEditorUserId"
                      :items="eligibleEditorItems"
                      :placeholder="t('library.selectEditorToAdd')"
                      :search-placeholder="t('access.searchUsers')"
                      :empty-text="t('access.noUsersAvailable')"
                      trigger-class="h-8 text-xs"
                      class="min-w-0 flex-1"
                    />
                    <Button
                      type="button"
                      size="sm"
                      variant="secondary"
                      class="h-8 shrink-0 text-xs"
                      data-testid="add-editor-submit"
                      :disabled="!newEditorUserId || isAddingEditor"
                      @click="handleAddEditor"
                    >
                      <Loader2 v-if="isAddingEditor" class="mr-1 h-3.5 w-3.5 animate-spin" />
                      {{ t('ui.add') }}
                    </Button>
                  </div>
                  <p v-if="!canListAllUsers" class="text-[11px] text-muted-foreground">
                    {{ t('library.inviteTeammatesHint') }}
                  </p>
                </div>

                <div class="flex items-center justify-end">
                  <a
                    :href="selectedCollectionMembersUrl"
                    target="_blank"
                    rel="noopener noreferrer"
                    data-testid="open-collection-members-link"
                    class="inline-flex items-center gap-1 text-xs font-medium text-muted-foreground transition-colors hover:text-foreground hover:underline"
                  >
                    {{ t('library.manageMembersInLibrary') }}
                    <ExternalLink class="h-3 w-3" />
                  </a>
                </div>
              </div>
            </div>
          </div>

          <!-- Progressive disclosure trigger on create; edit mode shows fields directly -->
          <button
            v-if="!isEditing"
            type="button"
            data-testid="more-options-toggle"
            :aria-expanded="showMoreOptions"
            class="flex w-full items-center justify-between rounded-md px-1 py-1 text-left text-xs text-muted-foreground transition-colors hover:text-foreground"
            @click="showMoreOptions = !showMoreOptions"
          >
            <span class="inline-flex items-center gap-1.5 font-medium">
              <ChevronRight class="h-3.5 w-3.5 transition-transform" :class="showMoreOptions && 'rotate-90'" />
              {{ t('library.moreOptions') }}
            </span>
            <span v-if="!showMoreOptions" class="truncate text-muted-foreground/80">
              {{ moreOptionsSummary }}
            </span>
          </button>

          <div v-if="isEditing || showMoreOptions" class="space-y-4">
            <!-- Description -->
            <div class="grid gap-1.5">
              <Label for="description">{{ t('ui.descriptionOptional') }}</Label>
              <Textarea id="description" v-model="description" :placeholder="t('ui.provideDetailsAboutThisQuery')" rows="2" />
            </div>

            <!-- Save Timestamp Checkbox -->
            <div class="flex items-start gap-2.5">
              <Checkbox
                id="save_timestamp"
                class="mt-0.5"
                :model-value="saveTimestamp"
                @update:model-value="onToggleSaveTimestamp"
              />
              <div class="space-y-0.5 leading-snug">
                <Label for="save_timestamp" class="cursor-pointer text-sm font-normal">
                  {{ t('library.saveTimeRangeAndLimit') }}
                </Label>
                <p class="text-xs text-muted-foreground">
                  {{ t('ui.includeTheCurrentTimeRangeAndLimitInTheSavedQuery') }}
                </p>
              </div>
            </div>
          </div>
        </div>

        <div class="shrink-0 flex items-center justify-end gap-2 border-t bg-background px-6 py-3.5">
          <Button type="button" variant="outline" @click="handleClose">{{ t('ui.cancel') }}</Button>
          <Button type="submit" :disabled="isSubmitting || !isValid">
            <SaveIcon v-if="!isSubmitting" class="mr-2 h-4 w-4" />
            <Loader2 v-else class="mr-2 h-4 w-4 animate-spin" />
            {{ isEditing ? t('ui.updateQuery') : t('ui.saveQuery') }}
          </Button>
        </div>
      </form>
    </DialogContent>
  </Dialog>
</template>

<style scoped>
.required::after {
  content: " *";
  color: hsl(var(--destructive));
}
</style>
