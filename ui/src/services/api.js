const BASE_URL = '/api'

export const api = {
  async fetchLogs(sourceId, params = {}) {
    const response = await fetch(`${BASE_URL}/logs/${sourceId}`, {
      method: 'GET',
      headers: {
        'Content-Type': 'application/json'
      }
    })
    if (!response.ok) {
      throw new Error('Failed to fetch logs')
    }
    const responseData = await response.json()
    return responseData.data || []
  },

  async fetchSources() {
    const response = await fetch(`${BASE_URL}/sources`)
    if (!response.ok) {
      throw new Error('Failed to fetch sources')
    }
    const responseData = await response.json()
    return responseData.data || { logs: [] }
  },

  async deleteSource(sourceId) {
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

  async updateSourceTTL(sourceId, ttlDays) {
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
  }
}
