import { describe, expect, it } from 'vitest'
import router from '@/router'
import { i18n } from '@/i18n'
import type { Viewer } from '@/router/access'
import { epochMs, parseZoneId, type ZoneId } from '@/utils/timeWindow'
import type { CatalogSnapshot, Loadable, SourceEntry, TeamEntry } from '../catalog'
import { routePages } from '../pages'
import { buildPaletteView, fuzzyMatch, type PaletteContext, type PaletteEffect, type PaletteItem, type PaletteView, type Translate } from '../palette'

const t: Translate = (key, named, plural) => (plural === undefined ? i18n.global.t(key, named ?? {}) : i18n.global.t(key, named ?? {}, plural))

function zone(name: string): ZoneId {
  const parsed = parseZoneId(name)
  if (!parsed) throw new Error(`bad zone ${name}`)
  return parsed
}

const ready = <T>(value: T): Loadable<T> => ({ state: 'ready', value, at: epochMs(0) })

const PAYMENTS: TeamEntry = { id: 3, name: 'Payments', slug: 'payments' }
const PLATFORM: TeamEntry = { id: 4, name: 'Platform', slug: 'platform' }

function source(id: number, teamId: number, name: string, extra: Partial<SourceEntry> = {}): SourceEntry {
  return { id, teamId, name, slug: name.toLowerCase().replace(/\s+/g, '-'), typeLabel: 'ClickHouse', connected: true, logchefql: true, ...extra }
}

const PAYMENT_SOURCES = [source(12, 3, 'checkout_api'), source(13, 3, 'ledger_worker'), source(14, 3, 'nginx_access', { typeLabel: 'VictoriaLogs' })]
const PLATFORM_SOURCES = [
  source(21, 4, 'app_logs'), source(22, 4, 'legacy', { connected: false }), source(23, 4, 'sql_only', { logchefql: false }),
  source(24, 4, 'Edge Logs'), source(25, 4, 'Edge Logs'),
]

function catalog(overrides: Partial<CatalogSnapshot> = {}): CatalogSnapshot {
  return {
    teams: ready([PAYMENTS, PLATFORM]),
    sourcesByTeam: new Map([[3, ready(PAYMENT_SOURCES)], [4, ready(PLATFORM_SOURCES)]]),
    savedQueries: ready([{ id: 7, name: 'Failed logins by IP', sourceName: 'app_logs' }]),
    dashboards: ready([{ id: 2, name: 'Checkout latency' }]),
    alerts: ready([{ id: 5, name: 'Auth failures spike', sourceId: 21, firing: true }]),
    collections: ready([{ id: 9, name: 'On-call', personal: false }]),
    ...overrides,
  }
}

const VIEWERS = {
  admin: { isAdmin: true, isAnyTeamAdmin: true, alertsEnabled: true, oauthEnabled: true },
  teamAdmin: { isAdmin: false, isAnyTeamAdmin: true, alertsEnabled: true, oauthEnabled: true },
  member: { isAdmin: false, isAnyTeamAdmin: false, alertsEnabled: true, oauthEnabled: true },
} satisfies Record<string, Viewer>

const NOW = epochMs(Date.UTC(2026, 9, 27, 10, 30))

function context(overrides: Partial<PaletteContext> = {}): PaletteContext {
  return {
    schemaFields: new Set(),
    askChefEnabled: false,
    explorer: null,
    viewer: VIEWERS.admin,
    routeName: 'LogExplorer',
    current: { teamId: 3, sourceId: 12 },
    recents: [{ teamId: 3, sourceId: 12, at: NOW - 60_000 }, { teamId: 4, sourceId: 21, at: NOW - 12 * 60_000 }],
    theme: 'light',
    timezone: 'utc',
    zone: zone('UTC'),
    now: NOW,
    locale: 'en',
    pages: routePages(router),
    t,
    ...overrides,
  }
}

const view = (text: string, ctx = context(), snapshot = catalog(), caret = text.length) =>
  buildPaletteView(text, { start: caret, end: caret }, ctx, snapshot)

const items = (result: PaletteView) => result.groups.flatMap((group) => group.items)
const ids = (result: PaletteView, groupId: string) => result.groups.find((group) => group.id === groupId)?.items.map((item) => item.id) ?? []
const first = (result: PaletteView) => items(result)[0]

