import { isValueSuggestableField } from "./logchefql-autocomplete";

export interface SQLValueCompletionContext {
  field: string;
  fieldType: string;
  partial: string;
  start: number;
  end: number;
}

/** Where the caret sits: in SQL code, inside a literal or comment, or inside a value literal of a known field. */
export type SQLCompletionCursor =
  | { kind: "code" }
  | { kind: "literal" }
  | ({ kind: "value" } & SQLValueCompletionContext);

type SQLToken = { kind: "identifier" | "value" | "symbol"; text: string };

function closingQuote(text: string, start: number): number {
  const quote = text[start];
  for (let i = start + 1; i < text.length; i++) {
    if (text[i] === "\\") i++;
    else if (text[i] === quote) {
      if (text[i + 1] === quote) i++;
      else return i;
    }
  }
  return -1;
}

function valueField(tokens: SQLToken[]): string | null {
  let index = tokens.length - 1;
  const operator = tokens[index]?.text.toUpperCase();
  if (operator !== "=" && operator !== "!=" && operator !== "<>" && operator !== "LIKE") {
    while (tokens[index]?.text === "," && tokens[index - 1]?.kind === "value") index -= 2;
    if (tokens[index]?.text !== "(" || tokens[index - 1]?.text.toUpperCase() !== "IN") return null;
    index--;
  }
  if (tokens[index - 1]?.text.toUpperCase() === "NOT") index--;
  const field = tokens[index - 1];
  return field?.kind === "identifier" ? field.text : null;
}

export function getSQLCompletionCursor(
  text: string,
  offset: number,
  schema: Record<string, { type: string }>,
): SQLCompletionCursor {
  const tokens: SQLToken[] = [];
  let index = 0;
  while (index < offset) {
    const char = text[index];
    if (/\s/.test(char)) {
      index++;
      continue;
    }
    if (text.startsWith("--", index) || text.startsWith("/*", index)) {
      const block = text.startsWith("/*", index);
      const end = block ? text.indexOf("*/", index + 2) : text.indexOf("\n", index + 2);
      if (end < 0 || offset < end + (block ? 2 : 1)) return { kind: "literal" };
      index = end + (block ? 2 : 1);
      continue;
    }
    if (char === "'" || char === '"' || char === "`") {
      let end = closingQuote(text, index);
      if (end < 0 || offset <= end) {
        // A quote glued to more text opens the next literal, unless that text is SQL that can follow a literal.
        if (end >= 0 && !/^(?:$|[\s),;]|::|\|\||--|\/\*)/.test(text.slice(end + 1, end + 3))) end = -1;
        if (char !== "'") return { kind: "literal" };
        const field = valueField(tokens);
        if (field === null || !Object.hasOwn(schema, field)) return { kind: "literal" };
        const column = schema[field];
        if (!isValueSuggestableField(column.type)) return { kind: "literal" };
        return {
          kind: "value", field, fieldType: column.type,
          partial: text.slice(index + 1, offset).replace(/\\(['\\])|''/g, (_match, escaped: string | undefined) => escaped ?? "'"),
          start: index + 1, end: end < 0 ? offset : end + 1,
        };
      }
      const content = text.slice(index + 1, end);
      tokens.push({ kind: char === "'" ? "value" : "identifier", text: content.replaceAll(char + char, char) });
      index = end + 1;
      continue;
    }
    const match = /^(?:[a-z_][a-z0-9_$]*|\d+(?:\.\d+)?|<>|[!<>=]=?|[^\s])/i.exec(text.slice(index));
    if (!match) return { kind: "code" };
    const token = match[0];
    tokens.push({ kind: /^[a-z_]/i.test(token) ? "identifier" : /^\d/.test(token) ? "value" : "symbol", text: token });
    index += token.length;
  }
  return { kind: "code" };
}

export function buildSQLValueInsertText(value: string): string {
  return `${value.replace(/\\/g, "\\\\").replace(/'/g, "''")}'`;
}
