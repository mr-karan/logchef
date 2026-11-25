import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { computed, createApp, nextTick, reactive } from "vue";
import { i18n } from "@/i18n";
import type { SavedQuery } from "@/api/savedQueries";

const stubs = await vi.hoisted(async () => {
  const { defineComponent, h } = await import("vue");
  const passthrough = (tag: string) => defineComponent({
    inheritAttrs: false,
    setup(_, { attrs, slots }) {
      return () => h(tag, attrs, slots.default?.());
    },
  });
  return { passthrough };
});

const mockRouterPush = vi.hoisted(() => vi.fn());
const mockList = vi.hoisted(() => vi.fn(() => Promise.resolve({ success: true, data: [] })));
const state = reactive({
  queries: [] as SavedQuery[],
  selectedQueryId: null as string | null,
  activeSavedQueryName: "",
  hasDiverged: false,
  canSave: true,
});

vi.mock("vue-router", () => ({
  useRouter: () => ({
    push: mockRouterPush,
    resolve: (path: string) => ({ href: path }),
  }),
}));
vi.mock("@/components/ui/dropdown-menu", () => ({
  DropdownMenu: stubs.passthrough("div"),
  DropdownMenuContent: stubs.passthrough("div"),
  DropdownMenuItem: stubs.passthrough("button"),
  DropdownMenuLabel: stubs.passthrough("div"),
  DropdownMenuSeparator: stubs.passthrough("hr"),
  DropdownMenuTrigger: stubs.passthrough("div"),
}));
vi.mock("@/components/ui/button", () => ({ Button: stubs.passthrough("button") }));
vi.mock("@/stores/savedQueries", () => ({
  useSavedQueriesStore: () => ({
    get queries() { return state.queries; },
    list: mockList,
  }),
}));
vi.mock("@/stores/explore", () => ({
  useExploreStore: () => ({
    get selectedQueryId() { return state.selectedQueryId; },
    get activeSavedQueryName() { return state.activeSavedQueryName; },
    get hasDivergedFromSavedQuery() { return state.hasDiverged; },
  }),
}));
vi.mock("@/stores/auth", () => ({
  useAuthStore: () => ({ isAuthenticated: true }),
}));
vi.mock("@/composables/useSavedQueries", () => ({
  useSavedQueries: () => ({
    isEditingExistingQuery: computed(() => !!state.selectedQueryId),
    canSaveQuery: computed(() => state.canSave),
  }),
}));
vi.mock("@/composables/useToast", () => ({ useToast: () => ({ toast: vi.fn() }) }));

import SavedQueriesDropdown from "../SavedQueriesDropdown.vue";

function query(id: number, name: string): SavedQuery {
  return {
    id,
    source_id: 7,
    name,
    description: "",
    query_language: "logchefql",
    editor_mode: "builder",
    query_content: "{}",
    created_at: "",
    updated_at: "",
  };
}

describe("SavedQueriesDropdown", () => {
  let host: HTMLDivElement;

  beforeEach(() => {
    mockRouterPush.mockReset();
    mockList.mockReset().mockResolvedValue({ success: true, data: [] });
    state.queries = [query(1, "5xx spikes"), query(2, "Auth timeouts")];
    state.selectedQueryId = null;
    state.activeSavedQueryName = "";
    state.hasDiverged = false;
    state.canSave = true;
    host = document.createElement("div");
    document.body.appendChild(host);
  });

  afterEach(() => {
    host.remove();
  });

  it("shows direct save action, flat query list, and Open Library link", async () => {
    const selected: SavedQuery[] = [];
    let saveClicks = 0;
    const app = createApp(SavedQueriesDropdown, {
      selectedTeamId: 1,
      selectedSourceId: 7,
      onSave: () => { saveClicks += 1; },
      "onSelect-saved-query": (q: SavedQuery) => selected.push(q),
    });
    app.use(i18n);
    app.mount(host);
    await nextTick();

    expect(host.textContent).toContain("Save query…");
    expect(host.textContent).toContain("Saved queries (2)");
    expect(host.textContent).toContain("5xx spikes");
    expect(host.textContent).toContain("Auth timeouts");
    expect(host.textContent).toContain("Open Library");

    const buttons = [...host.querySelectorAll("button")];
    buttons.find((b) => b.textContent?.includes("Save query…"))?.click();
    buttons.find((b) => b.textContent?.includes("5xx spikes"))?.click();
    buttons.find((b) => b.textContent?.includes("Open Library"))?.click();

    expect(saveClicks).toBe(1);
    expect(selected.map((q) => q.id)).toEqual([1]);
    expect(mockRouterPush).toHaveBeenCalledWith({ path: "/logs/library", query: {} });
    app.unmount();
  });
});
