import { readFileSync, readdirSync } from "node:fs";
import { resolve } from "node:path";
import { describe, expect, it } from "vitest";
import { createI18n } from "vue-i18n";
import { locales } from "./locales";
import type { SourceFieldGroupId } from "@/lib/sourceFields";
import { alertStateLabels, severityLabels } from "./alertLabels";

function catalog(code: string): Record<string, string> {
  const data: unknown = JSON.parse(readFileSync(resolve("src/i18n/locales", `${code}.json`), "utf8"));
  if (typeof data !== "object" || data === null || Array.isArray(data)) throw new Error(`Invalid catalog: ${code}`);
  const messages: Record<string, string> = {};
  for (const [key, value] of Object.entries(data)) {
    if (typeof value !== "string" || !value.trim()) throw new Error(`Empty or invalid message: ${code}:${key}`);
    messages[key] = value;
  }
  return messages;
}

function placeholders(message: string): string[] {
  return [...new Set([...message.matchAll(/\{(\w+)\}/g)].map(match => match[1]))].sort();
}

const english = catalog("en");

describe("translation catalogs", () => {
  it("covers dynamically selected field groups and alert labels", () => {
    const groups: Record<SourceFieldGroupId, boolean> = { core: true, context: true, filterable: true, system: true, other: true };
    const keys = [
      ...Object.keys(groups).flatMap(group => [`fields.${group}.label`, `fields.${group}.description`]),
      ...Object.values(alertStateLabels),
      ...Object.values(severityLabels),
    ];
    for (const key of keys) expect(Object.hasOwn(english, key), key).toBe(true);
  });
  it("registers every catalog exactly once", () => {
    const files = readdirSync(resolve("src/i18n/locales")).map(name => name.replace(/\.json$/, "")).sort();
    expect(locales.map(locale => locale.code).sort()).toEqual(files);
  });

  it.each(locales)("validates every $code message and interpolation", ({ code }) => {
    const messages = catalog(code);
    expect(Object.keys(messages).sort()).toEqual(Object.keys(english).sort());
    const translator = createI18n({ legacy: false, locale: code, fallbackLocale: false, flatJson: true, messages: { [code]: { ...messages } } });
    try {
      for (const [key, message] of Object.entries(messages)) {
        expect(placeholders(message), `${code}:${key}`).toEqual(placeholders(english[key]));
        expect(message, `${code}:${key} must not contain HTML`).not.toMatch(/<\/?[a-z][^>]*>/i);
        const values: Record<string, string | number> = Object.fromEntries(placeholders(message).map(name => [name, name === "count" ? 2 : "sample"]));
        for (const count of [0, 1, 2]) {
          const rendered = translator.global.t(key, { ...values, count }, count);
          expect(rendered, `${code}:${key}`).not.toBe(key);
          expect(rendered, `${code}:${key}`).not.toMatch(/\{\w+\}/);
        }
      }
    } finally { translator.dispose(); }
  });

  it("resolves all literal translation keys used by the frontend", () => {
    const root = resolve("src");
    for (const name of readdirSync(root, { recursive: true, encoding: "utf8" })) {
      if (!/\.(vue|ts)$/.test(name) || name.endsWith(".test.ts")) continue;
      const source = readFileSync(`${root}/${name}`, "utf8");
      const references = [...source.matchAll(/\bt\(\s*["']([^"']+)["']|keypath=["']([^"']+)["']/g)];
      for (const match of references) expect(Object.hasOwn(english, match[1] ?? match[2]), `${name}: ${match[1] ?? match[2]}`).toBe(true);
    }
  });
});
