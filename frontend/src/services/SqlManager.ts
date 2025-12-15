import { analyzeQuery, validateSQLWithDetails } from '@/utils/clickhouse-sql';
import type { TimeRange } from '@/types/query';
import { createTimeRangeCondition } from '@/utils/time-utils';

/**
 * SqlManager provides a centralized service for all SQL-related operations
 * including parsing, validation, and query modifications
 */
export class SqlManager {
  /**
   * Validates SQL syntax
   * @param sql The SQL query to validate
   * @returns Validation result with detailed error info
   */
  static validateSql(sql: string) {
    return validateSQLWithDetails(sql);
  }

  /**
   * Generate default SQL query based on provided parameters
   * @param params Parameters for SQL generation
   */
  static generateDefaultSql(params: {
    tableName: string;
    tsField: string;
    timeRange: TimeRange;
    limit: number;
    timezone?: string;
  }) {
    const { tableName, tsField, timeRange, limit, timezone } = params;

    // Generate time condition
    const timeCondition = createTimeRangeCondition(tsField, timeRange, true, timezone);

    // Create a basic SQL query
    const sql = `SELECT *
FROM ${tableName}
WHERE ${timeCondition}
ORDER BY \`${tsField}\` DESC
LIMIT ${limit}`;

    return {
      success: true,
      sql,
      warnings: []
    };
  }

