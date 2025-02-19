import { defineStore } from "pinia";
import { ref, computed } from "vue";
import type { Session, User } from "@/types";
import { authApi } from "@/api/auth";
import router from "@/router";
import type { AxiosError } from "axios";

interface SessionResponse {
  status: "success";
  data: {
    user: User;
    session: Session;
  };
}

interface ErrorResponse {
  status: "error";
  data: {
    error: string;
  };
}

type ApiResponse = SessionResponse | ErrorResponse;

export const useAuthStore = defineStore("auth", () => {
  const user = ref<User | null>(null);
  const session = ref<Session | null>(null);
  const isAuthenticated = computed(() => !!user.value);
  const isInitialized = ref(false);
  const isInitializing = ref(false);

  // Initialize auth state from session cookie
  async function initialize() {
    if (isInitialized.value || isInitializing.value) {
      return;
    }

    console.log("Initializing auth store...");
    isInitializing.value = true;

    try {
      console.log("Fetching session...");
      const response = (await authApi.getSession()) as ApiResponse;
      console.log("Session response:", response);

      if (response.status === "success") {
        user.value = response.data.user;
        session.value = response.data.session;
        console.log("Auth initialized successfully:", {
          user: user.value,
          isAuthenticated: isAuthenticated.value,
        });
      } else {
        // Handle session not found gracefully
        await clearState();
        console.log("No active session found");
      }
    } catch (error) {
      // Only log error if not a 401 (unauthorized)
      const axiosError = error as AxiosError;
      if (axiosError.response?.status !== 401) {
        console.error("Failed to initialize auth:", error);
      }
      await clearState();
    } finally {
      isInitialized.value = true;
      isInitializing.value = false;
    }
  }

  // Clear auth state
  async function clearState() {
    user.value = null;
    session.value = null;
  }

  // Start OIDC login flow
  async function startLogin(redirectPath?: string) {
    window.location.href = authApi.getLoginUrl(redirectPath);
  }

  // Logout user
  async function logout() {
    try {
      // Call backend logout endpoint
      await authApi.logout();
    } catch (error) {
      console.error("Failed to logout:", error);
    } finally {
      // Always clear state and redirect to login
      await clearState();
      router.push({ name: "Login" });
    }
  }

  return {
    user,
    session,
    isAuthenticated,
    isInitialized,
    initialize,
    startLogin,
    logout,
    clearState,
  };
});
