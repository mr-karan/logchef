import * as monaco from "monaco-editor";
import type { ColumnInfo } from "@/api/explore";
import {
  logchefqlToFilterConditions,
  filterConditionsToLogchefql,
} from "./logchefql";

/**
 * Token types for the LogFilter language
 */
export enum TokenType {
  Field = "field",
  Operator = "operator",
  String = "string",
  Number = "number",
  Keyword = "keyword",
  Delimiter = "delimiter",
  Comment = "comment",
}

/**
 * Operators supported by the LogFilter language
 */
export const OPERATORS = {
  EQUALS: "=",
  NOT_EQUALS: "!=",
  GREATER_THAN: ">",
  LESS_THAN: "<",
  GREATER_EQUAL: ">=",
  LESS_EQUAL: "<=",
  CONTAINS: "~",
  NOT_CONTAINS: "!~",
};

/**
 * Mapping between display operators and internal operators
 */
export const OPERATOR_MAPPINGS: Record<string, string> = {
  [OPERATORS.EQUALS]: "=",
  [OPERATORS.NOT_EQUALS]: "!=",
  [OPERATORS.GREATER_THAN]: ">",
  [OPERATORS.LESS_THAN]: "<",
  [OPERATORS.GREATER_EQUAL]: ">=",
  [OPERATORS.LESS_EQUAL]: "<=",
  [OPERATORS.CONTAINS]: "contains",
  [OPERATORS.NOT_CONTAINS]: "not_contains",
};

// Track if language has been registered to avoid duplicate registration
let isLanguageRegistered = false;

/**
 * Register the LogFilter language with Monaco
 */
export function registerLogFilterLanguage() {
  // Prevent duplicate registration which can cause issues
  if (isLanguageRegistered) return;
  isLanguageRegistered = true;

  // Register language
  monaco.languages.register({ id: "logfilter" });

  // Define syntax highlighting
  monaco.languages.setMonarchTokensProvider("logfilter", {
    tokenizer: {
      root: [
        // Comments
        [/#.*$/, TokenType.Comment],

        // Magic @ character - highlight specially to make it stand out
        [/@/, "magic-symbol"],

        // Field names - highlight both standalone and in expressions
        [/[a-zA-Z_][a-zA-Z0-9_]*(?=\s*[=!<>~])/, TokenType.Field],
        [/\b[a-zA-Z_][a-zA-Z0-9_]*\b/, "identifier"],

        // Operators with improved highlighting
        [/(?:!=|=|>=|<=|>|<|~|!~)/, TokenType.Operator],

        // Keywords with case insensitivity
        [/\b(?:AND|OR)\b/i, TokenType.Keyword],

        // Quoted strings with proper escaping support
        [/"(?:\\.|[^"\\])*"/, TokenType.String],
        [/'(?:\\.|[^'\\])*'/, TokenType.String],

        // Numbers with support for scientific notation
        [/\b\d+(\.\d+)?([eE][-+]?\d+)?\b/, TokenType.Number],

        // Parentheses for potential future expression grouping
        [/[()]/, "parenthesis"],

        // Delimiters with special highlighting
        [/;/, TokenType.Delimiter],
      ],
    },
  });

  // Define language configuration
  monaco.languages.setLanguageConfiguration("logfilter", {
    autoClosingPairs: [
      { open: "(", close: ")" },
      { open: '"', close: '"' },
      { open: "'", close: "'" },
    ],
    brackets: [["(", ")"]],
    comments: {
      lineComment: "#",
    },
  });
}

// Keep track of the current completion provider
let currentCompletionProvider: monaco.IDisposable | null = null;

/**
 * Register completion provider for LogFilter language
 */
