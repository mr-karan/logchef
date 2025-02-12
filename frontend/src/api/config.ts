import axios from "axios";

// Create base axios instance with common configuration
export const api = axios.create({
  baseURL: `${import.meta.env.VITE_API_URL}/api/v1`,
  headers: {
    "Content-Type": "application/json",
  },
  withCredentials: true, // Required for cookies
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
      // Clear local auth state
      window.location.href = "/";
      return Promise.reject(error);
    }

    // Handle CSRF token errors
    if (error.response?.status === 403) {
      // Could be CSRF token mismatch
      console.error("Security error:", error);
      return Promise.reject(new Error("Security verification failed"));
    }

    return Promise.reject(error);
  }
);
