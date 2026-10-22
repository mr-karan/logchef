import { quoteClickHouseIdentifier } from '@/utils/clickhouse';
import { sqlTimeCondition, type ResolvedWindow } from '@/utils/timeWindow';

export function generateDefaultSql(params: {
  tableName: string;
  tsField: string;
  window: ResolvedWindow;
  limit: number;
}): string {
  const { tableName, tsField, window, limit } = params;
  return `SELECT *
FROM ${tableName}
WHERE ${sqlTimeCondition(tsField, window)}
ORDER BY ${quoteClickHouseIdentifier(tsField)} DESC
LIMIT ${limit}`;
}

export const SqlManager = {
  generateDefaultSql(params: Parameters<typeof generateDefaultSql>[0]) {
    return { success: true, sql: generateDefaultSql(params), warnings: [] };
  },
};
