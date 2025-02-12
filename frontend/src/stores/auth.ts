import { defineStore } from "pinia";
import { ref } from "vue";
import { useRouter } from "vue-router";
import type { Session, User } from "@/types";
import { authApi } from "@/api/auth";

export const useAuthStore = defineStore("auth", () => {
  const router = useRouter();
  const user = ref<User | null>(null);
  const session = ref<Session | null>(null);
  const isAuthenticated = ref(false);

  // Initialize auth state from session cookie
  async function initialize() {
    try {
      const response = await authApi.getSession();
      if (response.status === "success") {
        user.value = response.data.user;
        session.value = response.data.session;
        isAuthenticated.value = true;
        return true;
      }
      return false;
    } catch (error) {
      console.error("Failed to initialize auth:", error);
      return false;
    }
  }

  // Login user
  async function login() {
    // Redirect to backend login endpoint
    window.location.href = authApi.getLoginUrl();
  }

  // Logout user
  async function logout() {
    try {
      // Call backend logout endpoint
      await authApi.logout();

      // Clear state
      user.value = null;
      session.value = null;
      isAuthenticated.value = false;

      // Redirect to login page
      await router.push("/");
    } catch (error) {
      console.error("Failed to logout:", error);
      // Even if backend logout fails, clear state and redirect
      user.value = null;
      session.value = null;
      isAuthenticated.value = false;
      await router.push("/");
    }
  }

  return {
    user,
    session,
    isAuthenticated,
    initialize,
    login,
    logout,
  };
});
