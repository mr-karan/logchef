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
import { CalendarIcon, Clock } from "lucide-vue-next";
import {
  getLocalTimeZone,
  now,
  toZoned,
  CalendarDateTime,
  type DateValue,
} from "@internationalized/date";
import { cn } from "@/lib/utils";

interface Props {
  modelValue?: string | null;
  includeTime?: boolean;
  placeholder?: string;
  class?: string;
  disabled?: boolean;
}

const props = withDefaults(defineProps<Props>(), {
  modelValue: null,
  includeTime: true,
  placeholder: "Select date...",
  class: "",
  disabled: false,
});

const emit = defineEmits<{
  'update:modelValue': [value: string | null];
}>();

const showPicker = ref(false);
const showCalendar = ref(false);

const currentTimezoneId = computed(() => getLocalTimeZone());

const timeValue = ref("00:00:00");

function formatDateForDisplay(isoString: string | null): string {
  if (!isoString) return "";
  try {
    const date = new Date(isoString);
    if (isNaN(date.getTime())) return isoString;
    
    if (props.includeTime) {
      return date.toLocaleString('en-US', {
        year: 'numeric',
        month: 'short',
        day: 'numeric',
        hour: '2-digit',
        minute: '2-digit',
        second: '2-digit',
        hour12: false
      });
    } else {
      return date.toLocaleDateString('en-US', {
        year: 'numeric',
        month: 'short',
        day: 'numeric'
      });
    }
  } catch {
    return isoString;
  }
}

function formatDateForInput(isoString: string | null): string {
  if (!isoString) return "";
  try {
    const date = new Date(isoString);
    if (isNaN(date.getTime())) return "";
    
    const year = date.getFullYear();
    const month = String(date.getMonth() + 1).padStart(2, '0');
    const day = String(date.getDate()).padStart(2, '0');
    const hours = String(date.getHours()).padStart(2, '0');
    const minutes = String(date.getMinutes()).padStart(2, '0');
    const seconds = String(date.getSeconds()).padStart(2, '0');
    
    if (props.includeTime) {
      return `${year}-${month}-${day} ${hours}:${minutes}:${seconds}`;
    } else {
      return `${year}-${month}-${day}`;
    }
  } catch {
    return "";
  }
}

const draftValue = ref(formatDateForInput(props.modelValue ?? null));

const displayValue = computed(() => formatDateForDisplay(props.modelValue ?? null));

watch(
  () => props.modelValue,
  (newValue) => {
    draftValue.value = formatDateForInput(newValue ?? null);
    if (newValue) {
      try {
        const date = new Date(newValue);
        if (!isNaN(date.getTime())) {
          const hours = String(date.getHours()).padStart(2, '0');
          const minutes = String(date.getMinutes()).padStart(2, '0');
          const seconds = String(date.getSeconds()).padStart(2, '0');
          timeValue.value = `${hours}:${minutes}:${seconds}`;
        }
      } catch {
        // ignore
      }
    }
  },
  { immediate: true }
);

function parseInputValue(input: string): Date | null {
  const trimmed = input.trim();
  if (!trimmed) return null;
  
  try {
    if (trimmed.match(/^\d{4}-\d{2}-\d{2}$/)) {
      const [year, month, day] = trimmed.split('-').map(Number);
      return new Date(year, month - 1, day, 0, 0, 0);
    }
    
    if (trimmed.match(/^\d{4}-\d{2}-\d{2} \d{2}:\d{2}:\d{2}$/)) {
      const [datePart, timePart] = trimmed.split(' ');
      const [year, month, day] = datePart.split('-').map(Number);
      const [hours, minutes, seconds] = timePart.split(':').map(Number);
      return new Date(year, month - 1, day, hours, minutes, seconds);
    }
    
    const date = new Date(trimmed);
    if (!isNaN(date.getTime())) {
      return date;
    }
  } catch {
    // ignore
  }
  
  return null;
}

function applyValue() {
  const parsed = parseInputValue(draftValue.value);
  if (parsed) {
    // Emit local datetime string (YYYY-MM-DD HH:mm:ss) instead of UTC ISO8601
    // This ensures the user's intended local time is preserved for SQL queries
    // that use timezone-aware functions like toDateTime({{var}}, 'Asia/Calcutta')
    const year = parsed.getFullYear();
    const month = String(parsed.getMonth() + 1).padStart(2, '0');
    const day = String(parsed.getDate()).padStart(2, '0');
    const hours = String(parsed.getHours()).padStart(2, '0');
    const minutes = String(parsed.getMinutes()).padStart(2, '0');
    const seconds = String(parsed.getSeconds()).padStart(2, '0');
    
    const localDateTimeString = `${year}-${month}-${day} ${hours}:${minutes}:${seconds}`;
    emit('update:modelValue', localDateTimeString);
    showPicker.value = false;
  }
}