  /**
   * Update SQL query with new time range
   * @param params Parameters for updating time range
   */
  static updateTimeRange(params: {
    sql: string;
    tsField: string;
    timeRange: TimeRange;
    timezone?: string;
  }) {
    const { sql, tsField, timeRange, timezone } = params;

    if (!sql.trim()) {
      return sql;
    }

    try {
      const newTimeCondition = createTimeRangeCondition(tsField, timeRange, true, timezone);
      const tsFieldForRegex = tsField.replace(/`/g, '');
      let updatedSql = sql;
      let conditionEffectivelyReplaced = false;

      // Analyze the query structure
      const analysis = analyzeQuery(sql);

      // Case 1: Time range detected with the right format that we can replace
      if (analysis?.timeRangeInfo) {
        const fieldFromAnalysis = analysis.timeRangeInfo.field.replace(/`/g, '');

        // Only replace if:
        // 1. It's for the same field
        // 2. It's using a standard toDateTime BETWEEN format
        if (fieldFromAnalysis.toLowerCase() === tsFieldForRegex.toLowerCase() &&
            analysis.timeRangeInfo.format === 'toDateTime-between') {

          console.log("SqlManager: Found standard toDateTime BETWEEN format that can be safely replaced");

          // Create a precise pattern based on the analyzed time range
          const specificBetweenPattern = new RegExp(
            `\`?${tsFieldForRegex}\`?\\s+BETWEEN\\s+toDateTime\\(['"].*?['"](?:,\\s*['"].*?['"])?\\)\\s+AND\\s+toDateTime\\(['"].*?['"](?:,\\s*['"].*?['"])?\\)`,
            'i'
          );

          if (specificBetweenPattern.test(updatedSql)) {
            updatedSql = updatedSql.replace(specificBetweenPattern, newTimeCondition);
            console.log("SqlManager: Replaced standard toDateTime BETWEEN time range");
            conditionEffectivelyReplaced = true;
          }
        } else if (analysis.timeRangeInfo.format === 'now-interval' ||
                   analysis.timeRangeInfo.format === 'other') {
          // For complex time formats, don't attempt to replace
          console.log(`SqlManager: Found ${analysis.timeRangeInfo.format} time condition format, preserving user query`);
          // Still consider the time condition handled, just not replaced
          conditionEffectivelyReplaced = true;
        }
      }

      // Case 2: No time condition found, add one
      if (!conditionEffectivelyReplaced && !analysis?.hasTimeFilter) {
        console.log("SqlManager: No time condition found, adding new time condition");

        // Check if there's a WHERE clause
        if (updatedSql.toUpperCase().includes('WHERE')) {
          // Add condition to existing WHERE
          const whereClauseRegex = /\bWHERE\b(.*?)(?:\bGROUP BY\b|\bORDER BY\b|\bLIMIT\b|\bHAVING\b|$)/is;
          const match = whereClauseRegex.exec(updatedSql);

          if (match) {
            const whereContent = match[1].trim();
            const whereEndPos = match.index + match[0].length - match[1].length;

            // Insert the new condition after the existing WHERE conditions
            updatedSql =
              updatedSql.substring(0, match.index + 'WHERE'.length) +
              ' ' + whereContent + (whereContent ? ' AND ' : '') + newTimeCondition +
              updatedSql.substring(whereEndPos);

            console.log("SqlManager: Added time condition to existing WHERE clause");
            conditionEffectivelyReplaced = true;
          }
        } else {
          // Add a new WHERE clause before GROUP BY/ORDER BY/LIMIT
          const insertBeforeRegex = /\s+(?:GROUP BY|ORDER BY|LIMIT|HAVING)\s+/i;
          const match = insertBeforeRegex.exec(updatedSql);

          if (match) {
            updatedSql =
              updatedSql.substring(0, match.index) +
              ` WHERE ${newTimeCondition}` +
              updatedSql.substring(match.index);
            console.log("SqlManager: Added new WHERE clause with time condition before clauses");
            conditionEffectivelyReplaced = true;
          } else {
            // If no clauses, add WHERE at the end
            updatedSql = `${updatedSql.trim()} WHERE ${newTimeCondition}`;
            console.log("SqlManager: Added new WHERE clause with time condition at the end");
            conditionEffectivelyReplaced = true;
          }
        }
      }

      console.log("original SQL:", sql);
      console.log("parsed timestamp field:", tsField);
      console.log("new time condition will be inserted:", timeRange);
      console.log("newTimeCondition ",newTimeCondition);
      console.log("tsFieldForRegex",tsFieldForRegex);

      return updatedSql;
    } catch (error) {
      console.error("SqlManager: Error updating SQL with new time range:", error);
      return sql; // Return original SQL on error
    }
  }

  /**
   * Update SQL query with new limit
   * @param sql The SQL query to update
   * @param limit The new limit value
   */
  static updateLimit(sql: string, limit: number) {
    if (!sql.trim()) {
      return sql;
    }

    try {
      // Check if the query has a LIMIT clause
      const limitRegex = /\bLIMIT\s+\d+\s*$/i;
      const hasLimit = limitRegex.test(sql);

      let updatedSql = sql;

      if (hasLimit) {
        // Replace existing LIMIT clause
        updatedSql = sql.replace(limitRegex, `LIMIT ${limit}`);
      } else {
        // Add LIMIT clause at the end
        updatedSql = `${sql.trim()}\nLIMIT ${limit}`;
      }

      return updatedSql;
    } catch (error) {
      console.error("SqlManager: Error updating SQL with new limit:", error);
      return sql; // Return original SQL on error
    }
  }

  /**
   * Ensure SQL query has the correct LIMIT clause
   * Used by sqlForExecution computed property
   */
  static ensureCorrectLimit(sql: string, limit: number) {
    if (!sql || !sql.trim()) {
      return sql || '';
    }

    try {
      // Detect existing LIMIT clause
      const limitRegex = /(\bLIMIT\s+)(\d+)(\s*(?:;|\s|$))/i;
      const match = sql.match(limitRegex);

      // If no match found, need to add a LIMIT clause
      if (!match) {
        // Check if there's a semicolon at the end
        if (sql.trim().endsWith(';')) {
          // Insert before the semicolon
          return `${sql.substring(0, sql.lastIndexOf(';'))} LIMIT ${limit};`;
        } else {
          // Add at the end
          return `${sql.trim()} LIMIT ${limit}`;
        }
      }

      // Found a LIMIT clause - check if it needs updating
      const existingLimit = parseInt(match[2], 10);
      if (existingLimit !== limit) {
        // Replace the existing LIMIT value, preserving whitespace and semicolons
        return sql.replace(limitRegex, `$1${limit}$3`);
      }

      // No change needed
      return sql;
    } catch (error) {
      console.error("SqlManager: Error ensuring correct limit in SQL:", error);
      return sql; // Return original SQL on error
    }
  }

  /**
   * Prepare complete query for execution
   * May add missing time range or limit clauses
   */
  static prepareForExecution(params: {
    sql: string;
    tsField: string;
    timeRange: TimeRange;
    limit: number;
    timezone?: string;
  }) {
    const { sql, tsField, timeRange, limit, timezone } = params;

    if (!sql.trim()) {
      return {
        success: false,
        sql: '',
        error: 'Query is empty',
        warnings: []
      };
    }

    try {
      // Note: Variable substitution should be handled by the caller (useQuery.ts)
      // before calling this method for proper separation of concerns

      // Step 1: Validate SQL first
      const validation = this.validateSql(sql);
      if (!validation.valid) {
        return {
          success: false,
          sql,
          error: validation.error || 'Invalid SQL syntax',
          warnings: []
        };
      }

      // Step 2: Update time range if needed
      let processedSql = this.updateTimeRange({
        sql,
        tsField,
        timeRange,
        timezone
      });

      // Step 3: Ensure correct limit
      processedSql = this.ensureCorrectLimit(processedSql, limit);

      return {
        success: true,
        sql: processedSql,
        error: null,
        warnings: []
      };
    } catch (error) {
      return {
        success: false,
        sql,
        error: error instanceof Error ? error.message : 'Unknown error preparing SQL',
        warnings: []
      };
    }
  }

  /**
   * Creates a valid SQL time range condition
   * @param tsField Timestamp field name
   * @param timeRange Time range to use
   * @param useTimezone Whether to include timezone info
   * @param timezone Optional timezone to use (defaults to UTC)
   */
  static createTimeCondition(params: {
    tsField: string;
    timeRange: TimeRange;
    timezone?: string;
  }) {
    const { tsField, timeRange, timezone } = params;
    return createTimeRangeCondition(tsField, timeRange, true, timezone);
  }
}