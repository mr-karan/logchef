import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { createApp, defineComponent, nextTick, type App } from "vue";
import { createPinia } from "pinia";
import { createMemoryHistory, createRouter, type Router } from "vue-router";
import { i18n } from "@/i18n";
import type { User } from "@/types";
import type { TeamMembershipSummary, TeamWithMemberCount } from "@/api/teams";

type Envelope = { status: "success"; data: unknown } | { status: "error"; message: string; error_type: string };

interface ApiCall {
  method: string;
  url: string;
  body?: unknown;
}

const server = {
  users: [] as User[],
  teams: [] as TeamWithMemberCount[],
  memberships: [] as TeamMembershipSummary[],
  calls: [] as ApiCall[],
};

function respond(method: string, url: string, body?: unknown): Envelope {
  server.calls.push({ method, url, body });
  const ok = (data: unknown): Envelope => ({ status: "success", data });
  if (url === "/meta") return ok({ version: "test", http_server_timeout: "30s", max_query_limit: 1000 });
  if (url === "/me") {
    return ok({
      user: server.users.find((u) => u.id === "1"),
      session: { id: "s1", user_id: "1", expires_at: "", created_at: "" },
    });
  }
  if (url === "/users") return ok([...server.users]);
  if (url === "/admin/teams") return ok([...server.teams]);
  if (url === "/me/teams") return ok([]);
  if (url === "/admin/team-memberships") return ok([...server.memberships]);

  const userUpdate = url.match(/^\/admin\/users\/(\d+)$/);
  if (userUpdate && method === "put") {
    const id = userUpdate[1];
    const patch = body as Partial<User>;
    server.users = server.users.map((u) => (u.id === id ? { ...u, ...patch } : u));
    return ok({ user: server.users.find((u) => u.id === id) });
  }

  const memberPost = url.match(/^\/teams\/(\d+)\/members$/);
  if (memberPost && method === "post") {
    const teamId = Number(memberPost[1]);
    const payload = body as { user_id: number; role: "admin" | "editor" | "member" };
    const existing = server.memberships.find(
      (m) => m.team_id === teamId && m.user_id === payload.user_id,
    );
    if (existing) {
      existing.role = payload.role;
    } else {
      const team = server.teams.find((t) => t.id === teamId);
      server.memberships.push({
        team_id: teamId,
        team_name: team?.name ?? `Team ${teamId}`,
        team_managed: !!team?.managed,
        user_id: payload.user_id,
        role: payload.role,
        created_at: "2026-01-01T00:00:00Z",
      });
    }
    return ok({ message: "Team member added successfully" });
  }
  if (memberPost && method === "get") {
    return ok([]);
  }

  const memberDelete = url.match(/^\/teams\/(\d+)\/members\/(\d+)$/);
  if (memberDelete && method === "delete") {
    const teamId = Number(memberDelete[1]);
    const userId = Number(memberDelete[2]);
    server.memberships = server.memberships.filter(
      (m) => !(m.team_id === teamId && m.user_id === userId),
    );
    return ok({ message: "Team member removed successfully" });
  }

  throw new Error(`unexpected request ${method} ${url}`);
}

vi.mock("@/api/apiUtils", () => ({
  apiClient: {
    get: async (url: string) => respond("get", url),
    post: async (url: string, data?: unknown) => respond("post", url, data),
    put: async (url: string, data?: unknown) => respond("put", url, data),
    delete: async (url: string) => respond("delete", url),
  },
}));

const toasts = vi.hoisted((): Array<{ kind: string; message: string; description?: string }> => []);
vi.mock("vue-sonner", () => {
  const record = (kind: string) => (message: string, opts?: { description?: string }) => {
    toasts.push({ kind, message, description: opts?.description });
  };
  return { toast: Object.assign(record("default"), { error: record("error"), success: record("success") }) };
});

import { useAuthStore } from "@/stores/auth";
import UsersList from "../UsersList.vue";

async function settle() {
  for (let i = 0; i < 8; i++) {
    await new Promise((resolve) => setTimeout(resolve, 0));
    await nextTick();
  }
}

