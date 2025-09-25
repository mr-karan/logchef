import { parseAndTranslateLogchefQL } from '@/utils/logchefql/api';
import { tokenize, QueryParser, SQLVisitor } from '@/utils/logchefql/index';
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
      timezone,
      schema
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
    let parsedAST: any = null; // Store the parsed AST for later use
    const meta: NonNullable<QueryResult['meta']> = {
      fieldsUsed: [],
      operations: ['sort', 'limit'] // Base operations
    };

    if (logchefqlQuery?.trim()) {
      try {
        // Replace dynamic variables with placeholders while preserving variable names
        const queryForParsing = logchefqlQuery.replace(/{{(\w+)}}/g, "'__VAR_$1__'");

        // Parse the query to get the AST
        const { tokens } = tokenize(queryForParsing);
        const parser = new QueryParser(tokens);
        const { ast, errors } = parser.parse();

        if (errors.length > 0 || !ast) {
          warnings.push(errors[0]?.message || "Failed to parse LogchefQL query.");
        } else {
          parsedAST = ast;

          // Generate SQL conditions from AST
          const visitor = new SQLVisitor(false, schema);
          const { sql } = visitor.generate(ast);
          logchefqlConditions = sql;

          // Convert __VAR_ placeholders back to {{variable}} format for consistent display
          logchefqlConditions = logchefqlConditions.replace(/'__VAR_(\w+)__'/g, '{{$1}}');
          // Also handle cases where quotes might be missing
          logchefqlConditions = logchefqlConditions.replace(/__VAR_(\w+)__/g, '{{$1}}');

          // Add filter operation if we have conditions
          if (logchefqlConditions && !meta.operations.includes('filter')) {
            meta.operations.push('filter');
          }

          // Extract field information from the tokens
          const fieldsUsed = tokens
            .filter(token => token.type === 'key')
            .map(token => token.value)
            .filter((field, index, self) => self.indexOf(field) === index);

          meta.fieldsUsed = fieldsUsed;
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
    // Generate SELECT clause - check if we have custom field selection
    let selectClause = 'SELECT *';

    // Check if the parsed AST has custom select fields (pipe operator was used)
    if (parsedAST && parsedAST.type === 'query' && parsedAST.select) {
      const visitor = new SQLVisitor(false, schema);
      const customSelectClause = visitor.generateSelectClause(parsedAST.select, tsField);
      selectClause = `SELECT ${customSelectClause}`;
    }

    const finalSqlParts = [
      selectClause,
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

  /**
   * Updates the time range in an existing SQL query
   * @param sql The existing SQL query
   * @param tsField The timestamp field name
   * @param newTimeRange The new time range to apply
   * @param timezone Optional timezone (defaults to user timezone)
   * @returns Result object with success status and updated SQL
   */
  static updateTimeRange(
    sql: string,
    tsField: string,
    newTimeRange: TimeRange,
    timezone?: string
  ): { success: boolean; sql?: string; error?: string } {
    try {
      // Validate inputs
      if (!sql || typeof sql !== 'string') {
        return { success: false, error: 'SQL query is required' };
      }
      if (!tsField || typeof tsField !== 'string') {
        return { success: false, error: 'Timestamp field is required' };
      }
      if (!newTimeRange || !newTimeRange.start || !newTimeRange.end) {
        return { success: false, error: 'Valid time range is required' };
      }

      // Delegate to SqlManager which has more robust implementation
      const updatedSql = SqlManager.updateTimeRange({
        sql,
        tsField,
        timeRange: newTimeRange,
        timezone
      });

      return {
        success: true,
        sql: updatedSql
      };

    } catch (error: any) {
      return {
        success: false,
        error: `Failed to update time range: ${error.message}`
      };
    }
  }

  /**
   * Updates the limit in an existing SQL query
   * @param sql The existing SQL query
   * @param limit The new limit value
   * @returns Result object with success status and updated SQL
   */
  static updateLimit(
    sql: string,
    limit: number
  ): { success: boolean; sql?: string; error?: string } {
    try {
      // Validate inputs
      if (!sql || typeof sql !== 'string') {
        return { success: false, error: 'SQL query is required' };
      }
      if (typeof limit !== 'number' || limit <= 0) {
        return { success: false, error: 'Valid limit is required' };
      }

      // Delegate to SqlManager which has robust implementation
      const updatedSql = SqlManager.updateLimit(sql, limit);

      return {
        success: true,
        sql: updatedSql
      };

    } catch (error: any) {
      return {
        success: false,
        error: `Failed to update limit: ${error.message}`
      };
    }
  }
}