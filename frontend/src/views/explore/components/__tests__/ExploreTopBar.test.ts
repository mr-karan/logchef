import { createApp, nextTick } from "vue";
import { createPinia, setActivePinia } from "pinia";
import { afterEach, expect, it, vi } from "vitest";
import { i18n } from "@/i18n";
import { sourcesApi, type Source } from "@/api/sources";
import { useExploreStore } from "@/stores/explore";
import { usePreferencesStore } from "@/stores/preferences";
import ExploreTopBar from "../ExploreTopBar.vue";

const shownSource: Source = {
  id: 3, name: "Checkout", source_type: "clickhouse", _meta_is_auto_created: false,
  _meta_ts_field: "timestamp", connection: { host: "", database: "logs", table_name: "checkout" },
  ttl_days: 30, created_at: "", updated_at: "", is_connected: true,
};

afterEach(() => {
  vi.restoreAllMocks();
  localStorage.clear();
});

it.each([
  { teamId: 1, sourceId: 4 },
  { teamId: 2, sourceId: null },
])("copies the shown query during a pending pick to $teamId/$sourceId", async selection => {
  const pinia = createPinia();
  setActivePinia(pinia);
  vi.spyOn(sourcesApi, "listTeamSources").mockResolvedValue({ status: "success", data: [shownSource] });
  vi.spyOn(sourcesApi, "getTeamSource").mockResolvedValue({ status: "success", data: shownSource });
  useExploreStore().open({ teamId: 1, sourceId: 3, content: {
    kind: "query", mode: "logchefql", text: 'service="checkout"', window: { kind: "relative", spec: "1h" }, limit: 100,
  } });
  const writeText = vi.fn<(text: string) => Promise<void>>().mockResolvedValue();
  const clipboard = Object.getOwnPropertyDescriptor(navigator, "clipboard");
  Object.defineProperty(navigator, "clipboard", { value: { writeText }, configurable: true });
  const host = document.createElement("div");
  document.body.append(host);
  const app = createApp(ExploreTopBar, {
    selection,
    availableTeams: [], availableSources: [], selectedSource: shownSource,
  }).use(pinia).use(i18n);
  app.mount(host);
  try {
    const button = host.querySelector("svg.lucide-terminal")?.closest("button");
    expect(button).toBeTruthy();
    button?.click();
    expect(writeText).toHaveBeenCalledOnce();
    expect(writeText.mock.lastCall?.[0]).toContain("-t 1 -S 3");
    expect(writeText.mock.lastCall?.[0]).toContain('service="checkout"');
  } finally {
    app.unmount();
    host.remove();
    if (clipboard) Object.defineProperty(navigator, "clipboard", clipboard);
    else Reflect.deleteProperty(navigator, "clipboard");
  }
});

it("switches the timezone while native SQL owns the time range", async () => {
  const pinia = createPinia();
  setActivePinia(pinia);
  useExploreStore().open({ teamId: 1, sourceId: 3, content: {
    kind: "query", mode: "native", text: "SELECT 1", window: { kind: "relative", spec: "1h" }, limit: 100,
  } });
  const host = document.createElement("div");
  document.body.append(host);
  const app = createApp(ExploreTopBar, {
    selection: { teamId: 1, sourceId: 3 },
    availableTeams: [], availableSources: [], selectedSource: shownSource,
  }).use(pinia).use(i18n);
  app.mount(host);
  try {
    const buttons = [...host.querySelectorAll("button")];
    expect(buttons.find(b => b.querySelector("svg.lucide-clock"))?.disabled).toBe(true);
    buttons.find(b => b.textContent?.trim() === "UTC")?.click();
    await nextTick();
    expect(usePreferencesStore().preferences.timezone).toBe("utc");
  } finally {
    app.unmount();
    host.remove();
  }
});

it.each([
  { trigger: "ui.resultLimit", selected: "100 rows" },
  { trigger: "ui.queryTimeout", selected: "30s" },
])("announces the selected value in the $trigger menu", async ({ trigger, selected }) => {
  const pinia = createPinia();
  setActivePinia(pinia);
  useExploreStore().open({ teamId: 1, sourceId: 3, content: {
    kind: "query", mode: "logchefql", text: "", window: { kind: "relative", spec: "1h" }, limit: 100,
  } });
  const host = document.createElement("div");
  document.body.append(host);
  const app = createApp(ExploreTopBar, {
    selection: { teamId: 1, sourceId: 3 },
    availableTeams: [], availableSources: [], selectedSource: shownSource,
  }).use(pinia).use(i18n);
  app.mount(host);
  try {
    const button = host.querySelector<HTMLButtonElement>(`button[title="${i18n.global.t(trigger)}"]`);
    expect(button).toBeTruthy();
    button?.dispatchEvent(new KeyboardEvent("keydown", { key: "Enter", bubbles: true }));
    await vi.waitFor(() => expect(document.querySelector("[role=menu]")).not.toBeNull());
    const checked = [...document.querySelectorAll("[role=menuitemradio][aria-checked=true]")].map(item => item.textContent?.trim());
    expect(checked).toEqual([selected]);
  } finally {
    app.unmount();
    host.remove();
  }
});
