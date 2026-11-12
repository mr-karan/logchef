import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { createApp, nextTick, ref } from "vue";
import { createPinia } from "pinia";
import { i18n } from "@/i18n";
import type { User } from "@/types";
import type { Collection, CollectionItem, CollectionLinkKind } from "@/api/collections";
import type { SavedQuery } from "@/api/savedQueries";

// Only the HTTP API and the auth context are faked. The collections store,
// the policy module and the pane render for real, and the fake API records
// the link writes the pane sends.
const mockUser = ref<User | null>(null);
const server = {
  callerRole: "member" as Collection["caller_role"],
  personal: false,
  items: [] as CollectionItem[],
  addRequests: [] as { collection: number; saved_query_id: number; kind?: CollectionLinkKind }[],
  updateRequests: [] as { collection: number; query: number; kind: CollectionLinkKind }[],
  removeRequests: [] as { collection: number; query: number }[],
  addFails: false,
};

const ok = <T>(data: T) => Promise.resolve({ status: "success" as const, data });

function collection(id: number, name: string, personal = false): Collection {
  return {
    id, name, is_personal: personal, created_by: 99, caller_role: server.callerRole,
    member_count: 1, team_count: 0, item_count: server.items.length, created_at: "", updated_at: "",
  };
}

function query(id: number, name: string, createdBy: number | null): SavedQuery {
  return {
    id, source_id: 1, name, description: "", query_language: "logchefql", editor_mode: "builder",
    query_content: "{}", created_by: createdBy, created_at: "", updated_at: "", source_name: "logs",
  };
}

function item(q: SavedQuery, kind: CollectionLinkKind): CollectionItem {
  return { collection_id: 1, kind, sort_order: 0, added_by: 99, item_added_at: "", query: q, runnable: true };
}

const mine = query(1, "My query", 10);
const theirs = query(2, "Their query", 99);
const unpinned = query(3, "Unpinned mine", 10);
const unpinnedTheirs = query(4, "Unpinned theirs", 99);

vi.mock("@/api/collections", () => ({
  collectionsApi: {
    list: () => ok([collection(1, "Shared", server.personal), collection(2, "Target"), collection(3, "My Collection", true)]),
    listItems: () => ok([...server.items]),
    listMembers: () => ok([]),
    listTeams: () => ok([]),
    addItem: (id: number, payload: { saved_query_id: number; kind?: CollectionLinkKind }) => {
      server.addRequests.push({ collection: id, ...payload });
      if (server.addFails) return Promise.reject({ status: "error", message: "forbidden", error_type: "AuthorizationError" });
      return ok({ collection_id: id, saved_query_id: payload.saved_query_id, kind: payload.kind ?? "pin", added_by: 10, sort_order: 0, created_at: "" });
    },
    updateItem: (id: number, queryId: number, payload: { kind: CollectionLinkKind }) => {
      server.updateRequests.push({ collection: id, query: queryId, kind: payload.kind });
      server.items = server.items.map((i) => (i.query.id === queryId ? { ...i, kind: payload.kind } : i));
      return ok({ collection_id: id, saved_query_id: queryId, kind: payload.kind, added_by: 10, sort_order: 0, created_at: "" });
    },
    removeItem: (id: number, queryId: number) => {
      server.removeRequests.push({ collection: id, query: queryId });
      server.items = server.items.filter((i) => i.query.id !== queryId);
      return ok({ message: "ok" });
    },
  },
}));
vi.mock("@/api/savedQueries", () => ({
  savedQueriesApi: {
    list: () => ok([mine, theirs, unpinned, unpinnedTheirs]),
  },
}));
vi.mock("@/stores/auth", () => ({
  useAuthStore: () => ({
    get user() { return mockUser.value; },
    get isAuthenticated() { return mockUser.value !== null; },
  }),
}));
vi.mock("@/stores/teams", () => ({
  useTeamsStore: () => ({ userTeams: [], adminTeams: [], loadUserTeams: async () => {}, loadAdminTeams: async () => {} }),
}));
vi.mock("vue-router", async (importOriginal) => ({
  ...(await importOriginal<typeof import("vue-router")>()),
  useRouter: () => ({ push: vi.fn() }),
}));

const { default: CollectionDetailPane } = await import("../CollectionDetailPane.vue");

function user(role: User["role"]): User {
  return { id: "10", email: "user@example.com", full_name: "User", role, status: "active", account_type: "human", created_at: "", updated_at: "" };
}

let host: HTMLElement | null = null;
let unmount: (() => void) | null = null;

async function flush() {
  for (let i = 0; i < 6; i++) {
    await new Promise((resolve) => setTimeout(resolve, 0));
    await nextTick();
  }
}

async function render(): Promise<HTMLElement> {
  host = document.createElement("div");
  document.body.appendChild(host);
  const app = createApp(CollectionDetailPane, { collectionId: 1 });
  app.use(createPinia());
  app.use(i18n);
  app.mount(host);
  unmount = () => app.unmount();
  await flush();
  return host;
}

function rowFor(root: HTMLElement, name: string): HTMLElement {
  const row = [...root.querySelectorAll<HTMLElement>("tbody tr")].find((r) => r.textContent?.includes(name));
  if (!row) throw new Error(`row "${name}" not found`);
  return row;
}

function kindOf(root: HTMLElement, name: string): string | null {
  return rowFor(root, name).querySelector("[data-link-kind]")?.getAttribute("data-link-kind") ?? null;
}

async function openRowMenu(root: HTMLElement, name: string): Promise<string[]> {
  const trigger = rowFor(root, name).querySelector<HTMLButtonElement>("button[aria-label='Actions']");
  if (!trigger) throw new Error(`actions menu for "${name}" not found`);
  trigger.click();
  await flush();
  return [...document.body.querySelectorAll<HTMLElement>("[role=menuitem]")].map((m) => m.textContent?.trim() ?? "");
}

