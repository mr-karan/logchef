import { describe, it, expect } from "vitest";
import {
  snapPanelWidth,
  clampPanelHeight,
  normalizeDashboardLayout,
  sumHistogramCounts,
  packLayout,
  moveItem,
  sizeForPanel,
  reflowPanels,
  validatePanelsBlob,
  columnTrackWidth,
  pointToCell,
  cellRect,
  cellToMoveIndex,
  previewMoveLayout,
  snapResize,
  previewResizeLayout,
  addTileSlot,
  layoutRowCount,
  copyableDashboardPanels,
  clampDashboardName,
  dashboardCopyName,
  dashboardLayout,
  emptyDashboardPanels,
  switchPanelKind,
  trimDashboardGroupBy,
  withDashboardLayout,
  type PanelSize,
  type GridGeometry,
} from "@/utils/dashboardPanels";
import type {
  DashboardPanel,
  DashboardLayoutItem,
  DashboardPanels,
  DashboardQueryPanel,
} from "@/api/dashboards";

function panel(id: string, overrides: Partial<DashboardQueryPanel> = {}): DashboardQueryPanel {
  return {
    id,
    kind: "query",
    title: id,
    team_id: 1,
    source_id: 1,
    query: { mode: "result", language: "logchefql", text: "" },
    viz: { type: "table" },
    ...overrides,
  };
}

function chart(id: string, query: Partial<DashboardQueryPanel["query"]> = {}, overrides: Partial<DashboardQueryPanel> = {}): DashboardQueryPanel {
  return panel(id, { query: { mode: "count", language: "logchefql", text: "", interval: "auto", ...query }, viz: { type: "chart", mark: "line", stack: "none" }, ...overrides });
}

/** A blob whose single tab holds `layout`. */
function blobOf(panels: DashboardPanel[], layout: DashboardLayoutItem[] = [], rest: Partial<DashboardPanels> = {}): DashboardPanels {
  return { ...emptyDashboardPanels(), ...rest, panels, tabs: [{ id: "main", title: "Main", layout }] };
}

describe("snapPanelWidth", () => {
  it("snaps arbitrary widths to the nearest allowed preset", () => {
    expect(snapPanelWidth(7)).toBe(6); // equidistant 6/8 -> first-wins keeps 6
    expect(snapPanelWidth(5)).toBe(4); // equidistant 4/6 -> first-wins keeps 4
    expect(snapPanelWidth(11)).toBe(12);
    expect(snapPanelWidth(1)).toBe(2);
    expect(snapPanelWidth(3)).toBe(3);
    expect(snapPanelWidth(12)).toBe(12);
    expect(snapPanelWidth(2)).toBe(2);
    expect(snapPanelWidth(8)).toBe(8);
    expect(snapPanelWidth(9)).toBe(9);
  });

  it("falls back to a default for missing/invalid widths", () => {
    expect(snapPanelWidth(undefined)).toBe(6);
    expect(snapPanelWidth(0)).toBe(6);
    expect(snapPanelWidth(NaN)).toBe(6);
  });
});

describe("clampPanelHeight", () => {
  it("clamps into the 1..6 range", () => {
    expect(clampPanelHeight(0)).toBe(2); // 0 is falsy -> default
    expect(clampPanelHeight(3)).toBe(3);
    expect(clampPanelHeight(9)).toBe(6);
    expect(clampPanelHeight(-2)).toBe(1);
    expect(clampPanelHeight(undefined)).toBe(2);
  });
});

