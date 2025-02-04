<script setup lang="ts">
import { ref, computed, watch } from 'vue'
import { Button } from '@/components/ui/button'
import { Popover, PopoverContent, PopoverTrigger } from '@/components/ui/popover'
import { Input } from '@/components/ui/input'
import { Label } from '@/components/ui/label'
import { RangeCalendar } from '@/components/ui/range-calendar'
import { cn } from '@/lib/utils'
import { CalendarIcon, Search } from 'lucide-vue-next'
import type { DateRange } from 'radix-vue'
import { getLocalTimeZone, now, ZonedDateTime, toZoned, CalendarDateTime } from '@internationalized/date'

interface Props {
    modelValue?: DateRange | null
}

const props = withDefaults(defineProps<Props>(), {
    modelValue: null
})

const emit = defineEmits<{
    (e: 'update:modelValue', value: DateRange): void
}>()

// UI state
const showDatePicker = ref(false)
const showFromCalendar = ref(false)
const showToCalendar = ref(false)
const searchQuery = ref('')
const errorMessage = ref('')

// Separate date and time state
const draftTimeState = ref({
    start: {
        hour: 0,
        minute: 0,
        second: 0
    },
    end: {
        hour: 23,
        minute: 59,
        second: 59
    }
})

// Date state
const currentTime = now(getLocalTimeZone())
const dateRange = ref<DateRange>({
    start: currentTime.subtract({ hours: 1 }),
    end: currentTime
})

// Initialize time state from current dateRange
draftTimeState.value.start = {
    hour: dateRange.value.start.hour,
    minute: dateRange.value.start.minute,
    second: dateRange.value.start.second
}
draftTimeState.value.end = {
    hour: dateRange.value.end.hour,
    minute: dateRange.value.end.minute,
    second: dateRange.value.end.second
}

// Draft state for editing
const draftRange = ref<{
    start: string;
    end: string;
}>({
    start: formatDateTime(dateRange.value.start),
    end: formatDateTime(dateRange.value.end)
})

// Initialize modelValue if not provided
if (!props.modelValue?.start || !props.modelValue?.end) {
    emit('update:modelValue', dateRange.value)
}

// Sync internal state with external value
watch(() => props.modelValue, (newValue) => {
    if (newValue?.start && newValue?.end) {
        dateRange.value = newValue
        draftRange.value = {
            start: formatDateTime(newValue.start),
            end: formatDateTime(newValue.end)
        }
        draftTimeState.value.start = {
            hour: newValue.start.hour,
            minute: newValue.start.minute,
            second: newValue.start.second
        }
        draftTimeState.value.end = {
            hour: newValue.end.hour,
            minute: newValue.end.minute,
            second: newValue.end.second
        }
    }
}, { immediate: true })

// Watch for changes to the draft range inputs and update time state
watch(() => draftRange.value.start, (newVal) => {
    const parsed = parseDateTime(newVal)
    if (parsed) {
        draftTimeState.value.start = {
            hour: parsed.hour,
            minute: parsed.minute,
            second: parsed.second
        }
    }
})

watch(() => draftRange.value.end, (newVal) => {
    const parsed = parseDateTime(newVal)
    if (parsed) {
        draftTimeState.value.end = {
            hour: parsed.hour,
            minute: parsed.minute,
            second: parsed.second
        }
    }
})

const quickRanges = {
    'Last 5 minutes': { hours: 0, minutes: 5 },
    'Last 15 minutes': { hours: 0, minutes: 15 },
    'Last 30 minutes': { hours: 0, minutes: 30 },
    'Last 1 hour': { hours: 1 },
    'Last 3 hours': { hours: 3 },
    'Last 6 hours': { hours: 6 },
    'Last 12 hours': { hours: 12 },
    'Last 24 hours': { hours: 24 },
    'Last 2 days': { days: 2 },
    'Last 7 days': { days: 7 },
    'Last 30 days': { days: 30 },
    'Last 90 days': { days: 90 },
} as const

const filteredRanges = computed(() => {
    if (!searchQuery.value) return quickRanges
    return Object.fromEntries(
        Object.entries(quickRanges).filter(([label]) =>
            label.toLowerCase().includes(searchQuery.value.toLowerCase())
        )
    )
})

function formatDateTime(date: ZonedDateTime | null | undefined): string {
    if (!date) return ''

    try {
        const zonedDate = toZoned(date, getLocalTimeZone())
        const isoString = zonedDate.toString()
        const [datePart, timePart] = isoString.split('T')
        const time = timePart.slice(0, 8) // Get HH:MM:SS
        return `${datePart} ${time}`
    } catch (e) {
        console.error('Error formatting date:', e)
        return ''
    }
}