export function registerCompletionProvider(columns: ColumnInfo[] = []) {
  // Clean up previous provider
  if (currentCompletionProvider) {
    currentCompletionProvider.dispose();
    currentCompletionProvider = null;
  }

  // Create completion provider
  currentCompletionProvider = monaco.languages.registerCompletionItemProvider(
    "logfilter",
    {
      // Trigger characters for showing completions
      // Include all letters to trigger completions as you type
      triggerCharacters: [
        " ",
        ";",
        "=",
        "!",
        ">",
        "<",
        "~",
        "@",
        "a",
        "b",
        "c",
        "d",
        "e",
        "f",
        "g",
        "h",
        "i",
        "j",
        "k",
        "l",
        "m",
        "n",
        "o",
        "p",
        "q",
        "r",
        "s",
        "t",
        "u",
        "v",
        "w",
        "x",
        "y",
        "z",
        "A",
        "B",
        "C",
        "D",
        "E",
        "F",
        "G",
        "H",
        "I",
        "J",
        "K",
        "L",
        "M",
        "N",
        "O",
        "P",
        "Q",
        "R",
        "S",
        "T",
        "U",
        "V",
        "W",
        "X",
        "Y",
        "Z",
      ],

      provideCompletionItems: (model, position) => {
        try {
          // Get current line content
          const lineContent = model.getLineContent(position.lineNumber);
          const beforeCursor = lineContent.substring(0, position.column - 1);
          const lastChar = beforeCursor.charAt(beforeCursor.length - 1);

          // Get current word for better suggestions
          const wordInfo = model.getWordUntilPosition(position);
          const wordRange = {
            startLineNumber: position.lineNumber,
            endLineNumber: position.lineNumber,
            startColumn: wordInfo.startColumn,
            endColumn: wordInfo.endColumn,
          };

          // Current word being typed
          const currentWord = wordInfo.word.toLowerCase();

          console.log(
            `Providing completions at position ${position.lineNumber}:${position.column}, word: "${currentWord}"`
          );

          // Handle @ character trigger - insert field pattern
          if (lastChar === "@") {
            return {
              suggestions: columns.map((column) => ({
                label: column.name,
                kind: monaco.languages.CompletionItemKind.Field,
                insertText: column.name, // No trailing space
                range: {
                  startLineNumber: position.lineNumber,
                  endLineNumber: position.lineNumber,
                  startColumn: position.column - 1,
                  endColumn: position.column,
                },
                documentation: {
                  value: `**Field:** ${column.name}\n\n**Type:** ${column.type}`,
                  isTrusted: true,
                },
                detail: column.type,
                sortText: "0" + column.name, // Prioritize field suggestions
              })),
            };
          }

          // ALWAYS suggest fields when typing an identifier
          // This enables auto-complete as soon as you start typing a field name
          if (/^[a-zA-Z0-9_]*$/.test(currentWord)) {
            // Filter columns that match the current partial word
            const filteredColumns = columns.filter((column) =>
              column.name.toLowerCase().includes(currentWord)
            );

            // Sort columns by how closely they match (exact prefix match first)
            filteredColumns.sort((a, b) => {
              const aStartsWith = a.name.toLowerCase().startsWith(currentWord);
              const bStartsWith = b.name.toLowerCase().startsWith(currentWord);

              if (aStartsWith && !bStartsWith) return -1;
              if (!aStartsWith && bStartsWith) return 1;
              return a.name.localeCompare(b.name);
            });

            // Create suggestions for columns - without trailing space
            const fieldSuggestions = filteredColumns.map((column, index) => ({
              label: column.name,
              kind: monaco.languages.CompletionItemKind.Field,
              insertText: column.name, // No trailing space
              range: wordRange,
              detail: column.type,
              documentation: {
                value: `**Field:** ${column.name}\n\n**Type:** ${column.type}`,
                isTrusted: true,
              },
              // Ensure exact matches are sorted first
              sortText: column.name.toLowerCase().startsWith(currentWord)
                ? "0" + index
                : "1" + index,
            }));

            // Add example suggestions
            const exampleSuggestions = [];
            if (currentWord.length === 0 || "namespace".includes(currentWord)) {
              exampleSuggestions.push({
                label: "namespace='production'",
                kind: monaco.languages.CompletionItemKind.User,
                insertText: "namespace='production'",
                range: wordRange,
                detail: "Filter by namespace",
                documentation: {
                  value: "Filter logs by namespace='production'",
                  isTrusted: true,
                },
                sortText: "9-example-1",
              });
            }

            if (currentWord.length === 0 || "severity".includes(currentWord)) {
              exampleSuggestions.push({
                label: "severity_text='ERROR'",
                kind: monaco.languages.CompletionItemKind.User,
                insertText: "severity_text='ERROR'",
                range: wordRange,
                detail: "Filter by error severity",
                documentation: {
                  value: "Filter logs with ERROR severity",
                  isTrusted: true,
                },
                sortText: "9-example-2",
              });
            }

            // When we're at start of line or after a delimiter, include both examples and fields
            if (
              beforeCursor.trim() === "" ||
              /;\s*$/.test(beforeCursor) ||
              /\b(AND|OR)\s+$/i.test(beforeCursor)
            ) {
              return {
                suggestions: [...fieldSuggestions, ...exampleSuggestions],
              };
            }

            // Otherwise just suggest fields
            return {
              suggestions: fieldSuggestions,
            };
          }

          // After a field name, suggest operators
          if (
            /[a-zA-Z0-9_]\s*$/.test(beforeCursor) &&
            !beforeCursor.includes("=") &&
            !beforeCursor.includes(">") &&
            !beforeCursor.includes("<") &&
            !beforeCursor.includes("~")
          ) {
            return {
              suggestions: [
                {
                  label: "=",
                  kind: monaco.languages.CompletionItemKind.Operator,
                  insertText: "=",
                  range: wordRange,
                  sortText: "1=",
                },
                {
                  label: "!=",
                  kind: monaco.languages.CompletionItemKind.Operator,
                  insertText: "!=",
                  range: wordRange,
                  sortText: "2!=",
                },
                {
                  label: ">",
                  kind: monaco.languages.CompletionItemKind.Operator,
                  insertText: ">",
                  range: wordRange,
                  sortText: "3>",
                },
                {
                  label: "<",
                  kind: monaco.languages.CompletionItemKind.Operator,
                  insertText: "<",
                  range: wordRange,
                  sortText: "4<",
                },
                {
                  label: ">=",
                  kind: monaco.languages.CompletionItemKind.Operator,
                  insertText: ">=",
                  range: wordRange,
                  sortText: "5>=",
                },
                {
                  label: "<=",
                  kind: monaco.languages.CompletionItemKind.Operator,
                  insertText: "<=",
                  range: wordRange,
                  sortText: "6<=",
                },
                {
                  label: "~",
                  kind: monaco.languages.CompletionItemKind.Operator,
                  insertText: "~",
                  range: wordRange,
                  sortText: "7~",
                },
                {
                  label: "!~",
                  kind: monaco.languages.CompletionItemKind.Operator,
                  insertText: "!~",
                  range: wordRange,
                  sortText: "8!~",
                },
              ],
            };
          }

          // After an operator, suggest common values for that field
          if (/[a-zA-Z0-9_]+\s*[=<>!~]+\s*$/.test(beforeCursor)) {
            // Extract the field name
            const fieldMatch = beforeCursor.match(
              /([a-zA-Z0-9_]+)\s*[=<>!~]+\s*$/
            );
            if (fieldMatch && fieldMatch[1]) {
              const fieldName = fieldMatch[1].toLowerCase();

              // Suggest common values based on field name
              const valueSuggestions = [];

              if (
                fieldName === "severity_text" ||
                fieldName.includes("severity")
              ) {
                valueSuggestions.push(
                  {
                    label: "'ERROR'",
                    insertText: "'ERROR'",
                    detail: "Error level",
                  },
                  {
                    label: "'INFO'",
                    insertText: "'INFO'",
                    detail: "Info level",
                  },
                  {
                    label: "'WARN'",
                    insertText: "'WARN'",
                    detail: "Warning level",
                  },
                  {
                    label: "'DEBUG'",
                    insertText: "'DEBUG'",
                    detail: "Debug level",
                  }
                );
              } else if (
                fieldName === "namespace" ||
                fieldName.includes("namespace")
              ) {
                valueSuggestions.push(
                  {
                    label: "'production'",
                    insertText: "'production'",
                    detail: "Production namespace",
                  },
                  {
                    label: "'staging'",
                    insertText: "'staging'",
                    detail: "Staging namespace",
                  },
                  {
                    label: "'development'",
                    insertText: "'development'",
                    detail: "Development namespace",
                  }
                );
              } else if (
                fieldName.includes("status") ||
                fieldName.includes("code")
              ) {
                valueSuggestions.push(
                  { label: "200", insertText: "200", detail: "OK status" },
                  { label: "404", insertText: "404", detail: "Not Found" },
                  { label: "500", insertText: "500", detail: "Server Error" }
                );
              }

              // Add generic string/number suggestions
              if (valueSuggestions.length === 0) {
                valueSuggestions.push(
                  { label: "''", insertText: "''", detail: "Empty string" },
                  {
                    label: "'value'",
                    insertText: "'value'",
                    detail: "String value",
                  }
                );

                // Number suggestions for operators that work with numbers
                if (beforeCursor.includes(">") || beforeCursor.includes("<")) {
                  valueSuggestions.push(
                    { label: "0", insertText: "0", detail: "Zero" },
                    { label: "100", insertText: "100", detail: "Number value" }
                  );
                }
              }

              return {
                suggestions: valueSuggestions.map((suggestion, index) => ({
                  label: suggestion.label,
                  kind: monaco.languages.CompletionItemKind.Value,
                  insertText: suggestion.insertText,
                  range: wordRange,
                  detail: suggestion.detail,
                  sortText: `value-${index}`,
                })),
              };
            }
          }

          // After a value, suggest logical operators or semicolon
          if (
            (beforeCursor.includes("=") ||
              beforeCursor.includes(">") ||
              beforeCursor.includes("<") ||
              beforeCursor.includes("~")) &&
            (beforeCursor.includes("'") ||
              beforeCursor.includes('"') ||
              /\d+\s*$/.test(beforeCursor))
          ) {
            return {
              suggestions: [
                {
                  label: "AND",
                  kind: monaco.languages.CompletionItemKind.Keyword,
                  insertText: " AND",
                  range: wordRange,
                  detail: "Combine with another condition (all must match)",
                  sortText: "keyword-1",
                },
                {
                  label: "OR",
                  kind: monaco.languages.CompletionItemKind.Keyword,
                  insertText: " OR",
                  range: wordRange,
                  detail: "Combine with another condition (any can match)",
                  sortText: "keyword-2",
                },
                {
                  label: ";",
                  kind: monaco.languages.CompletionItemKind.Operator,
                  insertText: ";",
                  range: wordRange,
                  detail: "Start a new expression",
                  sortText: "operator-1",
                },
              ],
            };
          }

          // Default - just suggest fields
          if (currentWord.length > 0) {
            const filteredColumns = columns.filter((column) =>
              column.name.toLowerCase().includes(currentWord)
            );

            if (filteredColumns.length > 0) {
              return {
                suggestions: filteredColumns.map((column) => ({
                  label: column.name,
                  kind: monaco.languages.CompletionItemKind.Field,
                  insertText: column.name,
                  range: wordRange,
                  detail: column.type,
                })),
              };
            }
          }

          return { suggestions: [] };
        } catch (error) {
          console.error("Error providing completions:", error);
          return { suggestions: [] };
        }
      },
    }
  );

  return currentCompletionProvider;
}

