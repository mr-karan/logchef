import type { FieldInfo } from './autocomplete';

export interface ClickHouseColumn {
  name: string;
  type: string;
}

/**
 * Convert ClickHouse column types to LogChefQL field types
 */
export function convertClickHouseTypeToFieldType(clickhouseType: string): FieldInfo['type'] {
  // Normalize the type by removing codec and other decorations
  const baseType = clickhouseType
    .replace(/\s+CODEC\([^)]+\)/g, '')
    .replace(/LowCardinality\(([^)]+)\)/g, '$1')
    .trim();

  // Map ClickHouse types to our field types - order matters!
  // Check complex types first before simple string matching
  if (baseType.startsWith('Map(')) {
    return 'object'; // Maps are accessed like objects in LogChefQL
  }

  if (baseType.startsWith('Array(')) {
    return 'array';
  }

  if (baseType.includes('String')) {
    return 'string';
  }

  if (baseType.includes('Int') || baseType.includes('UInt') || baseType.includes('Float') || baseType.includes('Double') || baseType.includes('Decimal')) {
    return 'number';
  }

  if (baseType.includes('DateTime') || baseType.includes('Date')) {
    return 'string'; // Dates are handled as strings in queries
  }

  if (baseType.includes('Bool')) {
    return 'boolean';
  }

  // Default to string for unknown types
  return 'string';
}

/**
 * Generate field description based on ClickHouse type
 */
export function generateFieldDescription(name: string, clickhouseType: string): string {
  const baseType = clickhouseType
    .replace(/\s+CODEC\([^)]+\)/g, '')
    .replace(/LowCardinality\(([^)]+)\)/g, '$1')
    .trim();

  if (baseType.startsWith('Map(')) {
    return `Map field - use ${name}.key syntax to access nested values`;
  }

  if (baseType.startsWith('Array(')) {
    return `Array field containing ${baseType}`;
  }

  if (baseType.includes('DateTime')) {
    return 'Timestamp field';
  }

  if (name === 'severity_text') {
    return 'Log severity level';
  }

  if (name === 'service_name') {
    return 'Service that generated the log';
  }

  if (name === 'body' || name === 'message') {
    return 'Log message content';
  }

  return `${baseType} field`;
}

/**
 * Generate example values based on field name and type
 */
export function generateFieldExamples(name: string, clickhouseType: string): string[] | undefined {
  const baseType = clickhouseType
    .replace(/\s+CODEC\([^)]+\)/g, '')
    .replace(/LowCardinality\(([^)]+)\)/g, '$1')
    .trim();

  if (name === 'severity_text') {
    return ['info', 'error', 'warn', 'debug', 'trace'];
  }

  if (name === 'service_name') {
    return ['api', 'web', 'database', 'auth'];
  }

  if (name === 'namespace') {
    return ['production', 'staging', 'development'];
  }

  if (baseType.includes('DateTime')) {
    return ['2024-01-01T00:00:00Z', '2024-01-01 10:30:45'];
  }

  if (baseType.includes('Bool')) {
    return ['true', 'false'];
  }

  if (baseType.includes('Int') || baseType.includes('UInt')) {
    return ['0', '1', '200', '404', '500'];
  }

  // Don't provide examples for Map or Array types - they need log samples
  if (baseType.startsWith('Map(') || baseType.startsWith('Array(')) {
    return undefined;
  }

  return undefined;
}

/**
 * Convert ClickHouse schema columns to LogChefQL FieldInfo
 */
export function convertSchemaToFields(columns: ClickHouseColumn[]): FieldInfo[] {
  return columns.map(column => ({
    name: column.name,
    type: convertClickHouseTypeToFieldType(column.type),
    description: generateFieldDescription(column.name, column.type),
    examples: generateFieldExamples(column.name, column.type)
  }));
}

/**
 * Check if a field supports nested access (like Maps)
 */
export function supportsNestedAccess(fieldType: string, clickhouseType: string): boolean {
  const baseType = clickhouseType
    .replace(/\s+CODEC\([^)]+\)/g, '')
    .replace(/LowCardinality\(([^)]+)\)/g, '$1')
    .trim();

  return baseType.startsWith('Map(') || baseType.startsWith('Nested(');
}