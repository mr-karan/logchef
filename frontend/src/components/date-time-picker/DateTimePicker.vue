<script setup lang="ts">
import { ref, computed, watch } from "vue";
import { Button } from "@/components/ui/button";
import {
  Popover,
  PopoverContent,
  PopoverTrigger,
} from "@/components/ui/popover";
import { Input } from "@/components/ui/input";
import { Calendar } from "@/components/ui/calendar";
import { CalendarIcon, Clock, ChevronDown, Search } from "lucide-vue-next";
import type { DateRange } from "radix-vue";
import {
  getLocalTimeZone,
  now,
  ZonedDateTime,
  toZoned,
  CalendarDateTime,
  type DateValue,
  parseDateTime,
} from "@internationalized/date";
import { cn } from "@/lib/utils";

interface Props {
  modelValue?: DateRange | null;
  class?: string;
  disabled?: boolean;
  selectedQuickRange?: string | null;
}

const props = withDefaults(defineProps<Props>(), {
  modelValue: null,
  class: "",
  disabled: false,
  selectedQuickRange: null,
});

const emit = defineEmits(['update:modelValue', 'update:timezone']);

// UI state
const showDatePicker = ref(false);
const showFromCalendar = ref(false);
const showToCalendar = ref(false);
const quickRangeSearch = ref("");

// Timezone state
const timezonePreference = ref(
  localStorage.getItem("logchef_timezone") || "local"
);
const currentTimezoneId = computed(() =>
  timezonePreference.value === "local" ? getLocalTimeZone() : "UTC"
);

// Date state
const currentTime = now(currentTimezoneId.value);
const dateRange = ref<{ start: DateValue; end: DateValue }>({
  start: currentTime.subtract({ minutes: 15 }),
  end: currentTime,
});

// Selected quick range (synced with props)
const selectedQuickRange = ref<string | null>(props.selectedQuickRange || null);

// Recently used absolute ranges (stored in localStorage)
const recentRanges = ref<Array<{ from: string; to: string; label: string }>>(
  JSON.parse(localStorage.getItem("logchef_recent_ranges") || "[]")
);

// Draft state for absolute time inputs
const draftState = ref({
  from: "now-1h",
  to: "now",
});

// Quick ranges organized by category
const quickRanges = [
  { label: "Last 5 minutes", value: "5m", duration: { minutes: 5 } },
  { label: "Last 15 minutes", value: "15m", duration: { minutes: 15 } },
  { label: "Last 30 minutes", value: "30m", duration: { minutes: 30 } },
  { label: "Last 1 hour", value: "1h", duration: { hours: 1 } },
  { label: "Last 3 hours", value: "3h", duration: { hours: 3 } },
  { label: "Last 6 hours", value: "6h", duration: { hours: 6 } },
  { label: "Last 12 hours", value: "12h", duration: { hours: 12 } },
  { label: "Last 24 hours", value: "24h", duration: { hours: 24 } },
  { label: "Last 2 days", value: "2d", duration: { days: 2 } },
  { label: "Last 7 days", value: "7d", duration: { days: 7 } },
  { label: "Last 30 days", value: "30d", duration: { days: 30 } },
  { label: "Last 90 days", value: "90d", duration: { days: 90 } },
] as const;

// Filtered quick ranges based on search
const filteredQuickRanges = computed(() => {
  if (!quickRangeSearch.value) return quickRanges;
  const search = quickRangeSearch.value.toLowerCase();
  return quickRanges.filter(r => 
    r.label.toLowerCase().includes(search) || 
    r.value.toLowerCase().includes(search)
  );
});

// Initialize from props
if (!props.modelValue?.start || !props.modelValue?.end) {
  emit("update:modelValue", { start: dateRange.value.start, end: dateRange.value.end });
}

// Watch for changes in props.selectedQuickRange
watch(
  () => props.selectedQuickRange,
  (newValue) => {
    if (newValue !== selectedQuickRange.value) {
      selectedQuickRange.value = newValue;
    }
  },
  { immediate: true }
);

