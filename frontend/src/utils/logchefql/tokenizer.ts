import type { Token, ParseError } from './types';

const OPERATOR_CHARS = new Set(['=', '!', '~', '>', '<']);
const WHITESPACE = /\s/;
const KEY_CHARS = /[a-zA-Z0-9_.:-]/;
const DOT_CHAR = '.';

// Helper to determine if a quote could be part of a key based on context
function couldBeKey(position: number, tokens: Token[], current: any): boolean {
  // If we have no tokens yet, or the last token was a boolean operator, paren, or pipe,
  // then a quote could be the start of a key
  if (tokens.length === 0) return true;

  const lastToken = tokens[tokens.length - 1];
  if (lastToken.type === 'bool' || lastToken.type === 'paren' || lastToken.type === 'pipe') {
    return true;
  }

  // If there's no current token and last token is not an operator, could be a key
  if (!current && lastToken.type !== 'operator') {
    return true;
  }

  return false;
}

// Helper function to parse nested field with quoted segments
function parseNestedField(input: string, startPos: number): {
  fieldValue: string;
  endPos: number;
  hasQuotedSegments: boolean;
} {
  let pos = startPos;
  let fieldValue = '';
  let hasQuotedSegments = false;
  let inQuotedSegment = false;
  let quoteChar = '';

  while (pos < input.length) {
    const char = input[pos];

    if (inQuotedSegment) {
      fieldValue += char;
      if (char === quoteChar) {
        // End of quoted segment
        inQuotedSegment = false;
        hasQuotedSegments = true;
        quoteChar = '';
      }
      pos++;
    } else {
      if (char === '"' || char === "'") {
        // Start of quoted segment
        inQuotedSegment = true;
        quoteChar = char;
        fieldValue += char;
        pos++;
      } else if (KEY_CHARS.test(char) || char === '.') {
        // Continue building field name (include dots for nested fields)
        fieldValue += char;
        pos++;
      } else if (WHITESPACE.test(char) || OPERATOR_CHARS.has(char) || char === '(' || char === ')' || char === '|') {
        // End of field - hit something that's definitely not part of a key
        break;
      } else {
        // For other characters, include them as part of the field (more permissive)
        fieldValue += char;
        pos++;
      }
    }
  }

  return {
    fieldValue,
    endPos: pos,
    hasQuotedSegments
  };
}

export function tokenize(input: string): { tokens: Token[]; errors: ParseError[] } {
  const tokens: Token[] = [];
  const errors: ParseError[] = [];
  let current: { type: Token['type']; value: string; line: number; column: number; quoted?: boolean } | null = null;
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
        // Only include quoted property if it was explicitly quoted
        ...(current.quoted === true ? { quoted: true } : {}),
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

    // Handle boolean operators FIRST (match whole words only)
    if (/[a-zA-Z]/.test(char)) {
      const word = input.slice(i).match(/^[a-zA-Z]+/)?.[0] ?? '';
      const lower = word.toLowerCase();

      // More robust word boundary detection for boolean operators
      const prevChar = i > 0 ? input[i - 1] : '';
      const nextChar = i + word.length < input.length ? input[i + word.length] : '';

      // Previous character must be whitespace, operator, paren, pipe, or start of string
      const validPrev = !prevChar || /[\s=!~><()|\-+*/]/.test(prevChar);
      // Next character must be whitespace, operator, paren, pipe, or end of string
      const validNext = !nextChar || /[\s=!~><()|\-+*/]/.test(nextChar);

      const isValidBooleanContext = validPrev && validNext;

      if ((lower === 'and' || lower === 'or') && isValidBooleanContext) {
        pushCurrent();
        tokens.push({
          type: 'bool',
          value: lower,
          position: { line, column },
        });
        i += word.length - 1; // Skip the rest of the word
        column += word.length;
        continue;
      }

      // Not a boolean operator - continue to key/value handling
    }

     // Handle key characters and nested field access SECOND (before string literals)
     // But only treat quotes as part of keys if we're at the start of tokens or after certain tokens
     if (KEY_CHARS.test(char) || ((char === '"' || char === "'") && couldBeKey(i, tokens, current))) {
       // Check if we're starting a new key (not continuing a current token)
       if (!current || (current.type !== 'key' && current.type !== 'value')) {
         pushCurrent();

         // Try to parse as nested field - start from the current position
         const nestedResult = parseNestedField(input, i);

         // If we found a nested field or quoted segments, consume it all as a key
         if (nestedResult.fieldValue.includes('.') || nestedResult.hasQuotedSegments || nestedResult.fieldValue !== char) {
           current = {
             type: 'key',
             value: nestedResult.fieldValue,
             line,
             column,
             quoted: nestedResult.hasQuotedSegments
           };

           // Update position based on how much we consumed
           const consumed = nestedResult.endPos - i;
           i = nestedResult.endPos - 1; // -1 because the loop will increment
           column += consumed;
           continue;
         }

         // Fall back to simple key handling
         current = { type: 'key', value: char, line, column };
       } else if (current.type === 'key' || current.type === 'value') {
         current.value += char;
       }

       column++;
       continue;
     }

    // Start string literals (only if not already handled as part of a key)
if ((char === '"' || char === "'") && !inString) {
    pushCurrent();
    inString = true;
    stringDelimiter = char;
    current = { type: 'value', value: '', line, column, quoted: true };
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

    // Handle pipe operator
    if (char === '|') {
      pushCurrent();
      tokens.push({
        type: 'pipe',
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
      if ((char === '>' || char === '<' || char === '!' || char === '=') && peek === '=') {
        current!.value += '=';
        i++; // Skip the next character since we consumed it
        column += 2;
        continue;
      }

      column++;
      continue;
    }

    // Handle alphabetic characters as part of keys/values (if not already handled as boolean operators)
    if (/[a-zA-Z]/.test(char)) {
      // If we get here, it means we already checked for boolean operators above
      // and this is not a boolean operator, so treat as key or value
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

  // Check for unterminated string literals
  if (inString) {
    // Provide better error context with the string delimiter used
    const currentValue = current?.value || '';
    const preview = currentValue.length > 20 ? currentValue.substring(0, 20) + '...' : currentValue;

    errors.push({
      code: 'UNTERMINATED_STRING',
      message: `Unterminated string literal starting with ${stringDelimiter}${preview}${stringDelimiter}. Missing closing quote.`,
      position: {
        line: current?.line || line,
        column: current?.column || column
      }
    });

    // Recovery: Push the current string token anyway to allow partial parsing
    if (current) {
      tokens.push({
        type: current.type,
        value: current.value,
        position: { line: current.line, column: current.column },
        quoted: true,
        // Mark this as potentially incomplete for debugging
        incomplete: true
      });
    }
  }

  return { tokens, errors };
}