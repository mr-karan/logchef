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
  const ms = value * multiplier;
  // A pathologically long digit string (e.g. a 300-digit granularity) can
  // overflow `value` to Infinity; a zero/negative value is also nonsensical.
  // Reject rather than hand callers an unbounded/non-positive step, since
  // fillBucketGaps loops `ts += stepMs` until it passes the last bucket.
  return Number.isFinite(ms) && ms > 0 ? ms : null;
}

export function getColorForGroupValue(value: string): string {
  if (!value) {
    return severityColorMapping.default0;
  }

  const lowerValue = value.toLowerCase();
  // Object.hasOwn guards against prototype pollution via values like
  // "__proto__" or "constructor", which would otherwise resolve through the
  // prototype chain to a non-string (e.g. Object.prototype.constructor) and
  // break this function's string-only contract.
  if (Object.hasOwn(severityColorMapping, lowerValue)) {
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

// Insert zero-count rows for every missing time bucket between the first and
// last returned bucket. ClickHouse only returns rows for buckets that matched
// at least one log (no WITH FILL), so sparse or grouped series come back with
// gaps; a continuous time axis then plots isolated bars/points with dead space
// between them. Filling the gaps makes the series read as a real histogram
// (and lets line/area charts draw a continuous baseline). VictoriaLogs already
// zero-fills server-side, so filled input is simply left unchanged.
function fillBucketGaps<T extends { ts: number }>(
  rows: T[],
  stepMs: number,
  makeZero: (ts: number) => T,
): T[] {
  // Require a finite, positive step. A non-finite (Infinity/NaN) or
  // non-positive step would otherwise make `for (...; ts += stepMs)` below
  // loop forever (or never advance) instead of safely skipping the fill.
  if (rows.length < 2 || !Number.isFinite(stepMs) || stepMs <= 0) return rows;
  const first = rows[0].ts;
  const last = rows[rows.length - 1].ts;
  // Safety: never expand beyond a sane bucket count (guards against a bad
  // step/window producing a runaway loop).
  if ((last - first) / stepMs > 5000) return rows;

  const byTs = new Map(rows.map((row) => [row.ts, row]));
  const out: T[] = [];
  for (let ts = first; ts <= last + stepMs / 2; ts += stepMs) {
    out.push(byTs.get(ts) ?? makeZero(ts));
  }
  // Belt-and-suspenders: keep any real bucket that didn't land on the generated
  // grid (should not happen for interval-aligned buckets, but never drop data).
  const present = new Set(out.map((row) => row.ts));
  for (const row of rows) {
    if (!present.has(row.ts)) out.push(row);
  }
  out.sort((left, right) => left.ts - right.ts);
  return out;
}

/**
 * Estimate the bucket cadence from the data itself when no granularity string
 * is available: the smallest positive gap between consecutive sorted
 * timestamps. Using the minimum (rather than just the first pair) avoids a
 * sparse head — e.g. a 5-minute gap before otherwise-dense 1-minute data —
 * setting a too-large step and under-filling the rest of the series.
 */
function inferBucketWidthMs(sortedTimestamps: number[], fallback = 60_000): number {
  let min = Infinity;
  for (let index = 1; index < sortedTimestamps.length; index += 1) {
    const diff = sortedTimestamps[index] - sortedTimestamps[index - 1];
    if (diff > 0 && diff < min) {
      min = diff;
    }
  }
  return Number.isFinite(min) ? min : fallback;
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
      inferBucketWidthMs(sortedBuckets.map((bucket) => new Date(bucket.bucket).getTime()));

    const rawRows = sortedBuckets.map((bucket) => {
      const ts = new Date(bucket.bucket).getTime();
      return {
        ts,
        bucket: bucket.bucket,
        bucketEndTs: ts + bucketWidthMs,
        total: bucket.log_count,
        count: bucket.log_count,
      } satisfies HistogramChartRow;
    });

    // Zero-fill missing buckets, then set each row's end to the next row's start
    // so bar widths and the crosshair snap stay contiguous.
    const rows = fillBucketGaps(rawRows, bucketWidthMs, (ts) => ({
      ts,
      bucket: new Date(ts).toISOString(),
      bucketEndTs: ts + bucketWidthMs,
      total: 0,
      count: 0,
    }));
    rows.forEach((row, index) => {
      row.bucketEndTs =
        index < rows.length - 1 ? rows[index + 1].ts : row.ts + bucketWidthMs;
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

  const populatedRows = [...bucketRows.values()].sort((left, right) => left.ts - right.ts);
  const derivedBucketWidth =
    parseGranularityToMilliseconds(granularity) ??
    inferBucketWidthMs(populatedRows.map((row) => row.ts));

  // Zero-fill gaps; the forEach below then sets every series key to 0 on the
  // inserted rows and recomputes contiguous bucket ends.
  const rowList = fillBucketGaps<HistogramChartRow>(populatedRows, derivedBucketWidth, (ts) => ({
    ts,
    bucket: new Date(ts).toISOString(),
    bucketEndTs: ts,
    total: 0,
  }));

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
