import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { createApp, defineComponent, nextTick, type App } from "vue";
import { createPinia } from "pinia";
import { createMemoryHistory, createRouter, type Router } from "vue-router";
import { i18n } from "@/i18n";

vi.mock("@/api/apiUtils", () => ({
  apiClient: {
    get: async () => ({ status: "success", data: {
      id: 1, name: "My dashboard", description: "", created_at: "", updated_at: "", can_edit: true,
      panels: { version: 1, panels: [], layout: [] },
    } }),
  },
}));

import DashboardView from "../DashboardView.vue";

async function settle() {
  await new Promise((resolve) => setTimeout(resolve, 0));
  await nextTick();
}

describe("DashboardView discard confirmation", () => {
  let app: App;
  let host: HTMLDivElement;
  let router: Router;

  beforeEach(async () => {
    vi.stubGlobal("ResizeObserver", class {
      observe() {}
      unobserve() {}
      disconnect() {}
    });
    vi.spyOn(window, "confirm").mockReturnValue(false);
    host = document.createElement("div");
    document.body.appendChild(host);
    router = createRouter({ history: createMemoryHistory(), routes: [
      { path: "/", component: { template: "<div />" } },
      { path: "/dashboards/:id", component: DashboardView },
      { path: "/dashboards", component: { template: "<div>Dashboard list</div>" } },
    ] });
    await router.push("/dashboards/1");
    app = createApp(defineComponent({ template: "<router-view />" }));
    app.use(createPinia()).use(i18n).use(router).mount(host);
    await settle();
    button("Edit").click();
    await nextTick();
    const name = host.querySelector<HTMLInputElement>('input[aria-label="Name"]');
    if (!name) throw new Error("dashboard name was not rendered");
    name.value = "My edited dashboard";
    name.dispatchEvent(new Event("input", { bubbles: true }));
    await nextTick();
  });

  afterEach(() => {
    app.unmount();
    host.remove();
    vi.restoreAllMocks();
    vi.unstubAllGlobals();
  });

  function button(text: string, root: ParentNode = host) {
    const match = [...root.querySelectorAll("button")].find((element) => element.textContent?.trim() === text);
    if (!match) throw new Error(`button ${text} was not rendered`);
    return match;
  }

  function dialogButton(text: string) {
    const dialog = document.querySelector('[role="alertdialog"]');
    if (!dialog) throw new Error("discard dialog was not rendered");
    return button(text, dialog);
  }

  it("keeps edits when the discard dialog is canceled and discards only on confirmation", async () => {
    button("Cancel").click();
    await settle();
    expect(document.querySelector('[role="alertdialog"]')).not.toBeNull();
    expect(window.confirm).not.toHaveBeenCalled();
    dialogButton("Cancel").click();
    await settle();
    expect(host.querySelector<HTMLInputElement>('input[aria-label="Name"]')?.value).toBe("My edited dashboard");
    button("Cancel").click();
    await settle();
    dialogButton("Discard changes").click();
    await settle();
    expect(host.querySelector("h1")?.textContent).toContain("My dashboard");
  });

  it("confirms in-app Back once before leaving the dashboard", async () => {
    host.querySelector<HTMLButtonElement>("button")?.click();
    await settle();
    expect(document.querySelector('[role="alertdialog"]')).not.toBeNull();
    expect(router.currentRoute.value.path).toBe("/dashboards/1");
    dialogButton("Discard changes").click();
    await settle();
    expect(router.currentRoute.value.path).toBe("/dashboards");
    expect(window.confirm).not.toHaveBeenCalled();
  });
});
