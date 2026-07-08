import { describe, it, expect } from "vitest";
import {
  snapPanelWidth,
  clampPanelHeight,
  normalizeDashboardLayout,
  sumHistogramCounts,
} from "@/utils/dashboardPanels";
import type { DashboardPanel, DashboardLayoutItem } from "@/api/dashboards";

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
