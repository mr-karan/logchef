import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { createApp, nextTick, ref } from "vue";
import { createPinia, type Pinia } from "pinia";
import { i18n } from "@/i18n";
import type { User } from "@/types";
import type { Collection, CollectionMember, CollectionTeam } from "@/api/collections";
import type { Team, TeamWithMemberCount, UserTeamMembership } from "@/api/teams";

// Only the HTTP API and the auth/teams context are faked; the collections
// store, permission composable, and pane render for real. The fake API keeps
// server-side state so list counts and rosters reflect each mutation.
const mockUser = ref<User | null>(null);
const server = {
  callerRole: "owner" as Collection["caller_role"],
  members: [] as CollectionMember[],
  teams: [] as CollectionTeam[],
  // Teams the caller belongs to (/me/teams) and all teams (/admin/teams).
  userTeams: [] as UserTeamMembership[],
  adminTeams: [] as TeamWithMemberCount[],
  addTeamRequests: [] as number[],
  removeTeamRequests: [] as number[],
};

const ok = <T>(data: T) => Promise.resolve({ status: "success" as const, data });

function serverCollection(): Collection {
  return {
    id: 1, name: "Shared queries", is_personal: false, created_by: 99, caller_role: server.callerRole,
    member_count: server.members.length, team_count: server.teams.length, item_count: 0,
    created_at: "", updated_at: "",
  };
}

function teamName(teamId: number): string {
  return [...server.adminTeams, ...server.userTeams].find((t) => t.id === teamId)?.name ?? `Team ${teamId}`;
}

vi.mock("@/api/collections", () => ({
  collectionsApi: {
    list: () => ok([serverCollection()]),
    listItems: () => ok([]),
    listMembers: () => ok([...server.members]),
    listTeams: () => ok([...server.teams]),
    addTeam: (_id: number, payload: { team_id: number }) => {
      server.addTeamRequests.push(payload.team_id);
      if (!server.teams.some((t) => t.team_id === payload.team_id)) {
        server.teams.push({ collection_id: 1, team_id: payload.team_id, team_name: teamName(payload.team_id), created_at: "" });
      }
      return ok({ message: "ok" });
    },
    removeTeam: (_id: number, teamId: number) => {
      server.removeTeamRequests.push(teamId);
      server.teams = server.teams.filter((t) => t.team_id !== teamId);
      return ok({ message: "ok" });
    },
  },
}));
vi.mock("@/stores/auth", () => ({
  useAuthStore: () => ({
    get user() { return mockUser.value; },
    get isAuthenticated() { return mockUser.value !== null; },
  }),
}));

// Mirrors the real teams store contract: loadUserTeams always fetches, and
// loadAdminTeams serves its cache unless forced. The caches start from what an
// earlier view loaded, which may be stale relative to the server.
const cachedUserTeams = ref<UserTeamMembership[]>([]);
const cachedAdminTeams = ref<TeamWithMemberCount[]>([]);
vi.mock("@/stores/teams", () => ({
  useTeamsStore: () => ({
    get userTeams() { return cachedUserTeams.value; },
    get adminTeams() { return cachedAdminTeams.value; },
    loadUserTeams: async () => { cachedUserTeams.value = [...server.userTeams]; },
    loadAdminTeams: async (forceReload = false) => {
      if (!forceReload && cachedAdminTeams.value.length > 0) return;
      cachedAdminTeams.value = [...server.adminTeams];
    },
  }),
}));
vi.mock("vue-router", async (importOriginal) => ({
  ...(await importOriginal<typeof import("vue-router")>()),
  useRouter: () => ({ push: vi.fn() }),
}));

const { default: CollectionDetailPane } = await import("../CollectionDetailPane.vue");
const { useCollectionsStore } = await import("@/stores/collections");

function user(role: User["role"]): User {
  return {
    id: "10", email: "user@example.com", full_name: "User", role, status: "active",
    account_type: "human", created_at: "", updated_at: "",
  };
}