describe("normalizeDashboardLayout", () => {
  it("maps layout entries to panels and sorts by (y, x)", () => {
    const panels = [panel("a"), panel("b"), panel("c")];
    const layout: DashboardLayoutItem[] = [
      { id: "b", x: 6, y: 0, w: 6, h: 2 },
      { id: "a", x: 0, y: 0, w: 6, h: 2 },
      { id: "c", x: 0, y: 2, w: 12, h: 3 },
    ];
    const result = normalizeDashboardLayout(panels, layout);
    expect(result.map((r) => r.panel.id)).toEqual(["a", "b", "c"]);
  });

  it("clamps x so a panel cannot overflow the 12-column grid", () => {
    const result = normalizeDashboardLayout(
      [panel("a")],
      [{ id: "a", x: 10, y: 0, w: 6, h: 2 }]
    );
    expect(result[0].w).toBe(6);
    expect(result[0].x).toBe(6); // 12 - 6
  });

  it("snaps invalid widths and clamps invalid heights", () => {
    const result = normalizeDashboardLayout(
      [panel("a")],
      [{ id: "a", x: 0, y: 0, w: 7, h: 99 }]
    );
    expect(result[0].w).toBe(6);
    expect(result[0].h).toBe(6);
  });

  it("drops layout entries whose panel no longer exists", () => {
    const result = normalizeDashboardLayout(
      [panel("a")],
      [
        { id: "a", x: 0, y: 0, w: 6, h: 2 },
        { id: "ghost", x: 6, y: 0, w: 6, h: 2 },
      ]
    );
    expect(result).toHaveLength(1);
    expect(result[0].panel.id).toBe("a");
  });

  it("flows panels with no layout entry onto rows below the placed ones", () => {
    const panels = [panel("a"), panel("orphan")];
    const layout: DashboardLayoutItem[] = [{ id: "a", x: 0, y: 0, w: 12, h: 3 }];
    const result = normalizeDashboardLayout(panels, layout);
    const orphan = result.find((r) => r.panel.id === "orphan")!;
    expect(orphan.y).toBe(3); // below the h=3 panel at y=0
    expect(orphan.x).toBe(0);
  });

  it("tolerates empty inputs", () => {
    expect(normalizeDashboardLayout([], [])).toEqual([]);
  });
});

describe("sumHistogramCounts", () => {
  it("sums log counts across all buckets", () => {
    expect(
      sumHistogramCounts([
        { bucket: "t1", log_count: 11 },
        { bucket: "t2", log_count: 54 },
        { bucket: "t3", log_count: 48 },
      ])
    ).toBe(113);
  });

  it("sums grouped buckets (multiple rows per timestamp)", () => {
    expect(
      sumHistogramCounts([
        { bucket: "t1", log_count: 11, group_value: "api" },
        { bucket: "t1", log_count: 7, group_value: "cdn" },
        { bucket: "t2", log_count: 5, group_value: "api" },
      ])
    ).toBe(23);
  });

  it("returns 0 for empty/nullish input and ignores non-finite counts", () => {
    expect(sumHistogramCounts([])).toBe(0);
    expect(sumHistogramCounts(null)).toBe(0);
    expect(sumHistogramCounts(undefined)).toBe(0);
    expect(
      sumHistogramCounts([
        { bucket: "t1", log_count: Number.NaN },
        { bucket: "t2", log_count: 5 },
      ])
    ).toBe(5);
  });
});

// --- Edit-mode layout math --------------------------------------------------

describe("packLayout", () => {
  it("flows panels left-to-right on a row then wraps to the next", () => {
    const order: PanelSize[] = [
      { id: "a", w: 6, h: 2 },
      { id: "b", w: 6, h: 2 },
      { id: "c", w: 6, h: 2 },
    ];
    const layout = packLayout(order);
    expect(layout).toEqual([
      { id: "a", x: 0, y: 0, w: 6, h: 2 },
      { id: "b", x: 6, y: 0, w: 6, h: 2 },
      { id: "c", x: 0, y: 2, w: 6, h: 2 }, // wrapped below the tallest of row 0
    ]);
  });

  it("wraps below the tallest panel of the row just closed", () => {
    const layout = packLayout([
      { id: "a", w: 6, h: 3 },
      { id: "b", w: 6, h: 1 },
      { id: "c", w: 12, h: 2 },
    ]);
    // row 0 holds a (h3) + b (h1); c wraps to y = max(3,1) = 3
    expect(layout.find((l) => l.id === "c")).toEqual({ id: "c", x: 0, y: 3, w: 12, h: 2 });
  });

  it("snaps widths and clamps heights before packing", () => {
    const layout = packLayout([{ id: "a", w: 7, h: 99 }]);
    expect(layout[0]).toEqual({ id: "a", x: 0, y: 0, w: 6, h: 6 });
  });

  it("packs a full-width panel onto its own row", () => {
    const layout = packLayout([
      { id: "a", w: 12, h: 2 },
      { id: "b", w: 6, h: 2 },
    ]);
    expect(layout).toEqual([
      { id: "a", x: 0, y: 0, w: 12, h: 2 },
      { id: "b", x: 0, y: 2, w: 6, h: 2 },
    ]);
  });

  it("tolerates empty input", () => {
    expect(packLayout([])).toEqual([]);
  });
});

