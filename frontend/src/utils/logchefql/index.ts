// Define constants for token types and special characters
const DELIMITER = " ";
const DOT = ".";
const UNDERSCORE = "_";
const EQUAL_SIGN = "=";
const NOT_EQUAL = "!=";
const TILDE = "~";
const TILDE_EQUAL = "~=";
const NOT_TILDE = "!~";
const DOUBLE_QUOTE = '"';
const SINGLE_QUOTE = "'";
const NEWLINE = "\n";
const BRACKET_OPEN = "(";
const BRACKET_CLOSE = ")";
const GREATER_THAN = ">";
const LESS_THAN = "<";
const GREATER_EQUAL = ">=";
const LESS_EQUAL = "<=";

// Define token types for syntax highlighting
export const CharType = Object.freeze({
  KEY: "logchefqlKey",
  VALUE: "logchefqlValue",
  OPERATOR: "logchefqlOperator",
  NUMBER: "number",
  STRING: "string",
  SPACE: "space",
});

export const tokenTypes = [
  CharType.KEY,
  CharType.VALUE,
  CharType.OPERATOR,
  CharType.NUMBER,
  CharType.STRING,
];

// Boolean operators
export const BoolOperator = Object.freeze({
  AND: "and",
  OR: "or",
});

// Comparison operators
export const Operator = Object.freeze({
  EQUALS: "=",
  NOT_EQUALS: "!=",
  CONTAINS: "~",
  NOT_CONTAINS: "!~",
  GREATER_THAN: ">",
  LESS_THAN: "<",
  GREATER_OR_EQUALS: ">=",
  LESS_OR_EQUALS: "<=",
});

export const VALID_KEY_VALUE_OPERATORS = [
  Operator.EQUALS,
  Operator.NOT_EQUALS,
  Operator.CONTAINS,
  Operator.NOT_CONTAINS,
  Operator.GREATER_THAN,
  Operator.LESS_THAN,
  Operator.GREATER_OR_EQUALS,
  Operator.LESS_OR_EQUALS,
];

export const VALID_BOOL_OPERATORS = [
  BoolOperator.AND,
  BoolOperator.OR,
];

// Character class for token parsing
class Char {
  value: string;
  pos: number;
  line: number;
  linePos: number;

  constructor(value: string, pos: number, line: number, linePos: number) {
    this.value = value;
    this.pos = pos;
    this.line = line;
    this.linePos = linePos;
  }

  isDelimiter() {
    return this.value === DELIMITER;
  }

  isKey() {
    return (
      this.value.match(/^[a-zA-Z0-9]$/) ||
      this.value === UNDERSCORE ||
      this.value === DOT
    );
  }

  isOp() {
    return (
      this.value === EQUAL_SIGN ||
      this.value === "!" ||
      this.value === TILDE ||
      this.value === GREATER_THAN ||
      this.value === LESS_THAN
    );
  }

  isGroupOpen() {
    return this.value === BRACKET_OPEN;
  }

  isGroupClose() {
    return this.value === BRACKET_CLOSE;
  }

  isDoubleQuote() {
    return this.value === DOUBLE_QUOTE;
  }

  isSingleQuote() {
    return this.value === SINGLE_QUOTE;
  }

  isBackslash() {
    return this.value === "\\";
  }

  isValue() {
    return (
      !this.isDoubleQuote() &&
      !this.isSingleQuote() &&
      !this.isDelimiter() &&
      !this.isGroupOpen() &&
      !this.isGroupClose()
    );
  }

  isNewline() {
    return this.value === NEWLINE;
  }
}

// Token class for syntax highlighting
class Token {
  start: number;
  length: number;
  type: string;
  value: string;
  line: number;
  linePos: number;

  constructor(char: Char, charType: string) {
    this.start = char.pos;
    this.length = char.value.length;
    this.type = charType;
    this.value = char.value;
    this.line = char.line;
    this.linePos = char.linePos;
  }

  addChar(char: Char) {
    this.value += char.value;
    this.length += char.value.length;
  }
}

// Expression class to represent a filter condition
export class Expression {
  key: string;
  operator: string;
  value: string;

  constructor(key: string, operator: string, value: string) {
    this.key = key;
    this.operator = operator;
    this.value = value;
  }

  toString() {
    return `${this.key}${this.operator}${this.value}`;
  }

