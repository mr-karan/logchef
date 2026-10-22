import { beforeEach, describe, expect, it, vi } from "vitest";
import { createPinia, setActivePinia } from "pinia";
import { sourcesApi } from "@/api/sources";
import { teamsApi } from "@/api/teams";
import { useContextStore } from "@/stores/context";

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
});
