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
      key: string;
      operator: Operator;
      value: Value;
    }
  | {
      type: 'logical';
      operator: BoolOperator;
      children: ASTNode[];
    }
  | {
      type: 'group';
      children: ASTNode[];
    };

export interface Token {
  type: 'key' | 'operator' | 'value' | 'paren' | 'bool';
  value: string;
  position: {
    line: number;
    column: number;
  };
}

export interface ParseError {
  code: string;
  message: string;
  position?: {
    line: number;
    column: number;
  };
}