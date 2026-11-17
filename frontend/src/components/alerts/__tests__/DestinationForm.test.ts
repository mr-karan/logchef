import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { createApp, h, nextTick } from "vue";
import { createPinia } from "pinia";
import { i18n } from "@/i18n";
import { controlledApi } from "@/test/controlledApi";
import type { NotificationDestination } from "@/api/notificationDestinations";
import DestinationForm from "../DestinationForm.vue";

const webhook: NotificationDestination = {
  id: 5, name: "On-call hook", type: "webhook", scope: "team", team_id: 3, config: { payload_version: 1 },
  has_secret: true, secret_hint: "hooks.example.com", is_active: true, created_by: 1,
  created_at: "2026-11-01T00:00:00Z", updated_at: "2026-11-01T00:00:00Z",
};

describe("DestinationForm", () => {
  let network: ReturnType<typeof controlledApi>;
  let host: HTMLDivElement;
  let unmount: () => void;

  function mount(props: Record<string, unknown>) {
    host = document.createElement("div");
    document.body.appendChild(host);
    const app = createApp({ render: () => h(DestinationForm, { teamMembers: [], teamId: 9, ...props }) });
    app.use(createPinia()).use(i18n).mount(host);
    unmount = () => { app.unmount(); host.remove(); };
  }

  function button(label: string): HTMLButtonElement {
    const found = [...host.querySelectorAll("button")].find((b) => b.textContent?.trim() === label);
    if (!found) throw new Error(`no button ${label}`);
    return found;
  }

  function body(index = 0): Record<string, unknown> {
    return JSON.parse(String(network.requests[index].config.data));
  }

  beforeEach(() => { network = controlledApi(); });
  afterEach(() => { unmount(); network.restore(); });

  it("saves an edit with a blank secret so the stored URL stays, in the destination's own team", async () => {
    mount({ destination: webhook });
    await nextTick();
    host.querySelector("form")!.dispatchEvent(new Event("submit"));
    await vi.waitFor(() => expect(network.requests).toHaveLength(1));
    expect(network.requests[0].config.method).toBe("put");
    expect(network.requests[0].config.url).toBe("/notification-destinations/5");
    expect(body()).toMatchObject({ name: "On-call hook", type: "webhook", scope: "team", team_id: 3, config: { payload_version: 1 }, secret: "", is_active: true });
  });

  it("tests a saved webhook by id when no new URL is typed", async () => {
    mount({ destination: webhook });
    await nextTick();
    button("Send test").click();
    await vi.waitFor(() => expect(network.requests).toHaveLength(1));
    expect(network.requests[0].config.url).toBe("/notification-destinations/test");
    expect(body()).toEqual({ id: 5 });
  });

  it("copies a destination into a new one that needs its URL again", async () => {
    mount({ copyOf: webhook });
    await nextTick();
    expect((host.querySelector("#destination-name") as HTMLInputElement).value).toBe("On-call hook (copy)");
    expect(button("Create destination").disabled).toBe(true);
    const url = host.querySelector("#destination-url") as HTMLInputElement;
    url.value = "https://hooks.example.com/new";
    url.dispatchEvent(new Event("input"));
    await nextTick();
    host.querySelector("form")!.dispatchEvent(new Event("submit"));
    await vi.waitFor(() => expect(network.requests).toHaveLength(1));
    expect(network.requests[0].config.method).toBe("post");
    expect(body()).toMatchObject({ name: "On-call hook (copy)", scope: "team", team_id: 3, secret: "https://hooks.example.com/new" });
  });

  function versionSelect(): HTMLButtonElement {
    const selects = [...host.querySelectorAll<HTMLButtonElement>("[role=combobox]")];
    const found = selects.find((b) => b.textContent?.includes("Version"));
    if (!found) throw new Error("no payload version select");
    return found;
  }

  async function chooseVersion(label: string) {
    versionSelect().dispatchEvent(new KeyboardEvent("keydown", { key: "ArrowDown", bubbles: true }));
    await vi.waitFor(() => expect(document.querySelector("[role=option]")).not.toBeNull());
    const option = [...document.querySelectorAll<HTMLElement>("[role=option]")].find((el) => el.textContent?.trim() === label);
    if (!option) throw new Error(`no option ${label}`);
    option.dispatchEvent(new KeyboardEvent("keydown", { key: "Enter", bubbles: true }));
    await nextTick(); await nextTick();
  }

  it("creates a new webhook at payload version 2", async () => {
    mount({});
    await nextTick();
    expect(versionSelect().textContent?.trim()).toBe("Version 2");
    const name = host.querySelector("#destination-name") as HTMLInputElement;
    name.value = "Receiver";
    name.dispatchEvent(new Event("input"));
    const url = host.querySelector("#destination-url") as HTMLInputElement;
    url.value = "https://hooks.example.com/new";
    url.dispatchEvent(new Event("input"));
    await nextTick();
    host.querySelector("form")!.dispatchEvent(new Event("submit"));
    await vi.waitFor(() => expect(network.requests).toHaveLength(1));
    expect(body()).toMatchObject({ type: "webhook", config: { payload_version: 2 }, secret: "https://hooks.example.com/new" });
  });

  it("tests a version 1 webhook at version 2 by id before saving the change", async () => {
    mount({ destination: webhook });
    await nextTick();
    expect(versionSelect().textContent?.trim()).toBe("Version 1 (legacy)");
    await chooseVersion("Version 2");
    button("Send test").click();
    await vi.waitFor(() => expect(network.requests).toHaveLength(1));
    expect(body()).toEqual({ id: 5, config: { payload_version: 2 } });
    host.querySelector("form")!.dispatchEvent(new Event("submit"));
    await vi.waitFor(() => expect(network.requests).toHaveLength(2));
    expect(body(1)).toMatchObject({ type: "webhook", config: { payload_version: 2 }, secret: "" });
  });

  it("edits a Slack destination: its URL field, a test by id, and a save with no config", async () => {
    const slack: NotificationDestination = { ...webhook, id: 8, name: "#ops", type: "slack", config: {}, secret_hint: "hooks.slack.com" };
    mount({ destination: slack });
    await nextTick();
    expect(host.textContent).toContain("Slack webhook URL");
    expect((host.querySelector("#destination-url") as HTMLInputElement).placeholder).toContain("hooks.slack.com");
    button("Send test").click();
    await vi.waitFor(() => expect(network.requests).toHaveLength(1));
    expect(body()).toEqual({ id: 8 });
    const url = host.querySelector("#destination-url") as HTMLInputElement;
    url.value = "https://hooks.slack.com/services/T000/B000/XXXX";
    url.dispatchEvent(new Event("input"));
    await nextTick();
    host.querySelector("form")!.dispatchEvent(new Event("submit"));
    await vi.waitFor(() => expect(network.requests).toHaveLength(2));
    expect(body(1)).toMatchObject({ type: "slack", config: {}, secret: "https://hooks.slack.com/services/T000/B000/XXXX" });
  });

  it("edits a PagerDuty destination: its key field, a test by id, and a save with no config", async () => {
    const pagerduty: NotificationDestination = { ...webhook, id: 9, name: "On-call", type: "pagerduty", config: {}, secret_hint: "…cdef" };
    mount({ destination: pagerduty });
    await nextTick();
    expect(host.textContent).toContain("Routing key");
    const key = host.querySelector("#destination-routing-key") as HTMLInputElement;
    expect(key.type).toBe("password");
    expect(key.placeholder).toContain("…cdef");
    button("Send test").click();
    await vi.waitFor(() => expect(network.requests).toHaveLength(1));
    expect(body()).toEqual({ id: 9 });
    key.value = "0123456789abcdef0123456789abcdef";
    key.dispatchEvent(new Event("input"));
    await nextTick();
    host.querySelector("form")!.dispatchEvent(new Event("submit"));
    await vi.waitFor(() => expect(network.requests).toHaveLength(2));
    expect(body(1)).toMatchObject({ type: "pagerduty", config: {}, secret: "0123456789abcdef0123456789abcdef" });
  });
});

