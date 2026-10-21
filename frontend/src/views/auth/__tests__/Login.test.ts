import { createSSRApp } from 'vue'
import { renderToString } from 'vue/server-renderer'
import { createPinia } from 'pinia'
import { createMemoryHistory, createRouter } from 'vue-router'
import { describe, expect, it } from 'vitest'
import { i18n } from '@/i18n'
import Login from '../Login.vue'

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
    ['unauthorized', 'Access denied. Please contact your administrator to request access.'],
    ['user_inactive', 'Your account is inactive. Please contact your administrator.'],
    ['invalid_token', 'Authentication failed. Please try again.'],
    ['email_not_verified', 'Your email address is not verified. Please verify it with your identity provider and try again.'],
    ['session_not_found', 'Authentication session expired. Please try again.'],
    ['session_expired', 'Authentication session expired. Please try again.'],
    ['user_not_found', 'Access denied. Please contact your administrator to request access.'],
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
