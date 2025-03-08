import { Parser as ClickhouseSQLParser } from './index';

interface SQLClause {
  clause: string;
  content: string;
  position: {
    startIndex: number;
    endIndex: number;
  };
}

interface ParsedSQL {
  select: SQLClause | null;
  from: SQLClause | null;
  where: SQLClause | null;
  orderBy: SQLClause | null;
  limit: SQLClause | null;
  groupBy: SQLClause | null;
  having: SQLClause | null;
  rest: string;
  rawSQL: string;
}

/**
 * SQL Tokenizer/Parser
 * Breaks down a SQL query into its component clauses
 */
export class SQLTokenizer {
  private baseParser: ClickhouseSQLParser;

  constructor() {
    this.baseParser = new ClickhouseSQLParser();
  }

  /**
   * Parse a SQL query into its component clauses
   * 
   * @param sql The SQL query to parse
   * @returns An object containing the parsed clauses
   */
  public parseSQL(sql: string): ParsedSQL {
    // Initialize result with empty values
    const result: ParsedSQL = {
      select: null,
      from: null,
      where: null,
      orderBy: null,
      limit: null,
      groupBy: null,
      having: null,
      rest: '',
      rawSQL: sql
    };

    // First make sure we can actually parse this SQL as valid
    try {
      this.baseParser.parse(sql);
    } catch (error) {
      console.error("Error parsing SQL:", error);
      result.rest = sql;
      return result;
    }

    // Extract the clauses using regex patterns
    // Note: This approach is simplistic but effective for most standard SQL queries
    // More complex queries might need a more sophisticated SQL parser
    
    // Extract SELECT clause
    const selectMatch = sql.match(/SELECT\s+(.*?)(?:\s+FROM\s+|$)/si);
    if (selectMatch) {
      result.select = {
        clause: 'SELECT',
        content: selectMatch[1].trim(),
        position: {
          startIndex: selectMatch.index || 0,
          endIndex: (selectMatch.index || 0) + selectMatch[0].length
        }
      };
    }

    // Extract FROM clause
    const fromMatch = sql.match(/FROM\s+(.*?)(?:\s+(?:WHERE|GROUP BY|HAVING|ORDER BY|LIMIT)\s+|$)/si);
    if (fromMatch) {
      result.from = {
        clause: 'FROM',
        content: fromMatch[1].trim(),
        position: {
          startIndex: fromMatch.index || 0,
          endIndex: (fromMatch.index || 0) + fromMatch[0].length
        }
      };
    }

    // Extract WHERE clause
    const whereMatch = sql.match(/WHERE\s+(.*?)(?:\s+(?:GROUP BY|HAVING|ORDER BY|LIMIT)\s+|$)/si);
    if (whereMatch) {
      result.where = {
        clause: 'WHERE',
        content: whereMatch[1].trim(),
        position: {
          startIndex: whereMatch.index || 0,
          endIndex: (whereMatch.index || 0) + whereMatch[0].length
        }
      };
    }

    // Extract GROUP BY clause
    const groupByMatch = sql.match(/GROUP BY\s+(.*?)(?:\s+(?:HAVING|ORDER BY|LIMIT)\s+|$)/si);
    if (groupByMatch) {
      result.groupBy = {
        clause: 'GROUP BY',
        content: groupByMatch[1].trim(),
        position: {
          startIndex: groupByMatch.index || 0,
          endIndex: (groupByMatch.index || 0) + groupByMatch[0].length
        }
      };
    }

    // Extract HAVING clause
    const havingMatch = sql.match(/HAVING\s+(.*?)(?:\s+(?:ORDER BY|LIMIT)\s+|$)/si);
    if (havingMatch) {
      result.having = {
        clause: 'HAVING',
        content: havingMatch[1].trim(),
        position: {
          startIndex: havingMatch.index || 0,
          endIndex: (havingMatch.index || 0) + havingMatch[0].length
        }
      };
    }

    // Extract ORDER BY clause
    const orderByMatch = sql.match(/ORDER BY\s+(.*?)(?:\s+LIMIT\s+|$)/si);
    if (orderByMatch) {
      result.orderBy = {
        clause: 'ORDER BY',
        content: orderByMatch[1].trim(),
        position: {
          startIndex: orderByMatch.index || 0,
          endIndex: (orderByMatch.index || 0) + orderByMatch[0].length
        }
      };
    }

    // Extract LIMIT clause
    const limitMatch = sql.match(/LIMIT\s+(.*?)$/si);
    if (limitMatch) {
      result.limit = {
        clause: 'LIMIT',
        content: limitMatch[1].trim(),
        position: {
          startIndex: limitMatch.index || 0,
          endIndex: (limitMatch.index || 0) + limitMatch[0].length
        }
      };
    }

    // If we didn't match any clauses, just store the whole query as rest
    if (!result.select && !result.from && !result.where &&
        !result.groupBy && !result.having && !result.orderBy && !result.limit) {
      result.rest = sql;
    }

    return result;
  }

