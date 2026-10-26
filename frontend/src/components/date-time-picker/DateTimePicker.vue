<script setup lang="ts">
import { QUICK_RANGES, type QuickRange } from "@/lib/quickRanges";
import { ref, computed, watch } from "vue";
import { Button } from "@/components/ui/button";
import {
  Popover,
  PopoverContent,
  PopoverTrigger,
} from "@/components/ui/popover";
import { Input } from "@/components/ui/input";
import { Calendar } from "@/components/ui/calendar";
import { Tooltip, TooltipContent, TooltipProvider, TooltipTrigger } from "@/components/ui/tooltip";
import { CalendarIcon, Clock, ChevronDown, Search } from "lucide-vue-next";
import { today, type DateValue } from "@internationalized/date";
import { cn } from "@/lib/utils";
import {
  epochMs,
  formatWallClock,
  parseWallClock,
  windowLabel,
  type EpochMs,
  type ResolvedWindow,
  type TimeWindow,
} from "@/utils/timeWindow";
import { useI18n } from "vue-i18n";

const { t, locale } = useI18n();

interface Props {
  modelValue: TimeWindow;
  /** The window as the owner resolved it. The picker shows these instants in its zone. */
  resolved: ResolvedWindow;
  zonePreference: "utc" | "local";
  class?: string;
  /** Locks the time range and says why. The zone switch stays usable. */
  disabledReason?: string;
}

const props = withDefaults(defineProps<Props>(), {
  class: "",
});

const emit = defineEmits<{
  "update:modelValue": [TimeWindow];
  "update:zonePreference": ["utc" | "local"];
}>();

// UI state
const showDatePicker = ref(false);
const showFromCalendar = ref(false);
const showToCalendar = ref(false);
const quickRangeSearch = ref("");

// Today in the picker's zone focuses the calendars.
const todayDate = computed(() => today(props.resolved.zone));

// Recently used absolute ranges, stored as instants and shown in the current zone.
// Older entries stored zone-less wall-clock text and are dropped.
const RECENT_RANGES_KEY = "logchef_recent_ranges";
type RecentRange = { version: 2; start: EpochMs; end: EpochMs };

function isRecentRange(value: unknown): value is RecentRange {
  if (typeof value !== "object" || value === null) return false;
  const { version, start, end } = value as Record<string, unknown>;
  return version === 2 && Number.isFinite(start) && Number.isFinite(end);
}

function loadRecentRanges(): RecentRange[] {
  try {
    const stored: unknown = JSON.parse(localStorage.getItem(RECENT_RANGES_KEY) || "[]");
    return Array.isArray(stored) ? stored.filter(isRecentRange) : [];
  } catch {
    return [];
  }
}

const recentRanges = ref<RecentRange[]>(loadRecentRanges());

function rememberRange(start: EpochMs, end: EpochMs) {
  const others = recentRanges.value.filter((r) => r.start !== start || r.end !== end);
  recentRanges.value = [{ version: 2 as const, start, end }, ...others].slice(0, 5);
  localStorage.setItem(RECENT_RANGES_KEY, JSON.stringify(recentRanges.value));
}

function recentRangeLabel(range: RecentRange): string {
  const { zone } = props.resolved;
  return t("time.range", { from: formatWallClock(range.start, zone), to: formatWallClock(range.end, zone) });
}

// Draft text for the absolute inputs, and the instants the text was written from.
// Applying text the user did not edit keeps the window, or the instant of an
// unedited wall-clock bound. A "now-15m" bound has no fixed instant.
const draftState = ref({ from: "now-1h", to: "now" });
let shown: { from: string; to: string; start: EpochMs | null; end: EpochMs | null } | null = null;

const DATE_ONLY_REGEX = /^\d{4}-\d{2}-\d{2}$/;
const DATE_TIME_NO_SECONDS_REGEX = /^\d{4}-\d{2}-\d{2}[ T]\d{2}:\d{2}$/;
const DATE_TIME_WITH_SECONDS_REGEX = /^\d{4}-\d{2}-\d{2}[ T]\d{2}:\d{2}:\d{2}$/;

function quickRangeLabel(range: QuickRange): string {
  const label = windowLabel({ kind: "relative", spec: range });
  return label ? t(label.key, { count: label.count }, label.count) : range;
}

