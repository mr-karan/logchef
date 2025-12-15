<script setup lang="ts">
import { ref, computed } from "vue";
import { Button } from "@/components/ui/button";
import { DateTimePicker } from "@/components/date-time-picker";
import { useTimeRange } from "@/composables/useTimeRange";
import { useExploreStore } from "@/stores/explore";
import { useQuery } from "@/composables/useQuery";
import {
  DropdownMenu,
  DropdownMenuContent,
  DropdownMenuItem,
  DropdownMenuLabel,
  DropdownMenuSeparator,
  DropdownMenuTrigger,
} from "@/components/ui/dropdown-menu";
import { useSourcesStore } from "@/stores/sources";
import { SqlManager } from "@/services/SqlManager";

const exploreStore = useExploreStore();
const { timeRange, quickRangeLabelToRelativeTime } = useTimeRange();
const { isDirty, dirtyReason } = useQuery();

// Reference to the DateTimePicker component
const dateTimePickerRef = ref<InstanceType<typeof DateTimePicker> | null>(null);

// Helper function to convert relative time format to DateTimePicker's quick range label format
const quickRangeLabelFromRelativeTime = (
  relativeTime: string
): string | null => {
  // Map from relative time format to DateTimePicker quick range label
  switch (relativeTime) {
    case "5m":
      return "Last 5m";
    case "15m":
      return "Last 15m";
    case "30m":
      return "Last 30m";
    case "1h":
      return "Last 1h";
    case "3h":
      return "Last 3h";
    case "6h":
      return "Last 6h";
    case "12h":
      return "Last 12h";
    case "24h":
      return "Last 24h";
    case "2d":
      return "Last 2d";
    case "7d":
      return "Last 7d";
    case "30d":
      return "Last 30d";
    case "90d":
      return "Last 90d";
    default:
      return null;
  }
};

// Handle date range selection
const handleDateRangeChange = (value: any) => {
  console.log("TimeRangeSelector: Time range changed");

  // Check if this update came from a quick range selection in DateTimePicker
  if (dateTimePickerRef.value?.selectedQuickRange) {
    // Convert quick range label to relative time format
    const relativeTime = quickRangeLabelToRelativeTime(
      dateTimePickerRef.value.selectedQuickRange
    );
    if (relativeTime) {
      console.log(
        "TimeRangeSelector: Setting relative time range:",
        relativeTime
      );
      // Set the relative time in the store (which also sets the absolute time range)
      exploreStore.setRelativeTimeRange(relativeTime);

      // Update SQL if needed - ensure query includes time constraints
      if (exploreStore.activeMode === 'sql') {
        updateSqlForTimeChange();
      }

      // Debug dirty state and force UI update
      setTimeout(() => {
        const dirtyState = isDirty.value;
        console.log(
          "TimeRangeSelector: After relative time change - isDirty:",
          dirtyState,
          "dirtyReason:",
          JSON.stringify(dirtyReason.value),
          "lastExecutedState:",
          exploreStore.lastExecutedState ? "exists" : "null"
        );
      }, 100);

      return;
    }
  }

  // If no relative time was found, update as absolute time range
  // This also clears the selectedRelativeTime in the store
  console.log("TimeRangeSelector: Setting absolute time range");
  timeRange.value = value;

  // Update SQL if needed - ensure query includes time constraints
  if (exploreStore.activeMode === 'sql') {
    updateSqlForTimeChange();
  }

  // Debug dirty state and force UI update
  setTimeout(() => {
    const dirtyState = isDirty.value;
    console.log(
      "TimeRangeSelector: After absolute time change - isDirty:",
      dirtyState,
      "dirtyReason:",
      JSON.stringify(dirtyReason.value),
      "lastExecutedState:",
      exploreStore.lastExecutedState ? "exists" : "null"
    );
  }, 100);
};

