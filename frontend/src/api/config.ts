import axios from "axios";
import { useAuthStore } from "@/stores/auth";
import { navigationService } from "@/services/navigation";

// Create base axios instance with common configuration
export const api = axios.create({
  baseURL: `/api/v1`,
  headers: {
    "Content-Type": "application/json",
  },
  withCredentials: false, // Explicitly disable CORS credentials since we're same-origin
  xsrfCookieName: "csrf_token", // Name of the cookie sent by the server
  xsrfHeaderName: "X-CSRF-Token", // Header that contains the token
  timeout: 10000, // 10 second timeout
});

// Add request interceptor for security headers
api.interceptors.request.use((config) => {
  // Add security headers
  config.headers["X-Requested-With"] = "XMLHttpRequest";

  // Prevent caching of auth endpoints
  if (config.url?.startsWith("/auth/")) {
    config.headers["Cache-Control"] = "no-cache, no-store, must-revalidate";
    config.headers["Pragma"] = "no-cache";
    config.headers["Expires"] = "0";
  }

  return config;
});

// Add response interceptor for handling auth errors
api.interceptors.response.use(
  (response) => response,
  async (error) => {
    // Handle session expiration
    if (error.response?.status === 401) {
      // Clear auth state using the store
      const authStore = useAuthStore();
      await authStore.clearState();

      // Get current path for redirect
      const currentPath = window.location.pathname + window.location.search;
      await navigationService.goToLogin(currentPath);
      return Promise.reject(error);
    }

    // Handle forbidden errors
    if (error.response?.status === 403) {
      await navigationService.goToForbidden();
      return Promise.reject(error);
    }

    // Handle CSRF token errors
    if (
      error.response?.status === 403 &&
      error.response?.data?.code === "CSRF_TOKEN_MISMATCH"
    ) {
      // Could be CSRF token mismatch
      console.error("Security error:", error);
      return Promise.reject(new Error("Security verification failed"));
    }

    return Promise.reject(error);
  }
);
