import { apiClient } from "./apiUtils";
import type { Team } from "./types";
import type { QueryLanguage } from "@/lib/queryMetadata";

// Optional per-source ClickHouse query settings applied to every query run
// against the source. All keys are optional; keys left unset are omitted from
// the wire JSON (Go side uses pointer + omitempty). Key names must match the
// Go JSON tags exactly (note: `result_overflow_mode`, not `overflow_mode`).
export interface ClickHouseQuerySettings {
  max_execution_time?: number;
  max_result_rows?: number;
  max_result_bytes?: number;
  max_rows_to_read?: number;
  max_bytes_to_read?: number;
  readonly?: number;
  result_overflow_mode?: string;
}

export interface ClickHouseConnectionInfo {
  host: string;
  username?: string;
  password?: string;
  database: string;
  table_name: string;
  tls_enable?: boolean;
  settings?: ClickHouseQuerySettings;
}

export interface VictoriaLogsConnectionInfo {
  base_url: string;
  auth?: {
    mode?: string;
    username?: string;
    password?: string;
    token?: string;
  };
  tenant?: {
    account_id?: string;
    project_id?: string;
  };
  scope?: {
    query?: string;
  };
  headers?: Record<string, string>;
}

export type SourceConnectionInfo =
  | ClickHouseConnectionInfo
  | VictoriaLogsConnectionInfo
  | Record<string, unknown>;

// Narrow a source's connection to the ClickHouse shape. Returns null for
// non-ClickHouse sources so callers can gate table-coordinate UI.
export function asClickHouseConnection(
  connection: SourceConnectionInfo | undefined | null,
): ClickHouseConnectionInfo | null {
  if (!connection || typeof connection !== "object") return null;
  if ("table_name" in connection && "database" in connection) {
    return connection as ClickHouseConnectionInfo;
  }
  return null;
}

export interface ValidateConnectionRequestInfo {
  source_type?: string;
  connection: SourceConnectionInfo;
  timestamp_field?: string;
  severity_field?: string;
}

export interface Source {
  id: number;
  name: string;
  _meta_is_auto_created: boolean;
  source_type: string;
  query_languages?: string[];
  saved_query_editor_modes?: string[];
  alert_editor_modes?: string[];
  capabilities?: string[];
  _meta_ts_field: string;
  _meta_severity_field?: string;
  connection: SourceConnectionInfo;
  description?: string;
  ttl_days: number;
  created_at: string;
  updated_at: string;
  is_connected: boolean;
  schema?: string;
  columns?: ColumnInfo[];
  // ClickHouse specific properties
  engine?: string;
  engine_params?: string[];
  sort_keys?: string[];
}

export interface ColumnInfo {
  name: string;
  type: string;
}

export interface SourceWithTeamsResponse {
  source: Source;
  teams: Team[];
}

export interface SourceWithTeams extends Source {
  teams: Team[];
}

export interface CreateSourcePayload {
  name: string;
  source_type?: string;
  meta_is_auto_created: boolean;
  meta_ts_field?: string;
  meta_severity_field?: string;
  connection: SourceConnectionInfo;
  description?: string;
  ttl_days: number;
  schema?: string;
}

export interface UpdateSourcePayload {
  name?: string;
  description?: string;
  ttl_days?: number;
  meta_ts_field?: string;
  meta_severity_field?: string;
  connection?: SourceConnectionInfo;
}

export interface InspectionDetail {
  key?: string;
  label: string;
  value: string;
  monospace?: boolean;
  multiline?: boolean;
}

export interface InspectionMetric {
  key?: string;
  label: string;
  value: string;
  hint?: string;
}

export interface SourceSchemaField {
  name: string;
  type: string;
  is_nullable?: boolean;
  is_primary_key?: boolean;
  default_expression?: string;
  comment?: string;
  compressed?: string;
  uncompressed?: string;
  compression_ratio?: number;
  avg_row_size?: number;
  row_count?: number;
}

export interface SourceSchemaInspection {
  fields: SourceSchemaField[];
  sort_keys?: string[];
  create_query?: string;
  ttl?: string;
}

export interface SourceActivity {
  rows_1h: number;
  rows_24h: number;
  rows_7d: number;
  latest_ts?: string | null;
  hourly_buckets: { bucket: string; rows: number }[];
  daily_buckets: { bucket: string; rows: number }[];
}

export interface SourceInspection {
  details?: InspectionDetail[];
  storage?: InspectionMetric[];
  activity?: SourceActivity | null;
  schema?: SourceSchemaInspection | null;
}

