# LogChefQL Test Suite

This comprehensive test suite validates the LogChefQL query language implementation, focusing on safety, correctness, and functionality.

## Test Coverage

The test suite includes **50+ test cases** covering:

### P0 Safety & Correctness Tests
- **SQL String Escaping**: Proper escaping of quotes and backslashes in JSON/Map path parameters
- **Boolean Tokenization**: Correct whole-word matching for `and`/`or` operators
- **Unterminated String Handling**: Error detection for unclosed string literals

### Core Functionality Tests
- **Basic Field Queries**: All comparison operators (=, !=, >, <, >=, <=, ~, !~)
- **JSON/Map Field Access**: Nested field access with proper SQL generation
- **Boolean Logic**: Complex AND/OR combinations with parentheses
- **Pipe Operator**: Field selection and projection
- **Schema-Aware Generation**: Different SQL generation based on column types

### Edge Cases & Error Handling
- Empty queries and whitespace handling
- Special characters in field names
- Unicode character support
- Very long field names
- Complex nested queries
- Performance validation for large queries

## Running the Tests

### Run all LogChefQL tests
```bash
pnpm test:logchefql
```

### Run all tests (including LogChefQL)
```bash
pnpm test
```

### Run tests in watch mode
```bash
pnpm test:watch
```

### Run specific test file
```bash
pnpm test src/utils/logchefql/__tests__/logchefql.test.ts
```

## Test Schema

The tests use a realistic schema based on the provided API response:

```typescript
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
```

## Key Test Categories

### 1. P0 Safety Tests
- SQL injection prevention through proper escaping
- Correct boolean operator parsing
- Unterminated string error detection

### 2. Query Language Features
- All comparison operators
- JSON field extraction
- Map field access
- Boolean logic combinations
- Field projection with pipe operator

### 3. Schema Integration
- Map type handling
- JSON type handling
- String type handling
- Fallback behavior for unknown types

### 4. Error Handling
- Malformed queries
- Unterminated strings
- Invalid syntax
- Edge cases

## Test Examples

```typescript
// Basic field query
'severity_text = "error"'

// JSON field access with escaping
'log_attributes.user\'name = "test"'

// Complex boolean logic
'(severity_text = "error" and service_name = "api") or severity_text = "critical"'

// Field projection
'severity_text = "error" | severity_text, service_name, log_attributes.level'

// Map field access
'log_attributes.service.version = "1.0"'
```

## Adding New Tests

When adding new test cases:

1. **Follow the existing structure**: Group tests by functionality
2. **Include edge cases**: Test boundary conditions and error scenarios
3. **Test P0 fixes**: Ensure safety and correctness are maintained
4. **Use descriptive names**: Make test names clear and specific
5. **Add comments**: Explain complex test scenarios

## Continuous Integration

These tests should be run as part of the CI/CD pipeline to ensure:

- No regressions in LogChefQL functionality
- P0 safety fixes remain intact
- New features don't break existing functionality
- Performance characteristics are maintained