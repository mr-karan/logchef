import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { createPinia, setActivePinia } from "pinia";
import { sourcesApi } from "@/api/sources";
import { teamsApi } from "@/api/teams";
import { useContextStore } from "../context";
import { useSourcesStore } from "../sources";
import { useTeamsStore } from "../teams";

// Real context and sources stores. Only the sources endpoints are stubbed.
const ok = <T>(data: T) => Promise.resolve({ status: "success" as const, data });

function deferred() {
  let release: () => void = () => {};
  const done = new Promise<void>((resolve) => { release = resolve; });
  return { done, release };
}

describe("sources store", () => {
  beforeEach(() => {
    setActivePinia(createPinia());
    vi.spyOn(sourcesApi, "getTeamSource").mockImplementation((_teamId: number, sourceId: number) => ok({ id: sourceId }) as never);
  });

  afterEach(() => {
    vi.restoreAllMocks();
    localStorage.clear();
  });

  it("keeps a left team's late source list out of the current team", async () => {
    let releaseSlowTeam: () => void = () => {};
    const slowTeamLoaded = new Promise<void>((resolve) => { releaseSlowTeam = resolve; });
    vi.spyOn(sourcesApi, "listTeamSources").mockImplementation(async (teamId: number) => {
      if (teamId === 2) await slowTeamLoaded;
      return ok(teamId === 2 ? [{ id: 7 }] : [{ id: 3 }, { id: 4 }]) as never;
    });
    const sources = useSourcesStore();
    const context = useContextStore();

    context.selectTeam(2);
    await vi.waitFor(() => expect(sourcesApi.listTeamSources).toHaveBeenCalledWith(2, expect.objectContaining({ signal: expect.any(AbortSignal) })));
    const leftTeamList = sources.loadTeamSources(2);
    context.selectTeam(1);
    context.selectSource(4);
    releaseSlowTeam();
    await leftTeamList;

    await vi.waitFor(() => expect(sources.currentSourceDetails?.id).toBe(4));
    expect(context.teamId).toBe(1);
    expect(sources.teamSources.map((source) => source.id)).toEqual([3, 4]);
  });

  it("shares one request between concurrent loads of a team", async () => {
    const list = vi.spyOn(sourcesApi, "listTeamSources").mockImplementation(() => ok([{ id: 90 }]) as never);
    const sources = useSourcesStore();

    const [first, second] = await Promise.all([sources.loadTeamSources(9), sources.loadTeamSources(9)]);

    expect(list).toHaveBeenCalledTimes(1);
    expect(second).toBe(first);
  });

  it("selects the first connected source of a team with no stored source", async () => {
    vi.spyOn(sourcesApi, "listTeamSources").mockReturnValue(ok([{ id: 6, is_connected: false }, { id: 7, is_connected: true }]) as never);
    useSourcesStore();
    const context = useContextStore();

    context.selectTeam(2);

    await vi.waitFor(() => expect(context.sourceId).toBe(7));
  });

  it("fetches a team's sources again when the context returns to it", async () => {
    const list = vi.spyOn(sourcesApi, "listTeamSources").mockImplementation((teamId: number) => ok([{ id: teamId * 10 }]) as never);
    const sources = useSourcesStore();
    const context = useContextStore();

    context.selectTeam(1);
    await vi.waitFor(() => expect(sources.teamSources).toEqual([{ id: 10 }]));
    context.selectTeam(2);
    await vi.waitFor(() => expect(sources.teamSources).toEqual([{ id: 20 }]));
    context.selectTeam(1);

    await vi.waitFor(() => expect(list.mock.calls.filter(([teamId]) => teamId === 1)).toHaveLength(2));
  });

  it.each([
    ["deleting a source", () => useSourcesStore().deleteSource(4)],
    ["adding a source to the team", () => useTeamsStore().addTeamSource(1, 5)],
    ["removing a source from the team", () => useTeamsStore().removeTeamSource(1, 4)],
  ])("shows the team's new sources after %s", async (_change, change) => {
    const before = [{ id: 3, name: "checkout" }, { id: 4, name: "cart" }];
    const after = [{ id: 3, name: "checkout-v2" }, { id: 5, name: "audit" }];
    const list = vi.spyOn(sourcesApi, "listTeamSources").mockReturnValue(ok(before) as never);
    vi.spyOn(sourcesApi, "updateSource").mockReturnValue(ok({ id: 3, name: "checkout-v2" }) as never);
    vi.spyOn(sourcesApi, "deleteSource").mockReturnValue(ok(null) as never);
    vi.spyOn(sourcesApi, "listAllSourcesForAdmin").mockReturnValue(ok([]) as never);
    vi.spyOn(teamsApi, "addTeamSource").mockReturnValue(ok(null) as never);
    vi.spyOn(teamsApi, "removeTeamSource").mockReturnValue(ok(null) as never);
    vi.spyOn(teamsApi, "listTeamSources").mockReturnValue(ok(after) as never);
    const sources = useSourcesStore();
    const context = useContextStore();
    context.selectTeam(1);
    await vi.waitFor(() => expect(sources.teamSources).toEqual(before));

    list.mockReturnValue(ok(after) as never);
    await change();

    await vi.waitFor(() => expect(sources.teamSources).toEqual(after));
  });

  it("rejects when a team's sources or a source's details fail to load", async () => {
    const failure = { status: "error", message: "Sources are unavailable", error_type: "ServerError" };
    vi.spyOn(sourcesApi, "listTeamSources").mockRejectedValue(failure);
    vi.mocked(sourcesApi.getTeamSource).mockRejectedValue(failure);
    const sources = useSourcesStore();

    await expect(sources.loadTeamSources(9)).rejects.toMatchObject({ message: failure.message });
    await expect(sources.loadSourceDetails(9, 1)).rejects.toMatchObject({ message: failure.message });
  });

  it("serves the current team's sources and its connected source's details without a request", async () => {
    const list = vi.spyOn(sourcesApi, "listTeamSources").mockReturnValue(ok([{ id: 3, is_connected: true }]) as never);
    const details = vi.mocked(sourcesApi.getTeamSource).mockReturnValue(ok({ id: 3, is_connected: true }) as never);
    const sources = useSourcesStore();
    useContextStore().selectTeam(1);
    await vi.waitFor(() => expect(sources.currentSourceDetails?.id).toBe(3));

    expect(await sources.loadTeamSources(1)).toEqual([{ id: 3, is_connected: true }]);
    expect(await sources.loadSourceDetails(1, 3)).toEqual({ id: 3, is_connected: true });
    expect(list).toHaveBeenCalledTimes(1);
    expect(details).toHaveBeenCalledTimes(1);
  });

  it("caches disconnected details until a caller asks to refresh", async () => {
    vi.spyOn(sourcesApi, "listTeamSources").mockReturnValue(ok([{ id: 3 }]) as never);
    const details = vi.mocked(sourcesApi.getTeamSource).mockReturnValue(ok({ id: 3, is_connected: false }) as never);
    const sources = useSourcesStore();
    useContextStore().selectTeam(1);
    await vi.waitFor(() => expect(sources.currentSourceDetails?.is_connected).toBe(false));

    details.mockReturnValue(ok({ id: 3, is_connected: true }) as never);
    expect(await sources.loadSourceDetails(1, 3)).toEqual({ id: 3, is_connected: false });
    expect(details).toHaveBeenCalledTimes(1);
    await sources.loadSourceDetails(1, 3, true);

    expect(details).toHaveBeenCalledTimes(2);
    expect(sources.currentSourceDetails?.is_connected).toBe(true);
  });

  it("joins an invalidation refresh instead of serving the retained old metadata", async () => {
    const list = vi.spyOn(sourcesApi, "listTeamSources").mockReturnValue(ok([{ id: 3, name: "old" }]) as never);
    const details = vi.mocked(sourcesApi.getTeamSource).mockReturnValue(ok({ id: 3, name: "old", is_connected: true }) as never);
    const sources = useSourcesStore();
    useContextStore().selectTeam(1);
    await vi.waitFor(() => expect(sources.currentSourceDetails?.name).toBe("old"));
    const freshList = deferred();
    const freshDetails = deferred();
    list.mockImplementation(async () => { await freshList.done; return ok([{ id: 3, name: "new" }]) as never; });
    details.mockImplementation(async () => { await freshDetails.done; return ok({ id: 3, name: "new", is_connected: true }) as never; });
    sources.invalidateTeamSources();
    let receivedList: unknown;
    let receivedDetails: unknown;
    const loadingList = sources.loadTeamSources(1).then(value => { receivedList = value; });
    await new Promise(resolve => setImmediate(resolve));
    expect(receivedList).toBeUndefined();
    expect(sources.currentSourceDetails?.name).toBe("old");
    freshList.release();
    await loadingList;
    await vi.waitFor(() => expect(details).toHaveBeenCalledTimes(2));
    const loadingDetails = sources.loadSourceDetails(1, 3).then(value => { receivedDetails = value; });
    await new Promise(resolve => setImmediate(resolve));
    expect(receivedDetails).toBeUndefined();
    freshDetails.release();
    await loadingDetails;
    expect(receivedList).toEqual([{ id: 3, name: "new" }]);
    expect(receivedDetails).toEqual({ id: 3, name: "new", is_connected: true });
    expect(list).toHaveBeenCalledTimes(2);
    expect(details).toHaveBeenCalledTimes(2);
  });

  it("neither joins nor keeps a team's sources requested before an invalidation", async () => {
    const before = [{ id: 3, name: "old" }];
    const after = [{ id: 3, name: "new" }];
    const firstLoad = deferred();
    const list = vi.spyOn(sourcesApi, "listTeamSources")
      .mockImplementationOnce(async () => {
        await firstLoad.done;
        return ok(before) as never;
      })
      .mockReturnValue(ok(after) as never);
    const sources = useSourcesStore();
    useContextStore().selectTeam(1);
    await vi.waitFor(() => expect(list).toHaveBeenCalledTimes(1));

    sources.invalidateTeamSources();
    await vi.waitFor(() => expect(sources.teamSources).toEqual(after));
    firstLoad.release();
    await firstLoad.done;
    await new Promise((resolve) => setImmediate(resolve));

    expect(list).toHaveBeenCalledTimes(2);
    expect(sources.teamSources).toEqual(after);
  });

  it.each([
    ["before the context returns", true],
    ["after the context returns", false],
  ])("keeps details requested before the context left their team out of the cache when they arrive %s", async (_when, releaseBeforeReturn) => {
    vi.spyOn(sourcesApi, "listTeamSources").mockImplementation((teamId: number) => ok([{ id: teamId === 1 ? 3 : 7 }]) as never);
    const slowDetails = deferred();
    vi.mocked(sourcesApi.getTeamSource)
      .mockImplementationOnce(async () => {
        await slowDetails.done;
        return ok({ id: 3, name: "old" }) as never;
      })
      .mockImplementation((_teamId: number, sourceId: number) => ok({ id: sourceId, name: "new" }) as never);
    const sources = useSourcesStore();
    const context = useContextStore();
    context.selectTeam(1);
    await vi.waitFor(() => expect(sourcesApi.getTeamSource).toHaveBeenCalledWith(1, 3));

    context.selectTeam(2);
    await vi.waitFor(() => expect(sources.currentSourceDetails?.id).toBe(7));
    if (releaseBeforeReturn) {
      slowDetails.release();
      await new Promise((resolve) => setImmediate(resolve));
    }
    context.selectTeam(1);
    await vi.waitFor(() => expect(sources.currentSourceDetails?.name).toBe("new"));
    slowDetails.release();
    await new Promise((resolve) => setImmediate(resolve));

    expect(sources.currentSourceDetails?.name).toBe("new");
  });

  it("keeps the current source's details while an invalidation refetches them, and when the refetch fails", async () => {
    vi.spyOn(sourcesApi, "listTeamSources").mockReturnValue(ok([{ id: 3 }]) as never);
    const details = vi.mocked(sourcesApi.getTeamSource).mockReturnValue(ok({ id: 3, name: "old", is_connected: true }) as never);
    const sources = useSourcesStore();
    useContextStore().selectTeam(1);
    await vi.waitFor(() => expect(sources.currentSourceDetails?.name).toBe("old"));

    const refetch = deferred();
    details.mockImplementation(async () => {
      await refetch.done;
      return ok({ id: 3, name: "new", is_connected: true }) as never;
    });
    sources.invalidateTeamSources();
    await vi.waitFor(() => expect(details).toHaveBeenCalledTimes(2));
    expect(sources.currentSourceDetails?.name).toBe("old");
    refetch.release();
    await vi.waitFor(() => expect(sources.currentSourceDetails?.name).toBe("new"));

    details.mockRejectedValue({ status: "error", message: "Source is unreachable", error_type: "ServerError" });
    sources.invalidateTeamSources();
    await vi.waitFor(() => expect(details).toHaveBeenCalledTimes(3));
    await new Promise((resolve) => setImmediate(resolve));
    expect(sources.currentSourceDetails?.name).toBe("new");
  });
});
