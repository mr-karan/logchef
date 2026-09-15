import { afterEach, describe, expect, it } from "vitest";
import { createApp, defineComponent, h, nextTick } from "vue";
import { useI18n } from "vue-i18n";
import { createPinia } from "pinia";
import DateTimePicker from "@/components/date-time-picker/DateTimePicker.vue";
import { detectLocale, isLocalePreference, locales } from "./locales";
import { i18n, setLocale } from "./index";

afterEach(async () => { await setLocale("en"); });

describe("interface languages", () => {
  it.each([
    [["en-US"], "en"], [["zh-Hant-HK"], "zh-TW"], [["zh-SG"], "zh-CN"],
    [["zh-Hans-HK"], "zh-CN"], [["zh-Hant-CN"], "zh-TW"],
    [["pt-PT"], "pt-BR"], [["xx", "fr-CA"], "fr"], [["xx"], "en"], [[], "en"],
  ])("detects %j as %s", (languages, expected) => {
    expect(detectLocale(languages)).toBe(expected);
  });

  it("rejects unsupported preference values", () => {
    for (const value of [null, {}, "../../en", "xx", ""]) expect(isLocalePreference(value)).toBe(false);
    for (const { code } of locales) expect(isLocalePreference(code)).toBe(true);
    expect(isLocalePreference("auto")).toBe(true);
  });

  it("updates mounted text without remounting or losing user input", async () => {
    const host = document.createElement("div");
    const component = defineComponent({ setup() {
      const { t } = useI18n();
      return () => h("section", [h("button", t("ui.run")), h("input", { value: 'level="error"' })]);
    } });
    const app = createApp(component).use(i18n);
    app.mount(host);
    try {
      const input = host.querySelector("input");
      await setLocale("zh-CN");
      await nextTick();
      expect(host.textContent).toBe("运行");
      expect(host.querySelector("input")).toBe(input);
      expect(input?.value).toBe('level="error"');
      expect(document.documentElement.lang).toBe("zh-CN");
      await setLocale("de");
      await nextTick();
      expect(host.textContent).toBe("Ausführen");
    } finally { app.unmount(); }
  });

  it("keeps the latest selection when language downloads overlap", async () => {
    await Promise.all([setLocale("ja"), setLocale("fr"), setLocale("it")]);
    expect(i18n.global.locale.value).toBe("it");
  });

  it("translates quick ranges without changing their stored identifiers", async () => {
    const host = document.createElement("div");
    const app = createApp(DateTimePicker, { selectedQuickRange: "Last 24h" }).use(createPinia()).use(i18n);
    app.mount(host);
    try {
      expect(host.textContent).toContain("Last 24 hours");
      await setLocale("de");
      await nextTick();
      expect(host.textContent).toContain("Letzte 24 Stunden");
      await setLocale("zh-CN");
      await nextTick();
      expect(host.textContent).toContain("过去 24 小时");
    } finally { app.unmount(); }
  });

  it("does not apply a selection after its component or account changes", async () => {
    const controller = new AbortController();
    const loading = setLocale("ko", controller.signal);
    controller.abort();
    await loading;
    expect(i18n.global.locale.value).toBe("en");
  });

  it("loads every advertised language and interpolates counts", async () => {
    for (const { code } of locales) {
      await setLocale(code);
      expect(i18n.global.locale.value).toBe(code);
      const message = i18n.global.t("dashboards.panelCount", { count: 2 }, 2);
      expect(message).toContain("2");
      expect(message).not.toContain("{count}");
      expect(message).not.toContain("dashboards.panelCount");
    }
  });
});