function itemById(result: PaletteView, id: string): PaletteItem {
  const found = items(result).find((item) => item.id === id)
  if (!found) throw new Error(`no item ${id} in ${items(result).map((item) => item.id).join(', ')}`)
  return found
}

function explore(team: number, sourceId: number, q: string, range: string): PaletteEffect {
  return { kind: 'navigate', to: { path: '/logs/explore', query: { team: String(team), source: String(sourceId), mode: 'logchefql', q, t: range } } }
}

describe('fuzzyMatch', () => {
  it('ranks exact over prefix over word start over substring over subsequence', () => {
    const scores = ['alerts', 'alerts overview', 'new alerts', 'xalertsx', 'a-l-e-r-t-s'].map((text) => fuzzyMatch('alerts', text)?.score ?? 0)
    expect([...scores].sort((a, b) => b - a)).toEqual(scores)
    expect(new Set(scores).size).toBe(scores.length)
  })

  it('returns the matched ranges and rejects out-of-order letters', () => {
    expect(fuzzyMatch('ale', 'New alert')?.marks).toEqual([{ start: 4, end: 7 }])
    expect(fuzzyMatch('ckt', 'checkout')?.marks).toEqual([{ start: 0, end: 1 }, { start: 4, end: 5 }, { start: 7, end: 8 }])
    expect(fuzzyMatch('tkc', 'checkout')).toBeNull()
  })
})

