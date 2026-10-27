import { describe, expect, it } from "vitest";
import { rgbaToHex } from "./monaco-theme";

describe("Monaco theme colors", () => {
  it.each([
    { rgba: [255, 255, 255, 255], hex: "#ffffff" },
    { rgba: [9, 16, 32, 255], hex: "#091020" },
    { rgba: [0, 0, 0, 255], hex: "#000000" },
    { rgba: [255, 255, 255, 26], hex: "#ffffff1a" },
    { rgba: [0, 0, 0, 0], hex: "#00000000" },
  ])("converts $rgba to $hex", ({ rgba, hex }) => {
    expect(rgbaToHex(new Uint8ClampedArray(rgba))).toBe(hex);
  });
});
