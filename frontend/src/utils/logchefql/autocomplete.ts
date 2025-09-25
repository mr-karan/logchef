import type { Token } from './types';

export interface AutocompleteContext {
  query: string;
  position: number;
  tokens: Token[];
  currentToken?: Token;
  previousToken?: Token;
  line: number;
  column: number;
}

export interface AutocompleteSuggestion {
  label: string;
  kind: 'keyword' | 'operator' | 'field' | 'value' | 'function';
  insertText: string;
  detail?: string;
  documentation?: string;
  sortText?: string;
}

export interface FieldInfo {
  name: string;
  type: 'string' | 'number' | 'boolean' | 'object' | 'array';
  description?: string;
  examples?: string[];
}

export class LogChefQLAutocomplete {
  private commonFields: FieldInfo[] = [
    // Removed hardcoded schema-specific fields like log_attributes
    // These should come from the actual schema to avoid duplicates
  ];

  private allFields: FieldInfo[] = [];

  private operators = [
    { label: '=', kind: 'operator' as const, insertText: '= ', detail: 'Equals', documentation: 'Exact match' },
    { label: '!=', kind: 'operator' as const, insertText: '!= ', detail: 'Not equals', documentation: 'Does not match' },
    { label: '~', kind: 'operator' as const, insertText: '~ ', detail: 'Regex match', documentation: 'Regular expression match' },
    { label: '!~', kind: 'operator' as const, insertText: '!~ ', detail: 'Regex not match', documentation: 'Does not match regex' },
    { label: '>', kind: 'operator' as const, insertText: '> ', detail: 'Greater than', documentation: 'Numeric/string comparison' },
    { label: '<', kind: 'operator' as const, insertText: '< ', detail: 'Less than', documentation: 'Numeric/string comparison' },
    { label: '>=', kind: 'operator' as const, insertText: '>= ', detail: 'Greater or equal', documentation: 'Numeric/string comparison' },
    { label: '<=', kind: 'operator' as const, insertText: '<= ', detail: 'Less or equal', documentation: 'Numeric/string comparison' }
  ];

  private booleanOperators = [
    { label: 'and', kind: 'keyword' as const, insertText: ' and ', detail: 'Logical AND', documentation: 'Both conditions must be true' },
    { label: 'or', kind: 'keyword' as const, insertText: ' or ', detail: 'Logical OR', documentation: 'Either condition must be true' }
  ];

  private pipeOperators = [
    { label: '|', kind: 'operator' as const, insertText: ' | ', detail: 'Pipe to SELECT', documentation: 'Select specific fields from results' }
  ];

  constructor(private customFields: FieldInfo[] = []) {
    this.updateAllFields();
  }

  private updateAllFields(): void {
    // Merge common fields with custom fields, avoiding duplicates
    const fieldMap = new Map<string, FieldInfo>();

    // Add common fields first
    this.commonFields.forEach(field => fieldMap.set(field.name, field));

    // Add/override with custom fields (schema fields take precedence)
    this.customFields.forEach(field => fieldMap.set(field.name, field));

    this.allFields = Array.from(fieldMap.values());
  }

  public getSuggestions(context: AutocompleteContext): AutocompleteSuggestion[] {
    const suggestions: AutocompleteSuggestion[] = [];

    // Analyze context to determine what suggestions to provide
    const contextType = this.determineContextType(context);
    // console.log('Context type:', contextType, 'for query:', context.query);

    switch (contextType) {
      case 'field':
        suggestions.push(...this.getFieldSuggestions(context));
        // Only add parentheses if we have actual fields to work with
        if ((context.tokens.length === 0 || !context.currentToken) && this.allFields.length > 0) {
          suggestions.push({
            label: '()',
            kind: 'keyword' as const,
            insertText: '(${1:condition})',
            detail: 'Grouping parentheses',
            documentation: 'Group conditions together',
            sortText: 'keyword_paren'
          });
        }
        break;
      case 'operator':
        suggestions.push(...this.getOperatorSuggestions(context));
        break;
      case 'value':
        suggestions.push(...this.getValueSuggestions(context));
        break;
      case 'boolean':
        suggestions.push(...this.getBooleanOperatorSuggestions(context));
        break;
      case 'pipe':
        suggestions.push(...this.getPipeOperatorSuggestions(context));
        suggestions.push(...this.getBooleanOperatorSuggestions(context));
        break;
      case 'select':
        suggestions.push(...this.getSelectFieldSuggestions(context));
        break;
      default:
        // General suggestions when context is unclear
        suggestions.push(...this.getGeneralSuggestions(context));
    }

    // Filter and sort suggestions based on current input
    return this.filterAndSortSuggestions(suggestions, context);
  }

