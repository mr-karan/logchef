import { defineStore } from "pinia";
import { computed, ref } from "vue";
import { exploreApi, type HistogramDataPoint } from "@/api/explore";
import type { TemplateVariable } from "@/api/logchefql";
import { calculateOptimalGranularity } from '@/services/HistogramService';
import { toRfc3339, type ResolvedWindow } from "@/utils/timeWindow";

interface HistogramState {
  data: HistogramDataPoint[];
  isLoading: boolean;
  error: string | null;
  notice: string | null;
  granularity: string | null;
  groupByField: string | null;
}

export const useExploreHistogramStore = defineStore("exploreHistogram", () => {
  const state = ref<HistogramState>({
    data: [],
    isLoading: false,
    error: null,
    notice: null,
    granularity: null,
    groupByField: null,
  });

  // Cancels a superseded histogram fetch so a slower earlier request can't
  // overwrite the results of a newer one (last-writer-wins race). See #101.
  let inFlightController: AbortController | null = null;

  const histogramData = computed(() => state.value.data);
  const isLoadingHistogram = computed(() => state.value.isLoading);
  const histogramError = computed(() => state.value.error);
  const histogramNotice = computed(() => state.value.notice);
  const histogramGranularity = computed(() => state.value.granularity);
  const groupByField = computed(() => state.value.groupByField);

  function clearHistogramData() {
    // Cancel any in-flight fetch so its late response can't repopulate the data
    // we're clearing (e.g. on source change). See #101.
    if (inFlightController) {
      inFlightController.abort();
      inFlightController = null;
    }
    state.value.data = [];
    state.value.error = null;
    state.value.notice = null;
    state.value.granularity = null;
    state.value.isLoading = false;
  }

  function setGroupByField(field: string | null) {
    state.value.groupByField = field;
  }

  async function fetchHistogramData(options: {
    teamId: number;
    sourceId: number;
    variables: TemplateVariable[];
    extraStreamFilters?: string[];
    queryText: string;
    window: ResolvedWindow;
    queryTimeout: number;
  }) {
    const { teamId, sourceId, variables, queryText, window } = options;

    if (!queryText) {
      clearHistogramData();
      state.value.error = "Run a query first to see the histogram";
      return;
    }

    // Supersede any earlier in-flight fetch so its response can't overwrite
    // this newer one (#101).
    inFlightController?.abort();
    const controller = new AbortController();
    inFlightController = controller;

    state.value.isLoading = true;
    state.value.error = null;
    state.value.notice = null;

    try {
      const startISO = toRfc3339(window.start);
      const endISO = toRfc3339(window.end);
      const response = await exploreApi.getHistogramData(sourceId, {
        extra_stream_filters: options.extraStreamFilters,
        query_text: queryText,
        limit: 100,
        window: calculateOptimalGranularity(startISO, endISO),
        // Day buckets align to the zone the labels show.
        timezone: window.zone,
        start_time: startISO,
        end_time: endISO,
        group_by: state.value.groupByField === "__none__" || state.value.groupByField === null
          ? undefined
          : state.value.groupByField,
        query_timeout: options.queryTimeout,
        variables: variables.length > 0 ? variables : undefined,
      }, teamId, controller.signal);

      // A newer fetch started (or this one was cancelled) while awaiting: drop
      // the result so it can't clobber fresher data (#101).
      if (inFlightController !== controller) return;

      state.value.data = response.data?.data || [];
      state.value.granularity = response.data?.granularity || null;
      state.value.notice = response.data?.notice || null;
      state.value.error = response.data ? null : "Failed to fetch histogram data";
    } catch (error) {
      // A cancelled/superseded fetch must stay silent, leaving the newer fetch's
      // state (or the cleared state) untouched.
      if (inFlightController !== controller) return;
      clearHistogramData();
      state.value.error = error instanceof Error ? error.message : String(error);
    } finally {
      // Only clear loading/controller if this run is still the active one; a
      // superseded run must not flip the newer run's loading flag.
      if (inFlightController === controller) {
        inFlightController = null;
        state.value.isLoading = false;
      }
    }
  }

  return {
    histogramData,
    isLoadingHistogram,
    histogramError,
    histogramNotice,
    histogramGranularity,
    groupByField,

    clearHistogramData,
    setGroupByField,
    fetchHistogramData,
  };
});
