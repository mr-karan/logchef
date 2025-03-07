// Define constants for token types
export const CharType = Object.freeze({
  KEYWORD: "keyword",
  FUNCTION: "function",
  TYPE: "type",
  OPERATOR: "operator",
  STRING: "string",
  NUMBER: "number",
  DELIMITER: "delimiter",
  COMMENT: "comment",
  IDENTIFIER: "identifier",
  PUNCTUATION: "punctuation",
  VARIABLE: "variable",
  SPACE: "space",
});

// Token types for Monaco editor
export const tokenTypes = [
  CharType.KEYWORD,
  CharType.FUNCTION,
  CharType.TYPE,
  CharType.OPERATOR,
  CharType.STRING,
  CharType.NUMBER,
  CharType.DELIMITER,
  CharType.COMMENT,
  CharType.IDENTIFIER,
  CharType.PUNCTUATION,
  CharType.VARIABLE,
];

// Common SQL keywords for ClickHouse
export const SQL_KEYWORDS = [
  "SELECT", "FROM", "WHERE", "JOIN", "LEFT", "RIGHT", "INNER", "OUTER", "ON", "GROUP", "BY",
  "HAVING", "ORDER", "ASC", "DESC", "LIMIT", "OFFSET", "UNION", "ALL", "AND", "OR", "NOT",
  "CASE", "WHEN", "THEN", "ELSE", "END", "IS", "NULL", "AS", "DISTINCT", "BETWEEN", "IN",
  "INTERVAL", "WITH", "PREWHERE", "TOP", "SAMPLE", "USING"
];

// Common ClickHouse data types
export const SQL_TYPES = [
  "Int8", "Int16", "Int32", "Int64", "Int128", "Int256",
  "UInt8", "UInt16", "UInt32", "UInt64", "UInt128", "UInt256",
  "Float32", "Float64", "Decimal", "String", "FixedString",
  "UUID", "Date", "DateTime", "DateTime64", "IPv4", "IPv6", "Array", "Tuple", "Map", "Enum"
];

// Common ClickHouse functions for log analytics
export const CLICKHOUSE_FUNCTIONS = [
  // Aggregate functions
  "count", "sum", "avg", "min", "max", "any", "anyHeavy",
  "quantile", "median", "stddev", "variance", "covariance",
  "correlation", "uniq", "uniqExact", "uniqCombined",
  "groupArray", "groupArrayInsertAt", "groupUniqArray", "topK",
  "histogram", "countIf", "sumIf", "avgIf", "minIf", "maxIf",
  
  // Date/time functions
  "now", "today", "yesterday", "toStartOfHour", "toStartOfDay",
  "toStartOfWeek", "toStartOfMonth", "toStartOfQuarter", "toStartOfYear",
  "toDateTime", "toDateTime64", "toDate", "formatDateTime",
  "dateDiff", "toUnixTimestamp", "fromUnixTimestamp",
  "toYear", "toMonth", "toDayOfMonth", "toHour", "toMinute", "toSecond",
  "toWeek", "toISOWeek", "toISOYear", "toRelativeHourNum",
  "toRelativeDayNum", "toRelativeWeekNum", "toRelativeMonthNum",
  
  // String functions
  "position", "positionCaseInsensitive",
  "substring", "substringUTF8", "replaceOne", "replaceAll",
  "replaceRegexpOne", "replaceRegexpAll", "lower", "upper",
  "lowerUTF8", "upperUTF8", "reverse", "reverseUTF8",
  "match", "extract", "extractAll", "extractAllGroupsHorizontal",
  "toLowerCase", "toUpperCase", "trim", "trimLeft", "trimRight",
  "trimBoth", "concat", "empty", "notEmpty", "length", "lengthUTF8",
  
  // JSON functions
  "JSONHas", "JSONLength", "JSONType", "JSONExtractString",
  "JSONExtractInt", "JSONExtractUInt", "JSONExtractFloat",
  "JSONExtractBool", "JSONExtractRaw", "JSONExtractArrayRaw",
  "JSONExtract", "simpleJSONExtract", "visitParamHas", "visitParamExtractString",
  
  // Array functions
  "arrayJoin", "splitByChar", "splitByString", "arrayConcat",
  "arrayElement", "has", "indexOf", "countEqual", "arrayFilter",
  "arrayMap", "arrayFlatten", "arrayCompact", "arrayReverse",
  "arraySlice", "arrayDistinct", "arrayEnumerate", "arrayUniq",
  
  // Window functions
  "row_number", "rank", "dense_rank", "lag", "lead",
  
  // Conditional functions
  "if", "multiIf", "ifNull", "nullIf", "coalesce", "isNull", "isNotNull",
  "assumeNotNull", "greatest", "least",
  
  // Type conversion
  "toInt8", "toInt16", "toInt32", "toInt64", "toInt128", "toInt256",
  "toUInt8", "toUInt16", "toUInt32", "toUInt64", "toUInt128", "toUInt256",
  "toFloat32", "toFloat64", "toDecimal32", "toDecimal64", "toDecimal128",
  "toString", "toFixedString", "toDate32", "toUUID",
  
  // Formatting and display
  "formatReadableSize", "formatReadableTimeDelta", "formatRow",
  "formatBytes", "bitmaskToList", "formatDateTime"
];

