import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { createApp, defineComponent, nextTick, ref } from "vue";

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
  return { passthrough, inputStub, textareaStub, checkboxStub };
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
  Select: stubs.passthrough("div"),
  SelectContent: stubs.passthrough("div"),
  SelectItem: stubs.passthrough("div"),
  SelectTrigger: stubs.passthrough("button"),
  SelectValue: stubs.passthrough("span"),
}));
vi.mock("@/api/sources", () => ({ asClickHouseConnection: () => null }));
vi.mock("@/stores/savedQueries", () => ({
  useSavedQueriesStore: () => ({
    data: { teams: [] },
    fetchUserTeams: vi.fn(() => Promise.resolve()),
  }),
}));
vi.mock("@/stores/collections", () => ({
  useCollectionsStore: () => ({
    collections: [],
    personalCollection: null,
    sharedCollections: [],
    fetchCollections: vi.fn(() => Promise.resolve()),
  }),
}));
vi.mock("@/stores/teams", () => ({
  useTeamsStore: () => ({ currentTeamId: 1, teams: [], loadTeams: vi.fn(() => Promise.resolve()) }),
}));
vi.mock("@/stores/sources", () => ({
  useSourcesStore: () => ({
    teamSources: [],
    currentSourceDetails: null,
    loadTeamSources: vi.fn(() => Promise.resolve()),
  }),
}));
vi.mock("@/stores/explore", () => ({
  useExploreStore: () => ({
    sourceId: 7,
    activeMode: "logchefql",
    logchefqlCode: 'level="info"',
    nativeQuery: "",
    selectedRelativeTime: null,
    timeRange: null,
    limit: 100,
  }),
}));
vi.mock("@/stores/variables", () => ({ useVariableStore: () => ({ allVariables: [] }) }));
vi.mock("pinia", () => ({ storeToRefs: () => ({ allVariables: ref([]) }) }));
vi.mock("vue-router", () => ({ useRoute: () => ({ query: {} }) }));
vi.mock("@/composables/useToast", () => ({ useToast: () => ({ toast: vi.fn() }) }));

import SaveQueryModal from "../SaveQueryModal.vue";

describe("SaveQueryModal submission lock", () => {
  let host: HTMLDivElement;

  beforeEach(() => {
    host = document.createElement("div");
    document.body.appendChild(host);
  });

  afterEach(() => {
    host.remove();
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
});
