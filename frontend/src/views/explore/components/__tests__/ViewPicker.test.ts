import { createApp, h, nextTick } from "vue";
import { createPinia, setActivePinia } from "pinia";
import { afterEach, expect, it, vi } from "vitest";
import { i18n } from "@/i18n";
import type { SavedView } from "@/api/savedViews";
import { useSavedViewsStore } from "@/stores/savedViews";
import { useExploreStore } from "@/stores/explore";
import { relativeWindow } from "@/utils/timeWindow";
import ViewPicker from "../ViewPicker.vue";

const api = vi.hoisted(() => ({ list: vi.fn(), create: vi.fn(), update: vi.fn() }));
vi.mock("@/api/savedViews", () => ({ savedViewsApi: api }));

// The result on screen. Only its columns, rows and aggregation matter to the picker.
type Results = { columns: { name: string; type: string }[]; rows: Record<string, unknown>[]; aggregation: unknown };
const runs = vi.hoisted(() => ({ state: null as unknown as { results: Results | null } }));
vi.mock("@/stores/exploreRuns", async () => {
  const { reactive } = await import("vue");
  runs.state = reactive({ results: null as Results | null });
  return { useExploreRuns: () => runs.state };
});

let unmount: (() => void) | null = null;

afterEach(() => {
  runs.state.results = null;
  unmount?.();
  unmount = null;
  document.body.innerHTML = "";
  localStorage.clear();
});

function view(id: number, scope: SavedView["scope"], name: string, can_edit = true): SavedView {
  return {
    id, source_id: 5, scope, name, description: "", definition: { version: 1, columns: [{ name: "msg" }] },
    is_default: false, managed: false, revision: 1, created_at: "", updated_at: "", can_edit, can_set_default: can_edit,
  };
}

async function mount(views: SavedView[], active: number | null, defaultViewId: number | null = null, sharedLayout?: unknown) {
  const pinia = createPinia();
  setActivePinia(pinia);
  const store = useSavedViewsStore();
  store.enter(2, 5, { views, default_view_id: defaultViewId }, active, sharedLayout);
  const selected: (number | null)[] = [];
  const host = document.createElement("div");
  document.body.append(host);
  const app = createApp({ render: () => h(ViewPicker, { displayMode: "table", onSelect: (id: number | null) => selected.push(id) }) }).use(pinia).use(i18n);
  app.mount(host);
  unmount = () => app.unmount();
  await nextTick();
  return { host, store, selected };
}

async function openMenu(host: HTMLElement) {
  const trigger = host.querySelector<HTMLElement>("[data-testid=view-picker]")!;
  trigger.dispatchEvent(new PointerEvent("pointerdown", { bubbles: true, button: 0, pointerType: "mouse" }));
  trigger.dispatchEvent(new KeyboardEvent("keydown", { key: "Enter", bubbles: true }));
  await nextTick();
  await nextTick();
  return [...document.body.querySelectorAll<HTMLElement>("[role=menuitem]")];
}

it("names the active view, or No view", async () => {
  const none = await mount([view(1, "personal", "errors")], null);
  expect(none.host.textContent).toContain("No view");
  unmount?.();
  const active = await mount([view(1, "personal", "errors")], 1);
  expect(active.host.textContent).toContain("errors");
  expect(active.host.querySelector("[data-testid=view-modified]")).toBeNull();
});

it("marks a modified view and offers Update view only when the caller can edit it", async () => {
  const { host, store } = await mount([view(1, "team", "team view", false)], 1);
  store.reportTableLayout(1, { columns: [{ name: "level" }] });
  await nextTick();
  expect(host.querySelector("[data-testid=view-modified]")).not.toBeNull();
  const labels = (await openMenu(host)).map(item => item.textContent?.trim());
  expect(labels).toContain("Reset to view");
  expect(labels).not.toContain("Update view");
});

it("lists the views in scope groups and emits the pick", async () => {
  const { host, selected } = await mount([view(1, "personal", "mine"), view(2, "team", "ours"), view(3, "source", "everyone")], 1);
  const items = await openMenu(host);
  const labels = items.map(item => item.textContent?.trim());
  expect(labels.slice(0, 3)).toEqual(["mine", "ours", "everyone"]);
  expect(labels).toEqual(expect.arrayContaining(["No view", "Save as new view", "Reset to view"]));
  // An unmodified view has nothing to update.
  expect(labels).not.toContain("Update view");
  expect(document.body.textContent).toContain("Personal");
  expect(document.body.textContent).toContain("Team");
  items[1].click();
  await nextTick();
  expect(selected).toEqual([2]);
});

