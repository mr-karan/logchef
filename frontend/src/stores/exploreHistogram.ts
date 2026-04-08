import { defineStore } from "pinia";
import { computed, ref } from "vue";
import { exploreApi } from "@/api/explore";
import { useTeamsStore } from "@/stores/teams";
import { useContextStore } from "@/stores/context";
import { HistogramService, type HistogramData } from '@/services/HistogramService';
import { useVariables } from "@/composables/useVariables";

interface HistogramState {
  data: HistogramData[];
  isLoading: boolean;
  error: string | null;
  granularity: string | null;
  groupByField: string | null;
}

export const useExploreHistogramStore = defineStore("exploreHistogram", () => {
  const contextStore = useContextStore();

  const state = ref<HistogramState>({
    data: [],
    isLoading: false,
    error: null,
    granularity: null,
    groupByField: null,
  });

  const histogramData = computed(() => state.value.data);
  const isLoadingHistogram = computed(() => state.value.isLoading);
  const histogramError = computed(() => state.value.error);
  const histogramGranularity = computed(() => state.value.granularity);
  const groupByField = computed(() => state.value.groupByField);

  function clearHistogramData() {
    state.value.data = [];
    state.value.error = null;
    state.value.granularity = null;
    state.value.isLoading = false;
  }

  function setGroupByField(field: string | null) {
    state.value.groupByField = field;
  }

  async function fetchHistogramData(options: {
    queryText: string;
    timeRange: { start: any; end: any } | null;
    timezone?: string;
    queryTimeout?: number;
    granularity?: string;
  }) {
    const { queryText, timeRange, timezone, queryTimeout, granularity } = options;

    if (!queryText) {
      clearHistogramData();
      state.value.error = "Run a query first to see the histogram";
      return { success: false, error: { message: "Run a query first" } };
    }

    state.value.isLoading = true;
    state.value.error = null;

    try {
      const currentTeamId = useTeamsStore().currentTeamId;
      if (!currentTeamId) {
        state.value.error = "No team selected";
        state.value.isLoading = false;
        return { success: false, error: { message: "No team selected" } };
      }

      const sourceId = contextStore.sourceId;
      if (!sourceId) {
        state.value.error = "No source selected";
        state.value.isLoading = false;
        return { success: false, error: { message: "No source selected" } };
      }

      let windowGranularity = granularity;
      let startISO: string | undefined;
      let endISO: string | undefined;
      if (!windowGranularity && timeRange) {
        startISO = new Date(
          timeRange.start.year, timeRange.start.month - 1, timeRange.start.day,
          'hour' in timeRange.start ? timeRange.start.hour : 0,
          'minute' in timeRange.start ? timeRange.start.minute : 0,
          'second' in timeRange.start ? timeRange.start.second : 0
        ).toISOString();
        endISO = new Date(
          timeRange.end.year, timeRange.end.month - 1, timeRange.end.day,
          'hour' in timeRange.end ? timeRange.end.hour : 0,
          'minute' in timeRange.end ? timeRange.end.minute : 0,
          'second' in timeRange.end ? timeRange.end.second : 0
        ).toISOString();
        windowGranularity = HistogramService.calculateOptimalGranularity(startISO, endISO);
      } else if (timeRange) {
        startISO = new Date(
          timeRange.start.year, timeRange.start.month - 1, timeRange.start.day,
          'hour' in timeRange.start ? timeRange.start.hour : 0,
          'minute' in timeRange.start ? timeRange.start.minute : 0,
          'second' in timeRange.start ? timeRange.start.second : 0
        ).toISOString();
        endISO = new Date(
          timeRange.end.year, timeRange.end.month - 1, timeRange.end.day,
          'hour' in timeRange.end ? timeRange.end.hour : 0,
          'minute' in timeRange.end ? timeRange.end.minute : 0,
          'second' in timeRange.end ? timeRange.end.second : 0
        ).toISOString();
      }

      // Get variables for backend substitution
      const { getVariablesForApi } = useVariables();
      const variables = getVariablesForApi();

      const params = {
        raw_sql: queryText,
        limit: 100,
        window: windowGranularity || '1m',
        timezone: timezone || undefined,
        start_time: startISO,
        end_time: endISO,
        group_by: state.value.groupByField === "__none__" || state.value.groupByField === null
          ? undefined
          : state.value.groupByField,
        query_timeout: queryTimeout,
        variables: variables.length > 0 ? variables : undefined,
      };

      const response = await exploreApi.getHistogramData(sourceId, params, currentTeamId);

      if (response.data) {
        state.value.data = response.data.data || [];
        state.value.granularity = response.data.granularity || null;
        state.value.error = null;
        return { success: true, data: response.data };
      } else {
        state.value.data = [];
        state.value.granularity = null;
        state.value.error = "Failed to fetch histogram data";
        return { success: false, error: { message: "Failed to fetch histogram data" } };
      }
    } catch (error) {
      const errorMessage = error instanceof Error ? error.message : String(error);
      clearHistogramData();
      state.value.error = errorMessage;
      return { success: false, error: { message: errorMessage } };
    } finally {
      state.value.isLoading = false;
    }
  }

  return {
    histogramData,
    isLoadingHistogram,
    histogramError,
    histogramGranularity,
    groupByField,

    clearHistogramData,
    setGroupByField,
    fetchHistogramData,
  };
});
