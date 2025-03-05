/**
 * A simplified parser for LogChefQL that doesn't require Nearley.js
 * This is a fallback for web environments where the full parser might have issues.
 */

import type { LogChefQLNode, ComparisonNode, BinaryNode, ValueNode } from './ast';

/**
 * Simple parser for LogChefQL expressions
 * Handles both complete and incomplete expressions during typing
 */
export function parseSimple(expression: string): LogChefQLNode | null {
  if (!expression || !expression.trim()) {
    return null;
  }

  try {
    // Check for `AND` or `OR` with no right operand yet (incomplete)
    const andOrIncomplete = /\s+(AND|OR)\s*$/i.test(expression.trim());
    if (andOrIncomplete) {
      // Just parse the left side for now
      const leftPart = expression.replace(/\s+(AND|OR)\s*$/i, '').trim();
      return parseExpression(leftPart);
    }

    // Handle semicolon-separated expressions (legacy support)
    // This is no longer recommended as we're moving to AND/OR syntax
    if (expression.includes(';')) {
      // Split by semicolons and only parse the first part
      const parts = expression.split(';').filter(p => p.trim());
      if (parts.length > 0) {
        return parseExpression(parts[0].trim());
      }
      return null;
    }

    // Regular parsing
    return parseExpression(expression.trim());
  } catch (error) {
    // If we get an error on a potentially incomplete expression, handle it gracefully
    if (isIncompleteExpression(expression)) {
      // Extract any valid field names
      const fieldMatch = expression.match(/^([a-zA-Z0-9_]+)/);
      if (fieldMatch) {
        const field = fieldMatch[1];
        
        // Return a partial comparison node
        return {
          type: 'comparison',
          field: field.trim(),
          operator: '=', // Default operator
          value: { type: 'string', value: '' }
        };
      }
    }
    
    console.error("Error parsing expression:", error);
    return null;
  }
}

/**
 * Parse an expression with proper operator precedence
 */
function parseExpression(expression: string): LogChefQLNode {
  // Handle parenthesized expressions first
  expression = expression.trim();
  
  // Check if the entire expression is wrapped in parentheses
  if (expression.startsWith('(') && expression.endsWith(')')) {
    // Make sure it's not just a part of the expression that's in parentheses
    const inner = expression.substring(1, expression.length - 1).trim();
    let balance = 0;
    let allBalanced = true;
    
    for (let i = 0; i < inner.length; i++) {
      if (inner[i] === '(') balance++;
      else if (inner[i] === ')') balance--;
      
      if (balance < 0) {
        allBalanced = false;
        break;
      }
    }
    
    if (allBalanced && balance === 0) {
      return parseExpression(inner);
    }
  }
  
  // Look for OR operator (lower precedence than AND) - case insensitive
  // Also support capital OR and lowercase or
  const orParts = splitByOperatorCaseInsensitive(expression, 'OR');
  if (orParts.length > 1) {
    let node: LogChefQLNode = parseExpression(orParts[0]);
    
    for (let i = 1; i < orParts.length; i++) {
      node = {
        type: 'binary',
        operator: 'OR',
        left: node,
        right: parseExpression(orParts[i])
      };
    }
    
    return node;
  }
  
  // Look for AND operator (higher precedence than OR) - case insensitive
  // Also support capital AND and lowercase and
  const andParts = splitByOperatorCaseInsensitive(expression, 'AND');
  if (andParts.length > 1) {
    let node: LogChefQLNode = parseExpression(andParts[0]);
    
    for (let i = 1; i < andParts.length; i++) {
      node = {
        type: 'binary',
        operator: 'AND',
        left: node,
        right: parseExpression(andParts[i])
      };
    }
    
    return node;
  }
  
  // Must be a comparison
  return parseComparison(expression);
}

/**
 * Split an expression by a logical operator (AND or OR), handling case insensitivity
 * and respecting parentheses and quoted strings
 */
function splitByOperatorCaseInsensitive(expression: string, operator: string): string[] {
  const result: string[] = [];
  let current = '';
  let inSingleQuote = false;
  let inDoubleQuote = false;
  let parenDepth = 0;
  
  // Convert to uppercase for easier comparison
  const upperOperator = operator.toUpperCase();
  
  let i = 0;
  while (i < expression.length) {
    // Check for the operator with proper word boundaries, but only if we're not in quotes or parentheses
    if (!inSingleQuote && !inDoubleQuote && parenDepth === 0) {
      // Look for: [space]OPERATOR[space] pattern (case insensitive)
      // This allows both "AND" and "and" to work, with proper word boundaries
      const remaining = expression.substring(i);
      const operatorRegex = new RegExp(`^\\s+${upperOperator}\\s+`, 'i');
      const match = remaining.match(operatorRegex);
      
      if (match) {
        if (current.trim()) {
          result.push(current.trim());
        }
        current = '';
        i += match[0].length;
        continue;
      }
    }
    
    const char = expression[i];
    
    // Handle quotes and parentheses
    if (char === "'" && (i === 0 || expression[i-1] !== '\\')) {
      inSingleQuote = !inSingleQuote;
    } else if (char === '"' && (i === 0 || expression[i-1] !== '\\')) {
      inDoubleQuote = !inDoubleQuote;
    } else if (!inSingleQuote && !inDoubleQuote) {
      if (char === '(') parenDepth++;
      else if (char === ')') parenDepth--;
    }
    
    current += char;
    i++;
  }
  
  // Add the last part
  if (current.trim()) {
    result.push(current.trim());
  }
  
  return result;
}

