import { afterEach, describe, expect, it, vi } from "vitest";
import { createApp, defineComponent, h, nextTick } from "vue";
import { createPinia } from "pinia";
import { createMemoryHistory, createRouter, RouterView, type Router } from "vue-router";
import { i18n } from "@/i18n";
import type { AuditEvent, AuditEventFilters, AuditEventsPage } from "@/api/admin";

const calls: { filters: AuditEventFilters; cursor: string }[] = [];
const pages: (AuditEventsPage | Error)[] = [];
const exports: AuditEventFilters[] = [];

const ok = <T>(data: T) => Promise.resolve({ status: "success" as const, data });

vi.mock("@/api/admin", async (importOriginal) => ({
  ...(await importOriginal<typeof import("@/api/admin")>()),
  auditApi: {
    listEvents: (filters: AuditEventFilters, cursor: string) => {
      calls.push({ filters, cursor });
      const page = pages.shift() ?? { events: [], next_cursor: "" };
      if (page instanceof Error) return Promise.reject({ status: "error", message: page.message, error_type: "DatabaseError" });
      return ok(page);
    },
    exportEvents: (filters: AuditEventFilters) => {
      exports.push(filters);
      return Promise.resolve(new Blob([""]));
    },
  },
}));
vi.mock("@/api/users", () => ({
  usersApi: { listUsers: () => ok([{ id: "3", email: "admin@example.com", full_name: "Admin" }]) },
}));
vi.mock("@/api/sources", () => ({
  sourcesApi: { listAllSourcesForAdmin: () => ok([{ id: 7, name: "app logs" }]) },
}));
vi.mock("@/api/teams", () => ({
  teamsApi: { listAllTeams: () => ok([{ id: 2, name: "payments" }]) },
}));

const { default: AuditLog } = await import("../AuditLog.vue");

function event(overrides: Partial<AuditEvent>): AuditEvent {
  return {
    id: "0190-a",
    occurred_at: "2026-11-15T10:00:00Z",
    action: "source.update",
    outcome: "success",
    reason: "",
    actor_type: "user",
    actor_id: 3,
    actor_label: "admin@example.com",
    auth_method: "session",
    credential_id: "",
    client_id: "",
    target_type: "source",
    target_id: "7",
    target_label: "app logs",
    team_id: null,
    source_id: 7,
    ip: "192.0.2.10",
    user_agent: "Mozilla/5.0",
    http_method: "PUT",
    route: "/api/v1/admin/sources/:sourceID",
    status: 200,
    changes: { ttl_days: { from: 7, to: 30 } },
    detail: {},
    ...overrides,
  };
}

let unmount: (() => void) | null = null;
let host: HTMLElement | null = null;

async function flush() {
  for (let i = 0; i < 6; i++) {
    await new Promise((resolve) => setTimeout(resolve, 0));
    await nextTick();
  }
}

const Blank = defineComponent({ render: () => h("div") });

async function render(path: string): Promise<{ root: HTMLElement; router: Router }> {
  const router = createRouter({
    history: createMemoryHistory(),
    routes: [
      { path: "/admin/audit-log", name: "AdminAuditLog", component: AuditLog },
      { path: "/admin/query-activity", name: "AdminQueryActivity", component: Blank },
      { path: "/admin/sources/:sourceId/edit", name: "EditSource", component: Blank },
      { path: "/admin/teams/:id", name: "TeamSettings", component: Blank },
    ],
  });
  await router.push(path);
  host = document.createElement("div");
  document.body.appendChild(host);
  const app = createApp({ render: () => h(RouterView) });
  app.use(createPinia());
  app.use(i18n);
  app.use(router);
  app.mount(host);
  unmount = () => app.unmount();
  await flush();
  return { root: host, router };
}

function button(root: HTMLElement, text: string): HTMLButtonElement {
  const found = [...root.querySelectorAll("button")].find((b) => b.textContent?.trim() === text);
  if (!found) throw new Error(`no button ${text}`);
  return found;
}

