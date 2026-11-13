import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { createApp, defineComponent, nextTick, type App } from "vue";
import { createPinia } from "pinia";
import { createMemoryHistory, createRouter, type Router } from "vue-router";
import { i18n } from "@/i18n";

vi.mock("@/api/meta", () => ({ metaApi: { getMeta: async () => ({ status: "success", data: { version: "new-version", http_server_timeout: "30s", max_query_limit: 100000 } }) } }));

const compatibility = vi.hoisted(() => ({ future: false, stub: false }));
const api = vi.hoisted(() => ({ get: vi.fn(), put: vi.fn(), post: vi.fn() }));
vi.mock("@/api/apiUtils", () => ({ apiClient: api }));
vi.mock("../components/PanelBuilderDrawer.vue", () => ({
  default: defineComponent({
    props: ["open", "panelId"],
    template: '<div v-if="open" data-testid="panel-editor" :data-panel-id="panelId">Panel editor</div>',
  }),
}));
import { useDashboardsStore } from "@/stores/dashboards";
import { toast } from "vue-sonner";
const dashboard = {
  id: 1, revision: 1, name: "My dashboard", description: "", created_at: "", updated_at: "", can_edit: true, corrupt: false,
  panels: { version: 2, features: [], tabs: [{ id: "main", title: "Main", layout: [] }], panels: [] },
};
const conflictError = { status: "error", error_type: "ConflictError", message: "modified", statusCode: 409 };

import DashboardView from "../DashboardView.vue";
import VersionBanner from "@/components/VersionBanner.vue";
import { useMetaStore } from "@/stores/meta";

async function settle() {
  await new Promise((resolve) => setTimeout(resolve, 0));
  await nextTick();
}

