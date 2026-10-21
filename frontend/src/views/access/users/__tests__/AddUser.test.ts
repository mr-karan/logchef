import { afterEach, describe, expect, it, vi } from "vitest";
import { createApp, nextTick } from "vue";
import { createPinia } from "pinia";
import { i18n } from "@/i18n";

const server = { createdEmails: [] as string[] };

vi.mock("@/api/users", async (importOriginal) => ({
  ...(await importOriginal<typeof import("@/api/users")>()),
  usersApi: {
    createUser: (payload: { email: string }) => {
      server.createdEmails.push(payload.email);
      return Promise.resolve({ status: "success", data: { id: "1", ...payload } });
    },
    listUsers: () => Promise.reject({ status: "error", message: "list failed", error_type: "DatabaseError" }),
  },
}));

const { default: AddUser } = await import("../AddUser.vue");

let host: HTMLElement | null = null;
let unmount: (() => void) | null = null;

async function flush() {
  for (let i = 0; i < 5; i++) {
    await new Promise((resolve) => setTimeout(resolve, 0));
    await nextTick();
  }
}

function dialog(): HTMLElement | null {
  return document.body.querySelector<HTMLElement>("[role=dialog]");
}

function fill(id: string, value: string) {
  const input = dialog()?.querySelector<HTMLInputElement>(`#${id}`);
  if (!input) throw new Error(`input ${id} not found`);
  input.value = value;
  input.dispatchEvent(new Event("input"));
}

afterEach(() => {
  unmount?.();
  unmount = null;
  host?.remove();
  host = null;
  server.createdEmails = [];
});

describe("AddUser", () => {
  it("closes the dialog when the user is created even if the list refresh fails", async () => {
    vi.spyOn(console, "error").mockImplementation(() => {});
    host = document.createElement("div");
    document.body.appendChild(host);
    const app = createApp(AddUser);
    app.use(createPinia());
    app.use(i18n);
    app.mount(host);
    unmount = () => app.unmount();

    host.querySelector("button")?.click();
    await flush();
    fill("full_name", "Customer A");
    fill("email", "user@example.com");
    await flush();
    dialog()?.querySelector("form")?.dispatchEvent(new Event("submit", { cancelable: true }));
    await flush();

    expect(server.createdEmails).toEqual(["user@example.com"]);
    expect(dialog()).toBeNull();
  });
});