  /**
   * Reconstruct a SQL query from parsed components
   * 
   * @param parsed The parsed SQL components
   * @returns A reconstructed SQL query string
   */
  public reconstructSQL(parsed: ParsedSQL): string {
    const parts: string[] = [];

    if (parsed.select) {
      parts.push(`SELECT ${parsed.select.content}`);
    }

    if (parsed.from) {
      parts.push(`FROM ${parsed.from.content}`);
    }

    if (parsed.where) {
      parts.push(`WHERE ${parsed.where.content}`);
    }

    if (parsed.groupBy) {
      parts.push(`GROUP BY ${parsed.groupBy.content}`);
    }

    if (parsed.having) {
      parts.push(`HAVING ${parsed.having.content}`);
    }

    if (parsed.orderBy) {
      parts.push(`ORDER BY ${parsed.orderBy.content}`);
    }

    if (parsed.limit) {
      parts.push(`LIMIT ${parsed.limit.content}`);
    }

    if (parts.length === 0 && parsed.rest) {
      return parsed.rest;
    }

    return parts.join(' ');
  }

  /**
   * Add time range conditions to a parsed SQL query
   * 
   * @param parsed The parsed SQL components
   * @param timeField The timestamp field name
   * @param startTime The start timestamp (ISO string or number)
   * @param endTime The end timestamp (ISO string or number)
   * @returns Updated parsed SQL components
   */
  public addTimeRangeConditions(
    parsed: ParsedSQL,
    timeField: string = 'timestamp',
    startTime?: string | number,
    endTime?: string | number
  ): ParsedSQL {
    // If there's no time range to add, return the original
    if (!startTime && !endTime) {
      return parsed;
    }

    // Format start and end times for SQL
    const formatTime = (time?: string | number): string | undefined => {
      if (!time) return undefined;
    
      // If it's already a string that looks like a formatted date, just return it
      if (typeof time === 'string' && 
          (time.includes('-') || time.includes(':') || time.includes('T'))) {
        return `'${time}'`;
      }
    
      // If it's a number (timestamp), convert to formatted string
      // For ClickHouse DateTime, we need format: 'YYYY-MM-DD HH:MM:SS'
      if (typeof time === 'number' || !isNaN(Number(time))) {
        const date = new Date(Number(time) * 1000); // Convert seconds to milliseconds
      
        // Format as 'YYYY-MM-DD HH:MM:SS'
        const year = date.getFullYear();
        const month = String(date.getMonth() + 1).padStart(2, '0');
        const day = String(date.getDate()).padStart(2, '0');
        const hours = String(date.getHours()).padStart(2, '0');
        const minutes = String(date.getMinutes()).padStart(2, '0');
        const seconds = String(date.getSeconds()).padStart(2, '0');
      
        return `'${year}-${month}-${day} ${hours}:${minutes}:${seconds}'`;
      }
    
      // Default case
      return `'${time}'`;
    };

    const formattedStartTime = formatTime(startTime);
    const formattedEndTime = formatTime(endTime);

    // Build time conditions
    const timeConditions: string[] = [];
    if (formattedStartTime) {
      timeConditions.push(`${timeField} >= ${formattedStartTime}`);
    }
    if (formattedEndTime) {
      timeConditions.push(`${timeField} <= ${formattedEndTime}`);
    }

    // Join time conditions
    const timeCondition = timeConditions.join(' AND ');

    // Create a deep copy of the parsed object to avoid mutations
    const result: ParsedSQL = JSON.parse(JSON.stringify(parsed));

    // Handle WHERE clause
    if (result.where) {
      // Already has a WHERE clause, append the time condition
      result.where.content = `(${result.where.content}) AND (${timeCondition})`;
    } else {
      // No WHERE clause yet, create one
      result.where = {
        clause: 'WHERE',
        content: timeCondition,
        position: {
          startIndex: -1, // Will be calculated during reconstruction
          endIndex: -1    // Will be calculated during reconstruction
        }
      };
    }

    return result;
  }

