import { Parser } from './index';

/**
 * Converts a LogchefQL query string to a ClickHouse SQL WHERE clause with params.
 * 
 * @param query The LogchefQL query string
 * @returns An object containing the SQL WHERE clause and parameters array
 */
export function parseLogchefQL(query: string): { sql: string; params: Array<string | number> } {
  if (!query || query.trim() === '') {
    throw new Error('Empty query');
  }

  const parser = new Parser();
  parser.parse(query);
  return parser.toSQL();
}

/**
 * Translates a LogchefQL query to a ClickHouse SQL query string
 * 
 * @param logchefQL The LogchefQL query string
 * @param options Additional options for query building
 * @returns A formatted SQL query string
 */
export function translateLogchefQLToSQL(
  logchefQL: string,
  options: {
    table?: string;
    limit?: number;
    includeTimeRange?: boolean;
  } = {}
): string {
  // Default values
  const {
    table = 'logs',
    limit = 100,
    includeTimeRange = false
  } = options;

  try {
    // If we have a LogchefQL query, parse it and build SQL
    if (logchefQL && logchefQL.trim()) {
      // Parse the LogchefQL query
      const { sql, params } = parseLogchefQL(logchefQL);
      
      // Build the complete SQL query
      let fullSql = `SELECT * FROM ${table}`;
      
      // Add WHERE clause if we have conditions
      if (sql) {
        fullSql += ` ${sql}`;
      }
      
      // Add LIMIT only if requested
      if (includeTimeRange) {
        fullSql += ` LIMIT ${limit}`;
      }
      
      // Replace parameterized values with actual values for display
      let formattedSQL = fullSql;
      params.forEach(param => {
        formattedSQL = formattedSQL.replace('?', typeof param === 'string' ? `'${param}'` : param.toString());
      });
      
      return formattedSQL;
    } else {
      // Return default query for empty input
      return `SELECT * FROM ${table}${includeTimeRange ? ` LIMIT ${limit}` : ''}`;
    }
  } catch (error) {
    console.error('Error translating LogchefQL to SQL:', error);
    // Return default query with error comment for invalid input
    return `-- Error translating LogchefQL query: ${error instanceof Error ? error.message : 'Unknown error'}
SELECT * FROM ${table} LIMIT ${limit}`;
  }
}

/**
 * Test function to debug LogchefQL parsing
 * This function is for development/debugging only
 */
export function testLogchefQLParser(query: string): void {
  console.log('Testing LogchefQL parser with:', query);
  
  try {
    const parser = new Parser();
    parser.parse(query);
    
    console.log('Parser state:', {
      state: parser.state,
      key: parser.key,
      keyValueOperator: parser.keyValueOperator,
      value: parser.value,
      boolOperator: parser.boolOperator,
    });
    
    console.log('AST root:', parser.root);
    
    const { sql, params } = parser.toSQL();
    console.log('Generated SQL:', sql);
    console.log('Parameters:', params);
  } catch (error) {
    console.error('Parser error:', error);
  }
}
