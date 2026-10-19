import type { FilterCondition } from '@/api/logchefql';
import type { ResolvedWindow } from '@/utils/timeWindow';

// Re-export for backwards compatibility
export type QueryCondition = FilterCondition;

// Schema info type (moved from logchefql)
export interface SchemaInfo {
  columns: Array<{
    name: string;
    type: string;
  }>;
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
  window: ResolvedWindow;
  limit: number;
  whereConditions?: string[];

  // Optional ordering
  orderBy?: OrderByOptions;

  // LogchefQL (when translating)
  logchefqlQuery?: string;

  // Schema information for type-aware SQL generation
  schema?: SchemaInfo;
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
    conditions?: QueryCondition[];
    /** SQL query analysis results from parser */
    queryAnalysis?: {
      type: string;
      tables: string[];
      columns: string[];
      hasTimeFilter: boolean;
      hasLimit: boolean;
      limitValue?: number;
      timeRangeInfo?: TimeRangeInfo;
    };
    /** Direct access to time range information if available */
    timeRangeInfo?: TimeRangeInfo;
  };
}

/**
 * Time range information extracted from queries
 */
export interface TimeRangeInfo {
  field: string;
  startTime: string;
  endTime: string;
  timezone?: string;
  isTimezoneAware: boolean;
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
