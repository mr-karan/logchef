import type { ChartConfig } from "@/components/ui/chart";
import type { HistogramData } from "@/services/HistogramService";

export interface HistogramChartSeries {
  key: string;
  label: string;
  color: string;
}

export interface HistogramChartRow extends Record<string, number | string> {
  ts: number;
  bucket: string;
  bucketEndTs: number;
  total: number;
}

export interface HistogramChartModel {
  rows: HistogramChartRow[];
  series: HistogramChartSeries[];
  chartConfig: ChartConfig;
  isGrouped: boolean;
  bucketWidthMs: number;
}

const severityColorMapping: Record<string, string> = {
  error: "#EE6666",
  err: "#EE6666",
  fatal: "#CC3333",
  critical: "#CC3333",
  crit: "#CC3333",
  alert: "#CC3333",
  emerg: "#990000",
  emergency: "#990000",
  warn: "#FAC858",
  warning: "#FAC858",
  info: "#5470C6",
  information: "#5470C6",
  notice: "#73C0DE",
  debug: "#91CC75",
  trace: "#B5C334",
  verbose: "#C6E579",
  get: "#73C0DE",
  post: "#91CC75",
  put: "#FAC858",
  delete: "#EE6666",
  patch: "#9A60B4",
  options: "#6E7074",
  head: "#5D9B9B",
  default0: "#5470C6",
  default1: "#91CC75",
  default2: "#FAC858",
  default3: "#EE6666",
  default4: "#73C0DE",
  default5: "#FC8452",
  default6: "#9A60B4",
  default7: "#ea7ccc",
  default8: "#3BA272",
  default9: "#27727B",
  default10: "#E062AE",
  default11: "#FFB980",
  default12: "#5D9B9B",
  default13: "#D48265",
  default14: "#C6E579",
  default15: "#8378EA",
};

export function parseGranularityToMilliseconds(granularity?: string | null): number | null {
  if (!granularity) {
    return null;
  }

  const match = granularity.trim().match(/^(\d+)(s|m|h|d)$/i);
  if (!match) {
    return null;
  }

  const value = Number(match[1]);
  const unit = match[2].toLowerCase();
  const multiplier = unit === "s" ? 1000 : unit === "m" ? 60_000 : unit === "h" ? 3_600_000 : 86_400_000;
  return value * multiplier;
}

export function getColorForGroupValue(value: string): string {
  if (!value) {
    return severityColorMapping.default0;
  }

  const lowerValue = value.toLowerCase();
  if (severityColorMapping[lowerValue]) {
    return severityColorMapping[lowerValue];
  }

  for (const [key, color] of Object.entries(severityColorMapping)) {
    if (key.startsWith("default")) {
      continue;
    }

    if (lowerValue.includes(key)) {
      return color;
    }
  }

  let hash = 0;
  for (let index = 0; index < value.length; index += 1) {
    hash = (hash << 5) - hash + value.charCodeAt(index);
    hash |= 0;
  }

  return severityColorMapping[`default${Math.abs(hash) % 16}`];
}

export function formatCompactCount(value: number): string {
  if (value < 1_000) {
    return Math.round(value).toLocaleString();
  }

  if (value < 1_000_000) {
    return `${(Math.round(value / 100) / 10).toLocaleString()}K`;
  }

  if (value < 1_000_000_000) {
    return `${(Math.round(value / 100_000) / 10).toLocaleString()}M`;
  }

  return `${(Math.round(value / 100_000_000) / 10).toLocaleString()}B`;
}

export function formatHistogramTimestamp(
  timestamp: number | Date,
  range: { start: number; end: number },
  granularity?: string | null,
): string {
  const value = timestamp instanceof Date ? timestamp : new Date(timestamp);
  const showSeconds = granularity?.endsWith("s") ?? false;
  const rangeStart = new Date(range.start);
  const rangeEnd = new Date(range.end);
  const showDate =
    range.end - range.start >= 86_400_000 ||
    rangeStart.toDateString() !== rangeEnd.toDateString();

  const formatter = new Intl.DateTimeFormat(undefined, {
    ...(showDate ? { month: "short", day: "2-digit" } : {}),
    hour: "2-digit",
    minute: "2-digit",
    ...(showSeconds ? { second: "2-digit" } : {}),
    hour12: false,
  });

  return formatter.format(value);
}

