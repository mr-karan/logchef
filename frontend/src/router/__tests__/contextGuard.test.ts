import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { createPinia, setActivePinia } from 'pinia'
import router from '@/router'
import { contextRouterGuard } from '@/router/contextGuard'
import { authApi } from '@/api/auth'
import { metaApi } from '@/api/meta'
import { teamsApi, type UserTeamMembership } from '@/api/teams'
import { sourcesApi } from '@/api/sources'
import { useAuthStore } from '@/stores/auth'
import { useContextStore } from '@/stores/context'
import { useSourcesStore } from '@/stores/sources'
import type { User } from '@/types'

// Real router routes and real auth/context/teams/sources stores. Only the HTTP
// calls are stubbed: /me, /meta, /me/teams and /teams/:id/sources.
const STORAGE_KEY = 'logchef_context'

const flushPromises = () => new Promise((resolve) => setTimeout(resolve))

const ok = <T>(data: T) => Promise.resolve({ status: 'success' as const, data })

function membership(id: number): UserTeamMembership {
  return { id, name: `Team ${id}`, description: '', created_at: '', updated_at: '', member_count: 1, role: 'member' }
}

function user(role: User['role']): User {
  return {
    id: '1', email: 'owner@example.com', full_name: 'Owner', role, status: 'active',
    account_type: 'human', created_at: '', updated_at: '',
  }
}

function persistContext(lastTeamId: number, sourcePerTeam: Record<number, number>) {
  localStorage.setItem(STORAGE_KEY, JSON.stringify({ lastTeamId, sourcePerTeam }))
}

function storedContext(): { lastTeamId: number | null; sourcePerTeam: Record<string, number> } {
  return JSON.parse(localStorage.getItem(STORAGE_KEY) ?? 'null')
}

async function signIn(role: User['role']) {
  vi.spyOn(metaApi, 'getMeta').mockReturnValue(ok({ version: 'test' }) as never)
  vi.spyOn(authApi, 'getSession').mockReturnValue(
    ok({ user: user(role), session: { id: 's', user_id: '1', expires_at: '', created_at: '' } }) as never,
  )
  await useAuthStore().initialize()
}

const library = () => router.resolve({ name: 'Library' })

describe('contextRouterGuard', () => {
  let listUserTeams: ReturnType<typeof vi.spyOn>
  let listTeamSources: ReturnType<typeof vi.spyOn>

  beforeEach(() => {
    setActivePinia(createPinia())
    localStorage.clear()
    listUserTeams = vi.spyOn(teamsApi, 'listUserTeams')
    listTeamSources = vi.spyOn(sourcesApi, 'listTeamSources').mockImplementation(
      (teamId: number) => ok([{ id: teamId * 10 }]) as never,
    )
  })

  afterEach(() => {
    vi.restoreAllMocks()
  })

  it('keeps a persisted team and source that are still permitted', async () => {
    await signIn('member')
    persistContext(2, { 2: 20 })
    listUserTeams.mockReturnValue(ok([membership(1), membership(2)]) as never)
    useSourcesStore()

    await contextRouterGuard(library())
    await flushPromises()

    const context = useContextStore()
    expect(context.teamId).toBe(2)
    expect(context.sourceId).toBe(20)
    expect(storedContext().lastTeamId).toBe(2)
    expect(listTeamSources.mock.calls).toEqual([[2]])
  })

  it('falls back to a remaining team before loading sources for a revoked team', async () => {
    await signIn('member')
    persistContext(14, { 14: 140, 3: 30 })
    listUserTeams.mockReturnValue(ok([membership(3)]) as never)
    // Created before navigation, as App.vue does through the explore store.
    useSourcesStore()

    await contextRouterGuard(library())
    await flushPromises()

    const context = useContextStore()
    expect(context.teamId).toBe(3)
    expect(context.sourceId).toBe(30)
    expect(listTeamSources.mock.calls).toEqual([[3]])
    expect(storedContext()).toEqual({ lastTeamId: 3, sourcePerTeam: { 3: 30 } })
  })

  it('clears memory and persisted context when no team remains', async () => {
    await signIn('member')
    persistContext(14, { 14: 140 })
    listUserTeams.mockReturnValue(ok([]) as never)
    useSourcesStore()

    await contextRouterGuard(library())
    await flushPromises()

    const context = useContextStore()
    expect(context.teamId).toBeNull()
    expect(context.sourceId).toBeNull()
    expect(storedContext()).toEqual({ lastTeamId: null, sourcePerTeam: {} })
    expect(listTeamSources).not.toHaveBeenCalled()
  })

  it('does not treat a failed membership fetch as zero teams', async () => {
    await signIn('member')
    persistContext(14, { 14: 140 })
    listUserTeams.mockRejectedValue({ status: 'error', message: 'unavailable', error_type: 'ServerError' })
    useSourcesStore()

    await contextRouterGuard(library())
    await flushPromises()

    expect(useContextStore().teamId).toBeNull()
    expect(storedContext()).toEqual({ lastTeamId: 14, sourcePerTeam: { 14: 140 } })
    expect(listTeamSources).not.toHaveBeenCalled()
  })

  it('does not refetch membership once a team is selected', async () => {
    await signIn('member')
    persistContext(2, {})
    listUserTeams.mockReturnValue(ok([membership(2)]) as never)

    await contextRouterGuard(library())
    await contextRouterGuard(router.resolve({ name: 'LogExplorer' }))

    expect(useContextStore().teamId).toBe(2)
    expect(listUserTeams).toHaveBeenCalledTimes(1)
  })

  it('keeps an explicit team URL so the server can deny it', async () => {
    await signIn('member')
    persistContext(2, {})

    await contextRouterGuard(router.resolve({ name: 'LogExplorer', query: { team: '99' } }))

    expect(useContextStore().teamId).toBe(99)
    expect(listUserTeams).not.toHaveBeenCalled()
  })

  it('keeps a global admin on a persisted team outside their memberships', async () => {
    await signIn('admin')
    persistContext(14, { 14: 140 })

    await contextRouterGuard(library())

    expect(useContextStore().teamId).toBe(14)
    expect(listUserTeams).not.toHaveBeenCalled()
  })
})
