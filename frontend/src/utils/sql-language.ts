import * as monaco from "monaco-editor";

/**
 * Token types for the SQL language
 */
export enum TokenType {
  Keyword = "keyword",
  Function = "function",
  Table = "table",
  Column = "column",
  Type = "type",
  String = "string",
  Number = "number",
  Operator = "operator",
  Comment = "comment",
  Variable = "variable",
  Delimiter = "delimiter",
  Identifier = "identifier",
}

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
  "count()", "sum()", "avg()", "min()", "max()", "any()", "anyHeavy()",
  "quantile()", "median()", "stddev()", "variance()", "covariance()", 
  "correlation()", "uniq()", "uniqExact()", "uniqCombined()", 
  "groupArray()", "groupArrayInsertAt()", "groupUniqArray()", "topK()", 
  "histogram()", "countIf()", "sumIf()", "avgIf()", "minIf()", "maxIf()",
  
  // Date/time functions
  "now()", "today()", "yesterday()", "toStartOfHour()", "toStartOfDay()", 
  "toStartOfWeek()", "toStartOfMonth()", "toStartOfQuarter()", "toStartOfYear()",
  "toDateTime()", "toDateTime64()", "toDate()", "formatDateTime()", 
  "dateDiff()", "toUnixTimestamp()", "fromUnixTimestamp()", 
  "toYear()", "toMonth()", "toDayOfMonth()", "toHour()", "toMinute()", "toSecond()",
  "toWeek()", "toISOWeek()", "toISOYear()", "toRelativeHourNum()",
  "toRelativeDayNum()", "toRelativeWeekNum()", "toRelativeMonthNum()",
  
  // String functions
  "like", "notLike", "ilike", "position()", "positionCaseInsensitive()",
  "substring()", "substringUTF8()", "replaceOne()", "replaceAll()", 
  "replaceRegexpOne()", "replaceRegexpAll()", "lower()", "upper()",
  "lowerUTF8()", "upperUTF8()", "reverse()", "reverseUTF8()",
  "match()", "extract()", "extractAll()", "extractAllGroupsHorizontal()",
  "toLowerCase()", "toUpperCase()", "trim()", "trimLeft()", "trimRight()",
  "trimBoth()", "concat()", "empty()", "notEmpty()", "length()", "lengthUTF8()",
  
  // JSON functions
  "JSONHas()", "JSONLength()", "JSONType()", "JSONExtractString()", 
  "JSONExtractInt()", "JSONExtractUInt()", "JSONExtractFloat()", 
  "JSONExtractBool()", "JSONExtractRaw()", "JSONExtractArrayRaw()",
  "JSONExtract()", "simpleJSONExtract()", "visitParamHas()", "visitParamExtractString()",
  
  // Array functions
  "arrayJoin()", "splitByChar()", "splitByString()", "arrayConcat()",
  "arrayElement()", "has()", "indexOf()", "countEqual()", "arrayFilter()",
  "arrayMap()", "arrayFlatten()", "arrayCompact()", "arrayReverse()",
  "arraySlice()", "arrayDistinct()", "arrayEnumerate()", "arrayUniq()",
  
  // Window functions
  "row_number()", "rank()", "dense_rank()", "lag()", "lead()",
  
  // Conditional functions
  "if()", "multiIf()", "ifNull()", "nullIf()", "coalesce()", "isNull()", "isNotNull()",
  "assumeNotNull()", "greatest()", "least()",
  
  // Type conversion
  "toInt8()", "toInt16()", "toInt32()", "toInt64()", "toInt128()", "toInt256()",
  "toUInt8()", "toUInt16()", "toUInt32()", "toUInt64()", "toUInt128()", "toUInt256()",
  "toFloat32()", "toFloat64()", "toDecimal32()", "toDecimal64()", "toDecimal128()",
  "toString()", "toFixedString()", "toDate32()", "toUUID()",
  
  // Formatting and display
  "formatReadableSize()", "formatReadableTimeDelta()", "formatRow()",
  "formatBytes()", "bitmaskToList()", "formatDateTime()"
];

// Track if language has been registered with a safe global flag
let isLanguageRegistered = false;

/**
 * Register the SQL language with Monaco
 */
