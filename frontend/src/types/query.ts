import type { DateValue, CalendarDateTime } from '@internationalized/date';

/**
 * Time range representation with start and end dates
 */
export interface TimeRange {
  start: DateValue;
  end: DateValue;
}

/**
 * Options for SQL ordering
 */
export interface OrderByOptions {
  field: string;
  direction: 'ASC' | 'DESC';
}

/**
 * Complete options for SQL query generation
 */
export interface QueryOptions {
  // Table information
  tableName: string;
  tsField: string;

  // Query constraints
  timeRange: TimeRange;
  limit: number;
  whereConditions?: string[];

  // Optional ordering
  orderBy?: OrderByOptions;

  // LogchefQL (when translating)
  logchefqlQuery?: string;
}

/**
 * Result of a query operation with enhanced error handling
 */
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

/**
 * SQL AST representation for parsing and manipulating queries
 */
export interface SQLAst {
  selectClause: {
    columns: string[];
    distinct: boolean;
  };
  fromClause: {
    table: string;
  };
  whereClause: {
    conditions: string[];
    timeCondition?: string;
  };
  groupByClause?: {
    columns: string[];
  };
  orderByClause?: {
    columns: { field: string; direction: 'ASC' | 'DESC' }[];
  };
  limitClause?: {
    limit: number;
  };
}

/**
 * Type for SQL generation approach
 */
export type SqlGenerationMode = 'default' | 'logchefql-translation' | 'time-update';

/**
 * Type for editor mode
 */
export type EditorMode = 'logchefql' | 'clickhouse-sql';