describe('buildPaletteView', () => {
  it('opens on recent sources, suggestions and pages', () => {
    const result = view('')
    expect(result.mode).toBe('empty')
    expect(result.groups.map((group) => group.id)).toEqual(['recent', 'suggested', 'goto'])
    expect(ids(result, 'recent')).toEqual(['recent:3:12', 'recent:4:21'])
    expect(itemById(result, 'recent:4:21')).toMatchObject({ label: { text: 'Platform/app_logs' }, meta: '12m ago', enter: { kind: 'navigate', to: { path: '/logs/explore', query: { team: '4', source: '21' } } } })
    expect(ids(result, 'suggested')).toEqual(['action:newQuery', 'action:addAlert', 'action:inspectSource', 'action:toggleTheme'])
  })

  it('shows one loading row while recent sources wait for the catalog', () => {
    const result = view('', context(), catalog({ teams: { state: 'loading', stale: null }, sourcesByTeam: new Map() }))
    expect(ids(result, 'recent')).toEqual(['status:recent'])
  })

  it('searches pages, actions and entities and keeps Search in logs last', () => {
    const result = view('ale')
    expect(result.mode).toBe('search')
    expect(result.groups[0].id).toBe('pages')
    expect(ids(result, 'pages').slice(0, 2)).toEqual(['page:AlertsOverview', 'page:AlertCreate'])
    expect(ids(result, 'actions')).toContain('action:addAlert')
    expect(ids(result, 'alerts')).toEqual(['alert:5'])
    expect(itemById(result, 'alert:5')).toMatchObject({ detail: 'Platform/app_logs', meta: 'Firing' })
    expect(result.groups.at(-1)?.id).toBe('fallback')
  })

  it('needs every word to match', () => {
    expect(ids(view('pay check'), 'sources')).toEqual(['source:3:12'])
    expect(ids(view('pay zzz'), 'sources')).toEqual([])
  })

  it('caps a group at eight rows and counts them all', () => {
    const dashboards = Array.from({ length: 12 }, (_, index) => ({ id: index + 1, name: `Board ${index + 1}` }))
    const group = view('board', context(), catalog({ dashboards: ready(dashboards) })).groups.find((entry) => entry.id === 'dashboards')
    expect(group?.items).toHaveLength(8)
    expect(group?.count).toBe('12')
  })

  it('shows a loading or retry row per list without hiding the others', () => {
    const result = view('check', context(), catalog({ dashboards: { state: 'failed', stale: null }, collections: { state: 'loading', stale: null } }))
    expect(itemById(result, 'status:dashboards')).toMatchObject({ state: 'failed', enter: { kind: 'retry', list: 'dashboards' } })
    expect(itemById(result, 'status:collections')).toMatchObject({ state: 'loading', enter: null })
    expect(ids(result, 'sources')).toContain('source:3:12')
  })

  describe('tagged query', () => {
    it('runs on the tagged source with its query and range', () => {
      const result = view('@payments/checkout_api status>=500 last:1h')
      expect(result.mode).toBe('query')
      expect(first(result)).toMatchObject({ shape: 'run', enter: explore(3, 12, 'status>=500', '1h') })
      expect(result.modEnter).toEqual(explore(3, 12, 'status>=500', '1h'))
      expect(first(result)).toMatchObject({ run: { rangeLabel: 'Last 1 hour', span: '09:30 to 10:30' } })
    })

    it('shows from and to in the preferred zone and adds dates across midnight', () => {
      expect(first(view('@payments/checkout_api last:1h', context({ zone: zone('Asia/Kolkata'), timezone: 'local' })))).toMatchObject({ run: { span: '15:00 to 16:00' } })
      const midnight = context({ now: epochMs(Date.UTC(2026, 9, 27, 0, 30)) })
      expect(first(view('@payments/checkout_api last:1h', midnight))).toMatchObject({ run: { span: 'Oct 26, 23:30 to Oct 27, 00:30' } })
      expect(first(view('@payments/checkout_api last:today'))).toMatchObject({ run: { rangeLabel: 'Today', span: '00:00 to 10:30' } })
    })

    it('writes the default range and an empty query into the URL', () => {
      expect(first(view('@payments/checkout_api'))).toMatchObject({ enter: explore(3, 12, '', '15m'), run: { rangeLabel: 'Last 15 minutes' } })
    })

    it('completes teams and sources under the caret, with ghost text for the top match', () => {
      const result = view('@pay')
      expect(result.groups.map((group) => group.id)).toEqual(['sources', 'teams'])
      expect(ids(result, 'sources')).toEqual(['complete:source:3:12', 'complete:source:3:13', 'complete:source:3:14'])
      expect(result.groups[0].count).toBe('payments · 3')
      expect(result.ghost).toEqual({ text: 'ments/checkout_api', edit: { text: '@payments/checkout_api ', caret: 23 } })
      expect(itemById(result, 'complete:team:3')).toMatchObject({ tab: { text: '@payments/', caret: 10 }, enter: { kind: 'edit', edit: { text: '@payments/', caret: 10 } } })
      expect(itemById(result, 'complete:source:3:13')).toMatchObject({ enter: explore(3, 13, '', '15m'), tab: { text: '@payments/ledger_worker ', caret: 24 } })
    })

    it('finds a source by its own name after the @', () => {
      expect(ids(view('@app'), 'sources')).toEqual(['complete:source:4:21'])
    })

    it('completes a range without guessing its unit, and blocks the run until then', () => {
      const text = '@payments/checkout_api status>=500 last:1'
      const result = view(text)
      expect(first(result)).toMatchObject({ shape: 'blocked', id: 'run' })
      expect(result.modEnter).toBeNull()
      expect(ids(result, 'ranges').slice(0, 3)).toEqual(['complete:range:1h', 'complete:range:15m', 'complete:range:12h'])
      expect(result.ghost).toEqual({ text: 'h', edit: { text: `${text}h `, caret: text.length + 2 } })
      expect(result.footer.hints).toContainEqual({ keys: ['tab'], label: 'accept', detail: '1h' })
    })

    it('replaces a range in the middle and keeps the query after it', () => {
      const result = view('@payments/checkout_api last:1 level=error', context(), catalog(), 29)
      expect(itemById(result, 'complete:range:1h').tab).toEqual({ text: '@payments/checkout_api last:1h level=error', caret: 31 })
      expect(result.ghost).toBeNull()
    })

    it('offers no ghost text or Tab while text is selected', () => {
      const result = buildPaletteView('@pay', { start: 1, end: 4 }, context(), catalog())
      expect(result.ghost).toBeNull()
      expect(items(result).every((item) => item.tab === null)).toBe(true)
    })

    it.each([
      ['@nope ', 'No team named @nope'],
      ['@payments/nope ', 'No source nope in Payments'],
      ['@platform/legacy ', 'legacy is not connected'],
      ['@platform/sql_only ', 'sql_only does not support LogchefQL'],
      ['@payments/checkout_api last:1x', 'Unknown time range last:1x. Try 15m, 1h, 7d or today'],
      ['@payments/checkout_api level=1 @platform/app_logs', 'Only one @team/source per query'],
    ])('blocks %j: %s', (text, reason) => {
      expect(first(view(text, context(), catalog(), 0))).toMatchObject({ shape: 'blocked', enter: null, reason })
    })

    it('lets each row run when two sources share a name', () => {
      const result = view('@platform/edge-logs')
      expect(first(result)).toMatchObject({ shape: 'blocked', reason: 'More than one match for @platform/edge-logs. Pick one below' })
      expect(itemById(result, 'complete:source:4:24').enter).toEqual(explore(4, 24, '', '15m'))
      expect(itemById(result, 'complete:source:4:25').enter).toEqual(explore(4, 25, '', '15m'))
    })

    it('waits for a team whose sources are not loaded, and asks for them', () => {
      const result = view('@platform/app_logs ', context(), catalog({ sourcesByTeam: new Map([[3, ready(PAYMENT_SOURCES)]]) }))
      expect(first(result)).toMatchObject({ shape: 'status', state: 'loading', label: 'Looking up @platform/app_logs…' })
      expect(result.needsTeamSources).toEqual([4])
    })
  })

  it.each(['loading', 'failed'])('does not guess across same-slug teams with %s sources', (state) => {
    const other = { ...PAYMENTS, id: 8 }
    const unresolved: Loadable<readonly SourceEntry[]> = state === 'loading' ? { state, stale: null } : { state: 'failed', stale: null }
    const snapshot = catalog({ teams: ready([PAYMENTS, other]), sourcesByTeam: new Map<number, Loadable<readonly SourceEntry[]>>([[3, ready(PAYMENT_SOURCES)], [8, unresolved]]) })
    const result = view('@payments/checkout_api ', context(), snapshot)
    expect(result.modEnter).toBeNull()
    expect(first(result)?.shape).toBe('status')
  })

  describe('Search in logs', () => {
    it('offers literal text search with the chosen source', () => {
      const result = view('timeout')
      expect(result.modEnter).toEqual({ kind: 'searchText', target: { teamId: 3, sourceId: 12, text: 'timeout', range: '15m' } })
      expect(itemById(result, 'fallback')).toMatchObject({ shape: 'row', detail: expect.stringContaining('Payments/checkout_api') })
    })

    it.each(['GET /api?x=1', 'error: a > b', 'level=error', 'failed @platform/app', 'failed @platform#1/app#10'])('searches operator-like text literally, %s', text => {
      const result = view(text)
      expect(result.modEnter).toEqual({ kind: 'searchText', target: { teamId: 3, sourceId: 12, text, range: '15m' } })
      expect(itemById(result, 'fallback')).toMatchObject({ detail: expect.stringContaining('Literal text') })
    })

    it('runs a range-only input with an empty query', () => {
      const result = view('last:1h')
      expect(result.modEnter).toEqual(explore(3, 12, '', '1h'))
      expect(itemById(result, 'fallback')).toMatchObject({ run: { rangeLabel: 'Last 1 hour' } })
    })

    it('uses a recent source only without a current source', () => {
      const ctx = context({ current: null, recents: [{ teamId: 4, sourceId: 21, at: NOW }] })
      expect(view('level=error', ctx).modEnter).toEqual({ kind: 'searchText', target: { teamId: 4, sourceId: 21, text: 'level=error', range: '15m' } })
    })

    it.each(['idle', 'loading', 'failed', 'removed'] as const)('preserves the current source when it is %s', state => {
      const list: Loadable<readonly SourceEntry[]> = state === 'removed' ? ready([]) : state === 'idle' ? { state } : { state, stale: null }
      const snapshot = catalog({ sourcesByTeam: new Map<number, Loadable<readonly SourceEntry[]>>([[3, list], [4, ready(PLATFORM_SOURCES)]]) })
      const result = view('timeout', context(), snapshot)
      expect(result.modEnter?.kind).not.toBe('searchText')
      expect(itemById(result, 'fallback')).toMatchObject(state === 'removed'
        ? { shape: 'blocked', reason: expect.stringContaining('12') }
        : { shape: 'status', state: state === 'failed' ? 'failed' : 'loading', enter: state === 'failed' ? { kind: 'retry', list: { teamSources: 3 } } : null })
    })

    it.each(['loading', 'failed'])('searches the cached current source while refresh is %s', state => {
      const list: Loadable<readonly SourceEntry[]> = state === 'loading'
        ? { state: 'loading', stale: PAYMENT_SOURCES }
        : { state: 'failed', stale: PAYMENT_SOURCES }
      const snapshot = catalog({ sourcesByTeam: new Map<number, Loadable<readonly SourceEntry[]>>([[3, list], [4, ready(PLATFORM_SOURCES)]]) })
      expect(view('timeout', context(), snapshot).modEnter).toEqual({
        kind: 'searchText', target: { teamId: 3, sourceId: 12, text: 'timeout', range: '15m' },
      })
    })

    it.each([
      ['level=error', context({ current: null, recents: [] }), 'Type @team/source to pick a source'],
      ['level=error', context({ current: { teamId: 4, sourceId: 22 } }), 'legacy is not connected'],
    ])('blocks %j with a reason', (text, ctx, reason) => {
      const result = view(text, ctx)
      expect(itemById(result, 'fallback')).toMatchObject({ shape: 'blocked', reason })
      expect(result.modEnter).toBeNull()
    })
  })

  it('derives the selected non-first ghost in the model and suppresses it for caret changes', () => {
    const text = '@pay'
    const selected = 'complete:source:3:13'
    expect(buildPaletteView(text, { start: 4, end: 4 }, context(), catalog(), selected).ghost?.text).toBe('ments/ledger_worker')
    expect(buildPaletteView(text, { start: 1, end: 4 }, context(), catalog(), selected).ghost).toBeNull()
    expect(buildPaletteView(text, { start: 3, end: 3 }, context(), catalog(), selected).ghost).toBeNull()
    expect(buildPaletteView('@pay rest', { start: 4, end: 4 }, context(), catalog(), selected).ghost).toBeNull()
  })

  it('describes the selected range completion in the footer', () => {
    const text = '@payments/checkout_api last:'
    const result = buildPaletteView(text, { start: text.length, end: text.length }, context(), catalog(), 'complete:range:15m')
    expect(result.ghost?.text).toBe('15m')
    expect(result.footer.hints).toContainEqual({ keys: ['tab'], label: 'accept', detail: '15m' })
  })

  it.each([24, 25])('retains duplicate source %s through Tab, query and time completion', sourceId => {
    const ambiguous = view('@platform/edge-logs level=error')
    const row = itemById(ambiguous, `complete:source:4:${sourceId}`)
    expect(row).toMatchObject({ detail: expect.stringContaining(String(sourceId)) })
    expect(itemById(view('edge'), `source:4:${sourceId}`)).toMatchObject({ detail: expect.stringContaining(String(sourceId)) })
    expect(row.tab?.text).toBe(`@platform/edge-logs#${sourceId} level=error`)
    const text = `${row.tab?.text} last:1`
    const time = itemById(view(text), 'complete:range:1h').tab
    expect(time).not.toBeNull()
    expect(view(time?.text ?? '').modEnter).toEqual(explore(4, sourceId, 'level=error', '1h'))
    expect(view(`@platform/edge-logs#${sourceId}`).modEnter).toEqual(explore(4, sourceId, '', '15m'))
  })

  it('qualifies colliding teams and lets accessible IDs override cosmetic names', () => {
    const other = { ...PLATFORM, id: 8 }
    const snapshot = catalog({ teams: ready([PLATFORM, other]), sourcesByTeam: new Map([[4, ready(PLATFORM_SOURCES)], [8, ready([source(81, 8, 'app_logs')])]]) })
    const teamEdit = itemById(view('@plat', context(), snapshot), 'complete:team:8').tab
    expect(teamEdit?.text).toBe('@platform#8/')
    expect(ids(view(teamEdit?.text ?? '', context(), snapshot), 'sources')).toEqual(['complete:source:8:81'])
    const row = itemById(view('@platform/app_logs level=error', context(), snapshot), 'complete:source:8:81')
    expect(row.tab?.text).toBe('@platform#8/app_logs level=error')
    expect(view(row.tab?.text ?? '', context(), snapshot).modEnter).toEqual(explore(8, 81, 'level=error', '15m'))
    expect(view('@cosmetic#8/cosmetic#81 level=error', context(), snapshot).modEnter).toEqual(explore(8, 81, 'level=error', '15m'))
    expect(view('@platform#999/app_logs#81', context(), snapshot).modEnter).toBeNull()
    expect(view('@platform#4/app_logs#81', context(), snapshot).modEnter).toBeNull()
  })

  describe('permissions', () => {
    const pages = (viewer: Viewer) => ids(view('', context({ viewer })), 'goto').map((id) => id.replace('page:', ''))
    const everyone = ['LogExplorer', 'Library', 'AlertsOverview', 'AlertCreate', 'Dashboards']
    const settings = ['Profile', 'Preferences', 'ConnectedApps']

    it('shows admins every page', () => {
      expect(pages(VIEWERS.admin)).toEqual([
        ...everyone, 'ManageUsers', 'ServiceTokens', 'Teams', 'Sources', 'NewSource', 'SourceInspection', 'AdminSettings', 'AdminQueryActivity', ...settings,
      ])
    })

    it('shows team admins Teams and no global admin page', () => {
      expect(pages(VIEWERS.teamAdmin)).toEqual([...everyone, 'Teams', ...settings])
    })

    it('shows members no admin page', () => {
      expect(pages(VIEWERS.member)).toEqual([...everyone, ...settings])
    })

    it('hides alerts and connected apps when the server has them off', () => {
      const viewer = { ...VIEWERS.member, alertsEnabled: false, oauthEnabled: false }
      expect(pages(viewer)).toEqual(['LogExplorer', 'Library', 'Dashboards', 'Profile', 'Preferences'])
      const result = view('a', context({ viewer }))
      expect(ids(result, 'alerts')).toEqual([])
      expect(ids(result, 'actions')).not.toContain('action:addAlert')
    })

    it('offers source inspection to admins only', () => {
      expect(ids(view('', context({ viewer: VIEWERS.member })), 'suggested')).not.toContain('action:inspectSource')
    })
  })

  it('cycles the theme and flips the timezone', () => {
    expect(itemById(view('theme'), 'action:toggleTheme').enter).toEqual({ kind: 'setTheme', mode: 'dark' })
    expect(itemById(view('times'), 'action:switchTimezone').enter).toEqual({ kind: 'setTimezone', timezone: 'local' })
  })

  it('navigates only to routes the router knows', () => {
    const results = [view(''), view('a'), view('o'), view('@pay')]
    const targets = results.flatMap(items).flatMap((item) => (item.enter?.kind === 'navigate' ? [item.enter.to] : []))
    expect(targets.length).toBeGreaterThan(20)
    for (const to of targets) expect(router.resolve(to).name, JSON.stringify(to)).not.toBe('NotFound')
  })
})