export function registerSqlLanguage() {
  // Prevent duplicate registration using both our flag and a check if language exists
  if (isLanguageRegistered) {
    console.log("SQL language already registered, skipping");
    return;
  }
  
  // Extra safety check - see if language is already registered
  try {
    const languages = monaco.languages.getLanguages();
    if (languages.some(lang => lang.id === "clickhouse-sql")) {
      console.log("SQL language found in Monaco registry, skipping registration");
      isLanguageRegistered = true;
      return;
    }
  } catch (error) {
    console.warn("Error checking existing languages:", error);
  }
  
  isLanguageRegistered = true;

  // Register language if not already registered by Monaco
  // Monaco already has SQL language built-in, but we want to enhance it with ClickHouse specifics
  monaco.languages.register({ id: "clickhouse-sql" });

  // Define syntax highlighting for ClickHouse SQL
  monaco.languages.setMonarchTokensProvider("clickhouse-sql", {
    defaultToken: "",
    ignoreCase: true, // SQL is case-insensitive
    tokenizer: {
      root: [
        // Comments
        [/--.*$/, TokenType.Comment],
        [/\/\*/, { token: TokenType.Comment, next: '@comment' }],

        // Numbers
        [/\d*\.\d+([eE][-+]?\d+)?/, TokenType.Number],
        [/\d+/, TokenType.Number],

        // Strings
        [/'/, { token: TokenType.String, next: '@string_single' }],
        [/"/, { token: TokenType.String, next: '@string_double' }],
        [/`/, { token: TokenType.Identifier, next: '@identifier_backtick' }],

        // Keywords
        [/\b(SELECT|FROM|WHERE|JOIN|GROUP BY|ORDER BY|HAVING|LIMIT|OFFSET|UNION|ALL|DISTINCT|CASE|WHEN|THEN|ELSE|END|IS NULL|IS NOT NULL|AS|ON|AND|OR|NOT|USING|WITH)\b/i, TokenType.Keyword],
        
        // ClickHouse specific keywords
        [/\b(FINAL|SAMPLE|ARRAY JOIN|LEFT ARRAY JOIN|PREWHERE|SETTINGS|FORMAT|AST|DESCRIBE|OPTIMIZE|TTL|LIVE|WATCH|STAGE|VOLATILE|MATERIALIZED)\b/i, TokenType.Keyword],
        
        // Types
        [new RegExp('\\b(' + SQL_TYPES.join('|') + ')\\b', 'i'), TokenType.Type],
        
        // Functions - handle both with and without parentheses
        [/\b[a-zA-Z_][a-zA-Z0-9_]*(?=\s*\()/i, TokenType.Function],
        
        // Table names - detecting with context
        [/\b([a-zA-Z_][a-zA-Z0-9_]*\.){1,2}[a-zA-Z_][a-zA-Z0-9_]*/i, TokenType.Table],
        
        // Variables (parameters)
        [/\${[^}]*}/, TokenType.Variable],
        
        // Operators
        [/[+\-*/%<>=!&|^~]/, TokenType.Operator],
        
        // Delimiters
        [/[;,.]/, TokenType.Delimiter],
        [/[()]/, TokenType.Delimiter]
      ],
      
      comment: [
        [/[^/*]+/, TokenType.Comment],
        [/\/\*/, TokenType.Comment, '@push'],
        [/\*\//, TokenType.Comment, '@pop'],
        [/[/*]/, TokenType.Comment]
      ],
      
      string_single: [
        [/[^']+/, TokenType.String],
        [/''/, TokenType.String],
        [/'/, { token: TokenType.String, next: '@pop' }]
      ],
      
      string_double: [
        [/[^"]+/, TokenType.String],
        [/""/, TokenType.String],
        [/"/, { token: TokenType.String, next: '@pop' }]
      ],
      
      identifier_backtick: [
        [/[^`]+/, TokenType.Identifier],
        [/``/, TokenType.Identifier],
        [/`/, { token: TokenType.Identifier, next: '@pop' }]
      ]
    }
  });

  // Define language configuration
  monaco.languages.setLanguageConfiguration("clickhouse-sql", {
    autoClosingPairs: [
      { open: "(", close: ")" },
      { open: '"', close: '"' },
      { open: "'", close: "'" },
      { open: "`", close: "`" }
    ],
    brackets: [
      ["(", ")"]
    ],
    comments: {
      lineComment: "--",
      blockComment: ["/*", "*/"]
    },
    wordPattern: /(-?\d*\.\d\w*)|([^\`\~\!\@\#\%\^\&\*\(\)\-\=\+\[\{\]\}\\\|\;\:\'\"\,\.\<\>\/\?\s]+)/g
  });
}

// Keep track of the current completion provider
let currentCompletionProvider: monaco.IDisposable | null = null;

/**
 * Register completion provider for SQL language with ClickHouse specifics
 * @param tableColumns Array of column names and types available for autocompletion
 * @param tables Array of table names available for autocompletion
 */
export function registerSqlCompletionProvider(
  tableColumns: Array<{ name: string; type: string }> = [],
  tables: Array<{ name: string; database?: string }> = []
) {
  // Clean up previous provider
  if (currentCompletionProvider) {
    currentCompletionProvider.dispose();
    currentCompletionProvider = null;
  }

  // Create completion provider
  currentCompletionProvider = monaco.languages.registerCompletionItemProvider(
    "clickhouse-sql",
    {
      // Trigger characters for showing completions
      triggerCharacters: [" ", ".", "(", ","],

      provideCompletionItems: (model, position) => {
        try {
          // Get current line content
          const lineContent = model.getLineContent(position.lineNumber);
          const beforeCursor = lineContent.substring(0, position.column - 1);
          const lastChar = beforeCursor.charAt(beforeCursor.length - 1);
          
          // Get current word
          const wordInfo = model.getWordUntilPosition(position);
          const wordRange = {
            startLineNumber: position.lineNumber,
            endLineNumber: position.lineNumber,
            startColumn: wordInfo.startColumn,
            endColumn: wordInfo.endColumn,
          };
          
          const suggestions: monaco.languages.CompletionItem[] = [];

          // After dot, suggest columns of the table before the dot
          if (lastChar === '.') {
            const tableNameMatch = beforeCursor.match(/(\w+)\.\s*$/);
            if (tableNameMatch && tableNameMatch[1]) {
              const tableName = tableNameMatch[1];
              // Add relevant columns for this table
              return {
                suggestions: tableColumns.map(column => ({
                  label: column.name,
                  kind: monaco.languages.CompletionItemKind.Field,
                  insertText: column.name,
                  range: wordRange,
                  detail: column.type,
                  documentation: {
                    value: `**Column:** ${column.name}\n\n**Type:** ${column.type}`,
                    isTrusted: true
                  }
                }))
              };
            }
          }

          // At the beginning of a line or after certain keywords, suggest SELECT
          if (
            beforeCursor.trim() === "" ||
            /\b(FROM|WHERE|AND|OR|,)\s*$/.test(beforeCursor)
          ) {
            // Add keywords
            SQL_KEYWORDS.forEach(keyword => {
              suggestions.push({
                label: keyword,
                kind: monaco.languages.CompletionItemKind.Keyword,
                insertText: keyword,
                range: wordRange,
                sortText: `0-${keyword}`, // Make keywords appear first
              });
            });
          }

          // After FROM, suggest tables
          if (/\bFROM\s+$/.test(beforeCursor.toUpperCase())) {
            tables.forEach(table => {
              const fullName = table.database ? `${table.database}.${table.name}` : table.name;
              suggestions.push({
                label: fullName,
                kind: monaco.languages.CompletionItemKind.Class,
                insertText: fullName,
                range: wordRange,
                sortText: `1-${fullName}`,
                documentation: {
                  value: `**Table:** ${fullName}`,
                  isTrusted: true
                }
              });
            });
          }

          // After certain keywords or at the beginning of expressions, suggest functions
          if (
            /\b(SELECT|WHERE|HAVING|,|AND|OR|=|>|<|>=|<=|!=|\()\s*$/.test(beforeCursor.toUpperCase()) ||
            beforeCursor.trim() === ""
          ) {
            CLICKHOUSE_FUNCTIONS.forEach(func => {
              const hasParams = func.includes('()');
              const insertText = hasParams 
                ? func.replace('()', '($1)') 
                : `${func}($1)`;
              
              suggestions.push({
                label: func,
                kind: monaco.languages.CompletionItemKind.Function,
                insertText: hasParams 
                  ? insertText 
                  : func,
                insertTextRules: hasParams 
                  ? monaco.languages.CompletionItemInsertTextRule.InsertAsSnippet 
                  : undefined,
                range: wordRange,
                sortText: `2-${func}`,
                documentation: {
                  value: `**Function:** ${func}`,
                  isTrusted: true
                }
              });
            });
          }
          
          // After WHERE or AND/OR, suggest columns for filter conditions
          if (/\b(WHERE|AND|OR)\s+$/.test(beforeCursor.toUpperCase())) {
            tableColumns.forEach(column => {
              suggestions.push({
                label: column.name,
                kind: monaco.languages.CompletionItemKind.Field,
                insertText: column.name,
                range: wordRange,
                sortText: `1-${column.name}`, // Higher priority for WHERE clauses
                detail: column.type,
                documentation: {
                  value: `**Column:** ${column.name}\n\n**Type:** ${column.type}`,
                  isTrusted: true
                }
              });
            });
          }

          // After SELECT, suggest columns and functions
          if (/\bSELECT\s+$/.test(beforeCursor.toUpperCase())) {
            tableColumns.forEach(column => {
              suggestions.push({
                label: column.name,
                kind: monaco.languages.CompletionItemKind.Field,
                insertText: column.name,
                range: wordRange,
                sortText: `3-${column.name}`,
                detail: column.type,
                documentation: {
                  value: `**Column:** ${column.name}\n\n**Type:** ${column.type}`,
                  isTrusted: true
                }
              });
            });
          }

          // Handle partial word typing
          const currentWord = wordInfo.word.toLowerCase();
          if (currentWord.length > 0) {
            // Filter all suggestions to include only those that match current word
            const matchingKeywords = SQL_KEYWORDS.filter(keyword => 
              keyword.toLowerCase().includes(currentWord));
            
            const matchingFunctions = CLICKHOUSE_FUNCTIONS.filter(func => 
              func.toLowerCase().includes(currentWord));
            
            const matchingColumns = tableColumns.filter(column => 
              column.name.toLowerCase().includes(currentWord));

            const matchingTables = tables.filter(table => 
              table.name.toLowerCase().includes(currentWord) || 
              (table.database && table.database.toLowerCase().includes(currentWord)));

            // Add matching keywords
            matchingKeywords.forEach(keyword => {
              suggestions.push({
                label: keyword,
                kind: monaco.languages.CompletionItemKind.Keyword,
                insertText: keyword,
                range: wordRange,
                sortText: `0-${keyword}`,
              });
            });

            // Add matching functions
            matchingFunctions.forEach(func => {
              const hasParams = func.includes('()');
              const insertText = hasParams 
                ? func.replace('()', '($1)') 
                : `${func}($1)`;
              
              suggestions.push({
                label: func,
                kind: monaco.languages.CompletionItemKind.Function,
                insertText: hasParams 
                  ? insertText 
                  : func,
                insertTextRules: hasParams 
                  ? monaco.languages.CompletionItemInsertTextRule.InsertAsSnippet 
                  : undefined,
                range: wordRange,
                sortText: `2-${func}`,
              });
            });

            // Add matching columns
            matchingColumns.forEach(column => {
              suggestions.push({
                label: column.name,
                kind: monaco.languages.CompletionItemKind.Field,
                insertText: column.name,
                range: wordRange,
                sortText: `3-${column.name}`,
                detail: column.type,
              });
            });

            // Add matching tables
            matchingTables.forEach(table => {
              const fullName = table.database ? `${table.database}.${table.name}` : table.name;
              suggestions.push({
                label: fullName,
                kind: monaco.languages.CompletionItemKind.Class,
                insertText: fullName,
                range: wordRange,
                sortText: `1-${fullName}`,
              });
            });
          }

          return { suggestions };
        } catch (error) {
          console.error("Error providing SQL completions:", error);
          return { suggestions: [] };
        }
      },
    }
  );

  return currentCompletionProvider;
}

/**
 * Show suggestions in the SQL editor
 * @param editor The Monaco editor instance
 */
export function showSqlSuggestions(editor: monaco.editor.IStandaloneCodeEditor) {
  if (!editor) return;

  try {
    // Focus the editor first
    editor.focus();
    
    // Trigger suggestions
    editor.trigger('keyboard', 'editor.action.triggerSuggest', {});
  } catch (error) {
    console.error("Error showing SQL suggestions:", error);
  }
}

/**
 * Get multi-line Monaco Editor options optimized for SQL
 */
export function getSqlMonacoOptions() {
  return {
    fontSize: 13,
    minimap: { enabled: false },
    lineNumbers: "on",
    glyphMargin: false,
    folding: true,
    lineDecorationsWidth: 0,
    lineNumbersMinChars: 3,
    scrollBeyondLastLine: false,
    automaticLayout: true,
    wordWrap: "on",
    padding: { top: 8, bottom: 8 },
    fontFamily: "ui-monospace, SFMono-Regular, Menlo, Monaco, Consolas, monospace",
    fontLigatures: true,
    
    // SQL-specific settings
    autoIndent: "full",
    formatOnPaste: true,
    formatOnType: true,
    
    // Suggestions improvements for SQL
    quickSuggestions: {
      other: true,
      comments: false,
      strings: false,
    },
    quickSuggestionsDelay: 50,
    parameterHints: {
      enabled: true,
    },
    suggestOnTriggerCharacters: true,
    acceptSuggestionOnEnter: "on",
    tabCompletion: "on",
    
    // Selection behavior
    selectionHighlight: true,
    
    // SQL specific settings
    wordBasedSuggestions: "off", // We'll provide our own SQL-specific suggestions
    
    // Improved error highlighting
    colorDecorators: true,
    
    // Cursor improvements
    cursorBlinking: "smooth",
    cursorSmoothCaretAnimation: "on",
    
    // Better bracket matching
    matchBrackets: "always",
    autoClosingBrackets: "always",
    
    // Better scrollbar visibility for SQL
    scrollbar: {
      vertical: "auto",
      horizontal: "auto",
      verticalScrollbarSize: 8,
      horizontalScrollbarSize: 8,
      verticalSliderSize: 8,
      horizontalSliderSize: 8,
      alwaysConsumeMouseWheel: false,
    },
  };
}