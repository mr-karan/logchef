<script setup lang="ts">
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
import { Loader2 } from 'lucide-vue-next';
import { useSavedQueriesStore } from '@/stores/savedQueries';
import { useTeamsStore } from '@/stores/teams';
import { useSourcesStore } from '@/stores/sources';
import { useExploreStore } from '@/stores/explore';
import { useRoute } from 'vue-router';
import { TOAST_DURATION } from '@/lib/constants';
import { useToast } from '@/components/ui/toast';

const props = defineProps<{
  isOpen: boolean;
  initialData?: any; // For creating a new query
  editData?: any;    // For editing an existing query
  queryContent?: string;
  isEditMode?: boolean;
}>();

const route = useRoute();

const emit = defineEmits<{
  (e: 'close'): void;
  (e: 'save', data: any): void;
  (e: 'update', queryId: string, data: any): void;
}>();

const savedQueriesStore = useSavedQueriesStore();
const teamsStore = useTeamsStore();
const sourcesStore = useSourcesStore();
const exploreStore = useExploreStore();
const { toast } = useToast();

// Form state
const name = ref('');
const description = ref('');
const saveTimestamp = ref(true);
const isSubmitting = ref(false);
const isEditing = computed(() => !!props.editData || (!!props.initialData && props.isEditMode));
const queryId = ref('');

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

// Get current team name
const currentTeamName = computed(() => {
  if (!teamsStore.currentTeamId) return '';

  const team = teamsStore.teams.find(t => t.id === teamsStore.currentTeamId);
  return team ? team.name : '';
});

// Get source name for display
const sourceName = computed(() => {
  if (!currentSourceId.value) return '';

  // Find the source in the sources list
  const source = sourcesStore.teamSources.find(s => s.id === currentSourceId.value);
  // Return the source name if available, otherwise fallback to table_name
  return source ? (source.name || source.connection.table_name) : '';
});

// Form validation
const isValid = computed(() => {
  return !!name.value.trim();
});

// Load teams and sources on mount if needed
onMounted(async () => {
  const promises = [];

  if (!savedQueriesStore.data.teams.length) {
    promises.push(savedQueriesStore.fetchUserTeams());
  }

  if (currentSourceId.value && !sourcesStore.teamSources.length) {
    // Load teams first if needed
    if (!teamsStore.teams.length) {
      promises.push(teamsStore.loadTeams());
    }

    // Then load sources for the current team
    if (teamsStore.currentTeamId) {
      promises.push(sourcesStore.loadTeamSources(teamsStore.currentTeamId));
    }
  }

  if (promises.length > 0) {
    await Promise.all(promises);
  }

  // Initialize form with initial data if provided
  if (props.initialData) {
    name.value = props.initialData.name || '';
    description.value = props.initialData.description || '';
    
    // If we're editing, attempt to parse the query content for better UX
    try {
      const content = JSON.parse(props.initialData.query_content);
      
      // Set save timestamp based on whether timeRange exists in the data
      if (content.timeRange && content.timeRange.absolute) {
        saveTimestamp.value = true;
      }
    } catch (e) {
      console.error("Failed to parse query content from initialData", e);
    }
  }
  
  // Also check URL parameters for editing existing query
  const queryId = route.query.query_id;
  if (queryId && !props.initialData) {
    console.log(`Editing query ID ${queryId} from URL parameters`);
  }
});

// Watch for changes in initialData or editData
watch([() => props.initialData, () => props.editData], ([newInitialData, newEditData]) => {
  if (newEditData) {
    // Editing existing query takes precedence
    name.value = newEditData.name || '';
    description.value = newEditData.description || '';
    queryId.value = newEditData.id?.toString() || '';
    
    try {
      const content = JSON.parse(newEditData.query_content);
      if (content.timeRange && content.timeRange.absolute) {
        saveTimestamp.value = true;
      }
    } catch (e) {
      console.error("Failed to parse query content from editData in watcher", e);
    }
  } else if (newInitialData) {
    // Creating new query
    name.value = newInitialData.name || '';
    description.value = newInitialData.description || '';
    queryId.value = '';
    
    try {
      const content = JSON.parse(newInitialData.query_content);
      if (content.timeRange && content.timeRange.absolute) {
        saveTimestamp.value = true;
      }
    } catch (e) {
      console.error("Failed to parse query content from initialData in watcher", e);
    }
  }
}, { deep: true });

