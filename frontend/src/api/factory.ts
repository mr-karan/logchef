import { api } from "./config";
import type { APIResponse } from "./types";

export function createApiClient<T>(basePath: string) {
  return {
    list: (params?: Record<string, any>) => 
      api.get<APIResponse<T[]>>(basePath, { params }),
    
    get: (id: string | number, params?: Record<string, any>) => 
      api.get<APIResponse<T>>(`${basePath}/${id}`, { params }),
    
    create: (data: any) => 
      api.post<APIResponse<T>>(basePath, data),
    
    update: (id: string | number, data: any) => 
      api.put<APIResponse<T>>(`${basePath}/${id}`, data),
    
    delete: (id: string | number) => 
      api.delete<APIResponse<{ message: string }>>(`${basePath}/${id}`),
    
    // Custom endpoint method
    custom: (path: string, method: 'get' | 'post' | 'put' | 'delete' = 'get', data?: any, params?: Record<string, any>) => {
      const fullPath = path.startsWith('/') ? path : `${basePath}/${path}`;
      
      switch (method) {
        case 'get':
          return api.get<APIResponse<any>>(fullPath, { params });
        case 'post':
          return api.post<APIResponse<any>>(fullPath, data, { params });
        case 'put':
          return api.put<APIResponse<any>>(fullPath, data, { params });
        case 'delete':
          return api.delete<APIResponse<any>>(fullPath, { params });
      }
    }
  };
}
