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

  async function mountModal(prop: "editData" | "initialData") {
    const data = ref(query(true));
    const router = createRouter({ history: createMemoryHistory(), routes: [{ path: "/", component: { template: "<div />" } }] });
    await router.push("/");
    app = createApp(defineComponent({
      components: { SaveQueryModal },
      setup: () => ({ data, binding: prop }),
      template: '<SaveQueryModal :is-open="true" :is-edit-mode="true" v-bind="{ [binding]: data }" />',
    }));
    app.use(createPinia()).use(i18n).use(router);
    app.mount(host);
    await nextTick();
    return data;
  }

  function checkbox() {
    const control = document.querySelector('[id="save_timestamp"][role="checkbox"]');
    if (!control) throw new Error("timestamp checkbox was not rendered");
    return control;
  }

  it.each(["editData", "initialData"] as const)("turns off the timestamp when %s changes to a query without a range", async (prop) => {
    const data = await mountModal(prop);
    expect(checkbox().getAttribute("aria-checked")).toBe("true");
    data.value = query(false);
    await nextTick();
    expect(checkbox().getAttribute("aria-checked")).toBe("false");
    data.value = query(true);
    await nextTick();
    expect(checkbox().getAttribute("aria-checked")).toBe("true");
  });

  it("shows initial values before metadata loads and preserves edits when loading finishes", async () => {
    await mountModal("editData");
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
