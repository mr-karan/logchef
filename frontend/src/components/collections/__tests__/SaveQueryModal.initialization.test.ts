import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { createApp, defineComponent, nextTick, ref, type App } from "vue";
import { createPinia } from "pinia";
import { createMemoryHistory, createRouter } from "vue-router";
import { i18n } from "@/i18n";

const network = vi.hoisted((): { releases: Array<() => void> } => ({ releases: [] }));
vi.mock("@/api/apiUtils", () => ({
  apiClient: {
    get: () => new Promise((resolve) => {
      network.releases.push(() => resolve({ status: "success", data: [] }));
    }),
  },
}));

import SaveQueryModal from "../SaveQueryModal.vue";

function query(withTimeRange: boolean) {
  return {
    id: 1,
    name: "Saved query",
    description: "",
    query_content: JSON.stringify({
      version: 2,
      content: 'level="info"',
      timeRange: withTimeRange ? { relative: "15m" } : null,
    }),
  };
}

describe("SaveQueryModal initialization", () => {
  let app: App;
  let host: HTMLDivElement;

  beforeEach(() => {
    localStorage.clear();
    network.releases = [];
    host = document.createElement("div");
    document.body.appendChild(host);
  });

  afterEach(() => {
    app?.unmount();
    host.remove();
  });

  async function mountModal() {
    const data = ref(query(true));
    const isOpen = ref(true);
    const router = createRouter({ history: createMemoryHistory(), routes: [{ path: "/", component: { template: "<div />" } }] });
    await router.push("/");
    app = createApp(defineComponent({
      components: { SaveQueryModal },
      setup: () => ({ data, isOpen }),
      template: '<SaveQueryModal :is-open="isOpen" :edit-data="data" />',
    }));
    app.use(createPinia()).use(i18n).use(router);
    app.mount(host);
    await nextTick();
    return { data, isOpen };
  }

  function checkbox() {
    const control = document.querySelector('[id="save_timestamp"][role="checkbox"]');
    if (!control) throw new Error("timestamp checkbox was not rendered");
    return control;
  }

  it("turns off the timestamp when editData changes to a query without a range", async () => {
    const { data } = await mountModal();
    expect(checkbox().getAttribute("aria-checked")).toBe("true");
    data.value = query(false);
    await nextTick();
    expect(checkbox().getAttribute("aria-checked")).toBe("false");
    data.value = query(true);
    await nextTick();
    expect(checkbox().getAttribute("aria-checked")).toBe("true");
  });

  it("preserves edits when the open prop changes on the same instance", async () => {
    const { isOpen } = await mountModal();
    const name = document.querySelector<HTMLInputElement>("#name");
    if (!name) throw new Error("query name was not rendered");
    name.value = "My edit";
    name.dispatchEvent(new Event("input", { bubbles: true }));
    isOpen.value = false;
    await nextTick();
    isOpen.value = true;
    await nextTick();
    expect(document.querySelector<HTMLInputElement>("#name")?.value).toBe("My edit");
  });

  it("shows initial values before metadata loads and preserves edits when loading finishes", async () => {
    await mountModal();
    const name = document.querySelector<HTMLInputElement>("#name");
    if (!name) throw new Error("query name was not rendered");
    expect(name.value).toBe("Saved query");
    name.value = "My edit";
    name.dispatchEvent(new Event("input", { bubbles: true }));
    network.releases.forEach((release) => release());
    await new Promise((resolve) => setTimeout(resolve, 0));
    await nextTick();
    expect(name.value).toBe("My edit");
  });
});
