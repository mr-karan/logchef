import type { ExplorerAskContext } from "@/stores/askChef";
import { recognizeLogchefQL } from "./implicitQuery";
import { encodeWindowForUrl } from "@/utils/timeWindowCodec";
import { askItem, strongNavigationMatch } from "./ask";
import type { RouteLocationRaw } from "vue-router";
import { EXPLORE_PATH } from "@/lib/exploreLocation";
import { QUICK_RANGES } from "@/lib/quickRanges";
import type { RecentSource } from "@/lib/recentSources";
import { meetsRequirements, type Viewer } from "@/router/access";
import type { ThemeMode } from "@/stores/theme";
import {
  formatInZone,
  isSameZonedDay,
  relativeWindow,
  resolveWindow,
  windowLabel,
  type EpochMs,
  type TimeWindow,
  type ZoneId,
} from "@/utils/timeWindow";
import {
  current,
  type CatalogSnapshot,
  type EntityList,
  type ListName,
  type Loadable,
  type SourceEntry,
  type TeamEntry,
} from "./catalog";
import { TEXT_SEARCH_LIMITS } from "./textSearch";
import type { PaletteIcon } from "./icons";
import type { PalettePage } from "./pages";
import {
  applyCompletion,
  lexQuery,
  parsePaletteInput,
  type CompletionSlot,
  type InputEdit,
  type ParsedInput,
  type RelativeWindow,
  type Span,
  type TagToken,
  type Token,
} from "./parse";

export type Translate = (key: string, named?: Record<string, unknown>, plural?: number) => string;

export interface PaletteContext {
  readonly schemaFields: ReadonlySet<string>;
  readonly askChefEnabled: boolean;
  readonly explorer: { readonly context: ExplorerAskContext; readonly window: TimeWindow; readonly hasResults: boolean } | null;
  readonly viewer: Viewer;
  readonly routeName: string | null;
  readonly current: { readonly teamId: number; readonly sourceId: number } | null;
  readonly recents: readonly RecentSource[];
  readonly theme: ThemeMode;
  readonly timezone: "utc" | "local";
  readonly zone: ZoneId;
  readonly now: EpochMs;
  readonly locale: string;
  readonly pages: readonly PalettePage[];
  readonly t: Translate;
}

export interface TextSearchTarget {
  readonly teamId: number;
  readonly sourceId: number;
  readonly text: string;
  readonly range: string;
}

export type PaletteEffect =
  | { readonly kind: "askChef"; readonly context: ExplorerAskContext; readonly question: string | null; readonly navigation: "current" | "scoped" }
  | { readonly kind: "investigateAlert"; readonly alertId: number; readonly teamId: number | null }
  | { readonly kind: "searchText"; readonly target: TextSearchTarget }
  | { readonly kind: "navigate"; readonly to: RouteLocationRaw }
  | { readonly kind: "edit"; readonly edit: InputEdit }
  | { readonly kind: "setTheme"; readonly mode: ThemeMode }
  | { readonly kind: "setTimezone"; readonly timezone: "utc" | "local" }
  | { readonly kind: "copyLink" }
  | { readonly kind: "openDocs" }
  | { readonly kind: "signOut" }
  | { readonly kind: "retry"; readonly list: ListName };

export type GroupId =
  | "run"
  | "ask"
  | "recent"
  | "suggested"
  | "goto"
  | "pages"
  | "actions"
  | "sources"
  | "teams"
  | "ranges"
  | EntityList
  | "fallback";

export type KeyName = "up" | "down" | "enter" | "modEnter" | "modShiftEnter" | "tab" | "esc";

/** Text plus the ranges to emphasize (fuzzy hits) and to mute (a team prefix, the "@"). */
export interface MarkedText {
  readonly text: string;
  readonly marks: readonly Span[];
  readonly dim: readonly Span[];
}

export interface ExploreRun {
  readonly team: TeamEntry;
  readonly source: SourceEntry;
  readonly query: string;
  readonly queryTokens: readonly Token[];
  readonly window: TimeWindow;
  readonly rangeLabel: string;
  /** Concrete from/to in the user's zone, such as "09:30 to 10:30". */
  readonly span: string;
}

interface ItemBase {
  /** Stable across renders so the highlight survives refetches. */
  readonly id: string;
  /** null: shown but not choosable. */
  readonly enter: PaletteEffect | null;
  readonly tab: InputEdit | null;
}

export type PaletteItem =
  | (ItemBase & {
      readonly shape: "row";
      readonly icon: PaletteIcon;
      readonly label: MarkedText;
      readonly detail: string | null;
      readonly mono: boolean;
      readonly meta: string | null;
      readonly badge: "admin" | null;
      readonly shortcut: KeyName | null;
    })
  | (ItemBase & { readonly shape: "run"; readonly run: ExploreRun })
  | (ItemBase & { readonly shape: "blocked"; readonly icon: PaletteIcon; readonly label: string; readonly reason: string })
  | (ItemBase & { readonly shape: "status"; readonly state: "loading" | "failed"; readonly label: string });

export interface PaletteGroup {
  readonly id: GroupId;
  readonly heading: string | null;
  readonly count: string | null;
  readonly items: readonly PaletteItem[];
}

export interface FooterHint {
  readonly keys: readonly KeyName[];
  readonly label: string;
  readonly detail: string | null;
}

export type FooterStatus =
  | { readonly kind: "syntax" }
  | { readonly kind: "completing"; readonly label: string; readonly syntax: string }
  | { readonly kind: "text"; readonly text: string };

export interface PaletteView {
  readonly mode: "empty" | "search" | "query" | "ask";
  readonly askEnter: PaletteEffect | null;
  /** Painted under the input in query mode; empty otherwise. */
  readonly tokens: readonly Token[];
  /** The completion's missing suffix, shown after the caret. */
  readonly ghost: { readonly text: string; readonly edit: InputEdit } | null;
  readonly groups: readonly PaletteGroup[];
  /** Cmd/Ctrl+Enter: the fallback in search mode, the run in query mode. */
  readonly modEnter: PaletteEffect | null;
  /** Teams whose sources this view needs and nobody has asked for yet. */
  readonly needsTeamSources: readonly number[];
  readonly footer: { readonly hints: readonly FooterHint[]; readonly status: FooterStatus };
}

const DEFAULT_WINDOW: RelativeWindow = { kind: "relative", spec: "15m" };
const RANGE_SPECS: readonly string[] = [...QUICK_RANGES, "today", "yesterday"];
const MAX_RECENTS = 5;
const MAX_GROUP_ROWS = 8;
const MAX_COMPLETIONS = 12;

