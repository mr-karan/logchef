<script setup lang="ts">
import { ref, computed, watch, type WritableComputedRef } from 'vue'
import { Button } from '@/components/ui/button'
import { Popover, PopoverContent, PopoverTrigger } from '@/components/ui/popover'
import { Input } from '@/components/ui/input'
import { Label } from '@/components/ui/label'
import { Calendar } from '@/components/ui/calendar'
import { CalendarIcon, Clock } from 'lucide-vue-next'
import type { DateRange } from 'radix-vue'
import { getLocalTimeZone, now, ZonedDateTime, toZoned, CalendarDateTime, type DateValue } from '@internationalized/date'
import { cn } from '@/lib/utils'

interface Props {
    modelValue?: DateRange | null
    class?: string
    disabled?: boolean
}

const props = withDefaults(defineProps<Props>(), {
    modelValue: null,
    class: '',
    disabled: false
})

const emit = defineEmits<{
    (e: 'update:modelValue', value: DateRange): void
}>()

// UI state
const showDatePicker = ref(false)
const showFromCalendar = ref(false)
const showToCalendar = ref(false)
const errorMessage = ref('')

// Date state
const currentTime = now(getLocalTimeZone())
const dateRange = ref<{ start: DateValue; end: DateValue }>({
    start: currentTime.subtract({ minutes: 5 }),
    end: currentTime
})

// Computed DateRange for v-model binding
const calendarDateRange = computed({
    get: () => ({
        start: dateRange.value.start,
        end: dateRange.value.end
    }),
    set: (newValue: DateRange | null) => {
        if (newValue?.start && newValue?.end) {
            dateRange.value = {
                start: newValue.start as DateValue,
                end: newValue.end as DateValue
            }
        }
    }
}) as unknown as WritableComputedRef<DateRange>

// Initialize time state from current dateRange
const startZoned = toZoned(dateRange.value.start as CalendarDateTime, getLocalTimeZone())
const endZoned = toZoned(dateRange.value.end as CalendarDateTime, getLocalTimeZone())

// Separate date and time inputs
const draftState = ref({
    start: {
        date: formatDate(startZoned),
        time: formatTime(startZoned)
    },
    end: {
        date: formatDate(endZoned),
        time: formatTime(endZoned)
    }
})

// Initialize modelValue if not provided
if (!props.modelValue?.start || !props.modelValue?.end) {
    emit('update:modelValue', calendarDateRange.value)
}

// Sync internal state with external value
watch(() => props.modelValue, (newValue) => {
    if (newValue?.start && newValue?.end) {
        const start = toZoned(newValue.start as CalendarDateTime, getLocalTimeZone())
        const end = toZoned(newValue.end as CalendarDateTime, getLocalTimeZone())

        dateRange.value = {
            start: newValue.start,
            end: newValue.end
        }
        draftState.value = {
            start: {
                date: formatDate(start),
                time: formatTime(start)
            },
            end: {
                date: formatDate(end),
                time: formatTime(end)
            }
        }
    }
}, { immediate: true })

const quickRanges = [
    { label: 'Last 5m', duration: { minutes: 5 } },
    { label: 'Last 15m', duration: { minutes: 15 } },
    { label: 'Last 30m', duration: { minutes: 30 } },
    { label: 'Last 1h', duration: { hours: 1 } },
    { label: 'Last 3h', duration: { hours: 3 } },
    { label: 'Last 6h', duration: { hours: 6 } },
    { label: 'Last 12h', duration: { hours: 12 } },
    { label: 'Last 24h', duration: { hours: 24 } },
    { label: 'Last 2d', duration: { days: 2 } },
    { label: 'Last 7d', duration: { days: 7 } },
    { label: 'Last 30d', duration: { days: 30 } },
    { label: 'Last 90d', duration: { days: 90 } },
] as const

function formatDate(date: ZonedDateTime | null | undefined): string {
    if (!date) return ''
    try {
        const isoString = date.toString()
        return isoString.split('T')[0]
    } catch (e) {
        console.error('Error formatting date:', e)
        return ''
    }
}

function formatTime(date: ZonedDateTime | null | undefined): string {
    if (!date) return ''
    try {
        const isoString = date.toString()
        return isoString.split('T')[1].slice(0, 8) // Get HH:MM:SS
    } catch (e) {
        console.error('Error formatting time:', e)
        return ''
    }
}