/**
 * Show field suggestions
 */
export function showFieldSuggestions(
  editor: monaco.editor.IStandaloneCodeEditor
) {
  if (!editor) return;

  try {
    editor.focus();
    editor.trigger("keyboard", "editor.action.triggerSuggest", {});
  } catch (error) {
    console.error("Error showing field suggestions:", error);
  }
}

/**
 * Parse a filter expression to structured filter conditions
 *
 * This uses the LogChefQL parser to support complex expressions:
 * - Simple: field operator value
 * - Logical operators: field1 op1 value1 AND/OR field2 op2 value2
 *   (case insensitive, both "AND" and "and" work)
 * - Grouping: (field1 op1 value1 OR field2 op2 value2) AND field3 op3 value3
 * - Legacy support: field1 op1 value1; field2 op2 value2 (semicolon separated)
 */
export function parseFilterExpression(expression: string): any[] {
  if (!expression || !expression.trim()) {
    return [];
  }

  try {
    // First try to extract any complete conditions using the LogChefQL parser
    const logchefqlConditions = logchefqlToFilterConditions(expression);

    // If conditions were successfully extracted, use them
    if (logchefqlConditions && logchefqlConditions.length > 0) {
      return logchefqlConditions;
    }

    // If we didn't get any conditions, run the legacy parser as a fallback
    // This is mainly for handling incomplete expressions during typing
    const legacyConditions = legacyParseFilterExpression(expression);

    return legacyConditions;
  } catch (error) {
    console.error("Error parsing filter expression:", error);

    // Even if an error occurs, try the legacy parser
    return legacyParseFilterExpression(expression);
  }
}

