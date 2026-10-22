import { api } from '@/api/config'
import type { AxiosResponse, InternalAxiosRequestConfig } from 'axios'

export function controlledApi(matches: (config: InternalAxiosRequestConfig) => boolean = () => true) {
  const requests: {
    config: InternalAxiosRequestConfig;
    succeed: (data: unknown) => void;
    fail: (message: string) => void;
  }[] = []
  const originalAdapter = api.defaults.adapter
  api.defaults.adapter = (config) => {
    const response = (data: unknown): AxiosResponse => ({
      config, data: { status: 'success', data }, status: 200, statusText: 'OK', headers: {},
    })
    if (!matches(config)) return Promise.resolve(response([]))
    return new Promise((resolve, reject) => {
      requests.push({
        config,
        succeed: data => resolve(response(data)),
        fail: message => reject(new Error(message)),
      })
    })
  }
  return { requests, restore: () => { api.defaults.adapter = originalAdapter } }
}
