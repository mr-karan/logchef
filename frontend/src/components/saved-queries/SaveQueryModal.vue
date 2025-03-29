<script setup lang="ts">
import { ref, computed, onMounted } from 'vue';
import { SaveIcon } from 'lucide-vue-next';
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

const props = defineProps<{
  isOpen: boolean;
  initialData?: any;
  queryContent?: string;
  isEditMode?: boolean;
}>();

const emit = defineEmits<{
  (e: 'close'): void;
  (e: 'save', data: any): void;
}>();

const savedQueriesStore = useSavedQueriesStore();
const teamsStore = useTeamsStore();
const sourcesStore = useSourcesStore();
const exploreStore = useExploreStore();

// Form state
const name = ref(props.initialData?.name || '');
const description = ref(props.initialData?.description || '');
const saveTimestamp = ref(true);
const isSubmitting = ref(false);

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
  }
});

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

      // Create the payload
      const payload = {
        team_id: teamsStore.currentTeamId?.toString() || '',
        name: name.value,
        description: description.value,
        query_content: preparedContent,
        query_type: queryType,
        save_timestamp: saveTimestamp.value
      };

      emit('save', payload);
    } catch (contentError) {
      console.error('Error preparing query content:', contentError);
      // Close the modal and show error in parent component
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
        <DialogTitle>{{ isEditMode ? 'Edit Saved Query' : 'Save Query' }}</DialogTitle>
        <DialogDescription>{{ isEditMode ? editDescription : saveDescription }}</DialogDescription>
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
            {{ isEditMode ? 'Update' : 'Save' }}
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
