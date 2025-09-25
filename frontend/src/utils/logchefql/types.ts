export enum Operator {
  EQUALS = '=',
  NOT_EQUALS = '!=',
  REGEX = '~',
  NOT_REGEX = '!~',
  GT = '>',
  LT = '<',
  GTE = '>=',
  LTE = '<=',
}

export enum BoolOperator {
  AND = 'AND',
  OR = 'OR',
}

export type Value = string | number | boolean | null;

export type ASTNode =
  | {
      type: 'expression';
      key: string | NestedField;
      operator: Operator;
      value: Value;
      // Preserve whether the original value was quoted for display/formatting purposes
      quoted?: boolean;
    }
  | {
      type: 'logical';
      operator: BoolOperator;
      children: ASTNode[];
    }
  | {
      type: 'group';
      children: ASTNode[];
    }
  | {
      type: 'query';
      where?: ASTNode;
      select?: SelectField[];
    };

export interface SelectField {
  field: string | NestedField;
  alias?: string;
}

export interface NestedField {
  base: string;
  path: string[];
}

export interface Token {
  type: 'key' | 'operator' | 'value' | 'paren' | 'bool' | 'pipe';
  value: string;
  position: {
    line: number;
    column: number;
  };
  // Indicates the token originated from a quoted string literal
  quoted?: boolean;
  // Indicates the token is potentially incomplete (e.g., unterminated string)
  incomplete?: boolean;
}

export interface ParseError {
  code: string;
  message: string;
  position?: {
    line: number;
    column: number;
  };
}

export const ErrorCodes = {
  UNTERMINATED_STRING: 'UNTERMINATED_STRING',
  UNEXPECTED_END: 'UNEXPECTED_END',
  UNEXPECTED_TOKEN: 'UNEXPECTED_TOKEN',
  EXPECTED_OPERATOR: 'EXPECTED_OPERATOR',
  EXPECTED_VALUE: 'EXPECTED_VALUE',
  EXPECTED_CLOSING_PAREN: 'EXPECTED_CLOSING_PAREN',
  UNKNOWN_OPERATOR: 'UNKNOWN_OPERATOR',
  UNKNOWN_BOOLEAN_OPERATOR: 'UNKNOWN_BOOLEAN_OPERATOR',
  INVALID_TOKEN_TYPE: 'INVALID_TOKEN_TYPE',
} as const;