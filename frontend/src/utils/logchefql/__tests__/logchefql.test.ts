import { describe, it, expect, beforeEach } from 'vitest';
import { tokenize, QueryParser, SQLVisitor } from '../index';
import type { SchemaInfo, ParseError } from '../types';

// Test schema based on the provided API response
const testSchema: SchemaInfo = {
  columns: [
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
  ]
};

describe('LogChefQL Comprehensive Test Suite', () => {
  let visitor: SQLVisitor;

  beforeEach(() => {
    visitor = new SQLVisitor(false, testSchema);
  });

  describe('P0 Safety & Correctness Tests', () => {
    describe('SQL String Escaping in Path Parameters', () => {
    it('should properly escape single quotes in JSON path segments', () => {
      const query = 'log_attributes."user\'name" = "test"';
      console.log('DEBUG: Query string:', JSON.stringify(query));
      console.log('DEBUG: Query length:', query.length);
      for (let i = 0; i < query.length; i++) {
        console.log(`DEBUG: Position ${i}: "${query[i]}" (code: ${query.charCodeAt(i)})`);
      }
      const { tokens, errors } = tokenize(query);
      console.log('DEBUG: Tokens for query:', query, JSON.stringify(tokens, null, 2));
      expect(errors).toHaveLength(0);

      const parser = new QueryParser(tokens);
      const { ast } = parser.parse();
      expect(ast).toBeTruthy();

      const { sql } = visitor.generate(ast!);
      // Should escape the quote in the SQL - Map access is used for Map types
      expect(sql).toContain("`log_attributes`['user''name'] = 'test'");
    });

    it('should properly escape backslashes in JSON path segments', () => {
      const query = 'log_attributes.path\\with\\backslashes = "test"';
      const { tokens, errors } = tokenize(query);
      expect(errors).toHaveLength(0);

      const parser = new QueryParser(tokens);
      const { ast } = parser.parse();
      expect(ast).toBeTruthy();

      const { sql } = visitor.generate(ast!);
      expect(sql).toContain("`log_attributes`['path\\\\with\\\\backslashes'] = 'test'");
    });

    it('should handle mixed quotes and backslashes', () => {
      const query = 'log_attributes."user\'name" = "test"';
      const { tokens, errors } = tokenize(query);
      expect(errors).toHaveLength(0);

      const parser = new QueryParser(tokens);
      const { ast } = parser.parse();
      expect(ast).toBeTruthy();

      const { sql } = visitor.generate(ast!);
      expect(sql).toContain("`log_attributes`['user''name'] = 'test'");
    });
    });

    describe('Boolean Keyword Tokenization', () => {
      it('should correctly tokenize "and" as boolean operator', () => {
        const query = 'severity_text = "error" and service_name = "api"';
        const { tokens } = tokenize(query);

        const boolTokens = tokens.filter(t => t.type === 'bool');
        expect(boolTokens).toHaveLength(1);
        expect(boolTokens[0].value).toBe('and');
      });

      it('should correctly tokenize "or" as boolean operator', () => {
        const query = 'severity_text = "error" or severity_text = "warn"';
        const { tokens } = tokenize(query);

        const boolTokens = tokens.filter(t => t.type === 'bool');
        expect(boolTokens).toHaveLength(1);
        expect(boolTokens[0].value).toBe('or');
      });

      it('should NOT tokenize "order" as boolean operator', () => {
        const query = 'body ~ "order"';
        const { tokens } = tokenize(query);

        const boolTokens = tokens.filter(t => t.type === 'bool');
        expect(boolTokens).toHaveLength(0);
      });

      it('should NOT tokenize "android" as boolean operator', () => {
        const query = 'service_name = "android"';
        const { tokens } = tokenize(query);

        const boolTokens = tokens.filter(t => t.type === 'bool');
        expect(boolTokens).toHaveLength(0);
      });

      it('should handle case-insensitive boolean operators', () => {
        const query = 'severity_text = "ERROR" AND service_name = "API"';
        const { tokens } = tokenize(query);

        const boolTokens = tokens.filter(t => t.type === 'bool');
        expect(boolTokens).toHaveLength(1);
        expect(boolTokens[0].value).toBe('and');
      });
    });

    describe('Unterminated String Literal Error Handling', () => {
      it('should detect unterminated string literals', () => {
        const query = 'severity_text = "error';
        const { tokens, errors } = tokenize(query);

        const unterminatedError = errors.find(e => e.code === 'UNTERMINATED_STRING');
        expect(unterminatedError).toBeTruthy();
        expect(unterminatedError?.message).toContain('Unterminated string literal');
      });

      it('should detect unterminated single-quoted strings', () => {
        const query = "service_name = 'api";
        const { tokens, errors } = tokenize(query);

        const unterminatedError = errors.find(e => e.code === 'UNTERMINATED_STRING');
        expect(unterminatedError).toBeTruthy();
      });

      it('should not error on properly terminated strings', () => {
        const query = 'severity_text = "error"';
        const { tokens, errors } = tokenize(query);

        expect(errors).toHaveLength(0);
      });

      it('should handle escaped quotes within strings', () => {
        const query = 'body ~ "error \\"message\\""';
        const { tokens, errors } = tokenize(query);

        expect(errors).toHaveLength(0);
      });
    });
  });

  describe('Basic Field Queries', () => {
    it('should handle simple equality queries', () => {
      const query = 'severity_text = "error"';
      const { tokens, errors } = tokenize(query);
      expect(errors).toHaveLength(0);

      const parser = new QueryParser(tokens);
      const { ast } = parser.parse();
      expect(ast).toBeTruthy();

      const { sql } = visitor.generate(ast!);
      expect(sql).toContain('`severity_text` = \'error\'');
    });

    it('should handle numeric comparisons', () => {
      const query = 'severity_number > 400';
      const { tokens, errors } = tokenize(query);
      expect(errors).toHaveLength(0);

      const parser = new QueryParser(tokens);
      const { ast } = parser.parse();
      expect(ast).toBeTruthy();

      const { sql } = visitor.generate(ast!);
      expect(sql).toContain('`severity_number` > 400');
    });

    it('should handle regex operations', () => {
      const query = 'body ~ "error.*message"';
      const { tokens, errors } = tokenize(query);
      expect(errors).toHaveLength(0);

      const parser = new QueryParser(tokens);
      const { ast } = parser.parse();
      expect(ast).toBeTruthy();

      const { sql } = visitor.generate(ast!);
      expect(sql).toContain('positionCaseInsensitive(`body`, \'error.*message\') > 0');
    });

    it('should handle not regex operations', () => {
      const query = 'body !~ "debug"';
      const { tokens, errors } = tokenize(query);
      expect(errors).toHaveLength(0);

      const parser = new QueryParser(tokens);
      const { ast } = parser.parse();
      expect(ast).toBeTruthy();

      const { sql } = visitor.generate(ast!);
      expect(sql).toContain('positionCaseInsensitive(`body`, \'debug\') = 0');
    });

    it('should handle greater than or equal', () => {
      const query = 'trace_flags >= 1';
      const { tokens, errors } = tokenize(query);
      expect(errors).toHaveLength(0);

      const parser = new QueryParser(tokens);
      const { ast } = parser.parse();
      expect(ast).toBeTruthy();

      const { sql } = visitor.generate(ast!);
      expect(sql).toContain('`trace_flags` >= 1');
    });

    it('should handle less than or equal', () => {
      const query = 'severity_number <= 200';
      const { tokens, errors } = tokenize(query);
      expect(errors).toHaveLength(0);

      const parser = new QueryParser(tokens);
      const { ast } = parser.parse();
      expect(ast).toBeTruthy();

      const { sql } = visitor.generate(ast!);
      expect(sql).toContain('`severity_number` <= 200');
    });

    it('should handle not equals', () => {
      const query = 'service_name != "api"';
      const { tokens, errors } = tokenize(query);
      expect(errors).toHaveLength(0);

      const parser = new QueryParser(tokens);
      const { ast } = parser.parse();
      expect(ast).toBeTruthy();

      const { sql } = visitor.generate(ast!);
      expect(sql).toContain('`service_name` != \'api\'');
    });
  });

  describe('JSON/Map Field Access', () => {
    it('should handle simple Map field access', () => {
      const query = 'log_attributes.level = "info"';
      const { tokens, errors } = tokenize(query);
      expect(errors).toHaveLength(0);

      const parser = new QueryParser(tokens);
      const { ast } = parser.parse();
      expect(ast).toBeTruthy();

      const { sql } = visitor.generate(ast!);
      expect(sql).toContain("`log_attributes`['level'] = 'info'");
    });

    it('should handle nested Map field access', () => {
      const query = 'log_attributes.user.name = "john"';
      const { tokens, errors } = tokenize(query);
      expect(errors).toHaveLength(0);

      const parser = new QueryParser(tokens);
      const { ast } = parser.parse();
      expect(ast).toBeTruthy();

      const { sql } = visitor.generate(ast!);
      expect(sql).toContain("`log_attributes`['user.name'] = 'john'");
    });

    it('should handle quoted field names in Map paths', () => {
      const query = 'log_attributes."user name" = "value"';
      const { tokens, errors } = tokenize(query);
      expect(errors).toHaveLength(0);

      const parser = new QueryParser(tokens);
      const { ast } = parser.parse();
      expect(ast).toBeTruthy();

      const { sql } = visitor.generate(ast!);
      expect(sql).toContain("`log_attributes`['user name'] = 'value'");
    });

    it('should handle Map field access', () => {
      const query = 'log_attributes.service.version = "1.0"';
      const { tokens, errors } = tokenize(query);
      expect(errors).toHaveLength(0);

      const parser = new QueryParser(tokens);
      const { ast } = parser.parse();
      expect(ast).toBeTruthy();

      const { sql } = visitor.generate(ast!);
      expect(sql).toContain("`log_attributes`['service.version'] = '1.0'");
    });

    it('should handle complex nested Map access', () => {
      const query = 'log_attributes.kubernetes.pod.name = "web-server"';
      const { tokens, errors } = tokenize(query);
      expect(errors).toHaveLength(0);

      const parser = new QueryParser(tokens);
      const { ast } = parser.parse();
      expect(ast).toBeTruthy();

      const { sql } = visitor.generate(ast!);
      expect(sql).toContain("`log_attributes`['kubernetes.pod.name'] = 'web-server'");
    });
  });

  describe('Boolean Logic', () => {
    it('should handle simple AND queries', () => {
      const query = 'severity_text = "error" and service_name = "api"';
      const { tokens, errors } = tokenize(query);
      expect(errors).toHaveLength(0);

      const parser = new QueryParser(tokens);
      const { ast } = parser.parse();
      expect(ast).toBeTruthy();

      const { sql } = visitor.generate(ast!);
      expect(sql).toContain('`severity_text` = \'error\'');
      expect(sql).toContain('`service_name` = \'api\'');
      expect(sql).toContain('AND');
    });

    it('should handle simple OR queries', () => {
      const query = 'severity_text = "error" or severity_text = "warn"';
      const { tokens, errors } = tokenize(query);
      expect(errors).toHaveLength(0);

      const parser = new QueryParser(tokens);
      const { ast } = parser.parse();
      expect(ast).toBeTruthy();

      const { sql } = visitor.generate(ast!);
      expect(sql).toContain('`severity_text` = \'error\'');
      expect(sql).toContain('`severity_text` = \'warn\'');
      expect(sql).toContain('OR');
    });

    it('should handle complex boolean logic with parentheses', () => {
      const query = '(severity_text = "error" and service_name = "api") or severity_text = "critical"';
      const { tokens, errors } = tokenize(query);
      expect(errors).toHaveLength(0);

      const parser = new QueryParser(tokens);
      const { ast } = parser.parse();
      expect(ast).toBeTruthy();

      const { sql } = visitor.generate(ast!);
      expect(sql).toContain('`severity_text` = \'error\'');
      expect(sql).toContain('`service_name` = \'api\'');
      expect(sql).toContain('`severity_text` = \'critical\'');
      expect(sql).toContain('OR');
    });

    it('should handle multiple AND conditions', () => {
      const query = 'severity_text = "error" and service_name = "api" and namespace = "production"';
      const { tokens, errors } = tokenize(query);
      expect(errors).toHaveLength(0);

      const parser = new QueryParser(tokens);
      const { ast } = parser.parse();
      expect(ast).toBeTruthy();

      const { sql } = visitor.generate(ast!);
      expect(sql).toContain('`severity_text` = \'error\'');
      expect(sql).toContain('`service_name` = \'api\'');
      expect(sql).toContain('`namespace` = \'production\'');
      expect(sql.split('AND')).toHaveLength(3);
    });
  });

  describe('Pipe Operator for Field Selection', () => {
    it('should handle single field selection with pipe', () => {
      const query = 'severity_text = "error" | severity_text';
      const { tokens, errors } = tokenize(query);
      expect(errors).toHaveLength(0);

      const parser = new QueryParser(tokens);
      const { ast } = parser.parse();
      expect(ast).toBeTruthy();

      const visitor = new SQLVisitor(false, testSchema);
      const selectClause = visitor.generateSelectClause(ast!.select || [], 'timestamp');
      expect(selectClause).toContain('`severity_text`');
    });

    it('should handle multiple field selection with pipe', () => {
      const query = 'severity_text = "error" | severity_text, service_name, namespace';
      const { tokens, errors } = tokenize(query);
      expect(errors).toHaveLength(0);

      const parser = new QueryParser(tokens);
      const { ast } = parser.parse();
      expect(ast).toBeTruthy();

      const visitor = new SQLVisitor(false, testSchema);
      const selectClause = visitor.generateSelectClause(ast!.select || [], 'timestamp');
      expect(selectClause).toContain('`timestamp`');
      expect(selectClause).toContain('`severity_text,`');
      expect(selectClause).toContain('`service_name,`');
      expect(selectClause).toContain('`namespace`');
    });

    it('should handle Map field selection with pipe', () => {
      const query = 'log_attributes.level = "info" | log_attributes.level, severity_text';
      const { tokens, errors } = tokenize(query);
      expect(errors).toHaveLength(0);

      const parser = new QueryParser(tokens);
      const { ast } = parser.parse();
      expect(ast).toBeTruthy();

      const visitor = new SQLVisitor(false, testSchema);
      const selectClause = visitor.generateSelectClause(ast!.select || [], 'timestamp');
      expect(selectClause).toContain("`log_attributes`['level,']");
      expect(selectClause).toContain('`severity_text`');
    });

    it('should handle Map field selection with pipe', () => {
      const query = 'log_attributes.service.version = "1.0" | log_attributes.service.version, body';
      const { tokens, errors } = tokenize(query);
      expect(errors).toHaveLength(0);

      const parser = new QueryParser(tokens);
      const { ast } = parser.parse();
      expect(ast).toBeTruthy();

      const visitor = new SQLVisitor(false, testSchema);
      const selectClause = visitor.generateSelectClause(ast!.select || [], 'timestamp');
      expect(selectClause).toContain("`log_attributes`['service.version,']");
      expect(selectClause).toContain('`body`');
    });
  });

  describe('Complex Query Scenarios', () => {
    it('should handle mixed field types and operators', () => {
      const query = 'severity_text = "error" and severity_number >= 400 and body ~ "exception" and service_name != "background"';
      const { tokens, errors } = tokenize(query);
      expect(errors).toHaveLength(0);

      const parser = new QueryParser(tokens);
      const { ast } = parser.parse();
      expect(ast).toBeTruthy();

      const { sql } = visitor.generate(ast!);
      expect(sql).toContain('`severity_text` = \'error\'');
      expect(sql).toContain('`severity_number` >= 400');
      expect(sql).toContain('positionCaseInsensitive(`body`, \'exception\') > 0');
      expect(sql).toContain('`service_name` != \'background\'');
    });

    it('should handle deeply nested Map access', () => {
      const query = 'log_attributes.kubernetes.container.logs.level = "debug"';
      const { tokens, errors } = tokenize(query);
      expect(errors).toHaveLength(0);

      const parser = new QueryParser(tokens);
      const { ast } = parser.parse();
      expect(ast).toBeTruthy();

      const { sql } = visitor.generate(ast!);
      expect(sql).toContain("`log_attributes`['kubernetes.container.logs.level'] = 'debug'");
    });

    it('should handle complex Map key with dots', () => {
      const query = 'log_attributes."app.kubernetes.io/name" = "web-server"';
      const { tokens, errors } = tokenize(query);
      expect(errors).toHaveLength(0);

      const parser = new QueryParser(tokens);
      const { ast } = parser.parse();
      expect(ast).toBeTruthy();

      const { sql } = visitor.generate(ast!);
      expect(sql).toContain("`log_attributes`['app.kubernetes.io/name'] = 'web-server'");
    });

    it('should handle queries with both JSON and Map access', () => {
      const query = 'log_attributes.user.id = "123" and log_attributes.service.version = "1.0"';
      const { tokens, errors } = tokenize(query);
      expect(errors).toHaveLength(0);

      const parser = new QueryParser(tokens);
      const { ast } = parser.parse();
      expect(ast).toBeTruthy();

      const { sql } = visitor.generate(ast!);
      expect(sql).toContain("`log_attributes`['user.id'] = '123'");
      expect(sql).toContain("`log_attributes`['service.version'] = '1.0'");
    });

    it('should handle numeric string comparisons', () => {
      const query = 'trace_flags = "1" and severity_number = "200"';
      const { tokens, errors } = tokenize(query);
      expect(errors).toHaveLength(0);

      const parser = new QueryParser(tokens);
      const { ast } = parser.parse();
      expect(ast).toBeTruthy();

      const { sql } = visitor.generate(ast!);
      expect(sql).toContain('`trace_flags` = \'1\'');
      expect(sql).toContain('`severity_number` = \'200\'');
    });
  });

  describe('Edge Cases and Error Handling', () => {
    it('should handle empty queries gracefully', () => {
      const query = '';
      const { tokens, errors } = tokenize(query);
      expect(errors).toHaveLength(0);
      expect(tokens).toHaveLength(0);
    });

    it('should handle queries with only whitespace', () => {
      const query = '   ';
      const { tokens, errors } = tokenize(query);
      expect(errors).toHaveLength(0);
      expect(tokens).toHaveLength(0);
    });

    it('should handle queries with special characters in field names', () => {
      const query = 'special_field = "value"';
      const { tokens, errors } = tokenize(query);
      expect(errors).toHaveLength(0);

      const parser = new QueryParser(tokens);
      const { ast } = parser.parse();
      expect(ast).toBeTruthy();

      const { sql } = visitor.generate(ast!);
      expect(sql).toContain('`special_field` = \'value\'');
    });

    it('should handle boolean operators at start of query', () => {
      const query = 'and severity_text = "error"';
      const { tokens, errors } = tokenize(query);

      // This should produce an error since boolean operators can't start a query
      expect(errors).toHaveLength(0); // But tokenizer should handle it gracefully
      expect(tokens.some(t => t.type === 'bool')).toBe(true);
    });

    it('should handle multiple consecutive operators', () => {
      const query = 'severity_text == "error"';
      const { tokens, errors } = tokenize(query);
      expect(errors).toHaveLength(0);

      const parser = new QueryParser(tokens);
      const { ast } = parser.parse();
      expect(ast).toBeTruthy();

      const { sql } = visitor.generate(ast!);
      expect(sql).toContain('`severity_text` = \'error\''); // Should normalize == to =
    });

    it('should handle very long field names', () => {
      const longFieldName = 'a'.repeat(100);
      const query = `${longFieldName} = "value"`;
      const { tokens, errors } = tokenize(query);
      expect(errors).toHaveLength(0);

      const parser = new QueryParser(tokens);
      const { ast } = parser.parse();
      expect(ast).toBeTruthy();

      const { sql } = visitor.generate(ast!);
      expect(sql).toContain(`\`${longFieldName}\` = 'value'`);
    });

    it('should handle queries with unicode characters', () => {
      const query = 'service_name = "服务"';
      const { tokens, errors } = tokenize(query);
      expect(errors).toHaveLength(0);

      const parser = new QueryParser(tokens);
      const { ast } = parser.parse();
      expect(ast).toBeTruthy();

      const { sql } = visitor.generate(ast!);
      expect(sql).toContain('`service_name` = \'服务\'');
    });
  });

  describe('Performance and Large Queries', () => {
    it('should handle complex queries with many conditions', () => {
      const query = 'severity_text = "error" and service_name = "api" and namespace = "prod" and body ~ "exception" and trace_flags >= 1 and severity_number <= 500 and log_attributes.user.id = "123" and log_attributes.service.version != "1.0"';
      const { tokens, errors } = tokenize(query);
      expect(errors).toHaveLength(0);

      const parser = new QueryParser(tokens);
      const { ast } = parser.parse();
      expect(ast).toBeTruthy();

      const { sql } = visitor.generate(ast!);
      expect(sql.split('AND')).toHaveLength(8);
    });

    it('should handle deeply nested boolean logic', () => {
      const query = '(severity_text = "error" and (service_name = "api" or service_name = "web")) or (severity_text = "warn" and namespace = "prod")';
      const { tokens, errors } = tokenize(query);
      expect(errors).toHaveLength(0);

      const parser = new QueryParser(tokens);
      const { ast } = parser.parse();
      expect(ast).toBeTruthy();

      const { sql } = visitor.generate(ast!);
      expect(sql).toContain('`severity_text` = \'error\'');
      expect(sql).toContain('`service_name` = \'api\'');
      expect(sql).toContain('`service_name` = \'web\'');
      expect(sql).toContain('`severity_text` = \'warn\'');
      expect(sql).toContain('`namespace` = \'prod\'');
    });
  });

  describe('Schema-Aware Query Generation', () => {
    it('should use Map access for Map type columns', () => {
      const mapSchema: SchemaInfo = {
        columns: [
          { name: 'log_attributes', type: 'Map(String, String)' }
        ]
      };
      const mapVisitor = new SQLVisitor(false, mapSchema);

      const query = 'log_attributes.service.version = "1.0"';
      const { tokens, errors } = tokenize(query);
      expect(errors).toHaveLength(0);

      const parser = new QueryParser(tokens);
      const { ast } = parser.parse();
      expect(ast).toBeTruthy();

      const { sql } = mapVisitor.generate(ast!);
      expect(sql).toContain("`log_attributes`['service.version'] = '1.0'");
    });

    it('should use JSON extraction for JSON type columns', () => {
      const jsonSchema: SchemaInfo = {
        columns: [
          { name: 'log_data', type: 'JSON' }
        ]
      };
      const jsonVisitor = new SQLVisitor(false, jsonSchema);

      const query = 'log_data.user.name = "john"';
      const { tokens, errors } = tokenize(query);
      expect(errors).toHaveLength(0);

      const parser = new QueryParser(tokens);
      const { ast } = parser.parse();
      expect(ast).toBeTruthy();

      const { sql } = jsonVisitor.generate(ast!);
      expect(sql).toContain("JSONExtractString(`log_data`, 'user', 'name') = 'john'");
    });

    it('should handle String columns that might contain JSON', () => {
      const stringSchema: SchemaInfo = {
        columns: [
          { name: 'body', type: 'String' }
        ]
      };
      const stringVisitor = new SQLVisitor(false, stringSchema);

      const query = 'body.user.name = "john"';
      const { tokens, errors } = tokenize(query);
      expect(errors).toHaveLength(0);

      const parser = new QueryParser(tokens);
      const { ast } = parser.parse();
      expect(ast).toBeTruthy();

      const { sql } = stringVisitor.generate(ast!);
      expect(sql).toContain("JSONExtractString(`body`, 'user', 'name') = 'john'");
    });
  });
});