  private determineContextType(context: AutocompleteContext): string {
    const { currentToken, previousToken, tokens } = context;

    // Check if we're after a pipe operator (SELECT context)
    const pipeIndex = tokens.findIndex(token => token.type === 'pipe');
    if (pipeIndex !== -1 && context.position > tokens[pipeIndex].position.column) {
      return 'select';
    }

    // Check if we're at a position where pipe operator makes sense (before checking boolean)
    if (this.shouldSuggestPipe(context)) {
      return 'pipe';
    }

    // Determine context based on previous token
    if (previousToken) {
      switch (previousToken.type) {
        case 'key':
          return 'operator';
        case 'operator':
          return 'value';
        case 'value':
        case 'paren':
          return 'boolean';
      }
    }

    // Default to field suggestions
    return 'field';
  }

  private getFieldSuggestions(context: AutocompleteContext): AutocompleteSuggestion[] {
    // Check if user is trying to access nested fields by examining tokens
    if (context.tokens.length > 0) {
      const lastToken = context.tokens[context.tokens.length - 1];
      if (lastToken?.type === 'key' && lastToken.value.includes('.')) {
        // Extract base field name (everything before the dot)
        const dotIndex = lastToken.value.indexOf('.');
        if (dotIndex > 0) {
          const baseField = lastToken.value.substring(0, dotIndex);
          const field = this.allFields.find(f => f.name === baseField);

          if (field && (field.type === 'object' || field.description?.includes('Map field'))) {
            // This is a nested field access, but we don't have log samples to suggest keys
            return [{
              label: '(log samples needed)',
              kind: 'field' as const,
              insertText: '',
              detail: 'Nested field suggestions',
              documentation: `To suggest nested fields for ${baseField}, log samples are needed. Available keys depend on your actual log data.`,
              sortText: 'zzz_no_suggestions'
            }];
          }
        }
      }
    }

    // Regular field suggestions
    return this.allFields.map(field => ({
      label: field.name,
      kind: 'field' as const,
      insertText: field.name,
      detail: `${field.type} field`,
      documentation: field.description,
      sortText: `field_${field.name}`
    }));
  }

  private getOperatorSuggestions(context: AutocompleteContext): AutocompleteSuggestion[] {
    return this.operators.map(op => ({
      ...op,
      sortText: `op_${op.label}`
    }));
  }

  private getValueSuggestions(context: AutocompleteContext): AutocompleteSuggestion[] {
    const suggestions: AutocompleteSuggestion[] = [];

    // Get field type from previous tokens to suggest appropriate values
    const fieldToken = this.getFieldTokenFromContext(context);
    if (fieldToken) {
      const field = this.allFields.find(f => f.name === fieldToken.value);
      if (field && field.examples) {
        suggestions.push(...field.examples.map(example => ({
          label: `"${example}"`,
          kind: 'value' as const,
          insertText: `"${example}"`,
          detail: `Example ${field.type} value`,
          sortText: `value_${example}`
        })));
      }
    }

    // Add common value patterns
    suggestions.push(
      { label: 'true', kind: 'value' as const, insertText: 'true', detail: 'Boolean true', sortText: 'value_true' },
      { label: 'false', kind: 'value' as const, insertText: 'false', detail: 'Boolean false', sortText: 'value_false' },
      { label: 'null', kind: 'value' as const, insertText: 'null', detail: 'Null value', sortText: 'value_null' },
      { label: '"string"', kind: 'value' as const, insertText: '"${1:value}"', detail: 'String literal', sortText: 'value_string' }
    );

    return suggestions;
  }

