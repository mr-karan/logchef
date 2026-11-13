export interface ExactNumber {
  kind: "exact";
  coefficient: bigint;
  scale: number;
}
export type FrameNumber = ExactNumber | number;

export function normalizeExact(coefficient: bigint, scale: number): ExactNumber {
  if (coefficient === 0n) return { kind: "exact", coefficient: 0n, scale: 0 };
  while (coefficient % 10n === 0n) {
    coefficient /= 10n;
    scale--;
  }
  return { kind: "exact", coefficient, scale };
}

export function parseExact(value: unknown): ExactNumber | null {
  if (typeof value !== "string" && typeof value !== "number") return null;
  const literal = typeof value === "string" ? value : String(value);
  if (literal.length > 512) return null;
  const match = /^([+-]?)(\d+)(?:\.(\d*))?(?:[eE]([+-]?)(\d{1,4}))?$/.exec(literal);
  if (!match) return null;
  const fraction = match[3] ?? "";
  const exponent = Number(`${match[4] ?? ""}${match[5] ?? "0"}`);
  let digits = (match[2] + fraction).replace(/^0+/, "");
  if (!digits) return { kind: "exact", coefficient: 0n, scale: 0 };
  const trimmed = digits.replace(/0+$/, "");
  const scale = fraction.length - exponent - (digits.length - trimmed.length);
  digits = trimmed;
  if (digits.length > 100 || scale < -400 || scale > 400) return null;
  return { kind: "exact", coefficient: BigInt((match[1] === "-" ? "-" : "") + digits), scale };
}

export function isExact(value: unknown): value is ExactNumber {
  return typeof value === "object" && value !== null && "kind" in value && value.kind === "exact" &&
    "coefficient" in value && typeof value.coefficient === "bigint" && "scale" in value && typeof value.scale === "number";
}

export function addExact(a: ExactNumber, b: ExactNumber): ExactNumber {
  const scale = Math.max(a.scale, b.scale);
  return normalizeExact(a.coefficient * 10n ** BigInt(scale - a.scale) + b.coefficient * 10n ** BigInt(scale - b.scale), scale);
}

export function compareExact(a: ExactNumber, b: ExactNumber): number {
  const difference = addExact(a, { ...b, coefficient: -b.coefficient }).coefficient;
  return difference < 0n ? -1 : difference > 0n ? 1 : 0;
}

export function multiplyExact(a: ExactNumber, b: ExactNumber): ExactNumber {
  return normalizeExact(a.coefficient * b.coefficient, a.scale + b.scale);
}

export function roundExact(a: ExactNumber, decimals: number): ExactNumber {
  if (a.scale <= decimals) return a;
  const divisor = 10n ** BigInt(a.scale - decimals);
  return normalizeExact(roundDivide(a.coefficient, divisor), decimals);
}

function roundDivide(value: bigint, divisor: bigint): bigint {
  const sign = value < 0n ? -1n : 1n;
  const absolute = value * sign;
  return sign * (absolute / divisor + (absolute % divisor * 2n >= divisor ? 1n : 0n));
}

export function averageExact(sum: ExactNumber, count: number): ExactNumber {
  const shift = 10 - sum.scale;
  const numerator = shift >= 0 ? sum.coefficient * 10n ** BigInt(shift) : sum.coefficient;
  const denominator = BigInt(count) * (shift < 0 ? 10n ** BigInt(-shift) : 1n);
  return normalizeExact(roundDivide(numerator, denominator), 10);
}

export function exactString(value: ExactNumber, decimals?: number): string {
  const rounded = decimals === undefined ? value : roundExact(value, decimals);
  const negative = rounded.coefficient < 0n;
  const digits = (negative ? -rounded.coefficient : rounded.coefficient).toString();
  const point = digits.length - rounded.scale;
  let whole = point <= 0 ? "0" : point >= digits.length ? digits + "0".repeat(point - digits.length) : digits.slice(0, point);
  let fraction = point <= 0 ? "0".repeat(-point) + digits : point < digits.length ? digits.slice(point) : "";
  if (decimals !== undefined) fraction = fraction.padEnd(decimals, "0");
  return (negative ? "-" : "") + whole + (fraction ? "." + fraction : "");
}
