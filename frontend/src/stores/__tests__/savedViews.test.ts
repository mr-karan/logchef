import { beforeEach, describe, expect, it, vi } from "vitest";
import { createPinia, setActivePinia } from "pinia";
import { nextTick } from "vue";
import type { SavedView, SavedViewList } from "@/api/savedViews";
import type { SavedViewDefinition } from "@/lib/savedViewLayout";
import { useSavedViewsStore } from "../savedViews";
import { useActiveSavedViewStore } from "../activeSavedView";

const api = vi.hoisted(() => ({ list: vi.fn(), create: vi.fn(), update: vi.fn() }));
vi.mock("@/api/savedViews", () => ({ savedViewsApi: api }));

function view(id: number, overrides: Partial<SavedView> = {}, definition: Partial<SavedViewDefinition> = {}): SavedView {
  return {
    id, source_id: 5, scope: "personal", name: `view ${id}`, description: "",
    definition: { version: 1, columns: [{ name: "msg", width: 300 }, { name: "level" }], ...definition },
    is_default: false, managed: false, revision: 1, created_at: "", updated_at: "",
    can_edit: true, can_set_default: true, ...overrides,
  };
}

function list(...views: SavedView[]): SavedViewList {
  return { views, default_view_id: null };
}

beforeEach(() => {
  setActivePinia(createPinia());
  localStorage.clear();
  vi.clearAllMocks();
});

describe("saved views store: which view is active", () => {
  it("activates the view the URL names when the caller can read it", () => {
    const store = useSavedViewsStore();
    expect(store.enter(2, 5, list(view(1), view(2)), 2)).toBe("ready");
    expect(store.activeView?.id).toBe(2);
    expect(store.activeSeed).toEqual({ viewId: 2, revision: 1, definition: view(2).definition });
  });

  it("reports a URL view the caller cannot read and opens with no view", () => {
    const store = useSavedViewsStore();
    store.pick(1);
    expect(store.enter(2, 5, list(view(1)), 99)).toBe("unavailable");
    expect(store.activeViewId).toBeNull();
  });

  it("opens with no view when the list fails", () => {
    const store = useSavedViewsStore();
    expect(store.enter(2, 5, null, 1)).toBe("failed");
    expect(store.activeViewId).toBeNull();
    expect(store.views).toEqual([]);
  });

  it("remembers the last pick per team and source, including No view", () => {
    const store = useSavedViewsStore();
    store.enter(2, 5, list(view(1), view(2)), null);
    store.pick(2);
    store.enter(3, 5, list(view(1), view(2)), null);
    expect(store.activeViewId).toBeNull();
    store.enter(2, 5, list(view(1), view(2)), null);
    expect(store.activeViewId).toBe(2);
    store.pick(null);
    expect(localStorage.getItem("logchef-activeView-2-5")).toBe("null");
    store.enter(2, 5, list(view(1), view(2)), null);
    expect(store.activeViewId).toBeNull();
  });

  it("ignores a remembered view that no longer exists", () => {
    localStorage.setItem("logchef-activeView-2-5", "7");
    const store = useSavedViewsStore();
    store.enter(2, 5, list(view(1)), null);
    expect(store.activeViewId).toBeNull();
  });

  it("writes the view to the URL only for its own team and source", () => {
    const store = useSavedViewsStore();
    store.enter(2, 5, list(view(1)), 1);
    const active = useActiveSavedViewStore();
    expect(active.viewParameter(2, 5)).toBe(1);
    expect(active.viewParameter(3, 5)).toBeNull();
    expect(active.viewParameter(2, 6)).toBeNull();
  });
});

