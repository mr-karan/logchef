import { computed } from 'vue';
import { useExploreStore } from '@/stores/explore';
import { type DateValue, toCalendarDateTime } from '@internationalized/date';
import { calendarDateTimeToTimestamp, relativeTimeToLabel, timestampToCalendarDateTime } from '@/utils/time';

/**
 * Composable for handling time range functionality including
 * absolute and relative time management
 */
export function useTimeRange() {
  const exploreStore = useExploreStore();

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
      const timezone = exploreStore.getTimezoneIdentifier();
      const currentRange = exploreStore.timeRange;
      if (currentRange) {
        const currentStart = calendarDateTimeToTimestamp(currentRange.start, timezone);
        const currentEnd = calendarDateTimeToTimestamp(currentRange.end, timezone);
        const newStart = range.start.getTime();
        const newEnd = range.end.getTime();

        const isSameRange = Math.abs(currentStart - newStart) < 1000 &&
                            Math.abs(currentEnd - newEnd) < 1000;

        if (isSameRange) {
          return false;
        }
      }

      const start = timestampToCalendarDateTime(range.start.getTime(), timezone);
      const end = timestampToCalendarDateTime(range.end.getTime(), timezone);

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
    formatSqlDateTime,
    applyRelativeTimeRange,
    quickRangeLabelToRelativeTime,
    handleHistogramTimeRangeZoom,
    setDefaultTimeRange,
    getHumanReadableTimeRange
  };
}