// Add new function to intelligently update SQL when time changes
const updateSqlForTimeChange = () => {
  // Only update SQL if we're in SQL mode and have source details
  if (exploreStore.activeMode !== 'sql' || !timeRange.value) {
    return;
  }

  // Get the current SQL and source details
  const currentSql = exploreStore.rawSql;
  const sourceDetails = useSourcesStore().currentSourceDetails;

  if (!sourceDetails || !currentSql) {
    return;
  }

  try {
    // Use SqlManager to update time range in SQL
    const updatedSql = SqlManager.updateTimeRange({
      sql: currentSql,
      tsField: sourceDetails._meta_ts_field || 'timestamp',
      timeRange: timeRange.value,
      timezone: exploreStore.selectedTimezoneIdentifier || undefined
    });

    // Update SQL if it was changed
    if (updatedSql !== currentSql) {
      exploreStore.setRawSql(updatedSql);
      console.log("TimeRangeSelector: SQL updated for new time range");
    }
  } catch (err) {
    console.error("Error updating SQL for time range:", err);
  }
};

// Handler for timezone changes from DateTimePicker
const handleTimezoneChange = (timezoneId: string) => {
  console.log("TimeRangeSelector: Timezone changed to", timezoneId);
  exploreStore.setTimezoneIdentifier(timezoneId);
};

// Function to set limit
const handleLimitChange = (limit: number) => {
  console.log("TimeRangeSelector: Setting new limit:", limit);
  exploreStore.setLimit(limit);

  // Update SQL query when in SQL mode
  if (exploreStore.activeMode === 'sql') {
    updateSqlForLimitChange(limit);
  }
};

// Add new function to update SQL when limit changes
const updateSqlForLimitChange = (newLimit: number) => {
  // Only update SQL if we're in SQL mode
  if (exploreStore.activeMode !== 'sql') {
    return;
  }

  const currentSql = exploreStore.rawSql;
  if (!currentSql.trim()) {
    return;
  }

  try {
    // Use SqlManager to update limit in SQL
    const updatedSql = SqlManager.updateLimit(currentSql, newLimit);

    // Update SQL if it was changed
    if (updatedSql !== currentSql) {
      exploreStore.setRawSql(updatedSql);
      console.log("TimeRangeSelector: Updated SQL query with new limit");
    }
  } catch (error) {
    console.error("Error updating SQL with new limit:", error);
  }
};

// Function to open date picker programmatically
const openDatePicker = () => {
  if (dateTimePickerRef.value) {
    dateTimePickerRef.value.openDatePicker();
  }
};

// Current limit for display
const currentLimit = computed(() => exploreStore.limit);

// Expose method to parent components
defineExpose({
  openDatePicker,
});
</script>

<template>
  <div class="flex items-center space-x-2 flex-grow">
    <!-- Date/Time Picker -->
    <DateTimePicker ref="dateTimePickerRef" :model-value="timeRange" :selectedQuickRange="exploreStore.selectedRelativeTime
      ? quickRangeLabelFromRelativeTime(exploreStore.selectedRelativeTime)
      : null
      " @update:model-value="handleDateRangeChange" @update:timezone="handleTimezoneChange" class="h-8" />

    <!-- Limit Dropdown -->
    <DropdownMenu>
      <DropdownMenuTrigger asChild>
        <Button variant="outline" size="sm" class="h-8 text-sm justify-between px-2 min-w-[90px]">
          <span>Limit:</span> {{ currentLimit.toLocaleString() }}
        </Button>
      </DropdownMenuTrigger>
      <DropdownMenuContent align="end">
        <DropdownMenuLabel>Results Limit</DropdownMenuLabel>
        <DropdownMenuSeparator />
        <DropdownMenuItem v-for="limit in [100, 500, 1000, 2000, 5000, 10000]" :key="limit"
          @click="handleLimitChange(limit)" :disabled="currentLimit === limit">
          {{ limit.toLocaleString() }} rows
        </DropdownMenuItem>
      </DropdownMenuContent>
    </DropdownMenu>
  </div>
</template>
