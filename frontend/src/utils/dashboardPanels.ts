import { validatePresentation } from "./panelPresentation";
import {
  DASHBOARD_PANELS_VERSION,
  type DashboardLayoutItem,
  type DashboardPanel,
  type DashboardPanels,
  type DashboardQueryPanel,
  type DashboardTab,
  type DashboardViz,
  type DashboardVizType,
  type PanelQueryLanguage,
  type DashboardPanelQuery,
  type PanelInterval,
} from "@/api/dashboards";

// The dashboard grid is 12 columns wide. A panel spans 2 to 12 columns and 1 to
// 12 rows (mirrors the server-side validation in pkg/models).
const DASHBOARD_GRID_COLUMNS = 12;
const MIN_PANEL_WIDTH = 2;
const MIN_PANEL_HEIGHT = 1;
const MAX_PANEL_HEIGHT = 12;
const DEFAULT_PANEL_WIDTH = 6;
const DEFAULT_PANEL_HEIGHT = 2;
export const MAX_DASHBOARD_PANELS = 24;
export const MAX_DASHBOARD_TABS = 10;
export const MAX_TAB_TITLE_CODE_POINTS = 60;
export const MAX_DASHBOARD_NAME_CODE_POINTS = 200;
export const MAX_DASHBOARD_DESCRIPTION_CODE_POINTS = 2000;
export const MAX_TABLE_LIMIT = 10000;
const MAX_TRANSLATE_LIMIT = 100000;
// Sanity bound on the blob's cache_ttl_seconds (server also clamps to max_ttl at
// request time); mirrors the validation in pkg/models.
const MAX_DASHBOARD_CACHE_TTL_SECONDS = 86400;

const VALID_VIZ_TYPES: readonly DashboardVizType[] = ["stat", "chart", "pie", "toplist", "table"];
// Mirrors pkg/models.QueryLanguage's Valid() set (server: query.go).
const VALID_QUERY_LANGUAGES: readonly PanelQueryLanguage[] = ["logchefql", "clickhouse-sql", "logsql"];

/** The empty schema 2 blob a new dashboard starts from. */
export function emptyDashboardPanels(): DashboardPanels {
  return { version: DASHBOARD_PANELS_VERSION, features: [], tabs: [{ id: "main", title: "Main", layout: [] }], panels: [] };
}

/**
 * The tab a dashboard shows for `tabId`: that tab, or the first one when the
 * id names none (no `tab` in the URL, or a tab that was deleted).
 */
export function dashboardTab(blob: Pick<DashboardPanels, "tabs">, tabId: string | null | undefined): DashboardTab | undefined {
  const tabs = blob.tabs ?? [];
  return tabs.find((tab) => tab.id === tabId) ?? tabs[0];
}

/** The layout of one tab; empty when the id names no tab. */
export function tabLayout(blob: Pick<DashboardPanels, "tabs">, tabId: string): DashboardLayoutItem[] {
  return (blob.tabs ?? []).find((tab) => tab.id === tabId)?.layout ?? [];
}

/** The id of the tab that places a panel. */
export function panelTabId(blob: Pick<DashboardPanels, "tabs">, panelId: string): string | undefined {
  return (blob.tabs ?? []).find((tab) => (tab.layout ?? []).some((l) => l.id === panelId))?.id;
}

/** The blob with one tab's layout replaced; other tabs and fields are kept. */
export function withTabLayout(blob: DashboardPanels, tabId: string, layout: DashboardLayoutItem[]): DashboardPanels {
  return { ...blob, tabs: (blob.tabs ?? []).map((tab) => (tab.id === tabId ? { ...tab, layout } : tab)) };
}

/**
 * Trim the Unicode White_Space set from a group-by value, as Go's
 * strings.TrimSpace does: U+0085 is trimmed and U+FEFF is kept, unlike
 * JavaScript's trim(). Validation and request building both use it.
 */
export function trimDashboardGroupBy(value: string | undefined): string {
  return (value ?? "").replace(/^\p{White_Space}+|\p{White_Space}+$/gu, "");
}

/**
 * The localized copy name for a dashboard, cut by code points so it fits the
 * 200-code-point name bound. The localized suffix is preserved and a surrogate
 * pair is never split.
 */
export function dashboardCopyName(name: string, format: (name: string) => string): string {
  const points = Array.from(name);
  let keep = points.length;
  let candidate = format(name);
  while (keep > 0 && Array.from(candidate).length > MAX_DASHBOARD_NAME_CODE_POINTS) {
    keep--;
    candidate = format(points.slice(0, keep).join(""));
  }
  return candidate;
}

/** A name cut to the 200-code-point bound without splitting a surrogate pair. */
export function clampDashboardName(name: string): string {
  const points = Array.from(name);
  return points.length > MAX_DASHBOARD_NAME_CODE_POINTS ? points.slice(0, MAX_DASHBOARD_NAME_CODE_POINTS).join("") : name;
}

