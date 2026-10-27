import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { createPinia, setActivePinia } from 'pinia'
import router from '@/router'
import { meetsRequirements, routeRequirements, type RouteRequirement, type Viewer } from '@/router/access'
import { authApi } from '@/api/auth'
import { metaApi } from '@/api/meta'
import { teamsApi } from '@/api/teams'
import { useAuthStore } from '@/stores/auth'
import { useTeamsStore } from '@/stores/teams'

type Role = 'admin' | 'teamAdmin' | 'member'

const VIEWERS: Record<Role, Viewer> = {
  admin: { isAdmin: true, isAnyTeamAdmin: false, alertsEnabled: true, oauthEnabled: true },
  teamAdmin: { isAdmin: false, isAnyTeamAdmin: true, alertsEnabled: true, oauthEnabled: true },
  member: { isAdmin: false, isAnyTeamAdmin: false, alertsEnabled: true, oauthEnabled: true },
}

describe('routeRequirements', () => {
  it.each<[string, RouteRequirement[]]>([
    ['/logs/explore', []],
    ['/dashboards', []],
    ['/logs/alerts', ['alerts']],
    ['/logs/alerts/new', ['alerts']],
    ['/logs/alerts/7', ['alerts']],
    ['/admin/users', ['admin']],
    ['/admin/sources/stats', ['admin']],
    ['/admin/teams', ['anyTeamAdmin']],
    ['/admin/teams/3', ['anyTeamAdmin']],
    ['/settings/connected-apps', ['oauth']],
    ['/oauth/consent', ['oauth']],
  ])('%s needs %j', (path, expected) => {
    expect(routeRequirements(router.resolve(path).matched)).toEqual(expected)
  })

  it('reads requirements from a parent record', () => {
    const chain = [
      { path: '/admin', meta: { requiresAdmin: true } },
      { path: '/admin/child', meta: {} },
    ]
    expect(routeRequirements(chain)).toEqual(['admin'])
  })
})

describe('meetsRequirements', () => {
  it.each<[Role, RouteRequirement, boolean]>([
    ['admin', 'admin', true],
    ['admin', 'anyTeamAdmin', true],
    ['teamAdmin', 'admin', false],
    ['teamAdmin', 'anyTeamAdmin', true],
    ['member', 'admin', false],
    ['member', 'anyTeamAdmin', false],
    ['member', 'alerts', true],
    ['member', 'oauth', true],
  ])('%s meets %s: %s', (role, requirement, expected) => {
    expect(meetsRequirements([requirement], VIEWERS[role])).toBe(expected)
  })

  it('needs alerts and OAuth to be enabled on the server', () => {
    const member = VIEWERS.member
    expect(meetsRequirements(['alerts'], { ...member, alertsEnabled: false })).toBe(false)
    expect(meetsRequirements(['oauth'], { ...member, oauthEnabled: false })).toBe(false)
    expect(meetsRequirements([], { ...member, alertsEnabled: false, oauthEnabled: false })).toBe(true)
  })
})

describe('router guard', () => {
  let stopAfterGuard: () => void
  let landed: { path: string; name: unknown } | null

  beforeEach(() => {
    setActivePinia(createPinia())
    localStorage.clear()
    landed = null
    stopAfterGuard = router.beforeEach((to) => {
      landed = { path: to.path, name: to.name }
      return false
    })
  })

  afterEach(() => {
    stopAfterGuard()
    vi.restoreAllMocks()
  })

  async function signIn(role: Role, server: { alerts?: boolean; oauth?: boolean } = {}) {
    vi.spyOn(metaApi, 'getMeta').mockResolvedValue({
      status: 'success',
      data: {
        version: 'test', http_server_timeout: '30s', max_query_limit: 1000,
        alerts_enabled: server.alerts ?? true,
        ...(server.oauth === false ? {} : { oauth_issuer: 'http://localhost' }),
      },
    })
    vi.spyOn(authApi, 'getSession').mockResolvedValue({
      status: 'success',
      data: {
        user: {
          id: '1', email: 'owner@example.com', full_name: 'Owner', role: role === 'admin' ? 'admin' : 'member',
          status: 'active', account_type: 'human', created_at: '', updated_at: '',
        },
        session: { id: 's', user_id: '1', expires_at: '', created_at: '' },
      },
    })
    vi.spyOn(teamsApi, 'listUserTeams').mockResolvedValue({
      status: 'success',
      data: [{
        id: 1, name: 'Team 1', description: '', created_at: '', updated_at: '', member_count: 1,
        role: role === 'teamAdmin' ? 'admin' : 'member',
      }],
    })
    await useAuthStore().initialize()
    await useTeamsStore().loadUserTeams()
  }

  async function land(path: string) {
    await router.push(path)
    return landed
  }

  it.each<[Role, string, string]>([
    ['admin', '/admin/users', '/admin/users'],
    ['teamAdmin', '/admin/users', '/forbidden'],
    ['teamAdmin', '/admin/teams', '/admin/teams'],
    ['member', '/admin/teams', '/forbidden'],
    ['member', '/logs/alerts', '/logs/alerts'],
  ])('sends a %s from %s to %s', async (role, from, to) => {
    await signIn(role)
    expect((await land(from))?.path).toBe(to)
  })

  it('sends alert links to the explorer when alerting is off', async () => {
    await signIn('admin', { alerts: false })
    expect((await land('/logs/alerts/7'))?.path).toBe('/logs/explore')
  })

  it('shows Not Found for connected apps when OAuth is off', async () => {
    await signIn('member', { oauth: false })
    expect((await land('/settings/connected-apps'))?.name).toBe('NotFound')
  })

  it('sends a signed-out visitor to login before any role check', async () => {
    expect((await land('/admin/users'))?.name).toBe('Login')
  })
})
