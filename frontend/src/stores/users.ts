import { defineStore } from "pinia";
import { useBaseStore } from "./base";
import type { User } from "@/types";
import { usersApi } from "@/api/users";
import { computed } from "vue";

interface UsersState {
  users: User[];
}

export const useUsersStore = defineStore("users", () => {
  const state = useBaseStore<UsersState>({
    users: [],
  });

  // Computed properties
  const users = computed(() => state.data.value.users);

  async function loadUsers(forceReload = false) {
    // Skip if we already have users and no force reload
    if (users.value.length > 0 && !forceReload) {
      return { success: true, data: users.value };
    }

    return await state.callApi<User[]>({
      apiCall: () => usersApi.listUsers(),
      onSuccess: (data) => {
        state.data.value.users = data || [];
      },
      showToast: true,
    });
  }

  async function getUser(id: string) {
    return await state.callApi<{ user: User }>({
      apiCall: () => usersApi.getUser(id),
      showToast: true,
    });
  }

  async function createUser(data: {
    email: string;
    full_name: string;
    role: "admin" | "member";
  }) {
    const result = await state.callApi<{ user: User }>({
      apiCall: () => usersApi.createUser(data),
      onSuccess: (response) => {
        if (response.user) {
          state.data.value.users.push(response.user);
        }
      },
      successMessage: "User created successfully",
    });

    if (result.success) {
      // Reload users to ensure we have the latest data
      await loadUsers(true);
    }

    return result;
  }

  async function updateUser(
    id: string,
    data: {
      full_name?: string;
      role?: "admin" | "member";
      status?: "active" | "inactive";
    }
  ) {
    return await state.callApi<{ user: User }>({
      apiCall: () => usersApi.updateUser(id, data),
      onSuccess: (response) => {
        if (response.user) {
          const index = state.data.value.users.findIndex(
            (u) => u.id === response.user.id
          );
          if (index >= 0) {
            state.data.value.users[index] = response.user;
          }
        }
      },
      successMessage: "User updated successfully",
    });
  }

  async function deleteUser(id: string) {
    return await state.callApi<{ message: string }>({
      apiCall: () => usersApi.deleteUser(id),
      onSuccess: () => {
        state.data.value.users = state.data.value.users.filter(
          (u) => u.id !== id
        );
      },
      successMessage: "User deleted successfully",
    });
  }

  // Get users not in a specific team
  function getUsersNotInTeam(teamMemberIds: (string | number)[]) {
    return state.data.value.users.filter(
      (user) =>
        !teamMemberIds.includes(user.id) &&
        !teamMemberIds.includes(Number(user.id))
    );
  }

  return {
    users,
    isLoading: state.isLoading,
    error: state.error,
    loadUsers,
    getUser,
    createUser,
    updateUser,
    deleteUser,
    getUsersNotInTeam,
  };
});
