import { describe, it, expect, beforeEach, vi } from "vitest";
import { setActivePinia, createPinia } from "pinia";
import { ref } from "vue";
import type { User } from "@/types";
import type { UserTeamMembership } from "@/api/teams";
import type { SavedQuery } from "@/api/savedQueries";
import type { Collection } from "@/api/collections";

// Mock auth and teams stores. Real Pinia setup stores auto-unwrap refs at
// the property access boundary, so the mocks expose getters that return the
// unwrapped value. The composable reads `user.role` and `userTeams[]` — only
// those two surfaces need mocking.
const mockUser = ref<User | null>(null);
const mockUserTeams = ref<UserTeamMembership[]>([]);

vi.mock("@/stores/auth", () => ({
  useAuthStore: () => ({
    get user() {
      return mockUser.value;
    },
    get isAuthenticated() {
      return mockUser.value !== null;
    },
  }),
}));

vi.mock("@/stores/teams", () => ({
  useTeamsStore: () => ({
    get userTeams() {
      return mockUserTeams.value;
    },
  }),
}));

// Import the composable AFTER the mocks are declared.
const { useTeamPermissions } = await import("../useTeamPermissions");

const baseUser = (overrides: Partial<User> = {}): User => ({
  id: "10",
  email: "user@example.com",
  full_name: "User",
  role: "member",
  status: "active",
  account_type: "human",
  created_at: "",
  updated_at: "",
  ...overrides,
});

const baseTeam = (overrides: Partial<UserTeamMembership> = {}): UserTeamMembership => ({
  id: 1,
  name: "Team",
  description: "",
  created_at: "",
  updated_at: "",
  member_count: 1,
  role: "member",
  ...overrides,
});

function login(user: User | null, teams: UserTeamMembership[] = []) {
  mockUser.value = user;
  mockUserTeams.value = teams;
}

