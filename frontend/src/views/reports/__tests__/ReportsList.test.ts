import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { createApp, nextTick, type App } from "vue";
import { createPinia } from "pinia";
import { createMemoryHistory, createRouter } from "vue-router";
import { i18n } from "@/i18n";

const mocks = vi.hoisted(() => ({
  reports: {
    list: vi.fn(), get: vi.fn(), create: vi.fn(), update: vi.fn(), remove: vi.fn(),
    pause: vi.fn(), resume: vi.fn(), claim: vi.fn(), run: vi.fn(), preview: vi.fn(), runs: vi.fn(),
    recipientOptions: vi.fn(), leave: vi.fn(),
  },
  savedQueries: { list: vi.fn() },
  dashboards: { list: vi.fn(), get: vi.fn() },
}));

vi.mock("@/api/reports", async (importOriginal) => {
  const actual = await importOriginal<typeof import("@/api/reports")>();
  return { ...actual, reportsApi: mocks.reports };
});
vi.mock("@/api/savedQueries", async (importOriginal) => {
  const actual = await importOriginal<typeof import("@/api/savedQueries")>();
  return { ...actual, savedQueriesApi: mocks.savedQueries };
});
vi.mock("@/api/dashboards", async (importOriginal) => {
  const actual = await importOriginal<typeof import("@/api/dashboards")>();
  return { ...actual, dashboardsApi: mocks.dashboards };
});
vi.mock("@/stores/auth", () => ({ useAuthStore: () => ({ user: { id: "1", role: "member", full_name: "Owner", email: "owner@example.com" } }) }));

import ReportsList from "../ReportsList.vue";
import type { Report, ReportInput, ReportRun } from "@/api/reports";
import type { SavedQuery } from "@/api/savedQueries";

const savedQuery: SavedQuery = {
  id: 5, source_id: 2, name: "5xx by status", description: "", query_language: "clickhouse-sql", editor_mode: "native",
  query_content: JSON.stringify({ content: "SELECT 1 WHERE host = {{host}}", variables: [{ name: "host", label: "Host", type: "text", value: "", defaultValue: "api-1" }] }),
  created_at: "", updated_at: "", source_name: "app_logs",
};

// A list row carries no recipients; one report read by its editor does.
function report(input: Omit<ReportInput, "recipients">, id = 9): Report {
  return {
    id, name: input.name, source: { ...input.source, name: savedQuery.name }, schedule: input.schedule, timezone: input.timezone,
    time_range: input.time_range, range_anchor: input.range_anchor, variables: input.variables, max_rows: input.max_rows,
    skip_if_empty: input.skip_if_empty, attach_csv: input.attach_csv, owner: { id: 1, name: "Owner", email: "owner@example.com" }, state: "active",
    pause_reason: "", last_run: null, can_edit: true, warnings: [], created_at: "2026-11-16T00:00:00Z", updated_at: "2026-11-16T00:00:00Z",
  };
}

const sentRun: ReportRun = {
  id: 3, report_id: 9, trigger: "manual", scheduled_for: "2026-11-16T03:30:00Z", status: "sent", reason: "",
  panels: [{ panel_id: "query", title: "5xx by status", status: "ok", rows: 4 }],
  deliveries: [{ class: "user", target: "1", via: "direct", status: "sent", attempts: 1 }],
  started_at: "2026-11-16T03:30:00Z", finished_at: "2026-11-16T03:30:02Z",
};

async function settle() {
  await new Promise((resolve) => setTimeout(resolve, 0));
  await nextTick();
}

function setValue(el: HTMLInputElement | HTMLSelectElement, value: string) {
  el.value = value;
  el.dispatchEvent(new Event(el instanceof HTMLSelectElement ? "change" : "input", { bubbles: true }));
}

function button(label: string): HTMLButtonElement {
  const found = [...document.querySelectorAll<HTMLButtonElement>("button")].find((b) => b.textContent?.trim() === label);
  if (!found) throw new Error(`button ${label} not rendered`);
  return found;
}

