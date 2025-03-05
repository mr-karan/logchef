/**
 * AST type definitions for LogChefQL
 */

// Type definitions
export type LogChefQLNode = BinaryNode | ComparisonNode | ValueNode;

export interface BinaryNode {
  type: 'binary';
  operator: 'AND' | 'OR';
  left: LogChefQLNode;
  right: LogChefQLNode;
}

export interface ComparisonNode {
  type: 'comparison';
  field: string;
  operator: string;
  value: ValueNode;
}

export type ValueNode = StringNode | NumberNode;

export interface StringNode {
  type: 'string';
  value: string;
}

export interface NumberNode {
  type: 'number';
  value: number;
}

/**
 * Map LogChefQL operators to SQL operators
 */
export const OPERATOR_MAPPINGS: Record<string, string> = {
  '=': '=',
  '!=': '!=',
  '>': '>',
  '<': '<',
  '>=': '>=',
  '<=': '<=',
  '~': 'contains',
  '!~': 'not_contains',
};

// No default export needed - just use named exports