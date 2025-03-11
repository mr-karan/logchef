import { defineStore } from "pinia";
import { computed } from "vue";
import type { Session, User } from "@/types";
import { authApi, type SessionResponse } from "@/api/auth";
import router from "@/router";
import { useBaseStore } from "./base";

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

  // Computed properties
  const user = computed(() => state.data.value.user);
  const session = computed(() => state.data.value.session);
  const isAuthenticated = computed(() => !!user.value);
  const isInitialized = computed(() => state.data.value.isInitialized);
  const isInitializing = computed(() => state.data.value.isInitializing);

  // Initialize auth state from session cookie
  async function initialize() {
    if (isInitialized.value || isInitializing.value) {
      return;
    }

    console.log("Initializing auth store...");
    state.data.value.isInitializing = true;

    const result = await state.callApi<SessionResponse>({
      apiCall: () => authApi.getSession(),
      showToast: false,
      onSuccess: (response) => {
        state.data.value.user = response.user;
        state.data.value.session = response.session;
        console.log("Auth initialized successfully:", {
          user: user.value,
          isAuthenticated: isAuthenticated.value,
        });
      },
      onError: () => {
        // Handle session not found gracefully
        clearState();
        console.log("No active session found");
      },
    });

    state.data.value.isInitialized = true;
    state.data.value.isInitializing = false;

    return result;
  }

  // Clear auth state
  function clearState() {
    state.data.value.user = null;
    state.data.value.session = null;
  }

  // Start OIDC login flow
  function startLogin(redirectPath?: string) {
    window.location.href = authApi.getLoginUrl(redirectPath);
  }

  // Logout user
  async function logout() {
    const result = await state.callApi({
      apiCall: () => authApi.logout(),
      showToast: false,
    });

    // Always clear state and redirect to login, regardless of API result
    clearState();
    router.push({ name: "Login" });

    return result;
  }

  return {
    user,
    session,
    isAuthenticated,
    isInitialized,
    isInitializing,
    initialize,
    startLogin,
    logout,
    clearState,
  };
});
