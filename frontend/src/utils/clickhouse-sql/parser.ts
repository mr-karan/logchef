import { Parser as ClickhouseSQLParser } from './index';

/**
 * Simple SQL parser for basic operations
 */
export class SQLParser {
  private baseParser: ClickhouseSQLParser;

  constructor() {
    this.baseParser = new ClickhouseSQLParser();
  }

  /**
   * Validate SQL query
   * 
   * @param sql SQL query to validate
   * @returns True if valid, false otherwise
   */
  public validateSQL(sql: string): boolean {
    if (!sql || !sql.trim()) {
      return false;
    }
    
    try {
      this.baseParser.parse(sql);
      return true;
    } catch (error) {
      return false;
    }
  }

  /**
   * Extract table name from SQL query
   * 
   * @param sql SQL query
   * @returns Table name or null if not found
   */
  public extractTableName(sql: string): string | null {
    if (!sql || !sql.trim()) {
      return null;
    }

    try {
      // Simple regex to extract table name from FROM clause
      const fromMatch = sql.match(/FROM\s+([a-zA-Z0-9_.]+)(?:\s+|$)/i);
      if (fromMatch && fromMatch[1]) {
        return fromMatch[1].trim();
      }
      
      return null;
    } catch (error) {
      console.error("Error extracting table name:", error);
      return null;
    }
  }
}

/**
 * Check if a string is a valid SQL query
 * 
 * @param sql SQL query to check
 * @returns True if valid, false otherwise
 */
export function isValidSQL(sql: string): boolean {
  const parser = new SQLParser();
  return parser.validateSQL(sql);
}

/**
 * Extract table name from SQL query
 * 
 * @param sql SQL query
 * @returns Table name or null if not found
 */
export function extractTableFromSQL(sql: string): string | null {
  const parser = new SQLParser();
  return parser.extractTableName(sql);
}

/**
 * Re-export addTimeRangeToSQL from api.ts to maintain compatibility
 */
export { addTimeRangeToSQL } from './api';
