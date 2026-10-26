import { createApp, nextTick } from "vue";
import { afterEach, beforeEach, expect, it, vi } from "vitest";
import { i18n } from "@/i18n";

const toastError = vi.hoisted(() => vi.fn());
vi.mock("vue-sonner", () => ({ toast: Object.assign(vi.fn(), { error: toastError, success: vi.fn() }) }));

import SourceInspectionSchema from "@/views/sources/components/SourceInspectionSchema.vue";

const createQuery = "CREATE TABLE logs.app (ts DateTime) ENGINE = MergeTree ORDER BY ts";
const clipboard = Object.getOwnPropertyDescriptor(navigator, "clipboard");
let unmount: (() => void) | undefined;

beforeEach(() => {
  // Plain-http deployments have no Clipboard API.
  Reflect.deleteProperty(navigator, "clipboard");
  expect("clipboard" in navigator).toBe(false);
});

afterEach(() => {
  unmount?.();
  unmount = undefined;
  document.body.innerHTML = "";
  Reflect.deleteProperty(document, "execCommand");
  if (clipboard) Object.defineProperty(navigator, "clipboard", clipboard);
  else Reflect.deleteProperty(navigator, "clipboard");
  toastError.mockReset();
});

async function clickCopy(execCommand: (command: string) => boolean) {
  Object.defineProperty(document, "execCommand", { value: vi.fn(execCommand), configurable: true });
  const host = document.createElement("div");
  document.body.append(host);
  const app = createApp(SourceInspectionSchema, { schema: { fields: [], create_query: createQuery } }).use(i18n);
  app.mount(host);
  unmount = () => app.unmount();
  const button = host.querySelector<HTMLButtonElement>(`button[aria-label="${i18n.global.t("inspection.copyQuery")}"]`);
  if (!button) throw new Error("no copy button");
  button.click();
  await vi.waitFor(() => expect(document.execCommand).toHaveBeenCalledWith("copy"));
  await nextTick();
  return button;
}

it("copies the CREATE TABLE query without the Clipboard API", async () => {
  let copiedText = "";
  const button = await clickCopy(() => {
    copiedText = document.querySelector("textarea")?.value ?? "";
    return true;
  });

  expect(copiedText).toBe(createQuery);
  expect(button.textContent).toContain(i18n.global.t("pages.copied"));
  expect(toastError).not.toHaveBeenCalled();
});

it("reports a failed copy when the fallback fails too", async () => {
  const button = await clickCopy(() => false);

  await vi.waitFor(() => expect(toastError).toHaveBeenCalledWith(i18n.global.t("pages.copyFailed"), expect.anything()));
  expect(button.textContent).not.toContain(i18n.global.t("pages.copied"));
});
