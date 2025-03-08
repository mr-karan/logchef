import { parseSQL, addTimeRangeToSQL, extractTableFromSQL } from './parser';

// Test SQL parsing
const testSQL = `
SELECT 
  timestamp,
  message,
  level,
  service_name
FROM logs 
WHERE service_name = 'api' AND level = 'error'
ORDER BY timestamp DESC
LIMIT 100
`;

console.log('Original SQL:');
console.log(testSQL);

// Parse the SQL query
const parsed = parseSQL(testSQL);
console.log('\nParsed components:');
console.log('SELECT:', parsed.select?.content);
console.log('FROM:', parsed.from?.content);
console.log('WHERE:', parsed.where?.content);
console.log('ORDER BY:', parsed.orderBy?.content);
console.log('LIMIT:', parsed.limit?.content);

// Add time range conditions
const withTimeRange = addTimeRangeToSQL(
  testSQL,
  'timestamp',
  '2023-01-01 00:00:00',
  '2023-01-31 23:59:59'
);

console.log('\nSQL with time range:');
console.log(withTimeRange);

// Extract table name
const tableName = extractTableFromSQL(testSQL);
console.log('\nExtracted table name:', tableName);

// Test with a SQL query that doesn't have a WHERE clause
const testSQL2 = `
SELECT * FROM logs 
ORDER BY timestamp DESC
LIMIT 10
`;

console.log('\nSQL without WHERE clause:');
console.log(testSQL2);

const withTimeRange2 = addTimeRangeToSQL(
  testSQL2,
  'timestamp',
  '2023-01-01 00:00:00',
  '2023-01-31 23:59:59'
);

console.log('\nSQL with added time range:');
console.log(withTimeRange2);

// Additional tests
const complexSQL = `
SELECT 
  timestamp,
  JSONExtractString(params, 'request_id') as request_id,
  JSONExtractString(params, 'user_id') as user_id,
  message
FROM logs 
WHERE service_name = 'api' 
  AND JSONHas(params, 'error_code') = 1
  AND toDate(timestamp) BETWEEN '2023-01-01' AND '2023-01-31'
GROUP BY timestamp, request_id, user_id, message
HAVING count() > 1
ORDER BY timestamp DESC
LIMIT 50
`;

console.log('\nComplex SQL:');
console.log(complexSQL);

const parsedComplex = parseSQL(complexSQL);
console.log('\nParsed complex SQL components:');
console.log('SELECT:', parsedComplex.select?.content);
console.log('FROM:', parsedComplex.from?.content);
console.log('WHERE:', parsedComplex.where?.content);
console.log('GROUP BY:', parsedComplex.groupBy?.content);
console.log('HAVING:', parsedComplex.having?.content);
console.log('ORDER BY:', parsedComplex.orderBy?.content);
console.log('LIMIT:', parsedComplex.limit?.content);

const withTimeRangeComplex = addTimeRangeToSQL(
  complexSQL,
  'timestamp',
  '2023-01-01 00:00:00',
  '2023-01-31 23:59:59'
);

console.log('\nComplex SQL with time range:');
console.log(withTimeRangeComplex);