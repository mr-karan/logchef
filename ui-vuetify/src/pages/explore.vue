<template>
  <v-container fluid>
    <!-- Top Controls Bar -->
    <v-row class="mb-4">
      <v-col cols="12">
        <v-card>
          <v-card-text>
            <v-row align="center">
              <!-- Source Selection -->
              <v-col cols="12" md="3">
                <v-select
                  v-model="selectedSourceId"
                  :items="sources"
                  item-title="connection.table_name"
                  item-value="id"
                  label="Select Source"
                  :loading="loading"
                  :disabled="loading"
                  @update:model-value="handleSourceChange"
                />
              </v-col>

              <!-- Time Range Selection -->
              <v-col cols="12" md="6">
                <DateTimePicker
                  v-model="timeRange"
                  @update:model-value="handleTimeRangeChange"
                />
              </v-col>
            </v-row>
          </v-card-text>
        </v-card>
      </v-col>
    </v-row>

    <!-- Content Area -->
    <v-row>
      <v-col cols="12">
        <v-card>
          <v-card-text class="text-center py-12">
            <v-icon
              size="64"
              color="grey"
              class="mb-4"
            >
              mdi-table-search
            </v-icon>
            <div class="text-h6 text-medium-emphasis">
              Log Table Coming Soon
            </div>
            <div class="text-body-1 text-medium-emphasis">
              Advanced log exploration and visualization features are under development
            </div>
          </v-card-text>
        </v-card>
      </v-col>
    </v-row>
  </v-container>
</template>

<script lang="ts" setup>
import { ref, onMounted } from 'vue'
import { startOfHour, subHours } from 'date-fns'
import DateTimePicker from '@/components/DateTimePicker.vue'
import { useNotification } from '@/composables/useNotification'
import sourcesApi, { type Source } from '@/api/sources'

const notification = useNotification()

// Sources state
const sources = ref<Source[]>([])
const selectedSourceId = ref<string>('')
const selectedSource = ref<Source | null>(null)
const loading = ref(false)

// Time range state
const timeRange = ref({
  start: subHours(startOfHour(new Date()), 1),
  end: new Date()
})

// Handle time range changes
const handleTimeRangeChange = (range: { start: Date; end: Date }) => {
  timeRange.value = range
  // Here you can trigger log fetching with the new time range
  console.log('Time range changed:', range)
}

// Fetch source details when source selection changes
const handleSourceChange = async (sourceId: string) => {
  if (!sourceId) return
  
  try {
    loading.value = true
    const source = await sourcesApi.getSource(sourceId)
    selectedSource.value = source
  } catch (error) {
    notification.error('Failed to fetch source details')
    console.error('Error fetching source:', error)
  } finally {
    loading.value = false
  }
}

// Initialize component
onMounted(async () => {
  try {
    loading.value = true
    sources.value = await sourcesApi.listSources()
    
    // Select first source by default if available
    if (sources.value.length > 0) {
      selectedSourceId.value = sources.value[0].id
      await handleSourceChange(selectedSourceId.value)
    }
  } catch (error) {
    notification.error('Failed to fetch sources')
    console.error('Error fetching sources:', error)
  } finally {
    loading.value = false
  }
})
</script>
