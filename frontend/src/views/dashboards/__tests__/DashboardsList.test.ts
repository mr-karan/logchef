import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { createApp, nextTick, type App } from "vue";
import { createPinia } from "pinia";
import { createMemoryHistory, createRouter, type Router } from "vue-router";
import { i18n } from "@/i18n";

const mocks = vi.hoisted(() => ({
  list: vi.fn(),
  get: vi.fn(),
  create: vi.fn(),
  update: vi.fn(),
  remove: vi.fn(),
}));

vi.mock("@/api/dashboards", async (importOriginal) => {
  const actual = await importOriginal<typeof import("@/api/dashboards")>();
  return { ...actual, dashboardsApi: mocks };
});

// An admin may use any team, so Duplicate needs no team load.
vi.mock("@/stores/auth", () => ({ useAuthStore: () => ({ user: { id: 1, role: "admin" } }) }));
vi.mock("@/stores/teams", () => ({ useTeamsStore: () => ({ userTeams: [], loadUserTeams: async () => ({ success: true }) }) }));

import DashboardsList from "../DashboardsList.vue";
import { emptyDashboardPanels } from "@/utils/dashboardPanels";
import type { Dashboard, DashboardSummary } from "@/api/dashboards";

type ValidDashboard = Extract<Dashboard, { corrupt: false }>;

function summary(id: number, overrides: Partial<DashboardSummary> = {}): DashboardSummary {
  return {
    id,
    name: `Board ${id}`,
    description: "",
    panel_count: 2,
    panel_types: ["chart", "stat"],
    tab_count: 1,
    revision: 1,
    can_edit: true,
    corrupt: false,
    created_at: "2026-07-08T00:00:00Z",
    updated_at: "2026-07-08T00:00:00Z",
    ...overrides,
  };
}

function dashboard(id: number, overrides: Partial<ValidDashboard> = {}): ValidDashboard {
  return {
    id,
    name: `Board ${id}`,
    description: `about ${id}`,
    corrupt: false,
    panels: {
      ...emptyDashboardPanels(),
      tabs: [{ id: "main", title: "Main", layout: [{ id: "p1", x: 0, y: 0, w: 6, h: 2 }] }],
      panels: [{
        id: "p1", kind: "query", title: "errors", team_id: 1, source_id: 1,
        query: { mode: "count", language: "logchefql", text: "level=error", interval: "auto" },
        viz: { type: "stat", reduce: "sum" },
      }],
    },
    revision: 1,
    can_edit: true,
    created_at: "2026-07-08T00:00:00Z",
    updated_at: "2026-07-08T00:00:00Z",
    ...overrides,
  };
}

async function settle() {
  await new Promise((resolve) => setTimeout(resolve, 0));
  await nextTick();
}

