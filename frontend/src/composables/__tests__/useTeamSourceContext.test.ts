import { afterEach, describe, expect, it, vi } from "vitest";
import { createPinia, setActivePinia } from "pinia";
import { sourcesApi } from "@/api/sources";
import { useContextStore } from "@/stores/context";
import { useSourcesStore } from "@/stores/sources";
import { useTeamSourceContext } from "../useTeamSourceContext";

// Real context and sources stores. Only the sources endpoints are stubbed.
const ok = <T>(data: T) => Promise.resolve({ status: "success" as const, data });

describe("useTeamSourceContext", () => {
  afterEach(() => {
    vi.restoreAllMocks();
    localStorage.clear();
  });

  it("selects no source for a team the user has since left", async () => {
    setActivePinia(createPinia());
    let releaseSlowTeam: () => void = () => {};
    const slowTeamLoaded = new Promise<void>((resolve) => { releaseSlowTeam = resolve; });
    vi.spyOn(sourcesApi, "listTeamSources").mockImplementation(async (teamId: number) => {
      if (teamId === 2) await slowTeamLoaded;
      return ok(teamId === 2 ? [{ id: 7 }] : [{ id: 3 }, { id: 4 }]) as never;
    });
    vi.spyOn(sourcesApi, "getTeamSource").mockImplementation((_teamId: number, sourceId: number) => ok({ id: sourceId }) as never);
    useSourcesStore();
    const context = useContextStore();
    const { applyContextSelection } = useTeamSourceContext();

    context.selectTeam(2);
    await vi.waitFor(() => expect(useSourcesStore().isLoadingTeamSources).toBe(true));
    const superseded = applyContextSelection(2, 7, { maxWaitMs: 50 });
    context.selectTeam(1);
    context.selectSource(4);
    releaseSlowTeam();

    await expect(superseded).resolves.toBeNull();
    expect(context.teamId).toBe(1);
    expect(context.sourceId).toBe(4);
  });

  it("waits for the current team's sources when a left team's list returns first", async () => {
    setActivePinia(createPinia());
    const releases = new Map<number, () => void>();
    const loaded = new Map([2, 9].map((teamId) => [teamId, new Promise<void>((resolve) => releases.set(teamId, resolve))]));
    vi.spyOn(sourcesApi, "listTeamSources").mockImplementation(async (teamId: number) => {
      await loaded.get(teamId);
      return ok(teamId === 2 ? [{ id: 7 }] : teamId === 9 ? [{ id: 90 }, { id: 91 }] : [{ id: 3 }]) as never;
    });
    vi.spyOn(sourcesApi, "getTeamSource").mockImplementation((_teamId: number, sourceId: number) => ok({ id: sourceId }) as never);
    useSourcesStore();
    const { applyContextSelection } = useTeamSourceContext();

    const superseded = applyContextSelection(2, 7, { maxWaitMs: 50 });
    await vi.waitFor(() => expect(sourcesApi.listTeamSources).toHaveBeenCalledWith(2, expect.anything()));
    const current = applyContextSelection(9, 91);
    await vi.waitFor(() => expect(sourcesApi.listTeamSources).toHaveBeenCalledWith(9, expect.anything()));
    releases.get(2)?.();
    await new Promise((resolve) => setTimeout(resolve, 20));
    releases.get(9)?.();

    await expect(superseded).resolves.toBeNull();
    await expect(current).resolves.toBe(91);
    expect(useContextStore().sourceId).toBe(91);
  });
});
