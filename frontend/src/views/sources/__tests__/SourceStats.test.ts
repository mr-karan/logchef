import { afterEach, describe, expect, it, vi } from "vitest";
import { createApp, nextTick, reactive } from "vue";
import { createPinia } from "pinia";
import { i18n } from "@/i18n";
import type { Source, SourceActivity, SourceInspection } from "@/api/sources";

const server: {
  inspectionFails: boolean;
  activityFails: boolean;
  activityError?: unknown;
  inspectionErrorType: string;
  pendingInspections: Map<number, (message: string) => void> | null;
} = {
  inspectionFails: true,
  activityFails: true,
  activityError: undefined,
  inspectionErrorType: "DatabaseError",
  pendingInspections: null,
};
const route = reactive<{ query: Record<string, string> }>({ query: { sourceId: "7" } });

const source: Source = {
  id: 7, name: "app logs", _meta_is_auto_created: false, source_type: "clickhouse", _meta_ts_field: "timestamp",
  connection: {}, ttl_days: 7, created_at: "", updated_at: "", is_connected: true,
};
const otherSource: Source = { ...source, id: 8, name: "audit logs" };
const inspection: SourceInspection = {
  details: [{ key: "host", label: "Host", value: "localhost:9000" }],
  storage: [
    { key: "rows", label: "Rows", value: "706696159" },
    { key: "compressed", label: "Compressed Size", value: "31.78 GiB" },
    { key: "uncompressed", label: "Uncompressed Size", value: "231 GiB" },
    { key: "compression_ratio", label: "Compression Ratio", value: "7.27x" },
  ],
  schema: {
    fields: [{ name: "timestamp", type: "DateTime", is_primary_key: true }, { name: "message", type: "String" }],
    ttl: "toDateTime(timestamp) + toIntervalMonth(3)",
    sort_keys: ["timestamp"],
    create_query: "CREATE TABLE logs (timestamp DateTime, message String)",
  },
};
const activity: SourceActivity = { rows_1h: 1, rows_24h: 2, latest_ts: null, hourly_buckets: [] };

const ok = <T>(data: T) => Promise.resolve({ status: "success" as const, data });
const fail = (message: string) => Promise.reject({ status: "error", message, error_type: "DatabaseError" });

vi.mock("@/api/sources", async (importOriginal) => ({
  ...(await importOriginal<typeof import("@/api/sources")>()),
  sourcesApi: {
    listAllSourcesForAdmin: () => ok([source, otherSource]),
    getAdminSourceInspection: (sourceId: number) => {
      const pending = server.pendingInspections;
      if (pending) {
        return new Promise((_, reject) => {
          pending.set(sourceId, (message) => reject({ status: "error", message, error_type: "DatabaseError" }));
        });
      }
      if (!server.inspectionFails) return ok(inspection);
      return Promise.reject({ status: "error", message: "stats failed", error_type: server.inspectionErrorType });
    },
    getAdminSourceActivity: () => {
      if (server.activityError !== undefined) return Promise.reject(server.activityError);
      return server.activityFails ? fail("activity timed out") : ok(activity);
    },
  },
}));
vi.mock("vue-router", async (importOriginal) => ({
  ...(await importOriginal<typeof import("vue-router")>()),
  useRoute: () => route,
  useRouter: () => ({ replace: vi.fn() }),
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
  server.activityError = undefined;
  server.inspectionErrorType = "DatabaseError";
  server.pendingInspections = null;
  route.query = { sourceId: "7" };
});

describe("SourceStats", () => {
  it("explains an unsupported activity query without offering Retry", async () => {
    server.inspectionFails = false;
    server.activityError = { status: "error", message: "Recent activity unavailable", error_type: "ActivityUnsupported" };
    const root = await render();
    expect(root.textContent).toContain(i18n.global.t("inspection.activitySortKey"));
    expect(activityRetry(root)).toBeUndefined();
    expect(root.querySelector("h1")?.textContent).toBe(source.name);
  });

  it("keeps Retry when a proxy answers with a bare 503 page", async () => {
    server.inspectionFails = false;
    server.activityError = "<html><body>503 Service Unavailable</body></html>";
    const root = await render();
    expect(root.textContent).not.toContain(i18n.global.t("inspection.activitySortKey"));
    expect(activityRetry(root)).toBeDefined();
  });

  it.each([
    ["NotFoundError", "Source not found"],
    ["ValidationError", "Invalid source ID"],
  ])("shows a %s without a Retry that cannot succeed", async (errorType, message) => {
    server.inspectionFails = false;
    server.activityError = { status: "error", message, error_type: errorType };
    const root = await render();
    expect(root.textContent).toContain(message);
    expect(activityRetry(root)).toBeUndefined();
  });

  it("keeps Retry when the activity query times out", async () => {
    server.inspectionFails = false;
    server.activityError = { status: "error", message: "Recent activity timed out", error_type: "GeneralError" };
    const root = await render();
    expect(root.textContent).toContain("Recent activity timed out");
    expect(activityRetry(root)).toBeDefined();
  });

  it("shows formatted storage, sparse field markers, and a collapsed CREATE TABLE", async () => {
    server.inspectionFails = false;
    server.activityFails = false;
    const root = await render();
    expect(root.textContent).toContain("706,696,159");
    expect(root.textContent).toContain("7.27x");
    expect(root.textContent).toContain("3 months");
    expect(root.textContent).not.toContain("706696159");
    const headers = [...root.querySelectorAll("th")].map(header => header.textContent);
    expect(headers).toEqual(["Field", "Type", "Role"]);
    expect(root.querySelectorAll("tbody tr")[0]?.textContent).toContain("Primary Key");
    expect(root.querySelectorAll("tbody tr")[1]?.textContent).not.toContain("Primary Key");
    const disclosure = root.querySelector("details");
    expect(disclosure?.open).toBe(false);
    disclosure?.querySelector("summary")?.click();
    expect(disclosure?.open).toBe(true);
    expect(disclosure?.textContent).toContain("CREATE TABLE logs");
    expect(root.querySelector(".sparkline-stub")).not.toBeNull();
  });

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
    expect(root.textContent).toContain(i18n.global.t("sources.rowsLastHour"));
    expect(inspectionAlert(root)).toBeDefined();
  });

  it("drops Retry from the inspection failure when the source no longer exists", async () => {
    vi.spyOn(console, "error").mockImplementation(() => {});
    server.inspectionErrorType = "NotFoundError";
    const root = await render();

    const alert = inspectionAlert(root);
    expect(alert).toBeDefined();
    expect(alert?.querySelector("button")).toBeNull();
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
    expect(root.textContent).toContain("706,696,159");
  });

  it("keeps the selected source's failure when an earlier source fails later", async () => {
    vi.spyOn(console, "error").mockImplementation(() => {});
    const pending = new Map<number, (message: string) => void>();
    server.pendingInspections = pending;
    const root = await render();

    route.query = { sourceId: "8" };
    await flush();
    expect(pending.has(7) && pending.has(8)).toBe(true);
    pending.get(8)?.("stats timed out");
    await flush();
    expect(inspectionAlert(root)).toBeDefined();

    pending.get(7)?.("stats timed out");
    await flush();
    expect(inspectionAlert(root)).toBeDefined();
  });
});
