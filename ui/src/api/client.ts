import axios from 'axios'
import type { AxiosError, AxiosInstance, AxiosResponse } from 'axios'
import { createDiscreteApi } from 'naive-ui'

const { notification } = createDiscreteApi(['notification'])

const apiClient: AxiosInstance = axios.create({
  baseURL: '/api/v1',
  headers: {
    'Content-Type': 'application/json',
  },
})

apiClient.interceptors.response.use(
  (response: AxiosResponse) => response,
  (error: AxiosError) => {
    const response = error.response?.data
    
    // Handle API error responses
    if (response && response.status === 'error') {
      notification.error({
        title: 'API Error',
        content: response.data?.error || 'An unexpected error occurred',
        duration: 5000
      })
    } else {
      // Handle network/other errors
      notification.error({
        title: 'Error',
        content: error.message || 'An unexpected error occurred',
        duration: 5000
      })
    }
    
    return Promise.reject(error)
  },
)

export default apiClient