/**
 * Split an expression by an exact operator string, respecting parentheses and quoted strings
 * This is the original function kept for backwards compatibility
 */
function splitByOperator(expression: string, operator: string): string[] {
  const result: string[] = [];
  let current = '';
  let inSingleQuote = false;
  let inDoubleQuote = false;
  let parenDepth = 0;
  
  let i = 0;
  while (i < expression.length) {
    // Check for the operator, but only if we're not in quotes or parentheses
    if (!inSingleQuote && !inDoubleQuote && parenDepth === 0) {
      if (expression.substring(i, i + operator.length).toUpperCase() === operator) {
        if (current.trim()) {
          result.push(current.trim());
        }
        current = '';
        i += operator.length;
        continue;
      }
    }
    
    const char = expression[i];
    
    // Handle quotes and parentheses
    if (char === "'" && (i === 0 || expression[i-1] !== '\\')) {
      inSingleQuote = !inSingleQuote;
    } else if (char === '"' && (i === 0 || expression[i-1] !== '\\')) {
      inDoubleQuote = !inDoubleQuote;
    } else if (!inSingleQuote && !inDoubleQuote) {
      if (char === '(') parenDepth++;
      else if (char === ')') parenDepth--;
    }
    
    current += char;
    i++;
  }
  
  // Add the last part
  if (current.trim()) {
    result.push(current.trim());
  }
  
  return result;
}

/**
 * Check if an expression is incomplete (still being typed)
 */
export function isIncompleteExpression(expression: string): boolean {
  // Just a field name without operator
  if (/^[a-zA-Z0-9_]+$/.test(expression.trim())) {
    return true;
  }
  
  // Field with operator but no value yet
  if (/^[a-zA-Z0-9_]+\s*(=|!=|>=|<=|>|<|~|!~)(\s*)?$/.test(expression.trim())) {
    return true;
  }
  
  // Field with operator and opening quote but no closing quote
  if (/^[a-zA-Z0-9_]+\s*(=|!=|>=|<=|>|<|~|!~)\s*['"]([^'"]*)?$/.test(expression.trim())) {
    return true;
  }
  
  // Check for incomplete AND/OR expressions (ending with AND/OR)
  if (/\s(AND|OR)(\s*)?$/i.test(expression.trim())) {
    return true;
  }
  
  // Check for opening parenthesis without closing parenthesis
  if ((expression.match(/\(/g) || []).length > (expression.match(/\)/g) || []).length) {
    return true;
  }
  
  return false;
}

/**
 * Parse a comparison expression (field operator value)
 */
function parseComparison(expression: string): ComparisonNode {
  // Check if this is an incomplete expression
  if (isIncompleteExpression(expression)) {
    // For incomplete expressions, create a partial ComparisonNode with empty value
    const fieldMatch = expression.match(/^([a-zA-Z0-9_]+)/);
    if (fieldMatch) {
      const field = fieldMatch[1];
      
      // Check if there's an operator
      const operatorMatch = expression.match(/^[a-zA-Z0-9_]+\s*(=|!=|>=|<=|>|<|~|!~)/);
      const operator = operatorMatch ? operatorMatch[1] : '='; // Default to = if no operator yet
      
      return {
        type: 'comparison',
        field: field.trim(),
        operator: operator.trim(),
        value: { type: 'string', value: '' }
      };
    }
  }
  
  // Match field, operator, and value
  const match = expression.match(/^([a-zA-Z0-9_]+)\s*(=|!=|>=|<=|>|<|~|!~)\s*(.+)$/);
  
  if (!match) {
    throw new Error(`Invalid comparison expression: ${expression}`);
  }
  
  const [, field, operator, rawValue] = match;
  let value: ValueNode;
  
  // Parse the value
  if ((rawValue.startsWith('"') && rawValue.endsWith('"')) || 
      (rawValue.startsWith("'") && rawValue.endsWith("'"))) {
    // String value
    const stringValue = rawValue.substring(1, rawValue.length - 1);
    value = { type: 'string', value: stringValue };
  } else if (!isNaN(Number(rawValue))) {
    // Number value
    value = { type: 'number', value: Number(rawValue) };
  } else {
    // Default to string if not clearly a number
    value = { type: 'string', value: rawValue };
  }
  
  return {
    type: 'comparison',
    field: field.trim(),
    operator: operator.trim(),
    value
  };
}

/**
 * Check if an expression is valid LogChefQL
 */
export function isValidSimple(expression: string): boolean {
  try {
    const result = parseSimple(expression);
    return result !== null;
  } catch (error) {
    return false;
  }
}