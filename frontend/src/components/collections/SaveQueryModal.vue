<script setup lang="ts">
import { useI18n } from "vue-i18n";

import { ref, computed, onMounted, watch } from 'vue';
import { SaveIcon, Pencil } from 'lucide-vue-next';
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
import { Loader2 } from 'lucide-vue-next';
import { useSavedQueriesStore } from '@/stores/savedQueries';
import { useCollectionsStore } from '@/stores/collections';
import { useTeamsStore } from '@/stores/teams';
import { useSourcesStore } from '@/stores/sources';
import { asClickHouseConnection } from '@/api/sources';
import { useExploreStore } from '@/stores/explore';
import { useVariableStore } from '@/stores/variables';
import { useRoute } from 'vue-router';
import { TOAST_DURATION } from '@/lib/constants';
import { useToast } from '@/composables/useToast';
import { storeToRefs } from "pinia";
import { getExploreModeForQueryLanguage, getQueryLanguageLabel, resolveSavedQueryMetadata, type ExploreMode } from '@/lib/queryMetadata';
import type { SavedQueryTimeRange } from '@/api/savedQueries';
import { encodeStoredRange } from '@/utils/timeWindowCodec';

const { t } = useI18n();

const props = defineProps<{
  isOpen: boolean;
  initialData?: any; // For creating a new query
  editData?: any;    // For editing an existing query
  queryContent?: string;
  isEditMode?: boolean;
  queryType?: ExploreMode;
}>();

const route = useRoute();

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
const { toast } = useToast();

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
// Inline save-to-collection (new queries only); defaults to the personal collection.
const selectedCollectionId = ref<string>('');
const collectionOptions = computed(() => {
  const personal = collectionsStore.personalCollection;
  // Adding an item is owner-only on the backend, so only offer collections the
  // caller actually owns — otherwise the save succeeds but the pin 403s silently.
  const shared = collectionsStore.sharedCollections.filter((c) => c.caller_role === "owner");
  return personal ? [personal, ...shared] : shared;
});
const isSubmitting = ref(false);
const isEditing = computed(() => !!props.editData || (!!props.initialData && props.isEditMode));
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

  // If we have initial data, try to parse it
  if (props.initialData?.query_content) {
    try {
      const content = JSON.parse(props.initialData.query_content);
      if (content.sourceId) {
        return content.sourceId;
      }
    } catch (e) {
      console.error("Failed to parse query content", e);
    }
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
  if (props.initialData?.source_name) return props.initialData.source_name;
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
    query_language: props.editData?.query_language || props.initialData?.query_language,
    editor_mode: props.editData?.editor_mode || props.initialData?.editor_mode,
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

// Get the query content for display - prioritize props then editData/initialData
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
  
  // Then try initialData (when editing with initialData + isEditMode from Collections)
  if (props.initialData?.query_content) {
    try {
      const content = JSON.parse(props.initialData.query_content);
      if (content.content !== undefined) {
        return content.content;
      }
    } catch (e) { 
       console.error("Failed to parse initialData query content for display", e);
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
    await Promise.all(promises);
  }

  // Default the collection picker to the user's personal collection.
  if (!selectedCollectionId.value && collectionsStore.personalCollection) {
    selectedCollectionId.value = String(collectionsStore.personalCollection.id);
  }
});

function initFromProps() {
  const data = props.editData || props.initialData;
  name.value = data?.name || '';
  description.value = data?.description || '';
  queryId.value = isEditing.value ? data?.id?.toString() || '' : '';
  if (!data) queryId.value = route.query.id?.toString() || '';
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
  [() => props.initialData, () => props.editData, () => props.isEditMode, () => props.isOpen],
  initFromProps,
  { immediate: true },
);

