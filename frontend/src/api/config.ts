import axios from "axios";
import { showErrorToast } from "@/api/error-handler";
import type { RequestOptions } from "./apiUtils";
import type { APIErrorResponse } from "@/api/types";
import { basePath } from "@/utils/basePath";

let unauthorizedHandler: ((options: { toast: boolean }) => Promise<APIErrorResponse>) | undefined;
let reportedMissingUnauthorizedHandler = false;
let forbiddenHandler: (() => void) | undefined;

export function setUnauthorizedHandler(handler: NonNullable<typeof unauthorizedHandler>): void {
  unauthorizedHandler = handler;
}

export function handleUnauthorized(options: { toast: boolean }): Promise<APIErrorResponse> | undefined {
  if (unauthorizedHandler) return unauthorizedHandler(options);
  if (import.meta.env.DEV && !reportedMissingUnauthorizedHandler) {
    reportedMissingUnauthorizedHandler = true;
    console.error("No session expiration handler registered");
  }
}

export function setForbiddenHandler(handler: NonNullable<typeof forbiddenHandler>): void {
  forbiddenHandler = handler;
}

export const apiBaseURL = `${basePath}api/v1`;

// Create base axios instance with common configuration
export const api = axios.create({
  baseURL: apiBaseURL,
  headers: {
    "Content-Type": "application/json",
  },
  withCredentials: false, // Explicitly disable CORS credentials since we're same-origin
  xsrfCookieName: "csrf_token", // Name of the cookie sent by the server
  xsrfHeaderName: "X-CSRF-Token", // Header that contains the token
  timeout: 10000, // 10 second timeout
});

// Synchronous, so a request is sent before Vue's queued render runs instead of after it.
api.interceptors.request.use((config) => {
  // Only apply no-cache headers to authentication endpoints
  if (config.url?.startsWith("/auth/")) {
    config.headers["Cache-Control"] = "no-cache, no-store, must-revalidate";
    config.headers["Pragma"] = "no-cache";
    config.headers["Expires"] = "0";
  }

  return config;
}, undefined, { synchronous: true });

// Add response interceptor for handling auth errors
api.interceptors.response.use(
  (response) => {
    return response;
  },
  async (error) => {
    // Handle canceled requests silently - don't show error toasts for user-initiated cancellations
    // This happens on page navigation, reload, or manual query cancellation
    if (error.code === 'ERR_CANCELED' || error.name === 'CanceledError' || error.message === 'canceled') {
      // Return a special error that won't trigger error toasts
      return Promise.reject({
        status: "error",
        message: "Request canceled",
        error_type: "CanceledError",
        _silent: true, // Flag to suppress toast notifications
      });
    }

    // Create a standardized error response
    const errorResponse = error.response?.data || {
      status: "error",
      message: error.message || "Network error: Unable to connect to the server",
      error_type: "NetworkError",
    };

    if (error.response?.status === 401) {
      const isInitialSessionCheck = error.config?.url === "/me" &&
        !sessionStorage.getItem("hadPreviousSession");
      const expiration = handleUnauthorized({ toast: !isInitialSessionCheck });
      if (expiration) {
        const expired = await expiration;
        if (error.config?.signal?.aborted) throw new DOMException("Request canceled", "AbortError");
        return Promise.reject(expired);
      }
    }

    // Handle CSRF token errors (must be checked BEFORE generic 403 handler)
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
        _toastShown: true,
      };
      showErrorToast(csrfError);
      return Promise.reject(csrfError);
    }

    // Handle forbidden errors
    if (error.response?.status === 403) {
      // Handle demo instance specific errors
      if (error.response?.data?.error_type === "DEMO_INSTANCE") {
        const demoError = {
          status: "error",
          message: error.response.data.message || "This public demo is read-only. Explore dashboards and logs freely; self-host LogChef to create or change resources.",
          error_type: "DemoModeError",
          _toastShown: true,
        };
        showErrorToast(demoError);
        return Promise.reject(demoError);
      }

      // Callers can opt out of the toast (e.g. dashboard panels render an inline
      // locked state per panel and must not spawn one toast per forbidden source).
      const options: RequestOptions | undefined = error.config;
      const showToast = !options?.suppressErrorToast;
      // Create a custom error object with the specific message and error type
      const forbiddenError = {
        status: "error",
        message: "You don't have permission to access this resource.",
        error_type: "AuthorizationError",
        _toastShown: showToast,
      };
      if (showToast) {
        showErrorToast(forbiddenError);
      }

      // Only a navigation/page-load (GET) 403 should switch to the full-page
      // Forbidden view. Inline mutations (PUT/POST/PATCH/DELETE) are recoverable
      // user actions — e.g. updating a saved query you don't own — and must not
      // hijack the whole page; the toast is enough. Page-level access is enforced
      // independently by the router navigation guard (meta.requiresAdmin etc).
      const method = (error.config?.method || "get").toLowerCase();
      const isMutation = ["post", "put", "patch", "delete"].includes(method);
      if (!isMutation && !options?.suppressForbiddenRedirect) {
        forbiddenHandler?.();
      }
      return Promise.reject(forbiddenError);
    }

    // For other errors, we'll let the calling code handle them
    // but we standardize the error format
    return Promise.reject(errorResponse);
  }
);