/** A fresh query panel shell: a line chart with no source yet. */
export function newQueryPanel(id: string, title: string): DashboardQueryPanel {
  return {
    id,
    kind: "query",
    title,
    team_id: 0,
    source_id: 0,
    query: { mode: "count", language: "logchefql", text: "", interval: "auto" },
    viz: { type: "chart", mark: "line", stack: "none" },
  };
}

/** The builder's four choices. Pie and toplist are the two breakdown views. */
export type PanelBuilderKind = "chart" | "stat" | "breakdown" | "table";

export function panelBuilderKind(viz: Pick<DashboardViz, "type">): PanelBuilderKind {
  return viz.type === "pie" || viz.type === "toplist" ? "breakdown" : viz.type;
}

/**
 * The panel re-shaped for another builder kind. Fields the new kind does not
 * use are dropped; the query text, language, team and source stay.
 */
export function switchPanelKind(panel: DashboardQueryPanel, kind: PanelBuilderKind): DashboardQueryPanel {
  if (panelBuilderKind(panel.viz) === kind) return panel;
  if (panel.query.mode === "result") {
    const columns = panel.viz.columns ? { columns: panel.viz.columns } : {};
    switch (kind) {
      case "chart": return { ...panel, viz: { type: "chart", mark: "line", stack: "none", ...columns } };
      case "breakdown": return { ...panel, viz: { type: "toplist", ...columns } };
      case "stat": return { ...panel, viz: { type: "stat", reduce: "last", ...columns } };
      case "table": return { ...panel, viz: { type: "table", ...columns } };
    }
  }
  const { language, text } = panel.query;
  const translate = language === "logchefql" && panel.query.translate_limit !== undefined
    ? { translate_limit: panel.query.translate_limit }
    : {};
  const groupBy = panel.query.group_by !== undefined ? { group_by: panel.query.group_by } : {};
  // The interval and the range belong to the query, whatever draws it.
  const interval = panel.query.interval ?? "auto";
  const range = panel.query.range !== undefined ? { range: panel.query.range } : {};
  switch (kind) {
    case "chart":
      return { ...panel, query: { mode: "count", language, text, ...groupBy, interval, ...translate, ...range }, viz: { type: "chart", mark: "line", stack: "none" } };
    case "stat":
      return { ...panel, query: { mode: "count", language, text, interval, ...translate, ...range }, viz: { type: "stat", reduce: "sum" } };
    case "breakdown":
      return { ...panel, query: { mode: "count", language, text, ...groupBy, interval, ...translate, ...range }, viz: { type: "toplist" } };
    case "table":
      return { ...panel, query: { mode: "result", language, text, ...resultInterval(interval), ...range }, viz: { type: "table" } };
  }
}

/** A result query stores only a fixed interval: `auto` is its default, so the key is left out. */
export function resultInterval(interval: DashboardPanelQuery["interval"]): { interval?: PanelInterval } {
  return interval === undefined || interval === "auto" ? {} : { interval };
}

/** The range grammar of `query.range` and `default_time_range`, as the server checks it. */
const DASHBOARD_RANGE_PATTERN = /^([1-9]\d{0,3}[mhdw]|today|yesterday|today-[1-9]\d{0,3}d)$/;

export function isDashboardRange(value: string): boolean {
  return DASHBOARD_RANGE_PATTERN.test(value);
}

const PANEL_INTERVAL_VALUES: readonly string[] = ["auto", "1s", "5s", "10s", "15s", "30s", "1m", "5m", "10m", "15m", "30m", "1h", "2h", "3h", "6h", "12h", "24h"];

interface NormalizedPanel {
  panel: DashboardPanel;
  x: number;
  y: number;
  w: number;
  h: number;
}

/** Snap an arbitrary width to whole columns, within the 2..12 a panel may span. */
export function snapPanelWidth(width: number | undefined): number {
  if (!width || Number.isNaN(width)) {
    return DEFAULT_PANEL_WIDTH;
  }
  return Math.min(DASHBOARD_GRID_COLUMNS, Math.max(MIN_PANEL_WIDTH, Math.round(width)));
}

/** Clamp a height into the allowed 1..12 row range. */
export function clampPanelHeight(height: number | undefined): number {
  if (!height || Number.isNaN(height)) {
    return DEFAULT_PANEL_HEIGHT;
  }
  return Math.min(MAX_PANEL_HEIGHT, Math.max(MIN_PANEL_HEIGHT, Math.round(height)));
}

/**
 * Reconcile one tab's stored layout with the dashboard's panel list and produce
 * a render-ready set of grid placements. This:
 *
 *  - keeps only the panels that this layout places, so a tab shows its own
 *    panels and a layout entry that points at a deleted panel is dropped;
 *  - rounds widths into 2..12 columns and heights into 1..12 rows;
 *  - clamps x so the panel never overflows the 12-column grid;
 *  - returns placements sorted top-to-bottom, left-to-right for stable DOM order.
 */
