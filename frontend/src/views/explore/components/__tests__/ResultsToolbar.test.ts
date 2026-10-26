import { createApp, h, nextTick } from "vue";
import { createPinia, setActivePinia } from "pinia";
import { afterEach, expect, it } from "vitest";
import { i18n } from "@/i18n";
import ResultsToolbar from "../ResultsToolbar.vue";

let unmount: (() => void) | null = null;

afterEach(() => {
  unmount?.();
  unmount = null;
  document.body.innerHTML = "";
});

async function mount(props: Record<string, unknown>) {
  const pinia = createPinia();
  setActivePinia(pinia);
  const added: string[] = [];
  const host = document.createElement("div");
  document.body.append(host);
  const app = createApp({
    render: () =>
      h(ResultsToolbar, {
        isHistogramVisible: true, availableFields: [], displayMode: "table", logsCount: 100,
        onAddFastFilter: (field: string) => added.push(field),
        ...props,
      }),
  }).use(pinia).use(i18n);
  app.mount(host);
  unmount = () => app.unmount();
  await nextTick();
  return { host, added };
}

it("offers each unfiltered sort key as a one-click filter", async () => {
  const { host, added } = await mount({ fastFilterFields: ["namespace", "job_name"], canAddFastFilter: true });

  expect(host.textContent).toContain(i18n.global.t("explore.fastFilterLead"));
  host.querySelector<HTMLButtonElement>(`button[aria-label="${i18n.global.t("explore.addFastFilter", { field: "job_name" })}"]`)?.click();

  expect(added).toEqual(["job_name"]);
});

it("names the fields without buttons when the query mode cannot insert a filter", async () => {
  const { host } = await mount({ fastFilterFields: ["namespace"], canAddFastFilter: false });

  expect(host.querySelector("code")?.textContent).toBe("namespace");
  expect(host.querySelector(`button[aria-label="${i18n.global.t("explore.addFastFilter", { field: "namespace" })}"]`)).toBeNull();
});

it("shows no hint when every query already filters on a sort key", async () => {
  const { host } = await mount({ fastFilterFields: [] });

  expect(host.textContent).not.toContain(i18n.global.t("explore.fastFilterLead"));
});