// Sync internal state with external value
watch(
  () => props.modelValue,
  (newValue) => {
    if (newValue?.start && newValue?.end) {
      dateRange.value = {
        start: newValue.start,
        end: newValue.end,
      };
      // Update draft state for absolute inputs
      const start = toZoned(newValue.start as CalendarDateTime, currentTimezoneId.value);
      const end = toZoned(newValue.end as CalendarDateTime, currentTimezoneId.value);
      draftState.value = {
        from: formatDateTime(start),
        to: formatDateTime(end),
      };
    }
  },
  { immediate: true, deep: true }
);

// Watch for timezone changes
watch(
  () => timezonePreference.value,
  (newValue) => {
    localStorage.setItem("logchef_timezone", newValue);
    emit("update:timezone", newValue === "local" ? getLocalTimeZone() : "UTC");
  }
);

function formatDateTime(date: ZonedDateTime | null | undefined): string {
  if (!date) return "";
  try {
    const isoString = date.toString();
    const datePart = isoString.split("T")[0];
    const timePart = isoString.split("T")[1].slice(0, 8);
    return `${datePart} ${timePart}`;
  } catch (e) {
    return "";
  }
}

function parseRelativeTime(input: string): ZonedDateTime | null {
  const trimmed = input.trim().toLowerCase();
  
  // Handle "now"
  if (trimmed === "now") {
    return now(currentTimezoneId.value);
  }
  
  // Handle "now-Xm/h/d" format
  const relativeMatch = trimmed.match(/^now-(\d+)(m|h|d)$/);
  if (relativeMatch) {
    const value = parseInt(relativeMatch[1]);
    const unit = relativeMatch[2];
    const current = now(currentTimezoneId.value);
    
    switch (unit) {
      case 'm': return current.subtract({ minutes: value });
      case 'h': return current.subtract({ hours: value });
      case 'd': return current.subtract({ days: value });
    }
  }
  
  // Try to parse as absolute datetime
  try {
    // Handle "YYYY-MM-DD HH:mm:ss" format
    const parts = trimmed.split(' ');
    if (parts.length === 2) {
      const dateString = `${parts[0]}T${parts[1]}`;
      const calendarDate = parseDateTime(dateString);
      return toZoned(calendarDate, currentTimezoneId.value);
    }
    // Handle "YYYY-MM-DD" format (assume start of day)
    if (parts.length === 1 && parts[0].match(/^\d{4}-\d{2}-\d{2}$/)) {
      const dateString = `${parts[0]}T00:00:00`;
      const calendarDate = parseDateTime(dateString);
      return toZoned(calendarDate, currentTimezoneId.value);
    }
  } catch (e) {
    console.error("Error parsing date:", e);
  }
  
  return null;
}

function applyQuickRange(range: typeof quickRanges[number]) {
  const end = now(currentTimezoneId.value);
  const start = end.subtract(range.duration);

  dateRange.value = { start, end };
  selectedQuickRange.value = `Last ${range.value}`;
  draftState.value = {
    from: `now-${range.value}`,
    to: "now",
  };
  
  emit("update:modelValue", { start, end });
  showDatePicker.value = false;
}

function applyAbsoluteRange() {
  const start = parseRelativeTime(draftState.value.from);
  const end = parseRelativeTime(draftState.value.to);
  
  if (!start || !end) {
    return;
  }
  
  if (start.compare(end) > 0) {
    return;
  }

  dateRange.value = { start, end };
  selectedQuickRange.value = null;
  
  // Save to recent ranges
  const rangeEntry = {
    from: draftState.value.from,
    to: draftState.value.to,
    label: `${formatDateTime(start)} to ${formatDateTime(end)}`,
  };
  
  // Add to recent, avoiding duplicates
  const existingIndex = recentRanges.value.findIndex(
    r => r.from === rangeEntry.from && r.to === rangeEntry.to
  );
  if (existingIndex !== -1) {
    recentRanges.value.splice(existingIndex, 1);
  }
  recentRanges.value.unshift(rangeEntry);
  recentRanges.value = recentRanges.value.slice(0, 5); // Keep only 5 recent
  localStorage.setItem("logchef_recent_ranges", JSON.stringify(recentRanges.value));
  
  emit("update:modelValue", { start, end });
  showDatePicker.value = false;
}

