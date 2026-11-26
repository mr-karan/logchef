import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { createApp, defineComponent, nextTick } from "vue";
import { createPinia } from "pinia";
import { createMemoryHistory, createRouter } from "vue-router";
import { i18n } from "@/i18n";
import { useAuthStore } from "@/stores/auth";
import { useTeamsStore } from "@/stores/teams";
import { useMetaStore } from "@/stores/meta";
import InnerApp from "../InnerApp.vue";
import AccessLayout from "@/views/access/AccessLayout.vue";
import AdminOverview from "@/views/admin/AdminOverview.vue";

type Envelope = { status: "success"; data: unknown } | { status: "error"; message: string; error_type: string };

const mockState = {
  role: "admin" as "admin" | "member",
  teamRole: "member" as "admin" | "editor" | "member",
};

vi.mock("@/api/apiUtils", () => ({
  apiClient: {
    get: async (url: string): Promise<Envelope> => {
      const ok = (data: unknown): Envelope => ({ status: "success", data });
      if (url === "/meta") {
        return ok({ version: "2.8.0", http_server_timeout: "30s", max_query_limit: 1000, alerts_enabled: true });
      }
      if (url === "/me") {
        return ok({
          user: {
            id: "1",
            role: mockState.role,
            email: "user@example.com",
            full_name: "Test User",
            status: "active",
            account_type: "human",
            created_at: "",
            updated_at: "",
          },
          session: { id: "s1", user_id: "1", expires_at: "", created_at: "" },
        });
      }
      if (url === "/me/preferences") {
        return ok({ preferences: { theme: "auto" }, is_default: true });
      }
      if (url === "/me/teams") {
        return ok([
          {
            id: 10,
            name: "Platform",
            description: "",
            created_at: "",
            updated_at: "",
            member_count: 2,
            role: mockState.teamRole,
          },
        ]);
      }
      if (url === "/users") {
        return ok([
          { id: "1", email: "a@example.com", full_name: "A", role: "admin", status: "active", account_type: "human", created_at: "", updated_at: "" },
          { id: "2", email: "b@example.com", full_name: "B", role: "member", status: "inactive", account_type: "human", created_at: "", updated_at: "" },
          { id: "3", email: "svc@example.com", full_name: "Bot", role: "member", status: "active", account_type: "service", created_at: "", updated_at: "" },
        ]);
      }
      if (url === "/admin/teams") {
        return ok([
          { id: 10, name: "Platform", description: "", member_count: 1, created_at: "", updated_at: "" },
          { id: 11, name: "Orphan", description: "", member_count: 1, created_at: "", updated_at: "" },
        ]);
      }
      if (url === "/admin/team-memberships") {
        return ok([
          { team_id: 10, team_name: "Platform", team_managed: false, user_id: 1, role: "admin", created_at: "" },
          { team_id: 11, team_name: "Orphan", team_managed: false, user_id: 2, role: "member", created_at: "" },
        ]);
      }
      if (url === "/admin/sources") {
        return ok([
          { id: 1, name: "ClickHouse", source_type: "clickhouse", is_connected: true, connection: {}, created_at: "", updated_at: "" },
          { id: 2, name: "Down", source_type: "clickhouse", is_connected: false, connection: {}, created_at: "", updated_at: "" },
        ]);
      }
      if (url === "/admin/service-accounts") {
        return ok([
          { id: "3", email: "svc@example.com", full_name: "Bot", role: "member", status: "active", account_type: "service", created_at: "", updated_at: "" },
        ]);
      }
      return ok([]);
    },
  },
}));

vi.mock("@/components/VersionBanner.vue", () => ({
  default: defineComponent({ template: "<div />" }),
}));

async function settle() {
  for (let i = 0; i < 8; i++) {
    await new Promise((resolve) => setTimeout(resolve, 0));
    await nextTick();
  }
}

