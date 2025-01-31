import apiClient from './client'
import type { LogResponse, LogQueryParams } from './types'

const api = {
  async getLogs(sourceId: string, params: LogQueryParams) {
    try {
      const response = await apiClient.post<LogResponse>(`/sources/${sourceId}/logs`, {
        start_timestamp: params.start_timestamp,
        end_timestamp: params.end_timestamp,
        limit: params.limit
      })

      return response.data // Return the full response data
    } catch (error) {
      console.error('API Error:', error)
      throw error
    }
  }
}

export default api
