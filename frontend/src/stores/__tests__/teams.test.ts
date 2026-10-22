import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { createPinia, setActivePinia } from "pinia";
import { toast } from "vue-sonner";
import { controlledApi } from "@/test/controlledApi";
import { authApi } from "@/api/auth";
import { teamsApi, type TeamWithMemberCount, type UserTeamMembership } from "@/api/teams";
import { useAuthStore } from "../auth";
import { useContextStore } from "../context";
import { useTeamsStore } from "../teams";

vi.mock("vue-sonner", () => ({
  toast: Object.assign(vi.fn(), { error: vi.fn(), success: vi.fn() }),
}));

const ok = <T>(data: T) => Promise.resolve({ status: "success" as const, data });

const membership = (id: number, memberCount = 2): UserTeamMembership => ({
  id, name: `team-${id}`, description: "", created_at: "", updated_at: "", member_count: memberCount, role: "admin",
});
const adminTeam = (id: number, memberCount = 2): TeamWithMemberCount => ({
  id, name: `team-${id}`, description: "", created_by: "", created_at: "", updated_at: "", member_count: memberCount,
});

async function loadTeams(user: UserTeamMembership[], admin: TeamWithMemberCount[]) {
  vi.spyOn(teamsApi, "listUserTeams").mockReturnValue(ok(user) as never);
  vi.spyOn(teamsApi, "listAllTeams").mockReturnValue(ok(admin) as never);
  const teams = useTeamsStore();
  await teams.loadUserTeams();
  await teams.loadAdminTeams(true);
  return teams;
}

