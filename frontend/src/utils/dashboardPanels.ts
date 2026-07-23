import type { HistogramData } from "@/services/HistogramService";
import type {
  DashboardLayoutItem,
  DashboardPanel,
  DashboardPanels,
  DashboardPanelType,
  PanelQueryLanguage,
} from "@/api/dashboards";

// The dashboard grid is 12 columns wide. Panel widths are restricted to a small
// set of presets (mirrors the server-side validation in pkg/models/dashboards.go)
// and heights are clamped to 1..6 rows.
export const DASHBOARD_GRID_COLUMNS = 12;
export const ALLOWED_PANEL_WIDTHS = [2, 3, 4, 6, 8, 9, 12] as const;
export const MIN_PANEL_HEIGHT = 1;
export const MAX_PANEL_HEIGHT = 6;
export const DEFAULT_PANEL_WIDTH = 6;
export const DEFAULT_PANEL_HEIGHT = 2;
export const MAX_DASHBOARD_PANELS = 24;
export const DASHBOARD_PANELS_VERSION = 1;
// Sanity bound on the blob's cache_ttl_seconds (server also clamps to max_ttl at
// request time); mirrors the validation in internal/core/dashboards.go.
export const MAX_DASHBOARD_CACHE_TTL_SECONDS = 86400;

const VALID_PANEL_TYPES: readonly DashboardPanelType[] = ["timeseries", "stat", "table", "breakdown"];
// Mirrors pkg/models.QueryLanguage's Valid() set (server: query.go).
const VALID_QUERY_LANGUAGES: readonly PanelQueryLanguage[] = ["logchefql", "clickhouse-sql", "logsql"];

export interface NormalizedPanel {
  panel: DashboardPanel;
  x: number;
  y: number;
  w: number;
  h: number;
}

/** Snap an arbitrary width to the nearest allowed preset. */
export function snapPanelWidth(width: number | undefined): number {
  if (!width || Number.isNaN(width)) {
    return DEFAULT_PANEL_WIDTH;
  }
  let best: number = ALLOWED_PANEL_WIDTHS[0];
  let bestDelta = Math.abs(width - best);
  for (const candidate of ALLOWED_PANEL_WIDTHS) {
    const delta = Math.abs(width - candidate);
    if (delta < bestDelta) {
      best = candidate;
      bestDelta = delta;
    }
  }
  return best;
}

/** Clamp a height into the allowed 1..6 row range. */
export function clampPanelHeight(height: number | undefined): number {
  if (!height || Number.isNaN(height)) {
    return DEFAULT_PANEL_HEIGHT;
  }
  return Math.min(MAX_PANEL_HEIGHT, Math.max(MIN_PANEL_HEIGHT, Math.round(height)));
}

/**
 * Reconcile the stored layout with the panel list and produce a render-ready set
 * of grid placements. The layout blob and the panels list can drift (a panel may
 * lack a layout entry, or a layout entry may point at a deleted panel), so this:
 *
 *  - keeps only layout entries that map to a real panel;
 *  - clamps widths to the allowed presets and heights to 1..6;
 *  - clamps x so the panel never overflows the 12-column grid;
 *  - assigns a sensible fallback placement to panels missing a layout entry
 *    (flowed onto fresh rows below everything else);
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
  const unplaced: DashboardPanel[] = [];

  for (const panel of panels ?? []) {
    const item = layoutById.get(panel.id);
    if (!item) {
      unplaced.push(panel);
      continue;
    }
    const w = snapPanelWidth(item.w);
    const h = clampPanelHeight(item.h);
    const maxX = DASHBOARD_GRID_COLUMNS - w;
    const x = Math.min(Math.max(0, Math.round(item.x ?? 0)), Math.max(0, maxX));
    const y = Math.max(0, Math.round(item.y ?? 0));
    placed.push({ panel, x, y, w, h });
  }

  // Flow any layout-less panels onto rows below the tallest placed row.
  let nextRow = placed.reduce((max, p) => Math.max(max, p.y + p.h), 0);
  for (const panel of unplaced) {
    placed.push({ panel, x: 0, y: nextRow, w: DEFAULT_PANEL_WIDTH, h: DEFAULT_PANEL_HEIGHT });
    nextRow += DEFAULT_PANEL_HEIGHT;
  }

  placed.sort((a, b) => (a.y - b.y) || (a.x - b.x));
  return placed;
}

/**
 * Sum the log counts across every histogram bucket. This is how a "stat" panel
 * derives its single big number: the total match count over the window. Grouped
 * buckets (multiple rows per timestamp) all contribute to the total.
 */
export function sumHistogramCounts(buckets: HistogramData[] | null | undefined): number {
  if (!buckets || buckets.length === 0) {
    return 0;
  }
  return buckets.reduce((total, bucket) => {
    const count = Number(bucket?.log_count);
    return total + (Number.isFinite(count) ? count : 0);
  }, 0);
}

