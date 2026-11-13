import { describe, expect, it } from "vitest";
import { readFileSync } from "node:fs";
import { resolve } from "node:path";
const css = readFileSync(resolve("src/assets/index.css"), "utf8");

describe("categorical chart theme", () => {
  it.each([".light", ".dark"])("separates series hues in %s", (selector) => {
    const block = css.slice(css.indexOf(`${selector} {`)).split("}")[0];
    const hues = [...block.matchAll(/--chart-\d: oklch\([\d.]+ [\d.]+ ([\d.]+)\)/g)].map((m) => Number(m[1]));
    expect(hues).toHaveLength(5);
    for (let i = 0; i < hues.length; i++) for (const hue of hues.slice(i + 1)) {
      const distance = Math.abs(hue - hues[i]);
      expect(Math.min(distance, 360 - distance)).toBeGreaterThan(30);
    }
  });
});