// Prepare query content with proper structure
function prepareQueryContent(saveTimestamp: boolean): string {
  try {
    // Get the current active mode from the explore store
    const activeMode = exploreStore.activeMode || 'sql';

    // Get initial content if available
    let content: Record<string, any> = {};
    if (props.queryContent) {
      try {
        content = JSON.parse(props.queryContent);
      } catch (e) {
        console.error("Failed to parse provided query content", e);
      }
    } else if (props.initialData?.query_content) {
      try {
        content = JSON.parse(props.initialData.query_content);
      } catch (e) {
        console.error("Failed to parse initial query content", e);
      }
    }

    // Get the query content based on mode
    const queryContent = activeMode === 'logchefql' 
      ? exploreStore.logchefqlCode || ''
      : exploreStore.rawSql || '';
      
    // Validate query content
    if (!queryContent.trim()) {
      throw new Error(`${activeMode === 'logchefql' ? 'LogchefQL' : 'SQL'} content is required`);
    }

    // Time values setup
    const currentTime = Date.now();
    const oneHourAgo = currentTime - 3600000;
    
    // If saving timestamp, use the selected time range from explore store
    // Otherwise, use a default 1-hour range
    let timeRange = {
      absolute: {
        start: oneHourAgo,
        end: currentTime
      }
    };

    if (saveTimestamp && exploreStore.timeRange) {
      try {
        // Convert DateValue objects to timestamps
        const startDate = new Date(
          exploreStore.timeRange.start.year,
          exploreStore.timeRange.start.month - 1,
          exploreStore.timeRange.start.day,
          'hour' in exploreStore.timeRange.start ? exploreStore.timeRange.start.hour : 0,
          'minute' in exploreStore.timeRange.start ? exploreStore.timeRange.start.minute : 0,
          'second' in exploreStore.timeRange.start ? exploreStore.timeRange.start.second : 0
        );
        
        const endDate = new Date(
          exploreStore.timeRange.end.year,
          exploreStore.timeRange.end.month - 1,
          exploreStore.timeRange.end.day,
          'hour' in exploreStore.timeRange.end ? exploreStore.timeRange.end.hour : 0,
          'minute' in exploreStore.timeRange.end ? exploreStore.timeRange.end.minute : 0,
          'second' in exploreStore.timeRange.end ? exploreStore.timeRange.end.second : 0
        );
        
        timeRange.absolute.start = startDate.getTime();
        timeRange.absolute.end = endDate.getTime();
      } catch (e) {
        console.warn('Error parsing time range, using default:', e);
      }
    }

    // Create simplified structure
    const simplifiedContent = {
      version: content.version || 1,
      sourceId: content.sourceId || currentSourceId.value,
      timeRange: timeRange,
      limit: saveTimestamp ? exploreStore.limit : 100,
      content: queryContent,
    };

    return JSON.stringify(simplifiedContent);
  } catch (error) {
    console.error('Error preparing query content:', error);
    
    // Fallback to a minimal valid structure
    const currentTime = Date.now();
    const oneHourAgo = currentTime - 3600000;
    
    // Default values for timeRange
    let timeRange = {
      absolute: {
        start: oneHourAgo,
        end: currentTime
      }
    };
    
    // If saving timestamp and timeRange is available, use it
    if (saveTimestamp && exploreStore.timeRange) {
      try {
        const startDate = new Date(
          exploreStore.timeRange.start.year,
          exploreStore.timeRange.start.month - 1,
          exploreStore.timeRange.start.day,
          'hour' in exploreStore.timeRange.start ? exploreStore.timeRange.start.hour : 0,
          'minute' in exploreStore.timeRange.start ? exploreStore.timeRange.start.minute : 0,
          'second' in exploreStore.timeRange.start ? exploreStore.timeRange.start.second : 0
        );
        
        const endDate = new Date(
          exploreStore.timeRange.end.year,
          exploreStore.timeRange.end.month - 1,
          exploreStore.timeRange.end.day,
          'hour' in exploreStore.timeRange.end ? exploreStore.timeRange.end.hour : 0,
          'minute' in exploreStore.timeRange.end ? exploreStore.timeRange.end.minute : 0,
          'second' in exploreStore.timeRange.end ? exploreStore.timeRange.end.second : 0
        );
        
        timeRange.absolute.start = startDate.getTime();
        timeRange.absolute.end = endDate.getTime();
      } catch (e) {
        console.warn('Error parsing time range in fallback, using default:', e);
      }
    }
    
    return JSON.stringify({
      version: 1,
      sourceId: currentSourceId.value,
      timeRange: timeRange,
      limit: saveTimestamp ? exploreStore.limit : 100,
      content: exploreStore.activeMode === 'logchefql' ? 
        (exploreStore.logchefqlCode || '') : 
        (exploreStore.rawSql || ''),
    });
  }
}

