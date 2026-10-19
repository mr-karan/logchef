<script setup lang="ts">
import { useI18n } from "vue-i18n";

import { computed } from "vue";
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
import { usePreferencesStore } from "@/stores/preferences";
import { epochMs, formatInZone } from "@/utils/timeWindow";

const { t } = useI18n();

const store = useDashboardsStore();
const preferencesStore = usePreferencesStore();

const REFRESH_OPTIONS = [
  { get label() { return t('ui.off'); }, ms: 0 },
  { label: "30s", ms: 30_000 },
  { label: "1m", ms: 60_000 },
  { label: "5m", ms: 300_000 },
];

const refreshLabel = computed(
  () => REFRESH_OPTIONS.find((o) => o.ms === store.refreshIntervalMs)?.label ?? "Off"
);

// The window actually queried by the last refresh (may differ from the
// selection when caching snaps a rolling range to a TTL bucket). Shown so users
// can see what was executed rather than only what they picked.
function fmtTime(ms: number): string {
  return formatInZone(epochMs(ms), preferencesStore.zone, {
    month: "short",
    day: "numeric",
    hour: "2-digit",
    minute: "2-digit",
    second: "2-digit",
  });
}
const appliedRangeLabel = computed(() => {
  const r = store.appliedRange;
  if (!r) return null;
  return `${fmtTime(r.start)} → ${fmtTime(r.end)}`;
});

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
      :model-value="store.window"
      :resolved="store.effectiveRange"
      :zone-preference="preferencesStore.preferences.timezone"
      @update:model-value="store.setWindow"
      @update:zone-preference="preferencesStore.updatePreferences({ timezone: $event })"
    />

    <DropdownMenu>
      <DropdownMenuTrigger as-child>
        <Button variant='outline' size="sm" class='h-8 gap-1 text-xs' :title="t('ui.autoRefreshInterval')">
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
      :title="t('ui.refreshNow')"
      :disabled="isRefreshing"
      @click="manualRefresh"
    >
      <RefreshCw class="h-3.5 w-3.5" :class="{ 'animate-spin': isRefreshing }" />
    </Button>

    <span
      v-if="appliedRangeLabel"
      class="text-[11px] text-muted-foreground tabular-nums whitespace-nowrap"
      :title="t('ui.timeWindowActuallyQueriedMayBeSnappedToTheCacheBucket')"
    >
      {{ t('ui.queried') }} {{ appliedRangeLabel }}
    </span>
  </div>
</template>
