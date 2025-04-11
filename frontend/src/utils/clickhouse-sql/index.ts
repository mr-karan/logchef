// Export key elements from language.ts
export { CharType, tokenTypes, SQL_KEYWORDS, CLICKHOUSE_FUNCTIONS, SQL_TYPES, isNumeric } from './language';
import { Parser } from 'node-sql-parser';

// Configure parser options
const parserOptions = {
  database: 'PostgreSQL' // Use PostgreSQL mode as it's close enough to ClickHouse for our validation needs
};

// Create a parser instance for reuse
const sqlParser = new Parser();

/**
 * Validates a ClickHouse SQL query using a proper SQL parser
 * @param query The SQL query to validate
 * @returns Whether the query appears to have valid syntax
 */
export function validateSQL(query: string): boolean {
  // Empty query is not valid for direct execution
  if (!query || !query.trim()) {
    return false;
  }

  try {
    // Use the parser to validate the SQL syntax
    sqlParser.astify(query, parserOptions);
    return true;
  } catch (error) {
    console.debug("SQL validation error:", error);
    return false;
  }
}

/**
 * Validates a SQL query and returns detailed information about the validation
 * @param query The SQL query to validate
 * @returns Object with validation status, error message if any, and the AST if valid
 */
export function validateSQLWithDetails(query: string): {
  valid: boolean;
  error?: string;
  ast?: any;
  tables?: string[];
  columns?: string[];
} {
  if (!query || !query.trim()) {
    return { valid: false, error: "Query is empty" };
  }

  try {
    // Parse the query to get the AST
    const ast = sqlParser.astify(query, parserOptions);

    // Extract tables and columns
    const { tableList, columnList } = sqlParser.parse(query, parserOptions);

    return {
      valid: true,
      ast,
      tables: tableList,
      columns: columnList
    };
  } catch (error) {
    return {
      valid: false,
      error: error instanceof Error ? error.message : String(error)
    };
  }
}

/**
 * Extract information about the query like tables, columns, and query type
 * @param query The SQL query to analyze
 * @returns Information about the query or null if parsing fails
 */
export function analyzeQuery(query: string): {
  type: string;
  tables: string[];
  columns: string[];
  hasTimeFilter: boolean;
  hasLimit: boolean;
  limitValue?: number;
  timeRangeInfo?: {
    field: string;
    startTime: string;
    endTime: string;
    timezone?: string;
    isTimezoneAware: boolean;
  };
} | null {
  try {
    const { ast, tables = [], columns = [] } = validateSQLWithDetails(query);

    if (!ast) return null;

    // Extract query type (select, insert, etc.)
    const type = ast.type || 'unknown';

    // Check if query has a LIMIT clause
    const hasLimit = !!ast.limit;
    let limitValue: number | undefined = undefined;

    if (hasLimit && ast.limit) {
      // Extract limit value if present (handle different parser output formats)
      if (typeof ast.limit === 'number') {
        limitValue = ast.limit;
      } else if (ast.limit.value) {
        limitValue = parseInt(ast.limit.value, 10);
      }
    }

    // Check if the query has time filters (approximate check)
    const queryLower = query.toLowerCase();
    const hasTimeFilter =
      queryLower.includes('timestamp') ||
      queryLower.includes('datetime') ||
      queryLower.includes('date') ||
      queryLower.includes('time');

    // Enhanced time range detection
    let timeRangeInfo = undefined;

    // Pattern to detect: Standard time range pattern - WHERE `timestamp` BETWEEN toDateTime('...') AND toDateTime('...')
    const basicTimePattern = /WHERE\s+`?(\w+)`?\s+BETWEEN\s+toDateTime\(['"](.+?)[']\)(.*?)AND\s+toDateTime\(['"](.+?)[']\)(.*?)(\s|$)/i;
    const basicTimeMatch = query.match(basicTimePattern);

    // Pattern to detect: Timezone-aware time range - WHERE `timestamp` BETWEEN toDateTime('...', 'Timezone') AND toDateTime('...', 'Timezone')
    const tzTimePattern = /WHERE\s+`?(\w+)`?\s+BETWEEN\s+toDateTime\(['"](.+?)['"],\s*['"]([^'"]+)['"]\)(.*?)AND\s+toDateTime\(['"](.+?)['"],\s*['"]([^'"]+)['"]\)(.*?)(\s|$)/i;
    const tzTimeMatch = query.match(tzTimePattern);

    if (tzTimeMatch) {
      // Extract from timezone-aware pattern
      timeRangeInfo = {
        field: tzTimeMatch[1],
        startTime: tzTimeMatch[2],
        endTime: tzTimeMatch[5],
        timezone: tzTimeMatch[3], // Extract the timezone
        isTimezoneAware: true
      };
    } else if (basicTimeMatch) {
      // Extract from basic pattern
      timeRangeInfo = {
        field: basicTimeMatch[1],
        startTime: basicTimeMatch[2],
        endTime: basicTimeMatch[4],
        isTimezoneAware: false
      };
    }

    return {
      type,
      tables,
      columns,
      hasTimeFilter,
      hasLimit,
      limitValue,
      timeRangeInfo
    };
  } catch (error) {
    console.error("Error analyzing query:", error);
    return null;
  }
}
