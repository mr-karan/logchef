import { api } from "./config";
import type { APIResponse } from "./types";

/**
 * Generic API request function to reduce repetitive code
 */
export async function apiRequest<T>(
  method: "get" | "post" | "put" | "delete", 
  url: string, 
  data?: any
): Promise<APIResponse<T>> {
  const response = await api[method]<APIResponse<T>>(url, data);
  return response.data;
}

/**
 * Shorthand methods for common API operations
 */
export const apiClient = {
  get: <T>(url: string) => apiRequest<T>("get", url),
  post: <T>(url: string, data?: any) => apiRequest<T>("post", url, data),
  put: <T>(url: string, data?: any) => apiRequest<T>("put", url, data),
  delete: <T>(url: string) => apiRequest<T>("delete", url)
};
