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
  FieldTrigger = "fieldTrigger",
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

/**
 * Register the LogFilter language with Monaco
 */
export function registerLogFilterLanguage() {
  // Register language
  monaco.languages.register({ id: "logfilter" });

  // Define syntax highlighting
  monaco.languages.setMonarchTokensProvider("logfilter", {
    tokenizer: {
      root: [
        // Comments
        [/#.*$/, TokenType.Comment],
        
        // Field trigger (@)
        [/@/, TokenType.FieldTrigger],

        // Field names (more precise pattern for PromQL/LogQL style)
        [/([a-zA-Z_][a-zA-Z0-9_]*)(?=(!?~|!=|=|>=|<=|>|<))/, TokenType.Field],

        // Field names at start of input or after delimiter
        [/^([a-zA-Z_][a-zA-Z0-9_]*)$/, TokenType.Field],
        [/;\s*([a-zA-Z_][a-zA-Z0-9_]*)$/, TokenType.Field],

        // Operators
        [/(!?~|!=|=|>=|<=|>|<)/, TokenType.Operator],

        // Quoted strings
        [/"([^"\\]|\\.)*"/, TokenType.String],
        [/'([^'\\]|\\.)*'/, TokenType.String],

        // Numbers
        [/\b\d+(\.\d+)?\b/, TokenType.Number],

        // Semicolons (delimiters)
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
    wordPattern:
      /(-?\d*\.\d\w*)|([^\`\~\!\@\#\%\^\&\*\(\)\-\=\+\[\{\]\}\\\|\;\:\'\"\,\.\<\>\/\?\s]+)/g,
  });
}

/**
 * Register completion provider for LogFilter language
 * @param columns Available columns for autocompletion
 */
export function registerCompletionProvider(columns: ColumnInfo[] = []) {
  return monaco.languages.registerCompletionItemProvider("logfilter", {
    triggerCharacters: ["@", "=", "!", "~", ">", "<", ";"],
    provideCompletionItems: (model, position) => {
      const textUntilPosition = model.getValueInRange({
        startLineNumber: position.lineNumber,
        startColumn: 1,
        endLineNumber: position.lineNumber,
        endColumn: position.column,
      });

      const word = model.getWordUntilPosition(position);
      const range = {
        startLineNumber: position.lineNumber,
        endLineNumber: position.lineNumber,
        startColumn: word.startColumn,
        endColumn: word.endColumn,
      };

      // Check if we just typed @ to trigger field suggestions
      if (textUntilPosition.endsWith("@")) {
        return {
          suggestions: columns.map((column) => ({
            label: column.name,
            kind: monaco.languages.CompletionItemKind.Field,
            insertText: column.name,
            range: {
              startLineNumber: position.lineNumber,
              endLineNumber: position.lineNumber,
              startColumn: position.column,
              endColumn: position.column,
            },
            detail: column.type,
            sortText: "0" + column.name, // Prioritize these suggestions
          })),
        };
      }

      // Determine context for suggestions
      const context = determineCompletionContext(textUntilPosition);

      switch (context.type) {
        case "field":
          return provideFieldSuggestions(columns, range, context.prefix);
        case "operator":
          return provideOperatorSuggestions(range);
        case "value":
          return provideValueSuggestions(context.field, columns, range);
        default:
          return { suggestions: [] };
      }
    },
  });
}

/**
 * Determine the context for completion suggestions
 */
function determineCompletionContext(text: string): {
  type: "field" | "operator" | "value" | "none";
  field?: string;
  prefix?: string;
} {
  // Check if we're after a semicolon or at the start - suggest field
  if (text === "" || text.trim() === "" || /;\s*$/.test(text)) {
    return { type: "field", prefix: "" };
  }

  // Check if we're typing a field name (after semicolon or at start)
  const fieldStartMatch = /(?:^|;\s*)([a-zA-Z_][a-zA-Z0-9_]*)$/.exec(text);
  if (fieldStartMatch) {
    return { type: "field", prefix: fieldStartMatch[1] };
  }

  // Check if we're after a field name - suggest operator
  // No spaces required between field and operator (PromQL/LogQL style)
  const afterFieldMatch = /([a-zA-Z_][a-zA-Z0-9_]*)$/.exec(text);
  if (
    afterFieldMatch &&
    !/[=!~<>]/.test(text.slice(text.lastIndexOf(afterFieldMatch[1])))
  ) {
    return { type: "operator" };
  }

  // Check if we're after an operator - suggest value
  // No spaces required between operator and value (PromQL/LogQL style)
  const operatorMatch = /([a-zA-Z_][a-zA-Z0-9_]*)(!?~|!=|=|>=|<=|>|<)$/.exec(text);
  if (operatorMatch) {
    return { type: "value", field: operatorMatch[1] };
  }

  return { type: "none" };
}

/**
 * Provide field name suggestions
 */
function provideFieldSuggestions(
  columns: ColumnInfo[],
  range: monaco.IRange,
  prefix: string = ""
): monaco.languages.CompletionList {
  // Filter columns based on prefix for better matching
  const filteredColumns = prefix
    ? columns.filter((col) =>
        col.name.toLowerCase().includes(prefix.toLowerCase())
      )
    : columns;

  return {
    suggestions: filteredColumns.map((column) => ({
      label: column.name,
      kind: monaco.languages.CompletionItemKind.Field,
      insertText: column.name,
      range,
      detail: column.type,
      sortText:
        prefix && column.name.toLowerCase().startsWith(prefix.toLowerCase())
          ? "0" + column.name // Prioritize exact prefix matches
          : "1" + column.name,
    })),
  };
}

/**
 * Provide operator suggestions
 */
function provideOperatorSuggestions(
  range: monaco.IRange
): monaco.languages.CompletionList {
  return {
    suggestions: [
      {
        label: OPERATORS.EQUALS,
        kind: monaco.languages.CompletionItemKind.Operator,
        insertText: OPERATORS.EQUALS,
        range,
        detail: "Equals",
        documentation: "Field exactly matches value",
      },
      {
        label: OPERATORS.NOT_EQUALS,
        kind: monaco.languages.CompletionItemKind.Operator,
        insertText: OPERATORS.NOT_EQUALS,
        range,
        detail: "Not equals",
        documentation: "Field does not match value",
      },
      {
        label: OPERATORS.GREATER_THAN,
        kind: monaco.languages.CompletionItemKind.Operator,
        insertText: OPERATORS.GREATER_THAN,
        range,
        detail: "Greater than",
        documentation: "Field is greater than value",
      },
      {
        label: OPERATORS.LESS_THAN,
        kind: monaco.languages.CompletionItemKind.Operator,
        insertText: OPERATORS.LESS_THAN,
        range,
        detail: "Less than",
        documentation: "Field is less than value",
      },
      {
        label: OPERATORS.GREATER_EQUAL,
        kind: monaco.languages.CompletionItemKind.Operator,
        insertText: OPERATORS.GREATER_EQUAL,
        range,
        detail: "Greater than or equal",
        documentation: "Field is greater than or equal to value",
      },
      {
        label: OPERATORS.LESS_EQUAL,
        kind: monaco.languages.CompletionItemKind.Operator,
        insertText: OPERATORS.LESS_EQUAL,
        range,
        detail: "Less than or equal",
        documentation: "Field is less than or equal to value",
      },
      {
        label: OPERATORS.CONTAINS,
        kind: monaco.languages.CompletionItemKind.Operator,
        insertText: OPERATORS.CONTAINS,
        range,
        detail: "Contains",
        documentation: "Field contains value",
      },
      {
        label: OPERATORS.NOT_CONTAINS,
        kind: monaco.languages.CompletionItemKind.Operator,
        insertText: OPERATORS.NOT_CONTAINS,
        range,
        detail: "Not contains",
        documentation: "Field does not contain value",
      },
    ],
  };
}

/**
 * Provide value suggestions based on field type
 */
function provideValueSuggestions(
  field: string | undefined,
  columns: ColumnInfo[],
  range: monaco.IRange
): monaco.languages.CompletionList {
  if (!field) return { suggestions: [] };

  // Find the column to get its type
  const column = columns.find((col) => col.name === field);
  if (!column) return { suggestions: [] };

  // Provide suggestions based on column type
  switch (column.type.toLowerCase()) {
    case "string":
      return { suggestions: [] }; // Free-form text, no suggestions

    case "boolean":
      return {
        suggestions: [
          {
            label: "true",
            kind: monaco.languages.CompletionItemKind.Value,
            insertText: "true",
            range,
          },
          {
            label: "false",
            kind: monaco.languages.CompletionItemKind.Value,
            insertText: "false",
            range,
          },
        ],
      };

    // Add more type-specific suggestions as needed

    default:
      return { suggestions: [] };
  }
}

/**
 * Parse a filter expression string into FilterCondition objects
 */
export function parseFilterExpression(expression: string) {
  const filters = [];
  const trimmedExpression = expression.trim();

  if (!trimmedExpression) {
    return [];
  }

  // Split by semicolons to get individual filter sets
  const filterSets = trimmedExpression
    .split(";")
    .map((set) => set.trim())
    .filter(Boolean);

  for (const filterSet of filterSets) {
    // Updated regex for operator format: field=value, field!=value, field~value, field!~value
    // No spaces required between field, operator, and value (PromQL/LogQL style)
    const filterRegex =
      /([a-zA-Z_][a-zA-Z0-9_]*)(!?~|!=|=|>=|<=|>|<)([^;\s]+|"[^"]*"|'[^']*')/g;

    let match;
    while ((match = filterRegex.exec(filterSet)) !== null) {
      const field = match[1];
      const rawOperator = match[2];
      let value = match[3];

      // Remove quotes if present
      if (
        (value.startsWith('"') && value.endsWith('"')) ||
        (value.startsWith("'") && value.endsWith("'"))
      ) {
        value = value.substring(1, value.length - 1);
      }

      // Convert the display operator to the internal operator
      const mappedOperator = OPERATOR_MAPPINGS[rawOperator] || "=";

      filters.push({
        field,
        operator: mappedOperator,
        value,
      });
    }
  }

  return filters;
}

/**
 * Convert filter conditions to expression string
 */
export function filterConditionsToExpression(
  conditions: Array<{ field: string; operator: string; value: string }>
) {
  if (!conditions || conditions.length === 0) {
    return "";
  }

  // Build expressions for each condition
  return conditions
    .map((condition) => {
      // Find the display operator from our mapping
      const displayOperator =
        Object.entries(OPERATOR_MAPPINGS).find(
          ([, internalOp]) => internalOp === condition.operator
        )?.[0] || "=";

      // Check if value needs to be quoted
      const needsQuotes =
        !/^[0-9]+(\.[0-9]+)?$/.test(condition.value) &&
        condition.value.includes(" ");
      const quotedValue = needsQuotes
        ? `"${condition.value}"`
        : condition.value;

      return `${condition.field}${displayOperator}${quotedValue}`;
    })
    .join("; ");
}
