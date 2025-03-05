// Test script for LogChefQL simple parser
import { parseSimple } from './simple-parser.js';
import { generateSQL } from './sql-generator.js';

// Sample expressions to test
const expressions = [
  'status_code=404',
  'message~"error"',
  'duration>100',
  'method="GET" AND status_code=200',
  '(method="GET" OR method="POST") AND status_code>=400',
  'namespace="production" AND (severity="ERROR" OR contains_text~"failure")'
];

console.log('Testing LogChefQL simple parser:');
console.log('------------------------------');

for (const expr of expressions) {
  try {
    console.log(`\nTesting: ${expr}`);
    
    // Test parsing
    const ast = parseSimple(expr);
    console.log('Parsed AST:', JSON.stringify(ast, null, 2));
    
    // Test SQL generation
    const sql = generateSQL(ast);
    console.log('Generated SQL:', sql);
    
    console.log('✓ PASSED');
  } catch (error) {
    console.error(`✗ FAILED for "${expr}":`, error);
  }
}