describe("DashboardView discard confirmation", () => {
  let app: App;
  let host: HTMLDivElement;
  let router: Router;
  let store: ReturnType<typeof useDashboardsStore>;

  beforeEach(async () => {
    api.get.mockReset().mockResolvedValue({ status: "success", data: dashboard });
    api.put.mockReset().mockRejectedValue(conflictError);
    api.post.mockReset();
    vi.spyOn(toast, "error");
    vi.stubGlobal("ResizeObserver", class {
      observe() {}
      unobserve() {}
      disconnect() {}
    });
    vi.spyOn(window, "confirm").mockReturnValue(false);
    host = document.createElement("div");
    document.body.appendChild(host);
    router = createRouter({ history: createMemoryHistory(), routes: [
      { path: "/", component: { template: "<div />" } },
      { path: "/dashboards/:id", component: DashboardView },
      { path: "/dashboards", component: { template: "<div>Dashboard list</div>" } },
    ] });
    await router.push("/dashboards/1");
    app = createApp(defineComponent({ template: "<router-view />" }));
    const pinia = createPinia();
    app.use(pinia).use(i18n).use(router).mount(host);
    store = useDashboardsStore(pinia);
    await settle();
    button("Edit").click();
    await settle();
    const name = host.querySelector<HTMLInputElement>('input[aria-label="Name"]');
    if (!name) throw new Error("dashboard name was not rendered");
    name.value = "My edited dashboard";
    name.dispatchEvent(new Event("input", { bubbles: true }));
    await nextTick();
  });

  afterEach(() => {
    app.unmount();
    host.remove();
    vi.restoreAllMocks();
    vi.unstubAllGlobals();
  });

  function button(text: string, root: ParentNode = host) {
    const match = [...root.querySelectorAll("button")].find((element) => element.textContent?.trim() === text);
    if (!match) throw new Error(`button ${text} was not rendered`);
    return match;
  }

  function dialogButton(text: string) {
    const dialog = document.querySelector('[role="alertdialog"]');
    if (!dialog) throw new Error("discard dialog was not rendered");
    return button(text, dialog);
  }

  async function conflictOnSave() {
    button("Save").click();
    await settle();
    expect(document.querySelector('[role="alertdialog"]')?.textContent).toContain("Save as copy");
    expect(store.editName).toBe("My edited dashboard");
  }

  it("opens the conflict dialog after a 409 and keeps the draft when dismissed", async () => {
    await conflictOnSave();
    dialogButton("Keep editing").click();
    await settle();
    expect(document.querySelector('[role="alertdialog"]')).toBeNull();
    expect(store.isEditing).toBe(true);
    expect(store.editName).toBe("My edited dashboard");
  });

  it("saves a conflict as a copy and navigates to it", async () => {
    await conflictOnSave();
    api.post.mockResolvedValue({ status: "success", data: { ...dashboard, id: 2, name: "My edited dashboard (copy)" } });
    dialogButton("Save as copy").click();
    await settle();
    expect(router.currentRoute.value.path).toBe("/dashboards/2");
    expect(api.post.mock.calls[0]?.[1]).toMatchObject({ name: "My edited dashboard (copy)" });
    expect(document.querySelector('[role="alertdialog"]')).toBeNull();
  });

  it("preserves the draft and open panel editor when saving a copy fails", async () => {
    button("Add panel").click();
    await settle();
    const editor = host.querySelector('[data-testid="panel-editor"]');
    expect(editor).not.toBeNull();
    const panelId = editor?.getAttribute("data-panel-id");
    if (!panelId) throw new Error("panel editor has no panel");
    store.switchDraftPanelKind(panelId, "table");
    store.updateDraftPanel(panelId, { team_id: 1, source_id: 1 });
    const draft = JSON.stringify(store.editDraft);
    await conflictOnSave();
    api.post.mockRejectedValue({ status: "error", message: "Copy unavailable" });
    dialogButton("Save as copy").click();
    await settle();
    expect(host.querySelector('[data-testid="panel-editor"]')).toBe(editor);
    expect(JSON.stringify(store.editDraft)).toBe(draft);
    expect(store.isEditing).toBe(true);
    expect(document.querySelector('[role="alertdialog"]')).not.toBeNull();
    expect(toast.error).toHaveBeenCalledWith(expect.any(String), expect.objectContaining({ description: "Copy unavailable" }));
  });

  it("discards the draft after loading the latest dashboard", async () => {
    await conflictOnSave();
    api.get.mockResolvedValue({ status: "success", data: { ...dashboard, revision: 2, name: "Theirs" } });
    dialogButton("Discard and load latest").click();
    await settle();
    expect(document.querySelector('[role="alertdialog"]')).toBeNull();
    expect(store.isEditing).toBe(false);
    expect(host.querySelector("h1")?.textContent).toContain("Theirs");
  });

  it("shows a failed reload and keeps the conflict open until retry succeeds", async () => {
    await conflictOnSave();
    api.get.mockRejectedValueOnce({ status: "error", message: "Reload unavailable" });
    dialogButton("Discard and load latest").click();
    await settle();
    expect(document.querySelector('[role="alertdialog"]')).not.toBeNull();
    expect(store.isEditing).toBe(true);
    expect(store.editName).toBe("My edited dashboard");
    expect(toast.error).toHaveBeenCalledWith(expect.any(String), expect.objectContaining({ description: "Reload unavailable" }));
    api.get.mockResolvedValue({ status: "success", data: { ...dashboard, revision: 2, name: "Theirs" } });
    dialogButton("Discard and load latest").click();
    await settle();
    expect(host.querySelector("h1")?.textContent).toContain("Theirs");
    expect(store.isEditing).toBe(false);
  });

  it.each([false, true])("confirms overwrite against the fetched revision, repeated conflict %s", async (repeatConflict) => {
    await conflictOnSave();
    api.get.mockResolvedValue({ status: "success", data: { ...dashboard, revision: 2 } });
    dialogButton("Overwrite with mine").click();
    await settle();
    expect(document.querySelector('[role="alertdialog"]')?.textContent).toContain("Revision 2");
    expect(api.put).toHaveBeenCalledTimes(1);
    if (!repeatConflict) api.put.mockResolvedValue({ status: "success", data: { ...dashboard, revision: 3, name: "My edited dashboard" } });
    dialogButton("Overwrite").click();
    await settle();
    expect(api.put.mock.calls[1]?.[1]).toMatchObject({ revision: 2, name: "My edited dashboard" });
    if (repeatConflict) {
      expect(document.querySelector('[role="alertdialog"]')?.textContent).toContain("Save as copy");
      expect(store.isEditing).toBe(true);
      expect(store.editName).toBe("My edited dashboard");
    } else {
      expect(document.querySelector('[role="alertdialog"]')).toBeNull();
      expect(store.current?.revision).toBe(3);
    }
  });

  it("keeps edits when the discard dialog is canceled and discards only on confirmation", async () => {
    button("Cancel").click();
    await settle();
    expect(document.querySelector('[role="alertdialog"]')).not.toBeNull();
    expect(window.confirm).not.toHaveBeenCalled();
    dialogButton("Cancel").click();
    await settle();
    expect(host.querySelector<HTMLInputElement>('input[aria-label="Name"]')?.value).toBe("My edited dashboard");
    button("Cancel").click();
    await settle();
    dialogButton("Discard changes").click();
    await settle();
    expect(host.querySelector("h1")?.textContent).toContain("My dashboard");
  });

  it("blocks reload while dirty and removes the guard after discard or navigation", async () => {
    const reload = new Event("beforeunload", { cancelable: true });
    window.dispatchEvent(reload);
    expect(reload.defaultPrevented).toBe(true);
    expect(host.querySelector<HTMLInputElement>('input[aria-label="Name"]')?.value).toBe("My edited dashboard");
    button("Cancel").click();
    await settle();
    dialogButton("Discard changes").click();
    await settle();
    const cleanReload = new Event("beforeunload", { cancelable: true });
    window.dispatchEvent(cleanReload);
    expect(cleanReload.defaultPrevented).toBe(false);
    await router.push("/dashboards");
    const afterLeaving = new Event("beforeunload", { cancelable: true });
    window.dispatchEvent(afterLeaving);
    expect(afterLeaving.defaultPrevented).toBe(false);
  });

  it("confirms in-app Back once before leaving the dashboard", async () => {
    const back = host.querySelector<HTMLButtonElement>('button[aria-label="Back to dashboards"]');
    if (!back) throw new Error("Back button was not rendered");
    back.click();
    await settle();
    expect(document.querySelector('[role="alertdialog"]')).not.toBeNull();
    expect(router.currentRoute.value.path).toBe("/dashboards/1");
    dialogButton("Discard changes").click();
    await settle();
    expect(router.currentRoute.value.path).toBe("/dashboards");
    expect(window.confirm).not.toHaveBeenCalled();
  });
});