async function mountShell(path: string, role: "admin" | "member", teamRole: "admin" | "editor" | "member") {
  mockState.role = role;
  mockState.teamRole = teamRole;
  document.cookie = "sidebar_state=true; path=/";

  const host = document.createElement("div");
  document.body.appendChild(host);

  const router = createRouter({
    history: createMemoryHistory(),
    routes: [
      { path: "/", component: { template: "<div />" } },
      { path: "/logs/explore", component: { template: "<div>Explore</div>" } },
      { path: "/logs/library", component: { template: "<div>Library</div>" } },
      { path: "/logs/alerts", component: { template: "<div>Alerts</div>" } },
      { path: "/dashboards", component: { template: "<div>Dashboards</div>" } },
      { path: "/reports", component: { template: "<div>Reports</div>" } },
      { path: "/settings/profile", component: { template: "<div>Profile</div>" } },
      { path: "/settings/preferences", component: { template: "<div>Preferences</div>" } },
      {
        path: "/admin",
        component: AccessLayout,
        children: [
          { path: "", name: "AdminOverview", component: AdminOverview },
          { path: "users", name: "ManageUsers", component: { template: "<div>PeopleView</div>" } },
          { path: "teams", name: "Teams", component: { template: "<div>TeamsView</div>" } },
          { path: "service-tokens", name: "ServiceTokens", component: { template: "<div>ServiceTokensView</div>" } },
          { path: "sources", name: "Sources", component: { template: "<div>SourcesView</div>" } },
          { path: "sources/stats", name: "SourceInspection", component: { template: "<div>InspectionView</div>" } },
          { path: "query-activity", name: "AdminQueryActivity", component: { template: "<div>QueryActivityView</div>" } },
          { path: "audit-log", name: "AdminAuditLog", component: { template: "<div>AuditLogView</div>" } },
          { path: "settings", name: "AdminSettings", component: { template: "<div>SettingsView</div>" } },
        ],
      },
    ],
  });

  const app = createApp(InnerApp);
  const pinia = createPinia();
  app.use(pinia);
  app.use(i18n);
  app.use(router);

  await useMetaStore(pinia).loadMeta();
  await useAuthStore(pinia).initialize();
  await useTeamsStore(pinia).loadUserTeams();
  await router.push(path);
  await router.isReady();
  app.mount(host);
  await settle();

  return { host, app, router };
}

describe("Admin sidebar & section shell", () => {
  let cleanup: (() => void) | null = null;

  beforeEach(() => {
    cleanup = null;
  });

  afterEach(() => {
    cleanup?.();
    vi.restoreAllMocks();
  });

  it("shows global admins one Admin entry in the sidebar and grouped section nav under /admin", async () => {
    const { host, app } = await mountShell("/admin", "admin", "member");
    cleanup = () => {
      app.unmount();
      host.remove();
    };

    const sidebar = host.querySelector('[data-sidebar="sidebar"]');
    expect(sidebar).not.toBeNull();
    const sidebarAdminLinks = [...(sidebar?.querySelectorAll('a[href^="/admin"]') ?? [])];
    expect(sidebarAdminLinks).toHaveLength(1);
    expect(sidebarAdminLinks[0].getAttribute("href")).toBe("/admin");
    expect(sidebarAdminLinks[0].textContent?.trim()).toBe("Admin");

    const sectionNav = host.querySelector('main nav[aria-label="Administration"]');
    expect(sectionNav).not.toBeNull();
    const sectionHrefs = [...(sectionNav?.querySelectorAll("a") ?? [])].map((a) => a.getAttribute("href"));
    expect(sectionHrefs).toEqual([
      "/admin",
      "/admin/users",
      "/admin/teams",
      "/admin/service-tokens",
      "/admin/sources",
      "/admin/sources/stats",
      "/admin/query-activity",
      "/admin/audit-log",
      "/admin/settings",
    ]);

    // Overview cards render real counts (2 humans: 1 admin · 1 inactive, 2 teams: 1 with no admin, 2 sources: 1 unreachable, 1 service account)
    expect(host.textContent).toContain("Admin overview");
    expect(host.textContent).toContain("1 admins · 1 inactive");
    expect(host.textContent).toContain("1 with no admin");
    expect(host.textContent).toContain("1 unreachable");
  });

  it("shows team admins only Teams in both the sidebar and admin section nav", async () => {
    const { host, app } = await mountShell("/admin/teams", "member", "admin");
    cleanup = () => {
      app.unmount();
      host.remove();
    };

    const sidebar = host.querySelector('[data-sidebar="sidebar"]');
    const sidebarAdminLinks = [...(sidebar?.querySelectorAll('a[href^="/admin"]') ?? [])];
    expect(sidebarAdminLinks).toHaveLength(1);
    expect(sidebarAdminLinks[0].getAttribute("href")).toBe("/admin/teams");
    expect(sidebarAdminLinks[0].textContent?.trim()).toBe("Teams");

    const sectionNav = host.querySelector('main nav[aria-label="Administration"]');
    const sectionHrefs = [...(sectionNav?.querySelectorAll("a") ?? [])].map((a) => a.getAttribute("href"));
    expect(sectionHrefs).toEqual(["/admin/teams"]);
  });

  it("hides the Administration sidebar section for regular members", async () => {
    const { host, app } = await mountShell("/logs/explore", "member", "member");
    cleanup = () => {
      app.unmount();
      host.remove();
    };

    const sidebar = host.querySelector('[data-sidebar="sidebar"]');
    const sidebarAdminLinks = [...(sidebar?.querySelectorAll('a[href^="/admin"]') ?? [])];
    expect(sidebarAdminLinks).toHaveLength(0);
    expect(sidebar?.textContent).not.toContain("Administration");
  });
});