describe("DestinationForm email recipients", () => {
  let network: ReturnType<typeof controlledApi>;
  let host: HTMLDivElement;
  let unmount: () => void;

  beforeEach(() => { network = controlledApi(); });
  afterEach(() => { unmount(); network.restore(); });

  it("lists a recipient outside the member list so it can be removed", async () => {
    const email: NotificationDestination = {
      ...webhook, type: "email", has_secret: false, secret_hint: undefined, config: { user_ids: [7, 42] },
    };
    host = document.createElement("div");
    document.body.appendChild(host);
    const members = [{ team_id: 3, user_id: 7, role: "member" as const, created_at: "", updated_at: "", email: "alice@example.com", full_name: "Alice" }];
    const app = createApp({ render: () => h(DestinationForm, { teamMembers: members, teamId: 3, destination: email }) });
    app.use(createPinia()).use(i18n).mount(host);
    unmount = () => { app.unmount(); host.remove(); };
    await nextTick();
    expect(host.textContent).toContain("User #42");
    const other = [...host.querySelectorAll("label")].find((l) => l.textContent?.includes("User #42"))!;
    (other.querySelector("button") as HTMLButtonElement).click();
    await nextTick();
    host.querySelector("form")!.dispatchEvent(new Event("submit"));
    await vi.waitFor(() => expect(network.requests).toHaveLength(1));
    expect(JSON.parse(String(network.requests[0].config.data)).config).toEqual({ user_ids: [7], addresses: [] });
  });
});
