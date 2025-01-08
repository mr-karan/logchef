import axios from 'axios'

// Create base axios instance with common configuration
export const api = axios.create({
  baseURL: '/api',
  timeout: 10000,
})
