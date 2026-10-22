import { apiClient } from "./apiUtils";
import { apiBaseURL } from "./config";
import { streamSSE } from "./eventStream";
import type { TemplateVariable } from "./logchefql";

// Mirrors the backend AIRequestTimeout (90s): reasoning-capable models can take
// tens of seconds, and the shared axios client otherwise aborts after 10s.
const AI_GENERATE_TIMEOUT_SECONDS = 95;

// Keep these for the UI filter builder
export interface FilterCondition {
  field: string;
  operator:
    | "="
    | "!="
    | "~"
    | "!~"
    | "contains"
    | "not_contains"
    | "icontains"
    | "startswith"
    | "endswith"
    | "in"
    | "not_in"
    | "is_null"
    | "is_not_null";
  value: string;
}

// AI Query generation types
export interface AIGenerateSQLRequest {
  natural_language_query: string;
  current_query?: string; // Optional current query for context
  // Editor mode; the server combines it with the source backend to pick the
  // concrete target language. Empty/absent is treated as "native".
  mode?: "logchefql" | "native";
}

export interface AIGenerateSQLResponse {
  sql_query: string;
  // Concrete target language the server generated ("clickhouse-sql" |
  // "logchefql" | "logsql"). Optional.
  language?: string;
}


export interface ColumnInfo {
  name: string;
  type: string;
}

// Simplified query parameters - intended for API communication
export interface QueryParams {
  extra_stream_filters?: string[];
  query_text: string;
  limit?: number;
  window?: string;
  group_by?: string;
  timezone?: string; // User's timezone identifier (e.g., 'America/New_York', 'UTC')
  start_time?: string; // ISO formatted start time
  end_time?: string;   // ISO formatted end time
  query_timeout?: number; // Query timeout in seconds
  variables?: TemplateVariable[]; // Template variables for SQL substitution
}

export interface QueryStats {
  execution_time_ms: number;
  rows_read: number;
  bytes_read: number;
  rows_returned?: number;
  bytes_returned?: number;
  limit_applied?: number;
  truncated?: boolean;
  truncated_reason?: string;
}

export interface QueryWarning {
  code: string;
  message: string;
}

export interface QuerySuccessResponse {
  logs?: Record<string, unknown>[] | null; // For backward compatibility
  data?: Record<string, unknown>[] | null; // New structure
  stats: QueryStats;
  params?: QueryParams & {
    source_id: number;
  };
  columns: ColumnInfo[];
  query_id?: string; // Add query_id for cancellation
  warnings?: QueryWarning[];
}

export interface QueryErrorResponse {
  error: string;
  details?: string; // For exposing ClickHouse errors
}

export type QueryResponse = QuerySuccessResponse | QueryErrorResponse;

// Histogram data types
export interface HistogramDataPoint {
  bucket: string;
  log_count: number;
  group_value?: string; // Optional field for grouped data
  is_other?: boolean; // true when this row represents the synthetic remainder
  is_null?: boolean; // true when this row represents a structural SQL NULL group
}

export interface HistogramResponse {
  granularity: string;
  data: HistogramDataPoint[];
  /** Non-fatal notice (e.g., series were capped to a top-N set). */
  notice?: string;
}

// Log context types (surrounding logs around a target timestamp)
export interface LogContextRequest {
  source_id: number;
  timestamp: number;
  timestamp_rfc3339?: string;
  before_limit?: number;
  after_limit?: number;
  before_offset?: number;
  after_offset?: number;
  exclude_boundary?: boolean;
}

export interface LogContextResponse {
  target_timestamp: number;
  before_logs: Record<string, unknown>[];
  target_logs: Record<string, unknown>[];
  after_logs: Record<string, unknown>[];
  stats: QueryStats;
}

export interface ExportLogsRequest extends QueryParams {
  format?: "csv" | "ndjson";
}

export interface ExportJobResponse {
  id: string;
  status: "pending" | "running" | "complete" | "failed";
  format: "csv" | "ndjson";
  file_name?: string;
  error_message?: string;
  rows_exported?: number;
  bytes_written?: number;
  expires_at: string;
  completed_at?: string;
  created_at: string;
  updated_at: string;
  status_url?: string;
  download_url?: string;
}

export interface QuerySharePayload {
  version: number;
  mode: "logchefql" | "native";
  query: string;
  limit: number;
  time_range?: {
    relative?: string;
    absolute?: {
      start: number;
      end: number;
    };
  };
  timezone?: string;
  variables?: Array<{
    name: string;
    type: string;
    label?: string;
    inputType?: string;
    value: unknown;
    defaultValue?: unknown;
    isOptional?: boolean;
    isRequired?: boolean;
    options?: Array<{ value: string; label?: string }>;
  }>;
}