// Operators
export const OPERATORS = [
  "+", "-", "*", "/", "%", "=", "<>", "!=", ">", "<", ">=", "<=", "LIKE", "NOT LIKE", "ILIKE"
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

  isWhitespace(): boolean {
    return /\s/.test(this.value);
  }

  isNewline(): boolean {
    return this.value === "\n";
  }

  isDigit(): boolean {
    return /[0-9]/.test(this.value);
  }

  isAlpha(): boolean {
    return /[a-zA-Z_]/.test(this.value);
  }

  isAlphaNumeric(): boolean {
    return /[a-zA-Z0-9_]/.test(this.value);
  }

  isOperator(): boolean {
    return /[+\-*/%=<>!]/.test(this.value);
  }

  isStringQuote(): boolean {
    return this.value === "'" || this.value === '"';
  }

  isPunctuation(): boolean {
    return /[,.;:()\[\]{}]/.test(this.value);
  }

  isCommentStart(): boolean {
    return this.value === "-" || this.value === "/";
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
    this.length = 1;
    this.type = charType;
    this.value = char.value;
    this.line = char.line;
    this.linePos = char.linePos;
  }

  addChar(char: Char): void {
    this.value += char.value;
    this.length += 1;
  }
}

// Parser state enum
export const State = Object.freeze({
  INITIAL: "Initial",
  IDENTIFIER: "Identifier",
  NUMBER: "Number",
  STRING: "String",
  OPERATOR: "Operator",
  COMMENT: "Comment",
  PUNCTUATION: "Punctuation",
  ERROR: "Error",
});

