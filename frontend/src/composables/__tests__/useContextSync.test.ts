import { beforeEach, describe, expect, it, vi } from "vitest";
import { createPinia, setActivePinia } from "pinia";
import { sourcesApi } from "@/api/sources";
import { teamsApi } from "@/api/teams";
import { useContextStore } from "@/stores/context";
import { useSourcesStore } from "@/stores/sources";
import { useTeamsStore } from "@/stores/teams";

const mocks = vi.hoisted(() => ({
  route: { path: "/logs/alerts", query: {} as Record<string, string> },
  replace: vi.fn(),
}));

vi.mock("vue-router", async (importOriginal) => ({
  ...(await importOriginal<typeof import("vue-router")>()),
  useRoute: () => mocks.route,
  useRouter: () => ({ replace: mocks.replace }),
}));

import { useContextSync } from "../useContextSync";

// Real stores. Only vue-router and the teams and sources endpoints are stubbed.
const ok = <T>(data: T) => Promise.resolve({ status: "success" as const, data });

describe("useContextSync route ownership", () => {
  beforeEach(() => {
    vi.restoreAllMocks();
    mocks.replace.mockReset();
    mocks.route.path = "/logs/alerts";
    mocks.route.query = {};
    localStorage.clear();
    setActivePinia(createPinia());
    vi.spyOn(teamsApi, "listUserTeams").mockReturnValue(ok([{ id: 3, name: "payments" }]) as never);
    vi.spyOn(sourcesApi, "listTeamSources").mockReturnValue(ok([{ id: 7 }]) as never);
    vi.spyOn(sourcesApi, "getTeamSource").mockReturnValue(ok({ id: 7 }) as never);
    useContextStore().selectTeam(3);
    useContextStore().selectSource(7);
  });

  it("syncs context while the owning route is active", async () => {
    await useContextSync({ basePath: "/logs/alerts" }).initialize();

    expect(mocks.replace).toHaveBeenCalledWith({ path: "/logs/alerts", query: { team: "3", source: "7" } });
  });

  it("does not let a cached view rewrite a different route", async () => {
    const contextSync = useContextSync({ basePath: "/logs/alerts" });
    mocks.route.path = "/logs/library";

    await contextSync.initialize();

    expect(mocks.replace).not.toHaveBeenCalled();
  });

  it("binds an implicit sync path when the composable is created", async () => {
    const contextSync = useContextSync();
    mocks.route.path = "/logs/library";

    await contextSync.initialize();

    expect(mocks.replace).not.toHaveBeenCalled();
  });

  it("clears an unconfirmed source when the team's loaded list is empty", async () => {
    vi.mocked(sourcesApi.listTeamSources).mockReturnValue(ok([]) as never);

    await useContextSync().initialize();

    expect(useContextStore().sourceId).toBeNull();
    expect(useSourcesStore().currentSourceDetails).toBeNull();
    expect(mocks.replace).toHaveBeenCalledWith({ path: "/logs/alerts", query: { team: "3" } });
  });

  it("does not let a departed page's list restore its old source", async () => {
    await useTeamsStore().loadUserTeams();
    let release: () => void = () => {};
    const ready = new Promise<void>(resolve => { release = resolve; });
    vi.mocked(sourcesApi.listTeamSources)
      .mockImplementationOnce(async () => { await ready; return ok([{ id: 7 }, { id: 8 }]) as never; })
      .mockReturnValue(ok([{ id: 7 }, { id: 8 }]) as never);
    vi.mocked(sourcesApi.getTeamSource).mockImplementation((_team, id) => ok({ id }) as never);
    const loading = useContextSync().initialize();
    await vi.waitFor(() => expect(sourcesApi.listTeamSources).toHaveBeenCalledTimes(1));

    mocks.route.path = "/logs/library";
    useContextStore().selectSource(8);
    await vi.waitFor(() => expect(useSourcesStore().currentSourceDetails?.id).toBe(8));
    release();
    await loading;
    await new Promise(resolve => setImmediate(resolve));

    expect(useContextStore().sourceId).toBe(8);
    expect(useSourcesStore().currentSourceDetails?.id).toBe(8);
    expect(mocks.replace).not.toHaveBeenCalled();
  });

  it("never writes a removed source from an invalidated list to the URL", async () => {
    await useTeamsStore().loadUserTeams();
    let release: () => void = () => {};
    const ready = new Promise<void>(resolve => { release = resolve; });
    vi.mocked(sourcesApi.listTeamSources)
      .mockImplementationOnce(async () => { await ready; return ok([{ id: 7 }]) as never; })
      .mockReturnValue(ok([]) as never);
    const loading = useContextSync().initialize();
    await vi.waitFor(() => expect(sourcesApi.listTeamSources).toHaveBeenCalledTimes(1));

    useSourcesStore().invalidateTeamSources();
    await vi.waitFor(() => expect(useContextStore().sourceId).toBeNull());
    release();
    await loading;
    await new Promise(resolve => setImmediate(resolve));

    expect(useContextStore().sourceId).toBeNull();
    expect(mocks.replace.mock.calls.every(([location]) => !location.query.source)).toBe(true);
  });

  it("publishes only the latest page selection when callers join a pending list", async () => {
    await useTeamsStore().loadUserTeams();
    let release: () => void = () => {};
    const ready = new Promise<void>(resolve => { release = resolve; });
    vi.mocked(sourcesApi.listTeamSources).mockImplementation(async () => {
      await ready;
      return ok([{ id: 7 }, { id: 8 }]) as never;
    });
    vi.mocked(sourcesApi.getTeamSource).mockImplementation((_team, id) => ok({ id }) as never);
    const sync = useContextSync();
    const initializing = sync.initialize();
    await vi.waitFor(() => expect(sourcesApi.listTeamSources).toHaveBeenCalledTimes(1));
    const picking = sync.handleSourceChange(8);
    release();
    await Promise.all([initializing, picking]);

    expect(useContextStore().sourceId).toBe(8);
    expect(mocks.replace).toHaveBeenCalledTimes(1);
    expect(mocks.replace).toHaveBeenCalledWith({ path: "/logs/alerts", query: { team: "3", source: "8" } });
  });

  it("keeps the selected source when a list load fails", async () => {
    vi.mocked(sourcesApi.listTeamSources).mockRejectedValue(new Error("Unavailable"));

    await useContextSync().initialize();

    expect(useContextStore().sourceId).toBe(7);
    expect(mocks.replace).not.toHaveBeenCalled();
  });
});
