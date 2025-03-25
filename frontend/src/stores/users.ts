import { defineStore } from "pinia";
import { useBaseStore } from "./base";
import type { User } from "@/types";
import { usersApi } from "@/api/users";
import { computed } from "vue";
import { useApiQuery } from "@/composables/useApiQuery";
import type { 
  APIErrorResponse, 
  isSuccessResponse 
} from "@/api/types";

interface UsersState {
  users: User[];
}

export const useUsersStore = defineStore("users", () => {
  const state = useBaseStore<UsersState>({
    users: [],
  });

  // Use our API query composable for loading state only
  const { isLoading: apiLoading } = useApiQuery();

  // Computed properties
  const users = computed(() => state.data.value.users);

  // Helper function to handle errors
  function handleError(error: Error | APIErrorResponse, operation: string) {
    console.error(`[${operation} Error]`, error);
    
    const errorMessage = error instanceof Error ? error.message : error.message;
    const errorType = error instanceof Error ? 'UnknownError' : (error.error_type || 'UnknownError');
    const errorData = error instanceof Error ? undefined : error.data;
    
    state.error.value = {
      message: errorMessage,
      error_type: errorType,
      data: errorData,
      operation
    };
    
    return { 
      success: false,
      error: state.error.value
    };
  }

  async function loadUsers(forceReload = false) {
    return await state.withLoading('loadUsers', async () => {
      try {
        // Skip loading if we already have users and not forcing reload
        if (!forceReload && state.data.value.users.length > 0) {
          return { success: true, data: state.data.value.users };
        }
        
        const response = await usersApi.listUsers();
        // Directly access the data array from successful response
        const usersData = response.status === 'success' ? response.data || [] : [];
        
        state.data.value.users = usersData;
        return { 
          success: true, 
          data: usersData
        };
      } catch (error) {
        return handleError(error as Error, 'loadUsers');
      }
    });
  }

  async function getUser(id: string) {
    return await state.withLoading(`getUser-${id}`, async () => {
      try {
        const response = await usersApi.getUser(id);
        return { success: true, data: response };
      } catch (error) {
        return handleError(error as Error, `getUser-${id}`);
      }
    });
  }

  async function createUser(data: {
    email: string;
    full_name: string;
    role: "admin" | "member";
  }) {
    return await state.withLoading('createUser', async () => {
      try {
        const response = await usersApi.createUser(data);
        
        if (response.user) {
          // Update local state
          state.data.value.users.push(response.user);
          
          // Reload users to ensure we have the latest data
          await loadUsers(true);
        }
        
        return { 
          success: true, 
          data: response,
          message: "User created successfully"
        };
      } catch (error) {
        return handleError(error as Error, 'createUser');
      }
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
      try {
        const response = await usersApi.updateUser(id, data);
        
        if (response.user) {
          // Update in local state
          const index = state.data.value.users.findIndex(
            (u) => u.id === response.user.id
          );
          if (index >= 0) {
            state.data.value.users[index] = response.user;
          }
        }
        
        return { 
          success: true, 
          data: response,
          message: "User updated successfully"
        };
      } catch (error) {
        return handleError(error as Error, `updateUser-${id}`);
      }
    });
  }

  async function deleteUser(id: string) {
    return await state.withLoading(`deleteUser-${id}`, async () => {
      try {
        await usersApi.deleteUser(id);
        
        // Update local state
        state.data.value.users = state.data.value.users.filter(
          (u) => u.id !== id
        );
        
        return { 
          success: true,
          message: "User deleted successfully"
        };
      } catch (error) {
        return handleError(error as Error, `deleteUser-${id}`);
      }
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
    isLoading: computed(() => apiLoading.value || state.isLoading.value),
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
