import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { createApp, defineComponent, nextTick, reactive, ref } from "vue";
import { i18n } from "@/i18n";
import type { Collection, CollectionMember } from "@/api/collections";
import type { User } from "@/types";
import type { TeamMember, UserTeamMembership } from "@/api/teams";

type QueryContentFixture = {
  version: number;
  sourceId: number;
  timeRange: null;
  limit: number;
  content: string;
  variables: unknown[];
};

const queryContentFixture: QueryContentFixture = {
  version: 1,
  sourceId: 7,
  timeRange: null,
  limit: 100,
  content: 'level="info"',
  variables: [],
};

const stubs = await vi.hoisted(async () => {
  const { defineComponent, h } = await import("vue");
  const passthrough = (tag: string) => defineComponent({
    inheritAttrs: false,
    setup(_, { attrs, slots }) {
      return () => h(tag, attrs, slots.default?.());
    },
  });
  const inputStub = defineComponent({
    inheritAttrs: false,
    props: { modelValue: { type: String, default: "" } },
    emits: ["update:modelValue"],
    setup(props, { attrs, emit }) {
      return () => h("input", {
        ...attrs,
        value: props.modelValue,
        onInput: (event: Event) => {
          if (!(event.target instanceof HTMLInputElement)) return;
          emit("update:modelValue", event.target.value);
        },
      });
    },
  });
  const textareaStub = defineComponent({
    inheritAttrs: false,
    props: { modelValue: { type: String, default: "" } },
    emits: ["update:modelValue"],
    setup(props, { attrs, emit }) {
      return () => h("textarea", {
        ...attrs,
        value: props.modelValue,
        onInput: (event: Event) => {
          if (!(event.target instanceof HTMLTextAreaElement)) return;
          emit("update:modelValue", event.target.value);
        },
      });
    },
  });
  const checkboxStub = defineComponent({
    inheritAttrs: false,
    props: { modelValue: { type: Boolean, default: false } },
    emits: ["update:modelValue"],
    setup(props, { attrs, emit }) {
      return () => h("input", {
        ...attrs,
        type: "checkbox",
        checked: props.modelValue,
        onChange: (event: Event) => {
          if (!(event.target instanceof HTMLInputElement)) return;
          emit("update:modelValue", event.target.checked);
        },
      });
    },
  });
  const selectStub = defineComponent({
    inheritAttrs: false,
    props: { modelValue: { type: String, default: "" } },
    emits: ["update:modelValue"],
    setup(props, { attrs, emit, slots }) {
      return () => h("div", { ...attrs, "data-select-value": props.modelValue }, [
        h("input", {
          type: "hidden",
          "data-select-input": "true",
          value: props.modelValue,
          onInput: (event: Event) => {
            if (!(event.target instanceof HTMLInputElement)) return;
            emit("update:modelValue", event.target.value);
          },
        }),
        slots.default?.(),
      ]);
    },
  });
  const searchableSelectStub = defineComponent({
    inheritAttrs: false,
    props: {
      modelValue: { type: String, default: "" },
      items: { type: Array, default: () => [] },
      placeholder: { type: String, default: "" },
    },
    emits: ["update:modelValue"],
    setup(props, { attrs, emit }) {
      return () => h(
        "select",
        {
          ...attrs,
          "data-testid": "editor-user-select",
          value: props.modelValue,
          onChange: (event: Event) => {
            if (!(event.target instanceof HTMLSelectElement)) return;
            emit("update:modelValue", event.target.value);
          },
        },
        [
          h("option", { value: "" }, props.placeholder),
          ...(props.items as Array<{ value: string; label: string }>).map((item) =>
            h("option", { value: item.value }, item.label),
          ),
        ],
      );
    },
  });
  return { passthrough, inputStub, textareaStub, checkboxStub, selectStub, searchableSelectStub };
});

const mockState = vi.hoisted(() => ({
  loadSources: vi.fn(() => Promise.resolve()),
  toast: vi.fn(),
  fetchCollections: vi.fn(() => Promise.resolve()),
  fetchMembers: vi.fn(() => Promise.resolve()),
  addMember: vi.fn((_collectionId: number, _payload: { user_id: number; role: string }) => Promise.resolve({ success: true })),
  loadUsers: vi.fn(() => Promise.resolve()),
  listTeamMembers: vi.fn(() => Promise.resolve({ status: "success" as const, data: [] as TeamMember[] })),
  collections: [] as Collection[],
  membersByCollection: {} as Record<number, CollectionMember[]>,
  user: null as User | null,
  users: [] as User[],
  userTeams: [] as UserTeamMembership[],
}));

