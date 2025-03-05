import * as monaco from "monaco-editor";
import EditorWorker from "monaco-editor/esm/vs/editor/editor.worker?worker";
import { loader } from "@guolao/vue-monaco-editor";
import { registerLogFilterLanguage } from "./logfilter-language";
import { registerSqlLanguage } from "./sql-language";
import { registerLogChefQLLanguage } from "./monaco-logchefql";

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
  if (
    typeof window !== "undefined" &&
    typeof self !== "undefined" &&
    !self.MonacoEnvironment
  ) {
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
};

/**
 * Count and log all current Monaco editors and models
 * This is useful for debugging memory leaks
 */
export function logMonacoInstanceCounts() {
  if (typeof window !== "undefined" && window.monaco && window.monaco.editor) {
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
    occurrencesHighlight: true, // Enable highlighting other occurrences of the same field
    renderWhitespace: "boundary", // Show whitespace at word boundaries to help with spacing
    selectionHighlight: true, // Highlight the same selection elsewhere in the document

    // Error reporting improvements
    onDidChangeCursorPosition: true,
    lightbulb: {
      enabled: true, // Show lightbulb for code actions / error fixes
    },

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
      maxVisibleSuggestions: 12, // Reduced for cleaner interface
      filterGraceful: true,
      showMethods: false, // Cleaner suggestion UI
      showFunctions: false,
      showVariables: true,
      preview: true, // Show preview of what will be inserted
      snippetsPreventQuickSuggestions: false,
      localityBonus: true, // Prioritize suggestions based on proximity
      insertMode: "replace", // Replace existing word rather than insert
      showStatusBar: true, // Show status bar at bottom of suggestions
      previewMode: "prefix", // Show better preview of completion
      shareSuggestSelections: true, // Remember chosen suggestions
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
    cursorSurroundingLines: 0, // Don't scroll surrounding lines (not needed in single line)
    cursorSurroundingLinesStyle: "default",
    cursorWidth: 2, // Slightly wider cursor for visibility

    // Better accessibility
    accessibilitySupport: "auto",

    // Improved error highlighting and features
    colorDecorators: true,
    columnSelection: false, // No need for column selection in queries
    comments: {
      insertSpace: true,
    },

    // Modern code editing features
    bracketPairColorization: {
      enabled: true,
    },
    guides: {
      bracketPairs: true,
      highlightActiveIndentation: false,
      indentation: false,
    },

    // Better hover behavior
    hover: {
      enabled: true,
      delay: 300,
      sticky: true,
    },

    // Multiple cursor support for power users
    multiCursorModifier: "alt",
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

    // Define enhanced themes for light and dark modes with improved syntax highlighting
    monaco.editor.defineTheme("logchef-light", {
      base: "vs",
      inherit: true,
      colors: {
        // Editor appearance
        "editor.background": "#00000000", // Transparent background
        "editor.foreground": "#333333",
        
        // Cursor and selection
        "editorCursor.foreground": "#2563eb", // Primary blue
        "editor.selectionBackground": "#bfdbfe", // Light blue selection
        "editor.inactiveSelectionBackground": "#bfdbfeaa", // Semi-transparent selection when not focused
        "editor.selectionHighlightBackground": "#dbeafeaa", // Lighter blue for selection highlights
        "editor.lineHighlightBackground": "#f8fafc", // Very subtle line highlight
        "editor.lineHighlightBorder": "#e2e8f070", // Near invisible border for line highlight
        
        // Suggestion widget enhancements - akin to modern IDEs
        "editorSuggestWidget.background": "#ffffff",
        "editorSuggestWidget.border": "#e2e8f0",
        "editorSuggestWidget.selectedBackground": "#2563eb",
        "editorSuggestWidget.selectedForeground": "#ffffff",
        "editorSuggestWidget.highlightForeground": "#2563eb",
        "editorSuggestWidget.selectedIconForeground": "#ffffff",
        "editorSuggestWidget.focusHighlightForeground": "#2563eb",
        "editorSuggestWidget.focusBorder": "#2563eb",
        
        // Hover widget - cleaner, more defined style
        "editorHoverWidget.background": "#f8fafc",
        "editorHoverWidget.border": "#e2e8f0", 
        "editorHoverWidget.foreground": "#333333",
        "editorHoverWidget.statusBarBackground": "#f1f5f9",
        
        // Widget borders for better visibility
        "editorWidget.border": "#e2e8f0",
        "editorWidget.background": "#ffffff",
        
        // Find match highlighting
        "editor.findMatchBackground": "#fef9c3", // Light yellow for find matches
        "editor.findMatchHighlightBackground": "#fef9c3aa", // Transparent yellow for other matches
        "editor.findMatchBorder": "#eab308", // Yellow border for current match
        "editor.findRangeHighlightBackground": "#f8fafc", // Subtle highlight for find range
        
        // Scrollbar styling
        "scrollbarSlider.background": "#64748b30", // Light scrollbar
        "scrollbarSlider.hoverBackground": "#64748b50", // Darker on hover
        "scrollbarSlider.activeBackground": "#64748b70", // Darkest when active
        
        // Bracket match highlighting
        "editorBracketMatch.background": "#dbeafe80", // Light blue with transparency
        "editorBracketMatch.border": "#2563eb", // Blue border for matching brackets
        
        // Error/warning indicators
        "editorError.foreground": "#ef4444", // Red for errors
        "editorWarning.foreground": "#f59e0b", // Amber for warnings
        "editorInfo.foreground": "#3b82f6", // Blue for info
        
        // Guide lines
        "editorIndentGuide.background": "#e2e8f050", // Very subtle indent guides
        "editorIndentGuide.activeBackground": "#cbd5e1", // More visible active indent guide
      },
      rules: [
        // Field and identifiers
        { token: "field", foreground: "2563eb", fontStyle: "bold" }, // Primary blue
        { token: "identifier", foreground: "333333" },
        { token: "column", foreground: "2563eb", fontStyle: "bold" }, // Primary blue
        { token: "variable", foreground: "1e40af" }, // Dark blue
        
        // Operators and symbols
        { token: "operator", foreground: "0369a1", fontStyle: "bold" }, // Cyan
        { token: "magic-symbol", foreground: "dc2626", fontStyle: "bold" }, // Red
        { token: "delimiter", foreground: "475569", fontStyle: "bold" }, // Slate
        
        // Structure
        { token: "parenthesis", foreground: "475569" }, // Slate
        { token: "bracket", foreground: "475569" }, // Slate
        { token: "punctuation", foreground: "475569" }, // Slate
        
        // Constants and values
        { token: "string", foreground: "166534" }, // Green
        { token: "number", foreground: "c2410c" }, // Orange
        { token: "boolean", foreground: "7c3aed" }, // Purple
        { token: "regexp", foreground: "c2410c" }, // Orange
        
        // Keywords and constructs
        { token: "keyword", foreground: "7c3aed", fontStyle: "bold" }, // Purple
        { token: "type", foreground: "0891b2" }, // Light cyan
        { token: "function", foreground: "9333ea" }, // Violet
        { token: "predefined", foreground: "9333ea" }, // Violet
        
        // Misc
        { token: "comment", foreground: "65a30d", fontStyle: "italic" }, // Lime
        { token: "table", foreground: "0284c7" }, // Light blue
        
        // LogQL specific
        { token: "label", foreground: "2563eb", fontStyle: "bold" }, // Primary blue
        { token: "attribute", foreground: "0891b2" }, // Light cyan
        { token: "comparison-operator", foreground: "0369a1", fontStyle: "bold" }, // Cyan
        { token: "logical-operator", foreground: "7c3aed", fontStyle: "bold" }, // Purple
      ],
    });

    monaco.editor.defineTheme("logchef-dark", {
      base: "vs-dark",
      inherit: true,
      colors: {
        // Editor appearance
        "editor.background": "#00000000", // Transparent background
        "editor.foreground": "#e2e8f0", // Slate 200
        
        // Cursor and selection
        "editorCursor.foreground": "#60a5fa", // Blue 400
        "editor.selectionBackground": "#3b82f640", // More visible selection
        "editor.inactiveSelectionBackground": "#3b82f630", // Semi-transparent when not focused
        "editor.selectionHighlightBackground": "#3b82f620", // Lighter for selection highlights
        "editor.lineHighlightBackground": "#1e293b90", // Subtle line highlight
        "editor.lineHighlightBorder": "#334155", // Subtle border
        
        // Suggestion widget
        "editorSuggestWidget.background": "#1e293b", // Slate 800
        "editorSuggestWidget.border": "#334155", // Slate 700
        "editorSuggestWidget.selectedBackground": "#3b82f6", // Blue 500
        "editorSuggestWidget.selectedForeground": "#ffffff",
        "editorSuggestWidget.highlightForeground": "#38bdf8", // Sky 400
        "editorSuggestWidget.selectedIconForeground": "#ffffff",
        "editorSuggestWidget.focusHighlightForeground": "#38bdf8", // Sky 400
        "editorSuggestWidget.focusBorder": "#3b82f6", // Blue 500
        
        // Hover widget
        "editorHoverWidget.background": "#1e293b", // Slate 800
        "editorHoverWidget.border": "#334155", // Slate 700
        "editorHoverWidget.foreground": "#e2e8f0", // Slate 200
        "editorHoverWidget.statusBarBackground": "#0f172a", // Slate 900
        
        // Widget styling
        "editorWidget.border": "#334155", // Slate 700
        "editorWidget.background": "#1e293b", // Slate 800
        
        // Find match highlighting
        "editor.findMatchBackground": "#fbbf2450", // Semi-transparent amber
        "editor.findMatchHighlightBackground": "#fbbf2430", // More transparent amber
        "editor.findMatchBorder": "#fbbf24", // Amber border
        "editor.findRangeHighlightBackground": "#1e293b", // Subtle background
        
        // Scrollbar styling
        "scrollbarSlider.background": "#64748b40", // Semi-transparent scrollbar
        "scrollbarSlider.hoverBackground": "#64748b60", // More visible on hover
        "scrollbarSlider.activeBackground": "#64748b80", // Most visible when active
        
        // Bracket match highlighting
        "editorBracketMatch.background": "#3b82f620", // Blue with transparency
        "editorBracketMatch.border": "#60a5fa", // Blue 400 border
        
        // Error/warning indicators
        "editorError.foreground": "#f87171", // Red 400
        "editorWarning.foreground": "#fbbf24", // Amber 400
        "editorInfo.foreground": "#60a5fa", // Blue 400
        
        // Guide lines
        "editorIndentGuide.background": "#334155", // Slate 700
        "editorIndentGuide.activeBackground": "#475569", // Slate 600
      },
      rules: [
        // Fields and identifiers
        { token: "field", foreground: "60a5fa", fontStyle: "bold" }, // Blue 400
        { token: "identifier", foreground: "e2e8f0" }, // Slate 200
        { token: "column", foreground: "60a5fa", fontStyle: "bold" }, // Blue 400
        { token: "variable", foreground: "93c5fd" }, // Blue 300
        
        // Operators and symbols
        { token: "operator", foreground: "22d3ee", fontStyle: "bold" }, // Cyan 400
        { token: "magic-symbol", foreground: "f87171", fontStyle: "bold" }, // Red 400
        { token: "delimiter", foreground: "cbd5e1", fontStyle: "bold" }, // Slate 300
        
        // Structure
        { token: "parenthesis", foreground: "cbd5e1" }, // Slate 300
        { token: "bracket", foreground: "cbd5e1" }, // Slate 300
        { token: "punctuation", foreground: "cbd5e1" }, // Slate 300
        
        // Constants and values
        { token: "string", foreground: "4ade80" }, // Green 400
        { token: "number", foreground: "fb923c" }, // Orange 400
        { token: "boolean", foreground: "a78bfa" }, // Violet 400
        { token: "regexp", foreground: "fb923c" }, // Orange 400
        
        // Keywords and constructs
        { token: "keyword", foreground: "a78bfa", fontStyle: "bold" }, // Violet 400
        { token: "type", foreground: "22d3ee" }, // Cyan 400
        { token: "function", foreground: "c084fc" }, // Fuchsia 400
        { token: "predefined", foreground: "c084fc" }, // Fuchsia 400
        
        // Misc
        { token: "comment", foreground: "86efac", fontStyle: "italic" }, // Green 300
        { token: "table", foreground: "38bdf8" }, // Sky 400
        
        // LogQL specific
        { token: "label", foreground: "60a5fa", fontStyle: "bold" }, // Blue 400
        { token: "attribute", foreground: "22d3ee" }, // Cyan 400
        { token: "comparison-operator", foreground: "22d3ee", fontStyle: "bold" }, // Cyan 400
        { token: "logical-operator", foreground: "a78bfa", fontStyle: "bold" }, // Violet 400
      ],
    });

    // Register language definitions if needed
    try {
      debugLog("Registering custom language definitions");

      if (
        !monaco.languages.getLanguages().some((lang) => lang.id === "logfilter")
      ) {
        debugLog("Registering logfilter language");
        registerLogFilterLanguage();
      }

      if (
        !monaco.languages
          .getLanguages()
          .some((lang) => lang.id === "clickhouse-sql")
      ) {
        debugLog("Registering clickhouse-sql language");
        registerSqlLanguage();
      }

      if (
        !monaco.languages.getLanguages().some((lang) => lang.id === "logchefql")
      ) {
        debugLog("Registering logchefql language");
        registerLogChefQLLanguage();
      }
    } catch (langError) {
      console.error("Error registering languages:", langError);
    }

    // Set up safeguards
    if (typeof window !== "undefined") {
      // Log editor instances on navigation
      window.addEventListener("beforeunload", () => {
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
