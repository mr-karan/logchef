import { describe, it, expect } from 'vitest';
import { tokenize } from '../tokenizer';
import { LogChefQLAutocomplete, type AutocompleteContext, type FieldInfo } from '../autocomplete';

function createContext(query: string, position: number = query.length): AutocompleteContext {
  const { tokens } = tokenize(query);

  // Find current and previous tokens based on position
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

  // If position is at the end and we haven't found a current token,
  // the previous token is the last one
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

describe('LogChefQL Autocomplete', () => {
  // Create test fields that match the schema structure
  const testFields: FieldInfo[] = [
    { name: 'timestamp', type: 'string', description: 'Log entry timestamp', examples: ['2024-01-01T00:00:00Z'] },
    { name: 'level', type: 'string', description: 'Log level', examples: ['info', 'error', 'debug', 'warn'] },
    { name: 'message', type: 'string', description: 'Log message content' },
    { name: 'service', type: 'string', description: 'Service name' },
    { name: 'host', type: 'string', description: 'Hostname' },
    { name: 'user_id', type: 'string', description: 'User identifier' },
    { name: 'request_id', type: 'string', description: 'Request trace ID' },
    { name: 'log_attributes', type: 'object', description: 'Map field - use log_attributes.key syntax to access nested values' }
  ];

  const autocomplete = new LogChefQLAutocomplete(testFields);

  describe('Field Suggestions', () => {
    it('should suggest field names at query start', () => {
      const context = createContext('');
      const suggestions = autocomplete.getSuggestions(context);

      expect(suggestions.length).toBeGreaterThan(0);
      expect(suggestions.some(s => s.label === 'timestamp')).toBe(true);
      expect(suggestions.some(s => s.label === 'level')).toBe(true);
      expect(suggestions.some(s => s.label === 'message')).toBe(true);
    });

    it('should filter field suggestions based on prefix', () => {
      const context = createContext('tim', 3);
      const suggestions = autocomplete.getSuggestions(context);

      expect(suggestions.some(s => s.label === 'timestamp')).toBe(true);
      expect(suggestions.every(s => s.label.toLowerCase().includes('tim'))).toBe(true);
    });

    it('should provide field details and documentation', () => {
      const context = createContext('');
      const suggestions = autocomplete.getSuggestions(context);

      const timestampSuggestion = suggestions.find(s => s.label === 'timestamp');
      expect(timestampSuggestion).toBeDefined();
      expect(timestampSuggestion?.kind).toBe('field');
      expect(timestampSuggestion?.detail).toBe('string field');
      expect(timestampSuggestion?.documentation).toBe('Log entry timestamp');
    });
  });

  describe('Operator Suggestions', () => {
    it('should suggest operators after field name', () => {
      const context = createContext('level ');
      const suggestions = autocomplete.getSuggestions(context);

      expect(suggestions.some(s => s.label === '=')).toBe(true);
      expect(suggestions.some(s => s.label === '!=')).toBe(true);
      expect(suggestions.some(s => s.label === '~')).toBe(true);
      expect(suggestions.some(s => s.label === '!~')).toBe(true);
    });

    it('should provide operator documentation', () => {
      const context = createContext('level ');
      const suggestions = autocomplete.getSuggestions(context);

      const equalsSuggestion = suggestions.find(s => s.label === '=');
      expect(equalsSuggestion?.kind).toBe('operator');
      expect(equalsSuggestion?.detail).toBe('Equals');
      expect(equalsSuggestion?.documentation).toBe('Exact match');
    });

    it('should suggest comparison operators', () => {
      const context = createContext('timestamp ');
      const suggestions = autocomplete.getSuggestions(context);

      expect(suggestions.some(s => s.label === '>')).toBe(true);
      expect(suggestions.some(s => s.label === '<')).toBe(true);
      expect(suggestions.some(s => s.label === '>=')).toBe(true);
      expect(suggestions.some(s => s.label === '<=')).toBe(true);
    });
  });

  describe('Value Suggestions', () => {
    it('should suggest common values after operator', () => {
      const context = createContext('level = ');
      const suggestions = autocomplete.getSuggestions(context);

      expect(suggestions.some(s => s.label === 'true')).toBe(true);
      expect(suggestions.some(s => s.label === 'false')).toBe(true);
      expect(suggestions.some(s => s.label === 'null')).toBe(true);
      expect(suggestions.some(s => s.label === '"string"')).toBe(true);
    });

    it('should suggest field-specific examples when available', () => {
      const context = createContext('level = ');
      const suggestions = autocomplete.getSuggestions(context);

      // Level field should have example values
      expect(suggestions.some(s => s.label.includes('info'))).toBe(true);
      expect(suggestions.some(s => s.label.includes('error'))).toBe(true);
    });

    it('should provide value type information', () => {
      const context = createContext('level = ');
      const suggestions = autocomplete.getSuggestions(context);

      const exampleSuggestion = suggestions.find(s => s.label.includes('info'));
      expect(exampleSuggestion?.kind).toBe('value');
      expect(exampleSuggestion?.detail).toBe('Example string value');
    });
  });

  describe('Boolean Operator Suggestions', () => {
    it('should suggest boolean operators after complete expression', () => {
      const context = createContext('level = "info" ');
      const suggestions = autocomplete.getSuggestions(context);

      expect(suggestions.some(s => s.label === 'and')).toBe(true);
      expect(suggestions.some(s => s.label === 'or')).toBe(true);
    });

    it('should provide boolean operator documentation', () => {
      const context = createContext('level = "info" ');
      const suggestions = autocomplete.getSuggestions(context);

      const andSuggestion = suggestions.find(s => s.label === 'and');
      expect(andSuggestion?.kind).toBe('keyword');
      expect(andSuggestion?.detail).toBe('Logical AND');
      expect(andSuggestion?.documentation).toBe('Both conditions must be true');
    });
  });

  describe('Pipe Operator Suggestions', () => {
    it('should suggest pipe operator after complete WHERE clause', () => {
      const context = createContext('level = "info" and service = "api"');
      const suggestions = autocomplete.getSuggestions(context);

      expect(suggestions.some(s => s.label === '|')).toBe(true);
    });

    it('should provide pipe operator documentation', () => {
      const context = createContext('level = "info"');
      const suggestions = autocomplete.getSuggestions(context);

      const pipeSuggestion = suggestions.find(s => s.label === '|');
      if (pipeSuggestion) {
        expect(pipeSuggestion.kind).toBe('operator');
        expect(pipeSuggestion.detail).toBe('Pipe to SELECT');
        expect(pipeSuggestion.documentation).toBe('Select specific fields from results');
      }
    });

    it('should not suggest pipe operator when already present', () => {
      const context = createContext('level = "info" | ');
      const suggestions = autocomplete.getSuggestions(context);

      expect(suggestions.some(s => s.label === '|')).toBe(false);
    });
  });

  describe('SELECT Field Suggestions', () => {
    it('should suggest fields in SELECT context', () => {
      const context = createContext('level = "info" | ');
      const suggestions = autocomplete.getSuggestions(context);

      expect(suggestions.some(s => s.label === 'timestamp')).toBe(true);
      expect(suggestions.some(s => s.label === 'message')).toBe(true);
      expect(suggestions.some(s => s.label === 'service')).toBe(true);
    });

    it('should provide SELECT-specific field details', () => {
      const context = createContext('level = "info" | ');
      const suggestions = autocomplete.getSuggestions(context);

      const fieldSuggestion = suggestions.find(s => s.label === 'timestamp');
      expect(fieldSuggestion?.detail).toBe('Select string field');
    });
  });

  describe('Context-Aware Filtering', () => {
    it('should prioritize exact prefix matches', () => {
      const context = createContext('lev', 3);
      const suggestions = autocomplete.getSuggestions(context);

      // 'level' should come before other matches
      const levelIndex = suggestions.findIndex(s => s.label === 'level');
      expect(levelIndex).toBeGreaterThanOrEqual(0);

      if (levelIndex > 0) {
        // Any suggestion before 'level' should also start with 'lev'
        expect(suggestions[0].label.toLowerCase().startsWith('lev')).toBe(true);
      }
    });

    it('should handle partial tokens correctly', () => {
      const context = createContext('mes', 3);
      const suggestions = autocomplete.getSuggestions(context);

      expect(suggestions.some(s => s.label === 'message')).toBe(true);
    });
  });

  describe('Custom Fields Integration', () => {
    it('should accept and suggest custom fields', () => {
      const customFields: FieldInfo[] = [
        { name: 'custom_field', type: 'string', description: 'A custom field' },
        { name: 'user_data', type: 'object', description: 'User data object' }
      ];

      const customAutocomplete = new LogChefQLAutocomplete(customFields);
      const context = createContext('');
      const suggestions = customAutocomplete.getSuggestions(context);

      expect(suggestions.some(s => s.label === 'custom_field')).toBe(true);
      expect(suggestions.some(s => s.label === 'user_data')).toBe(true);
    });

    it('should update custom fields dynamically', () => {
      const autocompleteInstance = new LogChefQLAutocomplete(testFields);

      const newFields: FieldInfo[] = [
        ...testFields,
        { name: 'dynamic_field', type: 'number', description: 'A dynamically added field' }
      ];

      autocompleteInstance.updateCustomFields(newFields);

      const context = createContext('');
      const suggestions = autocompleteInstance.getSuggestions(context);

      expect(suggestions.some(s => s.label === 'dynamic_field')).toBe(true);
    });
  });

  describe('Parentheses and Grouping', () => {
    it('should suggest parentheses for grouping', () => {
      const context = createContext('');
      const suggestions = autocomplete.getSuggestions(context);

      expect(suggestions.some(s => s.label === '()')).toBe(true);
    });

    it('should provide grouping documentation', () => {
      const context = createContext('');
      const suggestions = autocomplete.getSuggestions(context);

      const parenSuggestion = suggestions.find(s => s.label === '()');
      expect(parenSuggestion?.kind).toBe('keyword');
      expect(parenSuggestion?.detail).toBe('Grouping parentheses');
      expect(parenSuggestion?.documentation).toBe('Group conditions together');
    });
  });

  describe('Error Tolerance', () => {
    it('should handle empty queries gracefully', () => {
      const context = createContext('');
      const suggestions = autocomplete.getSuggestions(context);

      expect(suggestions.length).toBeGreaterThan(0);
      expect(() => autocomplete.getSuggestions(context)).not.toThrow();
    });

    it('should handle malformed queries', () => {
      const context = createContext('field = = invalid');

      expect(() => autocomplete.getSuggestions(context)).not.toThrow();
    });

    it('should handle queries with no tokens', () => {
      const context = createContext('   ');

      expect(() => autocomplete.getSuggestions(context)).not.toThrow();
    });
  });

  describe('Insert Text Generation', () => {
    it('should provide appropriate insert text for operators', () => {
      const context = createContext('field ');
      const suggestions = autocomplete.getSuggestions(context);

      const equalsSuggestion = suggestions.find(s => s.label === '=');
      expect(equalsSuggestion?.insertText).toBe('= ');
    });

    it('should provide template insert text for values', () => {
      const context = createContext('field = ');
      const suggestions = autocomplete.getSuggestions(context);

      const stringSuggestion = suggestions.find(s => s.label === '"string"');
      expect(stringSuggestion?.insertText).toBe('"${1:value}"');
    });

    it('should provide spaced insert text for boolean operators', () => {
      const context = createContext('field = "value" ');
      const suggestions = autocomplete.getSuggestions(context);

      const andSuggestion = suggestions.find(s => s.label === 'and');
      expect(andSuggestion?.insertText).toBe(' and ');
    });
  });
});