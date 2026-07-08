import { api } from "./config";
import type { APIResponse } from "./types";

/**
 * Generic API request function to reduce repetitive code
 */
export async function apiRequest<T>(
  method: "get" | "post" | "put" | "patch" | "delete",
  url: string,
  data?: any,
  options?: { timeout?: number; signal?: AbortSignal; suppressErrorToast?: boolean }
): Promise<APIResponse<T>> {
  const config = {
    ...(options?.timeout ? { timeout: options.timeout * 1000 } : {}), // Convert seconds to milliseconds
    ...(options?.signal ? { signal: options.signal } : {}),
    // Custom flag read back off `error.config` by the response interceptor to
    // suppress the global error toast (e.g. per-panel 403s on a dashboard, where
    // an inline locked/error state is shown instead of a toast storm).
    ...(options?.suppressErrorToast ? { suppressErrorToast: true } : {})
  };
  
  let response;
  if (method === "get" || method === "delete") {
    response = await api[method]<APIResponse<T>>(url, config);
  } else {
    // post, put, patch all send data in body
    response = await api[method]<APIResponse<T>>(url, data, config);
  }
  return response.data;
}

/**
 * Shorthand methods for common API operations
 */
type RequestOptions = { timeout?: number; signal?: AbortSignal; suppressErrorToast?: boolean };

export const apiClient = {
  get: <T>(url: string, options?: RequestOptions) => apiRequest<T>("get", url, undefined, options),
  post: <T>(url: string, data?: any, options?: RequestOptions) => apiRequest<T>("post", url, data, options),
  put: <T>(url: string, data?: any, options?: RequestOptions) => apiRequest<T>("put", url, data, options),
  patch: <T>(url: string, data?: any, options?: RequestOptions) => apiRequest<T>("patch", url, data, options),
  delete: <T>(url: string, options?: RequestOptions) => apiRequest<T>("delete", url, undefined, options)
};
