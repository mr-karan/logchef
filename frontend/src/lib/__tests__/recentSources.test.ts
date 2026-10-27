import { beforeEach, describe, expect, it } from 'vitest'
import { MAX_RECENT_SOURCES, parseRecentSources, pushRecent, readRecentSources, recordRecentSource } from '@/lib/recentSources'

describe('recent sources', () => {
  beforeEach(() => localStorage.clear())

  it('moves a revisited source to the front with its new time', () => {
    const list = [{ teamId: 1, sourceId: 10, at: 1 }, { teamId: 2, sourceId: 20, at: 2 }]
    expect(pushRecent(list, { teamId: 2, sourceId: 20, at: 3 }, 8)).toEqual([{ teamId: 2, sourceId: 20, at: 3 }, { teamId: 1, sourceId: 10, at: 1 }])
  })

  it('keeps the same source in two teams apart', () => {
    expect(pushRecent([{ teamId: 1, sourceId: 10, at: 1 }], { teamId: 2, sourceId: 10, at: 2 }, 8)).toHaveLength(2)
  })

  it('keeps at most the newest eight per user', () => {
    for (let id = 1; id <= 10; id++) recordRecentSource('u1', 1, id, id)
    recordRecentSource('u1', 1, 10, 11)
    const recents = readRecentSources('u1')
    expect(recents).toHaveLength(MAX_RECENT_SOURCES)
    expect(recents.map((recent) => recent.sourceId)).toEqual([10, 9, 8, 7, 6, 5, 4, 3])
    expect(readRecentSources('u2')).toEqual([])
  })

  it.each([
    null,
    'not json',
    '[]',
    '{"items":[]}',
    '{"v":2,"items":[{"teamId":1,"sourceId":2,"at":3}]}',
    '{"v":1,"items":"nope"}',
  ])('reads %j as an empty list', (raw) => {
    expect(parseRecentSources(raw)).toEqual([])
  })

  it('drops malformed entries and keeps good ones', () => {
    const raw = JSON.stringify({ v: 1, items: [{ teamId: 1, sourceId: 2, at: 3 }, { teamId: '1', sourceId: 2, at: 3 }, { teamId: 1, sourceId: 0, at: 3 }, null] })
    expect(parseRecentSources(raw)).toEqual([{ teamId: 1, sourceId: 2, at: 3 }])
  })
})
