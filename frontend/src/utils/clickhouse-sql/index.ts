// Re-export from language.ts
export { SQL_KEYWORDS, CLICKHOUSE_FUNCTIONS, SQL_TYPES, isNumeric } from './language';

// Re-export from api.ts
export { validateSQL, extractTableName, formatSQLQuery } from './api';

// Constants for the parser
const SPACE = " ";
const NEWLINE = "\n";
const COMMENT_START = "--";
const QUOTE = "'";
const DOUBLE_QUOTE = '"';
const KEYWORDS = [
  "SELECT",
  "FROM",
  "WHERE",
  "GROUP",
  "ORDER",
  "BY",
  "LIMIT",
  "HAVING",
  "JOIN",
  "LEFT",
  "RIGHT",
  "INNER",
  "OUTER",
  "FULL",
  "ON",
  "AS",
  "WITH",
  "UNION",
  "ALL",
  "DISTINCT",
  "AND",
  "OR",
  "NOT",
  "IN",
  "BETWEEN",
  "LIKE",
  "IS",
  "NULL",
  "TRUE",
  "FALSE",
];

export const CharType = {
  KEYWORD: "keyword",
  IDENTIFIER: "identifier",
  STRING: "string",
  NUMBER: "number",
  OPERATOR: "operator",
  COMMENT: "comment",
  SPACE: "space",
  PUNCTUATION: "punctuation",
};

export const tokenTypes = [
  CharType.KEYWORD,
  CharType.IDENTIFIER,
  CharType.STRING,
  CharType.NUMBER,
  CharType.OPERATOR,
  CharType.COMMENT,
  CharType.PUNCTUATION,
];

