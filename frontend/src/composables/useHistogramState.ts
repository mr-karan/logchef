import { ref, computed, watch, type Ref, type ComputedRef } from 'vue';
import { useExploreStore } from '@/stores/explore';
import { useTeamsStore } from '@/stores/teams';
import { exploreApi } from '@/api/explore';
import { HistogramService, type HistogramData } from '@/services/HistogramService';

interface UseHistogramStateOptions {
  autoFetchOnQuerySuccess?: boolean;
}

interface UseHistogramStateReturn {
  data: Ref<HistogramData[]>;
  isLoading: Ref<boolean>;
  error: Ref<string | null>;
  granularity: Ref<string | null>;
  groupByField: Ref<string | null>;
  isEligible: ComputedRef<boolean>;
  fetch: (customGranularity?: string) => Promise<void>;
  clear: () => void;
  setGroupByField: (field: string | null) => void;
}

export function useHistogramState(options: UseHistogramStateOptions = {}): UseHistogramStateReturn {
  const exploreStore = useExploreStore();
  const teamsStore = useTeamsStore();

  const data = ref<HistogramData[]>([]);
  const isLoading = ref(false);
  const error = ref<string | null>(null);
  const granularity = ref<string | null>(null);
  const groupByField = ref<string | null>(null);

  const isEligible = computed(() => exploreStore.activeMode === 'logchefql');

  function clear() {
    data.value = [];
    error.value = null;
    granularity.value = null;
    isLoading.value = false;
  }

  function setGroupByField(field: string | null) {
    groupByField.value = field;
  }

  function formatTimeRange(): { start: string; end: string } | null {
    const timeRange = exploreStore.timeRange;
    if (!timeRange) return null;

    const formatDateTime = (dt: any) => {
      if (!dt) return '';
      const year = dt.year;
      const month = String(dt.month).padStart(2, '0');
      const day = String(dt.day).padStart(2, '0');
      const hour = String(dt.hour || 0).padStart(2, '0');
      const minute = String(dt.minute || 0).padStart(2, '0');
      const second = String(dt.second || 0).padStart(2, '0');
      return `${year}-${month}-${day}T${hour}:${minute}:${second}`;
    };

    return {
      start: formatDateTime(timeRange.start),
      end: formatDateTime(timeRange.end),
    };
  }

  async function fetch(customGranularity?: string): Promise<void> {
    if (!isEligible.value) {
      error.value = 'Histogram only available for LogchefQL queries';
      return;
    }

    const sql = exploreStore.generatedDisplaySql;
    if (!sql) {
      error.value = 'Run a LogchefQL query first';
      return;
    }

    const teamId = teamsStore.currentTeamId;
    if (!teamId) {
      error.value = 'No team selected';
      return;
    }

    const timeRange = formatTimeRange();
    if (!timeRange) {
      error.value = 'No time range set';
      return;
    }

    isLoading.value = true;
    error.value = null;

    try {
      const calculatedGranularity = customGranularity || 
        HistogramService.calculateOptimalGranularity(timeRange.start, timeRange.end);

      const groupBy = groupByField.value === '__none__' || !groupByField.value 
        ? undefined 
        : groupByField.value;

      const response = await exploreApi.getHistogramData(
        exploreStore.sourceId,
        {
          raw_sql: sql,
          limit: 100,
          window: calculatedGranularity,
          timezone: exploreStore.selectedTimezoneIdentifier || undefined,
          group_by: groupBy,
          query_timeout: exploreStore.queryTimeout,
        },
        teamId
      );

      if (response.data) {
        data.value = response.data.data || [];
        granularity.value = response.data.granularity || calculatedGranularity;
        error.value = null;
      } else {
        data.value = [];
        granularity.value = null;
        error.value = 'Failed to fetch histogram data';
      }
    } catch (err) {
      const errorMessage = err instanceof Error ? err.message : String(err);
      console.error('Histogram fetch error:', errorMessage);
      clear();
      error.value = errorMessage;
    } finally {
      isLoading.value = false;
    }
  }

  if (options.autoFetchOnQuerySuccess) {
    watch(
      () => exploreStore.lastExecutionTimestamp,
      (newTimestamp) => {
        if (newTimestamp && isEligible.value) {
          fetch();
        }
      }
    );
  }

  return {
    data,
    isLoading,
    error,
    granularity,
    groupByField,
    isEligible,
    fetch,
    clear,
    setGroupByField,
  };
}
