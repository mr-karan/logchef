import { describe, it, expect } from 'vitest';
import { tokenize } from '../tokenizer';
import { LogChefQLAutocomplete, type AutocompleteContext, type FieldInfo } from '../autocomplete';

function createContext(query: string, position: number = query.length): AutocompleteContext {
  const { tokens } = tokenize(query);

  let currentToken;
  let previousToken;

  for (let i = 0; i < tokens.length; i++) {
    const token = tokens[i];
    const tokenEnd = token.position.column + token.value.length - 1;

    if (position >= token.position.column && position <= tokenEnd) {
      currentToken = token;
      if (i > 0) previousToken = tokens[i - 1];
      break;
    } else if (position < token.position.column) {
      if (i > 0) previousToken = tokens[i - 1];
      break;
    }
  }

  if (!currentToken && !previousToken && tokens.length > 0) {
    previousToken = tokens[tokens.length - 1];
  }

  return {
    query,
    position,
    tokens,
    currentToken,
    previousToken,
    line: 1,
    column: position
  };
}

describe('Nested Fields Autocomplete', () => {
  describe('Schema-based field suggestions (no hardcoding)', () => {
    it('should only suggest fields from provided schema, not hardcoded ones', () => {
      // Test with minimal schema - should only get these fields
      const schemaFields: FieldInfo[] = [
        { name: 'timestamp', type: 'string', description: 'Timestamp field' },
        { name: 'custom_field', type: 'string', description: 'Custom field' }
      ];

      const autocomplete = new LogChefQLAutocomplete(schemaFields);
      const context = createContext('');
      const suggestions = autocomplete.getSuggestions(context);

      expect(suggestions).toHaveLength(3); // 2 fields + 1 parentheses
      expect(suggestions.some(s => s.label === 'timestamp')).toBe(true);
      expect(suggestions.some(s => s.label === 'custom_field')).toBe(true);
      expect(suggestions.some(s => s.label === '()')).toBe(true); // Parentheses are included

      // Should NOT suggest any hardcoded fields
      expect(suggestions.some(s => s.label === 'log_attributes')).toBe(false);
      expect(suggestions.some(s => s.label === 'level')).toBe(false);
      expect(suggestions.some(s => s.label === 'message')).toBe(false);
    });

    it('should handle Map fields without duplication', () => {
      // Simulate the user's actual schema
      const userSchemaFields: FieldInfo[] = [
        { name: 'timestamp', type: 'string', description: 'Timestamp field' },
        { name: 'severity_text', type: 'string', description: 'Log severity level', examples: ['info', 'error', 'warn', 'debug', 'trace'] },
        { name: 'service_name', type: 'string', description: 'Service that generated the log', examples: ['api', 'web', 'database', 'auth'] },
        { name: 'body', type: 'string', description: 'Log message content' },
        { name: 'log_attributes', type: 'object', description: 'Map field - use log_attributes.key syntax to access nested values' }
      ];

      const autocomplete = new LogChefQLAutocomplete(userSchemaFields);
      const context = createContext('');
      const suggestions = autocomplete.getSuggestions(context);

      // Should have exactly the schema fields, no duplicates
      const logAttributesFields = suggestions.filter(s => s.label === 'log_attributes');
      expect(logAttributesFields).toHaveLength(1);
      expect(logAttributesFields[0].detail).toBe('object field');
      expect(logAttributesFields[0].documentation).toBe('Map field - use log_attributes.key syntax to access nested values');
    });
  });

  describe('Nested field access suggestions', () => {
    it('should show helpful message for Map field access without log samples', () => {
      const schemaFields: FieldInfo[] = [
        { name: 'log_attributes', type: 'object', description: 'Map field - use log_attributes.key syntax to access nested values' }
      ];

      const autocomplete = new LogChefQLAutocomplete(schemaFields);
      const context = createContext('log_attributes.');

      const suggestions = autocomplete.getSuggestions(context);

      expect(suggestions).toHaveLength(1);
      expect(suggestions[0].label).toBe('(log samples needed)');
      expect(suggestions[0].documentation).toContain('To suggest nested fields for log_attributes, log samples are needed');
    });

    it('should handle partial nested field typing', () => {
      const schemaFields: FieldInfo[] = [
        { name: 'log_attributes', type: 'object', description: 'Map field - use log_attributes.key syntax to access nested values' }
      ];

      const autocomplete = new LogChefQLAutocomplete(schemaFields);
      const context = createContext('log_attributes.use');
      const suggestions = autocomplete.getSuggestions(context);

      // Should still show the "need samples" message
      expect(suggestions).toHaveLength(1);
      expect(suggestions[0].label).toBe('(log samples needed)');
    });

    it('should not interfere with regular field suggestions', () => {
      const schemaFields: FieldInfo[] = [
        { name: 'log_attributes', type: 'object', description: 'Map field' },
        { name: 'timestamp', type: 'string', description: 'Timestamp field' },
        { name: 'service_name', type: 'string', description: 'Service name' }
      ];

      const autocomplete = new LogChefQLAutocomplete(schemaFields);

      // Regular field suggestions should work normally
      const context = createContext('');
      const suggestions = autocomplete.getSuggestions(context);

      expect(suggestions).toHaveLength(4); // 3 fields + 1 parentheses
      expect(suggestions.some(s => s.label === 'log_attributes')).toBe(true);
      expect(suggestions.some(s => s.label === 'timestamp')).toBe(true);
      expect(suggestions.some(s => s.label === 'service_name')).toBe(true);
      expect(suggestions.some(s => s.label === '()')).toBe(true);
    });

    it('should only trigger nested suggestions for object/Map types', () => {
      const schemaFields: FieldInfo[] = [
        { name: 'regular_field', type: 'string', description: 'Regular string field' }
      ];

      const autocomplete = new LogChefQLAutocomplete(schemaFields);
      const context = createContext('regular_field.');
      const suggestions = autocomplete.getSuggestions(context);

      // Should not show nested field message for non-object fields
      // Should return empty or general suggestions
      expect(suggestions.every(s => s.label !== '(log samples needed)')).toBe(true);
    });
  });

  describe('Dynamic field updates', () => {
    it('should update fields without hardcoded interference', () => {
      // Start with empty fields
      const autocomplete = new LogChefQLAutocomplete([]);

      let context = createContext('');
      let suggestions = autocomplete.getSuggestions(context);
      expect(suggestions).toHaveLength(0);

      // Update with schema fields
      const newFields: FieldInfo[] = [
        { name: 'dynamic_field', type: 'string', description: 'Added dynamically' },
        { name: 'log_attributes', type: 'object', description: 'Map field' }
      ];

      autocomplete.updateCustomFields(newFields);

      context = createContext('');
      suggestions = autocomplete.getSuggestions(context);

      expect(suggestions).toHaveLength(3); // 2 fields + 1 parentheses
      expect(suggestions.some(s => s.label === 'dynamic_field')).toBe(true);
      expect(suggestions.some(s => s.label === 'log_attributes')).toBe(true);
      expect(suggestions.some(s => s.label === '()')).toBe(true);

      // No hardcoded fields should appear
      expect(suggestions.some(s => s.label === 'level')).toBe(false);
      expect(suggestions.some(s => s.label === 'message')).toBe(false);
    });
  });
});