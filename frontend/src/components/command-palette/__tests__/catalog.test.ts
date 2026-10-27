import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { alertsApi } from '@/api/alerts'
import type { RequestOptions } from '@/api/apiUtils'
import { collectionsApi, type Collection } from '@/api/collections'
import { dashboardsApi, type Dashboard } from '@/api/dashboards'
import { savedQueriesApi } from '@/api/savedQueries'
import type { Source } from '@/api/sources'
import { teamsApi, type UserTeamMembership } from '@/api/teams'
import type { APIResponse } from '@/api/types'
import { createCatalog, current, LIMITS, type Catalog, type OpenOptions } from '../catalog'

const ok = <T>(data: T): APIResponse<T> => ({ status: 'success', data })
const flush = () => new Promise((resolve) => setTimeout(resolve, 5))

function membership(id: number): UserTeamMembership {
  return { id, name: `Team ${id}`, description: '', created_at: '', updated_at: '', member_count: 1, role: 'member' }
}

function apiSource(id: number): Source {
  return {
    id, name: `Source ${id}`, _meta_is_auto_created: false, source_type: 'clickhouse', query_languages: ['logchefql', 'clickhouse-sql'],
    _meta_ts_field: 'timestamp', connection: {}, ttl_days: 7, created_at: '', updated_at: '', is_connected: true,
  }
}

function dashboard(id: number): Dashboard {
  return { id, name: `Board ${id}`, description: '', panels: { version: 1, layout: [], panels: [] }, created_at: '', updated_at: '' }
}

function collection(id: number): Collection {
  return { id, name: `Shelf ${id}`, is_personal: false, created_by: 1, member_count: 1, team_count: 0, item_count: 0, created_at: '', updated_at: '' }
}

const OPTIONS: OpenOptions = { userId: 'u1', isAdmin: false, alertsEnabled: true, priorityTeams: [] }

let requests: RequestOptions[]
let catalog: Catalog

function record(options: RequestOptions | undefined) {
  if (options) requests.push(options)
}

function stub(teams: UserTeamMembership[] = [membership(1)]) {
  return {
    userTeams: vi.spyOn(teamsApi, 'listUserTeams').mockImplementation(async (options) => { record(options); return ok(teams) }),
    allTeams: vi.spyOn(teamsApi, 'listAllTeams').mockImplementation(async (options) => { record(options); return ok([]) }),
    sources: vi.spyOn(teamsApi, 'listTeamSources').mockImplementation(async (teamId, options) => { record(options); return ok([apiSource(teamId * 10)]) }),
    savedQueries: vi.spyOn(savedQueriesApi, 'list').mockImplementation(async (_source, options) => { record(options); return ok([]) }),
    dashboards: vi.spyOn(dashboardsApi, 'list').mockImplementation(async (options) => { record(options); return ok([dashboard(1)]) }),
    alerts: vi.spyOn(alertsApi, 'list').mockImplementation(async (_source, options) => { record(options); return ok([]) }),
    collections: vi.spyOn(collectionsApi, 'list').mockImplementation(async (options) => { record(options); return ok([collection(1)]) }),
  }
}

beforeEach(() => {
  vi.useFakeTimers({ toFake: ['Date'] })
  vi.setSystemTime(new Date('2026-10-27T10:00:00Z'))
  requests = []
  catalog = createCatalog()
})

afterEach(() => {
  catalog.clear()
  vi.useRealTimers()
  vi.restoreAllMocks()
})

