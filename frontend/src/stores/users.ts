import { defineStore } from "pinia";
import { ref } from "vue";
import type { User } from "@/types";
import { usersApi } from "@/api/users";
import { isErrorResponse } from "@/api/types";

export const useUsersStore = defineStore("users", () => {
  const users = ref<User[]>([]);
  const isLoading = ref(false);
  const error = ref<string | null>(null);

  async function loadUsers() {
    if (isLoading.value) return;

    try {
      isLoading.value = true;
      error.value = null;

      const response = await usersApi.listUsers();

      if (isErrorResponse(response)) {
        error.value = response.data.error;
        return;
      }

      users.value = response.data;
    } catch (err) {
      error.value = "Failed to load users";
      console.error("Error loading users:", err);
    } finally {
      isLoading.value = false;
    }
  }

  // Get users not in a specific team
  function getUsersNotInTeam(teamMemberIds: string[]) {
    return users.value.filter((user) => !teamMemberIds.includes(user.id));
  }

  return {
    users,
    isLoading,
    error,
    loadUsers,
    getUsersNotInTeam,
  };
});