describe("useTeamsStore", () => {
  beforeEach(() => {
    setActivePinia(createPinia());
    localStorage.clear();
  });

  afterEach(() => vi.restoreAllMocks());

  describe("superseded reads", () => {
    let network: ReturnType<typeof controlledApi>;

    beforeEach(() => {
      vi.mocked(toast.error).mockClear();
      network = controlledApi();
    });
    afterEach(() => network.restore());

    it.each(['getTeam', 'listTeamMembers', 'listTeamSources'] as const)("drops an old %s failure after another team loads", async operation => {
      const teams = useTeamsStore();
      const oldLoad = teams[operation](1);
      await vi.waitFor(() => expect(network.requests).toHaveLength(1));
      const currentLoad = teams[operation](2);
      await vi.waitFor(() => expect(network.requests).toHaveLength(2));
      network.requests[1].succeed(operation === 'getTeam' ? adminTeam(2) : []);
      await currentLoad;
      network.requests[0].fail('Old team load failed');
      const result = await oldLoad;

      expect(result.success).toBe(false);
      expect(teams.error).toBeNull();
      expect(toast.error).not.toHaveBeenCalled();
    });

    it.each(['loadUserTeams', 'loadAdminTeams'] as const)("drops an old %s failure after a newer list loads", async operation => {
      const teams = useTeamsStore();
      const oldLoad = teams[operation]();
      await vi.waitFor(() => expect(network.requests).toHaveLength(1));
      const currentLoad = teams[operation]();
      await vi.waitFor(() => expect(network.requests).toHaveLength(2));
      network.requests[1].succeed([membership(2)]);
      await currentLoad;
      network.requests[0].fail('Old list load failed');
      const result = await oldLoad;

      expect(result.success).toBe(false);
      expect(teams.error).toBeNull();
      expect(toast.error).not.toHaveBeenCalled();
      expect(teams.teams.map(team => team.id)).toEqual([2]);
    });

    it('preserves the current error when an older request fails', async () => {
      const teams = useTeamsStore();
      const oldLoad = teams.getTeam(1);
      await vi.waitFor(() => expect(network.requests).toHaveLength(1));
      const currentLoad = teams.getTeam(2);
      await vi.waitFor(() => expect(network.requests).toHaveLength(2));
      network.requests[1].fail('Current team load failed');
      await currentLoad;
      const currentError = teams.error;
      network.requests[0].fail('Old team load failed');
      await oldLoad;

      expect(teams.error).toBe(currentError);
      expect(toast.error).toHaveBeenCalledTimes(1);
    });

    it('keeps the newest data when requests for the same team finish out of order', async () => {
      const teams = useTeamsStore();
      const oldLoad = teams.getTeam(1);
      await vi.waitFor(() => expect(network.requests).toHaveLength(1));
      const currentLoad = teams.getTeam(1);
      await vi.waitFor(() => expect(network.requests).toHaveLength(2));
      network.requests[1].succeed({ ...adminTeam(1), name: 'Fresh team name' });
      await currentLoad;
      network.requests[0].succeed({ ...adminTeam(1), name: 'Stale team name' });
      await oldLoad;

      expect(teams.getTeamById(1)?.name).toBe('Fresh team name');
      expect(teams.error).toBeNull();
      expect(toast.error).not.toHaveBeenCalled();
    });

    it('drops a request failure after the team state is cleared', async () => {
      const teams = useTeamsStore();
      const load = teams.listTeamMembers(1);
      await vi.waitFor(() => expect(network.requests).toHaveLength(1));
      teams.clearState();
      network.requests[0].fail('Cleared team load failed');
      const result = await load;

      expect(result.success).toBe(false);
      expect(teams.error).toBeNull();
      expect(toast.error).not.toHaveBeenCalled();
    });

    it.each(['getTeam', 'listTeamMembers', 'listTeamSources'] as const)("keeps current %s failures visible", async operation => {
      const teams = useTeamsStore();
      const load = teams[operation](1);
      await vi.waitFor(() => expect(network.requests).toHaveLength(1));
      network.requests[0].fail('Current team load failed');
      await load;

      expect(teams.error?.message).toBe('Current team load failed');
      expect(toast.error).toHaveBeenCalledTimes(1);
    });
  });

  describe("deleting the selected team", () => {
    it("selects the next team the user belongs to", async () => {
      const teams = await loadTeams([membership(1), membership(2)], [adminTeam(1), adminTeam(2), adminTeam(3)]);
      vi.spyOn(teamsApi, "deleteTeam").mockReturnValue(ok({ message: "deleted" }) as never);
      useContextStore().selectTeam(1);

      await teams.deleteTeam(1);

      expect(useContextStore().teamId).toBe(2);
      expect(teams.userTeams.map((t) => t.id)).toEqual([2]);
      expect(teams.adminTeams.map((t) => t.id)).toEqual([2, 3]);
    });

    it("falls back to an admin team when the user belongs to no other team", async () => {
      const teams = await loadTeams([membership(1)], [adminTeam(1), adminTeam(3)]);
      vi.spyOn(teamsApi, "deleteTeam").mockReturnValue(ok({ message: "deleted" }) as never);
      useContextStore().selectTeam(1);

      await teams.deleteTeam(1);

      expect(useContextStore().teamId).toBe(3);
    });

    it("clears the selection when no team is left", async () => {
      const teams = await loadTeams([membership(1)], [adminTeam(1)]);
      vi.spyOn(teamsApi, "deleteTeam").mockReturnValue(ok({ message: "deleted" }) as never);
      useContextStore().selectTeam(1);

      await teams.deleteTeam(1);

      expect(useContextStore().teamId).toBeNull();
    });

    it("keeps the selection when another team is deleted", async () => {
      const teams = await loadTeams([membership(1), membership(2)], []);
      vi.spyOn(teamsApi, "deleteTeam").mockReturnValue(ok({ message: "deleted" }) as never);
      useContextStore().selectTeam(1);

      await teams.deleteTeam(2);

      expect(useContextStore().teamId).toBe(1);
    });
  });

  describe("member changes", () => {
    it("moves the user off a team they leave without picking that team again", async () => {
      vi.spyOn(authApi, "localLogin").mockReturnValue(ok({}) as never);
      vi.spyOn(authApi, "getSession").mockReturnValue(ok({ user: { id: "7" }, session: {} }) as never);
      await useAuthStore().localLogin("user@example.com", "placeholder");
      const teams = await loadTeams([membership(1), membership(2)], [adminTeam(1), adminTeam(2)]);
      vi.spyOn(teamsApi, "removeTeamMember").mockReturnValue(ok({ message: "removed" }) as never);
      vi.mocked(teamsApi.listUserTeams).mockReturnValue(ok([membership(2)]) as never);
      useContextStore().selectTeam(1);

      await teams.removeTeamMember(1, 7);

      expect(useContextStore().teamId).toBe(2);
      expect(teams.userTeams.map((t) => t.id)).toEqual([2]);
    });

    it("clears the selection after the last membership ends even when admin teams remain", async () => {
      vi.spyOn(authApi, "localLogin").mockReturnValue(ok({}) as never);
      vi.spyOn(authApi, "getSession").mockReturnValue(ok({ user: { id: "7" }, session: {} }) as never);
      await useAuthStore().localLogin("user@example.com", "placeholder");
      const teams = await loadTeams([membership(1)], [adminTeam(1), adminTeam(2)]);
      vi.spyOn(teamsApi, "removeTeamMember").mockReturnValue(ok({ message: "removed" }) as never);
      vi.mocked(teamsApi.listUserTeams).mockReturnValue(ok([]) as never);
      useContextStore().selectTeam(1);

      await teams.removeTeamMember(1, 7);

      expect(useContextStore().teamId).toBeNull();
    });

    it("updates the member count in both team lists", async () => {
      const teams = await loadTeams([membership(1, 2)], [adminTeam(1, 2)]);
      vi.spyOn(teamsApi, "addTeamMember").mockReturnValue(ok({ message: "added" }) as never);
      vi.spyOn(teamsApi, "listTeamMembers").mockReturnValue(ok([]) as never);
      vi.spyOn(teamsApi, "removeTeamMember").mockReturnValue(ok({ message: "removed" }) as never);

      await teams.addTeamMember(1, { user_id: 9, role: "member" });
      expect(teams.userTeams[0].member_count).toBe(3);
      expect(teams.adminTeams[0].member_count).toBe(3);

      await teams.removeTeamMember(1, 9);
      expect(teams.userTeams[0].member_count).toBe(2);
      expect(teams.adminTeams[0].member_count).toBe(2);
    });
  });
});
