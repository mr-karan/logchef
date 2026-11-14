import { apiClient, type RequestOptions } from "./apiUtils";
import type { HistogramResponse, QuerySuccessResponse } from "./explore";
import type { QueryResponse as LogchefqlQueryResponse, TranslateResponse } from "./logchefql";

// Panel query languages accepted by the panel blob. Mirrors pkg/models QueryLanguage.
export type PanelQueryLanguage = "logchefql" | "clickhouse-sql" | "logsql";

// ---------------------------------------------------------------------------
// Panel blob schema 2 (RFC 0003). A panel is a query plus a visualization,
// tabs own the layouts, and the server writes the features list. Mirrors
// pkg/models/dashboards_v2.go, which stays the authority.
// ---------------------------------------------------------------------------

export const DASHBOARD_PANELS_VERSION = 2;

/**
 * The features this build's code understands. The server refuses a save to a
 * dashboard whose stored features are not all declared here, so a build can
 * never drop state it does not know. Each phase that adds blob state adds its
 * feature name here together with the code that keeps it. Never copy a
 * dashboard's stored `features` into this list.
 */
export const DASHBOARD_CLIENT_FEATURES: readonly string[] = ["result_frames", "formats", "result_charts", "stat_compare", "thresholds", "table_rules", "column_formats", "tabs", "free_grid", "panel_time"];

// A dashboard this client cannot round-trip: the server's stub for an older
// client, or a stored feature this build does not know.
export function dashboardNeedsReload(dashboard: Dashboard): boolean {
  if (dashboard.corrupt) return false;
  if (dashboard.panels_corrupt || dashboard.panels.version !== DASHBOARD_PANELS_VERSION) return true;
  return dashboard.panels.features.some((feature) => !DASHBOARD_CLIENT_FEATURES.includes(feature));
}

export const DASHBOARD_SCHEMA_HEADER = "X-Logchef-Dashboard-Schema";
export const DASHBOARD_FEATURES_HEADER = "X-Logchef-Dashboard-Features";

/** The client contract headers every dashboard request carries. */
export function dashboardClientHeaders(): Record<string, string> {
  return {
    [DASHBOARD_SCHEMA_HEADER]: String(DASHBOARD_PANELS_VERSION),
    [DASHBOARD_FEATURES_HEADER]: DASHBOARD_CLIENT_FEATURES.join(","),
  };
}

export type DashboardQueryMode = "count" | "result";
export type DashboardVizType = "stat" | "chart" | "pie" | "toplist" | "table";
export type DashboardChartMark = "line" | "area" | "bar";
export type DashboardChartStack = "none" | "stacked";

export interface DashboardPanelQuery {
  mode: DashboardQueryMode;
  language: PanelQueryLanguage;
  text: string;
  /** Field to group a count by. `count` mode only; omitted when blank. */
  group_by?: string;
  /**
   * Bucket size. In `count` mode it is the histogram window, and in `result`
   * mode it feeds `{{__interval}}`. Absent means `auto`.
   */
  interval?: "auto" | PanelInterval;
  /** Row cap for `result` mode. */
  limit?: number;
  /** `count` mode with LogchefQL only: passed as the translate request's limit. */
  translate_limit?: number;
  /** A rolling or calendar range ("7d", "today-6d") that replaces the dashboard range. */
  range?: string;
}

/** A histogram window, the fixed values of `query.interval`. */
export type PanelInterval =
  | "1s" | "5s" | "10s" | "15s" | "30s"
  | "1m" | "5m" | "10m" | "15m" | "30m"
  | "1h" | "2h" | "3h" | "6h" | "12h" | "24h";

