import * as monaco from "monaco-editor";
import { loader } from "@guolao/vue-monaco-editor";
import EditorWorker from "monaco-editor/esm/vs/editor/editor.worker?worker";
import { tokenTypes as logchefqlTokenTypes, Parser as LogchefQLParser } from "./logchefql";
import { tokenTypes as clickhouseTokenTypes, Parser as ClickHouseSQLParser } from "./clickhouse-sql";

export function getDefaultMonacoOptions() {
  return {
    readOnly: false,
    fontSize: 13,
    padding: {
      top: 6,
      bottom: 6,
    },
    contextmenu: false,
    tabCompletion: "on",
    overviewRulerLanes: 0,
    lineNumbersMinChars: 0,
    scrollBeyondLastLine: false,
    scrollbarVisibility: "hidden",
    scrollbar: {
      horizontal: "hidden",
      vertical: "hidden",
      alwaysConsumeMouseWheel: false,
      useShadows: false,
    },
    occurrencesHighlight: false,
    find: {
      addExtraSpaceOnTop: false,
      autoFindInSelection: "never",
      seedSearchStringFromSelection: false,
    },
    wordBasedSuggestions: "off",
    lineDecorationsWidth: 0,
    hideCursorInOverviewRuler: true,
    glyphMargin: false,
    scrollBeyondLastColumn: 0,
    automaticLayout: true,
    minimap: {
      enabled: false,
    },
    folding: false,
    lineNumbers: false,
    renderLineHighlight: "none",
    matchBrackets: "always",
    "semanticHighlighting.enabled": true,
  };
}

export function initMonacoSetup() {
  // Configure Monaco worker setup
  window.MonacoEnvironment = {
    getWorker: () => new EditorWorker(),
  };

  // Load Monaco configuration
  loader.config({ monaco });

  // Define themes
  monaco.editor.defineTheme("logchef-light", {
    base: "vs",
    inherit: true,
    colors: {},
    rules: [
      { token: "logchefqlKey", foreground: "0451a5" },
      { token: "logchefqlOperator", foreground: "0089ab" },
      { token: "logchefqlValue", foreground: "8b0000" },
      { token: "number", foreground: "098658" },
      { token: "string", foreground: "a31515" },
      { token: "keyword", foreground: "0000ff" },
      { token: "identifier", foreground: "001080" },
      { token: "operator", foreground: "0089ab" },
      { token: "comment", foreground: "008000" },
      { token: "punctuation", foreground: "000000" },
    ],
  });

  monaco.editor.defineTheme("logchef-dark", {
    base: "vs-dark",
    inherit: true,
    colors: {},
    rules: [
      { token: "logchefqlKey", foreground: "6e9fff" },
      { token: "logchefqlOperator", foreground: "0089ab" },
      { token: "logchefqlValue", foreground: "d16969" },
      { token: "number", foreground: "b5cea8" },
      { token: "string", foreground: "ce9178" },
      { token: "keyword", foreground: "569cd6" },
      { token: "identifier", foreground: "9cdcfe" },
      { token: "operator", foreground: "d4d4d4" },
      { token: "comment", foreground: "6a9955" },
      { token: "punctuation", foreground: "d4d4d4" },
    ],
  });

  // Register languages
  registerLogchefQL();
  registerClickhouseSQL();
}

function registerLogchefQL() {
  // Register LogchefQL language
  monaco.languages.register({ id: "logchefql" });
  monaco.languages.setLanguageConfiguration("logchefql", {
    autoClosingPairs: [
      { open: "(", close: ")" },
      { open: '"', close: '"' },
      { open: "'", close: "'" },
    ],
    wordPattern:
      /(-?\d*\.\d\w*)|([^\`\~\!\@\#\%\^\&\*\(\)\-\=\+\[\{\]\}\\\|\;\:\'\"\,\.\<\>\/\?\s]+)/g,
  });

  // Register semantic tokens provider
  monaco.languages.registerDocumentSemanticTokensProvider("logchefql", {
    getLegend: () => ({
      tokenTypes: logchefqlTokenTypes,
      tokenModifiers: [],
    }),
    provideDocumentSemanticTokens: (model) => {
      const parser = new LogchefQLParser();
      parser.parse(model.getValue());

      const data = parser.generateMonacoTokens();

      return {
        data: new Uint32Array(data),
        resultId: null,
      };
    },
    releaseDocumentSemanticTokens: () => {},
  });
}

function registerClickhouseSQL() {
  // Register ClickHouse SQL language
  monaco.languages.register({ id: "clickhouse-sql" });
  monaco.languages.setLanguageConfiguration("clickhouse-sql", {
    autoClosingPairs: [
      { open: "(", close: ")" },
      { open: '"', close: '"' },
      { open: "'", close: "'" },
    ],
    wordPattern:
      /(-?\d*\.\d\w*)|([^\`\~\!\@\#\%\^\&\*\(\)\-\=\+\[\{\]\}\\\|\;\:\'\"\,\.\<\>\/\?\s]+)/g,
  });

  // Register semantic tokens provider
  monaco.languages.registerDocumentSemanticTokensProvider("clickhouse-sql", {
    getLegend: () => ({
      tokenTypes: clickhouseTokenTypes,
      tokenModifiers: [],
    }),
    provideDocumentSemanticTokens: (model) => {
      try {
        const parser = new ClickHouseSQLParser();
        return {
          data: new Uint32Array(parser.parse(model.getValue())),
          resultId: null,
        };
      } catch (error) {
        // Return empty data on parsing error
        return {
          data: new Uint32Array(),
          resultId: null
        };
      }
    },
    releaseDocumentSemanticTokens: () => {},
  });
}