describe("useTeamPermissions", () => {
  beforeEach(() => {
    setActivePinia(createPinia());
    mockUser.value = null;
    mockUserTeams.value = [];
  });

  describe("isGlobalAdmin", () => {
    it("true when global admin", () => {
      login(baseUser({ role: "admin" }));
      expect(useTeamPermissions().isGlobalAdmin.value).toBe(true);
    });
    it("false when global member", () => {
      login(baseUser({ role: "member" }));
      expect(useTeamPermissions().isGlobalAdmin.value).toBe(false);
    });
  });

  describe("isAnyTeamAdmin", () => {
    it("true when user is admin in any team", () => {
      login(baseUser(), [baseTeam({ id: 1, role: "member" }), baseTeam({ id: 2, role: "admin" })]);
      expect(useTeamPermissions().isAnyTeamAdmin.value).toBe(true);
    });
    it("false when user is only editor (does not count)", () => {
      login(baseUser(), [baseTeam({ id: 1, role: "editor" })]);
      expect(useTeamPermissions().isAnyTeamAdmin.value).toBe(false);
    });
    it("false when user has no teams", () => {
      login(baseUser(), []);
      expect(useTeamPermissions().isAnyTeamAdmin.value).toBe(false);
    });
    it("true for global admin even without team membership", () => {
      login(baseUser({ role: "admin" }), []);
      expect(useTeamPermissions().isAnyTeamAdmin.value).toBe(true);
    });
  });

  describe("isAnyTeamCollectionMutator", () => {
    it.each([
      ["admin", true],
      ["editor", true],
      ["member", false],
    ] as const)("role=%s → %s", (role, want) => {
      login(baseUser(), [baseTeam({ id: 1, role })]);
      expect(useTeamPermissions().isAnyTeamCollectionMutator.value).toBe(want);
    });
    it("true for global admin without team membership", () => {
      login(baseUser({ role: "admin" }), []);
      expect(useTeamPermissions().isAnyTeamCollectionMutator.value).toBe(true);
    });
  });

  describe("isTeamCollectionMutator(teamId)", () => {
    it("admin in team A is mutator in A, not in B", () => {
      login(baseUser(), [
        baseTeam({ id: 1, role: "admin" }),
        baseTeam({ id: 2, role: "member" }),
      ]);
      const p = useTeamPermissions();
      expect(p.isTeamCollectionMutator(1)).toBe(true);
      expect(p.isTeamCollectionMutator(2)).toBe(false);
    });
    it("editor in team A is mutator in A, not in B", () => {
      login(baseUser(), [
        baseTeam({ id: 1, role: "editor" }),
        baseTeam({ id: 2, role: "member" }),
      ]);
      const p = useTeamPermissions();
      expect(p.isTeamCollectionMutator(1)).toBe(true);
      expect(p.isTeamCollectionMutator(2)).toBe(false);
    });
    it("global admin is mutator in any team they aren't a member of", () => {
      login(baseUser({ role: "admin" }), []);
      expect(useTeamPermissions().isTeamCollectionMutator(99)).toBe(true);
    });
    it("null teamId returns false (unless global admin)", () => {
      login(baseUser(), [baseTeam({ id: 1, role: "admin" })]);
      expect(useTeamPermissions().isTeamCollectionMutator(null)).toBe(false);
    });
  });

  describe("canSaveQuery", () => {
    it("true when user has at least one team", () => {
      login(baseUser(), [baseTeam({ id: 1, role: "member" })]);
      expect(useTeamPermissions().canSaveQuery.value).toBe(true);
    });
    it("false when user has no teams (no source access possible)", () => {
      login(baseUser(), []);
      expect(useTeamPermissions().canSaveQuery.value).toBe(false);
    });
    it("true for global admin even without team membership", () => {
      login(baseUser({ role: "admin" }), []);
      expect(useTeamPermissions().canSaveQuery.value).toBe(true);
    });
    it("false when not authenticated", () => {
      login(null);
      expect(useTeamPermissions().canSaveQuery.value).toBe(false);
    });
  });

  describe("canEditSavedQuery(query)", () => {
    const creator = baseUser({ id: "10" });
    const other = baseUser({ id: "20" });
    const admin = baseUser({ id: "30", role: "admin" });
    const query: SavedQuery = {
      id: 1,
      source_id: 1,
      name: "q",
      description: "",
      query_type: "logchefql",
      query_content: "",
      created_by: 10,
      created_at: "",
      updated_at: "",
    };

    it("creator can edit", () => {
      login(creator);
      expect(useTeamPermissions().canEditSavedQuery(query)).toBe(true);
    });
    it("non-creator member cannot edit", () => {
      login(other);
      expect(useTeamPermissions().canEditSavedQuery(query)).toBe(false);
    });
    it("global admin can edit", () => {
      login(admin);
      expect(useTeamPermissions().canEditSavedQuery(query)).toBe(true);
    });
    it("null query is not editable", () => {
      login(creator);
      expect(useTeamPermissions().canEditSavedQuery(null)).toBe(false);
    });
    it("legacy NULL-creator query: non-admin cannot edit, global admin can", () => {
      const legacy: SavedQuery = { ...query, created_by: undefined };
      login(creator);
      expect(useTeamPermissions().canEditSavedQuery(legacy)).toBe(false);
      login(admin);
      expect(useTeamPermissions().canEditSavedQuery(legacy)).toBe(true);
    });
  });

  describe("canManageCollection(collection)", () => {
    const owned: Collection = {
      id: 1,
      name: "owned",
      is_personal: false,
      created_by: 10,
      caller_role: "owner",
      member_count: 1,
      item_count: 0,
      created_at: "",
      updated_at: "",
    };
    const memberRole: Collection = { ...owned, id: 2, caller_role: "member" };
    const personal: Collection = { ...owned, id: 3, is_personal: true };

    it("owner can manage", () => {
      login(baseUser());
      expect(useTeamPermissions().canManageCollection(owned)).toBe(true);
    });
    it("member role cannot manage", () => {
      login(baseUser());
      expect(useTeamPermissions().canManageCollection(memberRole)).toBe(false);
    });
    it("personal collection is manageable by its user (owner role)", () => {
      login(baseUser());
      expect(useTeamPermissions().canManageCollection(personal)).toBe(true);
    });
    it("global admin can manage any collection", () => {
      login(baseUser({ role: "admin" }));
      expect(useTeamPermissions().canManageCollection(memberRole)).toBe(true);
    });
    it("null collection is not manageable", () => {
      login(baseUser());
      expect(useTeamPermissions().canManageCollection(null)).toBe(false);
    });
  });
});
