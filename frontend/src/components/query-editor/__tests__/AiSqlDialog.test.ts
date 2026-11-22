import { afterEach, expect, it } from "vitest";
import { createApp, h, nextTick, type App } from "vue";
import { i18n } from "@/i18n";
import AiSqlDialog from "../AiSqlDialog.vue";

let app: App | undefined;
afterEach(() => {
  app?.unmount();
  document.body.innerHTML = "";
});

it.each(["SQL", "LogsQL", "LogQL"])("names %s in the generation dialog", async (language) => {
  const host = document.createElement("div");
  document.body.append(host);
  app = createApp({
    render: () => h(AiSqlDialog, { open: true, isGenerating: false, error: null, generatedSql: null, language }),
  }).use(i18n);
  app.mount(host);
  await nextTick();

  const dialog = document.querySelector('[role="dialog"]');
  expect(dialog?.querySelector("h2")?.textContent?.trim()).toBe(`Generate ${language}`);
  expect(dialog?.textContent).toContain(`Generated ${language}`);
  expect([...dialog?.querySelectorAll("button") ?? []].some(button => button.textContent?.trim() === `Generate ${language}`)).toBe(true);
});
