import type { CollectionLinkKind } from "@/api/collections";
// Field information for auto-completion and sidebar
export interface FieldInfo {
  name: string;
  type: string;
  isTimestamp?: boolean;
  isSeverity?: boolean;
}

// Query save form data
export interface SaveQueryFormData {
  name: string;
  description: string;
  source_id: number;
  created_from_team_id?: number | null;
  query_language: 'logchefql' | 'clickhouse-sql' | 'logsql';
  editor_mode: 'builder' | 'native';
  query_content: string;
  // Optional collection to pin the query to on save (new queries only).
  collection_id?: number | null;
  // The link kind for that collection. Omitted or null means a pin.
  collection_kind?: CollectionLinkKind | null;
}

// Editor modes
export type EditorMode = 'logchefql' | 'clickhouse-sql';
