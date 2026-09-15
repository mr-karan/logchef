import { beforeEach, describe, expect, it, vi } from "vitest";

const mocks = vi.hoisted(() => ({
  route: { query: { id: "41" } },
  router: { replace: vi.fn(), push: vi.fn() },
  exploreStore: {
    activeMode: "logchefql",
    logchefqlCode: 'level="{{ level }}"',
    nativeQuery: "",
    setActiveSavedQueryName: vi.fn(),
    setSelectedQueryId: vi.fn(),
    resetQueryToDefaults: vi.fn(),
  },
  savedQueriesStore: {
    data: { queries: [] as Array<{ id: number; name: string; source_id: number }> },
    create: vi.fn(),
    update: vi.fn(),
    list: vi.fn(() => Promise.resolve({ success: true, data: [] })),
  },
  collectionsStore: { addItem: vi.fn() },
  contextStore: { teamId: 1, sourceId: 7 },
  toast: vi.fn(),
  canSaveQuery: true,
  canEditSavedQuery: true,
  isAnyTeamCollectionMutator: false,
  savedQueryGet: vi.fn(),
}));

vi.mock("vue-router", () => ({
  useRoute: () => mocks.route,
  useRouter: () => mocks.router,
}));
vi.mock("@/stores/explore", () => ({ useExploreStore: () => mocks.exploreStore }));
vi.mock("@/stores/savedQueries", () => ({ useSavedQueriesStore: () => mocks.savedQueriesStore }));
vi.mock("@/stores/collections", () => ({ useCollectionsStore: () => mocks.collectionsStore }));
vi.mock("@/stores/context", () => ({ useContextStore: () => mocks.contextStore }));
vi.mock("@/stores/variables", () => ({
  useVariableStore: () => ({ setAllVariable: vi.fn() }),
}));
vi.mock("@/composables/useTeamPermissions", () => ({
  useTeamPermissions: () => ({
    canSaveQuery: mocks.canSaveQuery,
    canEditSavedQuery: mocks.canEditSavedQuery,
    isAnyTeamCollectionMutator: mocks.isAnyTeamCollectionMutator,
  }),
}));
vi.mock("@/composables/useToast", () => ({ useToast: () => ({ toast: mocks.toast }) }));
vi.mock("@/api/savedQueries", () => ({ savedQueriesApi: { get: mocks.savedQueryGet } }));

import { useSavedQueries } from "../useSavedQueries";

const formData = {
  source_id: 7,
  created_from_team_id: 1,
  name: "new query",
  description: "",
  query_language: "logchefql" as const,
  editor_mode: "builder" as const,
  query_content: JSON.stringify({ version: 1, sourceId: 7, content: 'level="{{ level }}"' }),
};

describe("useSavedQueries save intent", () => {
  beforeEach(() => {
    vi.clearAllMocks();
    mocks.route.query = { id: "41" };
    mocks.exploreStore.logchefqlCode = 'level="{{ level }}"';
    mocks.exploreStore.nativeQuery = "";
    mocks.savedQueriesStore.data.queries = [];
    mocks.savedQueriesStore.create.mockResolvedValue({ success: true, data: { id: 99 } });
    mocks.savedQueriesStore.update.mockResolvedValue({ success: true, data: { id: 41 } });
    vi.stubGlobal("window", { confirm: vi.fn(() => false) });
  });

  it("creates a new query even when the current route has a saved-query id", async () => {
    const queries = useSavedQueries();
    await queries.handleSaveAsNewQueryClick();

    const result = await queries.handleSaveQuery(formData);

    expect(result.success).toBe(true);
    expect(mocks.savedQueriesStore.create).toHaveBeenCalledOnce();
    expect(mocks.savedQueriesStore.update).not.toHaveBeenCalled();
  });

  it("retains save-as-new intent after canceling a duplicate-name overwrite", async () => {
    mocks.savedQueriesStore.data.queries = [{ id: 12, name: formData.name, source_id: 7 }];
    const queries = useSavedQueries();
    await queries.handleSaveAsNewQueryClick();

    const first = await queries.handleSaveQuery(formData);
    const second = await queries.handleSaveQuery(formData);

    expect(first).toEqual({ success: false, canceled: true });
    expect(second).toEqual({ success: false, canceled: true });
    expect(mocks.savedQueriesStore.update).not.toHaveBeenCalled();
    expect(mocks.savedQueriesStore.create).not.toHaveBeenCalled();

    queries.closeSaveQueryModal();
    await queries.handleSaveQuery(formData);
    expect(mocks.savedQueriesStore.update).toHaveBeenCalledWith("41", expect.anything());
  });
});