describe("saved views store: working copy", () => {
  it("takes the display mode from a view that sets one and keeps changes per revision", () => {
    const store = useSavedViewsStore();
    store.enter(2, 5, list(view(1, {}, { display_mode: "compact" }), view(2)), 1);
    expect(store.displayMode).toBe("compact");
    store.setDisplayMode("json");
    expect(store.displayMode).toBe("json");
    expect(store.isModified).toBe(true);

    store.enter(2, 5, list(view(1, {}, { display_mode: "compact" })), 1);
    expect(store.displayMode).toBe("json");
    // A teammate's update replaces the local change.
    store.enter(2, 5, list(view(1, { revision: 2 }, { display_mode: "compact" })), 1);
    expect(store.displayMode).toBe("compact");

    store.pick(2);
    expect(store.displayMode).toBeNull();
  });

  it("is modified when the table's layout differs in a part the view sets", () => {
    const store = useSavedViewsStore();
    store.enter(2, 5, list(view(1), view(2)), 1);
    store.reportTableLayout(1, { columns: [{ name: "msg", width: 300 }, { name: "level" }], sort: [{ column: "timestamp", desc: true }] });
    // The view sets no sort, so the table's default sort is not a change.
    expect(store.isModified).toBe(false);
    store.reportTableLayout(2, { columns: [{ name: "other" }] });
    expect(store.isModified).toBe(false);
    store.reportTableLayout(1, { columns: [{ name: "msg", width: 420 }, { name: "level" }] });
    expect(store.isModified).toBe(true);
  });

  it("updates the view with the working layout and the revision it loaded", async () => {
    const store = useSavedViewsStore();
    store.enter(2, 5, list(view(1)), 1);
    store.reportTableLayout(1, { columns: [{ name: "level" }, { name: "msg", width: 420 }], sort: [{ column: "level", desc: false }] });
    api.update.mockResolvedValue({ data: view(1, { revision: 2 }, { columns: [{ name: "level" }, { name: "msg", width: 420 }] }) });
    const updated = await store.updateActive();
    expect(api.update).toHaveBeenCalledWith(2, 5, 1, {
      name: "view 1", description: "",
      definition: { version: 1, columns: [{ name: "level" }, { name: "msg", width: 420 }] },
      revision: 1,
    });
    expect(updated?.revision).toBe(2);
    expect(store.activeSeed?.revision).toBe(2);
  });

  it("saves a new view from the parts the user includes", async () => {
    const store = useSavedViewsStore();
    store.enter(2, 5, list(), null);
    store.reportTableLayout(null, { columns: [{ name: "a" }, { name: "b", width: 90 }], sort: [{ column: "a", desc: true }] });
    expect(store.newDefinition({ columns: true, sort: false, displayMode: true }, "table")).toEqual({
      version: 1, columns: [{ name: "a" }, { name: "b", width: 90 }], display_mode: "table",
    });
    const created = view(3, { name: "errors" });
    api.create.mockResolvedValue({ data: created });
    expect(await store.saveAsNew({ name: "errors", description: "", scope: "personal", definition: { version: 1 } })).toEqual(created);
    expect(store.views.map(v => v.id)).toEqual([3]);
  });

  it("saves and updates from the stored working copy when no table is mounted", async () => {
    // A reload in compact mode: the table never mounts to report its layout.
    localStorage.setItem("logchef-viewState-2-5", JSON.stringify({
      viewId: 1, revision: 1, columnOrder: ["msg", "level", "host"],
      columnSizing: { msg: 500 }, columnVisibility: { msg: true, level: true, host: false }, sorting: [],
    }));
    const store = useSavedViewsStore();
    store.enter(2, 5, list(view(1)), 1);
    expect(store.isModified).toBe(true);
    expect(store.newDefinition({ columns: true, sort: false, displayMode: false }, "compact").columns).toEqual([{ name: "msg", width: 500 }, { name: "level" }]);
    api.update.mockResolvedValue({ data: view(1, { revision: 2 }) });
    await store.updateActive();
    expect(api.update.mock.lastCall?.[3].definition.columns).toEqual([{ name: "msg", width: 500 }, { name: "level" }]);
  });

  it("ignores a stored working copy of another revision", () => {
    localStorage.setItem("logchef-viewState-2-5", JSON.stringify({
      viewId: 1, revision: 7, columnOrder: ["msg"], columnSizing: { msg: 500 }, columnVisibility: { msg: true }, sorting: [],
    }));
    const store = useSavedViewsStore();
    store.enter(2, 5, list(view(1)), 1);
    expect(store.isModified).toBe(false);
  });

  it("resets to the view by discarding both working copies", async () => {
    const store = useSavedViewsStore();
    store.enter(2, 5, list(view(1, {}, { display_mode: "compact" })), 1);
    store.setDisplayMode("json");
    localStorage.setItem("logchef-viewState-2-5", "{}");
    const token = store.layoutResetToken;
    store.resetActive();
    await nextTick();
    expect(store.displayMode).toBe("compact");
    expect(localStorage.getItem("logchef-viewState-2-5")).toBeNull();
    expect(localStorage.getItem("logchef-viewDisplayMode-2-5")).toBeNull();
    expect(store.layoutResetToken).toBe(token + 1);
  });
});
