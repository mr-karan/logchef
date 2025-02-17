import { defineStore } from "pinia";
import { ref } from "vue";
import { useRouter } from "vue-router";
import type { Session, User } from "@/types";
import { authApi } from "@/api/auth";

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
  const router = useRouter();
  const user = ref<User | null>(null);
  const session = ref<Session | null>(null);
  const isAuthenticated = ref(false);
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
        isAuthenticated.value = true;
        console.log("Auth initialized successfully:", {
          user: user.value,
          isAuthenticated: isAuthenticated.value,
        });
      } else {
        // Handle session not found gracefully
        user.value = null;
        session.value = null;
        isAuthenticated.value = false;
        console.log("No active session found");
      }
    } catch (error) {
      console.error("Failed to initialize auth:", error);
      user.value = null;
      session.value = null;
      isAuthenticated.value = false;
    } finally {
      isInitialized.value = true;
      isInitializing.value = false;
    }
  }

  // Login user
  async function login() {
    // Get current route's redirect query param
    const redirectPath = router.currentRoute.value.query.redirect as string;

    // Redirect to backend login endpoint with redirect_uri
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
      // Always clear state
      user.value = null;
      session.value = null;
      isAuthenticated.value = false;
      // Redirect to login page
      await router.push("/");
    }
  }

  return {
    user,
    session,
    isAuthenticated,
    isInitialized,
    initialize,
    login,
    logout,
  };
});