describe("DashboardView read compatibility", () => {
  it.each([[false, true], [true, true], [false, false]])("shows reload without an edit entry, stub=%s banner=%s", async (stub, banner) => {
    api.get.mockReset().mockImplementation(async () => ({ status: "success", data: {
      ...dashboard,
      panels_corrupt: compatibility.stub,
      min_schema: compatibility.stub ? 2 : undefined,
      panels: compatibility.stub ? { version: 1, layout: [], panels: [] } : { version: 2, features: compatibility.future ? ["future_feature"] : [], tabs: [{ id: "main", title: "Main", layout: compatibility.future ? [{ id: "p", x: 0, y: 0, w: 6, h: 2 }] : [] }], panels: compatibility.future ? [{ id: "p", title: "New panel", kind: "text" }] : [] },
    } }));
    compatibility.future = !stub;
    compatibility.stub = stub;
    vi.stubGlobal("ResizeObserver", class { observe() {} unobserve() {} disconnect() {} });
    const host = document.createElement("div");
    document.body.appendChild(host);
    const router = createRouter({ history: createMemoryHistory(), routes: [{ path: "/dashboards/:id", component: DashboardView }] });
    await router.push("/dashboards/1");
    const stamp = document.createElement("meta");
    stamp.name = "logchef-version";
    stamp.content = "old-version";
    if (banner) document.head.append(stamp);
    const pinia = createPinia();
    const meta = useMetaStore(pinia);
    await meta.loadMeta();
    const app = createApp(defineComponent({ components: { VersionBanner }, template: "<VersionBanner /><router-view />" }));
    try {
      app.use(pinia).use(i18n).use(router).mount(host);
      await settle();
      const notices = [...host.querySelectorAll('[role="status"]')];
      expect(notices.filter((notice) => notice.textContent?.includes("Logchef was updated"))).toHaveLength(banner ? 1 : 0);
      expect(notices.some((notice) => notice.textContent?.includes("This dashboard is read-only. Reload to use its newer features."))).toBe(true);
      expect([...host.querySelectorAll("button")].some((button) => button.textContent?.trim() === "Edit")).toBe(false);
      expect(host.textContent).not.toContain("This dashboard is empty");
      expect(host.textContent?.split("Logchef was updated").length).toBe(banner ? 2 : 1);
      if (!stub) expect(host.querySelector(".dash-panel__message")?.textContent).toContain("This panel uses a feature this version cannot show.");
    } finally {
      app.unmount(); host.remove(); stamp.remove(); compatibility.future = false; compatibility.stub = false; vi.unstubAllGlobals();
    }
  });
});
