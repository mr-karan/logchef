import { api } from './config'

export interface Source {
  ID: string
  Name: string
  TableName: string
  SchemaType: string
  DSN: string
  Description?: string
  CreatedAt: string
  UpdatedAt: string
}

export interface CreateSourcePayload {
  name: string
  schema_type: 'ncsa' | 'otel' | 'custom'
  dsn: string
  ttl_days: number
  description?: string
}

export const sourcesApi = {
  async createSource(payload: CreateSourcePayload) {
    const response = await api.post('/sources', payload)
    return response.data
  },

  async getSources() {
    const response = await api.get<{ status: string, data: Source[] }>('/sources')
    return response.data
  },

  async deleteSource(id: string) {
    const response = await api.delete(`/sources/${id}`)
    return response.data
  }
}