export function normalizeDashboardLayout(
  panels: DashboardPanel[],
  layout: DashboardLayoutItem[]
): NormalizedPanel[] {
  const layoutById = new Map<string, DashboardLayoutItem>();
  for (const item of layout ?? []) {
    if (item && typeof item.id === "string") {
      layoutById.set(item.id, item);
    }
  }

  const placed: NormalizedPanel[] = [];
  for (const panel of panels ?? []) {
    const item = layoutById.get(panel.id);
    if (!item) continue;
    const w = snapPanelWidth(item.w);
    const h = clampPanelHeight(item.h);
    const maxX = DASHBOARD_GRID_COLUMNS - w;
    const x = Math.min(Math.max(0, Math.round(item.x ?? 0)), Math.max(0, maxX));
    const y = Math.max(0, Math.round(item.y ?? 0));
    placed.push({ panel, x, y, w, h });
  }

  placed.sort((a, b) => (a.y - b.y) || (a.x - b.x));
  return placed;
}

// ---------------------------------------------------------------------------
// Edit-mode layout math.
//
// In edit mode the working state is an ORDERED list of panels; a tab's grid
// layout (x/y placement) is a pure function of the order of its panels plus
// each panel's size. Every structural edit (reorder, add, remove, resize, move
// to another tab) re-runs packLayout on the tab it touched, so that tab always
// reflows top-left-first into the 12-column grid with no overlaps. Keeping this
// deterministic and pure is what makes it unit-testable.
// ---------------------------------------------------------------------------

/** A panel id plus its requested size — the input to the packer. */
export interface PanelSize {
  id: string;
  w: number;
  h: number;
}

/**
 * Pack an ordered list of sized panels into the 12-column grid, top-left-first
 * ("shelf" packing): panels flow left→right on a row until the next one no longer
 * fits in the remaining columns, then wrap onto a fresh row below the tallest
 * panel of the row just closed. Widths round into 2..12 and heights clamp to
 * 1..12. The result feeds directly back into the tab's layout.
 */
export function packLayout(order: PanelSize[]): DashboardLayoutItem[] {
  const items: DashboardLayoutItem[] = [];
  let x = 0;
  let rowY = 0;
  let rowMaxH = 0;
  for (const size of order ?? []) {
    const w = snapPanelWidth(size.w);
    const h = clampPanelHeight(size.h);
    if (x + w > DASHBOARD_GRID_COLUMNS) {
      rowY += rowMaxH;
      x = 0;
      rowMaxH = 0;
    }
    items.push({ id: size.id, x, y: rowY, w, h });
    x += w;
    rowMaxH = Math.max(rowMaxH, h);
  }
  return items;
}

/** Move the item at `from` to index `to`, returning a new array (non-mutating). */
export function moveItem<T>(list: T[], from: number, to: number): T[] {
  const copy = list.slice();
  if (from < 0 || from >= copy.length || from === to) {
    return copy;
  }
  const [item] = copy.splice(from, 1);
  const target = Math.max(0, Math.min(to, copy.length));
  copy.splice(target, 0, item);
  return copy;
}

/**
 * The blob with one tab re-packed top-left-first: the tab's panels in their
 * panel-array order, each at its stored size. This is the single reflow used
 * after add / remove / resize / reorder. Other tabs keep their layouts, so
 * editing one tab never rearranges another.
 */
export function reflowTab(blob: DashboardPanels, tabId: string): DashboardPanels {
  const sizes = new Map(tabLayout(blob, tabId).map((l) => [l.id, l]));
  const order: PanelSize[] = [];
  for (const panel of blob.panels ?? []) {
    const size = sizes.get(panel.id);
    if (size) order.push({ id: panel.id, w: size.w, h: size.h });
  }
  return withTabLayout(blob, tabId, packLayout(order));
}

/**
 * The blob with a panel added to a tab: appended to the panel list, placed last
 * in that tab at the default size, and the tab re-packed.
 */
export function addPanelToTab(blob: DashboardPanels, tabId: string, panel: DashboardPanel): DashboardPanels {
  const layout = [...tabLayout(blob, tabId), { id: panel.id, x: 0, y: 0, w: DEFAULT_PANEL_WIDTH, h: DEFAULT_PANEL_HEIGHT }];
  return reflowTab(withTabLayout({ ...blob, panels: [...(blob.panels ?? []), panel] }, tabId, layout), tabId);
}

/**
 * The blob with a panel moved to another tab at its current size. It lands last
 * in the target tab, and both tabs are re-packed. The blob is returned as it
 * is when the panel is already there or either end is unknown.
 */
