import { quoteClickHouseIdentifier } from "@/utils/clickhouse";
import { sqlTimeCondition, type ResolvedWindow } from '@/utils/timeWindow';

/**
 * SqlManager provides a centralized service for all SQL-related operations
 * including query generation and modifications.
 * Note: SQL validation is handled by the backend (ClickHouse) - client-side
 * validation was removed as it couldn't handle template syntax and used
 * a PostgreSQL parser that doesn't match ClickHouse syntax.
 */
export class SqlManager {
  /**
   * Generate default SQL query based on provided parameters
   * @param params Parameters for SQL generation
   */
  static generateDefaultSql(params: {
    tableName: string;
    tsField: string;
    window: ResolvedWindow;
    limit: number;
  }) {
    const { tableName, tsField, window, limit } = params;

    const timeCondition = sqlTimeCondition(tsField, window);

    // Create a basic SQL query
    const sql = `SELECT *
FROM ${tableName}
WHERE ${timeCondition}
ORDER BY ${quoteClickHouseIdentifier(tsField)} DESC
LIMIT ${limit}`;

    return {
      success: true,
      sql,
      warnings: []
    };
  }
}
