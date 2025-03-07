import * as monaco from "monaco-editor";
import { loader } from "@guolao/vue-monaco-editor";

import {
  Parser as LogchefqlParser,
  tokenTypes as logchefqlTokenTypes,
} from "@/utils/logchefql";

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
    scrollbarVisibility: 'hidden',
    scrollbar: {
      horizontal: 'hidden',
      vertical: 'hidden',
      alwaysConsumeMouseWheel: false,
      useShadows: false,
    },
    occurrencesHighlight: false,
    find: {
      addExtraSpaceOnTop: false,
      autoFindInSelection: 'never',
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
    renderLineHighlight: 'none',
    matchBrackets: 'always',
    'semanticHighlighting.enabled': true,
    fixedOverflowWidgets: true,
    wordWrap: "on",
    links: false,
    cursorBlinking: "smooth",
    cursorStyle: "line",
    multiCursorModifier: "alt",
    lineHeight: 20,
    renderWhitespace: "none",
    selectOnLineNumbers: false,
    dragAndDrop: false,
  }
}

function initMonacoSetup() {
  loader.config({ monaco });
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
    ],
  });

  // Register logchefql language
  monaco.languages.register({ id: "logchefql" });
  monaco.languages.setLanguageConfiguration("logchefql", {
    autoClosingPairs: [
      { open: "(", close: ")" },
      { open: '"', close: '"' },
      { open: "'", close: "'" },
    ],
  });

  // Register semantic tokens provider
  monaco.languages.registerDocumentSemanticTokensProvider("logchefql", {
    getLegend: () => ({
      tokenTypes: logchefqlTokenTypes,
      tokenModifiers: []
    }),
    provideDocumentSemanticTokens: (model) => {
      try {
        const parser = new LogchefqlParser();
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
}

export { initMonacoSetup, getDefaultMonacoOptions };
