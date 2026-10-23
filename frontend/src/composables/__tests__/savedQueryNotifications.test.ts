import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { createApp } from "vue";
import { createPinia, setActivePinia } from "pinia";
import { createMemoryHistory, createRouter } from "vue-router";
import { toast } from "vue-sonner";
import { api } from "@/api/config";
import { useSavedQueries } from "../useSavedQueries";
import { useSavedQueriesStore } from "@/stores/savedQueries";
import { useContextStore } from "@/stores/context";
import { useCollectionsStore } from "@/stores/collections";

vi.mock("vue-sonner", () => ({
  toast: Object.assign(vi.fn(), { error: vi.fn(), success: vi.fn() }),
}));

const formData = {
  source_id: 7,
  created_from_team_id: 1,
  collection_id: 3,
  name: "Error logs",
  description: "",
  query_language: "logchefql" as const,
  editor_mode: "builder" as const,
  query_content: JSON.stringify({ version: 1, sourceId: 7, content: 'level="error"' }),
};
const savedQuery = { ...formData, id: 99, created_at: "", updated_at: "" };
const originalAdapter = api.defaults.adapter;

describe("saved query notifications", () => {
  beforeEach(() => {
    setActivePinia(createPinia());
    localStorage.clear();
    vi.clearAllMocks();
  });

  afterEach(() => {
    api.defaults.adapter = originalAdapter;
  });

  it("shows one success toast when saving a query into a collection", async () => {
    const requests: string[] = [];
    const responses = new Map<string, unknown>([
      ["post /saved-queries", savedQuery],
      ["get /saved-queries?source_id=7", [savedQuery]],
      ["post /collections/3/items", { message: "added" }],
      ["get /collections/3/items", [{ collection_id: 3, query: savedQuery }]],
      ["get /collections", []],
    ]);
    api.defaults.adapter = async (config) => {
      const request = `${config.method} ${config.url}`;
      requests.push(request);
      if (!responses.has(request)) throw new Error(`Unexpected request ${request}`);
      return { config, status: 200, statusText: "OK", headers: {}, data: { status: "success", data: responses.get(request) } };
    };
    const router = createRouter({
      history: createMemoryHistory(),
      routes: [{ path: "/", component: { render: () => null } }],
    });
    await router.push("/");
    await router.isReady();
    let queries: ReturnType<typeof useSavedQueries> | undefined;
    const app = createApp({
      setup() {
        queries = useSavedQueries();
        return () => null;
      },
    });
    app.use(createPinia()).use(router).mount(document.createElement("div"));
    try {
      expect(queries).toBeDefined();
      useContextStore().selectTeam(1);
      useContextStore().selectSource(7);
      const result = await queries?.handleSaveQuery(formData);

      expect(result?.id).toBe(99);
      expect(useSavedQueriesStore().queries.map(query => query.id)).toEqual([99]);
      expect(useCollectionsStore().data.items[3].map(item => item.query.id)).toEqual([99]);
      expect(requests.filter(request => request === "post /collections/3/items")).toHaveLength(1);
      expect(toast).toHaveBeenCalledExactlyOnceWith("Query created successfully", expect.anything());
      expect(toast.error).not.toHaveBeenCalled();
      expect(toast.success).not.toHaveBeenCalled();
    } finally {
      app.unmount();
    }
  });
});