  private getBooleanOperatorSuggestions(context: AutocompleteContext): AutocompleteSuggestion[] {
    return this.booleanOperators.map(op => ({
      ...op,
      sortText: `bool_${op.label}`
    }));
  }

  private getPipeOperatorSuggestions(context: AutocompleteContext): AutocompleteSuggestion[] {
    return this.pipeOperators.map(op => ({
      ...op,
      sortText: `pipe_${op.label}`
    }));
  }

  private getSelectFieldSuggestions(context: AutocompleteContext): AutocompleteSuggestion[] {
    // Similar to field suggestions but for SELECT context
    return this.allFields.map(field => ({
      label: field.name,
      kind: 'field' as const,
      insertText: field.name,
      detail: `Select ${field.type} field`,
      documentation: field.description,
      sortText: `select_${field.name}`
    }));
  }

  private getGeneralSuggestions(context: AutocompleteContext): AutocompleteSuggestion[] {
    // Return a mix of field and keyword suggestions
    const suggestions: AutocompleteSuggestion[] = [];

    // Add field suggestions
    suggestions.push(...this.getFieldSuggestions(context));

    // Only add parentheses if we have actual fields
    if (this.allFields.length > 0) {
      suggestions.push({
        label: '()',
        kind: 'keyword' as const,
        insertText: '(${1:condition})',
        detail: 'Grouping parentheses',
        documentation: 'Group conditions together',
        sortText: 'keyword_paren'
      });
    }

    return suggestions;
  }

  private shouldSuggestPipe(context: AutocompleteContext): boolean {
    // Suggest pipe if we have a complete WHERE clause and no pipe yet
    const { tokens } = context;
    const hasPipe = tokens.some(token => token.type === 'pipe');

    if (hasPipe) return false;

    // Simple heuristic: if we have field-operator-value pattern, suggest pipe
    const hasCompleteExpression = tokens.length >= 3 &&
      tokens.some(t => t.type === 'key') &&
      tokens.some(t => t.type === 'operator') &&
      tokens.some(t => t.type === 'value');

    return hasCompleteExpression;
  }

  private getFieldTokenFromContext(context: AutocompleteContext): Token | undefined {
    // Look backwards from current position to find the field token
    const { tokens } = context;

    for (let i = tokens.length - 1; i >= 0; i--) {
      if (tokens[i].type === 'key') {
        return tokens[i];
      }
      if (tokens[i].type === 'operator') {
        // Look for key token before this operator
        for (let j = i - 1; j >= 0; j--) {
          if (tokens[j].type === 'key') {
            return tokens[j];
          }
        }
      }
    }

    return undefined;
  }

  private filterAndSortSuggestions(
    suggestions: AutocompleteSuggestion[],
    context: AutocompleteContext
  ): AutocompleteSuggestion[] {
    const { currentToken } = context;

    if (!currentToken) {
      return suggestions.sort((a, b) => (a.sortText || a.label).localeCompare(b.sortText || b.label));
    }

    // Don't filter special nested field messages
    const specialSuggestions = suggestions.filter(s => s.label.startsWith('('));
    if (specialSuggestions.length > 0) {
      return specialSuggestions;
    }

    const prefix = currentToken.value.toLowerCase();

    // Filter suggestions that match the current prefix
    const filtered = suggestions.filter(suggestion =>
      suggestion.label.toLowerCase().startsWith(prefix) ||
      suggestion.label.toLowerCase().includes(prefix)
    );

    // Sort by relevance: exact prefix matches first, then contains matches
    return filtered.sort((a, b) => {
      const aLabel = a.label.toLowerCase();
      const bLabel = b.label.toLowerCase();

      const aExact = aLabel.startsWith(prefix);
      const bExact = bLabel.startsWith(prefix);

      if (aExact && !bExact) return -1;
      if (!aExact && bExact) return 1;

      return (a.sortText || a.label).localeCompare(b.sortText || b.label);
    });
  }

  public updateCustomFields(fields: FieldInfo[]): void {
    this.customFields = fields;
    this.updateAllFields();
  }
}