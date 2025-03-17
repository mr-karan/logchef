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
    // Admin endpoint for listing all sources
    const response = await api.get<APIResponse<Source[]>>("/admin/sources");
    return response.data;
  },

  async listTeamSources(teamId: number) {
    const response = await api.get<APIResponse<Source[]>>(
      `/teams/${teamId}/sources`
    );
    return response.data;
  },

  async getTeamSource(teamId: number, sourceId: number) {
    const response = await api.get<APIResponse<Source>>(
      `/teams/${teamId}/sources/${sourceId}`
    );
    return response.data;
  },

  async createSource(payload: CreateSourcePayload) {
    const response = await api.post<APIResponse<Source>>(
      "/admin/sources",
      payload
    );
    return response.data;
  },

  async deleteSource(id: number) {
    const response = await api.delete<APIResponse<{ message: string }>>(
      `/admin/sources/${id}`
    );
    return response.data;
  },

  async getSourceStats(sourceId: number) {
    const response = await api.get<APIResponse<SourceStats>>(
      `/admin/sources/${sourceId}/stats`
    );
    return response.data;
  },

  async getTeamSourceStats(teamId: number, sourceId: number) {
    const response = await api.get<APIResponse<SourceStats>>(
      `/teams/${teamId}/sources/${sourceId}/stats`
    );
    return response.data;
  },

  async getTeamSourceSchema(teamId: number, sourceId: number) {
    const response = await api.get<APIResponse<string>>(
      `/teams/${teamId}/sources/${sourceId}/schema`
    );
    return response.data;
  },

  async listSourceQueries(sourceId: number, groupByTeam: boolean = true) {
    const response = await api.get<
      APIResponse<TeamGroupedQuery[] | SavedTeamQuery[]>
    >(
      `/admin/sources/${sourceId}/queries${
        groupByTeam ? "?group_by_team=true" : ""
      }`
    );
    return response.data;
  },

  async listTeamSourceQueries(teamId: number, sourceId: number) {
    const response = await api.get<APIResponse<SavedTeamQuery[]>>(
      `/teams/${teamId}/sources/${sourceId}/queries`
    );
    return response.data;
  },

  async createSourceQuery(sourceId: number, payload: CreateTeamQueryRequest) {
    const response = await api.post<APIResponse<SavedTeamQuery>>(
      `/admin/sources/${sourceId}/queries`,
      payload
    );
    return response.data;
  },

  async createTeamSourceQuery(
    teamId: number,
    sourceId: number,
    payload: Omit<CreateTeamQueryRequest, "team_id">
  ) {
    const fullPayload: CreateTeamQueryRequest = {
      ...payload,
      team_id: teamId,
    };

    const response = await api.post<APIResponse<SavedTeamQuery>>(
      `/teams/${teamId}/sources/${sourceId}/queries`,
      fullPayload
    );
    return response.data;
  },

  async validateSourceConnection(
    connectionInfo: ConnectionRequestInfo & {
      timestamp_field?: string;
      severity_field?: string;
    }
  ) {
    const response = await api.post<
      APIResponse<{ success: boolean; message: string }>
    >("/admin/sources/validate", connectionInfo);
    return response.data;
  },
};