function parseDateTime(value: string): ZonedDateTime | null {
    if (!value) return null
    try {
        const [datePart, timePart] = value.split(' ')

        if (!datePart || !timePart) {
            errorMessage.value = 'Invalid date-time format. Expected "YYYY-MM-DD HH:mm:ss"'
            return null
        }

        const [year, month, day] = datePart.split('-').map(Number)

        const [hour, minute, second] = timePart.split(':').map(Number)

        if (isNaN(year) || isNaN(month) || isNaN(day)) {
            errorMessage.value = 'Invalid date format. Expected "YYYY-MM-DD"'
            return null
        }
        if (isNaN(hour) || isNaN(minute) || isNaN(second) ||
            hour < 0 || hour > 23 ||
            minute < 0 || minute > 59 ||
            second < 0 || second > 59) {
            errorMessage.value = 'Invalid time format. Expected "HH:mm:ss"'
            return null
        }

        try {
            // Create a CalendarDateTime first, then convert to ZonedDateTime
            const calendarDate = new CalendarDateTime(year, month, day, hour, minute, second)
            const result = toZoned(calendarDate, getLocalTimeZone())
            return result
        } catch (e) {
            console.error('Error creating ZonedDateTime:', e)
            errorMessage.value = 'Invalid date/time values'
            return null
        }
    } catch (e) {
        console.error('Error parsing date-time string:', e)
        errorMessage.value = 'Error parsing date-time. Please use format: YYYY-MM-DD HH:mm:ss'
        return null
    }
}

function handleCalendarUpdate(range: DateRange | null) {
    if (!range) return

    if (range.start) {
        // Set time to 00:00:00 when clicking a date
        const newDate = range.start.set({
            hour: 0,
            minute: 0,
            second: 0
        })
        draftRange.value.start = formatDateTime(newDate)
        // Update draft time state to match
        draftTimeState.value.start = {
            hour: 0,
            minute: 0,
            second: 0
        }
    }

    if (range.end) {
        // Set time to 00:00:00 when clicking a date
        const newDate = range.end.set({
            hour: 0,
            minute: 0,
            second: 0
        })
        draftRange.value.end = formatDateTime(newDate)
        // Update draft time state to match
        draftTimeState.value.end = {
            hour: 0,
            minute: 0,
            second: 0
        }
    }

    if (range.start && range.end) {
        showFromCalendar.value = false
        showToCalendar.value = false
    }
}

function handleApply() {
    // Clear any previous error
    errorMessage.value = ''

    const start = parseDateTime(draftRange.value.start)
    const end = parseDateTime(draftRange.value.end)

    if (!start || !end) {
        // Error message already set by parseDateTime
        return
    }

    // Validate that start is before end
    if (start.compare(end) > 0) {
        errorMessage.value = 'Start date must be before end date'
        return
    }

    // Update the actual date range and emit
    dateRange.value = { start, end }

    // Update draft time state with the latest valid times
    draftTimeState.value.start = {
        hour: start.hour,
        minute: start.minute,
        second: start.second
    }
    draftTimeState.value.end = {
        hour: end.hour,
        minute: end.minute,
        second: end.second
    }

    selectedQuickRange.value = null
    emitUpdate()
    showDatePicker.value = false
}

function applyQuickRange(range: keyof typeof quickRanges) {
    const end = now(getLocalTimeZone())
    const start = end.subtract(quickRanges[range])

    dateRange.value = { start, end }

    // Update draft time state with quick range times
    draftTimeState.value.start = {
        hour: start.hour,
        minute: start.minute,
        second: start.second
    }
    draftTimeState.value.end = {
        hour: end.hour,
        minute: end.minute,
        second: end.second
    }

    draftRange.value = {
        start: formatDateTime(start),
        end: formatDateTime(end)
    }
    selectedQuickRange.value = range
    emitUpdate()
    showDatePicker.value = false
}

function handleCancel() {
    // Clear any error message
    errorMessage.value = ''

    // Reset draft values to current values
    draftRange.value = {
        start: formatDateTime(dateRange.value.start),
        end: formatDateTime(dateRange.value.end)
    }
    showDatePicker.value = false
}

function handleKeyDown(e: KeyboardEvent) {
    if (e.key === 'Enter' && e.ctrlKey) {
        handleApply()
    } else if (e.key === 'Escape') {
        handleCancel()
    }
}

function emitUpdate() {
    if (dateRange.value?.start && dateRange.value?.end) {
        emit('update:modelValue', dateRange.value)
    }
}

// Get the currently selected range text
const selectedQuickRange = ref<string | null>(null)

