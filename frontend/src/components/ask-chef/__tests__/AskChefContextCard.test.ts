import { createApp, nextTick } from "vue";
import { createPinia, setActivePinia } from "pinia";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { i18n } from "@/i18n";
import { teamsApi, type UserTeamMembership } from "@/api/teams";
import { sourcesApi } from "@/api/sources";
import { useContextStore } from "@/stores/context";
import { useSourcesStore } from "@/stores/sources";
import { useTeamsStore } from "@/stores/teams";
import type { AskChefContext } from "@/stores/askChef";
import AskChefContextCard from "../AskChefContextCard.vue";

// Real teams, sources and context stores. Only /me/teams and
// /teams/:id/sources are stubbed.
const ok = <T>(data: T) => Promise.resolve({ status: "success" as const, data });
const flushPromises = () => new Promise((resolve) => setTimeout(resolve));

const team: UserTeamMembership = { id: 1, name: "payments", description: "", created_at: "", updated_at: "", member_count: 1, role: "member" };
const context: AskChefContext = { subject: { kind: "explorer" }, teamId: 1, sourceId: 3, start: Date.UTC(2026, 9, 19, 6), end: Date.UTC(2026, 9, 19, 7) };

let unmount: (() => void) | undefined;

function render(): HTMLElement {
  const host = document.createElement("div");
  const app = createApp(AskChefContextCard, { context });
  app.use(i18n).mount(host);
  unmount = () => app.unmount();
  return host;
}

describe("AskChefContextCard", () => {
  beforeEach(() => {
    setActivePinia(createPinia());
    localStorage.clear();
    vi.spyOn(teamsApi, "listUserTeams").mockReturnValue(ok([team]) as never);
    vi.spyOn(sourcesApi, "listTeamSources").mockReturnValue(ok([{ id: 3, name: "checkout-logs" }]) as never);
  });

  afterEach(() => {
    unmount?.();
    unmount = undefined;
    vi.restoreAllMocks();
  });

  it("names the team and source of the context", async () => {
    useSourcesStore();
    await useTeamsStore().loadUserTeams();
    useContextStore().selectTeam(1);
    await flushPromises();

    const root = render();
    await nextTick();
    expect(root.textContent).toContain("payments / checkout-logs");
    expect(root.textContent).not.toContain("#3");
  });

  it("shows the source id until the stores load", async () => {
    const root = render();
    await nextTick();
    expect(root.textContent).toContain("Explorer · #3");
  });
});
