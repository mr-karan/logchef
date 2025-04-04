import * as monaco from "monaco-editor";
import { loader } from "@guolao/vue-monaco-editor";
import EditorWorker from "monaco-editor/esm/vs/editor/editor.worker?worker";
import { tokenTypes as logchefqlTokenTypes, Parser as LogchefQLParser } from "./logchefql";
import {
  SQL_KEYWORDS,
  CLICKHOUSE_FUNCTIONS,
  SQL_TYPES,
  CharType
} from "./clickhouse-sql/language";

export function getDefaultMonacoOptions(): monaco.editor.IStandaloneEditorConstructionOptions {
  return {
    readOnly: false,
    fontSize: 13, // Adjusted for a slightly more compact look
    lineHeight: 20, // Adjusted for new font size
    fontFamily: "ui-monospace, SFMono-Regular, Menlo, Monaco, Consolas, 'Liberation Mono', 'Courier New', monospace",
    padding: { top: 8, bottom: 8 },
    contextmenu: false,
    tabCompletion: "on",
    overviewRulerLanes: 0,
    scrollBeyondLastLine: false,
    scrollbar: { // Explicit visibility settings
      vertical: "auto",
      horizontal: "auto",
      alwaysConsumeMouseWheel: false,
      useShadows: false,
      verticalScrollbarSize: 8, // Thinner scrollbars
      horizontalScrollbarSize: 8,
    },
    occurrencesHighlight: "off", // Fix type error - was false
    find: {
      addExtraSpaceOnTop: false,
      autoFindInSelection: "never",
      seedSearchStringFromSelection: "never",
    },
    wordBasedSuggestions: "off",
    renderLineHighlight: "none", // Explicitly disable line highlighting
    matchBrackets: "always",
    "semanticHighlighting.enabled": true,
    automaticLayout: true, // Essential for responsive containers
    minimap: { enabled: false },
    folding: false, // Keep folding off for both modes
    lineNumbers: "off", // Keep line numbers off for both modes
    glyphMargin: false,
    lineDecorationsWidth: 0,
    lineNumbersMinChars: 0, // Not needed if lineNumbers is off
    hideCursorInOverviewRuler: true,
    fixedOverflowWidgets: true,
    wordWrap: "off",
    // Standard cursor behavior
    cursorBlinking: "blink", // Standard blinking
    cursorStyle: "line",
    cursorWidth: 2,
    cursorSmoothCaretAnimation: "off", // Standard movement
    renderWhitespace: "none",
    // Enable placeholder support
    'placeholder': '',
    // Additional professional touches
    roundedSelection: false, // Use sharp selections
    quickSuggestions: { other: true, comments: false, strings: false }, // Enable quick suggestions
  };
}

export function initMonacoSetup() {
  // Configure Monaco worker setup (ensure this runs only once)
  if (!window.MonacoEnvironment) {
     window.MonacoEnvironment = {
       getWorker: () => new EditorWorker(),
     };
     // Load Monaco configuration
     loader.config({ monaco });
  }


  // Define themes - Simplified to avoid styling issues
  monaco.editor.defineTheme("logchef-light", {
    base: "vs",
    inherit: true,
    colors: {}, // Use default VS colors
    rules: [
      { token: "logchefqlKey", foreground: "0451a5" },
      { token: "logchefqlOperator", foreground: "000000" },
      { token: "number", foreground: "098658" },
      { token: "string", foreground: "a31515" },
      { token: "punctuation", foreground: "000000" },
      // ClickHouse specific (or general SQL)
      { token: "keyword", foreground: "0000ff" },
      { token: "identifier", foreground: "001080" },
      { token: "operator", foreground: "000000" },
      { token: "comment", foreground: "008000" },
      { token: "function", foreground: "795E26" },
      { token: "type", foreground: "267f99" },
    ],
  });

  monaco.editor.defineTheme("logchef-dark", {
    base: "vs-dark",
    inherit: true,
    colors: {}, // Use default VS Dark colors
    rules: [
      { token: "logchefqlKey", foreground: "9cdcfe" },
      { token: "logchefqlOperator", foreground: "d4d4d4" },
      { token: "number", foreground: "b5cea8" },
      { token: "string", foreground: "ce9178" },
      { token: "punctuation", foreground: "d4d4d4" },
      // ClickHouse specific (or general SQL)
      { token: "keyword", foreground: "569cd6" },
      { token: "identifier", foreground: "9cdcfe" },
      { token: "operator", foreground: "d4d4d4" },
      { token: "comment", foreground: "6a9955" },
      { token: "function", foreground: "dcdcaa" },
      { token: "type", foreground: "4ec9b0" },
    ],
  });

  // Register languages (ensure this runs only once)
  if (!monaco.languages.getLanguages().some(lang => lang.id === 'logchefql')) {
    registerLogchefQL();
  }
  if (!monaco.languages.getLanguages().some(lang => lang.id === 'clickhouse-sql')) {
    registerClickhouseSQL();
  }
}