  // Convert to ClickHouse SQL WHERE clause
  toSQL(): string {
    // Handle special JSON path notation (p.field)
    if (this.key.includes('.')) {
      const [parent, field] = this.key.split('.');
      return this.toJsonSQL(parent, field);
    }

    switch (this.operator) {
      case Operator.EQUALS:
        return `${this.key} = ?`;
      case Operator.NOT_EQUALS:
        return `${this.key} != ?`;
      case Operator.CONTAINS:
        return `${this.key} ILIKE ?`;
      case Operator.NOT_CONTAINS:
        return `NOT(${this.key} ILIKE ?)`;
      case Operator.GREATER_THAN:
        return `${this.key} > ?`;
      case Operator.LESS_THAN:
        return `${this.key} < ?`;
      case Operator.GREATER_OR_EQUALS:
        return `${this.key} >= ?`;
      case Operator.LESS_OR_EQUALS:
        return `${this.key} <= ?`;
      default:
        return `${this.key} = ?`;
    }
  }

  // Handle JSON extractions for nested fields
  private toJsonSQL(parent: string, field: string): string {
    switch (this.operator) {
      case Operator.EQUALS:
        return `JSONExtractString(${parent}, '${field}') = ?`;
      case Operator.NOT_EQUALS:
        return `JSONExtractString(${parent}, '${field}') != ?`;
      case Operator.CONTAINS:
        return `JSONExtractString(${parent}, '${field}') ILIKE ?`;
      case Operator.NOT_CONTAINS:
        return `NOT(JSONExtractString(${parent}, '${field}') ILIKE ?)`;
      case Operator.GREATER_THAN:
        return `JSONExtractString(${parent}, '${field}') > ?`;
      case Operator.LESS_THAN:
        return `JSONExtractString(${parent}, '${field}') < ?`;
      case Operator.GREATER_OR_EQUALS:
        return `JSONExtractString(${parent}, '${field}') >= ?`;
      case Operator.LESS_OR_EQUALS:
        return `JSONExtractString(${parent}, '${field}') <= ?`;
      default:
        return `JSONExtractString(${parent}, '${field}') = ?`;
    }
  }

  // Get parameter value for SQL query
  getParamValue(): string | number {
    if (this.operator === Operator.CONTAINS || this.operator === Operator.NOT_CONTAINS) {
      return `%${this.value}%`;
    }

    // Return numeric value if applicable
    if (isNumeric(this.value)) {
      return parseFloat(this.value);
    }

    return this.value;
  }
}

// Node class for the AST
export class Node {
  boolOperator: string;
  expression: Expression | null;
  left: Node | null;
  right: Node | null;

  constructor(
    boolOperator: string,
    expression: Expression | null,
    left: Node | null,
    right: Node | null
  ) {
    if ((left || right) && expression) {
      throw new Error("Node can't have both expression and children");
    }

    this.boolOperator = boolOperator;
    this.expression = expression;
    this.left = left;
    this.right = right;
  }

  setBoolOperator(boolOperator: string) {
    if (!VALID_BOOL_OPERATORS.includes(boolOperator)) {
      throw new Error(`Invalid boolean operator: ${boolOperator}`);
    }
    this.boolOperator = boolOperator;
  }

  setLeft(node: Node) {
    this.left = node;
  }

  setRight(node: Node) {
    this.right = node;
  }

  setExpression(expression: Expression) {
    this.expression = expression;
  }

  // Generate SQL from the AST
  toSQL(): { sql: string; params: Array<string | number> } {
    if (this.expression) {
      const sql = this.expression.toSQL();
      return {
        sql,
        params: [this.expression.getParamValue()],
      };
    }

    if (this.left && this.right) {
      const leftSQL = this.left.toSQL();
      const rightSQL = this.right.toSQL();

      const boolOp = this.boolOperator.toUpperCase();
      const sql = `(${leftSQL.sql}) ${boolOp} (${rightSQL.sql})`;
      const params = [...leftSQL.params, ...rightSQL.params];

      return { sql, params };
    }

    return { sql: "", params: [] };
  }
}

// Parser state enum
export const State = Object.freeze({
  INITIAL: "Initial",
  ERROR: "Error",
  KEY: "Key",
  VALUE: "Value",
  SINGLE_QUOTED_VALUE: "SingleQuotedValue",
  DOUBLE_QUOTED_VALUE: "DoubleQuotedValue",
  KEY_VALUE_OPERATOR: "KeyValueOperator",
  BOOL_OP_DELIMITER: "BoolOpDelimiter",
  EXPECT_BOOL_OP: "ExpectBoolOp",
});