afterEach(() => {
  unmount?.();
  unmount = null;
  host?.remove();
  host = null;
  calls.length = 0;
  pages.length = 0;
  exports.length = 0;
  localStorage.clear();
});

describe("AuditLog", () => {
  it("sends the URL filters to the API and defaults to the last 7 days, newest first", async () => {
    await render("/admin/audit-log?target_type=source&target_id=7&action=source.");
    expect(calls).toHaveLength(1);
    const { filters, cursor } = calls[0];
    expect(cursor).toBe("");
    expect(filters.target_type).toBe("source");
    expect(filters.target_id).toBe("7");
    expect(filters.action).toBe("source.");
    expect(filters.order).toBeUndefined();
    const days = (Date.parse(filters.to) - Date.parse(filters.from)) / 86_400_000;
    expect(days).toBe(7);
  });

  it("expands a row to show the changed field and links a source that still exists", async () => {
    pages.push({ events: [event({ changes: { ttl_days: { from: 7, to: 30 }, "connection.username": { from: "default", to: "" } } })], next_cursor: "" });
    const { root } = await render("/admin/audit-log");
    const row = root.querySelector<HTMLElement>("[data-audit-row]");
    expect(row?.querySelector("a")?.getAttribute("href")).toBe("/admin/sources/7/edit");
    expect(root.querySelector("[data-audit-detail]")).toBeNull();

    row?.click();
    await flush();
    const detail = root.querySelector<HTMLElement>("[data-audit-detail]");
    expect(detail?.textContent).toContain("ttl_days");
    expect(detail?.textContent).toContain("7");
    expect(detail?.textContent).toContain("30");
    expect(detail?.textContent).toContain("PUT /api/v1/admin/sources/:sourceID");
    expect(detail?.textContent).toContain('default→""');
  });

  it("renders an action and auth method it has never seen as plain text", async () => {
    pages.push({
      events: [event({ action: "alert_instance.acknowledge", auth_method: "scheduler", target_type: "alert", target_id: "4", target_label: "" })],
      next_cursor: "",
    });
    const { root } = await render("/admin/audit-log");
    const row = root.querySelector<HTMLElement>("[data-audit-row]");
    expect(row?.textContent).toContain("alert_instance.acknowledge");
    expect(row?.textContent).toContain("scheduler");
    expect(row?.textContent).toContain("4");
    expect(row?.querySelector("a")).toBeNull();
  });

  it("does not link a source that no longer exists", async () => {
    pages.push({ events: [event({ action: "source.delete", target_id: "9", target_label: "old logs" })], next_cursor: "" });
    const { root } = await render("/admin/audit-log");
    const row = root.querySelector<HTMLElement>("[data-audit-row]");
    expect(row?.textContent).toContain("old logs");
    expect(row?.querySelector("a")).toBeNull();
  });

  it("shows a query run's text, range and result with its source and team by name", async () => {
    const query = "SELECT * FROM logs WHERE service = 'api'";
    pages.push({
      events: [event({
        action: "query.run",
        target_label: "",
        team_id: 2,
        http_method: "POST",
        route: "/api/v1/teams/:teamID/sources/:sourceID/logs/query",
        changes: {},
        detail: { query, language: "clickhouse-sql", mode: "stream", time_from: "2026-11-15T09:00:00Z", time_to: "2026-11-15T10:00:00Z", rows_returned: 1200, bytes_read: 2048, duration_ms: 35 },
      })],
      next_cursor: "",
    });
    const { root } = await render("/admin/audit-log");
    const row = root.querySelector<HTMLElement>("[data-audit-row]");
    expect(row?.textContent).toContain("app logs");

    row?.click();
    await flush();
    const detail = root.querySelector<HTMLElement>("[data-audit-detail]");
    expect(detail?.querySelector("pre")?.textContent).toBe(query);
    const summary = detail?.querySelector<HTMLElement>("[data-audit-query]")?.textContent ?? "";
    expect(summary).toContain("Time range");
    expect(summary).toContain("1,200");
    expect(summary).toContain("Bytes read2 KiB");
    expect(summary).toContain("35 ms");
    expect(detail?.textContent).toContain("payments");
    expect(detail?.textContent).not.toContain("No field changes");
    // The query shows once, in the block, not again in the detail list.
    expect(detail?.textContent?.split(query)).toHaveLength(2);
  });

  it("shows an export's query from its create fields and a download's file", async () => {
    pages.push({
      events: [
        event({ id: "c", action: "log_export.create", target_type: "log_export", target_id: "job-1", target_label: "", changes: { query: { to: "SELECT 1" }, format: { to: "csv" } }, detail: { mode: "job" } }),
        event({ id: "d", action: "log_export.download", target_type: "log_export", target_id: "job-1", target_label: "", changes: {}, detail: { file_name: "logchef.csv", rows_exported: 3, bytes_written: 12 } }),
      ],
      next_cursor: "",
    });
    const { root } = await render("/admin/audit-log");
    const rows = root.querySelectorAll<HTMLElement>("[data-audit-row]");
    rows.forEach((row) => row.click());
    await flush();
    const [create, download] = [...root.querySelectorAll<HTMLElement>("[data-audit-detail]")];
    expect(create.querySelector("pre")?.textContent).toBe("SELECT 1");
    expect(create.textContent).toContain("format");
    expect(download.querySelector("pre")).toBeNull();
    expect(download.querySelector("[data-audit-query]")?.textContent).toContain("Bytes written12 B");
    expect(download.textContent).toContain("logchef.csv");
    expect(download.textContent).not.toContain("No field changes");
    expect(download.querySelector<HTMLAnchorElement>("a[href*='target_id=job-1']")).not.toBeNull();
  });

  it("loads the next page with the cursor and the same time range", async () => {
    pages.push({ events: [event({ id: "a" })], next_cursor: "cursor-1" });
    pages.push({ events: [event({ id: "b" })], next_cursor: "" });
    const { root } = await render("/admin/audit-log");
    button(root, "Load more").click();
    await flush();
    expect(calls).toHaveLength(2);
    expect(calls[1].cursor).toBe("cursor-1");
    expect(calls[1].filters).toEqual(calls[0].filters);
    expect(root.querySelectorAll("[data-audit-row]")).toHaveLength(2);
    expect([...root.querySelectorAll("button")].some((b) => b.textContent?.trim() === "Load more")).toBe(false);
  });

  it("sorts by time on the server and keeps the order in the URL", async () => {
    pages.push({ events: [event({})], next_cursor: "" });
    const { root, router } = await render("/admin/audit-log?outcome=failure");
    button(root, "Time").click();
    await flush();
    expect(router.currentRoute.value.query).toEqual({ outcome: "failure", order: "asc" });
    expect(calls).toHaveLength(2);
    expect(calls[1].filters.order).toBe("asc");
    expect(calls[1].filters.outcome).toBe("failure");
  });

  it("downloads the selected filters even when loading them failed", async () => {
    URL.createObjectURL = vi.fn(() => "blob:audit");
    URL.revokeObjectURL = vi.fn();
    pages.push({ events: [event({})], next_cursor: "" });
    pages.push(new Error("store down"));
    const { root, router } = await render("/admin/audit-log");
    await router.replace({ query: { target_type: "source", target_id: "7" } });
    await flush();
    expect(root.textContent).toContain("store down");
    button(root, "Download").click();
    await flush();
    expect(exports).toHaveLength(1);
    expect(exports[0].target_type).toBe("source");
    expect(exports[0].target_id).toBe("7");
  });

  it("tells an empty log apart from filters that match nothing", async () => {
    const empty = await render("/admin/audit-log");
    expect(empty.root.textContent).toContain("No audit events yet");
    unmount?.();
    host?.remove();

    const filtered = await render("/admin/audit-log?action=source.update");
    expect(filtered.root.textContent).toContain("No events match");
  });
});
