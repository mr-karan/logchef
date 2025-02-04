import { api } from './config'
import type { APIResponse } from './types'

export interface QueryParams {
    query?: string
    filter_groups?: any
    limit: number
    start_timestamp: number
    end_timestamp: number
    sort?: any
}

export interface QueryStats {
    execution_time_ms: number
}

export interface QueryResponse {
    logs: Record<string, any>[]
    stats: QueryStats
    params: QueryParams & {
        source_id: string
    }
}

export const exploreApi = {
    async getLogs(sourceId: string, params: QueryParams) {
        try {
            const response = await api.post<APIResponse<QueryResponse>>(`/sources/${sourceId}/logs/search`, params)
            return response.data
        } catch (error) {
            console.error('Error fetching logs:', error)
            throw error
        }
    }
}
