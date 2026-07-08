import { describe, it, expect } from "vitest";
import {
  snapPanelWidth,
  clampPanelHeight,
  normalizeDashboardLayout,
  sumHistogramCounts,
  packLayout,
  moveItem,
  reorderByTarget,
  sizeForPanel,
  reflowPanels,
  validatePanelsBlob,
  type PanelSize,
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

describe("reorderByTarget", () => {
  it("moves the dragged id to the target's slot", () => {
    expect(reorderByTarget(["a", "b", "c"], "c", "a")).toEqual(["c", "a", "b"]);
    expect(reorderByTarget(["a", "b", "c"], "a", "c")).toEqual(["b", "c", "a"]);
  });
  it("leaves order unchanged when dropping onto itself or an unknown id", () => {
    expect(reorderByTarget(["a", "b", "c"], "b", "b")).toEqual(["a", "b", "c"]);
    expect(reorderByTarget(["a", "b", "c"], "b", "ghost")).toEqual(["a", "b", "c"]);
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
