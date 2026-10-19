// @vitest-environment node
import { dirname, relative, resolve } from "node:path";
import { proxyCreateProgram } from "@volar/typescript";
import { toSourceOffset } from "@volar/typescript/lib/node/transform";
import { getServiceScript } from "@volar/typescript/lib/node/utils";
import { createParsedCommandLine, createVueLanguagePlugin, type Language } from "@vue/language-core";
import ts from "typescript";
import { describe, expect, it } from "vitest";

// Types keep a zone paired with its value inside the time module, but cannot stop
// a new file from reading or formatting in the browser zone. These rules can. They
// run on the type checker over the same program vue-tsc builds, so `.vue` script
// and template code is checked by type, not by spelling.

// A query variable's date is zone-less wall-clock text; the user's SQL names its
// zone. Browser-zone Date fields only round-trip that text.
const QUERY_VARIABLE_DATE = [/^components\/date-time-picker\/SingleDatePicker\.vue$/, /^components\/query-editor\/variableUtils\.ts$/];

type Rule = { name: string; allowed: RegExp[]; flags: (node: ts.Node, checker: ts.TypeChecker) => boolean };

const root = resolve(import.meta.dirname, "..");
const config = createParsedCommandLine(ts, ts.sys, resolve(root, "../tsconfig.app.json"));

const LOCALE_METHODS = new Set(["toLocaleString", "toLocaleDateString", "toLocaleTimeString"]);

const libDir = dirname(ts.getDefaultLibFilePath(config.options));

function isLibFile(declaration: ts.Declaration): boolean {
  return dirname(declaration.getSourceFile().fileName) === libDir;
}

function isLibInterface(symbol: ts.Symbol | undefined, name: string): boolean {
  return symbol?.declarations?.some((d) => ts.isInterfaceDeclaration(d) && d.name.text === name && isLibFile(d)) ?? false;
}

function isLibMember(symbol: ts.Symbol | undefined, owner: string): boolean {
  return symbol?.declarations?.some((d) => ts.isInterfaceDeclaration(d.parent) && d.parent.name.text === owner && isLibFile(d)) ?? false;
}

// The member name a call or read uses: `x.name`, or `x["name"]` with any string-literal-typed key.
function memberKey(node: ts.PropertyAccessExpression | ts.ElementAccessExpression, checker: ts.TypeChecker): string | undefined {
  if (ts.isPropertyAccessExpression(node)) return node.name.text;
  const key = checker.getTypeAtLocation(node.argumentExpression);
  return key.isStringLiteral() ? key.value : undefined;
}

function isMemberAccess(node: ts.Node): node is ts.PropertyAccessExpression | ts.ElementAccessExpression {
  return ts.isPropertyAccessExpression(node) || ts.isElementAccessExpression(node);
}

// An options argument counts only when its type always carries a timeZone.
function setsTimeZone(options: ts.Expression | undefined, checker: ts.TypeChecker): boolean {
  const timeZone = options && checker.getTypeAtLocation(options).getProperty("timeZone");
  return timeZone !== undefined && (timeZone.flags & ts.SymbolFlags.Optional) === 0;
}

function calleeFunction(node: ts.CallExpression, checker: ts.TypeChecker): ts.Declaration | undefined {
  return checker.getTypeAtLocation(node.expression).getSymbol()?.valueDeclaration;
}

function isDateConstruction(node: ts.Node, checker: ts.TypeChecker): node is ts.NewExpression {
  return ts.isNewExpression(node) && isLibInterface(checker.getTypeAtLocation(node.expression).getSymbol(), "DateConstructor");
}

function bindingKey(element: ts.BindingElement): string | undefined {
  const key = element.propertyName ?? element.name;
  return ts.isIdentifier(key) || ts.isStringLiteral(key) ? key.text : undefined;
}

