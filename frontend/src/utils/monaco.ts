import * as monaco from "monaco-editor";
import EditorWorker from "monaco-editor/esm/vs/editor/editor.worker?worker";
import { loader } from "@guolao/vue-monaco-editor";
import { registerLogFilterLanguage } from "./logfilter-language";
import { registerSqlLanguage } from "./sql-language";

// FUNDAMENTAL CHANGE: Initialize Monaco once, globally, and never reset it
// Monaco is a global singleton by design, and trying to reinitialize it causes issues
let monacoInitialized = false;

// Define a debug log function to track Monaco lifecycle
const DEBUG = true;
function debugLog(...args) {
  if (DEBUG) {
    console.log(`[Monaco Debug]`, ...args);
  }
}

// We will set up Monaco environment ONLY when explicitly requested
// This prevents cross-tab memory issues and ensures clean initialization
const setupMonacoEnvironment = () => {
  if (typeof window !== 'undefined' && typeof self !== 'undefined' && !self.MonacoEnvironment) {
    try {
      debugLog("Setting up Monaco environment");
      self.MonacoEnvironment = {
        getWorker(_, label) {
          debugLog("Creating Monaco worker", label);
          return new EditorWorker();
        },
      };
      debugLog("Monaco environment setup completed successfully");
      return true;
    } catch (error) {
      console.error("Failed to setup Monaco environment:", error);
      return false;
    }
  }
  return true; // Return true if already set up
}

/**
 * Count and log all current Monaco editors and models
 * This is useful for debugging memory leaks
 */
export function logMonacoInstanceCounts() {
  if (typeof window !== 'undefined' && window.monaco && window.monaco.editor) {
    try {
      const editors = window.monaco.editor.getEditors();
      const models = window.monaco.editor.getModels();
      debugLog(
        `Monaco instance counts - Editors: ${editors.length}, Models: ${models.length}`, 
        { editors, models }
      );
      return { editors, models };
    } catch (error) {
      console.error("Error counting Monaco instances:", error);
      return { editors: [], models: [] };
    }
  }
  return { editors: [], models: [] };
}

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
      maxVisibleSuggestions: 25,
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
      maxVisibleSuggestions: 25,
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
 * Check if Monaco is initialized
 */
export function isMonacoInitialized() {
  return monacoInitialized;
}

/**
 * Dispose specific Monaco editor instance safely
 * This should be called by components when they are unmounted
 */
export function disposeMonacoEditorInstance(editor) {
  if (!editor) return;
  
  try {
    debugLog("Disposing editor instance", editor.getId());
    
    // Get the model associated with this editor
    const model = editor.getModel();
    
    // Dispose the editor
    editor.dispose();
    
    // Log current instances after disposal
    logMonacoInstanceCounts();
    
    return true;
  } catch (error) {
    console.error("Error disposing Monaco editor instance:", error);
    return false;
  }
}

/**
 * Initialize Monaco Editor with custom themes and language definitions
 * This is now called ONCE at application startup
 */
export function initMonacoSetup() {
  // Check if already initialized
  if (monacoInitialized) {
    debugLog("Monaco already initialized, skipping");
    return;
  }
  
  // Setup Monaco environment first
  if (!setupMonacoEnvironment()) {
    console.error("Failed to set up Monaco environment");
    return;
  }
  
  try {
    debugLog("Initializing Monaco editor");
    
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
        "editorSuggestWidget.highlightForeground": "#00c853",
        "editorSuggestWidget.selectedIconForeground": "#ffffff",
        "editorHoverWidget.background": "#f8f8f8",
        "editorHoverWidget.border": "#e0e0e0",
      },
      rules: [
        { token: "field", foreground: "0451a5", fontStyle: "bold" },
        { token: "magic-symbol", foreground: "d32f2f", fontStyle: "bold" },
        { token: "identifier", foreground: "333333" },
        { token: "operator", foreground: "0089ab", fontStyle: "bold" },
        { token: "parenthesis", foreground: "333333" },
        { token: "string", foreground: "a31515" },
        { token: "number", foreground: "098658" },
        { token: "keyword", foreground: "7A3E9D", fontStyle: "bold" },
        { token: "delimiter", foreground: "666666", fontStyle: "bold" },
        { token: "comment", foreground: "008000", fontStyle: "italic" },
        { token: "function", foreground: "795E26" },
        { token: "table", foreground: "0070C1" },
        { token: "column", foreground: "0451a5", fontStyle: "bold" },
        { token: "type", foreground: "267f99" },
        { token: "variable", foreground: "001080" },
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
        "editorSuggestWidget.highlightForeground": "#00c853",
        "editorSuggestWidget.selectedIconForeground": "#ffffff",
        "editorHoverWidget.background": "#252526",
        "editorHoverWidget.border": "#454545",
      },
      rules: [
        { token: "field", foreground: "6e9fff", fontStyle: "bold" },
        { token: "magic-symbol", foreground: "ff5252", fontStyle: "bold" },
        { token: "identifier", foreground: "d4d4d4" },
        { token: "operator", foreground: "0089ab", fontStyle: "bold" },
        { token: "parenthesis", foreground: "d4d4d4" },
        { token: "string", foreground: "ce9178" },
        { token: "number", foreground: "b5cea8" },
        { token: "keyword", foreground: "c586c0", fontStyle: "bold" },
        { token: "delimiter", foreground: "cccccc", fontStyle: "bold" },
        { token: "comment", foreground: "6a9955", fontStyle: "italic" },
        { token: "function", foreground: "dcdcaa" },
        { token: "table", foreground: "4ec9b0" },
        { token: "column", foreground: "9cdcfe", fontStyle: "bold" },
        { token: "type", foreground: "4ec9b0" },
        { token: "variable", foreground: "9cdcfe" },
      ],
    });

    // Register language definitions if needed
    try {
      debugLog("Registering custom language definitions");
      
      if (!monaco.languages.getLanguages().some(lang => lang.id === "logfilter")) {
        debugLog("Registering logfilter language");
        registerLogFilterLanguage();
      }
      
      if (!monaco.languages.getLanguages().some(lang => lang.id === "clickhouse-sql")) {
        debugLog("Registering clickhouse-sql language");
        registerSqlLanguage();
      }
    } catch (langError) {
      console.error("Error registering languages:", langError);
    }
    
    // Set up safeguards
    if (typeof window !== 'undefined') {
      // Log editor instances on navigation
      window.addEventListener('beforeunload', () => {
        debugLog("Page unloading - current Monaco instances:");
        logMonacoInstanceCounts();
      });
    }
    
    // Mark as initialized
    monacoInitialized = true;
    debugLog("Monaco successfully initialized");
  } catch (error) {
    console.error("Error initializing Monaco:", error);
  }
}