export interface FuzzyMatch {
  readonly score: number;
  readonly marks: readonly Span[];
}

const WORD_BREAK = /[\s_\-./@:]/;
const TIER = { exact: 5000, prefix: 4000, wordStart: 3000, substring: 2000, subsequence: 1000 } as const;

/** exact > prefix > word-start prefix > substring > ordered subsequence; gaps and later starts cost. */
export function fuzzyMatch(query: string, text: string): FuzzyMatch | null {
  const needle = query.toLowerCase();
  const haystack = text.toLowerCase();
  if (!needle) return { score: 0, marks: [] };
  const run = (start: number) => [{ start, end: start + needle.length }];
  if (haystack === needle) return { score: TIER.exact, marks: run(0) };
  if (haystack.startsWith(needle)) return { score: TIER.prefix, marks: run(0) };
  for (let at = haystack.indexOf(needle, 1); at > 0; at = haystack.indexOf(needle, at + 1)) {
    if (WORD_BREAK.test(haystack[at - 1])) return { score: TIER.wordStart - at, marks: run(at) };
  }
  const at = haystack.indexOf(needle);
  if (at >= 0) return { score: TIER.substring - at, marks: run(at) };

  const marks: Span[] = [];
  let from = 0;
  let gaps = 0;
  for (const char of needle) {
    const found = haystack.indexOf(char, from);
    if (found < 0) return null;
    if (marks.length > 0) gaps += found - from;
    const last = marks[marks.length - 1];
    if (last && last.end === found) marks[marks.length - 1] = { start: last.start, end: found + 1 };
    else marks.push({ start: found, end: found + 1 });
    from = found + 1;
  }
  return { score: Math.max(1, TIER.subsequence - gaps * 10 - marks[0].start), marks };
}

function contiguous(match: FuzzyMatch | null): FuzzyMatch | null {
  return match && (match.score === 0 || match.score > TIER.subsequence) ? match : null;
}

function mergeSpans(spans: readonly Span[]): Span[] {
  const sorted = [...spans].sort((a, b) => a.start - b.start);
  const merged: Span[] = [];
  for (const span of sorted) {
    const last = merged[merged.length - 1];
    if (last && span.start <= last.end) merged[merged.length - 1] = { start: last.start, end: Math.max(last.end, span.end) };
    else merged.push(span);
  }
  return merged;
}

function matchWords(words: readonly string[], text: string): FuzzyMatch | null {
  let score = 0;
  const marks: Span[] = [];
  for (const word of words) {
    const match = fuzzyMatch(word, text);
    if (!match) return null;
    score += match.score;
    marks.push(...match.marks);
  }
  return { score, marks: mergeSpans(marks) };
}

interface Ranked {
  readonly item: PaletteItem;
  readonly score: number;
  /** Current team or source first. */
  readonly boost: number;
  /** Epoch ms of the last visit; 0 when never visited. */
  readonly recency: number;
  readonly label: string;
  readonly key: string;
}

function byRank(a: Ranked, b: Ranked): number {
  return b.score - a.score || b.boost - a.boost || b.recency - a.recency || a.label.localeCompare(b.label) || a.key.localeCompare(b.key);
}

interface Index {
  readonly teams: readonly TeamEntry[];
  readonly teamById: ReadonlyMap<number, TeamEntry>;
  /** null: not loaded and nothing stale to show. */
  sourcesOf(teamId: number): readonly SourceEntry[] | null;
  readonly sources: readonly SourceEntry[];
  sourceLabel(teamId: number, sourceId: number): string | null;
}

function indexCatalog(catalog: CatalogSnapshot): Index {
  const teams = current(catalog.teams) ?? [];
  const teamById = new Map(teams.map((team) => [team.id, team]));
  const sourcesOf = (teamId: number) => {
    const list = catalog.sourcesByTeam.get(teamId);
    return list ? current(list) : null;
  };
  const sources = teams.flatMap((team) => sourcesOf(team.id) ?? []);
  return {
    teams,
    teamById,
    sourcesOf,
    sources,
    sourceLabel(teamId, sourceId) {
      const team = teamById.get(teamId);
      const source = sourcesOf(teamId)?.find((entry) => entry.id === sourceId);
      return team && source ? `${team.name}/${source.name}` : null;
    },
  };
}

function needsLoad(list: Loadable<unknown> | undefined): boolean {
  return list === undefined || list.state === "idle";
}

function exploreRunLocation(run: Pick<ExploreRun, "team" | "source" | "query" | "window">): RouteLocationRaw {
  return {
    path: EXPLORE_PATH,
    query: { team: String(run.team.id), source: String(run.source.id), mode: "logchefql", q: run.query, ...encodeWindowForUrl(run.window) },
  };
}

function openSource(teamId: number, sourceId: number): PaletteEffect {
  return { kind: "navigate", to: { path: EXPLORE_PATH, query: { team: String(teamId), source: String(sourceId) } } };
}

function rangeLabel(window: TimeWindow, t: Translate): string {
  if (window.kind === "absolute") return t("time.absolute");
  const label = windowLabel(window);
  if (label) return t(label.key, { count: label.count }, label.count);
  return window.kind === "relative" && window.spec === "yesterday" ? t("palette.range.yesterday") : t("palette.range.today");
}

function rangeSpan(window: TimeWindow, ctx: PaletteContext): string {
  const { start, end, zone } = resolveWindow(window, ctx.zone, ctx.now);
  const time: Intl.DateTimeFormatOptions = { hour: "2-digit", minute: "2-digit", hourCycle: "h23" };
  const withDate: Intl.DateTimeFormatOptions = { ...time, month: "short", day: "numeric" };
  const options = isSameZonedDay(start, end, zone) ? time : withDate;
  return ctx.t("time.range", { from: formatInZone(start, zone, options, ctx.locale), to: formatInZone(end, zone, options, ctx.locale) });
}

function exploreRun(team: TeamEntry, source: SourceEntry, query: string, window: TimeWindow, ctx: PaletteContext): ExploreRun {
  return { team, source, query, queryTokens: lexQuery(query), window, rangeLabel: rangeLabel(window, ctx.t), span: rangeSpan(window, ctx) };
}

function timeAgo(at: number, ctx: PaletteContext): string {
  const format = new Intl.RelativeTimeFormat(ctx.locale, { numeric: "auto", style: "narrow" });
  const seconds = Math.max(0, (ctx.now - at) / 1000);
  if (seconds < 60) return format.format(0, "second");
  if (seconds < 3600) return format.format(-Math.floor(seconds / 60), "minute");
  if (seconds < 86_400) return format.format(-Math.floor(seconds / 3600), "hour");
  return format.format(-Math.floor(seconds / 86_400), "day");
}

