import { Parser as LogchefQLParser, Node } from "./logchefql";
import { translateToSQLConditions } from "./logchefql/api";
import { SQLParser } from "./clickhouse-sql/ast";

/**
 * QueryBuilder is a service that handles all query transformations
 * including LogchefQL to SQL conversion, timestamp handling, and format conversions.
 */
export class QueryBuilder {
  /**
   * Builds a complete SQL query from LogchefQL input
   * @param logchefqlQuery The LogchefQL query
   * @param options Query building options
   * @returns Complete SQL query ready for execution
   */
  static buildSqlFromLogchefQL(
    logchefqlQuery: string,
    options: QueryBuildOptions
  ): QueryResult {
    const {
      tableName,
      tsField,
      startTimestamp,
      endTimestamp,
      limit,
      includeTimeFilter,
      forDisplay,
    } = options;

    try {
      // Parse the LogchefQL query
      const parser = new LogchefQLParser();
      parser.parse(logchefqlQuery);

      // Generate SQL conditions from LogchefQL
      let whereConditions = "";
      if (parser.root) {
        whereConditions = translateToSQLConditions(parser.root);
      }

      // Build the base query without time filter
      let sqlQuery = `SELECT * FROM ${tableName}`;

      // Add WHERE clause with conditions if they exist
      if (whereConditions && whereConditions.trim()) {
        sqlQuery += `\nWHERE ${whereConditions}`;
      }

      // Add time filter if needed
      if (includeTimeFilter) {
        // Convert timestamps to milliseconds
        const startMs = startTimestamp * 1000;
        const endMs = endTimestamp * 1000;

        // Format the time condition based on display preference
        const timeCondition = this.formatTimeCondition(
          tsField,
          startMs,
          endMs,
          forDisplay
        );

        // Add time condition to the query
        if (sqlQuery.includes("WHERE")) {
          sqlQuery += `\n  AND ${timeCondition}`;
        } else {
          sqlQuery += `\nWHERE ${timeCondition}`;
        }
      }

      // Add ORDER BY and LIMIT
      sqlQuery += `\nORDER BY ${tsField} DESC`;
      if (limit) {
        sqlQuery += `\nLIMIT ${limit}`;
      }

      return {
        sql: sqlQuery,
        success: true,
        error: null,
      };
    } catch (error) {
      console.error("Error building SQL from LogchefQL:", error);
      return {
        sql: "",
        success: false,
        error: error instanceof Error ? error.message : String(error),
      };
    }
  }

