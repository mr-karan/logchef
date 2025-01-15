import { type APIResponse, isErrorResponse } from './types'
import { api } from './config'

export interface Column {
    name: string
    type: string
}

export const exploreApi = {
    async exploreSource(sourceId: string): Promise<Column[]> {
        const { data } = await api.get<APIResponse<Column[]>>(`/query/explore?source=${sourceId}`)
        
        if (isErrorResponse(data)) {
            throw new Error(data.data.error)
        }
        
        return data.data as Column[]
    }
}
