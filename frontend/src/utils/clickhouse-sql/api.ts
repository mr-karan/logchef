import { Parser } from './index';

/**
 * Format and validate ClickHouse SQL query
 * 
 * @param query The SQL query string to format and validate
 * @returns The formatted SQL query string
 */
export function formatClickHouseSQL(query: string): string {
  if (!query || query.trim() === '') {
    return '';
  }

  try {
    // Parse the query for validation
    const parser = new Parser();
    parser.parse(query);
    
    // Basic SQL formatting - this is a simple implementation
    // In a real application, you might want to use a more robust SQL formatter
    return formatSQL(query);
  } catch (error) {
    // Return the original query if there's a parsing error
    console.error('Error formatting ClickHouse SQL:', error);
    return query;
  }
}

/**
 * Validate a ClickHouse SQL query
 * 
 * @param query The SQL query string to validate
 * @returns Object with validation status and error message if invalid
 */
export function validateClickHouseSQL(query: string): { isValid: boolean; error?: string } {
  if (!query || query.trim() === '') {
    return { isValid: false, error: 'Empty query' };
  }

  try {
    // Parse the query for validation
    const parser = new Parser();
    parser.parse(query);
    return { isValid: true };
  } catch (error) {
    return { 
      isValid: false, 
      error: error instanceof Error ? error.message : 'Unknown error validating query' 
    };
  }
}

/**
 * Build a query for executing ClickHouse SQL with parameters
 * 
 * @param sql The SQL query string
 * @param params Optional parameters to merge with the query
 * @returns The complete query object ready for execution
 */
export function buildClickHouseQuery(
  sql: string,
  params?: {
    format?: string;
    limit?: number;
    timeout?: number;
    max_execution_time?: number;
  }
): { sql: string; params: Record<string, any> } {
  // Default parameters
  const queryParams: Record<string, any> = {
    format: 'JSON',
    ...params
  };

  // Extract and modify query parts if needed
  let sqlQuery = sql.trim();
  
  // Add LIMIT if specified and not already in the query
  if (params?.limit && !sqlQuery.toUpperCase().includes('LIMIT')) {
    sqlQuery += ` LIMIT ${params.limit}`;
  }

  return { 
    sql: sqlQuery, 
    params: queryParams 
  };
}

/**
 * Simple SQL formatter
 * This is a basic implementation - in a production app you might want to use a dedicated library
 * 
 * @param sql The SQL query to format
 * @returns Formatted SQL string
 */
function formatSQL(sql: string): string {
  const formattedSQL = sql
    .replace(/\s+/g, ' ')
    .replace(/\s*,\s*/g, ', ')
    .replace(/\(\s+/g, '(')
    .replace(/\s+\)/g, ')')
    .replace(/\s*=\s*/g, ' = ')
    .replace(/\s*>\s*/g, ' > ')
    .replace(/\s*<\s*/g, ' < ')
    .replace(/\s*>=\s*/g, ' >= ')
    .replace(/\s*<=\s*/g, ' <= ')
    .replace(/\s*<>\s*/g, ' <> ')
    .replace(/\s*!=\s*/g, ' != ')
    .replace(/\s+AND\s+/gi, ' AND ')
    .replace(/\s+OR\s+/gi, ' OR ')
    .replace(/\s+IN\s+/gi, ' IN ')
    .replace(/\s+FROM\s+/gi, '\nFROM ')
    .replace(/\s+WHERE\s+/gi, '\nWHERE ')
    .replace(/\s+GROUP\s+BY\s+/gi, '\nGROUP BY ')
    .replace(/\s+HAVING\s+/gi, '\nHAVING ')
    .replace(/\s+ORDER\s+BY\s+/gi, '\nORDER BY ')
    .replace(/\s+LIMIT\s+/gi, '\nLIMIT ')
    .replace(/\s+UNION\s+/gi, '\nUNION\n')
    .replace(/\s+JOIN\s+/gi, '\nJOIN ')
    .replace(/\s+LEFT\s+JOIN\s+/gi, '\nLEFT JOIN ')
    .replace(/\s+RIGHT\s+JOIN\s+/gi, '\nRIGHT JOIN ')
    .replace(/\s+INNER\s+JOIN\s+/gi, '\nINNER JOIN ')
    .replace(/\s+ON\s+/gi, '\n  ON ');

  return formattedSQL.trim();
}

/**
 * Generate a ClickHouse SQL query for log analytics
 * 
 * @param options Query generation options
 * @returns Generated SQL string
 */
export function generateLogAnalyticsQuery(options: {
  table: string;
  columns?: string[];
  filter?: string;
  groupBy?: string[];
  orderBy?: string;
  orderDirection?: 'ASC' | 'DESC';
  limit?: number;
  timeField?: string;
  startTime?: number | string;
  endTime?: number | string;
}): string {
  const {
    table,
    columns = ['*'],
    filter,
    groupBy = [],
    orderBy = 'timestamp',
    orderDirection = 'DESC',
    limit = 1000,
    timeField = 'timestamp',
    startTime,
    endTime,
  } = options;

  // Build the SELECT clause
  let sql = `SELECT ${columns.join(', ')} FROM ${table}`;

  // Build the WHERE clause
  const conditions: string[] = [];
  
  if (startTime) {
    conditions.push(`${timeField} >= ${typeof startTime === 'string' ? `'${startTime}'` : startTime}`);
  }
  
  if (endTime) {
    conditions.push(`${timeField} <= ${typeof endTime === 'string' ? `'${endTime}'` : endTime}`);
  }
  
  if (filter && filter.trim()) {
    conditions.push(`(${filter})`);
  }

  if (conditions.length > 0) {
    sql += ` WHERE ${conditions.join(' AND ')}`;
  }

  // Add GROUP BY if specified
  if (groupBy.length > 0) {
    sql += ` GROUP BY ${groupBy.join(', ')}`;
  }

  // Add ORDER BY
  sql += ` ORDER BY ${orderBy} ${orderDirection}`;

  // Add LIMIT
  sql += ` LIMIT ${limit}`;

  return sql;
}