  /**
   * Prepares an SQL query for execution with proper timestamp handling
   * @param sqlQuery The SQL query to prepare
   * @param options Query building options
   * @returns SQL query ready for execution
   */
  static prepareQueryForExecution(
    sqlQuery: string,
    options: QueryBuildOptions
  ): QueryResult {
    const { tsField, startTimestamp, endTimestamp, includeTimeFilter } =
      options;

    // Add better logging for debugging the incoming query
    console.log("prepareQueryForExecution received:", {
      sqlQuery,
      type: typeof sqlQuery,
      isEmpty: !sqlQuery || !sqlQuery.trim(),
      hasTableName: sqlQuery && sqlQuery.includes('FROM '),
      isActive: document.hasFocus(),
    });

    if (!sqlQuery || (typeof sqlQuery === "string" && !sqlQuery.trim())) {
      console.error("Empty SQL query received in prepareQueryForExecution");
      // Provide a default query instead of an error
      return {
        sql: this.getDefaultSQLQuery(options),
        success: true,
        error: null,
      };
    }
    
    // Check if query has a FROM clause with a table name
    const hasTableNameMatch = sqlQuery.match(/FROM\s+([^\s\n]+)/i);
    
    if (hasTableNameMatch && hasTableNameMatch[1]) {
      const tableName = hasTableNameMatch[1];
      console.log(`Query has specific table name: ${tableName}, preserving it`);
    }

    try {
      // Convert timestamps to milliseconds
      const startMs = startTimestamp * 1000;
      const endMs = endTimestamp * 1000;

      // Check if query has a timestamp filter
      const hasTimeFilter = this.hasTimestampFilter(sqlQuery, tsField);

      // Parse the SQL using the AST parser
      const parsedQuery = SQLParser.parse(sqlQuery, tsField);

      if (!parsedQuery) {
        throw new Error("Failed to parse SQL query");
      }

      let resultSql: string;

      // If query already has a timestamp filter, respect it
      if (hasTimeFilter) {
        // Convert any human-readable dates to timestamp functions
        resultSql = this.convertHumanReadableDatesToFunctions(
          sqlQuery,
          tsField,
          startMs,
          endMs
        );
      } else if (includeTimeFilter) {
        // Add timestamp filter using the AST
        const timeFilterQuery = SQLParser.applyTimeRange(
          parsedQuery,
          tsField,
          new Date(startMs).toISOString(),
          new Date(endMs).toISOString()
        );

        // Convert to SQL and replace ISO strings with timestamp functions
        resultSql = SQLParser.toSQL(timeFilterQuery).replace(
          new RegExp(`${tsField}\\s+BETWEEN\\s+'[^']+'\\s+AND\\s+'[^']+'`, "i"),
          `${tsField} BETWEEN fromUnixTimestamp64Milli(${startMs}) AND fromUnixTimestamp64Milli(${endMs})`
        );
      } else {
        // No timestamp filter needed
        resultSql = sqlQuery;
      }

      return {
        sql: resultSql,
        success: true,
        error: null,
      };
    } catch (error) {
      console.error("Error preparing query for execution:", error);

      // Fallback to simple handling when AST parsing fails
      try {
        return this.fallbackPrepareQuery(sqlQuery, options);
      } catch (fallbackError) {
        return {
          sql: "",
          success: false,
          error:
            fallbackError instanceof Error
              ? fallbackError.message
              : String(fallbackError),
        };
      }
    }
  }

  /**
   * Fallback method for preparing a query when AST parsing fails
   * @param sqlQuery The SQL query
   * @param options Query options
   * @returns Prepared query
   */
  private static fallbackPrepareQuery(
    sqlQuery: string,
    options: QueryBuildOptions
  ): QueryResult {
    const { tsField, startTimestamp, endTimestamp, includeTimeFilter } =
      options;

    // Convert timestamps to milliseconds
    const startMs = startTimestamp * 1000;
    const endMs = endTimestamp * 1000;

    // Format time condition
    const timeCondition = `${tsField} BETWEEN fromUnixTimestamp64Milli(${startMs}) AND fromUnixTimestamp64Milli(${endMs})`;

    // Check for existing timestamp filter
    const hasTimeFilter = this.hasTimestampFilter(sqlQuery, tsField);

    if (hasTimeFilter) {
      // Try to replace existing timestamp filter with regex patterns
      const patterns = [
        new RegExp(`${tsField}\\s+BETWEEN\\s+'[^']+'\\s+AND\\s+'[^']+'`, "i"),
        new RegExp(`${tsField}\\s+BETWEEN\\s+\\d+\\s+AND\\s+\\d+`, "i"),
        new RegExp(
          `${tsField}\\s+BETWEEN\\s+fromUnixTimestamp64Milli\\(\\d+\\)\\s+AND\\s+fromUnixTimestamp64Milli\\(\\d+\\)`,
          "i"
        ),
      ];

      for (const pattern of patterns) {
        if (pattern.test(sqlQuery)) {
          return {
            sql: sqlQuery.replace(pattern, timeCondition),
            success: true,
            error: null,
          };
        }
      }

      // Couldn't find a pattern to replace, return as is
      return { sql: sqlQuery, success: true, error: null };
    } else if (includeTimeFilter) {
      // Add time filter based on query structure
      const hasWhere = /\bWHERE\b/i.test(sqlQuery);

      if (hasWhere) {
        // Add to existing WHERE clause
        return {
          sql: sqlQuery.replace(/WHERE/i, `WHERE ${timeCondition} AND `),
          success: true,
          error: null,
        };
      } else {
        // Add new WHERE clause
        return {
          sql: sqlQuery.replace(
            /FROM\s+([^\n]+)/i,
            `FROM $1\nWHERE ${timeCondition}`
          ),
          success: true,
          error: null,
        };
      }
    } else {
      // No time filter needed
      return { sql: sqlQuery, success: true, error: null };
    }
  }

