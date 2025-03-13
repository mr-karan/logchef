// Constants for the parser
const DELIMITER = " ";
const DOT = ".";
const UNDERSCORE = "_";
const COLON = ":";
const SLASH = "/";
const BACKSLASH = "\\";
const BRACKET_OPEN = "(";
const BRACKET_CLOSE = ")";
const EQUAL_SIGN = "=";
const EXCL_MARK = "!";
const TILDE = "~";
const LOWER_THAN = "<";
const GREATER_THAN = ">";
const DOUBLE_QUOTE = '"';
const SINGLE_QUOTE = "'";
const NEWLINE = "\n";

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

  toRepresentation() {
    return this.toString();
  }
}

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

export const BoolOperator = Object.freeze({
  AND: "and",
  OR: "or",
});

export const Operator = Object.freeze({
  EQUALS: "=",
  NOT_EQUALS: "!=",
  EQUALS_REGEX: "=~",
  NOT_EQUALS_REGEX: "!~",
  GREATER_THAN: ">",
  LOWER_THAN: "<",
  GREATER_OR_EQUALS_THAN: ">=",
  LOWER_OR_EQUALS_THAN: "<=",
});

export const VALID_KEY_VALUE_OPERATORS = [
  Operator.EQUALS,
  Operator.NOT_EQUALS,
  Operator.EQUALS_REGEX,
  Operator.NOT_EQUALS_REGEX,
  Operator.GREATER_THAN,
  Operator.LOWER_THAN,
  Operator.GREATER_OR_EQUALS_THAN,
  Operator.LOWER_OR_EQUALS_THAN,
];

export const VALID_BOOL_OPERATORS = [BoolOperator.AND, BoolOperator.OR];

