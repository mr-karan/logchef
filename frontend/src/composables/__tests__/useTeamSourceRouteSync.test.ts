import { beforeEach, describe, expect, it, vi } from "vitest";

const mocks = vi.hoisted(() => ({
  route: {
    path: "/logs/alerts",
    query: {} as Record<string, string>,
  },
  replace: vi.fn(),
  contextStore: {
    teamId: 3 as number | null,
    sourceId: 7 as number | null,
  },
}));

vi.mock("vue-router", () => ({
  useRoute: () => mocks.route,
  useRouter: () => ({ replace: mocks.replace }),
}));

vi.mock("@/stores/context", () => ({
  useContextStore: () => mocks.contextStore,
}));

vi.mock("@/composables/useTeamSourceContext", () => ({
  useTeamSourceContext: () => ({
    applyContextSelection: vi.fn(),
    parseId: vi.fn(),
    resolveTeamId: vi.fn(),
  }),
}));

import { useTeamSourceRouteSync } from "../useTeamSourceRouteSync";

describe("useTeamSourceRouteSync route ownership", () => {
  beforeEach(() => {
    vi.clearAllMocks();
    mocks.route.path = "/logs/alerts";
    mocks.route.query = {};
    mocks.contextStore.teamId = 3;
    mocks.contextStore.sourceId = 7;
  });

  it("syncs context while the owning route is active", async () => {
    const routeSync = useTeamSourceRouteSync("/logs/alerts");

    await routeSync.syncUrlToContext();

    expect(mocks.replace).toHaveBeenCalledWith({
      path: "/logs/alerts",
      query: { team: "3", source: "7" },
    });
  });

  it("does not let a cached view rewrite a different route", async () => {
    const routeSync = useTeamSourceRouteSync("/logs/alerts");
    mocks.route.path = "/logs/library";

    await routeSync.syncUrlToContext();

    expect(routeSync.isCurrentRoute()).toBe(false);
    expect(mocks.replace).not.toHaveBeenCalled();
  });

  it("binds an implicit sync path when the composable is created", async () => {
    const routeSync = useTeamSourceRouteSync();
    mocks.route.path = "/logs/library";

    await routeSync.syncUrlToContext();

    expect(mocks.replace).not.toHaveBeenCalled();
  });
});
