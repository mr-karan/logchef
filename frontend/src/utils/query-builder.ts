import { parseAndTranslateLogchefQL } from './logchefql/api';
import { format } from 'date-fns';
import { Parser as LogchefQLParser } from './logchefql';
import type { CalendarDateTime } from '@internationalized/date';

// Interface for build options
export interface BuildSqlOptions {
  tableName: string;
  tsField: string;
  startDateTime: CalendarDateTime;
  endDateTime: CalendarDateTime;
  limit: number;
  logchefqlQuery?: string; // Optional LogchefQL query string
  selectColumns?: string[]; // Default to '*' // No longer used, always SELECT *
  orderByField?: string; // Default to tsField
  orderByDirection?: 'ASC' | 'DESC'; // Default to DESC
  whereClause?: string; // Additional WHERE conditions
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
   * Formats a time condition for ClickHouse using CalendarDateTime objects.
   */
  static formatTimeCondition(tsField: string, startDateTime: CalendarDateTime, endDateTime: CalendarDateTime): string {
    try {
      // Convert CalendarDateTime to JS Date objects for formatting
      const startDate = startDateTime.toDate('UTC'); // Assuming UTC for consistency, adjust if needed
      const endDate = endDateTime.toDate('UTC');

      // Format to ClickHouse-readable datetime format
      const start = format(startDate, "yyyy-MM-dd HH:mm:ss");
      const end = format(endDate, "yyyy-MM-dd HH:mm:ss");

      // Use backticks for the timestamp field
      return `\`${tsField}\` BETWEEN toDateTime('${start}') AND toDateTime('${end}')`;
    } catch (error: any) {
      console.error("Error formatting time condition:", error);
      throw new Error(`Failed to format time condition: ${error.message}`);
    }
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
   * Builds a complete ClickHouse SQL query from LogchefQL and other options.
   * This function constructs the final, executable query with enhanced error handling.
   */
  static buildSqlFromLogchefQL(options: BuildSqlOptions): QueryResult {
    const {
      tableName,
      tsField,
      startDateTime,
      endDateTime,
      limit,
      logchefqlQuery,
      // selectColumns = ['*'], // Removed, always SELECT *
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
    if (!startDateTime || !endDateTime) {
      return { success: false, sql: "", error: "Invalid start or end date/time." };
    }
    if (typeof limit !== 'number' || limit <= 0) {
      return { success: false, sql: "", error: "Invalid limit value." };
    }
    
    // No longer requiring namespace in query

    // --- Prepare base query components ---
    const selectClause = `SELECT *`;
    const fromClause = `FROM \`${tableName}\``; // Use backticks for table name
    const orderByClause = `ORDER BY \`${orderByField}\` ${orderByDirection}`; // Use backticks for order field
    const limitClause = `LIMIT ${limit}`;

    // --- Format Time Condition ---
    let timeCondition: string;
    try {
      timeCondition = QueryBuilder.formatTimeCondition(tsField, startDateTime, endDateTime);
    } catch (error: any) {
      return { success: false, sql: "", error: error.message };
    }

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
    let whereClause = `WHERE ${timeCondition}`;
    if (logchefqlConditions) {
      whereClause += ` AND (${logchefqlConditions})`;
      if (!meta.operations.includes('filter')) {
        meta.operations.push('filter');
      }
    }

    // --- Assemble the final query string ---
    const finalSqlParts = [
      selectClause,
      fromClause,
      whereClause,
      orderByClause,
      limitClause
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
   * Uses a simplified structure with direct timestamp values.
   * Returns a QueryResult with success status and metadata.
   */
  static getDefaultSQLQuery(options: Omit<BuildSqlOptions, 'logchefqlQuery'>): QueryResult {
    const {
      tableName,
      tsField,
      startDateTime,
      endDateTime,
      limit,
      // selectColumns = ['*'], // Removed
      orderByField = tsField,
      orderByDirection = 'DESC',
    } = options;

    // Basic validation
    if (!tableName || !tsField) {
      console.warn("Cannot generate default SQL: Missing tableName or tsField.");
      return {
        success: false,
        sql: `SELECT *\nFROM your_table\nORDER BY timestamp_field DESC\nLIMIT 100`, // Adjusted default limit
        error: "Missing table name or timestamp field",
        warnings: ["Using placeholder query"]
      };
    }

    if (!startDateTime || !endDateTime || typeof limit !== 'number') {
      console.warn("Cannot generate default SQL: Invalid date/time or limit.");
      return {
        success: false,
        sql: `SELECT *\nFROM \`${tableName}\`\n-- Invalid time range or limit provided\nORDER BY \`${tsField}\` DESC\nLIMIT 100`,
        error: "Invalid time range or limit parameters",
        warnings: ["Using fallback query with default values"]
      };
    }

    // Format time condition
    let timeCondition: string;
    try {
      timeCondition = QueryBuilder.formatTimeCondition(tsField, startDateTime, endDateTime);
    } catch (error: any) {
      console.warn("Cannot generate default SQL: Error formatting time condition.", error);
      return {
        success: false,
        sql: `SELECT *\nFROM \`${tableName}\`\n-- Error formatting time range\nORDER BY \`${tsField}\` DESC\nLIMIT ${limit}`,
        error: `Error formatting time condition: ${error.message}`,
        warnings: ["Using fallback query due to time formatting error"]
      };
    }

    // Combine conditions
    let whereClauseContent = timeCondition;
    if (options.whereClause) {
      whereClauseContent += ` AND (${options.whereClause})`;
    }

    const sql = [
      `SELECT *`,
      `FROM \`${tableName}\``, // Use backticks
      `WHERE ${whereClauseContent}`,
      `ORDER BY \`${orderByField}\` ${orderByDirection}`, // Use backticks
      `LIMIT ${limit}`
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

  // Removed formatQueryForDisplay method as we now use direct timestamp values
}
