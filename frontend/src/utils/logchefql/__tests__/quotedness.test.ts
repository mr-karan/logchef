import { describe, it, expect } from 'vitest';
import { tokenize } from '../tokenizer';
import { QueryParser } from '../parser';

describe('Value Quotedness Preservation', () => {
  describe('Tokenizer quotedness tracking', () => {
    it('should mark quoted values as quoted', () => {
      const { tokens } = tokenize('field = "123"');

      const valueToken = tokens.find(t => t.type === 'value');
      expect(valueToken).toBeDefined();
      expect(valueToken?.quoted).toBe(true);
      expect(valueToken?.value).toBe('123');
    });

    it('should not mark unquoted values as quoted', () => {
      const { tokens } = tokenize('field = 123');

      // Unquoted values like '123' are tokenized as 'key' type due to alphanumeric content
      const valueToken = tokens.find(t => t.value === '123');
      expect(valueToken).toBeDefined();
      expect(valueToken?.quoted).toBeUndefined(); // quoted property not present for unquoted tokens
      expect(valueToken?.value).toBe('123');
    });

    it('should handle single quotes correctly', () => {
      const { tokens } = tokenize("field = '456'");

      const valueToken = tokens.find(t => t.type === 'value');
      expect(valueToken).toBeDefined();
      expect(valueToken?.quoted).toBe(true);
      expect(valueToken?.value).toBe('456');
    });
  });

  describe('Parser value coercion based on quotedness', () => {
    it('should preserve quoted numeric strings as strings', () => {
      const { tokens } = tokenize('field = "123"');
      const parser = new QueryParser(tokens);
      const { ast } = parser.parse();

      expect(ast?.type).toBe('expression');
      if (ast?.type === 'expression') {
        expect(ast.value).toBe('123'); // String, not number
        expect(typeof ast.value).toBe('string');
      }
    });

    it('should coerce unquoted numeric strings to numbers', () => {
      const { tokens } = tokenize('field = 123');
      const parser = new QueryParser(tokens);
      const { ast } = parser.parse();

      expect(ast?.type).toBe('expression');
      if (ast?.type === 'expression') {
        expect(ast.value).toBe(123); // Number, not string
        expect(typeof ast.value).toBe('number');
      }
    });

    it('should preserve quoted boolean strings as strings', () => {
      const { tokens } = tokenize('field = "true"');
      const parser = new QueryParser(tokens);
      const { ast } = parser.parse();

      expect(ast?.type).toBe('expression');
      if (ast?.type === 'expression') {
        expect(ast.value).toBe('true'); // String, not boolean
        expect(typeof ast.value).toBe('string');
      }
    });

    it('should coerce unquoted boolean strings to booleans', () => {
      const { tokens } = tokenize('field = true');
      const parser = new QueryParser(tokens);
      const { ast } = parser.parse();

      expect(ast?.type).toBe('expression');
      if (ast?.type === 'expression') {
        expect(ast.value).toBe(true); // Boolean, not string
        expect(typeof ast.value).toBe('boolean');
      }
    });

    it('should preserve quoted null strings as strings', () => {
      const { tokens } = tokenize('field = "null"');
      const parser = new QueryParser(tokens);
      const { ast } = parser.parse();

      expect(ast?.type).toBe('expression');
      if (ast?.type === 'expression') {
        expect(ast.value).toBe('null'); // String, not null
        expect(typeof ast.value).toBe('string');
      }
    });

    it('should coerce unquoted null to null', () => {
      const { tokens } = tokenize('field = null');
      const parser = new QueryParser(tokens);
      const { ast } = parser.parse();

      expect(ast?.type).toBe('expression');
      if (ast?.type === 'expression') {
        expect(ast.value).toBe(null); // null, not string
      }
    });

    it('should handle decimal numbers correctly', () => {
      const { tokens: quotedTokens } = tokenize('field = "3.14"');
      const quotedParser = new QueryParser(quotedTokens);
      const { ast: quotedAst } = quotedParser.parse();

      const { tokens: unquotedTokens } = tokenize('field = 3.14');
      const unquotedParser = new QueryParser(unquotedTokens);
      const { ast: unquotedAst } = unquotedParser.parse();

      expect(quotedAst?.type).toBe('expression');
      expect(unquotedAst?.type).toBe('expression');

      if (quotedAst?.type === 'expression') {
        expect(quotedAst.value).toBe('3.14'); // String
        expect(typeof quotedAst.value).toBe('string');
      }

      if (unquotedAst?.type === 'expression') {
        expect(unquotedAst.value).toBe(3.14); // Number
        expect(typeof unquotedAst.value).toBe('number');
      }
    });

    it('should handle large integers correctly', () => {
      const largeInt = '999999999999999999999'; // Beyond safe integer range

      const { tokens: quotedTokens } = tokenize(`field = "${largeInt}"`);
      const quotedParser = new QueryParser(quotedTokens);
      const { ast: quotedAst } = quotedParser.parse();

      const { tokens: unquotedTokens } = tokenize(`field = ${largeInt}`);
      const unquotedParser = new QueryParser(unquotedTokens);
      const { ast: unquotedAst } = unquotedParser.parse();

      expect(quotedAst?.type).toBe('expression');
      expect(unquotedAst?.type).toBe('expression');

      if (quotedAst?.type === 'expression') {
        expect(quotedAst.value).toBe(largeInt); // String
        expect(typeof quotedAst.value).toBe('string');
      }

      if (unquotedAst?.type === 'expression') {
        // Large integer should remain as string even when unquoted (safety)
        expect(unquotedAst.value).toBe(largeInt); // String
        expect(typeof unquotedAst.value).toBe('string');
      }
    });

    it('should handle mixed types in complex expressions', () => {
      const { tokens } = tokenize('field1 = "123" and field2 = 456 and field3 = "true" and field4 = false');
      const parser = new QueryParser(tokens);
      const { ast } = parser.parse();

      expect(ast?.type).toBe('logical');

      if (ast?.type === 'logical') {
        const expressions = ast.children.filter(child => child.type === 'expression');
        expect(expressions).toHaveLength(4);

        // field1 = "123" - quoted numeric string
        if (expressions[0].type === 'expression') {
          expect(expressions[0].value).toBe('123');
          expect(typeof expressions[0].value).toBe('string');
        }

        // field2 = 456 - unquoted numeric
        if (expressions[1].type === 'expression') {
          expect(expressions[1].value).toBe(456);
          expect(typeof expressions[1].value).toBe('number');
        }

        // field3 = "true" - quoted boolean string
        if (expressions[2].type === 'expression') {
          expect(expressions[2].value).toBe('true');
          expect(typeof expressions[2].value).toBe('string');
        }

        // field4 = false - unquoted boolean
        if (expressions[3].type === 'expression') {
          expect(expressions[3].value).toBe(false);
          expect(typeof expressions[3].value).toBe('boolean');
        }
      }
    });
  });

  describe('AST quotedness preservation', () => {
    it('should preserve quotedness in expression AST nodes', () => {
      const { tokens: quotedTokens } = tokenize('field = "123"');
      const quotedParser = new QueryParser(quotedTokens);
      const { ast: quotedAst } = quotedParser.parse();

      const { tokens: unquotedTokens } = tokenize('field = 123');
      const unquotedParser = new QueryParser(unquotedTokens);
      const { ast: unquotedAst } = unquotedParser.parse();

      expect(quotedAst?.type).toBe('expression');
      expect(unquotedAst?.type).toBe('expression');

      if (quotedAst?.type === 'expression') {
        expect(quotedAst.quoted).toBe(true); // Quoted value should have quoted: true
      }

      if (unquotedAst?.type === 'expression') {
        expect(unquotedAst.quoted).toBeUndefined(); // Unquoted value should not have quoted property
      }
    });

    it('should handle mixed quotedness in complex expressions', () => {
      const { tokens } = tokenize('field1 = "quoted" and field2 = unquoted');
      const parser = new QueryParser(tokens);
      const { ast } = parser.parse();

      expect(ast?.type).toBe('logical');
      if (ast?.type === 'logical') {
        const expressions = ast.children.filter(child => child.type === 'expression');
        expect(expressions).toHaveLength(2);

        // First expression: field1 = "quoted"
        if (expressions[0].type === 'expression') {
          expect(expressions[0].quoted).toBe(true);
          expect(expressions[0].value).toBe('quoted');
        }

        // Second expression: field2 = unquoted
        if (expressions[1].type === 'expression') {
          expect(expressions[1].quoted).toBeUndefined();
          expect(expressions[1].value).toBe('unquoted');
        }
      }
    });
  });
});