const RULES: Rule[] = [
  {
    name: "Date formatted with toLocale*String and no timeZone",
    allowed: QUERY_VARIABLE_DATE,
    flags: (node, checker) => {
      if (!ts.isCallExpression(node) || !isMemberAccess(node.expression)) return false;
      const member = node.expression;
      const key = memberKey(member, checker);
      if (key === undefined || !LOCALE_METHODS.has(key)) return false;
      const method = checker.getApparentType(checker.getTypeAtLocation(member.expression)).getProperty(key);
      return isLibMember(method, "Date") && !setsTimeZone(node.arguments[1], checker);
    },
  },
  {
    name: "Intl.DateTimeFormat with no timeZone",
    allowed: [],
    flags: (node, checker) =>
      (ts.isNewExpression(node) || ts.isCallExpression(node)) &&
      isLibInterface(checker.getTypeAtLocation(node.expression).getSymbol(), "DateTimeFormatConstructor") &&
      !setsTimeZone(node.arguments?.[1], checker),
  },
  {
    name: "resolvedOptions().timeZone",
    allowed: [],
    flags: (node, checker) => {
      if (isMemberAccess(node) && memberKey(node, checker) === "timeZone") {
        return isLibMember(checker.getTypeAtLocation(node.expression).getProperty("timeZone"), "ResolvedDateTimeFormatOptions");
      }
      if (ts.isBindingElement(node) && ts.isObjectBindingPattern(node.parent) && bindingKey(node) === "timeZone") {
        return isLibMember(checker.getTypeAtLocation(node.parent).getProperty("timeZone"), "ResolvedDateTimeFormatOptions");
      }
      return false;
    },
  },
  {
    name: "zoneFromPreference with a fixed preference",
    allowed: [/^utils\/timeWindow[^/]*\.ts$/],
    flags: (node, checker) => {
      if (!ts.isCallExpression(node) || node.arguments.length === 0) return false;
      const fn = calleeFunction(node, checker);
      return (
        fn !== undefined &&
        ts.isFunctionDeclaration(fn) &&
        fn.name?.text === "zoneFromPreference" &&
        fn.getSourceFile().fileName === resolve(root, "utils/timeWindow.ts") &&
        checker.getTypeAtLocation(node.arguments[0]).isStringLiteral()
      );
    },
  },
  {
    name: "@internationalized/date import",
    allowed: [/^utils\/timeWindow[^/]*\.ts$/, /^components\/date-time-picker\//, /^components\/ui\/calendar\//],
    flags: (node) => {
      const specifier =
        ts.isImportDeclaration(node) || ts.isExportDeclaration(node)
          ? node.moduleSpecifier
          : ts.isCallExpression(node) && node.expression.kind === ts.SyntaxKind.ImportKeyword
            ? node.arguments[0]
            : undefined;
      return specifier !== undefined && ts.isStringLiteralLike(specifier) && specifier.text === "@internationalized/date";
    },
  },
  {
    name: "getLocalTimeZone",
    allowed: [/^utils\/timeWindow\.ts$/, /^components\/ui\/calendar\//, ...QUERY_VARIABLE_DATE],
    flags: (node, checker) => {
      if (!ts.isCallExpression(node)) return false;
      const fn = calleeFunction(node, checker);
      return fn !== undefined && ts.isFunctionDeclaration(fn) && fn.name?.text === "getLocalTimeZone" && fn.getSourceFile().fileName.includes("/@internationalized/date/");
    },
  },
  {
    // new Date(year, month, ...) reads its fields in the browser zone.
    name: "Date built from wall-clock fields",
    allowed: QUERY_VARIABLE_DATE,
    flags: (node, checker) => isDateConstruction(node, checker) && ((node.arguments?.length ?? 0) > 1 || (node.arguments?.some(ts.isSpreadElement) ?? false)),
  },
];

type Offense = { rule: string; file: string; line: number };

function isScanned(fileName: string): boolean {
  const name = relative(root, fileName);
  return !name.startsWith("..") && !name.includes("__tests__/") && !/\.(test|spec)\.ts$/.test(name) && !name.endsWith(".d.ts");
}

function buildProgram(rootNames: string[], host: ts.CompilerHost): { program: ts.Program; language: Language<string> } {
  const vue: { language?: Language<string> } = {};
  const createProgram = proxyCreateProgram(ts, ts.createProgram, (_, { options }) => ({
    languagePlugins: [createVueLanguagePlugin(ts, options, config.vueOptions, (id) => id)],
    setup: (language) => {
      vue.language = language;
    },
  }));
  const program = createProgram({ rootNames, options: config.options, host });
  if (!vue.language) throw new Error("vue language plugin did not start");
  return { program, language: vue.language };
}

// The 1-based line in the file's own text; `.vue` positions map back from the generated code.
function sourceLine(language: Language<string>, file: ts.SourceFile, node: ts.Node): number | undefined {
  const [serviceScript, , sourceScript] = getServiceScript(language, file.fileName);
  if (!serviceScript || !sourceScript) return file.getLineAndCharacterOfPosition(node.getStart(file)).line + 1;
  const text = sourceScript.snapshot.getText(0, sourceScript.snapshot.getLength());
  for (let position = node.getStart(file); position < node.getEnd(); position++) {
    const mapped = toSourceOffset(sourceScript, language, serviceScript, position, () => true);
    if (mapped) return text.slice(0, mapped[1]).split("\n").length;
  }
  return undefined;
}

function findOffenses(rootNames: string[], host: ts.CompilerHost = ts.createCompilerHost(config.options)): Offense[] {
  const { program, language } = buildProgram(rootNames, host);
  const checker = program.getTypeChecker();
  const offenses: Offense[] = [];
  // getSourceFile, not getSourceFiles: the tsgo bridge lists .ts files with no parsed statements.
  for (const file of rootNames.map((name) => program.getSourceFile(name))) {
    if (!file) throw new Error("scanned file missing from the program");
    const visit = (node: ts.Node): void => {
      for (const rule of RULES.filter((r) => r.flags(node, checker))) {
        const line = sourceLine(language, file, node);
        if (line !== undefined) offenses.push({ rule: rule.name, file: relative(root, file.fileName), line });
      }
      ts.forEachChild(node, visit);
    };
    visit(file);
  }
  return offenses.sort((a, b) => a.file.localeCompare(b.file) || a.line - b.line || a.rule.localeCompare(b.rule));
}

// Scans in-memory files placed in src, so `@/` imports resolve as they do for real code.
function findFixtureOffenses(files: Record<string, string>): Offense[] {
  const paths = new Map(Object.entries(files).map(([name, text]) => [resolve(root, "__guard_fixtures__", name), text]));
  const host = ts.createCompilerHost(config.options);
  const { fileExists, readFile, getSourceFile } = host;
  host.fileExists = (name) => paths.has(name) || fileExists(name);
  host.readFile = (name) => paths.get(name) ?? readFile(name);
  host.getSourceFile = (name, target, ...rest) => {
    const text = paths.get(name);
    return text === undefined ? getSourceFile(name, target, ...rest) : ts.createSourceFile(name, text, target);
  };
  return findOffenses([...paths.keys()], host);
}

function offensesBy(offenses: Offense[]): Record<string, string[]> {
  const grouped: Record<string, string[]> = {};
  for (const { rule, file, line } of offenses) (grouped[file.replace("__guard_fixtures__/", "")] ??= []).push(`${line}: ${rule}`);
  return grouped;
}

describe("time zone guard", () => {
  const sourceOffenses = findOffenses(ts.sys.readDirectory(root, [".ts", ".tsx", ".vue"]).filter(isScanned));

  it("scans .ts files and .vue scripts and templates", () => {
    const fixtures = findFixtureOffenses({
      "Template.vue": ['<script setup lang="ts">', "const ts = 0;", "</script>", "<template>", "  <span>{{ new Date(ts).toLocaleString() }}</span>", "  <b>{{ ts.toLocaleString() }}</b>", "</template>"].join("\n"),
    });
    expect(offensesBy(fixtures)).toEqual({ "Template.vue": ["5: Date formatted with toLocale*String and no timeZone"] });
    expect(sourceOffenses).toEqual(
      expect.arrayContaining([
        { rule: "getLocalTimeZone", file: "utils/timeWindow.ts", line: expect.any(Number) },
        { rule: "Date formatted with toLocale*String and no timeZone", file: "components/query-editor/variableUtils.ts", line: expect.any(Number) },
        { rule: "getLocalTimeZone", file: "components/ui/calendar/CalendarMonthGrid.vue", line: 150 },
      ]),
    );
  });

  it.each(RULES)("allows $name only where its zone is explicit", ({ name, allowed }) => {
    const found = sourceOffenses.filter((o) => o.rule === name && !allowed.some((pattern) => pattern.test(o.file))).map((o) => `${o.file}:${o.line}`);
    expect(found).toEqual([]);
  });

  it("flags each way of reading or formatting in the browser zone", () => {
    const fixtures = findFixtureOffenses({
      "chained.ts": "const ts = 0;\nexport const s = new Date(ts)\n  .toLocaleString();\n",
      "wrapped.ts": "const ts = 0;\nexport const s = new Date(\n  ts\n).toLocaleString();\n",
      "parenthesized.ts": "const ts = 0;\nconst d = (new Date(ts));\nexport const s = d.toLocaleString();\n",
      "elementAccess.ts": 'export const zone = new Intl.DateTimeFormat("en", { timeZone: "UTC" }).resolvedOptions()["timeZone"];\n',
      "destructured.ts": "const { timeZone } = new Intl.DateTimeFormat().resolvedOptions();\nexport { timeZone };\n",
      "template.ts": 'import { zoneFromPreference } from "@/utils/timeWindow";\nexport const zone = zoneFromPreference(`local`);\n',
      "constant.ts": 'import { zoneFromPreference as zoneOf } from "@/utils/timeWindow";\nconst pref = "utc";\nexport const zone = zoneOf(pref);\n',
      "formatter.ts": 'export const f = Intl.DateTimeFormat("en", { dateStyle: "short" });\n',
      "optionalZone.ts": 'declare const opts: Intl.DateTimeFormatOptions;\nexport const s = new Date(0).toLocaleString("en", opts);\n',
      "fields.ts": "declare const at: { year: number; month: number };\nexport const d = new Date(at.year, at.month - 1);\n",
      "imports.ts": 'import { getLocalTimeZone as here } from "@internationalized/date";\nexport const zone = here();\n',
    });
    expect(offensesBy(fixtures)).toEqual({
      "chained.ts": ["2: Date formatted with toLocale*String and no timeZone"],
      "wrapped.ts": ["2: Date formatted with toLocale*String and no timeZone"],
      "parenthesized.ts": ["3: Date formatted with toLocale*String and no timeZone"],
      "elementAccess.ts": ["1: resolvedOptions().timeZone"],
      "destructured.ts": ["1: Intl.DateTimeFormat with no timeZone", "1: resolvedOptions().timeZone"],
      "template.ts": ["2: zoneFromPreference with a fixed preference"],
      "constant.ts": ["3: zoneFromPreference with a fixed preference"],
      "formatter.ts": ["1: Intl.DateTimeFormat with no timeZone"],
      "optionalZone.ts": ["2: Date formatted with toLocale*String and no timeZone"],
      "fields.ts": ["2: Date built from wall-clock fields"],
      "imports.ts": ["1: @internationalized/date import", "2: getLocalTimeZone"],
    });
  });

  it("leaves numbers and zone-explicit formatting alone", () => {
    const fixtures = findFixtureOffenses({
      "allowed.ts": [
        'import { zoneFromPreference } from "@/utils/timeWindow";',
        'declare const pref: "utc" | "local";',
        "const count = 1234;",
        "export const a = count.toLocaleString();",
        'export const b = count.toLocaleString("en", { maximumFractionDigits: 1 });',
        'export const c = new Date(0).toLocaleString("en", { timeZone: "UTC" });',
        'export const d = new Intl.DateTimeFormat("en", { ...{ dateStyle: "short" as const }, timeZone: "UTC" });',
        "export const e = zoneFromPreference(pref);",
        "export const f = new Date(Date.UTC(2024, 0, 1));",
        "export const g = new Date(count);",
        'export const h = new Intl.NumberFormat("en").resolvedOptions().locale;',
      ].join("\n"),
    });
    expect(fixtures).toEqual([]);
  });
});
