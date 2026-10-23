import { defineStore } from "pinia";
import { computed, markRaw, shallowReactive, shallowRef } from "vue";
import { exploreApi, type ColumnInfo, type QueryStats, type QueryWarning } from "@/api/explore";
import { logchefqlApi, type FilterCondition, type TemplateVariable } from "@/api/logchefql";
import { asClickHouseConnection, getVictoriaLogsOptimizer, type Source } from "@/api/sources";
import { getErrorMessage } from "@/api/types";
import { WindowedHTTPError, type LogRow } from "@/api/windowed";
import { useVariables } from "@/composables/useVariables";
import { getNativeQueryLanguageForSource } from "@/lib/queryMetadata";
import { generateDefaultSql } from "@/services/SqlManager";
import { prepareLogchefQLTemplate } from "@/utils/logchefql/template";
import { toRfc3339, type ResolvedWindow, type TimeWindow } from "@/utils/timeWindow";
import { useExploreStore, type RunQuery } from "./explore";
import { useExploreHistogramStore } from "./exploreHistogram";
import { createWindowedSession, type WindowedSession, type Paging } from "./exploreWindowed";
import { useSourcesStore } from "./sources";
import { useTeamsStore } from "./teams";

export type RunStatus = "validating" | "running" | "succeeded" | "stopped" | "failed";

export interface Results {
  rows: LogRow[];
  columns: ColumnInfo[];
  stats: QueryStats;
  warnings: QueryWarning[];
  // The SQL that ran: the generated SQL of a LogchefQL query, the text of a native one.
  sql: string;
  compilation: { conditions: FilterCondition[]; fieldsUsed: string[] } | null;
}

/** One run. Everything but its status, error and results is fixed when it starts. */
export interface Execution {
  readonly id: number;
  readonly teamId: number;
  readonly source: Source;
  // What the run was asked for. A replay asks again, so an empty native query regenerates its default SQL.
  readonly asked: RunQuery;
  // What the run sent.
  readonly query: RunQuery;
  readonly window: TimeWindow;
  readonly resolved: ResolvedWindow;
  readonly limit: number;
  readonly timeout: number;
  readonly variables: TemplateVariable[];
  readonly streamFilters: string[];
  readonly controller: AbortController;
  // Called once, when the run first shows results.
  readonly onShown?: (execution: Execution) => void;
  status: RunStatus;
  error: string | null;
  results: Results | null;
  search: WindowedSession | null;
}

// The location Explorer shows. enter() replaces it whole, so no run or result crosses locations.
interface Here {
  readonly opened: RunQuery | null;
  readonly current: Execution | null;
  readonly shown: Execution | null;
}

const DEFAULT_QUERY_STATS: QueryStats = { execution_time_ms: 0, rows_read: 0, bytes_read: 0 };

function inferColumnType(value: unknown): string {
  if (typeof value === "number") return Number.isInteger(value) ? "Int64" : "Float64";
  if (typeof value === "boolean") return "Bool";
  if (typeof value === "string") {
    const trimmed = value.trim();
    return trimmed !== "" && !Number.isNaN(Date.parse(trimmed)) ? "DateTime64" : "String";
  }
  if (Array.isArray(value)) return "Array";
  return value === null || value === undefined ? "String" : "JSON";
}

// A windowed search returns no columns, so they are inferred from the first rows.
function normalizeColumns(columns: ColumnInfo[] | null | undefined, rows: LogRow[]): ColumnInfo[] {
  if (columns?.length) return columns;
  if (rows.length === 0) return [];
  const types = new Map<string, string>();
  for (const row of rows.slice(0, 25)) {
    for (const [key, value] of Object.entries(row)) {
      if (!types.has(key) && value !== null && value !== undefined) types.set(key, inferColumnType(value));
    }
  }
  return Object.keys(rows[0]).map((name) => ({ name, type: types.get(name) || "String" }));
}

function results(rows: LogRow[], columns: ColumnInfo[] | null | undefined, stats: QueryStats | null | undefined, rest: Partial<Results>): Results {
  return { rows: markRaw(rows), columns: normalizeColumns(columns, rows), stats: stats ?? DEFAULT_QUERY_STATS, warnings: [], sql: "", compilation: null, ...rest };
}

// An empty native query on a ClickHouse source runs the source's default SQL over the run's own window.
function withDefaultSql(query: RunQuery, source: Source, window: ResolvedWindow, limit: number): RunQuery {
  if (query.mode !== "native" || query.text.trim() || getNativeQueryLanguageForSource(source) !== "clickhouse-sql") return query;
  const connection = asClickHouseConnection(source.connection);
  const tableName = connection?.database && connection.table_name ? `${connection.database}.${connection.table_name}` : "default.logs";
  return { mode: "native", text: generateDefaultSql({ tableName, tsField: source._meta_ts_field || "timestamp", window, limit }) };
}

