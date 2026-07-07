import { describe, it, expect, beforeEach, vi } from 'vitest'
import { setActivePinia, createPinia } from 'pinia'
import { useMetaStore } from '../meta'

// Mock the meta API so we can drive the store from different server responses.
vi.mock('@/api/meta', () => ({
  metaApi: {
    getMeta: vi.fn(),
  },
}))

// Silence the toast side-effect via useToast when errors happen.
vi.mock('@/composables/useToast', () => ({
  useToast: () => ({ toast: vi.fn() }),
}))

import { metaApi } from '@/api/meta'

// apiClient wraps every response in { status, data } — useApiQuery keys off
// `status === "success"`. Build fake responses in that shape so the store's
// onSuccess handler runs end-to-end.
const okResponse = (overrides: Record<string, unknown> = {}) => ({
  status: 'success' as const,
  data: {
    version: '0.0.0-test',
    http_server_timeout: '30s',
    max_query_limit: 100000,
    max_query_timeout_seconds: 120,
    default_preview_limit: 1000,
    max_preview_limit: 100000,
    max_export_rows: 1000000,
    ...overrides,
  },
})

describe('useMetaStore alertsEnabled capability', () => {
  beforeEach(() => {
    setActivePinia(createPinia())
    vi.clearAllMocks()
  })

  it('defaults alertsEnabled to true before loadMeta runs', () => {
    const store = useMetaStore()
    expect(store.alertsEnabled).toBe(true)
  })

  it('defaults alertsEnabled to true when the field is absent from the API response (older server)', async () => {
    ;(metaApi.getMeta as any).mockResolvedValueOnce(okResponse())

    const store = useMetaStore()
    await store.loadMeta()

    expect(store.isInitialized).toBe(true)
    expect(store.alertsEnabled).toBe(true)
  })

  it('reflects alerts_enabled=false when the API says so', async () => {
    ;(metaApi.getMeta as any).mockResolvedValueOnce(okResponse({ alerts_enabled: false }))

    const store = useMetaStore()
    await store.loadMeta()

    expect(store.isInitialized).toBe(true)
    expect(store.alertsEnabled).toBe(false)
  })

  it('reflects alerts_enabled=true when the API explicitly says so', async () => {
    ;(metaApi.getMeta as any).mockResolvedValueOnce(okResponse({ alerts_enabled: true }))

    const store = useMetaStore()
    await store.loadMeta()

    expect(store.alertsEnabled).toBe(true)
  })
})