// Custom error class for parsing errors
export class LogchefQLError extends Error {
  constructor(message: string) {
    super(message);
    this.name = "LogchefQLError";
  }
}

export class ParserError extends LogchefQLError {
  errno: number;

  constructor(message: string, errno: number) {
    super(message);
    this.errno = errno;
  }

  toString() {
    return this.message;
  }
}

// Main parser class
export class Parser {
  pos: number;
  line: number;
  linePos: number;
  text: string;
  state: string;
  char: Char | null;
  key: string;
  value: string;
  keyValueOperator: string;
  boolOperator: string;
  currentNode: Node | null;
  nodesStack: Node[];
  boolOpStack: string[];
  errorText: string;
  errno: number;
  root: Node | null;
  typedChars: [Char, string][];

  constructor() {
    this.pos = 0;
    this.line = 0;
    this.linePos = 0;
    this.text = "";
    this.state = State.INITIAL;
    this.char = null;
    this.key = "";
    this.value = "";
    this.keyValueOperator = "";
    this.boolOperator = "and";
    this.currentNode = null;
    this.nodesStack = [];
    this.boolOpStack = [];
    this.errorText = "";
    this.errno = 0;
    this.root = null;
    this.typedChars = [];
  }

  setState(state: string) {
    this.state = state;
  }

  setText(text: string) {
    this.text = text;
  }

  setChar(char: Char) {
    this.char = char;
  }

  setCurrentNode(node: Node) {
    this.currentNode = node;
  }

  setErrorState(errorText: string, errno: number) {
    this.state = State.ERROR;
    this.errorText = errorText;
    this.errno = errno;
    if (this.char) {
      this.errorText += ` [char ${this.char.value} at ${this.char.pos}], errno=${errno}`;
    }
  }

  resetPos() {
    this.pos = 0;
  }

  resetKey() {
    this.key = "";
  }

  resetValue() {
    this.value = "";
  }

  resetKeyValueOperator() {
    this.keyValueOperator = "";
  }

  resetData() {
    this.resetKey();
    this.resetValue();
    this.resetKeyValueOperator();
  }

  resetBoolOperator() {
    this.boolOperator = "";
  }

  extendKey() {
    if (this.char) {
      this.key += this.char.value;
    }
  }

  extendValue() {
    if (this.char) {
      this.value += this.char.value;
    }
  }

  extendKeyValueOperator() {
    if (this.char) {
      this.keyValueOperator += this.char.value;
    }
  }

  extendBoolOperator() {
    if (this.char) {
      this.boolOperator += this.char.value;
    }
  }

  extendNodesStack() {
    if (this.currentNode) {
      this.nodesStack.push(this.currentNode);
    }
  }

  extendBoolOpStack() {
    this.boolOpStack.push(this.boolOperator);
  }

  newNode(
    boolOperator: string,
    expression: Expression | null,
    left: Node | null,
    right: Node | null
  ) {
    return new Node(boolOperator, expression, left, right);
  }

  newExpression() {
    return new Expression(this.key, this.keyValueOperator, this.value);
  }

  storeTypedChar(charType: string) {
    if (this.char) {
      this.typedChars.push([this.char, charType]);
    }
  }

  extendTree() {
    if (this.currentNode && !this.currentNode.left) {
      const node = this.newNode("", this.newExpression(), null, null);
      this.currentNode.setLeft(node);
      this.currentNode.setBoolOperator(this.boolOperator);
    } else if (this.currentNode && !this.currentNode.right) {
      const node = this.newNode("", this.newExpression(), null, null);
      this.currentNode.setRight(node);
      this.currentNode.setBoolOperator(this.boolOperator);
    } else {
      const right = this.newNode("", this.newExpression(), null, null);
      const node = this.newNode(this.boolOperator, null, this.currentNode, right);
      this.setCurrentNode(node);
    }
  }

  extendTreeFromStack(boolOperator: string) {
    const node = this.nodesStack.pop();
    if (node) {
      if (node.right === null) {
        node.right = this.currentNode;
        node.setBoolOperator(boolOperator);
        this.setCurrentNode(node);
      } else {
        const newNode = this.newNode(
          boolOperator,
          null,
          node,
          this.currentNode
        );
        this.setCurrentNode(newNode);
      }
    }
  }

