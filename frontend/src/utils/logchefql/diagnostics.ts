import type { ParseError } from '@/api/logchefql';

export interface FieldReference {
  name: string;
  baseName: string;
  start: number;
  end: number;
  complete: boolean;
}

/** The schema type of a field or its base name; inherited names such as "constructor" are not fields. */
export function getSchemaFieldType(schema: Record<string, { type: string }>, field: Pick<FieldReference, 'name' | 'baseName'>): string | undefined {
  const name = Object.hasOwn(schema, field.name) ? field.name : field.baseName;
  return Object.hasOwn(schema, name) ? schema[name].type : undefined;
}

export function getUnknownLogchefQLFields(query: string, schema: Record<string, { type: string }>): FieldReference[] {
  if (Object.keys(schema).length === 0) return [];
  return getLogchefQLFields(query).filter(field => field.complete && getSchemaFieldType(schema, field) === undefined);
}

export function getLogchefQLFields(query: string): FieldReference[] {
  const tokens = Array.from(query.matchAll(/\{\{\s*[a-zA-Z_][a-zA-Z0-9_]*\s*\}\}|\[\[|\]\]|"(?:[^"\\]|\\.)*(?:"|$)|'(?:[^'\\]|\\.)*(?:'|$)|@?[a-zA-Z_][a-zA-Z0-9_:@-]*|[-+]?[0-9]*\.?[0-9]+|!=|!~|>=|<=|[^\s]/g));
  const fields: FieldReference[] = [];
  let state: 'field' | 'operator' | 'value' | 'boolean' | 'projection' = 'field';

  for (let i = 0; i < tokens.length; i++) {
    const token = tokens[i];
    if (!token) continue;
    const text = token[0];
    if (text === '[[' || text === ']]') continue;
    if (text === '|') {
      state = 'projection';
      continue;
    }
    if (text === '(' || text === ')') continue;
    if (state === 'boolean' && /^(and|or)$/i.test(text)) {
      state = 'field';
      continue;
    }
    if (state === 'value') {
      state = 'boolean';
      continue;
    }
    if (state === 'operator') {
      if (/^(?:!=|!~|>=|<=|[=~><])$/.test(text)) state = 'value';
      continue;
    }
    if (state !== 'field' && state !== 'projection') continue;
    const segmentName = (segment: string) => /^['"]/.test(segment)
      ? segment.slice(1, -1).replace(/\\(.)/g, '$1')
      : segment;
    if (!/^@?[a-zA-Z_]|^['"]/.test(text)) continue;
    const baseName = segmentName(text);
    const segments = [baseName];
    let end = token.index + text.length;
    while (tokens[i + 1]?.[0] === '.') {
      const segment = tokens[i + 2];
      if (!segment || !/^@?[a-zA-Z_]|^['"]/.test(segment[0])) break;
      segments.push(segmentName(segment[0]));
      end = segment.index + segment[0].length;
      i += 2;
    }
    fields.push({
      name: segments.join('.'), baseName, start: token.index, end,
      complete: state === 'projection' || /^(?:!=|!~|>=|<=|[=~><])$/.test(tokens[i + 1]?.[0] ?? ''),
    });
    if (state === 'field') state = 'operator';
  }
  return fields;
}

export function getLogchefQLErrorRange(query: string, prepared: string, position?: ParseError['position']): { start: number; end: number } {
  const lines = query.split('\n');
  const lineIndex = Math.min(Math.max((position?.line ?? 1) - 1, 0), lines.length - 1);
  const line = lines[lineIndex] ?? '';
  const lineStart = lines.slice(0, lineIndex).reduce((offset, value) => offset + value.length + 1, 0);
  // Template substitution changes columns, so use the original line instead of an incorrect token.
  if (!position || line !== prepared.split('\n')[lineIndex]) {
    return { start: lineStart, end: lineStart + line.length };
  }
  const column = Math.max(position.column - 1, 0);
  const offset = Array.from(line).slice(0, column).join('').length;
  const start = Math.min(offset, Math.max(line.length - 1, 0));
  const token = line.slice(start).match(/^[\w@:-]+/)?.[0] ?? line[start] ?? '';
  return { start: lineStart + start, end: lineStart + start + token.length };
}

export function escapeHoverText(value: string): string {
  return value.replace(/[\\`*_{}\[\]()<>#+.!|~-]/g, '\\$&');
}
