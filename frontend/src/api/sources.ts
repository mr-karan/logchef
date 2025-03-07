import { api } from "./config";
import type { APIResponse, SavedTeamQuery, Team } from "./types";

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
  meta_is_auto_created: boolean;
  meta_ts_field?: string;
  connection: ConnectionRequestInfo;
  description?: string;
  ttl_days: number;
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
    rows: number; // This will be treated as a number in JavaScript even though it's uint64 in Go
    part_count: number; // This will be treated as a number in JavaScript even though it's uint64 in Go
  };
  column_stats: {
    database: string;
    table: string;
    column: string;
    compressed: string;
    uncompressed: string;
    compr_ratio: number;
    rows_count: number; // This will be treated as a number in JavaScript even though it's uint64 in Go
    avg_row_size: number;
  }[];
}

export const sourcesApi = {
  async listSources() {
    const response = await api.get<APIResponse<Source[]>>("/sources");
    return response.data;
  },

  async listUserSources() {
    const response = await api.get<APIResponse<SourceWithTeamsResponse[]>>(
      "/user/sources"
    );
    return response.data;
  },

  async getSource(id: number) {
    const response = await api.get<APIResponse<Source>>(`/sources/${id}`);
    return response.data;
  },

  async createSource(payload: CreateSourcePayload) {
    const response = await api.post<APIResponse<Source>>("/sources", payload);
    return response.data;
  },

  async deleteSource(id: number) {
    const response = await api.delete<APIResponse<void>>(`/sources/${id}`);
    return response.data;
  },

  async getSourceStats(sourceId: number) {
    const response = await api.get<APIResponse<SourceStats>>(
      `/sources/${sourceId}/stats`
    );
    return response.data;
  },

  async listSourceQueries(sourceId: number, groupByTeam: boolean = true) {
    const response = await api.get<APIResponse<TeamGroupedQuery[]>>(
      `/sources/${sourceId}/queries?groupByTeam=${groupByTeam}`
    );
    return response.data;
  },

  async createSourceQuery(sourceId: number, payload: CreateTeamQueryRequest) {
    const response = await api.post<APIResponse<SavedTeamQuery>>(
      `/sources/${sourceId}/queries`,
      payload
    );
    return response.data;
  },
};