// Filtered quick ranges based on search
const filteredQuickRanges = computed(() => {
  if (!quickRangeSearch.value) return QUICK_RANGES;
  const search = quickRangeSearch.value.toLowerCase();
  return QUICK_RANGES.filter(r =>
    quickRangeLabel(r).toLowerCase().includes(search) ||
    r.toLowerCase().includes(search)
  );
});

const selectedQuickRange = computed(() => (props.modelValue.kind === "relative" ? props.modelValue.spec : null));

// Sync the draft text with the window and the instants it resolved to.
watch(
  () => [props.modelValue, props.resolved] as const,
  ([window, { start, end, zone }]) => {
    const isDuration = window.kind === "relative" && /^\d+[mhd]$/.test(window.spec);
    const from = isDuration ? `now-${window.spec}` : formatWallClock(start, zone);
    const to = isDuration ? "now" : formatWallClock(end, zone);
    draftState.value = { from, to };
    shown = isDuration ? { from, to, start: null, end: null } : { from, to, start, end };
  },
  { immediate: true }
);

function normalizeAbsoluteInput(value: string, defaultTime = "00:00:00"): string {
  const trimmed = value.trim();

  if (!trimmed) return trimmed;
  if (trimmed === "now" || trimmed.startsWith("now-")) return trimmed;

  if (DATE_ONLY_REGEX.test(trimmed)) {
    return `${trimmed} ${defaultTime}`;
  }

  if (DATE_TIME_NO_SECONDS_REGEX.test(trimmed)) {
    return `${trimmed.replace("T", " ")}:00`;
  }

  if (DATE_TIME_WITH_SECONDS_REGEX.test(trimmed)) {
    return trimmed.replace("T", " ");
  }

  return trimmed;
}

function normalizeDraftInput(type: "from" | "to") {
  const defaultTime = type === "to" ? "23:59:59" : "00:00:00";
  const normalized = normalizeAbsoluteInput(draftState.value[type], defaultTime);
  if (normalized !== draftState.value[type]) {
    draftState.value[type] = normalized;
  }
}

function applyDefaultTime(type: "from" | "to", defaultTime = "00:00:00") {
  const trimmed = draftState.value[type].trim();
  const match = trimmed.match(/^(\d{4}-\d{2}-\d{2})(?:[ T].*)?$/);
  if (!match) return;

  draftState.value[type] = `${match[1]} ${defaultTime}`;
}

const NOW_UNIT_MS: Record<string, number> = { m: 60_000, h: 3_600_000, d: 86_400_000 };

// "now", "now-Xm|h|d", or wall-clock text in the picker's zone.
function parseInput(input: string, nowMs: number): EpochMs | null {
  const trimmed = input.trim().toLowerCase();
  if (trimmed === "now") return epochMs(nowMs);
  const relativeMatch = trimmed.match(/^now-(\d+)(m|h|d)$/);
  if (relativeMatch) return epochMs(nowMs - Number(relativeMatch[1]) * NOW_UNIT_MS[relativeMatch[2]]);
  return parseWallClock(normalizeAbsoluteInput(input), props.resolved.zone);
}

function applyQuickRange(range: QuickRange) {
  if (props.disabledReason) return;
  emit("update:modelValue", { kind: "relative", spec: range });
  showDatePicker.value = false;
}

function applyAbsoluteRange() {
  if (props.disabledReason) return;
  normalizeDraftInput("from");
  normalizeDraftInput("to");

  const { from, to } = draftState.value;
  if (props.modelValue.kind === "relative" && shown?.from === from && shown.to === to) {
    emit("update:modelValue", props.modelValue);
    showDatePicker.value = false;
    return;
  }
  const nowMs = Date.now();
  const start = (shown?.from === from ? shown.start : null) ?? parseInput(from, nowMs);
  const end = (shown?.to === to ? shown.end : null) ?? parseInput(to, nowMs);

  if (start === null || end === null || start > end) {
    return;
  }

  rememberRange(start, end);
  emit("update:modelValue", { kind: "absolute", start, end });
  showDatePicker.value = false;
}

