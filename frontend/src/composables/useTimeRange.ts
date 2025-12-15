import { computed } from 'vue';
import { useExploreStore } from '@/stores/explore';
import {
  CalendarDateTime,
  getLocalTimeZone,
  type DateValue,
  toCalendarDateTime,
  fromDate
} from '@internationalized/date';
import { relativeTimeToLabel } from '@/utils/time';

/**
 * Composable for handling time range functionality including
 * absolute and relative time management
 */
export function useTimeRange() {
  const exploreStore = useExploreStore();

  // Time range helpers
  const calendarDateTimeToTimestamp = (dateTime: DateValue | null | undefined): number | null => {
    if (!dateTime) return null;
    try {
      // Convert any DateValue to JS Date object using the local timezone
      const date = dateTime.toDate(getLocalTimeZone());
      return date.getTime();
    } catch (e) {
      console.error("Error converting DateValue to timestamp:", e);
      return null;
    }
  };

  const timestampToCalendarDateTime = (timestamp: number | null): CalendarDateTime | null => {
    if (timestamp === null) return null;
    try {
      const date = new Date(timestamp);
      if (isNaN(date.getTime())) return null;
      // Construct CalendarDateTime directly from JS Date parts
      return new CalendarDateTime(
        date.getFullYear(),
        date.getMonth() + 1, // JS month is 0-indexed
        date.getDate(),
        date.getHours(),
        date.getMinutes(),
        date.getSeconds(),
        date.getMilliseconds()
      );
    } catch (e) {
      console.error("Error converting timestamp to CalendarDateTime:", e);
      return null;
    }
  };

  // Get current time range from store
  const timeRange = computed({
    get() {
      return exploreStore.timeRange;
    },
    set(value) {
      // Handle null value properly - only set if there's an actual value
      if (value) {
        // Set the time range in the store using setTimeConfiguration
        exploreStore.setTimeConfiguration({
          absoluteRange: value
        });
      }

      // Note: setTimeConfiguration will automatically clear selectedRelativeTime
      // when setting an absolute time range, so we don't need to explicitly
      // call setRelativeTimeRange(null)
    }
  });

  // Format dates for SQL
  const formatSqlDateTime = (date: DateValue | undefined): string => {
    if (!date) {
      // Default to current date minus 1 hour if no date provided
      const now = new Date();
      const oneHourAgo = new Date(now.getTime() - 3600000);
      return oneHourAgo.toISOString().replace('T', ' ').substring(0, 19);
    }

    try {
      const zonedDate = toCalendarDateTime(date);
      const isoString = zonedDate.toString();
      // Format as 'YYYY-MM-DD HH:MM:SS'
      return isoString.replace('T', ' ').substring(0, 19);
    } catch (e) {
      console.error("Error formatting date for SQL:", e);
      return new Date().toISOString().replace('T', ' ').substring(0, 19);
    }
  };

  // Apply relative time range
  const applyRelativeTimeRange = (relativeTime: string) => {
    exploreStore.setRelativeTimeRange(relativeTime);
  };

  // Function to map quick range labels to relativeTime format
  const quickRangeLabelToRelativeTime = (label: string): string | null => {
    switch (label) {
      case 'Last 5m': return '5m';
      case 'Last 15m': return '15m';
      case 'Last 30m': return '30m';
      case 'Last 1h': return '1h';
      case 'Last 3h': return '3h';
      case 'Last 6h': return '6h';
      case 'Last 12h': return '12h';
      case 'Last 24h': return '24h';
      case 'Last 2d': return '2d';
      case 'Last 7d': return '7d';
      case 'Last 30d': return '30d';
      case 'Last 90d': return '90d';
      default: return null;
    }
  };

  // Handle histogram time range zooming
  const handleHistogramTimeRangeZoom = (range: { start: Date; end: Date }) => {
    try {
      // Convert native Dates directly to CalendarDateTime
      const start = toCalendarDateTime(fromDate(range.start, getLocalTimeZone()));
      const end = toCalendarDateTime(fromDate(range.end, getLocalTimeZone()));

      // Update the store's time range using the appropriate action
      // Use setTimeConfiguration as this is an absolute range selection
      exploreStore.setTimeConfiguration({
        absoluteRange: { start, end }
      });

      return true;
    } catch (e) {
      console.error('Error handling histogram time range:', e);
      return false;
    }
  };

  // Set default time range (last 15 minutes)
  const setDefaultTimeRange = () => {
    // Use relative time format instead of absolute timestamps
    exploreStore.setRelativeTimeRange('15m');
  };

  // Get human-readable time range label
  const getHumanReadableTimeRange = computed(() => {
    const currentRelativeTime = exploreStore.selectedRelativeTime;
    if (currentRelativeTime) {
      return relativeTimeToLabel(currentRelativeTime);
    }
    return null;
  });

  return {
    timeRange,
    calendarDateTimeToTimestamp,
    timestampToCalendarDateTime,
    formatSqlDateTime,
    applyRelativeTimeRange,
    quickRangeLabelToRelativeTime,
    handleHistogramTimeRangeZoom,
    setDefaultTimeRange,
    getHumanReadableTimeRange
  };
}
