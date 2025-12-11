import * as monaco from "monaco-editor";
import { loader } from "@guolao/vue-monaco-editor";
import EditorWorker from "monaco-editor/esm/vs/editor/editor.worker?worker";
import {
  SQL_KEYWORDS,
  CLICKHOUSE_FUNCTIONS,
  SQL_TYPES,
} from "./clickhouse-sql/language";
import {
  registerEnhancedLogChefQL,
  updateLogChefQLFields,
  updateLogChefQLFieldsFromSchema,
  updateLogChefQLFieldsFromSchemaAndSamples,
  type FieldInfo,
  type ClickHouseColumn
} from "./logchefql-minimal";

// Global cache for Monaco models to preserve across navigation
interface ModelCacheEntry {
  model: monaco.editor.ITextModel;
  lastUsed: number;
}

// Global model cache with source/language specific entries
const globalModelCache = new Map<string, ModelCacheEntry>();

// Keep track of active editors for proper cleanup and state management
const activeEditorInstances = new Set<monaco.editor.IStandaloneCodeEditor>();

// Maximum number of models to keep in cache
const MAX_CACHED_MODELS = 20;

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
    lineNumbers: "off", // Always keep line numbers off
    glyphMargin: false,
    lineDecorationsWidth: 0, // Set to 0 to prevent shifting when editing
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

// Helper function for single-line mode options
export function getSingleLineModeOptions(): Partial<monaco.editor.IStandaloneEditorConstructionOptions> {
  return {
    lineNumbers: "off",
    folding: false,
    wordWrap: "off",
    scrollBeyondLastLine: false,
    // These help prevent newlines
    acceptSuggestionOnEnter: "on",
    lineDecorationsWidth: 0, // Set to 0 to prevent shifting when editing
    glyphMargin: false,
    minimap: { enabled: false }
  };
}

// Track initialization state
let setupComplete = false;

// Initialize Monaco only once
export function initMonacoSetup() {
  // Skip if already initialized
  if (setupComplete) {
    console.log("Monaco setup already initialized, skipping");
    return;
  }

  console.log("Initializing Monaco environment and languages");

  // Configure Monaco worker setup (ensure this runs only once)
  if (!window.MonacoEnvironment) {
     window.MonacoEnvironment = {
       getWorker: () => new EditorWorker(),
     };
     // Load Monaco configuration
     loader.config({ monaco });
  }

  // Mark as initialized
  setupComplete = true;

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
    colors: {
      // Match the bluish-dark theme
      'editor.background': '#0c0e14', // Matches --card/--background (222.2 84% 4.9%)
      'editor.foreground': '#f0f6fc', // Matches --foreground (210 40% 98%)
      'editorCursor.foreground': '#78aeff', // Accent color based on --primary
      'editor.selectionBackground': '#1c2536', // Slightly lighter than background
      'editor.lineHighlightBackground': '#111522', // Subtle highlight
      'editorLineNumber.foreground': '#495167', // Muted color for line numbers
      'editorIndentGuide.background': '#1d2536', // Subtle indent guides
      'editorWidget.background': '#111522', // Dropdown backgrounds
      'dropdown.background': '#111522',
      'list.hoverBackground': '#1c2536',
    },
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
    // Register enhanced autocomplete after basic language setup
    registerEnhancedLogChefQL();
  }
  if (!monaco.languages.getLanguages().some(lang => lang.id === 'clickhouse-sql')) {
    registerClickhouseSQL();
  }
}

// Get or create a model from global cache
export function getOrCreateModel(value: string, language: string, sourceId?: number, key?: string): monaco.editor.ITextModel {
  // Create a unique cache key
  const cacheKey = key ?? `${language}-${sourceId ?? 'default'}`;

  // Check if model exists in cache
  if (globalModelCache.has(cacheKey)) {
    const entry = globalModelCache.get(cacheKey)!;

    try {
      // Check if model is disposed - if so, we'll need to recreate it
      if (entry.model.isDisposed()) {
        console.log(`Model for ${cacheKey} was disposed, recreating it`);
        // Create new model since the cached one was disposed
        const newModel = monaco.editor.createModel(value, language);
        // Update cache entry
        globalModelCache.set(cacheKey, {
          model: newModel,
          lastUsed: Date.now()
        });
        return newModel;
      }

      // Update last used timestamp for valid model
      entry.lastUsed = Date.now();

      // Update model content if needed
      if (entry.model.getValue() !== value) {
        entry.model.setValue(value);
      }
      return entry.model;
    } catch (e) {
      console.warn(`Error accessing cached model for ${cacheKey}, recreating it:`, e);
      // If any error occurs (like model is disposed), create a new one
      const newModel = monaco.editor.createModel(value, language);
      // Update cache entry
      globalModelCache.set(cacheKey, {
        model: newModel,
        lastUsed: Date.now()
      });
      return newModel;
    }
  }

  // Create new model and add to cache
  const model = monaco.editor.createModel(value, language);

  // Prune cache if needed
  pruneModelCache();

  // Add new model to cache
  globalModelCache.set(cacheKey, {
    model,
    lastUsed: Date.now()
  });

  return model;
}

