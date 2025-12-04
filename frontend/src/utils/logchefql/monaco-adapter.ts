import * as monaco from 'monaco-editor';
import { tokenize } from './tokenizer';
import { LogChefQLAutocomplete, type AutocompleteContext, type AutocompleteSuggestion, type FieldInfo } from './autocomplete';
import { convertSchemaToFields, type ClickHouseColumn } from './schema-converter';

export class MonacoLogChefQLAdapter {
  private autocomplete: LogChefQLAutocomplete;

  constructor(customFields: FieldInfo[] = []) {
    this.autocomplete = new LogChefQLAutocomplete(customFields);
  }

  /**
   * Update custom fields (e.g., from schema or log samples)
   */
  public updateCustomFields(fields: FieldInfo[]): void {
    this.autocomplete.updateCustomFields(fields);
  }

  /**
   * Create Monaco completion item provider for LogChefQL
   */
  public createCompletionItemProvider(): monaco.languages.CompletionItemProvider {
    return {
      provideCompletionItems: (model, position, context, token) => {
        try {
          const autocompleteContext = this.createAutocompleteContext(model, position);
          const suggestions = this.autocomplete.getSuggestions(autocompleteContext);

          const wordInfo = model.getWordUntilPosition(position);
          const range = {
            startLineNumber: position.lineNumber,
            endLineNumber: position.lineNumber,
            startColumn: wordInfo.startColumn,
            endColumn: wordInfo.endColumn
          };

          const monacoSuggestions = suggestions.map(suggestion =>
            this.convertToMonacoSuggestion(suggestion, range)
          );

          return {
            suggestions: monacoSuggestions,
            incomplete: false
          };
        } catch (error) {
          console.error('Error providing completion items:', error);
          return { suggestions: [] };
        }
      }
    };
  }

  /**
   * Create Monaco hover provider for LogChefQL
   */
  public createHoverProvider(): monaco.languages.HoverProvider {
    return {
      provideHover: (model, position) => {
        try {
          const wordInfo = model.getWordAtPosition(position);
          if (!wordInfo) return null;

          const word = wordInfo.word;
          const hoverInfo = this.getHoverInfo(word);

          if (hoverInfo) {
            return {
              contents: hoverInfo.contents,
              range: new monaco.Range(
                position.lineNumber,
                wordInfo.startColumn,
                position.lineNumber,
                wordInfo.endColumn
              )
            };
          }

          return null;
        } catch (error) {
          console.error('Error providing hover info:', error);
          return null;
        }
      }
    };
  }

  private createAutocompleteContext(
    model: monaco.editor.ITextModel,
    position: monaco.Position
  ): AutocompleteContext {
    const query = model.getValue();
    const offset = model.getOffsetAt(position);

    // Tokenize the query
    const { tokens } = tokenize(query);

    // Find current and previous tokens based on cursor position
    let currentToken;
    let previousToken;

    for (let i = 0; i < tokens.length; i++) {
      const token = tokens[i];
      // Convert 1-based position to 0-based offset
      const tokenStart = this.positionToOffset(model, token.position.line, token.position.column);
      const tokenEnd = tokenStart + token.value.length;

      if (offset >= tokenStart && offset <= tokenEnd) {
        currentToken = token;
        if (i > 0) previousToken = tokens[i - 1];
        break;
      } else if (offset < tokenStart) {
        if (i > 0) previousToken = tokens[i - 1];
        break;
      }
    }

    // If no current token found and we have tokens, use the last one as previous
    if (!currentToken && !previousToken && tokens.length > 0) {
      previousToken = tokens[tokens.length - 1];
    }

    return {
      query,
      position: offset,
      tokens,
      currentToken,
      previousToken,
      line: position.lineNumber,
      column: position.column
    };
  }

  private positionToOffset(
    model: monaco.editor.ITextModel,
    line: number,
    column: number
  ): number {
    // Monaco uses 1-based line numbers, tokenizer uses 1-based as well
    // Monaco uses 1-based column numbers, tokenizer uses 1-based as well
    return model.getOffsetAt({ lineNumber: line, column });
  }

  private convertToMonacoSuggestion(
    suggestion: AutocompleteSuggestion,
    range: monaco.IRange
  ): monaco.languages.CompletionItem {
    return {
      label: suggestion.label,
      kind: this.mapCompletionKind(suggestion.kind),
      insertText: suggestion.insertText,
      detail: suggestion.detail,
      documentation: suggestion.documentation,
      sortText: suggestion.sortText,
      range,
      // Enable snippet support for template strings
      insertTextRules: suggestion.insertText?.includes('${')
        ? monaco.languages.CompletionItemInsertTextRule.InsertAsSnippet
        : undefined
    };
  }

