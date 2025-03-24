import { defineStore } from "pinia";
import { useBaseStore } from "./base";
import type { User } from "@/types";
import { usersApi } from "@/api/users";
import { computed } from "vue";
import type { APIResponse } from "@/api/types";

interface UsersState {
  users: User[];
}

export const useUsersStore = defineStore("users", () => {
  const state = useBaseStore<UsersState>({
    users: [],
  });

  // Computed properties
  const users = computed(() => {
    console.log("Users computed value:", state.data.value.users);
    return state.data.value.users;
  });

  async function loadUsers() {
    console.log("Loading users...");
    const result = await state.callApi<APIResponse<User[]>>({
      apiCall: () => usersApi.listUsers(),
      onSuccess: (response) => {
        console.log("API Response:", response);
        if (Array.isArray(response)) {
          state.data.value.users = response;
          console.log("Updated users state:", state.data.value.users);
        }
      },
      showToast: true,
    });
    console.log("Load users result:", result);
    return result;
  }

  async function getUser(id: string) {
    return await state.callApi<APIResponse<{ user: User }>>({
      apiCall: () => usersApi.getUser(id),
      showToast: true,
    });
  }

  async function createUser(data: {
    email: string;
    full_name: string;
    role: "admin" | "member";
  }) {
    const result = await state.callApi<APIResponse<{ user: User }>>({
      apiCall: () => usersApi.createUser(data),
      onSuccess: (response) => {
        if (response.status === "success" && response.data.user) {
          state.data.value.users.push(response.data.user);
        }
      },
      successMessage: "User created successfully",
    });

    if (result.success) {
      await loadUsers();
    }

    return result;
  }

  async function updateUser(
    id: string,
    data: {
      full_name?: string;
      email?: string;
      role?: "admin" | "member";
      status?: "active" | "inactive";
    }
  ) {
    return await state.callApi<APIResponse<{ user: User }>>({
      apiCall: () => usersApi.updateUser(id, data),
      onSuccess: (response) => {
        if (response.status === "success" && response.data.user) {
          const index = state.data.value.users.findIndex(
            (u) => u.id === response.data.user.id
          );
          if (index >= 0) {
            state.data.value.users[index] = response.data.user;
          }
        }
      },
      successMessage: "User updated successfully",
    });
  }

  async function deleteUser(id: string) {
    return await state.callApi<APIResponse<{ message: string }>>({
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