  /**
   * Formats a SQL query for display with human-readable dates
   * @param sqlQuery The SQL query to format
   * @returns SQL query with human-readable dates
   */
  static formatQueryForDisplay(sqlQuery: string): string {
    if (!sqlQuery) return "";

    // Replace fromUnixTimestamp64Milli timestamp formats with human-readable dates
    return sqlQuery.replace(
      /fromUnixTimestamp64Milli\((\d+)\)/g,
      (match, timestamp) => {
        const date = new Date(parseInt(timestamp));

        // Format in local timezone as YYYY-MM-DD HH:MM:SS
        const year = date.getFullYear();
        const month = String(date.getMonth() + 1).padStart(2, "0");
        const day = String(date.getDate()).padStart(2, "0");
        const hours = String(date.getHours()).padStart(2, "0");
        const minutes = String(date.getMinutes()).padStart(2, "0");
        const seconds = String(date.getSeconds()).padStart(2, "0");

        return `'${year}-${month}-${day} ${hours}:${minutes}:${seconds}'`;
      }
    );
  }

  /**
   * Formats a time condition for a query
   * @param tsField Timestamp field name
   * @param startMs Start timestamp in milliseconds
   * @param endMs End timestamp in milliseconds
   * @param forDisplay Whether to format for display (human-readable) or execution
   * @returns Formatted time condition
   */
  static formatTimeCondition(
    tsField: string,
    startMs: number,
    endMs: number,
    forDisplay: boolean = false
  ): string {
    if (forDisplay) {
      // Format dates as human-readable strings in local timezone
      const formatDate = (timestamp: number): string => {
        const date = new Date(timestamp);
        const year = date.getFullYear();
        const month = String(date.getMonth() + 1).padStart(2, "0");
        const day = String(date.getDate()).padStart(2, "0");
        const hours = String(date.getHours()).padStart(2, "0");
        const minutes = String(date.getMinutes()).padStart(2, "0");
        const seconds = String(date.getSeconds()).padStart(2, "0");

        return `${year}-${month}-${day} ${hours}:${minutes}:${seconds}`;
      };

      return `${tsField} BETWEEN '${formatDate(startMs)}' AND '${formatDate(
        endMs
      )}'`;
    } else {
      // Use timestamp functions for execution
      return `${tsField} BETWEEN fromUnixTimestamp64Milli(${startMs}) AND fromUnixTimestamp64Milli(${endMs})`;
    }
  }

  /**
   * Checks if a SQL query has a timestamp filter
   * @param sqlQuery The SQL query to check
   * @param tsField Timestamp field name
   * @returns True if the query has a timestamp filter
   */
  static hasTimestampFilter(sqlQuery: string, tsField: string): boolean {
    try {
      const parsedQuery = SQLParser.parse(sqlQuery, tsField);
      return !!parsedQuery?.whereClause?.hasTimestampFilter;
    } catch (error) {
      // Fallback to regex check if parsing fails
      const timeFilterRegex = new RegExp(
        `${tsField}\\s+(BETWEEN|>=|>|<=|<|=)`,
        "i"
      );
      return timeFilterRegex.test(sqlQuery);
    }
  }