  private mapCompletionKind(kind: AutocompleteSuggestion['kind']): monaco.languages.CompletionItemKind {
    switch (kind) {
      case 'field':
        return monaco.languages.CompletionItemKind.Field;
      case 'operator':
        return monaco.languages.CompletionItemKind.Operator;
      case 'value':
        return monaco.languages.CompletionItemKind.Value;
      case 'keyword':
        return monaco.languages.CompletionItemKind.Keyword;
      case 'function':
        return monaco.languages.CompletionItemKind.Function;
      default:
        return monaco.languages.CompletionItemKind.Text;
    }
  }

  private getHoverInfo(word: string): { contents: monaco.IMarkdownString[] } | null {
    // Provide hover information for common LogChefQL elements
    const hoverMap: Record<string, { contents: monaco.IMarkdownString[] }> = {
      'and': {
        contents: [
          { value: '**and** (boolean operator)' },
          { value: 'Logical AND - both conditions must be true\n\nExample: `level = "error" and service = "api"`' }
        ]
      },
      'or': {
        contents: [
          { value: '**or** (boolean operator)' },
          { value: 'Logical OR - either condition must be true\n\nExample: `level = "error" or level = "warn"`' }
        ]
      },
      '=': {
        contents: [
          { value: '**=** (equals operator)' },
          { value: 'Exact match comparison\n\nExample: `level = "info"`' }
        ]
      },
      '!=': {
        contents: [
          { value: '**!=** (not equals operator)' },
          { value: 'Does not match comparison\n\nExample: `level != "debug"`' }
        ]
      },
      '~': {
        contents: [
          { value: '**~** (regex match operator)' },
          { value: 'Regular expression match\n\nExample: `message ~ "error.*timeout"`' }
        ]
      },
      '!~': {
        contents: [
          { value: '**!~** (regex not match operator)' },
          { value: 'Does not match regular expression\n\nExample: `message !~ "debug"`' }
        ]
      },
      'timestamp': {
        contents: [
          { value: '**timestamp** (field)' },
          { value: 'Log entry timestamp field\n\nExample: `timestamp > "2024-01-01T00:00:00Z"`' }
        ]
      },
      'level': {
        contents: [
          { value: '**level** (field)' },
          { value: 'Log level field (info, error, warn, debug)\n\nExample: `level = "error"`' }
        ]
      },
      'message': {
        contents: [
          { value: '**message** (field)' },
          { value: 'Log message content field\n\nExample: `message ~ "database connection"`' }
        ]
      },
      'service': {
        contents: [
          { value: '**service** (field)' },
          { value: 'Service name field\n\nExample: `service = "api"`' }
        ]
      }
    };

    return hoverMap[word.toLowerCase()] || null;
  }
}

/**
 * Register enhanced LogChefQL language support with advanced autocomplete
 */
export function registerEnhancedLogChefQL(customFields: FieldInfo[] = []): void {
  const LANGUAGE_ID = 'logchefql';
  const adapter = new MonacoLogChefQLAdapter(customFields);

  // Remove existing completion provider if it exists
  // Monaco doesn't provide a way to unregister, so we'll override
  monaco.languages.registerCompletionItemProvider(LANGUAGE_ID, adapter.createCompletionItemProvider());

  // Register hover provider for additional context
  monaco.languages.registerHoverProvider(LANGUAGE_ID, adapter.createHoverProvider());

  console.log('Enhanced LogChefQL language support registered with advanced autocomplete');
}

/**
 * Update fields for all registered LogChefQL autocomplete instances
 */
let globalAdapter: MonacoLogChefQLAdapter | null = null;

export function updateLogChefQLFields(fields: FieldInfo[]): void {
  if (globalAdapter) {
    globalAdapter.updateCustomFields(fields);
  }
}

export function getGlobalLogChefQLAdapter(): MonacoLogChefQLAdapter {
  if (!globalAdapter) {
    globalAdapter = new MonacoLogChefQLAdapter();
  }
  return globalAdapter;
}

/**
 * Update LogChefQL autocomplete fields from ClickHouse schema
 */
export function updateLogChefQLFieldsFromSchema(columns: ClickHouseColumn[]): void {
  const fields = convertSchemaToFields(columns);
  updateLogChefQLFields(fields);
}

/**
 * Update LogChefQL autocomplete with combined schema and log sample fields
 */
export function updateLogChefQLFieldsFromSchemaAndSamples(
  columns: ClickHouseColumn[],
  logSampleFields: FieldInfo[] = []
): void {
  const schemaFields = convertSchemaToFields(columns);
  const combinedFields = [...schemaFields, ...logSampleFields];
  updateLogChefQLFields(combinedFields);
}