  inStateInitial() {
    if (!this.char) return;

    this.resetData();
    this.setCurrentNode(this.newNode(this.boolOperator, null, null, null));

    if (this.char.isGroupOpen()) {
      this.extendNodesStack();
      this.extendBoolOpStack();
      this.setState(State.INITIAL);
      this.storeTypedChar(CharType.OPERATOR);
    } else if (this.char.isDelimiter()) {
      this.storeTypedChar(CharType.SPACE);
      this.setState(State.BOOL_OP_DELIMITER);
    } else if (this.char.isKey()) {
      this.extendKey();
      this.setState(State.KEY);
      this.storeTypedChar(CharType.KEY);
    } else {
      this.setErrorState("Invalid character", 1);
      return;
    }
  }

  inStateKey() {
    if (!this.char) return;

    if (this.char.isDelimiter()) {
      this.storeTypedChar(CharType.SPACE);
      this.setErrorState("Unexpected delimiter in key", 2);
      return;
    } else if (this.char.isKey()) {
      this.extendKey();
      this.storeTypedChar(CharType.KEY);
    } else if (this.char.isOp()) {
      this.extendKeyValueOperator();
      this.setState(State.KEY_VALUE_OPERATOR);
      this.storeTypedChar(CharType.OPERATOR);
    } else {
      this.setErrorState("Invalid character", 3);
      return;
    }
  }

  inStateKeyValueOperator() {
    if (!this.char) return;

    if (this.char.isDelimiter()) {
      this.storeTypedChar(CharType.SPACE);
      this.setErrorState("Unexpected delimiter in operator", 4);
    } else if (this.char.isOp()) {
      this.extendKeyValueOperator();
      this.storeTypedChar(CharType.OPERATOR);
    } else if (this.char.isValue()) {
      if (!VALID_KEY_VALUE_OPERATORS.includes(this.keyValueOperator)) {
        this.setErrorState(`Unknown operator: ${this.keyValueOperator}`, 10);
      } else {
        this.setState(State.VALUE);
        this.extendValue();
        this.storeTypedChar(CharType.VALUE);
      }
    } else if (this.char.isSingleQuote()) {
      if (!VALID_KEY_VALUE_OPERATORS.includes(this.keyValueOperator)) {
        this.setErrorState(`Unknown operator: ${this.keyValueOperator}`, 10);
      } else {
        this.setState(State.SINGLE_QUOTED_VALUE);
        this.storeTypedChar(CharType.VALUE);
      }
    } else if (this.char.isDoubleQuote()) {
      if (!VALID_KEY_VALUE_OPERATORS.includes(this.keyValueOperator)) {
        this.setErrorState(`Unknown operator: ${this.keyValueOperator}`, 10);
      } else {
        this.setState(State.DOUBLE_QUOTED_VALUE);
        this.storeTypedChar(CharType.VALUE);
      }
    } else {
      this.setErrorState("Invalid character", 4);
    }
  }

  inStateValue() {
    if (!this.char) return;

    if (this.char.isValue()) {
      this.extendValue();
      this.storeTypedChar(CharType.VALUE);
    } else if (this.char.isDelimiter()) {
      this.storeTypedChar(CharType.SPACE);
      this.setState(State.EXPECT_BOOL_OP);
      this.extendTree();
      this.resetData();
      this.resetBoolOperator();
    } else if (this.char.isGroupClose()) {
      if (!this.nodesStack.length) {
        this.setErrorState("Unmatched parenthesis", 9);
        return;
      } else {
        this.extendTree();
        this.resetData();
        if (this.boolOpStack.length) {
          this.boolOperator = this.boolOpStack.pop() || "and";
        }
        this.extendTreeFromStack(this.boolOperator);
        this.resetBoolOperator();
        this.setState(State.EXPECT_BOOL_OP);
        this.storeTypedChar(CharType.OPERATOR);
      }
    } else {
      this.setErrorState("Invalid character", 10);
      return;
    }
  }

