import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { createApp, defineComponent, nextTick, type App } from "vue";
import { createPinia } from "pinia";
import { createMemoryHistory, createRouter, type Router } from "vue-router";
import { i18n } from "@/i18n";
import type { TeamMember } from "@/api/teams";
import type { User } from "@/types";

type Envelope = { status: "success"; data: unknown } | { status: "error"; message: string; error_type: string };

interface RecordedCall {
  method: string;
  url: string;
  body?: unknown;
}

const members: Record<number, TeamMember[]> = {};
const requests: string[] = [];
const calls: RecordedCall[] = [];
const server = {
  teams: [
    { id: 1, name: "Alpha", description: "", member_count: 1, managed: false, created_at: "", updated_at: "" },
    { id: 2, name: "Beta", description: "", member_count: 1, managed: true, created_at: "", updated_at: "" },
  ],
  users: [
    { id: "10", email: "a@example.com", full_name: "Alice A", role: "member", status: "active", account_type: "human", created_at: "", updated_at: "" },
    { id: "11", email: "c@example.com", full_name: "Carol C", role: "member", status: "active", account_type: "human", created_at: "", updated_at: "" },
    { id: "12", email: "d@example.com", full_name: "Dan D", role: "member", status: "active", account_type: "human", created_at: "", updated_at: "" },
    { id: "20", email: "b@example.com", full_name: "Bob B", role: "member", status: "active", account_type: "human", created_at: "", updated_at: "" },
  ] as User[],
  members,
  removeMemberFails: false,
  requests,
  calls,
  heldTeamId: 0,
  releaseTeam: () => {},
};

function member(teamId: number, userId: number, email: string, role: "admin" | "editor" | "member" = "member"): TeamMember {
  return { team_id: teamId, user_id: userId, role, email, full_name: email, account_type: "human", created_at: "", updated_at: "" };
}

function respond(method: string, url: string, body?: unknown): Envelope {
  server.requests.push(`${method} ${url}`);
  server.calls.push({ method, url, body });
  const ok = (data: unknown): Envelope => ({ status: "success", data });
  if (url === "/meta") return ok({ version: "test", http_server_timeout: "30s", max_query_limit: 1000 });
  if (url === "/me") return ok({
    user: { id: "99", role: "admin", email: "admin@example.com", full_name: "Admin", status: "active", account_type: "human", created_at: "", updated_at: "" },
    session: { id: "test", user_id: "99", expires_at: "", created_at: "" },
  });
  if (url === "/admin/teams" || url === "/me/teams") return ok(server.teams);
  if (url === "/users") return ok(server.users);
  if (url === "/admin/sources") return ok([]);
  const match = url.match(/^\/teams\/(\d+)(\/members|\/sources)?(?:\/(\d+))?$/);
  if (!match) throw new Error(`unexpected request ${method} ${url}`);
  const teamId = Number(match[1]);
  if (!match[2]) return ok(server.teams.find((t) => t.id === teamId));
  if (match[2] === "/sources") return ok([]);
  if (method === "post") {
    const payload = body as { user_id: number; role: "admin" | "editor" | "member" };
    const list = server.members[teamId] ?? [];
    const existing = list.find((m) => m.user_id === payload.user_id);
    if (existing) {
      existing.role = payload.role;
    } else {
      const user = server.users.find((u) => Number(u.id) === payload.user_id);
      list.push(member(teamId, payload.user_id, user?.email ?? `user-${payload.user_id}@example.com`, payload.role));
      server.members[teamId] = list;
    }
    return ok({ message: "Team member added successfully" });
  }
  if (method === "delete") {
    return server.removeMemberFails
      ? { status: "error", message: "member removal rejected", error_type: "ValidationError" }
      : ok({ message: "ok" });
  }
  return ok(server.members[teamId] ?? []);
}

