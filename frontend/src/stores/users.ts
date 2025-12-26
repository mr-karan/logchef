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
          state.data.value.users = (response as User[]) || [];
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
      const result = await state.callApi({
        apiCall: () => usersApi.createUser(data),
        successMessage: "User created successfully",
        operationKey: 'createUser',
      });
      
      if (result && result.success) {
        // Reload all users from backend to ensure frontend state is in sync
        await loadUsers(true);
        console.log("Users reloaded after creating new user");
      }
      
      return result;
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
      const result = await state.callApi({
        apiCall: () => usersApi.updateUser(id, data),
        successMessage: "User updated successfully",
        operationKey: `updateUser-${id}`,
      });
      
      if (result && result.success) {
        // Reload all users from backend to ensure frontend state is in sync
        await loadUsers(true);
        console.log("Users reloaded after updating user");
      }
      
      return result;
    });
  }

  async function deleteUser(id: string) {
    return await state.withLoading(`deleteUser-${id}`, async () => {
      const result = await state.callApi({
        apiCall: () => usersApi.deleteUser(id),
        successMessage: "User deleted successfully",
        operationKey: `deleteUser-${id}`,
      });
      
      if (result && result.success) {
        // Reload all users from backend to ensure frontend state is in sync
        await loadUsers(true);
        console.log("Users reloaded after deleting user");
      }
      
      return result;
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
