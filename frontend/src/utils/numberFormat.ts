import type { NumberFormat } from "@/api/dashboards";
import { averageExact, compareExact, exactString, isExact, multiplyExact, normalizeExact, parseExact, roundExact, type ExactNumber, type FrameNumber } from "./exactNumber";

function grouped(value: ExactNumber, decimals: number | undefined, grouping: boolean, locale?: string): string {
  const [whole, fraction] = exactString(value, decimals).split(".");
  const formatter = new Intl.NumberFormat(locale, { useGrouping: grouping });
  const parts = formatter.formatToParts(whole === "-0" ? -0 : BigInt(whole));
  if (!fraction) return parts.map((p) => p.value).join("");
  const decimal = new Intl.NumberFormat(locale).formatToParts(1.1).find((p) => p.type === "decimal")?.value ?? ".";
  const digits = Array.from({ length: 10 }, (_, digit) => new Intl.NumberFormat(locale, { useGrouping: false }).format(digit));
  const localized = Array.from(fraction, (digit) => digits[Number(digit)]).join("");
  const lastInteger = parts.length - 1 - [...parts].reverse().findIndex((p) => p.type === "integer");
  return parts.map((p, i) => p.value + (i === lastInteger ? decimal + localized : "")).join("");
}

export function formatNumber(value: FrameNumber, format: NumberFormat = {}, options: { locale?: string; compact?: boolean } = {}): { text: string; exact: string } {
  const scale = parseExact(format.scale ?? 1);
  let scaled = isExact(value) && scale ? multiplyExact(value, scale) : typeof value === "number" ? parseExact(value * (format.scale ?? 1)) : null;
  if (!scaled) return { text: "Overflow", exact: "Overflow" };
  const baseUnit = format.unit === "bytes" ? " B" : format.unit === "duration_ms" ? " ms" : format.unit === "duration_s" ? " s" : format.unit === "percent" ? "%" : "";
  const exact = `${format.prefix ?? ""}${grouped(scaled, undefined, format.grouping ?? true, options.locale)}${baseUnit}${format.suffix ?? ""}`;
  let unit = baseUnit;
  const decimals = format.decimals ?? 2;
  const magnitude = (n: ExactNumber) => ({ ...n, coefficient: n.coefficient < 0n ? -n.coefficient : n.coefficient });
  if (format.unit === "bytes") {
    const units = ["B", "KiB", "MiB", "GiB", "TiB", "PiB"];
    let index = 0;
    while (index < units.length - 1 && compareExact(magnitude(roundExact(scaled, decimals)), normalizeExact(1024n, 0)) >= 0) {
      scaled = averageExact(scaled, 1024);
      index++;
    }
    unit = ` ${units[index]}`;
  } else if (format.unit === "duration_ms" || format.unit === "duration_s") {
    if (format.unit === "duration_s") scaled = multiplyExact(scaled, normalizeExact(1000n, 0));
    const units = ["ms", "s", "min", "h", "d"];
    const divisors = [1000, 60, 60, 24];
    let index = 0;
    while (index < divisors.length && compareExact(magnitude(roundExact(scaled, decimals)), normalizeExact(BigInt(divisors[index]), 0)) >= 0) {
      scaled = averageExact(scaled, divisors[index]);
      index++;
    }
    unit = ` ${units[index]}`;
  } else if (format.notation === "compact" || (format.notation !== "standard" && options.compact)) {
    const suffixes = ["", "K", "M", "B", "T", "Q"];
    let exponent = 0;
    while (exponent < suffixes.length - 1 && compareExact(magnitude(roundExact(scaled, decimals)), normalizeExact(1000n, 0)) >= 0) {
      scaled = multiplyExact(scaled, { kind: "exact", coefficient: 1n, scale: 3 });
      exponent++;
    }
    unit = suffixes[exponent] + unit;
  }
  const rounded = roundExact(scaled, decimals);
  return { text: `${format.prefix ?? ""}${grouped(rounded, format.decimals, format.grouping ?? true, options.locale)}${unit}${format.suffix ?? ""}`, exact };
}