function applyRecentRange(range: RecentRange) {
  if (props.disabledReason) return;
  rememberRange(range.start, range.end);
  emit("update:modelValue", { kind: "absolute", start: range.start, end: range.end });
  showDatePicker.value = false;
}

function handleCalendarSelect(type: 'from' | 'to', date: DateValue | undefined | null) {
  if (!date) return;
  const defaultTime = type === "to" ? "23:59:59" : "00:00:00";
  draftState.value[type] = `${date.toString().slice(0, 10)} ${defaultTime}`;
  normalizeDraftInput(type);

  if (type === 'from') {
    showFromCalendar.value = false;
  } else {
    showToCalendar.value = false;
  }
}

function clearRecentRanges() {
  recentRanges.value = [];
  localStorage.removeItem(RECENT_RANGES_KEY);
}

// Display text for trigger button
const triggerDisplayText = computed(() => {
  const label = windowLabel(props.modelValue);
  if (label) return t(label.key, { count: label.count }, label.count);
  const { start, end, zone } = props.resolved;
  return `${formatWallClock(start, zone)} - ${formatWallClock(end, zone)}`;
});

watch(
  () => props.disabledReason,
  (reason) => {
    if (!reason) return;
    showDatePicker.value = false;
    showFromCalendar.value = false;
    showToCalendar.value = false;
  }
);

// Function to open the date picker programmatically
function openDatePicker() {
  if (!props.disabledReason) showDatePicker.value = true;
}

const zoneOptions: ReadonlyArray<"local" | "utc"> = ["local", "utc"];
const isUtc = computed(() => props.zonePreference === "utc");
const timezoneDisplay = computed(() => (isUtc.value ? "UTC" : t("time.browser", { zone: props.resolved.zone })));

function toggleZonePreference() {
  emit("update:zonePreference", isUtc.value ? "local" : "utc");
}

// Expose methods to parent component
defineExpose({
  openDatePicker,
});
</script>