export class ClickHouseSQLError extends Error {
  constructor(message: string) {
    super(message);
    this.name = "ClickHouseSQLError";
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

  isSpace(): boolean {
    return this.value === SPACE || this.value === "\t";
  }

  isNewline(): boolean {
    return this.value === NEWLINE || this.value === "\r";
  }

  isWhitespace(): boolean {
    return this.isSpace() || this.isNewline();
  }

  isDigit(): boolean {
    return /[0-9]/.test(this.value);
  }

  isLetter(): boolean {
    return /[a-zA-Z]/.test(this.value);
  }

  isIdentifierStart(): boolean {
    return this.isLetter() || this.value === "_";
  }

  isIdentifierPart(): boolean {
    return (
      this.isLetter() ||
      this.isDigit() ||
      this.value === "_" ||
      this.value === "."
    );
  }

  isOperator(): boolean {
    return /[+\-*/<>=!&|^%]/.test(this.value);
  }

  isPunctuation(): boolean {
    return /[,;:()\[\]{}]/.test(this.value) && this.value !== "`";
  }

  isQuote(): boolean {
    return this.value === QUOTE;
  }

  isDoubleQuote(): boolean {
    return this.value === DOUBLE_QUOTE;
  }
  
  isBacktick(): boolean {
    return this.value === "`";
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

export enum State {
  INITIAL = "Initial",
  IDENTIFIER = "Identifier",
  KEYWORD = "Keyword",
  STRING = "String",
  QUOTED_IDENTIFIER = "QuotedIdentifier",
  BACKTICK_IDENTIFIER = "BacktickIdentifier", // For `identifier` style quotes
  NUMBER = "Number",
  OPERATOR = "Operator",
  COMMENT = "Comment",
  PUNCTUATION = "Punctuation",
  ERROR = "Error",
}

export class Parser {
  pos: number;
  line: number;
  linePos: number;
  text: string;
  state: State;
  char: Char | null;
  tokens: Token[];
  typedChars: [Char, string][];
  errorText: string;

  constructor() {
    this.pos = 0;
    this.line = 0;
    this.linePos = 0;
    this.text = "";
    this.state = State.INITIAL;
    this.char = null;
    this.tokens = [];
    this.typedChars = [];
    this.errorText = "";
  }

  setState(state: State) {
    this.state = state;
  }

  setText(text: string) {
    this.text = text;
  }

  setChar(char: Char) {
    this.char = char;
  }

  setErrorState(errorText: string) {
    this.state = State.ERROR;
    this.errorText = errorText;
    if (this.char) {
      this.errorText += ` [char ${this.char.value} at ${this.char.pos}]`;
    }
  }

  storeTypedChar(charType: string) {
    if (this.char) {
      this.typedChars.push([this.char, charType]);
    }
  }

  inStateInitial() {
    if (!this.char) return;

    if (this.char.isWhitespace()) {
      this.storeTypedChar(CharType.SPACE);
    } else if (this.char.isIdentifierStart()) {
      this.setState(State.IDENTIFIER);
      this.storeTypedChar(CharType.IDENTIFIER);
    } else if (this.char.isDigit()) {
      this.setState(State.NUMBER);
      this.storeTypedChar(CharType.NUMBER);
    } else if (this.char.isQuote()) {
      this.setState(State.STRING);
      this.storeTypedChar(CharType.STRING);
    } else if (this.char.isDoubleQuote()) {
      this.setState(State.QUOTED_IDENTIFIER);
      this.storeTypedChar(CharType.IDENTIFIER);
    } else if (this.char.isBacktick()) {
      this.setState(State.BACKTICK_IDENTIFIER);
      this.storeTypedChar(CharType.IDENTIFIER);
    } else if (this.char.isOperator()) {
      this.setState(State.OPERATOR);
      this.storeTypedChar(CharType.OPERATOR);

      // Check for comment start
      if (
        this.char.value === "-" &&
        this.pos + 1 < this.text.length &&
        this.text[this.pos + 1] === "-"
      ) {
        this.setState(State.COMMENT);
      }
    } else if (this.char.isPunctuation()) {
      this.setState(State.PUNCTUATION);
      this.storeTypedChar(CharType.PUNCTUATION);
    } else {
      this.setErrorState("Invalid character");
    }
  }

  inStateIdentifier() {
    if (!this.char) return;

    if (this.char.isIdentifierPart()) {
      this.storeTypedChar(CharType.IDENTIFIER);
    } else {
      // Check if the current token is a keyword
      const lastToken = this.getLastToken();
      if (lastToken && KEYWORDS.includes(lastToken.value.toUpperCase())) {
        lastToken.type = CharType.KEYWORD;
      }

      this.setState(State.INITIAL);
      this.inStateInitial(); // Process current char in INITIAL state
    }
  }

  inStateKeyword() {
    if (!this.char) return;

    if (this.char.isIdentifierPart()) {
      this.storeTypedChar(CharType.KEYWORD);
    } else {
      this.setState(State.INITIAL);
      this.inStateInitial(); // Process current char in INITIAL state
    }
  }

  inStateString() {
    if (!this.char) return;

    this.storeTypedChar(CharType.STRING);

    if (this.char.isQuote()) {
      // Check for escaped quote
      const prevPos = this.char.pos - 1;
      if (prevPos >= 0 && this.text[prevPos] === "\\") {
        // This is an escaped quote, stay in STRING state
      } else {
        this.setState(State.INITIAL);
      }
    }
  }

  inStateQuotedIdentifier() {
    if (!this.char) return;

    this.storeTypedChar(CharType.IDENTIFIER);

    if (this.char.isDoubleQuote()) {
      // Check for escaped quote
      const prevPos = this.char.pos - 1;
      if (prevPos >= 0 && this.text[prevPos] === "\\") {
        // This is an escaped quote, stay in QUOTED_IDENTIFIER state
      } else {
        this.setState(State.INITIAL);
      }
    }
  }

  inStateBacktickIdentifier() {
    if (!this.char) return;

    this.storeTypedChar(CharType.IDENTIFIER);

    if (this.char.isBacktick()) {
      // Check for escaped backtick
      const prevPos = this.char.pos - 1;
      if (prevPos >= 0 && this.text[prevPos] === "\\") {
        // This is an escaped backtick, stay in BACKTICK_IDENTIFIER state
      } else {
        this.setState(State.INITIAL);
      }
    }
  }

  inStateNumber() {
    if (!this.char) return;

    if (
      this.char.isDigit() ||
      this.char.value === "." ||
      this.char.value.toLowerCase() === "e"
    ) {
      this.storeTypedChar(CharType.NUMBER);
    } else {
      this.setState(State.INITIAL);
      this.inStateInitial(); // Process current char in INITIAL state
    }
  }

  inStateOperator() {
    if (!this.char) return;

    if (this.char.isOperator()) {
      this.storeTypedChar(CharType.OPERATOR);
    } else {
      this.setState(State.INITIAL);
      this.inStateInitial(); // Process current char in INITIAL state
    }
  }

  inStateComment() {
    if (!this.char) return;

    this.storeTypedChar(CharType.COMMENT);

    if (this.char.isNewline()) {
      this.setState(State.INITIAL);
    }
  }

  inStatePunctuation() {
    if (!this.char) return;

    this.setState(State.INITIAL);
    this.inStateInitial(); // Process current char in INITIAL state
  }

  getLastToken(): Token | null {
    if (this.typedChars.length === 0) return null;

    // Build tokens from typed chars
    const tokens: Token[] = [];
    let currentToken: Token | null = null;

    for (const [char, type] of this.typedChars) {
      if (!currentToken || currentToken.type !== type) {
        if (currentToken) {
          tokens.push(currentToken);
        }
        currentToken = new Token(char, type);
      } else {
        currentToken.addChar(char);
      }
    }

    if (currentToken) {
      tokens.push(currentToken);
    }

    return tokens.length > 0 ? tokens[tokens.length - 1] : null;
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

    // Check for keywords in identifiers
    for (const token of tokens) {
      if (
        token.type === CharType.IDENTIFIER &&
        KEYWORDS.includes(token.value.toUpperCase())
      ) {
        token.type = CharType.KEYWORD;
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

      if (typeIndex === -1) continue; // Skip token types not in the legend

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

  parse(text: string) {
    this.setText(text);
    this.pos = 0;
    this.line = 0;
    this.linePos = 0;
    this.state = State.INITIAL;
    this.typedChars = [];

    for (; this.pos < text.length; this.pos++) {
      const c = text[this.pos];
      this.setChar(new Char(c, this.pos, this.line, this.linePos));

      if (this.char.isNewline()) {
        this.line++;
        this.linePos = 0;
      } else {
        this.linePos++;
      }

      switch (this.state) {
        case State.INITIAL:
          this.inStateInitial();
          break;
        case State.IDENTIFIER:
          this.inStateIdentifier();
          break;
        case State.KEYWORD:
          this.inStateKeyword();
          break;
        case State.STRING:
          this.inStateString();
          break;
        case State.QUOTED_IDENTIFIER:
          this.inStateQuotedIdentifier();
          break;
        case State.BACKTICK_IDENTIFIER:
          this.inStateBacktickIdentifier();
          break;
        case State.NUMBER:
          this.inStateNumber();
          break;
        case State.OPERATOR:
          this.inStateOperator();
          break;
        case State.COMMENT:
          this.inStateComment();
          break;
        case State.PUNCTUATION:
          this.inStatePunctuation();
          break;
        case State.ERROR:
          throw new ClickHouseSQLError(this.errorText);
      }
    }

    return this.generateMonacoTokens();
  }
}
