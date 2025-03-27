import { parseAndTranslateLogchefQL } from './logchefql/api';
import { format } from 'date-fns'; // Using date-fns for reliable formatting
import { Parser as LogchefQLParser } from './logchefql';

// Interface for build options
export interface BuildSqlOptions {
  tableName: string;
  tsField: string;
  startTimestamp: number; // Unix timestamp in seconds
  endTimestamp: number;   // Unix timestamp in seconds
  limit: number;
  logchefqlQuery?: string; // Optional LogchefQL query string
  selectColumns?: string[]; // Default to '*'
  orderByField?: string; // Default to tsField
  orderByDirection?: 'ASC' | 'DESC'; // Default to DESC
}

// Interface for the result with enhanced error handling
export interface QueryResult {
  /** The resulting SQL query */
  sql: string;
  /** Whether the operation was successful */
  success: boolean;
  /** Error message if operation failed */
  error: string | null;
  /** Optional warnings that didn't prevent query generation */
  warnings?: string[];
  /** Metadata about the query for analytics */
  meta?: {
    fieldsUsed: string[];
    operations: ('filter' | 'sort' | 'limit')[];
  };
}

export class QueryBuilder {

  /**
   * Formats a Unix timestamp (seconds) into ClickHouse DateTime string format 'YYYY-MM-DD HH:MM:SS'.
   */
  private static formatTimestampForSQL(timestampSeconds: number): string {
    // ClickHouse generally expects 'YYYY-MM-DD HH:MM:SS' for DateTime fields in WHERE clauses
    const date = new Date(timestampSeconds * 1000);
    // Ensure date is valid before formatting
    if (isNaN(date.getTime())) {
        throw new Error(`Invalid timestamp provided: ${timestampSeconds}`);
    }
    return format(date, 'yyyy-MM-dd HH:mm:ss');
  }

  /**
   * Analyzes a LogchefQL query to extract metadata about fields and operations used
   */
  private static analyzeQuery(parser: LogchefQLParser): QueryResult['meta'] {
    const fieldsUsed: string[] = [];
    const operations: ('filter' | 'sort' | 'limit')[] = ['limit']; // Always has limit
    
    // Extract fields from the parser's typed chars
    parser.typedChars.forEach(([char, type]) => {
      if (type === 'logchefqlKey' && char.value && !fieldsUsed.includes(char.value)) {
        fieldsUsed.push(char.value);
      }
    });
    
    if (fieldsUsed.length > 0) {
      operations.push('filter');
    }
    
    return { fieldsUsed, operations };
  }

  /**
   * Creates the time condition part of the WHERE clause using standard DateTime format.
   * Ensures the timestamp field is quoted with backticks.
   */
  private static formatTimeCondition(tsField: string, startSeconds: number, endSeconds: number): string {
    try {
        const formattedStart = QueryBuilder.formatTimestampForSQL(startSeconds);
        const formattedEnd = QueryBuilder.formatTimestampForSQL(endSeconds);
        // Use backticks for the timestamp field identifier
        return `\`${tsField}\` BETWEEN '${formattedStart}' AND '${formattedEnd}'`;
    } catch (error: any) {
        console.error("Error formatting time condition:", error);
        // Re-throw or handle as appropriate, maybe return an empty string or a specific error condition
        throw new Error(`Failed to format time condition: ${error.message}`);
    }
  }

