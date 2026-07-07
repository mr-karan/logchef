import { apiClient } from "./apiUtils";

export interface MetaResponse {
  version: string;
  http_server_timeout: string;
  max_query_limit: number;
  max_query_timeout_seconds?: number;
  default_preview_limit?: number;
  max_preview_limit?: number;
  max_export_rows?: number;
  // Optional so a stale server without the field is treated as "enabled" by
  // the store default — preserves backwards compatibility.
  alerts_enabled?: boolean;
  local_auth_enabled?: boolean;
  oidc_enabled?: boolean;
}

export const metaApi = {
  /**
   * Get server metadata information
   */
  getMeta: () => apiClient.get<MetaResponse>("/meta"),
};