function team(id: number, name: string): Team {
  return { id, name, description: "", created_by: "", created_at: "", updated_at: "" };
}
const userTeam = (id: number, name: string): UserTeamMembership => ({ ...team(id, name), member_count: 1, role: "member" });
const adminTeam = (id: number, name: string): TeamWithMemberCount => ({ ...team(id, name), member_count: 1 });

const ownerRow: CollectionMember = { collection_id: 1, user_id: 99, role: "owner", created_at: "", email: "owner@example.com" };
const project14: CollectionTeam = { collection_id: 1, team_id: 14, team_name: "Project 14", created_at: "" };

let host: HTMLElement | null = null;
let unmount: (() => void) | null = null;
let pinia: Pinia | null = null;

async function flush() {
  for (let i = 0; i < 5; i++) {
    await new Promise((resolve) => setTimeout(resolve, 0));
    await nextTick();
  }
}

async function render(): Promise<HTMLElement> {
  host = document.createElement("div");
  document.body.appendChild(host);
  const app = createApp(CollectionDetailPane, { collectionId: 1 });
  pinia = createPinia();
  app.use(pinia);
  app.use(i18n);
  app.mount(host);
  unmount = () => app.unmount();
  await flush();
  return host;
}

function buttonTitles(root: HTMLElement): string[] {
  return [...root.querySelectorAll("button")].map((b) => (b.getAttribute("title") ?? b.textContent ?? "").trim());
}

// Dialogs and popovers teleport to document.body, so look there.
function findButton(text: string): HTMLButtonElement | undefined {
  return [...document.body.querySelectorAll("button")].find(
    (b) => (b.getAttribute("title") ?? b.textContent ?? "").trim() === text
  );
}

async function click(text: string) {
  const button = findButton(text);
  if (!button) throw new Error(`button "${text}" not found`);
  button.click();
  await flush();
}

function teamDialog(): HTMLElement {
  const dialog = [...document.body.querySelectorAll<HTMLElement>("[role=dialog]")].find((d) =>
    d.textContent?.includes("Share with team")
  );
  if (!dialog) throw new Error("share dialog not open");
  return dialog;
}

async function openPickerOptions(): Promise<string[]> {
  const trigger = teamDialog().querySelector<HTMLButtonElement>("[role=combobox]");
  if (!trigger) throw new Error("team picker not found");
  trigger.click();
  await flush();
  return [...document.body.querySelectorAll<HTMLButtonElement>("button[data-item]")].map((b) => b.textContent?.trim() ?? "");
}

function shareSubmit(): HTMLButtonElement {
  const submit = teamDialog().querySelector<HTMLButtonElement>("button[type=submit]");
  if (!submit) throw new Error("share submit not found");
  return submit;
}

// The Library list reads team_count from the collections store, not the roster.
function listTeamCount(): number | undefined {
  if (!pinia) throw new Error("pane not rendered");
  return useCollectionsStore(pinia).collections.find((c) => c.id === 1)?.team_count;
}

function metadataText(root: HTMLElement): string {
  return root.querySelector(".tabular-nums")?.parentElement?.parentElement?.textContent?.replace(/\s+/g, " ") ?? "";
}

beforeEach(() => {
  server.callerRole = "owner";
  server.members = [ownerRow];
  server.teams = [project14];
  server.userTeams = [];
  server.adminTeams = [];
  server.addTeamRequests = [];
  server.removeTeamRequests = [];
  cachedUserTeams.value = [];
  cachedAdminTeams.value = [];
});

afterEach(() => {
  unmount?.();
  host?.remove();
  host = null;
  unmount = null;
  pinia = null;
  document.body.innerHTML = "";
});