export function movePanelToTab(blob: DashboardPanels, panelId: string, tabId: string): DashboardPanels {
  const from = panelTabId(blob, panelId);
  const panel = (blob.panels ?? []).find((p) => p.id === panelId);
  const entry = from === undefined ? undefined : tabLayout(blob, from).find((l) => l.id === panelId);
  if (!panel || !entry || from === undefined || from === tabId || !(blob.tabs ?? []).some((tab) => tab.id === tabId)) {
    return blob;
  }
  // A tab's order is the panel-array order, so the panel goes to the end of
  // the array to land after the panels the target tab already has.
  const panels = [...blob.panels.filter((p) => p.id !== panelId), panel];
  let next = withTabLayout({ ...blob, panels }, from, tabLayout(blob, from).filter((l) => l.id !== panelId));
  next = withTabLayout(next, tabId, [...tabLayout(next, tabId), entry]);
  return reflowTab(reflowTab(next, from), tabId);
}

/** The blob without a tab and without the panels that tab places. */
export function removeTab(blob: DashboardPanels, tabId: string): DashboardPanels {
  const removed = new Set(tabLayout(blob, tabId).map((l) => l.id));
  return {
    ...blob,
    tabs: (blob.tabs ?? []).filter((tab) => tab.id !== tabId),
    panels: (blob.panels ?? []).filter((p) => !removed.has(p.id)),
  };
}

/** A tab title as it is stored: trimmed, or null when it is not 1 to 60 characters. */
export function validTabTitle(title: string): string | null {
  const trimmed = title.trim();
  const length = Array.from(trimmed).length;
  return length >= 1 && length <= MAX_TAB_TITLE_CODE_POINTS ? trimmed : null;
}

/**
 * Validate a panels blob against the same rules the server enforces on save
 * (pkg/models/dashboards_v2.go). Returns a friendly message on the first
 * violation, or null when valid. The server 400 remains the authoritative
 * backstop; this just surfaces problems inline before the round-trip.
 */
export function validatePanelsBlob(blob: DashboardPanels): string | null {
  if (!blob || blob.version !== DASHBOARD_PANELS_VERSION) {
    return `Unsupported dashboard version (expected ${DASHBOARD_PANELS_VERSION}).`;
  }
  const ttl = blob.cache_ttl_seconds;
  if (ttl !== undefined) {
    if (typeof ttl !== "number" || !Number.isFinite(ttl) || ttl < 0 || ttl > MAX_DASHBOARD_CACHE_TTL_SECONDS) {
      return `Cache TTL must be between 0 and ${MAX_DASHBOARD_CACHE_TTL_SECONDS} seconds.`;
    }
  }
  const tabs = blob.tabs ?? [];
  if (tabs.length < 1) return "A dashboard needs at least one tab.";
  if (tabs.length > MAX_DASHBOARD_TABS) return `A dashboard can have at most ${MAX_DASHBOARD_TABS} tabs (this one has ${tabs.length}).`;
  const panels = blob.panels ?? [];
  if (panels.length > MAX_DASHBOARD_PANELS) {
    return `A dashboard can have at most ${MAX_DASHBOARD_PANELS} panels (this one has ${panels.length}).`;
  }
  const seen = new Set<string>();
  for (const p of panels) {
    const error = validatePanel(p, seen);
    if (error) return error;
  }
  const seenLayoutIds = new Set<string>();
  const seenTabIds = new Set<string>();
  for (const tab of tabs) {
    if (!tab.id) return "A tab is missing an id.";
    if (seenTabIds.has(tab.id)) return `Duplicate tab id "${tab.id}".`;
    seenTabIds.add(tab.id);
    const titleLength = Array.from(tab.title ?? "").length;
    if (titleLength < 1 || titleLength > MAX_TAB_TITLE_CODE_POINTS) {
      return `Tab titles must be 1 to ${MAX_TAB_TITLE_CODE_POINTS} characters.`;
    }
    for (const l of tab.layout ?? []) {
      if (!l.id) {
        return "A panel layout entry is missing an id.";
      }
      if (seenLayoutIds.has(l.id)) {
        return `Duplicate layout id "${l.id}".`;
      }
      seenLayoutIds.add(l.id);
      if (!Number.isInteger(l.w) || l.w < MIN_PANEL_WIDTH || l.w > DASHBOARD_GRID_COLUMNS) {
        return `Panel layout has an invalid width (${l.w}); allowed: ${MIN_PANEL_WIDTH}-${DASHBOARD_GRID_COLUMNS}.`;
      }
      if (!Number.isInteger(l.h) || l.h < MIN_PANEL_HEIGHT || l.h > MAX_PANEL_HEIGHT) {
        return `Panel layout has an invalid height (${l.h}); allowed: ${MIN_PANEL_HEIGHT}-${MAX_PANEL_HEIGHT}.`;
      }
    }
  }
  return null;
}