async function chooseSelectOption(trigger: HTMLButtonElement, label: string) {
  trigger.dispatchEvent(new KeyboardEvent("keydown", { key: "ArrowDown", bubbles: true }));
  await vi.waitFor(() => expect(document.querySelector("[role=option]")).not.toBeNull());
  const option = [...document.querySelectorAll<HTMLElement>("[role=option]")].find(
    (el) => el.textContent?.trim() === label,
  );
  if (!option) throw new Error(`no option ${label}`);
  option.dispatchEvent(new KeyboardEvent("keydown", { key: "Enter", bubbles: true }));
  await settle();
}

describe("People (UsersList + UserDetailDrawer)", () => {
  let host: HTMLDivElement;
  let app: App;
  let router: Router;

  beforeEach(async () => {
    localStorage.clear();
    toasts.length = 0;
    server.calls = [];
    server.users = [
      {
        id: "1",
        email: "admin@example.com",
        full_name: "Alice Admin",
        role: "admin",
        status: "active",
        account_type: "human",
        created_at: "2026-01-01T00:00:00Z",
        updated_at: "2026-01-01T00:00:00Z",
      },
      {
        id: "2",
        email: "bob@example.com",
        full_name: "Bob Member",
        role: "member",
        status: "active",
        account_type: "human",
        created_at: "2026-01-02T00:00:00Z",
        updated_at: "2026-01-02T00:00:00Z",
      },
      {
        id: "3",
        email: "carol@example.com",
        full_name: "Carol Admin",
        role: "admin",
        status: "active",
        account_type: "human",
        created_at: "2026-01-03T00:00:00Z",
        updated_at: "2026-01-03T00:00:00Z",
      },
      {
        id: "99",
        email: "bot@service.logchef.internal",
        full_name: "CI Bot",
        role: "member",
        status: "active",
        account_type: "service",
        created_at: "2026-01-04T00:00:00Z",
        updated_at: "2026-01-04T00:00:00Z",
      },
    ];
    server.teams = [
      { id: 10, name: "Crux", description: "", created_by: "1", created_at: "", updated_at: "", member_count: 2, managed: false },
      { id: 20, name: "Nomad", description: "", created_by: "1", created_at: "", updated_at: "", member_count: 1, managed: true },
      { id: 30, name: "Platform", description: "", created_by: "1", created_at: "", updated_at: "", member_count: 0, managed: false },
    ];
    server.memberships = [
      { team_id: 10, team_name: "Crux", team_managed: false, user_id: 1, role: "admin", created_at: "" },
      { team_id: 10, team_name: "Crux", team_managed: false, user_id: 2, role: "member", created_at: "" },
      { team_id: 20, team_name: "Nomad", team_managed: true, user_id: 2, role: "editor", created_at: "" },
    ];

    host = document.createElement("div");
    document.body.appendChild(host);
    router = createRouter({
      history: createMemoryHistory(),
      routes: [
        { path: "/", component: { template: "<div />" } },
        { path: "/admin/users", name: "ManageUsers", component: UsersList },
        { path: "/admin/teams/:id", name: "TeamSettings", component: { template: "<div>Team</div>" } },
      ],
    });
    app = createApp(defineComponent({ template: "<router-view />" }));
    const pinia = createPinia();
    app.use(pinia);
    await useAuthStore(pinia).initialize();
    app.use(i18n);
    app.use(router);
    await router.push("/admin/users");
    await router.isReady();
    app.mount(host);
    await settle();
  });

  afterEach(() => {
    app.unmount();
    host.remove();
    vi.restoreAllMocks();
  });

  it("lists human users only with their team chips from one bulk memberships call", () => {
    expect(host.textContent).toContain("Alice Admin");
    expect(host.textContent).toContain("Bob Member");
    expect(host.textContent).not.toContain("CI Bot");

    const bobRow = host.querySelector('[data-testid="people-row-2"]');
    expect(bobRow?.textContent).toContain("Crux · member");
    expect(bobRow?.textContent).toContain("Nomad · editor");

    const membershipCalls = server.calls.filter((c) => c.url === "/admin/team-memberships");
    expect(membershipCalls).toHaveLength(1);
  });

  it("opens the right-side drawer showing teams and managed chip, and saves an unmanaged team role change right away", async () => {
    const bobRow = host.querySelector<HTMLElement>('[data-testid="people-row-2"]');
    bobRow?.click();
    await settle();

    const drawer = document.querySelector('[data-testid="user-detail-drawer"]');
    expect(drawer).not.toBeNull();
    expect(drawer?.textContent).toContain("Bob Member");
    expect(drawer?.textContent).toContain("Crux");
    expect(drawer?.textContent).toContain("Nomad");
    expect(drawer?.querySelector('[data-testid="drawer-team-row-20"]')?.textContent).toContain("managed");

    const cruxRoleTrigger = drawer?.querySelector<HTMLButtonElement>('[data-testid="drawer-team-role-10"]');
    if (!cruxRoleTrigger) throw new Error("missing Crux role select");

    await chooseSelectOption(cruxRoleTrigger, "Editor");

    const postCall = server.calls.find((c) => c.method === "post" && c.url === "/teams/10/members");
    expect(postCall?.body).toEqual({ user_id: 2, role: "editor" });
    expect(toasts.some((t) => t.message === "Team role updated")).toBe(true);
    expect(host.querySelector('[data-testid="people-row-2"]')?.textContent).toContain("Crux · editor");
  });

  it("asks with a ConfirmDialog before changing a role on a managed team", async () => {
    host.querySelector<HTMLElement>('[data-testid="people-row-2"]')?.click();
    await settle();

    const nomadRoleTrigger = document.querySelector<HTMLButtonElement>('[data-testid="drawer-team-role-20"]');
    if (!nomadRoleTrigger) throw new Error("missing Nomad role select");

    await chooseSelectOption(nomadRoleTrigger, "Admin");

    const dialog = document.querySelector("[role=alertdialog]");
    expect(dialog).not.toBeNull();
    expect(dialog?.textContent).toContain("provisioning");
    expect(server.calls.some((c) => c.method === "post" && c.url === "/teams/20/members")).toBe(false);

    const confirmBtn = [...(dialog?.querySelectorAll<HTMLButtonElement>("button") ?? [])].find(
      (b) => b.textContent?.trim() === "Change role",
    );
    confirmBtn?.click();
    await settle();

    const postCall = server.calls.find((c) => c.method === "post" && c.url === "/teams/20/members");
    expect(postCall?.body).toEqual({ user_id: 2, role: "admin" });
  });

  it("asks with a ConfirmDialog before demoting yourself from global admin", async () => {
    host.querySelector<HTMLElement>('[data-testid="people-row-1"]')?.click();
    await settle();

    const globalRoleTrigger = document.querySelector<HTMLButtonElement>('[data-testid="drawer-global-role"]');
    if (!globalRoleTrigger) throw new Error("missing global role select");

    await chooseSelectOption(globalRoleTrigger, "Member");

    const dialog = document.querySelector("[role=alertdialog]");
    expect(dialog).not.toBeNull();
    expect(dialog?.textContent).toContain("Demote yourself");
    expect(server.calls.some((c) => c.method === "put" && c.url === "/admin/users/1")).toBe(false);

    const confirmBtn = [...(dialog?.querySelectorAll<HTMLButtonElement>("button") ?? [])].find(
      (b) => b.textContent?.trim() === "Demote myself",
    );
    confirmBtn?.click();
    await settle();

    const putCall = server.calls.find((c) => c.method === "put" && c.url === "/admin/users/1");
    expect(putCall?.body).toEqual({ role: "member" });
  });

  it("opens the drawer automatically when navigated with ?user=<id>", async () => {
    await router.push({ name: "ManageUsers", query: { user: "2" } });
    await settle();

    const drawer = document.querySelector('[data-testid="user-detail-drawer"]');
    expect(drawer?.textContent).toContain("Bob Member");
  });

  it("removes a user from a team via ConfirmDialog inside the drawer", async () => {
    host.querySelector<HTMLElement>('[data-testid="people-row-2"]')?.click();
    await settle();

    const removeBtn = document.querySelector<HTMLButtonElement>('[data-testid="drawer-team-remove-10"]');
    if (!removeBtn) throw new Error("missing remove button");
    removeBtn.click();
    await settle();

    const dialog = document.querySelector("[role=alertdialog]");
    expect(dialog).not.toBeNull();
    const confirmBtn = [...(dialog?.querySelectorAll<HTMLButtonElement>("button") ?? [])].find(
      (b) => b.textContent?.trim() === "Remove from team",
    );
    confirmBtn?.click();
    await settle();

    expect(server.calls.some((c) => c.method === "delete" && c.url === "/teams/10/members/2")).toBe(true);
    expect(host.querySelector('[data-testid="people-row-2"]')?.textContent).not.toContain("Crux · member");
  });
});
