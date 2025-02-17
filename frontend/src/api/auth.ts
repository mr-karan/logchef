import { api } from "./config";
import type { APIResponse } from "./types";
import type { Session, User } from "@/types";

export interface SessionResponse {
  user: User;
  session: Session;
}

export const authApi = {
  /**
   * Get current session information
   * Called on app initialization and after page refresh
   */
  async getSession(): Promise<APIResponse<SessionResponse>> {
    try {
      console.log("Making session request...");
      const response = await api.get<APIResponse<SessionResponse>>(
        "/auth/session"
      );
      console.log("Session response received:", response.data);
      return response.data;
    } catch (error) {
      console.error("Error getting session:", error);
      throw error;
    }
  },

  /**
   * Get login URL for OIDC authentication
   * Backend will handle state parameter for CSRF protection
   */
  getLoginUrl(redirectPath?: string): string {
    // Use relative URL for login endpoint
    const loginUrl = "/api/v1/auth/login";
    const params = new URLSearchParams();

    // Add frontend redirect path if provided
    if (redirectPath) {
      params.append("redirect", redirectPath);
    }

    // Add params if any exist
    const queryString = params.toString();
    return queryString ? `${loginUrl}?${queryString}` : loginUrl;
  },

  /**
   * Secure logout
   * Calls backend to invalidate session and clear cookies
   */
  async logout(): Promise<APIResponse<void>> {
    try {
      const response = await api.post<APIResponse<void>>("/auth/logout");
      // Clear any local storage
      sessionStorage.clear();
      localStorage.clear();
      return response.data;
    } catch (error) {
      // Even if backend call fails, clear local data
      sessionStorage.clear();
      localStorage.clear();
      throw error;
    }
  },
};
