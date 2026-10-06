import { apiClient } from "./apiUtils";
import type { TokenScope } from "@/lib/tokenScopes";

export type OAuthClientKind = "native" | "web" | "cimd" | "unknown";
export type OAuthResourceKind = "api" | "mcp" | "unknown";

export interface OAuthClientInfo {
  id: string;
  name: string;
  kind: OAuthClientKind;
  /** Host of a CIMD client's metadata URL; the name is self-declared. */
  host?: string;
}

export interface OAuthScopeInfo {
  scope: TokenScope;
  description: string;
}

export interface OAuthConsentRequest {
  id: string;
  client: OAuthClientInfo;
  instance: string;
  resource: string;
  resource_kind: OAuthResourceKind;
  scopes: OAuthScopeInfo[];
  offline_access: boolean;
  redirect_uri: string;
  user: { id: number; email: string; full_name: string };
  expires_at: string;
}

export interface ConnectedApp {
  id: number;
  client: OAuthClientInfo;
  resource: string;
  resource_kind: OAuthResourceKind;
  scopes: TokenScope[];
  offline_access: boolean;
  created_at: string;
  last_used_at: string | null;
}

// Consent and Connected apps are session-only. The browser sends Origin on
// POST and DELETE, which the server checks against its public URL.
export const oauthApi = {
  getRequest: (id: string) =>
    apiClient.get<OAuthConsentRequest>(`/oauth/requests/${encodeURIComponent(id)}`, { suppressErrorToast: true }),
  decide: (id: string, approve: boolean) =>
    apiClient.post<{ redirect_url: string }>(`/oauth/requests/${encodeURIComponent(id)}/decision`, { approve }, { suppressErrorToast: true }),
  listConnectedApps: () => apiClient.get<ConnectedApp[]>("/me/connected-apps"),
  revokeConnectedApp: (id: number) => apiClient.delete<null>(`/me/connected-apps/${id}`),
};
