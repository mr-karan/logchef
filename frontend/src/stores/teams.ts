import { defineStore } from "pinia";
import { ref } from "vue";
import { teamsApi, type Team, type CreateTeamRequest } from "@/api/users";
import { isErrorResponse } from "@/api/types";
import { useToast } from "@/components/ui/toast/use-toast";
import { TOAST_DURATION } from "@/lib/constants";

export interface TeamWithMemberCount extends Team {
  memberCount: number;
}

export const useTeamsStore = defineStore("teams", () => {
  const teams = ref<TeamWithMemberCount[]>([]);
  const isLoading = ref(false);
  const error = ref<string | null>(null);
  const { toast } = useToast();

  async function loadTeams() {
    if (isLoading.value) return;

    try {
      isLoading.value = true;
      error.value = null;
      const response = await teamsApi.listTeams();

      if (isErrorResponse(response)) {
        error.value = response.data.error;
        toast({
          title: "Error",
          description: response.data.error,
          variant: "destructive",
          duration: TOAST_DURATION.ERROR,
        });
        teams.value = [];
        return;
      }

      // Initialize with empty array if teams is null
      const teamsData = response.data.teams || [];

      // Get member counts for each team
      const teamsWithMembers = await Promise.all(
        teamsData.map(async (team) => {
          const membersResponse = await teamsApi.listTeamMembers(team.id);
          const memberCount =
            isErrorResponse(membersResponse) || !membersResponse.data.members
              ? 0
              : membersResponse.data.members.length;
          return { ...team, memberCount };
        })
      );

      teams.value = teamsWithMembers;
    } catch (err) {
      error.value = "Failed to load teams";
      console.error("Error loading teams:", err);
      toast({
        title: "Error",
        description: "Failed to load teams",
        variant: "destructive",
        duration: TOAST_DURATION.ERROR,
      });
      teams.value = [];
    } finally {
      isLoading.value = false;
    }
  }

  async function createTeam(data: CreateTeamRequest) {
    try {
      const response = await teamsApi.createTeam(data);

      if (isErrorResponse(response)) {
        toast({
          title: "Error",
          description: response.data.error,
          variant: "destructive",
          duration: TOAST_DURATION.ERROR,
        });
        return false;
      }

      toast({
        title: "Success",
        description: "Team created successfully",
        duration: TOAST_DURATION.SUCCESS,
      });

      // Reload teams to get fresh data with member counts
      await loadTeams();
      return true;
    } catch (error) {
      console.error("Error creating team:", error);
      toast({
        title: "Error",
        description: "Failed to create team",
        variant: "destructive",
        duration: TOAST_DURATION.ERROR,
      });
      return false;
    }
  }

  async function deleteTeam(teamId: string) {
    try {
      const response = await teamsApi.deleteTeam(teamId);

      if (isErrorResponse(response)) {
        toast({
          title: "Error",
          description: response.data.error,
          variant: "destructive",
          duration: TOAST_DURATION.ERROR,
        });
        return false;
      }

      toast({
        title: "Success",
        description: "Team deleted successfully",
        duration: TOAST_DURATION.SUCCESS,
      });

      // Reload teams to get fresh data
      await loadTeams();
      return true;
    } catch (error) {
      console.error("Error deleting team:", error);
      toast({
        title: "Error",
        description: "Failed to delete team",
        variant: "destructive",
        duration: TOAST_DURATION.ERROR,
      });
      return false;
    }
  }

  return {
    teams,
    isLoading,
    error,
    loadTeams,
    createTeam,
    deleteTeam,
  };
});
