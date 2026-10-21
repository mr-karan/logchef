import { describe, it, expect, vi, beforeEach } from 'vitest'
import { toast } from 'vue-sonner'
import { useBaseStore } from '../base'

vi.mock('vue-sonner', () => ({
  toast: Object.assign(vi.fn(), { error: vi.fn(), success: vi.fn() }),
}))

const ok = <T>(data: T) => ({ status: 'success' as const, data })
const fail = (message: string) => ({ status: 'error' as const, message, error_type: 'ValidationError' })

function deferred<T>() {
  let resolve!: (value: T) => void
  const promise = new Promise<T>((r) => { resolve = r })
  return { promise, resolve }
}

describe('useBaseStore', () => {
  beforeEach(() => {
    vi.clearAllMocks()
    vi.spyOn(console, 'error').mockImplementation(() => {})
  })

  it('shows the success message once after a successful call', async () => {
    const store = useBaseStore({})

    const result = await store.callApi({ apiCall: async () => ok({ id: 1 }), successMessage: 'User created' })

    expect(result.success).toBe(true)
    expect(toast).toHaveBeenCalledTimes(1)
    expect(toast).toHaveBeenCalledWith('User created', expect.anything())
    expect(toast.error).not.toHaveBeenCalled()
  })

  it('shows no success message when toasts are off', async () => {
    const store = useBaseStore({})

    await store.callApi({ apiCall: async () => ok({ id: 1 }), successMessage: 'User created', showToast: false })

    expect(toast).not.toHaveBeenCalled()
  })

  it('shows no success message when the call or its follow-up fails', async () => {
    const store = useBaseStore({})

    await store.callApi({ apiCall: async () => fail('email already exists'), successMessage: 'User created' })
    await store.callApi({
      apiCall: async () => ok({ id: 1 }),
      successMessage: 'User created',
      onSuccess: async () => { throw fail('reload failed') },
    })

    expect(toast).not.toHaveBeenCalled()
    expect(toast.error).toHaveBeenCalledTimes(2)
  })

  it('clears a previous error when a later call succeeds', async () => {
    const store = useBaseStore({})

    await store.callApi({ apiCall: async () => fail('email already exists') })
    expect(store.error.value?.message).toBe('email already exists')

    const result = await store.callApi({ apiCall: async () => ok({ id: 1 }) })
    expect(result.success).toBe(true)
    expect(store.error.value).toBeNull()
  })

  it('clears a previous thrown error when a later call succeeds', async () => {
    const store = useBaseStore({})

    await store.callApi({ apiCall: () => Promise.reject(fail('boom')) })
    expect(store.error.value?.message).toBe('boom')

    await store.callApi({ apiCall: async () => ok(null) })
    expect(store.error.value).toBeNull()
  })

  it('waits for an async onSuccess before resolving', async () => {
    const store = useBaseStore({})
    const followUp = deferred<void>()
    let followUpDone = false

    const call = store.callApi({
      apiCall: async () => ok({ id: 1 }),
      onSuccess: async () => {
        await followUp.promise
        followUpDone = true
      },
    })

    let settled = false
    void call.then(() => { settled = true })
    await new Promise((r) => setTimeout(r, 0))
    expect(settled).toBe(false)

    followUp.resolve()
    await call
    expect(followUpDone).toBe(true)
  })

  it('reports a throwing async onSuccess as a failed call', async () => {
    const store = useBaseStore({})

    const result = await store.callApi({
      apiCall: async () => ok({ id: 1 }),
      onSuccess: async () => { throw fail('follow-up failed') },
    })

    expect(result.success).toBe(false)
    expect(store.error.value?.message).toBe('follow-up failed')
  })

  it('stays loading until every overlapping call on a key finishes', async () => {
    const store = useBaseStore({})
    const first = deferred<void>()
    const second = deferred<void>()

    const a = store.withLoading('save', () => first.promise)
    const b = store.withLoading('save', () => second.promise)
    expect(store.isLoadingOperation('save')).toBe(true)

    first.resolve()
    await a
    expect(store.isLoadingOperation('save')).toBe(true)
    expect(store.isLoading.value).toBe(true)

    second.resolve()
    await b
    expect(store.isLoadingOperation('save')).toBe(false)
    expect(store.isLoading.value).toBe(false)
  })

  it('stays loading after a nested callApi on the same key returns', async () => {
    const store = useBaseStore({})
    const reload = deferred<void>()
    let loadingDuringReload: boolean | undefined

    const op = store.withLoading('createUser', async () => {
      await store.callApi({ apiCall: async () => ok({ id: 1 }), operationKey: 'createUser' })
      loadingDuringReload = store.isLoadingOperation('createUser')
      await reload.promise
    })

    await new Promise((r) => setTimeout(r, 0))
    expect(loadingDuringReload).toBe(true)

    reload.resolve()
    await op
    expect(store.isLoadingOperation('createUser')).toBe(false)
  })
})
