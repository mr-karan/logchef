import { apiClient } from "./apiUtils";
import type { APIResponse } from "./types";
import type { Session, User } from "@/types";

export interface SessionResponse {
  user: User;
  session: Session;
}

export const authApi = {
  /**
   * Get current session information
   */
  getSession: () => apiClient.get<SessionResponse>("/auth/session"),

  /**
   * Get login URL for OIDC authentication
   */
  getLoginUrl(redirectPath?: string): string {
    const loginUrl = "/api/v1/auth/login";
    const params = redirectPath ? new URLSearchParams({ redirect: redirectPath }) : null;
    return params ? `${loginUrl}?${params}` : loginUrl;
  },

  /**
   * Secure logout
   */
  async logout(): Promise<APIResponse<void>> {
    const response = await apiClient.post<void>("/auth/logout");
    sessionStorage.clear();
    localStorage.clear();
    return response;
  },
};
