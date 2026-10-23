import { describe, expect, it, vi } from "vitest";
import { createApp } from "vue";
import { createPinia, setActivePinia } from "pinia";
import { RouterView } from "vue-router";
import { i18n } from "@/i18n";
import router from "@/router";

describe("logout page", () => {
  it("lands on the login page marked as logged out when the session is already gone", async () => {
    const pinia = createPinia();
    setActivePinia(pinia);
    const app = createApp(RouterView).use(pinia).use(router).use(i18n);
    app.mount(document.createElement("div"));
    try {
      await router.push("/logout");
      await vi.waitFor(() => expect(router.currentRoute.value.name).toBe("Login"), { timeout: 5000 });
      expect(router.currentRoute.value.query.logged_out).toBe("1");
    } finally {
      app.unmount();
    }
  });
});