/** Tooltip-specific formatter: always includes HH:MM:SS for precise identification. */
export function formatTooltipTimestamp(
  timestamp: number | Date,
  range: { start: number; end: number },
): string {
  const value = timestamp instanceof Date ? timestamp : new Date(timestamp);
  const rangeStart = new Date(range.start);
  const rangeEnd = new Date(range.end);
  const showDate =
    range.end - range.start >= 86_400_000 ||
    rangeStart.toDateString() !== rangeEnd.toDateString();

  const formatter = new Intl.DateTimeFormat(undefined, {
    ...(showDate ? { month: "short", day: "2-digit" } : {}),
    hour: "2-digit",
    minute: "2-digit",
    second: "2-digit",
    hour12: false,
  });

  return formatter.format(value);
}

export function buildHistogramChartModel(
  buckets: HistogramData[],
  granularity?: string | null,
): HistogramChartModel {
  if (!buckets.length) {
    const chartConfig = {
      count: {
        label: "Log Count",
        color: "var(--chart-1)",
      },
    } satisfies ChartConfig;

    return {
      rows: [],
      series: [{ key: "count", label: "Log Count", color: "var(--chart-1)" }],
      chartConfig,
      isGrouped: false,
      bucketWidthMs: parseGranularityToMilliseconds(granularity) ?? 60_000,
    };
  }

  const sortedBuckets = [...buckets].sort(
    (left, right) => new Date(left.bucket).getTime() - new Date(right.bucket).getTime(),
  );

  const isGrouped = sortedBuckets.some((bucket) => bucket.group_value && bucket.group_value !== "");

  if (!isGrouped) {
    const bucketWidthMs =
      parseGranularityToMilliseconds(granularity) ??
      (sortedBuckets.length > 1
        ? new Date(sortedBuckets[1].bucket).getTime() - new Date(sortedBuckets[0].bucket).getTime()
        : 60_000);

    const rows = sortedBuckets.map((bucket, index) => {
      const ts = new Date(bucket.bucket).getTime();
      const nextBucketTs =
        index < sortedBuckets.length - 1
          ? new Date(sortedBuckets[index + 1].bucket).getTime()
          : ts + bucketWidthMs;

      return {
        ts,
        bucket: bucket.bucket,
        bucketEndTs: nextBucketTs,
        total: bucket.log_count,
        count: bucket.log_count,
      } satisfies HistogramChartRow;
    });

    const chartConfig = {
      count: {
        label: "Log Count",
        color: "var(--chart-1)",
      },
    } satisfies ChartConfig;

    return {
      rows,
      series: [{ key: "count", label: "Log Count", color: "var(--chart-1)" }],
      chartConfig,
      isGrouped: false,
      bucketWidthMs,
    };
  }

  const bucketRows = new Map<number, HistogramChartRow>();
  const labelToSeriesKey = new Map<string, string>();
  const series: HistogramChartSeries[] = [];

  sortedBuckets.forEach((bucket) => {
    const ts = new Date(bucket.bucket).getTime();
    const groupLabel = bucket.group_value || "Other";

    if (!labelToSeriesKey.has(groupLabel)) {
      const seriesKey = `series_${series.length}`;
      labelToSeriesKey.set(groupLabel, seriesKey);
      series.push({
        key: seriesKey,
        label: groupLabel,
        color: getColorForGroupValue(groupLabel),
      });
    }

    const seriesKey = labelToSeriesKey.get(groupLabel)!;
    const existingRow =
      bucketRows.get(ts) ??
      ({
        ts,
        bucket: bucket.bucket,
        bucketEndTs: ts,
        total: 0,
      } satisfies HistogramChartRow);

    existingRow[seriesKey] = bucket.log_count;
    existingRow.total += bucket.log_count;
    bucketRows.set(ts, existingRow);
  });

  const rowList = [...bucketRows.values()].sort((left, right) => left.ts - right.ts);
  const derivedBucketWidth =
    parseGranularityToMilliseconds(granularity) ??
    (() => {
      for (let index = 1; index < rowList.length; index += 1) {
        const diff = rowList[index].ts - rowList[index - 1].ts;
        if (diff > 0) {
          return diff;
        }
      }

      return 60_000;
    })();

  rowList.forEach((row, index) => {
    row.bucketEndTs = index < rowList.length - 1 ? rowList[index + 1].ts : row.ts + derivedBucketWidth;

    series.forEach((seriesItem) => {
      if (typeof row[seriesItem.key] !== "number") {
        row[seriesItem.key] = 0;
      }
    });
  });

  const chartConfig = Object.fromEntries(
    series.map((seriesItem) => [
      seriesItem.key,
      {
        label: seriesItem.label,
        color: seriesItem.color,
      },
    ]),
  ) satisfies ChartConfig;

  return {
    rows: rowList,
    series,
    chartConfig,
    isGrouped: true,
    bucketWidthMs: derivedBucketWidth,
  };
}
