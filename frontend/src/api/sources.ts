import { apiClient } from "./apiUtils";
import type { SavedTeamQuery, Team } from "./types";

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

export interface Source {
  id: number;
  name: string;
  _meta_is_auto_created: boolean;
  _meta_ts_field: string;
  _meta_severity_field?: string;
  connection: ConnectionInfo;
  description?: string;
  ttl_days: number;
  created_at: string;
  updated_at: string;
  is_connected: boolean;
  schema?: string;
  columns?: ColumnInfo[];
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
  meta_is_auto_created: boolean;
  meta_ts_field?: string;
  meta_severity_field?: string;
  connection: ConnectionRequestInfo;
  description?: string;
  ttl_days: number;
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
}

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

  // Validation
  validateSourceConnection: (connectionInfo: ConnectionRequestInfo & {
    timestamp_field?: string;
    severity_field?: string;
  }) => apiClient.post<{ message: string }>("/admin/sources/validate", connectionInfo)
};
