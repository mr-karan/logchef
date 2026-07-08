import type { HistogramData } from "@/services/HistogramService";
import type {
  DashboardLayoutItem,
  DashboardPanel,
} from "@/api/dashboards";

// The dashboard grid is 12 columns wide. Panel widths are restricted to a small
// set of presets (mirrors the server-side validation in pkg/models/dashboards.go)
// and heights are clamped to 1..6 rows.
export const DASHBOARD_GRID_COLUMNS = 12;
export const ALLOWED_PANEL_WIDTHS = [3, 4, 6, 12] as const;
export const MIN_PANEL_HEIGHT = 1;
export const MAX_PANEL_HEIGHT = 6;
const DEFAULT_PANEL_WIDTH = 6;
const DEFAULT_PANEL_HEIGHT = 2;

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
