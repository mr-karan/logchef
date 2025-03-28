// Field information for auto-completion and sidebar
export interface FieldInfo {
  name: string;
  type: string;
  isTimestamp?: boolean;
  isSeverity?: boolean;
}

// Query execution state
export interface QueryExecutionState {
  timeRange: string;
  limit: number;
  query: string;
}

// Query save form data
export interface SaveQueryFormData {
  team_id: number;
  name: string;
  description: string;
  source_id: number;
  query_type: 'logchefql' | 'sql';
  query_content: string;
}

// Editor modes
export type EditorMode = 'logchefql' | 'clickhouse-sql';

// Query builder options
export interface QueryBuildOptions {
  tableName: string;
  tsField: string;
  startDateTime: CalendarDateTime;
  endDateTime: CalendarDateTime;
  limit: number;
  logchefqlQuery?: string;
}

// Import this to avoid type errors
import { CalendarDateTime } from '@internationalized/date';