describe('Ask Chef palette entry points', () => {
  const captured = { subject: { kind: 'explorer' }, teamId: 3, sourceId: 12, start: NOW - 7200000, end: NOW, zone: zone('UTC'), mode: 'native', query: 'SELECT * FROM logs' } satisfies NonNullable<PaletteContext['explorer']>['context'];
  const askContext = (extra: Partial<PaletteContext> = {}) => context({ askChefEnabled: true, explorer: { context: captured, window: { kind: 'relative', spec: '2h' }, hasResults: true }, ...extra });

  it('follows Search in logs with the captured current query and window', () => {
    const result = view('why are errors increasing', askContext());
    expect(result.groups.slice(-2).map(group => group.id)).toEqual(['fallback', 'ask']);
    expect(result.askEnter).toEqual({ kind: 'askChef', context: captured, question: 'why are errors increasing', navigation: 'current' });
    expect(itemById(result, 'ask')).toMatchObject({ shortcut: 'modShiftEnter' });
  });

  it('forces Ask after an optional source tag and parses its range once', () => {
    const result = view('@platform/app_logs ? why did 5xx spike last:1h', askContext());
    expect(result.mode).toBe('ask');
    expect(result.modEnter).toBeNull();
    expect(result.askEnter).toMatchObject({ kind: 'askChef', context: { teamId: 4, sourceId: 21, start: NOW - 3600000, end: NOW }, question: 'why did 5xx spike' });
    expect(items(result).some(item => item.shape === 'run')).toBe(false);
    expect(result.tokens.some(token => token.kind === 'askMarker')).toBe(true);
  });

  it('offers Ask for a scoped question without the explicit marker', () => {
    expect(view('@platform/app_logs why did this fail', askContext()).askEnter).toMatchObject({ kind: 'askChef', question: 'why did this fail', context: { teamId: 4, sourceId: 21 } });
  });

  it('keeps the current query with an explicit untagged range', () => {
    expect(view('? what changed last:1h', askContext()).askEnter).toMatchObject({ context: { query: captured.query, mode: 'native', start: NOW - 3600000, end: NOW }, question: 'what changed' });
  });

  it('requires a whole normalized prefix of an available page or action to hide Ask', () => {
    for (const text of ['alerts', 'ale', '  aLeRtS  ', 'sign out']) expect(items(view(text, askContext())).some(item => item.id === 'ask')).toBe(false);
    for (const text of ['alr', 'new alert errors', 'alerts why', 'why alerts failed']) expect(items(view(text, askContext())).some(item => item.id === 'ask')).toBe(true);
    expect(view('? alerts', askContext()).askEnter?.kind).toBe('askChef');
  });

  it.each(['? why did it fail last:1h', 'why did it fail last:1h'])('preserves %j while offering source completion off Explore', text => {
    const ctx = askContext({ routeName: 'TeamsList' });
    const row = itemById(view(text, ctx), 'ask');
    expect(row.enter?.kind).toBe('edit');
    if (row.enter?.kind !== 'edit') throw new Error('expected source edit');
    expect(row.enter.edit.caret).toBe(1);
    const completing = view(row.enter.edit.text, ctx, catalog(), row.enter.edit.caret);
    expect(completing.needsTeamSources).toEqual([]);
    const source = itemById(completing, 'complete:source:3:12');
    if (source.enter?.kind !== 'edit') throw new Error('expected source completion');
    expect(source.enter.edit.text).toContain('? why did it fail last:1h');
    expect(view(source.enter.edit.text, ctx).askEnter).toMatchObject({ kind: 'askChef', question: 'why did it fail', context: { teamId: 3, sourceId: 12, start: NOW - 3600000 } });
  });

  it('opens the results drawer with no invented question', () => {
    expect(itemById(view('', askContext()), 'ask:results').enter).toEqual({ kind: 'askChef', context: captured, question: null, navigation: 'current' });
    expect(items(view('', askContext({ routeName: 'TeamsList' }))).some(item => item.id === 'ask:results')).toBe(false);
    expect(items(view('', askContext({ explorer: { context: captured, window: { kind: 'relative', spec: '2h' }, hasResults: false } }))).some(item => item.id === 'ask:results')).toBe(false);
  });

  it('adds an investigation after each matched alert, including resolved alerts', () => {
    const result = view('failures', askContext(), catalog({ alerts: ready([{ id: 5, name: 'Auth failures spike', sourceId: 21, firing: false }, { id: 6, name: 'API failures', sourceId: 12, firing: true }]) }));
    expect(ids(result, 'alerts')).toEqual(['alert:6', 'alert-ask:6', 'alert:5', 'alert-ask:5']);
    expect(itemById(result, 'alert-ask:5').enter).toEqual({ kind: 'investigateAlert', alertId: 5, teamId: 4 });
  });

  it('hides every Ask entry and shortcut when disabled', () => {
    for (const text of ['', 'why did it fail', '? why did it fail', '@platform/app_logs ? why', 'failures']) {
      const result = view(text, askContext({ askChefEnabled: false }));
      expect(result.askEnter).toBeNull();
      expect(result.mode).not.toBe('ask');
      expect(items(result).some(item => 'icon' in item && item.icon === 'askChef')).toBe(false);
    }
  });

  it('keeps question-mark input usable as ordinary text when Ask is disabled', () => {
    const result = view('? why did it fail', askContext({ askChefEnabled: false }));
    expect(itemById(result, 'fallback').enter).toMatchObject({ kind: 'searchText', target: { text: '? why did it fail' } });
    expect(view('@platform/app_logs ? why', askContext({ askChefEnabled: false })).modEnter).toMatchObject({ kind: 'navigate', to: { query: { q: '? why' } } });
  });

  it('allows a tagged Ask on a source that supports only native queries', () => {
    expect(view('@platform/sql_only ? why did it fail', askContext()).askEnter).toMatchObject({ kind: 'askChef', context: { teamId: 4, sourceId: 23, mode: 'native' } });
  });

  it('keeps alert labels and both actions in the selected team for a shared source', () => {
    const shared = catalog({
      sourcesByTeam: new Map([[4, ready([source(12, 4, 'checkout_api')])], [3, ready(PAYMENT_SOURCES)]]),
      alerts: ready([{ id: 6, name: 'API failures', sourceId: 12, firing: false }]),
    });
    const result = view('failures', askContext(), shared);
    expect(itemById(result, 'alert:6')).toMatchObject({ detail: 'Payments/checkout_api', enter: { kind: 'navigate', to: { query: { team: '3', source: '12' } } } });
    expect(itemById(result, 'alert-ask:6').enter).toMatchObject({ kind: 'investigateAlert', teamId: 3 });
  });

  it.each(['? question last:nope', '? question last:1h last:2h', '?' + 'x'.repeat(2001), '@platform/app_logs ? q @payments/checkout_api'])('blocks malformed Ask input %j', text => {
    const result = view(text, askContext());
    expect(result.askEnter).toBeNull();
    expect(itemById(result, 'ask').shape).toBe('blocked');
  });
});


