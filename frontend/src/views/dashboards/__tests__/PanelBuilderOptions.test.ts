import { describe, expect, it } from "vitest";
import { createApp, h, nextTick, ref } from "vue";
import { i18n } from "@/i18n";
import type { DashboardPanelQuery, DashboardViz } from "@/api/dashboards";
import PanelBuilderOptions from "../components/PanelBuilderOptions.vue";

describe("table builder limit", () => {
  it("caps both the input and typed values at the execution limit", async () => {
    const changes: Partial<DashboardPanelQuery>[] = [];
    const host = document.createElement("div");
    const app = createApp({
      render: () => h(PanelBuilderOptions, {
        query: { mode: "result", language: "clickhouse-sql", text: "SELECT 1", limit: 100 },
        viz: { type: "table" },
        fieldSuggestions: [],
        "onUpdate:query": (patch) => changes.push(patch),
      }),
    });
    try {
      app.use(i18n).mount(host);
      const input = host.querySelector<HTMLInputElement>("#panel-limit");
      if (!input) throw new Error("table limit input missing");
      expect(input.max).toBe("10000");
      input.value = "100000";
      input.dispatchEvent(new Event("input", { bubbles: true }));
      await nextTick();
      expect(changes.at(-1)).toEqual({ limit: 10000 });
    } finally {
      app.unmount();
    }
  });
});

describe("result stat builder", () => {
  it("confirms untyped role kinds and accepts a decimal scale", async () => {
    const { resultFrame } = await import("@/utils/panelFrame");
    const changes: import("@/api/dashboards").DashboardViz[] = [];
    const host = document.createElement("div");
    const app = createApp({ render: () => h(PanelBuilderOptions, {
      query: { mode: "result", language: "logsql", text: "* | stats sum(amount) as n" },
      viz: { type: "stat" }, fieldSuggestions: [],
      frame: resultFrame({ columns: [{ name: "n", type: "String" }], rows: [{ n: "124000000" }], untyped: true }),
      "onUpdate:viz": (viz) => changes.push(viz),
    }) }).use(i18n);
    try {
      app.mount(host);
      expect(host.textContent).toContain("guessed");
      const value = host.querySelector<HTMLInputElement>("#stat-value");
      if (!value) throw new Error("value column input missing");
      value.value = "n";
      value.dispatchEvent(new Event("input", { bubbles: true }));
      await nextTick();
      expect(changes.at(-1)).toEqual({ type: "stat", value: "n", columns: { n: { kind: "number", confidence: "confirmed" } } });
      const scale = host.querySelector<HTMLInputElement>("#stat-scale");
      if (!scale) throw new Error("scale input missing");
      scale.value = "1e-7";
      scale.dispatchEvent(new Event("input", { bubbles: true }));
      await nextTick();
      expect(changes.at(-1)).toEqual({ type: "stat", format: { scale: 1e-7 } });
    } finally { app.unmount(); }
  });
});


describe("result options review regressions", () => {
  it("prunes replaced role confirmations and removes an empty format", async () => {
    const { resultFrame } = await import("@/utils/panelFrame");
    const viz = ref<DashboardViz>({ type: "stat", value: "old", columns: { old: { kind: "number", confidence: "confirmed" } }, format: { decimals: 2 } });
    const host = document.createElement("div");
    const app = createApp({ render: () => h(PanelBuilderOptions, {
      query: { mode: "result", language: "logsql", text: "*" }, viz: viz.value, fieldSuggestions: [],
      frame: resultFrame({ columns: [], rows: [{ old: "1", constructor: "42" }], untyped: true }),
      "onUpdate:viz": (next) => { viz.value = next; },
    }) }).use(i18n);
    try {
      app.mount(host);
      const role = host.querySelector<HTMLInputElement>("#stat-value")!;
      role.value = "constructor";
      role.dispatchEvent(new Event("input", { bubbles: true }));
      await nextTick();
      expect(viz.value).toMatchObject({ value: "constructor", columns: { constructor: { kind: "number", confidence: "confirmed" } } });
      if (viz.value.type !== "stat") throw new Error("expected stat");
      expect(Object.hasOwn(viz.value.columns!, "old")).toBe(false);
      const decimals = host.querySelector<HTMLInputElement>("#stat-decimals")!;
      decimals.value = "";
      decimals.dispatchEvent(new Event("input", { bubbles: true }));
      await nextTick();
      expect(viz.value.format).toBeUndefined();
    } finally { app.unmount(); }
  });
});