  /**
   * Builds a complete ClickHouse SQL query from LogchefQL and other options.
   * This function constructs the final, executable query with enhanced error handling.
   */
  static buildSqlFromLogchefQL(options: BuildSqlOptions): QueryResult {
    const {
      tableName,
      tsField,
      startTimestamp,
      endTimestamp,
      limit,
      logchefqlQuery,
      selectColumns = ['*'], // Default to selecting all columns
      orderByField = tsField, // Default ordering by timestamp field
      orderByDirection = 'DESC', // Default to descending order
    } = options;

    // --- Input Validation ---
    if (!tableName) {
      return { success: false, sql: "", error: "Table name is required." };
    }
    if (!tsField) {
        return { success: false, sql: "", error: "Timestamp field name is required." };
    }
    if (typeof startTimestamp !== 'number' || typeof endTimestamp !== 'number' || startTimestamp > endTimestamp) {
        return { success: false, sql: "", error: "Invalid start or end timestamp." };
    }
    if (typeof limit !== 'number' || limit <= 0) {
        return { success: false, sql: "", error: "Invalid limit value." };
    }

    // --- Format Time Condition ---
    let timeCondition: string;
    try {
        timeCondition = QueryBuilder.formatTimeCondition(tsField, startTimestamp, endTimestamp);
    } catch (error: any) {
        return { success: false, sql: "", error: error.message };
    }

    // --- Prepare base query components ---
    // Quote column names if they aren't '*' or potentially complex expressions/functions
    const safeSelectColumns = selectColumns.map(col =>
        col === '*' || col.includes('(') || col.includes(' ') ? col : `\`${col}\``
    ).join(', ');
    const selectClause = `SELECT ${safeSelectColumns}`;
    const fromClause = `FROM \`${tableName}\``;
    const timeWhereClause = `WHERE (${timeCondition})`;
    const orderByClause = `ORDER BY \`${orderByField}\` ${orderByDirection}`;
    const limitClause = `LIMIT ${limit}`;

    // --- Translate LogchefQL ---
    const warnings: string[] = [];
    let logchefqlConditions = "";
    let meta: QueryResult['meta'] = {
      fieldsUsed: [],
      operations: ['sort', 'limit'] // Base operations
    };

    if (logchefqlQuery && logchefqlQuery.trim()) {
      try {
        // First try to parse with our parser to extract metadata
        const parser = new LogchefQLParser();
        parser.parse(logchefqlQuery, false, false);
        
        if (parser.state === 'Error') {
          warnings.push(`LogchefQL parse warning: ${parser.errorText}`);
        } else {
          meta = QueryBuilder.analyzeQuery(parser);
        }
        
        // Then use the translator to get SQL conditions
        const translationResult = parseAndTranslateLogchefQL(logchefqlQuery);
        if (!translationResult.success) {
          // Don't fail completely, just add warning and continue with base query
          warnings.push(translationResult.error || "Failed to translate LogchefQL.");
        } else {
          // Assign the translated conditions
          logchefqlConditions = translationResult.sql || "";
        }
      } catch (error: any) {
        // Capture error but don't fail - use base query instead
        warnings.push(`LogchefQL error: ${error.message}`);
      }
    }

    // --- Combine WHERE conditions ---
    let whereClause = timeWhereClause;
    if (logchefqlConditions) {
      whereClause = `WHERE (${timeCondition}) AND (${logchefqlConditions})`;
      meta.operations.push('filter');
    }

    // --- Assemble the final query string ---
    const finalSql = [
      selectClause,
      fromClause,
      whereClause,
      orderByClause,
      limitClause,
    ].join('\n'); // Join with newlines for readability

    return { 
      success: true, 
      sql: finalSql, 
      error: null,
      warnings: warnings.length > 0 ? warnings : undefined,
      meta
    };
  }

