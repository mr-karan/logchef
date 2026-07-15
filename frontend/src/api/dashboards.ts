import { apiClient } from "./apiUtils";
import type { HistogramResponse, QuerySuccessResponse } from "./explore";
import type { QueryResponse as LogchefqlQueryResponse, TranslateResponse } from "./logchefql";

// Panel query languages accepted by the panel blob. Mirrors pkg/models QueryLanguage.
export type PanelQueryLanguage = "logchefql" | "clickhouse-sql" | "logsql";

// The three chart kinds a panel can render (mirrors pkg/models/dashboards.go).
export type DashboardPanelType = "timeseries" | "stat" | "table";

export interface DashboardPanelOptions {
  /** Field to group a timeseries by (produces stacked series). */
  group_by?: string;
  /** Row cap for table panels. */
  limit?: number;
  /** Subset of columns to display for table panels (empty/absent = all). */
  columns?: string[];
  /**
   * Timeseries render style. Absent = "line" (Grafana-like default) — this
   * also applies to legacy panels saved before this option existed, since the
   * runtime has no way to distinguish "legacy" from "new" at render time.
   * Explicit values are always honored.
   */
  chart?: "bars" | "line" | "area";
}

export interface DashboardPanel {
  id: string;
  title: string;
  type: DashboardPanelType;
  team_id: number;
  source_id: number;
  query: string;
  query_language: PanelQueryLanguage;
  options?: DashboardPanelOptions;
  /**
   * Response-only redaction flag (never sent on create/update). Set true by the
   * server when the requesting user lacks access to this panel's source: the
   * query text / language / options are blanked and only the placeholder
   * metadata (team_id, source_id, type, title) plus layout survive. The panel
   * renders a "locked" state and must NOT be executed (there is no query to run).
   */
  locked?: boolean;
}

export interface DashboardLayoutItem {
  id: string;
  x: number;
  y: number;
  w: number;
  h: number;
}

/** The versioned blob stored in dashboards.panels_json. */
export interface DashboardPanels {
  version: number;
  layout: DashboardLayoutItem[];
  panels: DashboardPanel[];
}

export interface Dashboard {
  id: number;
  name: string;
  description: string;
  panels: DashboardPanels;
  created_by?: number | null;
  created_by_name?: string;
  created_by_email?: string;
  created_at: string;
  updated_at: string;
  can_edit?: boolean;
}

export interface CreateDashboardRequest {
  name: string;
  description: string;
  panels: DashboardPanels;
}

export interface UpdateDashboardRequest {
  name: string;
  description: string;
  panels: DashboardPanels;
}

/** CRUD over the dashboards resource. */
export const dashboardsApi = {
  list: () => apiClient.get<Dashboard[]>("/dashboards"),
  get: (id: number) => apiClient.get<Dashboard>(`/dashboards/${id}`),
  create: (req: CreateDashboardRequest) => apiClient.post<Dashboard>("/dashboards", req),
  update: (id: number, req: UpdateDashboardRequest) => apiClient.put<Dashboard>(`/dashboards/${id}`, req),
  remove: (id: number) => apiClient.delete<{ message: string }>(`/dashboards/${id}`),
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
}

export interface SqlQueryRequestBody {
  query_text: string;
  limit?: number;
  start_time?: string;
  end_time?: string;
  timezone?: string;
  // See HistogramRequestBody.query_language.
  query_language?: PanelQueryLanguage;
}

export interface LogchefqlQueryRequestBody {
  query: string;
  // "YYYY-MM-DD HH:MM:SS" — the server bakes this straight into ClickHouse's
  // toDateTime(str, timezone) and does NOT accept RFC3339 here (unlike
  // HistogramRequestBody/SqlQueryRequestBody). Callers must send a UTC
  // wall-clock string paired with timezone: "UTC" — pairing it with the
  // viewer's real IANA zone shifts the query window (see PanelState store).
  start_time: string;
  end_time: string;
  timezone?: string;
  limit?: number;
  // See HistogramRequestBody.query_language.
  query_language?: PanelQueryLanguage;
}

export interface TranslateRequestBody {
  query: string;
  // "YYYY-MM-DD HH:MM:SS" — same contract/caveat as LogchefqlQueryRequestBody
  // above; full_sql generation silently no-ops (rather than erroring) if this
  // isn't in the exact expected format.
  start_time?: string;
  end_time?: string;
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