const plain = (text: string): MarkedText => ({ text, marks: [], dim: [] });

function sourceDetail(team: TeamEntry, source: SourceEntry, index: Index): string {
  const duplicate = (index.sourcesOf(team.id) ?? []).filter(entry => entry.slug === source.slug).length > 1;
  return `${source.typeLabel}${duplicate ? ` · #${source.id}` : ""}`;
}

function sourceText(team: TeamEntry, source: SourceEntry, marks: readonly Span[] = []): MarkedText {
  return { text: `${team.name}/${source.name}`, marks, dim: [{ start: 0, end: team.name.length + 1 }] };
}

function row(
  id: string,
  icon: PaletteIcon,
  label: MarkedText,
  enter: PaletteEffect | null,
  extra: Partial<Pick<Extract<PaletteItem, { shape: "row" }>, "detail" | "mono" | "meta" | "badge" | "shortcut" | "tab">> = {},
): PaletteItem {
  return {
    shape: "row", id, icon, label, enter,
    tab: extra.tab ?? null, detail: extra.detail ?? null, mono: extra.mono ?? false,
    meta: extra.meta ?? null, badge: extra.badge ?? null, shortcut: extra.shortcut ?? null,
  };
}

function blocked(id: string, icon: PaletteIcon, label: string, reason: string, tab: InputEdit | null = null): PaletteItem {
  return { shape: "blocked", id, icon, label, reason, enter: null, tab };
}

function status(id: string, state: "loading" | "failed", label: string, list: ListName): PaletteItem {
  return { shape: "status", id, state, label, enter: state === "failed" ? { kind: "retry", list } : null, tab: null };
}

function group(id: GroupId, heading: string | null, items: readonly PaletteItem[], count: string | null = null): PaletteGroup {
  return { id, heading, count, items };
}

type ActionId = "newQuery" | "addAlert" | "inspectSource" | "toggleTheme" | "switchTimezone" | "copyLink" | "openDocs" | "signOut";

interface PaletteAction {
  readonly id: ActionId;
  readonly icon: PaletteIcon;
  readonly suggested: boolean;
  label(ctx: PaletteContext, here: string): string;
  /** null: not available here. */
  effect(ctx: PaletteContext): PaletteEffect | null;
}

const NEXT_THEME: Record<ThemeMode, ThemeMode> = { light: "dark", dark: "auto", auto: "light" };
const THEME_LABEL: Record<ThemeMode, string> = { light: "ui.light", dark: "ui.dark", auto: "ui.system" };

function onExplore(ctx: PaletteContext): { teamId: number; sourceId: number } | null {
  return ctx.routeName === "LogExplorer" ? ctx.current : null;
}

const ACTIONS: readonly PaletteAction[] = [
  {
    id: "newQuery", icon: "newQuery", suggested: true,
    label: (ctx, here) => ctx.t("palette.action.newQuery", { source: here }),
    effect: (ctx) => {
      const at = onExplore(ctx);
      if (!at) return null;
      return { kind: "navigate", to: { path: EXPLORE_PATH, query: { team: String(at.teamId), source: String(at.sourceId), mode: "logchefql", q: "" } } };
    },
  },
  {
    id: "addAlert", icon: "newAlert", suggested: true,
    label: (ctx, here) => ctx.t("palette.action.addAlert", { source: here }),
    effect: (ctx) => {
      const at = onExplore(ctx);
      if (!at || !ctx.viewer.alertsEnabled) return null;
      return { kind: "navigate", to: { name: "AlertCreate", query: { team: String(at.teamId), source: String(at.sourceId) } } };
    },
  },
  {
    id: "inspectSource", icon: "inspectSource", suggested: true,
    label: (ctx, here) => ctx.t("palette.action.inspectSource", { source: here }),
    effect: (ctx) => {
      const at = onExplore(ctx);
      if (!at || !ctx.viewer.isAdmin) return null;
      return { kind: "navigate", to: { name: "SourceInspection", query: { sourceId: String(at.sourceId) } } };
    },
  },
  {
    id: "toggleTheme", icon: "theme", suggested: true,
    label: (ctx) => ctx.t("palette.action.theme", { mode: ctx.t(THEME_LABEL[NEXT_THEME[ctx.theme]]) }),
    effect: (ctx) => ({ kind: "setTheme", mode: NEXT_THEME[ctx.theme] }),
  },
  {
    id: "switchTimezone", icon: "timezone", suggested: false,
    label: (ctx) => ctx.t(ctx.timezone === "utc" ? "palette.action.useLocalTime" : "palette.action.useUtc"),
    effect: (ctx) => ({ kind: "setTimezone", timezone: ctx.timezone === "utc" ? "local" : "utc" }),
  },
  { id: "copyLink", icon: "link", suggested: false, label: (ctx) => ctx.t("palette.action.copyLink"), effect: () => ({ kind: "copyLink" }) },
  { id: "openDocs", icon: "docs", suggested: false, label: (ctx) => ctx.t("palette.action.docs"), effect: () => ({ kind: "openDocs" }) },
  { id: "signOut", icon: "signOut", suggested: false, label: (ctx) => ctx.t("palette.action.signOut"), effect: () => ({ kind: "signOut" }) },
];

function availableActions(ctx: PaletteContext, index: Index): { action: PaletteAction; label: string; effect: PaletteEffect }[] {
  const here = ctx.current ? index.sourceLabel(ctx.current.teamId, ctx.current.sourceId) ?? ctx.t("palette.currentSource") : "";
  return ACTIONS.flatMap((action) => {
    const effect = action.effect(ctx);
    return effect ? [{ action, label: action.label(ctx, here), effect }] : [];
  });
}

function visiblePages(ctx: PaletteContext): PalettePage[] {
  return ctx.pages.filter((page) => meetsRequirements(page.requirements, ctx.viewer));
}

function pageEffect(page: PalettePage, ctx: PaletteContext): PaletteEffect {
  if (page.palette.contextual && ctx.current) {
    return { kind: "navigate", to: { name: page.name, query: { team: String(ctx.current.teamId), source: String(ctx.current.sourceId) } } };
  }
  return { kind: "navigate", to: { name: page.name } };
}

function pageRow(page: PalettePage, ctx: PaletteContext, marks: readonly Span[] = []): PaletteItem {
  return row(`page:${page.name}`, page.palette.icon, { text: ctx.t(page.titleKey), marks, dim: [] }, pageEffect(page, ctx), {
    detail: page.path,
    badge: page.requirements.includes("admin") ? "admin" : null,
  });
}

