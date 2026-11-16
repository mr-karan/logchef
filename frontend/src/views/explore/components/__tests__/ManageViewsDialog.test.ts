import { createApp, h, nextTick, ref } from "vue";
import { createPinia, setActivePinia } from "pinia";
import { afterEach, beforeEach, expect, it, vi } from "vitest";
import { i18n } from "@/i18n";
import type { SavedView, SavedViewList } from "@/api/savedViews";
import { useSavedViewsStore } from "@/stores/savedViews";
import ManageViewsDialog from "../ManageViewsDialog.vue";

const api = vi.hoisted(() => ({ list: vi.fn(), create: vi.fn(), update: vi.fn(), remove: vi.fn(), setDefault: vi.fn(), clearDefault: vi.fn() }));
vi.mock("@/api/savedViews", () => ({ savedViewsApi: api }));

let unmount: (() => void) | null = null;

beforeEach(() => {
  vi.clearAllMocks();
});

afterEach(() => {
  unmount?.();
  unmount = null;
  document.body.innerHTML = "";
  localStorage.clear();
});

function view(id: number, name: string, overrides: Partial<SavedView> = {}): SavedView {
  return {
    id, source_id: 5, scope: "team", name, description: "",
    definition: { version: 1, columns: [{ name: "timestamp" }, { name: "msg", width: 300 }, { name: "level" }], sort: [{ column: "level", desc: false }] },
    is_default: false, managed: false, revision: 1, created_at: "", updated_at: "", can_edit: true, can_set_default: true, ...overrides,
  };
}

async function mount(views: SavedView[], { active = null, defaultViewId = null, resultColumns = null }: { active?: number | null; defaultViewId?: number | null; resultColumns?: string[] | null } = {}) {
  const pinia = createPinia();
  setActivePinia(pinia);
  const list: SavedViewList = { views, default_view_id: defaultViewId };
  api.list.mockResolvedValue({ data: list });
  const store = useSavedViewsStore();
  store.enter(2, 5, list, active);
  const selected: (number | null)[] = [];
  const open = ref(true);
  const host = document.createElement("div");
  document.body.append(host);
  const app = createApp({
    render: () => h(ManageViewsDialog, {
      open: open.value,
      "onUpdate:open": (value: boolean) => { open.value = value; },
      resultColumns,
      onSelect: (id: number | null) => selected.push(id),
    }),
  }).use(pinia).use(i18n);
  app.mount(host);
  unmount = () => app.unmount();
  await vi.waitFor(() => expect(document.body.querySelector("[data-testid=manage-view-" + (views[0]?.id ?? 0) + "]") ?? views.length === 0).toBeTruthy());
  return { store, selected };
}

const row = (id: number) => document.body.querySelector<HTMLElement>(`[data-testid=manage-view-${id}]`)!;
const actionsMenu = async (id: number) => {
  const trigger = row(id).querySelector<HTMLElement>("[aria-label=Actions]")!;
  trigger.dispatchEvent(new PointerEvent("pointerdown", { bubbles: true, button: 0, pointerType: "mouse" }));
  trigger.dispatchEvent(new KeyboardEvent("keydown", { key: "Enter", bubbles: true }));
  await nextTick();
  await nextTick();
  return [...document.body.querySelectorAll<HTMLElement>("[role=menuitem]")];
};

it("hides the edit actions of a view the caller cannot edit or that provisioning manages", async () => {
  await mount([
    view(1, "editable"),
    view(2, "read only", { can_edit: false, can_set_default: false }),
    view(3, "managed", { scope: "source", managed: true, can_edit: false, can_set_default: false }),
  ]);
  expect(row(1).querySelector("[data-testid=manage-view-rename]")).not.toBeNull();
  expect(row(1).querySelector("[data-testid=manage-view-default-toggle]")).not.toBeNull();
  for (const id of [2, 3]) {
    expect(row(id).querySelector("[data-testid=manage-view-rename]")).toBeNull();
    expect(row(id).querySelector("[data-testid=manage-view-default-toggle]")).toBeNull();
    const labels = (await actionsMenu(id)).map(item => item.textContent?.trim());
    expect(labels).toEqual(["Duplicate"]);
    document.body.dispatchEvent(new KeyboardEvent("keydown", { key: "Escape", bubbles: true }));
    await nextTick();
  }
  expect(row(3).textContent).toContain("Managed");
});

it("hides the edit actions of a managed view even when the server says the caller can edit", async () => {
  await mount([view(1, "managed", { managed: true })]);
  expect(row(1).querySelector("[data-testid=manage-view-rename]")).toBeNull();
  expect(row(1).querySelector("[data-testid=manage-view-default-toggle]")).toBeNull();
  expect((await actionsMenu(1)).map(item => item.textContent?.trim())).toEqual(["Duplicate"]);
});