export interface QueryShareResponse {
  token: string;
  share_url: string;
  team_id: number;
  source_id: number;
  payload: QuerySharePayload;
  expires_at: string;
  created_at: string;
  created_by: number;
}

// Persistent, server-backed query history (cross-device). Backed by
// GET /api/v1/me/query-history, which returns the caller's recent queries
// across all teams/sources, newest-first. See issue #58.
export type QueryHistoryLanguage = "logchefql" | "clickhouse-sql" | "logsql";

export interface QueryHistoryRecord {
  id: number;
  team_id: number;
  source_id: number;
  query_text: string;
  query_language: QueryHistoryLanguage;
  duration_ms: number;
  row_count: number;
  created_at: string;
}

export const exploreApi = {
  getLogs: (sourceId: number, params: QueryParams, teamId: number, signal?: AbortSignal) => {
    if (!teamId) {
      throw new Error("Team ID is required for querying logs");
    }
    
    // Extract timeout from params and convert to axios options
    const timeout = params.query_timeout || 30; // Default to 30 seconds
    
    return apiClient.post<QueryResponse>(
      `/teams/${teamId}/sources/${sourceId}/logs/query`,
      params,
      { timeout, signal }
    );
  },

  getHistogramData: (sourceId: number, params: QueryParams, teamId: number, signal?: AbortSignal) => {
    if (!teamId) {
      throw new Error("Team ID is required for getting histogram data");
    }

    // Clean up params to ensure group_by is only included when it has a meaningful value
    const histogramParams = {
      ...params
    };

    // Let the body-level params come through as they are,
    // but don't add an empty string for group_by if it's not meaningful
    if (histogramParams.group_by === '') {
      delete histogramParams.group_by;
    }

    // Extract timeout from params
    const timeout = params.query_timeout || 30; // Default to 30 seconds

    return apiClient.post<HistogramResponse>(
      `/teams/${teamId}/sources/${sourceId}/logs/histogram`,
      histogramParams,
      { timeout, signal }
    );
  },

  getLogContext: (sourceId: number, params: LogContextRequest, teamId: number) => {
    if (!teamId) {
      throw new Error("Team ID is required for getting log context");
    }
    return apiClient.post<LogContextResponse>(
      `/teams/${teamId}/sources/${sourceId}/logs/context`,
      params
    );
  },

  generateAISQL: (sourceId: number, params: AIGenerateSQLRequest, teamId: number) => {
    if (!teamId) {
      throw new Error("Team ID is required for AI SQL generation");
    }
    if (!sourceId) {
      throw new Error("Source ID is required for AI SQL generation");
    }
    // Reasoning models can think for a while, and the server allows up to 90s
    // (AIRequestTimeout); the shared client default of 10s would abort first.
    return apiClient.post<AIGenerateSQLResponse>(
      `/teams/${teamId}/sources/${sourceId}/generate-sql`,
      params,
      { timeout: AI_GENERATE_TIMEOUT_SECONDS }
    );
  },

  cancelQuery: (sourceId: number, queryId: string, teamId: number) => {
    if (!teamId) {
      throw new Error("Team ID is required for cancelling queries");
    }
    if (!sourceId) {
      throw new Error("Source ID is required for cancelling queries");
    }
    if (!queryId) {
      throw new Error("Query ID is required for cancelling queries");
    }
    return apiClient.post<{message: string; query_id: string}>(
      `/teams/${teamId}/sources/${sourceId}/logs/query/${queryId}/cancel`,
      {}
    );
  },

  createExportJob: (sourceId: number, params: ExportLogsRequest, teamId: number) => {
    if (!teamId) {
      throw new Error("Team ID is required for creating exports");
    }
    if (!sourceId) {
      throw new Error("Source ID is required for creating exports");
    }
    return apiClient.post<ExportJobResponse>(
      `/teams/${teamId}/sources/${sourceId}/exports`,
      params,
      { timeout: params.query_timeout || 120 }
    );
  },

  getExportJob: (sourceId: number, exportId: string, teamId: number) => {
    if (!teamId) {
      throw new Error("Team ID is required for checking export status");
    }
    if (!sourceId) {
      throw new Error("Source ID is required for checking export status");
    }
    if (!exportId) {
      throw new Error("Export ID is required for checking export status");
    }
    return apiClient.get<ExportJobResponse>(
      `/teams/${teamId}/sources/${sourceId}/exports/${encodeURIComponent(exportId)}`
    );
  },

  createQueryShare: (sourceId: number, payload: QuerySharePayload, teamId: number) => {
    if (!teamId) {
      throw new Error("Team ID is required for sharing queries");
    }
    if (!sourceId) {
      throw new Error("Source ID is required for sharing queries");
    }
    return apiClient.post<QueryShareResponse>(
      `/teams/${teamId}/sources/${sourceId}/query-shares`,
      { payload }
    );
  },

  getQueryShare: (token: string) => {
    if (!token) {
      throw new Error("Share token is required");
    }
    return apiClient.get<QueryShareResponse>(`/query-shares/${encodeURIComponent(token)}`);
  },

  // Fetch the caller's recent query history (newest-first). limit is clamped
  // server-side (default 50, max 200).
  getMyQueryHistory: (limit?: number) => {
    const search = typeof limit === "number" ? `?limit=${limit}` : "";
    return apiClient.get<QueryHistoryRecord[]>(`/me/query-history${search}`);
  }
};

