import { describe, it, expect } from 'vitest';
import {
  convertClickHouseTypeToFieldType,
  generateFieldDescription,
  generateFieldExamples,
  convertSchemaToFields,
  supportsNestedAccess,
  type ClickHouseColumn
} from '../schema-converter';

describe('Schema Converter', () => {
  describe('Type conversion', () => {
    it('should convert ClickHouse types to field types correctly', () => {
      expect(convertClickHouseTypeToFieldType('String')).toBe('string');
      expect(convertClickHouseTypeToFieldType('LowCardinality(String)')).toBe('string');
      expect(convertClickHouseTypeToFieldType('Int32')).toBe('number');
      expect(convertClickHouseTypeToFieldType('UInt64')).toBe('number');
      expect(convertClickHouseTypeToFieldType('Float64')).toBe('number');
      expect(convertClickHouseTypeToFieldType('DateTime64(3)')).toBe('string');
      expect(convertClickHouseTypeToFieldType('Date')).toBe('string');
      expect(convertClickHouseTypeToFieldType('Bool')).toBe('boolean');
      expect(convertClickHouseTypeToFieldType('Map(LowCardinality(String), String)')).toBe('object');
      expect(convertClickHouseTypeToFieldType('Array(String)')).toBe('array');
    });

    it('should handle CODEC decorations', () => {
      expect(convertClickHouseTypeToFieldType('String CODEC(ZSTD(1))')).toBe('string');
      expect(convertClickHouseTypeToFieldType('Int32 CODEC(DoubleDelta, LZ4)')).toBe('number');
    });
  });

  describe('Description generation', () => {
    it('should generate appropriate descriptions for common fields', () => {
      expect(generateFieldDescription('severity_text', 'LowCardinality(String)')).toBe('Log severity level');
      expect(generateFieldDescription('service_name', 'LowCardinality(String)')).toBe('Service that generated the log');
      expect(generateFieldDescription('body', 'String')).toBe('Log message content');
      expect(generateFieldDescription('timestamp', 'DateTime64(3)')).toBe('Timestamp field');
    });

    it('should generate Map-specific descriptions', () => {
      const description = generateFieldDescription('log_attributes', 'Map(LowCardinality(String), String)');
      expect(description).toBe('Map field - use log_attributes.key syntax to access nested values');
    });

    it('should generate Array-specific descriptions', () => {
      const description = generateFieldDescription('tags', 'Array(String)');
      expect(description).toBe('Array field containing Array(String)');
    });
  });

  describe('Example generation', () => {
    it('should generate examples for known fields', () => {
      expect(generateFieldExamples('severity_text', 'String')).toEqual(['info', 'error', 'warn', 'debug', 'trace']);
      expect(generateFieldExamples('service_name', 'String')).toEqual(['api', 'web', 'database', 'auth']);
      expect(generateFieldExamples('namespace', 'String')).toEqual(['production', 'staging', 'development']);
    });

    it('should generate examples for date fields', () => {
      const examples = generateFieldExamples('timestamp', 'DateTime64(3)');
      expect(examples).toContain('2024-01-01T00:00:00Z');
    });

    it('should not generate examples for Map fields', () => {
      expect(generateFieldExamples('log_attributes', 'Map(String, String)')).toBeUndefined();
    });
  });

  describe('Schema conversion', () => {
    it('should convert full schema to field list', () => {
      const columns: ClickHouseColumn[] = [
        { name: 'timestamp', type: 'DateTime64(3)' },
        { name: 'severity_text', type: 'LowCardinality(String)' },
        { name: 'service_name', type: 'LowCardinality(String)' },
        { name: 'body', type: 'String' },
        { name: 'log_attributes', type: 'Map(LowCardinality(String), String)' },
        { name: 'trace_flags', type: 'UInt32' }
      ];

      const fields = convertSchemaToFields(columns);

      expect(fields).toHaveLength(6);

      const timestampField = fields.find(f => f.name === 'timestamp');
      expect(timestampField).toEqual({
        name: 'timestamp',
        type: 'string',
        description: 'Timestamp field',
        examples: ['2024-01-01T00:00:00Z', '2024-01-01 10:30:45']
      });

      const logAttributesField = fields.find(f => f.name === 'log_attributes');
      expect(logAttributesField).toEqual({
        name: 'log_attributes',
        type: 'object',
        description: 'Map field - use log_attributes.key syntax to access nested values',
        examples: undefined
      });

      const traceFlagsField = fields.find(f => f.name === 'trace_flags');
      expect(traceFlagsField?.type).toBe('number');
    });
  });

  describe('Nested access detection', () => {
    it('should detect fields that support nested access', () => {
      expect(supportsNestedAccess('object', 'Map(String, String)')).toBe(true);
      expect(supportsNestedAccess('object', 'Nested(name String, value String)')).toBe(true);
      expect(supportsNestedAccess('string', 'String')).toBe(false);
      expect(supportsNestedAccess('number', 'Int32')).toBe(false);
    });
  });

  describe('Integration with autocomplete', () => {
    it('should handle the user schema format correctly', () => {
      // This matches the schema format from the user's example
      const userColumns: ClickHouseColumn[] = [
        { name: 'timestamp', type: 'DateTime64(3)' },
        { name: 'trace_id', type: 'String' },
        { name: 'span_id', type: 'String' },
        { name: 'trace_flags', type: 'UInt32' },
        { name: 'severity_text', type: 'LowCardinality(String)' },
        { name: 'severity_number', type: 'Int32' },
        { name: 'service_name', type: 'LowCardinality(String)' },
        { name: 'namespace', type: 'LowCardinality(String)' },
        { name: 'body', type: 'String' },
        { name: 'log_attributes', type: 'Map(LowCardinality(String), String)' }
      ];

      const fields = convertSchemaToFields(userColumns);

      expect(fields).toHaveLength(10);

      // Should only have one log_attributes field (no duplicates)
      const logAttributesFields = fields.filter(f => f.name === 'log_attributes');
      expect(logAttributesFields).toHaveLength(1);

      // Should be correctly typed as object with Map description
      const logAttributesField = logAttributesFields[0];
      expect(logAttributesField.type).toBe('object');
      expect(logAttributesField.description).toBe('Map field - use log_attributes.key syntax to access nested values');
      expect(logAttributesField.examples).toBeUndefined();
    });
  });
});