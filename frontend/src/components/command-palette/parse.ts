import { relativeWindow, type TimeWindow } from "@/utils/timeWindow";

/** Half-open span [start, end) into the input text, in UTF-16 units like selectionStart. */
export interface Span {
  readonly start: number;
  readonly end: number;
}

export type TokenKind =
  | "askMarker"
  | "askText"
  | "tagAt"
  | "tagTeam"
  | "tagSlash"
  | "tagSource"
  | "rangeKey"
  | "rangeValue"
  | "field"
  | "op"
  | "value"
  | "string"
  | "keyword"
  | "paren"
  | "pipe"
  | "space"
  | "text"
  | "invalid";

/** Tokens tile the text: contiguous, non-empty, covering [0, text.length). */
export interface Token extends Span {
  readonly kind: TokenKind;
}

/** The leading `@team[/source]` word, as typed. */
export interface TagToken extends Span {
  readonly team: string;
  readonly teamId: number | null;
  readonly sourceId: number | null;
  readonly slash: number | null;
  /** null until a "/" is typed. */
  readonly source: string | null;
}

export type RelativeWindow = Extract<TimeWindow, { kind: "relative" }>;

export type RangeToken =
  | (Span & { readonly kind: "valid"; readonly text: string; readonly window: RelativeWindow })
  | (Span & { readonly kind: "invalid"; readonly text: string });

/** What the word under the caret completes to. A completion replaces the whole word. */
export type CompletionSlot =
  | { readonly kind: "team"; readonly prefix: string; readonly replace: Span }
  | { readonly kind: "source"; readonly team: string; readonly prefix: string; readonly replace: Span }
  | { readonly kind: "range"; readonly prefix: string; readonly replace: Span };

export type ParseIssue =
  | { readonly kind: "extraTag"; readonly span: Span }
  | { readonly kind: "duplicateRange"; readonly span: Span }
  | { readonly kind: "invalidRange"; readonly span: Span; readonly text: string }
  | { readonly kind: "tooLong"; readonly limit: number };

export interface ParsedInput {
  readonly text: string;
  readonly tokens: readonly Token[];
  /** Set only when the first word is a tag. Its presence is query mode. */
  readonly tag: TagToken | null;
  /** The first standalone `last:` word. */
  readonly range: RangeToken | null;
  /** The text without the tag and range words, trimmed. */
  readonly query: string;
  readonly ask: Span | null;
  readonly slot: CompletionSlot | null;
  /** The selection end, clamped to the text. */
  readonly caret: number;
  /** False while a range of text is selected: no ghost text and no Tab completion then. */
  readonly collapsed: boolean;
  readonly issues: readonly ParseIssue[];
}

export interface InputEdit {
  readonly text: string;
  readonly caret: number;
}

export const MAX_INPUT_LENGTH = 2_000;

