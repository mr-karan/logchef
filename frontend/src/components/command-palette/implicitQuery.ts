import { lex, MAX_INPUT_LENGTH } from './parse';

export type ImplicitQuery =
  | { readonly kind: 'query' }
  | { readonly kind: 'text'; readonly reason: 'empty' | 'limit' | 'syntax' | 'unknownField' };

const MAX_NESTING = 100;
const CLOSED_STRING = /^(?:"(?:[^"\\]|\\[^\n])*"|'(?:[^'\\]|\\[^\n])*')$/;
const IDENTIFIER = /^@?[a-zA-Z_][a-zA-Z0-9_:@-]*$/;

export function recognizeLogchefQL(text: string, fields: ReadonlySet<string>): ImplicitQuery {
  if (text.length > MAX_INPUT_LENGTH) return { kind: 'text', reason: 'limit' };
  if (!text.trim()) return { kind: 'text', reason: 'empty' };
  const tokens = lex(text).filter(token => token.kind !== 'space');
  const value = (at: number) => text.slice(tokens[at]?.start ?? 0, tokens[at]?.end ?? 0);
  let at = 0;
  let depth = 0;
  let expectingComparison = true;
  let comparisons = 0;

  function segment(): string | null {
    const token = tokens[at];
    const raw = value(at);
    if (token?.kind === 'ident') { at++; return raw; }
    if (token?.kind === 'string' && CLOSED_STRING.test(raw)) {
      at++;
      return raw.slice(1, -1).replace(/\\(.)/g, '$1');
    }
    return null;
  }

  while (at < tokens.length) {
    const raw = value(at);
    if (expectingComparison) {
      if (raw === '(') {
        if (++depth > MAX_NESTING) return { kind: 'text', reason: 'limit' };
        at++;
        continue;
      }
      const firstToken = tokens[at];
      const first = segment();
      if (first === null) return { kind: 'text', reason: 'syntax' };
      const path = [first];
      while (value(at) === '.') {
        at++;
        const next = segment();
        if (next === null) return { kind: 'text', reason: 'syntax' };
        path.push(next);
      }
      if (tokens[at]?.kind !== 'op') return { kind: 'text', reason: 'syntax' };
      const name = path.join('.');
      const baseName = firstToken.kind === 'ident' ? first.split('.')[0] : first;
      if (!fields.has(name) && !fields.has(baseName)) return { kind: 'text', reason: 'unknownField' };
      at++;
      const token = tokens[at];
      const literal = value(at);
      if (token?.kind !== 'number' && !(token?.kind === 'ident' && IDENTIFIER.test(literal)) && !(token?.kind === 'string' && CLOSED_STRING.test(literal))) return { kind: 'text', reason: 'syntax' };
      at++;
      comparisons++;
      expectingComparison = false;
    } else if (raw === ')') {
      if (depth === 0) return { kind: 'text', reason: 'syntax' };
      depth--;
      at++;
    } else if (/^(and|or)$/i.test(raw)) {
      expectingComparison = true;
      at++;
    } else return { kind: 'text', reason: 'syntax' };
  }
  return comparisons > 0 && !expectingComparison && depth === 0 ? { kind: 'query' } : { kind: 'text', reason: 'syntax' };
}
