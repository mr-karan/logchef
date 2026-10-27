import { shallowRef, type ShallowRef } from "vue";
import { alertsApi } from "@/api/alerts";
import type { RequestOptions } from "@/api/apiUtils";
import { collectionsApi } from "@/api/collections";
import { dashboardsApi } from "@/api/dashboards";
import { isAbortError } from "@/api/error-handler";
import { savedQueriesApi } from "@/api/savedQueries";
import type { Source } from "@/api/sources";
import { teamsApi } from "@/api/teams";
import type { APIResponse } from "@/api/types";
import { getSourceTypeLabel, supportsQueryLanguage } from "@/lib/queryMetadata";
import { createConcurrencyLimiter } from "@/utils/promisePool";
import { epochMs, type EpochMs } from "@/utils/timeWindow";
import { slugify } from "./parse";

export interface TeamEntry {
  readonly id: number;
  readonly name: string;
  readonly slug: string;
}

export interface SourceEntry {
  readonly id: number;
  readonly teamId: number;
  readonly name: string;
  readonly slug: string;
  readonly typeLabel: string;
  readonly connected: boolean;
  readonly logchefql: boolean;
}

export interface SavedQueryEntry {
  readonly id: number;
  readonly name: string;
  readonly sourceName: string | null;
}

export interface DashboardEntry {
  readonly id: number;
  readonly name: string;
}

export interface AlertEntry {
  readonly id: number;
  readonly name: string;
  readonly sourceId: number;
  readonly firing: boolean;
}

export interface CollectionEntry {
  readonly id: number;
  readonly name: string;
  readonly personal: boolean;
}

/** `stale` keeps the last good value visible while a refresh runs or after it fails. */
export type Loadable<T> =
  | { readonly state: "idle" }
  | { readonly state: "loading"; readonly stale: T | null }
  | { readonly state: "ready"; readonly value: T; readonly at: EpochMs }
  | { readonly state: "failed"; readonly stale: T | null };

export type EntityList = "savedQueries" | "dashboards" | "alerts" | "collections";
export type ListName = "teams" | EntityList | { readonly teamSources: number };

export interface CatalogSnapshot {
  readonly teams: Loadable<readonly TeamEntry[]>;
  readonly sourcesByTeam: ReadonlyMap<number, Loadable<readonly SourceEntry[]>>;
  readonly savedQueries: Loadable<readonly SavedQueryEntry[]>;
  readonly dashboards: Loadable<readonly DashboardEntry[]>;
  /** Stays idle when alerts are disabled. */
  readonly alerts: Loadable<readonly AlertEntry[]>;
  readonly collections: Loadable<readonly CollectionEntry[]>;
}

/** The value to show now: fresh, or stale while refreshing or after a failure. */
export function current<T>(list: Loadable<T>): T | null {
  switch (list.state) {
    case "ready":
      return list.value;
    case "loading":
    case "failed":
      return list.stale;
    case "idle":
      return null;
  }
}

export const LIMITS = {
  timeoutSeconds: 8,
  freshForMs: 60_000,
  concurrency: 4,
  /** Teams whose sources load on open. The rest load when a tag names them. */
  eagerTeams: 24,
  maxItems: 2_000,
} as const;

export interface OpenOptions {
  readonly userId: string;
  readonly isAdmin: boolean;
  readonly alertsEnabled: boolean;
  /** Teams whose sources load first: the current team, then the teams of recent sources. */
  readonly priorityTeams: readonly number[];
}

/** The only owner of palette requests. It never uses Pinia store slots, so it cannot abort Explore's requests. */
export interface Catalog {
  readonly snapshot: Readonly<ShallowRef<CatalogSnapshot>>;
  /** Fetches each list that is missing, failed or older than freshForMs. Safe to call again while loading. */
  open(options: OpenOptions): void;
  /** Aborts what is in flight; those lists go back to what they showed before. */
  close(): void;
  ensureTeamSources(teamId: number): void;
  retry(list: ListName): void;
  clear(): void;
}

