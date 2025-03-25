import { defineStore } from "pinia";
import { computed } from "vue";
import type { Session, User } from "@/types";
import { authApi, type SessionResponse } from "@/api/auth";
import router from "@/router";
import { useBaseStore } from "./base";
import { useApiQuery } from "@/composables/useApiQuery";

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

  // Use our API query composable
  const { execute } = useApiQuery<SessionResponse>();

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

    const result = await execute(() => authApi.getSession(), {
      showToast: false,
      onSuccess: (response) => {
        state.data.value.user = response.user;
        state.data.value.session = response.session;
        // Mark that we've had a successful session for future reference
        sessionStorage.setItem("hadPreviousSession", "true");
        console.log("Auth initialized successfully:", {
          user: user.value,
          isAuthenticated: isAuthenticated.value,
        });
      },
      onError: () => {
        // Handle session not found gracefully
        clearState();
        // Log error for diagnostic purposes but don't show to user for initial load
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
    
    // On manual logout, we should also clear the session flag
    // but we don't do this on auth failures to distinguish between first visit and actual expiry
    if (state.data.value.isInitialized) {
      sessionStorage.removeItem("hadPreviousSession");
    }
  }

  // Start OIDC login flow
  function startLogin(redirectPath?: string) {
    window.location.href = authApi.getLoginUrl(redirectPath);
  }

  // Logout user
  async function logout() {
    const result = await execute(() => authApi.logout(), {
      showToast: false
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
