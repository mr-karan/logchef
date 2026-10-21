import { useExploreStore } from '@/stores/explore';
import { epochMs } from '@/utils/timeWindow';

/**
 * Composable for zooming the Explorer's time window from the histogram
 */
export function useTimeRange() {
  const exploreStore = useExploreStore();

  // Handle histogram time range zooming
  const handleHistogramTimeRangeZoom = (range: { start: Date; end: Date }) => {
    try {
      const current = exploreStore.resolved;
      const start = epochMs(range.start);
      const end = epochMs(range.end);

      const isSameRange = Math.abs(current.start - start) < 1000 && Math.abs(current.end - end) < 1000;
      if (isSameRange) {
        return false;
      }

      exploreStore.setWindow({ kind: 'absolute', start, end });
      return true;
    } catch (e) {
      console.error('Error handling histogram time range:', e);
      return false;
    }
  };

  return {
    handleHistogramTimeRangeZoom
  };
}
