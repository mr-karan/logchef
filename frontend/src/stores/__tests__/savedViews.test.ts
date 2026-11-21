import { beforeEach, describe, expect, it, vi } from "vitest";
import { createPinia, setActivePinia } from "pinia";
import { nextTick } from "vue";
import type { SavedView, SavedViewList } from "@/api/savedViews";
import type { SavedViewDefinition } from "@/lib/savedViewLayout";
import { useSavedViewsStore } from "../savedViews";
import { useActiveSavedViewStore } from "../activeSavedView";

const api = vi.hoisted(() => ({ list: vi.fn(), create: vi.fn(), update: vi.fn(), remove: vi.fn(), setDefault: vi.fn(), clearDefault: vi.fn() }));
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

  it("opens the default view when the browser remembers no choice", () => {
    const store = useSavedViewsStore();
    const withDefault = { views: [view(1), view(2)], default_view_id: 2 };
    store.enter(2, 5, withDefault, null);
    expect(store.activeViewId).toBe(2);
    expect(store.defaultViewId).toBe(2);
    // The URL's view wins over the default.
    store.enter(2, 5, withDefault, 1);
    expect(store.activeViewId).toBe(1);
  });

  it("keeps a remembered pick, including No view, over the default", () => {
    const store = useSavedViewsStore();
    const withDefault = { views: [view(1), view(2)], default_view_id: 2 };
    localStorage.setItem("logchef-activeView-2-5", "1");
    store.enter(2, 5, withDefault, null);
    expect(store.activeViewId).toBe(1);
    localStorage.setItem("logchef-activeView-2-5", "null");
    store.enter(2, 5, withDefault, null);
    expect(store.activeViewId).toBeNull();
  });

  it("falls back to the default when the remembered view no longer exists", () => {
    localStorage.setItem("logchef-activeView-2-5", "7");
    const store = useSavedViewsStore();
    store.enter(2, 5, { views: [view(1), view(2)], default_view_id: 2 }, null);
    expect(store.activeViewId).toBe(2);
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
    const shown = { displayMode: "table" as const, filter: 'level="error"', timeRange: "1h" };
    const parts = { columns: true, sort: false, displayMode: true, filter: false, timeRange: false };
    expect(store.newDefinition(parts, shown)).toEqual({
      version: 1, columns: [{ name: "a" }, { name: "b", width: 90 }], display_mode: "table",
    });
    expect(store.newDefinition({ ...parts, filter: true, timeRange: true }, shown)).toMatchObject({ filter: 'level="error"', time_range: "1h" });
    // A part the Explorer cannot express as a view is left out even when checked.
    expect(store.newDefinition({ ...parts, filter: true, timeRange: true }, { ...shown, filter: null, timeRange: null })).not.toHaveProperty("filter");
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
    expect(store.newDefinition({ columns: true, sort: false, displayMode: false, filter: false, timeRange: false }, { displayMode: "compact", filter: null, timeRange: null }).columns).toEqual([{ name: "msg", width: 500 }, { name: "level" }]);
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

describe("saved views store: manage", () => {
  it("reports no delete for a source the Explorer left while the list reloaded", async () => {
    const store = useSavedViewsStore();
    store.enter(2, 5, list(view(1), view(2)), 1);
    api.remove.mockResolvedValue({ data: { message: "View deleted" } });
    let release: (value: unknown) => void = () => {};
    api.list.mockReturnValue(new Promise((resolve) => { release = resolve; }));
    const removed = store.remove(view(1));
    await vi.waitFor(() => expect(api.list).toHaveBeenCalled());
    store.enter(2, 6, list(view(7)), 7);
    release({ data: list(view(2)) });
    expect(await removed).toBe(false);
    expect(store.activeViewId).toBe(7);
  });
});

describe("saved views store: a share's layout", () => {
  const layout: SavedViewDefinition = { version: 1, columns: [{ name: "host", width: 200 }], sort: [{ column: "host", desc: true }], display_mode: "compact" };
  const storage = () => Object.fromEntries(Object.keys(localStorage).map((key) => [key, localStorage.getItem(key)]));

  it("wins over a remembered view and changes no stored state", () => {
    localStorage.setItem("logchef-activeView-2-5", "1");
    const before = storage();
    const store = useSavedViewsStore();
    expect(store.enter(2, 5, list(view(1), view(2)), null, layout)).toBe("ready");
    expect(store.activeViewId).toBeNull();
    expect(store.sharedLayout).toEqual(layout);
    expect(store.activeSeed).toMatchObject({ viewId: null, definition: layout });
    expect(store.displayMode).toBe("compact");
    store.setDisplayMode("json");
    store.reportTableLayout(null, { columns: [{ name: "host", width: 260 }] });
    expect(store.displayMode).toBe("json");
    expect(storage()).toEqual(before);
    // No view id reaches the URL.
    expect(useActiveSavedViewStore().viewParameter(2, 5)).toBeNull();
  });

  it("wins over the default view", () => {
    const store = useSavedViewsStore();
    store.enter(2, 5, { views: [view(1)], default_view_id: 1 }, null, layout);
    expect(store.activeViewId).toBeNull();
    expect(store.sharedLayout).toEqual(layout);
  });

  it("gives way to a view the URL names beside the share", () => {
    const store = useSavedViewsStore();
    expect(store.enter(2, 5, list(view(1), view(2)), 2, layout)).toBe("ready");
    expect(store.activeViewId).toBe(2);
    expect(store.sharedLayout).toBeNull();
  });

  it("applies when the URL's view is not readable", () => {
    const store = useSavedViewsStore();
    expect(store.enter(2, 5, list(view(1)), 99, layout)).toBe("unavailable");
    expect(store.activeViewId).toBeNull();
    expect(store.sharedLayout).toEqual(layout);
  });

  it("is skipped by a share without a layout, which opens under the remembered view or the default", () => {
    localStorage.setItem("logchef-activeView-2-5", "1");
    const store = useSavedViewsStore();
    store.enter(2, 5, list(view(1), view(2)), null, undefined);
    expect(store.activeViewId).toBe(1);
    expect(store.sharedLayout).toBeNull();
    localStorage.clear();
    store.enter(2, 5, { views: [view(1), view(2)], default_view_id: 2 }, null, undefined);
    expect(store.activeViewId).toBe(2);
  });

  it("ignores a layout that does not parse", () => {
    localStorage.setItem("logchef-activeView-2-5", "1");
    vi.spyOn(console, "warn").mockImplementation(() => undefined);
    const store = useSavedViewsStore();
    store.enter(2, 5, list(view(1)), null, { version: 1, columns: "host" });
    expect(store.sharedLayout).toBeNull();
    expect(store.activeViewId).toBe(1);
  });

  it("ends when the user picks, and the next location resolves again", () => {
    localStorage.setItem("logchef-activeView-2-5", "1");
    const store = useSavedViewsStore();
    store.enter(2, 5, list(view(1), view(2)), null, layout);
    store.pick(null);
    expect(store.sharedLayout).toBeNull();
    expect(store.activeSeed).toBeNull();

    store.enter(2, 5, list(view(1), view(2)), null, layout);
    store.pick(2);
    expect(store.activeViewId).toBe(2);
    expect(store.sharedLayout).toBeNull();

    store.enter(2, 5, list(view(1), view(2)), null, layout);
    store.enter(2, 5, list(view(1), view(2)), null);
    expect(store.sharedLayout).toBeNull();
    expect(store.activeViewId).toBe(2);
  });

  it("keeps edits to the parts the share sets when the table seeds again, in memory only", () => {
    const store = useSavedViewsStore();
    store.enter(2, 5, list(), null, layout);
    const revision = store.activeSeed?.revision;
    store.reportTableLayout(null, { columns: [{ name: "host", width: 420 }, { name: "msg" }], sort: [{ column: "msg", desc: false }] });
    store.setDisplayMode("table");
    expect(store.activeSeed).toEqual({
      viewId: null,
      revision,
      definition: { version: 1, columns: [{ name: "host", width: 420 }, { name: "msg" }], sort: [{ column: "msg", desc: false }], display_mode: "table" },
    });
    expect(store.displayMode).toBe("table");
    expect(Object.keys(localStorage).filter((key) => key.startsWith("logchef-"))).toEqual([]);

    // A share that sets only the display mode keeps the table's own columns.
    store.enter(2, 5, list(), null, { version: 1, display_mode: "json" });
    store.reportTableLayout(null, { columns: [{ name: "msg" }] });
    expect(store.activeSeed?.definition).toEqual({ version: 1, display_mode: "json" });
  });

  it("seeds the table again each time a share opens", () => {
    const store = useSavedViewsStore();
    store.enter(2, 5, list(), null, layout);
    const first = store.activeSeed;
    store.enter(2, 5, list(), null, layout);
    expect(store.activeSeed?.revision).not.toBe(first?.revision);
  });

  it("saves as a new view from the shared layout", () => {
    const store = useSavedViewsStore();
    store.enter(2, 5, list(), null, layout);
    const parts = { columns: true, sort: true, displayMode: true, filter: false, timeRange: false };
    const shown = { displayMode: "compact" as const, filter: null, timeRange: null };
    expect(store.newDefinition(parts, shown)).toEqual(layout);
    store.reportTableLayout(null, { columns: [{ name: "host", width: 260 }], sort: [{ column: "host", desc: false }] });
    expect(store.newDefinition(parts, shown)).toMatchObject({ columns: [{ name: "host", width: 260 }], sort: [{ column: "host", desc: false }] });
  });
});

describe("saved views store: the layout a share carries", () => {
  it("takes the working layout and the shown display mode, never a query part", () => {
    const store = useSavedViewsStore();
    store.enter(2, 5, list(view(1, {}, { filter: "level=error", time_range: "1h", sort: [{ column: "ts", desc: true }] })), 1);
    expect(store.shareLayout("json")).toEqual({
      version: 1, columns: [{ name: "msg", width: 300 }, { name: "level" }], sort: [{ column: "ts", desc: true }], display_mode: "json",
    });
    store.reportTableLayout(1, { columns: [{ name: "level", width: 90 }] });
    expect(store.shareLayout("table")).toEqual({ version: 1, columns: [{ name: "level", width: 90 }], sort: [{ column: "ts", desc: true }], display_mode: "table" });
  });

  it("takes the table's layout with no view", () => {
    const store = useSavedViewsStore();
    store.enter(2, 5, list(view(1)), null);
    store.reportTableLayout(null, { columns: [{ name: "msg" }], sort: [{ column: "ts", desc: false }] });
    expect(store.shareLayout("table")).toEqual({ version: 1, columns: [{ name: "msg" }], sort: [{ column: "ts", desc: false }], display_mode: "table" });
  });
});

describe("saved views store: a saved query's layout", () => {
  it("takes the working columns and sort, never the display mode or a query part", () => {
    const store = useSavedViewsStore();
    store.enter(2, 5, list(view(1, {}, { filter: "level=error", time_range: "1h", display_mode: "json", sort: [{ column: "ts", desc: true }] })), 1);
    expect(store.savedQueryLayout()).toEqual({ version: 1, columns: [{ name: "msg", width: 300 }, { name: "level" }], sort: [{ column: "ts", desc: true }] });
    store.reportTableLayout(1, { columns: [{ name: "level", width: 90 }] });
    expect(store.savedQueryLayout()).toEqual({ version: 1, columns: [{ name: "level", width: 90 }], sort: [{ column: "ts", desc: true }] });
  });

  it("is null when the table has no columns or sort", () => {
    const store = useSavedViewsStore();
    store.enter(2, 5, list(), null);
    expect(store.savedQueryLayout()).toBeNull();
  });

  it("opens in memory over a remembered view and says it came from a saved query", () => {
    localStorage.setItem("logchef-activeView-2-5", "1");
    const store = useSavedViewsStore();
    const layout = { version: 1, columns: [{ name: "host", width: 200 }], sort: [{ column: "host", desc: true }] };
    expect(store.enter(2, 5, list(view(1)), null, layout, "savedQuery")).toBe("ready");
    expect(store.activeViewId).toBeNull();
    expect(store.activeSeed).toMatchObject({ viewId: null, definition: layout });
    expect(store.sharedLayoutOrigin).toBe("savedQuery");
    expect(localStorage.getItem("logchef-activeView-2-5")).toBe("1");
    // A query saved before layouts were stored opens under the remembered view.
    store.enter(2, 5, list(view(1)), null, undefined, "savedQuery");
    expect(store.activeViewId).toBe(1);
    store.enter(2, 5, list(view(1)), null, layout);
    expect(store.sharedLayoutOrigin).toBe("share");
  });
});
