import axios from 'axios'

// Create base axios instance with common configuration
export const api = axios.create({
  baseURL: '/api/v1',
  headers: {
    'Content-Type': 'application/json',
  },
})
