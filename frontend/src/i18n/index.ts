import { createI18n } from "vue-i18n";
import en from "./locales/en.json";
import { detectLocale, type Locale, type LocalePreference } from "./locales";

export type MessageKey = keyof typeof en;

declare module "vue-i18n" {
  export interface DefineLocaleMessage extends Partial<Record<MessageKey, string>> {}
}

export const i18n = createI18n<[Record<string, string>], string, false>({
  legacy: false,
  locale: "en",
  fallbackLocale: "en",
  flatJson: true,
  messages: { en },
});

const loaders = import.meta.glob<{ default: Record<string, string> }>("./locales/*.json");
const pending = new Map<Locale, Promise<void>>();
let request = 0;

async function loadLocale(locale: Locale): Promise<void> {
  if (i18n.global.availableLocales.includes(locale)) return;
  const existing = pending.get(locale);
  if (existing) return existing;
  const loader = loaders[`./locales/${locale}.json`];
  if (!loader) throw new Error(`Missing language pack: ${locale}`);
  const loading = loader().then((module) => {
    i18n.global.setLocaleMessage(locale, module.default);
  }).finally(() => pending.delete(locale));
  pending.set(locale, loading);
  return loading;
}

export async function setLocale(preference: LocalePreference, signal?: AbortSignal): Promise<void> {
  const id = ++request;
  const locale = preference === "auto" ? detectLocale(navigator.languages) : preference;
  await loadLocale(locale);
  // An earlier download must not undo a newer selection.
  if (id !== request || signal?.aborted) return;
  i18n.global.locale.value = locale;
  document.documentElement.lang = locale;
  document.documentElement.dir = "ltr";
}