function recentGroup(ctx: PaletteContext, catalog: CatalogSnapshot, index: Index): { group: PaletteGroup | null; needs: number[] } {
  const items: PaletteItem[] = [];
  const needs: number[] = [];
  let pending = false;
  for (const recent of ctx.recents) {
    if (items.length >= MAX_RECENTS) break;
    const team = index.teamById.get(recent.teamId);
    if (!team) {
      pending ||= catalog.teams.state === "loading" || catalog.teams.state === "idle";
      continue;
    }
    const sources = index.sourcesOf(team.id);
    if (!sources) {
      const list = catalog.sourcesByTeam.get(team.id);
      if (needsLoad(list)) needs.push(team.id);
      pending ||= list?.state !== "failed";
      continue;
    }
    const source = sources.find((entry) => entry.id === recent.sourceId);
    if (!source) continue;
    items.push(row(`recent:${team.id}:${source.id}`, "sources", sourceText(team, source), openSource(team.id, source.id), {
      detail: sourceDetail(team, source, index),
      meta: timeAgo(recent.at, ctx),
    }));
  }
  if (pending && items.length < MAX_RECENTS) items.push(status("status:recent", "loading", ctx.t("palette.loading.recent"), "teams"));
  return { group: items.length > 0 ? group("recent", ctx.t("palette.group.recent"), items) : null, needs };
}

function emptyView(ctx: PaletteContext, catalog: CatalogSnapshot, index: Index): PaletteView {
  const recent = recentGroup(ctx, catalog, index);
  const suggested = availableActions(ctx, index)
    .filter(({ action }) => action.suggested)
    .map(({ action, label, effect }) => row(`action:${action.id}`, action.icon, plain(label), effect));
  if (ctx.askChefEnabled && onExplore(ctx) && ctx.explorer?.hasResults) suggested.unshift(row("ask:results", "askChef", plain(ctx.t("palette.ask.results")), { kind: "askChef", context: ctx.explorer.context, question: null, navigation: "current" }));
  const groups = [
    recent.group,
    group("suggested", ctx.t("palette.group.suggested"), suggested),
    group("goto", ctx.t("palette.group.goto"), visiblePages(ctx).map((page) => pageRow(page, ctx))),
  ].flatMap((entry) => (entry && entry.items.length > 0 ? [entry] : []));
  return {
    askEnter: null, mode: "empty", tokens: [], ghost: null, groups, modEnter: null, needsTeamSources: recent.needs,
    footer: {
      hints: [hint(["up", "down"], ctx.t("palette.footer.navigate")), hint(["enter"], ctx.t("palette.footer.open")), hint(["esc"], ctx.t("palette.footer.close"))],
      status: { kind: "syntax" },
    },
  };
}

function hint(keys: readonly KeyName[], label: string, detail: string | null = null): FooterHint {
  return { keys, label, detail };
}

const GROUP_PRIORITY: readonly GroupId[] = ["pages", "actions", "sources", "savedQueries", "dashboards", "alerts", "collections"];

interface SearchGroup {
  readonly id: GroupId;
  readonly ranked: Ranked[];
  readonly statusRows: PaletteItem[];
  readonly count?: string;
}

function listStatus(id: EntityList | "teams", list: Loadable<unknown>, ctx: PaletteContext): PaletteItem[] {
  if (list.state === "failed" && list.stale === null) return [status(`status:${id}`, "failed", ctx.t(`palette.failed.${id}`), id)];
  if (list.state === "loading" && list.stale === null) return [status(`status:${id}`, "loading", ctx.t(`palette.loading.${id}`), id)];
  return [];
}

function rank(item: PaletteItem, label: string, match: FuzzyMatch, extra: Partial<Pick<Ranked, "boost" | "recency">> = {}): Ranked {
  return { item, score: match.score, boost: extra.boost ?? 0, recency: extra.recency ?? 0, label, key: item.id };
}

function recencyOf(ctx: PaletteContext, teamId: number, sourceId: number): number {
  return ctx.recents.find((recent) => recent.teamId === teamId && recent.sourceId === sourceId)?.at ?? 0;
}

function searchSources(words: readonly string[], ctx: PaletteContext, catalog: CatalogSnapshot, index: Index): SearchGroup {
  const ranked: Ranked[] = [];
  for (const team of index.teams) {
    for (const source of index.sourcesOf(team.id) ?? []) {
      const label = sourceText(team, source);
      const match = matchWords(words, label.text);
      if (!match) continue;
      const isCurrent = ctx.current?.teamId === team.id && ctx.current.sourceId === source.id;
      ranked.push(rank(
        row(`source:${team.id}:${source.id}`, "sources", sourceText(team, source, match.marks), openSource(team.id, source.id), { detail: sourceDetail(team, source, index) }),
        label.text, match, { boost: isCurrent ? 1 : 0, recency: recencyOf(ctx, team.id, source.id) },
      ));
    }
  }
  const statusRows = listStatus("teams", catalog.teams, ctx);
  for (const team of index.teams) {
    const list = catalog.sourcesByTeam.get(team.id);
    if (list?.state === "failed" && list.stale === null) {
      statusRows.push(status(`status:sources:${team.id}`, "failed", ctx.t("palette.failed.sources", { team: team.name }), { teamSources: team.id }));
    }
  }
  const loaded = index.teams.filter((team) => index.sourcesOf(team.id) !== null).length;
  const partial = loaded < index.teams.length ? ctx.t("palette.sources.partial", { loaded, total: index.teams.length }) : undefined;
  return { id: "sources", ranked, statusRows, count: partial };
}