function validatePanel(p: DashboardPanel, seen: Set<string>): string | null {
  const label = p.title || p.id;
  if (!p.id) {
    return "A panel is missing an id.";
  }
  if (seen.has(p.id)) {
    return `Duplicate panel id "${p.id}".`;
  }
  seen.add(p.id);
  if (p.kind !== "query") {
    return `Panel "${label}" has an unsupported kind.`;
  }
  if (p.locked) {
    return `Panel "${label}" is locked and cannot be saved.`;
  }
  if (!p.team_id || p.team_id <= 0) {
    return `Panel "${label}" needs a team.`;
  }
  if (!p.source_id || p.source_id <= 0) {
    return `Panel "${label}" needs a source.`;
  }
  const { query, viz } = p;
  if (!query || (query.mode !== "count" && query.mode !== "result")) {
    return `Panel "${label}" has an unsupported query mode.`;
  }
  if (!VALID_QUERY_LANGUAGES.includes(query.language)) {
    return `Panel "${label}" has an unsupported query language.`;
  }
  if (!viz || !VALID_VIZ_TYPES.includes(viz.type)) {
    return `Panel "${label}" has an unsupported type.`;
  }
  const vizKeys: Record<DashboardVizType, readonly string[]> = {
    stat: ["thresholds", "color_mode", "compare", "sparkline", "type", "value", "order_by", "reduce", "columns", "format"],
    chart: ["thresholds", "type", "x", "y", "series", "mark", "stack", "x_scale", "orientation", "show_values", "legend", "columns", "format"],
    pie: ["type", "label", "value", "donut", "min_slice_percent", "columns", "format"],
    toplist: ["type", "label", "value", "max_rows", "columns", "format"],
    table: ["rules", "column_formats", "type", "visible_columns", "columns"],
  };
  if (Object.keys(viz).some((key) => !vizKeys[viz.type].includes(key))) return `Panel "${label}" has an unsupported visualization option.`;
  if (viz.type === "chart" && viz.mark !== undefined && !["line", "area", "bar"].includes(viz.mark)) return `Panel "${label}" has an invalid chart mark.`;
  if (viz.type === "chart" && viz.stack !== undefined && !["none", "stacked"].includes(viz.stack)) return `Panel "${label}" has an invalid chart stack.`;
  const presentationError = validatePresentation(viz);
  if (presentationError) return presentationError;
  const count = query.mode === "count";
  if (count && viz.type === "table") {
    return `Table panel "${label}" needs result mode.`;
  }
  if (!count && viz.type === "chart" && (!viz.x || !viz.y?.length)) return `Panel "${label}" needs x and y columns.`;
  if (!count && (viz.type === "pie" || viz.type === "toplist") && (!viz.label || !viz.value)) return `Panel "${label}" needs label and value columns.`;
  if (viz.type === "chart") {
    if (count && (viz.x !== undefined || viz.y !== undefined || viz.series !== undefined)) return `Panel "${label}" cannot use result roles in count mode.`;
    if (viz.y && new Set(viz.y).size !== viz.y.length) return `Panel "${label}" repeats a y column.`;
    if ([viz.x, viz.series, ...(viz.y ?? [])].some((v) => v !== undefined && (!v || Array.from(v).length > 128))) return `Panel "${label}" has an invalid column role.`;
    if (viz.x_scale !== undefined && !["auto", "time", "category"].includes(viz.x_scale)) return `Panel "${label}" has an invalid x scale.`;
    if (viz.orientation !== undefined && !["horizontal", "vertical"].includes(viz.orientation)) return `Panel "${label}" has an invalid orientation.`;
  }
  if (viz.type === "pie" || viz.type === "toplist") {
    if (count && (viz.label !== undefined || viz.value !== undefined)) return `Panel "${label}" cannot use result roles in count mode.`;
    if ([viz.label, viz.value].some((v) => v !== undefined && (!v || Array.from(v).length > 128))) return `Panel "${label}" has an invalid column role.`;
    if (viz.type === "pie" && viz.min_slice_percent !== undefined && (!Number.isFinite(viz.min_slice_percent) || viz.min_slice_percent <= 0 || viz.min_slice_percent >= 100)) return `Panel "${label}" needs a slice percentage above 0 and below 100.`;
    if (viz.type === "toplist" && viz.max_rows !== undefined && (!Number.isInteger(viz.max_rows) || viz.max_rows < 1 || viz.max_rows > MAX_TABLE_LIMIT)) return `Panel "${label}" needs 1 to ${MAX_TABLE_LIMIT} rows.`;
  }
  if (viz.type === "stat") {
    if (!count && !viz.value) return `Panel "${label}" needs a value column.`;
    if (count && (viz.value !== undefined || viz.order_by !== undefined)) return `Panel "${label}" cannot use result roles in count mode.`;
    if (viz.reduce !== undefined && !["last", "first", "sum", "avg", "min", "max"].includes(viz.reduce)) return `Panel "${label}" has an invalid reducer.`;
    if (viz.sparkline !== undefined && typeof viz.sparkline !== "boolean") return `Panel "${label}" has an invalid sparkline.`;
    if (viz.compare !== undefined) {
      const c = viz.compare;
      if (!c || typeof c !== "object" || Object.keys(c).some(k => !["mode", "good"].includes(k)) || !["none", "previous_row"].includes(c.mode) || (c.good !== undefined && !["up", "down", "neutral"].includes(c.good))) return `Panel "${label}" has an invalid comparison.`;
      if (c.mode === "previous_row" && !["first", "last"].includes(viz.reduce ?? (count ? "sum" : "last"))) return `Panel "${label}" comparison needs first or last reducer.`;
    }
    if (!count && !viz.order_by && (viz.sparkline || viz.compare?.mode === "previous_row")) return `Panel "${label}" comparison or sparkline needs order_by.`;
    for (const role of [viz.value, viz.order_by]) {
      if (role !== undefined && (Array.from(role).length < 1 || Array.from(role).length > 128)) return `Panel "${label}" has an invalid column role.`;
    }
  }
  if (viz.type !== "table") {
    const f = viz.format;
    if (f) {
      if (f.notation !== undefined && !["auto", "standard", "compact"].includes(f.notation)) return `Panel "${label}" has an invalid notation.`;
      if (f.unit !== undefined && !["none", "percent", "bytes", "duration_ms", "duration_s"].includes(f.unit)) return `Panel "${label}" has an invalid unit.`;
      if (f.decimals !== undefined && (!Number.isInteger(f.decimals) || f.decimals < 0 || f.decimals > 10)) return `Panel "${label}" needs 0 to 10 decimals.`;
      if (f.scale !== undefined && (!Number.isFinite(f.scale) || f.scale === 0)) return `Panel "${label}" needs a finite, nonzero scale.`;
      if ([f.prefix, f.suffix].some((v) => v !== undefined && Array.from(v).length > 16)) return `Panel "${label}" has a format affix longer than 16 characters.`;
    }
  }
  if (count && viz.columns !== undefined) return `Panel "${label}" cannot set column kinds in count mode.`;
  const columns = Object.entries(viz.columns ?? {});
  if (columns.length > 100) return `Panel "${label}" has too many column kinds.`;
  for (const [name, c] of columns) {
    if (!name || Array.from(name).length > 128 || c.confidence !== "confirmed" || !["time", "date", "number", "category", "bool", "other"].includes(c.kind)) return `Panel "${label}" has an invalid confirmed column kind.`;
  }
  if (query.group_by !== undefined) {
    if (!count) return `Panel "${label}" cannot group a result query.`;
    if (trimDashboardGroupBy(query.group_by) === "") return `Panel "${label}" has a blank group-by field.`;
  }
  if ((viz.type === "pie" || viz.type === "toplist") && count && query.group_by === undefined) {
    return `Breakdown panel "${label}" requires a group-by field.`;
  }
  if (query.interval !== undefined && !PANEL_INTERVAL_VALUES.includes(query.interval)) {
    return `Panel "${label}" has an unsupported interval.`;
  }
  if (query.range !== undefined && !isDashboardRange(query.range)) {
    return `Panel "${label}" has an invalid time range.`;
  }
  if (query.limit !== undefined) {
    if (count) return `Panel "${label}" cannot limit a count query.`;
    if (!Number.isInteger(query.limit) || query.limit < 1 || query.limit > MAX_TABLE_LIMIT) {
      return `Panel "${label}" needs a row limit between 1 and ${MAX_TABLE_LIMIT}.`;
    }
  }
  if (query.translate_limit !== undefined) {
    if (!count || query.language !== "logchefql") return `Panel "${label}" cannot set a translate limit.`;
    if (!Number.isInteger(query.translate_limit) || query.translate_limit < 1 || query.translate_limit > MAX_TRANSLATE_LIMIT) {
      return `Panel "${label}" needs a translate limit between 1 and ${MAX_TRANSLATE_LIMIT}.`;
    }
  }
  if (viz.type === "table" && viz.visible_columns !== undefined) {
    if (viz.visible_columns.length === 0) return `Panel "${label}" lists no columns; remove the column subset to show every column.`;
    const names = new Set<string>();
    for (const name of viz.visible_columns) {
      if (!name) return `Panel "${label}" has an empty column name.`;
      if (names.has(name)) return `Panel "${label}" lists column "${name}" twice.`;
      names.add(name);
    }
  }
  return null;
}

