import type { APIResponse } from './types'
import { api } from './config'

// Generic log interface that can handle any fields
export interface Log {
  [key: string]: string | number | boolean | null | undefined
}

// Schema information for each log field
export interface LogField {
  name: string
  type: string
  description?: string
}

export interface QueryLogsParams {
  source: string
  limit?: number
  offset?: number
}

export type QueryLogsResponse = APIResponse<{
  logs: Log[]
  params: QueryLogsParams
}>

export const logsApi = {
  async queryLogs(params: QueryLogsParams): Promise<QueryLogsResponse> {
    const { source, limit = 100, offset = 0 } = params
    const response = await api.get<QueryLogsResponse>('/query/logs', {
      params: {
        source,
        limit,
        offset
      }
    })
    return response.data
  },

  async exploreSource(source: string): Promise<APIResponse<LogField[]>> {
    const response = await api.get<APIResponse<LogField[]>>('/query/explore', {
      params: { source }
    })
    return response.data
  }
}
