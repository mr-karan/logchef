import { describe, it, expect } from "vitest";
import {
  snapPanelWidth,
  clampPanelHeight,
  normalizeDashboardLayout,
  packLayout,
  moveItem,
  reflowTab,
  addPanelToTab,
  movePanelToTab,
  removeTab,
  dashboardTab,
  tabLayout,
  panelTabId,
  validTabTitle,
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
  emptyDashboardPanels,
  isDashboardRange,
  resultInterval,
  switchPanelKind,
  trimDashboardGroupBy,
  withTabLayout,
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

/**
 * A two-tab blob shaped like the corpus dashboard: "today" holds a, b and c,
 * "week" holds d and e. The panel list interleaves the tabs, so a tab's order
 * is a subsequence of it.
 */
function twoTabs(): DashboardPanels {
  return {
    ...emptyDashboardPanels(),
    panels: [panel("a"), panel("d"), panel("b"), panel("e"), panel("c")],
    tabs: [
      { id: "today", title: "Current day", layout: [
        { id: "a", x: 0, y: 0, w: 3, h: 2 },
        { id: "b", x: 3, y: 0, w: 5, h: 4 },
        { id: "c", x: 8, y: 0, w: 4, h: 4 },
      ] },
      { id: "week", title: "Last 7 days", layout: [
        { id: "d", x: 0, y: 0, w: 12, h: 1 },
        { id: "e", x: 0, y: 1, w: 7, h: 9 },
      ] },
    ],
  };
}

describe("snapPanelWidth", () => {
  it("keeps every whole width from 2 to 12", () => {
    for (let w = 2; w <= 12; w++) expect(snapPanelWidth(w)).toBe(w);
  });

  it("rounds to whole columns and clamps into 2..12", () => {
    expect(snapPanelWidth(4.4)).toBe(4);
    expect(snapPanelWidth(4.6)).toBe(5);
    expect(snapPanelWidth(1)).toBe(2);
    expect(snapPanelWidth(-3)).toBe(2);
    expect(snapPanelWidth(13)).toBe(12);
  });

  it("falls back to a default for missing/invalid widths", () => {
    expect(snapPanelWidth(undefined)).toBe(6);
    expect(snapPanelWidth(0)).toBe(6);
    expect(snapPanelWidth(NaN)).toBe(6);
  });
});

describe("clampPanelHeight", () => {
  it("clamps into the 1..12 range", () => {
    expect(clampPanelHeight(0)).toBe(2); // 0 is falsy -> default
    expect(clampPanelHeight(3)).toBe(3);
    expect(clampPanelHeight(9)).toBe(9);
    expect(clampPanelHeight(12)).toBe(12);
    expect(clampPanelHeight(13)).toBe(12);
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

  it("keeps a free width and clamps invalid sizes", () => {
    const free = normalizeDashboardLayout([panel("a")], [{ id: "a", x: 0, y: 0, w: 7, h: 11 }]);
    expect(free[0]).toMatchObject({ w: 7, h: 11 });
    const invalid = normalizeDashboardLayout([panel("a")], [{ id: "a", x: 0, y: 0, w: 40, h: 99 }]);
    expect(invalid[0]).toMatchObject({ w: 12, h: 12 });
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

  it("places only the panels of the tab whose layout it is given", () => {
    const blob = twoTabs();
    const today = normalizeDashboardLayout(blob.panels, tabLayout(blob, "today"));
    expect(today.map((r) => r.panel.id)).toEqual(["a", "b", "c"]);
    const week = normalizeDashboardLayout(blob.panels, tabLayout(blob, "week"));
    expect(week.map((r) => [r.panel.id, r.x, r.y, r.w, r.h])).toEqual([["d", 0, 0, 12, 1], ["e", 0, 1, 7, 9]]);
  });

  it("tolerates empty inputs", () => {
    expect(normalizeDashboardLayout([], [])).toEqual([]);
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

  it("clamps widths and heights before packing", () => {
    const layout = packLayout([{ id: "a", w: 40, h: 99 }]);
    expect(layout[0]).toEqual({ id: "a", x: 0, y: 0, w: 12, h: 12 });
  });

  it("packs free widths side by side and wraps when the next does not fit", () => {
    const layout = packLayout([
      { id: "a", w: 5, h: 2 },
      { id: "b", w: 7, h: 8 },
      { id: "c", w: 11, h: 1 },
    ]);
    expect(layout).toEqual([
      { id: "a", x: 0, y: 0, w: 5, h: 2 },
      { id: "b", x: 5, y: 0, w: 7, h: 8 },
      { id: "c", x: 0, y: 8, w: 11, h: 1 },
    ]);
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
  it("stops on any whole column from 2 to 12, and on any row up to 12", () => {
    const stride = (GEOM.width + GEOM.gap) / 12;
    const rowStride = GEOM.rowHeight + GEOM.gap;
    expect(snapResize(6, 2, -stride, 0, GEOM).w).toBe(5);
    expect(snapResize(6, 2, stride, 0, GEOM).w).toBe(7);
    expect(snapResize(6, 2, 4.4 * stride, 0, GEOM).w).toBe(10);
    expect(snapResize(6, 2, -9 * stride, 0, GEOM).w).toBe(2);
    expect(snapResize(6, 2, 0, 5 * rowStride, GEOM).h).toBe(7);
    expect(snapResize(6, 2, 0, 40 * rowStride, GEOM).h).toBe(12);
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

describe("reflowTab", () => {
  it("re-packs a tab in panel array order, preserving each panel's size", () => {
    const panels = [panel("a"), panel("b"), panel("c")];
    const layout: DashboardLayoutItem[] = [
      { id: "a", x: 6, y: 4, w: 6, h: 2 },
      { id: "b", x: 0, y: 0, w: 4, h: 1 },
      { id: "c", x: 0, y: 9, w: 12, h: 3 },
    ];
    const out = reflowTab(blobOf(panels, layout), "main");
    expect(tabLayout(out, "main")).toEqual([
      { id: "a", x: 0, y: 0, w: 6, h: 2 },
      { id: "b", x: 6, y: 0, w: 4, h: 1 },
      { id: "c", x: 0, y: 2, w: 12, h: 3 },
    ]);
  });

  it("keeps the blob's other fields, such as the cache TTL and the tab", () => {
    const out = reflowTab(blobOf([panel("a")], [{ id: "a", x: 3, y: 3, w: 6, h: 2 }], { cache_ttl_seconds: 0, features: [] }), "main");
    expect(out.cache_ttl_seconds).toBe(0);
    expect(out.tabs.map((tab) => [tab.id, tab.title])).toEqual([["main", "Main"]]);
    expect(out.features).toEqual([]);
  });

  it("reflows to close the gap after a panel is removed", () => {
    // Removing "b" from [a,b,c] leaves [a,c]; c slides up next to a.
    const panels = [panel("a"), panel("c")];
    const layout: DashboardLayoutItem[] = [
      { id: "a", x: 0, y: 0, w: 6, h: 2 },
      { id: "c", x: 0, y: 4, w: 6, h: 2 },
    ];
    const out = reflowTab(blobOf(panels, layout), "main");
    expect(tabLayout(out, "main")).toEqual([
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
    const out = reflowTab(blobOf(panels, layout), "main");
    expect(tabLayout(out, "main")).toEqual([
      { id: "a", x: 0, y: 0, w: 12, h: 2 },
      { id: "b", x: 0, y: 2, w: 6, h: 2 }, // pushed to its own row
    ]);
  });

  it("packs each tab from its own panels, in their order in the panel list", () => {
    // A resize in "week" made e full width: only that tab changes, and it
    // packs d then e, skipping the panels of "today" between them in the list.
    const blob = twoTabs();
    const resized = withTabLayout(blob, "week", [
      { id: "e", x: 0, y: 1, w: 12, h: 9 },
      { id: "d", x: 0, y: 0, w: 6, h: 1 },
    ]);
    const out = reflowTab(resized, "week");
    expect(tabLayout(out, "week")).toEqual([
      { id: "d", x: 0, y: 0, w: 6, h: 1 },
      { id: "e", x: 0, y: 1, w: 12, h: 9 },
    ]);
    expect(tabLayout(out, "today")).toBe(tabLayout(blob, "today"));
  });

  it("leaves another tab alone even when that tab is not packed", () => {
    const blob = twoTabs();
    const loose = withTabLayout(blob, "today", [
      { id: "a", x: 0, y: 5, w: 3, h: 2 },
      { id: "b", x: 6, y: 5, w: 5, h: 4 },
      { id: "c", x: 0, y: 9, w: 4, h: 4 },
    ]);
    expect(tabLayout(reflowTab(loose, "week"), "today")).toBe(tabLayout(loose, "today"));
    expect(tabLayout(reflowTab(loose, "today"), "today")).toEqual([
      { id: "a", x: 0, y: 0, w: 3, h: 2 },
      { id: "b", x: 3, y: 0, w: 5, h: 4 },
      { id: "c", x: 8, y: 0, w: 4, h: 4 },
    ]);
  });

  it("returns the blob for a tab it does not have", () => {
    const blob = twoTabs();
    expect(reflowTab(blob, "nope").tabs).toEqual(blob.tabs);
  });
});

describe("tabs", () => {
  it("resolves the requested tab, and the first tab for an unknown or absent id", () => {
    const blob = twoTabs();
    expect(dashboardTab(blob, "week")?.id).toBe("week");
    expect(dashboardTab(blob, "deleted")?.id).toBe("today");
    expect(dashboardTab(blob, null)?.id).toBe("today");
    expect(dashboardTab({ tabs: [] }, "week")).toBeUndefined();
  });

  it("finds the tab that places a panel", () => {
    const blob = twoTabs();
    expect(panelTabId(blob, "b")).toBe("today");
    expect(panelTabId(blob, "e")).toBe("week");
    expect(panelTabId(blob, "ghost")).toBeUndefined();
  });

  it("adds a panel to one tab in the next free slot at the default size", () => {
    // "week" ends with e (7 wide, 9 tall) at y=1: the 6-wide default does not
    // fit in the 5 columns beside it, so it wraps below.
    const out = addPanelToTab(twoTabs(), "week", panel("new"));
    expect(out.panels.map((p) => p.id)).toEqual(["a", "d", "b", "e", "c", "new"]);
    expect(tabLayout(out, "week")).toEqual([
      { id: "d", x: 0, y: 0, w: 12, h: 1 },
      { id: "e", x: 0, y: 1, w: 7, h: 9 },
      { id: "new", x: 0, y: 10, w: 6, h: 2 },
    ]);
    expect(tabLayout(out, "today")).toEqual(tabLayout(twoTabs(), "today"));
  });

  it("moves a panel to the end of another tab at its size and re-packs both", () => {
    const out = movePanelToTab(twoTabs(), "a", "week");
    // "today" closes the gap a left: b and c slide to the left edge.
    expect(tabLayout(out, "today")).toEqual([
      { id: "b", x: 0, y: 0, w: 5, h: 4 },
      { id: "c", x: 5, y: 0, w: 4, h: 4 },
    ]);
    // a (3 wide) lands after e (7 wide) on the same row.
    expect(tabLayout(out, "week")).toEqual([
      { id: "d", x: 0, y: 0, w: 12, h: 1 },
      { id: "e", x: 0, y: 1, w: 7, h: 9 },
      { id: "a", x: 7, y: 1, w: 3, h: 2 },
    ]);
    expect(out.panels.map((p) => p.id).sort()).toEqual(["a", "b", "c", "d", "e"]);
    expect(panelTabId(out, "a")).toBe("week");
  });

  it("does not move a panel to its own tab, to an unknown tab, or an unknown panel", () => {
    const blob = twoTabs();
    expect(movePanelToTab(blob, "a", "today")).toBe(blob);
    expect(movePanelToTab(blob, "a", "nope")).toBe(blob);
    expect(movePanelToTab(blob, "ghost", "week")).toBe(blob);
  });

  it("removes a tab together with the panels it places", () => {
    const out = removeTab(twoTabs(), "today");
    expect(out.tabs.map((tab) => tab.id)).toEqual(["week"]);
    expect(out.panels.map((p) => p.id)).toEqual(["d", "e"]);
    expect(validatePanelsBlob(out)).toBeNull();
  });

  it("stores a trimmed tab title of 1 to 60 characters", () => {
    expect(validTabTitle("  Last 7 days ")).toBe("Last 7 days");
    expect(validTabTitle("   ")).toBeNull();
    expect(validTabTitle("é".repeat(60))).toBe("é".repeat(60));
    expect(validTabTitle("é".repeat(61))).toBeNull();
    // 60 code points, 120 UTF-16 units.
    expect(validTabTitle("😀".repeat(60))).toBe("😀".repeat(60));
  });
});

describe("validatePanelsBlob", () => {
  function base(overrides: Partial<DashboardPanels> = {}): DashboardPanels {
    return { ...blobOf([panel("a")], [{ id: "a", x: 0, y: 0, w: 6, h: 2 }]), ...overrides };
  }
  function withLayout(layout: DashboardLayoutItem[]): DashboardPanels {
    return withTabLayout(base(), "main", layout);
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

  it("accepts 1 to 10 tabs with unique ids and titles of 1 to 60 characters", () => {
    const tabs = (n: number) => Array.from({ length: n }, (_, i) => ({ id: `t${i}`, title: `Tab ${i}`, layout: [] }));
    expect(validatePanelsBlob(twoTabs())).toBeNull();
    expect(validatePanelsBlob(base({ tabs: tabs(10), panels: [] }))).toBeNull();
    expect(validatePanelsBlob(base({ tabs: [] }))).toMatch(/at least one tab/i);
    expect(validatePanelsBlob(base({ tabs: tabs(11), panels: [] }))).toMatch(/at most 10 tabs/i);
    expect(validatePanelsBlob(base({ tabs: [{ id: "main", title: "", layout: [] }], panels: [] }))).toMatch(/tab titles/i);
    expect(validatePanelsBlob(base({ tabs: [{ id: "main", title: "x".repeat(61), layout: [] }], panels: [] }))).toMatch(/tab titles/i);
    expect(validatePanelsBlob(base({ tabs: [...tabs(1), ...tabs(1)], panels: [] }))).toMatch(/duplicate tab id/i);
    expect(validatePanelsBlob(base({ tabs: [{ id: "", title: "A", layout: [] }], panels: [] }))).toMatch(/tab is missing an id/i);
  });

  it("rejects a panel that two tabs place", () => {
    const blob = twoTabs();
    const twice = withTabLayout(blob, "week", [...tabLayout(blob, "week"), { id: "a", x: 7, y: 1, w: 3, h: 2 }]);
    expect(validatePanelsBlob(twice)).toMatch(/duplicate layout id "a"/i);
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

  it("rejects a layout width outside 2..12 and a height outside 1..12", () => {
    for (const w of [1, 13, 5.5]) {
      expect(validatePanelsBlob(withLayout([{ id: "a", x: 0, y: 0, w, h: 2 }]))).toMatch(/width/i);
    }
    for (const h of [0, 13, 2.5]) {
      expect(validatePanelsBlob(withLayout([{ id: "a", x: 0, y: 0, w: 6, h }]))).toMatch(/height/i);
    }
  });

  it("accepts every whole width from 2 to 12 and every height from 1 to 12", () => {
    for (let w = 2; w <= 12; w++) {
      expect(validatePanelsBlob(withLayout([{ id: "a", x: 0, y: 0, w, h: 2 }]))).toBeNull();
    }
    for (let h = 1; h <= 12; h++) {
      expect(validatePanelsBlob(withLayout([{ id: "a", x: 0, y: 0, w: 6, h }]))).toBeNull();
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

  it("validates result chart roles, bounds and future options", () => {
    for (const viz of [
      { type: "chart", x: "day", y: ["a", "b"], x_scale: "category", show_values: true },
      { type: "pie", label: "service", value: "n", min_slice_percent: 1 },
      { type: "toplist", label: "service", value: "n", max_rows: 20 },
    ] satisfies DashboardQueryPanel["viz"][]) {
      expect(validatePanelsBlob(withPanel(panel("a", { viz })))).toBeNull();
      expect(validatePanelsBlob(withPanel(panel("a", { viz: { ...viz, thresholds: [] } as unknown as DashboardQueryPanel["viz"] })))).toMatch(viz.type === "chart" ? /Invalid thresholds/ : /unsupported visualization option/);
    }
    expect(validatePanelsBlob(withPanel(panel("a", { viz: { type: "chart", x: "day", y: [] } })))).toMatch(/x and y/);
    expect(validatePanelsBlob(withPanel(panel("a", { viz: { type: "chart", x: "day", y: ["n", "n"] } })))).toMatch(/repeats/);
    expect(validatePanelsBlob(withPanel(panel("a", { viz: { type: "pie", label: "service", value: "n", min_slice_percent: 100 } })))).toMatch(/percentage/);
    expect(validatePanelsBlob(withPanel(panel("a", { viz: { type: "toplist", label: "service", value: "n", max_rows: 0 } })))).toMatch(/rows/);
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
    expect(validatePanelsBlob(withPanel(chart("a", { interval: "90m" as never })))).toMatch(/interval/i);
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
  it("preserves a result query and confirmed kinds when switching between a stat and a table", () => {
    const source: DashboardQueryPanel = { id: "r", kind: "query", title: "Revenue", team_id: 1, source_id: 1, query: { mode: "result", language: "clickhouse-sql", text: "SELECT sum(amount) AS n", limit: 42 }, viz: { type: "stat", value: "n", columns: { n: { kind: "number", confidence: "confirmed" } } } };
    const table = switchPanelKind(source, "table");
    expect(table.query).toBe(source.query);
    expect(table.viz).toEqual({ type: "table", columns: { n: { kind: "number", confidence: "confirmed" } } });
    const stat = switchPanelKind(table, "stat");
    expect(stat.query).toBe(source.query);
    expect(stat.viz).toEqual({ type: "stat", reduce: "last", columns: { n: { kind: "number", confidence: "confirmed" } } });
  });

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
    expect(tabLayout(panels, "main").map((l) => l.id)).toEqual(["a", "c"]);
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


describe("SQL result kind boundary", () => {
  it.each(["chart", "breakdown"] as const)("keeps a SQL result panel when asked for %s", (kind) => {
    const sql: DashboardQueryPanel = { id: "sql", kind: "query", title: "", team_id: 1, source_id: 1, query: { mode: "result", language: "clickhouse-sql", text: "SELECT 1 AS n" }, viz: { type: "stat", value: "n" } };
    const switched = switchPanelKind(sql, kind);
    expect(switched.query).toEqual(sql.query);
    expect(switched.viz.type).toBe(kind === "chart" ? "chart" : "toplist");
  });
});

it("rejects previous-row comparison with the count default sum reducer", () => {
  const stat = chart("count", {}, { viz: { type: "stat", compare: { mode: "previous_row" } } });
  expect(validatePanelsBlob(blobOf([stat]))).toContain("comparison needs first or last reducer");
  stat.viz = { type: "stat", reduce: "last", compare: { mode: "previous_row" } };
  expect(validatePanelsBlob(blobOf([stat]))).toBeNull();
});

// Phase 14: fixed intervals in both modes, a panel range, and the range grammar.
describe("panel time validation", () => {
  function withPanel(p: DashboardQueryPanel): DashboardPanels {
    return blobOf([p], [{ id: p.id, x: 0, y: 0, w: 6, h: 2 }]);
  }

  it("accepts every histogram window in count and result mode", () => {
    for (const interval of ["auto", "1s", "5s", "10s", "15s", "30s", "1m", "5m", "10m", "15m", "30m", "1h", "2h", "3h", "6h", "12h", "24h"] as const) {
      expect(validatePanelsBlob(withPanel(chart("a", { interval })))).toBeNull();
      expect(validatePanelsBlob(withPanel(panel("a", { query: { mode: "result", language: "clickhouse-sql", text: "SELECT 1", interval } })))).toBeNull();
    }
  });

  it.each(["90m", "1d", "", "24H"])("refuses the interval %j", (interval) => {
    expect(validatePanelsBlob(withPanel(chart("a", { interval: interval as never })))).toMatch(/interval/i);
  });

  it.each(["15m", "6h", "7d", "2w", "today", "yesterday", "today-6d", "today-9999d"])("accepts the range %s", (range) => {
    expect(isDashboardRange(range)).toBe(true);
    expect(validatePanelsBlob(withPanel(chart("a", { range })))).toBeNull();
  });

  it.each(["", "last week", "today-0d", "today-6h", "today-10000d", "yesterday-6d", "0d", "7 d", "now-7d"])("refuses the range %j", (range) => {
    expect(isDashboardRange(range)).toBe(false);
    expect(validatePanelsBlob(withPanel(chart("a", { range })))).toMatch(/time range/i);
  });

  it("keeps the interval and the range when the panel kind changes", () => {
    const count = chart("a", { interval: "24h", range: "today-6d" });
    for (const kind of ["stat", "breakdown", "table", "chart"] as const) {
      const switched = switchPanelKind({ ...count, viz: kind === "chart" ? { type: "stat", reduce: "sum" } : count.viz }, kind);
      expect(switched.query).toMatchObject({ interval: "24h", range: "today-6d" });
    }
    const auto = switchPanelKind(chart("b"), "table");
    expect(auto.query).not.toHaveProperty("interval");
    expect(auto.query).not.toHaveProperty("range");
    expect(resultInterval("auto")).toEqual({});
    expect(resultInterval(undefined)).toEqual({});
    expect(resultInterval("1h")).toEqual({ interval: "1h" });
  });
});
