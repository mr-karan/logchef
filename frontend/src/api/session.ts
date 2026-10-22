import { showErrorToast } from "@/api/error-handler";
import { useAuthStore } from "@/stores/auth";
import router from "@/router";

export function expireSession({ toast }: { toast: boolean }): void {
  useAuthStore().clearState();
  const isOnLoginPage = window.location.pathname.includes("/login") || router.currentRoute.value.name === "Login";
  if (toast && !isOnLoginPage) {
    showErrorToast({ status: "error", message: "Your session has expired. Please log in again.", error_type: "AuthenticationError" });
  }
  router.push({ name: "Login", query: { redirect: router.options.history.location }, replace: true });
}
