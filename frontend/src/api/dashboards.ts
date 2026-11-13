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
export const DASHBOARD_CLIENT_FEATURES: readonly string[] = [];

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
  /** Histogram window. `count` mode only; `auto` is the only value in this release. */
  interval?: "auto";
  /** Row cap for `result` mode. */
  limit?: number;
  /** `count` mode with LogchefQL only: passed as the translate request's limit. */
  translate_limit?: number;
}

export type DashboardViz =
  | { type: "stat"; reduce?: "sum" }
  | { type: "chart"; mark?: DashboardChartMark; stack?: DashboardChartStack }
  | { type: "pie"; donut?: boolean }
  | { type: "toplist" }
  | { type: "table"; visible_columns?: string[] };

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
   * Relative range the dashboard opens with ("15m", "6h", "7d", "today").
   * Absent = keep the viewer's current range.
   */
  default_time_range?: string;
  /** At least one tab. Each owns its layout; this release shows one. */
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