describe("moveItem", () => {
  it("moves an item forward", () => {
    expect(moveItem(["a", "b", "c", "d"], 0, 2)).toEqual(["b", "c", "a", "d"]);
  });
  it("moves an item backward", () => {
    expect(moveItem(["a", "b", "c", "d"], 3, 1)).toEqual(["a", "d", "b", "c"]);
  });
  it("is a no-op for same index and returns a fresh array", () => {
    const input = ["a", "b"];
    const out = moveItem(input, 1, 1);
    expect(out).toEqual(["a", "b"]);
    expect(out).not.toBe(input);
  });
  it("is a no-op for out-of-range from", () => {
    expect(moveItem(["a", "b"], 5, 0)).toEqual(["a", "b"]);
  });
});

// --- Grid-canvas pointer math (edit-mode direct manipulation) --------------

// A tidy geometry: 12 tracks across 1200px with no gap → 100px columns, 80px
// rows. Exact integer math keeps the assertions readable.
const GEOM: GridGeometry = { left: 0, top: 0, width: 1200, gap: 0, rowHeight: 80 };

describe("columnTrackWidth", () => {
  it("splits the canvas into 12 equal tracks when there's no gap", () => {
    expect(columnTrackWidth({ width: 1200, gap: 0 })).toBe(100);
  });
  it("subtracts the 11 inter-track gaps", () => {
    expect(columnTrackWidth({ width: 1200, gap: 10 })).toBeCloseTo(1090 / 12, 5);
  });
});

describe("pointToCell", () => {
  it("maps a pointer inside the canvas to its column and row", () => {
    expect(pointToCell(250, 90, GEOM)).toEqual({ col: 2, row: 1 });
  });
  it("honors the canvas viewport offset", () => {
    const geom: GridGeometry = { ...GEOM, left: 100, top: 50 };
    expect(pointToCell(350, 130, geom)).toEqual({ col: 2, row: 1 });
  });
  it("clamps columns to 0..11 and rows at 0", () => {
    expect(pointToCell(5000, -40, GEOM)).toEqual({ col: 11, row: 0 });
    expect(pointToCell(-40, -40, GEOM)).toEqual({ col: 0, row: 0 });
  });
  it("adds the scroll offset to the row calculation", () => {
    expect(pointToCell(0, 90, GEOM, 80)).toEqual({ col: 0, row: 2 });
  });
});

describe("cellRect", () => {
  it("computes the pixel rect for a placement (no gap)", () => {
    expect(cellRect({ x: 2, y: 1, w: 3, h: 2 }, GEOM)).toEqual({
      left: 200,
      top: 80,
      width: 300,
      height: 160,
    });
  });
  it("accounts for inter-track gaps in offset and span", () => {
    const rect = cellRect({ x: 1, y: 0, w: 2, h: 1 }, { width: 1200, gap: 10, rowHeight: 80 });
    const track = (1200 - 10 * 11) / 12;
    expect(rect.left).toBeCloseTo(track + 10, 5);
    expect(rect.width).toBeCloseTo(track * 2 + 10, 5);
  });
});

describe("cellToMoveIndex", () => {
  // a | b  (top row), c below — dragging c around the a/b row.
  const items: DashboardLayoutItem[] = [
    { id: "a", x: 0, y: 0, w: 6, h: 2 },
    { id: "b", x: 6, y: 0, w: 6, h: 2 },
    { id: "c", x: 0, y: 2, w: 6, h: 2 },
  ];
  it("inserts before a panel when the pointer is in its left half", () => {
    expect(cellToMoveIndex(items, "c", { col: 1, row: 0 })).toBe(0);
  });
  it("inserts after a panel when the pointer is in its right half", () => {
    expect(cellToMoveIndex(items, "c", { col: 4, row: 0 })).toBe(1);
  });
  it("appends when the pointer is in empty space below everything", () => {
    expect(cellToMoveIndex(items, "c", { col: 0, row: 9 })).toBe(2);
  });
});