/**
 * Legacy parser for backward compatibility and for handling incomplete expressions
 * during typing that would not yet be valid LogChefQL
 */
function legacyParseFilterExpression(expression: string): any[] {
  try {
    // Conditions array to store results
    const result: any[] = [];

    // Split by semicolons, handling quoted strings properly
    const conditions: string[] = splitBySemicolons(expression);

    // Process each condition - look for both semicolon and AND/OR separated values
    for (const condition of conditions) {
      const trimmedCondition = condition.trim();
      if (!trimmedCondition) continue;

      // Extract any partial field=value pairs that might be incomplete
      const fieldValuePairs = extractPartialFieldValuePairs(trimmedCondition);

      for (const pair of fieldValuePairs) {
        // Skip incomplete conditions that are still being typed
        // This prevents premature filtering while typing
        if (isIncompleteCondition(pair.trim())) {
          continue;
        }

        // Parse simple condition
        const parsedCond = parseSimpleCondition(pair.trim());
        if (parsedCond) result.push(parsedCond);
      }
    }

    return result;
  } catch (error) {
    console.error("Error in legacy parse:", error);
    return [];
  }
}

/**
 * Extract potential field=value pairs from an expression that might contain AND/OR
 */
function extractPartialFieldValuePairs(expression: string): string[] {
  const results: string[] = [];

  // First look for AND/OR separated values
  const andOrRegex = /\s+(AND|OR)\s+/i;
  if (andOrRegex.test(expression)) {
    // Split by AND/OR, handling quoted strings and parentheses
    let inQuote = false;
    let inParens = 0;
    let current = "";

    for (let i = 0; i < expression.length; i++) {
      const char = expression[i];

      // Handle quotes
      if (
        (char === '"' || char === "'") &&
        (i === 0 || expression[i - 1] !== "\\")
      ) {
        inQuote = !inQuote;
      }

      // Handle parentheses
      if (!inQuote) {
        if (char === "(") inParens++;
        else if (char === ")") inParens--;
      }

      // Check for operator boundary
      if (!inQuote && inParens === 0) {
        // Look for AND/OR pattern
        const restOfString = expression.substring(i);
        const match = restOfString.match(/^\s+(AND|OR)\s+/i);

        if (match) {
          // Found an operator, add the current part and move past the operator
          if (current.trim()) {
            results.push(current.trim());
          }
          current = "";
          i += match[0].length - 1; // -1 because the loop will increment i
          continue;
        }
      }

      current += char;
    }

    // Add the last part
    if (current.trim()) {
      results.push(current.trim());
    }
  } else {
    // No AND/OR, just add the whole expression
    results.push(expression);
  }

  return results;
}