function histogramEligible(source: Source | null, mode: RunQuery["mode"]): boolean {
  if (getVictoriaLogsOptimizer(source)?.histogram_enabled === false) return false;
  return mode === "logchefql" || getNativeQueryLanguageForSource(source) === "logsql";
}

/**
 * The one owner of Explorer runs: what ran, over which window, what is on
 * screen and what is running. Only the Explorer navigator starts or stops runs.
 */
export const useExploreRuns = defineStore("exploreRuns", () => {
  const explore = useExploreStore();
  const sources = useSourcesStore();
  const histogram = useExploreHistogramStore();

  const here = shallowRef<Here>({ opened: null, current: null, shown: null });
  let lastId = 0;

  const current = computed(() => here.value.current);
  const shown = computed(() => here.value.shown);
  const isRunning = computed(() => current.value?.status === "validating" || current.value?.status === "running");
  // A windowed search shows its rows while it pages, so its overlay ends with the first page.
  const showsLoading = computed(() => isRunning.value && current.value !== shown.value);
  const isHistogramEligible = computed(() => histogramEligible(sources.currentSourceDetails, shown.value?.query.mode ?? explore.activeMode));

  /** Shows a new location that opened with `opened`: stops its run and drops its results. */
  function enter(opened: RunQuery | null) {
    stop();
    here.value.current?.search?.stop();
    here.value.shown?.search?.stop();
    histogram.clearHistogramData();
    histogram.setGroupByField(null);
    here.value = { opened, current: null, shown: null };
  }

  /** Runs `asked` over the editor's window, limit and variables. Null when the source cannot run. */
  function run(asked: RunQuery, { validate = false, onShown }: { validate?: boolean; onShown?: Execution["onShown"] } = {}): Execution | null {
    const teamId = useTeamsStore().currentTeamId;
    const source = sources.currentSourceDetails;
    if (!teamId || !source?.is_connected || source.id !== explore.sourceId) return null;
    halt(here.value.current);
    explore.clearError();
    const resolved = explore.resolveNow();
    const execution = shallowReactive<Execution>({
      id: ++lastId,
      teamId,
      source,
      asked,
      query: withDefaultSql(asked, source, resolved, explore.limit),
      window: explore.window,
      resolved,
      limit: explore.limit,
      timeout: explore.queryTimeout,
      variables: useVariables().getVariablesForApi(),
      streamFilters: explore.streamFilters,
      controller: new AbortController(),
      onShown,
      status: validate && asked.mode === "logchefql" && asked.text.trim() ? "validating" : "running",
      error: null,
      results: null,
      search: null,
    });
    here.value = { ...here.value, current: execution };
    void execute(execution);
    return execution;
  }

  /** Re-runs what last ran here over the editor's window and limit, or the opened query when nothing ran. */
  function replay(): Execution | null {
    const { opened, current, shown } = here.value;
    const query = current?.status === "running" ? current.asked : shown?.asked ?? opened;
    return query && run(query);
  }

  /** Stops the running query or a Run still validating, and windowed paging and counting. */
  function stop() {
    if (isRunning.value) halt(here.value.current);
    else (shown.value ?? current.value)?.search?.stop();
  }

  function halt(execution: Execution | null) {
    if (execution?.status !== "validating" && execution?.status !== "running") return;
    // Set first, so the run's own late completion changes nothing.
    execution.status = "stopped";
    execution.controller.abort();
    execution.search?.stop();
  }

  async function execute(execution: Execution) {
    try {
      if (execution.status === "validating") {
        const invalid = await validationError(execution);
        if (execution.status !== "validating") return;
        if (invalid) return finish(execution, "failed", invalid);
        execution.status = "running";
      }
      const optimizer = getVictoriaLogsOptimizer(execution.source);
      if (optimizer) {
        try {
          return await searchWindows(execution, optimizer.histogram_enabled !== false);
        } catch (err) {
          if (!(err instanceof WindowedHTTPError && err.status === 422)) throw err;
        }
      }
      execution.search = null;
      const found = await queryOnce(execution);
      if (execution.status !== "running") return;
      publish(execution, found);
      finish(execution, "succeeded");
    } catch (err) {
      const message = getErrorMessage(err);
      finish(execution, "failed", execution.query.mode === "logchefql" ? `LogchefQL query error: ${message}` : message);
    }
  }

  function finish(execution: Execution, status: "succeeded" | "failed", error: string | null = null) {
    if (execution.status !== "validating" && execution.status !== "running") return;
    execution.status = status;
    execution.error = error;
    if (status === "failed" && !execution.results) execution.search?.stop();
  }

  async function validationError({ teamId, source, query }: Execution): Promise<string | null> {
    try {
      const response = await logchefqlApi.validate(teamId, source.id, prepareLogchefQLTemplate(query.text));
      const valid = response.status !== "success" || response.data?.valid !== false;
      return valid ? null : response.data?.error?.message || "Invalid LogchefQL syntax";
    } catch (err) {
      // The query request reports validation failures when this check is unavailable.
      console.warn("LogchefQL validation API error:", err);
      return null;
    }
  }

  // Each page shows as it arrives. An error after rows shows in the search panel.
  async function searchWindows(execution: Execution, withHistogram: boolean) {
    const { teamId, source, query, resolved, limit, timeout, variables, streamFilters } = execution;
    const search = execution.search = createWindowedSession({
      teamId,
      sourceId: source.id,
      histogram: withHistogram,
      request: {
        query_text: query.text,
        query_language: query.mode === "logchefql" ? "logchefql" : "logsql",
        start_time: toRfc3339(resolved.start),
        end_time: toRfc3339(resolved.end),
        timezone: resolved.zone,
        limit,
        query_timeout: timeout,
        variables,
        extra_stream_filters: streamFilters,
      },
      onRows: (rows, stats) => publish(execution, results(rows, null, stats, { sql: query.mode === "native" ? query.text : "" })),
    });
    if (await search.start()) finish(execution, search.error ? "failed" : "succeeded", execution.results ? null : search.error);
  }

  /** Pages the windowed search on screen. Its run is current and running until the page ends. */
  async function page(action: Paging) {
    const execution = action === "skip" && current.value?.status === "running" && current.value.search?.skippableWindow
      ? current.value : shown.value?.search ? shown.value : current.value;
    const search = execution?.search?.page(action);
    if (!execution || !search) return;
    if (current.value !== execution) halt(current.value);
    here.value = { ...here.value, current: execution };
    execution.status = "running";
    execution.error = null;
    try {
      if (await search) finish(execution, execution.search?.error ? "failed" : "succeeded", execution.results ? null : execution.search?.error);
    } catch (err) {
      finish(execution, "failed", getErrorMessage(err));
    }
  }

  async function queryOnce({ teamId, source, query, resolved, limit, timeout, variables, streamFilters, controller }: Execution): Promise<Results> {
    const optimizer = getVictoriaLogsOptimizer(source);
    const common = {
      extra_stream_filters: optimizer ? streamFilters : undefined,
      start_time: toRfc3339(resolved.start),
      end_time: toRfc3339(resolved.end),
      timezone: resolved.zone,
      query_timeout: timeout,
      variables: variables.length > 0 ? variables : undefined,
    };
    if (query.mode === "logchefql") {
      const response = await logchefqlApi.query(teamId, source.id, { ...common, query: query.text, limit }, { signal: controller.signal, timeout });
      if (response.status !== "success") throw response;
      const data = response.data;
      if (!data) throw new Error("Query execution failed");
      return results(data.logs ?? [], data.columns, data.stats, {
        sql: data.generated_query || data.generated_sql || "",
        compilation: { conditions: data.conditions ?? [], fieldsUsed: data.fields_used ?? [] },
      });
    }
    const response = await exploreApi.getLogs(source.id, { ...common, query_text: query.text, limit: optimizer ? limit : undefined }, teamId, controller.signal);
    if (response.status !== "success") throw response;
    const data = response.data;
    if (data && "error" in data) throw new Error(data.details || data.error);
    return results(data?.data ?? data?.logs ?? [], data?.columns, data?.stats, { warnings: data?.warnings ?? [], sql: query.text });
  }

  function publish(execution: Execution, found: Results) {
    if (here.value.current !== execution) return;
    const first = execution.results === null;
    execution.results = found;
    if (!first) return;
    here.value.shown?.search?.stop();
    here.value = { ...here.value, shown: execution };
    histogram.clearHistogramData();
    if (execution.search) histogram.setGroupByField(null);
    else fetchHistogram(execution);
    // Variables have no setter that saves them, so a run that shows results saves the draft.
    explore.persistDraft();
    execution.onShown?.(execution);
  }

  function fetchHistogram(execution: Execution) {
    if (!histogramEligible(execution.source, execution.query.mode)) return histogram.clearHistogramData();
    void histogram.fetchHistogramData({
      teamId: execution.teamId,
      sourceId: execution.source.id,
      variables: execution.variables,
      extraStreamFilters: getVictoriaLogsOptimizer(execution.source) ? execution.streamFilters : undefined,
      queryText: execution.query.mode === "logchefql" ? execution.results?.sql ?? "" : execution.query.text.trim() || "*",
      window: execution.resolved,
      queryTimeout: execution.timeout,
    });
  }

  function setGroupByField(field: string | null) {
    histogram.setGroupByField(field);
    if (shown.value && !shown.value.search) fetchHistogram(shown.value);
  }

  return {
    current,
    shown,
    results: computed(() => shown.value?.results ?? null),
    error: computed(() => current.value?.error ?? null),
    isRunning,
    showsLoading,
    isHistogramEligible,
    windowed: computed(() => shown.value?.search ?? current.value?.search),
    enter,
    run,
    replay,
    stop,
    page,
    setGroupByField,
  };
});
