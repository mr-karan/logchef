import * as monaco from "monaco-editor";
import EditorWorker from "monaco-editor/esm/vs/editor/editor.worker?worker";
import { loader } from "@guolao/vue-monaco-editor";
import { registerLogFilterLanguage } from "./logfilter-language";

// Setup Monaco Editor workers
self.MonacoEnvironment = {
  getWorker(_, label) {
    return new EditorWorker();
  },
};

/**
 * Get default Monaco Editor options for compact input fields
 */
export function getDefaultMonacoOptions() {
  return {
    fontSize: 13,
    minimap: { enabled: false },
    lineNumbers: "off",
    glyphMargin: false,
    folding: false,
    lineDecorationsWidth: 0,
    lineNumbersMinChars: 0,
    scrollBeyondLastLine: false,
    automaticLayout: true,
    wordWrap: "on",
    padding: { top: 8, bottom: 8 },
    scrollbar: {
      horizontal: "auto",
      vertical: "hidden",
      useShadows: false,
      horizontalScrollbarSize: 4,
    },
    overviewRulerBorder: false,
    overviewRulerLanes: 0,
    hideCursorInOverviewRuler: true,
    renderLineHighlight: "none",
    fixedOverflowWidgets: true,
    contextmenu: false,
    links: false,
    quickSuggestions: true,
    quickSuggestionsDelay: 100,
    parameterHints: {
      enabled: true,
    },
    suggestOnTriggerCharacters: true,
    acceptSuggestionOnEnter: "on",
    tabCompletion: "on",
    suggest: {
      showIcons: true,
      showStatusBar: true,
      preview: true,
      filterGraceful: true,
      snippetsPreventQuickSuggestions: false,
    },
  };
}

/**
 * Get single-line Monaco Editor options
 */
export function getSingleLineMonacoOptions() {
  return {
    ...getDefaultMonacoOptions(),
    lineNumbers: "off",
    folding: false,
    wordWrap: "off",
    wrappingIndent: "none",
    lineDecorationsWidth: 0,
    lineNumbersMinChars: 0,
    overviewRulerBorder: false,
    scrollBeyondLastLine: false,
    renderLineHighlight: "none",
    scrollbar: {
      horizontal: "auto",
      vertical: "hidden",
      horizontalScrollbarSize: 4,
    },
    find: {
      addExtraSpaceOnTop: false,
      autoFindInSelection: "never",
      seedSearchStringFromSelection: "never",
    },
  };
}

/**
 * Initialize Monaco Editor with custom themes and language definitions
 */
export function initMonacoSetup() {
  // Configure the loader with monaco instance
  loader.config({ monaco });

  // Define themes for light and dark modes
  monaco.editor.defineTheme("logchef-light", {
    base: "vs",
    inherit: true,
    colors: {
      "editor.background": "#ffffff",
      "editor.foreground": "#333333",
      "editorCursor.foreground": "#007acc",
      "editor.selectionBackground": "#add6ff",
      "editor.lineHighlightBackground": "#f5f5f5",
      "editorSuggestWidget.background": "#ffffff",
      "editorSuggestWidget.border": "#e0e0e0",
      "editorSuggestWidget.selectedBackground": "#e8e8e8",
    },
    rules: [
      { token: "field", foreground: "0451a5", fontStyle: "bold" },
      { token: "fieldTrigger", foreground: "0451a5", fontStyle: "bold" },
      { token: "operator", foreground: "0089ab" },
      { token: "string", foreground: "a31515" },
      { token: "number", foreground: "098658" },
      { token: "keyword", foreground: "7A3E9D" },
      { token: "delimiter", foreground: "666666" },
      { token: "comment", foreground: "008000", fontStyle: "italic" },
    ],
  });

  monaco.editor.defineTheme("logchef-dark", {
    base: "vs-dark",
    inherit: true,
    colors: {
      "editor.background": "#1e1e1e",
      "editor.foreground": "#d4d4d4",
      "editorCursor.foreground": "#569cd6",
      "editor.selectionBackground": "#264f78",
      "editor.lineHighlightBackground": "#2a2d2e",
      "editorSuggestWidget.background": "#252526",
      "editorSuggestWidget.border": "#454545",
      "editorSuggestWidget.selectedBackground": "#062f4a",
    },
    rules: [
      { token: "field", foreground: "6e9fff", fontStyle: "bold" },
      { token: "fieldTrigger", foreground: "6e9fff", fontStyle: "bold" },
      { token: "operator", foreground: "0089ab" },
      { token: "string", foreground: "ce9178" },
      { token: "number", foreground: "b5cea8" },
      { token: "keyword", foreground: "c586c0" },
      { token: "delimiter", foreground: "cccccc" },
      { token: "comment", foreground: "6a9955", fontStyle: "italic" },
    ],
  });

  // Register logfilter language
  registerLogFilterLanguage();
}
