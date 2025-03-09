<script setup lang="ts">
import { ref, computed, onMounted, watch } from 'vue';
import { z } from 'zod';
import { useForm } from 'vee-validate';
import { toTypedSchema } from '@vee-validate/zod';
import { SaveIcon } from 'lucide-vue-next';
import { Button } from '@/components/ui/button';
import {
  Dialog,
  DialogContent,
  DialogDescription,
  DialogFooter,
  DialogHeader,
  DialogTitle,
} from '@/components/ui/dialog';
import { Input } from '@/components/ui/input';
import { Textarea } from '@/components/ui/textarea';
import { Select, SelectContent, SelectItem, SelectTrigger, SelectValue } from '@/components/ui/select';
import { Form, FormControl, FormDescription, FormField, FormItem, FormLabel, FormMessage } from '@/components/ui/form';
import { Loader2 } from 'lucide-vue-next';
import { useSavedQueriesStore } from '@/stores/savedQueries';
import { useTeamsStore } from '@/stores/teams';
import { useSourcesStore } from '@/stores/sources';
import { useExploreStore } from '@/stores/explore';
import { useToast } from '@/components/ui/toast';
import { TOAST_DURATION } from '@/lib/constants';
import { formatSourceName } from '@/utils/format';

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
const { toast } = useToast();

// Form schema
const formSchema = z.object({
  team_id: z.string().min(1, "Please select a team"),
  name: z.string().min(1, "Name is required").max(100, "Name must be at most 100 characters"),
  description: z.string().max(500, "Description must be at most 500 characters").optional(),
  query_content: z.string().min(1, "Query content is required"),
});

// Get the current source ID
const currentSourceId = computed(() => {
  // Try to get from explore store
  if (exploreStore.data.sourceId) {
    return exploreStore.data.sourceId;
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

// Get teams eligible for this source
const eligibleTeams = computed(() => {
  if (!currentSourceId.value) {
    return teamsStore.teams || [];
  }

  // Get the teams that have access to this source
  return sourcesStore.getTeamsForSource(currentSourceId.value);
});

// Select the best default team
const defaultTeamId = computed(() => {
  if (props.initialData?.team_id) {
    return props.initialData.team_id;
  }

  // If no teams are eligible, return empty
  if (eligibleTeams.value.length === 0) {
    return '';
  }

  // If the current team in teamsStore is eligible, use it
  if (teamsStore.currentTeam?.id &&
    eligibleTeams.value.some(t => t.id === teamsStore.currentTeam?.id)) {
    return teamsStore.currentTeam.id;
  }

  // Otherwise, use the first eligible team
  return eligibleTeams.value[0]?.id || '';
});

// Form setup
const form = useForm({
  validationSchema: toTypedSchema(formSchema),
  initialValues: {
    team_id: props.initialData?.team_id || defaultTeamId.value,
    name: props.initialData?.name || '',
    description: props.initialData?.description || '',
    query_content: props.initialData?.query_content || props.queryContent || '',
  },
});

// Watch for changes in initial data and prop values
watch(() => props.initialData, (newValue) => {
  if (newValue) {
    form.setValues({
      team_id: newValue.team_id || defaultTeamId.value,
      name: newValue.name || '',
      description: newValue.description || '',
      query_content: newValue.query_content || props.queryContent || '',
    });
  }
}, { deep: true });

watch(() => props.queryContent, (newValue) => {
  if (newValue) {
    form.setFieldValue('query_content', newValue);
  }
});

// Loading state
const isSubmitting = ref(false);

// Teams available for selection
const teams = computed(() => {
  // If editing, just return the current team
  if (props.isEditMode) {
    if (props.initialData?.team_id) {
      const team = eligibleTeams.value.find(t => t.id === props.initialData.team_id);
      return team ? [team] : [];
    }
    return [];
  }

  // Otherwise, return eligible teams
  return eligibleTeams.value;
});

// Format source name for display
const sourceName = computed(() => {
  if (!currentSourceId.value) return 'Unknown Source';

  const source = sourcesStore.deduplicatedSources.find(s => s.id === currentSourceId.value);
  if (!source) return 'Unknown Source';

  const { database, table_name } = source.connection;
  return `${database}.${table_name}`;
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
});

// Handle form submission
async function onSubmit(values: any) {
  try {
    isSubmitting.value = true;
    emit('save', values);
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

      <Form :form="form" class="space-y-4" @submit="onSubmit">
        <!-- Source Information (non-editable) -->
        <div class="border rounded-md p-3 bg-muted/20">
          <div class="text-sm font-medium">Source</div>
          <div class="flex items-center justify-between mt-1">
            <div class="text-sm text-muted-foreground">
              {{ sourceName }}
            </div>
            <div class="text-xs px-2 py-0.5 rounded-full bg-primary/10 text-primary">
              {{ eligibleTeams.length }} {{ eligibleTeams.length === 1 ? 'team' : 'teams' }} available
            </div>
          </div>
        </div>

        <!-- Team Selection -->
        <FormField v-slot="{ field }" name="team_id">
          <FormItem>
            <FormLabel>Team</FormLabel>
            <FormControl>
              <Select :disabled="isEditMode || teams.length <= 1" :model-value="field.value"
                @update:model-value="(value) => field.onChange(value)">
                <SelectTrigger>
                  <SelectValue placeholder="Select a team" />
                </SelectTrigger>
                <SelectContent>
                  <SelectItem v-for="team in teams" :key="team.id" :value="String(team.id)">
                    {{ team.name }}
                  </SelectItem>
                </SelectContent>
              </Select>
            </FormControl>
            <FormDescription>
              Select which team this query will be shared with
            </FormDescription>
            <FormMessage />
          </FormItem>
        </FormField>

        <!-- Query Name -->
        <FormField v-slot="{ field }" name="name">
          <FormItem>
            <FormLabel>Name</FormLabel>
            <FormControl>
              <Input v-bind="field" placeholder="Enter a descriptive name" />
            </FormControl>
            <FormMessage />
          </FormItem>
        </FormField>

        <!-- Description -->
        <FormField v-slot="{ field }" name="description">
          <FormItem>
            <FormLabel>Description (Optional)</FormLabel>
            <FormControl>
              <Textarea v-bind="field" placeholder="Provide details about this query" rows="3" />
            </FormControl>
            <FormDescription>
              Briefly describe the purpose of this query.
            </FormDescription>
            <FormMessage />
          </FormItem>
        </FormField>
      </Form>

      <DialogFooter>
        <Button type="button" variant="outline" @click="handleClose">Cancel</Button>
        <Button type="submit" :disabled="isSubmitting" @click="form.handleSubmit(onSubmit)">
          <SaveIcon v-if="!isSubmitting" class="mr-2 h-4 w-4" />
          <Loader2 v-else class="mr-2 h-4 w-4 animate-spin" />
          {{ isEditMode ? 'Update' : 'Save' }}
        </Button>
      </DialogFooter>
    </DialogContent>
  </Dialog>
</template>