  inStateSingleQuotedValue() {
    if (!this.char) return;
    this.storeTypedChar(CharType.VALUE);

    if (this.char.isBackslash()) {
      // Handle escape sequences
      const nextPos = this.char.pos + 1;
      if (nextPos < this.text.length) {
        const nextChar = this.text[nextPos];
        if (nextChar === "'" || nextChar === "\\") {
          // Skip this backslash, will add the escaped char in the next iteration
          return;
        }
      }
      this.extendValue();
    } else if (this.char.value !== "'") {
      this.extendValue();
    } else if (this.char.isSingleQuote()) {
      const prevPos = this.char.pos - 1;
      if (prevPos >= 0 && this.text[prevPos] === "\\") {
        // Escaped quote
        this.value += "'";
      } else {
        this.setState(State.EXPECT_BOOL_OP);
        this.extendTree();
        this.resetData();
        this.resetBoolOperator();
      }
    } else {
      this.setErrorState("Invalid character", 11);
      return;
    }
  }

  inStateDoubleQuotedValue() {
    if (!this.char) return;
    this.storeTypedChar(CharType.VALUE);

    if (this.char.isBackslash()) {
      // Handle escape sequences
      const nextPos = this.char.pos + 1;
      if (nextPos < this.text.length) {
        const nextChar = this.text[nextPos];
        if (nextChar === '"' || nextChar === "\\") {
          // Skip this backslash, will add the escaped char in the next iteration
          return;
        }
      }
      this.extendValue();
    } else if (this.char.value !== '"') {
      this.extendValue();
    } else if (this.char.isDoubleQuote()) {
      const prevPos = this.char.pos - 1;
      if (prevPos >= 0 && this.text[prevPos] === "\\") {
        // Escaped quote
        this.value += '"';
      } else {
        this.setState(State.EXPECT_BOOL_OP);
        this.extendTree();
        this.resetData();
        this.resetBoolOperator();
      }
    } else {
      this.setErrorState("Invalid character", 11);
      return;
    }
  }

  inStateBoolOpDelimiter() {
    if (!this.char) return;

    if (this.char.isDelimiter()) {
      this.storeTypedChar(CharType.SPACE);
      return;
    } else if (this.char.isKey()) {
      this.setState(State.KEY);
      this.extendKey();
      this.storeTypedChar(CharType.KEY);
    } else if (this.char.isGroupOpen()) {
      this.extendNodesStack();
      this.extendBoolOpStack();
      this.storeTypedChar(CharType.OPERATOR);
      this.setState(State.INITIAL);
    } else if (this.char.isGroupClose()) {
      this.storeTypedChar(CharType.OPERATOR);
      if (!this.nodesStack.length) {
        this.setErrorState("Unmatched parenthesis", 15);
        return;
      } else {
        this.resetData();
        this.extendTreeFromStack(this.boolOpStack.pop() || "and");
        this.setState(State.EXPECT_BOOL_OP);
      }
    } else {
      this.setErrorState("Invalid character", 18);
      return;
    }
  }

  inStateExpectBoolOp() {
    if (!this.char) return;

    if (this.char.isDelimiter()) {
      this.storeTypedChar(CharType.SPACE);
      return;
    } else if (this.char.isGroupClose()) {
      if (!this.nodesStack.length) {
        this.setErrorState("Unmatched parenthesis", 19);
        return;
      } else {
        if (this.key && this.value && this.keyValueOperator) {
          this.extendTree();
        }
        this.resetData();
        this.resetBoolOperator();
        this.extendTreeFromStack(this.boolOpStack.pop() || "and");
        this.setState(State.EXPECT_BOOL_OP);
        this.storeTypedChar(CharType.OPERATOR);
      }
    } else {
      this.extendBoolOperator();
      this.storeTypedChar(CharType.OPERATOR);

      // Check if we've accumulated a valid boolean operator
      if (this.boolOperator.length > 3 ||
          !['a', 'n', 'd', 'o', 'r'].includes(this.char.value)) {
        this.setErrorState("Invalid boolean operator", 20);
        return;
      }

      // If we have a complete boolean operator, look for a delimiter next
      if (VALID_BOOL_OPERATORS.includes(this.boolOperator)) {
        const nextPos = this.char.pos + 1;
        if (this.text.length > nextPos) {
          const nextChar = this.text[nextPos];
          if (nextChar !== " ") {
            this.setErrorState("Expected delimiter after boolean operator", 23);
            return;
          } else {
            this.setState(State.BOOL_OP_DELIMITER);
          }
        }
      }
    }
  }

