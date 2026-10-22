import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { createApp, defineComponent, nextTick, type App } from "vue";
import { createPinia } from "pinia";
import { createMemoryHistory, createRouter, type Router } from "vue-router";
import { i18n } from "@/i18n";
import type { TeamMember } from "@/api/teams";

type Envelope = { status: "success"; data: unknown } | { status: "error"; message: string; error_type: string };

const members: Record<number, TeamMember[]> = {};
const requests: string[] = [];
const server = {
  teams: [
    { id: 1, name: "Alpha", description: "", member_count: 1, created_at: "", updated_at: "" },
    { id: 2, name: "Beta", description: "", member_count: 1, created_at: "", updated_at: "" },
  ],
  members,
  removeMemberFails: false,
  requests,
  heldTeamId: 0,
  releaseTeam: () => {},
};

function member(teamId: number, userId: number, email: string): TeamMember {
  return { team_id: teamId, user_id: userId, role: "member", email, full_name: email, created_at: "", updated_at: "" };
}

function respond(method: string, url: string): Envelope {
  server.requests.push(`${method} ${url}`);
  const ok = (data: unknown): Envelope => ({ status: "success", data });
  if (url === "/meta") return ok({ version: "test", http_server_timeout: "30s", max_query_limit: 1000 });
  if (url === "/me") return ok({
    user: { id: "99", role: "admin", email: "admin@example.com", full_name: "Admin", status: "active", account_type: "human", created_at: "", updated_at: "" },
    session: { id: "test", user_id: "99", expires_at: "", created_at: "" },
  });
  if (url === "/admin/teams" || url === "/me/teams") return ok(server.teams);
  if (url === "/users" || url === "/admin/sources") return ok([]);
  const match = url.match(/^\/teams\/(\d+)(\/members|\/sources)?(?:\/(\d+))?$/);
  if (!match) throw new Error(`unexpected request ${method} ${url}`);
  const teamId = Number(match[1]);
  if (!match[2]) return ok(server.teams.find((t) => t.id === teamId));
  if (match[2] === "/sources") return ok([]);
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
    post: async (url: string) => respond("post", url),
    put: async (url: string) => respond("put", url),
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

describe("TeamSettings", () => {
  let host: HTMLDivElement;
  let app: App;
  let router: Router;

  beforeEach(async () => {
    server.members = { 1: [member(1, 10, "a@example.com")], 2: [member(2, 20, "b@example.com")] };
    server.removeMemberFails = false;
    server.heldTeamId = 0;
    server.requests = [];
    toasts.length = 0;
    host = document.createElement("div");
    document.body.appendChild(host);
    router = createRouter({
      history: createMemoryHistory(),
      routes: [{ path: "/", component: { template: "<div />" } }, { path: "/admin/teams/:id", name: "TeamSettings", component: TeamSettings }, { path: "/admin/teams", name: "Teams", component: { template: "<div />" } }],
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
    const removeButton = host.querySelector<HTMLButtonElement>('button[title="Remove member"]');
    if (!removeButton) throw new Error("remove member button was not rendered");
    removeButton.click();
    await settle();

    const confirm = [...document.querySelectorAll<HTMLButtonElement>("[role=alertdialog] button")].find((b) => b.textContent?.trim() === "Remove member");
    if (!confirm) throw new Error("confirm button was not rendered");
    confirm.click();
    await settle();

    expect(server.requests).toContain("delete /teams/1/members/10");
    expect(toasts.filter((t) => t.kind === "error")).toHaveLength(1);
  });
});
