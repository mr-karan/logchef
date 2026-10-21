import { describe, expect, it, vi } from 'vitest'

vi.mock('@/stores/auth', () => { throw new Error('API config imported the auth store') })
vi.mock('@/router', () => { throw new Error('API config imported the router') })

describe('API dependency direction', () => {
  it('reports a missing unauthorized handler once in development', async () => {
    vi.resetModules()
    const { api } = await import('../config')
    const log = vi.spyOn(console, 'error').mockImplementation(() => {})
    api.defaults.adapter = async () => { throw { response: { status: 401 }, config: { url: '/teams' } } }
    try {
      await api.get('/teams').catch(() => {})
      await api.get('/teams').catch(() => {})
      expect(log).toHaveBeenCalledExactlyOnceWith('No session expiration handler registered')
    } finally { log.mockRestore() }
  })

  it('dispatches the SSE-compatible void session API to the registered handler', async () => {
    const { setUnauthorizedHandler } = await import('../config')
    const { expireSession } = await import('../session')
    const handler = vi.fn().mockResolvedValue({ status: 'error', message: 'expired' })
    setUnauthorizedHandler(handler)
    const result = expireSession({ toast: true })
    expect(result).toBeUndefined()
    expect(handler).toHaveBeenCalledExactlyOnceWith({ toast: true })
  })

  it('imports and dispatches a 401 without loading the auth store or router', async () => {
    const { api, setUnauthorizedHandler } = await import('../config')
    const sessionError = { status: 'error', message: 'expired', error_type: 'AuthenticationError', _toastShown: true }
    const handler = vi.fn().mockResolvedValue(sessionError)
    setUnauthorizedHandler(handler)
    const originalAdapter = api.defaults.adapter
    api.defaults.adapter = async () => { throw { response: { status: 401 }, config: { url: '/teams' } } }
    try {
      await expect(api.get('/teams')).rejects.toEqual(sessionError)
      expect(handler).toHaveBeenCalledWith({ toast: true })
    } finally {
      api.defaults.adapter = originalAdapter
    }
  })
})
