import { parseAndTranslateLogchefQL } from '@/utils/logchefql/api';
import { validateSQL, validateSQLWithDetails, analyzeQuery } from '@/utils/clickhouse-sql';
import {
  createTimeRangeCondition,
  timeRangeToCalendarDateTime,
  formatDateForSQL,
  getUserTimezone,
  formatTimezoneForSQL
} from '@/utils/time-utils';
import type { QueryOptions, QueryResult, TimeRange, TimeRangeInfo } from '@/types/query';

// Define the QueryCondition type used in meta
export interface QueryCondition {
  field: string;
  operator: string;
  value: string | number | boolean;
  type?: string;
}

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

    // Create timezone-aware time condition
    const timeCondition = createTimeRangeCondition(tsField, timeRange, true);
    const limitClause = `LIMIT ${limit}`;

    // --- Translate LogchefQL ---
    const warnings: string[] = [];
    let logchefqlConditions = '';
    const meta: NonNullable<QueryResult['meta']> = {
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

          // Add conditions if available from the enhanced translation
          if (translationResult.meta && Array.isArray(translationResult.meta.conditions)) {
            meta.conditions = [...translationResult.meta.conditions];
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

    // Add timezone metadata
    const userTimezone = getUserTimezone();
    meta.timeRangeInfo = {
      field: tsField,
      startTime: formatDateForSQL(timeRange.start, false),
      endTime: formatDateForSQL(timeRange.end, false),
      timezone: userTimezone,
      isTimezoneAware: true
    };

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

      // Create timezone-aware time condition
      const timeCondition = createTimeRangeCondition(tsField, timeRange, true);

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

      // Add timezone metadata
      const userTimezone = getUserTimezone();
      const meta: NonNullable<QueryResult['meta']> = {
        fieldsUsed: [],
        operations: ['sort', 'limit'] as ('sort' | 'limit')[],
        timeRangeInfo: {
          field: tsField,
          startTime: formatDateForSQL(timeRange.start, false),
          endTime: formatDateForSQL(timeRange.end, false),
          timezone: userTimezone,
          isTimezoneAware: true
        }
      };

      return {
        success: true,
        sql,
        error: null,
        meta
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
    // Leverage the enhanced validation function
    return validateSQL(sql);
  }

  /**
   * Validates a SQL query and returns detailed information
   * @param sql SQL query to validate
   * @returns Detailed validation result
   */
  static validateSQLWithDetails(sql: string): {
    valid: boolean;
    error?: string;
    ast?: any;
    tables?: string[];
    columns?: string[];
  } {
    return validateSQLWithDetails(sql);
  }

  /**
   * Analyzes a SQL query for structure and components
   * @param sql SQL query to analyze
   * @returns Analysis results or null if parsing fails
   */
  static analyzeQuery(sql: string) {
    return analyzeQuery(sql);
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

      // Enhanced validation with detailed information
      const validationResult = this.validateSQLWithDetails(query);
      if (!validationResult.valid) {
        return {
          success: false,
          sql: query,
          error: validationResult.error || "Invalid SQL syntax"
        };
      }

      // Analyze the query for additional information
      const queryAnalysis = this.analyzeQuery(query);

      // Final SQL to be executed - might be modified with automatic limit
      let finalSql = query;

      // Automatically add LIMIT if not present in the query
      if (queryAnalysis && !queryAnalysis.hasLimit) {
        finalSql = `${finalSql}\nLIMIT ${limit}`;
      }

      // The query is valid, return it with enhanced metadata
      return {
        success: true,
        sql: finalSql,
        error: null,
        meta: {
          fieldsUsed: validationResult.columns || [],
          operations: ['filter', 'sort'], // Assume these operations exist in most queries
          queryAnalysis: queryAnalysis || undefined,
          ...(queryAnalysis?.timeRangeInfo && { timeRangeInfo: queryAnalysis.timeRangeInfo })
        }
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