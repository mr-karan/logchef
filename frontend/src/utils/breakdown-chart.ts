import type { HistogramData } from "@/services/HistogramService";
import { getColorForGroupValue } from "@/utils/histogram-chart";

export const BREAKDOWN_OTHER_COLOR = "#6E7074";
export const MAX_DONUT_SLICES = 11;

export interface BreakdownCategory {
  /** Original grouped value. `isOther` is the structural discriminator. */
  value: string;
  label: string;
  count: number;
  percentage: number;
  color: string;
  isOther: boolean;
  isNull: boolean;
}

export interface BreakdownChartModel {
  categories: BreakdownCategory[];
  total: number;
}

function categoryKey(value: string, isOther: boolean, isNull: boolean): string {
  if (isOther) return "synthetic-other";
  if (isNull) return "structural-null";
  return `value:${value}`;
}

function labelFor(value: string, isOther: boolean, isNull: boolean): string {
  if (isOther) return "Other";
  if (isNull) return "(null)";
  return value === "" ? "(empty)" : value;
}

function sortCategories(categories: BreakdownCategory[]): BreakdownCategory[] {
  return categories.sort((a, b) => b.count - a.count || categoryKey(a.value, a.isOther, a.isNull).localeCompare(categoryKey(b.value, b.isOther, b.isNull)));
}

function withPercentages(categories: Omit<BreakdownCategory, "percentage">[], total: number): BreakdownCategory[] {
  return sortCategories(categories.map((category) => ({ ...category, percentage: total > 0 ? (category.count / total) * 100 : 0 })));
}

/** Collapse grouped histogram buckets over time while retaining synthetic Other structurally. */
export function buildBreakdownChartModel(buckets: HistogramData[] | null | undefined): BreakdownChartModel {
  const grouped = new Map<string, { value: string; isOther: boolean; isNull: boolean; count: number }>();
  for (const bucket of buckets ?? []) {
    const count = Number(bucket?.log_count);
    if (!Number.isFinite(count)) continue;
    const value = typeof bucket?.group_value === "string" ? bucket.group_value : "";
    const isOther = bucket?.is_other === true;
    const isNull = !isOther && bucket?.is_null === true;
    const key = categoryKey(value, isOther, isNull);
    const current = grouped.get(key);
    if (current) current.count += count;
    else grouped.set(key, { value, isOther, isNull, count });
  }
  const total = Array.from(grouped.values()).reduce((sum, category) => sum + category.count, 0);
  return {
    total,
    categories: withPercentages(
      Array.from(grouped.values()).map((category) => ({
        ...category,
        label: labelFor(category.value, category.isOther, category.isNull),
        color: category.isOther
          ? BREAKDOWN_OTHER_COLOR
          : getColorForGroupValue(labelFor(category.value, category.isOther, category.isNull)),
      })),
      total
    ),
  };
}

/**
 * Produce donut slices without losing counts. At most 11 slices are visible:
 * the largest real categories remain and all remaining real categories merge
 * into the structural synthetic Other (including any backend-provided remainder).
 */
export function buildDonutBreakdownChartModel(buckets: HistogramData[] | null | undefined): BreakdownChartModel {
  const model = buildBreakdownChartModel(buckets);
  if (model.categories.length <= MAX_DONUT_SLICES) return model;

  const real = model.categories.filter((category) => !category.isOther);
  const synthetic = model.categories.filter((category) => category.isOther);
  const keepCount = Math.max(0, MAX_DONUT_SLICES - 1);
  const kept = real.slice(0, keepCount);
  const folded = real.slice(keepCount);
  const otherCount = synthetic.reduce((sum, category) => sum + category.count, 0) + folded.reduce((sum, category) => sum + category.count, 0);
  const categories: Omit<BreakdownCategory, "percentage">[] = [...kept];
  if (otherCount > 0) {
    categories.push({
      value: "",
      label: "Other",
      count: otherCount,
      color: BREAKDOWN_OTHER_COLOR,
      isOther: true,
      isNull: false,
    });
  }
  return { total: model.total, categories: withPercentages(categories, model.total) };
}
