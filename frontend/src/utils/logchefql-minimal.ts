/**
 * Minimal LogchefQL utilities for Monaco integration.
 * The actual parsing and SQL generation is handled by the backend.
 * This file provides just enough for Monaco autocomplete and syntax highlighting.
 */

import * as monaco from 'monaco-editor';

// Types for Monaco autocomplete
export interface FieldInfo {
  name: string;
  type?: string;
  description?: string;
}

export interface ClickHouseColumn {
  name: string;
  type: string;
  default_type?: string;
  default_expression?: string;
  comment?: string;
}

// Field storage for autocomplete
let customFields: FieldInfo[] = [];

/**
 * Update custom fields for autocomplete
 */
export function updateLogChefQLFields(fields: FieldInfo[]): void {
  customFields = fields;
}

/**
 * Convert ClickHouse schema to field info
 */
export function convertSchemaToFields(columns: ClickHouseColumn[]): FieldInfo[] {
  return columns.map(col => ({
    name: col.name,
    type: col.type,
    description: col.comment || `Type: ${col.type}`
  }));
}

/**
 * Update fields from ClickHouse schema
 */
export function updateLogChefQLFieldsFromSchema(columns: ClickHouseColumn[]): void {
  customFields = convertSchemaToFields(columns);
}

/**
 * Update fields from schema and log samples
 */
export function updateLogChefQLFieldsFromSchemaAndSamples(
  columns: ClickHouseColumn[],
  logSampleFields: FieldInfo[] = []
): void {
  const schemaFields = convertSchemaToFields(columns);
  
  // Merge schema fields with sample fields, schema takes precedence
  const fieldMap = new Map<string, FieldInfo>();
  
  for (const field of logSampleFields) {
    fieldMap.set(field.name, field);
  }
  
  for (const field of schemaFields) {
    fieldMap.set(field.name, field);
  }
  
  customFields = Array.from(fieldMap.values());
}

/**
 * Get current fields
 */
export function getFields(): FieldInfo[] {
  return customFields;
}

// LogchefQL operators
const OPERATORS = ['=', '!=', '~', '!~', '>', '<', '>=', '<='];
const BOOLEAN_OPERATORS = ['and', 'or'];

/**
 * Create Monaco completion provider for LogchefQL
 */
function createCompletionProvider(): monaco.languages.CompletionItemProvider {
  return {
    provideCompletionItems: (model, position) => {
      const wordInfo = model.getWordUntilPosition(position);
      const range = {
        startLineNumber: position.lineNumber,
        endLineNumber: position.lineNumber,
        startColumn: wordInfo.startColumn,
        endColumn: wordInfo.endColumn
      };

      const lineContent = model.getLineContent(position.lineNumber);
      const textBeforeCursor = lineContent.substring(0, position.column - 1);

      const suggestions: monaco.languages.CompletionItem[] = [];

      // Check context: after operator, suggest values; after value, suggest boolean operators
      const hasOperator = OPERATORS.some(op => textBeforeCursor.includes(op));
      const endsWithBoolOp = /\b(and|or)\s*$/i.test(textBeforeCursor);
      const endsWithValue = /["']\s*$/.test(textBeforeCursor) || /\S+\s*$/.test(textBeforeCursor.replace(/\b(and|or)\b/gi, ''));

      if (endsWithBoolOp || !hasOperator) {
        // Suggest field names
        for (const field of customFields) {
          suggestions.push({
            label: field.name,
            kind: monaco.languages.CompletionItemKind.Field,
            insertText: field.name,
            detail: field.type || 'field',
            documentation: field.description,
            range
          });
        }
      }

      // After a complete expression, suggest boolean operators
      if (hasOperator && !endsWithBoolOp) {
        for (const op of BOOLEAN_OPERATORS) {
          suggestions.push({
            label: op,
            kind: monaco.languages.CompletionItemKind.Keyword,
            insertText: ` ${op} `,
            detail: 'boolean operator',
            range
          });
        }
      }

      // Suggest operators after field name
      const fieldPattern = new RegExp(`\\b(${customFields.map(f => f.name).join('|')})\\s*$`);
      if (fieldPattern.test(textBeforeCursor)) {
        for (const op of OPERATORS) {
          suggestions.push({
            label: op,
            kind: monaco.languages.CompletionItemKind.Operator,
            insertText: op,
            detail: 'comparison operator',
            range: {
              ...range,
              startColumn: position.column,
              endColumn: position.column
            }
          });
        }
      }

      return { suggestions };
    }
  };
}

/**
 * Create Monaco hover provider for LogchefQL
 */
function createHoverProvider(): monaco.languages.HoverProvider {
  return {
    provideHover: (model, position) => {
      const wordInfo = model.getWordAtPosition(position);
      if (!wordInfo) return null;

      const word = wordInfo.word.toLowerCase();

      // Check if it's a field
      const field = customFields.find(f => f.name.toLowerCase() === word);
      if (field) {
        return {
          contents: [
            { value: `**${field.name}**` },
            { value: field.type ? `Type: \`${field.type}\`` : '' },
            { value: field.description || '' }
          ].filter(c => c.value),
          range: new monaco.Range(
            position.lineNumber,
            wordInfo.startColumn,
            position.lineNumber,
            wordInfo.endColumn
          )
        };
      }

      // Check if it's a boolean operator
      if (BOOLEAN_OPERATORS.includes(word)) {
        return {
          contents: [
            { value: `**${word.toUpperCase()}**` },
            { value: 'Boolean operator to combine conditions' }
          ]
        };
      }

      return null;
    }
  };
}

// Track if enhanced LogchefQL is registered
let isRegistered = false;

/**
 * Register enhanced LogChefQL language support
 */
export function registerEnhancedLogChefQL(): void {
  if (isRegistered) return;
  
  const LANGUAGE_ID = 'logchefql';
  
  // Register completion provider
  monaco.languages.registerCompletionItemProvider(LANGUAGE_ID, createCompletionProvider());
  
  // Register hover provider
  monaco.languages.registerHoverProvider(LANGUAGE_ID, createHoverProvider());
  
  isRegistered = true;
}

