import { Operator, Expression, Node, Parser as LogchefQLParser } from "./index"; // Import Parser directly

/**
 * Translates a LogchefQL query node tree to SQL WHERE clause conditions.
 * @param node The root node of the parsed LogchefQL query.
 * @returns SQL WHERE clause conditions string (without the "WHERE" keyword), or empty string if node is null/empty.
 */
export function translateToSQLConditions(node: Node | null): string {
  if (!node) {
    return "";
  }

  const sqlConditions = nodeToSQL(node);
  return sqlConditions;
}

/**
 * Recursively converts a LogchefQL node to an SQL condition string.
 */
function nodeToSQL(node: Node | null): string {
  if (!node) return "";

  // If it's a leaf node with an expression
  if (node.expression) {
    return expressionToSQL(node.expression);
  }

  // If it's an internal node, process children recursively
  const leftSql = nodeToSQL(node.left);
  const rightSql = nodeToSQL(node.right);

  // Handle cases where one or both sides are empty
  if (!leftSql && !rightSql) return "";
  if (!leftSql) return rightSql; // No need for parentheses if only one side exists
  if (!rightSql) return leftSql; // No need for parentheses if only one side exists

  // Combine left and right with the boolean operator, adding parentheses
  // Parentheses are important for correct operator precedence in complex queries
  const operator = node.boolOperator.toUpperCase(); // AND / OR
  return `(${leftSql}) ${operator} (${rightSql})`;
}

/**
 * Converts a single LogchefQL expression (key-operator-value) to an SQL condition.
 */
function expressionToSQL(expr: Expression): string {
  // Basic validation
  if (!expr || !expr.key || !expr.operator) {
    console.warn("expressionToSQL: Invalid expression object", expr);
    return ""; // Return empty string for invalid expressions
  }

  const { key, operator, value } = expr;
  const formattedValue = formatValueForSQL(value);

  // Use backticks for keys to handle potential reserved words or special characters
  const formattedKey = `\`${key}\``;

  switch (operator) {
    case Operator.EQUALS:
      return `${formattedKey} = ${formattedValue}`;
    case Operator.NOT_EQUALS:
      return `${formattedKey} != ${formattedValue}`;
    case Operator.EQUALS_REGEX:
      // Use positionCaseInsensitive > 0 for substring check
      return `positionCaseInsensitive(${formattedKey}, ${formatValueForSQL(String(value))}) > 0`;
    case Operator.NOT_EQUALS_REGEX:
      // Use positionCaseInsensitive = 0 for substring absence check
      return `positionCaseInsensitive(${formattedKey}, ${formatValueForSQL(String(value))}) = 0`;
    case Operator.GREATER_THAN:
      return `${formattedKey} > ${formattedValue}`;
    case Operator.LOWER_THAN:
      return `${formattedKey} < ${formattedValue}`;
    case Operator.GREATER_OR_EQUALS_THAN:
      return `${formattedKey} >= ${formattedValue}`;
    case Operator.LOWER_OR_EQUALS_THAN:
      return `${formattedKey} <= ${formattedValue}`;
    default:
      console.warn(`expressionToSQL: Unsupported operator "${operator}"`, expr);
      // Fallback to equality or return empty string? Let's fallback for now.
      return `${formattedKey} = ${formattedValue}`;
  }
}

/**
 * Formats a value for safe inclusion in an SQL query.
 * Handles numbers and strings (with escaping).
 */
function formatValueForSQL(value: string | number | undefined | null): string {
    if (value === null || value === undefined) {
        return 'NULL'; // Or handle as needed, e.g., `''`?
    }
    const strValue = String(value);

    // Check if it's a number (integer or float)
    // Use a stricter check than isNaN alone
    if (/^-?\d+(\.\d+)?$/.test(strValue.trim())) {
        return strValue.trim();
    }

    // It's a string: escape single quotes and wrap in single quotes
    const escapedValue = strValue.replace(/'/g, "''");
    return `'${escapedValue}'`;
}

/**
 * Validates a LogchefQL query string using the Parser.
 * @param query The LogchefQL query string.
 * @returns True if the query is syntactically valid, false otherwise.
 */
export function validateLogchefQL(query: string): boolean {
  // Allow empty query as valid
  if (!query || query.trim() === "") {
    return true;
  }
  try {
    const parser = new LogchefQLParser();
    // Use raiseError=true to catch parsing issues
    parser.parse(query, true);
    // Additional check: ensure the parser produced a root node if the query wasn't empty
    return !!parser.root || query.trim() === "";
  } catch (error) {
    // console.warn("LogchefQL validation failed:", error); // Optional: Log for debugging
    return false;
  }
}

/**
 * Parses a LogchefQL query and returns the translated SQL conditions.
 * Includes error handling.
 * @param query The LogchefQL query string.
 * @returns Object containing success status, SQL conditions, or an error message.
 */
export function parseAndTranslateLogchefQL(query: string): { success: boolean; sql?: string; error?: string } {
  // Handle empty query explicitly
  if (!query || query.trim() === "") {
    return { success: true, sql: "" };
  }

  try {
    const parser = new LogchefQLParser();
    parser.parse(query, true); // Raise error on failure

    // Check if parsing resulted in a valid root node
    if (!parser.root) {
      // This might happen for queries that parse but are logically empty, e.g., "()"
      // Treat as success but with empty SQL
      return { success: true, sql: "" };
    }

    const sqlConditions = translateToSQLConditions(parser.root);
    return { success: true, sql: sqlConditions };
  } catch (error: any) {
    console.error("Error parsing or translating LogchefQL:", error);
    return { success: false, error: error?.message || "Failed to parse LogchefQL" };
  }
}