async function clickMenuItem(text: string) {
  const entry = [...document.body.querySelectorAll<HTMLElement>("[role=menuitem]")].find((m) => m.textContent?.trim() === text);
  if (!entry) throw new Error(`menu item "${text}" not found`);
  entry.click();
  await flush();
}

function findButton(text: string): HTMLButtonElement | undefined {
  return [...document.body.querySelectorAll("button")].find((b) => (b.textContent ?? "").trim() === text);
}

async function click(text: string) {
  const button = findButton(text);
  if (!button) throw new Error(`button "${text}" not found`);
  button.click();
  await flush();
}

function addDialog(): HTMLElement {
  const dialog = [...document.body.querySelectorAll<HTMLElement>("[role=dialog]")].find((d) => d.textContent?.includes("Add query"));
  if (!dialog) throw new Error("add dialog not open");
  return dialog;
}

async function addFromDialog(name: string) {
  const row = [...addDialog().querySelectorAll<HTMLElement>("li")].find((li) => li.textContent?.includes(name));
  const button = row?.querySelector<HTMLButtonElement>("button");
  if (!button) throw new Error(`add button for "${name}" not found`);
  button.click();
  await flush();
}

async function toggleEditorsCanEdit() {
  const box = addDialog().querySelector<HTMLButtonElement>("#add_editors_can_edit");
  if (!box) throw new Error("editors toggle not shown");
  box.click();
  await flush();
}

beforeEach(() => {
  mockUser.value = user("member");
  server.callerRole = "member";
  server.personal = false;
  server.items = [item(mine, "pin"), item(theirs, "managed")];
  server.addRequests = [];
  server.updateRequests = [];
  server.removeRequests = [];
  server.addFails = false;
});

afterEach(() => {
  unmount?.();
  host?.remove();
  host = null;
  unmount = null;
  document.body.innerHTML = "";
});

describe("CollectionDetailPane link kinds", () => {
  it("shows each link's kind in a shared collection", async () => {
    const root = await render();
    expect(kindOf(root, "My query")).toBe("pin");
    expect(kindOf(root, "Their query")).toBe("managed");
  });

  it("hides kinds in a personal collection", async () => {
    server.personal = true;
    server.items = [item(mine, "pin")];
    const root = await render();
    expect(kindOf(root, "My query")).toBeNull();
  });

  it("offers move, conversion and removal on the caller's own pin", async () => {
    const root = await render();
    expect(await openRowMenu(root, "My query")).toEqual(["Move", "Let collection editors edit", "Remove"]);
  });

  it("offers nothing on someone else's managed link", async () => {
    const root = await render();
    expect(rowFor(root, "Their query").querySelector("button[aria-label='Actions']")).toBeNull();
  });

  it("lets a global admin convert and remove any link", async () => {
    mockUser.value = user("admin");
    const root = await render();
    expect(await openRowMenu(root, "Their query")).toEqual(["Move", "Stop delegating edit", "Remove"]);
    await clickMenuItem("Stop delegating edit");
    expect(server.updateRequests).toEqual([{ collection: 1, query: 2, kind: "pin" }]);
  });

  it("converts a pin through PUT", async () => {
    const root = await render();
    await openRowMenu(root, "My query");
    await clickMenuItem("Let collection editors edit");
    expect(server.updateRequests).toEqual([{ collection: 1, query: 1, kind: "managed" }]);
    expect(kindOf(root, "My query")).toBe("managed");
  });

  it("adds the creator's query as managed by default and as a pin with the toggle off", async () => {
    await render();
    await click("Add query");
    await addFromDialog("Unpinned mine");
    await addFromDialog("Unpinned theirs");
    await toggleEditorsCanEdit();
    server.items = [item(mine, "pin"), item(theirs, "managed")];
    await addFromDialog("Unpinned mine");
    expect(server.addRequests).toEqual([
      { collection: 1, saved_query_id: 3, kind: "managed" },
      { collection: 1, saved_query_id: 4, kind: "pin" },
      { collection: 1, saved_query_id: 3, kind: "pin" },
    ]);
  });

  it("shows no toggle to a caller who created none of the queries", async () => {
    mockUser.value = { ...user("member"), id: "55" };
    await render();
    await click("Add query");
    expect(addDialog().querySelector("#add_editors_can_edit")).toBeNull();
    await addFromDialog("Unpinned mine");
    expect(server.addRequests).toEqual([{ collection: 1, saved_query_id: 3, kind: "pin" }]);
  });

  it("moves with the source link's kind and removes only after the add succeeded", async () => {
    mockUser.value = user("admin");
    const root = await render();
    await openRowMenu(root, "Their query");
    await clickMenuItem("Move");
    const trigger = document.body.querySelector<HTMLButtonElement>("[role=dialog] [role=combobox]");
    if (!trigger) throw new Error("move picker not found");
    trigger.click();
    await flush();
    const target = [...document.body.querySelectorAll<HTMLButtonElement>("button[data-item]")].find((b) => b.textContent?.includes("Target"));
    if (!target) throw new Error("move target not found");
    target.click();
    await flush();
    server.addFails = true;
    await click("Move");
    expect(server.addRequests).toEqual([{ collection: 2, saved_query_id: 2, kind: "managed" }]);
    expect(server.removeRequests).toEqual([]);
    server.addFails = false;
    await click("Move");
    expect(server.addRequests).toHaveLength(2);
    expect(server.removeRequests).toEqual([{ collection: 1, query: 2 }]);
  });
});