const IDLE = { state: "idle" } as const;

function emptySnapshot(): CatalogSnapshot {
  return { teams: IDLE, sourcesByTeam: new Map(), savedQueries: IDLE, dashboards: IDLE, alerts: IDLE, collections: IDLE };
}

function parseList<Raw, Entry>(response: APIResponse<Raw[]>, toEntry: (raw: Raw) => Entry): Entry[] | null {
  if (response.status !== "success" || !Array.isArray(response.data)) return null;
  return response.data.slice(0, LIMITS.maxItems).map(toEntry);
}

const toTeam = (team: { id: number; name: string }): TeamEntry => ({ id: team.id, name: team.name, slug: slugify(team.name) || String(team.id) });

function toSource(teamId: number) {
  return (source: Source): SourceEntry => ({
    id: source.id,
    teamId,
    name: source.name,
    slug: slugify(source.name) || String(source.id),
    typeLabel: getSourceTypeLabel(source),
    connected: source.is_connected,
    logchefql: supportsQueryLanguage(source, "logchefql"),
  });
}

type EntityValues = {
  savedQueries: readonly SavedQueryEntry[];
  dashboards: readonly DashboardEntry[];
  alerts: readonly AlertEntry[];
  collections: readonly CollectionEntry[];
};

const ENTITY_FETCHERS: { [K in EntityList]: (options: RequestOptions) => Promise<EntityValues[K] | null> } = {
  savedQueries: async (options) =>
    parseList(await savedQueriesApi.list(undefined, options), (query) => ({ id: query.id, name: query.name, sourceName: query.source_name ?? null })),
  dashboards: async (options) => parseList(await dashboardsApi.list(options), (dashboard) => ({ id: dashboard.id, name: dashboard.name })),
  alerts: async (options) =>
    parseList(await alertsApi.list(undefined, options), (alert) => ({ id: alert.id, name: alert.name, sourceId: alert.source_id, firing: alert.last_state === "firing" })),
  collections: async (options) =>
    parseList(await collectionsApi.list(options), (collection) => ({ id: collection.id, name: collection.name, personal: collection.is_personal })),
};

const ENTITY_LISTS: readonly EntityList[] = ["savedQueries", "dashboards", "alerts", "collections"];

function needsRefresh(list: Loadable<unknown> | undefined, now: number): boolean {
  if (!list) return true;
  switch (list.state) {
    case "idle":
    case "failed":
      return true;
    case "loading":
      return false;
    case "ready":
      return now - list.at > LIMITS.freshForMs;
  }
}

interface Slot<T> {
  readonly key: string;
  get(): Loadable<T> | undefined;
  set(next: Loadable<T>): void;
}