  /**
   * Extract a table name from a parsed SQL query
   * This is a best-effort extraction and might not work for complex queries
   * 
   * @param parsed The parsed SQL query
   * @returns The extracted table name, or null if not found
   */
  public extractTableName(parsed: ParsedSQL): string | null {
    if (!parsed.from) {
      return null;
    }

    // Simple table name (e.g., "FROM table")
    const simpleMatch = parsed.from.content.match(/^(\w+)$/);
    if (simpleMatch) {
      return simpleMatch[1];
    }

    // Table with alias (e.g., "FROM table AS t" or "FROM table t")
    const aliasMatch = parsed.from.content.match(/^(\w+)(?:\s+AS)?\s+\w+$/i);
    if (aliasMatch) {
      return aliasMatch[1];
    }

    // Table with database prefix (e.g., "FROM db.table")
    const dbPrefixMatch = parsed.from.content.match(/^(\w+\.\w+)$/);
    if (dbPrefixMatch) {
      return dbPrefixMatch[1].split('.')[1];
    }

    // Best effort for more complex cases - just take the first "word" after FROM
    const firstWordMatch = parsed.from.content.match(/^(\w+)/);
    if (firstWordMatch) {
      return firstWordMatch[1];
    }

    return null;
  }
}

// Helper function to check if a string is a valid SQL query
export function isValidSQL(sql: string): boolean {
  if (!sql || !sql.trim()) {
    return false;
  }
  
  try {
    const parser = new ClickhouseSQLParser();
    parser.parse(sql);
    return true;
  } catch (error) {
    return false;
  }
}

/**
 * Parse SQL query to extract various components
 * 
 * @param sql SQL query to parse
 * @returns Parsed SQL structure
 */
export function parseSQL(sql: string): ParsedSQL {
  const tokenizer = new SQLTokenizer();
  return tokenizer.parseSQL(sql);
}

/**
 * Add time range conditions and an optional limit to a SQL query
 * 
 * @param sql SQL query
 * @param timeField Field name for timestamp
 * @param startTime Start timestamp
 * @param endTime End timestamp 
 * @param limit Optional limit to add if not already present
 * @returns SQL query with time range conditions and limit
 */
export function addTimeRangeToSQL(
  sql: string,
  timeField: string = 'timestamp',
  startTime?: string | number,
  endTime?: string | number,
  limit: number = 100
): string {
  const tokenizer = new SQLTokenizer();
  const parsed = tokenizer.parseSQL(sql);
  
  // Add time range conditions
  const withTimeRange = tokenizer.addTimeRangeConditions(parsed, timeField, startTime, endTime);
  
  // Add LIMIT if not already present
  if (!withTimeRange.limit) {
    withTimeRange.limit = {
      clause: 'LIMIT',
      content: limit.toString(),
      position: {
        startIndex: -1,
        endIndex: -1
      }
    };
  }
  
  return tokenizer.reconstructSQL(withTimeRange);
}

/**
 * Extract table name from SQL query
 * 
 * @param sql SQL query
 * @returns Extracted table name or null if not found
 */
export function extractTableFromSQL(sql: string): string | null {
  const tokenizer = new SQLTokenizer();
  const parsed = tokenizer.parseSQL(sql);
  return tokenizer.extractTableName(parsed);
}