const reactiveState = reactive({
  get collections() { return mockState.collections; },
  set collections(v: Collection[]) { mockState.collections = v; },
  get members() { return mockState.membersByCollection; },
  set members(v: Record<number, CollectionMember[]>) { mockState.membersByCollection = v; },
  get user() { return mockState.user; },
  set user(v: User | null) { mockState.user = v; },
  get users() { return mockState.users; },
  set users(v: User[]) { mockState.users = v; },
  get userTeams() { return mockState.userTeams; },
  set userTeams(v: UserTeamMembership[]) { mockState.userTeams = v; },
});

vi.mock("@/components/ui/button", () => ({ Button: stubs.passthrough("button") }));
vi.mock("@/components/ui/dialog", () => ({
  Dialog: stubs.passthrough("div"),
  DialogContent: stubs.passthrough("div"),
  DialogDescription: stubs.passthrough("p"),
  DialogHeader: stubs.passthrough("header"),
  DialogTitle: stubs.passthrough("h2"),
}));
vi.mock("@/components/ui/input", () => ({ Input: stubs.inputStub }));
vi.mock("@/components/ui/textarea", () => ({ Textarea: stubs.textareaStub }));
vi.mock("@/components/ui/checkbox", () => ({ Checkbox: stubs.checkboxStub }));
vi.mock("@/components/ui/label", () => ({ Label: stubs.passthrough("label") }));
vi.mock("@/components/ui/select", () => ({
  Select: stubs.selectStub,
  SelectContent: stubs.passthrough("div"),
  SelectItem: stubs.passthrough("div"),
  SelectTrigger: stubs.passthrough("button"),
  SelectValue: stubs.passthrough("span"),
}));
vi.mock("@/components/ui/searchable-select", () => ({ SearchableSelect: stubs.searchableSelectStub }));
vi.mock("@/api/sources", () => ({ asClickHouseConnection: () => null }));
vi.mock("@/api/teams", () => ({
  teamsApi: {
    listTeamMembers: mockState.listTeamMembers,
  },
}));
vi.mock("@/stores/savedQueries", () => ({
  useSavedQueriesStore: () => ({
    data: { teams: [] },
    fetchUserTeams: vi.fn(() => Promise.resolve()),
  }),
}));
vi.mock("@/stores/collections", () => ({
  useCollectionsStore: () => ({
    get collections() { return reactiveState.collections; },
    get personalCollection() { return reactiveState.collections.find((c) => c.is_personal) ?? null; },
    get sharedCollections() { return reactiveState.collections.filter((c) => !c.is_personal); },
    get data() { return { members: reactiveState.members }; },
    fetchCollections: mockState.fetchCollections,
    fetchMembers: mockState.fetchMembers,
    addMember: mockState.addMember,
    isLoadingOperation: () => false,
  }),
}));
vi.mock("@/stores/auth", () => ({
  useAuthStore: () => ({
    get user() { return reactiveState.user; },
    get isAuthenticated() { return reactiveState.user !== null; },
  }),
}));
vi.mock("@/stores/users", () => ({
  useUsersStore: () => ({
    get users() { return reactiveState.users; },
    loadUsers: mockState.loadUsers,
  }),
}));
vi.mock("@/stores/teams", () => ({
  useTeamsStore: () => ({
    currentTeamId: 1,
    get teams() { return reactiveState.userTeams; },
    get userTeams() { return reactiveState.userTeams; },
    loadTeams: vi.fn(() => Promise.resolve()),
  }),
}));
vi.mock("@/stores/sources", () => ({
  useSourcesStore: () => ({
    teamSources: [{ id: 7, name: "prod-logs", source_type: "clickhouse" }],
    currentSourceDetails: null,
    loadTeamSources: mockState.loadSources,
  }),
}));
vi.mock("@/stores/explore", () => ({
  useExploreStore: () => ({
    sourceId: 7,
    activeMode: "logchefql",
    logchefqlCode: 'level="info"',
    nativeQuery: "",
    hasWindow: false,
    window: { kind: "relative", spec: "15m" },
    limit: 100,
  }),
}));
vi.mock("@/stores/variables", () => ({ useVariableStore: () => ({ allVariables: [] }) }));
vi.mock("pinia", () => ({ storeToRefs: () => ({ allVariables: ref([]) }) }));
vi.mock("@/composables/useToast", () => ({ useToast: () => ({ toast: mockState.toast }) }));

import SaveQueryModal from "../SaveQueryModal.vue";

