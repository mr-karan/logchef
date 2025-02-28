import * as monaco from "monaco-editor";
import type { ColumnInfo } from "@/api/explore";

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
  [OPERATORS.NOT_CONTAINS]: "notcontains",
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

  // Define syntax highlighting (improved for better PromQL/LogQL-like experience)
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

  // Define language configuration (simplified for better stability)
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

  // Create simple and reliable completion provider
  currentCompletionProvider = monaco.languages.registerCompletionItemProvider(
    "logfilter",
    {
      // Trigger characters for showing completions
      triggerCharacters: [" ", ";", "=", "!", ">", "<", "~"],

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

          // Handle @ character trigger - insert field pattern
          if (lastChar === "@") {
            return {
              suggestions: columns.map((column) => ({
                label: column.name,
                kind: monaco.languages.CompletionItemKind.Field,
                insertText: `${column.name} `,
                range: {
                  startLineNumber: position.lineNumber,
                  endLineNumber: position.lineNumber,
                  startColumn: position.column - 1,
                  endColumn: position.column,
                },
                // Add documentation for better UX
                documentation: {
                  value: `**Field:** ${column.name}\n\n**Type:** ${column.type}\n\nThis field can be used with operators like =, !=, >, <, etc.`,
                  isTrusted: true,
                  supportHtml: true
                },
                detail: column.type,
                // Chain command to trigger operator suggestions after field insertion
                command: {
                  title: "Trigger Suggest",
                  id: "editor.action.triggerSuggest",
                  arguments: [],
                },
              })),
            };
          }

          // Handle field name partial typing (after space, semicolon, or at start)
          // This helps with suggestions when typing 'n' for 'namespace'
          const currentWord = wordInfo.word;
          if (
            /\b(AND|OR)\s+$/i.test(beforeCursor) ||
            /;\s*$/.test(beforeCursor) ||
            beforeCursor.trim() === "" ||
            (currentWord.length > 0 && !/[=<>!~]/.test(beforeCursor))
          ) {
            // Filter columns that match the current partial word
            const matchingColumns = columns.filter((column) =>
              column.name.toLowerCase().startsWith(currentWord.toLowerCase())
            );

            if (matchingColumns.length > 0) {
              return {
                suggestions: matchingColumns.map((column) => ({
                  label: column.name,
                  kind: monaco.languages.CompletionItemKind.Field,
                  insertText: column.name,
                  range: wordRange,
                  detail: column.type,
                })),
              };
            }
          }

          // Simple text-based context detection for other scenarios
          if (
            /\b(AND|OR)\s*$/i.test(beforeCursor) ||
            /;\s*$/.test(beforeCursor) ||
            beforeCursor.trim() === ""
          ) {
            // Suggest fields
            return {
              suggestions: columns.map((column) => ({
                label: column.name,
                kind: monaco.languages.CompletionItemKind.Field,
                insertText: column.name,
                range: wordRange,
                detail: column.type,
              })),
            };
          }

          // After field name, suggest operators
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
                  insertText: "= ",
                  insertTextRules:
                    monaco.languages.CompletionItemInsertTextRule
                      .InsertAsSnippet,
                  range: wordRange,
                },
                {
                  label: "!=",
                  kind: monaco.languages.CompletionItemKind.Operator,
                  insertText: " != ",
                  range: wordRange,
                },
                {
                  label: ">",
                  kind: monaco.languages.CompletionItemKind.Operator,
                  insertText: " > ",
                  range: wordRange,
                },
                {
                  label: "<",
                  kind: monaco.languages.CompletionItemKind.Operator,
                  insertText: " < ",
                  range: wordRange,
                },
                {
                  label: ">=",
                  kind: monaco.languages.CompletionItemKind.Operator,
                  insertText: " >= ",
                  range: wordRange,
                },
                {
                  label: "<=",
                  kind: monaco.languages.CompletionItemKind.Operator,
                  insertText: " <= ",
                  range: wordRange,
                },
                {
                  label: "~",
                  kind: monaco.languages.CompletionItemKind.Operator,
                  insertText: " ~ ",
                  range: wordRange,
                },
                {
                  label: "!~",
                  kind: monaco.languages.CompletionItemKind.Operator,
                  insertText: " !~ ",
                  range: wordRange,
                },
              ],
            };
          }

          // After a value, suggest logical operators
          if (
            /[a-zA-Z0-9_"']\s*$/.test(beforeCursor) &&
            (beforeCursor.includes("=") ||
              beforeCursor.includes(">") ||
              beforeCursor.includes("<") ||
              beforeCursor.includes("~"))
          ) {
            return {
              suggestions: [
                {
                  label: "AND",
                  kind: monaco.languages.CompletionItemKind.Keyword,
                  insertText: " AND ",
                  range: wordRange,
                },
                {
                  label: "OR",
                  kind: monaco.languages.CompletionItemKind.Keyword,
                  insertText: " OR ",
                  range: wordRange,
                },
                {
                  label: ";",
                  kind: monaco.languages.CompletionItemKind.Operator,
                  insertText: "; ",
                  range: wordRange,
                },
              ],
            };
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
 * Show field suggestions - ultra-simplified to prevent crashes
 * This version just delegates to the standard suggestion mechanism
 */
export function showFieldSuggestions(
  editor: monaco.editor.IStandaloneCodeEditor,
  columns: ColumnInfo[] = []
) {
  if (!editor) return;

  try {
    // Focus the editor first
    editor.focus();
    
    // Use the editor instance to trigger suggestions instead of monaco.editor
    editor.trigger('keyboard', 'editor.action.triggerSuggest', {});
  } catch (error) {
    console.error("Error showing field suggestions:", error);
  }
}

/**
 * Parse a filter expression to structured filter conditions
 */
export function parseFilterExpression(expression: string): any[] {
  if (!expression || !expression.trim()) {
    return [];
  }

  const result: any[] = [];
  // Split by semicolons, but not if they're inside quotes
  const conditions: string[] = [];
  let currentCondition = '';
  let insideQuotes = false;
  let quoteChar = '';

  // Parse character by character to handle quotes and semicolons correctly
  for (let i = 0; i < expression.length; i++) {
    const char = expression[i];
    
    // Handle quotes
    if ((char === '"' || char === "'") && (i === 0 || expression[i-1] !== '\\')) {
      if (!insideQuotes) {
        insideQuotes = true;
        quoteChar = char;
      } else if (char === quoteChar) {
        insideQuotes = false;
      }
    }
    
    // Handle semicolons - only split if not in quotes
    if (char === ';' && !insideQuotes) {
      if (currentCondition.trim()) {
        conditions.push(currentCondition.trim());
      }
      currentCondition = '';
    } else {
      currentCondition += char;
    }
  }
  
  // Add the last condition if any
  if (currentCondition.trim()) {
    conditions.push(currentCondition.trim());
  }

  for (const condition of conditions) {
    // Check for various operator patterns, from longest to shortest
    const operatorPatterns = [
      { regex: /(>=|<=|!=|!~|~)/, op: (match: string) => match },
      { regex: /(=|>|<)/, op: (match: string) => match }
    ];

    let parsed = false;

    for (const pattern of operatorPatterns) {
      const match = condition.match(new RegExp(`^([\\w_]+)\\s*${pattern.regex.source}\\s*(.+)$`));
      
      if (match) {
        const [, field, operator, value] = match;
        
        // Map UI operators to API operators
        let apiOperator = operator;
        if (operator === '~') apiOperator = 'contains';
        if (operator === '!~') apiOperator = 'not_contains';
        
        // Parse value - handle quoted strings and numbers
        let parsedValue = value.trim();
        if ((parsedValue.startsWith('"') && parsedValue.endsWith('"')) || 
            (parsedValue.startsWith("'") && parsedValue.endsWith("'"))) {
          parsedValue = parsedValue.substring(1, parsedValue.length - 1);
        } else if (!isNaN(Number(parsedValue))) {
          parsedValue = Number(parsedValue);
        }

        result.push({
          field: field.trim(),
          operator: apiOperator,
          value: parsedValue
        });
        
        parsed = true;
        break;
      }
    }

    // If we couldn't parse the condition with an operator, we'll only treat it as a text search
    // if it appears to be a complete search term (contains space or is longer than 3 chars)
    // This prevents partial typing from generating preview SQL
    const trimmedCondition = condition.trim();
    if (!parsed && trimmedCondition && (trimmedCondition.includes(" ") || trimmedCondition.length > 3)) {
      result.push({
        field: "_raw",
        operator: "contains",
        value: trimmedCondition
      });
    }
  }

  return result;
}

/**
 * Convert filter conditions to an expression string
 */
export function filterConditionsToExpression(conditions: any[]): string {
  if (!conditions || !conditions.length) {
    return "";
  }

  // Simple implementation that just returns the value from raw text searches
  if (
    conditions.length === 1 &&
    conditions[0]?.field === "_raw" &&
    conditions[0]?.op === "contains"
  ) {
    return conditions[0].value || "";
  }

  // Basic implementation for other conditions
  return conditions
    .map((condition) => {
      const { field, op, value } = condition;
      switch (op) {
        case "=":
        case "!=":
        case ">":
        case "<":
        case ">=":
        case "<=":
          return `${field} ${op} ${
            typeof value === "string" ? `"${value}"` : value
          }`;
        case "contains":
          return `${field} ~ "${value}"`;
        case "not_contains":
          return `${field} !~ "${value}"`;
        default:
          return "";
      }
    })
    .filter(Boolean)
    .join(" AND ");
}