describe("DashboardsList", () => {
  let app: App;
  let host: HTMLDivElement;
  let router: Router;

  async function mount(rows: DashboardSummary[]) {
    mocks.list.mockResolvedValue({ status: "success", data: rows });
    host = document.createElement("div");
    document.body.appendChild(host);
    router = createRouter({ history: createMemoryHistory(), routes: [
      { path: "/", component: DashboardsList },
      { path: "/dashboards/:id", component: { template: "<div>Dashboard</div>" } },
    ] });
    await router.push("/");
    app = createApp(DashboardsList);
    app.use(createPinia()).use(i18n).use(router).mount(host);
    await settle();
  }

  beforeEach(() => {
    vi.clearAllMocks();
    vi.stubGlobal("ResizeObserver", class {
      observe() {}
      unobserve() {}
      disconnect() {}
    });
    mocks.create.mockResolvedValue({ status: "success", data: dashboard(9) });
  });

  afterEach(() => {
    app?.unmount();
    host?.remove();
    vi.unstubAllGlobals();
  });

  function cards(): HTMLElement[] {
    return [...host.querySelectorAll<HTMLElement>("h2")].map((h) => h.closest("div.group") as HTMLElement);
  }

  async function openMenu(card: HTMLElement) {
    const trigger = card.querySelector<HTMLButtonElement>(`button[aria-label="${i18n.global.t("ui.actions")}"]`);
    if (!trigger) throw new Error("actions menu trigger not rendered");
    trigger.dispatchEvent(new KeyboardEvent("keydown", { key: "Enter", bubbles: true }));
    await vi.waitFor(() => expect(document.querySelector("[role=menu]")).not.toBeNull());
    return [...document.querySelectorAll<HTMLElement>("[role=menuitem]")];
  }

  async function clickMenuItem(card: HTMLElement, label: string) {
    const items = await openMenu(card);
    const item = items.find((el) => el.textContent?.trim() === label);
    if (!item) throw new Error(`menu item ${label} not rendered among ${items.map((i) => i.textContent?.trim()).join(", ")}`);
    item.dispatchEvent(new PointerEvent("pointerdown", { bubbles: true, pointerType: "mouse" }));
    item.dispatchEvent(new PointerEvent("pointerup", { bubbles: true, pointerType: "mouse" }));
    item.click();
    await settle();
  }

  function dialog(): HTMLElement | null {
    return document.querySelector<HTMLElement>("[role=dialog]");
  }

  function nameInput(): HTMLInputElement {
    const input = dialog()?.querySelector<HTMLInputElement>("#dash-name");
    if (!input) throw new Error("name input not rendered");
    return input;
  }

  it("renders summaries from the derived counts and kinds", async () => {
    await mount([summary(1), summary(2, { panel_count: 0, panel_types: [] })]);
    expect(mocks.list).toHaveBeenCalledTimes(1);
    const [first, second] = cards();
    expect(first.textContent).toContain("2 panels");
    expect(first.querySelectorAll("svg.lucide-chart-column, svg.lucide-bar-chart-3, svg.lucide-hash").length).toBeGreaterThan(0);
    expect(second.textContent).toContain("0 panels");
  });

  it("Duplicate reads the full dashboard by id and prefills the copy from it", async () => {
    mocks.get.mockResolvedValue({ status: "success", data: dashboard(1) });
    await mount([summary(1)]);
    await clickMenuItem(cards()[0], i18n.global.t("ui.duplicate"));
    expect(mocks.get).toHaveBeenCalledWith(1);
    await vi.waitFor(() => expect(dialog()).not.toBeNull());
    expect(nameInput().value).toBe("Board 1 (copy)");

    const create = [...dialog()!.querySelectorAll("button")].find((b) => b.textContent?.trim() === i18n.global.t("ui.create"));
    create?.click();
    await settle();
    expect(mocks.create).toHaveBeenCalledTimes(1);
    const body = mocks.create.mock.calls[0][0];
    expect(body.name).toBe("Board 1 (copy)");
    expect(body.description).toBe("about 1");
    expect(body.panels.panels.map((p: { id: string }) => p.id)).toEqual(["p1"]);
    expect(body.panels.version).toBe(2);
  });

  it("opens no dialog when the read fails", async () => {
    mocks.get.mockRejectedValue({ status: "error", error_type: "NotFoundError", message: "gone" });
    await mount([summary(1)]);
    await clickMenuItem(cards()[0], i18n.global.t("ui.duplicate"));
    await settle();
    expect(mocks.get).toHaveBeenCalledWith(1);
    expect(dialog()).toBeNull();
  });

  it("lets a later Duplicate win over a slower earlier one", async () => {
    let resolveFirst!: (value: unknown) => void;
    mocks.get.mockImplementationOnce(() => new Promise((resolve) => { resolveFirst = resolve; }));
    mocks.get.mockResolvedValueOnce({ status: "success", data: dashboard(2) });
    await mount([summary(1), summary(2)]);
    await clickMenuItem(cards()[0], i18n.global.t("ui.duplicate"));
    await clickMenuItem(cards()[1], i18n.global.t("ui.duplicate"));
    await vi.waitFor(() => expect(nameInput().value).toBe("Board 2 (copy)"));
    resolveFirst({ status: "success", data: dashboard(1) });
    await settle();
    expect(nameInput().value).toBe("Board 2 (copy)");
  });

  it("hides Duplicate for a corrupt summary", async () => {
    await mount([summary(1, { corrupt: true, panel_count: 0, panel_types: [] })]);
    const items = await openMenu(cards()[0]);
    const labels = items.map((i) => i.textContent?.trim());
    expect(labels).not.toContain(i18n.global.t("ui.duplicate"));
    expect(labels).toContain(i18n.global.t("ui.delete"));
  });

  it("creates an empty dashboard as a schema 2 blob", async () => {
    await mount([summary(1)]);
    const newButton = [...host.querySelectorAll("button")].find((b) => b.textContent?.trim() === i18n.global.t("ui.newDashboard"));
    newButton?.click();
    await vi.waitFor(() => expect(dialog()).not.toBeNull());
    const input = nameInput();
    input.value = "Fresh";
    input.dispatchEvent(new Event("input", { bubbles: true }));
    await nextTick();
    const create = [...dialog()!.querySelectorAll("button")].find((b) => b.textContent?.trim() === i18n.global.t("ui.create"));
    create?.click();
    await settle();
    expect(mocks.create.mock.calls[0][0]).toEqual({ name: "Fresh", description: "", panels: emptyDashboardPanels() });
  });

  it("cuts the localized copy name to 200 code points and keeps the dialog within the bound", async () => {
    const name = "😀".repeat(200);
    mocks.get.mockResolvedValue({ status: "success", data: dashboard(1, { name }) });
    await mount([summary(1, { name })]);
    await clickMenuItem(cards()[0], i18n.global.t("ui.duplicate"));
    await vi.waitFor(() => expect(dialog()).not.toBeNull());
    const copy = nameInput().value;
    expect(Array.from(copy)).toHaveLength(200);
    expect(copy.endsWith(" (copy)")).toBe(true);

    const input = nameInput();
    input.value = "é".repeat(205);
    input.dispatchEvent(new Event("input", { bubbles: true }));
    await nextTick();
    expect(Array.from(nameInput().value)).toHaveLength(200);
  });
});