function registerLogchefQL() {
  const LANGUAGE_ID = "logchefql";

  monaco.languages.register({ id: LANGUAGE_ID });

  monaco.languages.setLanguageConfiguration(LANGUAGE_ID, {
    autoClosingPairs: [
      { open: "(", close: ")" },
      { open: '"', close: '"' },
      { open: "'", close: "'" },
    ],
    brackets: [
        ['(', ')'],
    ],
    // The word pattern is critical for proper cursor behavior when editing
    wordPattern: /(-?\d*\.\d\w*)|([a-zA-Z][\w._-]*)|(\b(and|or)\b)/,
    comments: {}
  });

  // Use Monarch syntax highlighting as the primary mechanism
  monaco.languages.setMonarchTokensProvider(LANGUAGE_ID, {
    ignoreCase: true,
    defaultToken: 'text',

    // Operators
    operators: [
      '=', '!=', '>', '<', '>=', '<=', '~', '!~'
    ],

    // Keywords (boolean operators)
    keywords: ['and', 'or'],

    // The main tokenizer
    tokenizer: {
      root: [
        // Identifiers - field names
        [/[a-zA-Z][\w._-]*/, {
          cases: {
            '@keywords': 'keyword',
            '@default': {
              token: 'logchefqlKey',
            }
          }
        }],

        // Operators
        [/(=|!=|>=|<=|>|<|~|!~)/, 'logchefqlOperator'],

        // Strings
        [/"([^"\\]|\\.)*$/, 'string.invalid'], // non-terminated string
        [/'([^'\\]|\\.)*$/, 'string.invalid'], // non-terminated string
        [/"/, { token: 'string.quote', bracket: '@open', next: '@string_double' }],
        [/'/, { token: 'string.quote', bracket: '@open', next: '@string_single' }],

        // Parentheses
        [/\(/, { token: 'delimiter.parenthesis', bracket: '@open' }],
        [/\)/, { token: 'delimiter.parenthesis', bracket: '@close' }],

        // Numbers
        [/\b\d+\b/, 'number'],

        // Whitespace
        [/\s+/, '']
      ],

      string_double: [
        [/[^\\"]+/, 'string'],
        [/\\./, 'string.escape'],
        [/"/, { token: 'string.quote', bracket: '@close', next: '@pop' }]
      ],

      string_single: [
        [/[^\\']+/, 'string'],
        [/\\./, 'string.escape'],
        [/'/, { token: 'string.quote', bracket: '@close', next: '@pop' }]
      ]
    }
  });

  // Add a completion provider for autocompletion
  monaco.languages.registerCompletionItemProvider(LANGUAGE_ID, {
    provideCompletionItems: (model, position) => {
      const wordInfo = model.getWordUntilPosition(position);
      const range = {
        startLineNumber: position.lineNumber,
        endLineNumber: position.lineNumber,
        startColumn: wordInfo.startColumn,
        endColumn: wordInfo.endColumn
      };

      const suggestions = [
        {
          label: 'and',
          kind: monaco.languages.CompletionItemKind.Keyword,
          insertText: 'and ',
          range: range
        },
        {
          label: 'or',
          kind: monaco.languages.CompletionItemKind.Keyword,
          insertText: 'or ',
          range: range
        }
      ];

      return { suggestions };
    }
  });
}

function registerClickhouseSQL() {
  const LANGUAGE_ID = "clickhouse-sql";

  monaco.languages.register({ id: LANGUAGE_ID });

  // Register a tokens provider for the language
  monaco.languages.setMonarchTokensProvider(LANGUAGE_ID, {
    defaultToken: 'invalid',
    tokenPostfix: '.sql',
    ignoreCase: true,

    // Basic tokenizing rules
    tokenizer: {
      root: [
        // Comments
        [/--.*$/, 'comment'],
        [/\/\*/, { token: 'comment.quote', next: '@comment' }],

        // Strings
        [/'/, { token: 'string', next: '@string' }],
        [/"/, { token: 'string.double', next: '@stringDouble' }],
        [/`/, { token: 'identifier.quote', next: '@quotedIdentifier' }],

        // Numbers
        [/\d+/, 'number'],
        [/\d*\.\d+([eE][-+]?\d+)?/, 'number.float'],
        [/0[xX][0-9a-fA-F]+/, 'number.hex'],

        // Keywords
        [new RegExp('\\b(' + SQL_KEYWORDS.join('|') + ')\\b', 'i'), 'keyword'],

        // Functions
        [new RegExp('\\b(' + CLICKHOUSE_FUNCTIONS.join('|') + ')\\b', 'i'), 'function'],

        // Data types
        [new RegExp('\\b(' + SQL_TYPES.join('|') + ')\\b', 'i'), 'type'],

        // Identifiers
        [/[a-zA-Z_][a-zA-Z0-9_]*/, 'identifier'],

        // Operators
        [/[+\-*/%&|^!=<>]+/, 'operator'],

        // Punctuation
        [/[;,.()]/, 'delimiter'],
      ],

      string: [
        [/[^']+/, 'string'],
        [/''/, 'string'],
        [/'/, { token: 'string', next: '@pop' }],
      ],

      stringDouble: [
        [/[^"]+/, 'string.double'],
        [/""/, 'string.double'],
        [/"/, { token: 'string.double', next: '@pop' }],
      ],

      quotedIdentifier: [
        [/[^`]+/, 'identifier'],
        [/``/, 'identifier'],
        [/`/, { token: 'identifier.quote', next: '@pop' }],
      ],

      comment: [
        [/[^/*]+/, 'comment'],
        [/\*\//, { token: 'comment.quote', next: '@pop' }],
        [/./, 'comment'],
      ],
    }
  });

  // Configure the language with autocomplete, formatting, etc.
  monaco.languages.setLanguageConfiguration(LANGUAGE_ID, {
    comments: {
      lineComment: '--',
      blockComment: ['/*', '*/'],
    },
    brackets: [
      ['{', '}'],
      ['[', ']'],
      ['(', ')'],
    ],
    autoClosingPairs: [
      { open: '{', close: '}' },
      { open: '[', close: ']' },
      { open: '(', close: ')' },
      { open: '"', close: '"' },
      { open: "'", close: "'" },
      { open: '`', close: '`' },
    ],
    surroundingPairs: [
      { open: '{', close: '}' },
      { open: '[', close: ']' },
      { open: '(', close: ')' },
      { open: '"', close: '"' },
      { open: "'", close: "'" },
      { open: '`', close: '`' },
    ],
  });

  // Define completions for SQL keywords, functions, etc.
  monaco.languages.registerCompletionItemProvider(LANGUAGE_ID, {
    provideCompletionItems: (model, position, context, token) => {
      const wordInfo = model.getWordUntilPosition(position);
      const wordRange = new monaco.Range(
        position.lineNumber,
        wordInfo.startColumn,
        position.lineNumber,
        wordInfo.endColumn
      );

      // Create completion items for keywords
      const keywordItems = SQL_KEYWORDS.map((keyword: string) => ({
        label: keyword.toUpperCase(),
        kind: monaco.languages.CompletionItemKind.Keyword,
        insertText: keyword.toUpperCase(),
        detail: 'Keyword',
        range: wordRange
      }));

      // Create completion items for functions
      const functionItems = CLICKHOUSE_FUNCTIONS.map((func: string) => ({
        label: func,
        kind: monaco.languages.CompletionItemKind.Function,
        insertText: func,
        detail: 'Function',
        documentation: `ClickHouse function: ${func}()`,
        range: wordRange
      }));

      // Create completion items for data types
      const typeItems = SQL_TYPES.map((type: string) => ({
        label: type,
        kind: monaco.languages.CompletionItemKind.TypeParameter,
        insertText: type,
        detail: 'Data Type',
        range: wordRange
      }));

      // Combine all items
      return {
        suggestions: [
          ...keywordItems,
          ...functionItems,
          ...typeItems,
        ]
      };
    }
  });

  // Register basic hover provider
  monaco.languages.registerHoverProvider(LANGUAGE_ID, {
    provideHover: (model, position) => {
      // Get the word at the current position
      const wordInfo = model.getWordAtPosition(position);
      if (!wordInfo) return null;

      const word = wordInfo.word.toLowerCase();

      // Display documentation for keywords
      if (SQL_KEYWORDS.some((k: string) => k.toLowerCase() === word)) {
        return {
          contents: [
            { value: `**${word.toUpperCase()}**` },
            { value: `SQL keyword used in ClickHouse queries.` }
          ]
        };
      }

      // Display documentation for functions
      if (CLICKHOUSE_FUNCTIONS.some((f: string) => f.toLowerCase() === word)) {
        return {
          contents: [
            { value: `**${word}()**` },
            { value: `ClickHouse SQL function. For detailed documentation, visit the [ClickHouse docs](https://clickhouse.com/docs/en/sql-reference/functions).` }
          ]
        };
      }

      // Display documentation for data types
      if (SQL_TYPES.some((t: string) => t.toLowerCase() === word)) {
        return {
          contents: [
            { value: `**${word}**` },
            { value: `ClickHouse data type. For detailed documentation, visit the [ClickHouse docs](https://clickhouse.com/docs/en/sql-reference/data-types).` }
          ]
        };
      }

      return null;
    }
  });
}
