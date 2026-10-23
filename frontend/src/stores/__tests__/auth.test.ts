import { beforeEach, describe, expect, it, vi } from "vitest";
import { createPinia, setActivePinia } from "pinia";

vi.mock("@/api/auth", () => ({
  authApi: { logout: vi.fn() },
}));

import { authApi } from "@/api/auth";
import router from "@/router";
import { useAuthStore } from "../auth";

describe("auth store logout", () => {
  beforeEach(() => {
    setActivePinia(createPinia());
  });

  it.each([
    ["succeeds", () => vi.mocked(authApi.logout).mockResolvedValue({ status: "success", data: undefined })],
    ["fails", () => vi.mocked(authApi.logout).mockRejectedValue(new Error("network"))],
  ])("lands on the login page marked as logged out when the request %s", async (_case, arrange) => {
    arrange();
    await useAuthStore().logout();
    await router.isReady();
    await vi.waitFor(() => expect(router.currentRoute.value.name).toBe("Login"));
    expect(router.currentRoute.value.query.logged_out).toBe("1");
  });
});