/**
 * Check if a condition is incomplete (still being typed)
 */
function isIncompleteCondition(condition: string): boolean {
  // Consider a condition incomplete if it's just a field name
  // or a field name and operator without a value
  return (
    /^[a-zA-Z0-9_]+$/.test(condition) || // Just a field name
    /^[a-zA-Z0-9_]+\s*[=<>!~]+$/.test(condition) || // Field and operator
    /^[a-zA-Z0-9_]+\s*[=<>!~]+\s*$/.test(condition) || // Field, operator and space
    /^[a-zA-Z0-9_]+\s*[=<>!~]+\s*['"]$/.test(condition) || // Field, operator and opening quote
    /^[a-zA-Z0-9_]+\s*[=<>!~]+\s*['"][^'"]*$/.test(condition)
  ); // Partial quoted string without closing quote
}

/**
 * Split expression by semicolons, respecting quotes
 */
function splitBySemicolons(expression: string): string[] {
  const result: string[] = [];
  let current = "";
  let inSingleQuote = false;
  let inDoubleQuote = false;

  for (let i = 0; i < expression.length; i++) {
    const char = expression[i];

    // Handle quotes
    if (char === "'" && (i === 0 || expression[i - 1] !== "\\")) {
      inSingleQuote = !inSingleQuote;
    } else if (char === '"' && (i === 0 || expression[i - 1] !== "\\")) {
      inDoubleQuote = !inDoubleQuote;
    }

    // Handle semicolons - only split if not in quotes
    if (char === ";" && !inSingleQuote && !inDoubleQuote) {
      if (current.trim()) {
        result.push(current.trim());
      }
      current = "";
    } else {
      current += char;
    }
  }

  // Add the last condition
  if (current.trim()) {
    result.push(current.trim());
  }

  return result;
}

/**
 * Parse a simple condition like "field operator value"
 */
function parseSimpleCondition(condition: string): any | null {
  // Skip empty conditions
  if (!condition) return null;

  // Raw text search for simple text without operators
  if (!condition.match(/[=<>!~]/)) {
    return {
      field: "_raw",
      operator: "contains",
      value: condition,
    };
  }

  // Match field, operator, and value
  const operatorMatch = condition.match(
    /^([a-zA-Z0-9_]+)\s*(=|!=|>=|<=|>|<|~|!~)\s*(.+)$/
  );

  if (operatorMatch) {
    const [, field, op, rawValue] = operatorMatch;

    // Parse the value - handle quoted strings and numbers
    let value = rawValue.trim();

    // Handle completely quoted strings
    if (
      (value.startsWith('"') && value.endsWith('"') && value.length > 1) ||
      (value.startsWith("'") && value.endsWith("'") && value.length > 1)
    ) {
      // Remove quotes
      value = value.substring(1, value.length - 1);
    }
    // Handle incomplete quoted strings - don't convert to condition yet
    else if (
      (value.startsWith('"') && !value.endsWith('"')) ||
      (value.startsWith("'") && !value.endsWith("'"))
    ) {
      // This is an incomplete string being typed
      return null;
    }
    // Handle numeric values
    else if (!isNaN(Number(value))) {
      // Convert to number
      value = Number(value);
    }

    // Special case: Empty string in quotes
    if (value === "") {
      // This is likely while typing, don't convert to condition yet
      if (rawValue === '""' || rawValue === "''") {
        // Only when there are explicitly empty quotes
        // Map display operator to internal operator
        let operator = op;
        if (op === "~") operator = "contains";
        if (op === "!~") operator = "not_contains";

        return {
          field: field.trim(),
          operator,
          value: "",
        };
      }
      return null;
    }

    // Map display operator to internal operator
    let operator = op;
    if (op === "~") operator = "contains";
    if (op === "!~") operator = "not_contains";

    return {
      field: field.trim(),
      operator,
      value,
    };
  }

  return null;
}

/**
 * Convert filter conditions to an expression string
 */
export function filterConditionsToExpression(conditions: any[]): string {
  if (!conditions || !conditions.length) {
    return "";
  }

  // Handle raw text search
  if (
    conditions.length === 1 &&
    conditions[0]?.field === "_raw" &&
    conditions[0]?.operator === "contains"
  ) {
    return conditions[0].value || "";
  }

  try {
    // Use the new LogChefQL format to convert conditions to an expression
    return formatConditionsAsLogChefQL(conditions);
  } catch (error) {
    console.error("Error converting conditions to expression:", error);

    // Fallback to legacy formatting
    return legacyFilterConditionsToExpression(conditions);
  }
}

/**
 * Format filter conditions as a LogChefQL expression string
 * Uses the modern AND/OR syntax instead of semicolons
 */
function formatConditionsAsLogChefQL(conditions: any[]): string {
  if (!conditions || !conditions.length) {
    return "";
  }

  // Format each condition
  return conditions
    .map((condition) => {
      if (!condition) return "";

      const field = condition.field;
      const op = condition.operator || condition.op;
      const value = condition.value;

      if (!field || !op) return "";

      // Format the value based on type
      const formatValue = (val: any) => {
        if (val === null || val === undefined) return "NULL";
        if (typeof val === "number") return val.toString();
        // Use single quotes for better readability
        return `'${val.toString().replace(/'/g, "\\'")}'`;
      };

      switch (op) {
        case "=":
        case "!=":
        case ">":
        case "<":
        case ">=":
        case "<=":
          return `${field} ${op} ${formatValue(value)}`;
        case "contains":
        case "~":
          return `${field} ~ ${formatValue(value)}`;
        case "not_contains":
        case "!~":
          return `${field} !~ ${formatValue(value)}`;
        default:
          return "";
      }
    })
    .filter(Boolean)
    .join(" AND "); // Join with AND operator instead of semicolons
}

/**
 * Legacy converter for backward compatibility
 */
function legacyFilterConditionsToExpression(conditions: any[]): string {
  // Format each condition
  return conditions
    .map((condition) => {
      if (!condition) return "";

      const field = condition.field;
      const op = condition.operator || condition.op;
      const value = condition.value;

      if (!field || !op) return "";

      // Format the value based on type
      const formatValue = (val: any) => {
        if (val === null || val === undefined) return "NULL";
        if (typeof val === "number") return val.toString();
        return `"${val.toString().replace(/"/g, '\\"')}"`; // Escape quotes
      };

      switch (op) {
        case "=":
        case "!=":
        case ">":
        case "<":
        case ">=":
        case "<=":
          return `${field} ${op} ${formatValue(value)}`;
        case "contains":
        case "~":
          return `${field} ~ ${formatValue(value)}`;
        case "not_contains":
        case "!~":
          return `${field} !~ ${formatValue(value)}`;
        default:
          return "";
      }
    })
    .filter(Boolean)
    .join(" AND ");
}
