import { describe, it, expect } from "vitest";
import {
  escapeHtml,
  sanitizeChartColor,
  resolveBarMode,
  shouldUseCrosshairYStacked,
} from "@/views/dashboards/components/PanelTimeseries.vue";

// A5 (P1 stored XSS): PanelTimeseries' tooltip renders a raw HTML string via
// unovis' crosshair (innerHTML), interpolating a grouped log-field value
// (attacker-controllable) and a series color. These pure helpers back that
// escaping/sanitization - see buildTooltipHtml in PanelTimeseries.vue.
describe("escapeHtml", () => {
  it("escapes a malicious group-label value into inert text", () => {
    const malicious = `<img src=x onerror=alert(1)>`;
    const escaped = escapeHtml(malicious);

    expect(escaped).toBe("&lt;img src=x onerror=alert(1)&gt;");
    expect(escaped).not.toContain("<img");
    expect(escaped.toLowerCase()).not.toContain("<script");
  });

  it("escapes all five reserved HTML characters", () => {
    expect(escapeHtml(`& < > " '`)).toBe("&amp; &lt; &gt; &quot; &#39;");
  });

  it("closing an existing tag/attribute context is neutralized", () => {
    const malicious = `"></span><script>alert(1)</script>`;
    const escaped = escapeHtml(malicious);
    expect(escaped).not.toMatch(/<script/i);
    expect(escaped).not.toContain('"><span');
  });

  it("passes through benign labels unchanged", () => {
    expect(escapeHtml("checkout-service")).toBe("checkout-service");
  });

  it("coerces non-string input (null/undefined/number) safely", () => {
    expect(escapeHtml(null)).toBe("");
    expect(escapeHtml(undefined)).toBe("");
    expect(escapeHtml(42)).toBe("42");
  });
});

describe("sanitizeChartColor", () => {
  it("allows known-safe color formats through unchanged", () => {
    expect(sanitizeChartColor("#EE6666")).toBe("#EE6666");
    expect(sanitizeChartColor("#fff")).toBe("#fff");
    expect(sanitizeChartColor("rgb(10, 20, 30)")).toBe("rgb(10, 20, 30)");
    expect(sanitizeChartColor("rgba(10, 20, 30, 0.5)")).toBe("rgba(10, 20, 30, 0.5)");
    expect(sanitizeChartColor("hsl(200, 50%, 50%)")).toBe("hsl(200, 50%, 50%)");
    expect(sanitizeChartColor("var(--chart-1)")).toBe("var(--chart-1)");
  });

  it("falls back to the default safe color for anything else, including an injection attempt", () => {
    expect(sanitizeChartColor(`red;"></span><img src=x onerror=alert(1)>`)).toBe("var(--chart-1)");
    expect(sanitizeChartColor("javascript:alert(1)")).toBe("var(--chart-1)");
    expect(sanitizeChartColor("")).toBe("var(--chart-1)");
    expect(sanitizeChartColor(null)).toBe("var(--chart-1)");
    expect(sanitizeChartColor(undefined)).toBe("var(--chart-1)");
  });
});

describe("resolveBarMode", () => {
  it("returns grouped when chart is bars and barMode is grouped", () => {
    expect(resolveBarMode("bars", "grouped")).toBe("grouped");
  });

  it("returns stacked when chart is bars and barMode is stacked", () => {
    expect(resolveBarMode("bars", "stacked")).toBe("stacked");
  });

  it("defaults to stacked when barMode is absent", () => {
    expect(resolveBarMode("bars", undefined)).toBe("stacked");
  });

  it("returns stacked for non-bar chart styles (line/area)", () => {
    expect(resolveBarMode("line", undefined)).toBe("stacked");
    expect(resolveBarMode("line", "grouped")).toBe("stacked");
    expect(resolveBarMode("area", undefined)).toBe("stacked");
  });

  it("returns stacked when chart is undefined", () => {
    expect(resolveBarMode(undefined, undefined)).toBe("stacked");
    expect(resolveBarMode(undefined, "grouped")).toBe("stacked");
  });
});

describe("shouldUseCrosshairYStacked", () => {
  it("returns false for line charts (uses regular y accessors)", () => {
    expect(shouldUseCrosshairYStacked("line", undefined)).toBe(false);
    expect(shouldUseCrosshairYStacked("line", "stacked")).toBe(false);
  });

  it("returns false for grouped bars (uses regular y accessors)", () => {
    expect(shouldUseCrosshairYStacked("bars", "grouped")).toBe(false);
  });

  it("returns true for stacked bars (uses yStacked accessors)", () => {
    expect(shouldUseCrosshairYStacked("bars", "stacked")).toBe(true);
    expect(shouldUseCrosshairYStacked("bars", undefined)).toBe(true);
  });

  it("returns true for area charts (uses yStacked accessors)", () => {
    expect(shouldUseCrosshairYStacked("area", undefined)).toBe(true);
    expect(shouldUseCrosshairYStacked("area", "grouped")).toBe(true);
  });

  it("returns true when chart is undefined (defaults to yStacked)", () => {
    expect(shouldUseCrosshairYStacked(undefined, undefined)).toBe(true);
  });
});
