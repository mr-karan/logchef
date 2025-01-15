import type { APIResponse } from './types'
import { api } from './config'

export interface Log {
  _timestamp: string
  msg: string
  _hostname?: string
  group_name?: string
  job_name?: string
  namespace?: string
  node_name?: string
  task_name?: string
  [key: string]: any
}

export interface QueryLogsParams {
  source: string
  limit?: number
  offset?: number
}

export type QueryLogsResponse = APIResponse<Log[]>

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
  }
}
