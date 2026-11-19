import { afterEach, describe, expect, it, vi } from "vitest";
import { createApp, nextTick, type App } from "vue";
import { createPinia } from "pinia";
import { i18n } from "@/i18n";
import type { SettingsByCategory, SystemSetting } from "@/api/settings";

const listed: SettingsByCategory[] = [];

vi.mock("@/api/settings", async (importOriginal) => ({
  ...(await importOriginal<typeof import("@/api/settings")>()),
  settingsApi: { listSettings: () => Promise.resolve({ status: "success", data: listed }) },
}));
vi.mock("@/stores/auth", () => ({ useAuthStore: () => ({ user: { id: "1", role: "admin", email: "admin@example.com" } }) }));

const { default: AdminSettings } = await import("../AdminSettings.vue");

function setting(key: string, value: string, valueType: SystemSetting["value_type"]): SystemSetting {
  return { key, value, value_type: valueType, category: "reports", is_sensitive: false, created_at: "", updated_at: "" };
}

describe("AdminSettings reports tab", () => {
  let app: App | undefined;
  let host: HTMLDivElement | undefined;

  afterEach(() => {
    app?.unmount();
    host?.remove();
  });

  async function show(enabled: string, domains: string) {
    listed.splice(0, listed.length, {
      category: "reports",
      settings: [setting("reports.external_recipients", enabled, "boolean"), setting("reports.allowed_email_domains", domains, "string")],
    });
    host = document.createElement("div");
    document.body.appendChild(host);
    app = createApp(AdminSettings);
    app.use(createPinia()).use(i18n).mount(host);
    await new Promise((resolve) => setTimeout(resolve, 0));
    await nextTick();
    const tab = [...host.querySelectorAll<HTMLButtonElement>("button")].find((b) => b.textContent?.trim() === i18n.global.t("routes.reports"));
    if (!tab) throw new Error("no Reports tab");
    tab.dispatchEvent(new MouseEvent("mousedown", { bubbles: true, button: 0 }));
    await vi.waitFor(() => expect(host!.textContent).toContain("Allowed Email Domains"));
  }

  it("says that no address passes when external recipients are on with no domain", async () => {
    await show("true", " ");
    expect(host!.querySelector('[data-testid="external-no-domains"]')?.textContent).toContain(i18n.global.t("admin.externalRecipientsNoDomains"));
  });

  it("says nothing once a domain is allowed, or while the setting is off", async () => {
    await show("true", "example.com");
    expect(host!.querySelector('[data-testid="external-no-domains"]')).toBeNull();
    app!.unmount();
    host!.remove();
    await show("false", "");
    expect(host!.querySelector('[data-testid="external-no-domains"]')).toBeNull();
  });
});
