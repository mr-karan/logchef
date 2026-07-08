import type { HistogramData } from "@/services/HistogramService";
import type {
  DashboardLayoutItem,
  DashboardPanel,
  DashboardPanels,
  DashboardPanelType,
} from "@/api/dashboards";

// The dashboard grid is 12 columns wide. Panel widths are restricted to a small
// set of presets (mirrors the server-side validation in pkg/models/dashboards.go)
// and heights are clamped to 1..6 rows.
export const DASHBOARD_GRID_COLUMNS = 12;
export const ALLOWED_PANEL_WIDTHS = [3, 4, 6, 12] as const;
export const MIN_PANEL_HEIGHT = 1;
export const MAX_PANEL_HEIGHT = 6;
export const DEFAULT_PANEL_WIDTH = 6;
export const DEFAULT_PANEL_HEIGHT = 2;
export const MAX_DASHBOARD_PANELS = 24;
export const DASHBOARD_PANELS_VERSION = 1;

const VALID_PANEL_TYPES: readonly DashboardPanelType[] = ["timeseries", "stat", "table"];

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
 * Reorder `ids` by moving `draggedId` to sit at `targetId`'s position (the drag-
 * and-drop reorder primitive). Dropping a panel onto itself, or referencing an
 * unknown id, leaves the order unchanged.
 */
export function reorderByTarget(ids: string[], draggedId: string, targetId: string): string[] {
  const from = ids.indexOf(draggedId);
  const to = ids.indexOf(targetId);
  if (from === -1 || to === -1 || from === to) {
    return ids.slice();
  }
  return moveItem(ids, from, to);
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
  }
  for (const l of blob.layout ?? []) {
    if (!(ALLOWED_PANEL_WIDTHS as readonly number[]).includes(l.w)) {
      return `Panel layout has an invalid width (${l.w}); allowed: ${ALLOWED_PANEL_WIDTHS.join(", ")}.`;
    }
    if (l.h < MIN_PANEL_HEIGHT || l.h > MAX_PANEL_HEIGHT) {
      return `Panel layout has an invalid height (${l.h}); allowed: ${MIN_PANEL_HEIGHT}-${MAX_PANEL_HEIGHT}.`;
    }
  }
  return null;
}
