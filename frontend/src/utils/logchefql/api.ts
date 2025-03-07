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
  } = options;

  // Start building the SQL query
  let sql = `SELECT ${columns.join(', ')} FROM ${table}`;
  let params: Array<string | number> = [];

  // Add time range conditions if provided
  const timeConditions: string[] = [];
  if (startTime) {
    timeConditions.push(`${timeField} >= ?`);
    params.push(startTime);
  }
  if (endTime) {
    timeConditions.push(`${timeField} <= ?`);
    params.push(endTime);
  }

  // Parse LogchefQL query if provided
  let logchefCondition = '';
  let logchefParams: Array<string | number> = [];
  if (logchefQL && logchefQL.trim()) {
    try {
      const { sql, params } = parseLogchefQL(logchefQL);
      logchefCondition = sql;
      logchefParams = params;
    } catch (error) {
      // Silently continue without LogchefQL conditions
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

  // Add ORDER BY and LIMIT
  sql += ` ORDER BY ${orderBy} ${orderDirection} LIMIT ${limit}`;

  // Combine all parameters
  params = [...params, ...logchefParams];

  return { sql, params };
}