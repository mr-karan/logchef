import { logchefqlApi } from '@/api/logchefql';
import {
  createTimeRangeCondition,
  timeRangeToCalendarDateTime,
  formatDateForSQL,
  getUserTimezone
} from '@/utils/time-utils';
import type { QueryOptions, QueryResult, TimeRange } from '@/types/query';
import { SqlManager } from './SqlManager';
import { useTeamsStore } from '@/stores/teams';
import { useSourcesStore } from '@/stores/sources';

// Re-export QueryCondition for backwards compatibility
export type { FilterCondition as QueryCondition } from '@/api/logchefql';

/**
 * Central service for all query generation and manipulation operations
 */
export class QueryService {
  /**
   * Translates a LogchefQL query to SQL using the backend API (async)
   * @param options Query generation options including LogchefQL
   * @returns Promise<QueryResult> with SQL and metadata
   */
  static async translateLogchefQLToSQLAsync(options: QueryOptions): Promise<QueryResult> {
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

    // Create timezone-aware time condition
    const timeCondition = createTimeRangeCondition(tsField, timeRange, true, timezone);
    const limitClause = `LIMIT ${limit}`;

    // --- Translate LogchefQL via backend ---
    const warnings: string[] = [];
    let logchefqlConditions = '';
    let selectClause = 'SELECT *';
    const meta: NonNullable<QueryResult['meta']> = {
      fieldsUsed: [],
      operations: ['sort', 'limit']
    };

    if (logchefqlQuery?.trim()) {
      try {
        // Replace dynamic variables with placeholders while preserving variable names
        const queryForParsing = logchefqlQuery.replace(/{{(\w+)}}/g, "'__VAR_$1__'");

        // Get team and source IDs for API call
        const teamsStore = useTeamsStore();
        const sourcesStore = useSourcesStore();
        const teamId = teamsStore.currentTeamId;
        const sourceId = sourcesStore.currentSourceDetails?.id;

        if (!teamId || !sourceId) {
          warnings.push("Team or source not available for translation");
        } else {
          const response = await logchefqlApi.translate(teamId, sourceId, { query: queryForParsing });
          
          if (response.data) {
            const translateResult = response.data;
            
            if (!translateResult.valid && translateResult.error) {
              warnings.push(translateResult.error.message || "Failed to parse LogchefQL query.");
            } else {
              logchefqlConditions = translateResult.sql || '';
              
              // Convert __VAR_ placeholders back to {{variable}} format
              logchefqlConditions = logchefqlConditions.replace(/'__VAR_(\w+)__'/g, '{{$1}}');
              logchefqlConditions = logchefqlConditions.replace(/__VAR_(\w+)__/g, '{{$1}}');

              // Add filter operation if we have conditions
              if (logchefqlConditions && !meta.operations.includes('filter')) {
                meta.operations.push('filter');
              }

              meta.fieldsUsed = translateResult.fields_used || [];
              meta.conditions = translateResult.conditions?.map((c: { field: string; operator: string; value: string; is_regex?: boolean }) => ({
                field: c.field,
                operator: c.operator,
                value: c.value,
                isRegex: c.is_regex
              }));
            }
          } else if ('status' in response && response.status === 'error') {
            warnings.push(`Translation API error: ${response.message}`);
          }
        }
      } catch (error: any) {
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
   * Synchronous version that returns a promise-based result
   * For backwards compatibility - wraps the async version
   * @deprecated Use translateLogchefQLToSQLAsync instead
   */
  static translateLogchefQLToSQL(options: QueryOptions): QueryResult {
    // For synchronous contexts, return a basic query without LogchefQL translation
    // The async translation should be called separately
    const {
      tableName,
      tsField,
      timeRange,
      limit,
      timezone
    } = options;

    if (!tableName || !tsField || !timeRange.start || !timeRange.end) {
      return { success: false, sql: "", error: "Missing required parameters." };
    }

    const formattedTableName = tableName.includes('`') || tableName.includes('.')
      ? tableName
      : `\`${tableName}\``;

    const formattedTsField = tsField.includes('`') ? tsField : `\`${tsField}\``;
    const timeCondition = createTimeRangeCondition(tsField, timeRange, true, timezone);

    const sql = [
      'SELECT *',
      `FROM ${formattedTableName}`,
      `WHERE ${timeCondition}`,
      `ORDER BY ${formattedTsField} DESC`,
      `LIMIT ${limit}`
    ].join('\n');

    return {
      success: true,
      sql,
      error: null,
      meta: {
        fieldsUsed: [],
        operations: ['sort', 'limit'],
        timeRangeInfo: {
          field: tsField,
          startTime: formatDateForSQL(timeRange.start, false),
          endTime: formatDateForSQL(timeRange.end, false),
          timezone: timezone || getUserTimezone(),
          isTimezoneAware: true
        }
      }
    };
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
      timezone: undefined
    });
  }

  /**
   * Prepares a query for execution by applying time range, limit, and other constraints
   * Now async for LogchefQL mode
   */
  static async prepareQueryForExecutionAsync(params: {
    mode: 'logchefql' | 'clickhouse-sql';
    query: string;
    tableName: string;
    tsField: string;
    timeRange: any;
    limit: number;
    timezone?: string;
  }): Promise<QueryResult> {
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

    // For LogchefQL mode, translate to SQL via backend
    return this.translateLogchefQLToSQLAsync({
      tableName: params.tableName,
      tsField: params.tsField,
      timeRange: params.timeRange,
      limit: params.limit,
      logchefqlQuery: params.query,
      timezone: params.timezone
    });
  }

  /**
   * Synchronous version for backwards compatibility
   * @deprecated Use prepareQueryForExecutionAsync instead
   */
  static prepareQueryForExecution(params: {
    mode: 'logchefql' | 'clickhouse-sql';
    query: string;
    tableName: string;
    tsField: string;
    timeRange: any;
    limit: number;
    timezone?: string;
  }): QueryResult {
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

    // For LogchefQL mode, return basic query (translation happens async)
    return this.translateLogchefQLToSQL({
      tableName: params.tableName,
      tsField: params.tsField,
      timeRange: params.timeRange,
      limit: params.limit,
      logchefqlQuery: '', // Empty - actual translation happens async
      timezone: params.timezone
    });
  }

  /**
   * Updates the time range in an existing SQL query
   */
  static updateTimeRange(
    sql: string,
    tsField: string,
    newTimeRange: TimeRange,
    timezone?: string
  ): { success: boolean; sql?: string; error?: string } {
    try {
      if (!sql || typeof sql !== 'string') {
        return { success: false, error: 'SQL query is required' };
      }
      if (!tsField || typeof tsField !== 'string') {
        return { success: false, error: 'Timestamp field is required' };
      }
      if (!newTimeRange || !newTimeRange.start || !newTimeRange.end) {
        return { success: false, error: 'Valid time range is required' };
      }

      const updatedSql = SqlManager.updateTimeRange({
        sql,
        tsField,
        timeRange: newTimeRange,
        timezone
      });

      return { success: true, sql: updatedSql };
    } catch (error: any) {
      return { success: false, error: `Failed to update time range: ${error.message}` };
    }
  }

  /**
   * Updates the limit in an existing SQL query
   */
  static updateLimit(
    sql: string,
    limit: number
  ): { success: boolean; sql?: string; error?: string } {
    try {
      if (!sql || typeof sql !== 'string') {
        return { success: false, error: 'SQL query is required' };
      }
      if (typeof limit !== 'number' || limit <= 0) {
        return { success: false, error: 'Valid limit is required' };
      }

      const updatedSql = SqlManager.updateLimit(sql, limit);
      return { success: true, sql: updatedSql };
    } catch (error: any) {
      return { success: false, error: `Failed to update limit: ${error.message}` };
    }
  }
}