describe("previewMoveLayout", () => {
  const order: PanelSize[] = [
    { id: "a", w: 6, h: 2 },
    { id: "b", w: 6, h: 2 },
    { id: "c", w: 6, h: 2 },
  ];
  it("re-inserts the dragged panel at the target index and repacks", () => {
    const out = previewMoveLayout(order, "c", 0);
    expect(out.map((i) => i.id)).toEqual(["c", "a", "b"]);
  });
  it("leaves the order untouched for an unknown dragged id", () => {
    const out = previewMoveLayout(order, "ghost", 0);
    expect(out.map((i) => i.id)).toEqual(["a", "b", "c"]);
  });
});

describe("snapResize", () => {
  it("snaps width to an allowed span and rounds height to whole rows", () => {
    expect(snapResize(6, 2, 600, 80, GEOM)).toEqual({ w: 12, h: 3 });
  });
  it("clamps a shrink to the minimum height", () => {
    expect(snapResize(6, 2, -300, -240, GEOM)).toEqual({ w: 3, h: 1 });
  });
});

describe("previewResizeLayout", () => {
  it("applies the new size to one panel and repacks the rest at their sizes", () => {
    const order: PanelSize[] = [
      { id: "a", w: 6, h: 2 },
      { id: "b", w: 6, h: 2 },
    ];
    const out = previewResizeLayout(order, "a", 12, 4);
    const a = out.find((i) => i.id === "a");
    expect(a).toMatchObject({ w: 12, h: 4 });
  });
});

describe("addTileSlot", () => {
  it("returns the slot a new default panel would pack into when it exactly fills the remainder", () => {
    const slot = addTileSlot([{ id: "a", w: 6, h: 2 }]);
    expect(slot).toMatchObject({ x: 6, y: 0, w: 6, h: 2 });
  });
  it("fills the whole canvas width on an empty dashboard", () => {
    expect(addTileSlot([])).toMatchObject({ x: 0, y: 0, w: 12 });
  });
  it("fills a narrow remainder in the bottom row instead of wrapping to an orphaned box", () => {
    // a (w8,h3) leaves only 4 free columns in row 0 — the default 6-wide tile
    // wouldn't fit there, but the tile should still occupy those 4 columns
    // (matching row 0's height) rather than dropping to a fresh row.
    const slot = addTileSlot([{ id: "a", w: 8, h: 3 }]);
    expect(slot).toEqual({ id: "__add__", x: 8, y: 0, w: 4, h: 3 });
  });
  it("wraps to a fresh row at the default size once the bottom row is exactly full", () => {
    const slot = addTileSlot([{ id: "a", w: 12, h: 2 }]);
    expect(slot).toEqual({ id: "__add__", x: 0, y: 2, w: 6, h: 2 });
  });
  it("considers only the bottom row, not earlier rows with free space", () => {
    const order: PanelSize[] = [
      { id: "a", w: 4, h: 2 }, // row 0: 4 used, 8 free (ignored — not the bottom row)
      { id: "b", w: 12, h: 2 }, // row 0 wraps here since 4+12>12; b takes row 1 alone
    ];
    const slot = addTileSlot(order);
    expect(slot).toEqual({ id: "__add__", x: 0, y: 4, w: 6, h: 2 });
  });
});

describe("layoutRowCount", () => {
  it("returns the bottom edge of the lowest panel", () => {
    expect(
      layoutRowCount([
        { id: "a", x: 0, y: 0, w: 6, h: 2 },
        { id: "b", x: 0, y: 2, w: 6, h: 3 },
      ])
    ).toBe(5);
  });
  it("is 0 for an empty layout", () => {
    expect(layoutRowCount([])).toBe(0);
  });
});

describe("sizeForPanel", () => {
  it("reads an existing layout entry", () => {
    const layout: DashboardLayoutItem[] = [{ id: "a", x: 0, y: 0, w: 4, h: 3 }];
    expect(sizeForPanel(layout, "a")).toEqual({ w: 4, h: 3 });
  });
  it("falls back to defaults for a panel with no layout entry", () => {
    expect(sizeForPanel([], "new")).toEqual({ w: 6, h: 2 });
  });
});

