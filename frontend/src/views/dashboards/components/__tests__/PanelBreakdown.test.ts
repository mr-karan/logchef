import { createApp, nextTick } from "vue";
import { afterEach, describe, expect, it, vi } from "vitest";

vi.mock("@unovis/vue", () => ({
  VisSingleContainer: { template: "<div class='unovis-stub'><slot /></div>" },
  VisDonut: { template: "<div class='donut-stub'>{{ $attrs['central-label'] }} {{ $attrs['central-sub-label'] }}</div>" },
}));

import PanelBreakdown from "@/views/dashboards/components/PanelBreakdown.vue";

const buckets = [
  { bucket: "a", group_value: "a very long category name that should truncate", log_count: 3 },
  { bucket: "a", group_value: "web", log_count: 2 },
];
let host: HTMLDivElement | null = null;

afterEach(() => {
  host?.remove();
  host = null;
});

async function render(view: "horizontal-bars" | "donut" = "horizontal-bars", notice?: string) {
  host = document.createElement("div");
  document.body.appendChild(host);
  createApp(PanelBreakdown, { buckets, groupBy: "service", view, notice }).mount(host);
  await nextTick();
  return host;
}

describe("PanelBreakdown", () => {
  it("renders horizontal bars by default, with a long-label title and notice", async () => {
    const root = await render("horizontal-bars", "Top groups only");
    expect(root.querySelector(".panel-breakdown__bars")).not.toBeNull();
    expect(root.querySelector(".panel-breakdown__label")?.getAttribute("title")).toContain("very long");
    expect(root.textContent).toContain("Top groups only");
  });

  it("renders donut center total and count/percentage legend", async () => {
    const root = await render("donut");
    expect(root.querySelector(".panel-breakdown__donut")).not.toBeNull();
    expect(root.textContent).toContain("Total");
    expect(root.textContent).toMatch(/3.*60%/);
  });
});
