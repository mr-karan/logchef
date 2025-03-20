import { Operator, VALID_KEY_VALUE_OPERATORS, Expression, Node } from "./index";

/**
 * Translates a LogchefQL query to SQL WHERE clause only
 * This function doesn't generate a complete SQL query, just the WHERE conditions
 * @param node The query node tree
 * @returns SQL WHERE clause conditions string (without the "WHERE" keyword)
 */
export function translateToSQLConditions(node: Node | null): string {
  if (!node) {
    console.warn(
      "translateToSQLConditions: Node is null, returning empty string"
    );
    return "";
  }

  console.log("translateToSQLConditions: Processing node", {
    hasExpression: !!node.expression,
    hasLeft: !!node.left,
    hasRight: !!node.right,
    boolOperator: node.boolOperator,
  });

  const sqlConditions = nodeToSQL(node);
  console.log(
    "translateToSQLConditions: Generated SQL conditions:",
    sqlConditions
  );

  return sqlConditions;
}

/**
 * Converts a LogchefQL node to SQL WHERE clause
 */
function nodeToSQL(node: Node | null): string {
  if (!node) return "";

  if (node.expression) {
    return expressionToSQL(node.expression);
  }

  const left = nodeToSQL(node.left);
  const right = nodeToSQL(node.right);

  // This is important! If either side is empty, don't include the condition
  if (!left && !right) return "";
  if (!left) return right;
  if (!right) return left;

  const operator = node.boolOperator.toUpperCase();
  
  // Check if left or right are already wrapped in parentheses
  const leftWrapped = left.trim().startsWith('(') && left.trim().endsWith(')');
  const rightWrapped = right.trim().startsWith('(') && right.trim().endsWith(')');
  
  // Only add parentheses if not already wrapped
  const wrappedLeft = leftWrapped ? left : `(${left})`;
  const wrappedRight = rightWrapped ? right : `(${right})`;
  
  return `${wrappedLeft} ${operator} ${wrappedRight}`;
}

/**
 * Converts a LogchefQL expression to SQL condition
 */
function expressionToSQL(expr: Expression): string {
  if (!expr) {
    console.warn("expressionToSQL called with null/undefined expression");
    return "";
  }

  const { key, operator, value } = expr;

  if (!key || !operator) {
    console.warn(
      "expressionToSQL: Invalid expression, missing key or operator",
      { key, operator, value }
    );
    return "";
  }

  // Handle different operators
  switch (operator) {
    case Operator.EQUALS:
      return `${key} = ${formatValue(value)}`;
    case Operator.NOT_EQUALS:
      return `${key} != ${formatValue(value)}`;
    case Operator.EQUALS_REGEX:
      return `positionCaseInsensitive(${key}, ${formatValue(value)}) > 0`;
    case Operator.NOT_EQUALS_REGEX:
      return `positionCaseInsensitive(${key}, ${formatValue(value)}) = 0`;
    case Operator.GREATER_THAN:
      return `${key} > ${formatValue(value)}`;
    case Operator.LOWER_THAN:
      return `${key} < ${formatValue(value)}`;
    case Operator.GREATER_OR_EQUALS_THAN:
      return `${key} >= ${formatValue(value)}`;
    case Operator.LOWER_OR_EQUALS_THAN:
      return `${key} <= ${formatValue(value)}`;
    default:
      return `${key} = ${formatValue(value)}`;
  }
}

/**
 * Formats a value for SQL, adding quotes if it's a string
 */
function formatValue(value: string): string {
  // If it's a numeric value, don't add quotes
  if (!isNaN(Number(value)) && value.trim() !== "") {
    return value;
  }

  // Otherwise, escape any single quotes and add quotes
  return `'${value.replace(/'/g, "''")}'`;
}

/**
 * Validates a LogchefQL query
 * @param query The query string
 * @returns True if valid, false otherwise
 */
export function validateLogchefQL(query: string): boolean {
  try {
    // Import parser dynamically to avoid circular dependencies
    const { Parser } = require("./index");
    const parser = new Parser();
    parser.parse(query, true);
    return true;
  } catch (error) {
    return false;
  }
}

/**
 * Test function for verifying the translation from LogchefQL to SQL
 * This can be used in unit tests or debugging
 * @param query The LogchefQL query to test
 * @returns The translated SQL condition
 */
export function testTranslateLogchefQL(query: string): string {
  try {
    // Import parser dynamically to avoid circular dependencies
    const { Parser } = require("./index");
    const parser = new Parser();
    parser.parse(query);
    if (parser.root) {
      return translateToSQLConditions(parser.root);
    }
    return "";
  } catch (error) {
    console.error("Error translating LogchefQL:", error);
    return `Error: ${error instanceof Error ? error.message : String(error)}`;
  }
}
