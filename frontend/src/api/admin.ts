import { apiClient } from "./apiUtils";

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

export interface QueryActivitySlowest {
  id: number;
  user_email: string;
  source_id: number;
  source_name: string;
  query_text: string;
  query_language: string;
  duration_ms: number;
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
  recent: QueryActivityRecent[];
  by_language: QueryActivityByLanguage[];
  by_source: QueryActivityBySource[];
  slowest: QueryActivitySlowest[];
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
  top_sources: TopSourceStat[];
  top_users: TopUserStat[];
  // Ascending by date.
  volume_by_day: DailyQueryVolume[];
}

export const adminApi = {
  // Fetch recent query activity across all users. `limit` controls the
  // recent-feed length; it is clamped server-side (default 100, max 500).
  getQueryActivity: (limit?: number) => {
    const search = typeof limit === "number" ? `?limit=${limit}` : "";
    return apiClient.get<QueryActivityResponse>(`/admin/query-activity${search}`);
  },

  // Fetch authoritative all-time usage stats over the last `days` (default 30,
  // clamped 1..365 server-side).
  getQueryStats: (days?: number) => {
    const search = typeof days === "number" ? `?days=${days}` : "";
    return apiClient.get<QueryStatsResponse>(`/admin/query-stats${search}`);
  },
};