describe("reflowPanels", () => {
  const blob = blobOf;

  it("re-packs layout in panel array order, preserving each panel's size", () => {
    const panels = [panel("a"), panel("b"), panel("c")];
    const layout: DashboardLayoutItem[] = [
      { id: "a", x: 6, y: 4, w: 6, h: 2 },
      { id: "b", x: 0, y: 0, w: 4, h: 1 },
      { id: "c", x: 0, y: 9, w: 12, h: 3 },
    ];
    const out = reflowPanels(blob(panels, layout));
    expect(dashboardLayout(out)).toEqual([
      { id: "a", x: 0, y: 0, w: 6, h: 2 },
      { id: "b", x: 6, y: 0, w: 4, h: 1 },
      { id: "c", x: 0, y: 2, w: 12, h: 3 },
    ]);
  });

  it("keeps the blob's other fields, such as the cache TTL and the tab", () => {
    const out = reflowPanels(blobOf([panel("a")], [], { cache_ttl_seconds: 0, features: [] }));
    expect(out.cache_ttl_seconds).toBe(0);
    expect(out.tabs.map((t) => t.id)).toEqual(["main"]);
    expect(out.features).toEqual([]);
  });

  it("places a just-added (layout-less) panel in the first free slot at default size", () => {
    // a=6-wide occupies the left half of row 0; the appended "new" panel (default
    // w6 h2) fits in the free right half of that same row.
    const panels = [panel("a"), panel("new")];
    const layout: DashboardLayoutItem[] = [{ id: "a", x: 0, y: 0, w: 6, h: 2 }];
    const out = reflowPanels(blob(panels, layout));
    expect(dashboardLayout(out)).toEqual([
      { id: "a", x: 0, y: 0, w: 6, h: 2 },
      { id: "new", x: 6, y: 0, w: 6, h: 2 },
    ]);
  });

  it("reflows to close the gap after a panel is removed", () => {
    // Removing "b" from [a,b,c] leaves [a,c]; c slides up next to a.
    const panels = [panel("a"), panel("c")];
    const layout: DashboardLayoutItem[] = [
      { id: "a", x: 0, y: 0, w: 6, h: 2 },
      { id: "c", x: 0, y: 4, w: 6, h: 2 },
    ];
    const out = reflowPanels(blob(panels, layout));
    expect(dashboardLayout(out)).toEqual([
      { id: "a", x: 0, y: 0, w: 6, h: 2 },
      { id: "c", x: 6, y: 0, w: 6, h: 2 },
    ]);
  });

  it("reflows after a resize widens a panel", () => {
    const panels = [panel("a"), panel("b")];
    const layout: DashboardLayoutItem[] = [
      { id: "a", x: 0, y: 0, w: 12, h: 2 }, // widened to full row
      { id: "b", x: 6, y: 0, w: 6, h: 2 },
    ];
    const out = reflowPanels(blob(panels, layout));
    expect(dashboardLayout(out)).toEqual([
      { id: "a", x: 0, y: 0, w: 12, h: 2 },
      { id: "b", x: 0, y: 2, w: 6, h: 2 }, // pushed to its own row
    ]);
  });
});