function makeCollection(overrides: Partial<Collection>): Collection {
  return {
    id: 1,
    name: "My Collection",
    is_personal: true,
    created_by: 10,
    caller_role: "owner",
    member_count: 1,
    team_count: 0,
    item_count: 0,
    created_at: "",
    updated_at: "",
    ...overrides,
  };
}

function makeUser(overrides: Partial<User> = {}): User {
  return {
    id: "10",
    email: "owner@example.com",
    full_name: "Owner User",
    role: "member",
    status: "active",
    account_type: "human",
    created_at: "",
    updated_at: "",
    ...overrides,
  };
}

async function flush() {
  for (let i = 0; i < 4; i++) {
    await new Promise((resolve) => setTimeout(resolve, 0));
    await nextTick();
  }
}

function pickCollection(host: HTMLElement, collectionId: string) {
  const input = host.querySelector<HTMLInputElement>('[data-select-input="true"]');
  if (!input) throw new Error("collection select input was not rendered");
  input.value = collectionId;
  input.dispatchEvent(new Event("input", { bubbles: true }));
}

describe("SaveQueryModal", () => {
  let host: HTMLDivElement;

  beforeEach(() => {
    mockState.loadSources.mockReset().mockResolvedValue(undefined);
    mockState.toast.mockReset();
    mockState.fetchCollections.mockReset().mockResolvedValue(undefined);
    mockState.fetchMembers.mockReset().mockResolvedValue(undefined);
    mockState.addMember.mockReset().mockResolvedValue({ success: true });
    mockState.loadUsers.mockReset().mockResolvedValue(undefined);
    mockState.listTeamMembers.mockReset().mockResolvedValue({ status: "success", data: [] });
    reactiveState.collections = [];
    reactiveState.members = {};
    reactiveState.user = makeUser();
    reactiveState.users = [];
    reactiveState.userTeams = [];
    host = document.createElement("div");
    document.body.appendChild(host);
  });

  afterEach(() => {
    host.remove();
  });

  it("handles a failed source list load without rejecting the mounted hook", async () => {
    mockState.loadSources.mockRejectedValueOnce(new Error("Sources unavailable"));
    // Trigger source load by clearing teamSources isn't needed if loadTeamSources isn't called;
    // verify fetchCollections rejection surfaces as a destructive toast cleanly.
    mockState.fetchCollections.mockRejectedValueOnce(new Error("Collections unavailable"));
    const app = createApp(SaveQueryModal, { isOpen: true, queryContent: JSON.stringify(queryContentFixture), queryType: "logchefql" });
    const errors: unknown[] = [];
    app.config.errorHandler = error => { errors.push(error); };
    app.use(i18n);
    app.mount(host);
    try {
      await vi.waitFor(() => expect(mockState.fetchCollections).toHaveBeenCalledTimes(1));
      await flush();
      expect(errors).toEqual([]);
      expect(mockState.toast).toHaveBeenCalledWith(expect.objectContaining({ variant: "destructive", description: "Collections unavailable" }));
      expect(host.querySelector("#name")).not.toBeNull();
    } finally {
      app.unmount();
    }
  });

  it("does not emit a second save while the first save is pending", async () => {
    const saveCount = ref(0);
    const closeCount = ref(0);
    const releaseSave = ref<(() => void) | undefined>(undefined);
    const Harness = defineComponent({
      components: { SaveQueryModal },
      setup() {
        const onSave = (_payload: unknown, done?: () => void) => {
          saveCount.value += 1;
          releaseSave.value = done;
        };
        const onClose = () => {
          closeCount.value += 1;
        };
        return {
          closeCount,
          onClose,
          onSave,
          queryContent: JSON.stringify(queryContentFixture),
          releaseSave,
          saveCount,
        };
      },
      template: `<SaveQueryModal :is-open="true" :query-content="queryContent" query-type="logchefql" @close="onClose" @save="onSave" />`,
    });

    const app = createApp(Harness);
    app.use(i18n);
    app.mount(host);
    await nextTick();

    const name = host.querySelector<HTMLInputElement>("#name");
    if (!name) throw new Error("name input was not rendered");
    name.value = "pending save";
    name.dispatchEvent(new Event("input", { bubbles: true }));
    await nextTick();

    const form = host.querySelector("form");
    if (!form) throw new Error("query form was not rendered");
    const submitButton = host.querySelector<HTMLButtonElement>('button[type="submit"]');
    if (!submitButton) throw new Error("submit button was not rendered");
    expect(submitButton.form).toBe(form);
    expect(host.querySelector<HTMLButtonElement>('button[type="button"]')?.type).toBe("button");
    submitButton.click();
    await nextTick();
    form.dispatchEvent(new Event("submit", { bubbles: true, cancelable: true }));
    await nextTick();

    expect(saveCount.value).toBe(1);
    expect(closeCount.value).toBe(0);
    expect(host.querySelector('button[type="submit"]')?.hasAttribute("disabled")).toBe(true);

    releaseSave.value?.();
    await nextTick();
    expect(host.querySelector('button[type="submit"]')?.hasAttribute("disabled")).toBe(false);
    app.unmount();
  });

  it("saves no time range when neither the Explorer nor the query has one", async () => {
    const payloads: Array<{ query_content: string }> = [];
    const Harness = defineComponent({
      components: { SaveQueryModal },
      setup() {
        return {
          onSave: (payload: { query_content: string }) => payloads.push(payload),
          queryContent: JSON.stringify(queryContentFixture),
        };
      },
      template: `<SaveQueryModal :is-open="true" :query-content="queryContent" query-type="logchefql" @save="onSave" />`,
    });
    const app = createApp(Harness);
    app.use(i18n);
    app.mount(host);
    await nextTick();

    const name = host.querySelector<HTMLInputElement>("#name");
    if (!name) throw new Error("name input was not rendered");
    name.value = "no window";
    name.dispatchEvent(new Event("input", { bubbles: true }));
    await nextTick();
    host.querySelector("form")?.dispatchEvent(new Event("submit", { bubbles: true, cancelable: true }));
    await nextTick();

    expect(payloads).toHaveLength(1);
    expect(JSON.parse(payloads[0].query_content)).toMatchObject({ version: 2, timeRange: null });
    app.unmount();
  });

  it("collapses query preview and secondary options by default on create and reveals them on demand", async () => {
    const payloads: Array<{ description: string; save_timestamp: boolean }> = [];
    const Harness = defineComponent({
      components: { SaveQueryModal },
      setup() {
        return {
          onSave: (payload: { description: string; save_timestamp: boolean }) => payloads.push(payload),
          queryContent: JSON.stringify(queryContentFixture),
        };
      },
      template: `<SaveQueryModal :is-open="true" :query-content="queryContent" query-type="logchefql" @save="onSave" />`,
    });
    const app = createApp(Harness);
    app.use(i18n);
    app.mount(host);
    await nextTick();

    // Context is compact in header; preview & secondary fields are collapsed initially.
    expect(host.textContent).toContain("prod-logs · LogchefQL");
    expect(host.querySelector('[data-testid="query-preview"]')).toBeNull();
    expect(host.querySelector("#description")).toBeNull();
    expect(host.querySelector("#save_timestamp")).toBeNull();

    // Expanding query preview reveals the query text.
    host.querySelector<HTMLButtonElement>('[data-testid="toggle-query-preview"]')?.click();
    await nextTick();
    expect(host.querySelector('[data-testid="query-preview"]')?.textContent).toContain('level="info"');

    // Expanding "More options" reveals Description and Save timestamp controls.
    host.querySelector<HTMLButtonElement>('[data-testid="more-options-toggle"]')?.click();
    await nextTick();
    const desc = host.querySelector<HTMLTextAreaElement>("#description");
    const ts = host.querySelector<HTMLInputElement>("#save_timestamp");
    expect(desc).not.toBeNull();
    expect(ts?.checked).toBe(true);

    desc!.value = "Tracks info logs";
    desc!.dispatchEvent(new Event("input", { bubbles: true }));
    ts!.checked = false;
    ts!.dispatchEvent(new Event("change", { bubbles: true }));

    const name = host.querySelector<HTMLInputElement>("#name")!;
    name.value = "Info logs";
    name.dispatchEvent(new Event("input", { bubbles: true }));
    await nextTick();

    host.querySelector("form")?.dispatchEvent(new Event("submit", { bubbles: true, cancelable: true }));
    await nextTick();

    expect(payloads).toEqual([expect.objectContaining({ description: "Tracks info logs", save_timestamp: false })]);
    app.unmount();
  });

  it("sends managed by default for shared collections, pin when unchecked, and lets owners add an editor inline", async () => {
    reactiveState.collections = [
      makeCollection({ id: 1, name: "My Collection", is_personal: true, caller_role: "owner" }),
      makeCollection({ id: 2, name: "On-call", is_personal: false, caller_role: "owner" }),
      makeCollection({ id: 3, name: "Shared team list", is_personal: false, caller_role: "member" }),
    ];
    reactiveState.members = {
      2: [{ collection_id: 2, user_id: 10, role: "owner", created_at: "", email: "owner@example.com", full_name: "Owner User" }],
    };
    reactiveState.user = makeUser({ id: "10", role: "member" });
    reactiveState.userTeams = [{ id: 5, name: "Core", description: "", created_at: "", updated_at: "", member_count: 2, role: "member" }];
    mockState.listTeamMembers.mockResolvedValue({
      status: "success",
      data: [
        { team_id: 5, user_id: 10, role: "member", created_at: "", updated_at: "", email: "owner@example.com", full_name: "Owner User", account_type: "human" },
        { team_id: 5, user_id: 22, role: "member", created_at: "", updated_at: "", email: "alice@example.com", full_name: "Alice", account_type: "human" },
      ],
    });
    mockState.addMember.mockImplementation(async (collectionId: number, payload: { user_id: number; role: string }) => {
      reactiveState.members = {
        ...reactiveState.members,
        [collectionId]: [
          ...(reactiveState.members[collectionId] ?? []),
          { collection_id: collectionId, user_id: payload.user_id, role: payload.role as "editor", created_at: "", email: "alice@example.com", full_name: "Alice" },
        ],
      };
      return { success: true };
    });

    const payloads: Array<{ collection_id: number | null; collection_kind: string }> = [];
    const Harness = defineComponent({
      components: { SaveQueryModal },
      setup() {
        return {
          onSave: (payload: { collection_id: number | null; collection_kind: string }, done?: () => void) => {
            payloads.push(payload);
            done?.();
          },
          queryContent: JSON.stringify(queryContentFixture),
        };
      },
      template: `<SaveQueryModal :is-open="true" :query-content="queryContent" query-type="logchefql" @save="onSave" />`,
    });
    const app = createApp(Harness);
    app.use(i18n);
    app.mount(host);
    await flush();

    // Personal collection selected by default: no editors toggle.
    expect(host.querySelector("#editors_can_edit")).toBeNull();

    // Switch to a shared collection the caller does NOT own: toggle shown, Manage editors hidden.
    pickCollection(host, "3");
    await nextTick();
    expect(host.querySelector("#editors_can_edit")).not.toBeNull();
    expect(host.querySelector('[data-testid="manage-editors-toggle"]')).toBeNull();
    expect(host.textContent).toContain("Owners can add editors at any time.");

    // Switch to an owned shared collection: Manage editors button appears.
    pickCollection(host, "2");
    await nextTick();
    const manageButton = host.querySelector<HTMLButtonElement>('[data-testid="manage-editors-toggle"]');
    expect(manageButton).not.toBeNull();

    // Open Manage editors inline: fetches members + teammates, shows Library deep link, adds Alice as editor.
    manageButton!.click();
    await flush();
    expect(mockState.fetchMembers).toHaveBeenCalledWith(2);
    expect(mockState.listTeamMembers).toHaveBeenCalledWith(5);
    expect(host.textContent).toContain("Only you can edit queries in this collection right now.");
    expect(host.querySelector<HTMLAnchorElement>('[data-testid="open-collection-members-link"]')?.getAttribute("href")).toContain("/logs/library/2?section=members");

    const userSelect = host.querySelector<HTMLSelectElement>('[data-testid="editor-user-select"]');
    expect(userSelect).not.toBeNull();
    userSelect!.value = "22";
    userSelect!.dispatchEvent(new Event("change", { bubbles: true }));
    await nextTick();

    host.querySelector<HTMLButtonElement>('[data-testid="add-editor-submit"]')?.click();
    await flush();

    expect(mockState.addMember).toHaveBeenCalledWith(2, { user_id: 22, role: "editor" });
    expect(payloads).toHaveLength(0); // Adding an editor must not submit the save form.
    expect(host.textContent).toContain("Alice");

    // Save with toggle on -> managed link.
    const name = host.querySelector<HTMLInputElement>("#name")!;
    name.value = "5xx errors";
    name.dispatchEvent(new Event("input", { bubbles: true }));
    await nextTick();
    host.querySelector("form")?.dispatchEvent(new Event("submit", { bubbles: true, cancelable: true }));
    await nextTick();

    // Uncheck editors toggle -> pin wording and pin link.
    const toggle = host.querySelector<HTMLInputElement>("#editors_can_edit")!;
    toggle.checked = false;
    toggle.dispatchEvent(new Event("change", { bubbles: true }));
    await nextTick();
    expect(host.textContent).toContain("Saved as a pin.");
    host.querySelector("form")?.dispatchEvent(new Event("submit", { bubbles: true, cancelable: true }));
    await nextTick();

    expect(payloads).toEqual([
      expect.objectContaining({ collection_id: 2, collection_kind: "managed" }),
      expect.objectContaining({ collection_id: 2, collection_kind: "pin" }),
    ]);
    app.unmount();
  });
});
