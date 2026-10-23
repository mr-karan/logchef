import { createApp, createSSRApp, nextTick } from 'vue'
import { renderToString } from 'vue/server-renderer'
import { createPinia } from 'pinia'
import { createMemoryHistory, createRouter } from 'vue-router'
import { afterEach, describe, expect, it, vi } from 'vitest'
import { i18n } from '@/i18n'
import { metaApi } from '@/api/meta'
import { useAuthStore } from '@/stores/auth'
import Login from '../Login.vue'

vi.mock('@/api/meta', () => ({ metaApi: { getMeta: vi.fn() } }))

async function renderLogin(query: string) {
  const router = createRouter({
    history: createMemoryHistory(),
    routes: [{ path: '/auth/login', component: Login }],
  })
  await router.push(`/auth/login${query}`)
  const app = createSSRApp(Login)
  app.use(createPinia()).use(router).use(i18n)
  return renderToString(app)
}

describe('login error messages', () => {
  it.each([
    ['access_denied', 'Access denied. Please contact your administrator to request access.'],
    ['unauthorized', 'Access denied. Please contact your administrator to request access.'],
    ['user_inactive', 'Your account is inactive. Please contact your administrator.'],
    ['invalid_token', 'Authentication failed. Please try again.'],
    ['email_not_verified', 'Your email address is not verified. Please verify it with your identity provider and try again.'],
    ['session_expired', 'Authentication session expired. Please try again.'],
    ['auth_error', 'An unexpected error occurred'],
    ['unknown_error', 'An unexpected error occurred'],
    ['toString', 'An unexpected error occurred'],
  ])('renders the message for %s', async (code, message) => {
    const html = await renderLogin(`?error=${code}`)
    expect(html).toContain(message)
    expect(html).toContain('Authentication Error')
  })

  it('does not show an error when no code is present', async () => {
    expect(await renderLogin('')).not.toContain('Authentication Error')
  })

  it('uses the generic message for repeated error parameters', async () => {
    expect(await renderLogin('?error=unauthorized&error=user_inactive'))
      .toContain('An unexpected error occurred')
  })
})

describe('SSO auto-redirect', () => {
  let unmount = () => {}
  afterEach(() => unmount())

  async function mountLogin(query: string, auth: { oidc: boolean; local: boolean }) {
    vi.mocked(metaApi.getMeta).mockResolvedValue({
      status: 'success',
      data: { version: 'test', oidc_enabled: auth.oidc, local_auth_enabled: auth.local },
    } as never)
    const router = createRouter({
      history: createMemoryHistory(),
      routes: [{ path: '/auth/login', name: 'Login', component: Login }],
    })
    await router.push(`/auth/login${query}`)
    const pinia = createPinia()
    const app = createApp(Login).use(pinia).use(router).use(i18n)
    const startLogin = vi.spyOn(useAuthStore(pinia), 'startLogin').mockResolvedValue({ success: true } as never)
    app.mount(document.createElement('div'))
    unmount = () => app.unmount()
    await vi.waitFor(() => expect(metaApi.getMeta).toHaveBeenCalled())
    await new Promise(resolve => setTimeout(resolve))
    await nextTick()
    return startLogin
  }

  it('starts SSO with the redirect when SSO is the only method', async () => {
    const startLogin = await mountLogin('?redirect=/logs/explore%3Fteam%3D1', { oidc: true, local: false })
    expect(startLogin).toHaveBeenCalledOnce()
    expect(startLogin).toHaveBeenCalledWith('/logs/explore?team=1')
  })

  it.each([
    ['local auth is also enabled', '', { oidc: true, local: true }],
    ['SSO is disabled', '', { oidc: false, local: true }],
    ['the callback failed', '?error=access_denied', { oidc: true, local: false }],
    ['the user just logged out', '?logged_out=1', { oidc: true, local: false }],
  ])('shows the page when %s', async (_case, query, auth) => {
    const startLogin = await mountLogin(query, auth)
    expect(startLogin).not.toHaveBeenCalled()
  })
})
