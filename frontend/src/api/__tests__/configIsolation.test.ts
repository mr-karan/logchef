import { describe, expect, it, vi } from 'vitest'

vi.mock('@/stores/auth', () => { throw new Error('API config imported the auth store') })
vi.mock('@/router', () => { throw new Error('API config imported the router') })

describe('API dependency direction', () => {
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