function searchEntities(words: readonly string[], ctx: PaletteContext, catalog: CatalogSnapshot, index: Index): SearchGroup[] {
  const groups: SearchGroup[] = [];
  const add = <T>(id: EntityList, list: Loadable<readonly T[]>, toRow: (entry: T, marks: readonly Span[]) => { item: PaletteItem; label: string }) => {
    const ranked: Ranked[] = [];
    for (const entry of current(list) ?? []) {
      const { label } = toRow(entry, []);
      const match = matchWords(words, label);
      if (match) ranked.push(rank(toRow(entry, match.marks).item, label, match));
    }
    groups.push({ id, ranked, statusRows: listStatus(id, list, ctx) });
  };
  add("savedQueries", catalog.savedQueries, (query, marks) => ({
    label: query.name,
    item: row(`savedQuery:${query.id}`, "savedQuery", { text: query.name, marks, dim: [] },
      { kind: "navigate", to: { name: "SavedQueryRedirect", params: { queryId: String(query.id) } } }, { detail: query.sourceName }),
  }));
  add("dashboards", catalog.dashboards, (dashboard, marks) => ({
    label: dashboard.name,
    item: row(`dashboard:${dashboard.id}`, "dashboards", { text: dashboard.name, marks, dim: [] },
      { kind: "navigate", to: { name: "DashboardView", params: { id: String(dashboard.id) } } }),
  }));
  if (ctx.viewer.alertsEnabled) {
    add("alerts", catalog.alerts, (alert, marks) => {
      const source = index.sources.find(entry => entry.id === alert.sourceId && entry.teamId === ctx.current?.teamId)
        ?? index.sources.find(entry => entry.id === alert.sourceId);
      return {
        label: alert.name,
        item: row(`alert:${alert.id}`, "alerts", { text: alert.name, marks, dim: [] },
          { kind: "navigate", to: { name: "AlertDetail", params: { alertID: String(alert.id) }, ...(source ? { query: { team: String(source.teamId), source: String(source.id) } } : {}) } },
          { detail: source ? index.sourceLabel(source.teamId, source.id) : null, meta: alert.firing ? ctx.t("alerts.stateFiring") : null }),
      };
    });
  }
  add("collections", catalog.collections, (collection, marks) => ({
    label: collection.name,
    item: row(`collection:${collection.id}`, "collection", { text: collection.name, marks, dim: [] },
      { kind: "navigate", to: { name: "LibraryCollection", params: { collectionID: String(collection.id) } } },
      { detail: ctx.t(collection.personal ? "palette.collection.personal" : "palette.collection.shared") }),
  }));
  return groups;
}

function fallbackItem(parsed: ParsedInput, ctx: PaletteContext, catalog: CatalogSnapshot, index: Index): PaletteItem {
  const label = ctx.t("palette.fallback.title");
  const resolve = (teamId: number, sourceId: number) => {
    const team = index.teamById.get(teamId);
    const source = index.sourcesOf(teamId)?.find((entry) => entry.id === sourceId);
    return team && source ? { team, source } : null;
  };
  const target = ctx.current ? resolve(ctx.current.teamId, ctx.current.sourceId)
    : ctx.recents.map(recent => resolve(recent.teamId, recent.sourceId)).find(target => target !== null) ?? null;
  if (ctx.current && !target) {
    const { teamId, sourceId } = ctx.current;
    const team = index.teamById.get(teamId);
    const list = team ? catalog.sourcesByTeam.get(teamId) : catalog.teams;
    if (!list || list.state !== "ready") {
      const failed = list?.state === "failed";
      return status("fallback", failed ? "failed" : "loading",
        `${ctx.t(failed ? "palette.failed.sources" : "palette.loading.tag", { team: team?.name ?? String(teamId), slug: `@${teamId}/${sourceId}` })}${failed ? ` · @${teamId}/${sourceId}` : ""}`,
        team ? { teamSources: teamId } : "teams");
    }
  }
  const problem = runProblem(parsed, target?.source ?? null, ctx)
    ?? (target ? null : ctx.current
      ? ctx.t("palette.problem.unknownSource", { slug: String(ctx.current.sourceId), team: index.teamById.get(ctx.current.teamId)?.name ?? String(ctx.current.teamId) })
      : ctx.t("palette.fallback.noSource", { syntax: "@team/source" }));
  if (problem || !target) return blocked("fallback", "searchLogs", label, problem ?? "");
  const window = parsed.range?.kind === "valid" ? parsed.range.window : DEFAULT_WINDOW;
  if (!parsed.query) {
    const runnable = exploreRun(target.team, target.source, "", window, ctx);
    return { shape: "run", id: "fallback", run: runnable, enter: { kind: "navigate", to: exploreRunLocation(runnable) }, tab: null };
  }
  const where = `${target.team.name}/${target.source.name}`;
  return row("fallback", "searchLogs", plain(label), {
    kind: "searchText", target: { teamId: target.team.id, sourceId: target.source.id, text: parsed.query, range: window.spec },
  }, { detail: ctx.t("palette.fallback.literal", { query: parsed.query, source: where, limit: TEXT_SEARCH_LIMITS.fields }), shortcut: "modEnter" });
}