describe("validatePanelsBlob", () => {
  function base(overrides: Partial<DashboardPanels> = {}): DashboardPanels {
    return { ...blobOf([panel("a")], [{ id: "a", x: 0, y: 0, w: 6, h: 2 }]), ...overrides };
  }
  function withLayout(layout: DashboardLayoutItem[]): DashboardPanels {
    return withDashboardLayout(base(), layout);
  }
  function withPanel(p: DashboardQueryPanel): DashboardPanels {
    return blobOf([p], [{ id: p.id, x: 0, y: 0, w: 6, h: 2 }]);
  }

  it("returns null for a valid blob of every kind", () => {
    expect(validatePanelsBlob(base())).toBeNull();
    expect(validatePanelsBlob(withPanel(chart("c", { group_by: "service", translate_limit: 50 })))).toBeNull();
    expect(validatePanelsBlob(withPanel(chart("s", {}, { viz: { type: "stat", reduce: "sum" } })))).toBeNull();
    expect(validatePanelsBlob(withPanel(chart("p", { group_by: "host" }, { viz: { type: "pie", donut: true } })))).toBeNull();
    expect(validatePanelsBlob(withPanel(chart("t", { group_by: "host" }, { viz: { type: "toplist" } })))).toBeNull();
    expect(validatePanelsBlob(withPanel(panel("tbl", { query: { mode: "result", language: "clickhouse-sql", text: "SELECT 1", limit: 100 }, viz: { type: "table", visible_columns: ["a"] } })))).toBeNull();
    expect(validatePanelsBlob(emptyDashboardPanels())).toBeNull();
  });

  it("rejects an unsupported version", () => {
    expect(validatePanelsBlob(base({ version: 1 }))).toMatch(/version/i);
  });

  it("requires exactly one tab in this release", () => {
    expect(validatePanelsBlob(base({ tabs: [] }))).toMatch(/at least one tab/i);
    expect(validatePanelsBlob(base({ tabs: [...base().tabs, { id: "b", title: "B", layout: [] }] }))).toMatch(/at most 1 tab/i);
    expect(validatePanelsBlob(base({ tabs: [{ id: "main", title: "", layout: [] }], panels: [] }))).toMatch(/tab titles/i);
  });

  it("rejects more than 24 panels", () => {
    const panels = Array.from({ length: 25 }, (_, i) => panel(`p${i}`));
    expect(validatePanelsBlob(blobOf(panels))).toMatch(/at most 24/i);
  });

  it("rejects duplicate panel ids", () => {
    expect(validatePanelsBlob(blobOf([panel("dup"), panel("dup")]))).toMatch(/duplicate/i);
  });

  it("rejects an invalid viz type and kind", () => {
    const bad = { ...panel("a"), viz: { type: "gauge" } } as unknown as DashboardQueryPanel;
    expect(validatePanelsBlob(withPanel(bad))).toMatch(/type/i);
    const text = { ...panel("a"), kind: "text" } as unknown as DashboardQueryPanel;
    expect(validatePanelsBlob(withPanel(text))).toMatch(/kind/i);
  });

  it("requires a team and a source", () => {
    expect(validatePanelsBlob(withPanel(panel("a", { team_id: 0 })))).toMatch(/team/i);
    expect(validatePanelsBlob(withPanel(panel("a", { source_id: 0 })))).toMatch(/source/i);
  });

  it("rejects invalid layout width/height", () => {
    expect(validatePanelsBlob(withLayout([{ id: "a", x: 0, y: 0, w: 5, h: 2 }]))).toMatch(/width/i);
    expect(validatePanelsBlob(withLayout([{ id: "a", x: 0, y: 0, w: 6, h: 9 }]))).toMatch(/height/i);
  });

  it("accepts the finer grid presets (2/8/9-wide panels)", () => {
    for (const w of [2, 8, 9]) {
      expect(validatePanelsBlob(withLayout([{ id: "a", x: 0, y: 0, w, h: 2 }]))).toBeNull();
    }
  });

  it("rejects an invalid query language and accepts every valid one", () => {
    const bogus = { ...panel("a"), query: { ...panel("a").query, language: "bogus" } } as unknown as DashboardQueryPanel;
    expect(validatePanelsBlob(withPanel(bogus))).toMatch(/query language/i);
    for (const language of ["logchefql", "clickhouse-sql", "logsql"] as const) {
      expect(validatePanelsBlob(withPanel(panel("a", { query: { mode: "result", language, text: "" } })))).toBeNull();
    }
  });

  it("rejects a layout entry with an empty id and duplicate layout ids", () => {
    expect(validatePanelsBlob(withLayout([{ id: "", x: 0, y: 0, w: 6, h: 2 }]))).toMatch(/missing an id/i);
    expect(validatePanelsBlob(withLayout([
      { id: "a", x: 0, y: 0, w: 6, h: 2 },
      { id: "a", x: 6, y: 0, w: 6, h: 2 },
    ]))).toMatch(/duplicate/i);
  });

  it("ties the mode to the viz: tables need result mode and result stats need a value", () => {
    expect(validatePanelsBlob(withPanel(chart("a", {}, { viz: { type: "table" } })))).toMatch(/result mode/i);
    expect(validatePanelsBlob(withPanel(panel("a", { viz: { type: "stat" } })))).toMatch(/value column/i);
  });

  it("checks the group-by rules", () => {
    expect(validatePanelsBlob(withPanel(chart("a", { group_by: "  \u0085" })))).toMatch(/blank group-by/i);
    expect(validatePanelsBlob(withPanel(chart("a", { group_by: "\ufeff" })))).toBeNull();
    expect(validatePanelsBlob(withPanel(chart("a", { group_by: "host" }, { viz: { type: "pie" } })))).toBeNull();
    expect(validatePanelsBlob(withPanel(chart("a", {}, { viz: { type: "pie" } })))).toMatch(/group-by/i);
    expect(validatePanelsBlob(withPanel(chart("a", {}, { viz: { type: "toplist" } })))).toMatch(/group-by/i);
    expect(validatePanelsBlob(withPanel(panel("a", { query: { mode: "result", language: "logchefql", text: "", group_by: "x" } })))).toMatch(/group/i);
  });

  it("checks the limit rules", () => {
    expect(validatePanelsBlob(withPanel(panel("a", { query: { mode: "result", language: "logchefql", text: "", limit: 0 } })))).toMatch(/row limit/i);
    expect(validatePanelsBlob(withPanel(panel("a", { query: { mode: "result", language: "logchefql", text: "", limit: 10001 } })))).toMatch(/row limit/i);
    expect(validatePanelsBlob(withPanel(chart("a", { limit: 10 })))).toMatch(/limit/i);
    expect(validatePanelsBlob(withPanel(chart("a", { language: "clickhouse-sql", translate_limit: 10 })))).toMatch(/translate limit/i);
    expect(validatePanelsBlob(withPanel(chart("a", { translate_limit: 100001 })))).toMatch(/translate limit/i);
    expect(validatePanelsBlob(withPanel(chart("a", { interval: "24h" as never })))).toMatch(/interval/i);
  });

  it("checks the visible columns of a table", () => {
    expect(validatePanelsBlob(withPanel(panel("a", { viz: { type: "table", visible_columns: [] } })))).toMatch(/no columns/i);
    expect(validatePanelsBlob(withPanel(panel("a", { viz: { type: "table", visible_columns: [""] } })))).toMatch(/empty column/i);
    expect(validatePanelsBlob(withPanel(panel("a", { viz: { type: "table", visible_columns: ["a", "a"] } })))).toMatch(/twice/i);
  });

  it("rejects a locked panel", () => {
    const locked = { ...panel("a"), locked: true } as unknown as DashboardQueryPanel;
    expect(validatePanelsBlob(withPanel(locked))).toMatch(/locked/i);
  });
});

