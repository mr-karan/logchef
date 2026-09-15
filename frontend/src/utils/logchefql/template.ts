/** Replace template values without adding quotes inside existing string literals. */
export function prepareLogchefQLTemplate(query: string): string {
  const tokens = /"(?:[^"\\]|\\.)*"|'(?:[^'\\]|\\.)*'|\{\{\s*[a-zA-Z_][a-zA-Z0-9_]*\s*\}\}|\[\[|\]\]/g;
  const variable = /\{\{\s*([a-zA-Z_][a-zA-Z0-9_]*)\s*\}\}/g;
  return query.replace(tokens, (token) => {
    if (token === '[[' || token === ']]') return '';
    const quoted = token.startsWith('"') || token.startsWith("'");
    const replaced = token.replace(variable, (_match, name: string) => `__VAR_${name}__`);
    return quoted ? replaced : `"${replaced}"`;
  });
}