function applyRecentRange(range: { from: string; to: string }) {
  draftState.value = { from: range.from, to: range.to };
  applyAbsoluteRange();
}

function handleCalendarSelect(type: 'from' | 'to', date: DateValue | undefined | null) {
  if (!date) return;
  const zonedDate = toZoned(date as CalendarDateTime, currentTimezoneId.value);
  const formatted = formatDateTime(zonedDate).split(' ')[0] + ' 00:00:00';
  draftState.value[type] = formatted;
  
  if (type === 'from') {
    showFromCalendar.value = false;
  } else {
    showToCalendar.value = false;
  }
}

function clearRecentRanges() {
  recentRanges.value = [];
  localStorage.removeItem("logchef_recent_ranges");
}

// Display text for trigger button
const triggerDisplayText = computed(() => {
  if (selectedQuickRange.value) {
    return selectedQuickRange.value;
  }
  
  if (!dateRange.value?.start || !dateRange.value?.end) {
    return "Select time range";
  }
  
  const start = toZoned(dateRange.value.start as CalendarDateTime, currentTimezoneId.value);
  const end = toZoned(dateRange.value.end as CalendarDateTime, currentTimezoneId.value);
  return `${formatDateTime(start)} - ${formatDateTime(end)}`;
});

// Function to open the date picker programmatically
function openDatePicker() {
  showDatePicker.value = true;
}

// Get current timezone display
const timezoneDisplay = computed(() => {
  if (timezonePreference.value === "local") {
    return `Browser Time (${getLocalTimeZone()})`;
  }
  return "UTC";
});

// Expose methods and computed properties to parent component
defineExpose({
  openDatePicker,
  selectedQuickRange,
  selectedRangeText: triggerDisplayText,
  currentTimezoneId,
});
</script>