describe("B11: degenerate grid geometry", () => {
  // A hidden/zero-measured canvas (getBoundingClientRect() returns all zeros,
  // e.g. before layout/paint, or an element with display:none) must not blow
  // up the pure grid math with NaN/Infinity from a division by zero.
  const DEGENERATE: GridGeometry = { left: 0, top: 0, width: 0, gap: 0, rowHeight: 0 };

  it("columnTrackWidth clamps to a finite, non-negative width", () => {
    expect(columnTrackWidth(DEGENERATE)).toBe(0);
    expect(Number.isFinite(columnTrackWidth(DEGENERATE))).toBe(true);
  });

  it("pointToCell returns a finite, clamped cell instead of NaN/Infinity", () => {
    const cell = pointToCell(50, 50, DEGENERATE);
    expect(Number.isFinite(cell.col)).toBe(true);
    expect(Number.isFinite(cell.row)).toBe(true);
    expect(cell).toEqual({ col: 0, row: 0 });
  });

  it("snapResize returns finite, clamped dimensions instead of NaN/Infinity", () => {
    const size = snapResize(6, 2, 100, 100, DEGENERATE);
    expect(Number.isFinite(size.w)).toBe(true);
    expect(Number.isFinite(size.h)).toBe(true);
    // Degenerate geometry can't inform a resize, so it's a safe no-op:
    // the starting size passes straight through the clamp helpers.
    expect(size).toEqual({ w: snapPanelWidth(6), h: clampPanelHeight(2) });
  });

  it("cellToMoveIndex stays finite even fed a degenerate-geometry cell", () => {
    const items: DashboardLayoutItem[] = [
      { id: "a", x: 0, y: 0, w: 6, h: 2 },
      { id: "b", x: 6, y: 0, w: 6, h: 2 },
    ];
    const cell = pointToCell(50, 50, DEGENERATE);
    const index = cellToMoveIndex(items, "a", cell);
    expect(Number.isFinite(index)).toBe(true);
    expect(index).toBe(0);
  });
});

