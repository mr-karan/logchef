import { describe, it, expect, vi, beforeEach, afterEach } from 'vitest'
import { AxiosError, AxiosHeaders, type AxiosAdapter, type InternalAxiosRequestConfig } from 'axios'
import { toast } from 'vue-sonner'
import { api, setUnauthorizedHandler, setForbiddenHandler } from '../config'
import { createSessionExpirationHandler, expireSession } from '../session'
import { useAuthStore } from '@/stores/auth'
import { apiClient } from '../apiUtils'
import { useBaseStore } from '@/stores/base'
import { hasShownErrorToast } from '@/api/error-handler'
import router from '@/router'

vi.mock('vue-sonner', () => ({
  toast: Object.assign(vi.fn(), { error: vi.fn(), success: vi.fn() }),
}))

vi.mock('@/router', () => ({
  default: {
    push: vi.fn().mockResolvedValue(undefined),
    currentRoute: { value: { name: 'Home' } },
    options: { history: { location: '/logs/explore?query=error#row-3' } },
  },
}))

vi.mock('@/stores/auth', () => ({
  useAuthStore: () => ({ clearState: vi.fn() }),
}))

setUnauthorizedHandler(createSessionExpirationHandler({ clearState: useAuthStore().clearState, router }))
setForbiddenHandler(() => { void router.push({ name: 'Forbidden' }) })

function respondWith(status: number, data: unknown): AxiosAdapter {
  return async (config: InternalAxiosRequestConfig) => {
    throw new AxiosError('Request failed', 'ERR_BAD_REQUEST', config, null, {
      status,
      statusText: '',
      data,
      headers: {},
      config: { ...config, headers: new AxiosHeaders() },
    })
  }
}

describe('api error toasts', () => {
  const originalAdapter = api.defaults.adapter

  beforeEach(() => {
    vi.clearAllMocks()
    vi.spyOn(console, 'error').mockImplementation(() => {})
    sessionStorage.setItem('hadPreviousSession', 'true')
  })

  afterEach(() => {
    api.defaults.adapter = originalAdapter
  })

  it.each([
    ['forbidden', 403, { status: 'error', message: 'forbidden', error_type: 'AuthorizationError' }],
    ['csrf mismatch', 403, { code: 'CSRF_TOKEN_MISMATCH' }],
    ['expired session', 401, { status: 'error', message: 'unauthorized', error_type: 'AuthenticationError' }],
  ])('shows exactly one toast for a %s response through a store call', async (_name, status, body) => {
    api.defaults.adapter = respondWith(status, body)
    const store = useBaseStore({})

    const result = await store.callApi({ apiCall: () => apiClient.post('/teams/1/members', {}) })

    expect(result.success).toBe(false)
    expect(toast.error).toHaveBeenCalledTimes(1)
  })

  it('rejects a session error object when a proxy returns a non-JSON 401 body', async () => {
    api.defaults.adapter = respondWith(401, '<html>401 Authorization Required</html>')

    const rejection = await api.get('/teams').catch((err: unknown) => err)

    expect(rejection).toEqual({
      status: 'error',
      message: 'Your session has expired. Please log in again.',
      error_type: 'AuthenticationError',
      _toastShown: true,
    })
    expect(toast.error).toHaveBeenCalledTimes(1)
  })

  it('rejects without a shown toast on the initial session check', async () => {
    sessionStorage.removeItem('hadPreviousSession')
    api.defaults.adapter = respondWith(401, { status: 'error', message: 'unauthorized', error_type: 'AuthenticationError' })

    const rejection = await api.get('/me').catch((err: unknown) => err)

    expect(rejection).toMatchObject({ status: 'error', error_type: 'AuthenticationError' })
    expect(hasShownErrorToast(rejection)).toBe(false)
    expect(toast.error).not.toHaveBeenCalled()
  })
})

describe('session expiration ownership', () => {
  const originalAdapter = api.defaults.adapter
  afterEach(() => { api.defaults.adapter = originalAdapter })

  it('shows one toast and starts one login navigation for parallel 401 responses', async () => {
    let finishNavigation: (() => void) | undefined
    const navigation = new Promise<void>(resolve => { finishNavigation = resolve })
    vi.mocked(router.push).mockImplementationOnce(() => navigation)
    vi.clearAllMocks()
    sessionStorage.setItem('hadPreviousSession', 'true')
    api.defaults.adapter = respondWith(401, { status: 'error', message: 'unauthorized' })
    const requests = Array.from({ length: 3 }, () => api.get('/teams').catch((error: unknown) => error))
    await vi.waitFor(() => expect(router.push).toHaveBeenCalled())
    try {
      expireSession({ toast: true })
      expect(router.push).toHaveBeenCalledExactlyOnceWith({ name: 'Login', query: { redirect: '/logs/explore?query=error#row-3' }, replace: true })
      expect(toast.error).toHaveBeenCalledTimes(1)
    } finally { finishNavigation?.() }
    const rejections = await Promise.all(requests)
    expect(rejections.every(hasShownErrorToast)).toBe(true)
    expect(router.push).toHaveBeenCalledTimes(1)
  })

  it('allows another expiration after the previous login navigation completes', async () => {
    vi.clearAllMocks()
    api.defaults.adapter = respondWith(401, { status: 'error', message: 'unauthorized' })
    await api.get('/teams').catch(() => {})
    await api.get('/teams').catch(() => {})
    expect(router.push).toHaveBeenCalledTimes(2)
    expect(toast.error).toHaveBeenCalledTimes(2)
  })

  it('does not toast or navigate again for a late 401 on the login page', async () => {
    router.currentRoute.value.name = 'Login'
    vi.clearAllMocks()
    api.defaults.adapter = respondWith(401, { status: 'error', message: 'unauthorized' })
    try {
      const rejection = await api.get('/teams').catch((error: unknown) => error)
      expect(hasShownErrorToast(rejection)).toBe(true)
      expect(router.push).not.toHaveBeenCalled()
      expect(toast.error).not.toHaveBeenCalled()
    } finally { router.currentRoute.value.name = 'Home' }
  })
})