describe('palette catalog', () => {
  it('fetches each list once per open and again only when stale', async () => {
    const api = stub()
    catalog.open(OPTIONS)
    catalog.open(OPTIONS)
    await flush()
    for (const spy of [api.userTeams, api.sources, api.savedQueries, api.dashboards, api.alerts, api.collections]) expect(spy).toHaveBeenCalledTimes(1)
    expect(current(catalog.snapshot.value.dashboards)).toEqual([{ id: 1, name: 'Board 1' }])
    expect(current(catalog.snapshot.value.sourcesByTeam.get(1) ?? { state: 'idle' })).toMatchObject([{ id: 10, teamId: 1, slug: 'source-10', connected: true, logchefql: true }])

    catalog.close()
    catalog.open(OPTIONS)
    await flush()
    expect(api.dashboards).toHaveBeenCalledTimes(1)

    vi.setSystemTime(Date.now() + LIMITS.freshForMs + 1)
    catalog.close()
    catalog.open(OPTIONS)
    await flush()
    expect(api.dashboards).toHaveBeenCalledTimes(2)
    expect(api.sources).toHaveBeenCalledTimes(2)
  })

  it('makes no alerts request when alerting is off', async () => {
    const api = stub()
    catalog.open({ ...OPTIONS, alertsEnabled: false })
    await flush()
    expect(api.alerts).not.toHaveBeenCalled()
    expect(catalog.snapshot.value.alerts).toEqual({ state: 'idle' })
  })

  it('bounds every request with a timeout, an abort signal and no toast or redirect', async () => {
    stub([membership(1), membership(2)])
    catalog.open(OPTIONS)
    await flush()
    expect(requests).toHaveLength(7)
    for (const request of requests) {
      expect(request).toMatchObject({ timeout: LIMITS.timeoutSeconds, suppressErrorToast: true, suppressForbiddenRedirect: true })
      expect(request.signal).toBeInstanceOf(AbortSignal)
    }
  })

  it('loads priority teams first, caps the sweep, and loads a named team on demand', async () => {
    const api = stub(Array.from({ length: 30 }, (_, index) => membership(index + 1)))
    catalog.open({ ...OPTIONS, priorityTeams: [30, 7, 30] })
    await flush()
    const order = api.sources.mock.calls.map(([teamId]) => teamId)
    expect(order).toHaveLength(LIMITS.eagerTeams)
    expect(order.slice(0, 4)).toEqual([30, 7, 1, 2])
    expect(catalog.snapshot.value.sourcesByTeam.has(29)).toBe(false)

    catalog.ensureTeamSources(29)
    await flush()
    expect(api.sources).toHaveBeenLastCalledWith(29, expect.anything())
    expect(catalog.snapshot.value.sourcesByTeam.get(29)?.state).toBe('ready')
  })

  it('never has more than four requests in flight', async () => {
    let active = 0
    let peak = 0
    const slow = async <T>(value: T) => {
      active++
      peak = Math.max(peak, active)
      await new Promise((resolve) => setTimeout(resolve, 2))
      active--
      return ok(value)
    }
    vi.spyOn(teamsApi, 'listUserTeams').mockImplementation(() => slow(Array.from({ length: 12 }, (_, index) => membership(index + 1))))
    vi.spyOn(teamsApi, 'listTeamSources').mockImplementation((teamId) => slow([apiSource(teamId)]))
    vi.spyOn(savedQueriesApi, 'list').mockImplementation(() => slow([]))
    vi.spyOn(dashboardsApi, 'list').mockImplementation(() => slow([]))
    vi.spyOn(alertsApi, 'list').mockImplementation(() => slow([]))
    vi.spyOn(collectionsApi, 'list').mockImplementation(() => slow([]))
    catalog.open(OPTIONS)
    await vi.waitFor(() => expect(catalog.snapshot.value.sourcesByTeam.get(12)?.state).toBe('ready'))
    expect(peak).toBe(LIMITS.concurrency)
  })

  it('keeps what it showed when a refresh is aborted by closing', async () => {
    const api = stub()
    catalog.open(OPTIONS)
    await flush()
    vi.setSystemTime(Date.now() + LIMITS.freshForMs + 1)
    let signal: AbortSignal | undefined
    api.dashboards.mockImplementation((options) => new Promise((_, reject) => {
      signal = options?.signal
      signal?.addEventListener('abort', () => reject(new DOMException('aborted', 'AbortError')))
    }))
    catalog.close()
    catalog.open(OPTIONS)
    await flush()
    expect(catalog.snapshot.value.dashboards).toMatchObject({ state: 'loading', stale: [{ id: 1 }] })

    catalog.close()
    await flush()
    expect(signal?.aborted).toBe(true)
    expect(catalog.snapshot.value.dashboards).toMatchObject({ state: 'ready', value: [{ id: 1 }] })
  })

  it('drops a response that arrives after the palette closed and opened again', async () => {
    const api = stub()
    let late: (value: APIResponse<Dashboard[]>) => void = () => {}
    api.dashboards.mockImplementationOnce(() => new Promise((resolve) => { late = resolve }))
    catalog.open(OPTIONS)
    await flush()
    expect(api.dashboards).toHaveBeenCalledTimes(1)
    catalog.close()
    catalog.open(OPTIONS)
    await flush()
    late(ok([dashboard(99)]))
    await flush()
    expect(current(catalog.snapshot.value.dashboards)).toEqual([{ id: 1, name: 'Board 1' }])
  })

  it('marks a failed list and loads it again on retry', async () => {
    const api = stub()
    api.dashboards.mockRejectedValueOnce({ status: 'error', message: 'boom', error_type: 'ServerError' })
    catalog.open(OPTIONS)
    await flush()
    expect(catalog.snapshot.value.dashboards).toEqual({ state: 'failed', stale: null })
    expect(catalog.snapshot.value.collections.state).toBe('ready')

    catalog.retry('dashboards')
    await flush()
    expect(catalog.snapshot.value.dashboards.state).toBe('ready')
  })

  it('forgets the previous user, and everything on clear', async () => {
    const api = stub()
    catalog.open(OPTIONS)
    await flush()
    api.dashboards.mockImplementation(() => new Promise(() => {}))
    catalog.close()
    catalog.open({ ...OPTIONS, userId: 'u2' })
    expect(catalog.snapshot.value.dashboards).toEqual({ state: 'loading', stale: null })
    await flush()
    expect(api.userTeams).toHaveBeenCalledTimes(2)

    catalog.clear()
    expect(catalog.snapshot.value.teams).toEqual({ state: 'idle' })
    expect(catalog.snapshot.value.sourcesByTeam.size).toBe(0)
  })

  it('lists every team for an admin who is in none', async () => {
    const api = stub([])
    api.allTeams.mockResolvedValue(ok([{ ...membership(5), created_by: '1' }]))
    catalog.open({ ...OPTIONS, isAdmin: true })
    await flush()
    expect(current(catalog.snapshot.value.teams)).toEqual([{ id: 5, name: 'Team 5', slug: 'team-5' }])
  })

  it('lists all teams once for an admin with an existing membership', async () => {
    const api = stub([membership(1)])
    api.allTeams.mockResolvedValue(ok([1, 5].map(id => ({ ...membership(id), created_by: '1' }))))
    catalog.open({ ...OPTIONS, isAdmin: true })
    catalog.open({ ...OPTIONS, isAdmin: true })
    await flush()
    expect(current(catalog.snapshot.value.teams)?.map(team => team.id)).toEqual([1, 5])
    expect(api.allTeams).toHaveBeenCalledTimes(1)
    expect(api.userTeams).not.toHaveBeenCalled()
  })

  it('keeps at most 2,000 items from a list', async () => {
    const api = stub()
    api.dashboards.mockResolvedValue(ok(Array.from({ length: 2_500 }, (_, index) => dashboard(index))))
    catalog.open(OPTIONS)
    await flush()
    expect(current(catalog.snapshot.value.dashboards)).toHaveLength(LIMITS.maxItems)
  })
})
