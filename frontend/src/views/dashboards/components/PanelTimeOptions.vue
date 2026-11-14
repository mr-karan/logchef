<script setup lang="ts">
import { computed, ref, watch } from "vue";
import { useI18n } from "vue-i18n";
import { Input } from "@/components/ui/input";
import { Label } from "@/components/ui/label";
import type { DashboardPanelQuery, PanelInterval } from "@/api/dashboards";
import { isDashboardRange, resultInterval } from "@/utils/dashboardPanels";
import { PANEL_INTERVALS, panelTimeBound } from "@/utils/dashboardRuntime";
import { CALENDAR_RANGES, relativeRangeLabel } from "@/utils/rangeLabel";
import { parseRelativeSpec, type TimeWindow } from "@/utils/timeWindow";

// The bucket size and the range of one panel. A control set back to its
// default removes its key from the query, so the blob stops using the feature.
const props = defineProps<{
  query: DashboardPanelQuery;
  /** The dashboard's range, which the panel follows unless it sets its own. */
  dashboardWindow: TimeWindow;
}>();
const emit = defineEmits<{ (e: "update:query", patch: Partial<DashboardPanelQuery>): void }>();
const { t } = useI18n();

const RANGE_PRESETS: readonly string[] = ["1h", "6h", "24h", "7d", "30d", ...CALENDAR_RANGES];
// Kept out of the template, where "}}" would end the interpolation.
const INTERVAL_MACRO = "{{__interval}}";
const TIME_FILTER_MACRO = "{{__time_filter}}";
const CUSTOM = "custom";

function rangeLabel(spec: string): string {
  const label = relativeRangeLabel(spec);
  return label ? t(label.key, { count: label.count }, label.count) : spec;
}

function setInterval(event: Event) {
  if (!(event.target instanceof HTMLSelectElement)) return;
  const value = event.target.value as "auto" | PanelInterval;
  // A count query stores `auto` by name. A result query leaves the default out.
  emit("update:query", props.query.mode === "count" ? { interval: value } : { interval: resultInterval(value).interval });
}

// The custom input stays open while its text is not a range yet.
const customOpen = ref(props.query.range !== undefined && !RANGE_PRESETS.includes(props.query.range));
const customText = ref(props.query.range ?? "");
watch(() => props.query.range, (range) => {
  if (range === undefined) return;
  customText.value = range;
  if (!RANGE_PRESETS.includes(range)) customOpen.value = true;
});
const rangeChoice = computed(() => (customOpen.value ? CUSTOM : props.query.range ?? ""));
const customInvalid = computed(() => customOpen.value && customText.value !== "" && !isDashboardRange(customText.value));

function setRange(event: Event) {
  if (!(event.target instanceof HTMLSelectElement)) return;
  const value = event.target.value;
  customOpen.value = value === CUSTOM;
  if (value === CUSTOM) {
    customText.value = props.query.range ?? "";
    return;
  }
  emit("update:query", { range: value === "" ? undefined : value });
}

function setCustomRange(value: string | number) {
  customText.value = String(value).trim();
  if (isDashboardRange(customText.value)) emit("update:query", { range: customText.value });
  else if (customText.value === "") emit("update:query", { range: undefined });
}

const fixedWindow = computed(() => !panelTimeBound(props.query));

// Day buckets only line up with a range that starts at midnight.
const dayBucketsOnRollingRange = computed(() => {
  if (props.query.interval !== "24h" || fixedWindow.value) return false;
  const spec = props.query.range ?? (props.dashboardWindow.kind === "relative" ? props.dashboardWindow.spec : null);
  return spec !== null && parseRelativeSpec(spec)?.kind === "duration";
});
</script>

<template>
  <div class="space-y-3" data-testid="panel-time-options">
    <div class="space-y-1">
      <Label for="panel-interval">{{ t('dashboardBuilder.BucketSize') }}</Label>
      <select
        id="panel-interval"
        class="w-full rounded border bg-background p-1.5 text-xs"
        :value="query.interval ?? 'auto'"
        @change="setInterval"
      >
        <option value="auto">{{ t('dashboardBuilder.BucketSizeAuto') }}</option>
        <option v-for="entry in PANEL_INTERVALS" :key="entry.window" :value="entry.window">{{ entry.window }}</option>
      </select>
      <p v-if="query.mode === 'result'" class="text-[11px] text-muted-foreground">
        {{ t('dashboardBuilder.BucketSizeMacroHint', { macro: INTERVAL_MACRO }) }}
      </p>
    </div>
    <div class="space-y-1">
      <Label for="panel-range">{{ t('dashboardBuilder.TimeRange') }}</Label>
      <select
        id="panel-range"
        class="w-full rounded border bg-background p-1.5 text-xs"
        :value="rangeChoice"
        :disabled="fixedWindow"
        @change="setRange"
      >
        <option value="">{{ t('dashboardBuilder.TimeRangeDashboard') }}</option>
        <option v-for="spec in RANGE_PRESETS" :key="spec" :value="spec">{{ rangeLabel(spec) }}</option>
        <option :value="CUSTOM">{{ t('dashboardBuilder.TimeRangeCustom') }}</option>
      </select>
      <Input
        v-if="customOpen && !fixedWindow"
        id="panel-range-custom"
        :model-value="customText"
        :aria-label="t('dashboardBuilder.TimeRangeCustom')"
        :aria-invalid="customInvalid"
        placeholder="today-6d"
        class="h-8 text-xs"
        @update:model-value="setCustomRange"
      />
      <p v-if="customInvalid" class="text-[11px] text-destructive" data-testid="panel-range-invalid">
        {{ t('dashboardBuilder.TimeRangeInvalid') }}
      </p>
      <p v-if="fixedWindow" class="text-[11px] text-muted-foreground" data-testid="panel-fixed-window-hint">
        {{ t('dashboardBuilder.FixedWindowHint', { macro: TIME_FILTER_MACRO }) }}
      </p>
      <p v-if="dayBucketsOnRollingRange" class="text-[11px] text-amber-700 dark:text-amber-400" data-testid="panel-day-buckets-hint">
        {{ t('dashboardBuilder.DayBucketsNeedCalendarRange') }}
      </p>
    </div>
  </div>
</template>
