/**
 * Minimal LogchefQL utilities for Monaco integration.
 * Provides types for schema-to-field conversion.
 * Autocomplete is handled by SqlMonacoEditor.vue (context-aware).
 * Syntax highlighting is handled by monaco.ts registerLogchefQL().
 */

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
