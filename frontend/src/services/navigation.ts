import router from "@/router";

export const navigationService = {
  /**
   * Navigate to login page with optional redirect
   */
  goToLogin(redirectPath?: string) {
    return router.push({
      path: "/",
      query: redirectPath ? { redirect: redirectPath } : undefined,
    });
  },

  /**
   * Navigate to forbidden page
   */
  goToForbidden() {
    return router.push({ name: "Forbidden" });
  },

  /**
   * Navigate to explore page
   */
  goToExplore() {
    return router.push({ name: "Explore" });
  },
};
