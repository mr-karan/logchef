/**
 * Minimal LogchefQL utilities for Monaco integration.
 * Field and column types for Monaco integration.
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