it("lists the views by name A-Z and marks the default", async () => {
  await mount([view(1, "zeta"), view(2, "alpha")], { defaultViewId: 1 });
  const names = [...document.body.querySelectorAll("tr[data-testid^=manage-view-] td:first-child span.font-medium")].map(el => el.textContent);
  expect(names).toEqual(["alpha", "zeta"]);
  expect(row(1).querySelector("[data-testid=manage-view-default]")).not.toBeNull();
  expect(row(2).querySelector("[data-testid=manage-view-default]")).toBeNull();
});

it("renames a view at the revision it loaded", async () => {
  const { store } = await mount([view(1, "errors", { revision: 4 })]);
  row(1).querySelector<HTMLElement>("[data-testid=manage-view-rename]")!.click();
  await nextTick();
  const input = row(1).querySelector<HTMLInputElement>("input")!;
  input.value = "server errors";
  input.dispatchEvent(new Event("input"));
  api.update.mockResolvedValue({ data: view(1, "server errors", { revision: 5 }) });
  await nextTick();
  row(1).querySelector("form")!.dispatchEvent(new Event("submit", { cancelable: true }));
  await vi.waitFor(() => expect(store.views[0].name).toBe("server errors"));
  expect(api.update.mock.lastCall?.slice(0, 3)).toEqual([2, 5, 1]);
  expect(api.update.mock.lastCall?.[3]).toMatchObject({ name: "server errors", revision: 4, definition: view(1, "").definition });
});

it("duplicates a view the caller cannot edit as a personal copy", async () => {
  await mount([view(2, "ours", { can_edit: false, can_set_default: false })]);
  api.create.mockResolvedValue({ data: view(9, "ours (copy)", { scope: "personal" }) });
  (await actionsMenu(2)).find(item => item.textContent?.trim() === "Duplicate")!.click();
  await vi.waitFor(() => expect(api.create).toHaveBeenCalled());
  expect(api.create.mock.lastCall?.[2]).toMatchObject({ name: "ours (copy)", scope: "personal", definition: view(2, "").definition });
});

it("sets and clears the default, then reads the server's default back", async () => {
  const { store } = await mount([view(1, "errors")]);
  api.setDefault.mockResolvedValue({ data: view(1, "errors", { is_default: true }) });
  api.list.mockResolvedValue({ data: { views: [view(1, "errors", { is_default: true })], default_view_id: 1 } });
  row(1).querySelector<HTMLElement>("[data-testid=manage-view-default-toggle]")!.click();
  await vi.waitFor(() => expect(store.defaultViewId).toBe(1));
  expect(api.setDefault).toHaveBeenCalledWith(2, 5, 1);

  api.list.mockResolvedValue({ data: { views: [view(1, "errors")], default_view_id: null } });
  await nextTick();
  row(1).querySelector<HTMLElement>("[data-testid=manage-view-default-toggle]")!.click();
  await vi.waitFor(() => expect(store.defaultViewId).toBeNull());
  expect(api.clearDefault).toHaveBeenCalledWith(2, 5, 1);
});

it("deletes after confirmation and leaves the view when it was active", async () => {
  const { store, selected } = await mount([view(1, "errors"), view(2, "wide")], { active: 1 });
  api.remove.mockResolvedValue({ data: { message: "View deleted" } });
  api.list.mockResolvedValue({ data: { views: [view(2, "wide")], default_view_id: null } });
  (await actionsMenu(1)).find(item => item.textContent?.trim() === "Delete")!.click();
  await nextTick();
  expect(api.remove).not.toHaveBeenCalled();
  const confirm = [...document.body.querySelectorAll<HTMLElement>("[role=alertdialog] button")].find(button => button.textContent?.trim() === "Delete")!;
  confirm.click();
  await vi.waitFor(() => expect(selected).toEqual([null]));
  expect(api.remove).toHaveBeenCalledWith(2, 5, 1);
  expect(store.views.map(v => v.id)).toEqual([2]);
});

it("names the columns a result lacks and removes them from the view", async () => {
  const { store } = await mount([view(1, "errors"), view(2, "editable elsewhere", { can_edit: false })], { resultColumns: ["timestamp", "msg"] });
  expect(row(1).querySelector("[data-testid=manage-view-missing]")?.textContent).toContain("Not in this result: level");
  api.update.mockResolvedValue({ data: view(1, "errors", { revision: 2 }) });
  (await actionsMenu(1)).find(item => item.textContent?.trim() === "Remove missing columns")!.click();
  await vi.waitFor(() => expect(api.update).toHaveBeenCalled());
  expect(api.update.mock.lastCall?.[3].definition).toEqual({ version: 1, columns: [{ name: "timestamp" }, { name: "msg", width: 300 }] });
  expect(store.views.find(v => v.id === 1)?.revision).toBe(2);
});