describe('implicit Run on Explore', () => {
  const captured = { subject: { kind: 'explorer' }, teamId: 3, sourceId: 12, start: NOW - 7200000, end: NOW, zone: zone('UTC'), mode: 'logchefql', query: 'status>=400' } satisfies NonNullable<PaletteContext['explorer']>['context'];
  const ctx = (extra: Partial<PaletteContext> = {}) => context({ schemaFields: new Set(['status', 'method']), explorer: { context: captured, window: { kind: 'relative', spec: '2h' }, hasResults: true }, ...extra });

  it('puts Run first and preserves the current untagged relative window', () => {
    const result = view('status>=500', ctx());
    expect(first(result)).toMatchObject({ shape: 'run', id: 'implicit:run', run: { query: 'status>=500', window: { kind: 'relative', spec: '2h' } }, enter: explore(3, 12, 'status>=500', '2h') });
    expect(result.modEnter).toEqual(first(result).enter);
    expect(result.mode).toBe('query');
    expect(result.tokens.some(token => token.kind === 'field')).toBe(true);
    expect(itemById(result, 'fallback').enter?.kind).toBe('searchText');
  });

  it('retains absolute windows and keeps a valid explicit range', () => {
    const absolute = ctx({ explorer: { context: captured, window: { kind: 'absolute', start: epochMs(1000), end: epochMs(2000) }, hasResults: true } });
    expect(first(view('status>=500', absolute))).toMatchObject({ run: { rangeLabel: 'Absolute time range' } });
    expect(first(view('method="GET" and status>=400', absolute)).enter).toEqual({ kind: 'navigate', to: { path: '/logs/explore', query: { team: '3', source: '12', mode: 'logchefql', q: 'method="GET" and status>=400', start: '1000', end: '2000' } } });
    expect(first(view('status>=500 last:1h', ctx())).enter).toEqual(explore(3, 12, 'status>=500', '1h'));
  });

  it.each(['what failed', 'https://example.com/?q=x', 'a=b', 'error: count > 5', 'status>500 and', 'status="bad', 'status=500 last:nope'])('does not promote %j into a run', text => {
    expect(items(view(text, ctx())).some(item => item.id === 'implicit:run')).toBe(false);
  });

  it('requires current Explore schema and never infers a source from recents off Explore', () => {
    for (const extra of [{ routeName: 'TeamsList' }, { schemaFields: new Set<string>() }, { current: null }, { explorer: null }]) {
      expect(items(view('status>=500', ctx(extra))).some(item => item.id === 'implicit:run')).toBe(false);
    }
    expect(items(view('? status>=500', ctx({ askChefEnabled: true }))).some(item => item.shape === 'run')).toBe(false);
    expect(first(view('@payments/checkout_api status>=500', ctx())).id).toBe('run');
  });
});
