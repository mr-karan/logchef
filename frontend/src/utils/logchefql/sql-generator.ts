import type { ASTNode } from './types';
import { Operator } from './types';

export class SQLVisitor {
  private readonly parameterized: boolean;
  private params: unknown[] = [];

  constructor(parameterized = false) {
    this.parameterized = parameterized;
  }

  public generate(node: ASTNode): { sql: string; params: unknown[] } {
    this.params = [];
    return {
      sql: this.visit(node),
      params: this.parameterized ? this.params : [],
    };
  }

  private visit(node: ASTNode): string {
    switch (node.type) {
      case 'expression':
        return this.visitExpression(node);
      case 'logical':
        return this.visitLogical(node);
      case 'group':
        return this.visitGroup(node);
      default:
        return '';
    }
  }

  private visitExpression(node: ASTNode & { type: 'expression' }): string {
    const column = this.escapeIdentifier(node.key);

    // For regex operations, always handle the value as a string
    // regardless of its original type
    const value = node.operator === Operator.REGEX || node.operator === Operator.NOT_REGEX
      ? this.formatStringValue(node.value)
      : this.formatValue(node.value);

    switch (node.operator) {
      case Operator.REGEX:
        return `positionCaseInsensitive(${column}, ${value}) > 0`;
      case Operator.NOT_REGEX:
        return `positionCaseInsensitive(${column}, ${value}) = 0`;
      case Operator.EQUALS:
        return `${column} = ${value}`;
      case Operator.NOT_EQUALS:
        return `${column} != ${value}`;
      case Operator.GT:
        return `${column} > ${value}`;
      case Operator.LT:
        return `${column} < ${value}`;
      case Operator.GTE:
        return `${column} >= ${value}`;
      case Operator.LTE:
        return `${column} <= ${value}`;
      default:
        return '';
    }
  }

  private visitLogical(node: ASTNode & { type: 'logical' }): string {
    if (node.children.length === 0) {
      return '';
    }

    if (node.children.length === 1) {
      return this.visit(node.children[0]);
    }

    const conditions = node.children.map(child => this.visit(child)).filter(Boolean);

    if (conditions.length === 0) {
      return '';
    }

    if (conditions.length === 1) {
      return conditions[0];
    }

    const operator = node.operator;
    return conditions.map(condition => `(${condition})`).join(` ${operator} `);
  }

  private visitGroup(node: ASTNode & { type: 'group' }): string {
    if (node.children.length === 0) {
      return '';
    }

    if (node.children.length === 1) {
      return this.visit(node.children[0]);
    }

    // Handle multiple expressions in a group - if there's no explicit operator,
    // default to AND (similar to SQL WHERE clause behavior)
    const conditions = node.children.map(child => this.visit(child)).filter(Boolean);

    if (conditions.length === 0) {
      return '';
    }

    if (conditions.length === 1) {
      return conditions[0];
    }

    return `(${conditions.join(' AND ')})`;
  }

  private escapeIdentifier(identifier: string): string {
    return `\`${identifier.replace(/`/g, '``')}\``;
  }

  private formatValue(value: unknown): string {
    if (this.parameterized) {
      this.params.push(value);
      return '?';
    }

    if (value === null) {
      return 'NULL';
    }

    if (typeof value === 'boolean') {
      return value ? '1' : '0';
    }

    if (typeof value === 'number') {
      return value.toString();
    }

    // String value - properly escape using proper SQL escaping (replace single quotes with two single quotes)
    return this.formatStringValue(value);
  }

  // Added helper method to ensure consistent string formatting
  private formatStringValue(value: unknown): string {
    if (this.parameterized) {
      this.params.push(value);
      return '?';
    }

    // Handle null/undefined cases
    if (value === null || value === undefined) {
      return 'NULL';
    }

    // Ensure the value is treated as a string and properly escaped
    // for SQL - use standard SQL escaping (double single quotes)
    const stringValue = String(value);
    const escapedValue = stringValue.replace(/'/g, "''");
    return `'${escapedValue}'`;
  }
}