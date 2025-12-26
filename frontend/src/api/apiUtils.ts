import { api } from "./config";
import type { APIResponse } from "./types";

/**
 * Generic API request function to reduce repetitive code
 */
export async function apiRequest<T>(
  method: "get" | "post" | "put" | "patch" | "delete",
  url: string,
  data?: any,
  options?: { timeout?: number; signal?: AbortSignal }
): Promise<APIResponse<T>> {
  const config = {
    ...(options?.timeout ? { timeout: options.timeout * 1000 } : {}), // Convert seconds to milliseconds
    ...(options?.signal ? { signal: options.signal } : {})
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
export const apiClient = {
  get: <T>(url: string, options?: { timeout?: number; signal?: AbortSignal }) => apiRequest<T>("get", url, undefined, options),
  post: <T>(url: string, data?: any, options?: { timeout?: number; signal?: AbortSignal }) => apiRequest<T>("post", url, data, options),
  put: <T>(url: string, data?: any, options?: { timeout?: number; signal?: AbortSignal }) => apiRequest<T>("put", url, data, options),
  patch: <T>(url: string, data?: any, options?: { timeout?: number; signal?: AbortSignal }) => apiRequest<T>("patch", url, data, options),
  delete: <T>(url: string, options?: { timeout?: number; signal?: AbortSignal }) => apiRequest<T>("delete", url, undefined, options)
};
