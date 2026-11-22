import { handleUnauthorized } from "@/api/config";
import type { Router } from "vue-router";
import type { APIErrorResponse } from "@/api/types";

let handlingSessionExpiration = false;

export function expireSession({ announce }: { announce: boolean }): void {
  void handleUnauthorized({ announce });
}

export function createSessionExpirationHandler({ clearState, router }: {
  clearState: () => void;
  router: Router;
}): (options: { announce: boolean }) => Promise<APIErrorResponse> {
  return async ({ announce }) => {
    const isOnLoginPage = window.location.pathname.includes("/login") ||
      router.currentRoute.value.name === "Login";
    const sessionError: APIErrorResponse = {
      status: "error",
      message: "Your session has expired. Please log in again.",
      error_type: "AuthenticationError",
      _toastShown: isOnLoginPage || handlingSessionExpiration || announce,
    };

    if (!handlingSessionExpiration && !isOnLoginPage) {
      handlingSessionExpiration = true;
      const currentPath = router.options.history.location;
      try {
        clearState();
        await router.push({
          name: "Login",
          query: announce ? { redirect: currentPath, session_ended: "1" } : { redirect: currentPath },
          replace: true,
        }).catch(() => undefined);
      } finally {
        handlingSessionExpiration = false;
      }
    }
    return sessionError;
  };
}
