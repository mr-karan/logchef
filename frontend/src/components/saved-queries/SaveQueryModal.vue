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

    // If we have initial content, try to parse it first
    let content: Record<string, any> = {};
    if (props.initialData?.query_content) {
      try {
        content = JSON.parse(props.initialData.query_content);
      } catch (e) {
        // Not valid JSON, create a new object
        console.error("Failed to parse initial query content", e);
      }
    } else if (props.queryContent) {
      try {
        content = JSON.parse(props.queryContent);
      } catch (e) {
        // Not valid JSON, create a new object
        console.error("Failed to parse provided query content", e);
      }
    }

    // Get the appropriate query content based on mode
    let queryContent = '';

    if (activeMode === 'logchefql') {
      // For LogchefQL mode, use the current logchefQL code
      queryContent = exploreStore.logchefqlCode || '';

      // Ensure we have LogchefQL content
      if (!queryContent.trim()) {
        throw new Error("LogchefQL content is required for LogchefQL query type");
      }
    } else {
      // For SQL mode, use the current raw SQL
      queryContent = exploreStore.rawSql || '';

      // Ensure we have SQL content
      if (!queryContent.trim()) {
        throw new Error("SQL content is required for SQL query type");
      }
    }

    // Get current time values for timestamps
    const currentTime = Date.now();
    const oneHourAgo = currentTime - 3600000;

    // Get time range from explore store or use defaults, ensuring positive values
    let startTime = oneHourAgo;
    if (exploreStore.timeRange && exploreStore.timeRange.start) {
      try {
        const parsedTime = new Date(exploreStore.timeRange.start.toString()).getTime();
        // Ensure it's a valid positive timestamp (at least 1ms since epoch)
        startTime = parsedTime > 0 ? parsedTime : Math.max(1, oneHourAgo);
      } catch (e) {
        console.warn('Error parsing start time, using default:', e);
        startTime = Math.max(1, oneHourAgo);
      }
    }

    let endTime = currentTime;
    if (exploreStore.timeRange && exploreStore.timeRange.end) {
      try {
        const parsedTime = new Date(exploreStore.timeRange.end.toString()).getTime();
        // Ensure it's a valid positive timestamp (at least 1ms since epoch)
        endTime = parsedTime > 0 ? parsedTime : Math.max(1, currentTime);
      } catch (e) {
        console.warn('Error parsing end time, using default:', e);
        endTime = Math.max(1, currentTime);
      }
    }
    
    // Final safety check - ensure times are positive
    startTime = Math.max(1, startTime);
    endTime = Math.max(1, endTime);

    // Create simplified structure
    const simplifiedContent = {
      version: content.version || 1,
      sourceId: content.sourceId || currentSourceId.value,
      // Always include timeRange with valid timestamps that are non-null positive values
      timeRange: {
        absolute: {
          start: saveTimestamp ? Math.max(1, startTime || oneHourAgo) : Math.max(1, oneHourAgo),
          end: saveTimestamp ? Math.max(1, endTime || currentTime) : Math.max(1, currentTime),
        }
      },
      // Always include limit
      limit: saveTimestamp ? (content.limit || exploreStore.limit) : 100,
      content: queryContent,
    };

    return JSON.stringify(simplifiedContent);
  } catch (error) {
    console.error('Error preparing query content:', error);
    // Return a minimal valid structure with guaranteed positive non-null timestamps
    const currentTime = Date.now();
    const oneHourAgo = currentTime - 3600000;
    
    // Ensure startTime is a positive value (at least 1 ms since epoch)
    const safeStartTime = saveTimestamp && exploreStore.timeRange
      ? Math.max(1, new Date(exploreStore.timeRange.start.toString()).getTime() || oneHourAgo)
      : Math.max(1, oneHourAgo);
      
    // Ensure endTime is a positive value (at least 1 ms since epoch)
    const safeEndTime = saveTimestamp && exploreStore.timeRange
      ? Math.max(1, new Date(exploreStore.timeRange.end.toString()).getTime() || currentTime)
      : Math.max(1, currentTime);
      
    return JSON.stringify({
      version: 1,
      sourceId: currentSourceId.value,
      timeRange: {
        absolute: {
          start: safeStartTime,
          end: safeEndTime,
        }
      },
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