const selectedRangeText = computed(() => {
    if (!dateRange.value?.start || !dateRange.value?.end) return 'Select time range'

    // If a quick range is selected, show that
    if (selectedQuickRange.value) {
        return selectedQuickRange.value
    }

    // Otherwise show the full date range
    return `${formatDateTime(dateRange.value.start)} - ${formatDateTime(dateRange.value.end)}`
})
</script>

<template>
    <!-- Main Date Picker Popover -->
    <Popover v-model:open="showDatePicker">
        <PopoverTrigger as-child>
            <Button variant="outline" :class="[
                'min-w-[200px] max-w-[320px]',
                selectedQuickRange ? 'w-[200px]' : 'w-[320px]'
            ]">
                <div class="flex items-center">
                    <CalendarIcon class="mr-2 h-4 w-4 flex-shrink-0" />
                    <span>{{ selectedRangeText }}</span>
                </div>
            </Button>
        </PopoverTrigger>
        <PopoverContent class="w-[550px] p-4" align="start">
            <div class="flex space-x-4">
                <!-- Left side - Date inputs -->
                <div class="w-[300px] space-y-4">
                    <div class="space-y-4">
                        <!-- From -->
                        <div class="space-y-1.5">
                            <Label class="text-xs text-muted-foreground">From</Label>
                            <div class="relative">
                                <Input v-model="draftRange.start" class="pr-10 font-mono text-sm h-9"
                                    @keydown="handleKeyDown" />
                                <!-- From Calendar Popover -->
                                <Popover v-model:open="showFromCalendar">
                                    <PopoverTrigger as-child>
                                        <Button variant="ghost" size="icon"
                                            class="absolute right-0 top-0 h-full w-9 px-0 hover:bg-accent">
                                            <CalendarIcon class="h-4 w-4" />
                                        </Button>
                                    </PopoverTrigger>
                                    <PopoverContent class="w-auto p-3" :side="'right'" :align="'start'"
                                        :side-offset="10">
                                        <RangeCalendar v-model="dateRange" class="rounded-md border"
                                            :weekday-format="'short'" @update:model-value="handleCalendarUpdate" />
                                    </PopoverContent>
                                </Popover>
                            </div>
                        </div>
                        <!-- To -->
                        <div class="space-y-1.5">
                            <Label class="text-xs text-muted-foreground">To</Label>
                            <div class="relative">
                                <Input v-model="draftRange.end" class="pr-10 font-mono text-sm h-9"
                                    @keydown="handleKeyDown" />
                                <!-- To Calendar Popover -->
                                <Popover v-model:open="showToCalendar">
                                    <PopoverTrigger as-child>
                                        <Button variant="ghost" size="icon"
                                            class="absolute right-0 top-0 h-full w-9 px-0 hover:bg-accent">
                                            <CalendarIcon class="h-4 w-4" />
                                        </Button>
                                    </PopoverTrigger>
                                    <PopoverContent class="w-auto p-3" :side="'right'" :align="'start'"
                                        :side-offset="10">
                                        <RangeCalendar v-model="dateRange" class="rounded-md border"
                                            :weekday-format="'short'" @update:model-value="handleCalendarUpdate" />
                                    </PopoverContent>
                                </Popover>
                            </div>
                        </div>

                        <!-- Error message -->
                        <div v-if="errorMessage" class="text-sm text-destructive">
                            {{ errorMessage }}
                        </div>

                        <!-- Action buttons moved here -->
                        <div class="flex justify-end space-x-2 pt-2">
                            <Button variant="outline" @click="handleCancel">Cancel</Button>
                            <Button @click="handleApply">Apply</Button>
                        </div>
                    </div>
                </div>

                <!-- Right side - Quick ranges -->
                <div class="border-l pl-4 w-[200px]">
                    <div class="space-y-3">
                        <div class="sticky top-0 bg-background">
                            <div class="relative">
                                <Search class="absolute left-2 top-2.5 h-4 w-4 text-muted-foreground" />
                                <Input v-model="searchQuery" class="pl-8 h-9 text-sm w-full"
                                    placeholder="Search quick ranges" />
                            </div>
                        </div>
                        <div class="space-y-0.5 max-h-[400px] overflow-y-auto pr-2">
                            <Button v-for="(range, label) in filteredRanges" :key="label" variant="ghost"
                                class="justify-start w-full h-9 px-2 text-sm hover:bg-accent"
                                @click="applyQuickRange(label)">
                                {{ label }}
                            </Button>
                        </div>
                    </div>
                </div>
            </div>
        </PopoverContent>
    </Popover>
</template>

<style scoped>
.v-popper__popper {
    max-width: none !important;
}
</style>
