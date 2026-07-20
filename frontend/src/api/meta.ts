import { apiClient } from "./apiUtils";

// Server-advertised dashboard result-cache policy (whole seconds). The client
// clamps its per-dashboard TTL to `max_ttl_seconds` — the SAME clamp the server
// applies — so the snap bucket, the cache directive, and the server-side cache
// TTL all coincide. ABSENT on an old server (see the store's fail-closed parse).
export interface DashboardCachePolicy {
  enabled: boolean;
  default_ttl_seconds: number;
  max_ttl_seconds: number;
}

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
  // Dashboard result-cache policy; ABSENT on an old server (→ null in the store,
  // which means "cache unavailable / fail closed").
  dashboard_cache?: DashboardCachePolicy;
}

export const metaApi = {
  /**
   * Get server metadata information
   */
  getMeta: () => apiClient.get<MetaResponse>("/meta"),
};
