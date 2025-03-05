import * as monaco from "monaco-editor";
import type { ColumnInfo } from "@/api/explore";
import { OPERATOR_MAPPINGS } from "./logchefql/ast";

/**
 * Token types for the LogChefQL language
 */
export enum TokenType {
  Field = "field",
  Operator = "operator",
  String = "string",
  Number = "number",
  Keyword = "keyword",
  Delimiter = "delimiter",
  Comment = "comment",
  Parenthesis = "parenthesis",
  Identifier = "identifier",
}

// Track if language has been registered to avoid duplicate registration
let isLanguageRegistered = false;

/**
 * Register the LogChefQL language with Monaco
 */
export function registerLogChefQLLanguage() {
  // Prevent duplicate registration which can cause issues
  if (isLanguageRegistered) return;
  isLanguageRegistered = true;

  // Register language
  monaco.languages.register({ id: "logchefql" });

  // Define syntax highlighting
  monaco.languages.setMonarchTokensProvider("logchefql", {
    tokenizer: {
      root: [
        // Comments
        [/#.*$/, TokenType.Comment],

        // Keywords (AND, OR)
        [/\b(?:AND|OR)\b/i, TokenType.Keyword],

        // Parentheses
        [/[()]/, TokenType.Parenthesis],

        // Field names - highlight both standalone and in expressions
        [/[a-zA-Z_][a-zA-Z0-9_]*(?=\s*[=!<>~])/, TokenType.Field],
        [/\b[a-zA-Z_][a-zA-Z0-9_]*\b/, TokenType.Identifier],

        // Operators
        [/(?:!=|=|>=|<=|>|<|~|!~)/, TokenType.Operator],

        // Quoted strings with proper escaping support
        [/"(?:\\.|[^"\\])*"/, TokenType.String],
        [/'(?:\\.|[^'\\])*'/, TokenType.String],

        // Numbers with support for scientific notation
        [/\b\d+(\.\d+)?([eE][-+]?\d+)?\b/, TokenType.Number],
      ],
    },
  });

  // Define language configuration
  monaco.languages.setLanguageConfiguration("logchefql", {
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
 * Register completion provider for LogChefQL language
 */
export function registerCompletionProvider(columns: ColumnInfo[] = []) {
  // Clean up previous provider
  if (currentCompletionProvider) {
    currentCompletionProvider.dispose();
    currentCompletionProvider = null;
  }

  // Create completion provider
  currentCompletionProvider = monaco.languages.registerCompletionItemProvider(
    "logchefql",
    {
      // Trigger characters for showing completions
      // Include all letters to trigger completions as you type
      triggerCharacters: [
        " ",
        "(",
        ")",
        "=",
        "!",
        ">",
        "<",
        "~",
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

          // Always suggest fields when typing an identifier
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

            // Create suggestions for columns
            const fieldSuggestions = filteredColumns.map((column, index) => ({
              label: column.name,
              kind: monaco.languages.CompletionItemKind.Field,
              insertText: column.name,
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

            // Add example suggestions with LogChefQL syntax
            const exampleSuggestions = [];

            // Basic field equality examples
            if (currentWord.length === 0 || "namespace".includes(currentWord)) {
              exampleSuggestions.push({
                label: "namespace='production'",
                kind: monaco.languages.CompletionItemKind.User,
                insertText: "namespace='production'",
                range: wordRange,
                detail: "Filter by namespace",
                documentation: {
                  value:
                    "**Filter Example**\n\nFilters logs to show only those with namespace set to 'production'.\n\nThis is a simple field equality filter using the `=` operator.",
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
                  value:
                    "**Error Logs Filter**\n\nShows only logs with severity level 'ERROR'.\n\nUse this to quickly find error messages in your logs.",
                  isTrusted: true,
                },
                sortText: "9-example-2",
              });
            }

            // Example with AND operator
            if (currentWord.length === 0 || "service".includes(currentWord)) {
              exampleSuggestions.push({
                label: "service='api' AND status_code>=400",
                kind: monaco.languages.CompletionItemKind.User,
                insertText: "service='api' AND status_code>=400",
                range: wordRange,
                detail: "API errors filter",
                documentation: {
                  value:
                    "**API Errors Filter**\n\nThis example uses the `AND` operator to combine two conditions:\n\n1. The service must be 'api'\n2. The status code must be 400 or higher\n\nBoth conditions must be true for a log to be included in results.",
                  isTrusted: true,
                },
                sortText: "9-example-3",
              });
            }

            // Example with OR operator
            if (currentWord.length === 0 || "method".includes(currentWord)) {
              exampleSuggestions.push({
                label: "method='GET' OR method='POST'",
                kind: monaco.languages.CompletionItemKind.User,
                insertText: "method='GET' OR method='POST'",
                range: wordRange,
                detail: "HTTP methods filter",
                documentation: {
                  value:
                    "**HTTP Methods Filter**\n\nThis example uses the `OR` operator to match logs with either:\n\n- HTTP method 'GET' OR\n- HTTP method 'POST'\n\nThe log will be included in results if EITHER condition is true.",
                  isTrusted: true,
                },
                sortText: "9-example-4",
              });
            }

            // Example for complex expression with parentheses
            if (currentWord.length === 0) {
              exampleSuggestions.push({
                label: "(status_code>=400 AND status_code<500)",
                kind: monaco.languages.CompletionItemKind.User,
                insertText: "(status_code>=400 AND status_code<500)",
                range: wordRange,
                detail: "Client error filter",
                documentation: {
                  value:
                    "**Client Error Filter**\n\nThis example uses parentheses to group conditions. It finds logs where:\n\n- status_code is between 400 and 499 (client errors)\n\nParentheses are used for clarity, but they're not strictly needed in this example since we're only using AND operators.",
                  isTrusted: true,
                },
                sortText: "9-example-5",
              });
            }

            // Example for complex nested expression
            if (currentWord.length === 0) {
              exampleSuggestions.push({
                label: "(method='GET' OR method='POST') AND status_code>=400",
                kind: monaco.languages.CompletionItemKind.User,
                insertText:
                  "(method='GET' OR method='POST') AND status_code>=400",
                range: wordRange,
                detail: "Complex API error filter",
                documentation: {
                  value:
                    "**Complex Filter with Operator Precedence**\n\nThis example demonstrates the importance of parentheses for controlling operator precedence. It finds logs where:\n\n- The HTTP method is either GET or POST, AND\n- The status code is 400 or higher (an error)\n\nWithout parentheses, the `AND` would bind more tightly than the `OR`, giving incorrect results. Parentheses ensure the `OR` expression is evaluated as a unit.",
                  isTrusted: true,
                },
                sortText: "9-example-6",
              });
            }

            // Show both examples and fields when appropriate:
            // - At the start of a line
            // - After AND/OR operators
            // - After an opening parenthesis
            // - When typing a partial field name
            const shouldShowExamples =
              beforeCursor.trim() === "" || // Empty line
              /\s+(?:AND|OR|and|or)\s+$/i.test(beforeCursor) || // After logical operator
              /\(\s*$/.test(beforeCursor) || // After opening parenthesis
              (beforeCursor.trim() === "" && currentWord.length > 0); // Starting to type

            if (shouldShowExamples || currentWord.length > 0) {
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

          // After a complete expression, suggest logical operators
          const isCompleteExpression =
            /[a-zA-Z0-9_]+\s*[=<>!~]+\s*(?:['"][^'"]*['"]|\d+)$/i.test(
              beforeCursor
            ) || /\)$/.test(beforeCursor);

          if (isCompleteExpression) {
            return {
              suggestions: [
                {
                  label: "AND",
                  kind: monaco.languages.CompletionItemKind.Keyword,
                  insertText: " AND ",
                  range: wordRange,
                  detail: "Combine with another condition (all must match)",
                  documentation: {
                    value:
                      "**AND Operator**\n\nThe AND operator requires both conditions to be true.\n\nExample: `service='api' AND status_code=404`",
                    isTrusted: true,
                  },
                  sortText: "keyword-1",
                },
                {
                  label: "OR",
                  kind: monaco.languages.CompletionItemKind.Keyword,
                  insertText: " OR ",
                  range: wordRange,
                  detail: "Combine with another condition (any can match)",
                  documentation: {
                    value:
                      "**OR Operator**\n\nThe OR operator requires at least one condition to be true.\n\nExample: `method='GET' OR method='POST'`",
                    isTrusted: true,
                  },
                  sortText: "keyword-2",
                },
                {
                  label: "and",
                  kind: monaco.languages.CompletionItemKind.Keyword,
                  insertText: " and ",
                  range: wordRange,
                  detail: "Lowercase AND operator (identical to AND)",
                  documentation: {
                    value:
                      "**Lowercase AND**\n\nLogChefQL supports both uppercase `AND` and lowercase `and` operators.\n\nThey function identically - it's just a matter of style preference.",
                    isTrusted: true,
                  },
                  sortText: "keyword-3",
                },
                {
                  label: "or",
                  kind: monaco.languages.CompletionItemKind.Keyword,
                  insertText: " or ",
                  range: wordRange,
                  detail: "Lowercase OR operator (identical to OR)",
                  documentation: {
                    value:
                      "**Lowercase OR**\n\nLogChefQL supports both uppercase `OR` and lowercase `or` operators.\n\nThey function identically - it's just a matter of style preference.",
                    isTrusted: true,
                  },
                  sortText: "keyword-4",
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
