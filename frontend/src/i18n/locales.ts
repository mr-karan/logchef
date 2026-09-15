export const locales = [
  { code: "en", name: "English" },
  { code: "zh-CN", name: "简体中文" },
  { code: "zh-TW", name: "繁體中文" },
  { code: "es", name: "Español" },
  { code: "fr", name: "Français" },
  { code: "de", name: "Deutsch" },
  { code: "pt-BR", name: "Português (Brasil)" },
  { code: "ja", name: "日本語" },
  { code: "ko", name: "한국어" },
  { code: "hi", name: "हिन्दी" },
  { code: "it", name: "Italiano" },
] as const;

export type Locale = (typeof locales)[number]["code"];
export type LocalePreference = Locale | "auto";

export function isLocale(value: unknown): value is Locale {
  return locales.some((locale) => locale.code === value);
}

export function isLocalePreference(value: unknown): value is LocalePreference {
  return value === "auto" || isLocale(value);
}

export function detectLocale(languages: readonly string[]): Locale {
  for (const language of languages) {
    const tag = language.toLowerCase().replaceAll("_", "-");
    const exact = locales.find((locale) => locale.code.toLowerCase() === tag);
    if (exact) return exact.code;
    const base = tag.split("-")[0];
    if (base === "zh") {
      if (tag.includes("-hans")) return "zh-CN";
      if (tag.includes("-hant")) return "zh-TW";
      return /-(hant|tw|hk|mo)(-|$)/.test(tag) ? "zh-TW" : "zh-CN";
    }
    if (base === "pt") return "pt-BR";
    if (isLocale(base)) return base;
  }
  return "en";
}