// Custom error class
export class ClickHouseSQLError extends Error {
  constructor(message: string) {
    super(message);
    this.name = "ClickHouseSQLError";
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

  setText(text: string): void {
    this.text = text;
  }

  setChar(char: Char): void {
    this.char = char;
  }

  setState(state: string): void {
    this.state = state;
  }

  setErrorState(errorText: string): void {
    this.state = State.ERROR;
    this.errorText = errorText;
    if (this.char) {
      this.errorText += ` [char ${this.char.value} at ${this.char.pos}]`;
    }
  }

  storeTypedChar(charType: string): void {
    if (this.char) {
      this.typedChars.push([this.char, charType]);
    }
  }

  // Parse initial state
  inStateInitial(): void {
    if (!this.char) return;

    if (this.char.isWhitespace()) {
      this.storeTypedChar(CharType.SPACE);
    } else if (this.char.isAlpha()) {
      this.setState(State.IDENTIFIER);
      this.storeTypedChar(CharType.IDENTIFIER);
    } else if (this.char.isDigit()) {
      this.setState(State.NUMBER);
      this.storeTypedChar(CharType.NUMBER);
    } else if (this.char.isStringQuote()) {
      this.setState(State.STRING);
      this.storeTypedChar(CharType.STRING);
    } else if (this.char.isOperator()) {
      this.setState(State.OPERATOR);
      this.storeTypedChar(CharType.OPERATOR);
    } else if (this.char.isPunctuation()) {
      this.setState(State.PUNCTUATION);
      this.storeTypedChar(CharType.PUNCTUATION);
    } else if (this.char.isCommentStart()) {
      const nextPos = this.char.pos + 1;
      if (this.text.length > nextPos) {
        const nextChar = this.text[nextPos];
        if ((this.char.value === '-' && nextChar === '-') ||
            (this.char.value === '/' && nextChar === '*')) {
          this.setState(State.COMMENT);
          this.storeTypedChar(CharType.COMMENT);
          return;
        }
      }
      
      if (this.char.value === '-') {
        this.setState(State.OPERATOR);
        this.storeTypedChar(CharType.OPERATOR);
      } else if (this.char.value === '/') {
        this.setState(State.OPERATOR);
        this.storeTypedChar(CharType.OPERATOR);
      }
    } else {
      this.setErrorState("Invalid character in initial state");
    }
  }

  // Parse identifier state (keywords, functions, types, etc.)
  inStateIdentifier(): void {
    if (!this.char) return;

    if (this.char.isAlphaNumeric() || this.char.value === '.') {
      this.storeTypedChar(CharType.IDENTIFIER);
    } else {
      // Check if this is a keyword, function, or type
      const identifier = this.typedChars
        .filter(([_, type]) => type === CharType.IDENTIFIER)
        .map(([char, _]) => char.value)
        .join('');

      // Update token type for the already stored characters
      if (SQL_KEYWORDS.includes(identifier.toUpperCase())) {
        for (let i = this.typedChars.length - identifier.length; i < this.typedChars.length; i++) {
          this.typedChars[i][1] = CharType.KEYWORD;
        }
      } else if (CLICKHOUSE_FUNCTIONS.includes(identifier.toLowerCase())) {
        for (let i = this.typedChars.length - identifier.length; i < this.typedChars.length; i++) {
          this.typedChars[i][1] = CharType.FUNCTION;
        }
      } else if (SQL_TYPES.includes(identifier)) {
        for (let i = this.typedChars.length - identifier.length; i < this.typedChars.length; i++) {
          this.typedChars[i][1] = CharType.TYPE;
        }
      }

      // Process the current character
      if (this.char.isWhitespace()) {
        this.setState(State.INITIAL);
        this.storeTypedChar(CharType.SPACE);
      } else if (this.char.isOperator()) {
        this.setState(State.OPERATOR);
        this.storeTypedChar(CharType.OPERATOR);
      } else if (this.char.isPunctuation()) {
        this.setState(State.PUNCTUATION);
        this.storeTypedChar(CharType.PUNCTUATION);
      } else {
        this.setErrorState("Invalid character in identifier state");
      }
    }
  }

  // Parse number state
  inStateNumber(): void {
    if (!this.char) return;

    if (this.char.isDigit() || this.char.value === '.' || 
        this.char.value.toLowerCase() === 'e' || 
        (this.char.value === '-' && this.typedChars[this.typedChars.length - 1][0].value.toLowerCase() === 'e')) {
      this.storeTypedChar(CharType.NUMBER);
    } else if (this.char.isWhitespace()) {
      this.setState(State.INITIAL);
      this.storeTypedChar(CharType.SPACE);
    } else if (this.char.isOperator()) {
      this.setState(State.OPERATOR);
      this.storeTypedChar(CharType.OPERATOR);
    } else if (this.char.isPunctuation()) {
      this.setState(State.PUNCTUATION);
      this.storeTypedChar(CharType.PUNCTUATION);
    } else {
      this.setErrorState("Invalid character in number state");
    }
  }

  // Parse string state
  inStateString(): void {
    if (!this.char) return;

    const quoteType = this.typedChars.find(([_, type]) => type === CharType.STRING)?.[0].value;
    
    if (this.char.value === quoteType && this.typedChars[this.typedChars.length - 1][0].value !== '\\') {
      this.storeTypedChar(CharType.STRING);
      this.setState(State.INITIAL);
    } else {
      this.storeTypedChar(CharType.STRING);
    }
  }

  // Parse operator state
  inStateOperator(): void {
    if (!this.char) return;

    if (this.char.isOperator()) {
      this.storeTypedChar(CharType.OPERATOR);
    } else {
      if (this.char.isWhitespace()) {
        this.setState(State.INITIAL);
        this.storeTypedChar(CharType.SPACE);
      } else if (this.char.isAlpha()) {
        this.setState(State.IDENTIFIER);
        this.storeTypedChar(CharType.IDENTIFIER);
      } else if (this.char.isDigit()) {
        this.setState(State.NUMBER);
        this.storeTypedChar(CharType.NUMBER);
      } else if (this.char.isStringQuote()) {
        this.setState(State.STRING);
        this.storeTypedChar(CharType.STRING);
      } else if (this.char.isPunctuation()) {
        this.setState(State.PUNCTUATION);
        this.storeTypedChar(CharType.PUNCTUATION);
      } else {
        this.setErrorState("Invalid character in operator state");
      }
    }
  }

  // Parse comment state
  inStateComment(): void {
    if (!this.char) return;

    // Check if it's a single-line or multi-line comment
    if (this.typedChars.length >= 2 && 
        this.typedChars[this.typedChars.length - 2][0].value === '-' && 
        this.typedChars[this.typedChars.length - 1][0].value === '-') {
      // Single-line comment
      if (this.char.isNewline()) {
        this.storeTypedChar(CharType.COMMENT);
        this.setState(State.INITIAL);
      } else {
        this.storeTypedChar(CharType.COMMENT);
      }
    } else if (this.typedChars.length >= 2 && 
               this.typedChars[this.typedChars.length - 2][0].value === '/' && 
               this.typedChars[this.typedChars.length - 1][0].value === '*') {
      // Multi-line comment
      this.storeTypedChar(CharType.COMMENT);
      if (this.char.value === '*') {
        const nextPos = this.char.pos + 1;
        if (this.text.length > nextPos && this.text[nextPos] === '/') {
          // End of multi-line comment coming up
        }
      } else if (this.typedChars.length >= 2 && 
                 this.typedChars[this.typedChars.length - 2][0].value === '*' && 
                 this.char.value === '/') {
        this.setState(State.INITIAL);
      }
    } else {
      this.storeTypedChar(CharType.COMMENT);
    }
  }

  // Parse punctuation state
  inStatePunctuation(): void {
    if (!this.char) return;

    this.setState(State.INITIAL);
    if (this.char.isWhitespace()) {
      this.storeTypedChar(CharType.SPACE);
    } else if (this.char.isAlpha()) {
      this.setState(State.IDENTIFIER);
      this.storeTypedChar(CharType.IDENTIFIER);
    } else if (this.char.isDigit()) {
      this.setState(State.NUMBER);
      this.storeTypedChar(CharType.NUMBER);
    } else if (this.char.isStringQuote()) {
      this.setState(State.STRING);
      this.storeTypedChar(CharType.STRING);
    } else if (this.char.isOperator()) {
      this.setState(State.OPERATOR);
      this.storeTypedChar(CharType.OPERATOR);
    } else if (this.char.isPunctuation()) {
      this.storeTypedChar(CharType.PUNCTUATION);
    } else {
      this.setErrorState("Invalid character in punctuation state");
    }
  }

  // Generate Monaco-compatible token data
  generateMonacoTokens(): number[] {
    const tokens: Token[] = [];
    let token: Token | null = null;

    for (const [char, charType] of this.typedChars) {
      if (token === null) {
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

    // Generate Monaco-compatible token data
    const data: number[] = [];
    let prevToken: Token | null = null;

    for (const token of tokens) {
      let deltaLine = 0;
      let deltaStart = token.linePos;
      let tokenLength = token.length;
      let typeIndex = tokenTypes.indexOf(token.type);

      if (typeIndex === -1) {
        // Skip spaces and other non-highlighted tokens
        continue;
      }

      if (prevToken !== null) {
        deltaLine = token.line - prevToken.line;
        deltaStart = deltaLine === 0 ? token.linePos - prevToken.linePos : token.linePos;
      }

      prevToken = token;
      
      // Format: [deltaLine, deltaStart, length, tokenType, tokenModifiers]
      data.push(deltaLine, deltaStart, tokenLength, typeIndex, 0);
    }

    return data;
  }

  // Parse the SQL text
  parse(text: string, raiseError: boolean = true): void {
    this.setText(text);
    this.pos = 0;
    this.line = 0;
    this.linePos = 0;
    this.typedChars = [];
    this.state = State.INITIAL;

    for (let i = 0; i < text.length; i++) {
      if (this.state === State.ERROR) {
        break;
      }

      const char = new Char(text[i], i, this.line, this.linePos);
      this.setChar(char);

      if (char.isNewline()) {
        this.line += 1;
        this.linePos = 0;
        // Also store the newline
        if (this.state === State.COMMENT) {
          this.storeTypedChar(CharType.COMMENT);
        } else {
          this.storeTypedChar(CharType.SPACE);
          this.setState(State.INITIAL);
        }
      } else {
        // Process based on current state
        switch (this.state) {
          case State.INITIAL:
            this.inStateInitial();
            break;
          case State.IDENTIFIER:
            this.inStateIdentifier();
            break;
          case State.NUMBER:
            this.inStateNumber();
            break;
          case State.STRING:
            this.inStateString();
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
          default:
            this.setErrorState(`Unknown state: ${this.state}`);
            break;
        }
      }

      this.pos += 1;
      if (!char.isNewline()) {
        this.linePos += 1;
      }
    }

    if (this.state === State.ERROR && raiseError) {
      throw new ClickHouseSQLError(this.errorText);
    }
  }
}

// Helper for checking numeric strings
export function isNumeric(value: string): boolean {
  return !isNaN(parseFloat(value)) && isFinite(Number(value));
}