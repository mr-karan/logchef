import * as monaco from "monaco-editor";
import { loader } from "@guolao/vue-monaco-editor";
import EditorWorker from "monaco-editor/esm/vs/editor/editor.worker?worker";

import {
  Parser as LogchefQLParser,
  tokenTypes as logchefqlTokenTypes,
} from "@/utils/logchefql";
import {
  Parser as ClickhouseSQLParser,
  tokenTypes as sqlTokenTypes,
} from "@/utils/clickhouse-sql";

/**
 * Get default monaco editor options
 */
function getDefaultMonacoOptions() {
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
    lineNumbersMinChars: 0,
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

/**
 * Initialize Monaco editor setup
 * - Sets up worker
 * - Configures Monaco
 * - Defines themes
 */
function initMonacoSetup() {
  // Configure Monaco worker setup
  window.MonacoEnvironment = {
    getWorker: () => new EditorWorker(),
  };

  // Load Monaco configuration
  loader.config({ monaco });

  // Define themes
  monaco.editor.defineTheme("logchef", {
    base: "vs",
    inherit: true,
    colors: {},
    rules: [
      { token: "field", foreground: "0451a5" },
      { token: "alias", foreground: "0451a5", fontStyle: "bold" },
      { token: "operator", foreground: "0089ab" },
      { token: "argument", foreground: "0451a5" },
      { token: "modifier", foreground: "0089ab" },
      { token: "error", foreground: "ff0000" },
      { token: "logchefqlKey", foreground: "0451a5" },
      { token: "logchefqlOperator", foreground: "0089ab" },
      { token: "logchefqlValue", foreground: "8b0000" },
      { token: "number", foreground: "098658" },
      { token: "string", foreground: "a31515" },
      // SQL specific tokens
      { token: "keyword", foreground: "0000ff" },
      { token: "function", foreground: "795e26" },
      { token: "type", foreground: "267f99" },
      { token: "identifier", foreground: "001080" },
      { token: "comment", foreground: "008000" },
      { token: "punctuation", foreground: "000000" },
      { token: "variable", foreground: "0070c1" },
    ],
  });

  monaco.editor.defineTheme("logchef-dark", {
    base: "vs-dark",
    inherit: true,
    colors: {},
    rules: [
      { token: "field", foreground: "6e9fff" },
      { token: "alias", foreground: "6e9fff", fontStyle: "bold" },
      { token: "operator", foreground: "0089ab" },
      { token: "argument", foreground: "ffffff" },
      { token: "modifier", foreground: "fa83f8" },
      { token: "error", foreground: "ff0000" },
      { token: "logchefqlKey", foreground: "6e9fff" },
      { token: "logchefqlOperator", foreground: "0089ab" },
      { token: "logchefqlValue", foreground: "ce9178" },
      { token: "number", foreground: "b5cea8" },
      { token: "string", foreground: "ce9178" },
      // SQL specific tokens
      { token: "keyword", foreground: "569cd6" },
      { token: "function", foreground: "dcdcaa" },
      { token: "type", foreground: "4ec9b0" },
      { token: "identifier", foreground: "9cdcfe" },
      { token: "comment", foreground: "608b4e" },
      { token: "punctuation", foreground: "d4d4d4" },
      { token: "variable", foreground: "4fc1ff" },
    ],
  });

  // Register languages
  registerLanguages();
}

/**
 * Register languages for Monaco editor
 */
function registerLanguages() {
  // Register LogchefQL language
  monaco.languages.register({ id: "logchefql" });
  monaco.languages.setLanguageConfiguration("logchefql", {
    autoClosingPairs: [
      { open: "(", close: ")" },
      { open: '"', close: '"' },
      { open: "'", close: "'" },
    ],
    wordPattern: /(-?\d*\.\d\w*)|([^\`\~\!\@\#\%\^\&\*\(\)\-\=\+\[\{\]\}\\\|\;\:\'\"\,\.\<\>\/\?\s]+)/g,
  });
  
  monaco.languages.registerDocumentSemanticTokensProvider("logchefql", {
    getLegend: () => ({
      tokenTypes: logchefqlTokenTypes,
      tokenModifiers: []
    }),
    provideDocumentSemanticTokens: (model) => {
      try {
        const parser = new LogchefQLParser();
        parser.parse(model.getValue(), false);
    
        const data = parser.generateMonacoTokens();
    
        return {
          data: new Uint32Array(data),
          resultId: null,
        };
      } catch (e) {
        console.error("Error parsing LogchefQL:", e);
        return {
          data: new Uint32Array([]),
          resultId: null,
        };
      }
    },
    releaseDocumentSemanticTokens: () => { }
  });

  // Register ClickHouse SQL language
  monaco.languages.register({
    id: "clickhouse-sql",
    extensions: ['.sql'],
    aliases: ['ClickHouse SQL', 'Clickhouse SQL', 'clickhouse'],
    mimetypes: ['application/sql', 'text/x-sql'],
  });
  
  monaco.languages.setLanguageConfiguration("clickhouse-sql", {
    autoClosingPairs: [
      { open: "(", close: ")" },
      { open: '"', close: '"' },
      { open: "'", close: "'" },
      { open: "[", close: "]" },
      { open: "{", close: "}" },
    ],
    brackets: [
      ["{", "}"],
      ["[", "]"],
      ["(", ")"],
    ],
    comments: {
      lineComment: "--",
      blockComment: ["/*", "*/"],
    },
    wordPattern: /(-?\d*\.\d\w*)|([^\`\~\!\@\#\%\^\&\*\(\)\-\=\+\[\{\]\}\\\|\;\:\'\"\,\.\<\>\/\?\s]+)/g,
  });
  
  monaco.languages.registerDocumentSemanticTokensProvider("clickhouse-sql", {
    getLegend: () => ({
      tokenTypes: sqlTokenTypes,
      tokenModifiers: []
    }),
    provideDocumentSemanticTokens: (model) => {
      try {
        const parser = new ClickhouseSQLParser();
        parser.parse(model.getValue(), false);
    
        const data = parser.generateMonacoTokens();
    
        return {
          data: new Uint32Array(data),
          resultId: null,
        };
      } catch (e) {
        console.error("Error parsing ClickHouse SQL:", e);
        return {
          data: new Uint32Array([]),
          resultId: null,
        };
      }
    },
    releaseDocumentSemanticTokens: () => { }
  });
}

/**
 * Get the default SQL query to use when no query is provided
 */
function getDefaultSQLQuery(tableName = "logs") {
  const threeDaysAgo = new Date();
  threeDaysAgo.setHours(threeDaysAgo.getHours() - 3);

  const now = new Date();

  // Format dates for ClickHouse
  const startTimeFormatted = threeDaysAgo
    .toISOString()
    .replace("T", " ")
    .replace("Z", "");
  const endTimeFormatted = now.toISOString().replace("T", " ").replace("Z", "");

  return `SELECT *
FROM ${tableName}
WHERE timestamp >= '${startTimeFormatted}'
  AND timestamp <= '${endTimeFormatted}'
LIMIT 100`;
}

export { initMonacoSetup, getDefaultMonacoOptions, getDefaultSQLQuery };