function parseDateTime(date: string, time: string): ZonedDateTime | null {
    if (!date || !time) return null
    try {
        const [year, month, day] = date.split('-').map(Number)
        const [hour, minute, second] = time.split(':').map(Number)

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
            const calendarDate = new CalendarDateTime(year, month, day, hour, minute, second)
            return toZoned(calendarDate, getLocalTimeZone())
        } catch (e) {
            console.error('Error creating ZonedDateTime:', e)
            errorMessage.value = 'Invalid date/time values'
            return null
        }
    } catch (e) {
        console.error('Error parsing date-time:', e)
        errorMessage.value = 'Error parsing date-time. Please use format: YYYY-MM-DD HH:mm:ss'
        return null
    }
}

function handleCalendarUpdate(type: 'start' | 'end', date: DateValue | null) {
    if (!date) return

    const zonedDate = toZoned(date as CalendarDateTime, getLocalTimeZone())
    draftState.value[type].date = formatDate(zonedDate)

    // Close the respective calendar popover
    if (type === 'start') {
        showFromCalendar.value = false
    } else {
        showToCalendar.value = false
    }
}

function formatDisplayText() {
    const start = toZoned(dateRange.value.start as CalendarDateTime, getLocalTimeZone())
    const end = toZoned(dateRange.value.end as CalendarDateTime, getLocalTimeZone())
    return `${formatDate(start)} ${formatTime(start)} - ${formatDate(end)} ${formatTime(end)}`
}

function handleApply() {
    errorMessage.value = ''

    const start = parseDateTime(draftState.value.start.date, draftState.value.start.time)
    const end = parseDateTime(draftState.value.end.date, draftState.value.end.time)

    if (!start || !end) return

    if (start.compare(end) > 0) {
        errorMessage.value = 'Start date must be before end date'
        return
    }

    dateRange.value = { start, end }
    selectedQuickRange.value = null
    emitUpdate()
    showDatePicker.value = false
}

function applyQuickRange(range: typeof quickRanges[number]) {
    const end = now(getLocalTimeZone())
    const start = end.subtract(range.duration)

    dateRange.value = { start, end }
    draftState.value = {
        start: {
            date: formatDate(start),
            time: formatTime(start)
        },
        end: {
            date: formatDate(end),
            time: formatTime(end)
        }
    }
    selectedQuickRange.value = range.label
    emitUpdate()
    showDatePicker.value = false
}

function handleCancel() {
    errorMessage.value = ''
    const start = toZoned(dateRange.value.start as CalendarDateTime, getLocalTimeZone())
    const end = toZoned(dateRange.value.end as CalendarDateTime, getLocalTimeZone())
    draftState.value = {
        start: {
            date: formatDate(start),
            time: formatTime(start)
        },
        end: {
            date: formatDate(end),
            time: formatTime(end)
        }
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
        emit('update:modelValue', calendarDateRange.value)
    }
}

// Get the currently selected range text
const selectedQuickRange = ref<string | null>(null)

const selectedRangeText = computed(() => {
    if (!dateRange.value?.start || !dateRange.value?.end) return 'Select time range'
    if (selectedQuickRange.value) return selectedQuickRange.value
    return formatDisplayText()
})

// Compute duration between dates
const durationText = computed(() => {
    if (!dateRange.value?.start || !dateRange.value?.end) return ''
    const start = toZoned(dateRange.value.start as CalendarDateTime, getLocalTimeZone())
    const end = toZoned(dateRange.value.end as CalendarDateTime, getLocalTimeZone())
    const diffMs = end.toDate().getTime() - start.toDate().getTime()
    const diffDays = Math.floor(diffMs / (1000 * 60 * 60 * 24))
    const diffHours = Math.floor((diffMs % (1000 * 60 * 60 * 24)) / (1000 * 60 * 60))
    const diffMinutes = Math.floor((diffMs % (1000 * 60 * 60)) / (1000 * 60))

    if (diffDays > 0) return `Duration: ${diffDays}d ${diffHours}h ${diffMinutes}m`
    if (diffHours > 0) return `Duration: ${diffHours}h ${diffMinutes}m`
    return `Duration: ${diffMinutes}m`
})
</script>

