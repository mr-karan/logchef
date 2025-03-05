/**
 * LogChefQL - A simple and powerful query language for filtering logs
 * 
 * Examples:
 * - Simple field equality: status_code=404
 * - String matching: message~"error"
 * - Numeric comparison: duration>100
 * - Logical operators: method="GET" AND status_code=200
 * - Grouping: (method="GET" OR method="POST") AND status_code>=400
 */

import type { LogChefQLNode } from './logchefql/ast';
import { parseSimple, isValidSimple } from './logchefql/simple-parser';
import { generateSQL } from './logchefql/sql-generator';

export interface FilterCondition {
  field: string;
  operator?: string;
  op?: string;
  value: any;
}

/**
 * Parse a LogChefQL expression and generate SQL
 */
export function logchefqlToSQL(expression: string): string {
  try {
    const ast = parseSimple(expression);
    
    if (!ast) {
      return '';
    }
    
    return generateSQL(ast);
  } catch (error) {
    console.error('Error converting LogChefQL to SQL:', error);
    return '';
  }
}

/**
 * Parse a LogChefQL expression and convert to filter conditions
 * suitable for use with the existing API
 */
export function logchefqlToFilterConditions(expression: string): FilterCondition[] {
  try {
    const ast = parseSimple(expression);
    
    if (!ast) {
      return [];
    }
    
    // Extract filter conditions from the AST
    const conditions: FilterCondition[] = [];
    extractFilterConditions(ast, conditions);
    
    return conditions;
  } catch (error) {
    console.error('Error converting LogChefQL to filter conditions:', error);
    return [];
  }
}

/**
 * Extract filter conditions from an AST node
 */
function extractFilterConditions(node: LogChefQLNode, conditions: FilterCondition[]): void {
  if (node.type === 'binary') {
    // For binary operations (AND/OR), we need to extract from both sides
    // However, we can't represent OR operations in the flat filter conditions,
    // so we'll need to handle them specially in a real implementation.
    // For now, we'll just extract from both sides.
    extractFilterConditions(node.left, conditions);
    extractFilterConditions(node.right, conditions);
  } else if (node.type === 'comparison') {
    // For comparisons, add a filter condition
    conditions.push({
      field: node.field,
      operator: node.operator,
      value: node.value.type === 'string' ? node.value.value : node.value.value
    });
  }
}

/**
 * Convert filter conditions to a LogChefQL expression
 */
export function filterConditionsToLogchefql(conditions: FilterCondition[]): string {
  if (!conditions || !conditions.length) {
    return '';
  }
  
  // Convert each condition to a LogChefQL expression
  return conditions
    .map((condition) => {
      if (!condition || !condition.field) return '';
      
      const field = condition.field;
      const op = condition.operator || condition.op || '=';
      const value = condition.value;
      
      if (value === undefined || value === null) return '';
      
      // Format the value based on its type
      const formattedValue = typeof value === 'string' 
        ? `'${value.replace(/'/g, "\\'")}'` 
        : value;
      
      return `${field}${op}${formattedValue}`;
    })
    .filter(Boolean)
    .join(' AND ');
}

/**
 * Validate a LogChefQL expression
 */
export function isValidLogchefql(expression: string): boolean {
  return isValidSimple(expression);
}

// Re-export types
export type { LogChefQLNode } from './logchefql/ast';