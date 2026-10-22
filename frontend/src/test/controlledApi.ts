import { api } from '@/api/config'
import { AxiosError, CanceledError, type AxiosResponse, type InternalAxiosRequestConfig } from 'axios'

export function controlledApi(matches: (config: InternalAxiosRequestConfig) => boolean = () => true) {
  const requests: {
    config: InternalAxiosRequestConfig;
    succeed: (data: unknown) => void;
    fail: (message: string) => void;
    respond: (status: number, data: unknown) => void;
  }[] = []
  const originalAdapter = api.defaults.adapter
  api.defaults.adapter = (config) => {
    const response = (data: unknown): AxiosResponse => ({
      config, data: { status: 'success', data }, status: 200, statusText: 'OK', headers: {},
    })
    if (!matches(config)) return Promise.resolve(response([]))
    return new Promise((resolve, reject) => {
      config.signal?.addEventListener?.('abort', () => reject(new CanceledError(undefined, undefined, config)))
      requests.push({
        config,
        succeed: data => resolve(response(data)),
        fail: message => reject(new Error(message)),
        respond: (status, data) => reject(new AxiosError(`Request failed with status code ${status}`, undefined, config, undefined, {
          config, data, status, statusText: '', headers: {},
        })),
      })
    })
  }
  return { requests, restore: () => { api.defaults.adapter = originalAdapter } }
}
