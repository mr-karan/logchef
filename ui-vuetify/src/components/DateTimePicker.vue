<template>
  <v-menu v-model="menu" :close-on-content-click="false">
    <template v-slot:activator="{ props }">
      <v-text-field
        v-bind="props"
        :model-value="displayText"
        label="Select time range"
        prepend-icon="mdi-clock-outline"
        readonly
        :error-messages="errorMessage"
      />
    </template>

    <v-card min-width="800">
      <v-row no-gutters>
        <!-- Left side: Date picker -->
        <v-col cols="6">
          <v-card-text class="pa-2">
            <div class="text-subtitle-1 mb-2">Start Date</div>
            <v-date-picker
              :model-value="startDate"
              @update:model-value="updateStartDate"
              color="primary"
              :max="endDate"
            />
          </v-card-text>
        </v-col>

        <!-- Right side: Time picker -->
        <v-col cols="6" class="border-l">
          <v-card-text class="pa-2">
            <div class="text-subtitle-1 mb-2">Start Time</div>
            <v-time-picker
              v-model="startTime"
              format="24hr"
              @update:model-value="updateStartTime"
            />
          </v-card-text>
        </v-col>

        <!-- Divider -->
        <v-col cols="12">
          <v-divider />
        </v-col>

        <!-- Left side: Date picker -->
        <v-col cols="6">
          <v-card-text class="pa-2">
            <div class="text-subtitle-1 mb-2">End Date</div>
            <v-date-picker
              :model-value="endDate"
              @update:model-value="updateEndDate"
              color="primary"
              :min="startDate"
            />
          </v-card-text>
        </v-col>

        <!-- Right side: Time picker -->
        <v-col cols="6" class="border-l">
          <v-card-text class="pa-2">
            <div class="text-subtitle-1 mb-2">End Time</div>
            <v-time-picker
              v-model="endTime"
              format="24hr"
              @update:model-value="updateEndTime"
            />
          </v-card-text>
        </v-col>
      </v-row>

      <v-divider />

      <!-- Quick presets -->
      <v-card-text>
        <div class="d-flex gap-2">
          <v-btn
            v-for="preset in presets"
            :key="preset.label"
            variant="outlined"
            size="small"
            @click="applyPreset(preset)"
          >
            {{ preset.label }}
          </v-btn>
        </div>
      </v-card-text>

      <v-card-actions>
        <v-spacer />
        <v-btn variant="text" @click="menu = false">Cancel</v-btn>
        <v-btn color="primary" @click="applySelection">Apply</v-btn>
      </v-card-actions>
    </v-card>
  </v-menu>
</template>

<script setup lang="ts">
import { ref, computed, watch } from 'vue'
import { format, startOfHour, subHours, startOfDay, endOfDay, parseISO, isValid } from 'date-fns'

const props = defineProps<{
  modelValue: { start: Date; end: Date } | null
}>()

const emit = defineEmits(['update:modelValue'])

// UI state
const menu = ref(false)
const tab = ref('date')
const errorMessage = ref<string | null>(null)

// Computed range with validation
const range = computed({
  get() {
    return props.modelValue || {
      start: subHours(startOfHour(new Date()), 1),
      end: new Date()
    }
  },
  set(value) {
    emit('update:modelValue', value)
  }
})

// Display text for the field
const displayText = computed(() => {
  if (!range.value?.start || !range.value?.end) return 'Select time range'
  return `${format(range.value.start, 'MMM dd, yyyy HH:mm')} - ${format(range.value.end, 'MMM dd, yyyy HH:mm')}`
})

// Preset ranges
const presets = [
  {
    label: 'Last 1 hour',
    getRange: () => ({
      start: subHours(startOfHour(new Date()), 1),
      end: new Date()
    })
  },
  {
    label: 'Last 4 hours',
    getRange: () => ({
      start: subHours(startOfHour(new Date()), 4),
      end: new Date()
    })
  },
  {
    label: 'Today',
    getRange: () => ({
      start: startOfDay(new Date()),
      end: endOfDay(new Date())
    })
  },
  {
    label: 'Yesterday',
    getRange: () => ({
      start: startOfDay(subHours(new Date(), 24)),
      end: endOfDay(subHours(new Date(), 24))
    })
  }
]

// Date/Time state
const startDate = ref(format(range.value.start, 'yyyy-MM-dd'))
const endDate = ref(format(range.value.end, 'yyyy-MM-dd'))
const startTime = ref(format(range.value.start, 'HH:mm'))
const endTime = ref(format(range.value.end, 'HH:mm'))

// Initialize from props
watch(() => props.modelValue, (newValue) => {
  if (newValue) {
    const start = new Date(newValue.start)
    const end = new Date(newValue.end)
    startDate.value = format(start, 'yyyy-MM-dd')
    endDate.value = format(end, 'yyyy-MM-dd')
    startTime.value = format(start, 'HH:mm')
    endTime.value = format(end, 'HH:mm')
  }
}, { immediate: true })

// Update handlers
const updateStartDate = (date: string) => {
  if (!date) return
  startDate.value = date
  const [year, month, day] = date.split('-').map(Number)
  const [hours, minutes] = startTime.value.split(':').map(Number)
  const newDate = new Date(year, month - 1, day, hours, minutes)
  if (isValid(newDate)) {
    range.value = { ...range.value, start: newDate }
    validateRange()
  }
}

const updateEndDate = (date: string) => {
  if (!date) return
  endDate.value = date
  const [year, month, day] = date.split('-').map(Number)
  const [hours, minutes] = endTime.value.split(':').map(Number)
  const newDate = new Date(year, month - 1, day, hours, minutes)
  if (isValid(newDate)) {
    range.value = { ...range.value, end: newDate }
    validateRange()
  }
}

const updateStartTime = (time: string) => {
  if (!time) return
  startTime.value = time
  const [hours, minutes] = time.split(':').map(Number)
  const newDate = new Date(range.value.start)
  newDate.setHours(hours, minutes)
  range.value = { ...range.value, start: newDate }
  validateRange()
}

const updateEndTime = (time: string) => {
  if (!time) return
  endTime.value = time
  const [hours, minutes] = time.split(':').map(Number)
  const newDate = new Date(range.value.end)
  newDate.setHours(hours, minutes)
  range.value = { ...range.value, end: newDate }
  validateRange()
}

// Apply final selection
const applySelection = () => {
  if (!errorMessage.value) {
    emit('update:modelValue', range.value)
    menu.value = false
  }
}

// Validate the range
const validateRange = () => {
  errorMessage.value = range.value.start >= range.value.end
    ? 'End time must be after start time'
    : null
}

// Apply preset range
const applyPreset = (preset: { label: string; getRange: () => { start: Date; end: Date } }) => {
  range.value = preset.getRange()
  menu.value = false
}
</script>

<style scoped>
.border-l {
  border-left: 1px solid rgb(var(--v-border-color));
}

.gap-2 {
  gap: 8px;
}
</style>
