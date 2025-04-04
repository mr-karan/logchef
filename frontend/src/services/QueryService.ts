import { parseAndTranslateLogchefQL } from '@/utils/logchefql/api';
import { validateSQL } from '@/utils/clickhouse-sql';
import { createTimeRangeCondition, timeRangeToCalendarDateTime, formatDateForSQL } from '@/utils/time-utils';
import type { QueryOptions, QueryResult, TimeRange } from '@/types/query';

/**
 * Central service for all query generation and manipulation operations
 */
export class QueryService {
  /**
   * Translates a LogchefQL query to SQL with proper error handling
   * @param options Query generation options including LogchefQL
   * @returns QueryResult with SQL and metadata
   */
  static translateLogchefQLToSQL(options: QueryOptions): QueryResult {
    const {
      tableName,
      tsField,
      timeRange,
      limit,
      logchefqlQuery = ''
    } = options;

    // --- Input Validation ---
    if (!tableName) {
      return { success: false, sql: "", error: "Table name is required." };
    }
    if (!tsField) {
      return { success: false, sql: "", error: "Timestamp field name is required." };
    }
    if (!timeRange.start || !timeRange.end) {
      return { success: false, sql: "", error: "Invalid start or end date/time." };
    }
    if (typeof limit !== 'number' || limit <= 0) {
      return { success: false, sql: "", error: "Invalid limit value." };
    }

    // Convert time range to CalendarDateTime (or use directly if it already is)
    const calendarTimeRange = timeRangeToCalendarDateTime(timeRange);
    if (!calendarTimeRange) {
      return { success: false, sql: "", error: "Failed to convert time range to proper format." };
    }

    // --- Prepare base query components ---
    const formattedTableName = tableName.includes('`') || tableName.includes('.')
      ? tableName
      : `\`${tableName}\``;

    const formattedTsField = tsField.includes('`') ? tsField : `\`${tsField}\``;
    const orderByClause = `ORDER BY ${formattedTsField} DESC`;
    const timeCondition = createTimeRangeCondition(tsField, timeRange);
    const limitClause = `LIMIT ${limit}`;

    // --- Translate LogchefQL ---
    const warnings: string[] = [];
    let logchefqlConditions = '';
    const meta: QueryResult['meta'] = {
      fieldsUsed: [],
      operations: ['sort', 'limit'] // Base operations
    };

    if (logchefqlQuery?.trim()) {
      try {
        // Use the translator to get SQL conditions
        const translationResult = parseAndTranslateLogchefQL(logchefqlQuery);
        if (!translationResult.success) {
          // Don't fail completely, just add warning and continue with base query
          warnings.push(translationResult.error || "Failed to translate LogchefQL.");
        } else {
          // Assign the translated conditions
          logchefqlConditions = translationResult.sql || "";

          // Add filter operation if we have conditions
          if (logchefqlConditions && !meta.operations.includes('filter')) {
            meta.operations.push('filter');
          }

          // If translationResult has meta and fieldsUsed, add them to our meta
          if (translationResult.meta && Array.isArray(translationResult.meta.fieldsUsed)) {
            meta.fieldsUsed = [...translationResult.meta.fieldsUsed];
          }
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
    }

    // --- Assemble the final query string ---
    const finalSqlParts = [
      `SELECT *`,
      `FROM ${formattedTableName}`,
      whereClause,
      orderByClause,
      limitClause
    ].join('\n');

    return {
      success: true,
      sql: finalSqlParts,
      error: null,
      warnings: warnings.length > 0 ? warnings : undefined,
      meta
    };
  }

  /**
   * Generates a default SQL query
   * @param options Query generation options
   * @returns QueryResult with default SQL
   */
  static generateDefaultSQL(options: QueryOptions): QueryResult {
    const {
      tableName,
      tsField,
      timeRange,
      limit,
      orderBy
    } = options;

    // Basic validation
    if (!tableName || !tsField) {
      console.warn("Cannot generate default SQL: Missing tableName or tsField.");
      return {
        success: false,
        sql: `SELECT *\nFROM your_table\nORDER BY timestamp_field DESC\nLIMIT 100`,
        error: "Missing table name or timestamp field",
        warnings: ["Using placeholder query"]
      };
    }

    if (!timeRange.start || !timeRange.end || typeof limit !== 'number') {
      console.warn("Cannot generate default SQL: Invalid date/time or limit.");
      return {
        success: false,
        sql: `SELECT *\nFROM \`${tableName}\`\n-- Invalid time range or limit provided\nORDER BY \`${tsField}\` DESC\nLIMIT 100`,
        error: "Invalid time range or limit parameters",
        warnings: ["Using fallback query with default values"]
      };
    }

    try {
      // Format table name
      const formattedTableName = tableName.includes('`') || tableName.includes('.')
        ? tableName
        : `\`${tableName}\``;

      // Format timestamp field
      const formattedTsField = tsField.includes('`') ? tsField : `\`${tsField}\``;

      // Create time condition
      const timeCondition = createTimeRangeCondition(tsField, timeRange);

      // Set order by
      const orderByField = orderBy ?
        (orderBy.field.includes('`') ? orderBy.field : `\`${orderBy.field}\``) :
        formattedTsField;
      const orderDirection = orderBy ? orderBy.direction : 'DESC';

      // Build the SQL query manually
      const sql = [
        `SELECT *`,
        `FROM ${formattedTableName}`,
        `WHERE ${timeCondition}`,
        `ORDER BY ${orderByField} ${orderDirection}`,
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
    } catch (error: any) {
      console.error("Error generating default SQL:", error);
      return {
        success: false,
        sql: `-- Error: ${error.message}\nSELECT *\nFROM \`${tableName}\`\nLIMIT ${limit}`,
        error: error.message,
        warnings: ["Error occurred during SQL generation"]
      };
    }
  }

  /**
   * Validates a SQL query
   * @param sql SQL query to validate
   * @returns Whether the query is valid
   */
  static validateSQL(sql: string): boolean {
    // Leverage the existing validation function
    return validateSQL(sql);
  }

  /**
   * Prepares a query for execution
   * @param options Query generation options
   * @returns SQL query ready for execution
   */
  static prepareQueryForExecution(options: {
    mode: 'logchefql' | 'clickhouse-sql' | 'sql';
    query: string;
    tableName: string;
    tsField: string;
    timeRange: TimeRange;
    limit: number;
  }): QueryResult {
    const { mode, query, tableName, tsField, timeRange, limit } = options;

    if (mode === 'clickhouse-sql' || mode === 'sql') {
      if (!query?.trim()) {
        // Generate default SQL if query is empty
        return this.generateDefaultSQL({ tableName, tsField, timeRange, limit });
      }

      // Validate the SQL query
      if (!this.validateSQL(query)) {
        return {
          success: false,
          sql: query,
          error: "Invalid SQL syntax"
        };
      }

      // The query is valid, return it as-is
      return {
        success: true,
        sql: query,
        error: null
      };
    } else {
      // For LogchefQL, always translate to SQL
      return this.translateLogchefQLToSQL({
        tableName,
        tsField,
        timeRange,
        limit,
        logchefqlQuery: query
      });
    }
  }
}