function searchView(parsed: ParsedInput, ctx: PaletteContext, catalog: CatalogSnapshot, index: Index): PaletteView {
  const words = parsed.text.trim().split(/\s+/);
  const pages: Ranked[] = visiblePages(ctx).flatMap((page) => {
    const label = ctx.t(page.titleKey);
    const match = matchWords(words, label);
    return match ? [rank(pageRow(page, ctx, match.marks), label, match)] : [];
  });
  const actions: Ranked[] = availableActions(ctx, index).flatMap(({ action, label, effect }) => {
    const match = matchWords(words, label);
    return match ? [rank(row(`action:${action.id}`, action.icon, { text: label, marks: match.marks, dim: [] }, effect), label, match)] : [];
  });
  const searched: SearchGroup[] = [
    { id: "pages", ranked: pages, statusRows: [] },
    { id: "actions", ranked: actions, statusRows: [] },
    searchSources(words, ctx, catalog, index),
    ...searchEntities(words, ctx, catalog, index),
  ];
  const best = (entry: SearchGroup) => entry.ranked.reduce((top, candidate) => Math.max(top, candidate.score), 0);
  const ordered = searched
    .filter((entry) => entry.ranked.length > 0 || entry.statusRows.length > 0)
    .sort((a, b) => best(b) - best(a) || GROUP_PRIORITY.indexOf(a.id) - GROUP_PRIORITY.indexOf(b.id));
  const groups = ordered.map((entry) => {
    const ranked = entry.ranked.sort(byRank);
    const count = entry.count ? `${ranked.length} · ${entry.count}` : ranked.length > 0 ? String(ranked.length) : null;
    const rows = ranked.slice(0, MAX_GROUP_ROWS).flatMap(candidate => {
      const item = candidate.item;
      if (ctx.askChefEnabled && item.id.startsWith("alert:") && item.enter?.kind === "navigate") {
        const alertId = Number(item.id.slice("alert:".length));
        const alert = current(catalog.alerts)?.find(entry => entry.id === alertId);
        const source = index.sources.find(entry => entry.id === alert?.sourceId && entry.teamId === ctx.current?.teamId)
          ?? index.sources.find(entry => entry.id === alert?.sourceId);
        return [item, row(`alert-ask:${alertId}`, "askChef", plain(ctx.t("palette.ask.investigate")), { kind: "investigateAlert", alertId, teamId: source?.teamId ?? ctx.current?.teamId ?? null })];
      }
      return [item];
    });
    return group(entry.id, ctx.t(`palette.group.${entry.id}`), [...rows, ...entry.statusRows], count);
  });
  const at = onExplore(ctx);
  const team = at ? index.teamById.get(at.teamId) : null;
  const source = at ? index.sourcesOf(at.teamId)?.find(source => source.id === at.sourceId) : null;
  const implicit = at && team && source?.connected && source.logchefql && ctx.explorer && !runProblem(parsed, source, ctx)
    && recognizeLogchefQL(parsed.query, ctx.schemaFields).kind === "query"
    ? exploreRun(team, source, parsed.query, parsed.range?.kind === "valid" ? parsed.range.window : ctx.explorer.window, ctx) : null;
  const implicitEffect: PaletteEffect | null = implicit ? { kind: "navigate", to: exploreRunLocation(implicit) } : null;
  const fallback = fallbackItem(parsed, ctx, catalog, index);
  const labels = [...visiblePages(ctx).map(page => ctx.t(page.titleKey)), ...availableActions(ctx, index).map(action => action.label)];
  const ask = !strongNavigationMatch(parsed.text, labels) ? askItem(parsed, ctx, null, runProblem(parsed, null, ctx)) : null;
  const results = searched.reduce((total, entry) => total + entry.ranked.length, 0);
  const hints = [hint(["up", "down"], ctx.t("palette.footer.navigate")), hint(["enter"], ctx.t(implicit ? "palette.footer.run" : "palette.footer.open"))];
  if (implicitEffect || fallback.enter) hints.push(hint(["modEnter"], ctx.t(implicit ? "palette.footer.run" : "palette.footer.searchLogs")));
  return {
    askEnter: ask?.enter ?? null, mode: implicit ? "query" : "search", tokens: implicit ? parsed.tokens : [], ghost: null,
    groups: [...(implicit ? [group("run", null, [{ shape: "run", id: "implicit:run", run: implicit, enter: implicitEffect, tab: null }])] : []), ...groups, group("fallback", null, [fallback]), ...(ask ? [group("ask", null, [ask])] : [])],
    modEnter: implicitEffect ?? fallback.enter, needsTeamSources: ctx.current && needsLoad(catalog.sourcesByTeam.get(ctx.current.teamId)) ? [ctx.current.teamId] : [],
    footer: { hints, status: { kind: "text", text: implicit ? "LogchefQL" : ctx.t("palette.footer.results", { count: results }, results) } },
  };
}

type TagTarget =
  | { readonly kind: "none" }
  | { readonly kind: "loading"; readonly slug: string; readonly teams: readonly TeamEntry[] }
  | { readonly kind: "failed"; readonly list: ListName; readonly label: string }
  | { readonly kind: "unknownTeam"; readonly slug: string }
  | { readonly kind: "team"; readonly team: TeamEntry }
  | { readonly kind: "unknownSource"; readonly team: TeamEntry; readonly slug: string }
  | { readonly kind: "ambiguous"; readonly slug: string }
  | { readonly kind: "resolved"; readonly team: TeamEntry; readonly source: SourceEntry };

function resolveTag(tag: TagToken, catalog: CatalogSnapshot, t: Translate): TagTarget {
  const teamSlug = tag.team.toLowerCase();
  if (!teamSlug && tag.teamId === null) return { kind: "none" };
  const teams = current(catalog.teams);
  if (!teams) {
    return catalog.teams.state === "failed" ? { kind: "failed", list: "teams", label: t("palette.failed.teams") } : { kind: "loading", slug: teamSlug, teams: [] };
  }
  const named = teams.filter((team) => tag.teamId === null ? team.slug === teamSlug : team.id === tag.teamId);
  if (named.length === 0) return { kind: "unknownTeam", slug: teamSlug };
  const sourceSlug = tag.source?.toLowerCase() ?? "";
  if (!sourceSlug && tag.sourceId === null) return named.length === 1 ? { kind: "team", team: named[0] } : { kind: "ambiguous", slug: teamSlug };

  const matches: { team: TeamEntry; source: SourceEntry }[] = [];
  const pending: TeamEntry[] = [];
  let failed: TeamEntry | null = null;
  for (const team of named) {
    const list = catalog.sourcesByTeam.get(team.id);
    const sources = list ? current(list) : null;
    if (!sources || (named.length > 1 && list?.state !== "ready")) {
      if (list?.state === "failed") failed = team;
      else pending.push(team);
      continue;
    }
    for (const source of sources) if (tag.sourceId === null ? source.slug === sourceSlug : source.id === tag.sourceId) matches.push({ team, source });
  }
  if (matches.length > 1) return { kind: "ambiguous", slug: `${teamSlug}/${sourceSlug}` };
  if (pending.length > 0) return { kind: "loading", slug: `${teamSlug}/${sourceSlug}`, teams: pending };
  if (failed) return { kind: "failed", list: { teamSources: failed.id }, label: t("palette.failed.sources", { team: failed.name }) };
  if (matches.length === 1) return { kind: "resolved", ...matches[0] };
  return { kind: "unknownSource", team: named[0], slug: sourceSlug };
}

function runProblem(parsed: ParsedInput, source: SourceEntry | null, ctx: PaletteContext): string | null {
  for (const issue of parsed.issues) {
    switch (issue.kind) {
      case "tooLong":
        return ctx.t("palette.problem.tooLong", { limit: issue.limit.toLocaleString(ctx.locale) });
      case "extraTag":
        return ctx.t("palette.problem.extraTag", { syntax: "@team/source" });
      case "duplicateRange":
        return ctx.t("palette.problem.duplicateRange", { syntax: "last:" });
      case "invalidRange":
        return ctx.t("palette.problem.invalidRange", { text: `last:${issue.text}` });
    }
  }
  if (source && !source.connected) return ctx.t("palette.problem.disconnected", { source: source.name });
  if (source && !source.logchefql) return ctx.t("palette.problem.unsupported", { source: source.name });
  return null;
}

function runEffect(team: TeamEntry, source: SourceEntry, parsed: ParsedInput, ctx: PaletteContext): { run: ExploreRun; effect: PaletteEffect } | null {
  if (runProblem(parsed, source, ctx)) return null;
  const window = parsed.range?.kind === "valid" ? parsed.range.window : DEFAULT_WINDOW;
  const run = exploreRun(team, source, parsed.query, window, ctx);
  return { run, effect: { kind: "navigate", to: exploreRunLocation(run) } };
}

