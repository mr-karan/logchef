import { defineStore } from "pinia";
import { useBaseStore } from "./base";
import type { User } from "@/types";
import { usersApi } from "@/api/users";
import { computed } from "vue";
import { useApiQuery } from "@/composables/useApiQuery";

interface UsersState {
  users: User[];
}

export const useUsersStore = defineStore("users", () => {
  const state = useBaseStore<UsersState>({
    users: [],
  });

  // Use our API query composable
  const { execute, isLoading } = useApiQuery<User[]>();

  // Computed properties
  const users = computed(() => state.data.value.users);

  async function loadUsers() {
    console.log("Loading users...");
    const result = await state.withLoading('loadUsers', async () => {
      return await execute(() => usersApi.listUsers(), {
        onSuccess: (response) => {
          state.data.value.users = response ?? [];
        },
        defaultData: [],
        showToast: true,
      });
    });
    
    return result;
  }

  async function getUser(id: string) {
    return await state.withLoading(`getUser-${id}`, async () => {
      return await execute(() => usersApi.getUser(id), {
        showToast: true,
      });
    });
  }

  async function createUser(data: {
    email: string;
    full_name: string;
    role: "admin" | "member";
  }) {
    const result = await state.withLoading('createUser', async () => {
      return await execute(() => usersApi.createUser(data), {
        successMessage: "User created successfully",
        onSuccess: (response) => {
          if (response.user) {
            state.data.value.users.push(response.user);
          }
        },
      });
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
    return await state.withLoading(`updateUser-${id}`, async () => {
      return await execute(() => usersApi.updateUser(id, data), {
        successMessage: "User updated successfully",
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
      });
    });
  }

  async function deleteUser(id: string) {
    return await state.withLoading(`deleteUser-${id}`, async () => {
      return await execute(() => usersApi.deleteUser(id), {
        successMessage: "User deleted successfully",
        onSuccess: () => {
          state.data.value.users = state.data.value.users.filter(
            (u) => u.id !== id
          );
        },
      });
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
    isLoading: computed(() => isLoading.value || state.isLoading.value),
    error: state.error,
    loadUsers,
    getUser,
    createUser,
    updateUser,
    deleteUser,
    getUsersNotInTeam,
    // Loading state helpers
    isLoadingOperation: state.isLoadingOperation,
  };
});