// ---------------------------------------------------------------------------
// Pointer-drag math (edit-mode direct manipulation).
//
// The grid canvas has 12 equal column tracks separated by a fixed gap, and
// fixed-height row tracks separated by the same gap. Given the canvas geometry,
// these pure functions translate pointer pixels into grid cells, grid cells into
// a move-insertion index, and pointer deltas into snapped resize spans — the
// components only wire pointer events to them. All previews go back through
// packLayout so the drag ghost/placeholder always reflects the exact layout that
// a commit would produce (top-left gravity reflow, no collision engine).
// ---------------------------------------------------------------------------

/** Pixel geometry of the grid canvas, in the same coordinate space as the pointer. */
export interface GridGeometry {
  /** Canvas left edge (viewport px, e.g. getBoundingClientRect().left). */
  left: number;
  /** Canvas top edge (viewport px). */
  top: number;
  /** Canvas inner width in px. */
  width: number;
  /** Gap between tracks in px. */
  gap: number;
  /** Height of one row track in px (excluding the gap). */
  rowHeight: number;
}

interface GridCell {
  col: number;
  row: number;
}

/** Pixel rect of a grid placement, relative to the canvas origin. */
interface PixelRect {
  left: number;
  top: number;
  width: number;
  height: number;
}

/** Width of one column track in px (12 tracks + 11 gaps fill the canvas). */
export function columnTrackWidth(geom: Pick<GridGeometry, "width" | "gap">): number {
  const track = (geom.width - geom.gap * (DASHBOARD_GRID_COLUMNS - 1)) / DASHBOARD_GRID_COLUMNS;
  // A hidden/zero-measured canvas ({width:0, gap:0}) or a bad gap can produce
  // a non-finite or negative track width; clamp to 0 rather than let it
  // poison downstream cell/resize math (pointToCell, cellRect, snapResize).
  return Number.isFinite(track) ? Math.max(0, track) : 0;
}