export const VALID_BOOL_OPERATORS_CHARS = ["a", "n", "d", "o", "r"];

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
}

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
      throw new LogchefQLError(
        "either (left or right) or expression at same time"
      );
    }

    this.boolOperator = boolOperator;
    this.expression = expression;
    this.left = left;
    this.right = right;
  }

  setBoolOperator(boolOperator: string) {
    if (!VALID_BOOL_OPERATORS.includes(boolOperator as any)) {
      throw new LogchefQLError(`invalid bool operator: ${boolOperator}`);
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
}

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
      this.value === DOT ||
      this.value === COLON ||
      this.value === SLASH
    );
  }

  isOp() {
    return (
      this.value === EQUAL_SIGN ||
      this.value === EXCL_MARK ||
      this.value === TILDE ||
      this.value === LOWER_THAN ||
      this.value === GREATER_THAN
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

  isDoubleQuotedValue() {
    return !this.isDoubleQuote();
  }

  isSingleQuote() {
    return this.value === SINGLE_QUOTE;
  }

  isSingleQuotedValue() {
    return !this.isSingleQuote();
  }

  isBackslash() {
    return this.value === BACKSLASH;
  }

  isEquals() {
    return this.value === EQUAL_SIGN;
  }

  isValue() {
    return (
      !this.isDoubleQuote() &&
      !this.isSingleQuote() &&
      !this.isDelimiter() &&
      !this.isGroupOpen() &&
      !this.isGroupClose() &&
      !this.isEquals()
    );
  }

  isNewline() {
    return this.value === NEWLINE;
  }
}

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
      const node = this.newNode(
        this.boolOperator,
        null,
        this.currentNode,
        right
      );
      this.setCurrentNode(node);
    }
  }

  extendTreeFromStack(boolOperator: string) {
    const node = this.nodesStack.pop();
    if (node && node.right === null) {
      node.right = this.currentNode;
      node.setBoolOperator(boolOperator);
      this.setCurrentNode(node);
    } else if (node) {
      const newNode = this.newNode(boolOperator, null, node, this.currentNode);
      this.setCurrentNode(newNode);
    }
  }

  inStateInitial() {
    this.resetData();
    this.setCurrentNode(this.newNode(this.boolOperator, null, null, null));

    if (this.char?.isGroupOpen()) {
      this.extendNodesStack();
      this.extendBoolOpStack();
      this.setState(State.INITIAL);
      this.storeTypedChar(CharType.OPERATOR);
    } else if (this.char?.isDelimiter()) {
      this.storeTypedChar(CharType.SPACE);
      this.setState(State.BOOL_OP_DELIMITER);
    } else if (this.char?.isKey()) {
      this.extendKey();
      this.setState(State.KEY);
      this.storeTypedChar(CharType.KEY);
    } else {
      this.setErrorState("invalid character", 1);
      return;
    }
  }

  inStateKey() {
    if (this.char?.isDelimiter()) {
      this.storeTypedChar(CharType.SPACE);
      this.setErrorState("unexpected delimiter in key", 2);
      return;
    } else if (this.char?.isKey()) {
      this.extendKey();
      this.storeTypedChar(CharType.KEY);
    } else if (this.char?.isOp()) {
      this.extendKeyValueOperator();
      this.setState(State.KEY_VALUE_OPERATOR);
      this.storeTypedChar(CharType.OPERATOR);
    } else {
      this.setErrorState("invalid character", 3);
      return;
    }
  }

  inStateKeyValueOperator() {
    if (this.char?.isDelimiter()) {
      this.storeTypedChar(CharType.SPACE);
      this.setErrorState("unexpected delimiter in operator", 4);
    } else if (this.char?.isOp()) {
      this.extendKeyValueOperator();
      this.storeTypedChar(CharType.OPERATOR);
    } else if (this.char?.isValue()) {
      if (!VALID_KEY_VALUE_OPERATORS.includes(this.keyValueOperator)) {
        this.setErrorState(`unknown operator: ${this.keyValueOperator}`, 10);
      } else {
        this.setState(State.VALUE);
        this.extendValue();
        this.storeTypedChar(CharType.VALUE);
      }
    } else if (this.char?.isSingleQuote()) {
      if (!VALID_KEY_VALUE_OPERATORS.includes(this.keyValueOperator)) {
        this.setErrorState(`unknown operator: ${this.keyValueOperator}`, 10);
      } else {
        this.setState(State.SINGLE_QUOTED_VALUE);
        this.storeTypedChar(CharType.VALUE);
      }
    } else if (this.char?.isDoubleQuote()) {
      if (!VALID_KEY_VALUE_OPERATORS.includes(this.keyValueOperator)) {
        this.setErrorState(`unknown operator: ${this.keyValueOperator}`, 10);
      } else {
        this.setState(State.DOUBLE_QUOTED_VALUE);
        this.storeTypedChar(CharType.VALUE);
      }
    } else {
      this.setErrorState("invalid character", 4);
    }
  }

  inStateValue() {
    if (this.char?.isValue()) {
      this.extendValue();
      this.storeTypedChar(CharType.VALUE);
    } else if (this.char?.isDelimiter()) {
      this.storeTypedChar(CharType.SPACE);
      this.setState(State.EXPECT_BOOL_OP);
      this.extendTree();
      this.resetData();
      this.resetBoolOperator();
    } else if (this.char?.isGroupClose()) {
      if (!this.nodesStack.length) {
        this.setErrorState("unmatched parenthesis", 9);
        return;
      } else {
        this.extendTree();
        this.resetData();
        if (this.boolOpStack.length) {
          this.boolOperator = this.boolOpStack.pop() || "";
        }
        this.extendTreeFromStack(this.boolOperator);
        this.resetBoolOperator();
        this.setState(State.EXPECT_BOOL_OP);
        this.storeTypedChar(CharType.OPERATOR);
      }
    } else {
      this.setErrorState("invalid character", 10);
      return;
    }
  }

  inStateSingleQuotedValue() {
    this.storeTypedChar(CharType.VALUE);
    if (this.char?.isSingleQuotedValue()) {
      this.extendValue();
    } else if (this.char?.isSingleQuote()) {
      const prevPos = this.char.pos - 1;
      if (this.text[prevPos] === "\\") {
        this.extendValue();
      } else {
        this.setState(State.EXPECT_BOOL_OP);
        this.extendTree();
        this.resetData();
        this.resetBoolOperator();
      }
    } else {
      this.setErrorState("invalid character", 11);
      return;
    }
  }

  inStateDoubleQuotedValue() {
    this.storeTypedChar(CharType.VALUE);
    if (this.char?.isDoubleQuotedValue()) {
      this.extendValue();
    } else if (this.char?.isDoubleQuote()) {
      const prevPos = this.char.pos - 1;
      if (this.text[prevPos] === "\\") {
        this.extendValue();
      } else {
        this.setState(State.EXPECT_BOOL_OP);
        this.extendTree();
        this.resetData();
        this.resetBoolOperator();
      }
    } else {
      this.setErrorState("invalid character", 11);
      return;
    }
  }

  inStateBoolOpDelimiter() {
    if (this.char?.isDelimiter()) {
      this.storeTypedChar(CharType.SPACE);
      return;
    } else if (this.char?.isKey()) {
      this.setState(State.KEY);
      this.extendKey();
      this.storeTypedChar(CharType.KEY);
    } else if (this.char?.isGroupOpen()) {
      this.extendNodesStack();
      this.extendBoolOpStack();
      this.storeTypedChar(CharType.OPERATOR);
      this.setState(State.INITIAL);
    } else if (this.char?.isGroupClose()) {
      this.storeTypedChar(CharType.OPERATOR);
      if (!this.nodesStack.length) {
        this.setErrorState("unmatched parenthesis", 15);
        return;
      } else {
        this.resetData();
        const boolOp = this.boolOpStack.pop() || "";
        this.extendTreeFromStack(boolOp);
        this.setState(State.EXPECT_BOOL_OP);
      }
    } else {
      this.setErrorState("invalid character", 18);
      return;
    }
  }

  inStateExpectBoolOp() {
    if (this.char?.isDelimiter()) {
      this.storeTypedChar(CharType.SPACE);
      return;
    } else if (this.char?.isGroupClose()) {
      if (!this.nodesStack.length) {
        this.setErrorState("unmatched parenthesis", 19);
        return;
      } else {
        if (this.key && this.value && this.keyValueOperator) {
          this.extendTree();
        }
        this.resetData();
        this.resetBoolOperator();
        const boolOp = this.boolOpStack.pop() || "";
        this.extendTreeFromStack(boolOp);
        this.setState(State.EXPECT_BOOL_OP);
        this.storeTypedChar(CharType.OPERATOR);
      }
    } else {
      this.extendBoolOperator();
      this.storeTypedChar(CharType.OPERATOR);
      if (
        this.boolOperator.length > 3 ||
        !VALID_BOOL_OPERATORS_CHARS.includes(this.char.value)
      ) {
        this.setErrorState("invalid character", 20);
      } else {
        if (VALID_BOOL_OPERATORS.includes(this.boolOperator)) {
          const nextPos = this.char.pos + 1;
          if (this.text.length > nextPos) {
            const nextChar = new Char(this.text[nextPos], nextPos, 0, 0);
            if (!nextChar.isDelimiter()) {
              this.setErrorState("expected delimiter after bool operator", 23);
              return;
            } else {
              this.setState(State.BOOL_OP_DELIMITER);
            }
          }
        }
      }
    }
  }

  inStateLastChar() {
    if (this.state === State.INITIAL && !this.nodesStack.length) {
      this.setErrorState("empty input", 24);
    } else if (this.state === State.INITIAL || this.state === State.KEY) {
      this.setErrorState("unexpected EOF", 25);
    } else if (this.state === State.VALUE) {
      this.extendTree();
      this.resetBoolOperator();
    } else if (
      this.state === State.BOOL_OP_DELIMITER &&
      this.state !== State.EXPECT_BOOL_OP
    ) {
      this.setErrorState("unexpected EOF", 26);
      return;
    }

    if (this.state !== State.ERROR && this.nodesStack.length) {
      this.setErrorState("unmatched parenthesis", 27);
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
        if (token.type == charType) {
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
    for (const token of tokens) {
      if (token.type === CharType.VALUE) {
        // Check if numeric
        if (!isNaN(Number(token.value))) {
          token.type = CharType.NUMBER;
        } else {
          token.type = CharType.STRING;
        }
      }
    }
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
        deltaStart =
          deltaLine === 0 ? token.start - prevToken.start : token.linePos;
        prevToken = token;
      } else {
        prevToken = token;
      }
      data.push(deltaLine, deltaStart, tokenLength, typeIndex, tokenModifier);
    }
    return data;
  }

  parse(text: string, raiseError?: boolean, ignoreLastChar?: boolean) {
    this.setText(text);
    for (let i = 0; i < text.length; i++) {
      if (this.state === State.ERROR) {
        break;
      }
      const c = text[i];
      this.setChar(new Char(c, this.pos, this.line, this.linePos));
      if (this.char.isNewline()) {
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
}

// Helper function to check if a value is numeric
export function isNumeric(value: string): boolean {
  return !isNaN(parseFloat(value)) && isFinite(Number(value));
}
