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
  VALUE: "logchefqlValue", // Base type before number/string refinement
  OPERATOR: "logchefqlOperator",
  NUMBER: "number", // Final type for numeric values
  STRING: "string", // Final type for non-numeric values/quoted strings
  SPACE: "space",
  COMMENT: "comment", // Added for potential future use
  PUNCTUATION: "punctuation", // For brackets, quotes
});

// Updated tokenTypes for Monaco legend
export const tokenTypes = [
  CharType.KEY,
  CharType.OPERATOR,
  CharType.NUMBER,
  CharType.STRING,
  CharType.PUNCTUATION,
  // CharType.VALUE is intermediate, not directly used in legend
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
  EQUALS_REGEX: "~",  // Changed from "=~" to "~" for simplicity
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

  isKey() { return /^[a-zA-Z0-9_.:\/]$/.test(this.value); } // Slightly simpler regex

  isOp() { return /[=!<>\~]/.test(this.value); } // Includes ~, excludes +-*/ etc.

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
      this.storeTypedChar(CharType.PUNCTUATION); // Store '(' as punctuation
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
    // isValue() check within the state machine logic is more robust
    const char = this.char;
    if (!char) return; // Should not happen in loop

    if (char.isDelimiter()) {
      this.storeTypedChar(CharType.SPACE);
      this.setState(State.EXPECT_BOOL_OP);
      this.extendTree();
      this.resetData();
      this.resetBoolOperator();
    } else if (char.isGroupClose()) {
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
        this.storeTypedChar(CharType.PUNCTUATION); // Store ')' as punctuation
      }
    } else {
      this.setErrorState("invalid character", 10);
      return;
    }
  }

  inStateSingleQuotedValue() {
    const char = this.char;
    if (!char) return;

    if (char.isSingleQuote()) {
      // Check for escaped quote
      const prevPos = char.pos - 1;
      if (prevPos >= 0 && this.text[prevPos] === BACKSLASH) {
        // It's an escaped quote, part of the value
        this.extendValue();
        this.storeTypedChar(CharType.VALUE); // Still part of the string value
      } else {
        // It's the closing quote
        this.storeTypedChar(CharType.PUNCTUATION); // Store closing quote
        this.setState(State.EXPECT_BOOL_OP);
        this.extendTree();
        this.resetData();
        this.resetBoolOperator();
      }
    } else {
      // Any other character is part of the value
      this.extendValue();
      this.storeTypedChar(CharType.VALUE); // Store as VALUE
    }
  }

  inStateDoubleQuotedValue() {
    const char = this.char;
    if (!char) return;

    if (char.isDoubleQuote()) {
      const prevPos = char.pos - 1;
      if (prevPos >= 0 && this.text[prevPos] === BACKSLASH) {
        this.extendValue();
        this.storeTypedChar(CharType.VALUE);
      } else {
        this.storeTypedChar(CharType.PUNCTUATION); // Store closing quote
        this.setState(State.EXPECT_BOOL_OP);
        this.extendTree();
        this.resetData();
        this.resetBoolOperator();
      }
    } else {
        this.extendValue();
        this.storeTypedChar(CharType.VALUE);
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
    let currentToken: Token | null = null;

    for (const [char, charType] of this.typedChars) {
      // Handle specific punctuation/operators immediately
      if (charType === CharType.PUNCTUATION || charType === CharType.OPERATOR || charType === CharType.SPACE) {
        if (currentToken) {
          tokens.push(currentToken); // Push previous token
          currentToken = null;
        }
        tokens.push(new Token(char, charType)); // Push the punctuation/operator/space token
        continue;
      }

      // Group consecutive characters of the same base type (KEY or VALUE)
      if (currentToken === null) {
        currentToken = new Token(char, charType);
      } else {
        // Group KEYs together, and VALUEs together.
        // Other types were handled above.
        if (currentToken.type === charType && (charType === CharType.KEY || charType === CharType.VALUE)) {
          currentToken.addChar(char);
        } else {
          tokens.push(currentToken);
          currentToken = new Token(char, charType);
        }
      }
    }
    // Push the last token if it exists
    if (currentToken !== null) {
      tokens.push(currentToken);
    }

    // Refine token types (VALUE -> NUMBER or STRING) and filter out spaces
    const finalTokens = tokens
      .map(token => {
        if (token.type === CharType.VALUE) {
          // Use the stricter isNumeric check
          token.type = isNumeric(token.value) ? CharType.NUMBER : CharType.STRING;
        }
        return token;
      })
      .filter(token => token.type !== CharType.SPACE); // Exclude space tokens from semantic highlighting

    // --- Generate Monaco data array ---
    const data: number[] = [];
    let prevLine = 0;
    let prevChar = 0;

    for (const token of finalTokens) {
        const deltaLine = token.line - prevLine;
        const deltaStart = deltaLine === 0 ? token.linePos - prevChar : token.linePos;
        const typeIndex = tokenTypes.indexOf(token.type);

        // Monaco expects typeIndex >= 0. Handle unknown types gracefully.
        if (typeIndex === -1) {
             console.warn(`Unknown token type encountered: ${token.type}`);
             continue; // Skip this token if its type isn't in the legend
        }

        data.push(
            deltaLine,       // delta line
            deltaStart,      // delta start char
            token.length,    // length
            typeIndex,       // token type index
            0                // token modifier (0 for none)
        );

        prevLine = token.line;
        prevChar = token.linePos;
    }
    // console.log("Generated Monaco Tokens Data:", data); // Debugging
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

// Helper function (can be moved or kept here) - Stricter numeric check
export function isNumeric(value: string): boolean {
  if (typeof value !== 'string' || value.trim() === '') return false;
  // Allows integers and floats, handles negative sign
  return /^-?\d+(\.\d+)?$/.test(value.trim());
}
