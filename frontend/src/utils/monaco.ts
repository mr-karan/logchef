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

// Track if Monaco has been initialized
let isMonacoInitialized = false;

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
    // Enable quick suggestions for better UX
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
    suggest: {
      showIcons: true,
      maxVisibleSuggestions: 12,
      filterGraceful: true,
      snippetsPreventQuickSuggestions: false,
      showMethods: true,
      showFunctions: true,
      showVariables: true,
    },
    // Improve cursor behavior
    cursorBlinking: "phase",
    cursorSmoothCaretAnimation: "on",
    cursorStyle: "line",
    cursorWidth: 2,
    // Font settings
    fontFamily:
      "ui-monospace, SFMono-Regular, Menlo, Monaco, Consolas, monospace",
    fontLigatures: true,
    // Selection behavior
    selectionHighlight: true,
    // Performance optimizations
    renderWhitespace: "none",
    renderControlCharacters: false,
    renderIndentGuides: false,
  };
}

/**
 * Get single-line Monaco Editor options
 * Enhanced for better query experience similar to Grafana's LogQL
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
    // Ensure cursor is visible
    hideCursorInOverviewRuler: false,
    occurrencesHighlight: false,
    renderWhitespace: "boundary", // Show whitespace at word boundaries to help with spacing
    selectionHighlight: false,
    
    // Enhanced autocomplete behavior for better UX
    quickSuggestions: {
      other: true,
      comments: false,
      strings: true, // Enable in strings for better filter value suggestions
    },
    quickSuggestionsDelay: 0, // Immediate suggestions
    snippetSuggestions: "inline",
    suggestSelection: "first",
    acceptSuggestionOnEnter: "smart",
    suggest: {
      showIcons: true,
      maxVisibleSuggestions: 12,
      filterGraceful: true,
      showMethods: false, // Cleaner suggestion UI
      showFunctions: false,
      showVariables: true,
      preview: true, // Show preview of what will be inserted
      snippetsPreventQuickSuggestions: false,
      localityBonus: true, // Prioritize suggestions based on proximity
      insertMode: "replace", // Replace existing word rather than insert
    },
    tabCompletion: "on",
    wordBasedSuggestions: "off", // Disable word-based to avoid noise in our custom language
    
    // Highlight matching brackets for better query writing
    matchBrackets: "always",
    autoClosingBrackets: "always",
    autoClosingQuotes: "always",
    autoSurround: "languageDefined",
    
    // Cursor improvements
    cursorBlinking: "smooth",
    cursorSmoothCaretAnimation: "on",
    
    // No rulers for single-line editor
    
    // Better accessibility
    accessibilitySupport: "auto",
    
    // Improved error highlighting
    colorDecorators: true,
  };
}

/**
 * Initialize Monaco Editor with custom themes and language definitions
 */
export function initMonacoSetup() {
  // Prevent multiple initializations
  if (isMonacoInitialized) return;
  isMonacoInitialized = true;

  try {
    // Configure the loader with monaco instance
    loader.config({ monaco });

    // Define themes for light and dark modes
    monaco.editor.defineTheme("logchef-light", {
      base: "vs",
      inherit: true,
      colors: {
        "editor.background": "#00000000",
        "editor.foreground": "#333333",
        "editorCursor.foreground": "#007acc",
        "editor.selectionBackground": "#add6ff",
        "editor.lineHighlightBackground": "#00000000",
        "editorSuggestWidget.background": "#ffffff",
        "editorSuggestWidget.border": "#e0e0e0",
        "editorSuggestWidget.selectedBackground": "#0078d4",
        "editorSuggestWidget.selectedForeground": "#ffffff",
        "editorSuggestWidget.highlightForeground": "#0078d4",
        // Enhanced indication of active suggestion
        "editorSuggestWidget.focusHighlightForeground": "#0078d4",
        "editorSuggestWidget.selectedIconForeground": "#ffffff",
        // Better hover and tooltip styling
        "editorHoverWidget.background": "#f8f8f8",
        "editorHoverWidget.border": "#e0e0e0",
      },
      rules: [
        // Field tokens with distinct styling
        { token: "field", foreground: "0451a5", fontStyle: "bold" },
        // Magic symbol with distinctive color
        { token: "magic-symbol", foreground: "d32f2f", fontStyle: "bold" },
        // Regular identifiers
        { token: "identifier", foreground: "333333" },
        // Operators with prominent styling
        { token: "operator", foreground: "0089ab", fontStyle: "bold" },
        // Parentheses for future grouping support
        { token: "parenthesis", foreground: "333333" },
        // String values
        { token: "string", foreground: "a31515" },
        // Numeric values
        { token: "number", foreground: "098658" },
        // Keywords (AND, OR)
        { token: "keyword", foreground: "7A3E9D", fontStyle: "bold" },
        // Delimiters (semicolons)
        { token: "delimiter", foreground: "666666", fontStyle: "bold" },
        // Comments
        { token: "comment", foreground: "008000", fontStyle: "italic" },
      ],
    });

    monaco.editor.defineTheme("logchef-dark", {
      base: "vs-dark",
      inherit: true,
      colors: {
        "editor.background": "#00000000",
        "editor.foreground": "#d4d4d4",
        "editorCursor.foreground": "#569cd6",
        "editor.selectionBackground": "#264f78",
        "editor.lineHighlightBackground": "#00000000",
        "editorSuggestWidget.background": "#252526",
        "editorSuggestWidget.border": "#454545",
        "editorSuggestWidget.selectedBackground": "#0078d4",
        "editorSuggestWidget.selectedForeground": "#ffffff",
        "editorSuggestWidget.highlightForeground": "#18a3ff",
        // Enhanced indication of active suggestion
        "editorSuggestWidget.focusHighlightForeground": "#18a3ff",
        "editorSuggestWidget.selectedIconForeground": "#ffffff",
        // Better hover and tooltip styling
        "editorHoverWidget.background": "#252526",
        "editorHoverWidget.border": "#454545",
      },
      rules: [
        // Field tokens with distinct styling similar to Grafana's LogQL
        { token: "field", foreground: "6e9fff", fontStyle: "bold" },
        // Magic symbol with distinctive color
        { token: "magic-symbol", foreground: "ff5252", fontStyle: "bold" },
        // Regular identifiers
        { token: "identifier", foreground: "d4d4d4" },
        // Operators with prominent styling
        { token: "operator", foreground: "0089ab", fontStyle: "bold" },
        // Parentheses for future grouping support
        { token: "parenthesis", foreground: "d4d4d4" },
        // String values
        { token: "string", foreground: "ce9178" },
        // Numeric values
        { token: "number", foreground: "b5cea8" },
        // Keywords (AND, OR)
        { token: "keyword", foreground: "c586c0", fontStyle: "bold" },
        // Delimiters (semicolons)
        { token: "delimiter", foreground: "cccccc", fontStyle: "bold" },
        // Comments
        { token: "comment", foreground: "6a9955", fontStyle: "italic" },
      ],
    });

    // Register logfilter language
    registerLogFilterLanguage();
  } catch (error) {
    console.error("Error initializing Monaco:", error);
  }
}
