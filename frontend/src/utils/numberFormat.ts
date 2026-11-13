import type { NumberFormat } from "@/api/dashboards";
import { averageExact, compareExact, exactString, isExact, multiplyExact, normalizeExact, parseExact, roundExact, type ExactNumber, type FrameNumber } from "./exactNumber";

function grouped(value: ExactNumber, decimals: number | undefined, grouping: boolean, locale?: string): string {
  const text = exactString(value, decimals);
  const [whole, fraction] = text.split(".");
  const parts = new Intl.NumberFormat(locale).formatToParts(1.1);
  const decimal = parts.find((p) => p.type === "decimal")?.value ?? ".";
  const integer = BigInt(whole).toLocaleString(locale, { useGrouping: grouping });
  return (whole === "-0" ? "-" : "") + integer + (fraction ? decimal + fraction : "");
}

export function formatNumber(value: FrameNumber, format: NumberFormat = {}, options: { locale?: string; compact?: boolean } = {}): { text: string; exact: string } {
  const scale = parseExact(format.scale ?? 1);
  let scaled = isExact(value) && scale ? multiplyExact(value, scale) : typeof value === "number" ? parseExact(value * (format.scale ?? 1)) : null;
  if (!scaled) return { text: "", exact: "" };
  const exact = `${format.prefix ?? ""}${grouped(scaled, undefined, format.grouping ?? true, options.locale)}${format.suffix ?? ""}`;
  let unit = "";
  if (format.unit === "percent") unit = "%";
  if (format.unit === "bytes") {
    const units = ["B", "KiB", "MiB", "GiB", "TiB", "PiB"];
    let index = 0;
    const absolute = { ...scaled, coefficient: scaled.coefficient < 0n ? -scaled.coefficient : scaled.coefficient };
    let divisor = 1;
    while (index < units.length - 1 && compareExact(absolute, normalizeExact(BigInt(divisor * 1024), 0)) >= 0) { divisor *= 1024; index++; }
    scaled = averageExact(scaled, divisor);
    unit = ` ${units[index]}`;
  }
  if (format.unit === "duration_ms" || format.unit === "duration_s") {
    if (format.unit === "duration_ms") scaled = multiplyExact(scaled, { kind: "exact", coefficient: 1n, scale: 3 });
    unit = " s";
  }
  if (format.notation === "compact" || (format.notation !== "standard" && options.compact)) {
    const absolute = { ...scaled, coefficient: scaled.coefficient < 0n ? -scaled.coefficient : scaled.coefficient };
    const suffixes = ["", "K", "M", "B", "T", "Q"];
    let exponent = 0;
    while (exponent < suffixes.length - 1 && compareExact(absolute, { kind: "exact", coefficient: 1n, scale: -3 * (exponent + 1) }) >= 0) exponent++;
    scaled = multiplyExact(scaled, { kind: "exact", coefficient: 1n, scale: 3 * exponent });
    unit = suffixes[exponent] + unit;
  }
  const rounded = roundExact(scaled, format.decimals ?? 2);
  return { text: `${format.prefix ?? ""}${grouped(rounded, format.decimals, format.grouping ?? true, options.locale)}${unit}${format.suffix ?? ""}`, exact };
}
