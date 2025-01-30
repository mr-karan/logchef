import type { Source, CreateSourcePayload } from './types'
import apiClient from './client'

interface APIResponse<T> {
  status: string
  data: T
}

interface SourcesResponse {
  sources: Source[]
}

interface SourceResponse {
  source: Source
}

const api = {
  async listSources(): Promise<Source[]> {
    try {
      const response = await apiClient.get('/sources')
      if (response?.data?.data?.sources) {
        return response.data.data.sources
      }

      console.error('Unexpected API response structure:', response.data)
      return []
    } catch (error) {
      console.error('API Error:', error)
      throw error
    }
  },

  async getSource(id: string): Promise<Source> {
    try {
      const { data } = await apiClient.get<APIResponse<SourceResponse>>(`/sources/${id}`)
      return data.data.source
    } catch (error) {
      console.error('API Error:', error)
      throw error
    }
  },

  async createSource(payload: CreateSourcePayload): Promise<Source> {
    try {
      const { data } = await apiClient.post<APIResponse<SourceResponse>>('/sources', payload)
      return data.data.source
    } catch (error) {
      console.error('API Error:', error)
      throw error
    }
  },

  async deleteSource(id: string): Promise<void> {
    try {
      await apiClient.delete(`/sources/${id}`)
    } catch (error) {
      console.error('API Error:', error)
      throw error
    }
  },
}

export default api
