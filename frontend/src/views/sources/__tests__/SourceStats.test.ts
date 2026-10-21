import { afterEach, describe, expect, it, vi } from "vitest";
import { createApp, nextTick } from "vue";
import { createPinia } from "pinia";
import { i18n } from "@/i18n";
import type { Source, SourceActivity, SourceInspection } from "@/api/sources";

const server = {
  inspectionFails: true,
  activityFails: true,
};

const source: Source = {
  id: 7, name: "app logs", _meta_is_auto_created: false, source_type: "clickhouse", _meta_ts_field: "timestamp",
  connection: {}, ttl_days: 7, created_at: "", updated_at: "", is_connected: true,
};
const inspection: SourceInspection = { details: [], storage: [], schema: null };
const activity: SourceActivity = { rows_1h: 1, rows_24h: 2, latest_ts: null, hourly_buckets: [] };

const ok = <T>(data: T) => Promise.resolve({ status: "success" as const, data });
const fail = (message: string) => Promise.reject({ status: "error", message, error_type: "DatabaseError" });

vi.mock("@/api/sources", async (importOriginal) => ({
  ...(await importOriginal<typeof import("@/api/sources")>()),
  sourcesApi: {
    listAllSourcesForAdmin: () => ok([source]),
    getAdminSourceInspection: () => (server.inspectionFails ? fail("stats timed out") : ok(inspection)),
    getAdminSourceActivity: () => (server.activityFails ? fail("activity timed out") : ok(activity)),
  },
}));
vi.mock("vue-router", async (importOriginal) => ({
  ...(await importOriginal<typeof import("vue-router")>()),
  useRoute: () => ({ query: { sourceId: "7" } }),
  useRouter: () => ({ replace: vi.fn() }),
}));
vi.mock("@/views/sources/components/SourceInspectionOverview.vue", () => ({
  default: { template: "<div class='overview-stub' />" },
}));
vi.mock("@/views/sources/components/SourceInspectionSchema.vue", () => ({
  default: { template: "<div class='schema-stub' />" },
}));
vi.mock("@/components/visualizations/SourceSparkline.vue", () => ({
  default: { template: "<div class='sparkline-stub' />" },
}));

const { default: SourceStats } = await import("../SourceStats.vue");

const inspectionFailedText = "Failed to load source inspection. Please try again.";

let host: HTMLElement | null = null;
let unmount: (() => void) | null = null;

async function flush() {
  for (let i = 0; i < 5; i++) {
    await new Promise((resolve) => setTimeout(resolve, 0));
    await nextTick();
  }
}

async function render(): Promise<HTMLElement> {
  host = document.createElement("div");
  document.body.appendChild(host);
  const app = createApp(SourceStats);
  app.use(createPinia());
  app.use(i18n);
  app.mount(host);
  unmount = () => app.unmount();
  await flush();
  return host;
}

function inspectionAlert(root: HTMLElement): HTMLElement | undefined {
  return [...root.querySelectorAll<HTMLElement>(".border-destructive")].find((el) =>
    el.textContent?.includes(inspectionFailedText)
  );
}

function activityRetry(root: HTMLElement): HTMLButtonElement | undefined {
  const alert = inspectionAlert(root);
  return [...root.querySelectorAll("button")].find(
    (b) => b.textContent?.trim() === "Retry" && !alert?.contains(b)
  );
}

afterEach(() => {
  unmount?.();
  unmount = null;
  host?.remove();
  host = null;
  server.inspectionFails = true;
  server.activityFails = true;
});

describe("SourceStats", () => {
  it("keeps the inspection failure visible after a successful activity retry", async () => {
    vi.spyOn(console, "error").mockImplementation(() => {});
    const root = await render();
    expect(inspectionAlert(root)).toBeDefined();

    server.activityFails = false;
    const retry = activityRetry(root);
    if (!retry) throw new Error("activity retry not found");
    retry.click();
    await flush();

    expect(activityRetry(root)).toBeUndefined();
    expect(inspectionAlert(root)).toBeDefined();
  });

  it("clears the inspection failure once an inspection retry succeeds", async () => {
    vi.spyOn(console, "error").mockImplementation(() => {});
    const root = await render();
    const retry = inspectionAlert(root)?.querySelector("button");
    if (!retry) throw new Error("inspection retry not found");

    server.inspectionFails = false;
    retry.click();
    await flush();

    expect(inspectionAlert(root)).toBeUndefined();
    expect(root.querySelector(".overview-stub")).not.toBeNull();
  });
});