<template>
  <div :class="cn('flex items-center gap-1', props.class)">
    <Popover v-model:open="showDatePicker">
      <PopoverTrigger as-child>
        <Button
          variant="ghost"
          :class="[
            'h-7 px-2.5 gap-1.5 font-normal text-sm border border-transparent hover:border-border hover:bg-muted/50',
            props.disabled ? 'opacity-50 cursor-not-allowed' : '',
          ]"
          :disabled="props.disabled"
        >
          <Clock class="h-3.5 w-3.5 text-muted-foreground" />
          <span>{{ triggerDisplayText }}</span>
          <ChevronDown class="h-3 w-3 text-muted-foreground ml-0.5" />
        </Button>
      </PopoverTrigger>
      
      <PopoverContent
        v-if="!props.disabled"
        class="w-[580px] p-0"
        align="start"
        side="bottom"
      >
        <div class="flex">
          <!-- Left Panel: Absolute time range -->
          <div class="w-[260px] p-4 border-r">
            <h4 class="text-sm font-medium mb-3">Absolute time range</h4>
            
            <!-- From input -->
            <div class="space-y-1.5 mb-3">
              <label class="text-xs text-muted-foreground">From</label>
              <div class="relative">
                <Input
                  v-model="draftState.from"
                  class="h-8 text-sm font-mono pr-8"
                  placeholder="now-1h or YYYY-MM-DD HH:mm:ss"
                  @keydown.enter="applyAbsoluteRange"
                />
                <Popover v-model:open="showFromCalendar">
                  <PopoverTrigger as-child>
                    <button class="absolute right-1 top-1 p-1 hover:bg-muted rounded">
                      <CalendarIcon class="h-4 w-4 text-muted-foreground" />
                    </button>
                  </PopoverTrigger>
                  <PopoverContent class="w-auto p-0" side="right" align="start">
                    <Calendar
                      class="rounded-md border"
                      @update:model-value="(date) => handleCalendarSelect('from', date)"
                    />
                  </PopoverContent>
                </Popover>
              </div>
            </div>
            
            <!-- To input -->
            <div class="space-y-1.5 mb-4">
              <label class="text-xs text-muted-foreground">To</label>
              <div class="relative">
                <Input
                  v-model="draftState.to"
                  class="h-8 text-sm font-mono pr-8"
                  placeholder="now or YYYY-MM-DD HH:mm:ss"
                  @keydown.enter="applyAbsoluteRange"
                />
                <Popover v-model:open="showToCalendar">
                  <PopoverTrigger as-child>
                    <button class="absolute right-1 top-1 p-1 hover:bg-muted rounded">
                      <CalendarIcon class="h-4 w-4 text-muted-foreground" />
                    </button>
                  </PopoverTrigger>
                  <PopoverContent class="w-auto p-0" side="right" align="start">
                    <Calendar
                      class="rounded-md border"
                      @update:model-value="(date) => handleCalendarSelect('to', date)"
                    />
                  </PopoverContent>
                </Popover>
              </div>
            </div>
            
            <!-- Apply button -->
            <Button 
              class="w-full h-8" 
              @click="applyAbsoluteRange"
            >
              Apply time range
            </Button>
            
            <!-- Recently used -->
            <div v-if="recentRanges.length > 0" class="mt-4 pt-4 border-t">
              <div class="flex items-center justify-between mb-2">
                <span class="text-xs text-muted-foreground">Recently used</span>
                <button 
                  class="text-xs text-muted-foreground hover:text-foreground"
                  @click="clearRecentRanges"
                >
                  Clear
                </button>
              </div>
              <div class="space-y-1">
                <button
                  v-for="(range, idx) in recentRanges"
                  :key="idx"
                  class="w-full text-left text-xs p-1.5 hover:bg-muted rounded truncate"
                  @click="applyRecentRange(range)"
                >
                  {{ range.from }} to {{ range.to }}
                </button>
              </div>
            </div>
            
            <!-- Timezone info -->
            <div class="mt-4 pt-4 border-t">
              <div class="flex items-center justify-between">
                <span class="text-xs text-muted-foreground">{{ timezoneDisplay }}</span>
                <button 
                  class="text-xs text-primary hover:underline"
                  @click="timezonePreference = timezonePreference === 'local' ? 'utc' : 'local'"
                >
                  Change
                </button>
              </div>
            </div>
          </div>
          
          <!-- Right Panel: Quick ranges -->
          <div class="flex-1 p-4">
            <!-- Search -->
            <div class="relative mb-3">
              <Search class="absolute left-2 top-1/2 -translate-y-1/2 h-4 w-4 text-muted-foreground" />
              <Input
                v-model="quickRangeSearch"
                class="h-8 pl-8 text-sm"
                placeholder="Search quick ranges"
              />
            </div>
            
            <!-- Quick range list -->
            <div class="space-y-0.5 max-h-[320px] overflow-y-auto">
              <button
                v-for="range in filteredQuickRanges"
                :key="range.value"
                :class="[
                  'w-full text-left px-3 py-2 text-sm rounded-md transition-colors',
                  selectedQuickRange === `Last ${range.value}` 
                    ? 'bg-primary/10 text-primary border-l-2 border-primary' 
                    : 'hover:bg-muted'
                ]"
                @click="applyQuickRange(range)"
              >
                {{ range.label }}
              </button>
              
              <div v-if="filteredQuickRanges.length === 0" class="text-sm text-muted-foreground text-center py-4">
                No matching ranges
              </div>
            </div>
          </div>
        </div>
      </PopoverContent>
    </Popover>
    
    <!-- Timezone indicator -->
    <Button
      variant="ghost"
      size="sm"
      class="h-8 px-2 text-xs text-muted-foreground hover:text-foreground"
      @click="timezonePreference = timezonePreference === 'local' ? 'utc' : 'local'"
      title="Click to toggle timezone"
    >
      {{ timezonePreference === "local" ? "Local" : "UTC" }}
    </Button>
  </div>
</template>