/**
 * Map a pointer position (viewport px) to the grid cell under it. `scrollTop`
 * is the scroll offset of a scrolling ancestor whose scrolling does NOT move
 * the canvas rect (pass 0 when `geom` comes from a fresh getBoundingClientRect,
 * which already reflects page scroll). Out-of-bounds pointers clamp to the
 * nearest valid column; rows clamp at 0 and grow without bound below.
 */
export function pointToCell(
  px: number,
  py: number,
  geom: GridGeometry,
  scrollTop = 0
): GridCell {
  // Column i starts at i * (track + gap), so a single stride division hit-tests
  // both the track and the gap trailing it.
  const colStride = (geom.width + geom.gap) / DASHBOARD_GRID_COLUMNS;
  const rowStride = geom.rowHeight + geom.gap;
  // Degenerate geometry (a hidden/zero-measured canvas: width/gap/rowHeight
  // all 0) would otherwise divide by zero here, producing NaN/Infinity
  // instead of a safe fallback cell.
  const col = colStride > 0 ? Math.floor((px - geom.left) / colStride) : 0;
  const row = rowStride > 0 ? Math.floor((py - geom.top + scrollTop) / rowStride) : 0;
  return {
    col: Math.min(DASHBOARD_GRID_COLUMNS - 1, Math.max(0, col)),
    row: Math.max(0, row),
  };
}

/** Pixel rect (canvas-relative) for a grid placement. */
export function cellRect(
  item: { x: number; y: number; w: number; h: number },
  geom: Pick<GridGeometry, "width" | "gap" | "rowHeight">
): PixelRect {
  const track = columnTrackWidth(geom);
  return {
    left: item.x * (track + geom.gap),
    top: item.y * (geom.rowHeight + geom.gap),
    width: item.w * track + (item.w - 1) * geom.gap,
    height: item.h * geom.rowHeight + (item.h - 1) * geom.gap,
  };
}

/**
 * Given the layout at drag start (reading order) and the cell under the pointer,
 * compute where the dragged panel should be inserted among the OTHER panels
 * (an index into the order with the dragged panel removed, 0..n). Rules:
 *
 *  - a panel counts as "before" the pointer when it sits entirely above the
 *    pointer row, or starts at/above that row and ends left of the pointer col;
 *  - when the pointer is inside a panel, its right half means "insert after";
 *  - empty space below everything appends to the end.
 *
 * Hit-testing against the drag-START layout (not the live preview) keeps the
 * target stable — previewing a move doesn't shift the cells being tested.
 *
 * Safe against a degenerate/NaN `cell` (e.g. derived from a zero-measured
 * canvas): every comparison below is false for NaN operands, so an
 * unreadable cell just falls through to "insert at index 0" instead of
 * producing a NaN/Infinite index.
 */
export function cellToMoveIndex(
  items: DashboardLayoutItem[],
  draggedId: string,
  cell: GridCell
): number {
  const others = (items ?? []).filter((i) => i.id !== draggedId);
  let index = 0;
  for (const o of others) {
    const above = o.y + o.h <= cell.row;
    const leftOf = o.y <= cell.row && o.x + o.w <= cell.col;
    const contains =
      o.x <= cell.col && cell.col < o.x + o.w && o.y <= cell.row && cell.row < o.y + o.h;
    if (above || leftOf) {
      index++;
    } else if (contains && cell.col >= o.x + o.w / 2) {
      index++;
    }
  }
  return Math.min(index, others.length);
}

/**
 * Preview layout for a move drag: the dragged panel is lifted out of the order
 * and re-inserted at `insertIndex` (an index into the order without it, i.e.
 * cellToMoveIndex's output), then everything re-packs. The dragged panel's slot
 * in the result is the drop placeholder; committing the move produces exactly
 * this layout.
 */
