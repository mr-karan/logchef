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
const lokiSource: Source = {
  id: 8, name: "loki", source_type: "loki", _meta_is_auto_created: false,
  _meta_ts_field: "_time", _meta_severity_field: "detected_level", ttl_days: 0, created_at: "", updated_at: "", is_connected: true,
  query_languages: ["logql"],
  connection: { base_url: "http://loki.test:3100", tenant_id: "dev", selector: '{service_name=~".+"}', principal: "readonly", parser: "json", auth: { mode: "bearer" }, headers: { "X-Gateway-Key": "" } },
};
const update = vi.fn((..._args: unknown[]) => Promise.resolve({ status: "success", data: source }));
const create = vi.fn((..._args: unknown[]): Promise<unknown> => Promise.resolve({ status: "success", data: lokiSource }));
vi.mock("@/api/sources", async (importOriginal) => ({
  ...(await importOriginal<typeof import("@/api/sources")>()),
  sourcesApi: {
    listAllSourcesForAdmin: () => Promise.resolve({ status: "success", data: [source, lokiSource] }),
    updateSource: (...args: unknown[]) => update(...args),
    createSource: (...args: unknown[]) => create(...args),
  },
}));
const route = vi.hoisted(() => ({ params: {} as Record<string, string>, query: {} }));
vi.mock("vue-router", () => ({ useRoute: () => route, useRouter: () => ({ push: vi.fn() }) }));
const { default: AddSource } = await import("../AddSource.vue");
let unmount: (() => void) | undefined;
afterEach(() => { unmount?.(); document.body.replaceChildren(); vi.clearAllMocks(); });
async function flush() {
  for (let i = 0; i < 5; i++) { await new Promise((resolve) => setTimeout(resolve, 0)); await nextTick(); }
}
async function mount(sourceId?: number) {
  route.params = sourceId ? { sourceId: String(sourceId) } : {};
  const root = document.createElement("div");
  document.body.appendChild(root);
  const app = createApp(AddSource);
  app.use(createPinia()).use(i18n).mount(root);
  unmount = () => app.unmount();
  await flush();
  return root;
}
async function render() {
  const root = await mount(7);
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

describe("Loki sources", () => {
  async function fillNewLokiSource(root: HTMLElement, name: string, principal: string) {
    root.querySelector<HTMLButtonElement>("#source_type_loki")?.click();
    await flush();
    await input(root, "source_name", name);
    await input(root, "loki_base_url", "http://loki.test:3100");
    await input(root, "loki_tenant_id", "dev");
    await input(root, "loki_selector", '{service_name=~".+"}');
    await input(root, "loki_principal", principal);
  }

  it("creates two sources that differ only in the principal", async () => {
    for (const [name, principal] of [["loki-a", "payments-readonly"], ["loki-b", "payments-admin"]]) {
      const root = await mount();
      await fillNewLokiSource(root, name, principal);
      expect(submit(root).disabled).toBe(false);
      submit(root).click();
      await flush();
      unmount?.();
      document.body.replaceChildren();
    }
    expect(create).toHaveBeenCalledTimes(2);
    const connections = create.mock.calls.map(([payload]) => (payload as { source_type: string; connection: Record<string, unknown> }));
    for (const payload of connections) expect(payload.source_type).toBe("loki");
    expect(connections.map((payload) => payload.connection)).toEqual([
      { base_url: "http://loki.test:3100", tenant_id: "dev", selector: '{service_name=~".+"}', principal: "payments-readonly" },
      { base_url: "http://loki.test:3100", tenant_id: "dev", selector: '{service_name=~".+"}', principal: "payments-admin" },
    ]);
  });

  it("needs a selector", async () => {
    const root = await mount();
    await fillNewLokiSource(root, "loki-a", "");
    await input(root, "loki_selector", "");
    expect(submit(root).disabled).toBe(true);
  });

  it("names the principal when the identity already exists", async () => {
    create.mockImplementationOnce(() => Promise.reject({ status: "error", message: "source identity exists", error_type: "ConflictError" }));
    const root = await mount();
    await fillNewLokiSource(root, "loki-a", "payments-readonly");
    submit(root).click();
    await flush();
    expect(root.textContent).toContain("Set a different access principal");
  });

  it("keeps the identity fields read-only on edit and sends no connection when it is unchanged", async () => {
    const root = await mount(8);
    for (const id of ["loki_base_url", "loki_tenant_id", "loki_selector", "loki_principal"]) {
      expect(root.querySelector<HTMLInputElement>(`#${id}`)?.disabled, id).toBe(true);
    }
    await input(root, "source_name", "loki renamed");
    submit(root).click();
    await flush();
    expect(update).toHaveBeenCalledWith(8, expect.not.objectContaining({ connection: expect.anything() }));
  });

  it("keeps the stored headers and parser when only the token changes", async () => {
    const root = await mount(8);
    expect(root.querySelector("#loki_parser")?.textContent).toContain("JSON");
    await input(root, "loki_token", "rotated-token");
    submit(root).click();
    await flush();
    expect(update).toHaveBeenCalledWith(8, expect.objectContaining({
      connection: {
        base_url: "http://loki.test:3100", tenant_id: "dev", selector: '{service_name=~".+"}', principal: "readonly",
        parser: "json", auth: { mode: "bearer", token: "rotated-token" }, headers: { "X-Gateway-Key": "" },
      },
    }));
  });

  it("changes the parser without asking for the stored token again", async () => {
    // jsdom lacks the pointer capture and scrolling that the select calls.
    Object.assign(Element.prototype, { hasPointerCapture: () => false, releasePointerCapture: () => {}, scrollIntoView: () => {} });
    const root = await mount(8);
    const trigger = root.querySelector<HTMLButtonElement>("#loki_parser");
    if (!trigger) throw new Error("Missing parser select");
    trigger.dispatchEvent(new PointerEvent("pointerdown", { bubbles: true, button: 0, pointerType: "mouse" }));
    trigger.click();
    await flush();
    const option = [...document.querySelectorAll<HTMLElement>('[role="option"]')].find((element) => element.textContent?.trim() === "logfmt");
    if (!option) throw new Error("Missing logfmt option");
    option.dispatchEvent(new PointerEvent("pointerup", { bubbles: true, button: 0, pointerType: "mouse" }));
    option.click();
    await flush();
    expect(trigger.textContent).toContain("logfmt");
    expect(submit(root).disabled).toBe(false);
    submit(root).click();
    await flush();
    expect(update).toHaveBeenCalledWith(8, expect.objectContaining({
      connection: expect.objectContaining({ parser: "logfmt", auth: { mode: "bearer", token: "" }, headers: { "X-Gateway-Key": "" } }),
    }));
  });
});
