import { apiClient } from "./apiUtils";
import type { VariableState } from "@/stores/variables";

export interface SavedQueryContent {
  version: number;
  sourceId: number | string;
  timeRange: {
    relative?: string;
    absolute?: {
      start: number;
      end: number;
    };
  } | null;
  limit: number;
  content: string;
  variables?: VariableState[];
}

/**
 * Saved query representation. Cross-team — visibility is via source access
 * through any team membership; edit access is creator + global admin.
 *
 * The legacy is_bookmarked flag is gone in v1.6 — users curate queries via
 * Collections (every user has an auto-created personal collection).
 */
export interface SavedQuery {
  id: number;
  source_id: number;
  created_from_team_id?: number | null;
  name: string;
  description: string;
  query_type: string;
  query_content: string; // JSON string of SavedQueryContent
  created_by?: number | null;
  created_at: string;
  updated_at: string;
  source_name?: string;
  // Per-request authorization hints from the server for the calling user.
  // can_edit reflects delegated collection-editor access; can_delete is
  // creator/global-admin only. Absent when the server didn't compute them.
  can_edit?: boolean;
  can_delete?: boolean;
  // runnable = caller has source access to run it. Set on the admin "all queries"
  // browse list; rows for unreachable sources are shown locked. Absent elsewhere
  // (treat absent as runnable — those lists are already source-access-gated).
  runnable?: boolean;
}

export interface ResolvedSavedQuery extends SavedQuery {
  resolved_team_id: number;
}

/**
 * Team information
 */
export interface Team {
  id: number;
  name: string;
  description?: string;
}

/**
 * Saved Queries API client
 */
export const savedQueriesApi = {
  list: (sourceId?: number) => {
    const url =
      sourceId !== undefined && sourceId !== null
        ? `/saved-queries?source_id=${sourceId}`
        : "/saved-queries";
    return apiClient.get<SavedQuery[]>(url);
  },

  // listAll returns every saved query across all sources (global-admin only),
  // each marked .runnable for the caller. Backs the Library "All queries" view.
  // Admin-scoped endpoint (route-level requireAdmin), separate from the
  // source-gated /saved-queries used by the explorer dropdown + CLI.
  listAll: () => apiClient.get<SavedQuery[]>("/admin/saved-queries"),

  get: (queryId: number | string) =>
    apiClient.get<SavedQuery>(`/saved-queries/${queryId}`),

  create: (query: {
    source_id: number;
    created_from_team_id?: number | null;
    name: string;
    description: string;
    query_type: string;
    query_content: string;
  }) => apiClient.post<SavedQuery>("/saved-queries", query),

  update: (
    queryId: number | string,
    query: Partial<Omit<SavedQuery, "id" | "source_id" | "created_by" | "created_at" | "updated_at">>
  ) => apiClient.put<SavedQuery>(`/saved-queries/${queryId}`, query),

  delete: (queryId: number | string) =>
    apiClient.delete<{ message: string }>(`/saved-queries/${queryId}`),

  resolve: (queryId: number | string, preferredTeamId?: number | string | null) => {
    const suffix = preferredTeamId ? `?team_id=${encodeURIComponent(String(preferredTeamId))}` : "";
    return apiClient.get<ResolvedSavedQuery>(`/saved-queries/${queryId}/resolve${suffix}`);
  },

  getUserTeams: () => apiClient.get<Team[]>("/me/teams"),
};