  /**
   * Converts human-readable dates in a query to timestamp functions
   * @param sqlQuery The SQL query with human-readable dates
   * @param tsField Timestamp field name
   * @param startMs Start timestamp in milliseconds
   * @param endMs End timestamp in milliseconds
   * @returns SQL query with timestamp functions
   */
  private static convertHumanReadableDatesToFunctions(
    sqlQuery: string,
    tsField: string,
    startMs: number,
    endMs: number
  ): string {
    // Find and replace human-readable dates with timestamp functions
    const timeConditionRegex = new RegExp(
      `${tsField}\\s+BETWEEN\\s+'([^']+)'\\s+AND\\s+'([^']+)'`,
      "i"
    );

    const match = sqlQuery.match(timeConditionRegex);
    if (match) {
      // Replace with timestamp functions
      return sqlQuery.replace(
        timeConditionRegex,
        `${tsField} BETWEEN fromUnixTimestamp64Milli(${startMs}) AND fromUnixTimestamp64Milli(${endMs})`
      );
    }

    // If no matches found, return as is
    return sqlQuery;
  }

  /**
   * Creates a default SQL query
   * @param options Query options
   * @returns Default SQL query
   */
  static getDefaultSQLQuery(options: QueryBuildOptions): string {
    const {
      tableName,
      tsField,
      startTimestamp,
      endTimestamp,
      limit,
      includeTimeFilter,
      forDisplay,
    } = options;

    // Ensure we have a valid table name before building a query
    if (!tableName) {
      console.warn('getDefaultSQLQuery called with empty tableName');
      // Instead of returning empty string, preserve the FROM clause for later substitution
      let sqlQuery = `SELECT * FROM `;

      // Add time filter if needed
      if (includeTimeFilter) {
        const startMs = startTimestamp * 1000;
        const endMs = endTimestamp * 1000;

        const timeCondition = this.formatTimeCondition(
          tsField,
          startMs,
          endMs,
          forDisplay
        );

        sqlQuery += `\nWHERE ${timeCondition}`;
      }

      // Add order by and limit
      sqlQuery += `\nORDER BY ${tsField} DESC`;

      if (limit) {
        sqlQuery += `\nLIMIT ${limit}`;
      }

      return sqlQuery;
    }

    // Build a simple default query with the provided table name
    let sqlQuery = `SELECT * FROM ${tableName}`;

    // Add time filter if needed
    if (includeTimeFilter) {
      const startMs = startTimestamp * 1000;
      const endMs = endTimestamp * 1000;

      const timeCondition = this.formatTimeCondition(
        tsField,
        startMs,
        endMs,
        forDisplay
      );

      sqlQuery += `\nWHERE ${timeCondition}`;
    }

    // Add order by and limit
    sqlQuery += `\nORDER BY ${tsField} DESC`;

    if (limit) {
      sqlQuery += `\nLIMIT ${limit}`;
    }

    return sqlQuery;
  }

  /**
   * Format a timestamp for display in local timezone
   * @param timestamp Unix timestamp in seconds
   * @returns Formatted date string in YYYY-MM-DD HH:MM:SS format
   */
  static formatTimestampForDisplay(timestamp: number): string {
    const date = new Date(timestamp * 1000);

    // Format date as YYYY-MM-DD HH:MM:SS in user's local timezone
    const year = date.getFullYear();
    const month = String(date.getMonth() + 1).padStart(2, "0");
    const day = String(date.getDate()).padStart(2, "0");
    const hours = String(date.getHours()).padStart(2, "0");
    const minutes = String(date.getMinutes()).padStart(2, "0");
    const seconds = String(date.getSeconds()).padStart(2, "0");

    return `${year}-${month}-${day} ${hours}:${minutes}:${seconds}`;
  }
}

/**
 * Options for building a query
 */
export interface QueryBuildOptions {
  /** Table name */
  tableName: string;
  /** Timestamp field name */
  tsField: string;
  /** Start timestamp in seconds (unix epoch) */
  startTimestamp: number;
  /** End timestamp in seconds (unix epoch) */
  endTimestamp: number;
  /** Maximum number of results to return */
  limit?: number;
  /** Whether to include time filters */
  includeTimeFilter: boolean;
  /** Whether to format for display (with readable dates) */
  forDisplay?: boolean;
}

/**
 * Result of a query building operation
 */
export interface QueryResult {
  /** The resulting SQL query */
  sql: string;
  /** Whether the operation was successful */
  success: boolean;
  /** Error message if operation failed */
  error: string | null;
}
