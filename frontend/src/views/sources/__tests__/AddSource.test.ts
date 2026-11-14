import { afterEach, describe, expect, it, vi } from "vitest";
import { createApp, nextTick } from "vue";
import { createPinia } from "pinia";
import { i18n } from "@/i18n";
import type { Source } from "@/api/sources";

const source: Source = {
  id: 7, name: "logs", source_type: "clickhouse", _meta_is_auto_created: false,
  _meta_ts_field: "timestamp", ttl_days: 7, created_at: "", updated_at: "", is_connected: true,
  connection: { host: "original.example:9000", username: "reader", has_password: true, database: "default", table_name: "logs", tls_enable: true, activity: { allow_view: true } },
};
const update = vi.fn((..._args: unknown[]) => Promise.resolve({ status: "success", data: source }));
vi.mock("@/api/sources", async (importOriginal) => ({
  ...(await importOriginal<typeof import("@/api/sources")>()),
  sourcesApi: { listAllSourcesForAdmin: () => Promise.resolve({ status: "success", data: [source] }), updateSource: (...args: unknown[]) => update(...args) },
}));
vi.mock("vue-router", () => ({ useRoute: () => ({ params: { sourceId: "7" }, query: {} }), useRouter: () => ({ push: vi.fn() }) }));
const { default: AddSource } = await import("../AddSource.vue");
let unmount: (() => void) | undefined;
afterEach(() => { unmount?.(); document.body.replaceChildren(); vi.clearAllMocks(); });
async function flush() {
  for (let i = 0; i < 5; i++) { await new Promise((resolve) => setTimeout(resolve, 0)); await nextTick(); }
}
async function render() {
  const root = document.createElement("div");
  document.body.appendChild(root);
  const app = createApp(AddSource);
  app.use(createPinia()).use(i18n).mount(root);
  unmount = () => app.unmount();
  await flush();
  const activityToggle = [...root.querySelectorAll<HTMLButtonElement>("button")].find((button) => button.textContent?.includes("Recent activity on ClickHouse views"));
  if (!activityToggle) throw new Error("Missing activity settings toggle");
  activityToggle.click();
  await flush();
  return root;
}
async function input(root: HTMLElement, id: string, value: string) {
  const field = root.querySelector<HTMLInputElement>(`#${id}`);
  if (!field) throw new Error(`Missing input ${id}`);
  field.value = value;
  field.dispatchEvent(new Event("input", { bubbles: true }));
  await flush();
}
function submit(root: HTMLElement) {
  const button = root.querySelector<HTMLButtonElement>('button[type="submit"]');
  if (!button) throw new Error("Missing submit button");
  return button;
}
describe("ClickHouse edit credentials", () => {
  it.each(["host", "username"])("requires password after %s changes", async (id) => {
    const root = await render();
    expect(submit(root).disabled).toBe(false);
    await input(root, id, "new.example");
    expect(submit(root).disabled).toBe(true);
    root.querySelector("form")?.dispatchEvent(new Event("submit", { bubbles: true, cancelable: true }));
    await flush();
    expect(update).not.toHaveBeenCalled();
    await input(root, "password", "reentered");
    expect(submit(root).disabled).toBe(false);
  });
  it("allows an activity toggle without reentering password", async () => {
    const root = await render();
    const toggle = root.querySelector<HTMLButtonElement>("#clickhouse_allow_view_activity");
    if (!toggle) throw new Error("Missing view activity switch");
    toggle.click();
    await flush();
    expect(submit(root).disabled).toBe(false);
    submit(root).click();
    await flush();
    expect(update).toHaveBeenCalledWith(7, expect.objectContaining({ connection: expect.objectContaining({ tls_enable: true, password: "" }) }));
  });
  it("allows activity-only changes and preserves TLS without reentering password", async () => {
    const root = await render();
    await input(root, "ch_activity_max_rows_to_read", "1000");
    expect(submit(root).disabled).toBe(false);
    submit(root).click();
    await flush();
    expect(update).toHaveBeenCalledWith(7, expect.objectContaining({ connection: expect.objectContaining({ host: "original.example:9000", tls_enable: true, password: "", activity: { allow_view: true, max_rows_to_read: 1000 } }) }));
  });
  it.each([["ch_activity_max_rows_to_read", "10000000001"], ["ch_activity_max_bytes_to_read", "1099511627777"]])("rejects excessive %s", async (id, value) => {
    const root = await render();
    await input(root, id, value);
    expect(submit(root).disabled).toBe(true);
  });
});