  inStateLastChar() {
    if (this.state === State.INITIAL && !this.nodesStack.length) {
      this.setErrorState("Empty input", 24);
    } else if (this.state === State.INITIAL || this.state === State.KEY) {
      this.setErrorState("Unexpected EOF", 25);
    } else if (this.state === State.VALUE) {
      this.extendTree();
      this.resetBoolOperator();
    } else if (this.state === State.SINGLE_QUOTED_VALUE || this.state === State.DOUBLE_QUOTED_VALUE) {
      this.setErrorState("Unterminated string", 26);
      return;
    } else if (this.state === State.BOOL_OP_DELIMITER && this.state !== State.EXPECT_BOOL_OP) {
      this.setErrorState("Unexpected EOF", 27);
      return;
    }

    if (this.state !== State.ERROR && this.nodesStack.length) {
      this.setErrorState("Unmatched parenthesis", 28);
      return;
    }
  }

  generateMonacoTokens() {
    const tokens: Token[] = [];
    let token: Token | null = null;

    for (const [char, charType] of this.typedChars) {
      if (token == null) {
        token = new Token(char, charType);
      } else {
        if (token.type === charType) {
          token.addChar(char);
        } else {
          tokens.push(token);
          token = new Token(char, charType);
        }
      }
    }

    if (token !== null) {
      tokens.push(token);
    }

    // Convert value tokens to number or string types for better highlighting
    for (const token of tokens) {
      if (token.type === CharType.VALUE) {
        if (isNumeric(token.value)) {
          token.type = CharType.NUMBER;
        } else {
          token.type = CharType.STRING;
        }
      }
    }

    // Generate Monaco-compatible token data
    const data: number[] = [];
    const tokenModifier = 0;
    let prevToken: Token | null = null;

    for (const token of tokens) {
      let deltaLine = 0;
      let deltaStart = token.linePos;
      let tokenLength = token.length;
      let typeIndex = tokenTypes.indexOf(token.type);

      if (prevToken != null) {
        deltaLine = token.line - prevToken.line;
        deltaStart = deltaLine === 0 ? token.start - prevToken.start : token.linePos;
        prevToken = token;
      } else {
        prevToken = token;
      }

      data.push(deltaLine, deltaStart, tokenLength, typeIndex, tokenModifier);
    }

    return data;
  }

  parse(text: string, raiseError: boolean = true, ignoreLastChar: boolean = false) {
    this.setText(text);

    for (let i = 0; i < text.length; i++) {
      if (this.state === State.ERROR) {
        break;
      }

      this.setChar(new Char(text[i], i, this.line, this.linePos));

      if (this.char && this.char.isNewline()) {
        this.line += 1;
        this.linePos = 0;
        this.pos += 1;
        continue;
      }

      if (this.state === State.INITIAL) {
        this.inStateInitial();
      } else if (this.state === State.KEY) {
        this.inStateKey();
      } else if (this.state === State.VALUE) {
        this.inStateValue();
      } else if (this.state === State.SINGLE_QUOTED_VALUE) {
        this.inStateSingleQuotedValue();
      } else if (this.state === State.DOUBLE_QUOTED_VALUE) {
        this.inStateDoubleQuotedValue();
      } else if (this.state === State.KEY_VALUE_OPERATOR) {
        this.inStateKeyValueOperator();
      } else if (this.state === State.BOOL_OP_DELIMITER) {
        this.inStateBoolOpDelimiter();
      } else if (this.state === State.EXPECT_BOOL_OP) {
        this.inStateExpectBoolOp();
      } else {
        this.setErrorState(`Unknown state: ${this.state}`, 1);
      }

      this.pos += 1;
      this.linePos += 1;
    }

    if (this.state === State.ERROR) {
      if (raiseError) {
        throw new ParserError(this.errorText, this.errno);
      } else {
        return;
      }
    }

    if (!ignoreLastChar) {
      this.inStateLastChar();
    }

    if (this.state === State.ERROR) {
      if (raiseError) {
        throw new ParserError(this.errorText, this.errno);
      } else {
        return;
      }
    }

    this.root = this.currentNode;
  }

  // Generate SQL from the parsed query
  toSQL(): { sql: string; params: Array<string | number> } {
    if (!this.root) {
      return { sql: "", params: [] };
    }

    const result = this.root.toSQL();

    // Add WHERE keyword for complete SQL clause
    if (result.sql) {
      result.sql = `WHERE ${result.sql}`;
    }

    return result;
  }
}

function isNumeric(str) {
  if (typeof str != "string") return false
  return !isNaN(str) &&
      !isNaN(parseFloat(str))
}
