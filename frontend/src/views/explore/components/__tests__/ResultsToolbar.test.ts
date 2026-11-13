import { createApp, h, nextTick } from "vue";
import { createPinia, setActivePinia } from "pinia";
import { afterEach, expect, it, vi } from "vitest";
import { i18n } from "@/i18n";
import type { QueryStats } from "@/api/explore";
import ResultsToolbar from "../ResultsToolbar.vue";

let stats: QueryStats | null = null;
vi.mock("@/stores/exploreRuns", () => ({
  useExploreRuns: () => ({
    get results() {
      return stats ? { rows: [], columns: [], stats, warnings: [], sql: "", compilation: null } : null;
    },
    isHistogramEligible: false,
    windowed: false,
  }),
}));

let unmount: (() => void) | null = null;

afterEach(() => {
  unmount?.();
  unmount = null;
  stats = null;
  document.body.innerHTML = "";
});

async function mount(props: Record<string, unknown>) {
  const pinia = createPinia();
  setActivePinia(pinia);
  const added: string[] = [];
  const host = document.createElement("div");
  document.body.append(host);
  const app = createApp({
    render: () =>
      h(ResultsToolbar, {
        isHistogramVisible: true, availableFields: [], displayMode: "table", logsCount: 100,
        onAddFastFilter: (field: string) => added.push(field),
        ...props,
      }),
  }).use(pinia).use(i18n);
  app.mount(host);
  unmount = () => app.unmount();
  await nextTick();
  return { host, added };
}

it("offers each unfiltered sort key as a one-click filter", async () => {
  const { host, added } = await mount({ fastFilterFields: ["namespace", "job_name"], canAddFastFilter: true });

  expect(host.textContent).toContain(i18n.global.t("explore.fastFilterLead"));
  host.querySelector<HTMLButtonElement>(`button[aria-label="${i18n.global.t("explore.addFastFilter", { field: "job_name" })}"]`)?.click();

  expect(added).toEqual(["job_name"]);
});

it("names the fields without buttons when the query mode cannot insert a filter", async () => {
  const { host } = await mount({ fastFilterFields: ["namespace"], canAddFastFilter: false });

  expect(host.querySelector("code")?.textContent).toBe("namespace");
  expect(host.querySelector(`button[aria-label="${i18n.global.t("explore.addFastFilter", { field: "namespace" })}"]`)).toBeNull();
});

it("shows no hint when every query already filters on a sort key", async () => {
  const { host } = await mount({ fastFilterFields: [] });

  expect(host.textContent).not.toContain(i18n.global.t("explore.fastFilterLead"));
});

it("shows how many rows a cut result kept", async () => {
  stats = { execution_time_ms: 5, rows_read: 1000, bytes_read: 0, rows_returned: 100, limit_applied: 100, limit_source: "request", truncation: "rows" };
  const { host } = await mount({ logsCount: 100 });

  expect(host.textContent).toContain("First 100 rows");
});

it("names a byte cut and an unproven cap", async () => {
  stats = { execution_time_ms: 5, rows_read: 1000, bytes_read: 0, rows_returned: 7, limit_applied: 100, truncation: "bytes" };
  expect((await mount({ logsCount: 7 })).host.textContent).toContain("Cut by size");
  unmount?.();
  stats = { execution_time_ms: 5, rows_read: 1000, bytes_read: 0, rows_returned: 100, limit_applied: 100, truncation: "unknown" };
  expect((await mount({ logsCount: 100 })).host.textContent).toContain("May be incomplete");
});

it("stays quiet when a cap cut nothing", async () => {
  stats = { execution_time_ms: 5, rows_read: 20, bytes_read: 0, rows_returned: 20, limit_applied: 20, limit_source: "statement", truncation: "none" };
  const { host } = await mount({ logsCount: 20 });

  expect(host.textContent).not.toContain("First");
  expect(host.textContent).not.toContain("incomplete");
});
