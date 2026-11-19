import { api } from "./config";
import { apiClient, type RequestOptions } from "./apiUtils";

// ---------------------------------------------------------------------------
// Admin "Query Activity" (#58)
//
// Honest RECENT-activity view over the capped query_history table. The backend
// aggregates over a bounded recent window, so these figures are deliberately
// framed as recent activity — NOT authoritative all-time analytics.
//
// Response shape mirrors GET /api/v1/admin/query-activity exactly (snake_case).
// ---------------------------------------------------------------------------

export interface QueryActivityRecent {
  id: number;
  user_id: number;
  user_email: string;
  team_id: number;
  source_id: number;
  source_name: string;
  query_text: string;
  query_language: string;
  duration_ms: number;
  row_count: number;
  created_at: string;
}

export interface QueryActivityByLanguage {
  language: string;
  count: number;
}

export interface QueryActivityBySource {
  source_id: number;
  source_name: string;
  count: number;
}

export interface QueryActivityResponse {
  total: number;
  window_size: number;
  window_limit: number;
  recent: QueryActivityRecent[];
  by_language: QueryActivityByLanguage[];
  by_source: QueryActivityBySource[];
  slowest: QueryActivityRecent[];
}

// ---------------------------------------------------------------------------
// Admin "Query Stats" (#127)
//
// AUTHORITATIVE all-time usage analytics backed by a non-pruned daily rollup
// (query_stats_daily). Unlike the recent-activity view above (capped at 200
// rows/user), these aggregates are complete for the requested window.
//
// Response shape mirrors GET /api/v1/admin/query-stats exactly (snake_case).
// ---------------------------------------------------------------------------

export interface TopSourceStat {
  source_id: number;
  // Empty string when the source row has been deleted (LEFT JOIN).
  source_name: string;
  query_count: number;
  avg_duration_ms: number;
}

export interface TopUserStat {
  user_id: number;
  user_email: string;
  query_count: number;
}

export interface DailyQueryVolume {
  date: string;
  query_count: number;
}

export interface QueryStatsResponse {
  // Inclusive start of the window, "YYYY-MM-DD" (UTC).
  since: string;
  days: number;
  summary: {
    query_count: number;
    active_users: number;
    active_sources: number;
    avg_duration_ms: number;
  };
  top_sources: TopSourceStat[];
  top_users: TopUserStat[];
  // Ascending by date.
  volume_by_day: DailyQueryVolume[];
}

export interface QueryActivityFilters {
  q?: string;
  user_id?: number;
  source_id?: number;
  language?: string;
}

export const adminApi = {
  // Fetch recent query activity across all users. `limit` controls the
  // recent-feed length; it is clamped server-side (default 100, max 500).
  getQueryActivity: (limit = 100, filters: QueryActivityFilters = {}, options?: RequestOptions) => {
    const search = new URLSearchParams({ limit: String(limit) });
    for (const [key, value] of Object.entries(filters)) {
      if (value !== undefined && value !== "") search.set(key, String(value));
    }
    return apiClient.get<QueryActivityResponse>(`/admin/query-activity?${search}`, options);
  },

  // Fetch authoritative all-time usage stats over the last `days` (default 30,
  // clamped 1..365 server-side).
  getQueryStats: (days?: number, options?: RequestOptions) => {
    const search = typeof days === "number" ? `?days=${days}` : "";
    return apiClient.get<QueryStatsResponse>(`/admin/query-stats${search}`, options);
  },
};

// ---------------------------------------------------------------------------
// Admin audit log (RFC 0009)
//
// Mirrors GET /api/v1/admin/audit-events and its NDJSON export. Actions, auth
// methods and target types are open strings: new event kinds ship with the
// server, and the viewer renders whatever it receives.
// ---------------------------------------------------------------------------

export type AuditOutcome = "success" | "failure";

export interface AuditChange {
  from?: unknown;
  to?: unknown;
}

export interface AuditEvent {
  id: string;
  occurred_at: string;
  action: string;
  outcome: AuditOutcome;
  reason: string;
  actor_type: string;
  actor_id: number | null;
  actor_label: string;
  auth_method: string;
  credential_id: string;
  client_id: string;
  target_type: string;
  target_id: string;
  target_label: string;
  team_id: number | null;
  source_id: number | null;
  ip: string;
  user_agent: string;
  http_method: string;
  route: string;
  status: number;
  changes: Record<string, AuditChange>;
  detail: Record<string, unknown>;
}

export interface AuditEventsPage {
  events: AuditEvent[];
  next_cursor: string;
}

/** Server-side filters, named as the API names its query parameters. */
export interface AuditEventFilters {
  from: string;
  to: string;
  action?: string;
  actor_id?: string;
  outcome?: AuditOutcome;
  target_type?: string;
  target_id?: string;
  order?: "asc" | "desc";
}

// An export reads up to 100,000 events, which outlasts the default timeout.
const AUDIT_EXPORT_TIMEOUT_MS = 5 * 60 * 1000;

function auditQuery(filters: AuditEventFilters, extra: Record<string, string> = {}): string {
  const params = new URLSearchParams();
  for (const [key, value] of Object.entries({ ...filters, ...extra })) {
    if (value) params.set(key, value);
  }
  return params.toString();
}

export const auditApi = {
  listEvents: (filters: AuditEventFilters, cursor: string, options?: RequestOptions) =>
    apiClient.get<AuditEventsPage>(`/admin/audit-events?${auditQuery(filters, { cursor })}`, options),

  // A blob response turns the error body into a Blob too, so the API error
  // is parsed back out of it here.
  exportEvents: async (filters: AuditEventFilters): Promise<Blob> => {
    try {
      const response = await api.get<Blob>(`/admin/audit-events/export?${auditQuery(filters)}`, {
        responseType: "blob",
        timeout: AUDIT_EXPORT_TIMEOUT_MS,
      });
      return response.data;
    } catch (err: unknown) {
      if (err instanceof Blob) throw blobError(await err.text());
      throw err;
    }
  },
};

// A proxy can answer with an HTML page; the caller then shows its own message.
function blobError(text: string): unknown {
  try {
    return JSON.parse(text);
  } catch {
    return { status: "error", message: "", error_type: "UnknownError" };
  }
}