<template>
    <div :class="cn('flex transition-all', props.class)">
        <Popover v-model:open="showDatePicker">
            <PopoverTrigger as-child>
                <Button variant="outline" :class="[
                    'min-w-[200px] max-w-[420px]',
                    selectedQuickRange ? 'w-[200px]' : 'w-auto',
                    props.disabled ? 'opacity-50 cursor-not-allowed' : ''
                ]" :disabled="props.disabled">
                    <div class="flex items-center">
                        <CalendarIcon class="mr-2 h-4 w-4 flex-shrink-0" />
                        <span>{{ selectedRangeText }}</span>
                    </div>
                </Button>
            </PopoverTrigger>
            <PopoverContent v-if="!props.disabled" class="w-[400px] p-4" align="start" side="bottom">
                <div class="space-y-4">
                    <!-- Date/Time Inputs -->
                    <div class="space-y-4">
                        <!-- From -->
                        <div class="space-y-2">
                            <Label class="text-sm font-medium">From</Label>
                            <div class="flex gap-2">
                                <div class="relative flex-1">
                                    <Input v-model="draftState.start.date" class="pr-10 font-mono text-sm h-9"
                                        placeholder="YYYY-MM-DD" @keydown="handleKeyDown" />
                                    <Popover v-model:open="showFromCalendar">
                                        <PopoverTrigger as-child>
                                            <Button variant="ghost" size="icon"
                                                class="absolute right-0 top-0 h-full w-9 px-0 hover:bg-accent">
                                                <CalendarIcon class="h-4 w-4" />
                                            </Button>
                                        </PopoverTrigger>
                                        <PopoverContent class="w-auto p-0" :side="'bottom'" :align="'end'">
                                            <Calendar
                                                :selected-date="dateRange.start !== undefined ? dateRange.start : null"
                                                class="rounded-md border"
                                                @update:model-value="date => handleCalendarUpdate('start', date)" />
                                        </PopoverContent>
                                    </Popover>
                                </div>
                                <div class="relative w-[120px]">
                                    <Input v-model="draftState.start.time" class="font-mono text-sm h-9"
                                        placeholder="HH:mm:ss" @keydown="handleKeyDown" />
                                    <Button variant="ghost" size="icon"
                                        class="absolute right-0 top-0 h-full w-9 px-0 hover:bg-accent pointer-events-none">
                                        <Clock class="h-4 w-4" />
                                    </Button>
                                </div>
                            </div>
                        </div>
                        <!-- To -->
                        <div class="space-y-2">
                            <Label class="text-sm font-medium">To</Label>
                            <div class="flex gap-2">
                                <div class="relative flex-1">
                                    <Input v-model="draftState.end.date" class="pr-10 font-mono text-sm h-9"
                                        placeholder="YYYY-MM-DD" @keydown="handleKeyDown" />
                                    <Popover v-model:open="showToCalendar">
                                        <PopoverTrigger as-child>
                                            <Button variant="ghost" size="icon"
                                                class="absolute right-0 top-0 h-full w-9 px-0 hover:bg-accent">
                                                <CalendarIcon class="h-4 w-4" />
                                            </Button>
                                        </PopoverTrigger>
                                        <PopoverContent class="w-auto p-0" :side="'bottom'" :align="'end'">
                                            <Calendar
                                                :selected-date="dateRange.end !== undefined ? dateRange.end : null"
                                                class="rounded-md border"
                                                @update:model-value="date => handleCalendarUpdate('end', date)" />
                                        </PopoverContent>
                                    </Popover>
                                </div>
                                <div class="relative w-[120px]">
                                    <Input v-model="draftState.end.time" class="font-mono text-sm h-9"
                                        placeholder="HH:mm:ss" @keydown="handleKeyDown" />
                                    <Button variant="ghost" size="icon"
                                        class="absolute right-0 top-0 h-full w-9 px-0 hover:bg-accent pointer-events-none">
                                        <Clock class="h-4 w-4" />
                                    </Button>
                                </div>
                            </div>
                        </div>
                    </div>

                    <!-- Duration -->
                    <div class="text-sm text-muted-foreground">
                        {{ durationText }}
                    </div>

                    <!-- Error message -->
                    <div v-if="errorMessage" class="text-sm text-destructive">
                        {{ errorMessage }}
                    </div>

                    <!-- Quick Ranges -->
                    <div class="space-y-2">
                        <Label class="text-sm font-medium">Quick Ranges</Label>
                        <div class="grid grid-cols-4 gap-2">
                            <Button v-for="range in quickRanges" :key="range.label" variant="outline" size="sm" :class="[
                                'h-8',
                                selectedQuickRange === range.label && 'bg-accent text-accent-foreground'
                            ]" @click="applyQuickRange(range)">
                                {{ range.label }}
                            </Button>
                        </div>
                    </div>

                    <!-- Action buttons -->
                    <div class="flex justify-end space-x-2 pt-2">
                        <Button variant="outline" @click="handleCancel">Cancel</Button>
                        <Button @click="handleApply">Apply</Button>
                    </div>
                </div>
            </PopoverContent>
        </Popover>
    </div>
</template>

<style scoped>
/* Ensure popovers aren't too wide */
:deep(.v-popper__popper) {
    max-width: 350px;
}
</style>