it("marks the default view", async () => {
  const { host } = await mount([view(1, "personal", "mine"), view(2, "team", "ours")], null, 2);
  const items = await openMenu(host);
  expect(items[0].querySelector("[data-testid=view-default]")).toBeNull();
  expect(items[1].querySelector("[data-testid=view-default]")?.textContent).toBe("Default");
});

it("saves the filter and time range only when checked, and only what a view can hold", async () => {
  const { host } = await mount([], null);
  const explore = useExploreStore();
  const save = async (check: string[]) => {
    const items = await openMenu(host);
    items.find(item => item.textContent?.trim() === "Save as new view")!.click();
    await nextTick();
    await nextTick();
    const name = document.body.querySelector<HTMLInputElement>("#view-name")!;
    name.value = "errors";
    name.dispatchEvent(new Event("input"));
    for (const id of check) document.body.querySelector<HTMLElement>(`[data-testid=${id}]`)!.click();
    await nextTick();
    document.body.querySelector<HTMLFormElement>("[role=dialog] form")!.dispatchEvent(new Event("submit", { cancelable: true }));
    await vi.waitFor(() => expect(api.create).toHaveBeenCalled());
    const definition = api.create.mock.lastCall?.[2].definition;
    api.create.mockClear();
    return definition;
  };
  api.create.mockResolvedValue({ data: view(9, "personal", "errors") });

  explore.setLogchefqlCode('level="error"');
  explore.setWindow(relativeWindow("1h")!);
  const unchecked = await save([]);
  expect(unchecked).not.toHaveProperty("filter");
  expect(unchecked).not.toHaveProperty("time_range");

  const checked = await save(["view-include-filter", "view-include-time-range"]);
  expect(checked).toMatchObject({ filter: 'level="error"', time_range: "1h" });

  // An empty editor has no filter to save.
  explore.setLogchefqlCode("");
  await openMenu(host);
  const items = [...document.body.querySelectorAll<HTMLElement>("[role=menuitem]")];
  items.find(item => item.textContent?.trim() === "Save as new view")!.click();
  await nextTick();
  await nextTick();
  expect(document.body.querySelector("[data-testid=view-include-filter]")?.hasAttribute("disabled")).toBe(true);
  expect(document.body.querySelector("[data-testid=view-include-time-range]")?.hasAttribute("disabled")).toBe(false);
});

it("warns when a result with rows lacks a view column and names it", async () => {
  const { host } = await mount([view(1, "personal", "errors")], 1);
  const mark = () => host.querySelector<HTMLElement>("[data-testid=view-missing-columns]");
  const result = (names: string[], rows: number, aggregation: unknown = null): Results => ({
    columns: names.map(name => ({ name, type: "String" })), rows: Array.from({ length: rows }, () => ({})), aggregation,
  });

  runs.state.results = result(["timestamp", "msg"], 3);
  await nextTick();
  expect(mark()).toBeNull();

  runs.state.results = result(["timestamp", "level"], 3);
  await nextTick();
  expect(mark()?.getAttribute("title")).toBe("Not in this result: msg");

  // No rows, or an aggregation with its own columns, shows no mark.
  runs.state.results = result(["timestamp", "level"], 0);
  await nextTick();
  expect(mark()).toBeNull();
  runs.state.results = result(["count"], 3, { kind: "count" });
  await nextTick();
  expect(mark()).toBeNull();
});

it("names a share's layout and offers No view and Save as new view", async () => {
  const { host, selected } = await mount([view(1, "personal", "errors")], null, null, { version: 1, columns: [{ name: "host" }] });
  expect(host.textContent).toContain("Shared layout");
  const items = await openMenu(host);
  const labels = items.map(item => item.textContent?.trim());
  expect(labels).toEqual(expect.arrayContaining(["No view", "Save as new view"]));
  expect(labels).not.toContain("Reset to view");
  items.find(item => item.textContent?.trim() === "No view")!.click();
  await nextTick();
  expect(selected).toEqual([null]);
});
