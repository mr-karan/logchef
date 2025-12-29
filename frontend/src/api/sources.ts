import { apiClient } from "./apiUtils";
import type { SavedTeamQuery, Team } from "./types";

export type BackendType = "clickhouse" | "victorialogs";

interface ConnectionInfo {
  host: string;
  database: string;
  table_name: string;
}

interface ConnectionRequestInfo {
  host: string;
  username: string;
  password: string;
  database: string;
  table_name: string;
}

export interface VictoriaLogsConnectionInfo {
  url: string;
  account_id?: string;
  project_id?: string;
  stream_labels?: Record<string, string>;
}

export interface Source {
  id: number;
  name: string;
  backend_type: BackendType;
  _meta_is_auto_created: boolean;
  _meta_ts_field: string;
  _meta_severity_field?: string;
  connection: ConnectionInfo;
  victorialogs_connection?: VictoriaLogsConnectionInfo;
  description?: string;
  ttl_days: number;
  created_at: string;
  updated_at: string;
  is_connected: boolean;
  schema?: string;
  columns?: ColumnInfo[];
  engine?: string;
  engine_params?: string[];
  sort_keys?: string[];
}

export function isVictoriaLogsSource(source: Source): boolean {
  return source.backend_type === "victorialogs";
}

export function isClickHouseSource(source: Source): boolean {
  return !source.backend_type || source.backend_type === "clickhouse";
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

export interface TeamGroupedQuery {
  team_id: number;
  team_name: string;
  queries: SavedTeamQuery[];
}

export interface CreateSourcePayload {
  name: string;
  backend_type?: BackendType;
  meta_is_auto_created: boolean;
  meta_ts_field?: string;
  meta_severity_field?: string;
  connection?: ConnectionRequestInfo;
  victorialogs_connection?: VictoriaLogsConnectionInfo;
  description?: string;
  ttl_days?: number;
  schema?: string;
}

export interface CreateTeamQueryRequest {
  team_id: number;
  name: string;
  description?: string;
  query_content: string;
}

export interface SourceStats {
  table_stats: {
    database: string;
    table: string;
    compressed: string;
    uncompressed: string;
    compr_rate: number;
    rows: number;
    part_count: number;
  };
  column_stats: {
    database: string;
    table: string;
    column: string;
    compressed: string;
    uncompressed: string;
    compr_ratio: number;
    rows_count: number;
    avg_row_size: number;
  }[];
  table_info?: {
    database: string;
    name: string;
    engine: string;
    engine_params?: string[];
    columns?: { name: string; type: string }[];
    sort_keys?: string[];
    ext_columns?: { name: string; type: string; default_expression?: string; is_nullable?: boolean; is_primary_key?: boolean; comment?: string }[];
  };
  ttl?: string;
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
  updateSource: (id: number, payload: Partial<Source>) =>
    apiClient.put<Source>(`/admin/sources/${id}`, payload),
  deleteSource: (id: number) =>
    apiClient.delete<{ message: string }>(`/admin/sources/${id}`),

  // Source stats and schema (admin and team-scoped versions)
  getAdminSourceStats: (sourceId: number) =>
    apiClient.get<SourceStats>(`/admin/sources/${sourceId}/stats`),
  getTeamSourceStats: (teamId: number, sourceId: number) =>
    apiClient.get<SourceStats>(`/teams/${teamId}/sources/${sourceId}/stats`),
  getTeamSourceSchema: (teamId: number, sourceId: number) =>
    apiClient.get<string>(`/teams/${teamId}/sources/${sourceId}/schema`),

  // Team-scoped source queries
  listTeamSourceQueries: (teamId: number, sourceId: number) =>
    apiClient.get<SavedTeamQuery[]>(`/teams/${teamId}/sources/${sourceId}/queries`),
  createTeamSourceQuery: (teamId: number, sourceId: number, payload: Omit<CreateTeamQueryRequest, "team_id">) =>
    apiClient.post<SavedTeamQuery>(
      `/teams/${teamId}/sources/${sourceId}/queries`,
      { ...payload, team_id: teamId }
    ),

  validateSourceConnection: (connectionInfo: {
    backend_type?: BackendType;
    connection?: ConnectionRequestInfo;
    victorialogs_connection?: VictoriaLogsConnectionInfo;
    timestamp_field?: string;
    severity_field?: string;
  }) => apiClient.post<{ message: string }>("/admin/sources/validate", connectionInfo),

  // Field values for sidebar exploration
  // Time range is required for performance (avoids full table scan)
  // LogchefQL query is optional - filters field values based on user's query
  getFieldValues: (
    teamId: number,
    sourceId: number,
    fieldName: string,
    fieldType: string,
    startTime: string,  // ISO8601 format
    endTime: string,    // ISO8601 format
    timezone?: string,
    limit?: number,
    logchefql?: string,  // Optional LogchefQL query to filter field values
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
    if (logchefql) {
      url += `&logchefql=${encodeURIComponent(logchefql)}`;
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
    logchefql?: string,  // Optional LogchefQL query to filter field values
    signal?: AbortSignal // Optional abort signal for request cancellation
  ) => {
    let url = `/teams/${teamId}/sources/${sourceId}/fields/values?` +
      `limit=${limit || 10}` +
      `&start_time=${encodeURIComponent(startTime)}` +
      `&end_time=${encodeURIComponent(endTime)}`;
    if (timezone) {
      url += `&timezone=${encodeURIComponent(timezone)}`;
    }
    if (logchefql) {
      url += `&logchefql=${encodeURIComponent(logchefql)}`;
    }
    return apiClient.get<AllFieldValuesResult>(url, { signal });
  },
};
