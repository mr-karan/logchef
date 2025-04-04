// Simple test script for the modernized LogchefQL implementation
import { parseToSQL, validateQuery } from './index'; // Corrected import path

// Test queries
const testQueries = [
  {
    query: 'status=200 AND (service="payments" OR env~"prod")',
    name: 'Complex query with parentheses'
  },
  {
    query: 'status=500',
    name: 'Simple equality'
  },
  {
    query: 'response_time>100',
    name: 'Greater than comparison'
  },
  {
    query: 'message~"error"',
    name: 'Regex matching'
  },
  {
    query: 'status!=404 AND level="error"',
    name: 'Not equals with AND'
  }
];

// Run tests
console.log('LogchefQL Parser Test Results:');
console.log('==============================');

testQueries.forEach((test, index) => {
  console.log(`\nTest ${index + 1}: ${test.name}`);
  console.log(`Query: ${test.query}`);

  // Test standard parsing
  try {
    const result = parseToSQL(test.query);
    console.log('SQL:');
    console.log(result.sql);
    console.log('Errors:', result.errors.length ? result.errors : 'None');
    console.log('Valid:', validateQuery(test.query));
  } catch (error) {
    console.error('ERROR:', error instanceof Error ? error.message : String(error));
  }

  // Test parameterized query
  try {
    const paramResult = parseToSQL(test.query, { parameterized: true });
    console.log('Parameterized SQL:');
    console.log(paramResult.sql);
    console.log('Parameters:', paramResult.params);
  } catch (error) {
    console.error('ERROR in parameterized query:', error instanceof Error ? error.message : String(error));
  }
});

console.log('\n==============================');
console.log('Tests completed!');