function tagMarks(insert: string, match: FuzzyMatch | null, offset: number): MarkedText {
  const slash = insert.indexOf("/");
  const dim: Span[] = [{ start: 0, end: 1 }];
  if (slash > 0) dim.push({ start: slash, end: slash + 1 });
  return { text: insert, marks: (match?.marks ?? []).map((mark) => ({ start: mark.start + offset, end: mark.end + offset })), dim };
}

interface Completions {
  readonly groups: PaletteGroup[];
  readonly first: { readonly insert: string; readonly edit: InputEdit } | null;
}

function teamTag(team: TeamEntry, index: Index): string {
  return `${team.slug}${index.teams.filter(entry => entry.slug === team.slug).length > 1 ? `#${team.id}` : ""}`;
}

function sourceCompletion(team: TeamEntry, source: SourceEntry, match: FuzzyMatch | null, offset: number, slot: CompletionSlot, parsed: ParsedInput, ctx: PaletteContext, index: Index) {
  const duplicate = (index.sourcesOf(team.id) ?? []).filter(entry => entry.slug === source.slug).length > 1;
  const insert = `@${teamTag(team, index)}/${source.slug}${duplicate ? `#${source.id}` : ""}`;
  const edit = applyCompletion(parsed.text, slot.replace, insert, true);
  const runnable = parsed.ask ? null : runEffect(team, source, parsed, ctx);
  return {
    insert, edit,
    item: row(`complete:source:${team.id}:${source.id}`, "sources", tagMarks(insert, match, offset), runnable?.effect ?? { kind: "edit", edit }, {
      detail: sourceDetail(team, source, index), mono: true, tab: parsed.collapsed ? edit : null,
    }),
  };
}

function tagCompletions(slot: CompletionSlot, parsed: ParsedInput, ctx: PaletteContext, index: Index): Completions {
  const sources: { ranked: Ranked; insert: string; edit: InputEdit }[] = [];
  const teamRows: { ranked: Ranked; insert: string; edit: InputEdit }[] = [];
  if (slot.kind === "team") {
    for (const team of index.teams) {
      const teamMatch = parsed.tag?.teamId === null ? contiguous(fuzzyMatch(slot.prefix, team.slug)) : parsed.tag?.teamId === team.id ? fuzzyMatch("", team.slug) : null;
      if (teamMatch) {
        const insert = `@${teamTag(team, index)}/`;
        const edit = applyCompletion(parsed.text, slot.replace, insert, false);
        const count = index.sourcesOf(team.id)?.length;
        const item = row(`complete:team:${team.id}`, "teams", tagMarks(insert, teamMatch, 1), { kind: "edit", edit }, {
          detail: count === undefined ? ctx.t("palette.team.label") : ctx.t("palette.team.sources", { count }, count),
          mono: true, meta: ctx.t("palette.team.pick"), tab: parsed.collapsed ? edit : null,
        });
        teamRows.push({ ranked: rank(item, team.slug, teamMatch, { boost: ctx.current?.teamId === team.id ? 1 : 0 }), insert, edit });
      }
      for (const source of index.sourcesOf(team.id) ?? []) {
        const match = contiguous(fuzzyMatch(slot.prefix, `${team.slug}/${source.slug}`));
        if (!match) continue;
        const completion = sourceCompletion(team, source, match, 1, slot, parsed, ctx, index);
        sources.push({ ...completion, ranked: rank(completion.item, completion.insert, match, { recency: recencyOf(ctx, team.id, source.id) }) });
      }
    }
  } else if (slot.kind === "source") {
    for (const team of index.teams.filter((entry) => parsed.tag?.teamId === null ? entry.slug === slot.team.toLowerCase() : entry.id === parsed.tag?.teamId)) {
      for (const source of index.sourcesOf(team.id) ?? []) {
        const match = parsed.tag?.sourceId === null ? contiguous(fuzzyMatch(slot.prefix, source.slug)) : parsed.tag?.sourceId === source.id ? fuzzyMatch("", source.slug) : null;
        if (!match) continue;
        const completion = sourceCompletion(team, source, match, teamTag(team, index).length + 2, slot, parsed, ctx, index);
        sources.push({ ...completion, ranked: rank(completion.item, completion.insert, match, { recency: recencyOf(ctx, team.id, source.id) }) });
      }
    }
  }
  const sortRanked = <T extends { ranked: Ranked }>(list: T[]) => list.sort((a, b) => byRank(a.ranked, b.ranked)).slice(0, MAX_COMPLETIONS);
  const topSources = sortRanked(sources);
  const topTeams = sortRanked(teamRows);
  const sourceTeams = new Set(topSources.map((entry) => entry.insert.slice(1, entry.insert.indexOf("/"))));
  const sourceCount = sourceTeams.size === 1 ? `${[...sourceTeams][0]} · ${topSources.length}` : String(topSources.length);
  const groups = [
    group("sources", ctx.t("palette.group.sources"), topSources.map((entry) => entry.ranked.item), sourceCount),
    group("teams", ctx.t("palette.group.teams"), topTeams.map((entry) => entry.ranked.item)),
  ].filter((entry) => entry.items.length > 0);
  const first = topSources[0] ?? topTeams[0] ?? null;
  return { groups, first: first ? { insert: first.insert, edit: first.edit } : null };
}

function rangeCompletions(slot: Extract<CompletionSlot, { kind: "range" }>, parsed: ParsedInput, ctx: PaletteContext): Completions {
  const matches = RANGE_SPECS.flatMap((spec, order) => {
    const window = relativeWindow(spec);
    const match = fuzzyMatch(slot.prefix, spec);
    if (!window || !match || (slot.prefix && match.score < TIER.prefix)) return [];
    return [{ spec, order, window, match }];
  });
  if (slot.prefix) matches.sort((a, b) => b.match.score - a.match.score || a.spec.length - b.spec.length || a.order - b.order);
  const rows = matches.map(({ spec, window, match }) => {
    const insert = `last:${spec}`;
    const edit = applyCompletion(parsed.text, slot.replace, insert, true);
    const label: MarkedText = { text: insert, marks: match.marks.map((mark) => ({ start: mark.start + 5, end: mark.end + 5 })), dim: [{ start: 0, end: 5 }] };
    const item = row(`complete:range:${spec}`, "range", label, { kind: "edit", edit }, {
      detail: rangeLabel(window, ctx.t), mono: true, tab: parsed.collapsed ? edit : null,
    });
    return { item, insert, edit };
  });
  const first = rows[0] ?? null;
  return {
    groups: rows.length > 0 ? [group("ranges", ctx.t("palette.group.ranges"), rows.map((entry) => entry.item), "last:")] : [],
    first: first ? { insert: first.insert, edit: first.edit } : null,
  };
}