describe("result chart builder", () => {
  it("stores roles and options while changing marks and preserves y order", async () => {
    const { ref } = await import("vue");
    const { resultFrame } = await import("@/utils/panelFrame");
    const viz = ref<import("@/api/dashboards").DashboardViz>({ type: "chart", x: "client", y: ["revenue", "amount"], mark: "bar", x_scale: "category", show_values: true });
    const host = document.createElement("div");
    const app = createApp({ render: () => h(PanelBuilderOptions, {
      query: { mode: "result", language: "clickhouse-sql", text: "SELECT client, amount, revenue" },
      viz: viz.value, fieldSuggestions: [], frame: resultFrame({ columns: [{ name: "client", type: "String" }, { name: "amount", type: "UInt64" }, { name: "revenue", type: "UInt64" }], rows: [] }),
      "onUpdate:viz": (next) => { viz.value = next; },
    }) }).use(i18n);
    try {
      app.mount(host);
      expect(host.querySelector("#panel-groupby")).toBeNull();
      [...host.querySelectorAll("button")].find((b) => b.textContent?.trim() === "Area")?.click();
      await nextTick();
      expect(viz.value).toMatchObject({ type: "chart", x: "client", y: ["revenue", "amount"], mark: "area", x_scale: "category", show_values: true });
      host.querySelector<HTMLButtonElement>('[aria-label="Move amount up"]')?.click();
      await nextTick();
      expect(viz.value).toMatchObject({ y: ["amount", "revenue"] });
      const select = host.querySelector<HTMLSelectElement>("#chart-scale");
      if (!select) throw new Error("x scale missing");
      select.value = "time";
      select.dispatchEvent(new Event("change", { bubbles: true }));
      await nextTick();
      expect(viz.value).toMatchObject({ x_scale: "time", y: ["amount", "revenue"] });
    } finally { app.unmount(); }
  });
  it("keeps label and value roles when switching a toplist to pie", async () => {
    const changes: import("@/api/dashboards").DashboardViz[] = [];
    const host = document.createElement("div");
    const app = createApp({ render: () => h(PanelBuilderOptions, {
      query: { mode: "result", language: "clickhouse-sql", text: "SELECT client, amount" },
      viz: { type: "toplist", label: "client", value: "amount" }, fieldSuggestions: [],
      "onUpdate:viz": (next) => changes.push(next),
    }) }).use(i18n);
    try {
      app.mount(host);
      [...host.querySelectorAll("button")].find((b) => b.textContent?.trim() === "Donut")?.click();
      await nextTick();
      expect(changes.at(-1)).toMatchObject({ type: "pie", label: "client", value: "amount", donut: true });
      expect(host.querySelector("#panel-breakdown-groupby")).toBeNull();
    } finally { app.unmount(); }
  });
});

it('prunes result-chart confirmations, retains missing series and clears default options', async () => {
  const { resultFrame } = await import('@/utils/panelFrame');
  const viz = ref<DashboardViz>({ type: 'chart', x: 'old', y: ['n'], series: 'gone', columns: { old: { kind: 'category', confidence: 'confirmed' }, n: { kind: 'number', confidence: 'confirmed' } }, show_values: true, legend: false, orientation: 'horizontal', x_scale: 'category' });
  const host = document.createElement('div');
  const app = createApp({ render: () => h(PanelBuilderOptions, {
    query: { mode: 'result', language: 'logsql', text: '*' }, viz: viz.value, fieldSuggestions: [],
    frame: resultFrame({ columns: [], rows: [{ next: 'api', n: '12' }], untyped: true }),
    'onUpdate:viz': (next) => { viz.value = next; },
  }) }).use(i18n);
  try {
    app.mount(host);
    expect(host.querySelector('#chart-series')?.textContent).toContain('gone (missing)');
    const x = host.querySelector<HTMLSelectElement>('#chart-x')!;
    x.value = 'next'; x.dispatchEvent(new Event('change'));
    await nextTick();
    expect(viz.value.columns).not.toHaveProperty('old');
    const y = [...host.querySelectorAll<HTMLInputElement>('fieldset input[type=checkbox]')][1];
    y.dispatchEvent(new Event('change'));
    await nextTick();
    expect(viz.value.columns).not.toHaveProperty('n');
    for (const text of ['Show values', 'Show legend', 'Horizontal']) {
      const input = [...host.querySelectorAll('label')].find(l => l.textContent?.trim() === text)?.querySelector('input');
      expect(input).toBeTruthy(); input?.dispatchEvent(new Event('change')); await nextTick();
    }
    const scale = host.querySelector<HTMLSelectElement>('#chart-scale')!;
    scale.value = 'auto'; scale.dispatchEvent(new Event('change')); await nextTick();
    expect(viz.value).not.toHaveProperty('show_values');
    expect(viz.value).not.toHaveProperty('legend');
    expect(viz.value).not.toHaveProperty('orientation');
    expect(viz.value).not.toHaveProperty('x_scale');
  } finally { app.unmount(); }
});
