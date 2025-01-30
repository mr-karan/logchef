import apiClient from './client'
import type { LogResponse, LogQueryParams } from './types'

interface APIResponse<T> {
  status: string
  data: T
}

const api = {
  async getLogs(sourceId: string, params: LogQueryParams): Promise<LogResponse> {
    try {
      const { data } = await apiClient.post<APIResponse<LogResponse>>(`/sources/${sourceId}/logs`, {
        start_timestamp: params.start_timestamp,
        end_timestamp: params.end_timestamp,
        limit: params.limit
      })
      return data.data
    } catch (error) {
      console.error('API Error:', error)
      throw error
    }
  }
}

export default api
