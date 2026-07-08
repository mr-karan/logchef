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
  type PanelSize,
  type GridGeometry,
} from "@/utils/dashboardPanels";
import type {
  DashboardPanel,
  DashboardLayoutItem,
  DashboardPanels,
} from "@/api/dashboards";

function panel(id: string, overrides: Partial<DashboardPanel> = {}): DashboardPanel {
  return {
    id,
    title: id,
    type: "table",
    team_id: 1,
    source_id: 1,
    query: "",
    query_language: "logchefql",
    ...overrides,
  };
}

describe("snapPanelWidth", () => {
  it("snaps arbitrary widths to the nearest allowed preset", () => {
    expect(snapPanelWidth(7)).toBe(6);
    expect(snapPanelWidth(5)).toBe(4); // equidistant 4/6 -> first-wins keeps 4
    expect(snapPanelWidth(11)).toBe(12);
    expect(snapPanelWidth(1)).toBe(3);
    expect(snapPanelWidth(3)).toBe(3);
    expect(snapPanelWidth(12)).toBe(12);
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
  it("returns the slot a new default panel would pack into", () => {
    const slot = addTileSlot([{ id: "a", w: 6, h: 2 }]);
    expect(slot).toMatchObject({ x: 6, y: 0, w: 6, h: 2 });
  });
  it("places the tile at the origin on an empty dashboard", () => {
    expect(addTileSlot([])).toMatchObject({ x: 0, y: 0 });
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
  function blob(panels: DashboardPanel[], layout: DashboardLayoutItem[]): DashboardPanels {
    return { version: 1, panels, layout };
  }

  it("re-packs layout in panel array order, preserving each panel's size", () => {
    const panels = [panel("a"), panel("b"), panel("c")];
    const layout: DashboardLayoutItem[] = [
      { id: "a", x: 6, y: 4, w: 6, h: 2 },
      { id: "b", x: 0, y: 0, w: 4, h: 1 },
      { id: "c", x: 0, y: 9, w: 12, h: 3 },
    ];
    const out = reflowPanels(blob(panels, layout));
    expect(out.layout).toEqual([
      { id: "a", x: 0, y: 0, w: 6, h: 2 },
      { id: "b", x: 6, y: 0, w: 4, h: 1 },
      { id: "c", x: 0, y: 2, w: 12, h: 3 },
    ]);
  });

  it("places a just-added (layout-less) panel in the first free slot at default size", () => {
    // a=6-wide occupies the left half of row 0; the appended "new" panel (default
    // w6 h2) fits in the free right half of that same row.
    const panels = [panel("a"), panel("new")];
    const layout: DashboardLayoutItem[] = [{ id: "a", x: 0, y: 0, w: 6, h: 2 }];
    const out = reflowPanels(blob(panels, layout));
    expect(out.layout).toEqual([
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
    expect(out.layout).toEqual([
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
    expect(out.layout).toEqual([
      { id: "a", x: 0, y: 0, w: 12, h: 2 },
      { id: "b", x: 0, y: 2, w: 6, h: 2 }, // pushed to its own row
    ]);
  });
});

describe("validatePanelsBlob", () => {
  function base(overrides: Partial<DashboardPanels> = {}): DashboardPanels {
    return {
      version: 1,
      panels: [panel("a", { team_id: 1, source_id: 1 })],
      layout: [{ id: "a", x: 0, y: 0, w: 6, h: 2 }],
      ...overrides,
    };
  }

  it("returns null for a valid blob", () => {
    expect(validatePanelsBlob(base())).toBeNull();
  });

  it("rejects an unsupported version", () => {
    expect(validatePanelsBlob(base({ version: 2 }))).toMatch(/version/i);
  });

  it("rejects more than 24 panels", () => {
    const panels = Array.from({ length: 25 }, (_, i) => panel(`p${i}`, { team_id: 1, source_id: 1 }));
    expect(validatePanelsBlob(base({ panels, layout: [] }))).toMatch(/at most 24/i);
  });

  it("rejects duplicate panel ids", () => {
    const panels = [panel("dup", { team_id: 1, source_id: 1 }), panel("dup", { team_id: 1, source_id: 1 })];
    expect(validatePanelsBlob(base({ panels, layout: [] }))).toMatch(/duplicate/i);
  });

  it("rejects an invalid panel type", () => {
    const panels = [panel("a", { team_id: 1, source_id: 1, type: "pie" as any })];
    expect(validatePanelsBlob(base({ panels }))).toMatch(/type/i);
  });

  it("requires a team and a source", () => {
    expect(validatePanelsBlob(base({ panels: [panel("a", { team_id: 0, source_id: 1 })] }))).toMatch(/team/i);
    expect(validatePanelsBlob(base({ panels: [panel("a", { team_id: 1, source_id: 0 })] }))).toMatch(/source/i);
  });

  it("rejects invalid layout width/height", () => {
    expect(validatePanelsBlob(base({ layout: [{ id: "a", x: 0, y: 0, w: 5, h: 2 }] }))).toMatch(/width/i);
    expect(validatePanelsBlob(base({ layout: [{ id: "a", x: 0, y: 0, w: 6, h: 9 }] }))).toMatch(/height/i);
  });
});
