import type { APIResponse } from './types'
import { api } from './config'

export interface Source {
  id: string
  table_name: string
  schema_type: string
  dsn: string
  description?: string
  ttl_days: number
  created_at: string
  updated_at: string
  is_connected: boolean
}

export interface CreateSourcePayload {
  table_name: string
  schema_type: string
  dsn: string
  description?: string
  ttl_days: number
}

export const sourcesApi = {
  async listSources() {
    return api.get<APIResponse<Source[]>>('/sources')
  },

  async getSource(id: string) {
    return api.get<APIResponse<Source>>(`/sources/${id}`)
  },

  async createSource(payload: CreateSourcePayload) {
    return api.post<APIResponse<Source>>('/sources', payload)
  },

  async deleteSource(id: string) {
    return api.delete<APIResponse<{ message: string }>>(`/sources/${id}`)
  },
}
