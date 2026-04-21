import { apiClient } from "./apiUtils";

export interface MetaResponse {
  version: string;
  http_server_timeout: string;
  max_query_limit: number;
  default_preview_limit?: number;
  max_preview_limit?: number;
  max_export_rows?: number;
}

export const metaApi = {
  /**
   * Get server metadata information
   */
  getMeta: () => apiClient.get<MetaResponse>("/meta"),
};
