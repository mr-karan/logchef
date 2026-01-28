export { CharType, tokenTypes, SQL_KEYWORDS, CLICKHOUSE_FUNCTIONS, SQL_TYPES, isNumeric } from './language';

export function analyzeQuery(query: string): {
  type: string;
  tables: string[];
  columns: string[];
  hasTimeFilter: boolean;
  hasLimit: boolean;
  limitValue?: number;
  timeRangeInfo?: {
    field: string;
    startTime: string;
    endTime: string;
    timezone?: string;
    isTimezoneAware: boolean;
    format: 'toDateTime-between' | 'now-interval' | 'other';
  };
} | null {
  if (!query || !query.trim()) {
    return null;
  }

  try {
    const queryLower = query.toLowerCase().trim();

    const type = queryLower.startsWith('select') ? 'select' :
                 queryLower.startsWith('insert') ? 'insert' :
                 queryLower.startsWith('update') ? 'update' :
                 queryLower.startsWith('delete') ? 'delete' : 'unknown';

    const hasTimeFilter =
      queryLower.includes('timestamp') ||
      queryLower.includes('datetime') ||
      queryLower.includes('date') ||
      queryLower.includes('time') ||
      queryLower.includes('now(') ||
      queryLower.includes('today(');

    const limitMatch = query.match(/\bLIMIT\s+(\d+)/i);
    const hasLimit = !!limitMatch;
    const limitValue = limitMatch ? parseInt(limitMatch[1], 10) : undefined;

    let timeRangeInfo = undefined;

    const tzTimePattern = /WHERE\s+`?(\w+)`?\s+BETWEEN\s+toDateTime\(['"](.+?)['"],\s*['"]([^'"]+)['"]\)(.*?)AND\s+toDateTime\(['"](.+?)['"],\s*['"]([^'"]+)['"]\)(.*?)(\s|$)/i;
    const tzTimeMatch = query.match(tzTimePattern);

    const basicTimePattern = /WHERE\s+`?(\w+)`?\s+BETWEEN\s+toDateTime\(['"](.+?)[']\)(.*?)AND\s+toDateTime\(['"](.+?)[']\)(.*?)(\s|$)/i;
    const basicTimeMatch = query.match(basicTimePattern);

    const nowIntervalPattern = /WHERE\s+`?(\w+)`?\s*(?:>=|<=|>|<)\s*now\(\s*(?:['"]([^'"]+)['"]\s*)?\)\s*(?:-|\+)\s*INTERVAL\s+(\d+)\s+(\w+)/i;
    const nowIntervalMatch = query.match(nowIntervalPattern);

    const timestampFieldsWithConditions = /WHERE.*?`?(\w+(?:timestamp|time|date)\w*)`?\s*(?:>=|<=|>|<|=|BETWEEN|IN)/i;
    const timestampFieldMatch = query.match(timestampFieldsWithConditions);

    if (tzTimeMatch) {
      timeRangeInfo = {
        field: tzTimeMatch[1],
        startTime: tzTimeMatch[2],
        endTime: tzTimeMatch[5],
        timezone: tzTimeMatch[3],
        isTimezoneAware: true,
        format: 'toDateTime-between' as const
      };
    } else if (basicTimeMatch) {
      timeRangeInfo = {
        field: basicTimeMatch[1],
        startTime: basicTimeMatch[2],
        endTime: basicTimeMatch[4],
        isTimezoneAware: false,
        format: 'toDateTime-between' as const
      };
    } else if (nowIntervalMatch) {
      timeRangeInfo = {
        field: nowIntervalMatch[1],
        startTime: `now() - INTERVAL ${nowIntervalMatch[3]} ${nowIntervalMatch[4]}`,
        endTime: 'now()',
        timezone: nowIntervalMatch[2],
        isTimezoneAware: !!nowIntervalMatch[2],
        format: 'now-interval' as const
      };
    } else if (timestampFieldMatch) {
      timeRangeInfo = {
        field: timestampFieldMatch[1],
        startTime: 'custom',
        endTime: 'custom',
        isTimezoneAware: false,
        format: 'other' as const
      };
    }

    return {
      type,
      tables: [],
      columns: [],
      hasTimeFilter,
      hasLimit,
      limitValue,
      timeRangeInfo
    };
  } catch (error) {
    console.error("Error analyzing query:", error);
    return null;
  }
}
