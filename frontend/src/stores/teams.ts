import { defineStore } from "pinia";
import { ref, computed } from "vue";
import { teamsApi, type Team, type CreateTeamRequest } from "@/api/teams";
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
  const currentTeamId = ref<number | null>(null);
  const { toast } = useToast();

  // Computed property for the current team
  const currentTeam = computed(() =>
    currentTeamId.value
      ? teams.value.find((t) => t.id === currentTeamId.value)
      : null
  );

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
      const teamsData = response.data || [];

      // Get member counts for each team
      const teamsWithMembers = await Promise.all(
        teamsData.map(async (team) => {
          const membersResponse = await teamsApi.listTeamMembers(team.id);
          const memberCount =
            isErrorResponse(membersResponse) || !membersResponse.data
              ? 0
              : membersResponse.data.length;
          return { ...team, memberCount };
        })
      );

      teams.value = teamsWithMembers;

      // Set current team if none is selected and we have teams
      if (!currentTeamId.value && teams.value.length > 0) {
        setCurrentTeam(teams.value[0].id);
      }
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

  // Set the current team
  function setCurrentTeam(teamId: number) {
    currentTeamId.value = teamId;
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

  async function deleteTeam(teamId: number) {
    if (isLoading.value) return false;

    try {
      isLoading.value = true;
      error.value = null;
      const response = await teamsApi.deleteTeam(teamId);

      if (isErrorResponse(response)) {
        error.value = response.data.error;
        toast({
          title: "Error",
          description: response.data.error,
          variant: "destructive",
          duration: TOAST_DURATION.ERROR,
        });
        return false;
      }

      // Remove from local state
      teams.value = teams.value.filter((t) => t.id !== teamId);

      // If this was the current team, reset current team
      if (currentTeamId.value === teamId) {
        currentTeamId.value = teams.value.length > 0 ? teams.value[0].id : null;
      }

      toast({
        title: "Success",
        description: "Team deleted successfully",
        duration: TOAST_DURATION.SUCCESS,
      });

      return true;
    } catch (error) {
      console.error("Error deleting team:", error);
      return false;
    } finally {
      isLoading.value = false;
    }
  }

  // Get team source IDs - helper method for filtering
  async function getTeamSourceIds(teamId: number) {
    if (isLoading.value) return [];

    try {
      isLoading.value = true;
      error.value = null;
      const response = await teamsApi.listTeamSources(teamId);

      if (isErrorResponse(response)) {
        error.value = response.data.error;
        return [];
      }

      return (response.data || []).map((source) => source.id);
    } catch (error) {
      console.error("Error getting team sources:", error);
      return [];
    } finally {
      isLoading.value = false;
    }
  }

  return {
    teams,
    isLoading,
    error,
    currentTeamId,
    currentTeam,
    loadTeams,
    createTeam,
    deleteTeam,
    setCurrentTeam,
    getTeamSourceIds,
  };
});