vi.mock("@/api/apiUtils", () => ({
  apiClient: {
    get: async (url: string) => {
      if (url === `/teams/${server.heldTeamId}`) {
        await new Promise<void>((resolve) => { server.releaseTeam = resolve; });
      }
      return respond("get", url);
    },
    post: async (url: string, data?: unknown) => respond("post", url, data),
    put: async (url: string, data?: unknown) => respond("put", url, data),
    delete: async (url: string) => respond("delete", url),
  },
}));

const toasts = vi.hoisted((): Array<{ kind: string; message: string }> => []);
vi.mock("vue-sonner", () => {
  const record = (kind: string) => (message: string) => { toasts.push({ kind, message }); };
  return { toast: Object.assign(record("default"), { error: record("error"), success: record("success") }) };
});

import { useAuthStore } from "@/stores/auth";

import TeamSettings from "../TeamSettings.vue";

async function settle() {
  for (let i = 0; i < 10; i++) {
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

async function openRowMenu(row: Element, itemLabel: string) {
  const trigger = row.querySelector<HTMLButtonElement>('button[title="Actions"]');
  if (!trigger) throw new Error("RowActionsMenu trigger not found");
  trigger.dispatchEvent(new KeyboardEvent("keydown", { key: "Enter", bubbles: true }));
  await vi.waitFor(() => expect(document.querySelector("[role=menuitem]")).not.toBeNull());
  const item = [...document.querySelectorAll<HTMLElement>("[role=menuitem]")].find(
    (el) => el.textContent?.trim() === itemLabel,
  );
  if (!item) throw new Error(`menu item ${itemLabel} not found`);
  item.click();
  await settle();
}

describe("TeamSettings", () => {
  let host: HTMLDivElement;
  let app: App;
  let router: Router;

  beforeEach(async () => {
    localStorage.clear();
    server.members = {
      1: [member(1, 10, "a@example.com", "member")],
      2: [member(2, 20, "b@example.com", "admin")],
    };
    server.removeMemberFails = false;
    server.heldTeamId = 0;
    server.requests = [];
    server.calls = [];
    toasts.length = 0;
    host = document.createElement("div");
    document.body.appendChild(host);
    router = createRouter({
      history: createMemoryHistory(),
      routes: [
        { path: "/", component: { template: "<div />" } },
        { path: "/admin/users", name: "ManageUsers", component: { template: "<div>People</div>" } },
        { path: "/admin/teams/:id", name: "TeamSettings", component: TeamSettings },
        { path: "/admin/teams", name: "Teams", component: { template: "<div />" } },
      ],
    });
    app = createApp(defineComponent({ template: "<router-view />" }));
    const pinia = createPinia();
    app.use(pinia);
    await useAuthStore(pinia).initialize();
    app.use(i18n);
    app.use(router);
    await router.push("/admin/teams/1");
    app.mount(host);
    await settle();
  });

  afterEach(() => {
    app.unmount();
    host.remove();
  });

  it("loads the new team's members when the route moves to another team", async () => {
    expect(host.textContent).toContain("a@example.com");

    await router.push("/admin/teams/2");
    await settle();

    expect(server.requests).toContain("get /teams/2/members");
    expect(server.requests).toContain("get /teams/2/sources");
    expect(host.textContent).toContain("Beta");
    expect(host.textContent).toContain("b@example.com");
    expect(host.textContent).not.toContain("a@example.com");
  });

  it("does not load members or sources after a team request is superseded", async () => {
    server.heldTeamId = 2;
    await router.push("/admin/teams/2");
    await settle();
    await router.push("/admin/teams/1");
    await settle();
    server.releaseTeam();
    await settle();

    expect(server.requests).not.toContain("get /teams/2/members");
    expect(server.requests).not.toContain("get /teams/2/sources");
  });

  it("shows one error toast when member removal fails", async () => {
    server.removeMemberFails = true;
    const row = host.querySelector('[data-testid="team-member-row-10"]');
    if (!row) throw new Error("member row not found");
    await openRowMenu(row, "Remove from team");

    const confirm = [...document.querySelectorAll<HTMLButtonElement>("[role=alertdialog] button")].find(
      (b) => b.textContent?.trim() === "Remove from team",
    );
    if (!confirm) throw new Error("confirm button was not rendered");
    confirm.click();
    await settle();

    expect(server.requests).toContain("delete /teams/1/members/10");
    expect(toasts.filter((t) => t.kind === "error")).toHaveLength(1);
  });

  it("changes a team member's role inline and calls POST /teams/:id/members with the new role", async () => {
    const trigger = host.querySelector<HTMLButtonElement>('[data-testid="member-role-select-10"]');
    if (!trigger) throw new Error("inline role select not rendered");

    await chooseSelectOption(trigger, "Editor");

    const postCall = server.calls.find((c) => c.method === "post" && c.url === "/teams/1/members");
    expect(postCall?.body).toEqual({ user_id: 10, role: "editor" });
    expect(toasts.some((t) => t.message === "Team role updated")).toBe(true);
  });

  it("asks with a ConfirmDialog before demoting the last team admin or changing a role on a managed team", async () => {
    await router.push("/admin/teams/2");
    await settle();

    const trigger = host.querySelector<HTMLButtonElement>('[data-testid="member-role-select-20"]');
    if (!trigger) throw new Error("inline role select not rendered");

    await chooseSelectOption(trigger, "Member");

    const dialog = document.querySelector("[role=alertdialog]");
    expect(dialog).not.toBeNull();
    expect(dialog?.textContent).toContain("last team admin");
    expect(dialog?.textContent).toContain("provisioning");
    expect(server.calls.some((c) => c.method === "post" && c.url === "/teams/2/members")).toBe(false);

    const confirm = [...(dialog?.querySelectorAll<HTMLButtonElement>("button") ?? [])].find(
      (b) => b.textContent?.trim() === "Change role",
    );
    confirm?.click();
    await settle();

    const postCall = server.calls.find((c) => c.method === "post" && c.url === "/teams/2/members");
    expect(postCall?.body).toEqual({ user_id: 20, role: "member" });
  });

  it("adds multiple members at once with one shared role", async () => {
    host.querySelector<HTMLButtonElement>('[data-testid="add-members-trigger"]')?.click();
    await settle();

    document.querySelector<HTMLButtonElement>('[data-testid="add-member-option-11"]')?.click();
    document.querySelector<HTMLButtonElement>('[data-testid="add-member-option-12"]')?.click();
    await settle();

    const roleTrigger = document.querySelector<HTMLButtonElement>('[data-testid="add-members-role"]');
    if (!roleTrigger) throw new Error("add members role select not rendered");
    await chooseSelectOption(roleTrigger, "Editor");

    const submit = document.querySelector<HTMLButtonElement>('[data-testid="add-members-submit"]');
    expect(submit?.textContent).toContain("Add 2 members");
    submit?.click();
    await settle();

    const postCalls = server.calls.filter((c) => c.method === "post" && c.url === "/teams/1/members");
    expect(postCalls.map((c) => c.body)).toEqual([
      { user_id: 11, role: "editor" },
      { user_id: 12, role: "editor" },
    ]);
  });

  it("navigates to People with ?user=<id> from RowActionsMenu and renders the accurate role legend", async () => {
    const legend = host.querySelector('[data-testid="team-role-legend"]')?.textContent ?? "";
    expect(legend).toContain("Admin");
    expect(legend).toContain("Editor");
    expect(legend).toContain("dashboards");
    expect(legend).not.toContain("saved queries");

    const row = host.querySelector('[data-testid="team-member-row-10"]');
    if (!row) throw new Error("member row not found");
    await openRowMenu(row, "View in People");

    expect(router.currentRoute.value.name).toBe("ManageUsers");
    expect(router.currentRoute.value.query.user).toBe("10");
  });
});
