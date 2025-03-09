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
 * Builds a SQL query from LogchefQL with additional parameters
 * 
 * @param logchefQL The LogchefQL query string
 * @param options Additional query options (table, columns, limit, etc.)
 * @returns A complete SQL query string with params for execution
 */
export function buildSQLFromLogchefQL(
  logchefQL: string,
  options: {
    table: string;
    columns?: string[];
    limit?: number;
    orderBy?: string;
    orderDirection?: 'ASC' | 'DESC';
    timeField?: string;
    startTime?: number;
    endTime?: number;
    includeTimeRange?: boolean;
  }
): { sql: string; params: Array<string | number> } {
  // Default values
  const {
    table,
    columns = ['*'],
    limit = 100,
    orderBy = 'timestamp',
    orderDirection = 'DESC',
    timeField = 'timestamp',
    startTime,
    endTime,
    includeTimeRange = true, // By default, include time range for execution
  } = options;

  // Start building the SQL query
  let sql = `SELECT ${columns.join(', ')} FROM ${table}`;
  let params: Array<string | number> = [];

  // Add time range conditions if provided and requested
  const timeConditions: string[] = [];
  if (includeTimeRange) {
    if (startTime) {
      timeConditions.push(`${timeField} >= ?`);
      params.push(startTime);
    }
    if (endTime) {
      timeConditions.push(`${timeField} <= ?`);
      params.push(endTime);
    }
  }

  // First try simple key=value pattern for robustness
  let logchefCondition = '';
  let logchefParams: Array<string | number> = [];
  
  if (logchefQL && logchefQL.trim()) {
    // Parse LogchefQL query if provided
    try {
      // Try with the full parser
      const result = parseLogchefQL(logchefQL);
      logchefCondition = result.sql;
      logchefParams = result.params;
      
      // If the parser returned empty SQL but we have a query, try fallback
      if (!logchefCondition && logchefQL.trim()) {
        throw new Error('Parser returned empty SQL');
      }
    } catch (error) {
      console.error('Error parsing with main parser:', error);
      
      // Attempt a simple regex fallback for common patterns when parser fails
      const keyValueRegexes = [
        /^([a-zA-Z0-9_.]+)\s*=\s*"([^"]*)"$/,    // key="value"
        /^([a-zA-Z0-9_.]+)\s*=\s*'([^']*)'$/,    // key='value'
        /^([a-zA-Z0-9_.]+)\s*=\s*([^'"]\S*)$/    // key=value (unquoted)
      ];
      
      for (const regex of keyValueRegexes) {
        const match = logchefQL.trim().match(regex);
        if (match) {
          const [_, key, value] = match;
          logchefCondition = `WHERE ${key} = ?`;
          logchefParams = [value];
          break;
        }
      }
      
      // If we still couldn't parse, try a more aggressive approach for simple expressions
      if (!logchefCondition) {
        // Try to extract key=value pattern with minimal validation
        const simpleMatch = logchefQL.trim().match(/^([a-zA-Z0-9_.]+)\s*=\s*(.+)$/);
        if (simpleMatch) {
          const [_, key, rawValue] = simpleMatch;
          // Clean up the value - remove quotes if present
          let value = rawValue.trim();
          if ((value.startsWith('"') && value.endsWith('"')) || 
              (value.startsWith("'") && value.endsWith("'"))) {
            value = value.substring(1, value.length - 1);
          }
          logchefCondition = `WHERE ${key} = ?`;
          logchefParams = [value];
        } else {
          console.warn('Failed to parse LogchefQL with all fallbacks, continuing without conditions.');
        }
      }
    }
  }

  // Combine all conditions
  const conditions: string[] = [];
  if (timeConditions.length > 0) {
    conditions.push(timeConditions.join(' AND '));
  }
  if (logchefCondition) {
    // Remove the 'WHERE ' prefix from the logchefQL condition
    conditions.push(logchefCondition.replace(/^WHERE\s+/, ''));
  }

  // Add WHERE clause if we have conditions
  if (conditions.length > 0) {
    sql += ` WHERE ${conditions.join(' AND ')}`;
  }

  // Add ORDER BY and LIMIT if requested
  if (includeTimeRange) {
    sql += ` ORDER BY ${orderBy} ${orderDirection} LIMIT ${limit}`;
  }

  // Combine all parameters
  params = [...params, ...logchefParams];

  return { sql, params };
}

/**
 * Translates a LogchefQL query to a ClickHouse SQL query string
 * This is used for the SQL preview mode in the editor
 * 
 * @param logchefQL The LogchefQL query string
 * @param options Additional options for query building
 * @returns A formatted SQL query string
 */
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
    
    return;
  } catch (error) {
    console.error('Parser error:', error);
  }
}

export function translateLogchefQLToSQL(
  logchefQL: string,
  options: {
    table?: string;
    startTime?: Date;
    endTime?: Date;
    limit?: number;
    includeTimeRange?: boolean;
  } = {}
): string {
  // Default values
  const {
    table = 'logs',
    limit = 100,
    startTime = new Date(Date.now() - 3 * 60 * 60 * 1000), // 3 hours ago
    endTime = new Date(),
    includeTimeRange = false, // By default, don't include time range for display
  } = options;

  // Format dates for ClickHouse
  const startTimeFormatted = startTime.toISOString().replace('T', ' ').replace('Z', '');
  const endTimeFormatted = endTime.toISOString().replace('T', ' ').replace('Z', '');

  try {
    // Use the improved buildSQLFromLogchefQL function which now has better fallback handling
    if (logchefQL && logchefQL.trim()) {
      // Build SQL using our consistent function
      const { sql, params } = buildSQLFromLogchefQL(logchefQL, {
        table,
        limit,
        startTime: includeTimeRange ? startTime.getTime() : undefined,
        endTime: includeTimeRange ? endTime.getTime() : undefined,
        includeTimeRange
      });
      
      // Replace parameterized values with actual values for display
      let formattedSQL = sql;
      params.forEach(param => {
        formattedSQL = formattedSQL.replace('?', typeof param === 'string' ? `'${param}'` : param.toString());
      });
      
      console.log(`Formatted SQL from buildSQLFromLogchefQL: ${formattedSQL}`);
      return formattedSQL;
    } else {
      // Return default query for empty input
      return `SELECT * FROM ${table}${includeTimeRange ? `
WHERE timestamp >= '${startTimeFormatted}'
  AND timestamp <= '${endTimeFormatted}'
LIMIT ${limit}` : ''}`;
    }
  } catch (error) {
    console.error('Error translating LogchefQL to SQL:', error);
    // Return default query with error comment for invalid input
    return `-- Error translating LogchefQL query: ${error instanceof Error ? error.message : 'Unknown error'}
SELECT * FROM ${table}${includeTimeRange ? `
WHERE timestamp >= '${startTimeFormatted}'
  AND timestamp <= '${endTimeFormatted}'
LIMIT ${limit}` : ''}`;
  }
}
