import * as monaco from "monaco-editor";
import { loader } from "@guolao/vue-monaco-editor";
import EditorWorker from "monaco-editor/esm/vs/editor/editor.worker?worker";
import { tokenTypes as logchefqlTokenTypes, Parser as LogchefQLParser } from "./logchefql";
import { tokenTypes as clickhouseTokenTypes, Parser as ClickHouseSQLParser } from "./clickhouse-sql";

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
    occurrencesHighlight: false, // Consider 'multi' if needed later
    find: {
      addExtraSpaceOnTop: false,
      autoFindInSelection: "never",
      seedSearchStringFromSelection: "never", // Usually better defaults
    },
    wordBasedSuggestions: false, // Explicitly false
    renderLineHighlight: "none",
    matchBrackets: "always",
    "semanticHighlighting.enabled": true,
    automaticLayout: true, // Essential for responsive containers
    minimap: { enabled: false },
    folding: false, // Keep folding off for LogchefQL default
    lineNumbers: "off", // Keep line numbers off for LogchefQL default
    glyphMargin: false,
    lineDecorationsWidth: 0,
    lineNumbersMinChars: 0, // Not needed if lineNumbers is off
    hideCursorInOverviewRuler: true,
    fixedOverflowWidgets: true, // Important for suggestion widgets
    wordWrap: "off", // Default to off, enable per language if needed
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


  // Define themes - Adjusted rules based on refined tokens
  monaco.editor.defineTheme("logchef-light", {
    base: "vs",
    inherit: true,
    colors: {}, // Keep empty unless specific background/foreground needed
    rules: [
      { token: "logchefqlKey", foreground: "0451a5" }, // Same as identifier usually
      { token: "logchefqlOperator", foreground: "000000" }, // Simple black for = != etc.
      // logchefqlValue is intermediate, use NUMBER and STRING
      { token: "number", foreground: "098658" },
      { token: "string", foreground: "a31515" },
      { token: "punctuation", foreground: "000000" }, // Brackets, quotes
      // ClickHouse specific (or general SQL)
      { token: "keyword", foreground: "0000ff" }, // SELECT, FROM, WHERE
      { token: "identifier", foreground: "001080" }, // Table/column names
      { token: "operator", foreground: "000000" }, // SQL operators like AND, OR
      { token: "comment", foreground: "008000" },
      // { token: "type", foreground: "267f99" }, // Data types if needed
    ],
  });

  monaco.editor.defineTheme("logchef-dark", {
    base: "vs-dark",
    inherit: true,
    colors: {},
    rules: [
      { token: "logchefqlKey", foreground: "9cdcfe" }, // Same as identifier
      { token: "logchefqlOperator", foreground: "d4d4d4" }, // White/grey for = !=
      { token: "number", foreground: "b5cea8" },
      { token: "string", foreground: "ce9178" },
      { token: "punctuation", foreground: "d4d4d4" }, // Brackets, quotes
      // ClickHouse specific (or general SQL)
      { token: "keyword", foreground: "569cd6" }, // SELECT, FROM, WHERE
      { token: "identifier", foreground: "9cdcfe" }, // Table/column names
      { token: "operator", foreground: "d4d4d4" }, // SQL operators like AND, OR
      { token: "comment", foreground: "6a9955" },
      // { token: "type", foreground: "4ec9b0" }, // Data types if needed
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

  // Set language configuration directly for ClickHouse SQL
  monaco.languages.setLanguageConfiguration(LANGUAGE_ID, {
     autoClosingPairs: [
       { open: "(", close: ")" },
       { open: '"', close: '"' }, // Keep if CH uses double quotes for identifiers
       { open: "'", close: "'" },
       { open: "`", close: "`" }, // Add backticks if used
     ],
     brackets: [
        ['(', ')'],
        ['[', ']'], // If CH uses array brackets
        ['{', '}'] // If CH uses map brackets
     ],
     comments: {
        lineComment: "--",
        blockComment: ["/*", "*/"]
     },
     // Add specific ClickHouse keywords/functions to wordPattern if necessary
     // Or rely on completion provider and semantic tokens primarily.
  });

  // Register semantic tokens provider for ClickHouse SQL
  monaco.languages.registerDocumentSemanticTokensProvider(LANGUAGE_ID, {
    getLegend: () => ({
      tokenTypes: clickhouseTokenTypes, // Use the imported token types
      tokenModifiers: [],
    }),
    provideDocumentSemanticTokens: (model, _token) => {
      // console.time(`SemanticTokens ${LANGUAGE_ID}`);
      try {
        const parser = new ClickHouseSQLParser(); // Use the specific CH parser
        const tokensData = parser.parse(model.getValue()); // Assuming parser returns the Uint32Array data directly
        // console.timeEnd(`SemanticTokens ${LANGUAGE_ID}`);
        return {
          data: new Uint32Array(tokensData),
          // resultId: null,
        };
      } catch (error) {
        console.warn(`Error providing semantic tokens for ${LANGUAGE_ID}:`, error);
        // Return empty data on parsing error to avoid breaking highlighting
        return { data: new Uint32Array() };
      }
    },
    releaseDocumentSemanticTokens: (_resultId) => {},
  });
}