// ---------------------------------------------------------------------------
// Live tail (SSE)
//
// The tail endpoint streams Server-Sent Events over a plain GET (session-cookie
// auth is automatic). Axios can't consume a streaming body, so live tail uses
// the fetch + ReadableStream API directly with the dependency-free SSE parser.
// ---------------------------------------------------------------------------

// query_language values accepted by the tail endpoint. clickhouse-sql is
// rejected server-side (400) by design, so it is intentionally excluded here.
export type TailQueryLanguage = "logchefql" | "logsql";

export interface TailNotice {
  code?: string;
  message?: string;
  /**
   * Cumulative dropped-row count for the whole tail session, as reported by
   * the server. When present, prefer this over parsing `message`. Older
   * server builds omit this field and only send the human-readable message.
   */
  dropped_total?: number;
}

export interface TailEnd {
  reason?: string;
  message?: string;
}

export interface TailCallbacks {
  /** Fired once the stream is open (initial `: ok` received / headers flushed). */
  onOpen?: () => void;
  /** A batch of new rows (oldest-first, as the backend emits them). */
  onRows?: (rows: Record<string, any>[]) => void;
  /** A `notice` event (e.g. rate-limited drop). */
  onNotice?: (notice: TailNotice) => void;
  /** An `end` event (TTL expiry, error, or normal completion). */
  onEnd?: (end: TailEnd) => void;
  /** A heartbeat comment (`: hb`), useful for liveness UI. */
  onHeartbeat?: () => void;
}

/**
 * Build the live-tail SSE URL. query may be empty (an unfiltered tail).
 */
export function buildTailUrl(
  teamId: number,
  sourceId: number,
  query: string,
  queryLanguage: TailQueryLanguage
): string {
  if (!teamId) throw new Error("Team ID is required for live tail");
  if (!sourceId) throw new Error("Source ID is required for live tail");
  const params = new URLSearchParams({
    query: query ?? "",
    query_language: queryLanguage,
  });
  return `${apiBaseURL}/teams/${teamId}/sources/${sourceId}/logs/tail?${params.toString()}`;
}

function parseJsonSafe<T>(data: string): T | null {
  try {
    return JSON.parse(data) as T;
  } catch {
    return null;
  }
}

/**
 * Open an abortable live-tail SSE stream and dispatch parsed frames to the
 * provided callbacks. Resolves when the stream closes (server `end`, reader
 * done, or abort). Rejects on connection/HTTP errors (e.g. 429 over cap); the
 * rejected error carries `.status` when available. Aborting via `signal`
 * resolves silently rather than rejecting.
 */
export async function subscribeToTail(
  url: string,
  signal: AbortSignal,
  callbacks: TailCallbacks
): Promise<void> {
  const outcome = await streamSSE(
    url,
    { method: "GET", signal, label: "Live tail request", onOpen: () => callbacks.onOpen?.() },
    (frame) => {
      if (frame.type === "comment") {
        if (frame.text === "hb") callbacks.onHeartbeat?.();
        return;
      }
      switch (frame.event) {
        case "rows": {
          const rows = parseJsonSafe<Record<string, unknown>[]>(frame.data);
          if (Array.isArray(rows) && rows.length) callbacks.onRows?.(rows);
          return;
        }
        case "notice":
          callbacks.onNotice?.(parseJsonSafe<TailNotice>(frame.data) ?? {});
          return;
        case "end":
          callbacks.onEnd?.(parseJsonSafe<TailEnd>(frame.data) ?? {});
          return "stop";
      }
    }
  );

  // A silent EOF would leave the UI showing "Live" forever.
  if (outcome === "eof") {
    callbacks.onEnd?.({
      reason: "connection_lost",
      message: "Connection closed unexpectedly before the stream signaled completion.",
    });
  }
}