export type ColumnKind = "time" | "date" | "number" | "category" | "bool" | "other";
export type StatReduce = "last" | "first" | "sum" | "avg" | "min" | "max";
export interface ConfirmedColumn { kind: ColumnKind; confidence: "confirmed" }
export interface NumberFormat {
 notation?: "auto" | "standard" | "compact";
 decimals?: number;
 grouping?: boolean;
 scale?: number;
 unit?: "none" | "percent" | "bytes" | "duration_ms" | "duration_s";
 prefix?: string;
 suffix?: string;
}
export interface StatCompare { mode: "none" | "previous_row"; good?: "up" | "down" | "neutral" }
export type PaletteColor = "green" | "amber" | "red" | "blue" | "purple" | "gray";
export interface ThresholdStep { value: number | string | null; color: PaletteColor }
export interface TableRule {
 columns: string[];
 op: ">" | ">=" | "<" | "<=" | "=" | "!=" | "contains" | "not_contains" | "empty" | "not_empty";
 value?: number | string;
 color: PaletteColor;
 scope: "cell" | "row";
}
export interface TimeFormat { kind: "time"; date?: "iso" | "medium" | "none"; time?: "none" | "minutes" | "seconds" | "millis" }
export type ColumnFormat = (NumberFormat & { kind?: "number" }) | TimeFormat;
export type DashboardViz =
  | { type: "stat"; thresholds?: ThresholdStep[]; color_mode?: "value" | "background"; compare?: StatCompare; sparkline?: boolean; value?: string; order_by?: string; reduce?: StatReduce; columns?: Record<string, ConfirmedColumn>; format?: NumberFormat }
  | { type: "chart"; thresholds?: ThresholdStep[]; x?: string; y?: string[]; series?: string; mark?: DashboardChartMark; stack?: DashboardChartStack; x_scale?: "auto" | "time" | "category"; orientation?: "horizontal" | "vertical"; show_values?: boolean; legend?: boolean; columns?: Record<string, ConfirmedColumn>; format?: NumberFormat }
  | { type: "pie"; label?: string; value?: string; donut?: boolean; min_slice_percent?: number; columns?: Record<string, ConfirmedColumn>; format?: NumberFormat }
  | { type: "toplist"; label?: string; value?: string; max_rows?: number; columns?: Record<string, ConfirmedColumn>; format?: NumberFormat }
  | { type: "table"; rules?: TableRule[]; column_formats?: Record<string, ColumnFormat>; visible_columns?: string[]; columns?: Record<string, ConfirmedColumn> };

export interface DashboardQueryPanel {
  id: string;
  kind: "query";
  title: string;
  team_id: number;
  source_id: number;
  query: DashboardPanelQuery;
  viz: DashboardViz;
  locked?: false;
}

/**
 * A panel the server redacted because the viewer lacks access to its source:
 * the query is gone and only the placeholder fields and the viz type survive.
 * It renders a locked state and must never be executed.
 */
export interface DashboardLockedPanel {
  id: string;
  kind: "query";
  title: string;
  team_id: number;
  source_id: number;
  viz: { type: DashboardVizType };
  locked: true;
}

export type DashboardPanel = DashboardQueryPanel | DashboardLockedPanel;

export interface DashboardLayoutItem {
  id: string;
  x: number;
  y: number;
  w: number;
  h: number;
}

export interface DashboardTab {
  id: string;
  title: string;
  layout: DashboardLayoutItem[];
}

/** The versioned blob stored in dashboards.panels_json. */
export interface DashboardPanels {
  version: number;
  /** Written by the server from what the blob uses; sent back unchanged. */
  features: string[];
  /**
   * Per-dashboard result-cache TTL in seconds. Absent = the client default
   * (600). 0 = caching OFF. >0 = that TTL. Snake_case matches the server's tag.
   */
  cache_ttl_seconds?: number;
  /**
   * Relative range the dashboard opens with ("15m", "6h", "7d", "today", "today-6d").
   * Absent = keep the viewer's current range.
   */
  default_time_range?: string;
  /** At least one tab, at most 10. Each owns its layout, and a panel sits in exactly one. */
  tabs: DashboardTab[];
  /** Panel definitions, independent of placement. */
  panels: DashboardPanel[];
}

interface DashboardBase {
  id: number;
  name: string;
  description: string;
  created_by?: number | null;
  created_by_name?: string;
  created_by_email?: string;
  created_at: string;
  updated_at: string;
  revision: number;
  can_edit: boolean;
  panels_corrupt?: boolean;
  min_schema?: number;
}

/**
 * A dashboard as GET, create and update return it. A corrupt row carries its
 * stored blob as an opaque string for repair and no panels.
 */
export type Dashboard =
  | (DashboardBase & { corrupt: false; panels: DashboardPanels })
  | (DashboardBase & { corrupt: true; raw_panels_json: string });

/** One row of the dashboard list: derived counts and kinds, never a blob. */
export interface DashboardSummary {
  id: number;
  name: string;
  description: string;
  panel_count: number;
  /** Distinct visualization kinds, sorted. */
  panel_types: DashboardVizType[];
  tab_count: number;
  created_by_name?: string;
  created_by_email?: string;
  revision: number;
  can_edit: boolean;
  corrupt: boolean;
  created_at: string;
  updated_at: string;
}

export interface CreateDashboardRequest {
  name: string;
  description: string;
  panels: DashboardPanels;
}

export interface UpdateDashboardRequest {
  revision: number;
  name: string;
  description: string;
  panels: DashboardPanels;
}

function withClientHeaders(options?: RequestOptions): RequestOptions {
  return { ...options, headers: { ...dashboardClientHeaders(), ...options?.headers } };
}