export function previewMoveLayout(
  order: PanelSize[],
  draggedId: string,
  insertIndex: number
): DashboardLayoutItem[] {
  const list = order ?? [];
  const dragged = list.find((o) => o.id === draggedId);
  if (!dragged) {
    return packLayout(list);
  }
  const rest = list.filter((o) => o.id !== draggedId);
  const idx = Math.max(0, Math.min(insertIndex, rest.length));
  rest.splice(idx, 0, dragged);
  return packLayout(rest);
}

/**
 * Snap a resize drag to grid spans: the panel's size at drag start plus the
 * pointer delta, rounded to whole columns in 2..12 and whole rows in 1..12.
 */
export function snapResize(
  startW: number,
  startH: number,
  dxPx: number,
  dyPx: number,
  geom: Pick<GridGeometry, "width" | "gap" | "rowHeight">
): { w: number; h: number } {
  const colStride = (geom.width + geom.gap) / DASHBOARD_GRID_COLUMNS;
  const rowStride = geom.rowHeight + geom.gap;
  // Same degenerate-geometry guard as pointToCell: a zero stride would
  // otherwise divide by zero and produce a NaN/Infinite unit count.
  const wUnits = colStride > 0 ? startW + dxPx / colStride : startW;
  const hUnits = rowStride > 0 ? Math.max(MIN_PANEL_HEIGHT, Math.round(startH + dyPx / rowStride)) : startH;
  return {
    w: snapPanelWidth(Math.max(1, wUnits)),
    h: clampPanelHeight(hUnits),
  };
}

/** Preview layout for a resize drag: same order, one panel at the new size. */
export function previewResizeLayout(
  order: PanelSize[],
  id: string,
  w: number,
  h: number
): DashboardLayoutItem[] {
  return packLayout((order ?? []).map((o) => (o.id === id ? { id: o.id, w, h } : o)));
}

/**
 * Where the ghost "+ Add panel" tile goes. Unlike a real panel (which always
 * packs at the default size, see `addPanelToTab`), the ghost is a visual
 * affordance for "there's room here" — it fills whatever space is actually
 * left in the current bottom row instead of a fixed phantom size that can
 * land disconnected below everything (e.g. a 6-wide default that doesn't fit
 * a 4-wide remainder wraps to its own orphaned row). Only once the bottom row
 * is exactly full does the tile drop to a fresh row at the default size.
 */
export function addTileSlot(
  order: PanelSize[],
  w: number = DEFAULT_PANEL_WIDTH,
  h: number = DEFAULT_PANEL_HEIGHT
): DashboardLayoutItem {
  // Replay packLayout's shelf-packing cursor (without placing a phantom item)
  // to find where the bottom row currently ends.
  let x = 0;
  let rowY = 0;
  let rowMaxH = 0;
  for (const size of order ?? []) {
    const itemW = snapPanelWidth(size.w);
    const itemH = clampPanelHeight(size.h);
    if (x + itemW > DASHBOARD_GRID_COLUMNS) {
      rowY += rowMaxH;
      x = 0;
      rowMaxH = 0;
    }
    x += itemW;
    rowMaxH = Math.max(rowMaxH, itemH);
  }

  const available = DASHBOARD_GRID_COLUMNS - x;
  if (available <= 0) {
    // Bottom row is exactly full: wrap to a fresh row at the default size.
    return { id: "__add__", x: 0, y: rowY + rowMaxH, w: snapPanelWidth(w), h: clampPanelHeight(h) };
  }
  // Fill the real remaining width of the bottom row, matching that row's
  // established height so the tile reads as part of it (falls back to the
  // default height when the row hasn't started yet, i.e. an empty canvas).
  return { id: "__add__", x, y: rowY, w: available, h: rowMaxH > 0 ? rowMaxH : clampPanelHeight(h) };
}

/** Total rows a layout occupies (bottom edge of its lowest panel). */
export function layoutRowCount(layout: DashboardLayoutItem[]): number {
  return (layout ?? []).reduce((max, l) => Math.max(max, l.y + l.h), 0);
}

/**
 * The panel blob for a copy of a dashboard. A panel is left out when it is
 * locked (the caller cannot read its query) or when `canUseTeam` rejects its
 * team: the server refuses to create a panel whose team the caller is not in.
 * A dashboard's creator gets unlocked panels even after leaving a team, so the
 * locked flag alone is not enough. The returned blob shares no references with
 * `blob`.
 */
export function copyableDashboardPanels(
  blob: DashboardPanels,
  canUseTeam: (teamId: number) => boolean
): { panels: DashboardPanels; skipped: number } {
  const kept = (blob.panels ?? []).filter((p): p is DashboardQueryPanel => !p.locked && canUseTeam(p.team_id));
  const keptIds = new Set(kept.map((p) => p.id));
  const panels: DashboardPanels = JSON.parse(
    JSON.stringify({
      ...blob,
      panels: kept,
      tabs: (blob.tabs ?? []).map((tab) => ({ ...tab, layout: (tab.layout ?? []).filter((l) => keptIds.has(l.id)) })),
    })
  );
  return { panels, skipped: (blob.panels ?? []).length - kept.length };
}