// ---------------------------------------------------------------------------
// Edit-mode layout math.
//
// In edit mode the working state is an ORDERED list of panels; the grid layout
// (x/y placement) is a pure function of that order plus each panel's size. Every
// structural edit (reorder, add, remove, resize) re-runs packLayout so the grid
// always reflows top-left-first into the 12-column grid with no overlaps. Keeping
// this deterministic and pure is what makes it unit-testable.
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
 * panel of the row just closed. Widths snap to presets and heights clamp to 1..6.
 * The result feeds directly back into panels_json.layout.
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
 * Read the size for a panel from an existing layout, falling back to defaults for
 * panels that have no layout entry yet (e.g. a just-added panel).
 */
export function sizeForPanel(layout: DashboardLayoutItem[], id: string): { w: number; h: number } {
  const item = (layout ?? []).find((l) => l.id === id);
  return {
    w: snapPanelWidth(item?.w),
    h: clampPanelHeight(item?.h),
  };
}

/**
 * Rebuild a DashboardPanels blob so its layout is a clean top-left-first packing
 * of `panels` in their current array order, preserving each panel's size (or
 * applying defaults for panels missing a layout entry). This is the single
 * reflow used after add / remove / resize / reorder.
 */
export function reflowPanels(blob: DashboardPanels): DashboardPanels {
  const order: PanelSize[] = (blob.panels ?? []).map((p) => {
    const { w, h } = sizeForPanel(blob.layout ?? [], p.id);
    return { id: p.id, w, h };
  });
  return {
    version: blob.version || DASHBOARD_PANELS_VERSION,
    panels: [...(blob.panels ?? [])],
    layout: packLayout(order),
  };
}

/**
 * Validate a panels blob against the same rules the server enforces on save
 * (pkg/models/dashboards.go). Returns a friendly message on the first violation,
 * or null when valid. The server 400 remains the authoritative backstop; this
 * just surfaces problems inline before the round-trip.
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
  const panels = blob.panels ?? [];
  if (panels.length > MAX_DASHBOARD_PANELS) {
    return `A dashboard can have at most ${MAX_DASHBOARD_PANELS} panels (this one has ${panels.length}).`;
  }
  const seen = new Set<string>();
  for (const p of panels) {
    if (!p.id) {
      return "A panel is missing an id.";
    }
    if (seen.has(p.id)) {
      return `Duplicate panel id "${p.id}".`;
    }
    seen.add(p.id);
    if (!VALID_PANEL_TYPES.includes(p.type)) {
      return `Panel "${p.title || p.id}" has an unsupported type.`;
    }
    if (!p.team_id || p.team_id <= 0) {
      return `Panel "${p.title || p.id}" needs a team.`;
    }
    if (!p.source_id || p.source_id <= 0) {
      return `Panel "${p.title || p.id}" needs a source.`;
    }
    if (!VALID_QUERY_LANGUAGES.includes(p.query_language)) {
      return `Panel "${p.title || p.id}" has an unsupported query language.`;
    }
    if (p.type === "breakdown" && !p.options?.group_by?.trim()) {
      return `Breakdown panel "${p.title || p.id}" requires a group-by field.`;
    }
    const breakdownView = p.options?.breakdown_view as string | undefined;
    if (breakdownView !== undefined) {
      if (p.type !== "breakdown") {
        if (breakdownView !== "") return `Panel "${p.title || p.id}" cannot use a breakdown view.`;
      } else if (breakdownView !== "horizontal-bars" && breakdownView !== "donut") {
        return `Breakdown panel "${p.title || p.id}" has an unsupported breakdown view.`;
      }
    }
  }
  const seenLayoutIds = new Set<string>();
  for (const l of blob.layout ?? []) {
    if (!l.id) {
      return "A panel layout entry is missing an id.";
    }
    if (seenLayoutIds.has(l.id)) {
      return `Duplicate layout id "${l.id}".`;
    }
    seenLayoutIds.add(l.id);
    if (!(ALLOWED_PANEL_WIDTHS as readonly number[]).includes(l.w)) {
      return `Panel layout has an invalid width (${l.w}); allowed: ${ALLOWED_PANEL_WIDTHS.join(", ")}.`;
    }
    if (l.h < MIN_PANEL_HEIGHT || l.h > MAX_PANEL_HEIGHT) {
      return `Panel layout has an invalid height (${l.h}); allowed: ${MIN_PANEL_HEIGHT}-${MAX_PANEL_HEIGHT}.`;
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

export interface GridCell {
  col: number;
  row: number;
}

/** Pixel rect of a grid placement, relative to the canvas origin. */
export interface PixelRect {
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
 * pointer delta, snapped to the allowed width presets and clamped to 1..6 rows.
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
 * packs at the default size, see `reflowPanels`), the ghost is a visual
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
