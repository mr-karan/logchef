import { defineStore } from "pinia";
import { useBaseStore } from "./base";
import type { User } from "@/types";
import { usersApi, type UpdateUserRequest } from "@/api/users";
import { computed } from "vue";
import { i18n } from "@/i18n";

interface UsersState {
  users: User[];
}

export const useUsersStore = defineStore("users", () => {
  const state = useBaseStore<UsersState>({
    users: [],
  });
  
  // Define users as a computed property that returns the array directly
  const users = computed(() => state.data.value.users || []);

  // Use the centralized error handler from base store

  async function loadUsers(forceReload = false) {
    // Skip loading if we already have users and not forcing reload
    if (!forceReload && state.data.value.users && state.data.value.users.length > 0) {
      return { success: true, data: state.data.value.users };
    }
    return await state.callApi({
      apiCall: () => usersApi.listUsers(),
      operationKey: 'loadUsers',
      onSuccess: (response) => {
        // Store the users array from the response - without trying to access response.data
        // because callApi already extracts the data property from the response
        state.data.value.users = (response as User[]) || [];
      },
      showToast: false,
    });
  }

  async function getUser(id: string) {
    return await state.withLoading(`getUser-${id}`, async () => {
      try {
        const response = await usersApi.getUser(id);
        return { success: true, data: response };
      } catch (error) {
        return state.handleError(error as Error, `getUser-${id}`);
      }
    });
  }

  async function createUser(data: {
    email: string;
    full_name: string;
    role: "admin" | "member";
  }) {
    return await state.callApi({
      apiCall: () => usersApi.createUser(data),
      successMessage: i18n.global.t("access.userCreated"),
      operationKey: 'createUser',
      onSuccess: async () => {
        await loadUsers(true);
      },
    });
  }

  async function updateUser(
    id: string,
    data: UpdateUserRequest
  ) {
    return await state.callApi({
      apiCall: () => usersApi.updateUser(id, data),
      successMessage: i18n.global.t("access.userUpdated"),
      operationKey: `updateUser-${id}`,
      onSuccess: async () => {
        await loadUsers(true);
      },
    });
  }

  async function deleteUser(id: string) {
    return await state.callApi({
      apiCall: () => usersApi.deleteUser(id),
      successMessage: i18n.global.t("access.userDeleted"),
      operationKey: `deleteUser-${id}`,
      onSuccess: async () => {
        await loadUsers(true);
      },
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
    // State
    users,
    isLoading: state.isLoading,
    error: state.error,
    
    // Actions
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
