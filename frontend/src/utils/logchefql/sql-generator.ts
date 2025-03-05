import type { LogChefQLNode, BinaryNode, ComparisonNode, StringNode, NumberNode } from './ast';
import { OPERATOR_MAPPINGS } from './ast';

/**
 * Generates SQL from a LogChefQL AST
 */
export function generateSQL(node: LogChefQLNode): string {
  switch (node.type) {
    case 'binary':
      return generateBinarySQL(node);
    case 'comparison':
      return generateComparisonSQL(node);
    case 'string':
    case 'number':
      return generateValueSQL(node);
    default:
      throw new Error(`Unknown node type: ${(node as any).type}`);
  }
}

/**
 * Generates SQL for binary expressions (AND/OR)
 */
function generateBinarySQL(node: BinaryNode): string {
  const left = generateSQL(node.left);
  const right = generateSQL(node.right);
  
  // Add parentheses to ensure correct precedence
  return `(${left}) ${node.operator} (${right})`;
}

/**
 * Generates SQL for comparison expressions (field operator value)
 */
function generateComparisonSQL(node: ComparisonNode): string {
  const field = node.field;
  const value = generateValueSQL(node.value);
  
  // Map the operator to its SQL equivalent
  const sqlOperator = mapOperatorToSQL(node.operator);
  
  switch (sqlOperator) {
    case 'contains':
      return `position(${field}, ${value}) > 0`;
    case 'not_contains':
      return `position(${field}, ${value}) = 0`;
    default:
      return `${field} ${sqlOperator} ${value}`;
  }
}

/**
 * Generates SQL for values (strings, numbers)
 */
function generateValueSQL(node: StringNode | NumberNode): string {
  if (node.type === 'string') {
    // Escape single quotes by doubling them
    const escapedValue = node.value.replace(/'/g, "''");
    return `'${escapedValue}'`;
  } else {
    return node.value.toString();
  }
}

/**
 * Maps LogChefQL operators to SQL operators
 */
function mapOperatorToSQL(operator: string): string {
  const sqlOperator = OPERATOR_MAPPINGS[operator];
  if (!sqlOperator) {
    throw new Error(`Unknown operator: ${operator}`);
  }
  return sqlOperator;
}