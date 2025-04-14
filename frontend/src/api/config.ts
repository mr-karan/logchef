import axios from "axios";
import { useAuthStore } from "@/stores/auth";
import router from "@/router";
import { showErrorToast } from "@/api/error-handler";

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
  // Only apply no-cache headers to authentication endpoints
  if (config.url?.startsWith("/auth/")) {
    config.headers["Cache-Control"] = "no-cache, no-store, must-revalidate";
    config.headers["Pragma"] = "no-cache";
    config.headers["Expires"] = "0";
  }

  return config;
});

// Add response interceptor for handling auth errors
api.interceptors.response.use(
  (response) => {
    return response;
  },
  async (error) => {
    // Create a standardized error response
    const errorResponse = error.response?.data || {
      status: "error",
      message: error.message || "Network error: Unable to connect to the server",
      error_type: "NetworkError",
    };

    // Handle session expiration
    if (error.response?.status === 401) {
      // Clear auth state using the store
      const authStore = useAuthStore();
      await authStore.clearState();

      // Get current path for redirect, including hash if present
      const currentPath = window.location.pathname + window.location.search +
        (window.location.hash ? window.location.hash : '');

      // Check if we're already on the login page (including /auth/login pattern)
      const isOnLoginPage = window.location.pathname.includes("/auth/login") ||
                          window.location.pathname.includes("/login") ||
                          router.currentRoute.value.name === "Login";

      // Check if this is the initial session check or a later API call
      const isInitialSessionCheck = error.config?.url === "/me" &&
                                  !sessionStorage.getItem("hadPreviousSession");

      // Only show the toast if:
      // 1. We're not on the login page already
      // 2. This isn't the first session check when the app loads
      if (!isOnLoginPage && !isInitialSessionCheck) {
        // Create a custom error object with the specific message and error type
        const sessionError = {
          status: "error",
          message: "Your session has expired. Please log in again.",
          error_type: "AuthenticationError",
        };
        showErrorToast(sessionError);
      }

      // Redirect to login with the current path as redirect parameter
      router.push({
        name: "Login",
        query: { redirect: currentPath },
        replace: true // Replace current history entry to avoid navigation issues after login
      });
      return Promise.reject(errorResponse);
    }

    // Handle forbidden errors
    if (error.response?.status === 403) {
      // Create a custom error object with the specific message and error type
      const forbiddenError = {
        status: "error",
        message: "You don't have permission to access this resource.",
        error_type: "AuthorizationError",
      };

      // Only show toast if not a CSRF error (which is handled separately)
      if (error.response?.data?.code !== "CSRF_TOKEN_MISMATCH") {
        showErrorToast(forbiddenError);
        router.push({ name: "Forbidden" });
      }
      return Promise.reject(forbiddenError);
    }

    // Handle CSRF token errors
    if (
      error.response?.status === 403 &&
      error.response?.data?.code === "CSRF_TOKEN_MISMATCH"
    ) {
      // Create a custom error object with the specific message and error type
      const csrfError = {
        status: "error",
        message:
          "Security verification failed. Please refresh the page and try again.",
        error_type: "SecurityError",
      };
      showErrorToast(csrfError);
      return Promise.reject(csrfError);
    }

    // For other errors, we'll let the calling code handle them
    // but we standardize the error format
    return Promise.reject(errorResponse);
  }
);