// Register an editor instance for global management
export function registerEditorInstance(editor: monaco.editor.IStandaloneCodeEditor) {
  if (!activeEditorInstances.has(editor)) {
    activeEditorInstances.add(editor);
  }
}

// Unregister an editor instance when fully disposing
export function unregisterEditorInstance(editor: monaco.editor.IStandaloneCodeEditor) {
  if (activeEditorInstances.has(editor)) {
    activeEditorInstances.delete(editor);
  }
}

// Safe disposal logic for lightweight editor cleanup
export function lightweightEditorDisposal(editor: monaco.editor.IStandaloneCodeEditor) {
  // Don't unregister the editor, just make it invisible and readonly
  try {
    // First make it read-only to prevent modifications
    editor.updateOptions({ readOnly: true });

    // Get model info before hiding the editor
    const model = editor.getModel();
    const modelId = model?.id;

    // Make it invisible
    const domNode = editor.getDomNode();
    if (domNode) {
      // Use visibility to maintain layout, not display:none which can cause issues
      domNode.style.visibility = 'hidden';
    }

    // Important: Do NOT detach the model from the editor
    // Just log that we're preserving it
    console.log(`Lightweight disposal completed, preserved model: ${modelId}`);

    return true;
  } catch (e) {
    console.error("Error during lightweight editor disposal:", e);
    return false;
  }
}

// Reactivate a previously deactivated editor
export function reactivateEditor(editor: monaco.editor.IStandaloneCodeEditor, language: string, content: string, sourceId?: number) {
  try {
    // First make the editor visible if it was hidden
    const domNode = editor.getDomNode();
    if (domNode) {
      domNode.style.visibility = 'visible';
    }

    // Ensure editor is not read-only
    editor.updateOptions({ readOnly: false });

    // Get the current model
    let currentModel = editor.getModel();

    // Check if model is null or disposed and needs to be restored
    if (!currentModel || currentModel.isDisposed()) {
      // Get a new or cached model using our improved getOrCreateModel
      const modelKey = `${language}-${sourceId ?? 'default'}`;
      const model = getOrCreateModel(content, language, sourceId, modelKey);

      // Reattach model to editor
      editor.setModel(model);
      console.log(`Reattached model to editor for ${modelKey}`);
    }

    // Force layout refresh to ensure proper rendering
    editor.layout();
    console.log('Editor successfully reactivated with model:', editor.getModel()?.id);
    return true;
  } catch (e) {
    console.error("Error reactivating editor:", e);
    return false;
  }
}

// Prune the model cache when it gets too large
function pruneModelCache() {
  if (globalModelCache.size <= MAX_CACHED_MODELS) {
    return;
  }

  // Sort entries by last used timestamp
  const entries = Array.from(globalModelCache.entries())
    .sort(([, a], [, b]) => a.lastUsed - b.lastUsed);

  // Remove oldest entries until we're under the limit
  const entriesToRemove = entries.slice(0, globalModelCache.size - MAX_CACHED_MODELS);
  for (const [key, entry] of entriesToRemove) {
    try {
      if (!entry.model.isDisposed()) {
        entry.model.dispose();
      }
    } catch (e) {
      console.warn(`Error disposing model ${key}:`, e);
    }
    globalModelCache.delete(key);
  }

  console.log(`Pruned ${entriesToRemove.length} models from cache. Cache size: ${globalModelCache.size}`);
}

// Update LogChefQL autocomplete fields
export function updateLogChefQLAutocompleteFields(fields: FieldInfo[]) {
  updateLogChefQLFields(fields);
}

// Update LogChefQL autocomplete fields from ClickHouse schema
export function updateLogChefQLFieldsFromClickHouseSchema(columns: ClickHouseColumn[]) {
  updateLogChefQLFieldsFromSchema(columns);
}

// Update LogChefQL autocomplete fields from schema and log samples
export function updateLogChefQLFieldsFromSchemaAndLogSamples(
  columns: ClickHouseColumn[],
  logSampleFields: FieldInfo[] = []
) {
  updateLogChefQLFieldsFromSchemaAndSamples(columns, logSampleFields);
}

// Export types for external use
export type { ClickHouseColumn, FieldInfo };

// Clean up all Monaco resources when app is unloaded
export function disposeAllMonacoResources() {
  // Dispose all active editors
  for (const editor of activeEditorInstances) {
    try {
      editor.dispose();
    } catch (e) {
      console.warn("Error disposing editor:", e);
    }
  }
  activeEditorInstances.clear();

  // Dispose all cached models
  for (const [key, entry] of globalModelCache.entries()) {
    try {
      if (!entry.model.isDisposed()) {
        entry.model.dispose();
      }
    } catch (e) {
      console.warn(`Error disposing model ${key}:`, e);
    }
  }
  globalModelCache.clear();
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
    comments: {},
    // Empty onEnterRules to prevent automatic new lines in the editor
    onEnterRules: []
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

  // Enhanced autocomplete is now handled by monaco-adapter.ts and will be registered automatically
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
