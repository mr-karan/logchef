import { api } from "./config";
import type { APIResponse } from "./types";

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
  id: string;
  _meta_is_auto_created: boolean;
  _meta_ts_field: string;
  connection: ConnectionInfo;
  description?: string;
  ttl_days: number;
  created_at: string;
  updated_at: string;
  is_connected: boolean;
}

export interface CreateSourcePayload {
  meta_is_auto_created: boolean;
  meta_ts_field?: string;
  connection: ConnectionRequestInfo;
  description?: string;
  ttl_days: number;
}

export const sourcesApi = {
  async listSources() {
    const response = await api.get<APIResponse<Source[]>>("/sources");
    return response.data;
  },

  async getSource(id: string) {
    const response = await api.get<APIResponse<Source>>(`/sources/${id}`);
    return response.data;
  },

  async createSource(payload: CreateSourcePayload) {
    const response = await api.post<APIResponse<Source>>("/sources", payload);
    return response.data;
  },

  async deleteSource(id: string) {
    const response = await api.delete<APIResponse<void>>(`/sources/${id}`);
    return response.data;
  },
};