const TAG = /^@([a-z0-9_.-]*)(?:#([0-9]+))?(?:\/([a-z0-9_.-]*)(?:#([0-9]+))?)?$/i;
const RANGE = /^(last:)([a-z0-9]*)$/i;
const KEYWORDS = new Set(["and", "or"]);

const LEXEMES: readonly [RegExp, Lexeme["kind"]][] = [
  [/[ \t\n\r]+/y, "space"],
  [/"(?:[^"\\]|\\[\s\S]?)*(?:"|$)|'(?:[^'\\]|\\[\s\S]?)*(?:'|$)/y, "string"],
  [/!=|!~|>=|<=|[=~><]/y, "op"],
  [/\|/y, "pipe"],
  [/[()]/y, "paren"],
  [/[-+]?[0-9]*\.?[0-9]+/y, "number"],
  [/@?[a-zA-Z_][a-zA-Z0-9_:@-]*(?:\.[a-zA-Z_][a-zA-Z0-9_:@-]*)*/y, "ident"],
];

export interface Lexeme extends Span {
  readonly kind: "space" | "string" | "op" | "pipe" | "paren" | "number" | "ident" | "other";
}

export function lex(text: string): Lexeme[] {
  const lexemes: Lexeme[] = [];
  let at = 0;
  while (at < text.length) {
    let matched: Lexeme | null = null;
    for (const [pattern, kind] of LEXEMES) {
      pattern.lastIndex = at;
      const match = pattern.exec(text);
      if (match && match[0].length > 0) {
        matched = { kind, start: at, end: at + match[0].length };
        break;
      }
    }
    const lexeme = matched ?? { kind: "other", start: at, end: at + 1 };
    lexemes.push(lexeme);
    at = lexeme.end;
  }
  return lexemes;
}

interface Word extends Span {
  readonly lexemes: readonly Lexeme[];
  /** Parenthesis depth where the word starts. */
  readonly depth: number;
}

function words(text: string, lexemes: readonly Lexeme[]): Word[] {
  const result: Word[] = [];
  let depth = 0;
  let current: Lexeme[] = [];
  let currentDepth = 0;
  const flush = () => {
    if (current.length === 0) return;
    result.push({ start: current[0].start, end: current[current.length - 1].end, lexemes: current, depth: currentDepth });
    current = [];
  };
  for (const lexeme of lexemes) {
    if (lexeme.kind === "space") {
      flush();
      continue;
    }
    if (current.length === 0) currentDepth = depth;
    current.push(lexeme);
    if (lexeme.kind === "paren") depth = Math.max(0, depth + (text[lexeme.start] === "(" ? 1 : -1));
  }
  flush();
  return result;
}

function queryTokens(text: string, lexemes: readonly Lexeme[]): Token[] {
  const solid = lexemes.filter((lexeme) => lexeme.kind !== "space");
  const position = new Map(solid.map((lexeme, index) => [lexeme, index]));
  const neighbour = (lexeme: Lexeme, step: 1 | -1) => solid[(position.get(lexeme) ?? -2) + step];
  return lexemes.map((lexeme) => {
    switch (lexeme.kind) {
      case "space":
      case "string":
      case "op":
      case "pipe":
      case "paren":
        return { ...lexeme, kind: lexeme.kind };
      case "number":
        return { ...lexeme, kind: "value" };
      case "ident": {
        if (neighbour(lexeme, -1)?.kind === "op") return { ...lexeme, kind: "value" };
        if (neighbour(lexeme, 1)?.kind === "op") return { ...lexeme, kind: "field" };
        const word = text.slice(lexeme.start, lexeme.end).toLowerCase();
        return { ...lexeme, kind: KEYWORDS.has(word) ? "keyword" : "text" };
      }
      case "other":
        return { ...lexeme, kind: "text" };
    }
  });
}

export function lexQuery(text: string): Token[] {
  return mergeText(queryTokens(text, lex(text)));
}

function mergeText(tokens: readonly Token[]): Token[] {
  const merged: Token[] = [];
  for (const token of tokens) {
    const last = merged[merged.length - 1];
    if (last && last.kind === token.kind && (token.kind === "text" || token.kind === "space")) {
      merged[merged.length - 1] = { ...last, end: token.end };
    } else {
      merged.push(token);
    }
  }
  return merged;
}

function tagTokens(tag: TagToken): Token[] {
  const tokens: Token[] = [{ kind: "tagAt", start: tag.start, end: tag.start + 1 }];
  const teamEnd = tag.slash ?? tag.end;
  if (teamEnd > tag.start + 1) tokens.push({ kind: "tagTeam", start: tag.start + 1, end: teamEnd });
  if (tag.source !== null) {
    tokens.push({ kind: "tagSlash", start: teamEnd, end: teamEnd + 1 });
    if (tag.end > teamEnd + 1) tokens.push({ kind: "tagSource", start: teamEnd + 1, end: tag.end });
  }
  return tokens;
}

function rangeTokens(word: Span, keyLength: number, valid: boolean): Token[] {
  const tokens: Token[] = [{ kind: "rangeKey", start: word.start, end: word.start + keyLength }];
  if (word.end > word.start + keyLength) {
    tokens.push({ kind: valid ? "rangeValue" : "invalid", start: word.start + keyLength, end: word.end });
  }
  return tokens;
}

/** Remove each span plus one whitespace next to it, keeping every other character. */
function withoutSpans(text: string, spans: readonly Span[]): string {
  let result = "";
  let at = 0;
  for (const span of [...spans].sort((a, b) => a.start - b.start)) {
    let { start, end } = span;
    if (/\s/.test(text[end] ?? "")) end += 1;
    else if (start > at && /\s/.test(text[start - 1] ?? "")) start -= 1;
    result += text.slice(at, start);
    at = end;
  }
  return (result + text.slice(at)).trim();
}

function clamp(value: number, max: number): number {
  return Math.min(Math.max(0, Math.trunc(value)), max);
}

function parseTag(text: string, word: Span): TagToken | null {
  const match = TAG.exec(text.slice(word.start, word.end));
  if (!match) return null;
  const id = (value: string | undefined) => value === undefined ? null : Number(value);
  const teamId = id(match[2]);
  const sourceId = id(match[4]);
  if ([teamId, sourceId].some(value => value !== null && (!Number.isSafeInteger(value) || value <= 0))) return null;
  const slash = text.indexOf("/", word.start);
  return { start: word.start, end: word.end, team: match[1], teamId, source: match[3] ?? null, sourceId, slash: slash >= word.start && slash < word.end ? slash : null };
}

function askMarker(text: string, tag: TagToken | null): Span | null {
  const offset = tag?.end ?? 0;
  const rest = text.slice(offset);
  const start = offset + rest.length - rest.trimStart().length;
  return text[start] === "?" ? { start, end: start + 1 } : null;
}

/** Total: every string parses. Problems are issues, never exceptions. */
export function parsePaletteInput(text: string, selection: Span, askEnabled = true): ParsedInput {
  const start = clamp(Math.min(selection.start, selection.end), text.length);
  const caret = clamp(Math.max(selection.start, selection.end), text.length);
  const collapsed = start === caret;

  if (text.length > MAX_INPUT_LENGTH) {
    const first = /^\s*(\S*)/.exec(text.slice(0, 256));
    const leading = first ? first[0].length - first[1].length : 0;
    const tag = first ? parseTag(text, { start: leading, end: leading + first[1].length }) : null;
    return {
      ask: askEnabled ? askMarker(text, tag) : null, text, tokens: [{ kind: "text", start: 0, end: text.length }], tag, range: null, query: text.trim(), slot: null,
      caret, collapsed, issues: [{ kind: "tooLong", limit: MAX_INPUT_LENGTH }],
    };
  }

  const lexemes = lex(text);
  const all = words(text, lexemes);
  const issues: ParseIssue[] = [];
  const tag = all[0] ? parseTag(text, all[0]) : null;
  const ask = askEnabled ? askMarker(text, tag) : null;
  let range: RangeToken | null = null;
  const palette: Token[] = [];
  const removed: Span[] = [];
  let slot: CompletionSlot | null = null;
  const covers = (word: Span) => caret > word.start && caret <= word.end;

  if (tag) {
    palette.push(...tagTokens(tag));
    removed.push(tag);
    if (covers(tag)) {
      const slashAt = tag.slash ?? tag.end;
      const replace = { start: tag.start, end: tag.end };
      slot = tag.source !== null && caret > slashAt
        ? { kind: "source", team: tag.team, prefix: text.slice(slashAt + 1, caret), replace }
        : { kind: "team", prefix: text.slice(tag.start + 1, Math.min(caret, slashAt)), replace };
    }
  }

  if (ask) {
    palette.push({ ...ask, kind: "askMarker" });
    removed.push(ask);
  }

  for (const word of all.slice(tag ? 1 : 0)) {
    const wordText = text.slice(word.start, word.end);
    const rangeMatch = word.depth === 0 ? RANGE.exec(wordText) : null;
    if (rangeMatch) {
      const value = rangeMatch[2];
      const parsedWindow = relativeWindow(value);
      const window = parsedWindow?.kind === "relative" ? parsedWindow : null;
      if (range) {
        issues.push({ kind: "duplicateRange", span: word });
        palette.push({ kind: "invalid", start: word.start, end: word.end });
      } else {
        range = window ? { kind: "valid", start: word.start, end: word.end, text: value, window } : { kind: "invalid", start: word.start, end: word.end, text: value };
        if (!window) issues.push({ kind: "invalidRange", span: word, text: value });
        palette.push(...rangeTokens(word, rangeMatch[1].length, window !== null));
      }
      removed.push(word);
      if (covers(word)) {
        const valueStart = word.start + rangeMatch[1].length;
        slot = { kind: "range", prefix: text.slice(valueStart, Math.max(valueStart, caret)), replace: { start: word.start, end: word.end } };
      }
    } else if (tag && TAG.test(wordText) && wordText.includes("/")) {
      issues.push({ kind: "extraTag", span: word });
      palette.push({ kind: "invalid", start: word.start, end: word.end });
    }
  }

  const tokens = mergeText(
    queryTokens(text, lexemes)
      .flatMap((token) => (palette.some((p) => p.start < token.end && token.start < p.end) ? [] : [token]))
      .map<Token>(token => ask && token.start >= ask.end && token.kind !== "space" ? { ...token, kind: "askText" satisfies TokenKind } : token)
      .concat(palette)
      .sort((a, b) => a.start - b.start),
  );
  return { text, tokens, tag, range, ask, query: withoutSpans(text, removed), slot, caret, collapsed, issues };
}

/** A finished word (a source tag, a range) gets one trailing space so typing continues. */
export function applyCompletion(text: string, replace: Span, insert: string, finished: boolean): InputEdit {
  const after = text.slice(replace.end);
  const spaced = finished && !/^\s/.test(after);
  const next = text.slice(0, replace.start) + insert + (spaced ? " " : "") + after;
  const caret = replace.start + insert.length + (finished ? 1 : 0);
  return { text: next, caret: Math.min(caret, next.length) };
}

/** The slug a team or source name is typed as: lowercase, other runs become "-". */
export function slugify(name: string): string {
  return name.toLowerCase().replace(/[^a-z0-9_.-]+/g, "-").replace(/^-+|-+$/g, "");
}
