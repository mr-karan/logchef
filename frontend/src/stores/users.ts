import { defineStore } from "pinia";
import { useBaseStore } from "./base";
import type { User } from "@/types";
import { usersApi } from "@/api/users";
import { computed } from "vue";
import type { APIErrorResponse } from "@/api/types";

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
    return await state.withLoading('loadUsers', async () => {
      // Skip loading if we already have users and not forcing reload
      if (!forceReload && state.data.value.users && state.data.value.users.length > 0) {
        return { success: true, data: state.data.value.users };
      }
      
      console.log("Loading users from API...");
      return await state.callApi({
        apiCall: () => usersApi.listUsers(),
        operationKey: 'loadUsers',
        onSuccess: (response) => {
          console.log("User API response:", response);
          // Store the users array from the response - without trying to access response.data
          // because callApi already extracts the data property from the response
          state.data.value.users = response || [];
        },
        showToast: false,
      });
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
    return await state.withLoading('createUser', async () => {
      return await state.callApi({
        apiCall: () => usersApi.createUser(data),
        successMessage: "User created successfully",
        operationKey: 'createUser',
        onSuccess: (response) => {
          if (response) {
            // Create a new array instead of modifying the existing one for better reactivity
            const newUsers = [response, ...(state.data.value.users || [])];
            state.data.value.users = newUsers;
            console.log("User added to store:", response);
          }
        }
      });
    });
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
      return await state.callApi({
        apiCall: () => usersApi.updateUser(id, data),
        successMessage: "User updated successfully",
        operationKey: `updateUser-${id}`,
        onSuccess: (response) => {
          if (response?.status === 'success' && response.data?.user) {
            // Update in local state
            const index = state.data.value.users.findIndex(
              (u) => u.id === response.data.user.id
            );
            if (index >= 0) {
              state.data.value.users[index] = response.data.user;
              console.log("User updated in store:", response.data.user);
            }
          }
        }
      });
    });
  }

  async function deleteUser(id: string) {
    return await state.withLoading(`deleteUser-${id}`, async () => {
      return await state.callApi({
        apiCall: () => usersApi.deleteUser(id),
        successMessage: "User deleted successfully",
        operationKey: `deleteUser-${id}`,
        onSuccess: () => {
          // Update local state
          state.data.value.users = state.data.value.users.filter(
            (u) => u.id !== id
          );
        }
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
