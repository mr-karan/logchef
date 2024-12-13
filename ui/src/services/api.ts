interface LogQueryParams {
    offset: number
    limit: number
    start_time: string
    end_time: string
    search_query?: string
    severity_text?: string
}

interface SchemaParams {
    start_time?: string
    end_time?: string
}

const BASE_URL = '/api'

// Add utility function for chunking arrays
function chunk<T>(arr: T[], size: number): T[][] {
    return Array.from({ length: Math.ceil(arr.length / size) }, (_, i) =>
        arr.slice(i * size, i * size + size)
    )
}

export const api = {
    async getLogs(sourceId: string, params: LogQueryParams, signal?: AbortSignal) {
        const queryParams = new URLSearchParams()

        // Required params
        queryParams.append('limit', params.limit.toString())
        queryParams.append('offset', params.offset.toString())
        queryParams.append('start_time', params.start_time)
        queryParams.append('end_time', params.end_time)

        // Optional params
        if (params.search_query) {
            queryParams.append('search', params.search_query)
        }
        if (params.severity_text) {
            queryParams.append('severity', params.severity_text)
        }

        // console.log('API request URL:', `${BASE_URL}/logs/${sourceId}?${queryParams.toString()}`)

        const response = await fetch(`${BASE_URL}/logs/${sourceId}?${queryParams.toString()}`, {
            method: 'GET',
            headers: {
                'Content-Type': 'application/json'
            },
            signal
        })

        if (!response.ok) {
            throw new Error('Failed to fetch logs')
        }

        const responseData = await response.json()
        return {
            ...responseData.data,
            // Add chunked property for progressive loading
            chunks: chunk(responseData.data.logs || [], 1000)
        }
    },

    async fetchSources() {
        const response = await fetch(`${BASE_URL}/sources`)
        if (!response.ok) {
            throw new Error('Failed to fetch sources')
        }
        const responseData = await response.json()
        return responseData.data
    },

    async deleteSource(sourceId: string) {
        const response = await fetch(`${BASE_URL}/sources/${sourceId}`, {
            method: 'DELETE',
            headers: {
                'Content-Type': 'application/json'
            }
        })

        const responseText = await response.text()

        if (!response.ok) {
            const errorData = responseText ? JSON.parse(responseText) : {}
            throw new Error(errorData.message || 'Failed to delete source')
        }

        return responseText
    },

    async updateSourceTTL(sourceId: string, ttlDays: number) {
        const response = await fetch(`${BASE_URL}/sources/${sourceId}`, {
            method: 'PUT',
            headers: {
                'Content-Type': 'application/json',
            },
            body: JSON.stringify({
                ttl_days: ttlDays
            })
        })

        if (!response.ok) {
            throw new Error('Failed to update TTL')
        }

        return response.json()
    },

    async getLogSchema(sourceId: string, params?: SchemaParams, signal?: AbortSignal) {
        const queryParams = new URLSearchParams()

        if (params?.start_time) {
            queryParams.append('start_time', params.start_time)
        }
        if (params?.end_time) {
            queryParams.append('end_time', params.end_time)
        }

        const response = await fetch(`${BASE_URL}/logs/${sourceId}/schema?${queryParams.toString()}`, {
            headers: {
                'Content-Type': 'application/json'
            },
            signal
        })

        if (!response.ok) {
            throw new Error('Failed to fetch schema')
        }

        const responseData = await response.json()
        return responseData.data
    }
}
