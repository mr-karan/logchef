import { api } from "./config";
import { apiClient, type RequestOptions } from "./apiUtils";

// ---------------------------------------------------------------------------
// Admin Query Activity
//
// Mirrors GET /api/v1/admin/query-stats (core.QueryActivity in Go), read from
// the daily rollups. Every count covers `window` and honors `filters`; each
// `prior` covers `prior_window`, the same number of days just before it.
// Dates are "YYYY-MM-DD" (UTC), both ends inclusive.
// ---------------------------------------------------------------------------

export type QueryAccount = "human" | "service" | "all";

/** Average latency is total_duration_ms / query_count. */
export interface QueryTotals {
  query_count: number;
  total_duration_ms: number;
}

export interface DateWindow {
  since: string;
  until: string;
  days: number;
}

export interface ActivityPeriod extends QueryTotals {
  active_users: number;
  active_sources: number;
  avg_daily_active_users: number;
}

export interface DailyQueryStat {
  date: string;
  query_count: number;
  active_users: number;
}

/** Identity fields are "" when the user is deleted. */
export interface UserQueryStat {
  user_id: number;
  email: string;
  full_name: string;
  account_type: "human" | "service" | "";
  active_days: number;
  current: QueryTotals;
  prior: QueryTotals;
}

/** Open findings of a source; analyzed is false before its first report. */
export interface AdvisorFindingCounts {
  analyzed: boolean;
  high: number;
  medium: number;
  low: number;
}

export interface SourceActivity {
  source_id: number;
  source_name: string;
  source_type: string;
  /** A team that links the source now, to open it in Explorer; null when none does. */
  team_id: number | null;
  /** Service-account queries in the window, whatever the account filter. */
  service_query_count: number;
  current: QueryTotals;
  prior: QueryTotals;
  /** Null for a source the schema advisor does not analyze. */
  advisor: AdvisorFindingCounts | null;
}

export interface ShapeFilterItem {
  column: string;
  path?: string[];
  access?: string;
  kind: string;
}

export interface ShapeActivityRow {
  source_id: number;
  source_name: string;
  source_type: string;
  query_language: string;
  shape_hash: string;
  shape: ShapeFilterItem[];
  max_lookback_s: number | null;
  current: QueryTotals;
  /** Null when the shape rollup no longer keeps the whole prior window. */
  prior: QueryTotals | null;
  finding: { id: string; title: string; impact: string } | null;
}

export interface QueryStatsResponse {
  window: DateWindow;
  prior_window: DateWindow;
  filters: {
    account: QueryAccount;
    source_id: number | null;
    source_name: string;
    user_id: number | null;
    user_name: string;
  };
  /** Ignores filters.account. */
  account_totals: { human: number; service: number; all: number };
  /** Active users of the selected account type. */
  total_users: number;
  summary: { current: ActivityPeriod; prior: ActivityPeriod };
  /** Every day of the window, ascending. */
  volume_by_day: DailyQueryStat[];
  top_users: UserQueryStat[];
  service_summary: { account_count: number; query_count: number; top: UserQueryStat[] } | null;
  sources: SourceActivity[];
  /** No user or account filter applies; `since` is where kept rows start. */
  shapes: { since: string; rows: ShapeActivityRow[]; unanalyzed_query_count: number };
  language_mix: { language: string; query_count: number }[];
}

export interface QueryStatsParams {
  days: number;
  account: QueryAccount;
  source_id?: number;
  user_id?: number;
}

export const adminApi = {
  getQueryStats: (params: QueryStatsParams, options?: RequestOptions) => {
    const search = new URLSearchParams({ days: String(params.days), account: params.account });
    if (params.source_id) search.set("source_id", String(params.source_id));
    if (params.user_id) search.set("user_id", String(params.user_id));
    return apiClient.get<QueryStatsResponse>(`/admin/query-stats?${search}`, options);
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