<template>
  <div :class="cn('flex items-center gap-1', props.class)">
    <Popover v-model:open="showDatePicker">
      <TooltipProvider>
        <Tooltip :disabled="!props.disabledReason">
          <TooltipTrigger as-child>
            <!-- A disabled button gets no pointer events, so the wrapper carries the tooltip. -->
            <span class="inline-flex" :class="{ 'cursor-not-allowed': props.disabledReason }">
              <PopoverTrigger as-child>
                <Button
                  variant="ghost"
                  :class="cn('h-7 px-2.5 gap-1.5 font-normal text-sm border border-transparent', props.disabledReason ? 'pointer-events-none opacity-50' : 'hover:border-border hover:bg-muted/50')"
                  :disabled="!!props.disabledReason"
                >
                  <Clock class="h-3.5 w-3.5 text-muted-foreground" />
                  <span>{{ triggerDisplayText }}</span>
                  <ChevronDown class="h-3 w-3 text-muted-foreground ml-0.5" />
                </Button>
              </PopoverTrigger>
            </span>
          </TooltipTrigger>
          <TooltipContent side="bottom">
            <p class="text-xs">{{ props.disabledReason }}</p>
          </TooltipContent>
        </Tooltip>
      </TooltipProvider>
      
      <PopoverContent
        class="w-[580px] p-0"
        align="start"
        side="bottom"
      >
        <div class="flex">
          <!-- Left Panel: Absolute time range -->
          <div class="w-[260px] p-4 border-r">
            <h4 class="text-sm font-medium mb-3">{{ t('time.absolute') }}</h4>
            
            <!-- From input -->
            <div class="space-y-1.5 mb-3">
              <label class="text-xs text-muted-foreground">{{ t('time.from') }}</label>
              <div class="relative">
                <Input
                  v-model="draftState.from"
                  class="h-8 text-sm font-mono pr-8"
                  placeholder="now-1h or YYYY-MM-DD (time optional)"
                  @keydown.enter="applyAbsoluteRange"
                  @blur="normalizeDraftInput('from')"
                  @dblclick="applyDefaultTime('from', '00:00:00')"
                />
                <Popover v-model:open="showFromCalendar">
                  <PopoverTrigger as-child>
                    <button class="absolute right-1 top-1 p-1 hover:bg-muted rounded">
                      <CalendarIcon class="h-4 w-4 text-muted-foreground" />
                    </button>
                  </PopoverTrigger>
                  <PopoverContent class="w-auto p-0" side="right" align="start">
                    <Calendar
                      :locale="locale"
                      class="rounded-md border"
                      :placeholder="todayDate"
                      @update:model-value="(date) => handleCalendarSelect('from', date)"
                    />
                  </PopoverContent>
                </Popover>
              </div>
            </div>
            
            <!-- To input -->
            <div class="space-y-1.5 mb-4">
              <label class="text-xs text-muted-foreground">{{ t('time.to') }}</label>
              <div class="relative">
                <Input
                  v-model="draftState.to"
                  class="h-8 text-sm font-mono pr-8"
                  placeholder="now or YYYY-MM-DD (time optional)"
                  @keydown.enter="applyAbsoluteRange"
                  @blur="normalizeDraftInput('to')"
                  @dblclick="applyDefaultTime('to', '23:59:59')"
                />
                <Popover v-model:open="showToCalendar">
                  <PopoverTrigger as-child>
                    <button class="absolute right-1 top-1 p-1 hover:bg-muted rounded">
                      <CalendarIcon class="h-4 w-4 text-muted-foreground" />
                    </button>
                  </PopoverTrigger>
                  <PopoverContent class="w-auto p-0" side="right" align="start">
                    <Calendar
                      :locale="locale"
                      class="rounded-md border"
                      :placeholder="todayDate"
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
              {{ t('time.apply') }}
            </Button>
            
            <!-- Recently used -->
            <div v-if="recentRanges.length > 0" class="mt-4 pt-4 border-t">
              <div class="flex items-center justify-between mb-2">
                <span class="text-xs text-muted-foreground">{{ t('time.recent') }}</span>
                <button 
                  class="text-xs text-muted-foreground hover:text-foreground"
                  @click="clearRecentRanges"
                >
                  {{ t('ui.clear') }}
                </button>
              </div>
              <div class="space-y-1">
                <button
                  v-for="(range, idx) in recentRanges"
                  :key="idx"
                  class="w-full text-left text-xs p-1.5 hover:bg-muted rounded truncate"
                  @click="applyRecentRange(range)"
                >
                  {{ recentRangeLabel(range) }}
                </button>
              </div>
            </div>
            
            <!-- Timezone info -->
            <div class="mt-4 pt-4 border-t">
              <div class="flex items-center justify-between">
                <span class="text-xs text-muted-foreground">{{ timezoneDisplay }}</span>
                <button 
                  class="text-xs text-primary hover:underline"
                  @click="toggleZonePreference"
                >
                  {{ t('time.change') }}
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
                :placeholder="t('time.searchRanges')"
              />
            </div>
            
            <!-- Quick range list -->
            <div class="space-y-0.5 max-h-[320px] overflow-y-auto">
              <button
                v-for="range in filteredQuickRanges"
                :key="range"
                :class="[
                  'w-full text-left px-3 py-2 text-sm rounded-md transition-colors',
                  selectedQuickRange === range
                    ? 'bg-primary/10 text-primary border-l-2 border-primary' 
                    : 'hover:bg-muted'
                ]"
                @click="applyQuickRange(range)"
              >
                {{ quickRangeLabel(range) }}
              </button>
              
              <div v-if="filteredQuickRanges.length === 0" class="text-sm text-muted-foreground text-center py-4">
                {{ t('time.noRanges') }}
              </div>
            </div>
          </div>
        </div>
      </PopoverContent>
    </Popover>
    
    <div class="inline-flex h-7 items-center rounded-md bg-muted/60 p-0.5 text-xs" role="group" :title="t('pages.toggleTimezone')">
      <button
        v-for="zone in zoneOptions"
        :key="zone"
        type="button"
        class="h-6 rounded px-2 font-normal transition-colors"
        :class="zonePreference === zone ? 'bg-background text-foreground shadow-sm' : 'text-muted-foreground hover:text-foreground'"
        :aria-pressed="zonePreference === zone"
        @click="zonePreference !== zone && emit('update:zonePreference', zone)"
      >
        {{ zone === "utc" ? "UTC" : t("ui.local") }}
      </button>
    </div>
  </div>
</template>
