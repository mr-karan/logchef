import { defineStore } from "pinia";
import { computed } from "vue";
import type { Session, User } from "@/types";
import { authApi, type SessionResponse } from "@/api/auth";
import router from "@/router";
import { useBaseStore } from "./base";
import { useApiQuery } from "@/composables/useApiQuery";
import { useMetaStore } from "./meta";
import { useContextStore } from "./context";
import type { APIErrorResponse } from "@/api/types";

interface AuthState {
  user: User | null;
  session: Session | null;
  isInitialized: boolean;
  isInitializing: boolean;
}

export const useAuthStore = defineStore("auth", () => {
  const state = useBaseStore<AuthState>({
    user: null,
    session: null,
    isInitialized: false,
    isInitializing: false,
  });

  // Use our API query composable for loading state only
  const { isLoading: apiLoading } = useApiQuery<SessionResponse>();

  // Computed properties
  const user = computed(() => state.data.value.user);
  const session = computed(() => state.data.value.session);
  const isAuthenticated = computed(() => !!user.value);
  const isInitialized = computed(() => state.data.value.isInitialized);
  const isInitializing = computed(() => state.data.value.isInitializing);
  const error = computed(() => state.error.value);

  // Initialize auth state from session cookie
  async function initialize() {
    if (isInitialized.value || isInitializing.value) {
      return { success: false, message: "Already initializing" };
    }

    return await state.withLoading('initialize', async () => {
      try {
        console.log("Initializing auth store...");
        state.data.value.isInitializing = true;

        // Load server meta information first (it's a public endpoint)
        const metaStore = useMetaStore();
        await metaStore.loadMeta();

        const result = await state.callApi({
          apiCall: () => authApi.getSession(),
          showToast: false,
          operationKey: 'initialize',
          onSuccess: (response: SessionResponse | null) => {
            if (response) {
              state.data.value.user = response.user;
              state.data.value.session = response.session;
              // Mark that we've had a successful session for future reference
              sessionStorage.setItem("hadPreviousSession", "true");
              console.log("Auth initialized successfully:", {
                user: user.value,
                isAuthenticated: isAuthenticated.value,
              });
            }
          },
          onError: () => {
            // Handle session not found gracefully
            clearState();
            // Log error for diagnostic purposes but don't show to user for initial load
            console.log("No active session found");
          },
        });

        state.data.value.isInitialized = true;
        return result;
      } catch (error) {
        return state.handleError(error as Error | APIErrorResponse, 'initialize');
      } finally {
        state.data.value.isInitializing = false;
      }
    });
  }

  // Clear auth state
  function clearState() {
    state.data.value.user = null;
    state.data.value.session = null;

    // On manual logout, we should also clear the session flag
    // but we don't do this on auth failures to distinguish between first visit and actual expiry
    if (state.data.value.isInitialized) {
      sessionStorage.removeItem("hadPreviousSession");
    }
  }

  // Start OIDC login flow
  async function startLogin(redirectPath?: string) {
    return await state.withLoading('startLogin', async () => {
      try {
        const loginUrl = authApi.getLoginUrl(redirectPath);
        window.location.href = loginUrl;
        return { success: true };
      } catch (error) {
        console.error("Failed to initiate login:", error);
        return state.handleError(error as Error | APIErrorResponse, 'startLogin');
      }
    });
  }

  async function logout() {
    return await state.withLoading('logout', async () => {
      try {
        const result = await state.callApi({
          apiCall: () => authApi.logout(),
          showToast: false,
          operationKey: 'logout'
        });

        clearState();
        useContextStore().clear();
        router.push({ name: "Login" });

        return result;
      } catch (error) {
        clearState();
        useContextStore().clear();
        router.push({ name: "Login" });
        return state.handleError(error as Error | APIErrorResponse, 'logout');
      }
    });
  }

  return {
    user,
    session,
    isAuthenticated,
    isInitialized,
    isInitializing,
    error,
    initialize,
    startLogin,
    logout,
    clearState,
    // Loading state helpers
    isLoading: computed(() => apiLoading.value || state.isLoading.value),
    isLoadingOperation: state.isLoadingOperation,
  };
});