describe("trimDashboardGroupBy", () => {
  it("trims the Unicode White_Space set like Go, keeping U+FEFF", () => {
    expect(trimDashboardGroupBy("  service ")).toBe("service");
    expect(trimDashboardGroupBy("\u0085service\u0085")).toBe("service");
    expect(trimDashboardGroupBy("\ufeffservice")).toBe("\ufeffservice");
    expect(trimDashboardGroupBy("\u0085")).toBe("");
    expect(trimDashboardGroupBy(undefined)).toBe("");
  });
});

describe("dashboardCopyName", () => {
  const format = (name: string) => `${name} (copy)`;

  it("keeps a short name whole", () => {
    expect(dashboardCopyName("Ops", format)).toBe("Ops (copy)");
  });

  it("cuts the base by code points until the localized name fits 200", () => {
    const name = "é".repeat(200);
    const copy = dashboardCopyName(name, format);
    expect(Array.from(copy)).toHaveLength(200);
    expect(copy.endsWith(" (copy)")).toBe(true);
    expect(copy.startsWith("é".repeat(193))).toBe(true);
  });

  it("never splits a surrogate pair", () => {
    const name = "😀".repeat(200);
    const copy = dashboardCopyName(name, format);
    expect(Array.from(copy)).toHaveLength(200);
    expect(copy).toBe("😀".repeat(193) + " (copy)");
    expect(clampDashboardName(name)).toBe("😀".repeat(200));
    expect(Array.from(clampDashboardName("😀".repeat(201)))).toHaveLength(200);
  });
});

describe("switchPanelKind", () => {
  const source = chart("a", { group_by: "host", translate_limit: 50, text: "level=error" });

  it("keeps the text, language, team and source and drops what the kind does not use", () => {
    const stat = switchPanelKind(source, "stat");
    expect(stat.query).toEqual({ mode: "count", language: "logchefql", text: "level=error", interval: "auto", translate_limit: 50 });
    expect(stat.viz).toEqual({ type: "stat", reduce: "sum" });
    const table = switchPanelKind(source, "table");
    expect(table.query).toEqual({ mode: "result", language: "logchefql", text: "level=error" });
    expect(table.viz).toEqual({ type: "table" });
    const breakdown = switchPanelKind(source, "breakdown");
    expect(breakdown.query.group_by).toBe("host");
    expect(breakdown.viz).toEqual({ type: "toplist" });
    expect(switchPanelKind(breakdown, "chart").viz).toEqual({ type: "chart", mark: "line", stack: "none" });
  });

  it("returns the same panel for the same kind", () => {
    expect(switchPanelKind(source, "chart")).toBe(source);
    const pie = chart("p", { group_by: "h" }, { viz: { type: "pie", donut: true } });
    expect(switchPanelKind(pie, "breakdown")).toBe(pie);
  });
});

describe("copyableDashboardPanels", () => {
  const anyTeam = () => true;
  const locked = { id: "b", kind: "query", title: "b", team_id: 1, source_id: 1, viz: { type: "table" }, locked: true } as const;

  it("leaves out locked panels and their layout entries", () => {
    const blob = blobOf([panel("a"), locked, panel("c")], [
      { id: "a", x: 0, y: 0, w: 6, h: 2 },
      { id: "b", x: 6, y: 0, w: 6, h: 2 },
      { id: "c", x: 0, y: 2, w: 12, h: 2 },
    ], { cache_ttl_seconds: 60 });
    const { panels, skipped } = copyableDashboardPanels(blob, anyTeam);
    expect(skipped).toBe(1);
    expect(panels.panels.map((p) => p.id)).toEqual(["a", "c"]);
    expect(dashboardLayout(panels).map((l) => l.id)).toEqual(["a", "c"]);
    expect(panels.cache_ttl_seconds).toBe(60);
    expect(panels.version).toBe(2);
  });

  it("returns a copy that does not share references with the source", () => {
    const blob = blobOf([panel("a")]);
    const { panels } = copyableDashboardPanels(blob, anyTeam);
    panels.panels[0].title = "changed";
    expect(blob.panels[0].title).toBe("a");
  });

  it("leaves out panels whose team the caller cannot use", () => {
    const blob = blobOf([panel("a", { team_id: 1 }), panel("b", { team_id: 2 })]);
    const { panels, skipped } = copyableDashboardPanels(blob, (teamId) => teamId === 1);
    expect(panels.panels.map((p) => p.id)).toEqual(["a"]);
    expect(skipped).toBe(1);
  });
});