// Field values types for sidebar exploration
export interface FieldValueInfo {
  value: string;
  count: number;
}

export interface FieldValuesResult {
  field_name: string;
  field_type: string;
  is_low_cardinality: boolean;
  values: FieldValueInfo[];
  total_distinct: number;
}

export type AllFieldValuesResult = Record<string, FieldValuesResult>;

export const sourcesApi = {
  // Source management
  listAllSourcesForAdmin: () =>
    apiClient.get<Source[]>("/admin/sources"),
  listTeamSources: (teamId: number) =>
    apiClient.get<Source[]>(`/teams/${teamId}/sources`),
  getTeamSource: (teamId: number, sourceId: number) =>
    apiClient.get<Source>(`/teams/${teamId}/sources/${sourceId}`),
  createSource: (payload: CreateSourcePayload) =>
    apiClient.post<Source>("/admin/sources", payload),
  updateSource: (id: number, payload: UpdateSourcePayload) =>
    apiClient.put<Source>(`/admin/sources/${id}`, payload),
  deleteSource: (id: number) =>
    apiClient.delete<{ message: string }>(`/admin/sources/${id}`),

  // Source inspection and schema (admin and team-scoped versions)
  getAdminSourceInspection: (sourceId: number) =>
    apiClient.get<SourceInspection>(`/admin/sources/${sourceId}/stats`),
  getTeamSourceInspection: (teamId: number, sourceId: number) =>
    apiClient.get<SourceInspection>(`/teams/${teamId}/sources/${sourceId}/stats`),
  getTeamSourceSchema: (teamId: number, sourceId: number) =>
    apiClient.get<string>(`/teams/${teamId}/sources/${sourceId}/schema`),

  // Validation
  validateSourceConnection: (connectionInfo: ValidateConnectionRequestInfo) =>
    apiClient.post<{ message: string }>("/admin/sources/validate", {
      source_type: connectionInfo.source_type || "clickhouse",
      connection: connectionInfo.connection,
      timestamp_field: connectionInfo.timestamp_field,
      severity_field: connectionInfo.severity_field,
    }),

  // Field values for sidebar exploration
  // Time range is required for performance (avoids full table scan)
  // Query is optional - filters field values based on the current datasource-native query
  getFieldValues: (
    teamId: number,
    sourceId: number,
    fieldName: string,
    fieldType: string,
    startTime: string,  // ISO8601 format
    endTime: string,    // ISO8601 format
    timezone?: string,
    limit?: number,
    queryLanguage?: QueryLanguage,
    query?: string,      // Optional datasource-native query to filter field values
    signal?: AbortSignal // Optional abort signal for request cancellation
  ) => {
    let url = `/teams/${teamId}/sources/${sourceId}/fields/${encodeURIComponent(fieldName)}/values?` +
      `limit=${limit || 10}` +
      `&type=${encodeURIComponent(fieldType)}` +
      `&start_time=${encodeURIComponent(startTime)}` +
      `&end_time=${encodeURIComponent(endTime)}`;
    if (timezone) {
      url += `&timezone=${encodeURIComponent(timezone)}`;
    }
    if (queryLanguage) {
      url += `&query_language=${encodeURIComponent(queryLanguage)}`;
    }
    if (query) {
      url += `&query=${encodeURIComponent(query)}`;
    }
    return apiClient.get<FieldValuesResult>(url, { signal });
  },
  getAllFieldValues: (
    teamId: number,
    sourceId: number,
    startTime: string,  // ISO8601 format
    endTime: string,    // ISO8601 format
    timezone?: string,
    limit?: number,
    queryLanguage?: QueryLanguage,
    query?: string,      // Optional datasource-native query to filter field values
    signal?: AbortSignal // Optional abort signal for request cancellation
  ) => {
    let url = `/teams/${teamId}/sources/${sourceId}/fields/values?` +
      `limit=${limit || 10}` +
      `&start_time=${encodeURIComponent(startTime)}` +
      `&end_time=${encodeURIComponent(endTime)}`;
    if (timezone) {
      url += `&timezone=${encodeURIComponent(timezone)}`;
    }
    if (queryLanguage) {
      url += `&query_language=${encodeURIComponent(queryLanguage)}`;
    }
    if (query) {
      url += `&query=${encodeURIComponent(query)}`;
    }
    return apiClient.get<AllFieldValuesResult>(url, { signal });
  },
};
