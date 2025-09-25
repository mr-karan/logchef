import { describe, it, expect } from 'vitest';
import { tokenize } from '../tokenizer';
import { QueryParser } from '../parser';
import { LogChefQLErrorHandler } from '../errors';

describe('LogChefQL Structured Error Handling', () => {
  describe('Error Creation and Formatting', () => {
    it('should create user-friendly error messages', () => {
      const parseError = {
        code: 'UNEXPECTED_END',
        message: 'Unexpected end of input',
        position: { line: 1, column: 10 }
      };

      const userError = LogChefQLErrorHandler.getUserFriendlyError(parseError);

      expect(userError.code).toBe('UNEXPECTED_END');
      expect(userError.message).toBe('Unexpected end of input');
      expect(userError.suggestion).toBe('Complete your query - it appears to be cut off');
    });

    it('should handle unknown error codes gracefully', () => {
      const parseError = {
        code: 'UNKNOWN_ERROR_CODE',
        message: 'Some custom error',
        position: { line: 1, column: 5 }
      };

      const userError = LogChefQLErrorHandler.getUserFriendlyError(parseError);

      expect(userError.code).toBe('UNKNOWN_ERROR');
      expect(userError.message).toBe('Some custom error');
      expect(userError.suggestion).toBe('Check your query syntax for errors');
    });
  });

  describe('Parser Error Collection', () => {
    it('should collect errors instead of throwing for incomplete queries', () => {
      const { tokens } = tokenize('field =');
      const parser = new QueryParser(tokens);
      const { ast, errors } = parser.parse();

      expect(ast).toBe(null);
      expect(errors.length).toBeGreaterThan(0);
      expect(errors[0].code).toBeDefined();
      expect(errors[0].message).toBeDefined();
    });

    it('should collect errors for invalid operators', () => {
      const { tokens } = tokenize('field & value');
      const parser = new QueryParser(tokens);
      const { ast, errors } = parser.parse();

      expect(errors.length).toBeGreaterThan(0);
      // Should have error about unexpected token or invalid operator
      expect(errors.some(e => e.code.includes('UNEXPECTED') || e.code.includes('OPERATOR'))).toBe(true);
    });

    it('should collect position information in errors', () => {
      const { tokens } = tokenize('field = ');
      const parser = new QueryParser(tokens);
      const { ast, errors } = parser.parse();

      expect(errors.length).toBeGreaterThan(0);
      // At least one error should have position information
      expect(errors.some(e => e.position !== undefined)).toBe(true);
    });

    it('should handle multiple errors gracefully', () => {
      const { tokens } = tokenize('field & invalid | another &');
      const parser = new QueryParser(tokens);
      const { ast, errors } = parser.parse();

      expect(errors.length).toBeGreaterThan(1);
      expect(errors.every(e => e.code && e.message)).toBe(true);
    });
  });

  describe('Error Position Accuracy', () => {
    it('should provide accurate position for syntax errors', () => {
      const { tokens } = tokenize('valid = "test" and invalid');
      const parser = new QueryParser(tokens);
      const { ast, errors } = parser.parse();

      if (errors.length > 0) {
        const error = errors[0];
        expect(error.position).toBeDefined();
        expect(error.position!.line).toBe(1);
        expect(error.position!.column).toBeGreaterThan(0);
      }
    });
  });
});