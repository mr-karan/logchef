import type { Router } from "vue-router";
import { showErrorToast } from "@/api/error-handler";
import type { APIErrorResponse } from "@/api/types";

let handlingSessionExpiration = false;

export async function expireSession({ toast, clearState, router }: {
  toast: boolean;
  clearState: () => void;
  router: Router;
}): Promise<APIErrorResponse> {
  const isOnLoginPage = window.location.pathname.includes("/login") ||
    router.currentRoute.value.name === "Login";
  const sessionError: APIErrorResponse = {
    status: "error",
    message: "Your session has expired. Please log in again.",
    error_type: "AuthenticationError",
    _toastShown: isOnLoginPage || handlingSessionExpiration || toast,
  };

  if (!handlingSessionExpiration && !isOnLoginPage) {
    handlingSessionExpiration = true;
    const currentPath = router.options.history.location;
    try {
      clearState();
      if (toast) showErrorToast(sessionError);
      await router.push({
        name: "Login",
        query: { redirect: currentPath },
        replace: true,
      }).catch(() => undefined);
    } finally {
      handlingSessionExpiration = false;
    }
  }
  return sessionError;
}
