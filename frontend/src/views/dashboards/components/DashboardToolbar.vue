<script setup lang="ts">
import { computed, ref } from "vue";
import type { DateRange } from "reka-ui";
import { RefreshCw, ChevronDown } from "lucide-vue-next";
import { Button } from "@/components/ui/button";
import {
  DropdownMenu,
  DropdownMenuTrigger,
  DropdownMenuContent,
  DropdownMenuItem,
} from "@/components/ui/dropdown-menu";
import { DateTimePicker } from "@/components/date-time-picker";
import { useDashboardsStore } from "@/stores/dashboards";
import {
  timestampToCalendarDateTime,
  parseRelativeTimeString,
  calendarDateTimeToTimestamp,
} from "@/utils/time";

const store = useDashboardsStore();
const dateTimePickerRef = ref<InstanceType<typeof DateTimePicker> | null>(null);

const REFRESH_OPTIONS = [
  { label: "Off", ms: 0 },
  { label: "30s", ms: 30_000 },
  { label: "1m", ms: 60_000 },
  { label: "5m", ms: 300_000 },
];

const refreshLabel = computed(
  () => REFRESH_OPTIONS.find((o) => o.ms === store.refreshIntervalMs)?.label ?? "Off"
);

// Feed the picker a DateRange derived from the store's effective (absolute) range.
const pickerModel = computed<DateRange>(() => {
  const range = store.effectiveRange;
  return {
    start: timestampToCalendarDateTime(range.start),
    end: timestampToCalendarDateTime(range.end),
  } as DateRange;
});

const selectedQuickRange = computed(() =>
  store.timeRelative ? `Last ${store.timeRelative}` : null
);

function handleRangeChange(value: any) {
  const quick = dateTimePickerRef.value?.selectedQuickRange as string | null | undefined;
  if (quick) {
    const relative = quick.replace(/^Last\s+/i, "").trim();
    try {
      parseRelativeTimeString(relative);
      store.setRelativeTime(relative);
      return;
    } catch {
      // not a parseable relative preset — fall through to absolute
    }
  }
  if (value?.start && value?.end) {
    store.setAbsoluteRange(
      calendarDateTimeToTimestamp(value.start),
      calendarDateTimeToTimestamp(value.end)
    );
  }
}

function selectRefresh(ms: number) {
  store.setRefreshInterval(ms);
}

const isRefreshing = computed(() => store.isLoadingOperation("loadDashboard"));

function manualRefresh() {
  void store.refreshAllPanels();
}
</script>

<template>
  <div class="flex items-center gap-2 flex-wrap">
    <DateTimePicker
      ref="dateTimePickerRef"
      :model-value="pickerModel"
      :selected-quick-range="selectedQuickRange"
      @update:model-value="handleRangeChange"
    />

    <DropdownMenu>
      <DropdownMenuTrigger as-child>
        <Button variant="outline" size="sm" class="h-8 gap-1 text-xs" title="Auto-refresh interval">
          <RefreshCw class="h-3.5 w-3.5" />
          <span>{{ refreshLabel }}</span>
          <ChevronDown class="h-3 w-3 opacity-60" />
        </Button>
      </DropdownMenuTrigger>
      <DropdownMenuContent align="end" class="w-28">
        <DropdownMenuItem
          v-for="opt in REFRESH_OPTIONS"
          :key="opt.ms"
          class="text-xs"
          :class="{ 'font-semibold': opt.ms === store.refreshIntervalMs }"
          @click="selectRefresh(opt.ms)"
        >
          {{ opt.label }}
        </DropdownMenuItem>
      </DropdownMenuContent>
    </DropdownMenu>

    <Button
      variant="outline"
      size="sm"
      class="h-8 w-8 p-0"
      title="Refresh now"
      :disabled="isRefreshing"
      @click="manualRefresh"
    >
      <RefreshCw class="h-3.5 w-3.5" :class="{ 'animate-spin': isRefreshing }" />
    </Button>
  </div>
</template>