describe("CollectionDetailPane team sharing", () => {
  it("lets an owner without user-directory access share with teams", async () => {
    mockUser.value = user("member");

    const root = await render();
    const titles = buttonTitles(root);
    expect(root.textContent).toContain("Shared teams");
    expect(root.textContent).toContain("Project 14");
    expect(titles).toContain("Share with team");
    expect(titles).toContain("Remove team");
    // Inviting individual users still needs a user directory.
    expect(titles).not.toContain("Invite member");
  });

  it("shows a participating global admin the team roster without share controls", async () => {
    mockUser.value = user("admin");
    server.callerRole = "member";

    const root = await render();
    const titles = buttonTitles(root);
    expect(root.textContent).toContain("Project 14");
    expect(titles).not.toContain("Share with team");
    expect(titles).not.toContain("Remove team");
  });

  it("hides the team roster from team-derived members", async () => {
    mockUser.value = user("member");
    server.callerRole = "member";

    const root = await render();
    expect(root.textContent).not.toContain("Shared teams");
    expect(root.textContent).not.toContain("Project 14");
  });

  it("shares with one of the owner's own teams and refreshes roster and counts", async () => {
    mockUser.value = user("member");
    server.userTeams = [userTeam(14, "Project 14"), userTeam(21, "Infrastructure")];
    // Another team exists, but the owner is not in it.
    server.adminTeams = [adminTeam(14, "Project 14"), adminTeam(21, "Infrastructure"), adminTeam(30, "Payments")];

    const root = await render();
    expect(metadataText(root)).toContain("1 team");

    await click("Share with team");
    // Already-shared Project 14 and the foreign Payments team are not offered.
    expect(await openPickerOptions()).toEqual(["Infrastructure"]);
    await click("Infrastructure");
    shareSubmit().click();
    await flush();

    expect(server.addTeamRequests).toEqual([21]);
    expect(document.body.querySelector("[role=dialog]")).toBeNull();
    expect(root.textContent).toContain("Infrastructure");
    expect(metadataText(root)).toContain("2 teams");
    expect(listTeamCount()).toBe(2);
  });

  it("lets an admin owner share with any team", async () => {
    mockUser.value = user("admin");
    server.adminTeams = [adminTeam(14, "Project 14"), adminTeam(30, "Payments")];

    const root = await render();
    await click("Share with team");
    expect(await openPickerOptions()).toEqual(["Payments"]);
    await click("Payments");
    shareSubmit().click();
    await flush();

    expect(server.addTeamRequests).toEqual([30]);
    expect(root.textContent).toContain("Payments");
  });

  it("removes a share after confirmation and refreshes roster and counts", async () => {
    mockUser.value = user("member");
    // The owner left Project 14 but can still revoke the share.
    server.userTeams = [];

    const root = await render();
    await click("Remove team");
    expect(server.removeTeamRequests).toEqual([]);
    await click("Remove");

    expect(server.removeTeamRequests).toEqual([14]);
    expect(root.textContent).not.toContain("Project 14");
    expect(root.textContent).toContain("Not shared with any team.");
    expect(metadataText(root)).toContain("0 teams");
    expect(listTeamCount()).toBe(0);
  });

  it("refreshes stale team candidates each time the picker opens", async () => {
    mockUser.value = user("member");
    server.teams = [];
    // An earlier view cached a membership list that is now out of date.
    cachedUserTeams.value = [userTeam(14, "Project 14")];
    server.userTeams = [userTeam(14, "Project 14"), userTeam(21, "Infrastructure")];

    await render();
    await click("Share with team");
    expect(await openPickerOptions()).toEqual(["Project 14", "Infrastructure"]);
    await click("Infrastructure");
    expect(shareSubmit().disabled).toBe(false);
    await click("Cancel");

    // The owner leaves Infrastructure in another session; the pending
    // selection must not survive into the next open.
    server.userTeams = [userTeam(14, "Project 14")];
    await click("Share with team");
    expect(shareSubmit().disabled).toBe(true);
    expect(await openPickerOptions()).toEqual(["Project 14"]);
  });

  it("refreshes the cached admin team directory when the picker opens", async () => {
    mockUser.value = user("admin");
    server.teams = [];
    cachedAdminTeams.value = [adminTeam(14, "Project 14")];
    server.adminTeams = [adminTeam(14, "Project 14"), adminTeam(40, "Created elsewhere")];

    await render();
    await click("Share with team");
    expect(await openPickerOptions()).toEqual(["Project 14", "Created elsewhere"]);
  });
});