/** CRUD over the dashboards resource. Every request carries the client contract headers. */
export const dashboardsApi = {
  list: (options?: RequestOptions) => apiClient.get<DashboardSummary[]>("/dashboards", withClientHeaders(options)),
  get: (id: number) => apiClient.get<Dashboard>(`/dashboards/${id}`, withClientHeaders()),
  create: (req: CreateDashboardRequest) => apiClient.post<Dashboard>("/dashboards", req, withClientHeaders()),
  update: (id: number, req: UpdateDashboardRequest) =>
    apiClient.put<Dashboard>(`/dashboards/${id}`, req, withClientHeaders()),
  remove: (id: number) => apiClient.delete<{ message: string }>(`/dashboards/${id}`, withClientHeaders()),
};

// ---------------------------------------------------------------------------
// Panel data path.
//
// Panels execute from the frontend through the SAME team-scoped endpoints the
// explorer uses, so team/source authorization is enforced server-side. All of
// these pass `suppressErrorToast` so a viewer lacking access to several panels'
// sources gets per-panel inline locked/error states instead of a toast storm.
// ---------------------------------------------------------------------------

const PANEL_TIMEOUT_SECONDS = 30;

// Opts a panel request into the per-dashboard server-side result cache. Only
// dashboard panel requests send it (never explorer/ad-hoc queries). Omitted
// entirely when caching is off for the dashboard. Mirrors pkg/models
// CacheDirective; the server clamps ttl_seconds to its configured max_ttl.
export interface CacheDirective {
  scope: string;
  ttl_seconds: number;
}

export interface HistogramRequestBody {
  query_text: string;
  window?: string;
  group_by?: string;
  start_time?: string; // RFC3339
  end_time?: string; // RFC3339
  timezone?: string;
  limit?: number;
  // The panel's own query language (e.g. "logsql" for a VictoriaLogs source),
  // so the server can dispatch query_text natively instead of assuming
  // ClickHouse SQL. Optional/forward-compatible: older server builds ignore
  // unknown JSON fields.
  query_language?: PanelQueryLanguage;
  // Dashboard result-cache directive; omitted when caching is off.
  cache?: CacheDirective;
}

export interface SqlQueryRequestBody {
  query_text: string;
  limit?: number;
  start_time?: string;
  end_time?: string;
  timezone?: string;
  /** The window that `{{__interval}}` renders. */
  interval?: PanelInterval;
  // See HistogramRequestBody.query_language.
  query_language?: PanelQueryLanguage;
  // Dashboard result-cache directive; omitted when caching is off.
  cache?: CacheDirective;
}

export interface LogchefqlQueryRequestBody {
  query: string;
  start_time: string; // RFC3339
  end_time: string; // RFC3339
  timezone?: string;
  limit?: number;
  // See HistogramRequestBody.query_language.
  query_language?: PanelQueryLanguage;
  // Dashboard result-cache directive; omitted when caching is off.
  cache?: CacheDirective;
}

export interface TranslateRequestBody {
  query: string;
  start_time?: string; // RFC3339
  end_time?: string; // RFC3339
  timezone?: string;
  limit?: number;
  // See HistogramRequestBody.query_language.
  query_language?: PanelQueryLanguage;
}

export const dashboardPanelApi = {
  histogram: (teamId: number, sourceId: number, body: HistogramRequestBody, signal?: AbortSignal) =>
    apiClient.post<HistogramResponse>(
      `/teams/${teamId}/sources/${sourceId}/logs/histogram`,
      body,
      { signal, timeout: PANEL_TIMEOUT_SECONDS, suppressErrorToast: true }
    ),

  logsQuery: (teamId: number, sourceId: number, body: SqlQueryRequestBody, signal?: AbortSignal) =>
    apiClient.post<QuerySuccessResponse>(
      `/teams/${teamId}/sources/${sourceId}/logs/query`,
      body,
      { signal, timeout: PANEL_TIMEOUT_SECONDS, suppressErrorToast: true }
    ),

  logchefqlQuery: (teamId: number, sourceId: number, body: LogchefqlQueryRequestBody, signal?: AbortSignal) =>
    apiClient.post<LogchefqlQueryResponse>(
      `/teams/${teamId}/sources/${sourceId}/logchefql/query`,
      body,
      { signal, timeout: PANEL_TIMEOUT_SECONDS, suppressErrorToast: true }
    ),

  translate: (teamId: number, sourceId: number, body: TranslateRequestBody, signal?: AbortSignal) =>
    apiClient.post<TranslateResponse>(
      `/teams/${teamId}/sources/${sourceId}/logchefql/translate`,
      body,
      { signal, timeout: PANEL_TIMEOUT_SECONDS, suppressErrorToast: true }
    ),
};
