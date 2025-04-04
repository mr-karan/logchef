import type { Token, ParseError } from './types';

const OPERATOR_CHARS = new Set(['=', '!', '~', '>', '<']);
const WHITESPACE = /\s/;
const KEY_CHARS = /[a-zA-Z0-9_./:-]/;

export function tokenize(input: string): { tokens: Token[]; errors: ParseError[] } {
  const tokens: Token[] = [];
  const errors: ParseError[] = [];
  let current: { type: Token['type']; value: string; line: number; column: number } | null = null;
  let line = 1;
  let column = 1;
  let inEscape = false;
  let inString = false;
  let stringDelimiter = '';

  const pushCurrent = () => {
    if (current) {
      tokens.push({
        type: current.type,
        value: current.value,
        position: { line: current.line, column: current.column },
      });
      current = null;
    }
  };

  for (let i = 0; i < input.length; i++) {
    const char = input[i];

    // Handle line breaks for accurate position tracking
    if (char === '\n') {
      line++;
      column = 1;

      // End any token that was in progress
      if (!inString) {
        pushCurrent();
      } else if (inString) {
        // Add newline to string token
        current!.value += char;
      }
      continue;
    }

    // Handle string literals with proper escaping
    if (inString) {
      if (inEscape) {
        // Handle escaped characters
        current!.value += char;
        inEscape = false;
      } else if (char === '\\') {
        inEscape = true;
      } else if (char === stringDelimiter) {
        // End of string
        pushCurrent();
        inString = false;
        stringDelimiter = '';
      } else {
        current!.value += char;
      }
      column++;
      continue;
    }

    // Start string literals
    if ((char === '"' || char === "'") && !inString) {
      pushCurrent();
      inString = true;
      stringDelimiter = char;
      current = { type: 'value', value: '', line, column };
      column++;
      continue;
    }

    // Handle whitespace (outside strings)
    if (WHITESPACE.test(char)) {
      pushCurrent();
      column++;
      continue;
    }

    // Handle parentheses
    if (char === '(' || char === ')') {
      pushCurrent();
      tokens.push({
        type: 'paren',
        value: char,
        position: { line, column },
      });
      column++;
      continue;
    }

    // Handle operator characters
    if (OPERATOR_CHARS.has(char)) {
      // Special handling for multi-character operators
      const peek = input[i + 1];

      // Check if we're already building an operator
      if (current?.type === 'operator') {
        // Extend the current operator (e.g., !=, >=, <=)
        current.value += char;
      } else {
        // Start a new operator
        pushCurrent();
        current = { type: 'operator', value: char, line, column };
      }

      // Look ahead for compound operators
      if ((char === '>' || char === '<' || char === '!') && peek === '=') {
        current!.value += '=';
        i++; // Skip the next character since we consumed it
        column += 2;
        continue;
      }

      column++;
      continue;
    }

    // Handle boolean operators
    if (/[aAnNdDoOrR]/.test(char)) {
      const lowerChar = char.toLowerCase();
      const word = input.substring(i, i + 3).toLowerCase();

      if (word === 'and' || word === 'or ') {
        pushCurrent();
        tokens.push({
          type: 'bool',
          value: word.trim(),
          position: { line, column },
        });
        i += 2; // Skip the rest of the word
        column += 3;
        continue;
      }

      // Not a boolean operator, treat as key or value
      if (!current) {
        current = {
          type: KEY_CHARS.test(char) ? 'key' : 'value',
          value: char,
          line,
          column
        };
      } else {
        current.value += char;
      }
      column++;
      continue;
    }

    // Handle key characters
    if (KEY_CHARS.test(char)) {
      if (!current) {
        current = { type: 'key', value: char, line, column };
      } else if (current.type === 'key' || current.type === 'value') {
        current.value += char;
      } else {
        pushCurrent();
        current = { type: 'key', value: char, line, column };
      }
      column++;
      continue;
    }

    // Handle other characters as part of values
    if (!current) {
      current = { type: 'value', value: char, line, column };
    } else {
      current.value += char;
    }
    column++;
  }

  // Push any remaining token
  pushCurrent();

  return { tokens, errors };
}