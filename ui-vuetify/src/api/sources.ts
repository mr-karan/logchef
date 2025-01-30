import axios, { AxiosError } from 'axios'

interface APIErrorResponse {
  status: string
  data: {
    error: string
  }
}

function getErrorMessage(error: unknown): string {
  if (error instanceof AxiosError && error.response?.data) {
    const response = error.response.data as APIErrorResponse
    if (response.status === 'error' && response.data.error) {
      return response.data.error
    }
  }
  return 'An unexpected error occurred'
}

export interface Source {
  id: string
  schema_type: 'managed' | 'unmanaged'
  connection: ConnectionInfo
  description?: string
  ttl_days: number
  created_at: string
  updated_at: string
  is_connected: boolean
  schema?: string
  columns?: Array<{ name: string; type: string }>
}

export interface ConnectionInfo {
  host: string  // Now expects format "host:port"
  username: string
  password: string
  database: string
  table_name: string
}

export interface CreateSourcePayload {
  schema_type: string
  connection: ConnectionInfo
  description?: string
  ttl_days: number
}

const api = {
  async listSources(): Promise<Source[]> {
    try {
      const response = await axios.get('/api/v1/sources')
      return response.data.data.sources
    } catch (error) {
      throw new Error(getErrorMessage(error))
    }
  },

  async getSource(id: string): Promise<Source> {
    try {
      const response = await axios.get(`/api/v1/sources/${id}`)
      return response.data.data.source
    } catch (error) {
      throw new Error(getErrorMessage(error))
    }
  },

  async createSource(payload: CreateSourcePayload): Promise<Source> {
    try {
      const response = await axios.post('/api/v1/sources', payload)
      return response.data.data.source
    } catch (error) {
      throw new Error(getErrorMessage(error))
    }
  },

  async deleteSource(id: string): Promise<void> {
    try {
      await axios.delete(`/api/v1/sources/${id}`)
    } catch (error) {
      throw new Error(getErrorMessage(error))
    }
  }
}

export default api