export function createCatalog(): Catalog {
  const snapshot = shallowRef<CatalogSnapshot>(emptySnapshot());
  const run = createConcurrencyLimiter(LIMITS.concurrency);
  let userId: string | null = null;
  let options: OpenOptions | null = null;
  let controller: AbortController | null = null;
  let generation = 0;
  const reverts = new Map<string, () => void>();

  const teamsSlot: Slot<readonly TeamEntry[]> = {
    key: "teams",
    get: () => snapshot.value.teams,
    set: (teams) => { snapshot.value = { ...snapshot.value, teams }; },
  };
  const entitySlots: { [K in EntityList]: Slot<EntityValues[K]> } = {
    savedQueries: { key: "savedQueries", get: () => snapshot.value.savedQueries, set: (savedQueries) => { snapshot.value = { ...snapshot.value, savedQueries }; } },
    dashboards: { key: "dashboards", get: () => snapshot.value.dashboards, set: (dashboards) => { snapshot.value = { ...snapshot.value, dashboards }; } },
    alerts: { key: "alerts", get: () => snapshot.value.alerts, set: (alerts) => { snapshot.value = { ...snapshot.value, alerts }; } },
    collections: { key: "collections", get: () => snapshot.value.collections, set: (collections) => { snapshot.value = { ...snapshot.value, collections }; } },
  };
  const sourcesSlot = (teamId: number): Slot<readonly SourceEntry[]> => ({
    key: `sources:${teamId}`,
    get: () => snapshot.value.sourcesByTeam.get(teamId),
    set: (next) => { snapshot.value = { ...snapshot.value, sourcesByTeam: new Map(snapshot.value.sourcesByTeam).set(teamId, next) }; },
  });

  async function load<T>(slot: Slot<T>, fetch: (request: RequestOptions) => Promise<T | null>): Promise<boolean> {
    const active = controller;
    const previous = slot.get();
    if (!active || !needsRefresh(previous, Date.now())) return false;
    const stale = previous ? current(previous) : null;
    const loadGeneration = generation;
    reverts.set(slot.key, () => slot.set(previous ?? IDLE));
    slot.set({ state: "loading", stale });
    const request: RequestOptions = { timeout: LIMITS.timeoutSeconds, signal: active.signal, suppressErrorToast: true, suppressForbiddenRedirect: true };
    try {
      const value = await run(async () => (loadGeneration === generation ? fetch(request) : null));
      if (loadGeneration !== generation) return false;
      slot.set(value === null ? { state: "failed", stale } : { state: "ready", value, at: epochMs(Date.now()) });
      return value !== null;
    } catch (error) {
      if (loadGeneration !== generation) return false;
      slot.set(isAbortError(error) ? previous ?? IDLE : { state: "failed", stale });
      return false;
    } finally {
      if (loadGeneration === generation) reverts.delete(slot.key);
    }
  }

  async function fetchTeams(request: RequestOptions): Promise<TeamEntry[] | null> {
    return parseList(await (options?.isAdmin ? teamsApi.listAllTeams(request) : teamsApi.listUserTeams(request)), toTeam);
  }

  function loadTeamSources(teamId: number) {
    void load(sourcesSlot(teamId), async (request) => parseList(await teamsApi.listTeamSources(teamId, request), toSource(teamId)));
  }

  function loadEagerSources() {
    const teams = current(snapshot.value.teams);
    if (!teams) return;
    const priority = [...new Set(options?.priorityTeams ?? [])];
    const ordered = [
      ...priority.flatMap((id) => teams.filter((team) => team.id === id)),
      ...teams.filter((team) => !priority.includes(team.id)),
    ];
    for (const team of ordered.slice(0, LIMITS.eagerTeams)) loadTeamSources(team.id);
  }

  async function loadTeams() {
    await load(teamsSlot, fetchTeams);
    loadEagerSources();
  }

  function loadEntity(list: EntityList) {
    if (list === "alerts" && !options?.alertsEnabled) return;
    switch (list) {
      case "savedQueries":
        return void load(entitySlots.savedQueries, ENTITY_FETCHERS.savedQueries);
      case "dashboards":
        return void load(entitySlots.dashboards, ENTITY_FETCHERS.dashboards);
      case "alerts":
        return void load(entitySlots.alerts, ENTITY_FETCHERS.alerts);
      case "collections":
        return void load(entitySlots.collections, ENTITY_FETCHERS.collections);
    }
  }

  function close() {
    if (!controller) return;
    generation++;
    controller.abort();
    controller = null;
    for (const revert of reverts.values()) revert();
    reverts.clear();
  }

  function clear() {
    close();
    generation++;
    userId = null;
    options = null;
    snapshot.value = emptySnapshot();
  }

  return {
    snapshot,
    open(next) {
      if (userId !== next.userId) {
        clear();
        userId = next.userId;
      }
      options = next;
      controller ??= new AbortController();
      void loadTeams();
      for (const list of ENTITY_LISTS) loadEntity(list);
    },
    close,
    ensureTeamSources(teamId) {
      loadTeamSources(teamId);
    },
    retry(list) {
      if (list === "teams") void loadTeams();
      else if (typeof list === "string") loadEntity(list);
      else loadTeamSources(list.teamSources);
    },
    clear,
  };
}
