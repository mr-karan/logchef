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
import { SqlManager } from './SqlManager';

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
   * Generates a default SQL template without specific dates (uses placeholders)
   * @param tableName The table name to use
   * @param tsField The timestamp field name
   * @param limit The query limit
   * @returns SQL template string with placeholder for time range
   */
  static generateDefaultSQLTemplate(tableName: string, tsField: string, limit: number): string {
    // Format table and field names
    const formattedTableName = tableName.includes('`') || tableName.includes('.')
      ? tableName
      : `\`${tableName}\``;

    const formattedTsField = tsField.includes('`') ? tsField : `\`${tsField}\``;

    // Get user timezone for the template
    const userTimezone = getUserTimezone();
    const formattedTimezone = formatTimezoneForSQL(userTimezone);

    // Create a template with placeholders for actual dates
    return [
      `SELECT *`,
      `FROM ${formattedTableName}`,
      `WHERE ${formattedTsField} BETWEEN toDateTime('{{startTime}}', '${formattedTimezone}')`,
      `AND toDateTime('{{endTime}}', '${formattedTimezone}')`,
      `ORDER BY ${formattedTsField} DESC`,
      `LIMIT ${limit}`
    ].join('\n');
  }

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
      logchefqlQuery = '',
      timezone
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

    // Create timezone-aware time condition, passing the specific timezone if provided
    const timeCondition = createTimeRangeCondition(tsField, timeRange, true, timezone);
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
        // Replace dynamic variables with placeholders while preserving variable names
        const queryForParsing = logchefqlQuery.replace(/{{(\w+)}}/g, "'__VAR_$1__'");
        // Use the translator to get SQL conditions
        const translationResult = parseAndTranslateLogchefQL(queryForParsing);
        if (!translationResult.success) {
          // Don't fail completely, just add warning and continue with base query
          warnings.push(translationResult.error || "Failed to translate LogchefQL.");
        } else {
          // Assign the translated conditions
          logchefqlConditions = translationResult.sql || "";
          // Note: Keep the translated SQL even if it contains variable placeholders
          // The variable substitution will happen later in the pipeline

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
      timezone: timezone || userTimezone,
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
   * Validates a ClickHouse SQL query
   * @param query SQL query to validate
   */
  static validateSQL(query: string): boolean {
    return SqlManager.validateSql(query).valid;
  }

  /**
   * Generates a default SQL query for a given time range
   */
  static generateDefaultSQL(params: {
    tableName: string;
    tsField: string;
    timeRange: any;
    limit: number;
  }) {
    return SqlManager.generateDefaultSql({
      ...params,
      timezone: undefined // Maintain backwards compatibility
    });
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
   * Prepares a query for execution by applying time range, limit, and other constraints
   */
  static prepareQueryForExecution(params: {
    mode: 'logchefql' | 'clickhouse-sql';
    query: string;
    tableName: string;
    tsField: string;
    timeRange: any;
    limit: number;
    timezone?: string;
  }) {
    // For SQL mode, delegate to SqlManager
    if (params.mode === 'clickhouse-sql') {
      return SqlManager.prepareForExecution({
        sql: params.query,
        tsField: params.tsField,
        timeRange: params.timeRange,
        limit: params.limit,
        timezone: params.timezone
      });
    }

    // For LogchefQL mode, translate to SQL
    return this.translateLogchefQLToSQL({
      tableName: params.tableName,
      tsField: params.tsField,
      timeRange: params.timeRange,
      limit: params.limit,
      logchefqlQuery: params.query,
      timezone: params.timezone
    });
  }
}