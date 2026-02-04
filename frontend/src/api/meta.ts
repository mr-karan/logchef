import { apiClient } from "./apiUtils";

export interface MetaResponse {
  version: string;
  http_server_timeout: string;
  max_query_limit: number;
}

export const metaApi = {
  /**
   * Get server metadata information
   */
  getMeta: () => apiClient.get<MetaResponse>("/meta"),
};