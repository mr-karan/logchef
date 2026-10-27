const KEY_PREFIX = "logchef_recent_sources:";
const VERSION = 1;
export const MAX_RECENT_SOURCES = 8;

export interface RecentSource {
  readonly teamId: number;
  readonly sourceId: number;
  readonly at: number;
}

export function pushRecent(list: readonly RecentSource[], entry: RecentSource, max: number): RecentSource[] {
  const rest = list.filter((item) => item.teamId !== entry.teamId || item.sourceId !== entry.sourceId);
  return [entry, ...rest].slice(0, max);
}

const isId = (value: unknown): value is number => typeof value === "number" && Number.isInteger(value) && value > 0;

function parseRecent(value: unknown): RecentSource | null {
  if (typeof value !== "object" || value === null || !("teamId" in value) || !("sourceId" in value) || !("at" in value)) return null;
  const { teamId, sourceId, at } = value;
  if (!isId(teamId) || !isId(sourceId) || typeof at !== "number" || !Number.isFinite(at)) return null;
  return { teamId, sourceId, at };
}

export function parseRecentSources(raw: string | null): RecentSource[] {
  if (!raw) return [];
  try {
    const data: unknown = JSON.parse(raw);
    if (typeof data !== "object" || data === null || !("v" in data) || data.v !== VERSION || !("items" in data)) return [];
    if (!Array.isArray(data.items)) return [];
    return data.items.flatMap((item: unknown) => parseRecent(item) ?? []).slice(0, MAX_RECENT_SOURCES);
  } catch {
    return [];
  }
}

export function readRecentSources(userId: string): RecentSource[] {
  try {
    return parseRecentSources(localStorage.getItem(KEY_PREFIX + userId));
  } catch {
    return [];
  }
}

export function recordRecentSource(userId: string, teamId: number, sourceId: number, at: number): void {
  const items = pushRecent(readRecentSources(userId), { teamId, sourceId, at }, MAX_RECENT_SOURCES);
  try {
    localStorage.setItem(KEY_PREFIX + userId, JSON.stringify({ v: VERSION, items }));
  } catch {
    // Storage full or blocked: recents are a convenience.
  }
}
