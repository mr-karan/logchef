import { describe, expect, it } from 'vitest'
import router from '@/router'
import { i18n } from '@/i18n'
import { routePages } from '../pages'

const NOT_IN_PALETTE: Record<string, string> = {
  OAuthConsent: 'opened by an OAuth client authorize redirect, never by navigation',
}

const hasParams = (path: string) => path.includes(':')

describe('palette pages', () => {
  it('every page-like route opts in or says why not', () => {
    const undecided = router.getRoutes()
      .filter((record) => typeof record.name === 'string' && !record.aliasOf && !record.redirect)
      .filter((record) => !record.meta.public && !hasParams(record.path))
      .filter((record) => !record.meta.palette && !(String(record.name) in NOT_IN_PALETTE))
      .map((record) => String(record.name))
    expect(undecided).toEqual([])
  })

  it('lists pages in route declaration order with real paths and titles', () => {
    const pages = routePages(router)
    expect(pages.map((page) => page.name)).toEqual([
      'LogExplorer', 'Library', 'AlertsOverview', 'AlertCreate', 'Dashboards',
      'ManageUsers', 'ServiceTokens', 'Teams', 'Sources', 'NewSource', 'SourceInspection',
      'AdminSettings', 'AdminQueryActivity', 'Profile', 'Preferences', 'ConnectedApps',
    ])
    for (const page of pages) {
      expect(router.resolve(page.path).name).toBe(page.name)
      expect(i18n.global.te(page.titleKey, 'en')).toBe(true)
    }
  })

  it('gates each page with the requirements of its matched chain', () => {
    const requirements = Object.fromEntries(routePages(router).map((page) => [page.name, page.requirements]))
    expect(requirements.LogExplorer).toEqual([])
    expect(requirements.AlertCreate).toEqual(['alerts'])
    expect(requirements.Teams).toEqual(['anyTeamAdmin'])
    expect(requirements.Sources).toEqual(['admin'])
    expect(requirements.ConnectedApps).toEqual(['oauth'])
  })
})