// Prepare query content with proper structure
function prepareQueryContent(saveTimestamp: boolean): string {
  try {
    // Use displayQueryMode to get the correct mode (from editData/initialData when editing)
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
    } else if (props.initialData?.query_content) {
      try {
        content = JSON.parse(props.initialData.query_content);
      } catch (e) {
        console.error("Failed to parse initial query content", e);
      }
    }

    // `content` above resolves from props.queryContent FIRST when present -
    // and the Explorer always passes queryContent (the live editor state,
    // which has no timeRange/limit of its own). That makes `content.timeRange`
    // /`content.limit` useless for "what was actually persisted" once a query
    // is loaded in the Explorer. The metadata-only path below needs the
    // *actual saved record*, so parse editData/initialData directly here,
    // independent of the queryContent prop.
    let originalSavedContent: Record<string, any> = {};
    if (props.editData?.query_content) {
      try {
        originalSavedContent = JSON.parse(props.editData.query_content);
      } catch (e) {
        console.error("Failed to parse edit query content", e);
      }
    } else if (props.initialData?.query_content) {
      try {
        originalSavedContent = JSON.parse(props.initialData.query_content);
      } catch (e) {
        console.error("Failed to parse initial query content", e);
      }
    }

    // Use displayQueryContent which handles fallback from editData/initialData
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

// Add computed properties for the descriptions
const editDescription = 'Update this saved query.'
const saveDescription = 'Save this query for reuse. Collections can organize it afterward.'
</script>

<template>
  <Dialog :open="isOpen" @update:open="(val) => !val && handleClose()">
    <DialogContent class="sm:max-w-[475px]">
      <DialogHeader>
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
        <DialogDescription>
          {{ isEditing ? editDescription : saveDescription }}
        </DialogDescription>
      </DialogHeader>

      <form @submit="handleSubmit" class="space-y-4">
        <!-- Source information (non-editable) -->
        <div class="border rounded-md p-3 bg-muted/20">
          <div>
            <div class='text-sm font-medium'>{{ t('ui.source') }}</div>
            <div class="text-sm text-muted-foreground mt-1">
              {{ sourceName }}
            </div>
          </div>
        </div>

        <!-- Query Content Preview -->
        <div class="border rounded-md p-3">
          <div class="text-sm font-medium mb-2">
            {{ displayQueryLanguageLabel }} {{ t('ui.query') }}
          </div>
          <pre
            class="text-xs bg-muted p-2 rounded overflow-auto max-h-[120px] whitespace-pre-wrap break-all">{{ displayQueryContent }}</pre>
        </div>

        <!-- Query Name -->
        <div class="grid gap-2">
          <Label for="name" class="required">{{ t('ui.name') }}</Label>
          <Input id="name" v-model="name" :placeholder="t('ui.enterADescriptiveName')" required />
        </div>

        <!-- Description -->
        <div class="grid gap-2">
          <Label for="description">{{ t('ui.descriptionOptional') }}</Label>
          <Textarea id="description" v-model="description" :placeholder="t('ui.provideDetailsAboutThisQuery')" rows="3" />
          <p class="text-sm text-muted-foreground">
            {{ t('ui.brieflyDescribeThePurposeOfThisQuery') }}
          </p>
        </div>

        <!-- Add to collection (new queries only) -->
        <div v-if="!isEditing" class="grid gap-2">
          <Label>{{ t('ui.addToCollection2') }}</Label>
          <Select v-model="selectedCollectionId">
            <SelectTrigger>
              <SelectValue :placeholder="t('ui.chooseACollection')" />
            </SelectTrigger>
            <SelectContent>
              <SelectItem v-for="c in collectionOptions" :key="c.id" :value="String(c.id)">
                {{ c.name }}<span v-if="c.is_personal" class="ml-1 text-xs text-muted-foreground">{{ t('ui.personal') }}</span>
              </SelectItem>
            </SelectContent>
          </Select>
          <p class="text-sm text-muted-foreground">
            {{ t('ui.savesTheQueryAndAddsItToThisCollectionInOneStep') }}
          </p>
        </div>

        <!-- Save Timestamp Checkbox -->
        <div class="flex items-start space-x-3 space-y-0 rounded-md border p-4">
          <Checkbox id="save_timestamp" :model-value="saveTimestamp" @update:model-value="onToggleSaveTimestamp" />
          <div class="space-y-1 leading-none">
            <Label for="save_timestamp">{{ t('ui.saveCurrentTimestamp') }}</Label>
            <p class="text-sm text-muted-foreground">
              {{ t('ui.includeTheCurrentTimeRangeAndLimitInTheSavedQuery') }}
            </p>
          </div>
        </div>

        <div class="flex justify-end space-x-4 pt-4">
          <Button type="button" variant='outline' @click="handleClose">{{ t('ui.cancel') }}</Button>
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
