import { apiClient, type RequestOptions } from "./apiUtils";
import type { SavedViewDefinition } from "@/lib/savedViewLayout";

export type SavedViewScope = "personal" | "team" | "source";

/** A named Explorer setup for one source (RFC 0007). */
export interface SavedView {
  id: number;
  source_id: number;
  scope: SavedViewScope;
  user_id?: number;
  team_id?: number;
  name: string;
  description: string;
  definition: SavedViewDefinition;
  is_default: boolean;
  managed: boolean;
  revision: number;
  created_by?: number;
  created_at: string;
  updated_at: string;
  can_edit: boolean;
  can_set_default: boolean;
}

/** The views the caller can see for one team and source, personal first, then team, then source. */
export interface SavedViewList {
  views: SavedView[];
  default_view_id: number | null;
}

export interface CreateSavedViewRequest {
  name: string;
  description: string;
  scope: SavedViewScope;
  definition: SavedViewDefinition;
}

export interface UpdateSavedViewRequest {
  name: string;
  description: string;
  definition: SavedViewDefinition;
  revision: number;
}

const base = (teamId: number, sourceId: number) => `/teams/${teamId}/sources/${sourceId}/views`;

export const savedViewsApi = {
  list: (teamId: number, sourceId: number, options?: RequestOptions) =>
    apiClient.get<SavedViewList>(base(teamId, sourceId), options),

  create: (teamId: number, sourceId: number, view: CreateSavedViewRequest) =>
    apiClient.post<SavedView>(base(teamId, sourceId), view),

  update: (teamId: number, sourceId: number, viewId: number, view: UpdateSavedViewRequest) =>
    apiClient.put<SavedView>(`${base(teamId, sourceId)}/${viewId}`, view),

  remove: (teamId: number, sourceId: number, viewId: number) =>
    apiClient.delete<{ message: string }>(`${base(teamId, sourceId)}/${viewId}`),

  setDefault: (teamId: number, sourceId: number, viewId: number) =>
    apiClient.put<SavedView>(`${base(teamId, sourceId)}/${viewId}/default`),

  clearDefault: (teamId: number, sourceId: number, viewId: number) =>
    apiClient.delete<SavedView>(`${base(teamId, sourceId)}/${viewId}/default`),
};
