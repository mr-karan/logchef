import { describe, expect, it } from "vitest";
import { BREAKDOWN_OTHER_COLOR, buildBreakdownChartModel, buildDonutBreakdownChartModel } from "@/utils/breakdown-chart";

describe("breakdown chart transform", () => {
  it("aggregates time buckets, sorts descending, and calculates total percentages", () => {
    const model = buildBreakdownChartModel([
      { bucket: "a", group_value: "api", log_count: 3 },
      { bucket: "b", group_value: "web", log_count: 5 },
      { bucket: "b", group_value: "api", log_count: 4 },
    ]);
    expect(model.total).toBe(12);
    expect(model.categories.map((c) => [c.label, c.count])).toEqual([["api", 7], ["web", 5]]);
    expect(model.categories[0].percentage).toBeCloseTo(58.333, 2);
  });

  it("keeps real Other, __other__, empty, and synthetic Other structurally distinct", () => {
    const model = buildBreakdownChartModel([
      { bucket: "a", group_value: "Other", log_count: 1 },
      { bucket: "a", group_value: "__other__", log_count: 2 },
      { bucket: "a", group_value: "", log_count: 3 },
      { bucket: "a", group_value: "Other", is_other: true, log_count: 4 },
      { bucket: "b", group_value: "__other__", is_other: true, log_count: 5 },
    ]);
    expect(model.categories).toHaveLength(4);
    expect(model.categories.find((c) => c.label === "(empty)")?.count).toBe(3);
    expect(model.categories.filter((c) => c.label === "Other")).toHaveLength(2);
    expect(model.categories.find((c) => c.isOther)).toMatchObject({ count: 9, color: BREAKDOWN_OTHER_COLOR });
    expect(model.categories.find((c) => c.value === "__other__")?.isOther).toBe(false);
  });

  it("keeps structural null and empty groups separate from literal display values", () => {
    const model = buildBreakdownChartModel([
      { bucket: "a", group_value: "", log_count: 2 },
      { bucket: "a", group_value: "(empty)", log_count: 3 },
      { bucket: "a", group_value: "", is_null: true, log_count: 4 },
      { bucket: "a", group_value: "(null)", log_count: 5 },
      { bucket: "a", group_value: "null", log_count: 6 },
    ]);

    expect(model.categories).toHaveLength(5);
    expect(model.categories.filter((category) => category.label === "(empty)").map((category) => category.count).sort()).toEqual([2, 3]);
    expect(model.categories.filter((category) => category.label === "(null)").map((category) => category.count).sort()).toEqual([4, 5]);
    expect(model.categories.find((category) => category.isNull)).toMatchObject({
      label: "(null)",
      count: 4,
    });
    expect(model.categories.find((category) => category.value === "null" && !category.isNull)).toMatchObject({ count: 6 });
  });

  it("uses deterministic category colors", () => {
    expect(buildBreakdownChartModel([{ bucket: "a", group_value: "orders", log_count: 1 }]).categories[0].color)
      .toBe(buildBreakdownChartModel([{ bucket: "b", group_value: "orders", log_count: 9 }]).categories[0].color);
  });

  it("caps donut slices and folds discarded categories into structural Other without losing totals", () => {
    const buckets = [
      ...Array.from({ length: 13 }, (_, i) => ({ bucket: "a", group_value: `c${i}`, log_count: 13 - i })),
      { bucket: "a", group_value: "Other", is_other: true, log_count: 7 },
    ];
    const model = buildDonutBreakdownChartModel(buckets);
    expect(model.categories).toHaveLength(11);
    expect(model.total).toBe(98);
    expect(model.categories.reduce((sum, c) => sum + c.count, 0)).toBe(98);
    expect(model.categories.find((c) => c.isOther)).toMatchObject({ count: 13, color: BREAKDOWN_OTHER_COLOR });
  });
});
