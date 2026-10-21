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
});