function handleCalendarSelect(date: DateValue | undefined | null) {
  if (!date) return;
  
  try {
    const zonedDate = toZoned(date as CalendarDateTime, currentTimezoneId.value);
    const year = zonedDate.year;
    const month = String(zonedDate.month).padStart(2, '0');
    const day = String(zonedDate.day).padStart(2, '0');
    
    if (props.includeTime) {
      draftValue.value = `${year}-${month}-${day} ${timeValue.value}`;
    } else {
      draftValue.value = `${year}-${month}-${day}`;
    }
    
    showCalendar.value = false;
  } catch (e) {
    console.error("Error selecting date:", e);
  }
}

function handleTimeChange(e: Event) {
  const input = e.target as HTMLInputElement;
  timeValue.value = input.value || "00:00:00";
  
  if (draftValue.value) {
    const datePart = draftValue.value.split(' ')[0];
    if (datePart && datePart.match(/^\d{4}-\d{2}-\d{2}$/)) {
      draftValue.value = `${datePart} ${timeValue.value}`;
    }
  }
}

function setNow() {
  const current = now(currentTimezoneId.value);
  const year = current.year;
  const month = String(current.month).padStart(2, '0');
  const day = String(current.day).padStart(2, '0');
  const hours = String(current.hour).padStart(2, '0');
  const minutes = String(current.minute).padStart(2, '0');
  const seconds = String(current.second).padStart(2, '0');
  
  timeValue.value = `${hours}:${minutes}:${seconds}`;
  
  if (props.includeTime) {
    draftValue.value = `${year}-${month}-${day} ${hours}:${minutes}:${seconds}`;
  } else {
    draftValue.value = `${year}-${month}-${day}`;
  }
}

function clear() {
  draftValue.value = "";
  timeValue.value = "00:00:00";
  emit('update:modelValue', null);
  showPicker.value = false;
}
</script>

<template>
  <div :class="cn('relative', props.class)">
    <Popover v-model:open="showPicker">
      <PopoverTrigger as-child>
        <Button
          variant="outline"
          :class="[
            'w-full justify-start text-left font-normal h-9',
            !displayValue && 'text-muted-foreground',
            props.disabled && 'opacity-50 cursor-not-allowed',
          ]"
          :disabled="props.disabled"
        >
          <CalendarIcon class="mr-2 h-4 w-4 flex-shrink-0" />
          <span class="truncate">{{ displayValue || placeholder }}</span>
        </Button>
      </PopoverTrigger>
      
      <PopoverContent
        v-if="!props.disabled"
        class="w-auto p-0"
        align="start"
        side="bottom"
      >
        <div class="p-3 space-y-3">
          <div class="space-y-1.5">
            <label class="text-xs text-muted-foreground">
              {{ includeTime ? 'Date & Time' : 'Date' }}
            </label>
            <div class="flex gap-2">
              <div class="relative flex-1">
                <Input
                  v-model="draftValue"
                  class="h-8 text-sm font-mono pr-8"
                  :placeholder="includeTime ? 'YYYY-MM-DD HH:mm:ss' : 'YYYY-MM-DD'"
                  @keydown.enter="applyValue"
                />
                <Popover v-model:open="showCalendar">
                  <PopoverTrigger as-child>
                    <button class="absolute right-1 top-1 p-1 hover:bg-muted rounded">
                      <CalendarIcon class="h-4 w-4 text-muted-foreground" />
                    </button>
                  </PopoverTrigger>
                  <PopoverContent class="w-auto p-0" side="right" align="start">
                    <Calendar
                      class="rounded-md border"
                      @update:model-value="handleCalendarSelect"
                    />
                  </PopoverContent>
                </Popover>
              </div>
            </div>
          </div>
          
          <div v-if="includeTime" class="space-y-1.5">
            <label class="text-xs text-muted-foreground flex items-center gap-1">
              <Clock class="h-3 w-3" />
              Time
            </label>
            <Input
              type="time"
              step="1"
              :value="timeValue"
              class="h-8 text-sm font-mono"
              @input="handleTimeChange"
            />
          </div>
          
          <div class="flex gap-2">
            <Button 
              variant="outline"
              size="sm"
              class="flex-1 h-8"
              @click="setNow"
            >
              Now
            </Button>
            <Button 
              variant="outline"
              size="sm"
              class="h-8"
              @click="clear"
            >
              Clear
            </Button>
            <Button 
              size="sm"
              class="flex-1 h-8"
              @click="applyValue"
            >
              Apply
            </Button>
          </div>
        </div>
      </PopoverContent>
    </Popover>
  </div>
</template>