describe("ReportsList", () => {
  let app: App;
  let host: HTMLDivElement;

  async function mount(path: string, rows: Report[] = []) {
    mocks.reports.list.mockResolvedValue({ status: "success", data: rows });
    mocks.reports.get.mockImplementation(async (id: number) => {
      const row = rows.find((r) => r.id === id);
      return { status: "success", data: row && { ...row, recipients: [{ kind: "user", user_id: 1, name: "Owner", email: "owner@example.com" }] } };
    });
    host = document.createElement("div");
    document.body.appendChild(host);
    const router = createRouter({ history: createMemoryHistory(), routes: [{ path: "/reports/:id?", name: "Reports", component: ReportsList }] });
    await router.push(path);
    app = createApp(ReportsList);
    app.use(createPinia()).use(i18n).use(router).mount(host);
    await settle();
  }

  beforeEach(() => {
    vi.clearAllMocks();
    vi.stubGlobal("ResizeObserver", class { observe() {} unobserve() {} disconnect() {} });
    mocks.savedQueries.list.mockResolvedValue({ status: "success", data: [savedQuery] });
    mocks.reports.create.mockImplementation(async (input: ReportInput) => ({ status: "success", data: report(input) }));
    mocks.reports.run.mockResolvedValue({ status: "success", data: { ...sentRun, status: "running" } });
    mocks.reports.runs.mockResolvedValue({ status: "success", data: [sentRun] });
    mocks.reports.remove.mockResolvedValue({ status: "success", data: null });
    mocks.dashboards.list.mockResolvedValue({ status: "success", data: [{ id: 4, name: "Edge" }] });
    mocks.reports.recipientOptions.mockResolvedValue({ status: "success", data: { users: [{ id: 1, name: "Owner", email: "owner@example.com" }, { id: 2, name: "Mate", email: "mate@example.com" }], teams: [{ id: 3, name: "payments" }] } });
    mocks.reports.leave.mockResolvedValue({ status: "success", data: null });
    mocks.dashboards.get.mockResolvedValue({ status: "success", data: { id: 4, corrupt: false, panels: { tabs: [{ id: "main", title: "Main", layout: [] }] } } });
  });

  afterEach(() => {
    app?.unmount();
    host?.remove();
    vi.unstubAllGlobals();
  });

  it("creates a report on a saved query, sends a test and shows the run", async () => {
    await mount("/reports?saved_query=5");
    await vi.waitFor(() => expect(document.querySelector("#report-source")).not.toBeNull());
    const source = document.querySelector<HTMLSelectElement>("#report-source")!;
    await vi.waitFor(() => expect(source.value).toBe("5"));
    expect(document.querySelector<HTMLInputElement>("#report-var-host")!.placeholder).toBe("api-1");

    setValue(document.querySelector<HTMLInputElement>("#report-name")!, "Daily 5xx");
    setValue(document.querySelector<HTMLSelectElement>("#report-frequency")!, "weekly");
    await settle();
    setValue(document.querySelector<HTMLSelectElement>("#report-frequency")!, "hourly");
    setValue(document.querySelector<HTMLSelectElement>("#report-range")!, "today");
    await settle();
    button(i18n.global.t("reports.create")).click();
    await settle();

    expect(mocks.reports.create).toHaveBeenCalledTimes(1);
    const input = mocks.reports.create.mock.calls[0][0] as ReportInput;
    expect(input.source).toEqual({ kind: "saved_query", saved_query_id: 5 });
    expect(input.schedule).toEqual({ frequency: "hourly", minute: 0 });
    expect(input.time_range).toBe("today");
    expect(input.range_anchor).toBe("send_time");
    expect(input.variables).toEqual({});
    // A new report goes to the person who creates it.
    expect(input.recipients).toEqual([{ kind: "user", user_id: 1 }]);

    button(i18n.global.t("reports.sendTest")).click();
    await settle();
    expect(mocks.reports.run).toHaveBeenCalledWith(9, "me");
    expect(mocks.reports.create).toHaveBeenCalledTimes(1);

    button(i18n.global.t("reports.history")).click();
    await settle();
    await vi.waitFor(() => expect(document.querySelector('[data-run-status="sent"]')).not.toBeNull());
    expect(mocks.reports.runs).toHaveBeenCalledWith(9, 0, 20);
    expect(document.querySelector('[data-run-status="sent"]')!.textContent).toContain(i18n.global.t("reports.status_sent"));
    // The list's last run follows the history the drawer loaded.
    expect(host.querySelector('[data-report-id="9"]')!.textContent).toContain(i18n.global.t("reports.status_sent"));
  });

  it("offers the CSV attachment for a saved query only", async () => {
    await mount("/reports?saved_query=5");
    await vi.waitFor(() => expect(document.querySelector("#report-attach-csv")).not.toBeNull());
    document.querySelector<HTMLButtonElement>("#report-attach-csv")!.click();
    setValue(document.querySelector<HTMLSelectElement>("#report-source-kind")!, "dashboard");
    await settle();
    expect(document.querySelector("#report-attach-csv")).toBeNull();
    await vi.waitFor(() => expect(document.querySelector<HTMLSelectElement>("#report-dashboard")!.options.length).toBe(2));
    setValue(document.querySelector<HTMLSelectElement>("#report-dashboard")!, "4");
    setValue(document.querySelector<HTMLInputElement>("#report-name")!, "Edge daily");
    await settle();
    button(i18n.global.t("reports.create")).click();
    await settle();
    const input = mocks.reports.create.mock.calls[0][0] as ReportInput;
    expect(input.source).toEqual({ kind: "dashboard", dashboard_id: 4 });
    expect(input.attach_csv).toBe(false);
  });

  it("takes a list default and keeps a stored list as a list", async () => {
    const listQuery: SavedQuery = {
      ...savedQuery, id: 6, name: "by hosts",
      query_content: JSON.stringify({ content: "SELECT 1 WHERE host IN ({{hosts}})", variables: [{ name: "hosts", label: "Hosts", type: "text", inputType: "multiselect", value: [], defaultValue: ["api-1", "api-2"] }] }),
    };
    mocks.savedQueries.list.mockResolvedValue({ status: "success", data: [listQuery] });
    const existing = report({ name: "Hosts", source: { kind: "saved_query", saved_query_id: 6 }, schedule: { frequency: "daily", hour: 9, minute: 0 }, timezone: "UTC", time_range: "24h", range_anchor: "send_time", variables: { hosts: ["web-1", "web-2"] }, max_rows: 25, skip_if_empty: false, attach_csv: true }, 7);
    mocks.reports.update.mockImplementation(async (_id: number, input: ReportInput) => ({ status: "success", data: report(input, 7) }));
    await mount("/reports/7", [existing]);
    await vi.waitFor(() => expect(document.querySelector<HTMLInputElement>("#report-var-hosts")?.value).toBe("web-1, web-2"));
    setValue(document.querySelector<HTMLInputElement>("#report-name")!, "Hosts daily");
    await settle();
    button(i18n.global.t("ui.save")).click();
    await settle();
    expect((mocks.reports.update.mock.calls[0][1] as ReportInput).variables).toEqual({ hosts: ["web-1", "web-2"] });
    // A save keeps the stored attachment setting: PUT replaces every field.
    expect((mocks.reports.update.mock.calls[0][1] as ReportInput).attach_csv).toBe(true);

    setValue(document.querySelector<HTMLInputElement>("#report-var-hosts")!, "");
    await settle();
    expect(button(i18n.global.t("ui.save")).disabled).toBe(false);
    button(i18n.global.t("ui.save")).click();
    await settle();
    expect((mocks.reports.update.mock.calls[1][1] as ReportInput).variables).toEqual({});
  });

  it("does not let a save that finishes late take over another report", async () => {
    const base = { source: { kind: "saved_query" as const, saved_query_id: 5 }, schedule: { frequency: "daily" as const, hour: 9, minute: 0 }, timezone: "UTC", time_range: "24h", range_anchor: "send_time" as const, variables: {}, max_rows: 25, skip_if_empty: false, attach_csv: false };
    const a = report({ ...base, name: "A" }, 1);
    const b = report({ ...base, name: "B" }, 2);
    let finishA: (value: unknown) => void = () => {};
    mocks.reports.update.mockImplementationOnce(() => new Promise((resolve) => { finishA = resolve; }));
    mocks.reports.update.mockImplementation(async (id: number, input: ReportInput) => ({ status: "success", data: report(input, id) }));
    await mount("/reports", [a, b]);
    const edit = (id: number) => host.querySelector<HTMLButtonElement>(`[data-report-id="${id}"] button[aria-label="${i18n.global.t("ui.edit")}"]`)!;

    edit(1).click();
    await settle();
    setValue(document.querySelector<HTMLInputElement>("#report-name")!, "A changed");
    await settle();
    button(i18n.global.t("ui.save")).click();
    await settle();
    button(i18n.global.t("ui.close")).click();
    await settle();
    edit(2).click();
    await settle();
    finishA({ status: "success", data: report({ ...base, name: "A changed" }, 1) });
    await settle();

    setValue(document.querySelector<HTMLInputElement>("#report-name")!, "B edited");
    await settle();
    button(i18n.global.t("ui.save")).click();
    await settle();
    expect(mocks.reports.update.mock.calls.map((c) => [c[0], (c[1] as ReportInput).name])).toEqual([[1, "A changed"], [2, "B edited"]]);
  });

  it("deletes a report only after the confirm dialog", async () => {
    const existing = report({ name: "Nightly", source: { kind: "saved_query", saved_query_id: 5 }, schedule: { frequency: "daily", hour: 9, minute: 0 }, timezone: "UTC", time_range: "24h", range_anchor: "send_time", variables: {}, max_rows: 25, skip_if_empty: false, attach_csv: false }, 4);
    await mount("/reports", [existing]);
    expect(host.textContent).toContain("Nightly");
    const trigger = host.querySelector<HTMLButtonElement>(`button[aria-label="${i18n.global.t("ui.actions")}"]`)!;
    trigger.dispatchEvent(new KeyboardEvent("keydown", { key: "Enter", bubbles: true }));
    await vi.waitFor(() => expect(document.querySelector("[role=menu]")).not.toBeNull());
    const item = [...document.querySelectorAll<HTMLElement>("[role=menuitem]")].find((el) => el.textContent?.trim() === i18n.global.t("ui.delete"))!;
    item.dispatchEvent(new PointerEvent("pointerdown", { bubbles: true, pointerType: "mouse" }));
    item.dispatchEvent(new PointerEvent("pointerup", { bubbles: true, pointerType: "mouse" }));
    item.click();
    await settle();
    expect(mocks.reports.remove).not.toHaveBeenCalled();
    const confirm = [...document.querySelectorAll<HTMLButtonElement>("[role=alertdialog] button")].find((b) => b.textContent?.trim() === i18n.global.t("ui.delete"))!;
    confirm.click();
    await settle();
    expect(mocks.reports.remove).toHaveBeenCalledWith(4);
    await vi.waitFor(() => expect(host.textContent).not.toContain("Nightly"));
  });

  it("reads the stored recipients before a save and adds a team", async () => {
    const existing = report({ name: "Nightly", source: { kind: "saved_query", saved_query_id: 5 }, schedule: { frequency: "daily", hour: 9, minute: 0 }, timezone: "UTC", time_range: "24h", range_anchor: "send_time", variables: {}, max_rows: 25, skip_if_empty: false, attach_csv: false }, 4);
    mocks.reports.update.mockImplementation(async (id: number, input: ReportInput) => ({ status: "success", data: report(input, id) }));
    let finishGet: (value: unknown) => void = () => {};
    await mount("/reports", [existing]);
    mocks.reports.get.mockImplementationOnce(() => new Promise((resolve) => { finishGet = resolve; }));
    host.querySelector<HTMLButtonElement>(`[data-report-id="4"] button[aria-label="${i18n.global.t("ui.edit")}"]`)!.click();
    await settle();
    setValue(document.querySelector<HTMLInputElement>("#report-name")!, "Nightly 5xx");
    await settle();
    // Until the stored recipients arrive, a save would drop them.
    expect(button(i18n.global.t("ui.save")).disabled).toBe(true);

    finishGet({ status: "success", data: { ...existing, recipients: [{ kind: "user", user_id: 2, name: "Mate", email: "mate@example.com" }] } });
    await vi.waitFor(() => expect(document.querySelector('[data-recipient="user:2"]')).not.toBeNull());
    const picker = document.querySelector<HTMLSelectElement>("#report-add-recipient")!;
    expect([...picker.options].map((o) => o.value)).toEqual(["", "user:1", "team:3"]);
    setValue(picker, "team:3");
    await settle();
    expect(document.querySelector('[data-recipient="team:3"]')).not.toBeNull();
    expect(picker.value).toBe("");

    button(i18n.global.t("ui.save")).click();
    await settle();
    const input = mocks.reports.update.mock.calls[0][1] as ReportInput;
    expect(input.name).toBe("Nightly 5xx");
    expect(input.recipients).toEqual([{ kind: "user", user_id: 2 }, { kind: "team", team_id: 3 }]);
  });

  it("stops receiving a report sent to the caller only after the confirm dialog", async () => {
    const received = { ...report({ name: "Their report", source: { kind: "saved_query", saved_query_id: 5 }, schedule: { frequency: "daily", hour: 9, minute: 0 }, timezone: "UTC", time_range: "", range_anchor: "send_time", variables: {}, max_rows: 0, skip_if_empty: false, attach_csv: false }, 8), can_edit: false };
    await mount("/reports", [received]);
    const row = host.querySelector('[data-report-id="8"]')!;
    expect(row.querySelector(`button[aria-label="${i18n.global.t("ui.edit")}"]`)).toBeNull();
    row.querySelector<HTMLButtonElement>(`button[aria-label="${i18n.global.t("reports.leave")}"]`)!.click();
    await settle();
    expect(mocks.reports.leave).not.toHaveBeenCalled();
    const confirm = [...document.querySelectorAll<HTMLButtonElement>("[role=alertdialog] button")].find((b) => b.textContent?.trim() === i18n.global.t("reports.leave"))!;
    confirm.click();
    await settle();
    expect(mocks.reports.leave).toHaveBeenCalledWith(8);
    // A team may still send it, so the list is read again.
    expect(mocks.reports.list).toHaveBeenCalledTimes(2);
  });
});