  /**
   * Generates a default SQL query when no specific LogchefQL is provided.
   * Uses the same structure as buildSqlFromLogchefQL but without LogchefQL translation.
   * Returns a QueryResult with success status and metadata.
   */
  static getDefaultSQLQuery(options: Omit<BuildSqlOptions, 'logchefqlQuery'>): QueryResult {
     const {
       tableName,
       tsField,
       startTimestamp,
       endTimestamp,
       limit,
       selectColumns = ['*'],
       orderByField = tsField,
       orderByDirection = 'DESC',
     } = options;

     // Basic validation for default query generation
     if (!tableName || !tsField) {
         console.warn("Cannot generate default SQL: Missing tableName or tsField.");
         // Return a placeholder template with error
         return {
           success: false,
           sql: `SELECT *\nFROM your_table\nORDER BY timestamp_field DESC\nLIMIT 1000`,
           error: "Missing table name or timestamp field",
           warnings: ["Using placeholder query"]
         };
     }
     
     if (typeof startTimestamp !== 'number' || typeof endTimestamp !== 'number' || typeof limit !== 'number') {
         console.warn("Cannot generate default SQL: Invalid timestamp or limit.");
         return {
           success: false,
           sql: `SELECT *\nFROM \`${tableName}\`\n-- Invalid time range or limit provided\nORDER BY \`${tsField}\` DESC\nLIMIT 1000`,
           error: "Invalid time range or limit parameters",
           warnings: ["Using fallback query with default values"]
         };
     }

     let timeCondition: string;
     try {
         timeCondition = QueryBuilder.formatTimeCondition(tsField, startTimestamp, endTimestamp);
     } catch (error: any) {
         console.error("Error formatting time condition for default query:", error);
         // Handle error, maybe return query without time filter or with a comment
         return {
           success: false,
           sql: `SELECT ${selectColumns.join(', ')}\nFROM \`${tableName}\`\n-- Error generating time filter: ${error.message}\nORDER BY \`${orderByField}\` ${orderByDirection}\nLIMIT ${limit}`,
           error: `Error formatting time condition: ${error.message}`,
           warnings: ["Using query without time filter"]
         };
     }

     // Quote identifiers similarly to buildSqlFromLogchefQL
     const safeSelectColumns = selectColumns.map(col =>
        col === '*' || col.includes('(') || col.includes(' ') ? col : `\`${col}\``
     ).join(', ');

     const sql = [
       `SELECT ${safeSelectColumns}`,
       `FROM \`${tableName}\``,
       `WHERE ${timeCondition}`, // Always include time condition for default query
       `ORDER BY \`${orderByField}\` ${orderByDirection}`,
       `LIMIT ${limit}`,
     ].join('\n');

     return {
       success: true,
       sql,
       error: null,
       meta: {
         fieldsUsed: [],
         operations: ['sort', 'limit']
       }
     };
  }

  /**
   * Formats a SQL query for display purposes, replacing specific timestamp
   * functions or formats with human-readable dates in the user's local timezone.
   * NOTE: This is for DISPLAY ONLY and does not affect query execution.
   * It currently targets the 'YYYY-MM-DD HH:MM:SS' format used in WHERE clauses.
   *
   * @param sqlQuery The SQL query string potentially containing time conditions.
   * @param tsField The timestamp field name used in the query.
   * @returns SQL query string with time conditions potentially formatted for display.
   */
  static formatQueryForDisplay(sqlQuery: string, tsField: string): string {
    if (!sqlQuery || !tsField) return sqlQuery;

    // Regex to find the specific BETWEEN clause format we generate
    // It captures the field, start date, and end date strings
    const timeConditionRegex = new RegExp(
      // Match the timestamp field (potentially quoted)
      `([\\\`]?${tsField}[\\\`]?)` +
      // Match BETWEEN and the opening quote for the start date
      `\\s+BETWEEN\\s+'` +
      // Capture the start date string (YYYY-MM-DD HH:MM:SS)
      `(\\d{4}-\\d{2}-\\d{2}\\s+\\d{2}:\\d{2}:\\d{2})` +
      // Match the closing quote, AND, and opening quote for the end date
      `'\\s+AND\\s+'` +
      // Capture the end date string (YYYY-MM-DD HH:MM:SS)
      `(\\d{4}-\\d{2}-\\d{2}\\s+\\d{2}:\\d{2}:\\d{2})` +
      // Match the final closing quote
      `'`,
      'gi' // Global and case-insensitive search
    );

    return sqlQuery.replace(timeConditionRegex, (match, field, startDateStr, endDateStr) => {
      try {
        // Attempt to parse the captured date strings (assuming they are UTC or server time)
        // and format them nicely in the user's local time zone.
        const startDate = new Date(startDateStr + 'Z'); // Assume UTC if no timezone info
        const endDate = new Date(endDateStr + 'Z'); // Assume UTC

        // Format in local timezone (e.g., "yyyy-MM-dd HH:mm:ss")
        // Adjust format string as desired for display
        const localStartDate = format(startDate, 'yyyy-MM-dd HH:mm:ss');
        const localEndDate = format(endDate, 'yyyy-MM-dd HH:mm:ss');

        // Reconstruct the clause with local dates (still as strings for display)
        // Note: This is purely for visual representation in the editor.
        return `${field} BETWEEN '${localStartDate}' AND '${localEndDate}' /* Local Time */`;

      } catch (e) {
        // If parsing/formatting fails, return the original match
        console.warn("Could not format date for display:", e);
        return match;
      }
    });
  }
}