function targetProblem(target: TagTarget, ctx: PaletteContext): PaletteItem | null {
  const title = ctx.t("palette.run.title");
  switch (target.kind) {
    case "loading":
      return status("status:tag", "loading", ctx.t("palette.loading.tag", { slug: `@${target.slug}` }), "teams");
    case "failed":
      return status("status:tag", "failed", target.label, target.list);
    case "unknownTeam":
      return blocked("run", "warning", title, ctx.t("palette.problem.unknownTeam", { slug: `@${target.slug}` }));
    case "unknownSource":
      return blocked("run", "warning", title, ctx.t("palette.problem.unknownSource", { slug: target.slug, team: target.team.name }));
    case "ambiguous":
      return blocked("run", "warning", title, ctx.t("palette.problem.ambiguous", { slug: `@${target.slug}` }));
    case "none":
    case "team":
    case "resolved":
      return null;
  }
}

function queryView(parsed: ParsedInput, tag: TagToken, ctx: PaletteContext, catalog: CatalogSnapshot, index: Index, selectedId: string | null): PaletteView {
  const target = resolveTag(tag, catalog, ctx.t);
  const replace = { start: tag.start, end: tag.end };
  const implied: CompletionSlot | null = target.kind === "team"
    ? { kind: "source", team: tag.team, prefix: "", replace }
    : target.kind === "none" ? { kind: "team", prefix: "", replace }
    : target.kind === "ambiguous" ? { kind: "source", team: tag.team, prefix: tag.source ?? "", replace } : null;
  const slot = parsed.slot ?? implied;
  const completions = slot?.kind === "range" ? rangeCompletions(slot, parsed, ctx) : slot ? tagCompletions(slot, parsed, ctx, index) : { groups: [], first: null };
  const firstTab = parsed.collapsed ? completions.first?.edit ?? null : null;

  const lead: PaletteItem[] = [];
  let modEnter: PaletteEffect | null = null;
  let askEnter: PaletteEffect | null = null;
  if (target.kind === "resolved") {
    const runnable = parsed.ask ? null : runEffect(target.team, target.source, parsed, ctx);
    const problem = runProblem(parsed, target.source, ctx);
    const ask = askItem(parsed, ctx, target, runProblem(parsed, null, ctx));
    if (parsed.ask) {
      if (ask) lead.push(ask);
      askEnter = ask?.enter ?? null;
    } else if (runnable) {
      lead.push({ shape: "run", id: "run", run: runnable.run, enter: runnable.effect, tab: firstTab });
      modEnter = runnable.effect;
      if (ask && parsed.query) { lead.push(ask); askEnter = ask.enter; }
    } else {
      lead.push(blocked("run", "run", ctx.t("palette.run.title"), problem ?? "", firstTab));
      if (ask?.shape === "row" && parsed.query) { lead.push(ask); askEnter = ask.enter; }
    }
  } else {
    const problem = targetProblem(target, ctx);
    const typingTag = parsed.slot !== null && parsed.slot.kind !== "range" && completions.groups.length > 0;
    if (problem && (problem.shape === "status" || target.kind === "ambiguous" || !typingTag)) lead.push(problem);
  }

  const all = [...lead, ...completions.groups.flatMap(group => group.items)];
  const edit = (all.find(item => item.id === selectedId) ?? all.find(item => item.enter || item.tab))?.tab;
  const ghost = parsed.collapsed && parsed.slot && parsed.caret === parsed.text.length && parsed.slot.replace.end === parsed.caret && edit
    && edit.text.toLowerCase().startsWith(parsed.text.toLowerCase()) && edit.text.trimEnd().length > parsed.text.length
    ? { text: edit.text.slice(parsed.text.length).trimEnd(), edit }
    : null;

  const needs = target.kind === "loading" ? target.teams.filter((team) => needsLoad(catalog.sourcesByTeam.get(team.id))).map((team) => team.id) : [];
  if (target.kind === "team" && needsLoad(catalog.sourcesByTeam.get(target.team.id))) needs.push(target.team.id);

  const hints = [hint(["enter"], ctx.t(modEnter ? "palette.footer.run" : "palette.footer.open"))];
  if (ghost && slot?.kind === "range") hints.push(hint(["tab"], ctx.t("palette.footer.accept"), ghost.edit.text.slice(slot.replace.start, ghost.edit.caret).trim().slice("last:".length)));
  else if (completions.first && parsed.collapsed) hints.push(hint(["tab"], ctx.t("palette.footer.complete")));
  hints.push(hint(["up", "down"], ctx.t("palette.footer.navigate")));
  const statusLine: FooterStatus = parsed.slot?.kind === "range"
    ? { kind: "completing", label: ctx.t("palette.footer.completing"), syntax: "last:" }
    : parsed.slot
      ? { kind: "completing", label: ctx.t("palette.footer.completing"), syntax: "@team/source" }
      : { kind: "text", text: "LogchefQL" };

  return {
    askEnter, mode: parsed.ask && ctx.askChefEnabled ? "ask" : "query", tokens: parsed.tokens, ghost,
    groups: [...(lead.length > 0 ? [group("run", null, lead)] : []), ...completions.groups],
    modEnter, needsTeamSources: needs,
    footer: { hints, status: statusLine },
  };
}

/** Pure: everything the dialog renders and everything a key does. */
export function buildPaletteView(text: string, selection: Span, ctx: PaletteContext, catalog: CatalogSnapshot, selectedId: string | null = null): PaletteView {
  const parsed = parsePaletteInput(text, selection, ctx.askChefEnabled);
  const index = indexCatalog(catalog);
  if (text.trim() === "") return emptyView(ctx, catalog, index);
  if (parsed.ask && !parsed.tag) {
    const ask = askItem(parsed, ctx, null, runProblem(parsed, null, ctx));
    return { mode: "ask", tokens: parsed.tokens, ghost: null, groups: ask ? [group("ask", null, [ask])] : [], askEnter: ask?.enter ?? null, modEnter: null, needsTeamSources: [], footer: { hints: [hint(["enter"], ctx.t("askChef.title")), hint(["up", "down"], ctx.t("palette.footer.navigate"))], status: { kind: "text", text: ctx.t("askChef.title") } } };
  }
  if (parsed.tag) return queryView(parsed, parsed.tag, ctx, catalog, index, selectedId);
  return searchView(parsed, ctx, catalog, index);
}
