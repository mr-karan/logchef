import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { createPinia, setActivePinia } from "pinia";
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

    it("does not reselect a team the user left when falling back to admin teams", async () => {
      vi.spyOn(authApi, "localLogin").mockReturnValue(ok({}) as never);
      vi.spyOn(authApi, "getSession").mockReturnValue(ok({ user: { id: "7" }, session: {} }) as never);
      await useAuthStore().localLogin("user@example.com", "placeholder");
      const teams = await loadTeams([membership(1)], [adminTeam(1), adminTeam(2)]);
      vi.spyOn(teamsApi, "removeTeamMember").mockReturnValue(ok({ message: "removed" }) as never);
      vi.mocked(teamsApi.listUserTeams).mockReturnValue(ok([]) as never);
      useContextStore().selectTeam(1);

      await teams.removeTeamMember(1, 7);

      expect(useContextStore().teamId).toBe(2);
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