// Handle form submission
async function handleSubmit(event: Event) {
  event.preventDefault();

  if (!isValid.value) {
    return;
  }

  try {
    isSubmitting.value = true;

    // Get the current active mode from the explore store for query_type
    const activeMode = exploreStore.activeMode || 'sql';

    // Determine query type - this is separate from the content structure
    const queryType = activeMode === 'logchefql' ? 'logchefql' : 'sql';

    try {
      // Prepare the query content with the proper structure
      const preparedContent = prepareQueryContent(saveTimestamp.value);

      // Create the base payload
      const payload = {
        team_id: teamsStore.currentTeamId?.toString() || '',
        source_id: currentSourceId.value,
        name: name.value,
        description: description.value,
        query_content: preparedContent,
        query_type: queryType,
        save_timestamp: saveTimestamp.value
      };

      if (isEditing.value && queryId.value) {
        // We're updating an existing query
        console.log(`Updating existing query ID: ${queryId.value}`);
        emit('update', queryId.value, payload);
      } else {
        // We're creating a new query
        console.log('Creating new query');
        emit('save', payload);
      }
    } catch (contentError) {
      console.error('Error preparing query content:', contentError);
      toast({
        title: 'Error',
        description: 'Failed to prepare query content',
        variant: 'destructive',
        duration: TOAST_DURATION.ERROR
      });
      emit('close');
      throw contentError;
    }
  } catch (error) {
    console.error('SaveQueryModal: Error in handleSubmit:', error);
    // The parent component will handle showing the error toast
  } finally {
    isSubmitting.value = false;
  }
}

// Close the modal
function handleClose() {
  emit('close');
}

// Add computed properties for the descriptions
const editDescription = 'Update details for this saved query.'
const saveDescription = 'Save your current query configuration for future use.'
</script>

<template>
  <Dialog :open="isOpen" @update:open="(val) => !val && handleClose()">
    <DialogContent class="sm:max-w-[475px]">
      <DialogHeader>
        <DialogTitle>
          <span v-if="isEditing" class="flex items-center">
            <Pencil class="h-4 w-4 mr-2" />
            Edit Saved Query
          </span>
          <span v-else class="flex items-center">
            <SaveIcon class="h-4 w-4 mr-2" />
            Save New Query
          </span>
        </DialogTitle>
        <DialogDescription>
          {{ isEditing ? editDescription : saveDescription }}
        </DialogDescription>
      </DialogHeader>

      <form @submit="handleSubmit" class="space-y-4">
        <!-- Source and Team Information (non-editable) -->
        <div class="border rounded-md p-3 bg-muted/20">
          <div class="grid grid-cols-2 gap-4">
            <!-- Team Information -->
            <div>
              <div class="text-sm font-medium">Team</div>
              <div class="text-sm text-muted-foreground mt-1">
                {{ currentTeamName }}
              </div>
            </div>

            <!-- Source Information -->
            <div>
              <div class="text-sm font-medium">Source</div>
              <div class="text-sm text-muted-foreground mt-1">
                {{ sourceName }}
              </div>
            </div>
          </div>
        </div>
        
        <!-- Query Content Preview -->
        <div class="border rounded-md p-3">
          <div class="text-sm font-medium mb-2">
            {{ exploreStore.activeMode === 'logchefql' ? 'LogchefQL' : 'SQL' }} Query
          </div>
          <pre class="text-xs bg-muted p-2 rounded overflow-auto max-h-[120px] whitespace-pre-wrap break-all">{{ exploreStore.activeMode === 'logchefql' ? exploreStore.logchefqlCode : exploreStore.rawSql }}</pre>
        </div>

        <!-- Query Name -->
        <div class="grid gap-2">
          <Label for="name" class="required">Name</Label>
          <Input id="name" v-model="name" placeholder="Enter a descriptive name" required />
        </div>

        <!-- Description -->
        <div class="grid gap-2">
          <Label for="description">Description (Optional)</Label>
          <Textarea id="description" v-model="description" placeholder="Provide details about this query" rows="3" />
          <p class="text-sm text-muted-foreground">
            Briefly describe the purpose of this query.
          </p>
        </div>

        <!-- Save Timestamp Checkbox -->
        <div class="flex items-start space-x-3 space-y-0 rounded-md border p-4">
          <Checkbox id="save_timestamp" v-model="saveTimestamp" />
          <div class="space-y-1 leading-none">
            <Label for="save_timestamp">Save current timestamp</Label>
            <p class="text-sm text-muted-foreground">
              Include the current time range and limit in the saved query.
            </p>
          </div>
        </div>

        <div class="flex justify-end space-x-4 pt-4">
          <Button type="button" variant="outline" @click="handleClose">Cancel</Button>
          <Button type="submit" :disabled="isSubmitting || !isValid">
            <SaveIcon v-if="!isSubmitting" class="mr-2 h-4 w-4" />
            <Loader2 v-else class="mr-2 h-4 w-4 animate-spin" />
            {{ isEditing ? 'Update Query' : 'Save Query' }}
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
