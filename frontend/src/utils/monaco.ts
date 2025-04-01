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
    fontSize: 14, // Slightly larger default
    lineHeight: 21, // Explicit line height
    fontFamily: "ui-monospace, SFMono-Regular, Menlo, Monaco, Consolas, 'Liberation Mono', 'Courier New', monospace",
    padding: { top: 8, bottom: 8 }, // Consistent padding
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
      seedSearchStringFromSelection: "never", // Usually better defaults
    },
    wordBasedSuggestions: "off", // Fix type error - was false
    renderLineHighlight: "none",
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
    fixedOverflowWidgets: true, // Important for suggestion widgets
    wordWrap: "off", // Default to off for both modes
    // Force cursor options to be visible
    cursorBlinking: "phase",  // Use phase for a smoother blink
    cursorStyle: "line",       // Ensure line style for visibility
    cursorWidth: 2,            // Make cursor slightly thicker
    cursorSmoothCaretAnimation: "on", // Smooth transitions when moving cursor
    renderWhitespace: "none",  // Don't show whitespace by default
    // Enable placeholder support
    'placeholder': '', // Default empty, will be set by component
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
    // More robust word pattern needed? Default might be okay with semantic tokens.
    // wordPattern: /(-?\d*\.\d\w*)|([^\`\~\!\@\#\%\^\&\*\(\)\-\=\+\[\{\]\}\\\|\;\:\'\"\,\.\<\>\/\?\s]+)/g,
    brackets: [
        ['(', ')'],
    ],
    comments: {
        // Add if LogchefQL supports comments, e.g., lineComment: '//'
    }
  });

  // Register semantic tokens provider
  monaco.languages.registerDocumentSemanticTokensProvider(LANGUAGE_ID, {
    getLegend: () => ({
      tokenTypes: logchefqlTokenTypes,
      tokenModifiers: [], // No modifiers used currently
    }),
    provideDocumentSemanticTokens: (model, _token) => {
      // console.time(`SemanticTokens ${LANGUAGE_ID}`); // Perf check
      try {
        const parser = new LogchefQLParser();
        parser.parse(model.getValue()); // Don't raise errors here, let highlighting fail gracefully
        const data = parser.generateMonacoTokens();
        // console.timeEnd(`SemanticTokens ${LANGUAGE_ID}`);
        return {
          data: new Uint32Array(data),
          // resultId: null, // Optional: Caching strategy not implemented here
        };
      } catch (error) {
        console.warn(`Error providing semantic tokens for ${LANGUAGE_ID}:`, error);
        return { data: new Uint32Array() }; // Return empty on error
      }
    },
    releaseDocumentSemanticTokens: (_resultId) => {}, // No-op if not using resultId
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
